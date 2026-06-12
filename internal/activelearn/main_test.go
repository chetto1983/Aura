package activelearn

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the activelearn mechanism tests under goleak. The one bounded
// worker goroutine each Learner starts MUST be reaped by Close (cancel + wg.Wait);
// a Learner whose Close is skipped, or a worker that survives cancel, leaks here.
// This is the structural goroutine-leak assertion (T-08.2-13).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
