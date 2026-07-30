package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/jackc/pgx/v5/pgxpool"
)

const docsUsage = "usage: aura docs {ingest <path> [--source-id cli] [--source-kind local]|search <query> [--document-id id] [--limit 8]|status <job-id>|list [--limit 20]|bench <path> --query <query>}"

// docsCLIService is the retrieval surface `aura docs` drives. It exposes Retrieve, NOT
// Search: Service.Search is a bare delegation to the sparse fulltext backend, while
// Retrieve is the two-stage pipeline production actually executes — the document_search
// tool falls back to it whenever FreezeRetrievalPlans errors, which today is always.
// Benching Search timed the seed stage alone and called it retrieval.
type docsCLIService interface {
	IngestPath(ctx context.Context, req documents.IngestRequest, path string) (*documents.Job, error)
	Retrieve(ctx context.Context, req documents.SearchRequest) ([]documents.SearchHit, error)
	GetJob(ctx context.Context, id string) (*documents.Job, error)
	ListJobs(ctx context.Context, limit int) ([]documents.Job, error)
}

var _ docsCLIService = (*documents.Service)(nil)

type docsServiceFactory func(context.Context) (docsCLIService, func(), error)

func runDocs(args []string) {
	ctx, err := withOperatorIdentity(context.Background())
	if err == nil {
		err = runDocsCommand(ctx, args, os.Stdout, newDocsService)
	}
	if err != nil {
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
	query, documentID, limit, err := parseDocsSearchArgs(args)
	if err != nil {
		return err
	}
	svc, closeFn, err := factory(ctx)
	if err != nil {
		return err
	}
	defer closeFn()

	start := time.Now()
	hits, err := svc.Retrieve(ctx, docsSearchRequest(ctx, query, documentID, limit))
	if err != nil {
		return err
	}
	return writeJSON(out, map[string]any{
		"query":        query,
		"hits":         hits,
		"retrieval_ms": time.Since(start).Milliseconds(),
	})
}

func parseDocsSearchArgs(args []string) (query, documentID string, limit int, err error) {
	limit = 8
	var queryParts []string
	for i := 0; i < len(args); i++ {
		name, inlineValue, hasInlineValue := strings.Cut(args[i], "=")
		switch name {
		case "--document-id", "--limit":
			value := inlineValue
			if !hasInlineValue {
				i++
				if i >= len(args) || strings.HasPrefix(args[i], "--") {
					return "", "", 0, fmt.Errorf("%s requires a value", name)
				}
				value = args[i]
			}
			if strings.TrimSpace(value) == "" {
				return "", "", 0, fmt.Errorf("%s requires a value", name)
			}
			if name == "--document-id" {
				documentID = value
				continue
			}
			parsed, parseErr := strconv.Atoi(value)
			if parseErr != nil || parsed <= 0 {
				return "", "", 0, fmt.Errorf("--limit requires a positive integer, got %q", value)
			}
			limit = parsed
		default:
			if strings.HasPrefix(name, "-") {
				return "", "", 0, fmt.Errorf("unknown flag %q", name)
			}
			queryParts = append(queryParts, args[i])
		}
	}
	query = strings.Join(queryParts, " ")
	if strings.TrimSpace(query) == "" {
		return "", "", 0, fmt.Errorf("docs search requires <query>")
	}
	return query, documentID, limit, nil
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
	var hits []documents.SearchHit
	for range 5 {
		start := time.Now()
		if hits, err = svc.Retrieve(ctx, docsSearchRequest(ctx, *query, job.DocumentID, 8)); err != nil {
			return err
		}
		latencies = append(latencies, time.Since(start))
	}
	p95 := percentile(latencies, 0.95)
	return writeJSON(out, map[string]any{
		"file":                  job.FileName,
		"size_bytes":            job.SizeBytes,
		"chunks":                job.SparseChunks,
		"hits":                  len(hits),
		"time_to_searchable_ms": timeToSearchable.Milliseconds(),
		"retrieval_p95_ms":      p95.Milliseconds(),
		"industrial_score":      industrialScore(timeToSearchable, p95, job.SparseChunks, len(hits)),
	})
}

// docsSearchRequest builds a CLI retrieval request. The identity is load-bearing, not
// bookkeeping: with AURA_MUSR_ISOLATION on, Searcher.Search and every scoped seed query
// fail closed on an empty principal and return before any I/O — which is why `aura docs
// search` used to print zero hits in zero milliseconds and exit 0, and why the benched
// retrieval p95 was the p95 of a short circuit. runDocs resolves the operator once onto
// the context, the way the document_search tool reads it from identityctx.
func docsSearchRequest(ctx context.Context, query, documentID string, limit int) documents.SearchRequest {
	return documents.SearchRequest{
		Query:      query,
		DocumentID: documentID,
		Limit:      limit,
		IdentityID: identityctx.IdentityID(ctx),
	}
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
	svc := newDocumentCLIService(documentServiceDeps{
		cfg:       cfg,
		pool:      pool,
		graph:     mcp,
		maxBytes:  documentMaxBytes(cfg),
		revisions: defaultRetrievalRevisions(),
	})
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
	return &runtimeDocumentIngestor{cfg: cfg, pool: pool, MaxBytes: documentMaxBytes(cfg)}
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
	svc := newDocumentIngestService(documentServiceDeps{
		cfg: i.cfg, pool: i.pool, graph: mcp, maxBytes: i.MaxBytes,
	})
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
	cfg                *config.Config
	retrievalRevisions *documents.RetrievalRevisions
}

// Retrieve runs the two-stage retrieval pipeline for the document_search tool. It
// seeds from the dense chunk_embedding index (embed sidecar) or the sparse fulltext
// index, reranks the seeds via the optional aura-rerank sidecar, and 1-hop expands the
// winners. Every stage is fail-soft: a down embed/rerank sidecar degrades to the
// RRF/vector seed order, so retrieval never blocks on optional infrastructure.
func (s docsToolSearcher) Retrieve(ctx context.Context, req documents.SearchRequest) ([]documents.SearchHit, error) {
	service, closeFn, err := s.openRetrievalService(ctx)
	if err != nil {
		return nil, err
	}
	defer closeFn()
	return service.Retrieve(ctx, req)
}

func (s docsToolSearcher) FreezeRetrievalPlans(
	req documents.SearchRequest,
) (documents.FrozenRetrievalPlans, error) {
	if s.cfg == nil {
		return documents.FrozenRetrievalPlans{},
			fmt.Errorf("document search config is nil")
	}
	health := configuredRetrievalHealth(s.cfg)
	service := &documents.Service{
		RetrievalRevisions: s.frozenRetrievalRevisions(),
		RetrievalHealth:    &health,
		MUSRIsolation:      s.cfg.MUSRIsolation,
	}
	return service.FreezeRetrievalPlans(req)
}

func (s docsToolSearcher) ExecuteRetrievalPlan(
	ctx context.Context,
	frozen documents.FrozenRetrievalPlans,
	planID string,
	req documents.SearchRequest,
) (documents.RetrievalResult, error) {
	service, closeFn, err := s.openRetrievalService(ctx)
	if err != nil {
		return documents.RetrievalResult{}, err
	}
	defer closeFn()
	return service.ExecuteRetrievalPlan(ctx, frozen, planID, req)
}

func (s docsToolSearcher) openRetrievalService(
	ctx context.Context,
) (*documents.Service, func(), error) {
	if s.cfg == nil {
		return nil, nil, fmt.Errorf("document search config is nil")
	}
	mcp, err := knowledge.Open(ctx, &s.cfg.Neo4j)
	if err != nil {
		return nil, nil, err
	}
	svc := newDocumentRetrievalService(documentServiceDeps{
		cfg: s.cfg, graph: mcp, revisions: s.frozenRetrievalRevisions(),
	})
	return svc, func() { _ = mcp.Close() }, nil
}

func (s docsToolSearcher) frozenRetrievalRevisions() documents.RetrievalRevisions {
	if s.retrievalRevisions != nil {
		return *s.retrievalRevisions
	}
	return defaultRetrievalRevisions()
}

func configuredRetrievalHealth(
	cfg *config.Config,
) documents.RetrievalDependencyHealth {
	if cfg == nil {
		return documents.RetrievalDependencyHealth{}
	}
	rerankBase, _, _ := cfg.RerankRoute()
	return documents.RetrievalDependencyHealth{
		Sparse: cfg.Neo4j.BoltURL != "",
		Vector: cfg.Neo4j.BoltURL != "" && cfg.Neo4j.EmbedURL != "",
		Reranker: cfg.Neo4j.BoltURL != "" && cfg.Neo4j.EmbedURL != "" &&
			rerankBase != "",
		Expand: cfg.Neo4j.BoltURL != "",
	}
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
	slices.Sort(cp)
	idx := int(float64(len(cp)-1) * p)
	return cp[idx]
}

// industrialScore grades one ingest + retrieval round trip. The hits term is not
// cosmetic: without it the score printed 74 for a pipeline that returned nothing at all,
// because a retrieval short-circuiting before any I/O looks exactly like a fast one. The
// weights mirror liveIndustrialScore in internal/documents/document_ingest_live_test.go.
func industrialScore(searchable, retrievalP95 time.Duration, chunks, hits int) float64 {
	score := 100.0
	if chunks == 0 {
		score -= 40
	}
	if hits == 0 {
		score -= 25
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
