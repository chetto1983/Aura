package swarm

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain installs the package-wide goroutine-leak gate (D-25). Copied from
// internal/agent/workflow/workflow_test.go:17-18 — every swarm test runs under it,
// and the concurrent runner tests additionally assert per-test via
// goleak.VerifyNone(t).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
