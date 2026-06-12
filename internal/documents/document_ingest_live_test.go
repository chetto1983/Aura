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

func TestLiveDocumentIngestE2E(t *testing.T) {
	if os.Getenv("AURA_DOC_TEST_PDF") == "" &&
		os.Getenv("AURA_DOC_TEST_XLSX") == "" &&
		os.Getenv("AURA_DOC_TEST_DOCX") == "" {
		t.Skip("set at least one of AURA_DOC_TEST_PDF, AURA_DOC_TEST_XLSX, or AURA_DOC_TEST_DOCX")
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

func liveP95(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), values...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	return sorted[int(float64(len(sorted)-1)*0.95)]
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
