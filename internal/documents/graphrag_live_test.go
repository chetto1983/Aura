//go:build graphrag_live

package documents

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/chetto1983/aura/internal/rerank"
)

// rerankDominantFloor is the rerank-stage p95 above which the reranker is judged to be
// actually doing cross-encoder work (spike 070: GPU rerank ~333ms; a failed/identity
// rerank is sub-50ms). Only then is the "vector + expand each well under rerank"
// budget asserted; without the GPU sidecar the absolute per-stage budget still holds.
const rerankDominantFloor = 50 * time.Millisecond

// stageBudget is the per-stage p95 ceiling for the cheap stages (vector seed, graph
// expand). Spike 070 Q2 measured ~10–15ms over a DIRECT Bolt driver; Aura's production
// path reads through the mcp-neo4j-cypher stdio MCP seam (CLAUDE.md bans a native Go
// driver), which adds ~40–50ms of subprocess/JSON-RPC round-trip per read. 150ms is the
// honest ceiling for that path with headroom for a shared host — still a small fraction
// of the 333ms GPU rerank and the 500ms end-to-end budget, so the spike thesis (vector
// + graph are cheap, rerank dominates) holds. RET-05's perf gate tightens from here.
const stageBudget = 150 * time.Millisecond

// e2eBudget is the end-to-end p95 ceiling (RET-02 parity): seed + rerank + expand stays
// under 500ms even with the GPU reranker as the dominant stage.
const e2eBudget = 500 * time.Millisecond

// syncEmbedQueue runs the embedding worker synchronously inside IngestPath so the
// chunk_embedding HNSW index is populated before GraphRAG seeds — exercising the real
// dense vector path instead of the fulltext fallback. It records the outcome for the
// test to log; IngestPath itself ignores the enqueue result.
type syncEmbedQueue struct {
	worker   *EmbeddingWorker
	embedded int
	err      error
}

func (q *syncEmbedQueue) Enqueue(ctx context.Context, doc ExtractedDocument) error {
	q.err = q.worker.Process(ctx, doc)
	q.embedded = len(doc.Chunks)
	return q.err
}

