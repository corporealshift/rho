# A tray icon that reads the busiest session

**Date:** 2026-11-27
**Status:** Approved by Kyle in conversation
**Amends:** `clients/goclient` (`SessionSummary` gains `tasks_total` / `tasks_done`),
`cmd/rho` (a `tray` subcommand), `protocol/spec.md` §7.2 (the summary's task fields,
written as the daemon emits them).

## Problem

Every existing client needs a window open to be useful: the terminal UI, the phone, the
run page. Task progress — how many of a session's tasks are ticked, whether it is running
or blocked — is only visible by opening that session. Kyle wants a glance: an icon in the
desktop's notification area, on his KDE Plasma desktop and on Windows.

## Decision

1. **Watch-only.** The tray is an icon, a tooltip, and a read-only menu of task rows.
   It has no approve or deny, no jump to the TUI, nothing to click. Every row is
   `Disable`d or carries no click channel, so a click does nothing.
2. **It polls; it never subscribes.** This is the load-bearing decision, not an
   oversight. A subscriber counts as an attached client (§7.18), and a permission request
   is broadcast to every attached client and waits for the first verdict — up to the
   ten-minute default — before the session goes `blocked`. With nobody attached the daemon
   refuses the gated call at once (`daemon/api/requests.go`). An icon that subscribed and
   could never answer would turn an instant denial into a ten-minute stall. So the tray
   calls only `rho.session.list` and `rho.session.state`, and never appears in
   `Handler.subscribers`.
3. **Two reads, one of them cheap.** `rho.session.list` already carries the state,
   `tasks_total`, `tasks_done` and `updated_at`, and that is enough for the icon and the
   tooltip. `rho.session.state` re-derives the whole log (`protocol.Project`), so it runs
   only when the session changes or its summary key — state, total, done, `updated_at` —
   moves. An unchanged key means no event was appended, so the projection cannot differ.
   Poll interval defaults to 2s; a task list changes on the scale of minutes.
4. **One session is shown.** `--session ID` wins when that session is listed; otherwise
   the busiest session wins, ranked by state (running, blocked, paused, error, idle,
   completed), then open tasks, then total tasks, then recency. A completed session ranks
   last: it is the least worth watching.
5. **The icon is drawn in Go, not shipped as an asset.** It carries the session state and
   the task fraction, which a fixed picture cannot. One supersampled 32-pixel drawing
   (4 samples per edge, so a ring is not a staircase) becomes a PNG on Linux —
   StatusNotifierItem takes a pixmap — and an ICO with 16- and 32-pixel entries on Windows,
   where `Shell_NotifyIcon` hands a path to `LoadImage`, which wants an icon container.
   The bytes are re-encoded only when `iconKey` (offline reason, state, done/total) moves:
   new bytes cost a redraw on Linux and a temp file on Windows.
6. **Menu rows are relabelled in place.** A toolkit cannot move a menu row, only relabel
   one, and relabelling is what a task finishing actually is. `ResetMenu` runs only when
   the number of rows or their kinds change, because a re-shown row is appended at the end
   rather than put back where it was. The shape is fixed — header, separator, task rows,
   separator, footer — so an empty session reads as two separators with nothing between
   them. At most 12 task rows, then `… n more`; rows are clamped to 60 runes and the
   tooltip to 127, which is `NOTIFYICONDATA.szTip` on Windows.
7. **It never starts a daemon.** Every other command calls `daemon.EnsureRunning`, because
   they need an answer now. An icon that lives in the desktop would otherwise start a
   daemon on every boot. `rho tray` reads `daemon.RunningAddr` for the address, falls back
   to the configured bind (a wildcard bind becomes loopback), says so on stderr, and waits:
   an unreachable daemon is an offline icon and a retry on a 1s→30s ladder. The ladder
   resets after any dial that succeeded, however short the connection then lived.
