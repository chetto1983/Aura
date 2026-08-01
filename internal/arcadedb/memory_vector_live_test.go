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

func vectorClient(t *testing.T) *Client {
	t.Helper()
	password := strings.TrimSpace(os.Getenv("ARCADEDB_PASSWORD"))
	if password == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("ARCADEDB_PASSWORD must be set in CI: a skipped integration tier is a falsely-green job")
		}
		t.Skip("ARCADEDB_PASSWORD not set")
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
		t.Skipf("arcadedb: %v", err)
	}
	embedURL := strings.TrimSpace(os.Getenv("AURA_EMBED_BASE_URL"))
	if embedURL == "" {
		embedURL = "http://127.0.0.1:8081"
	}
	embedder := NewSidecarEmbedder(embedURL, os.Getenv("AURA_EMBED_MODEL"), "", 60*time.Second)
	if embedder == nil {
		t.Skip("no embedder configured")
	}
	if _, err := embedder.Embed(context.Background(), []string{"probe"}); err != nil {
		t.Skipf("embedding sidecar unreachable: %v", err)
	}
	return client.WithEmbedder(embedder)
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

// The whole point, end to end: schema, backfill, then a question in the wrong
// language for the corpus.
func TestMemoryVectorAnswersACrossLingualQuestion(t *testing.T) {
	client := vectorClient(t)
	ctx := context.Background()

	if err := client.EnsureMemorySchema(ctx); err != nil {
		t.Fatalf("schema: %v", err)
	}
	embedded, err := client.EmbedMissingFacts(ctx, 200)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	t.Logf("backfilled %d fact vectors", embedded)

	total := 0
	if rows, err := client.Query(ctx, "SELECT count(*) AS n FROM "+factEdgeType, nil); err == nil && len(rows) > 0 {
		switch n := rows[0]["n"].(type) {
		case float64:
			total = int(n)
		case int:
			total = n
		}
	}
	if total == 0 {
		t.Skip("no facts stored; seed some before running this")
	}
	t.Logf("%d facts in memory", total)

	// English: the lexical leg already answered this one, and must keep doing so.
	english, err := client.SearchFactsHybrid(ctx, "analyzer recall Italian English", 3, time.Time{})
	if err != nil {
		t.Fatalf("english search: %v", err)
	}
	if len(english) == 0 {
		t.Fatal("the English question lost its answer — the fusion regressed the lexical leg")
	}
	t.Logf("EN  %.90s", english[0].Statement)

	// Italian: same question, the language the operator actually asks in. The
	// facts are in English. Lexical retrieval returned zero here.
	italian, err := client.SearchFactsHybrid(
		ctx, "perche la ricerca testuale rendeva la meta in italiano", 3, time.Time{})
	if err != nil {
		t.Fatalf("italian search: %v", err)
	}
	if len(italian) == 0 {
		t.Fatal("the Italian question found nothing: the dense leg is not contributing")
	}
	for i, hit := range italian {
		t.Logf("IT%d %.90s", i+1, hit.Statement)
	}

	lexical, err := client.SearchFacts(ctx, "perche la ricerca testuale rendeva la meta in italiano", 3, time.Time{})
	if err != nil {
		t.Logf("lexical-only comparison errored (which is itself the old behaviour): %v", err)
	}
	t.Logf("lexical-only on the same Italian question: %d hits", len(lexical))
	if len(lexical) >= len(italian) {
		t.Logf("NOTE: lexical alone matched %d — the corpus may have grown Italian text since "+
			"this gap was measured; the fusion must still not be WORSE", len(lexical))
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
	if len(hybrid) != len(lexical) {
		t.Fatalf("with no embedder the hybrid returned %d and the lexical %d; they must agree",
			len(hybrid), len(lexical))
	}
}
