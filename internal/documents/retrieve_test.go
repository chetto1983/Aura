package documents

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/rerank"
)

// TestRetrieveRerankReordersSeeds: rerank-on reorders the seeds by rerank score and
// attaches the rerank relevance score to the winners.
func TestRetrieveRerankReordersSeeds(t *testing.T) {
	knowledge := &fakeRetrieveKnowledge{seedRows: []map[string]any{
		seedRow("doc-1", "chunk-0", "alpha", 0.9),
		seedRow("doc-1", "chunk-1", "bravo", 0.5),
		seedRow("doc-1", "chunk-2", "charlie", 0.1),
	}}
	reranker := &fakeReranker{scored: []rerank.Scored{
		{Index: 2, Score: 0.99},
		{Index: 0, Score: 0.80},
		{Index: 1, Score: 0.20},
	}}
	svc := &Service{
		Searcher:      &fakeSearchBackend{},
		Knowledge:     knowledge,
		QueryEmbedder: &fakeQueryEmbedder{vector: []float64{0.1, 0.2}},
		Reranker:      reranker,
	}

	hits, err := svc.Retrieve(t.Context(), SearchRequest{Query: "q", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got := chunkIDs(hits); got != "chunk-2,chunk-0,chunk-1" {
		t.Fatalf("rerank order = %s", got)
	}
	if hits[0].Score != 0.99 {
		t.Fatalf("winner score = %v, want rerank score 0.99", hits[0].Score)
	}
	if reranker.calls != 1 || len(reranker.gotDocs) != 3 {
		t.Fatalf("reranker calls=%d docs=%v", reranker.calls, reranker.gotDocs)
	}
}

// TestRetrieveRerankIdentityKeepsSeedOrder: a reranker returning identity order (the
// fail-soft rerank-off contract) keeps the seed order and the seed scores unchanged.
func TestRetrieveRerankIdentityKeepsSeedOrder(t *testing.T) {
	knowledge := &fakeRetrieveKnowledge{seedRows: []map[string]any{
		seedRow("doc-1", "chunk-0", "alpha", 0.9),
		seedRow("doc-1", "chunk-1", "bravo", 0.5),
		seedRow("doc-1", "chunk-2", "charlie", 0.1),
	}}
	// identity(docs): input order, score 0 — what RerankClient returns when degraded.
	reranker := &fakeReranker{scored: []rerank.Scored{
		{Index: 0, Score: 0}, {Index: 1, Score: 0}, {Index: 2, Score: 0},
	}}
	svc := &Service{
		Searcher:      &fakeSearchBackend{},
		Knowledge:     knowledge,
		QueryEmbedder: &fakeQueryEmbedder{vector: []float64{0.1, 0.2}},
		Reranker:      reranker,
	}

	hits, err := svc.Retrieve(t.Context(), SearchRequest{Query: "q", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got := chunkIDs(hits); got != "chunk-0,chunk-1,chunk-2" {
		t.Fatalf("identity order = %s, want seed order preserved", got)
	}
	if hits[0].Score != 0.9 {
		t.Fatalf("seed score = %v, want 0.9 preserved on identity", hits[0].Score)
	}
}

// TestRetrieveEmbedderErrorFallsBackToFulltext: an embedder failure seeds from the
// sparse fulltext Search (RRF) path with no hard fail and never runs the vector seed.
func TestRetrieveEmbedderErrorFallsBackToFulltext(t *testing.T) {
	knowledge := &fakeRetrieveKnowledge{seedRows: []map[string]any{
		seedRow("doc-1", "vector-only", "should not be used", 0.99),
	}}
	searcher := &fakeSearchBackend{hits: []SearchHit{
		{DocumentID: "doc-1", ChunkID: "ft-0", Text: "fulltext zero", Score: 3.2},
		{DocumentID: "doc-1", ChunkID: "ft-1", Text: "fulltext one", Score: 2.1},
	}}
	svc := &Service{
		Searcher:      searcher,
		Knowledge:     knowledge,
		QueryEmbedder: &fakeQueryEmbedder{err: errors.New("embed sidecar down")},
		Reranker:      &fakeReranker{scored: []rerank.Scored{{Index: 0, Score: 0}, {Index: 1, Score: 0}}},
	}

	hits, err := svc.Retrieve(t.Context(), SearchRequest{Query: "q", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got := chunkIDs(hits); !strings.HasPrefix(got, "ft-0,ft-1") {
		t.Fatalf("hits = %s, want fulltext seeds", got)
	}
	if len(searcher.requests) != 1 {
		t.Fatalf("fulltext searcher called %d times", len(searcher.requests))
	}
	if knowledge.readContains("queryNodes") {
		t.Fatal("vector seed must be skipped when the embedder fails")
	}
}

// TestRetrieveDocumentScopedUsesNativePreFilter: a request scoped to a document_id seeds
// with the cosine pre-filter over that document's own chunks (spike 075), never the global
// queryNodes top-k that crowds a small doc out against a large corpus.
func TestRetrieveDocumentScopedUsesNativePreFilter(t *testing.T) {
	knowledge := &fakeRetrieveKnowledge{seedRows: []map[string]any{
		seedRow("doc-7", "chunk-0", "rated torque 47 Nm protection class IP54", 0.9),
	}}
	svc := &Service{
		Searcher:      &fakeSearchBackend{},
		Knowledge:     knowledge,
		QueryEmbedder: &fakeQueryEmbedder{vector: []float64{0.1, 0.2}},
		Reranker:      &fakeReranker{scored: []rerank.Scored{{Index: 0, Score: 0}}},
	}

	hits, err := svc.Retrieve(t.Context(), SearchRequest{Query: "protection class rating", DocumentID: "doc-7", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got := chunkIDs(hits); got != "chunk-0" {
		t.Fatalf("scoped hits = %s, want chunk-0", got)
	}
	if !knowledge.readContains("vector.similarity.cosine") {
		t.Fatal("scoped retrieval must use the document_id cosine pre-filter")
	}
	if knowledge.readContains("queryNodes") {
		t.Fatal("scoped retrieval must NOT use the global queryNodes top-k")
	}
	for _, call := range knowledge.reads {
		if strings.Contains(call.query, "vector.similarity.cosine") && call.params["document_id"] != "doc-7" {
			t.Fatalf("pre-filter document_id = %v, want doc-7", call.params["document_id"])
		}
	}
}

func TestRetrieveVectorQueriesExcludeInactiveOrDeletedChunks(t *testing.T) {
	knowledge := &fakeRetrieveKnowledge{seedRows: []map[string]any{
		seedRow("doc-1", "chunk-0", "alpha", 0.9),
	}}
	svc := &Service{
		Searcher:      &fakeSearchBackend{},
		Knowledge:     knowledge,
		QueryEmbedder: &fakeQueryEmbedder{vector: []float64{0.1}},
		Reranker:      &fakeReranker{scored: []rerank.Scored{{Index: 0, Score: 0}}},
	}
	if _, err := svc.Retrieve(t.Context(), SearchRequest{Query: "q"}); err != nil {
		t.Fatal(err)
	}
	for _, call := range knowledge.reads {
		if strings.Contains(call.query, "queryNodes") {
			for _, want := range []string{"coalesce(node.active, true) = true", "node.deleted_at IS NULL"} {
				if !strings.Contains(call.query, want) {
					t.Fatalf("vector seed query missing %q:\n%s", want, call.query)
				}
			}
		}
	}
}

func TestRetrieveScopedVectorQueryExcludesInactiveOrDeletedChunks(t *testing.T) {
	knowledge := &fakeRetrieveKnowledge{seedRows: []map[string]any{
		seedRow("doc-1", "chunk-0", "alpha", 0.9),
	}}
	svc := &Service{
		Searcher:      &fakeSearchBackend{},
		Knowledge:     knowledge,
		QueryEmbedder: &fakeQueryEmbedder{vector: []float64{0.1}},
		Reranker:      &fakeReranker{scored: []rerank.Scored{{Index: 0, Score: 0}}},
	}
	if _, err := svc.Retrieve(t.Context(), SearchRequest{Query: "q", DocumentID: "doc-1"}); err != nil {
		t.Fatal(err)
	}
	for _, call := range knowledge.reads {
		if strings.Contains(call.query, "vector.similarity.cosine") {
			for _, want := range []string{"coalesce(node.active, true) = true", "node.deleted_at IS NULL"} {
				if !strings.Contains(call.query, want) {
					t.Fatalf("scoped vector seed query missing %q:\n%s", want, call.query)
				}
			}
		}
	}
}

func TestRetrieveExpansionExcludesInactiveOrDeletedNeighbors(t *testing.T) {
	knowledge := &fakeRetrieveKnowledge{
		seedRows: []map[string]any{
			seedRow("doc-1", "chunk-0", "alpha", 0.9),
			seedRow("doc-1", "chunk-1", "bravo", 0.8),
		},
		expandRows: []map[string]any{seedRow("doc-1", "chunk-2", "context", 0)},
	}
	svc := &Service{
		Searcher:      &fakeSearchBackend{},
		Knowledge:     knowledge,
		QueryEmbedder: &fakeQueryEmbedder{vector: []float64{0.1}},
		Reranker:      &fakeReranker{scored: []rerank.Scored{{Index: 0, Score: 0.9}, {Index: 1, Score: 0.8}}},
	}
	if _, err := svc.Retrieve(t.Context(), SearchRequest{Query: "q", Limit: 1}); err != nil {
		t.Fatal(err)
	}
	for _, call := range knowledge.reads {
		if strings.Contains(call.query, "NEXT_CHUNK") {
			for _, want := range []string{"coalesce(n.active, true) = true", "n.deleted_at IS NULL"} {
				if !strings.Contains(call.query, want) {
					t.Fatalf("neighbor query missing %q:\n%s", want, call.query)
				}
			}
		}
	}
}

// TestRetrieveExpandsOnlyWinners: after rerank, only the top-K winners (not the whole
// seed pool) are passed to the 1-hop graph expansion.
func TestRetrieveExpandsOnlyWinners(t *testing.T) {
	knowledge := &fakeRetrieveKnowledge{seedRows: []map[string]any{
		seedRow("doc-1", "chunk-0", "a", 0.9),
		seedRow("doc-1", "chunk-1", "b", 0.8),
		seedRow("doc-1", "chunk-2", "c", 0.7),
		seedRow("doc-1", "chunk-3", "d", 0.6),
		seedRow("doc-1", "chunk-4", "e", 0.5),
	}}
	reranker := &fakeReranker{scored: []rerank.Scored{
		{Index: 3, Score: 0.99}, {Index: 1, Score: 0.88},
		{Index: 0, Score: 0.50}, {Index: 2, Score: 0.40}, {Index: 4, Score: 0.30},
	}}
	svc := &Service{
		Searcher:      &fakeSearchBackend{},
		Knowledge:     knowledge,
		QueryEmbedder: &fakeQueryEmbedder{vector: []float64{0.1}},
		Reranker:      reranker,
	}

	if _, err := svc.Retrieve(t.Context(), SearchRequest{Query: "q", Limit: 2}); err != nil {
		t.Fatal(err)
	}
	ids, ok := knowledge.expandWinnerIDs()
	if !ok {
		t.Fatal("no NEXT_CHUNK expansion read recorded")
	}
	if len(ids) != 2 || ids[0] != "chunk-3" || ids[1] != "chunk-1" {
		t.Fatalf("expanded ids = %v, want only the top-2 winners [chunk-3 chunk-1]", ids)
	}
}

// TestRetrieveBelowThresholdKeepsSeedOrder: the non-monotonic guard keeps the seed
// order when the top rerank score is below RerankThreshold.
func TestRetrieveBelowThresholdKeepsSeedOrder(t *testing.T) {
	knowledge := &fakeRetrieveKnowledge{seedRows: []map[string]any{
		seedRow("doc-1", "chunk-0", "alpha", 0.9),
		seedRow("doc-1", "chunk-1", "bravo", 0.5),
		seedRow("doc-1", "chunk-2", "charlie", 0.1),
	}}
	// Reranker WANTS to reorder, but its top score 0.30 is below the 0.50 threshold.
	reranker := &fakeReranker{scored: []rerank.Scored{
		{Index: 2, Score: 0.30}, {Index: 0, Score: 0.20}, {Index: 1, Score: 0.10},
	}}
	svc := &Service{
		Searcher:        &fakeSearchBackend{},
		Knowledge:       knowledge,
		QueryEmbedder:   &fakeQueryEmbedder{vector: []float64{0.1}},
		Reranker:        reranker,
		RerankThreshold: 0.50,
	}

	hits, err := svc.Retrieve(t.Context(), SearchRequest{Query: "q", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got := chunkIDs(hits); got != "chunk-0,chunk-1,chunk-2" {
		t.Fatalf("below-threshold order = %s, want seed order kept", got)
	}
}

// TestRetrieveExpansionAppendsNeighborContext: winners come first in reranked order,
// then the unique 1-hop neighbours as context.
func TestRetrieveExpansionAppendsNeighborContext(t *testing.T) {
	knowledge := &fakeRetrieveKnowledge{
		seedRows: []map[string]any{
			seedRow("doc-1", "chunk-0", "alpha", 0.9),
			seedRow("doc-1", "chunk-1", "bravo", 0.5),
		},
		expandRows: []map[string]any{
			seedRow("doc-1", "chunk-1", "bravo (already a winner)", 0), // dedup'd out
			seedRow("doc-1", "chunk-9", "neighbour context", 0),
		},
	}
	reranker := &fakeReranker{scored: []rerank.Scored{{Index: 0, Score: 0}, {Index: 1, Score: 0}}}
	svc := &Service{
		Searcher:      &fakeSearchBackend{},
		Knowledge:     knowledge,
		QueryEmbedder: &fakeQueryEmbedder{vector: []float64{0.1}},
		Reranker:      reranker,
	}

	hits, err := svc.Retrieve(t.Context(), SearchRequest{Query: "q", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got := chunkIDs(hits); got != "chunk-0,chunk-1,chunk-9" {
		t.Fatalf("hits = %s, want winners then unique neighbour", got)
	}
	for _, call := range knowledge.reads {
		if strings.Contains(call.query, "HAS_CHUNK") {
			t.Fatal("legacy Retrieve expanded through the GraphRAG route")
		}
	}
}

// TestRetrieveNoRerankerMatchesSearch: with no reranker configured Retrieve is exactly
// Search (today's fulltext order) — the no-regression contract.
func TestRetrieveNoRerankerMatchesSearch(t *testing.T) {
	searcher := &fakeSearchBackend{hits: []SearchHit{
		{DocumentID: "doc-1", ChunkID: "ft-0", Score: 3.0},
		{DocumentID: "doc-1", ChunkID: "ft-1", Score: 2.0},
	}}
	svc := &Service{Searcher: searcher}

	hits, err := svc.Retrieve(t.Context(), SearchRequest{Query: "manual", Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if got := chunkIDs(hits); got != "ft-0,ft-1" {
		t.Fatalf("no-reranker order = %s, want fulltext Search order", got)
	}
	if len(searcher.requests) != 1 || searcher.requests[0].Query != "manual" {
		t.Fatalf("Search not delegated: %#v", searcher.requests)
	}
}

// TestRetrieveVectorSeedErrorFallsBackToFulltext: a vector-query error (with a working
// embedder) still falls back to the sparse fulltext seeds with no hard fail.
func TestRetrieveVectorSeedErrorFallsBackToFulltext(t *testing.T) {
	knowledge := &fakeRetrieveKnowledge{seedErr: errors.New("vector index offline")}
	searcher := &fakeSearchBackend{hits: []SearchHit{{DocumentID: "doc-1", ChunkID: "ft-0", Text: "fallback"}}}
	svc := &Service{
		Searcher:      searcher,
		Knowledge:     knowledge,
		QueryEmbedder: &fakeQueryEmbedder{vector: []float64{0.1}},
		Reranker:      &fakeReranker{scored: []rerank.Scored{{Index: 0, Score: 0}}},
	}

	hits, err := svc.Retrieve(t.Context(), SearchRequest{Query: "q", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got := chunkIDs(hits); got != "ft-0" {
		t.Fatalf("hits = %s, want fulltext fallback after a vector-seed error", got)
	}
	if len(searcher.requests) != 1 {
		t.Fatalf("fulltext searcher called %d times", len(searcher.requests))
	}
}

// TestRetrieveSeedErrorPropagates: with the reranker on but no usable seed source
// (embedder fails, no fulltext searcher) Retrieve surfaces the seed error.
func TestRetrieveSeedErrorPropagates(t *testing.T) {
	svc := &Service{
		QueryEmbedder: &fakeQueryEmbedder{err: errors.New("embed down")},
		Reranker:      &fakeReranker{},
	}
	if _, err := svc.Retrieve(t.Context(), SearchRequest{Query: "q"}); err == nil {
		t.Fatal("want a seed-fallback error when no searcher is configured")
	}
}

// TestRetrieveEmptySeedsReturnsEmpty: an empty seed pool yields an empty result without
// calling the reranker or expansion.
func TestRetrieveEmptySeedsReturnsEmpty(t *testing.T) {
	reranker := &fakeReranker{}
	svc := &Service{
		Searcher:      &fakeSearchBackend{}, // returns nil hits
		QueryEmbedder: &fakeQueryEmbedder{err: errors.New("embed down")},
		Reranker:      reranker,
	}

	hits, err := svc.Retrieve(t.Context(), SearchRequest{Query: "q"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("hits = %d, want 0", len(hits))
	}
	if reranker.calls != 0 {
		t.Fatalf("reranker called %d times on an empty seed pool", reranker.calls)
	}
}

func TestEffectiveLimitClamps(t *testing.T) {
	cases := []struct{ in, want int }{{0, defaultSearchLimit}, {-3, defaultSearchLimit}, {5, 5}, {99, maxSearchLimit}}
	for _, tc := range cases {
		if got := effectiveLimit(tc.in); got != tc.want {
			t.Fatalf("effectiveLimit(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func chunkIDs(hits []SearchHit) string {
	ids := make([]string, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.ChunkID)
	}
	return strings.Join(ids, ",")
}

func seedRow(docID, chunkID, text string, score float64) map[string]any {
	return map[string]any{
		"document_id":  docID,
		"file_name":    "manual.pdf",
		"chunk_id":     chunkID,
		"text":         text,
		"locator_json": "",
		"heading_path": []string{},
		"score":        score,
	}
}

type fakeRetrieveKnowledge struct {
	seedRows   []map[string]any
	expandRows []map[string]any
	seedErr    error
	expandErr  error
	reads      []knowledgeCall
}

func (f *fakeRetrieveKnowledge) Read(_ context.Context, query string, params map[string]any) ([]map[string]any, error) {
	f.reads = append(f.reads, knowledgeCall{query: query, params: params})
	switch {
	case strings.Contains(query, "queryNodes") || strings.Contains(query, "vector.similarity.cosine"):
		return f.seedRows, f.seedErr
	case strings.Contains(query, "NEXT_CHUNK"):
		return f.expandRows, f.expandErr
	default:
		return nil, nil
	}
}

func (f *fakeRetrieveKnowledge) Write(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (f *fakeRetrieveKnowledge) readContains(needle string) bool {
	for _, call := range f.reads {
		if strings.Contains(call.query, needle) {
			return true
		}
	}
	return false
}

func (f *fakeRetrieveKnowledge) expandWinnerIDs() ([]string, bool) {
	for _, call := range f.reads {
		if !strings.Contains(call.query, "NEXT_CHUNK") {
			continue
		}
		ids, ok := call.params["winner_ids"].([]string)
		return ids, ok
	}
	return nil, false
}

type fakeQueryEmbedder struct {
	vector []float64
	err    error
	calls  int
}

func (f *fakeQueryEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make([][]float64, len(texts))
	for i := range texts {
		out[i] = f.vector
	}
	return out, nil
}

type fakeReranker struct {
	scored  []rerank.Scored
	err     error
	calls   int
	gotDocs []string
}

func (f *fakeReranker) Rerank(_ context.Context, _ string, docs []string) ([]rerank.Scored, error) {
	f.calls++
	f.gotDocs = docs
	if f.err != nil {
		return nil, f.err
	}
	return f.scored, nil
}
