package handlers

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain installs the package-wide goroutine-leak gate. The handlers are leak-free
// by construction: the agent_job drains the FakeClient's pre-closed channels (no
// background goroutine), and the auto-reject test joins its goroutine before
// returning. A leak here would mean a handler spawned an unjoined goroutine.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
