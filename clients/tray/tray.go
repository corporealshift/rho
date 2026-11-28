//go:build linux || windows

package tray

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"fyne.io/systray"
)

// Run puts the icon in the tray and keeps it reading the daemon.
//
// It returns when the process is asked to stop (SIGINT/SIGTERM) or when the
// desktop takes the icon away. A daemon that cannot be reached is not a reason
// to exit: the icon stays, reads as offline, and catches up when the daemon
// comes back.
func Run(ctx context.Context, cfg WatchConfig) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	ui := &trayUI{}
	ready := make(chan struct{})
	watched := make(chan error, 1)

	go func() {
		// Nothing may be added to the menu before the toolkit has a menu to
		// add it to; on Windows those calls are dropped rather than queued.
		select {
		case <-ready:
		case <-ctx.Done():
		}
		watched <- Watch(ctx, cfg, ui.publish)
		systray.Quit()
	}()

	systray.Run(func() {
		ui.publish(offline("connecting to the daemon"))
		close(ready)
	}, stop)

	stop()
	err := <-watched
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// trayUI is the icon and the menu as the desktop sees them. It is built once
// and then edited in place, because a toolkit cannot move a menu row: it can
// relabel one, and relabelling is what a task finishing actually is.
type trayUI struct {
	mu    sync.Mutex
	lines []Line
	// items lines up with lines, one entry per row, with a nil for each
	// separator so a row can be found by its position.
	items   []*systray.MenuItem
	iconKey string
}

func (u *trayUI) publish(v View) {
	u.mu.Lock()
	defer u.mu.Unlock()

	// The icon is re-encoded only when its key moves, because handing the
	// toolkit new bytes costs a redraw on Linux and a temp file on Windows.
	if key := iconKey(v); key != u.iconKey {
		u.iconKey = key
		systray.SetIcon(platformIcon(v))
	}
	systray.SetTitle(Title(v))
	systray.SetTooltip(Tooltip(v))

	lines := Lines(v)
	if sameShape(u.lines, lines) {
		for i, line := range lines {
			if u.items[i] != nil {
				u.items[i].SetTitle(line.Text)
			}
		}
		u.lines = lines
		return
	}

	// A different number of rows is a different menu. Rows are not hidden and
	// shown instead: a toolkit puts a shown row at the end, which is not where
	// it was.
	systray.ResetMenu()
	u.items = nil
	for _, line := range lines {
		switch line.Kind {
		case LineSeparator:
			systray.AddSeparator()
			u.items = append(u.items, nil)
		case LineTask:
			u.items = append(u.items, systray.AddMenuItem(line.Text, ""))
		default:
			// The header and the footer are labels, not things to click.
			item := systray.AddMenuItem(line.Text, "")
			item.Disable()
			u.items = append(u.items, item)
		}
	}
	u.lines = lines
}

// sameShape reports whether two menus have the same kind of row in the same
// place, which is when relabelling is enough.
func sameShape(a, b []Line) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Kind != b[i].Kind {
			return false
		}
	}
	return true
}
