package runner

import (
	"context"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/conversations"
)

type tracerProjectionSource struct {
	turns []conversations.ProjectionTurn
}

func (s tracerProjectionSource) ListProjectionTurns(
	context.Context,
	string,
	conversations.ProjectionCursor,
	int,
) ([]conversations.ProjectionTurn, conversations.ProjectionCursor, error) {
	return append([]conversations.ProjectionTurn(nil), s.turns...),
		conversations.ProjectionCursor{ConversationID: "conversation-1", Seq: 1}, nil
}

type tracerProjectionSink struct {
	projections []arcadedb.ConversationProjection
}

func (s *tracerProjectionSink) ApplyConversationProjection(_ context.Context, p arcadedb.ConversationProjection) error {
	s.projections = append(s.projections, p)
	return nil
}

func TestConversationProjectionTracer(t *testing.T) {
	source := tracerProjectionSource{turns: []conversations.ProjectionTurn{{
		IdentityID: "identity-a", ConversationID: "conversation-1", Seq: 1,
		Role: "user", Content: "Remember the blue notebook", ContentHash: "hash-1",
		OccurredAt: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC),
		SourceRef:  "postgres://conversation/conversation-1/turn/1",
	}}}
	sink := &tracerProjectionSink{}
	projector := NewConversationProjector(source, sink, 16)

	cursor, err := projector.ProjectPage(context.Background(), "identity-a", conversations.ProjectionCursor{})
	if err != nil {
		t.Fatalf("ProjectPage: %v", err)
	}
	if cursor.ConversationID != "conversation-1" || cursor.Seq != 1 {
		t.Fatalf("cursor = %+v", cursor)
	}
	if len(sink.projections) != 1 || len(sink.projections[0].Turns) != 1 {
		t.Fatalf("projections = %+v", sink.projections)
	}
	projected := sink.projections[0].Turns[0]
	if projected.IdentityID != "identity-a" || projected.SourceRef == "" {
		t.Fatalf("projected turn lost identity/provenance: %+v", projected)
	}
}
