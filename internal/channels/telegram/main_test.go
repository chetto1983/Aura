package telegram

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the telegram package's tests under goleak. The bot polling
// goroutine (Plan 13-03), the per-turn fanout pump, and the async
// document-convert path (Plan 13-04/13-08) are all on the explicit goleak list
// (amendment #15); this gate fails the package if any leaks. It also covers the
// db_integration tier (Store onboarding round-trip). Mirrors
// internal/agent/tools/main_test.go.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
