//go:build arcadedb_integration

package arcadedb

import (
	"context"
	"fmt"
	"testing"
	"time"
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
	deleted, err := client.SearchConversationTurnsHybrid(context.Background(), "identity-a", "deleteviolet", 5)
	if err != nil {
		t.Fatalf("search deleted identity: %v", err)
	}
	if len(deleted.Turns) != 0 {
		t.Fatalf("deleted identity still sees projection: %+v", deleted.Turns)
	}
	foreign, err := client.SearchConversationTurnsHybrid(context.Background(), "identity-b", "foreignsilver", 5)
	if err != nil {
		t.Fatalf("search foreign identity: %v", err)
	}
	if len(foreign.Turns) != 1 {
		t.Fatalf("foreign identity was altered by another delete: %+v", foreign.Turns)
	}
	if err := client.DeleteIdentityConversationProjections(context.Background(), "identity-b"); err != nil {
		t.Fatalf("DeleteIdentityConversationProjections: %v", err)
	}
}
