package runs

import (
	"strings"
	"testing"
)

// The run's reflection reads its history: every step, the session that did
// it, and what came of it (docs/specs/2026-10-11-run-reflection-design.md).
func TestARunKeepsItsHistory(t *testing.T) {
	g := newRig(t)
	g.claude.answers["final"] = []string{"```json\n{\"blockers\":[{\"title\":\"Median of an even count\",\"detail\":\"averages the middle two\"}],\"notes\":[]}\n```"}
	g.gh.polls = []poll{failing}
	r := g.started("H1")
	g.doAll(r, "p")
	g.tick() // the final review blocks: work again
	g.finish(map[string]string{r.File(TasksFile): doneTasks + "- [x] Median of an even count\n"})
	g.tick() // pr, ci fails: ci-fix
	g.finish(map[string]string{"stats.go": "gofmt'd"})
	g.tick() // push, ci passes

	var steps []string
	for _, h := range r.History {
		steps = append(steps, string(h.Step))
	}
	want := "setup work final-review work pr ci ci-fix push ci reflect"
	if got := strings.Join(steps, " "); got != want {
		t.Fatalf("history =\n %s\nwant\n %s", got, want)
	}
	h := r.History
	if h[1].Session != "S1" || !h[1].OK || h[2].Session != "" || h[6].Session != "S1" {
		t.Errorf("session records: %+v, %+v, %+v", h[1], h[2], h[6])
	}
	if !strings.Contains(h[2].Note, "Median of an even count") || h[2].Result != "1 blockers" {
		t.Errorf("final review: %+v", h[2])
	}
	if h[5].Result != CIFail || !strings.Contains(h[5].Note, "gofmt -l found stats.go") || h[8].Result != CIPass {
		t.Errorf("ci records: %+v, %+v", h[5], h[8])
	}
}

func TestAFailedTryIsInTheHistory(t *testing.T) {
	g := newRig(t)
	r := g.started("H1")
	g.d.sessions["S1"].state = "error"
	g.tick()
	last := r.History[len(r.History)-1]
	if last.Step != StepWork || last.OK || last.Session != "S1" || !strings.Contains(last.Why, "the session ended error") {
		t.Errorf("the failed try: %+v", last)
	}
}

func TestANoteIsCutFromTheFront(t *testing.T) {
	long := strings.Repeat("early line\n", 1000) + "FAIL TestMedian at the end"
	got := Record{Note: long}.cut().Note
	if len(got) > maxNote+50 || !strings.HasSuffix(got, "FAIL TestMedian at the end") || !strings.HasPrefix(got, "(earlier output cut)") {
		t.Errorf("cut note is %d bytes: %q…", len(got), got[:60])
	}
}
