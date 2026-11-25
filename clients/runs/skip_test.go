package runs

import (
	"slices"
	"strings"
	"testing"
)

// skip is /skip on a run's home.
func (g *rig) skip(id string) {
	h := g.d.homes[id]
	h.labels = withRun(h.labels, LabelSkip)
}

// skipGoal is /skip on a goal's home.
func (g *rig) skipGoal(id string) {
	h := g.d.homes[id]
	h.labels = withPrefix(h.labels, goalPrefix, LabelGoalSkip)
}

// parkedAtFinalReview is a run waiting at final-review for Claude, which is
// not answering.
func (g *rig) parkedAtFinalReview(id string) *Run {
	g.t.Helper()
	g.claude.answers["final"] = []string{"LIMIT", "LIMIT"}
	r := g.started(id)
	g.doAll(r, "p")
	g.tick()
	if r.Step != StepFinalReview || r.Parked == "" {
		g.t.Fatalf("not parked: %+v", r)
	}
	return r
}

// A skipped final review passes the work as it is, and the PR says Claude did
// not review it.
func TestASkipPassesAParkedGate(t *testing.T) {
	g := newRig(t)
	g.gh.polls = []poll{pending}
	r := g.parkedAtFinalReview("H1")
	asked := len(g.claude.asked)
	g.skip("H1")
	g.tick()

	if r.Step != StepCI || r.Parked != "" || r.Skip || len(r.Notes) != 0 {
		t.Fatalf("run = %+v\nlog:\n%s", r, g.log.String())
	}
	if len(g.claude.asked) != asked {
		t.Errorf("claude was asked at a skipped gate: %q", g.claude.asked[asked:])
	}
	var row Record
	for _, h := range r.History {
		if h.Step == StepFinalReview {
			row = h
		}
	}
	if !row.OK || row.Result != "skipped by the owner" {
		t.Errorf("history row = %+v", row)
	}
	if !slices.Equal(r.Skipped, []Step{StepFinalReview}) {
		t.Errorf("skipped = %v", r.Skipped)
	}
	if labels := g.d.homes["H1"].labels; !slices.Equal(labels, []string{"run:ci"}) {
		t.Errorf("labels = %q", labels)
	}
	log := g.log.String()
	if !strings.Contains(log, "final-review skipped by the owner") || strings.Contains(log, "Claude answered") {
		t.Errorf("log:\n%s", log)
	}
	if body := g.gh.prs[0]; !strings.Contains(body, "The owner skipped Claude's final review") || strings.Contains(body, "Claude reviewed the finished work") {
		t.Errorf("pr = %q", body)
	}
}

// A skip given where there is no gate does nothing, and is not kept for the
// next gate, which the owner has not seen.
func TestASkipOffAGateDoesNothing(t *testing.T) {
	g := newRig(t)
	r := g.started("H1")
	g.skip("H1")
	g.tick()
	if r.Step != StepWork || r.Skip {
		t.Fatalf("run = %+v", r)
	}
	if labels := g.d.homes["H1"].labels; !slices.Equal(labels, []string{"run:work"}) {
		t.Errorf("labels = %q", labels)
	}
	if !strings.Contains(g.log.String(), "not one of Claude's gates; nothing skipped") {
		t.Errorf("log:\n%s", g.log.String())
	}
	g.doAll(r, "p")
	g.tick()
	if r.Step != StepDone || !slices.Contains(g.claude.asked, "final") || len(r.Skipped) != 0 {
		t.Errorf("run = %+v, claude asked %q", r, g.claude.asked)
	}
}

func TestASkippedGoalCheckIsMetAndSaysSo(t *testing.T) {
	g := newRig(t)
	g.claude.answers["breakdown"] = []string{oneBrief}
	g.claude.answers["check"] = []string{"LIMIT", "LIMIT"}
	g.askGoal("G1", "Add Median to stats")
	g.tick()
	goal := g.goal("G1")
	g.work(g.currentRun(goal))
	g.tick() // the check parks
	if goal.Step != GoalCheck || goal.Parked == "" {
		t.Fatalf("goal = %+v", goal)
	}
	g.skipGoal("G1")
	g.tick()

	if goal.Step != GoalDone || goal.Parked != "" || goal.Skip {
		t.Fatalf("goal = %+v\nlog:\n%s", goal, g.log.String())
	}
	if n := len(goal.Verdicts); n != 1 || !goal.Verdicts[0].Met || !goal.Verdicts[0].Skipped {
		t.Errorf("verdicts = %+v", goal.Verdicts)
	}
	if n := strings.Count(strings.Join(g.claude.asked, " "), "check"); n != 1 {
		t.Errorf("claude was asked for the check %d times", n)
	}
	final := g.gh.prs[len(g.gh.prs)-1]
	if !strings.Contains(final, "## Not checked by Claude") || strings.Contains(final, "Claude's verdict") {
		t.Errorf("final pr = %q", final)
	}
}

// The breakdown writes the briefs, so nothing can stand in for it.
func TestAGoalBreakdownCannotBeSkipped(t *testing.T) {
	g := newRig(t)
	g.claude.answers["breakdown"] = []string{"LIMIT", "LIMIT"}
	g.askGoal("G1", "Add Median to stats")
	g.tick()
	goal := g.goal("G1")
	g.skipGoal("G1")
	g.tick()
	if goal.Step != GoalBreakdown || goal.Skip || goal.Parked == "" {
		t.Fatalf("goal = %+v", goal)
	}
	if labels := g.d.homes["G1"].labels; !slices.Equal(labels, []string{"goal:breakdown"}) {
		t.Errorf("labels = %q", labels)
	}
	if !strings.Contains(g.log.String(), "which only the check allows; nothing skipped") {
		t.Errorf("log:\n%s", g.log.String())
	}
}
