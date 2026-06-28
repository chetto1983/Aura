//go:build retrieval_eval

// retrieval_eval_test.go is the LIVE retrieval eval harness entry point (RET-05). It
// drives the REAL two-stage documents.Service.Retrieve over a freshly-ingested G220-class
// corpus, scoring nDCG@10 / Recall@5 / MRR for vector-only (identity reranker) vs
// vector+rerank (the real GPU rerank sidecar) across the judged set, and asserts (a) a
// mean nDCG@10 lift and (b) ZERO queries regressing beyond the noise threshold — the
// non-monotonic guard verification.
//
// It is a GATED tier (`-tags retrieval_eval`), NOT part of CI / quality-full: the live
// "rerank beats vector" comparison needs the GPU reranker. NO-SKIP-AS-GREEN: under $CI an
// unset corpus/AURA_RERANK_BASE_URL is a misconfigured job (t.Fatal), and a reranker that
// reorders nothing (sidecar unreachable) t.Fatals under $CI rather than passing green.
//
// REPRODUCE (GPU host):
//
//	set -a; . ./.env; set +a
//	export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
//	export AURA_DOC_TEST_PDF=/path/to/G220.pdf AURA_RERANK_BASE_URL=http://127.0.0.1:8085
//	go test -tags retrieval_eval -run TestRetrievalEval -timeout 900s -v ./internal/eval/
package eval

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/chetto1983/aura/internal/rerank"
)

func TestRetrievalEval(t *testing.T) {
	pdf := os.Getenv("AURA_DOC_TEST_PDF")
	rerankURL := os.Getenv("AURA_RERANK_BASE_URL")
	if pdf == "" || rerankURL == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("retrieval_eval requires AURA_DOC_TEST_PDF and AURA_RERANK_BASE_URL under CI — a skipped tier must never pass as green")
		}
		t.Skip("set AURA_DOC_TEST_PDF and AURA_RERANK_BASE_URL (GPU rerank sidecar) to run the retrieval_eval tier locally")
	}

	minLift := retrievalEnvFloat("AURA_RERANK_EVAL_MIN_LIFT", defaultMinLift)
	noise := retrievalEnvFloat("AURA_RERANK_EVAL_NOISE", defaultRegressionNoise)
	minJudged := retrievalEnvInt("AURA_RERANK_EVAL_MIN_JUDGED", defaultMinJudged)

	judgments, err := loadJudgments(filepath.Join("testdata", "retrieval_judgments.json"))
	if err != nil {
		t.Fatalf("load judgments: %v", err)
	}
	if len(judgments) < 30 {
		t.Fatalf("judged set has %d entries, want >= 30", len(judgments))
	}

	cfg := config.LoadDB()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
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
	embedClient := &documents.EmbeddingClient{BaseURL: cfg.Neo4j.EmbedURL, Dimensions: cfg.Neo4j.EmbedDimensions}
	embed := &evalEmbedQueue{worker: &documents.EmbeddingWorker{
		Jobs:      documents.NewPostgresJobStore(pool),
		Generator: embedClient,
		Indexer:   &documents.Indexer{Client: mcp},
	}}
	svc := &documents.Service{
		Jobs:          documents.NewPostgresJobStore(pool),
		Extractor:     &documents.ExtractClient{BaseURL: baseURL},
		Indexer:       &documents.Indexer{Client: mcp},
		Searcher:      &documents.Searcher{Client: mcp},
		Embedder:      embed, // synchronous: embeddings present before we query
		Knowledge:     mcp,
		QueryEmbedder: embedClient,
	}

	job, err := svc.IngestPath(ctx, documents.IngestRequest{SourceID: "retrieval-eval", SourceKind: "local"}, pdf)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if job.SparseChunks < 1 {
		t.Fatalf("ingested %d chunks, want >= 1", job.SparseChunks)
	}
	t.Logf("ingest+embed %s: chunks=%d embedded=%d embedErr=%v", job.FileName, job.SparseChunks, embed.embedded, embed.err)

	realReranker := &rerank.RerankClient{BaseURL: rerankURL}
	var results []queryEval
	for _, j := range judgments {
		rel, err := resolveRelevant(ctx, mcp, job.DocumentID, j.RelevantPhrases)
		if err != nil {
			t.Fatalf("resolve relevant for %q: %v", j.Query, err)
		}
		if len(rel) == 0 {
			t.Logf("skip %q: no chunk in this corpus carries phrases %v", j.Query, j.RelevantPhrases)
			continue
		}

		// Vector-only baseline: identity reranker keeps the seed order through the same
		// two-stage pipeline. Treatment: the real GPU reranker.
		svc.Reranker = evalReranker{}
		vhits, err := svc.Retrieve(ctx, documents.SearchRequest{Query: j.Query, DocumentID: job.DocumentID, Limit: 10})
		if err != nil {
			t.Fatalf("vector retrieve %q: %v", j.Query, err)
		}
		svc.Reranker = realReranker
		rhits, err := svc.Retrieve(ctx, documents.SearchRequest{Query: j.Query, DocumentID: job.DocumentID, Limit: 10})
		if err != nil {
			t.Fatalf("rerank retrieve %q: %v", j.Query, err)
		}

		vRanked := rankedChunkIDs(vhits)
		rRanked := rankedChunkIDs(rhits)
		results = append(results, queryEval{
			query:        j.Query,
			relevant:     len(rel),
			vectorNDCG:   ndcgAtK(vRanked, rel, 10),
			rerankNDCG:   ndcgAtK(rRanked, rel, 10),
			vectorRecall: recallAtK(vRanked, rel, 5),
			rerankRecall: recallAtK(rRanked, rel, 5),
			vectorMRR:    mrr(vRanked, rel),
			rerankMRR:    mrr(rRanked, rel),
			changed:      !equalIDs(vRanked, rRanked),
		})
	}

	if len(results) < minJudged {
		t.Fatalf("only %d judged queries resolved relevant chunks (need >= %d) — corpus/phrase mismatch with %s", len(results), minJudged, job.FileName)
	}

	s := summarize(results, minLift, noise)
	if err := writeRetrievalReport(retrievalReportPath, results, s, rerankURL, true); err != nil {
		t.Fatalf("write report: %v", err)
	}
	t.Logf("retrieval eval: judged=%d reordered=%d nDCG@10 vec=%.4f rr=%.4f lift=%+.4f Recall@5 vec=%.4f rr=%.4f regressions=%d",
		s.judged, s.changed, s.meanVectorNDCG, s.meanRerankNDCG, s.lift, s.meanVectorRec, s.meanRerankRec, len(s.regressions))

	// No-skip-as-green: a reranker that reordered nothing means the sidecar is absent /
	// unreachable. Under CI that is a misconfigured job (fatal); locally skip the lift
	// assertion (the GPU sidecar is not present on this host).
	if s.changed == 0 {
		if os.Getenv("CI") != "" {
			t.Fatalf("reranker reordered ZERO of %d judged queries — sidecar at %s unreachable? (a no-op rerank must not pass green under CI)", s.judged, rerankURL)
		}
		t.Skipf("reranker reordered zero queries (GPU sidecar at %s likely absent) — lift assertion skipped locally", rerankURL)
	}

	if s.lift < minLift-1e-9 {
		t.Fatalf("mean nDCG@10 lift %+.4f below required %.4f — rerank did not beat vector-only on the judged set", s.lift, minLift)
	}
	if len(s.regressions) > 0 {
		t.Fatalf("rerank regressed %d queries beyond the %.2f noise threshold (non-monotonic guard breach): %v", len(s.regressions), noise, s.regressions)
	}
}
