//go:build !windows

package tray

import "sync"

var (
	stopOnce sync.Once
	stopCh   = make(chan struct{})
)

// On non-Windows platforms the tray is a no-op: Run blocks until Stop is
// called so cmd/aura/main.go can use the same shutdown sequence as Windows.
func run(_ Options) error {
	<-stopCh
	return nil
}

func stop() { stopOnce.Do(func() { close(stopCh) }) }
