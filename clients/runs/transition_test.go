package runs

import (
	"reflect"
	"testing"
	"time"
)

func at(s Step) Run { return Run{Home: "H", Step: s} }

// sessionHistory is a history in which the session did some of the run's work.
var sessionHistory = []Record{{Step: StepSetup, OK: true}, {Step: StepWork, Session: "S1", OK: true}}

func TestTransition(t *testing.T) {
	ok := Outcome{OK: true}
	failed := Outcome{Why: "no plan.md"}
	tests := []struct {
		name string
		from Run
		o    Outcome
		want Step
		// check looks at more than the step, when it matters.
		check func(t *testing.T, r Run)
	}{
		{name: "setup goes to work", from: at(StepSetup), o: ok, want: StepWork,
			check: func(t *testing.T, r Run) {
				if !r.Waiting {
					t.Errorf("work waits for a slot: %+v", r)
				}
			}},
		{name: "work goes to the final review", from: at(StepWork), o: ok, want: StepFinalReview},
		{name: "work after the review goes to the PR", from: Run{Step: StepWork, FinalReviewed: true}, o: ok, want: StepPR},
		{name: "work with a PR open goes up to it", from: Run{Step: StepWork, FinalReviewed: true, PR: 7}, o: ok, want: StepPush},
		{name: "final review with blockers", from: at(StepFinalReview), o: Outcome{OK: true, Blockers: 2}, want: StepWork,
			check: func(t *testing.T, r Run) {
				if !r.FinalReviewed || !r.Waiting {
					t.Errorf("the final review happens once, and work waits for a slot: %+v", r)
				}
			}},
		{name: "final review clean", from: at(StepFinalReview), o: ok, want: StepPR},
		{name: "pr goes to ci", from: at(StepPR), o: ok, want: StepCI},
		{name: "ci passes", from: at(StepCI), o: Outcome{OK: true, CI: CIPass}, want: StepDone},
		{name: "ci merged", from: at(StepCI), o: Outcome{OK: true, CI: CIMerged}, want: StepDone},
		{name: "a goal's run passes ci and is merged", from: Run{Step: StepCI, Goal: "G"}, o: Outcome{OK: true, CI: CIPass}, want: StepMerge},
		{name: "a goal's run found merged is done", from: Run{Step: StepCI, Goal: "G"}, o: Outcome{OK: true, CI: CIMerged}, want: StepDone},
		{name: "merge", from: Run{Step: StepMerge, Goal: "G"}, o: ok, want: StepDone},
		{name: "merge fails at the cap", from: Run{Step: StepMerge, Goal: "G", Attempt: otherTries - 1}, o: failed, want: StepFailed,
			check: func(t *testing.T, r Run) {
				if r.FailedAt != StepMerge {
					t.Errorf("failed at %q", r.FailedAt)
				}
			}},
		{name: "ci closed", from: at(StepCI), o: Outcome{OK: true, CI: CIClosed}, want: StepFailed,
			check: func(t *testing.T, r Run) {
				if r.FailedAt != StepCI {
					t.Errorf("failed at %q", r.FailedAt)
				}
			}},
		{name: "ci pending waits", from: Run{Step: StepCI, Attempt: 2}, o: Outcome{Wait: true}, want: StepCI,
			check: func(t *testing.T, r Run) {
				if r.Attempt != 2 {
					t.Errorf("a wait cost an attempt: %d", r.Attempt)
				}
			}},
		{name: "ci fails", from: at(StepCI), o: Outcome{OK: true, CI: CIFail}, want: StepCIFix,
			check: func(t *testing.T, r Run) {
				if r.CIFixes != 1 || !r.Waiting {
					t.Errorf("run = %+v", r)
				}
			}},
		{name: "ci fails at the cap", from: Run{Step: StepCI, CIFixes: MaxCIFixes}, o: Outcome{OK: true, CI: CIFail}, want: StepFailed,
			check: func(t *testing.T, r Run) {
				if r.FailedAt != StepCIFix {
					t.Errorf("failed at %q", r.FailedAt)
				}
			}},
		{name: "a ci poll that errors is tried ten times", from: Run{Step: StepCI, Attempt: 8}, o: failed, want: StepCI},
		{name: "and fails on the tenth", from: Run{Step: StepCI, Attempt: 9}, o: failed, want: StepFailed},
		{name: "ci-fix goes to push", from: at(StepCIFix), o: ok, want: StepPush},
		{name: "push goes to ci", from: at(StepPush), o: ok, want: StepCI},

		{name: "a session step is retried once", from: at(StepWork), o: failed, want: StepWork,
			check: func(t *testing.T, r Run) {
				if r.Attempt != 1 || !r.Waiting {
					t.Errorf("retry: %+v", r)
				}
			}},
		{name: "then fails", from: Run{Step: StepWork, Attempt: 1}, o: failed, want: StepFailed,
			check: func(t *testing.T, r Run) {
				if r.FailedAt != StepWork || r.Why != "no plan.md" {
					t.Errorf("failed at %q, why %q", r.FailedAt, r.Why)
				}
			}},
		{name: "a claude gate gets three tries", from: Run{Step: StepFinalReview, Attempt: 1}, o: failed, want: StepFinalReview},
		{name: "and fails on the third", from: Run{Step: StepFinalReview, Attempt: 2}, o: failed, want: StepFailed},
		{name: "claude not answering parks a gate", from: Run{Step: StepFinalReview, Attempt: 2}, o: Outcome{Wait: true, Parked: "limit"}, want: StepFinalReview,
			check: func(t *testing.T, r Run) {
				if r.Attempt != 2 || r.Parked != "limit" {
					t.Errorf("run = %+v", r)
				}
			}},
		{name: "an answer ends the park", from: Run{Step: StepFinalReview, Parked: "limit"}, o: ok, want: StepPR,
			check: func(t *testing.T, r Run) {
				if r.Parked != "" {
					t.Errorf("still parked: %q", r.Parked)
				}
			}},
		{name: "a skip is spent at its gate", from: Run{Step: StepFinalReview, Skip: true, Parked: "limit"}, o: Outcome{OK: true, Skipped: true}, want: StepPR,
			check: func(t *testing.T, r Run) {
				if r.Skip || r.Parked != "" {
					t.Errorf("run = %+v", r)
				}
			}},
		{name: "so does a failed try", from: Run{Step: StepFinalReview, Parked: "limit"}, o: failed, want: StepFinalReview,
			check: func(t *testing.T, r Run) {
				if r.Parked != "" || r.Attempt != 1 {
					t.Errorf("run = %+v", r)
				}
			}},
		{name: "the pr gets three tries", from: Run{Step: StepPR, Attempt: 2}, o: failed, want: StepFailed},
		{name: "a finished run stays finished", from: at(StepDone), o: failed, want: StepDone},
		// A run reflects before it ends, when its session did any of its work
		// (docs/specs/2026-10-11-run-reflection-design.md).
		{name: "ci passes after work: reflect", from: Run{Step: StepCI, History: sessionHistory}, o: Outcome{OK: true, CI: CIPass}, want: StepReflect,
			check: func(t *testing.T, r Run) {
				if r.FailedAt != "" || !r.Waiting {
					t.Errorf("a reflection waits for a slot, and a merged run is no failure: %+v", r)
				}
			}},
		{name: "merge after work: reflect", from: Run{Step: StepMerge, Goal: "G", History: sessionHistory}, o: ok, want: StepReflect},
		{name: "a failure after work reflects first", from: Run{Step: StepCI, CIFixes: MaxCIFixes, History: sessionHistory}, o: Outcome{OK: true, CI: CIFail}, want: StepReflect,
			check: func(t *testing.T, r Run) {
				if r.FailedAt != StepCIFix || r.Why == "" {
					t.Errorf("the failure is recorded before the reflection: failed at %q, why %q", r.FailedAt, r.Why)
				}
			}},
		{name: "a reflection ends a merged run", from: Run{Step: StepReflect, History: sessionHistory}, o: ok, want: StepDone},
		{name: "a reflection ends a failed run as it failed", from: Run{Step: StepReflect, FailedAt: StepCIFix, Why: "still failing", History: sessionHistory}, o: ok, want: StepFailed,
			check: func(t *testing.T, r Run) {
				if r.FailedAt != StepCIFix || r.Why != "still failing" {
					t.Errorf("failed at %q, why %q", r.FailedAt, r.Why)
				}
			}},
		{name: "a reflection that did not work still ends the run", from: Run{Step: StepReflect, History: sessionHistory}, o: failed, want: StepDone},
		{name: "and is not retried on a failed run", from: Run{Step: StepReflect, FailedAt: StepWork, Why: "y", History: sessionHistory}, o: failed, want: StepFailed,
			check: func(t *testing.T, r Run) {
				if r.Why != "y" {
					t.Errorf("why = %q: the reflection's own failure replaced the run's", r.Why)
				}
			}},
		// The run's session outlives its steps; only the step's own state goes.
		{name: "a new step keeps the session", from: Run{Step: StepWork, Session: "S", Start: "abc", Prompted: true, Stopped: true, NudgedAt: 9, Nudges: 2},
			o: ok, want: StepFinalReview,
			check: func(t *testing.T, r Run) {
				if r.Session != "S" || r.Start != "" || r.Prompted || r.Stopped || r.NudgedAt != 0 || r.Nudges != 0 || r.Waiting {
					t.Errorf("run = %+v", r)
				}
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Transition(tt.from, tt.o)
			if got.Step != tt.want {
				t.Fatalf("step = %q, want %q", got.Step, tt.want)
			}
			if tt.check != nil {
				tt.check(t, got)
			}
		})
	}
}

