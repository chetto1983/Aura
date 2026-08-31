//go:build arcadedb_integration

package arcadedb

import (
	"context"
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
