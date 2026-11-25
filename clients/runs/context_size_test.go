package runs

import (
	"context"
	"strings"
	"testing"

	"github.com/corporealshift/rho/protocol"
)

// A run's session copies its home's context size
// (docs/specs/2026-10-08-context-size-design.md).
func TestARunsSessionCopiesItsHomesContext(t *testing.T) {
	for _, size := range []string{protocol.ContextLarge, protocol.ContextNormal, ""} {
		t.Run("home "+size, func(t *testing.T) {
			g := newRig(t)
			g.ask("H1", "Add a Median function to stats")
			g.d.homes["H1"].context = size
			g.started("H1")
			if got := g.d.sessions["S1"].context; got != size {
				t.Errorf("session context = %q, want the home's %q", got, size)
			}
		})
	}
}

// A goal's run homes copy the goal home's size, and so pass it on to their
// sessions.
func TestAGoalsRunsCopyItsContext(t *testing.T) {
	g := newRig(t)
	g.askGoal("G1", "Add Median and Mode to stats")
	g.d.homes["G1"].context = protocol.ContextLarge
	g.tick()
	first := g.currentRun(g.goal("G1"))
	if got := g.d.homes[first.Home].context; got != protocol.ContextLarge {
		t.Errorf("run home context = %q, want large", got)
	}
	g.tick()
	_, s := g.d.last()
	if s.context != protocol.ContextLarge {
		t.Errorf("the run's session context = %q, want large", s.context)
	}
}

// A home that cannot be read gives the daemon's default, and says why.
func TestAnUnreadableHomeGivesTheDefault(t *testing.T) {
	g := newRig(t)
	if got := g.rn.settingsOf(context.Background(), g.d, "missing"); got != (Settings{}) {
		t.Errorf("settings = %+v, want none", got)
	}
	if !strings.Contains(g.log.String(), "context size and effort unknown") {
		t.Errorf("nothing logged:\n%s", g.log.String())
	}
}
