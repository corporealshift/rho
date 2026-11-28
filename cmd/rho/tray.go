package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"

	"github.com/corporealshift/rho/clients/tray"
	"github.com/corporealshift/rho/daemon"
	"github.com/corporealshift/rho/daemon/config"
)

// launchTray is a variable so the command is testable without a desktop.
var launchTray = tray.Run

// cmdTray keeps an icon in the desktop's notification area showing how far a
// session has got.
//
// It reads the daemon rather than subscribing to it: a subscriber counts as an
// attached client, and a permission request then waits for the first attached
// client to answer (spec 7.18). An icon that can watch but never answer would
// turn an instant denial into a ten-minute stall.
//
// It also does not start a daemon. Every other command does, because they need
// an answer now; an icon that started one on every boot would be a daemon
// nobody asked for, so this one reads as offline and waits instead.
func cmdTray(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tray", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := rootFlag(fs)
	session := fs.String("session", "", "show this session (default: the busiest one)")
	poll := fs.Duration("poll", 0, "how often to re-read the daemon (default 2s)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	dir, err := resolveRoot(*root)
	if err != nil {
		fmt.Fprintf(stderr, "rho: %v\n", err)
		return exitError
	}
	cfg, err := config.Load(dir)
	if err != nil {
		fmt.Fprintf(stderr, "rho: %v\n", err)
		return exitError
	}

	addr, ok := daemon.RunningAddr(dir)
	if !ok {
		addr = dialAddr(cfg.Daemon.Bind)
		fmt.Fprintf(stderr, "rho: no daemon on %s; the icon will wait for one\n", addr)
	}

	if err := launchTray(context.Background(), tray.WatchConfig{
		Addr:    addr,
		Token:   cfg.Daemon.Token,
		Version: version,
		Session: *session,
		Poll:    *poll,
	}); err != nil {
		fmt.Fprintf(stderr, "rho: %v\n", err)
		return exitError
	}
	return exitOK
}

// dialAddr turns a configured bind into something a client can dial. A bind
// with no host is an address to listen on, not one to connect to, and the tray
// keeps the address it worked out at startup for as long as it lives.
func dialAddr(bind string) string {
	host, port, err := net.SplitHostPort(bind)
	if err != nil || port == "" {
		return "127.0.0.1:8737"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		return "127.0.0.1:" + port
	}
	return net.JoinHostPort(host, port)
}
