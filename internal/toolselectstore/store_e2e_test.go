//go:build reasoning_live

// E2E validation of the tool-selection self-improvement substrate against the REAL
// stack: opens a live mcp-neo4j-cypher client, writes oracle-confirmed
// :ToolSelectionExample nodes to Neo4j, and proves the (query-embedding -> tool)
// example survives BOTH mandatory Cypher list-param workarounds (the apoc.convert.toJson
// read + the UNWIND $rows write) — i.e. the embedding is recovered byte-for-byte on a
// Save -> Load round-trip. Owns the :ToolSelectionExample label in dev (slate-clean
// before + after).
//
// W-3 — TAG REUSE NOTE: this round-trip is gated by the SAME `reasoning_live` build
// tag the reasoning classifier's live corpus uses (reasoningstore/store_e2e_test.go,
// reasoning_classifier_live_test.go). We deliberately did NOT add a new CI tier
// (minimal-shape). An operator who disables `reasoning_live` therefore drops the live
// coverage of BOTH the reasoning classifier AND this :ToolSelectionExample store — so
// re-enable it (with Neo4j + granite up) when validating either.
//
//	# from WSL with .env loaded (mcp-neo4j-cypher on PATH):
//	go test -tags reasoning_live -run TestToolSelectStoreE2E ./internal/toolselectstore/
package toolselectstore

import (
	"context"
	"math"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/knowledge"
)

// envOrSkip returns the env var, or skips locally / t.Fatals under $CI when it is
// unset. No-skip-as-green: a required var missing in CI is a HARD failure (the tier
// must actually exercise Neo4j, never silently pass).
func envOrSkip(t *testing.T, key, def string) string {
	t.Helper()
	if v := os.Getenv(key); v != "" {
		return v
	}
	if def != "" {
		return def
	}
	if os.Getenv("CI") != "" {
		t.Fatalf("%s unset under $CI: the reasoning_live tier must run against a real Neo4j, never skip-as-green", key)
	}
	t.Skipf("%s unset; skipping live :ToolSelectionExample round-trip", key)
	return ""
}

func TestToolSelectStoreE2E(t *testing.T) {
	embedBase := envOrSkip(t, "AURA_EMBED_BASE_URL", "http://127.0.0.1:8081")
	if resp, err := http.Get(embedBase + "/health"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("granite sidecar unreachable under $CI: %v", err)
		}
		t.Skipf("granite sidecar unreachable: %v", err)
	} else {
		_ = resp.Body.Close()
	}

	ctx := context.Background()
	pw := envOrSkip(t, "NEO4J_PASSWORD", "")
	cfg := &knowledge.Config{
		BoltURL:           envOrSkip(t, "AURA_NEO4J_BOLT_URL", "bolt://127.0.0.1:7687"),
		User:              envOrSkip(t, "NEO4J_USER", "neo4j"),
		Password:          pw,
		Database:          "neo4j",
		MCPBinary:         envOrSkip(t, "AURA_MCP_NEO4J_CYPHER_BIN", "mcp-neo4j-cypher"),
		ConnectTimeoutSec: 15,
	}
	client, err := knowledge.Open(ctx, cfg)
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("cannot open mcp-neo4j-cypher under $CI: %v", err)
		}
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
		if _, err := client.Write(ctx, "MATCH (e:ToolSelectionExample) DETACH DELETE e", nil); err != nil {
			t.Logf("cleanup warn: %v", err)
		}
	}
	clean()       // slate-clean (this test owns the label in dev)
	defer clean() // and remove the nodes it created

	// One confirmed (query -> tool) pair: the documented bitcoin mis-route, oracle-
	// labeled to web_search. The point of the round-trip is that the embedding survives
	// BOTH Cypher workarounds, so we assert the recovered vector equals the saved one.
	query := "quanto costa il bitcoin adesso?"
	tool := "web_search"
	vecs, err := embedder.Embed(ctx, []string{query})
	if err != nil || len(vecs) != 1 {
		t.Fatalf("embed query: %v", err)
	}
	want := vecs[0]
	if err := store.Save(ctx, query, tool, want); err != nil {
		t.Fatalf("save example: %v", err)
	}

	loaded, err := store.LoadExamples(ctx)
	if err != nil {
		t.Fatalf("load examples: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d examples, want exactly 1 (Save->Load round-trip)", len(loaded))
	}
	got := loaded[0]
	if got.Tool != tool {
		t.Errorf("tool = %q, want %q", got.Tool, tool)
	}
	if got.Query != query {
		t.Errorf("query = %q, want %q", got.Query, query)
	}
	// The load-bearing assertion (RESEARCH risk #4): the embedding survives the
	// apoc.convert.toJson read + the UNWIND $rows write. A dropped/NULLed list column
	// would yield a zero-length or zeroed vector here.
	if len(got.Vec) != len(want) {
		t.Fatalf("recovered vector len %d, want %d (a Cypher list-param workaround was missed)", len(got.Vec), len(want))
	}
	for i := range want {
		if math.Abs(got.Vec[i]-want[i]) > 1e-9 {
			t.Fatalf("recovered vector diverged at dim %d: got %g want %g (embedding did not survive the round-trip)", i, got.Vec[i], want[i])
		}
	}
	t.Logf("Neo4j round-trip OK: 1 :ToolSelectionExample node, %dd vector recovered exactly", len(got.Vec))
}
