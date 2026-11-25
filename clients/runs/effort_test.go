package runs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corporealshift/rho/protocol"
)

// The run's session takes the runner's effort when its home has none
// (docs/specs/2026-10-16-session-effort-design.md).
func TestTheSessionTakesTheRunnersEffort(t *testing.T) {
	g := newRig(t)
	g.rn.Cfg.Effort = protocol.EffortMedium
	g.started("H1")
	if got := g.d.sessions["S1"].effort; got != protocol.EffortMedium {
		t.Errorf("effort = %q", got)
	}
}

// A home's own effort wins over the runner's.
func TestAHomesEffortBeatsTheRunners(t *testing.T) {
	g := newRig(t)
	g.rn.Cfg.Effort = protocol.EffortMedium
	g.ask("H1", "Add a Median function to stats")
	g.d.homes["H1"].effort = protocol.EffortHigh
	g.started("H1")
	if got := g.d.sessions["S1"].effort; got != protocol.EffortHigh {
		t.Errorf("effort = %q, want the home's high", got)
	}
}

// A goal's run homes copy the goal home's effort, and so pass it on to their
// sessions.
func TestAGoalsRunsCopyItsEffort(t *testing.T) {
	g := newRig(t)
	g.rn.Cfg.Effort = protocol.EffortMedium
	g.askGoal("G1", "Add Median and Mode to stats")
	g.d.homes["G1"].effort = protocol.EffortLow
	g.tick()
	first := g.currentRun(g.goal("G1"))
	if got := g.d.homes[first.Home].effort; got != protocol.EffortLow {
		t.Errorf("run home effort = %q, want low", got)
	}
	g.tick()
	if _, s := g.d.last(); s.effort != protocol.EffortLow {
		t.Errorf("the run's session effort = %q, want low", s.effort)
	}
}

// A level the daemon would refuse stops the runner at load.
func TestAnEffortIsCheckedAtLoad(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "runner"), 0o755)
	os.WriteFile(ConfigPath(root), []byte(`{"effort": "extreme"}`), 0o644)
	if _, err := LoadConfig(root); err == nil || !strings.Contains(err.Error(), "effort") {
		t.Errorf("err = %v, want one naming effort", err)
	}
	os.WriteFile(ConfigPath(root), []byte(`{"effort": "low"}`), 0o644)
	if c, err := LoadConfig(root); err != nil || c.Effort != "low" {
		t.Errorf("config = %+v, %v", c, err)
	}
}