8. **Linux and Windows only.** `fyne.io/systray` is pure Go on both: StatusNotifierItem
   plus dbusmenu over `github.com/godbus/dbus/v5` on Linux, `Shell_NotifyIcon` over
   `golang.org/x/sys/windows` on Windows. Its darwin backend is Objective-C, and
   `scripts/build-release.sh` cross-compiles darwin, linux and windows with
   `CGO_ENABLED=0`, so darwin gets a build-tagged `Run` that says there is no tray for it.
   The build tags are `linux || windows` and nothing wider: dbus v5.1.0 does not build for
   FreeBSD (`transport_unix.go`: `*unixTransport` does not implement `transport`, missing
   `SendNullByte`).
9. **`rho tray`, not a second binary.** `fyne.io/systray` is imported only by
   `clients/tray/tray.go`, which is build-tagged, so `cmd/rho` still builds for darwin with
   cgo off and `scripts/build-release.sh` is unchanged. Flags: `--root`, `--session`,
   `--poll`. `launchTray` is a variable, as `launchTUI` is, so the command is testable
   without a desktop.
10. **`goclient.SessionSummary` gains `tasks_total` / `tasks_done`.** The daemon has always
    sent them (`daemon/session/store.go`, and the notification payload uses the same two
    names). Spec §7.2 wrote them as a nested `tasks?: {total, done}`, which nothing emits
    and no client reads; the spec text is corrected to the flat pair. No schema or vector
    change follows: `protocol/schema/` holds `event.json` and `jsonrpc.json`, and the
    vectors cover events, projection, validation, cursors, ordering, assembly and
    rendering — none of them a method result.

## Rejected

- **Subscribing for push updates.** It would make the tray an attached client that cannot
  answer, which is the ten-minute stall in Decision 2.
- **Hand-rolling StatusNotifierItem and dbusmenu over godbus.** Tried first. It is more
  code for the same icon, and the property and menu-update semantics are not documented
  anywhere a person can trust.
- **A separate `cmd/rho-tray` binary in the release script.** Kyle chose the subcommand.
  The build tags already keep cgo out of the darwin build.
- **Icons as shipped assets.** The icon has to say which session and how far along it is.
- **Showing every session.** One session, chosen by rank or by `--session`. A tray with a
  submenu per session is a window in a worse place.
- **Approve and deny rows in the tray.** It would need the subscription the tray exists to
  avoid, plus a UI for the reason field.
- **Hide and show rows instead of rebuilding the menu.** A toolkit appends a re-shown row
  at the end, so the menu reorders itself.

## Verified

- **Live on Plasma 6 (Wayland), against the running daemon.** The item registered as
  `org.kde.StatusNotifierItem-<pid>-1` with the name the watcher lists, `Status: Active`,
  `Category: ApplicationStatus`, `Title: rho · running`, `ItemIsMenu: true`, a 32×32
  `IconPixmap`, and a menu at `/StatusNotifierItem/menu`. Its rows were the session's real
  task list — `rho · running · rho`, a separator, `✓ Branch tray-icon off main and confirm
  deps` … `○ Commit with named files only`, a separator, `4/11 tasks` — and its tooltip
  read `rho · running · rho · tasks 4/11 · turns 585 · 31s ago`. SIGTERM took the item off
  the bus.
- **Gate.** `gofmt -l .` empty; `go build ./...`, `go vet ./...`, `go test ./...` clean;
  `CGO_ENABLED=0` builds for `windows/amd64`, `windows/386`, `darwin/arm64`,
  `darwin/amd64` and `linux/arm64`.

## Notes for whoever runs it

- Plasma parks a newly registered item in the tray's hidden section until it is shown;
  registration is the same either way.
- `rho tray` reads the daemon's token from the root's `config.json`. With no daemon it says
  so once on stderr and keeps waiting rather than exiting.
- The tray is a client like any other: it needs the token, and it is not counted as
  attached to a session.
