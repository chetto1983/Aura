package documents

import (
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/rerank"
)

// scriptedClock returns a fixed sequence of timestamps, clamping at the last, so a
// test can assert exact per-stage durations from GraphRAG's monotonic timer.
type scriptedClock struct {
	times []time.Time
	i     int
}

func (c *scriptedClock) now() time.Time {
	t := c.times[c.i]
	if c.i < len(c.times)-1 {
		c.i++
	}
	return t
}

// TestGraphRAGStageTimingsPopulated: each stage is timed independently between its
// own start/end (the scripted clock yields distinct Vector/Rerank/Expand deltas).
func TestGraphRAGStageTimingsPopulated(t *testing.T) {
	knowledge := &fakeRetrieveKnowledge{
		seedRows: []map[string]any{
			seedRow("doc-1", "chunk-0", "alpha", 0.9),
			seedRow("doc-1", "chunk-1", "bravo", 0.5),
		},
		expandRows: []map[string]any{seedRow("doc-1", "chunk-2", "neighbour", 0)},
	}
	reranker := &fakeReranker{scored: []rerank.Scored{{Index: 1, Score: 0.9}, {Index: 0, Score: 0.8}}}
	base := time.Unix(0, 0)
	clk := &scriptedClock{times: []time.Time{
		base,                            // t0 (seed start)
		base.Add(12 * time.Millisecond), // t1 -> VectorMS = 12
		base.Add(30 * time.Millisecond), // t2 -> RerankMS = 18
		base.Add(35 * time.Millisecond), // t3 -> ExpandMS = 5
	}}
	svc := &Service{
		Searcher:      &fakeSearchBackend{},
		Knowledge:     knowledge,
		QueryEmbedder: &fakeQueryEmbedder{vector: []float64{0.1}},
		Reranker:      reranker,
		timeSource:    clk.now,
	}

	res, err := svc.GraphRAG(t.Context(), SearchRequest{Query: "q", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stages.VectorMS != 12 || res.Stages.RerankMS != 18 || res.Stages.ExpandMS != 5 {
		t.Fatalf("stages = %+v, want {Vector:12 Expand:5 Rerank:18}", res.Stages)
	}
}

// TestGraphRAGRerankReordersWinnersWithContext: rerank reorders the winners (in
// Hits, with rerank scores), and the unique 1-hop neighbours ride in Context.
func TestGraphRAGRerankReordersWinnersWithContext(t *testing.T) {
	knowledge := &fakeRetrieveKnowledge{
		seedRows: []map[string]any{
			seedRow("doc-1", "chunk-0", "alpha", 0.9),
			seedRow("doc-1", "chunk-1", "bravo", 0.5),
			seedRow("doc-1", "chunk-2", "charlie", 0.1),
		},
		expandRows: []map[string]any{
			seedRow("doc-1", "chunk-2", "winner dup", 0), // a winner -> deduped from Context
			seedRow("doc-1", "chunk-9", "neighbour", 0),
		},
	}
	reranker := &fakeReranker{scored: []rerank.Scored{
		{Index: 2, Score: 0.99}, {Index: 0, Score: 0.80}, {Index: 1, Score: 0.20},
	}}
	svc := &Service{
		Searcher:      &fakeSearchBackend{},
		Knowledge:     knowledge,
		QueryEmbedder: &fakeQueryEmbedder{vector: []float64{0.1}},
		Reranker:      reranker,
	}

	res, err := svc.GraphRAG(t.Context(), SearchRequest{Query: "q", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got := chunkIDs(res.Hits); got != "chunk-2,chunk-0,chunk-1" {
		t.Fatalf("Hits = %s, want reranked winners", got)
	}
	if res.Hits[0].Score != 0.99 {
		t.Fatalf("winner score = %v, want rerank score 0.99", res.Hits[0].Score)
	}
	if got := chunkIDs(res.Context); got != "chunk-9" {
		t.Fatalf("Context = %s, want only the unique neighbour chunk-9", got)
	}
}

// TestGraphRAGExpandsOnlyWinners: after rerank, only the top-K winners (not the
// whole seed pool) are passed to the 1-hop expansion (T-30-11 bound).
func TestGraphRAGExpandsOnlyWinners(t *testing.T) {
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

	if _, err := svc.GraphRAG(t.Context(), SearchRequest{Query: "q", Limit: 2}); err != nil {
		t.Fatal(err)
	}
	ids, ok := knowledge.expandWinnerIDs()
	if !ok {
		t.Fatal("no graph expansion read recorded")
	}
	if len(ids) != 2 || ids[0] != "chunk-3" || ids[1] != "chunk-1" {
		t.Fatalf("expanded ids = %v, want only the top-2 winners [chunk-3 chunk-1]", ids)
	}
}

// TestGraphRAGRerankOffKeepsSeedOrderWithContext: an identity reranker (degraded
// sidecar) keeps the seed order and seed scores, with the expanded context still
// attached and no error (rerank failure never blocks GraphRAG).
func TestGraphRAGRerankOffKeepsSeedOrderWithContext(t *testing.T) {
	knowledge := &fakeRetrieveKnowledge{
		seedRows: []map[string]any{
			seedRow("doc-1", "chunk-0", "alpha", 0.9),
			seedRow("doc-1", "chunk-1", "bravo", 0.5),
		},
		expandRows: []map[string]any{seedRow("doc-1", "chunk-7", "ctx", 0)},
	}
	reranker := &fakeReranker{scored: []rerank.Scored{{Index: 0, Score: 0}, {Index: 1, Score: 0}}}
	svc := &Service{
		Searcher:      &fakeSearchBackend{},
		Knowledge:     knowledge,
		QueryEmbedder: &fakeQueryEmbedder{vector: []float64{0.1}},
		Reranker:      reranker,
	}

	res, err := svc.GraphRAG(t.Context(), SearchRequest{Query: "q", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got := chunkIDs(res.Hits); got != "chunk-0,chunk-1" {
		t.Fatalf("Hits = %s, want seed order on identity rerank", got)
	}
	if res.Hits[0].Score != 0.9 {
		t.Fatalf("seed score = %v, want 0.9 preserved", res.Hits[0].Score)
	}
	if got := chunkIDs(res.Context); got != "chunk-7" {
		t.Fatalf("Context = %s, want neighbour attached even with rerank off", got)
	}
}

// TestGraphRAGEmbedderFailureFallsBackToFulltext: an embedder failure seeds from the
// sparse fulltext path (and never runs the vector seed), then still expands.
func TestGraphRAGEmbedderFailureFallsBackToFulltext(t *testing.T) {
	knowledge := &fakeRetrieveKnowledge{
		seedRows:   []map[string]any{seedRow("doc-1", "vector-only", "unused", 0.99)},
		expandRows: []map[string]any{seedRow("doc-1", "chunk-9", "ctx", 0)},
	}
	searcher := &fakeSearchBackend{hits: []SearchHit{
		{DocumentID: "doc-1", ChunkID: "ft-0", Text: "fulltext zero", Score: 3.2},
		{DocumentID: "doc-1", ChunkID: "ft-1", Text: "fulltext one", Score: 2.1},
	}}
	reranker := &fakeReranker{scored: []rerank.Scored{{Index: 0, Score: 0}, {Index: 1, Score: 0}}}
	svc := &Service{
		Searcher:      searcher,
		Knowledge:     knowledge,
		QueryEmbedder: &fakeQueryEmbedder{err: errors.New("embed sidecar down")},
		Reranker:      reranker,
	}

	res, err := svc.GraphRAG(t.Context(), SearchRequest{Query: "q", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got := chunkIDs(res.Hits); got != "ft-0,ft-1" {
		t.Fatalf("Hits = %s, want fulltext seeds", got)
	}
	if knowledge.readContains("queryNodes") {
		t.Fatal("vector seed must be skipped when the embedder fails")
	}
}

// TestGraphRAGEmptySeedsReturnsEmpty: an empty seed pool short-circuits — no rerank,
// no expand — but the seed stage is still timed (VectorMS), the others stay zero.
func TestGraphRAGEmptySeedsReturnsEmpty(t *testing.T) {
	reranker := &fakeReranker{}
	base := time.Unix(0, 0)
	clk := &scriptedClock{times: []time.Time{base, base.Add(7 * time.Millisecond)}}
	svc := &Service{
		Searcher:      &fakeSearchBackend{}, // returns nil hits
		QueryEmbedder: &fakeQueryEmbedder{err: errors.New("embed down")},
		Reranker:      reranker,
		timeSource:    clk.now,
	}

	res, err := svc.GraphRAG(t.Context(), SearchRequest{Query: "q"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 || len(res.Context) != 0 {
		t.Fatalf("Hits=%d Context=%d, want empty", len(res.Hits), len(res.Context))
	}
	if reranker.calls != 0 {
		t.Fatalf("reranker called %d times on an empty seed pool", reranker.calls)
	}
	if res.Stages.VectorMS != 7 {
		t.Fatalf("VectorMS = %d, want 7 (seed stage timed even when empty)", res.Stages.VectorMS)
	}
	if res.Stages.RerankMS != 0 || res.Stages.ExpandMS != 0 {
		t.Fatalf("RerankMS/ExpandMS = %d/%d, want 0 (stages skipped)", res.Stages.RerankMS, res.Stages.ExpandMS)
	}
}

// TestGraphRAGSeedErrorPropagates: with the reranker on but no usable seed source
// (embedder fails, no fulltext searcher) GraphRAG surfaces the seed error.
func TestGraphRAGSeedErrorPropagates(t *testing.T) {
	svc := &Service{
		QueryEmbedder: &fakeQueryEmbedder{err: errors.New("embed down")},
		Reranker:      &fakeReranker{},
	}
	if _, err := svc.GraphRAG(t.Context(), SearchRequest{Query: "q"}); err == nil {
		t.Fatal("want a seed-fallback error when no searcher is configured")
	}
}
