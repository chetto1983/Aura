package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/jackc/pgx/v5/pgxpool"
)

const docsUsage = "usage: aura docs {ingest <path> [--source-id id] [--source-kind cli]|search <query> [--document-id id] [--limit 8]|status <job-id>|list [--limit 20]}"

// docsCLIService is the surface `aura docs` drives. Search uses the same host
// retriever as the agent tool and HTTP API, including passage evidence and degradation.
type docsCLIService interface {
	IngestDocumentPath(ctx context.Context, req assets.DocumentIngestRequest, path string) (assets.Asset, error)
	Retrieve(ctx context.Context, request documents.RetrievalRequest) (documents.RetrievalResponse, error)
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
	sourceID := fs.String("source-id", "", "stable source id")
	sourceKind := fs.String("source-kind", string(assets.SourceCLI), "source kind")
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
	kind := assets.SourceKind(strings.TrimSpace(*sourceKind))
	if kind != assets.SourceCLI {
		return fmt.Errorf("docs ingest --source-kind must be %q", assets.SourceCLI)
	}
	asset, err := svc.IngestDocumentPath(ctx, assets.DocumentIngestRequest{
		IdentityID: identityctx.IdentityID(ctx),
		SourceKind: kind,
		SourceRef:  strings.TrimSpace(*sourceID),
	}, fs.Arg(0))
	if err != nil {
		return err
	}
	return writeJSON(out, map[string]any{
		"asset_id":  asset.ID,
		"status":    asset.Status,
		"file_name": asset.FileName,
		"ingest_ms": time.Since(start).Milliseconds(),
	})
}

// docsSearch retrieves from the operator's library. The identity is load-bearing,
// not bookkeeping: every control-plane query and ArcadeDB candidate filter is scoped
// to it. runDocs resolves the operator once onto the context.
func docsSearch(ctx context.Context, args []string, out io.Writer, factory docsServiceFactory) error {
	query, documentIDs, limit, err := parseDocsSearchArgs(args)
	if err != nil {
		return err
	}
	svc, closeFn, err := factory(ctx)
	if err != nil {
		return err
	}
	defer closeFn()

	start := time.Now()
	response, err := svc.Retrieve(ctx, documents.RetrievalRequest{
		IdentityID: identityctx.IdentityID(ctx), Query: query,
		Limit: limit, DocumentIDs: documentIDs,
	})
	if err != nil {
		return err
	}
	return writeJSON(out, map[string]any{
		"query":               response.Query,
		"profile":             response.Profile,
		"status":              response.Status,
		"degradation_reason":  response.DegradationReason,
		"rejected_candidates": response.RejectedCandidates,
		"documents":           response.Documents,
		"retrieval_ms":        time.Since(start).Milliseconds(),
	})
}

