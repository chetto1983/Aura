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
			Role: "user", Content: "Remember the blue notebook",
			ContentHash: conversationContentHash("Remember the blue notebook"),
			OccurredAt:  time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC),
			SourceRef:   "postgres://conversation/conversation-1/turn/1",
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

func TestConversationProjectionSearchFailsClosedAcrossIdentity(t *testing.T) {
	const row = `{"result":[{"identity_id":"identity-a","conversation_id":"conversation-1","turn_seq":1,"role":"user","content":"Remember the blue notebook","content_hash":"hash-1","occurred_at":"2026-08-31T12:00:00Z","source_ref":"postgres://conversation/conversation-1/turn/1"}]}`
	client, rec := recordingClient(t, row)
	result, err := client.SearchConversationTurnsHybrid(context.Background(), "identity-a", "blue notebook", 5)
	if err != nil {
		t.Fatalf("SearchConversationTurnsHybrid: %v", err)
	}
	if len(result.Turns) != 1 || result.Turns[0].SourceRef == "" {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(rec.statements[0], "identity_id = :identity_id") || rec.params[0]["identity_id"] != "identity-a" {
		t.Fatalf("search is not identity-scoped: %s params=%v", rec.statements[0], rec.params[0])
	}

	foreign, err := client.SearchConversationTurnsHybrid(context.Background(), "identity-b", "blue notebook", 5)
	if err != nil {
		t.Fatalf("foreign SearchConversationTurnsHybrid: %v", err)
	}
	if len(foreign.Turns) != 0 {
		t.Fatalf("foreign identity observed turn: %+v", foreign.Turns)
	}
}

// Projection is the only place the reasoning->turn edge can be closed: the trace is
// written synchronously at commit time, when the ConversationTurn vertex does not exist
// yet, and ArcadeDB answers a CREATE EDGE with an empty TO side by doing nothing at all
// and reporting success. Measured 2026-09-03 on the live graph: 89 traces, 0 INITIATED_BY.
func TestConversationProjectionClosesReasoningInitiatorEdge(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	projection := ConversationProjection{
		IdentityID: "identity-a", ConversationID: "conversation-1",
		Turns: []ConversationTurnProjection{{
			IdentityID: "identity-a", ConversationID: "conversation-1", Seq: 18,
			Role: "assistant", Content: "The blue notebook is on the shelf",
			ContentHash: conversationContentHash("The blue notebook is on the shelf"),
			OccurredAt:  time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC),
			SourceRef:   "postgres://conversation/conversation-1/turn/18",
		}},
	}
	if err := client.ApplyConversationProjection(context.Background(), projection); err != nil {
		t.Fatalf("ApplyConversationProjection: %v", err)
	}
	var linking []int
	for i, statement := range rec.statements {
		if strings.Contains(statement, "CREATE EDGE INITIATED_BY") {
			linking = append(linking, i)
		}
	}
	if len(linking) != 1 {
		t.Fatalf("expected exactly one INITIATED_BY link per projected turn, got %d:\n%s",
			len(linking), rec.joined())
	}
	statement := rec.statements[linking[0]]
	if !strings.Contains(statement, "IF NOT EXISTS") {
		t.Errorf("reasoning initiator link is not replay-safe: %s", statement)
	}
	if !strings.Contains(statement, reasoningTraceType) || !strings.Contains(statement, conversationTurnType) {
		t.Errorf("link does not join a trace to its turn: %s", statement)
	}
	params := rec.params[linking[0]]
	for name, want := range map[string]any{
		"identity_id": "identity-a", "conversation_id": "conversation-1", "turn_seq": float64(18),
	} {
		if got := params[name]; got != want {
			t.Errorf("link bound %s = %v, want %v", name, got, want)
		}
	}
}
