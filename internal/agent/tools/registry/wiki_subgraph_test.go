package tools

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/aura/aura/internal/storage/search"
	"github.com/aura/aura/internal/wiki"
)

// ── fake searcher ─────────────────────────────────────────────────────────────

type fakeSubgraphSearcher struct {
	results []search.Result
}

func (f *fakeSubgraphSearcher) Search(_ context.Context, _ string, limit int) ([]search.Result, error) {
	if limit < len(f.results) {
		return f.results[:limit], nil
	}
	return f.results, nil
}

func (f *fakeSubgraphSearcher) IsIndexed() bool { return true }

// SearchHybrid satisfies search.HybridSearcher so the tool uses the fast path.
func (f *fakeSubgraphSearcher) SearchHybrid(_ context.Context, _ string, limit int) ([]search.Result, error) {
	return f.Search(context.Background(), "", limit)
}

// emptySubgraphSearcher returns no hits.
type emptySubgraphSearcher struct{}

func (e *emptySubgraphSearcher) Search(_ context.Context, _ string, _ int) ([]search.Result, error) {
	return nil, nil
}
func (e *emptySubgraphSearcher) IsIndexed() bool { return true }

// ── helper: minimal wiki.Store with a few pages ───────────────────────────────

func newSubgraphTestStore(t *testing.T, pages map[string]*wiki.Page) *wiki.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := wiki.NewStore(dir, slog.Default())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	for slug, page := range pages {
		page.SchemaVersion = wiki.CurrentSchemaVersion
		page.CreatedAt = "2026-01-01T00:00:00Z"
		page.UpdatedAt = "2026-01-01T00:00:00Z"
		if page.PromptVersion == "" {
			page.PromptVersion = "v1"
		}
		if err := store.WritePage(context.Background(), page); err != nil {
			t.Fatalf("WritePage(%s): %v", slug, err)
		}
	}
	// Rebuild the graph index from the written pages.
	store.RebuildGraph(context.Background())
	return store
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestWikiSubgraph_HappyPath(t *testing.T) {
	pages := map[string]*wiki.Page{
		"customer-delta": {Title: "Customer Delta", Body: "Customer Delta has code 615827. [[supplier-acme]]"},
		"supplier-acme":  {Title: "Supplier Acme", Body: "Supplier Acme delivers to [[customer-delta]]."},
		"unrelated-node": {Title: "Unrelated Node", Body: "Completely unrelated content."},
	}
	store := newSubgraphTestStore(t, pages)

	searcher := &fakeSubgraphSearcher{
		results: []search.Result{
			{Slug: "customer-delta", Title: "Customer Delta", Score: 0.95},
		},
	}

	tool := NewWikiSubgraphTool(store, searcher)
	if tool == nil {
		t.Fatal("NewWikiSubgraphTool returned nil")
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"query":         "delta automazioni codice cliente",
		"depth":         float64(1),
		"budget_tokens": float64(500),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == "wiki_subgraph: no wiki pages found for query" {
		t.Fatal("got no-results response; want subgraph content")
	}
	if !strings.Contains(result, "NODE") {
		t.Errorf("result missing NODE lines:\n%s", result)
	}
	if !strings.Contains(result, "customer-delta") {
		t.Errorf("result missing seeded page 'customer-delta':\n%s", result)
	}
	if !strings.Contains(result, "SNIPPET Customer Delta has code 615827.") {
		t.Errorf("result missing page-body snippet needed for one-call QA:\n%s", result)
	}
}

func TestWikiSubgraph_QueryCapsuleHeaderShowsSeedsAndSignals(t *testing.T) {
	pages := map[string]*wiki.Page{
		"customer-delta": {Title: "Customer Delta", Body: "Customer Delta has code 615827. [[supplier-acme]]"},
		"supplier-acme":  {Title: "Supplier Acme", Body: "Supplier Acme delivers to [[customer-delta]]."},
	}
	store := newSubgraphTestStore(t, pages)

	searcher := &fakeSubgraphSearcher{
		results: []search.Result{
			{
				Slug:        "customer-delta",
				Title:       "Customer Delta",
				Score:       0.95,
				ScoreExact:  0.80,
				ScoreFTS:    0.70,
				ScoreVector: 0.20,
			},
			{Slug: "supplier-acme", Title: "Supplier Acme", Score: 0.10},
		},
	}
	tool := NewWikiSubgraphTool(store, searcher)

	result, err := tool.Execute(context.Background(), map[string]any{
		"query":         "delta automazioni codice cliente",
		"depth":         float64(1),
		"budget_tokens": float64(500),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	mustContain := []string{
		"CAPSULE wiki_subgraph",
		"QUERY delta automazioni codice cliente",
		"TRAVERSAL bfs depth=1 budget_tokens=500",
		"SEARCH_SEED customer-delta score=0.950 exact=0.800 fts=0.700 vector=0.200 title=\"Customer Delta\"",
		"SEED_CONTENT",
		"SEED_SNIPPET customer-delta Customer Delta has code 615827.",
		"PPR_SEED customer-delta",
		"CONTENT",
		"NODE customer-delta",
	}
	for _, want := range mustContain {
		if !strings.Contains(result, want) {
			t.Errorf("result missing %q:\n%s", want, result)
		}
	}
}

func TestWikiSubgraph_RendersRelatedConfidence(t *testing.T) {
	pages := map[string]*wiki.Page{
		"alpha": {
			Title:   "Alpha",
			Body:    "Alpha page.",
			Related: []wiki.RelatedRef{{Slug: "beta", Confidence: wiki.ConfidenceAmbiguous}},
		},
		"beta": {Title: "Beta", Body: "Beta page."},
	}
	store := newSubgraphTestStore(t, pages)

	tool := NewWikiSubgraphTool(store, &fakeSubgraphSearcher{
		results: []search.Result{{Slug: "alpha", Title: "Alpha", Score: 0.95}},
	})
	result, err := tool.Execute(context.Background(), map[string]any{
		"query":         "alpha",
		"depth":         1,
		"budget_tokens": 500,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "EDGE alpha --AMBIGUOUS--> beta") {
		t.Fatalf("result missing related confidence edge:\n%s", result)
	}
}

func TestWikiSubgraph_NoHits(t *testing.T) {
	store := newSubgraphTestStore(t, map[string]*wiki.Page{
		"a": {Title: "A", Body: "No relevant content."},
	})

	tool := NewWikiSubgraphTool(store, &emptySubgraphSearcher{})
	result, err := tool.Execute(context.Background(), map[string]any{"query": "xyz"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "wiki_subgraph: no wiki pages found for query" {
		t.Errorf("want no-results message, got: %s", result)
	}
}

func TestWikiSubgraph_BudgetTruncation(t *testing.T) {
	// Single page with enough content to exceed a tiny budget.
	body := strings.Repeat("Lorem ipsum delta automazioni test content. ", 40)
	pages := map[string]*wiki.Page{
		"big-page": {Title: "Big Page", Body: body},
	}
	store := newSubgraphTestStore(t, pages)

	searcher := &fakeSubgraphSearcher{
		results: []search.Result{
			{Slug: "big-page", Score: 0.9},
		},
	}
	tool := NewWikiSubgraphTool(store, searcher)

	// budget_tokens=1 → only 3 chars; should truncate with marker.
	result, err := tool.Execute(context.Background(), map[string]any{
		"query":         "delta",
		"budget_tokens": float64(1),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "CAPSULE wiki_subgraph") {
		t.Fatalf("tiny budget should still return query capsule header, got:\n%s", result)
	}
	if !strings.Contains(result, "truncated") {
		t.Fatalf("tiny budget should return an actionable truncation marker, got:\n%s", result)
	}
	if !strings.Contains(result, "increase budget_tokens") || !strings.Contains(result, "narrower query") {
		t.Fatalf("truncation marker should tell the model how to recover, got:\n%s", result)
	}
}

func TestWikiSubgraph_NilDeps(t *testing.T) {
	if NewWikiSubgraphTool(nil, nil) != nil {
		t.Error("NewWikiSubgraphTool(nil, nil) should return nil")
	}
	store := newSubgraphTestStore(t, nil)
	if NewWikiSubgraphTool(store, nil) != nil {
		t.Error("NewWikiSubgraphTool(store, nil) should return nil")
	}
}
