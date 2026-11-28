package tray

import (
	"context"
	"errors"
	"time"

	"github.com/corporealshift/rho/clients/goclient"
	"github.com/corporealshift/rho/protocol"
)

// The tray reads the daemon rather than subscribing to it, and that is the
// point rather than an oversight. A subscriber counts as an attached client,
// and a permission request then waits the full ten minutes for the first
// attached client to answer (spec 7.18); with nobody attached the daemon
// refuses the call at once. A tray that can watch but never answer would turn
// an instant denial into a stall, so it stays out of the race.

// Read intervals. Poll is how often the daemon is asked what it thinks; a
// longer gap costs a tray nothing, because a task list changes on the scale of
// minutes and the icon is not a progress bar.
const (
	defaultPoll    = 2 * time.Second
	callTimeout    = 10 * time.Second
	minReconnect   = time.Second
	maxReconnect   = 30 * time.Second
	trayClientName = "rho-tray"
)

// WatchConfig says where the daemon is and which session to show.
type WatchConfig struct {
	Addr    string
	Token   string
	Version string
	Session string // empty shows the busiest session
	Poll    time.Duration
}

// Watch reads the daemon until ctx ends, handing every new View to publish.
//
// It returns only when ctx is done. A daemon it cannot reach becomes an
// offline View and a retry on a backoff ladder, because a tray that exits when
// the daemon is down is a tray somebody has to start again by hand.
func Watch(ctx context.Context, cfg WatchConfig, publish func(View)) error {
	if cfg.Poll <= 0 {
		cfg.Poll = defaultPoll
	}
	backoff := minReconnect
	for ctx.Err() == nil {
		c, err := dial(ctx, cfg)
		if err != nil {
			publish(offline(err.Error()))
			if !sleep(ctx, backoff) {
				return ctx.Err()
			}
			backoff = doubled(backoff)
			continue
		}
		// A connection that was made resets the ladder, however short it then
		// lived: a daemon restarted every few seconds should be read every few
		// seconds, not after a growing wait.
		backoff = minReconnect
		err = pollLoop(ctx, c, cfg, publish)
		c.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			err = errConnectionEnded
		}
		publish(offline(err.Error()))
		if !sleep(ctx, backoff) {
			return ctx.Err()
		}
		backoff = doubled(backoff)
	}
	return ctx.Err()
}

func dial(ctx context.Context, cfg WatchConfig) (*goclient.Client, error) {
	dialCtx, cancel := context.WithTimeout(ctx, goclient.DialTimeout)
	defer cancel()
	version := cfg.Version
	if version == "" {
		version = "0.0.0"
	}
	return goclient.Dial(dialCtx, cfg.Addr, cfg.Token, trayClientName, version)
}

// pollLoop reads one connection. Any read failure ends it and hands the
// reconnect to Watch, which re-dials and re-picks a session from scratch.
func pollLoop(ctx context.Context, c *goclient.Client, cfg WatchConfig, publish func(View)) error {
	ticker := time.NewTicker(cfg.Poll)
	defer ticker.Stop()

	var state pollState
	for {
		list, err := listSessions(ctx, c)
		if err != nil {
			return err
		}
		pick, ok := PickSession(list, cfg.Session)
		if !ok {
			state = pollState{}
			publish(offline(missingReason(list, cfg.Session)))
		} else {
			v, err := state.viewFor(pick, func(id string) (protocol.State, error) {
				return fetchState(ctx, c, id)
			})
			if err != nil {
				return err
			}
			publish(v)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// pollState remembers what the last read said, so the expensive read runs only
// when the cheap one shows something moved.
type pollState struct {
	session string
	key     summaryKey
	have    bool
	st      protocol.State
}

// summaryKey is what a summary says about a session. rho.session.list already
// carries the state and the task counts, so an unchanged key means no event
// was appended since the last read and the projection cannot have changed.
// That matters because rho.session.state re-derives the whole log.
type summaryKey struct {
	state     string
	total     int
	done      int
	updatedAt time.Time
}

func keyOf(s goclient.SessionSummary) summaryKey {
	return summaryKey{s.State, s.TasksTotal, s.TasksDone, s.UpdatedAt.UTC()}
}

// viewFor builds the View for one summary, fetching a fresh projection only
// when the session is new or its summary changed. fetch is the daemon read,
// taken as an argument so the decision is testable without a daemon.
func (p *pollState) viewFor(s goclient.SessionSummary, fetch func(string) (protocol.State, error)) (View, error) {
	key := keyOf(s)
	if !p.have || p.session != s.SessionID || key != p.key {
		st, err := fetch(s.SessionID)
		if err != nil {
			return View{}, err
		}
		p.session, p.key, p.have, p.st = s.SessionID, key, true, st
	}
	return viewOf(s, p.st), nil
}

func viewOf(s goclient.SessionSummary, st protocol.State) View {
	v := View{
		SessionID:  s.SessionID,
		State:      protocol.SessionState(s.State),
		Workspace:  s.Workspace,
		LastPrompt: s.LastPrompt,
		UpdatedAt:  s.UpdatedAt,
		Tasks:      st.Tasks,
		Turns:      st.Turns,
		MaxTurns:   st.Budget.MaxTurns,
	}
	if st.Goal != nil {
		v.Goal = st.Goal.Condition
	}
	return v
}

// missingReason says why there is nothing to show, in the words a person reads
// in the tray.
func missingReason(list []goclient.SessionSummary, want string) string {
	switch {
	case want != "":
		return "session " + want + " is not listed"
	case len(list) == 0:
		return "the daemon has no sessions"
	default:
		return "no session to show"
	}
}

// listSessions and fetchState each bound one read, so a daemon that has stopped
// answering cannot hang the tray: the connection then fails and Watch redials.
func listSessions(ctx context.Context, c *goclient.Client) ([]goclient.SessionSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return c.List(ctx)
}

func fetchState(ctx context.Context, c *goclient.Client, sessionID string) (protocol.State, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return c.State(ctx, sessionID)
}

// errConnectionEnded reports a connection that stopped without an error to
// explain it, which is what a daemon that is being shut down looks like.
var errConnectionEnded = errors.New("the connection to the daemon ended")

// sleep waits out d, and reports false when ctx ended first.
func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func doubled(d time.Duration) time.Duration {
	if d *= 2; d > maxReconnect {
		return maxReconnect
	}
	return d
}
