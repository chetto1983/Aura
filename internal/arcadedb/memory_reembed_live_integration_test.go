//go:build arcadedb_integration

// The re-embed measurement, against a live ArcadeDB.
//
// ReEmbedAllFacts exists for one situation: the embedding model changed, so every stored
// vector is in the wrong geometry. Its selection is `WHERE statement IS NOT NULL LIMIT
// batch` -- no ordering, no progress, and nothing about the predicate shrinks as work is
// done, because re-embedding a fact does not stop it matching. It therefore returns the
// same first `batch` rows on every call, and a corpus larger than one batch keeps the old
// geometry in its tail forever. Measured through the MCP surface on 2026-09-03: two
// consecutive `memory_reembed all=true batch=30` calls against a 55-fact memory both
// reported `embedded: 30`, where a draining sweep would have reported 30 then 25.
//
// Run: go test -tags arcadedb_integration ./internal/arcadedb/ -run ReEmbed
package arcadedb

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// constantEmbedder answers every text with the same vector, so "which model wrote this"
// is readable straight off the stored value.
type constantEmbedder struct{ value float64 }

func (e constantEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	vectors := make([][]float64, len(texts))
	for index := range texts {
		vectors[index] = vectorOf(e.value)
	}
	return vectors, nil
}

func TestReEmbedAllFactsReachesTheWholeCorpusNotTheFirstBatch(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	now := time.Now().UTC()

	const facts = 7
	const batch = 3
	old := client.WithEmbedder(constantEmbedder{value: 1})
	for index := range facts {
		fact := mergeFact(
			fmt.Sprintf("ReEmbedSubject%02d", index), "knows", "ReEmbedObject",
			fmt.Sprintf("ReEmbedSubject%02d knows the object.", index))
		if _, err := old.UpsertFact(ctx, fact, now); err != nil {
			t.Fatalf("seed %d: %v", index, err)
		}
	}
	if stale := countFactsEmbeddedAs(t, client, 1); stale != facts {
		t.Fatalf("seeded %d facts in the old geometry, want %d", stale, facts)
	}

	// The model changed. ONE call must cover the whole corpus: `all` is deliberately not
	// idempotent-by-skipping, so "call it until it returns zero" is not the contract and
	// never terminates by design. batch is a round size, not a ceiling on the work.
	fresh := client.WithEmbedder(constantEmbedder{value: 2})
	embedded, err := fresh.ReEmbedAllFacts(ctx, batch)
	if err != nil {
		t.Fatalf("ReEmbedAllFacts: %v", err)
	}
	if embedded != facts {
		t.Errorf("re-embedded %d of %d facts in one call: the tail past batch=%d was never selected",
			embedded, facts, batch)
	}

	if stale := countFactsEmbeddedAs(t, client, 1); stale != 0 {
		t.Errorf("%d fact(s) kept the old model's vector: the tail of the corpus is in the wrong geometry", stale)
	}
	if fresh := countFactsEmbeddedAs(t, client, 2); fresh != facts {
		t.Errorf("%d fact(s) carry the new vector, want %d", fresh, facts)
	}
}

// countFactsEmbeddedAs counts the facts whose first vector component is value, which the
// constant embedder makes a stand-in for "written by that model".
func countFactsEmbeddedAs(t *testing.T, client *Client, value float64) int {
	t.Helper()
	rows, err := client.Query(context.Background(),
		"SELECT count(*) AS n FROM "+factEdgeType+" WHERE embedding[0] = :value",
		map[string]any{"value": value})
	if err != nil {
		t.Fatalf("count embedded facts: %v", err)
	}
	if len(rows) == 0 {
		return 0
	}
	return int(rowInt(rows[0], "n"))
}
