package tray

import (
	"strings"
	"testing"
	"time"

	"github.com/corporealshift/rho/clients/goclient"
	"github.com/corporealshift/rho/protocol"
)

func summary(id, state string, total, done int, updated time.Time) goclient.SessionSummary {
	return goclient.SessionSummary{
		SessionID: id, State: state, TasksTotal: total, TasksDone: done, UpdatedAt: updated,
		Workspace: "/home/kyle/Projects/rho",
	}
}

func TestPickSessionExplicit(t *testing.T) {
	list := []goclient.SessionSummary{
		summary("sess-a", "running", 3, 1, time.Now()),
		summary("sess-b", "completed", 9, 9, time.Now()),
	}
	got, ok := PickSession(list, "sess-b")
	if !ok || got.SessionID != "sess-b" {
		t.Fatalf("an explicit id must win even when the session is finished: %+v ok=%v", got, ok)
	}
	if _, ok := PickSession(list, "sess-missing"); ok {
		t.Fatal("a session that is not listed must not be reported as found")
	}
}

func TestPickSessionBusiest(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		list []goclient.SessionSummary
		want string
	}{
		{
			name: "running beats everything",
			list: []goclient.SessionSummary{
				summary("idle-many", "idle", 20, 1, now),
				summary("run-few", "running", 2, 1, now.Add(-time.Hour)),
			},
			want: "run-few",
		},
		{
			name: "blocked beats paused",
			list: []goclient.SessionSummary{
				summary("paused", "paused", 5, 0, now),
				summary("blocked", "blocked", 1, 0, now.Add(-time.Hour)),
			},
			want: "blocked",
		},
		{
			name: "more open tasks wins the tie",
			list: []goclient.SessionSummary{
				summary("few-open", "running", 9, 8, now),
				summary("many-open", "running", 4, 1, now.Add(-time.Hour)),
			},
			want: "many-open",
		},
		{
			name: "recency breaks a full tie",
			list: []goclient.SessionSummary{
				summary("stale", "running", 3, 1, now.Add(-3*time.Hour)),
				summary("fresh", "running", 3, 1, now),
			},
			want: "fresh",
		},
		{
			name: "error outranks idle",
			list: []goclient.SessionSummary{
				summary("idle", "idle", 10, 0, now),
				summary("errored", "error", 1, 0, now.Add(-time.Hour)),
			},
			want: "errored",
		},
		{
			name: "nothing but completed still picks one",
			list: []goclient.SessionSummary{
				summary("old", "completed", 1, 1, now.Add(-time.Hour)),
				summary("new", "completed", 1, 1, now),
			},
			want: "new",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := PickSession(c.list, "")
			if !ok {
				t.Fatal("no session picked")
			}
			if got.SessionID != c.want {
				t.Fatalf("picked %s, want %s", got.SessionID, c.want)
			}
		})
	}
	if _, ok := PickSession(nil, ""); ok {
		t.Fatal("an empty list must not pick a session")
	}
}

func TestProgressAndFraction(t *testing.T) {
	v := View{Tasks: []protocol.Task{
		{Status: protocol.TaskDone},
		{Status: protocol.TaskInProgress},
		{Status: protocol.TaskFailed},
		{Status: protocol.TaskDone},
	}}
	done, total := Progress(v)
	if done != 2 || total != 4 {
		t.Fatalf("progress = %d/%d, want 2/4", done, total)
	}
	if f := Fraction(v); f != 0.5 {
		t.Fatalf("fraction = %v, want 0.5", f)
	}
	if f := Fraction(View{}); f != 0 {
		t.Fatalf("a session with no tasks should read as empty, got %v", f)
	}
}

