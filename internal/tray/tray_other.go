//go:build !windows

package tray

import (
	"log/slog"
	"sync"
)

var (
	stopOnce sync.Once
	stopCh   = make(chan struct{})
)

// On non-Windows platforms the tray is a no-op: Run blocks until Stop is
// called so cmd/aura/main.go can use the same shutdown sequence as Windows.
func run(opts Options) error {
	if opts.DashboardURL != "" {
		slog.Info("tray: platform not supported, running headless", "dashboard_url", opts.DashboardURL)
	} else {
		slog.Info("tray: platform not supported, running headless")
	}
	<-stopCh
	return nil
}

func stop() { stopOnce.Do(func() { close(stopCh) }) }
