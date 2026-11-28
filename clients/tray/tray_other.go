//go:build !(linux || windows)

package tray

import (
	"context"
	"fmt"
	"runtime"
)

// Run has no tray to put the icon in on this platform. The darwin backend of
// fyne.io/systray is Objective-C, and scripts/build-release.sh cross-compiles
// with cgo off, so darwin gets this rather than an icon it cannot build.
func Run(ctx context.Context, cfg WatchConfig) error {
	return fmt.Errorf("rho tray: no system tray for %s/%s", runtime.GOOS, runtime.GOARCH)
}