func TestTooltip(t *testing.T) {
	v := View{
		State: protocol.StateRunning, Workspace: "/home/kyle/Projects/rho",
		Tasks: []protocol.Task{{Status: protocol.TaskDone}, {Status: protocol.TaskPending}},
		Turns: 7, MaxTurns: 60, UpdatedAt: time.Now().Add(-90 * time.Second),
	}
	got := Tooltip(v)
	for _, want := range []string{"running", "rho", "tasks 1/2", "turns 7/60", "1m ago"} {
		if !strings.Contains(got, want) {
			t.Errorf("tooltip %q is missing %q", got, want)
		}
	}
	if len([]rune(got)) > tooltipLimit {
		t.Errorf("tooltip is %d runes, over the Windows limit", len([]rune(got)))
	}

	// An unlimited budget shows the turn count without a denominator.
	v.MaxTurns = 0
	if got := Tooltip(v); !strings.Contains(got, "turns 7 ") && !strings.HasSuffix(got, "turns 7") {
		t.Errorf("no budget should still show turns, got %q", got)
	}

	// A long workspace path must not push the useful part off the end.
	v.Workspace = "/" + strings.Repeat("deep/", 40) + "rho"
	if got := Tooltip(v); len([]rune(got)) > tooltipLimit {
		t.Errorf("clamped tooltip is %d runes", len([]rune(got)))
	}

	if got := Tooltip(offline("daemon not reachable")); !strings.Contains(got, "daemon not reachable") {
		t.Errorf("offline tooltip = %q", got)
	}
}

func TestLines(t *testing.T) {
	v := View{
		SessionID: "01JSESSIONID000000000000AA",
		State:     protocol.StateRunning,
		Workspace: "/home/kyle/Projects/rho",
		Tasks: []protocol.Task{
			{Title: "draw the icon", Status: protocol.TaskDone},
			{Title: "watch the daemon", Status: protocol.TaskInProgress},
			{Title: "wire the cli", Status: protocol.TaskPending},
			{Title: "a title long enough that the menu has to cut it off with an ellipsis", Status: protocol.TaskBlocked},
		},
		Turns: 3, MaxTurns: 10,
	}
	lines := Lines(v)
	if lines[0].Kind != LineHeader || !strings.Contains(lines[0].Text, "running") {
		t.Fatalf("header = %+v", lines[0])
	}
	if !strings.Contains(lines[0].Text, "rho") {
		t.Errorf("header should name the workspace, got %q", lines[0].Text)
	}
	var tasks []string
	for _, l := range lines {
		if l.Kind == LineTask {
			tasks = append(tasks, l.Text)
		}
	}
	if len(tasks) != 4 {
		t.Fatalf("got %d task lines, want 4: %q", len(tasks), tasks)
	}
	for i, want := range []string{"✓", "●", "○", "!"} {
		if !strings.HasPrefix(tasks[i], want) {
			t.Errorf("task %d = %q, want the %s glyph", i, tasks[i], want)
		}
	}
	if !strings.HasSuffix(tasks[3], "…") {
		t.Errorf("a long title should be cut with an ellipsis, got %q", tasks[3])
	}
	last := lines[len(lines)-1]
	if last.Kind != LineFooter || last.Text != "1/4 tasks · 3/10 turns" {
		t.Errorf("footer = %+v", last)
	}
}