func parseDocsSearchArgs(args []string) (query string, documentIDs []string, limit int, err error) {
	limit = 8
	var queryParts []string
	remaining := args
	for len(remaining) > 0 {
		arg := remaining[0]
		remaining = remaining[1:]
		name, inlineValue, hasInlineValue := strings.Cut(arg, "=")
		switch name {
		case "--document-id", "--limit":
			value := inlineValue
			if !hasInlineValue {
				if len(remaining) == 0 {
					return "", nil, 0, fmt.Errorf("%s requires a value", name)
				}
				value = remaining[0]
				remaining = remaining[1:]
				if strings.HasPrefix(value, "--") {
					return "", nil, 0, fmt.Errorf("%s requires a value", name)
				}
			}
			if strings.TrimSpace(value) == "" {
				return "", nil, 0, fmt.Errorf("%s requires a value", name)
			}
			if name == "--document-id" {
				documentIDs = append(documentIDs, value)
				continue
			}
			parsed, parseErr := strconv.ParseInt(value, 10, 32)
			if parseErr != nil || parsed <= 0 {
				return "", nil, 0, fmt.Errorf("--limit requires a positive integer, got %q", value)
			}
			limit = int(parsed)
		default:
			if strings.HasPrefix(name, "-") {
				return "", nil, 0, fmt.Errorf("unknown flag %q", name)
			}
			queryParts = append(queryParts, arg)
		}
	}
	query = strings.TrimSpace(strings.Join(queryParts, " "))
	if query == "" {
		return "", nil, 0, documents.ErrEmptyDocumentQuery
	}
	return query, documentIDs, limit, nil
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

// docsCLI joins the ingest service and the shared document retriever used by the
// other host surfaces.
type docsCLI struct {
	*documents.Service
	library  *documentLibrary
	ingestor *runtimeDocumentIngestor
}

func (c docsCLI) IngestDocumentPath(
	ctx context.Context,
	req assets.DocumentIngestRequest,
	path string,
) (assets.Asset, error) {
	if c.ingestor == nil {
		return assets.Asset{}, fmt.Errorf("document ingestor is not configured")
	}
	return c.ingestor.IngestDocumentPath(ctx, req, path)
}

func (c docsCLI) Retrieve(
	ctx context.Context,
	request documents.RetrievalRequest,
) (documents.RetrievalResponse, error) {
	return c.library.Retrieve(ctx, request)
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
		library:  newDocumentLibrary(pool, cfg),
		ingestor: newRuntimeDocumentIngestor(cfg, pool),
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

// IngestDocument persists a non-presigned document in the owner's object store
// before enqueueing it. It is the canonical ingress used by document_index; the
// legacy IngestPath method remains internal to the queued processor until the
// Docling stage replaces it.
func (i *runtimeDocumentIngestor) IngestDocument(ctx context.Context, req assets.DocumentIngestRequest) (assets.Asset, error) {
	if i == nil || i.cfg == nil || i.pool == nil {
		return assets.Asset{}, fmt.Errorf("document ingestor is not configured")
	}
	objects, err := buildObjectStore(ctx, i.cfg)
	if err != nil {
		return assets.Asset{}, err
	}
	return buildAssetService(i.cfg, i.pool, objects).IngestDocument(ctx, req)
}

func (i *runtimeDocumentIngestor) IngestDocumentPath(
	ctx context.Context,
	req assets.DocumentIngestRequest,
	rawPath string,
) (assets.Asset, error) {
	if i == nil || i.cfg == nil || i.pool == nil {
		return assets.Asset{}, fmt.Errorf("document ingestor is not configured")
	}
	resolved, relative, err := documentPathWithinRoot(i.cfg.WorkspaceDir, rawPath)
	if err != nil {
		return assets.Asset{}, err
	}
	// #nosec G304 -- resolved is not caller-controlled by the time it reaches here:
	// documentPathWithinRoot resolves symlinks on BOTH the workspace root and the target
	// before comparing them, then rejects any path whose relative form escapes the root.
	// Resolving before the comparison is what defeats a symlink pointing outside.
	f, err := os.Open(resolved)
	if err != nil {
		return assets.Asset{}, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return assets.Asset{}, err
	}
	if info.IsDir() {
		return assets.Asset{}, fmt.Errorf("document path %q is a directory", rawPath)
	}
	locator := relative
	if req.SourceRef != "" {
		locator = "explicit:" + req.SourceRef
	}
	refHash := sha256.Sum256([]byte(locator))
	req.SourceRef = fmt.Sprintf("sha256:%x", refHash)
	req.FileName = filepath.Base(resolved)
	req.SizeBytes = info.Size()
	req.Reader = f
	return i.IngestDocument(ctx, req)
}

func documentPathWithinRoot(root, rawPath string) (resolved, relative string, err error) {
	if strings.TrimSpace(root) == "" {
		return "", "", fmt.Errorf("document workspace root is not configured")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("resolve document workspace root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", fmt.Errorf("resolve document workspace root: %w", err)
	}
	resolved = rawPath
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(root, resolved)
	}
	resolved, err = filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", "", fmt.Errorf("resolve document path: %w", err)
	}
	relative, err = filepath.Rel(root, resolved)
	if err != nil {
		return "", "", fmt.Errorf("relativize document path: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", "", fmt.Errorf("document path %q is outside workspace root", rawPath)
	}
	return resolved, filepath.ToSlash(relative), nil
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
