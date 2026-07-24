package adaptive

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

type adaptiveTurnWriter interface {
	CommitTurn(context.Context, conversations.AppendTurnParams, *sqlc.InsertCacheMetricParams, Event) error
}

type baselineTurnWriter interface {
	AppendTurn(context.Context, conversations.AppendTurnParams) error
	AppendAssistantTurnWithCacheMetric(context.Context, conversations.AppendTurnParams, sqlc.InsertCacheMetricParams) error
}

// PolicyAwareTurnCommitter records adaptive outcomes only while the fresh
// authoritative gate says Observe. Off, rollback, or a policy-read partition
// persists the normal Runner turn through its established baseline path.
type PolicyAwareTurnCommitter struct {
	gate     *PolicyController
	adaptive adaptiveTurnWriter
	baseline baselineTurnWriter
}

// NewPolicyAwareTurnCommitter routes outcomes through the fresh distributed gate.
func NewPolicyAwareTurnCommitter(
	gate *PolicyController,
	adaptive adaptiveTurnWriter,
	baseline baselineTurnWriter,
) *PolicyAwareTurnCommitter {
	return &PolicyAwareTurnCommitter{gate: gate, adaptive: adaptive, baseline: baseline}
}

// CommitTurn persists through the adaptive or baseline path selected at commit time.
func (c *PolicyAwareTurnCommitter) CommitTurn(
	ctx context.Context,
	turn conversations.AppendTurnParams,
	metric *sqlc.InsertCacheMetricParams,
	event Event,
) error {
	if c == nil || c.gate == nil || c.adaptive == nil || c.baseline == nil {
		return errors.New("policy-aware adaptive turn committer is not fully configured")
	}
	gate, gateErr := c.gate.Gate(ctx, event.DecisionID)
	if gateErr != nil || !gate.Observe {
		if metric == nil {
			if err := c.baseline.AppendTurn(ctx, turn); err != nil {
				return fmt.Errorf("persist baseline turn after adaptive gate: %w", err)
			}
			return nil
		}
		if err := c.baseline.AppendAssistantTurnWithCacheMetric(ctx, turn, *metric); err != nil {
			return fmt.Errorf("persist baseline assistant after adaptive gate: %w", err)
		}
		return nil
	}
	return c.adaptive.CommitTurn(ctx, turn, metric, event)
}