// Blockers go back to work once: the second time the work is done, it goes to
// the PR without another review.
func TestBlockersBounceOnce(t *testing.T) {
	r := at(StepFinalReview)
	r = Transition(r, Outcome{OK: true, Blockers: 1})
	r = Transition(r, Outcome{OK: true}) // the blockers' work
	if r.Step != StepPR {
		t.Errorf("after the blockers the run went to %q, want pr", r.Step)
	}
}

func TestResume(t *testing.T) {
	tests := []struct {
		failedAt Step
		want     Step
	}{
		{StepWork, StepWork},
		{StepFinalReview, StepFinalReview},
		{StepPR, StepPR},
		{StepCIFix, StepCI},
		// Steps the runner had before it was one session.
		{"verify", StepWork},
		{"fix", StepWork},
		{"replan-review", StepWork},
	}
	for _, tt := range tests {
		r := Run{Step: StepFailed, FailedAt: tt.failedAt, Why: "x", Attempt: 2, CIFixes: MaxCIFixes, Session: "S"}
		got := Resume(r)
		if got.Step != tt.want || got.FailedAt != "" || got.Why != "" || got.Attempt != 0 || got.Waiting != tt.want.Session() || got.Session != "S" {
			t.Errorf("resume from %q = %+v", tt.failedAt, got)
		}
		if tt.failedAt == StepCIFix && got.CIFixes != 0 {
			t.Errorf("ci fixes = %d", got.CIFixes)
		}
	}
	if r := Resume(at(StepWork)); r.Step != StepWork {
		t.Error("resuming a run that has not failed changed it")
	}
}

