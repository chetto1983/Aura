package runner

import (
	"context"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/arcadedb"
)

// ReasoningGraphSink is the narrow identity-scoped graph persistence boundary.
type ReasoningGraphSink interface {
	UpsertReasoningTrace(context.Context, arcadedb.ReasoningTrace) error
}

// ReasoningTraceBuilder accumulates one authorized provider-visible attempt.
// The RED stub deliberately retains no state; the production behavior lands in GREEN.
type ReasoningTraceBuilder struct{}

// Reset discards every event from a repudiated provider attempt.
func (*ReasoningTraceBuilder) Reset() {}

func (r *Runner) observeReasoningGraph(_ *turnTracker, _ *agent.Event) {}

func (r *Runner) commitSourceTurn(_ context.Context, _ *turnTracker, _ *agent.Event) {}
