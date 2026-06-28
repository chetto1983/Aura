//go:build document_ingest_live

package documents

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/knowledge"
)

// docTestEnvs are the per-format file-path env vars the live tier reads.
var docTestEnvs = []string{
	"AURA_DOC_TEST_PDF",
	"AURA_DOC_TEST_XLSX",
	"AURA_DOC_TEST_DOCX",
	"AURA_DOC_TEST_PPTX",
	"AURA_DOC_TEST_HTML",
}

func TestLiveDocumentIngestE2E(t *testing.T) {
	anySet := false
	for _, key := range docTestEnvs {
		if os.Getenv(key) != "" {
			anySet = true
			break
		}
	}
	// No-skip-as-green: under $CI an unset env set is a misconfigured job, never a
	// silent green pass; locally it skips so a developer without fixtures is not
	// blocked. GitHub Actions always sets CI=true.
	if !anySet {
		if os.Getenv("CI") != "" {
			t.Fatalf("document_ingest_live requires at least one of %v under CI — a skipped tier must never pass as green", docTestEnvs)
		}
		t.Skipf("set at least one of %v to run the document_ingest_live tier locally", docTestEnvs)
	}

	cfg := config.LoadDB()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if os.Getenv("AURA_DOC_TEST_RESET") == "1" {
		schema, err := knowledge.OpenSchema(ctx, &cfg.Neo4j)
		if err != nil {
			t.Fatal(err)
		}
		if err = knowledge.Reset(ctx, schema, pool); err != nil {
			_ = schema.Close(ctx)
			t.Fatal(err)
		}
		_ = schema.Close(ctx)
	}

	mcp, err := knowledge.Open(ctx, &cfg.Neo4j)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mcp.Close() }()

	baseURL := cfg.DocumentsBaseURL
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8083"
	}
	svc := &Service{
		Jobs:      NewPostgresJobStore(pool),
		Extractor: &ExtractClient{BaseURL: baseURL},
		Indexer:   &Indexer{Client: mcp},
		Searcher:  &Searcher{Client: mcp},
	}

	cases := []struct {
		name      string
		env       string
		query     string
		maxIngest time.Duration
		minScore  float64
	}{
		{name: "pdf", env: "AURA_DOC_TEST_PDF", query: "safety reset", maxIngest: 3 * time.Second, minScore: 85},
		{name: "xlsx", env: "AURA_DOC_TEST_XLSX", query: "automazione linea", maxIngest: 3 * time.Second, minScore: 85},
		{name: "docx", env: "AURA_DOC_TEST_DOCX", query: "robot corso base", maxIngest: time.Second, minScore: 85},
		{name: "pptx", env: "AURA_DOC_TEST_PPTX", query: "agenda", maxIngest: 3 * time.Second, minScore: 85},
		{name: "html", env: "AURA_DOC_TEST_HTML", query: "introduction", maxIngest: 3 * time.Second, minScore: 85},
	}
	for _, tc := range cases {
		path := os.Getenv(tc.env)
		if path == "" {
			t.Logf("skip %s: %s unset", tc.name, tc.env)
			continue
		}
		query := os.Getenv("AURA_DOC_TEST_QUERY")
		if query == "" {
			query = tc.query
		}
		start := time.Now()
		job, err := svc.IngestPath(ctx, IngestRequest{SourceID: "live-e2e", SourceKind: "local"}, path)
		if err != nil {
			t.Fatalf("%s ingest: %v", tc.name, err)
		}
		ingest := time.Since(start)
		if job.SparseChunks < 1 {
			t.Fatalf("%s: ingested %d chunks, want >= 1", tc.name, job.SparseChunks)
		}
		// The reading-order chain must hold for every format: one document has
		// exactly chunk_count-1 :NEXT_CHUNK edges.
		edgeRows, err := mcp.Read(ctx,
			"MATCH (:Chunk {document_id:$id})-[:NEXT_CHUNK]->() RETURN count(*) AS count",
			map[string]any{"id": job.DocumentID})
		if err != nil {
			t.Fatalf("%s next-chunk count: %v", tc.name, err)
		}
		if got := countFromRows(edgeRows, -1); got != job.SparseChunks-1 {
			t.Fatalf("%s: NEXT_CHUNK edges = %d, want chunk_count-1 = %d", tc.name, got, job.SparseChunks-1)
		}
		var latencies []time.Duration
		var hits []SearchHit
		for range 5 {
			start = time.Now()
			hits, err = svc.Search(ctx, SearchRequest{Query: query, DocumentID: job.DocumentID, Limit: 8})
			if err != nil {
				t.Fatalf("%s search: %v", tc.name, err)
			}
			latencies = append(latencies, time.Since(start))
		}
		p95 := liveP95(latencies)
		score := liveIndustrialScore(ingest, p95, len(hits), job.SparseChunks)
		t.Logf("%s file=%s chunks=%d searchable=%s retrieval_p95=%s hits=%d score=%.1f",
			tc.name, job.FileName, job.SparseChunks, ingest, p95, len(hits), score)
		if ingest > tc.maxIngest {
			t.Fatalf("%s ingest %s exceeds %s", tc.name, ingest, tc.maxIngest)
		}
		if p95 > 50*time.Millisecond {
			t.Fatalf("%s retrieval p95 %s exceeds 50ms", tc.name, p95)
		}
		if score < tc.minScore {
			t.Fatalf("%s score %.1f below %.1f", tc.name, score, tc.minScore)
		}
	}
}

func liveIndustrialScore(ingest, p95 time.Duration, hits, chunks int) float64 {
	score := 100.0
	if chunks == 0 {
		score -= 40
	}
	if hits == 0 {
		score -= 25
	}
	if ingest > 3*time.Second {
		score -= float64((ingest - 3*time.Second).Milliseconds()) / 1000
	}
	if p95 > 50*time.Millisecond {
		score -= float64((p95 - 50*time.Millisecond).Milliseconds()) / 10
	}
	if score < 0 {
		return 0
	}
	return score
}
