//go:build arcadedb_integration

package arcadedb

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"
)

func TestMemoryRecallPeriodLiveCompleteAcrossConversations(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	r := periodTestRequest()
	start := r.From
	for _, fixture := range []struct {
		identity, conversation string
		seqs                   []int
		instants               []time.Time
	}{
		{r.IdentityID, "conversation-a", []int{1, 7, 9, 12}, []time.Time{start.Add(-time.Millisecond), start, start.Add(time.Hour), r.To}},
		{r.IdentityID, "conversation-b", []int{1, 8}, []time.Time{start, start}},
		{r.IdentityID, "conversation-active", []int{1}, []time.Time{start}},
		{r.IdentityID, "conversation-deleted", []int{1}, []time.Time{start}},
		{"identity-b", "conversation-foreign", []int{1}, []time.Time{start}},
	} {
		projection := ConversationProjection{IdentityID: fixture.identity, ConversationID: fixture.conversation}
		for index, seq := range fixture.seqs {
			content := "The same reminder test, from a distinct source turn."
			projection.Turns = append(projection.Turns, ConversationTurnProjection{
				IdentityID: fixture.identity, ConversationID: fixture.conversation, Seq: seq,
				Role: "user", Content: content, ContentHash: conversationContentHash(content),
				OccurredAt: fixture.instants[index],
				SourceRef:  fmt.Sprintf("postgres://aura/conversations/%s/turns/%d", fixture.conversation, seq),
			})
		}
		if err := client.ApplyConversationProjection(ctx, projection); err != nil {
			t.Fatalf("project %s: %v", fixture.conversation, err)
		}
	}
	if _, err := client.Command(ctx, "UPDATE ConversationTurn SET deleted_at = :deleted_at WHERE conversation_id = :conversation_id",
		map[string]any{"deleted_at": r.To.UTC().Format(time.RFC3339), "conversation_id": "conversation-deleted"}); err != nil {
		t.Fatal(err)
	}
	r.ExcludeConversationIDs = []string{"conversation-active"}
	var refs []string
	pages := 0
	for {
		result, err := client.RecallMemory(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		if pages > 4 {
			t.Fatal("period cursor failed to terminate")
		}
		for _, evidence := range result.Evidence {
			if evidence.Fact != nil || evidence.Conversation == nil {
				t.Fatalf("non-conversation evidence: %+v", evidence)
			}
			for _, turn := range evidence.Conversation.Turns {
				if slices.Contains(refs, turn.SourceRef) {
					t.Fatalf("repeated boundary: %s", turn.SourceRef)
				}
				instant, err := time.Parse(time.RFC3339Nano, turn.OccurredAt)
				if err != nil || instant.Before(start) || !instant.Before(r.To) {
					t.Fatalf("turn outside local day: %+v", turn)
				}
				refs = append(refs, turn.SourceRef)
			}
		}
		if result.NextCursor == "" {
			break
		}
		r = RecallRequest{IdentityID: r.IdentityID, Mode: RecallModeScroll, Cursor: result.NextCursor,
			ExcludeConversationIDs: r.ExcludeConversationIDs, From: start, To: r.To}
	}
	want := []string{
		"postgres://aura/conversations/conversation-a/turns/7",
		"postgres://aura/conversations/conversation-b/turns/1",
		"postgres://aura/conversations/conversation-b/turns/8",
		"postgres://aura/conversations/conversation-a/turns/9",
	}
	if !slices.Equal(refs, want) || pages != 2 {
		t.Fatalf("period pages=%d, refs=%v, want %v", pages, refs, want)
	}
}
