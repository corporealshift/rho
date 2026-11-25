package runs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/corporealshift/rho/clients/github"
	"github.com/corporealshift/rho/protocol"
)

// ClaudeConfig is how the runner asks Claude.
type ClaudeConfig struct {
	Path    string          `json:"path"`
	Model   string          `json:"model"`
	Timeout github.Duration `json:"timeout"`
}

// Config is <root>/runner/config.json. Every field has a default, so the
// file is optional.
type Config struct {
	// Poll and MaxJobs govern the whole runner process, the GitHub watcher's
	// jobs included when it runs alongside.
	Poll    github.Duration `json:"poll"`
	MaxJobs int             `json:"max_jobs"`
	// Label goes on every PR a run opens, so the comments job answers
	// review comments on it.
	Label  string       `json:"label"`
	Claude ClaudeConfig `json:"claude"`
	// Turns caps the run's session's turns, over the whole run. Zero, the
	// default, is no cap.
	Turns int `json:"turns"`
	// GoalRounds is how many rounds of runs a goal gets before it stops and
	// waits for Kyle. Default 5.
	GoalRounds int `json:"goal_rounds"`
	// Effort is the run's session's effort, when its home has none. Empty
	// leaves it to the server (docs/specs/2026-10-16-session-effort-design.md).
	Effort string `json:"effort"`
}

// ConfigPath is where LoadConfig looks.
func ConfigPath(root string) string { return filepath.Join(root, "runner", "config.json") }

// LoadConfig reads the config, or gives the defaults when there is none.
func LoadConfig(root string) (Config, error) {
	var c Config
	b, err := os.ReadFile(ConfigPath(root))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return Config{}, fmt.Errorf("runs: %w", err)
	default:
		if err := json.Unmarshal(b, &c); err != nil {
			return Config{}, fmt.Errorf("runs: %s: %w", ConfigPath(root), err)
		}
	}
	if err := c.validEffort(); err != nil {
		return Config{}, fmt.Errorf("runs: %s: %w", ConfigPath(root), err)
	}
	c.withDefaults()
	return c, nil
}

func (c *Config) withDefaults() {
	if c.Poll <= 0 {
		c.Poll = github.Duration(2 * time.Minute)
	}
	if c.MaxJobs < 1 {
		c.MaxJobs = 1
	}
	if c.Label == "" {
		c.Label = "rho"
	}
	if c.Claude.Timeout <= 0 {
		c.Claude.Timeout = github.Duration(15 * time.Minute)
	}
	if c.Turns < 0 {
		c.Turns = 0
	}
	if c.GoalRounds < 1 {
		c.GoalRounds = 5
	}
}

// validEffort refuses an effort the daemon would refuse.
func (c Config) validEffort() error {
	if c.Effort == "" {
		return nil
	}
	if err := protocol.ValidEffort(c.Effort); err != nil {
		return fmt.Errorf("effort: %w", err)
	}
	return nil
}
