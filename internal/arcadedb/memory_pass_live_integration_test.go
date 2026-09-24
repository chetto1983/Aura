//go:build arcadedb_integration

// The pass against a live ArcadeDB: after a route change every memory row is outside the
// space, memory is lexical, and one pass brings it back dense.
//
// Run: arcade-it.sh MemoryPassLive
package arcadedb

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestMemoryPassLiveMovesTheMemoryAndReopensTheGate(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	now := time.Now().UTC()
	routeA := constantEmbedder{value: 1, space: "es1-route-a"}
	for _, subject := range []string{"PassOne", "PassTwo", "PassPoison"} {
		fact := mergeFact(subject, "keeps", "PassObject", subject+" keeps the pass object.")
		if _, err := client.WithEmbedder(routeA).UpsertFact(ctx, fact, now); err != nil {
			t.Fatalf("UpsertFact(%s): %v", subject, err)
		}
	}
	projection := liveConversationProjection("identity-a", "conversation-pass", 1, "passturnamber")
	if err := client.WithEmbedder(routeA).ApplyConversationProjection(ctx, projection); err != nil {
		t.Fatalf("ApplyConversationProjection: %v", err)
	}
	if err := client.WithEmbedder(routeA).UpsertReasoningTrace(ctx, validReasoningTrace()); err != nil {
		t.Fatalf("UpsertReasoningTrace: %v", err)
	}

	routeB := &refusingEmbedder{refuse: []string{"PassPoison keeps the pass object."}, status: http.StatusBadRequest, space: "es1-route-b"}
	onB := client.WithEmbedder(routeB)
	if open, err := onB.memoryDenseOpen(ctx, "es1-route-b"); err != nil || open {
		t.Fatalf("before the pass: open=%v err=%v, want closed", open, err)
	}
	tally, err := onB.reembedMemory(ctx)
	if err != nil {
		t.Fatalf("reembedMemory: %v", err)
	}
	if tally.embedded != 4 || tally.refused != 1 {
		t.Fatalf("tally = %+v, want 2 facts + 1 turn + 1 trace embedded and 1 fact refused", tally)
	}
	onB.memoryGate.checked = time.Time{}
	if open, err := onB.memoryDenseOpen(ctx, "es1-route-b"); err != nil || !open {
		t.Fatalf("after the pass: open=%v err=%v, want open (the refusal does not count)", open, err)
	}

	// Review Focus 1: a stale writer still on route A closes the gate again, and the next
	// run re-embeds what it wrote.
	stale := mergeFact("PassLate", "keeps", "PassObject", "PassLate keeps the pass object.")
	if _, err := client.WithEmbedder(routeA).UpsertFact(ctx, stale, now); err != nil {
		t.Fatalf("stale UpsertFact: %v", err)
	}
	onB = client.WithEmbedder(routeB)
	onB.memoryGate.checked = time.Time{}
	if open, _ := onB.memoryDenseOpen(ctx, "es1-route-b"); open {
		t.Fatal("a stale writer's vector did not close the gate")
	}
	if _, err := onB.reembedMemory(ctx); err != nil {
		t.Fatalf("second reembedMemory: %v", err)
	}
	onB.memoryGate.checked = time.Time{}
	if open, _ := onB.memoryDenseOpen(ctx, "es1-route-b"); !open {
		t.Fatal("the second run did not re-embed the stale writer's fact")
	}
}
