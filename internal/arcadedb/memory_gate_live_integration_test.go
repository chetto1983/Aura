//go:build arcadedb_integration

// The gate against a live ArcadeDB: a memory written on one route is served densely to that
// route and lexically to any other, with the reason named.
//
// Run: arcade-it.sh MemoryGateLive
package arcadedb

import (
	"context"
	"testing"
	"time"
)

func TestMemoryGateLiveFollowsTheStamps(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	now := time.Now().UTC()
	routeA := constantEmbedder{value: 1, space: "es1-route-a"}
	fact := mergeFact("GateSubject", "keeps", "GateObject", "GateSubject keeps the gate object.")
	// One token: a multi-word query must clear a lexical score floor (LexicalMinScore) that
	// BM25 over a one-fact corpus never reaches, and this test is about the gate.
	const gateQuery = "GateSubject"
	if _, err := client.WithEmbedder(routeA).UpsertFact(ctx, fact, now); err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	onA, err := client.WithEmbedder(routeA).SearchFactsHybrid(ctx, gateQuery, 5, now)
	if err != nil {
		t.Fatalf("SearchFactsHybrid on route A: %v", err)
	}
	if onA.RetrievalPath != retrievalPathHybrid {
		t.Fatalf("route A over its own memory = %+v, want hybrid", onA)
	}
	onB, err := client.WithEmbedder(constantEmbedder{value: 2, space: "es1-route-b"}).
		SearchFactsHybrid(ctx, gateQuery, 5, now)
	if err != nil {
		t.Fatalf("SearchFactsHybrid on route B: %v", err)
	}
	if onB.RetrievalPath != retrievalPathLexical || onB.Reason != reasonEmbeddingSpaceMismatch {
		t.Fatalf("route B over route A's memory = %+v, want lexical with the mismatch named", onB)
	}
	if len(onB.Facts) == 0 {
		t.Fatal("the lexical fallback lost the fact")
	}
}
