package arcadedb

import (
	"context"
	"errors"
	"time"
)

// ReasoningDeleteSelector names one identity-scoped reasoning source to remove.
type ReasoningDeleteSelector struct {
	IdentityID     string
	TraceID        string
	ConversationID string
	SourceRef      string
}

// DeleteExpiredReasoning removes at most limit terminal traces eligible at now.
func (c *Client) DeleteExpiredReasoning(context.Context, string, time.Time, int) (int, error) {
	return 0, errors.New("arcadedb: reasoning expiry lifecycle not implemented")
}

// DeleteReasoningBySource removes one identity-scoped trace source immediately.
func (c *Client) DeleteReasoningBySource(context.Context, ReasoningDeleteSelector) (int, error) {
	return 0, errors.New("arcadedb: reasoning source deletion not implemented")
}
