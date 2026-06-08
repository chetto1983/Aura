package setup

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the setup package's tests under goleak. The setup wizard's SSE
// pump (the 2s telegram_setup_pending poll loop, Plan 13-07) is on the explicit
// goleak list (amendment #15) — it must exit on client disconnect AND server
// shutdown; this gate fails the package if it leaks. Mirrors
// internal/agent/tools/main_test.go. The non-test package body (server.go,
// handlers.go) lands in Plan 13-07; this harness exists first so those tests
// inherit the leak check.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
