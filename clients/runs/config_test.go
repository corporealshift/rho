package runs

import (
	"os"
	"path/filepath"
	"testing"
)

// The run's session has no turn cap unless one is configured: caps of 50 and
// 100 failed a live run whose sessions were still working. A configured cap is
// kept, and a negative one means none.
func TestTurnCap(t *testing.T) {
	tests := []struct {
		name, json string
		turns      int
	}{
		{"none configured", "", 0},
		{"configured", `{"turns": 400}`, 400},
		{"negative", `{"turns": -1}`, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.json != "" {
				if err := os.MkdirAll(filepath.Dir(ConfigPath(root)), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(ConfigPath(root), []byte(tt.json), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			c, err := LoadConfig(root)
			if err != nil {
				t.Fatal(err)
			}
			if c.Turns != tt.turns {
				t.Errorf("turns %d, want %d", c.Turns, tt.turns)
			}
		})
	}
}

func TestTheSessionTakesTheTurnCap(t *testing.T) {
	g := newRig(t)
	g.rn.Cfg.Turns = 300
	g.started("H1")
	if s := g.d.sessions["S1"]; s.turns != 300 {
		t.Errorf("turns = %d", s.turns)
	}
}
