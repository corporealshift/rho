// Package runs is the runner behind `rho runner`: it takes a brief to a green
// pull request. One session does the run's work: it plans, writes its tasks
// and carries them out, and the same session is prompted again for the final
// review's blockers, for failing CI and for the reflection. The runner does
// the mechanical steps itself and asks Claude for the final review
// (docs/specs/2026-11-25-single-session-runs-design.md). It also drives goals:
// a broad goal Claude breaks into briefs, worked as runs one after another on
// a goal branch, and checked until it is met (goal*.go,
// docs/specs/2026-10-06-goals-design.md).
//
// The order of the steps around the work is this package's code; the order
// of the work itself is the session's, written down in plan.md and tasks.md.
//
// It is a client, like the GitHub watcher: it imports goclient, the watcher's
// helpers and protocol, never the daemon. boundary_test.go enforces that.
package runs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Step names where a run is. The names are what Kyle and the labels use.
type Step string

const (
	StepSetup Step = "setup"
	// StepWork is the session planning, writing its tasks and doing them; it
	// comes back here to do the final review's blockers.
	StepWork        Step = "work"
	StepFinalReview Step = "final-review"
	StepPR          Step = "pr"
	StepCI          Step = "ci"
	StepCIFix       Step = "ci-fix"
	StepPush        Step = "push"
	StepMerge       Step = "merge"
	// StepReflect is the run looking back before it ends, done or failed
	// (docs/specs/2026-10-11-run-reflection-design.md).
	StepReflect Step = "reflect"
	StepDone    Step = "done"
	StepFailed  Step = "failed"
)

// Session reports whether a step is done by the run's session.
func (s Step) Session() bool {
	switch s {
	case StepWork, StepCIFix, StepReflect:
		return true
	}
	return false
}

// Claude reports whether a step is one of Claude's gates.
func (s Step) Claude() bool { return s == StepFinalReview }

// Over reports whether a run has finished, either way.
func (s Step) Over() bool { return s == StepDone || s == StepFailed }

// known reports whether a step is one this runner has. A run saved by the
// runner before it was one session can be at a step that is gone.
func (s Step) known() bool {
	switch s {
	case StepSetup, StepWork, StepFinalReview, StepPR, StepCI, StepCIFix, StepPush, StepMerge, StepReflect, StepDone, StepFailed:
		return true
	}
	return false
}

// Run is one brief on its way to a pull request.
type Run struct {
	// Home is the session the run was asked for in; its id is the run's id.
	Home string `json:"home"`
	// Workspace is Kyle's checkout. The run works in Worktree beside it, on
	// Branch, which starts from Base.
	Workspace string `json:"workspace"`
	Slug      string `json:"slug"`
	Branch    string `json:"branch"`
	Base      string `json:"base,omitempty"`
	Worktree  string `json:"worktree"`
	// Goal is the home of the goal this run is one brief of, if any. Such a
	// run starts from the goal's branch, which Base names, opens its PR
	// against it, and is merged into it once green
	// (docs/specs/2026-10-06-goals-design.md).
	Goal string `json:"goal,omitempty"`
	// Brief is the text the run was given, when it was given one; empty
	// means the session writes it from the home's conversation first.
	Brief string `json:"brief,omitempty"`

	Step     Step   `json:"step"`
	FailedAt Step   `json:"failed_at,omitempty"`
	Why      string `json:"why,omitempty"`
	// Attempt counts failed tries of the current step. It starts again
	// whenever the step changes.
	Attempt       int  `json:"attempt,omitempty"`
	CIFixes       int  `json:"ci_fixes,omitempty"`
	FinalReviewed bool `json:"final_reviewed,omitempty"`
	// Blockers is what the final review found, for the session's next
	// prompt; empty once it has been passed on.
	Blockers []Blocker `json:"blockers,omitempty"`

	// Waiting means the current step needs the session and has no slot yet.
	Waiting bool `json:"waiting,omitempty"`
	// Parked is why the current Claude gate is waiting for Claude, which is
	// not answering; empty when it is not
	// (docs/specs/2026-10-13-claude-park-and-skip-design.md).
	Parked string `json:"parked,omitempty"`
	// Skip is the owner's word to pass the current Claude gate without
	// asking Claude, and Skipped every gate passed that way, for the PR
	// (docs/specs/2026-10-13-claude-park-and-skip-design.md).
	Skip    bool   `json:"skip,omitempty"`
	Skipped []Step `json:"skipped,omitempty"`
	// Session is the run's one session. It outlives the steps: each step
	// that needs the model prompts it again, and it is stopped when the run
	// ends. A new one is made only when it has ended some other way.
	Session string `json:"session,omitempty"`
	// Start is the commit the worktree was at when the current step's prompt
	// was sent. Prompted is whether it has been sent, and Stopped whether
	// the step has taken the session's answer as final.
	Start    string `json:"start,omitempty"`
	Prompted bool   `json:"prompted,omitempty"`
	Stopped  bool   `json:"stopped,omitempty"`
	// Fresh is a session made for the run and not prompted yet: it has
	// nothing of the run in its context, so it is told where things stand.
	Fresh bool `json:"fresh,omitempty"`
	// NudgedAt is how long the session's log was when it was last told it
	// had stopped short, and Nudges how many times it has been told during
	// the current step.
	NudgedAt int `json:"nudged_at,omitempty"`
	Nudges   int `json:"nudges,omitempty"`

	// Output is the last CI failure, for the ci-fix prompt.
	Output string `json:"output,omitempty"`

	// Labelled is the run label last put on the home, so it is set only when
	// it changes.
	Labelled string `json:"labelled,omitempty"`

	// CISince is when the run started watching its PR's checks, to give up
	// waiting on a repository that has none.
	CISince time.Time `json:"ci_since,omitzero"`

	// Issue is the GitHub issue the run was asked for, if any, and Reported
	// whether the issue has been told how the run ended.
	Issue    int  `json:"issue,omitempty"`
	Reported bool `json:"reported,omitempty"`

	PR    int      `json:"pr,omitempty"`
	PRURL string   `json:"pr_url,omitempty"`
	Notes []string `json:"notes,omitempty"`

	// History is every step the run has finished, in order, for its
	// reflection (docs/specs/2026-10-11-run-reflection-design.md).
	History []Record `json:"history,omitempty"`

	// Since is when the current step began; SinceKey is the step it was
	// taken for. Statused is the
	// fingerprint of the status last set on the home, so it is set only when
	// it changes (status.go).
	Since    time.Time `json:"since,omitzero"`
	SinceKey string    `json:"since_key,omitempty"`
	Statused string    `json:"statused,omitempty"`

	Started time.Time `json:"started"`
	Updated time.Time `json:"updated"`
}

