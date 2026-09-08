package runner

import (
	"context"
	"fmt"
	"iter"

	"github.com/chetto1983/aura/internal/agent"
)

// PreparePendingSteer consumes a saved batch only after the host owns the same
// conversation lock as an ordinary turn. An already-drained batch costs no model call.
func (r *Runner) PreparePendingSteer(ctx context.Context, convID string) (iter.Seq2[*agent.Event, error], bool, error) {
	if !threadLockHeld(ctx) {
		return nil, false, fmt.Errorf("pending steer: conversation lock is required")
	}
	ctx, err := r.scopeContextToConversation(ctx, convID)
	if err != nil {
		return nil, false, err
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if r.steer == nil {
		return nil, false, nil
	}
	msgs := r.steer.Drain(convID)
	if len(msgs) == 0 {
		return nil, false, nil
	}
	inner := r.turnLocked(ctx, convID, leftoverTurnInput(msgs))
	return r.deliverLeftoverSteer(ctx, convID, inner), true, nil
}
