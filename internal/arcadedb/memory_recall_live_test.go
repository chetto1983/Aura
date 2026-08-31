//go:build arcadedb_integration

package arcadedb

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestMemoryRecallLive_VectorFuseMixedTier(t *testing.T) {
	client := disposableMemoryClient(t).WithEmbedder(liveEmbedder(t))
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	identityID := "identity-memory-recall-live"
	query := "aurora notebook Turin"

	write(t, client, Fact{
		Subject: "Aurora notebook", Predicate: "stored_in", Object: "Turin archive",
		Statement: "The aurora notebook is stored in the Turin archive.",
		Source:    FactSource{RunID: "memory-recall-live", MemoryIDs: []string{"fact-source"}, WriterRole: WriterParent},
	}, now)
	content := "We discussed the aurora notebook route through Turin."
	projection := ConversationProjection{
		IdentityID: identityID, ConversationID: "conversation-memory-recall-live",
		Turns: []ConversationTurnProjection{
			{
				IdentityID: identityID, ConversationID: "conversation-memory-recall-live", Seq: 1,
				Role: "assistant", Content: "I found the earlier travel note.",
				ContentHash: conversationContentHash("I found the earlier travel note."),
				OccurredAt:  now.Add(time.Second), SourceRef: "postgres://memory-recall-live/turn/1",
			},
			{
				IdentityID: identityID, ConversationID: "conversation-memory-recall-live", Seq: 2,
				Role: "user", Content: content, ContentHash: conversationContentHash(content),
				OccurredAt: now.Add(2 * time.Second), SourceRef: "postgres://memory-recall-live/turn/2",
			},
		},
	}
	if err := client.ApplyConversationProjection(ctx, projection); err != nil {
		t.Fatalf("ApplyConversationProjection: %v", err)
	}

	result, err := client.RecallMemory(ctx, RecallRequest{
		IdentityID: identityID, Query: query, Limit: 5,
	})
	if err != nil {
		t.Fatalf("RecallMemory: %v", err)
	}
	if result.Retrieval.Path != retrievalPathHybrid || result.Retrieval.EffectivePath != effectivePathMixed {
		t.Fatalf("retrieval = %+v; evidence = %+v", result.Retrieval, result.Evidence)
	}
	if result.Retrieval.FactCount != 1 || result.Retrieval.ConversationCount != 1 {
		t.Fatalf("tier counts = %+v", result.Retrieval)
	}
	for _, evidence := range result.Evidence {
		if evidence.Kind == RecallEvidenceConversation &&
			(len(evidence.Conversation.Turns) != 2 || evidence.Conversation.AnchorSeq != 2) {
			t.Fatalf("conversation evidence = %+v", evidence.Conversation)
		}
	}
}

func TestMemoryRecallLive_BrowseCursor(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	identityID := "identity-memory-recall-browse"
	conversationID := "conversation-memory-recall-browse"
	projection := ConversationProjection{IdentityID: identityID, ConversationID: conversationID}
	for seq, content := range []string{
		"The first bounded turn.", "The second bounded turn.",
		"The third bounded turn.", "The fourth bounded turn.",
	} {
		projection.Turns = append(projection.Turns, ConversationTurnProjection{
			IdentityID: identityID, ConversationID: conversationID, Seq: seq + 1,
			Role: "user", Content: content, ContentHash: conversationContentHash(content),
			OccurredAt: now.Add(time.Duration(seq) * time.Second),
			SourceRef:  fmt.Sprintf("postgres://memory-recall-browse/turn/%d", seq+1),
		})
	}
	if err := client.ApplyConversationProjection(ctx, projection); err != nil {
		t.Fatalf("ApplyConversationProjection: %v", err)
	}

	opened, err := client.RecallMemory(ctx, RecallRequest{
		IdentityID: identityID, Mode: RecallModeOpen,
		ConversationID: conversationID, AnchorSeq: 2,
		Direction: RecallDirectionAfter, Limit: 2,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if len(opened.Evidence) != 1 || len(opened.Evidence[0].Conversation.Turns) != 2 || opened.NextCursor == "" {
		t.Fatalf("opened = %+v", opened)
	}
	cursor, err := decodeRecallCursor(opened.NextCursor)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	if cursor.AnchorSeq != 3 || cursor.PageSize != 2 {
		t.Fatalf("cursor = %+v", cursor)
	}

	scrolled, err := client.RecallMemory(ctx, RecallRequest{
		IdentityID: identityID, Mode: RecallModeScroll,
		ConversationID: conversationID, AnchorSeq: cursor.AnchorSeq,
		Direction: cursor.Direction, Cursor: opened.NextCursor, Limit: cursor.PageSize,
	})
	if err != nil {
		t.Fatalf("scroll: %v", err)
	}
	turns := scrolled.Evidence[0].Conversation.Turns
	if len(turns) != 2 || turns[0].Seq != 3 || turns[1].Seq != 4 {
		t.Fatalf("scrolled turns = %+v", turns)
	}
}
