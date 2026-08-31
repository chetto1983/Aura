package arcadedb

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestConversationSchemaStatements(t *testing.T) {
	statements := conversationSchemaStatements()
	if len(statements) == 0 {
		t.Fatal("conversation schema is empty")
	}
	joined := strings.Join(statements, "\n")
	for _, statement := range statements {
		if strings.HasPrefix(statement, "CREATE") && !strings.Contains(statement, "IF NOT EXISTS") {
			t.Fatalf("schema statement is not replay-safe: %s", statement)
		}
	}
	for _, required := range []string{
		"Conversation", "ConversationTurn", "HAS_TURN", "NEXT_TURN",
		"identity_id", "conversation_id", "turn_seq", "role", "content",
		"content_hash", "occurred_at", "source_ref", "deleted_at", "embedding",
		"FULL_TEXT", "LSM_VECTOR",
	} {
		if !strings.Contains(joined, required) {
			t.Errorf("conversation schema missing %q", required)
		}
	}
	for _, forbidden := range []string{"reasoning", "tool_calls", "tool_call_id", "raw_result"} {
		if strings.Contains(strings.ToLower(joined), forbidden) {
			t.Errorf("conversation schema exposes forbidden field %q", forbidden)
		}
	}
}

func TestConversationProjectionIsIdempotentAndIdentityScoped(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	projection := ConversationProjection{
		IdentityID: "identity-a", ConversationID: "conversation-1",
		Turns: []ConversationTurnProjection{{
			IdentityID: "identity-a", ConversationID: "conversation-1", Seq: 1,
			Role: "user", Content: "Remember the blue notebook", ContentHash: "hash-1",
			OccurredAt: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC),
			SourceRef:  "postgres://conversation/conversation-1/turn/1",
		}},
	}
	if err := client.ApplyConversationProjection(context.Background(), projection); err != nil {
		t.Fatalf("ApplyConversationProjection(first): %v", err)
	}
	if err := client.ApplyConversationProjection(context.Background(), projection); err != nil {
		t.Fatalf("ApplyConversationProjection(replay): %v", err)
	}
	joined := rec.joined()
	if !strings.Contains(joined, "UPSERT") || !strings.Contains(joined, "IF NOT EXISTS") {
		t.Fatalf("projection is not idempotent:\n%s", joined)
	}
	for _, params := range rec.params {
		if got, ok := params["identity_id"]; ok && got != "identity-a" {
			t.Fatalf("foreign identity reached graph: %v", got)
		}
	}

	foreign := projection
	foreign.Turns = append([]ConversationTurnProjection(nil), projection.Turns...)
	foreign.Turns[0].IdentityID = "identity-b"
	before := len(rec.statements)
	if err := client.ApplyConversationProjection(context.Background(), foreign); err == nil {
		t.Fatal("foreign turn identity accepted")
	}
	if len(rec.statements) != before {
		t.Fatal("foreign projection reached ArcadeDB")
	}
}
