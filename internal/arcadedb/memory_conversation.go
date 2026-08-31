package arcadedb

import (
	"context"
	"errors"
	"time"
)

var errConversationProjectionNotImplemented = errors.New("arcadedb: conversation projection is not implemented")

// ConversationTurnProjection is one PostgreSQL-authoritative searchable turn.
type ConversationTurnProjection struct {
	IdentityID     string
	ConversationID string
	Seq            int
	Role           string
	Content        string
	ContentHash    string
	OccurredAt     time.Time
	SourceRef      string
}

// ConversationProjection batches authoritative turns under their graph parent.
type ConversationProjection struct {
	IdentityID     string
	ConversationID string
	Turns          []ConversationTurnProjection
}

// ConversationTurnHit is one identity-scoped hybrid-search result.
type ConversationTurnHit struct {
	IdentityID     string
	ConversationID string
	Seq            int
	Role           string
	Content        string
	ContentHash    string
	OccurredAt     string
	SourceRef      string
}

// ConversationSearchResult reports the retrieval path and explicit abstention.
type ConversationSearchResult struct {
	Turns         []ConversationTurnHit
	RetrievalPath string
	Abstained     bool
	Reason        string
}

func conversationSchemaStatements() []string { return nil }

// ApplyConversationProjection idempotently writes eligible turns and their order.
func (c *Client) ApplyConversationProjection(ctx context.Context, projection ConversationProjection) error {
	return errConversationProjectionNotImplemented
}

// SearchConversationTurnsHybrid searches only turns owned by identityID.
func (c *Client) SearchConversationTurnsHybrid(
	ctx context.Context,
	identityID, query string,
	limit int,
) (ConversationSearchResult, error) {
	return ConversationSearchResult{}, errConversationProjectionNotImplemented
}
