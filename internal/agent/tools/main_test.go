package tools

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain keeps goleak parity with the rest of internal/agent: the tools
// package spawns no goroutines today, but the gate guards against a future tool
// (sandbox/session-bound) leaking one. Internal test package so the spillover
// tests can reach unexported helpers (truncatePreview, validateID).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