func TestUpgrade(t *testing.T) {
	for _, tt := range []struct {
		from Run
		want Step
	}{
		{Run{Step: "plan", Session: "OLD"}, StepWork},
		{Run{Step: "check", Session: "OLD"}, StepWork},
		{Run{Step: "check", PR: 7}, StepCI},
		{Run{Step: StepWork, Session: "S"}, StepWork},
		{Run{Step: StepFailed, FailedAt: "verify"}, StepFailed},
	} {
		got := upgrade(tt.from)
		if got.Step != tt.want {
			t.Errorf("upgrade(%q) = %q, want %q", tt.from.Step, got.Step, tt.want)
		}
		if !tt.from.Step.known() && got.Step == StepWork && (got.Session != "" || !got.Waiting) {
			t.Errorf("an old step's session does one step, so work starts a new one: %+v", got)
		}
		if tt.from.Step == StepWork && got.Session != "S" {
			t.Errorf("a run at work keeps its session: %+v", got)
		}
	}
}

func TestRunRoundTrip(t *testing.T) {
	root := t.TempDir()
	if _, ok, err := Load(root, "H"); ok || err != nil {
		t.Fatalf("missing run: %v, %v", ok, err)
	}
	r := Run{Home: "H", Workspace: "C:/src/x", Slug: "add-x-0001", Branch: "rho/add-x-0001", Worktree: "w",
		Step: StepCIFix, CIFixes: 3, Session: "S1", Start: "abc", Notes: []string{"n"}, Blockers: []Blocker{{Title: "t", Detail: "d"}},
		Started: time.Date(2026, 9, 30, 1, 0, 0, 0, time.UTC), Updated: time.Date(2026, 9, 30, 2, 0, 0, 0, time.UTC)}
	if err := r.Save(root); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Load(root, "H")
	if err != nil || !ok || !reflect.DeepEqual(got, r) {
		t.Fatalf("round trip = %+v, %v, %v", got, ok, err)
	}
	all, err := LoadAll(root)
	if err != nil || len(all) != 1 {
		t.Fatalf("LoadAll = %d, %v", len(all), err)
	}
	if r.File(PlanFile) != ".rho/runs/add-x-0001/plan.md" {
		t.Errorf("File = %q", r.File(PlanFile))
	}
}
