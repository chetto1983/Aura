package agent

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the package tests under goleak so a leaked OTel batch processor
// goroutine, a leaked stream goroutine, or any other residual goroutine fails the
// suite (Phase-1/2 discipline; Req#3). Tests that build a TracerProvider MUST
// Shutdown it so the batcher's background goroutine returns.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
