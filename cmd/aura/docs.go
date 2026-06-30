package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/chetto1983/aura/internal/rerank"
	"github.com/jackc/pgx/v5/pgxpool"
)

const docsUsage = "usage: aura docs {ingest <path> [--source-id cli] [--source-kind local]|search <query> [--document-id id] [--limit 8]|status <job-id>|list [--limit 20]|bench <path> --query <query>}"

type docsCLIService interface {
	IngestPath(ctx context.Context, req documents.IngestRequest, path string) (*documents.Job, error)
	Search(ctx context.Context, req documents.SearchRequest) ([]documents.SearchHit, error)
	GetJob(ctx context.Context, id string) (*documents.Job, error)
	ListJobs(ctx context.Context, limit int) ([]documents.Job, error)
}

type docsServiceFactory func(context.Context) (docsCLIService, func(), error)

func runDocs(args []string) {
	if err := runDocsCommand(context.Background(), args, os.Stdout, newDocsService); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runDocsCommand(ctx context.Context, args []string, out io.Writer, factory docsServiceFactory) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", docsUsage)
	}
	switch args[0] {
	case "ingest":
		return docsIngest(ctx, args[1:], out, factory)
	case "search":
		return docsSearch(ctx, args[1:], out, factory)
	case "status":
		return docsStatus(ctx, args[1:], out, factory)
	case "list":
		return docsList(ctx, args[1:], out, factory)
	case "bench":
		return docsBench(ctx, args[1:], out, factory)
	default:
		return fmt.Errorf("unknown docs command %q\n%s", args[0], docsUsage)
	}
}

func docsIngest(ctx context.Context, args []string, out io.Writer, factory docsServiceFactory) error {
	fs := flag.NewFlagSet("docs ingest", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	sourceID := fs.String("source-id", "cli", "source id")
	sourceKind := fs.String("source-kind", "local", "source kind")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("docs ingest requires <path>")
	}
	svc, closeFn, err := factory(ctx)
	if err != nil {
		return err
	}
	defer closeFn()

	start := time.Now()
	job, err := svc.IngestPath(ctx, documents.IngestRequest{SourceID: *sourceID, SourceKind: *sourceKind}, fs.Arg(0))
	if err != nil {
		return err
	}
	return writeJSON(out, map[string]any{
		"job_id":           job.ID,
		"document_id":      job.DocumentID,
		"status":           job.Status,
		"file_name":        job.FileName,
		"chunks":           job.SparseChunks,
		"ingest_ms":        time.Since(start).Milliseconds(),
		"embedding_status": "not_started",
	})
}

