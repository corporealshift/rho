package runs

import "fmt"

// The caps on a run.
const (
	// sessionTries is how many times a session step is tried: once, and once
	// more after a failure.
	sessionTries = 2
	// otherTries is how many times a Claude gate or the PR is tried before
	// the run fails, one poll apart.
	otherTries = 3
	// ciTries is how many polls of the checks may fail in a row: ci is a
	// poll, so one failed gh call is not much.
	ciTries = 10
	// MaxCIFixes is how many times the session is asked to fix CI.
	MaxCIFixes = 5
)

// Outcome is what came of doing a run's current step.
type Outcome struct {
	// OK is whether the step did its job: a session that left what its step
	// asks for, a Claude answer that parsed, a PR that opened.
	OK  bool
	Why string
	// Blockers is how many blockers the final review found.
	Blockers int
	// CI is what a poll of the PR's checks found.
	CI string
	// Wait is a step with nothing to do yet, such as checks still running.
	// It costs no attempt. Parked is why a Claude gate waits: Claude is not
	// answering (docs/specs/2026-10-13-claude-park-and-skip-design.md).
	Wait   bool
	Parked string
	// Note is what the step found worth keeping in the run's history.
	Note string
	// Skipped is a Claude gate the owner passed without asking Claude.
	Skipped bool
}

// Transition is the whole workflow: given where a run is and what came of
// its step, where it goes next. It is pure, so every arrow is a test row.
func Transition(r Run, o Outcome) Run {
	if r.Step.Over() {
		return r
	}
	if o.Wait {
		r.Parked = o.Parked
		return r
	}
	r.Parked = ""
	if r.Step == StepReflect {
		// Whatever came of it: the reflection never decides how a run ends,
		// and is not worth a second try.
		return finish(r)
	}
	if !o.OK {
		r.Attempt++
		tries := otherTries
		switch {
		case r.Step.Session():
			tries = sessionTries
		case r.Step == StepCI:
			tries = ciTries
		}
		if r.Attempt >= tries {
			return fail(r, r.Step, o.Why)
		}
		return enter(r, r.Step, false)
	}
	// The gate a skip was given at is passed, so it goes no further.
	r.Skip = false

	switch r.Step {
	case StepSetup:
		return enter(r, StepWork, true)
	case StepWork:
		if r.PR != 0 {
			// Work after the PR opened goes up to it.
			return enter(r, StepPush, true)
		}
		if r.FinalReviewed {
			return enter(r, StepPR, true)
		}
		return enter(r, StepFinalReview, true)
	case StepFinalReview:
		r.FinalReviewed = true
		if o.Blockers > 0 {
			return enter(r, StepWork, true)
		}
		return enter(r, StepPR, true)
	case StepCIFix:
		return enter(r, StepPush, true)
	case StepPR, StepPush:
		return enter(r, StepCI, true)
	case StepMerge:
		return finish(r)
	case StepCI:
		switch o.CI {
		case CIPass:
			if r.Goal != "" {
				return enter(r, StepMerge, true)
			}
			return finish(r)
		case CIMerged:
			return finish(r)
		case CIClosed:
			return fail(r, StepCI, "the PR was closed")
		case CIFail:
			if r.CIFixes >= MaxCIFixes {
				return fail(r, StepCIFix, fmt.Sprintf("CI still fails after %d fixes", MaxCIFixes))
			}
			r.CIFixes++
			return enter(r, StepCIFix, true)
		}
		return r
	}
	return r
}

// enter puts a run at a step. A new step starts its attempts again; a retry
// keeps them. The run's session is kept: a session step prompts it again,
// and waits for a slot to.
func enter(r Run, s Step, fresh bool) Run {
	r.Step = s
	if fresh {
		r.Attempt = 0
	}
	r.Start, r.Prompted, r.Stopped, r.NudgedAt, r.Nudges = "", false, false, 0, 0
	r.Waiting = s.Session()
	return r
}

// fail ends a run at a step, with the reason. The reason is recorded first,
// so a reflection in between hands it on unchanged.
func fail(r Run, at Step, why string) Run {
	r.FailedAt, r.Why = at, why
	return finish(r)
}

// finish ends a run, done or failed as FailedAt says. A run its session
// worked on reflects first, so what it taught is saved before it closes
// (docs/specs/2026-10-11-run-reflection-design.md); one no session touched,
// such as a failed setup, has nothing to reflect on.
func finish(r Run) Run {
	if r.Step != StepReflect && worked(r) {
		return enter(r, StepReflect, true)
	}
	if r.FailedAt != "" {
		return enter(r, StepFailed, true)
	}
	return enter(r, StepDone, true)
}

// worked reports whether any of a run's steps was done by a session.
func worked(r Run) bool {
	for _, rec := range r.History {
		if rec.Session != "" {
			return true
		}
	}
	return false
}

// Resume puts a failed run back at the step it failed at, with that step's
// counters cleared: asking again with /run is asking for another go. A run
// that failed at a step this runner no longer has goes back to work, which
// picks up from the plan and tasks already on its branch.
func Resume(r Run) Run {
	if r.Step != StepFailed {
		return r
	}
	switch {
	case r.FailedAt == StepCIFix:
		// It failed on CI, so it resumes by looking at CI again.
		r.CIFixes = 0
		r = enter(r, StepCI, true)
	case !r.FailedAt.known() || r.FailedAt.Over():
		r = enter(r, StepWork, true)
	default:
		r = enter(r, r.FailedAt, true)
	}
	r.FailedAt, r.Why = "", ""
	return r
}

// upgrade moves a run saved by the runner before it was one session off a
// step that is gone. Work picks up from what is on the branch; a run already
// past its PR goes back to watching CI.
func upgrade(r Run) Run {
	if r.Step.known() {
		return r
	}
	if r.PR != 0 {
		return enter(r, StepCI, true)
	}
	// The old step's session did one step; the new work prompt is for a
	// session that does them all, so it starts afresh.
	r.Session = ""
	return enter(r, StepWork, true)
}
