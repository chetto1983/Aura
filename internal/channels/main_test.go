package channels

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the channels package's tests under goleak. The registry's
// StartAll/StopAll lifecycle (Plan 13-03) launches per-channel goroutines whose
// graceful drain this gate guards (amendment #15). Mirrors
// internal/agent/tools/main_test.go; later plans drop tests in and inherit the
// leak check for free.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
