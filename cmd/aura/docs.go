package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/jackc/pgx/v5/pgxpool"
)

const docsUsage = "usage: aura docs {ingest <path> [--source-id cli] [--source-kind local]|search <query> [--document-id id] [--limit 8]|status <job-id>|list [--limit 20]}"

// docsCLIService is the surface `aura docs` drives. Search returns DOCUMENTS, not
// passages: it is the same digest ranking the document_search tool reads, so the CLI and
// the agent cannot disagree about what the library contains.
type docsCLIService interface {
	IngestPath(ctx context.Context, req documents.IngestRequest, path string) (*documents.Job, error)
	SearchDigests(ctx context.Context, identityID, query string, limit int) ([]documents.DigestHit, error)
	GetJob(ctx context.Context, id string) (*documents.Job, error)
	ListJobs(ctx context.Context, limit int) ([]documents.Job, error)
}

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
	job, err := svc.IngestPath(ctx, documents.IngestRequest{
		SourceID: *sourceID, SourceKind: *sourceKind, IdentityID: identityctx.IdentityID(ctx),
	}, fs.Arg(0))
	if err != nil {
		return err
	}
	return writeJSON(out, map[string]any{
		"job_id":      job.ID,
		"document_id": job.DocumentID,
		"status":      job.Status,
		"file_name":   job.FileName,
		"ingest_ms":   time.Since(start).Milliseconds(),
	})
}

// docsSearch ranks the operator's library. The identity is load-bearing, not bookkeeping:
// the digest query is identity-scoped in SQL, so an unresolved principal returns nothing.
// runDocs resolves the operator once onto the context, the way the document_search tool
// reads it from identityctx.
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
	hits, err := svc.SearchDigests(ctx, identityctx.IdentityID(ctx), query, limit)
	if err != nil {
		return err
	}
	hits = filterDigestHits(hits, documentID)
	return writeJSON(out, map[string]any{
		"query":        query,
		"hits":         hits,
		"retrieval_ms": time.Since(start).Milliseconds(),
	})
}

// filterDigestHits keeps --document-id meaning what it always meant: scope the answer to
// one document. A hit set is at most a few dozen rows, so this is a filter rather than a
// second query.
func filterDigestHits(hits []documents.DigestHit, documentID string) []documents.DigestHit {
	if strings.TrimSpace(documentID) == "" {
		return hits
	}
	scoped := make([]documents.DigestHit, 0, 1)
	for _, hit := range hits {
		if hit.DocumentID == documentID {
			scoped = append(scoped, hit)
		}
	}
	return scoped
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

// docsCLI joins the two halves `aura docs` needs: the ingest service that writes the
// catalog row, and the digest ranking that reads it back.
type docsCLI struct {
	*documents.Service
	library *documentLibrary
}

func (c docsCLI) SearchDigests(
	ctx context.Context,
	identityID, query string,
	limit int,
) ([]documents.DigestHit, error) {
	return c.library.SearchDigests(ctx, identityID, query, limit)
}

func newDocsService(ctx context.Context) (docsCLIService, func(), error) {
	cfg := config.LoadDB()
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		return nil, nil, err
	}
	svc := docsCLI{
		Service: newDocumentIngestService(documentServiceDeps{
			cfg:      cfg,
			pool:     pool,
			maxBytes: documentMaxBytes(cfg),
		}),
		library: newDocumentLibrary(pool),
	}
	return svc, pool.Close, nil
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
	svc := newDocumentIngestService(documentServiceDeps{
		cfg: i.cfg, pool: i.pool, maxBytes: i.MaxBytes,
	})
	return svc.IngestPath(ctx, req, path)
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
