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
)

// TestSearchDocumentScopedSparsePreFilterLive proves the scoped SPARSE pre-filter (spike
// 075, the fulltext-fallback twin of the vector pre-filter) on the live Neo4j: a generic
// query scoped to a small seeded document returns that document's own chunk by term
// overlap, where the old global db.index.fulltext.queryNodes(k)-THEN-filter would crowd it
// out against the operator's larger corpus. No embeddings are needed — the sparse path is
// the pre-embedding fallback. NO-SKIP-AS-GREEN: under $CI an unset NEO4J_PASSWORD is a
// misconfigured job (t.Fatal); locally it skips. Cleans up its writes.
func TestSearchDocumentScopedSparsePreFilterLive(t *testing.T) {
	cfg := config.LoadDB()
	if strings.TrimSpace(cfg.Neo4j.Password) == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("neo4j_integration requires NEO4J_PASSWORD under CI — a skipped tier must never pass as green")
		}
		t.Skip("set NEO4J_PASSWORD (+ stack up) to run the scoped sparse pre-filter live test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	mcp, err := knowledge.Open(ctx, &cfg.Neo4j)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mcp.Close() }()

	const targetDoc = "spf-live-target"
	const decoyDoc = "spf-live-decoy"
	cleanup := func() {
		for _, d := range []string{targetDoc, decoyDoc} {
			_, _ = mcp.Write(ctx, "MATCH (c:Chunk {document_id:$id}) DETACH DELETE c", map[string]any{"id": d})
		}
	}
	cleanup()
	defer cleanup()

	seed := func(doc, id, text string) {
		if _, werr := mcp.Write(ctx,
			"CREATE (c:Chunk {id:$id, document_id:$doc, text:$text, file_name:'live.md', locator_json:'', heading_path:[]})",
			map[string]any{"id": id, "doc": doc, "text": text}); werr != nil {
			t.Fatalf("seed %s: %v", id, werr)
		}
	}
	seed(targetDoc, "spf-t0", "Servo Drive G220. Protection class: IP54. Rated torque 47 Nm.")
	seed(decoyDoc, "spf-d0", "Barista coffee machine. Water tank 1.8 litres. Descale every two months.")

	searcher := &Searcher{Client: mcp}
	hits, err := searcher.Search(ctx, SearchRequest{Query: "protection class rating", DocumentID: targetDoc, Limit: 5})
	if err != nil {
		t.Fatalf("scoped sparse search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("scoped sparse search returned 0 hits — the document_id pre-filter did not surface the doc's chunk")
	}
	if hits[0].DocumentID != targetDoc {
		t.Fatalf("top hit document_id = %q, want %q", hits[0].DocumentID, targetDoc)
	}
	if low := strings.ToLower(hits[0].Text); !strings.Contains(low, "ip54") && !strings.Contains(low, "protection") {
		t.Fatalf("top hit text = %q, want the seeded G220 chunk", hits[0].Text)
	}
	t.Logf("scoped sparse pre-filter returned %d hits; top doc=%s text=%q", len(hits), hits[0].DocumentID, hits[0].Text)
}
