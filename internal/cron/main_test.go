package cron

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain installs the package-wide goroutine-leak gate. The schedule engine is
// leak-free by construction (no goroutines) and the store closes its pool via
// t.Cleanup; the tick loop / heartbeat ticker landing in 10-03 are guarded here too.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
