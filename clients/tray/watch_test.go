package tray

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/corporealshift/rho/clients/goclient"
	"github.com/corporealshift/rho/daemon/api"
	"github.com/corporealshift/rho/daemon/session"
	"github.com/corporealshift/rho/protocol"
)

func TestPollStateFetchesOnlyWhenTheSummaryMoves(t *testing.T) {
	var state pollState
	calls := 0
	fetch := func(string) (protocol.State, error) {
		calls++
		return protocol.State{State: protocol.StateRunning, Tasks: []protocol.Task{{ID: "1"}}}, nil
	}
	now := time.Now().UTC()
	s := goclient.SessionSummary{
		SessionID: "sess-a", State: "running", TasksTotal: 3, TasksDone: 1, UpdatedAt: now,
	}

	for range 4 {
		if _, err := state.viewFor(s, fetch); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("an unchanged summary re-fetched the projection %d times, want 1", calls)
	}

	// A new event moves updated_at, which is what a re-read is for.
	s.TasksDone++
	s.UpdatedAt = now.Add(time.Second)
	if _, err := state.viewFor(s, fetch); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("a changed summary must re-fetch: got %d fetches", calls)
	}

	// A different session is a different projection, whatever its summary says.
	other := goclient.SessionSummary{SessionID: "sess-b", State: "running", UpdatedAt: now}
	if _, err := state.viewFor(other, fetch); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("switching sessions must re-fetch: got %d fetches", calls)
	}
}

func TestPollStateCarriesTheProjectionIntoTheView(t *testing.T) {
	var state pollState
	st := protocol.State{
		State:  protocol.StateRunning,
		Tasks:  []protocol.Task{{ID: "1", Title: "one", Status: protocol.TaskDone}},
		Turns:  4,
		Budget: protocol.BudgetData{MaxTurns: 60},
		Goal:   &protocol.GoalData{Condition: "the gate passes"},
	}
	v, err := state.viewFor(goclient.SessionSummary{
		SessionID: "sess-a", Workspace: "/home/kyle/rho", State: "running", UpdatedAt: time.Now(),
	}, func(string) (protocol.State, error) { return st, nil })
	if err != nil {
		t.Fatal(err)
	}
	if v.SessionID != "sess-a" || v.State != protocol.StateRunning || v.Workspace != "/home/kyle/rho" {
		t.Fatalf("summary fields lost: %+v", v)
	}
	if v.Goal != "the gate passes" || v.Turns != 4 || v.MaxTurns != 60 || len(v.Tasks) != 1 {
		t.Fatalf("projection fields lost: %+v", v)
	}
}

func TestPollStateReportsAFetchFailure(t *testing.T) {
	var state pollState
	want := &goclient.RPCError{Code: protocol.CodeSessionNotFound, Message: "no such session"}
	_, err := state.viewFor(goclient.SessionSummary{SessionID: "gone"},
		func(string) (protocol.State, error) { return protocol.State{}, want })
	if err != want {
		t.Fatalf("a failed projection must reach the caller, got %v", err)
	}
	if state.have {
		t.Fatal("a failed fetch must not leave a half-built state behind")
	}
}

