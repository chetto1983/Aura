package conversations

import (
	"context"
	"errors"
	"time"
)

var errConversationProjectionNotImplemented = errors.New("conversation projection is not implemented")

// ProjectionCursor is the stable PostgreSQL source position used by replay and
// reconciliation. ConversationID and Seq are ordered together.
type ProjectionCursor struct {
	ConversationID string
	Seq            int
}

// ProjectionTurn is the searchable, reasoning-free projection of one
// authoritative PostgreSQL conversation turn.
type ProjectionTurn struct {
	IdentityID     string
	ConversationID string
	Seq            int
	Role           string
	Content        string
	ContentHash    string
	OccurredAt     time.Time
	SourceRef      string
}

func projectionTurnEligible(role, content, toolCallID string, toolCalls []byte) bool {
	return false
}

// ListProjectionTurns reads one stable page of eligible authoritative turns.
func (s *Store) ListProjectionTurns(
	ctx context.Context,
	identityID string,
	after ProjectionCursor,
	limit int,
) ([]ProjectionTurn, ProjectionCursor, error) {
	return nil, after, errConversationProjectionNotImplemented
}
