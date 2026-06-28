//go:build neo4j_integration

package documents

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/chetto1983/aura/internal/rerank"
)

// TestRetrieveDocumentScopedPreFilterLive proves the scoped vector pre-filter (spike 075)
// on the live Neo4j: a GENERIC query scoped to a small seeded document returns that
// document's chunk, where the old global-top-k-THEN-filter crowded it out against the
// operator's larger corpus. It seeds a target chunk + a decoy chunk (different document_id)
// with real granite embeddings, runs the public Service.Retrieve scoped to the target, and
// asserts the target chunk comes back. NO-SKIP-AS-GREEN: under $CI an unset NEO4J_PASSWORD
// is a misconfigured job (t.Fatal); locally it skips. Cleans up its writes.
func TestRetrieveDocumentScopedPreFilterLive(t *testing.T) {
	cfg := config.LoadDB()
	if strings.TrimSpace(cfg.Neo4j.Password) == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("neo4j_integration requires NEO4J_PASSWORD under CI — a skipped tier must never pass as green")
		}
		t.Skip("set NEO4J_PASSWORD (+ stack up) to run the scoped pre-filter live test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	mcp, err := knowledge.Open(ctx, &cfg.Neo4j)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mcp.Close() }()
	embed := &EmbeddingClient{BaseURL: cfg.Neo4j.EmbedURL, Dimensions: cfg.Neo4j.EmbedDimensions}

	const targetDoc = "retpf-live-target"
	const decoyDoc = "retpf-live-decoy"
	cleanup := func() {
		for _, d := range []string{targetDoc, decoyDoc} {
			_, _ = mcp.Write(ctx, "MATCH (c:Chunk {document_id:$id}) DETACH DELETE c", map[string]any{"id": d})
		}
	}
	cleanup()
	defer cleanup()

	seed := func(doc, id, text string) {
		vecs, eerr := embed.Embed(ctx, []string{text})
		if eerr != nil || len(vecs) != 1 {
			t.Fatalf("embed %s: %v", id, eerr)
		}
		if _, werr := mcp.Write(ctx,
			"CREATE (c:Chunk {id:$id, document_id:$doc, text:$text, file_name:'live.md', locator_json:'', heading_path:[], embedding:$emb})",
			map[string]any{"id": id, "doc": doc, "text": text, "emb": vecs[0]}); werr != nil {
			t.Fatalf("seed %s: %v", id, werr)
		}
	}
	seed(targetDoc, "retpf-t0", "Servo Drive G220. Rated torque: 47 Nm. Protection class: IP54.")
	seed(decoyDoc, "retpf-d0", "Barista coffee machine. Water tank 1.8 litres. Descale every two months.")

	svc := &Service{
		Searcher:      &Searcher{Client: mcp},
		Knowledge:     mcp,
		QueryEmbedder: embed,
		Reranker:      &rerank.RerankClient{BaseURL: cfg.RerankBaseURL},
	}
	// A generic query scoped to the target document — the spike-075 crowding case.
	hits, err := svc.Retrieve(ctx, SearchRequest{Query: "protection class rating", DocumentID: targetDoc, Limit: 5})
	if err != nil {
		t.Fatalf("scoped retrieve: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("scoped retrieve returned 0 hits — the document_id pre-filter did not surface the doc's chunk")
	}
	if hits[0].DocumentID != targetDoc {
		t.Fatalf("top hit document_id = %q, want %q", hits[0].DocumentID, targetDoc)
	}
	low := strings.ToLower(hits[0].Text)
	if !strings.Contains(low, "ip54") && !strings.Contains(low, "torque") {
		t.Fatalf("top hit text = %q, want the seeded G220 chunk", hits[0].Text)
	}
	t.Logf("scoped pre-filter returned %d hits; top doc=%s text=%q", len(hits), hits[0].DocumentID, hits[0].Text)
}
