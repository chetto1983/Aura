package arcadedb

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
		"FULL_TEXT", "LSM_VECTOR", "embed_space", "NULL_STRATEGY INDEX",
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

// The reconciler replays EVERY projected turn once a minute (NewDeleteReconciler in
// cmd/aura/chat_boot.go), so embedding a turn whose content and vector are already stored
// made the sidecar's load grow with the whole conversation history rather than with what
// changed. Measured 2026-09-14 on the appliance: the same four turns re-embedded every
// ~60 s, queued on the sidecar's single slot between the passages of a document ingest.
func TestConversationProjectionReplayEmbedsOnlyWhatChanged(t *testing.T) {
	const content = "Remember the blue notebook"
	storedWithVector := func(hash string) string {
		return `{"result":[{"turn_seq":1,"content_hash":"` + hash + `"}]}`
	}
	older := storedWithVector(conversationContentHash("an older wording"))
	tests := []struct {
		name         string
		stored       string
		embedderDown bool
		refuse       bool
		wantEmbeds   int
		wantWritten  bool
		wantCleared  bool
		wantStamp    any
	}{
		{"unchanged turn keeps its stored vector", storedWithVector(conversationContentHash(content)), false, false, 0, false, false, nil},
		{"edited turn is embedded again", older, false, false, 1, true, false, stubSpace},
		{"turn stored without a vector is embedded", `{"result":[]}`, false, false, 1, true, false, stubSpace},
		// Before this check a sidecar outage of one minute removed the vector of every turn
		// in the history, because the replay treated "could not embed now" as "stale".
		{"embedder down keeps a vector that still matches", storedWithVector(conversationContentHash(content)), true, false, 0, false, false, nil},
		{"embedder down still clears a vector of older content", older, true, false, 1, false, true, nil},
		{"a refused turn keeps the space that refused it", `{"result":[]}`, false, true, 1, false, true, stubSpace},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}
			if tt.embedderDown {
				stub.err = errors.New("sidecar down")
			}
			refusing := &refusingEmbedder{refuse: []string{content}, status: http.StatusBadRequest, space: stubSpace}
			var embedder DenseEmbedder = stub
			embeds := func() int { return len(stub.calls) }
			if tt.refuse {
				embedder = refusing
				embeds = func() int {
					n := 0
					for _, call := range refusing.calls {
						if !strings.HasSuffix(call[0], controlInput) {
							n++
						}
					}
					return n
				}
			}
			client, requests := routedClient(t, func(request recordedRequest) testResponse {
				statement, _ := request.Payload["command"].(string)
				if strings.HasPrefix(statement, "SELECT") && strings.Contains(statement, "embedding IS NOT NULL") {
					return testResponse{Body: tt.stored}
				}
				return testResponse{Body: `{"result":[]}`}
			})
			client.WithEmbedder(embedder)
			projection := ConversationProjection{
				IdentityID: "identity-a", ConversationID: "conversation-1",
				Turns: []ConversationTurnProjection{{
					IdentityID: "identity-a", ConversationID: "conversation-1", Seq: 1,
					Role: "user", Content: content, ContentHash: conversationContentHash(content),
					OccurredAt: time.Date(2026, 9, 14, 6, 0, 0, 0, time.UTC),
					SourceRef:  "postgres://conversation/conversation-1/turn/1",
				}},
			}
			if err := client.ApplyConversationProjection(context.Background(), projection); err != nil {
				t.Fatalf("ApplyConversationProjection: %v", err)
			}
			if embeds() != tt.wantEmbeds {
				t.Fatalf("embedder called %d times, want %d", embeds(), tt.wantEmbeds)
			}
			var wroteTurn, wroteVector, clearedVector bool
			var stamp any
			for _, request := range *requests {
				statement, _ := request.Payload["command"].(string)
				if !strings.Contains(statement, "content_hash = :content_hash") {
					continue
				}
				wroteTurn = true
				if !strings.Contains(statement, "embedding = :embedding") {
					continue
				}
				params, _ := request.Payload["params"].(map[string]any)
				stamp = params["embed_space"]
				if params["embedding"] != nil {
					wroteVector = true
				} else {
					clearedVector = true
				}
			}
			if !wroteTurn {
				t.Fatal("replay no longer writes the turn it exists to repair")
			}
			if wroteVector != tt.wantWritten || clearedVector != tt.wantCleared || stamp != tt.wantStamp {
				t.Fatalf("vector written=%v cleared=%v stamp=%v, want written=%v cleared=%v stamp=%v",
					wroteVector, clearedVector, stamp, tt.wantWritten, tt.wantCleared, tt.wantStamp)
			}
		})
	}
}

