// Package tray runs a system tray icon so the user can see Aura is running
// and stop it from the OS shell.
//
// Run blocks the calling goroutine — it MUST be called from main because the
// underlying systray library requires the main thread on Windows. Stop is
// safe from any goroutine.
package tray

// Options configures the tray icon and menu.
type Options struct {
	Title   string // short label, shown next to the icon on some OSes
	Tooltip string // hover text
	Version string // displayed in a disabled menu header when non-empty
}

// Run starts the tray on the calling goroutine. Returns when the user clicks
// the Quit menu item or another goroutine calls Stop.
func Run(opts Options) error { return run(opts) }

// Stop unblocks Run from any goroutine. Safe to call multiple times.
func Stop() { stop() }
