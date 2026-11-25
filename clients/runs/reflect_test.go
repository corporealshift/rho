package runs

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/corporealshift/rho/protocol"
)

func event(typ protocol.EventType, data any) protocol.Event {
	b, _ := json.Marshal(data)
	return protocol.Event{Type: typ, Data: b}
}

func assistantTurn(text string) protocol.Event {
	return event(protocol.EventMessage, protocol.MessageData{Role: "assistant", Content: text})
}

// askClaude is a claude.ask call and its answer, made by rho when source is
// a module, else by the model.
func askClaude(id, source, prompt, answer string) []protocol.Event {
	args, _ := json.Marshal(map[string]string{"prompt": prompt})
	return []protocol.Event{
		event(protocol.EventToolCall, protocol.ToolCallData{CallID: id, Tool: "claude.ask", Arguments: args, Source: source}),
		event(protocol.EventToolResult, protocol.ToolResultData{CallID: id, Tool: "claude.ask", Content: answer, Status: "ok"}),
	}
}

func reflectedRun() (Run, map[string][]protocol.Event) {
	r := Run{Slug: "add-median-abcd", Brief: "Add a Median function to stats.", PRURL: "https://github.com/kyle/x/pull/7",
		History: []Record{
			{Step: StepSetup, OK: true},
			{Step: StepWork, Session: "S2", Why: "the session ended error (session S2)"},
			{Step: StepWork, Session: "S3", OK: true},
			{Step: StepFinalReview, OK: true, Result: "1 blockers", Note: "- Median of an even count: averages the middle two"},
			{Step: StepCI, OK: true, Result: CIFail, Note: "FAIL TestMedian: got 2, want 2.5"},
		}}
	logs := map[string][]protocol.Event{
		"S2": {assistantTurn("a"), assistantTurn("b"), assistantTurn("c")},
		"S3": append(append([]protocol.Event{assistantTurn("trying")},
			askClaude("c1", "module:claude", "rho is asking on behalf of a smaller local model that has spent 50 turns", "Sort a copy, not the input: sort.Float64s(slices.Clone(xs)).")...),
			askClaude("c2", "", "Which rounding does the brief want?", "Round half to even, as the plan says.")...),
	}
	return r, logs
}

func TestTheDigestTellsTheWholeRun(t *testing.T) {
	r, logs := reflectedRun()
	d := Digest(r, "- [x] Add Median\n  In stats.go.\n", logs)
	for _, want := range []string{
		"Add a Median function to stats.",
		"done: https://github.com/kyle/x/pull/7",
		"Add Median", // the task list
		"work: session S2, 3 turns: did not work: the session ended error",
		"ci: fail", "FAIL TestMedian: got 2, want 2.5",
		"final-review: 1 blockers", "Median of an even count",
		// Each ask, who asked, after how many turns, and the answer in full.
		"rho asked for the model after 1 turns", "Sort a copy, not the input: sort.Float64s(slices.Clone(xs)).",
		"the model asked after 1 turns", "Which rounding does the brief want?", "Round half to even, as the plan says.",
	} {
		if !strings.Contains(d, want) {
			t.Errorf("the digest lacks %q:\n%s", want, d)
		}
	}
	// rho's own ask is the same framing every time: only its answer counts.
	if strings.Contains(d, "rho is asking on behalf") {
		t.Errorf("the digest carries rho's framing of its own ask:\n%s", d)
	}
}

func TestAFailedRunSaysWhereItFailed(t *testing.T) {
	r, logs := reflectedRun()
	r.PRURL, r.FailedAt, r.Why = "", StepCIFix, "CI still fails after 5 fixes"
	if d := Digest(r, "", logs); !strings.Contains(d, "failed at ci-fix: CI still fails after 5 fixes") {
		t.Errorf("the digest does not say how the run ended:\n%s", d)
	}
}

func TestTheDigestIsBounded(t *testing.T) {
	r, logs := reflectedRun()
	for i := range 300 {
		r.History = append(r.History, Record{Step: StepCI, OK: true, Result: CIFail, Note: strings.Repeat("x", maxNote)})
		logs["S3"] = append(logs["S3"], askClaude("k"+string(rune('a'+i%26)), "", "q", strings.Repeat("y", 5000))...)
	}
	d := Digest(r, strings.Repeat("- [ ] t\n", 5000), logs)
	if len(d) > maxDigest+100 {
		t.Errorf("digest is %d bytes, bound %d", len(d), maxDigest)
	}
	// The asks come first: they are what got the model unstuck.
	if !strings.Contains(d, "Sort a copy, not the input") {
		t.Error("the bound cut Claude's answers")
	}
}