// The projection must survive a database that has no reasoning schema, because the
// INITIATED_BY link READS ReasoningTrace -- a type the conversation schema neither
// creates nor owns. The first version of that line returned the error like the two
// writes above it, on the reasoning that a backend which cannot serve one write did not
// serve the others either. That is true for the two that write types this schema creates
// and false for the one that reads someone else's: on a database provisioned with
// conversations only, the statement raises and the whole projection was lost -- every
// turn, for that identity, on every replay. Caught by the live tier on 2026-09-04, where
// all three conversation-projection tests failed at their first Apply.
func TestConversationProjectionSurvivesAnAbsentReasoningSchema(t *testing.T) {
	client, requests := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		if strings.Contains(statement, "CREATE EDGE INITIATED_BY") {
			return testResponse{
				Status: http.StatusBadRequest,
				Body:   `{"detail":"Type with name 'ReasoningTrace' was not found"}`,
			}
		}
		return testResponse{Body: `{"result":[]}`}
	})

	projection := ConversationProjection{
		IdentityID: "identity-a", ConversationID: "conversation-1",
		Turns: []ConversationTurnProjection{{
			IdentityID: "identity-a", ConversationID: "conversation-1", Seq: 1,
			Role: "user", Content: "restartgapblue",
			ContentHash: conversationContentHash("restartgapblue"),
			OccurredAt:  time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC),
			SourceRef:   "postgres://conversation/conversation-1/turn/1",
		}},
	}
	if err := client.ApplyConversationProjection(context.Background(), projection); err != nil {
		t.Fatalf("a missing reasoning schema lost the whole projection: %v", err)
	}

	// The turn itself still has to be written: surviving the decoration's failure is not
	// the same as skipping the work the projection exists for.
	var wroteTurn bool
	for _, request := range *requests {
		statement, _ := request.Payload["command"].(string)
		if strings.Contains(statement, "UPDATE "+conversationTurnType) &&
			strings.Contains(statement, "content_hash") {
			wroteTurn = true
		}
	}
	if !wroteTurn {
		t.Fatal("projection returned success without writing its turn")
	}
}

// The reconciler replays every conversation once a minute; a projection of several changed
// turns must reach the embedder as ONE request, not one per turn (spec §5, Turns).
func TestConversationProjectionEmbedsItsChangedTurnsInOneRequest(t *testing.T) {
	embedder := &stubEmbedder{vectors: [][][]float64{{vectorOf(1), vectorOf(2), vectorOf(3)}}}
	client, _ := routedClient(t, func(recordedRequest) testResponse { return testResponse{Body: `{"result":[]}`} })
	client.WithEmbedder(embedder)
	projection := ConversationProjection{IdentityID: "identity-a", ConversationID: "conversation-1"}
	for seq := 1; seq <= 3; seq++ {
		content := fmt.Sprintf("turn number %d", seq)
		projection.Turns = append(projection.Turns, ConversationTurnProjection{
			IdentityID: "identity-a", ConversationID: "conversation-1", Seq: seq,
			Role: "user", Content: content, ContentHash: conversationContentHash(content),
			OccurredAt: time.Date(2026, 9, 23, 6, 0, seq, 0, time.UTC),
			SourceRef:  fmt.Sprintf("postgres://conversation/conversation-1/turn/%d", seq),
		})
	}
	if err := client.ApplyConversationProjection(context.Background(), projection); err != nil {
		t.Fatalf("ApplyConversationProjection: %v", err)
	}
	if len(embedder.calls) != 1 || len(embedder.calls[0]) != 3 {
		t.Fatalf("embedder calls = %v, want one request carrying the three turns", embedder.calls)
	}
}
