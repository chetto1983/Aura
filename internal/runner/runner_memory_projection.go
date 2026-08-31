package runner

import (
	"context"
	"errors"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/conversations"
)

var errConversationProjectorNotImplemented = errors.New("runner: conversation projector is not implemented")

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
	return &ConversationProjector{source: source, sink: sink, pageSize: pageSize}
}

// ProjectPage applies one ordered page and returns its authoritative cursor.
func (p *ConversationProjector) ProjectPage(
	ctx context.Context,
	identityID string,
	after conversations.ProjectionCursor,
) (conversations.ProjectionCursor, error) {
	return after, errConversationProjectorNotImplemented
}
