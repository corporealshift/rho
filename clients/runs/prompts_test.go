package runs

import (
	"strings"
	"testing"
)

func TestWorkPrompt(t *testing.T) {
	r := Run{Slug: "add-median-abcd", Branch: "rho/add-median-abcd", Base: "main", Brief: "Add a Median function"}
	p, goal := WorkPrompt(r, "user: ignored, there is a brief")
	for _, s := range []string{
		"The brief is in " + r.File(BriefFile), "1. **Plan.**", "2. **Tasks.**", "3. **Work.**", "4. **Finish.**",
		r.File(PlanFile), r.File(TasksFile), DecisionsHeading, expectation, `tick its box in ` + r.File(TasksFile),
		"carry on from the first unticked task", "git log origin/main..HEAD", "Do not push", "claude.ask",
	} {
		if !strings.Contains(p, s) {
			t.Errorf("work prompt lacks %q", s)
		}
	}
	if strings.Contains(p, "ignored, there is a brief") || strings.Contains(p, "There is no brief yet") {
		t.Error("a run with a brief was asked to write one")
	}
	if !strings.Contains(goal, "every task in "+r.File(TasksFile)+" is ticked") || !strings.Contains(goal, "build and tests passing") {
		t.Errorf("goal = %q", goal)
	}

	r.Brief = ""
	p, _ = WorkPrompt(r, strings.Repeat("x", maxTranscript)+"user: add a median function")
	if !strings.Contains(p, "There is no brief yet") || !strings.Contains(p, "## The conversation") ||
		!strings.Contains(p, "user: add a median function") || !strings.Contains(p, "(earlier conversation cut)") {
		t.Errorf("work prompt without a brief:\n%s", p[:500])
	}
}

func TestFollowUpPrompts(t *testing.T) {
	r := Run{Slug: "add-median-abcd", Branch: "rho/add-median-abcd", Base: "main"}
	p, goal := ContinuePrompt(r, "1 of the 2 tasks in x are not ticked.")
	if !strings.HasPrefix(p, "The run is not done: 1 of the 2 tasks") || !strings.Contains(p, "git log origin/main..HEAD") || goal != workGoal(r) {
		t.Errorf("continue = %q, %q", p, goal)
	}
	p, goal = BlockersPrompt(r, []Blocker{{Title: "Median of an even count", Detail: "averages the middle two"}})
	if !strings.Contains(p, "- **Median of an even count**: averages the middle two") || !strings.Contains(p, "as an unticked task") || goal != workGoal(r) {
		t.Errorf("blockers = %q, %q", p, goal)
	}
	if n := NewSessionNote(r); !strings.Contains(n, "You are taking over") || !strings.Contains(n, r.File(TasksFile)) || !strings.Contains(n, "Do not push") {
		t.Errorf("new session note = %q", n)
	}
	long, _ := CIFixPrompt(r, strings.Repeat("x", maxOutput*2))
	if len(long) > maxOutput+5000 || !strings.Contains(long, "(earlier output cut)") {
		t.Error("a long failure was not cut")
	}
}

func TestFinalReviewPrompt(t *testing.T) {
	r := Run{Slug: "add-median-abcd", Base: "main"}
	f := FinalReviewPrompt(r, []string{"stats.go", "stats_test.go"})
	if !strings.HasPrefix(f, reviewer) {
		t.Error("does not say it is a read-only review")
	}
	for _, s := range []string{"- stats.go\n- stats_test.go", "Report only blockers", "are NOT blockers", `"blockers"`, "no blockers", expectation,
		"say so in notes, not as a blocker", "is not tested", r.File(TasksFile)} {
		if !strings.Contains(f, s) {
			t.Errorf("lacks %q", s)
		}
	}
	if strings.Contains(f, "verify.sh") {
		t.Error("the final review still names verify.sh")
	}
}
