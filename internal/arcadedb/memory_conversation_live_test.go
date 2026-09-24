//go:build arcadedb_integration

package arcadedb

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/embeddings"
)

func conversationProjectionLiveClient(t *testing.T) *Client {
	t.Helper()
	client := disposableArcadeClient(t)
	for _, statement := range conversationSchemaStatements() {
		if _, err := client.Command(context.Background(), statement, nil); err != nil {
			t.Fatalf("conversation schema %q: %v", statement, err)
		}
	}
	return client
}

func liveConversationProjection(identityID, conversationID string, seq int, content string) ConversationProjection {
	return ConversationProjection{
		IdentityID: identityID, ConversationID: conversationID,
		Turns: []ConversationTurnProjection{{
			IdentityID: identityID, ConversationID: conversationID, Seq: seq,
			Role: "user", Content: content, ContentHash: conversationContentHash(content),
			OccurredAt: time.Date(2026, 8, 31, 12, seq, 0, 0, time.UTC),
			SourceRef:  fmt.Sprintf("postgres://aura/conversations/%s/turns/%d", conversationID, seq),
		}},
	}
}

func TestConversationProjectionLive_RestartGapAndReplay(t *testing.T) {
	client := conversationProjectionLiveClient(t)
	projection := liveConversationProjection("identity-a", "conversation-gap", 1, "restartgapblue")
	for range 2 {
		if err := client.ApplyConversationProjection(context.Background(), projection); err != nil {
			t.Fatalf("ApplyConversationProjection: %v", err)
		}
	}
	rows, err := client.Query(context.Background(),
		"SELECT count(*) AS n FROM ConversationTurn WHERE identity_id = :identity_id AND conversation_id = :conversation_id",
		map[string]any{"identity_id": "identity-a", "conversation_id": "conversation-gap"})
	if err != nil {
		t.Fatalf("count projected turns: %v", err)
	}
	if len(rows) != 1 || rowInt(rows[0], "n") != 1 {
		t.Fatalf("replay created duplicates: %+v", rows)
	}
}

type countingEmbedder struct{ texts int }

func (e *countingEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	e.texts += len(texts)
	return constantEmbedder{value: 1}.Embed(ctx, texts)
}

func (e *countingEmbedder) Space(ctx context.Context) (embeddings.Space, error) {
	return constantEmbedder{value: 1}.Space(ctx)
}

// The unit test proves the decision; this proves the query it rests on. A stored vector or
// stamp must count as an answer, and a turn whose vector AND stamp were lost must stop
// counting, or it would never be embedded again. A stamp without a vector is a refusal,
// and that one is kept.
func TestConversationProjectionLive_ReplayEmbedsOnlyWhatChanged(t *testing.T) {
	client := conversationProjectionLiveClient(t)
	embedder := &countingEmbedder{}
	client.WithEmbedder(embedder)
	ctx := context.Background()
	scope := map[string]any{"identity_id": "identity-a", "conversation_id": "conversation-replay"}
	apply := func(content string, wantTexts int) {
		t.Helper()
		if err := client.ApplyConversationProjection(ctx,
			liveConversationProjection("identity-a", "conversation-replay", 1, content)); err != nil {
			t.Fatalf("ApplyConversationProjection(%q): %v", content, err)
		}
		if embedder.texts != wantTexts {
			t.Fatalf("after %q the embedder saw %d texts, want %d", content, embedder.texts, wantTexts)
		}
		rows, err := client.Query(ctx, "SELECT count(*) AS n FROM ConversationTurn"+
			" WHERE identity_id = :identity_id AND conversation_id = :conversation_id AND embedding IS NOT NULL", scope)
		if err != nil || len(rows) != 1 || rowInt(rows[0], "n") != 1 {
			t.Fatalf("after %q the turn has no stored vector: rows=%+v err=%v", content, rows, err)
		}
	}

	apply("replayamber", 1)
	apply("replayamber", 1)
	apply("replaycobalt", 2)
	if _, err := client.Command(ctx, "UPDATE ConversationTurn SET embedding = NULL, embed_space = NULL"+
		" WHERE identity_id = :identity_id AND conversation_id = :conversation_id", scope); err != nil {
		t.Fatalf("clear stored vector: %v", err)
	}
	apply("replaycobalt", 3)
}

func TestConversationProjectionLive_EditReplacesDerivedContent(t *testing.T) {
	client := conversationProjectionLiveClient(t)
	if err := client.ApplyConversationProjection(context.Background(),
		liveConversationProjection("identity-a", "conversation-edit", 1, "beforeeditamber")); err != nil {
		t.Fatalf("ApplyConversationProjection(before): %v", err)
	}
	if err := client.ApplyConversationProjection(context.Background(),
		liveConversationProjection("identity-a", "conversation-edit", 1, "aftereditcobalt")); err != nil {
		t.Fatalf("ApplyConversationProjection(after): %v", err)
	}
	rows, err := client.Query(context.Background(),
		"SELECT content, content_hash FROM ConversationTurn WHERE identity_id = :identity_id AND conversation_id = :conversation_id",
		map[string]any{"identity_id": "identity-a", "conversation_id": "conversation-edit"})
	if err != nil {
		t.Fatalf("read edited turn: %v", err)
	}
	if len(rows) != 1 || rowString(rows[0], "content") != "aftereditcobalt" ||
		rowString(rows[0], "content_hash") != conversationContentHash("aftereditcobalt") {
		t.Fatalf("edit did not replace one derived record: %+v", rows)
	}
}

func TestConversationProjectionLive_DeleteConvergesAndIsIdentityScoped(t *testing.T) {
	client := conversationProjectionLiveClient(t)
	for _, projection := range []ConversationProjection{
		liveConversationProjection("identity-a", "conversation-delete", 1, "deleteviolet"),
		liveConversationProjection("identity-b", "conversation-foreign", 1, "foreignsilver"),
	} {
		if err := client.ApplyConversationProjection(context.Background(), projection); err != nil {
			t.Fatalf("ApplyConversationProjection: %v", err)
		}
	}
	for range 2 {
		if err := client.DeleteConversationProjection(context.Background(), "identity-a", "conversation-delete"); err != nil {
			t.Fatalf("DeleteConversationProjection: %v", err)
		}
	}
	recall := func(identityID, query string) RecallResult {
		t.Helper()
		result, err := client.RecallMemory(context.Background(), RecallRequest{
			IdentityID: identityID, Mode: RecallModeSemantic, Query: query,
		})
		if err != nil {
			t.Fatalf("recall %s %q: %v", identityID, query, err)
		}
		return result
	}
	if deleted := recall("identity-a", "deleteviolet"); len(deleted.Evidence) != 0 {
		t.Fatalf("deleted identity still sees projection: %+v", deleted.Evidence)
	}
	if foreign := recall("identity-b", "foreignsilver"); len(foreign.Evidence) != 1 {
		t.Fatalf("foreign identity was altered by another delete: %+v", foreign.Evidence)
	}
	if err := client.DeleteIdentityConversationProjections(context.Background(), "identity-b"); err != nil {
		t.Fatalf("DeleteIdentityConversationProjections: %v", err)
	}
}
