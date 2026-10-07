package conversations

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrTurnDecisionTarget is returned when no user turn sits at the seq a decision addresses.
var ErrTurnDecisionTarget = errors.New("conversations: no user turn at the decision's seq")

// TurnDecision is how a user turn's reasoning effort was decided (migration 0137). Every
// field is optional and "" is NULL, which is how a turn with no effort field differs from
// one that recorded the explicit effort `none`. The source vocabulary is the column's CHECK.
//
// arcadedb.TurnDecision declares the same fields in the same order: the projector converts
// one into the other, so a field added here must be added there too.
type TurnDecision struct {
	ContextKey      string
	Effort          string
	EffortRequested string
	EffortSource    string
	RouteKey        string
	PolicyVersion   string
	OriginRef       string
}

// RecordTurnDecision writes one user turn's decision provenance in one statement, scoped by
// the caller's identity (RLS) and by the conversation.
func (s *Store) RecordTurnDecision(ctx context.Context, conversationID string, seq int, d TurnDecision) error {
	id, err := db.ParseUUID("conversation_id", conversationID)
	if err != nil {
		return fmt.Errorf("record turn decision: %w", err)
	}
	if seq <= 0 {
		return fmt.Errorf("record turn decision %s: seq %d: %w", conversationID, seq, ErrTurnDecisionTarget)
	}
	return db.WithCallerIdentityTx(ctx, s.pool, func(q *sqlc.Queries) error {
		n, err := q.RecordConversationTurnDecision(ctx, sqlc.RecordConversationTurnDecisionParams{
			RecallContextKey:             optionalText(d.ContextKey),
			ReasoningEffort:              optionalText(d.Effort),
			ReasoningEffortRequested:     optionalText(d.EffortRequested),
			ReasoningEffortSource:        optionalText(d.EffortSource),
			ReasoningEffortRouteKey:      optionalText(d.RouteKey),
			ReasoningEffortPolicyVersion: optionalText(d.PolicyVersion),
			ReasoningEffortOriginRef:     optionalText(d.OriginRef),
			ConversationID:               id,
			Seq:                          int32(seq),
		})
		if err != nil {
			return fmt.Errorf("record turn decision %s seq %d: %w", conversationID, seq, err)
		}
		if n == 0 {
			return fmt.Errorf("record turn decision %s seq %d: %w", conversationID, seq, ErrTurnDecisionTarget)
		}
		return nil
	})
}

func turnDecisionFromColumns(key, effort, requested, source, route, policy, origin pgtype.Text) TurnDecision {
	return TurnDecision{
		ContextKey: key.String, Effort: effort.String, EffortRequested: requested.String,
		EffortSource: source.String, RouteKey: route.String, PolicyVersion: policy.String, OriginRef: origin.String,
	}
}
