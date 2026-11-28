package tray

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/corporealshift/rho/clients/goclient"
	"github.com/corporealshift/rho/protocol"
)

// View is everything the tray renders, projected from one session.
type View struct {
	SessionID  string
	State      protocol.SessionState
	Workspace  string
	Tasks      []protocol.Task
	Turns      int
	MaxTurns   int
	Goal       string
	LastPrompt string
	UpdatedAt  time.Time

	// Err is why there is nothing to show: a daemon that cannot be reached, or
	// a --session that is no longer listed. It replaces the session lines.
	Err string
}

// offline reports a view that carries no session, only a reason.
func offline(reason string) View {
	return View{State: protocol.StateIdle, Err: reason}
}

// PickSession chooses the session the tray shows. An explicit id wins when it
// is listed; otherwise the busiest session wins, ranked by state, then open
// tasks, then recency. A completed session still outranks nothing: it is the
// last thing worth showing.
func PickSession(list []goclient.SessionSummary, want string) (goclient.SessionSummary, bool) {
	if want != "" {
		for _, s := range list {
			if s.SessionID == want {
				return s, true
			}
		}
		return goclient.SessionSummary{}, false
	}
	var best goclient.SessionSummary
	found := false
	for _, s := range list {
		if !found || better(s, best) {
			best, found = s, true
		}
	}
	return best, found
}

func better(a, b goclient.SessionSummary) bool {
	ra, rb := stateRank(a.State), stateRank(b.State)
	if ra != rb {
		return ra < rb
	}
	if oa, ob := openTasks(a), openTasks(b); oa != ob {
		return oa > ob
	}
	if a.TasksTotal != b.TasksTotal {
		return a.TasksTotal > b.TasksTotal
	}
	return a.UpdatedAt.After(b.UpdatedAt)
}

// stateRank orders states by how much they are worth watching. An errored
// session outranks an idle one because it can be resumed.
func stateRank(s string) int {
	switch protocol.SessionState(s) {
	case protocol.StateRunning:
		return 0
	case protocol.StateBlocked:
		return 1
	case protocol.StatePaused:
		return 2
	case protocol.StateError:
		return 3
	case protocol.StateIdle:
		return 4
	case protocol.StateCompleted:
		return 5
	default:
		return 6
	}
}

func openTasks(s goclient.SessionSummary) int {
	if s.TasksDone > s.TasksTotal {
		return 0
	}
	return s.TasksTotal - s.TasksDone
}

// Progress counts finished tasks over all tasks.
func Progress(v View) (done, total int) {
	for _, t := range v.Tasks {
		if t.Status == protocol.TaskDone {
			done++
		}
	}
	return done, len(v.Tasks)
}

// Fraction is how far through the task list the session is, 0 to 1. A session
// with no tasks is not "done", so it reads as empty.
func Fraction(v View) float64 {
	done, total := Progress(v)
	if total == 0 {
		return 0
	}
	return float64(done) / float64(total)
}

// Title is the short name a desktop shell shows beside the icon.
func Title(v View) string {
	if v.Err != "" {
		return "rho · offline"
	}
	return fmt.Sprintf("rho · %s", v.State)
}

// tooltipLimit is NOTIFYICONDATA.szTip on Windows, minus the terminating NUL.
// Linux shows the whole thing, so the shorter bound decides.
const tooltipLimit = 127

// Tooltip is the one-line summary shown on hover.
func Tooltip(v View) string {
	if v.Err != "" {
		return clamp("rho: "+v.Err, tooltipLimit)
	}
	done, total := Progress(v)
	parts := []string{string(v.State)}
	if w := workspaceName(v.Workspace); w != "" {
		parts = append(parts, w)
	}
	if total > 0 {
		parts = append(parts, fmt.Sprintf("tasks %d/%d", done, total))
	}
	if v.Turns > 0 {
		if v.MaxTurns > 0 {
			parts = append(parts, fmt.Sprintf("turns %d/%d", v.Turns, v.MaxTurns))
		} else {
			parts = append(parts, fmt.Sprintf("turns %d", v.Turns))
		}
	}
	if !v.UpdatedAt.IsZero() {
		parts = append(parts, ago(v.UpdatedAt))
	}
	return clamp("rho · "+strings.Join(parts, " · "), tooltipLimit)
}

