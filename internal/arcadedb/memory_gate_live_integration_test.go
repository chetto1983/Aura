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

// Every dense leg filters by the reader's space (final review #6): route A's own memory is
// still served densely by recall and by reasoning search, so the engine accepts each
// filtered statement. A rejected statement would fall back to lexical with fusion_failed.
func TestMemoryGateLiveDenseLegsFilterByTheReadersSpace(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	routeA := constantEmbedder{value: 1, space: "es1-route-a"}
	fact := mergeFact("FilterSubject", "keeps", "FilterObject", "FilterSubject keeps the filter object.")
	if _, err := client.WithEmbedder(routeA).UpsertFact(ctx, fact, time.Now().UTC()); err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	trace := validReasoningTrace()
	if err := client.WithEmbedder(routeA).UpsertReasoningTrace(ctx, trace); err != nil {
		t.Fatalf("UpsertReasoningTrace: %v", err)
	}
	recall, err := client.WithEmbedder(routeA).RecallMemory(ctx, RecallRequest{
		IdentityID: trace.IdentityID, Mode: RecallModeSemantic, Query: "FilterSubject", Limit: 5,
	})
	if err != nil {
		t.Fatalf("RecallMemory: %v", err)
	}
	if recall.Retrieval.Path != retrievalPathHybrid || recall.Reason != "" {
		t.Fatalf("recall = path %q reason %q, want hybrid", recall.Retrieval.Path, recall.Reason)
	}
	reasoning, err := client.WithEmbedder(routeA).SearchReasoningTraces(ctx, trace.IdentityID, "deployment", 5)
	if err != nil {
		t.Fatalf("SearchReasoningTraces: %v", err)
	}
	if reasoning.RetrievalPath != retrievalPathHybrid || len(reasoning.Traces) == 0 {
		t.Fatalf("reasoning = %+v, want the trace found on the hybrid path", reasoning)
	}
}
