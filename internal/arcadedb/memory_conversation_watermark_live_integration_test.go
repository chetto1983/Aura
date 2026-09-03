//go:build arcadedb_integration

// The watermark measurement, against a live ArcadeDB.
//
// `projected_through_seq` was written by every projection and read by nothing until the
// context ladder needed it. A unit test would only restate the SQL this package emits;
// what has to hold is that the number a projection wrote is the number a later read gets
// back, and that a conversation nobody projected answers 0 rather than failing.
//
// Run: go test -tags arcadedb_integration ./internal/arcadedb/ -run Watermark
package arcadedb

import (
	"context"
	"testing"
	"time"
)

func TestProjectedThroughSeqReportsWhatTheProjectionWrote(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	const identity = "watermark-identity"
	const conversation = "watermark-conversation"

	// Nothing projected yet: 0, and no error. A caller reads that as "claim nothing".
	before, err := client.ProjectedThroughSeq(ctx, identity, conversation)
	if err != nil {
		t.Fatalf("ProjectedThroughSeq on an unprojected conversation: %v", err)
	}
	if before != 0 {
		t.Fatalf("watermark = %d before any projection, want 0", before)
	}

	now := time.Now().UTC()
	projection := ConversationProjection{IdentityID: identity, ConversationID: conversation}
	for _, seq := range []int{1, 2, 7} {
		content := "turn content"
		projection.Turns = append(projection.Turns, ConversationTurnProjection{
			IdentityID: identity, ConversationID: conversation, Seq: seq,
			Role: "user", Content: content, ContentHash: conversationContentHash(content),
			OccurredAt: now, SourceRef: "postgres://watermark/turn",
		})
	}
	if err := client.ApplyConversationProjection(ctx, projection); err != nil {
		t.Fatalf("ApplyConversationProjection: %v", err)
	}

	// The HIGHEST seq, not the count: turn sequences have gaps, so a count would claim
	// turns the graph does not hold.
	after, err := client.ProjectedThroughSeq(ctx, identity, conversation)
	if err != nil {
		t.Fatalf("ProjectedThroughSeq: %v", err)
	}
	if after != 7 {
		t.Fatalf("watermark = %d, want the highest projected seq (7)", after)
	}

	// Another identity's conversation is not this one's, even under the same name.
	other, err := client.ProjectedThroughSeq(ctx, "someone-else", conversation)
	if err != nil {
		t.Fatalf("ProjectedThroughSeq(other identity): %v", err)
	}
	if other != 0 {
		t.Fatalf("watermark = %d for another identity, want 0", other)
	}
}
