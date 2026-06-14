package handlers

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain installs the package-wide goroutine-leak gate. The handlers are
// leak-free by construction; a leak here means a handler spawned unjoined work.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
