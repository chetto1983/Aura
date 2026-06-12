//go:build reasoning_live

// E2E validation of the self-improvement substrate against the REAL stack:
// opens a live mcp-neo4j-cypher client, writes oracle-labeled :ReasoningExample
// nodes to Neo4j, and proves the classifier-with-store fixes a stable-fact query
// the seed-only baseline mis-routes. Owns the :ReasoningExample label in dev
// (slate-clean before + after). Skips if the granite sidecar, the cypher binary,
// or Neo4j are unavailable.
//
//	# from WSL with .env loaded (mcp-neo4j-cypher on PATH):
//	go test -tags reasoning_live -run TestReasoningStoreE2E ./internal/reasoningstore/
package reasoningstore

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/knowledge"
)

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func TestReasoningStoreE2E(t *testing.T) {
	embedBase := envOr("AURA_EMBED_BASE_URL", "http://127.0.0.1:8081")
	if resp, err := http.Get(embedBase + "/health"); err != nil {
		t.Skipf("granite sidecar unreachable: %v", err)
	} else {
		_ = resp.Body.Close()
	}

	ctx := context.Background()
	cfg := &knowledge.Config{
		BoltURL:           envOr("AURA_NEO4J_BOLT_URL", "bolt://127.0.0.1:7687"),
		User:              envOr("NEO4J_USER", "neo4j"),
		Password:          os.Getenv("NEO4J_PASSWORD"),
		Database:          "neo4j",
		MCPBinary:         envOr("AURA_MCP_NEO4J_CYPHER_BIN", "mcp-neo4j-cypher"),
		ConnectTimeoutSec: 15,
	}
	client, err := knowledge.Open(ctx, cfg)
	if err != nil {
		t.Skipf("cannot open mcp-neo4j-cypher (binary on PATH? Neo4j up? NEO4J_PASSWORD set?): %v", err)
	}
	defer func() { _ = client.Close() }()

	embedder := &documents.EmbeddingClient{
		BaseURL:    embedBase,
		Client:     &http.Client{Timeout: 30 * time.Second},
		Dimensions: knowledge.DefaultEmbedDimensions,
	}
	defer embedder.Client.CloseIdleConnections()

	store := &Store{Client: client}
	clean := func() {
		if _, err := client.Write(ctx, "MATCH (e:ReasoningExample) DETACH DELETE e", nil); err != nil {
			t.Logf("cleanup warn: %v", err)
		}
	}
	clean()       // slate-clean (this test owns the label in dev)
	defer clean() // and remove the nodes it created

	// The known weak case: a stable-fact question the seed-only baseline routes to
	// low (live test 1 confirmed "capitale dell'Italia" → low).
	query := "qual e la capitale dell'Italia?"
	base := prompt.NewReasoningClassifier(embedder, nil)
	basePred, ok := base.Classify(ctx, query)
	if !ok {
		t.Fatal("baseline classify not usable")
	}
	t.Logf("baseline: %q → %s", query, basePred)

	// Oracle-label a handful of stable-fact questions as `none` and persist them.
	examples := map[string]prompt.ReasoningTier{
		"qual e la capitale della Germania?":               prompt.ReasoningTierNone,
		"qual e la capitale del Portogallo?":               prompt.ReasoningTierNone,
		"chi ha scoperto l'America?":                       prompt.ReasoningTierNone,
		"quanti pianeti ci sono nel sistema solare?":       prompt.ReasoningTierNone,
		"in che anno e finita la seconda guerra mondiale?": prompt.ReasoningTierNone,
	}
	for text, tier := range examples {
		vecs, err := embedder.Embed(ctx, []string{text})
		if err != nil || len(vecs) != 1 {
			t.Fatalf("embed example %q: %v", text, err)
		}
		if err := store.Save(ctx, text, vecs[0], tier); err != nil {
			t.Fatalf("save example %q: %v", text, err)
		}
	}

	// Round-trip: the examples are really in Neo4j (write + APOC-JSON read).
	loaded, err := store.LoadExamples(ctx)
	if err != nil {
		t.Fatalf("load examples: %v", err)
	}
	if len(loaded) < len(examples) {
		t.Fatalf("loaded %d examples, want >= %d (Neo4j write/read round-trip)", len(loaded), len(examples))
	}
	t.Logf("Neo4j round-trip: %d :ReasoningExample nodes loaded with %dd vectors", len(loaded), len(loaded[0].Vec))

	// The classifier WITH the store should now route the stable-fact query to none.
	learned := prompt.NewReasoningClassifier(embedder, store)
	learnedPred, ok := learned.Classify(ctx, query)
	if !ok {
		t.Fatal("learned classify not usable")
	}
	t.Logf("learned:  %q → %s", query, learnedPred)

	if learnedPred != prompt.ReasoningTierNone {
		t.Errorf("after folding oracle-labeled stable facts from Neo4j, %q should route to none; got %s (baseline was %s)", query, learnedPred, basePred)
	}
}