func docsSearch(ctx context.Context, args []string, out io.Writer, factory docsServiceFactory) error {
	fs := flag.NewFlagSet("docs search", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	documentID := fs.String("document-id", "", "document id")
	limit := fs.Int("limit", 8, "limit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	query := strings.Join(fs.Args(), " ")
	if strings.TrimSpace(query) == "" {
		return fmt.Errorf("docs search requires <query>")
	}
	svc, closeFn, err := factory(ctx)
	if err != nil {
		return err
	}
	defer closeFn()

	start := time.Now()
	hits, err := svc.Search(ctx, documents.SearchRequest{Query: query, DocumentID: *documentID, Limit: *limit})
	if err != nil {
		return err
	}
	return writeJSON(out, map[string]any{
		"query":        query,
		"hits":         hits,
		"retrieval_ms": time.Since(start).Milliseconds(),
	})
}

func docsStatus(ctx context.Context, args []string, out io.Writer, factory docsServiceFactory) error {
	if len(args) != 1 {
		return fmt.Errorf("docs status requires <job-id>")
	}
	svc, closeFn, err := factory(ctx)
	if err != nil {
		return err
	}
	defer closeFn()
	job, err := svc.GetJob(ctx, args[0])
	if err != nil {
		return err
	}
	return writeJSON(out, job)
}

func docsList(ctx context.Context, args []string, out io.Writer, factory docsServiceFactory) error {
	fs := flag.NewFlagSet("docs list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	limit := fs.Int("limit", 20, "limit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	svc, closeFn, err := factory(ctx)
	if err != nil {
		return err
	}
	defer closeFn()
	jobs, err := svc.ListJobs(ctx, *limit)
	if err != nil {
		return err
	}
	return writeJSON(out, map[string]any{"jobs": jobs})
}

func docsBench(ctx context.Context, args []string, out io.Writer, factory docsServiceFactory) error {
	fs := flag.NewFlagSet("docs bench", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	query := fs.String("query", "", "retrieval query")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("docs bench requires <path>")
	}
	if strings.TrimSpace(*query) == "" {
		return fmt.Errorf("docs bench requires --query")
	}
	svc, closeFn, err := factory(ctx)
	if err != nil {
		return err
	}
	defer closeFn()

	path := fs.Arg(0)
	ingestStart := time.Now()
	job, err := svc.IngestPath(ctx, documents.IngestRequest{SourceID: "cli", SourceKind: "local"}, path)
	if err != nil {
		return err
	}
	timeToSearchable := time.Since(ingestStart)

	var latencies []time.Duration
	for range 5 {
		start := time.Now()
		if _, err = svc.Search(ctx, documents.SearchRequest{Query: *query, DocumentID: job.DocumentID, Limit: 8}); err != nil {
			return err
		}
		latencies = append(latencies, time.Since(start))
	}
	p95 := percentile(latencies, 0.95)
	return writeJSON(out, map[string]any{
		"file":                  job.FileName,
		"size_bytes":            job.SizeBytes,
		"chunks":                job.SparseChunks,
		"time_to_searchable_ms": timeToSearchable.Milliseconds(),
		"retrieval_p95_ms":      p95.Milliseconds(),
		"industrial_score":      industrialScore(timeToSearchable, p95, job.SparseChunks),
	})
}

func newDocsService(ctx context.Context) (docsCLIService, func(), error) {
	cfg := config.LoadDB()
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		return nil, nil, err
	}
	mcp, err := knowledge.Open(ctx, &cfg.Neo4j)
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	baseURL := documentsBaseURL(cfg)
	httpClient := documentHTTPClient(cfg)
	svc := &documents.Service{
		Jobs:      documents.NewPostgresJobStore(pool),
		Extractor: &documents.ExtractClient{BaseURL: baseURL, Client: httpClient},
		Indexer:   &documents.Indexer{Client: mcp},
		Searcher:  &documents.Searcher{Client: mcp},
	}
	return svc, func() {
		_ = mcp.Close()
		pool.Close()
	}, nil
}

type runtimeDocumentIngestor struct {
	cfg      *config.Config
	pool     *pgxpool.Pool
	MaxBytes int64
}

func newRuntimeDocumentIngestor(cfg *config.Config, pool *pgxpool.Pool) *runtimeDocumentIngestor {
	var maxBytes int64
	if cfg != nil {
		maxBytes = int64(cfg.AssetMaxDocumentBytes)
	}
	return &runtimeDocumentIngestor{cfg: cfg, pool: pool, MaxBytes: maxBytes}
}

func (i *runtimeDocumentIngestor) IngestPath(ctx context.Context, req documents.IngestRequest, path string) (*documents.Job, error) {
	if i == nil || i.cfg == nil || i.pool == nil {
		return nil, fmt.Errorf("document ingestor is not configured")
	}
	mcp, err := knowledge.Open(ctx, &i.cfg.Neo4j)
	if err != nil {
		return nil, err
	}
	defer func() { _ = mcp.Close() }()
	svc := &documents.Service{
		Jobs:      documents.NewPostgresJobStore(i.pool),
		Extractor: &documents.ExtractClient{BaseURL: documentsBaseURL(i.cfg), Client: documentHTTPClient(i.cfg)},
		Indexer:   &documents.Indexer{Client: mcp},
		Searcher:  &documents.Searcher{Client: mcp},
		Embedder:  runtimeEmbeddingQueue{cfg: i.cfg, pool: i.pool},
		MaxBytes:  i.MaxBytes,
	}
	return svc.IngestPath(ctx, req, path)
}

type runtimeEmbeddingQueue struct {
	cfg   *config.Config
	pool  *pgxpool.Pool
	store documents.IngestionJobCreator
	now   func() time.Time
}

func (q runtimeEmbeddingQueue) Enqueue(ctx context.Context, doc documents.ExtractedDocument) error {
	store := q.store
	if store == nil {
		if q.pool == nil {
			return fmt.Errorf("embedding queue is not configured")
		}
		store = documents.NewPostgresIngestionJobStore(q.pool)
	}
	queue := &documents.DurableEmbeddingQueue{
		Jobs:  store,
		Clock: q.clock,
	}
	return queue.Enqueue(ctx, doc)
}

func (q runtimeEmbeddingQueue) clock() time.Time {
	if q.now != nil {
		return q.now().UTC()
	}
	return time.Now().UTC()
}

type docsToolSearcher struct {
	cfg *config.Config
}

// Retrieve runs the two-stage retrieval pipeline for the document_search tool. It
// seeds from the dense chunk_embedding index (embed sidecar) or the sparse fulltext
// index, reranks the seeds via the optional aura-rerank sidecar, and 1-hop expands the
// winners. Every stage is fail-soft: a down embed/rerank sidecar degrades to the
// RRF/vector seed order, so retrieval never blocks on optional infrastructure.
func (s docsToolSearcher) Retrieve(ctx context.Context, req documents.SearchRequest) ([]documents.SearchHit, error) {
	if s.cfg == nil {
		return nil, fmt.Errorf("document search config is nil")
	}
	mcp, err := knowledge.Open(ctx, &s.cfg.Neo4j)
	if err != nil {
		return nil, err
	}
	defer func() { _ = mcp.Close() }()
	// One-knob local↔cloud rerank swap (D-28): AURA_RERANK_MODEL set → shared
	// OpenRouter endpoint + the single OPENROUTER_API_KEY; unset → local sidecar.
	rerankBase, rerankKey, rerankModel := s.cfg.RerankRoute()
	svc := &documents.Service{
		Searcher:      &documents.Searcher{Client: mcp},
		Knowledge:     mcp,
		QueryEmbedder: embeddingClient(s.cfg, documentHTTPClient(s.cfg)),
		Reranker: &rerank.RerankClient{
			BaseURL: rerankBase,
			Model:   rerankModel,
			APIKey:  rerankKey,
		},
	}
	return svc.Retrieve(ctx, req)
}

func documentsBaseURL(cfg *config.Config) string {
	if cfg != nil && cfg.DocumentsBaseURL != "" {
		return cfg.DocumentsBaseURL
	}
	return "http://127.0.0.1:8083"
}

func documentHTTPClient(cfg *config.Config) *http.Client {
	timeout := 120 * time.Second
	if cfg != nil && cfg.MultimodalTimeoutSec > 0 {
		timeout = time.Duration(cfg.MultimodalTimeoutSec) * time.Second
	}
	return &http.Client{Timeout: timeout}
}

func writeJSON(out io.Writer, value any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func percentile(values []time.Duration, p float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	cp := append([]time.Duration(nil), values...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	idx := int(float64(len(cp)-1) * p)
	return cp[idx]
}

func industrialScore(searchable, retrievalP95 time.Duration, chunks int) float64 {
	score := 100.0
	if chunks == 0 {
		score -= 40
	}
	if searchable > 3*time.Second {
		score -= float64((searchable - 3*time.Second).Milliseconds()) / 1000
	}
	if retrievalP95 > 50*time.Millisecond {
		score -= float64((retrievalP95 - 50*time.Millisecond).Milliseconds()) / 10
	}
	if score < 0 {
		return 0
	}
	return score
}
