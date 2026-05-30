package main

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the cmd/aura package tests under goleak so the chat REPL's
// per-turn streaming + two-stage Ctrl+C teardown is asserted leak-free (D-10/
// Req#3): a leaked stream goroutine or an un-cancelled signal notifier would trip
// here. goleak verifies AFTER all tests complete.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
