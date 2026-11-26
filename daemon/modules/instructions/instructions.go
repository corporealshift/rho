// Package instructions puts a repository's own instructions for agents in
// front of the model: rho.md, the file a person writes to tell rho how their
// project works.
//
// Claude Code reads CLAUDE.md, and other agents read AGENTS.md, but rho read
// nothing of the kind. What a repository wanted every session to know went
// into memory by hand, where it lived outside the repository and was neither
// reviewed nor versioned with the code it described. rho.md is that file in
// the repository. It is the only name read: one file with one name is easier
// to find, and to know is in effect, than a list of fallbacks.
//
// # Which file
//
// The search starts at the session's workspace and walks up to the repository
// root, the first directory holding .git, looking in each directory for
// rho.md in any case (RHO.md, Rho.md). The first found is the one used. So a
// subdirectory's rho.md wins over the root's, and a worktree
// reads its own branch's copy. Outside a repository only the workspace itself
// is searched: walking up from a home directory would find whatever happens to
// sit above it.
//
// # When
//
// A prefix block, at session start and again after each summarize retires the
// prefix, like memory and the date. The file is read at those two points only,
// so an edit during a session reaches the model at the next summarize, and the
// prefix stays stable in between (architecture invariant 6).
package instructions

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/corporealshift/rho/daemon/module"
)

// defaultMaxBytes bounds what the file costs in every request. A file past
// it is cut, and the block says so, because instructions the model cannot see
// are worse than none it believes complete.
const defaultMaxBytes = 16 << 10

// FileName is the file looked for, ignoring case.
const FileName = "rho.md"

// Module is the instructions module.
type Module struct {
	enabled  bool
	maxBytes int
	log      interface{ Warn(string, ...any) }
}

func (m *Module) Name() string { return "instructions" }

func (m *Module) Init(h module.Host, cfg module.Config) error {
	m.enabled = cfg.Enabled()
	if m.maxBytes = cfg.Int("max_bytes", defaultMaxBytes); m.maxBytes <= 0 {
		m.maxBytes = defaultMaxBytes
	}
	if h != nil {
		m.log = h.Log()
	}
	return nil
}

// SessionStart implements module.SessionStarter.
func (m *Module) SessionStart(_ context.Context, s module.Session) ([]module.ContextBlock, error) {
	return m.blocks(s), nil
}

// AfterCompaction implements module.CompactionHook. A summarize retires every
// prefix block, so the instructions go back in, read afresh.
func (m *Module) AfterCompaction(_ context.Context, s module.Session) ([]module.ContextBlock, error) {
	return m.blocks(s), nil
}

// BeforeCompaction implements module.CompactionHook. Nothing to keep in the
// summary: the file is put back afterwards.
func (m *Module) BeforeCompaction(context.Context, module.Session, module.Range) []string {
	return nil
}

func (m *Module) blocks(s module.Session) []module.ContextBlock {
	if !m.enabled || s == nil {
		return nil
	}
	ws := s.Workspace().Path
	if ws == "" {
		return nil
	}
	path := Find(ws)
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		m.warn("instructions: cannot read file", "path", path, "error", err)
		return nil
	}
	body := strings.TrimSpace(string(raw))
	if body == "" {
		return nil
	}
	return []module.ContextBlock{{Slot: "prefix", Content: Render(rel(ws, path), body, m.maxBytes)}}
}

// Find returns the instructions file for a workspace, or "" when there is
// none. See the package comment for the search.
func Find(ws string) string {
	ws = filepath.Clean(ws)
	root := repoRoot(ws)
	for dir := ws; ; {
		if p := match(dir); p != "" {
			return p
		}
		if root == "" || dir == root {
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// repoRoot is the nearest directory at or above dir that holds .git, a
// directory in a clone and a file in a worktree, or "" outside a repository.
func repoRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// match returns rho.md in dir as a regular file, ignoring case, so RHO.md,
// rho.md and Rho.md are all found on a case-sensitive disk.
func match(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !strings.EqualFold(e.Name(), FileName) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			return p
		}
	}
	return ""
}

// Render is the block the model sees: where the instructions came from, then
// the file, cut at maxBytes on a rune boundary.
func Render(name, body string, maxBytes int) string {
	cut := false
	if len(body) > maxBytes {
		body = body[:maxBytes]
		for !utf8.ValidString(body) {
			body = body[:len(body)-1]
		}
		cut = true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## Project instructions\n\nFrom `%s`, written by the people who work in this repository. "+
		"Follow them; where they conflict with a general habit of yours, they win.\n\n", filepath.ToSlash(name))
	b.WriteString(body)
	b.WriteString("\n")
	if cut {
		fmt.Fprintf(&b, "\n(Cut at %d bytes. Read `%s` for the rest.)\n", maxBytes, filepath.ToSlash(name))
	}
	return b.String()
}

// rel names the file relative to the workspace when it is inside or above
// it, which is how the model's own tools will name it.
func rel(ws, path string) string {
	if r, err := filepath.Rel(ws, path); err == nil {
		return r
	}
	return path
}

func (m *Module) warn(msg string, args ...any) {
	if m.log != nil {
		m.log.Warn(msg, args...)
	}
}