// TestGraphRAGLive proves the documented per-stage p95 budget over the live connected
// graph: vector seed and graph expand are trivially cheap; the rerank stage is the
// dominant bounded cost when the GPU sidecar is present, and absent it the vector +
// expand budget still asserts. It ingests + embeds the G220-class PDF, then runs
// GraphRAG and computes per-stage p95. NO-SKIP-AS-GREEN: under $CI an unset fixture is
// a misconfigured job (t.Fatal), never a silent green.
func TestGraphRAGLive(t *testing.T) {
	pdf := os.Getenv("AURA_DOC_TEST_PDF")
	rerankURL := os.Getenv("AURA_RERANK_BASE_URL")
	if pdf == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("graphrag_live requires AURA_DOC_TEST_PDF under CI — a skipped tier must never pass as green")
		}
		t.Skip("set AURA_DOC_TEST_PDF (and optionally AURA_RERANK_BASE_URL) to run the graphrag_live tier locally")
	}

	cfg := config.LoadDB()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	mcp, err := knowledge.Open(ctx, &cfg.Neo4j)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mcp.Close() }()

	baseURL := cfg.DocumentsBaseURL
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8083"
	}
	embedClient := &EmbeddingClient{BaseURL: cfg.Neo4j.EmbedURL, Dimensions: cfg.Neo4j.EmbedDimensions}
	embed := &syncEmbedQueue{worker: &EmbeddingWorker{
		Jobs:      NewPostgresJobStore(pool),
		Generator: embedClient,
		Indexer:   &Indexer{Client: mcp},
	}}
	svc := &Service{
		Jobs:          NewPostgresJobStore(pool),
		Extractor:     &ExtractClient{BaseURL: baseURL},
		Indexer:       &Indexer{Client: mcp},
		Searcher:      &Searcher{Client: mcp},
		Embedder:      embed, // synchronous: embeddings present before we query
		Knowledge:     mcp,
		QueryEmbedder: embedClient,
	}
	rerankActive := rerankURL != ""
	if rerankActive {
		svc.Reranker = &rerank.RerankClient{BaseURL: rerankURL}
	}

	ingestStart := time.Now()
	job, err := svc.IngestPath(ctx, IngestRequest{SourceID: "graphrag-live", SourceKind: "local"}, pdf)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if job.SparseChunks < 1 {
		t.Fatalf("ingested %d chunks, want >= 1", job.SparseChunks)
	}
	t.Logf("ingest+embed %s: chunks=%d embedded=%d embedErr=%v elapsed=%s",
		job.FileName, job.SparseChunks, embed.embedded, embed.err, time.Since(ingestStart))

	query := os.Getenv("AURA_DOC_TEST_QUERY")
	if query == "" {
		query = "how do I reset the converter to factory settings"
	}

	// Warm the MCP subprocess + vector index before measuring.
	for range 3 {
		if _, err = svc.GraphRAG(ctx, SearchRequest{Query: query, DocumentID: job.DocumentID, Limit: 5}); err != nil {
			t.Fatalf("graphrag warmup: %v", err)
		}
	}

	const reps = 12
	var vec, exp, rer, e2e []time.Duration
	var last GraphRAGResult
	for range reps {
		start := time.Now()
		res, gErr := svc.GraphRAG(ctx, SearchRequest{Query: query, DocumentID: job.DocumentID, Limit: 5})
		if gErr != nil {
			t.Fatalf("graphrag: %v", gErr)
		}
		e2e = append(e2e, time.Since(start))
		vec = append(vec, time.Duration(res.Stages.VectorMS)*time.Millisecond)
		rer = append(rer, time.Duration(res.Stages.RerankMS)*time.Millisecond)
		exp = append(exp, time.Duration(res.Stages.ExpandMS)*time.Millisecond)
		last = res
	}

	vp50, vp95 := livePercentile(vec, 0.50), liveP95(vec)
	ep50, ep95 := livePercentile(exp, 0.50), liveP95(exp)
	rp50, rp95 := livePercentile(rer, 0.50), liveP95(rer)
	e2p50, e2p95 := livePercentile(e2e, 0.50), liveP95(e2e)
	t.Logf("GraphRAG per-stage budget over %d reps (winners=%d context=%d rerank_active=%v):",
		reps, len(last.Hits), len(last.Context), rerankActive)
	t.Logf("  vector seed   p50=%s p95=%s", vp50, vp95)
	t.Logf("  graph expand  p50=%s p95=%s", ep50, ep95)
	t.Logf("  rerank        p50=%s p95=%s", rp50, rp95)
	t.Logf("  END-TO-END    p50=%s p95=%s", e2p50, e2p95)

	// Absolute per-stage budget (no GPU needed): vector + graph stay cheap over the MCP
	// path, a small fraction of the rerank stage and the end-to-end budget.
	if vp95 > stageBudget {
		t.Fatalf("vector-seed p95 %s exceeds %s budget", vp95, stageBudget)
	}
	if ep95 > stageBudget {
		t.Fatalf("graph-expand p95 %s exceeds %s budget", ep95, stageBudget)
	}
	if e2p95 > e2eBudget {
		t.Fatalf("end-to-end p95 %s exceeds %s budget", e2p95, e2eBudget)
	}
	if len(last.Hits) == 0 {
		t.Fatal("graphrag returned no winners")
	}

	// Rerank-dominant comparison only when the GPU reranker actually ran (spike 070):
	// vector and expand must each be well under the rerank stage. Absent the sidecar
	// (GPU-less host) the rerank stage is sub-floor and the comparison is skipped.
	if rerankActive && rp95 >= rerankDominantFloor {
		if vp95 >= rp95 {
			t.Fatalf("vector p95 %s not under rerank p95 %s", vp95, rp95)
		}
		if ep95 >= rp95 {
			t.Fatalf("expand p95 %s not under rerank p95 %s", ep95, rp95)
		}
	} else {
		t.Logf("rerank stage sub-floor (p95=%s < %s) or absent — vector+expand absolute budget asserted, rerank-dominant comparison skipped",
			rp95, rerankDominantFloor)
	}
}
