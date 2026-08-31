package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestConversationProjectionTracer(t *testing.T) {
	const content = "Remember the blue notebook"
	sum := sha256.Sum256([]byte(content))
	contentHash := hex.EncodeToString(sum[:])
	source := tracerProjectionSource{turns: []conversations.ProjectionTurn{{
		IdentityID: "identity-a", ConversationID: "conversation-1", Seq: 1,
		Role: "user", Content: content, ContentHash: contentHash,
		OccurredAt: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC),
		SourceRef:  "postgres://conversation/conversation-1/turn/1",
	}}}
	var commandCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(raw, &payload)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/query/aura" {
			_, _ = io.WriteString(w, `{"result":[{"identity_id":"identity-a","conversation_id":"conversation-1","turn_seq":1,"role":"user","content":"Remember the blue notebook","content_hash":"`+contentHash+`","occurred_at":"2026-08-31T12:00:00Z","source_ref":"postgres://conversation/conversation-1/turn/1"}]}`)
			return
		}
		if payload.Command != "" {
			commandCount++
		}
		_, _ = io.WriteString(w, `{"result":[]}`)
	}))
	t.Cleanup(server.Close)
	sink, err := arcadedb.New(arcadedb.Config{BaseURL: server.URL, Database: "aura", User: "root"})
	if err != nil {
		t.Fatalf("arcadedb.New: %v", err)
	}
	projector := NewConversationProjector(source, sink, 16)

	cursor, err := projector.ProjectPage(context.Background(), "identity-a", conversations.ProjectionCursor{})
	if err != nil {
		t.Fatalf("ProjectPage: %v", err)
	}
	if cursor.ConversationID != "conversation-1" || cursor.Seq != 1 {
		t.Fatalf("cursor = %+v", cursor)
	}
	if commandCount != 4 {
		t.Fatalf("ArcadeDB commands = %d, want conversation upsert, turn upsert, HAS_TURN, NEXT_TURN", commandCount)
	}
	result, err := sink.SearchConversationTurnsHybrid(context.Background(), "identity-a", "blue notebook", 5)
	if err != nil {
		t.Fatalf("SearchConversationTurnsHybrid: %v", err)
	}
	if len(result.Turns) != 1 || result.Turns[0].Content != content || result.Turns[0].SourceRef == "" {
		t.Fatalf("search result lost content/provenance: %+v", result)
	}
}