func TestMissingReason(t *testing.T) {
	list := []goclient.SessionSummary{summary("sess-a", "running", 1, 0, time.Now())}
	cases := []struct {
		name string
		list []goclient.SessionSummary
		want string
	}{
		{"an id that is not listed", list, "session sess-z is not listed"},
		{"no sessions at all", nil, "the daemon has no sessions"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.want
			if tc.name == "an id that is not listed" {
				if got := missingReason(tc.list, "sess-z"); got != want {
					t.Fatalf("got %q, want %q", got, want)
				}
			} else if got := missingReason(tc.list, ""); got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

// testDaemon serves the real API over a loopback port with nothing behind the
// session reads but a store, which is all the tray asks the daemon for.
func testDaemon(t *testing.T) (addr string, store *session.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := session.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(api.NewServer(api.NewHandler(nil, store, log), &api.Config{}, log))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://"), store
}

func newSession(t *testing.T, store *session.Store, workspace string) *session.Session {
	t.Helper()
	sess, err := store.Create(workspace, "tray-test", protocol.Options{Model: "m", PermissionMode: protocol.PermissionAsk}, 8000)
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

// tasksRevision counts up because a tasks event is a whole-plan replacement and
// each one must carry a revision above the last.
var tasksRevision int

func appendTasks(t *testing.T, sess *session.Session, tasks ...protocol.Task) {
	t.Helper()
	for i := range tasks {
		if tasks[i].BlockedBy == nil {
			tasks[i].BlockedBy = []string{}
		}
	}
	tasksRevision++
	_, err := sess.Append(protocol.EventTasks, protocol.TasksData{
		Revision: tasksRevision, Source: "model", Tasks: tasks,
	})
	if err != nil {
		t.Fatal(err)
	}
}

// views collects what Watch publishes so a test can read it without racing the
// loop, and hands out one view at a time.
type views struct {
	t      *testing.T
	mu     sync.Mutex
	seen   []View
	ready  chan View
	cancel context.CancelFunc
	watch  chan error
}

func startWatch(t *testing.T, cfg WatchConfig) *views {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	v := &views{t: t, ready: make(chan View, 64), cancel: cancel, watch: make(chan error, 1)}
	t.Cleanup(func() {
		cancel()
		select {
		case <-v.watch:
		case <-time.After(5 * time.Second):
			v.t.Error("Watch did not return after its context was cancelled")
		}
	})
	go func() { v.watch <- Watch(ctx, cfg, v.publish) }()
	return v
}

func (v *views) publish(view View) {
	v.mu.Lock()
	v.seen = append(v.seen, view)
	v.mu.Unlock()
	select {
	case v.ready <- view:
	default:
	}
}

// await waits for a published view that satisfies want, and fails the test with
// everything seen so far when none turns up.
func (v *views) await(what string, want func(View) bool) View {
	v.t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case got := <-v.ready:
			if want(got) {
				return got
			}
		case <-deadline:
			v.mu.Lock()
			defer v.mu.Unlock()
			v.t.Fatalf("no view matched %s; saw %d views, last: %+v", what, len(v.seen), lastOf(v.seen))
		}
	}
}

func lastOf(list []View) View {
	if len(list) == 0 {
		return View{}
	}
	return list[len(list)-1]
}

func TestWatchShowsTheBusiestSessionAndItsProgress(t *testing.T) {
	addr, store := testDaemon(t)
	sess := newSession(t, store, "/home/kyle/Projects/rho")
	appendTasks(t, sess,
		protocol.Task{ID: "1", Title: "write the view", Status: protocol.TaskDone},
		protocol.Task{ID: "2", Title: "draw the icon", Status: protocol.TaskInProgress},
	)

	v := startWatch(t, WatchConfig{Addr: addr, Poll: 40 * time.Millisecond})

	got := v.await("the session with its tasks", func(g View) bool {
		return g.SessionID == sess.ID() && len(g.Tasks) == 2
	})
	if done, total := Progress(got); done != 1 || total != 2 {
		t.Fatalf("progress from the daemon: got %d/%d, want 1/2", done, total)
	}
	if got.Workspace != "/home/kyle/Projects/rho" {
		t.Fatalf("workspace lost: %+v", got)
	}
	if got.Err != "" {
		t.Fatalf("a reachable daemon must not read as offline: %q", got.Err)
	}

	appendTasks(t, sess,
		protocol.Task{ID: "1", Title: "write the view", Status: protocol.TaskDone},
		protocol.Task{ID: "2", Title: "draw the icon", Status: protocol.TaskDone},
	)
	got = v.await("both tasks done", func(g View) bool {
		return g.SessionID == sess.ID() && len(g.Tasks) == 2 && g.Tasks[1].Status == protocol.TaskDone
	})
	if done, total := Progress(got); done != 2 || total != 2 {
		t.Fatalf("progress after the update: got %d/%d, want 2/2", done, total)
	}
}

func TestWatchReportsASessionItCannotFind(t *testing.T) {
	addr, _ := testDaemon(t)
	v := startWatch(t, WatchConfig{Addr: addr, Session: "sess-absent", Poll: 40 * time.Millisecond})

	got := v.await("the missing session reported", func(g View) bool {
		return g.Err == "session sess-absent is not listed"
	})
	if got.SessionID != "" {
		t.Fatalf("an offline view must carry no session: %+v", got)
	}
}

func TestWatchRetriesADaemonItCannotReach(t *testing.T) {
	// Port 1 on loopback refuses, so the dial fails at once rather than after
	// the handshake timeout.
	v := startWatch(t, WatchConfig{Addr: "127.0.0.1:1", Poll: 40 * time.Millisecond})

	got := v.await("an offline view", func(g View) bool { return g.Err != "" })
	if got.State != protocol.StateIdle {
		t.Fatalf("an offline view must still have a state to draw: %+v", got)
	}
	if Title(got) != "rho · offline" {
		t.Fatalf("offline title: %q", Title(got))
	}
}
