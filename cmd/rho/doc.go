// Command rho is the single binary: `rho daemon` runs the daemon in the foreground;
// every other subcommand (run, status, attach, stop, resume) connects to a running
// daemon and starts one detached if none is listening. `rho tray` is the exception: it
// reads a daemon and waits for one, because an icon in the desktop must not start a
// daemon nobody asked for.
package main