// Record is one finished step: which, the session that did it, if any, and
// what came of it.
type Record struct {
	Step    Step   `json:"step"`
	Session string `json:"session,omitempty"`
	OK      bool   `json:"ok"`
	Why     string `json:"why,omitempty"`
	// Result is what a step that judges found: how many blockers the final
	// review found, what CI said.
	Result string `json:"result,omitempty"`
	// Note is what the step found worth passing on: the final review's
	// blockers, a CI failure.
	Note string    `json:"note,omitempty"`
	At   time.Time `json:"at"`
}

// The bounds on a run's history: a loop of CI failures would otherwise carry
// every log in full.
const (
	maxNote    = 2000
	maxHistory = 200
)

// cut bounds the note, keeping its end, where a failure explains itself.
func (rec Record) cut() Record {
	if len(rec.Note) > maxNote {
		rec.Note = "(earlier output cut)\n" + rec.Note[len(rec.Note)-maxNote:]
	}
	return rec
}

// Dir is where the run files are committed on the branch, relative to the
// worktree.
func (r Run) Dir() string { return filepath.ToSlash(filepath.Join(".rho", "runs", r.Slug)) }

// File is one of the run's files, relative to the worktree.
func (r Run) File(name string) string { return r.Dir() + "/" + name }

// The run's files. The session writes the brief, when it was given none, the
// plan and the tasks; the runner adds the final review's blockers to the
// tasks.
const (
	BriefFile = "brief.md"
	PlanFile  = "plan.md"
	TasksFile = "tasks.md"
)

// RunsDir holds one state file per run.
func RunsDir(root string) string { return filepath.Join(root, "runner", "runs") }

func runPath(root, home string) string { return filepath.Join(RunsDir(root), home+".json") }

// Save writes a run's state through a temp file and a rename.
func (r Run) Save(root string) error {
	return saveJSON(RunsDir(root), r.Home, r)
}

// Load reads one run; ok is false when there is none.
func Load(root, home string) (Run, bool, error) {
	b, err := os.ReadFile(runPath(root, home))
	if errors.Is(err, fs.ErrNotExist) {
		return Run{}, false, nil
	}
	if err != nil {
		return Run{}, false, fmt.Errorf("runs: %w", err)
	}
	var r Run
	if err := json.Unmarshal(b, &r); err != nil {
		return Run{}, false, fmt.Errorf("runs: %s: %w", runPath(root, home), err)
	}
	return r, true, nil
}

// LoadAll reads every run, in no particular order.
func LoadAll(root string) ([]Run, error) {
	entries, err := os.ReadDir(RunsDir(root))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("runs: %w", err)
	}
	var out []Run
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		r, ok, err := Load(root, strings.TrimSuffix(name, ".json"))
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, r)
		}
	}
	return out, nil
}
