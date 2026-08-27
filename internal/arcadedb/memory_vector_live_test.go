//go:build arcadedb_integration

// The dense leg, against a live ArcadeDB and a live embedder.
//
// The property under test is the one the lexical-only design could not have at
// any threshold: a question asked in Italian reaching a fact written in English.
// Measured before this landed — `analyzer recall Italian English` returned the
// right fact first, `ricerca testuale italiano inglese` returned nothing.
//
//	go test -tags arcadedb_integration -run MemoryVector -v ./internal/arcadedb/
package arcadedb

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func liveEmbedder(t *testing.T) Embedder {
	t.Helper()
	embedURL := strings.TrimSpace(os.Getenv("AURA_EMBED_BASE_URL"))
	if embedURL == "" {
		embedURL = "http://127.0.0.1:8081"
	}
	embedder := NewSidecarEmbedder(embedURL, os.Getenv("AURA_EMBED_MODEL"), "", 60*time.Second)
	if embedder == nil {
		liveGap(t, "no embedder configured")
	}
	if _, err := embedder.Embed(context.Background(), []string{"probe"}); err != nil {
		liveGap(t, "embedding sidecar unreachable: %v", err)
	}
	return embedder
}

func vectorClient(t *testing.T) *Client {
	t.Helper()
	password := strings.TrimSpace(os.Getenv("ARCADEDB_PASSWORD"))
	if password == "" {
		liveGap(t, "ARCADEDB_PASSWORD not set")
	}
	base := strings.TrimSpace(os.Getenv("ARCADEDB_URL"))
	if base == "" {
		base = "http://127.0.0.1:2480"
	}
	database := strings.TrimSpace(os.Getenv("ARCADEDB_DATABASE"))
	if database == "" {
		database = "aura_memory"
	}
	client, err := New(Config{BaseURL: base, Database: database, User: "root", Password: password})
	if err != nil {
		liveGap(t, "arcadedb: %v", err)
	}
	return client.WithEmbedder(liveEmbedder(t))
}

func TestMemoryVectorSidecarReturnsTheIndexWidth(t *testing.T) {
	client := vectorClient(t)
	vectors, err := client.embedder.Embed(context.Background(), []string{"cliente a Torino"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vectors) != 1 || len(vectors[0]) != vectorDimensions {
		t.Fatalf("embedder returned %d dimensions, the index is declared at %d — "+
			"a mismatch is rejected by vector.neighbors at query time, not here",
			len(vectors[0]), vectorDimensions)
	}
}

// The whole point, end to end: a self-seeded English fact in a disposable
// database, then a semantically equivalent Italian question with no lexical
// shortcut. Mutable operator memory is never evaluation input.
func TestMemoryVectorAnswersACrossLingualQuestion(t *testing.T) {
	client := disposableMemoryClient(t).WithEmbedder(liveEmbedder(t))
	ctx := context.Background()
	now := time.Now().UTC()
	target := "The oriole project stores its quarterly invoices in the cobalt cabinet."
	fixtures := []Fact{
		{
			Subject: "Oriole project", Predicate: "stores_quarterly_invoices", Object: "Cobalt cabinet",
			Statement: target, Source: FactSource{RunID: "cross-lingual-eval", MemoryIDs: []string{"target"}},
		},
		{
			Subject: "Kestrel project", Predicate: "stores_annual_reports", Object: "Amber archive",
			Statement: "The kestrel project stores its annual reports in the amber archive.",
			Source:    FactSource{RunID: "cross-lingual-eval", MemoryIDs: []string{"distractor-1"}},
		},
		{
			Subject: "Heron project", Predicate: "keeps_design_samples", Object: "Northern laboratory",
			Statement: "The heron project keeps its design samples in the northern laboratory.",
			Source:    FactSource{RunID: "cross-lingual-eval", MemoryIDs: []string{"distractor-2"}},
		},
	}
	for i, fact := range fixtures {
		write(t, client, fact, now.Add(time.Duration(i)*time.Second))
	}

	query := "Dove conserva le fatture trimestrali il progetto rigogolo?"
	lexical, err := client.SearchFacts(ctx, query, 3, time.Time{})
	if err != nil {
		t.Fatalf("lexical-only comparison: %v", err)
	}
	for _, hit := range lexical {
		if hit.Statement == target {
			t.Fatalf("lexical retrieval found the target, so the fixture no longer proves the dense leg: %+v", lexical)
		}
	}

	hybrid, err := client.SearchFactsHybrid(ctx, query, 3, time.Time{})
	if err != nil {
		t.Fatalf("hybrid cross-lingual search: %v", err)
	}
	if hybrid.RetrievalPath != retrievalPathHybrid {
		t.Fatalf("retrieval path = %q, want %q (reason=%q)", hybrid.RetrievalPath, retrievalPathHybrid, hybrid.Reason)
	}
	if len(hybrid.Facts) == 0 || hybrid.Facts[0].Statement != target {
		t.Fatalf("cross-lingual target was not rank 1: %+v", hybrid.Facts)
	}
}

// With no embedder the hybrid search must be exactly the lexical one: the dense
// leg is an improvement, never a dependency.
func TestMemoryVectorDegradesWithoutAnEmbedder(t *testing.T) {
	client := vectorClient(t)
	ctx := context.Background()
	bare := client.WithEmbedder(nil)

	hybrid, err := bare.SearchFactsHybrid(ctx, "analyzer recall", 3, time.Time{})
	if err != nil {
		t.Fatalf("hybrid with no embedder: %v", err)
	}
	lexical, err := bare.SearchFacts(ctx, "analyzer recall", 3, time.Time{})
	if err != nil {
		t.Fatalf("lexical: %v", err)
	}
	if len(hybrid.Facts) != len(lexical) {
		t.Fatalf("with no embedder the hybrid returned %d and the lexical %d; they must agree",
			len(hybrid.Facts), len(lexical))
	}
}
