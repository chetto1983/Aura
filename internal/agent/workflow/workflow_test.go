package workflow_test

import (
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/workflow"
	"go.uber.org/goleak"
)

// TestMain wires the SC#1 goroutine-leak gate: every test in this package runs
// under goleak.VerifyNone, so a workflow agent (Sequential/Loop, and Parallel in
// Plan 06) that leaks a goroutine fails the package. W8: time-dependent tests use
// the Budget's injectable fake clock, NOT the Go 1.26 synctest package (which
// spawns background goroutines that would trip this gate). Copied verbatim from
// internal/db/db_test.go:26-28.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// Compile-time assertions that the exported workflow structs satisfy the
// cornerstone agent.Agent interface (D-02, SPEC Acceptance). If any method drifts
// the build breaks here.
var _ agent.Agent = (*workflow.SequentialAgent)(nil)
