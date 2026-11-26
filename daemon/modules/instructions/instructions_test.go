package instructions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corporealshift/rho/daemon/module"
	"github.com/corporealshift/rho/protocol"
)

type fakeSession struct{ dir string }

func (f fakeSession) ID() string                               { return "S1" }
func (f fakeSession) Workspace() module.Workspace              { return module.Workspace{Path: f.dir, Key: "k"} }
func (f fakeSession) Events(*string) ([]protocol.Event, error) { return nil, nil }
func (f fakeSession) State() protocol.State                    { return protocol.State{} }
func (f fakeSession) Append(protocol.EventType, any) (protocol.Event, error) {
	return protocol.Event{}, nil
}

// tree writes files under a fresh root; a name ending in / is a directory.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestFind(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		ws    string   // workspace, relative to the root
		names []string // nil means the default
		want  string   // relative to the root, "" for none
	}{
		{
			name:  "repo root",
			files: map[string]string{".git/": "", "RHO.md": "x"},
			ws:    ".", want: "RHO.md",
		},
		{
			name:  "any case",
			files: map[string]string{".git/": "", "rho.md": "x"},
			ws:    ".", want: "rho.md",
		},
		{
			name:  "walks up from a subdirectory to the root",
			files: map[string]string{".git/": "", "RHO.md": "x", "app/src/": ""},
			ws:    "app/src", want: "RHO.md",
		},
		{
			name:  "nearest wins",
			files: map[string]string{".git/": "", "RHO.md": "root", "app/RHO.md": "app", "app/src/": ""},
			ws:    "app/src", want: "app/RHO.md",
		},
		{
			name:  "worktree: .git is a file",
			files: map[string]string{"wt/.git": "gitdir: elsewhere", "wt/RHO.md": "x"},
			ws:    "wt", want: "wt/RHO.md",
		},
		{
			name:  "never above the repository root",
			files: map[string]string{"RHO.md": "outside", "repo/.git/": ""},
			ws:    "repo", want: "",
		},
		{
			name:  "outside a repository only the workspace is searched",
			files: map[string]string{"RHO.md": "above", "home/": ""},
			ws:    "home", want: "",
		},
		{
			name:  "outside a repository, the workspace's own file",
			files: map[string]string{"home/RHO.md": "x"},
			ws:    "home", want: "home/RHO.md",
		},
		{
			name:  "CLAUDE.md is not read by default",
			files: map[string]string{".git/": "", "CLAUDE.md": "x"},
			ws:    ".", want: "",
		},
		{
			name:  "configured order: the first name found wins in a directory",
			files: map[string]string{".git/": "", "CLAUDE.md": "c", "AGENTS.md": "a"},
			ws:    ".", names: []string{"RHO.md", "AGENTS.md", "CLAUDE.md"}, want: "AGENTS.md",
		},
		{
			name:  "a directory with the name is not a file",
			files: map[string]string{".git/": "", "RHO.md/": ""},
			ws:    ".", want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := tree(t, tt.files)
			names := tt.names
			if names == nil {
				names = defaultFiles
			}
			got := Find(filepath.Join(root, filepath.FromSlash(tt.ws)), names)
			want := ""
			if tt.want != "" {
				want = filepath.Join(root, filepath.FromSlash(tt.want))
			}
			if got != want {
				t.Errorf("Find = %q, want %q", got, want)
			}
		})
	}
}

func TestBlocks(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		cfg     module.Config
		want    []string // substrings of the one block; nil means no block
		notWant []string
	}{
		{
			name:  "the file goes in as a prefix block",
			files: map[string]string{".git/": "", "RHO.md": "Use the toolchain in ~/android-toolchain.\n"},
			want:  []string{"## Project instructions", "From `RHO.md`", "Use the toolchain in ~/android-toolchain."},
		},
		{
			name:  "no file, no block",
			files: map[string]string{".git/": ""},
		},
		{
			name:  "an empty file, no block",
			files: map[string]string{".git/": "", "RHO.md": " \n\n"},
		},
		{
			name:  "disabled",
			files: map[string]string{".git/": "", "RHO.md": "x"},
			cfg:   module.Config{"enabled": false},
		},
		{
			name:  "configured files",
			files: map[string]string{".git/": "", "AGENTS.md": "agents say hi"},
			cfg:   module.Config{"files": []any{"RHO.md", "AGENTS.md"}},
			want:  []string{"From `AGENTS.md`", "agents say hi"},
		},
		{
			name:  "a path in files is ignored",
			files: map[string]string{".git/": "", "docs/RHO.md": "nested", "RHO.md": "root"},
			cfg:   module.Config{"files": []any{"docs/RHO.md"}},
			want:  []string{"root"}, notWant: []string{"nested"},
		},
		{
			name:    "cut at max_bytes, and says so",
			files:   map[string]string{".git/": "", "RHO.md": strings.Repeat("a", 40) + "TAIL"},
			cfg:     module.Config{"max_bytes": 40},
			want:    []string{"(Cut at 40 bytes. Read `RHO.md` for the rest.)"},
			notWant: []string{"TAIL"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := tree(t, tt.files)
			m := &Module{}
			cfg := tt.cfg
			if cfg == nil {
				cfg = module.Config{}
			}
			if err := m.Init(nil, cfg); err != nil {
				t.Fatal(err)
			}
			s := fakeSession{dir: root}
			for hook, run := range map[string]func() ([]module.ContextBlock, error){
				"SessionStart":    func() ([]module.ContextBlock, error) { return m.SessionStart(context.Background(), s) },
				"AfterCompaction": func() ([]module.ContextBlock, error) { return m.AfterCompaction(context.Background(), s) },
			} {
				blocks, err := run()
				if err != nil {
					t.Fatalf("%s: %v", hook, err)
				}
				if tt.want == nil {
					if len(blocks) != 0 {
						t.Fatalf("%s: got %d blocks, want none: %q", hook, len(blocks), blocks)
					}
					continue
				}
				if len(blocks) != 1 || blocks[0].Slot != "prefix" {
					t.Fatalf("%s: got %+v, want one prefix block", hook, blocks)
				}
				for _, w := range tt.want {
					if !strings.Contains(blocks[0].Content, w) {
						t.Errorf("%s: block lacks %q:\n%s", hook, w, blocks[0].Content)
					}
				}
				for _, w := range tt.notWant {
					if strings.Contains(blocks[0].Content, w) {
						t.Errorf("%s: block has %q:\n%s", hook, w, blocks[0].Content)
					}
				}
			}
		})
	}
}

func TestAnEditReachesTheModelAtTheNextSummarize(t *testing.T) {
	root := tree(t, map[string]string{".git/": "", "RHO.md": "first"})
	m := &Module{}
	if err := m.Init(nil, module.Config{}); err != nil {
		t.Fatal(err)
	}
	s := fakeSession{dir: root}
	start, _ := m.SessionStart(context.Background(), s)
	if err := os.WriteFile(filepath.Join(root, "RHO.md"), []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, _ := m.AfterCompaction(context.Background(), s)
	if !strings.Contains(start[0].Content, "first") || !strings.Contains(after[0].Content, "second") {
		t.Errorf("start %q, after compaction %q", start[0].Content, after[0].Content)
	}
}

func TestRenderCutsOnARuneBoundary(t *testing.T) {
	out := Render("RHO.md", "ab€", 4) // € is 3 bytes: a cut at 4 lands inside it
	if !strings.Contains(out, "ab\n") || strings.ContainsRune(out, '\uFFFD') {
		t.Errorf("Render = %q", out)
	}
}
