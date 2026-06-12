package toolselectlearn

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the tool-selection loop tests under goleak. The Learner's async
// worker (one bounded activelearn goroutine) MUST be reaped by Close; a test that
// skips Close, or a worker that survives cancel, leaks here (T-08.2-13 parity).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