func TestReflectPrompt(t *testing.T) {
	p := ReflectPrompt(Run{Slug: "add-median-abcd"}, "THE DIGEST")
	for _, want := range []string{
		"THE DIGEST",
		"what worked", "what to do, not what to avoid",
		"at most 5", "memory.save", "same subject", "Saving nothing is a fine answer",
		"Do not change, create or commit any file",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("lacks %q", want)
		}
	}
}

// reflectingRun takes a run to its reflection, and leaves it there.
func (g *rig) reflectingRun(id string) *Run {
	g.t.Helper()
	g.holdReflect = true
	r := g.started(id)
	g.doAll(r, "p")
	g.tick()
	if r.Step != StepReflect {
		g.t.Fatalf("run at %q, want reflect\nlog:\n%s", r.Step, g.log.String())
	}
	return r
}

// The reflection is one more prompt to the run's session.
func TestARunReflectsBeforeItEnds(t *testing.T) {
	g := newRig(t)
	r := g.reflectingRun("H1")
	s := g.d.sessions["S1"]
	if len(g.d.order) != 1 || len(s.goals) != 1 {
		t.Errorf("sessions %d, goals %q", len(g.d.order), s.goals)
	}
	p := s.prompts[len(s.prompts)-1]
	for _, want := range []string{"How the run went", "Add a Median function to stats", "Test Median", "final-review: 0 blockers", "memory.save"} {
		if !strings.Contains(p, want) {
			t.Errorf("the reflection's prompt lacks %q:\n%s", want, p)
		}
	}
	start := r.Start
	// A reflection that writes a file anyway leaves nothing behind.
	g.finish(map[string]string{"notes.md": "what I learned"})
	g.tick()
	if r.Step != StepDone {
		t.Fatalf("run at %q after its reflection\nlog:\n%s", r.Step, g.log.String())
	}
	if len(g.git.resets) == 0 || g.git.resets[len(g.git.resets)-1] != start {
		t.Errorf("resets = %q, want the worktree back at %s", g.git.resets, start)
	}
	if last := r.History[len(r.History)-1]; last.Step != StepReflect || last.Session != "S1" {
		t.Errorf("the reflection is not in the history: %+v", last)
	}
	if s.state != protocol.StateCompleted {
		t.Errorf("the session was left %s", s.state)
	}
}

func TestAReflectionThatFailsStillEndsTheRun(t *testing.T) {
	g := newRig(t)
	r := g.reflectingRun("H1")
	g.d.sessions["S1"].state = protocol.StateError
	g.tick()
	if r.Step != StepDone || r.FailedAt != "" {
		t.Errorf("run = %q, failed at %q\nlog:\n%s", r.Step, r.FailedAt, g.log.String())
	}
	if n := len(g.d.order); n != 1 {
		t.Errorf("%d sessions: a reflection is never retried", n)
	}
}

func TestAFailedRunReflectsAndStaysFailed(t *testing.T) {
	g := newRig(t)
	g.holdReflect = true
	r := g.started("H1")
	for range sessionTries * (maxNudges + 1) {
		g.finish(nil)
		g.tick()
	}
	if r.Step != StepReflect || r.FailedAt != StepWork {
		t.Fatalf("run = %q, failed at %q", r.Step, r.FailedAt)
	}
	if !strings.Contains(g.log.String(), "failed at work") {
		t.Errorf("the failure is not logged when it happens:\n%s", g.log.String())
	}
	s := g.d.sessions["S1"]
	if p := s.prompts[len(s.prompts)-1]; !strings.Contains(p, "failed at work") || !strings.Contains(p, "did not work") {
		t.Errorf("the digest does not say how the run failed:\n%s", p)
	}
	g.finish(nil)
	g.tick()
	if r.Step != StepFailed || r.FailedAt != StepWork || !strings.Contains(r.Why, "is not written") {
		t.Errorf("run = %+v", r)
	}
}
