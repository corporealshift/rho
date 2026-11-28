package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/corporealshift/rho/clients/tray"
	"github.com/corporealshift/rho/daemon"
)

// stubTray replaces the launcher so the tray command can be driven without a
// desktop, and records what it was asked to watch.
func stubTray(t *testing.T) *tray.WatchConfig {
	t.Helper()
	var got tray.WatchConfig
	prev := launchTray
	launchTray = func(_ context.Context, c tray.WatchConfig) error {
		got = c
		return nil
	}
	t.Cleanup(func() { launchTray = prev })
	return &got
}

func TestTrayWatchesTheRunningDaemon(t *testing.T) {
	root := startDaemon(t)
	watched := stubTray(t)

	code, _, stderr := runCLI("tray", "--root", root)
	if code != exitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	want, ok := daemon.RunningAddr(root)
	if !ok {
		t.Fatal("the daemon this test started is not listed as running")
	}
	if watched.Addr != want {
		t.Errorf("addr: got %q, want the running daemon %q", watched.Addr, want)
	}
	if watched.Session != "" {
		t.Errorf("no --session must mean the busiest session, got %q", watched.Session)
	}
	if watched.Version != version {
		t.Errorf("version: got %q, want %q", watched.Version, version)
	}
}

func TestTrayTakesASessionAndAPollInterval(t *testing.T) {
	root := startDaemon(t)
	watched := stubTray(t)

	code, _, stderr := runCLI("tray", "--root", root, "--session", "sess-1", "--poll", "5s")
	if code != exitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if watched.Session != "sess-1" {
		t.Errorf("session: got %q, want sess-1", watched.Session)
	}
	if watched.Poll != 5*time.Second {
		t.Errorf("poll: got %v, want 5s", watched.Poll)
	}
}

// A tray that cannot find a daemon waits rather than starting one: an icon
// living in the desktop would otherwise start a daemon on every boot.
func TestTrayWaitsForADaemonInsteadOfStartingOne(t *testing.T) {
	root := t.TempDir()
	watched := stubTray(t)

	code, _, stderr := runCLI("tray", "--root", root)
	if code != exitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if _, ok := daemon.RunningAddr(root); ok {
		t.Fatal("rho tray started a daemon it was only asked to watch")
	}
	if watched.Addr != "127.0.0.1:8737" {
		t.Errorf("addr: got %q, want the default bind", watched.Addr)
	}
	if stderr == "" {
		t.Error("waiting for a daemon should say so")
	}
}

func TestTrayRejectsAnUnknownFlag(t *testing.T) {
	code, _, stderr := runCLI("tray", "--nope")
	if code != exitUsage {
		t.Errorf("exit %d, want 64, stderr %q", code, stderr)
	}
}

func TestUsageListsTray(t *testing.T) {
	_, stdout, _ := runCLI("help")
	if !strings.Contains(stdout, "  tray ") {
		t.Errorf("usage does not list tray:\n%s", stdout)
	}
}

func TestDialAddrDialsWhatItCanReach(t *testing.T) {
	cases := []struct {
		bind, want string
	}{
		{"127.0.0.1:8737", "127.0.0.1:8737"},
		{":8737", "127.0.0.1:8737"},
		{"0.0.0.0:8737", "127.0.0.1:8737"},
		{"[::]:8737", "127.0.0.1:8737"},
		{"10.0.0.5:9000", "10.0.0.5:9000"},
		{"", "127.0.0.1:8737"},
		{"not-an-address", "127.0.0.1:8737"},
	}
	for _, tc := range cases {
		if got := dialAddr(tc.bind); got != tc.want {
			t.Errorf("dialAddr(%q) = %q, want %q", tc.bind, got, tc.want)
		}
	}
}
