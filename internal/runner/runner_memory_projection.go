package runner

import (
	"context"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/conversations"
)

const defaultConversationProjectionPageSize = 100

// ConversationProjectionSource is the PostgreSQL-authoritative replay feed.
type ConversationProjectionSource interface {
	ListProjectionTurns(context.Context, string, conversations.ProjectionCursor, int) ([]conversations.ProjectionTurn, conversations.ProjectionCursor, error)
}

// ConversationProjectionSink is the rebuildable derived graph boundary.
type ConversationProjectionSink interface {
	ApplyConversationProjection(context.Context, arcadedb.ConversationProjection) error
}

// ConversationProjector moves ordered authoritative pages into the derived graph.
type ConversationProjector struct {
	source   ConversationProjectionSource
	sink     ConversationProjectionSink
	pageSize int
}

// NewConversationProjector binds an authoritative source to one derived sink.
func NewConversationProjector(source ConversationProjectionSource, sink ConversationProjectionSink, pageSize int) *ConversationProjector {
	if pageSize <= 0 {
		pageSize = defaultConversationProjectionPageSize
	}
	return &ConversationProjector{source: source, sink: sink, pageSize: pageSize}
}

// ProjectPage applies one ordered page and returns its authoritative cursor.
// The cursor advances only after every graph write succeeds, so a retry replays
// the whole page instead of skipping a partially projected tail.
func (p *ConversationProjector) ProjectPage(
	ctx context.Context,
	identityID string,
	after conversations.ProjectionCursor,
) (conversations.ProjectionCursor, error) {
	if p == nil || p.source == nil || p.sink == nil {
		return after, fmt.Errorf("runner: conversation projector is not initialized")
	}
	if strings.TrimSpace(identityID) == "" {
		return after, fmt.Errorf("runner: conversation projection identity must be non-empty")
	}
	turns, next, err := p.source.ListProjectionTurns(ctx, identityID, after, p.pageSize)
	if err != nil {
		return after, fmt.Errorf("runner: list conversation projection page: %w", err)
	}
	if len(turns) == 0 {
		return next, nil
	}
	projections := make([]arcadedb.ConversationProjection, 0)
	byConversation := make(map[string]int)
	for _, turn := range turns {
		if turn.IdentityID != identityID {
			return after, fmt.Errorf("runner: projection source returned foreign identity %q", turn.IdentityID)
		}
		index, ok := byConversation[turn.ConversationID]
		if !ok {
			index = len(projections)
			byConversation[turn.ConversationID] = index
			projections = append(projections, arcadedb.ConversationProjection{
				IdentityID: identityID, ConversationID: turn.ConversationID,
			})
		}
		projections[index].Turns = append(projections[index].Turns, arcadedb.ConversationTurnProjection{
			IdentityID: turn.IdentityID, ConversationID: turn.ConversationID,
			Seq: turn.Seq, Role: turn.Role, Content: turn.Content,
			ContentHash: turn.ContentHash, OccurredAt: turn.OccurredAt, SourceRef: turn.SourceRef,
		})
	}
	for _, projection := range projections {
		if err := p.sink.ApplyConversationProjection(ctx, projection); err != nil {
			return after, fmt.Errorf("runner: apply conversation projection %s: %w", projection.ConversationID, err)
		}
	}
	return next, nil
}

// OfferConversation schedules fail-soft projection work for an identity.
func (p *ConversationProjector) OfferConversation(identityID string) bool { return false }

// Flush waits for every accepted offer and reports projection failures.
func (p *ConversationProjector) Flush(ctx context.Context) error {
	return fmt.Errorf("runner: conversation projection flush: not implemented")
}

// Reconcile replays PostgreSQL from the beginning and prunes absent graph records.
func (p *ConversationProjector) Reconcile(ctx context.Context, identityID string) error {
	return fmt.Errorf("runner: conversation projection reconcile: not implemented")
}

// DeleteConversation removes one derived projection idempotently.
func (p *ConversationProjector) DeleteConversation(ctx context.Context, identityID, conversationID string) error {
	return fmt.Errorf("runner: delete conversation projection: not implemented")
}

// Close drains accepted work and joins the ordered worker.
func (p *ConversationProjector) Close(ctx context.Context) error {
	return fmt.Errorf("runner: close conversation projector: not implemented")
}