const taskLimit = 60

// LineKind says what a menu row is, so the platform backends can render each
// kind the way that platform does.
type LineKind int

const (
	LineHeader LineKind = iota
	LineTask
	LineSeparator
	LineFooter
)

// Line is one read-only row of the tray menu.
type Line struct {
	Kind LineKind
	Text string
}

// Lines are the tray menu's read-only rows, in order.
//
// The shape is fixed: header, separator, task rows, separator, footer. A tray
// toolkit cannot remove a separator, only show and hide ordinary items, so the
// separators are always in the same place and an empty session reads as two
// separators with nothing between them.
func Lines(v View) []Line {
	if v.Err != "" {
		return []Line{
			{LineHeader, "rho · offline"},
			{LineSeparator, ""},
			{LineSeparator, ""},
			{LineFooter, clamp(v.Err, taskLimit)},
		}
	}
	lines := []Line{{LineHeader, header(v)}}
	lines = append(lines, Line{LineSeparator, ""})
	shown := 0
	for _, t := range v.Tasks {
		if shown == maxTasks {
			break
		}
		lines = append(lines, Line{LineTask, clamp(glyph(t.Status)+" "+t.Title, taskLimit)})
		shown++
	}
	if left := len(v.Tasks) - shown; left > 0 {
		lines = append(lines, Line{LineTask, fmt.Sprintf("… %d more", left)})
	}
	if v.Goal != "" {
		lines = append(lines, Line{LineSeparator, ""}, Line{LineFooter, clamp("goal: "+v.Goal, taskLimit)})
	} else {
		lines = append(lines, Line{LineSeparator, ""}, Line{LineFooter, footer(v)})
	}
	return lines
}

// maxTasks bounds the menu. A 40-task list does not fit on screen, and the
// point of the tray is a glance, not the whole plan.
const maxTasks = 12

func header(v View) string {
	w := workspaceName(v.Workspace)
	if w == "" {
		return fmt.Sprintf("rho · %s · %s", v.State, shortID(v.SessionID))
	}
	return fmt.Sprintf("rho · %s · %s", v.State, w)
}

func footer(v View) string {
	done, total := Progress(v)
	switch {
	case total > 0 && v.Turns > 0 && v.MaxTurns > 0:
		return fmt.Sprintf("%d/%d tasks · %d/%d turns", done, total, v.Turns, v.MaxTurns)
	case total > 0:
		return fmt.Sprintf("%d/%d tasks", done, total)
	case v.Turns > 0:
		return fmt.Sprintf("%d turns", v.Turns)
	default:
		return "no tasks"
	}
}

// glyph marks a task's status. These are chosen to exist in the fonts both
// Plasma and Windows ship.
func glyph(s protocol.TaskStatus) string {
	switch s {
	case protocol.TaskDone:
		return "✓"
	case protocol.TaskFailed:
		return "✗"
	case protocol.TaskCancelled:
		return "–"
	case protocol.TaskBlocked:
		return "!"
	case protocol.TaskInProgress:
		return "●"
	default:
		return "○"
	}
}

func workspaceName(p string) string {
	p = strings.TrimRight(p, `/\`)
	if p == "" {
		return ""
	}
	// Both separators, because a Windows path reaches a Linux tray and the
	// other way round when a session is watched from another machine.
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		p = p[i+1:]
	}
	return filepath.Base(p)
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < 0:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return t.Format("2006-01-02")
	}
}

// clamp cuts to n runes, keeping the limit a count of characters rather than
// bytes so a task title in any script fits.
func clamp(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return strings.TrimRight(string(runes[:n-1]), " ") + "…"
}