func TestLinesKeepAFixedShape(t *testing.T) {
	// The tray menu is a fixed pool of items, because a separator cannot be
	// removed once added. Every view must produce the same two separators.
	shape := func(lines []Line) string {
		names := map[LineKind]string{
			LineHeader: "header", LineTask: "task", LineSeparator: "sep", LineFooter: "footer",
		}
		var s []string
		for _, l := range lines {
			s = append(s, names[l.Kind])
		}
		return strings.Join(s, " ")
	}
	views := []View{
		offline("no sessions"),
		{State: protocol.StateIdle},
		{State: protocol.StateRunning, Tasks: []protocol.Task{{Title: "one", Status: protocol.TaskDone}}},
		{State: protocol.StateRunning, Goal: "ship it"},
	}
	for _, v := range views {
		lines := Lines(v)
		last := len(lines) - 1
		// The separators must land in the same slots for every view, or the
		// fixed pool of menu items the tray builds would not line up.
		if lines[0].Kind != LineHeader || lines[1].Kind != LineSeparator ||
			lines[last-1].Kind != LineSeparator || lines[last].Kind != LineFooter {
			t.Errorf("shape of %+v = %s, want header sep [task…] sep footer", v, shape(lines))
			continue
		}
		seps := 0
		for _, l := range lines {
			if l.Kind == LineSeparator {
				seps++
			}
		}
		if seps != 2 {
			t.Errorf("%d separators for %+v, want 2: %s", seps, v, shape(lines))
		}
	}
	// The cap is the other half of the contract: the pool has room for exactly
	// maxTasks rows plus the overflow line.
	full := View{State: protocol.StateRunning}
	for i := 0; i < maxTasks+9; i++ {
		full.Tasks = append(full.Tasks, protocol.Task{Title: "t", Status: protocol.TaskPending})
	}
	got := 0
	for _, l := range Lines(full) {
		if l.Kind == LineTask {
			got++
		}
	}
	if got != maxTasks+1 {
		t.Errorf("%d task rows, want %d", got, maxTasks+1)
	}
}

func TestLinesTruncateLongLists(t *testing.T) {
	v := View{State: protocol.StateRunning}
	for i := 0; i < maxTasks+5; i++ {
		v.Tasks = append(v.Tasks, protocol.Task{Title: "task", Status: protocol.TaskPending})
	}
	lines := Lines(v)
	counted := 0
	var last string
	for _, l := range lines {
		if l.Kind == LineTask {
			counted++
			last = l.Text
		}
	}
	if counted != maxTasks+1 {
		t.Fatalf("got %d task lines, want %d plus the overflow line", counted, maxTasks)
	}
	if !strings.Contains(last, "5 more") {
		t.Errorf("overflow line = %q", last)
	}
}

func TestLinesEmptyAndGoal(t *testing.T) {
	v := View{State: protocol.StateIdle, Tasks: []protocol.Task{}}
	lines := Lines(v)
	if lines[len(lines)-1].Text != "no tasks" {
		t.Errorf("footer = %q, want %q", lines[len(lines)-1].Text, "no tasks")
	}

	v.Goal = "ship the tray icon"
	lines = Lines(v)
	if got := lines[len(lines)-1]; got.Kind != LineFooter || got.Text != "goal: ship the tray icon" {
		t.Errorf("goal footer = %+v", got)
	}

	lines = Lines(offline("no sessions"))
	if lines[0].Text != "rho · offline" {
		t.Errorf("offline header = %q", lines[0].Text)
	}
}

func TestWorkspaceName(t *testing.T) {
	cases := map[string]string{
		"/home/kyle/Projects/rho":    "rho",
		`C:\Users\kyle\Projects\rho`: "rho",
		"/home/kyle/Projects/rho/":   "rho",
		"":                           "",
		"/":                          "",
		"rho":                        "rho",
	}
	for in, want := range cases {
		if got := workspaceName(in); got != want {
			t.Errorf("workspaceName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClampKeepsRunes(t *testing.T) {
	if got := clamp("abcdef", 6); got != "abcdef" {
		t.Errorf("no change expected, got %q", got)
	}
	if got := clamp("abcdef", 4); got != "abc…" {
		t.Errorf("clamp = %q", got)
	}
	// The limit counts characters, not bytes: 127 Japanese characters fit.
	long := strings.Repeat("あ", 200)
	if got := clamp(long, 127); len([]rune(got)) != 127 {
		t.Errorf("clamped to %d runes", len([]rune(got)))
	}
	if got := clamp("one two three", 8); got != "one two…" {
		t.Errorf("trailing space should be trimmed before the ellipsis, got %q", got)
	}
}

func TestTitle(t *testing.T) {
	if got := Title(View{State: protocol.StateBlocked}); got != "rho · blocked" {
		t.Errorf("title = %q", got)
	}
	if got := Title(offline("x")); got != "rho · offline" {
		t.Errorf("offline title = %q", got)
	}
}
