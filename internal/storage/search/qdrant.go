package search

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/storage/qdrant"
	"github.com/aura/aura/internal/stringx"
)

// QdrantConfig describes Aura's external vector index.
type QdrantConfig struct {
	BaseURL    string
	Collection string
	APIKey     string
	BatchSize  int
	Client     *http.Client
}

// PagesIndexedUnknown is the sentinel value for QdrantRebuildReport.PagesIndexed
// when the on-disk page count could not be determined (e.g. loadWikiDocuments
// failed during a warm-cache hit). Consumers should treat this as "unavailable",
// not as "zero pages on disk" (WR-05).
const PagesIndexedUnknown = -1

type QdrantRebuildReport struct {
	Collection  string `json:"collection"`
	DocsIndexed int    `json:"docs_indexed"`
	// PagesIndexed is the number of wiki pages enumerated on disk during the
	// rebuild. The value PagesIndexedUnknown (-1) means the disk enumeration
	// failed during a warm-cache hit; callers should not interpret it as zero.
	PagesIndexed int `json:"pages_indexed"`
	VectorSize   int `json:"vector_size"`
}

type qdrantSearcher struct {
	client     qdrant.Client
	embedFn    EmbeddingFunction
	collection string
}

type qdrantRepository struct {
	primary          *qdrantSearcher
	client           qdrant.Client
	embedFn          EmbeddingFunction
	collectionQdrant string
	wikiDir          string
	logger           *slog.Logger
	indexed          bool
	mu               sync.RWMutex
	db               *sql.DB // optional; enables SearchHybrid when non-nil
}

// NewQdrantSearcher creates a read-only Qdrant-backed semantic searcher.
// It uses Qdrant's Query Points endpoint with Aura-owned embeddings and
// payloads created by RebuildQdrantWikiDocuments.
func NewQdrantSearcher(cfg QdrantConfig, embedFn EmbeddingFunction) (*qdrantSearcher, error) {
	if embedFn == nil {
		return nil, fmt.Errorf("embedding function is required")
	}
	client, collection, err := newQdrantClientFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &qdrantSearcher{client: client, embedFn: embedFn, collection: collection}, nil
}

// NewQdrantRepository creates the runtime wiki search repository backed by Qdrant.
func NewQdrantRepository(cfg QdrantConfig, embedFn EmbeddingFunction, wikiDir string, logger *slog.Logger) (Repository, error) {
	if logger == nil {
		logger = slog.Default()
	}
	primary, err := NewQdrantSearcher(cfg, embedFn)
	if err != nil {
		return nil, err
	}
	return &qdrantRepository{
		primary:          primary,
		client:           primary.client,
		embedFn:          embedFn,
		collectionQdrant: primary.collection,
		wikiDir:          wikiDir,
		logger:           logger,
	}, nil
}

// NewQdrantRepositoryWithDB creates the wiki search repository with SQLite
// access so SearchHybrid can run the 3-channel RRF path (exact + FTS + vector).
// Pass the main Aura DB pool; the wiki FTS5 mirror (wiki_documents) lives there.
func NewQdrantRepositoryWithDB(cfg QdrantConfig, embedFn EmbeddingFunction, wikiDir string, db *sql.DB, logger *slog.Logger) (HybridRepository, error) {
	if logger == nil {
		logger = slog.Default()
	}
	primary, err := NewQdrantSearcher(cfg, embedFn)
	if err != nil {
		return nil, err
	}
	return &qdrantRepository{
		primary:          primary,
		client:           primary.client,
		embedFn:          embedFn,
		collectionQdrant: primary.collection,
		wikiDir:          wikiDir,
		logger:           logger,
		db:               db,
	}, nil
}

func (r *qdrantRepository) Search(ctx context.Context, query string, topK int) ([]Result, error) {
	return r.primary.Search(ctx, query, topK)
}

// SearchHybrid fuses three signal channels via RRF:
//   - exact: title substring match with diacritic-strip (boolean, 1.0 hit)
//   - fts:   FTS5 BM25 from the wiki_documents SQLite mirror
//   - vector: cosine similarity from Qdrant
//
// Falls back to vector-only when db is nil (built via NewQdrantRepository).
func (r *qdrantRepository) SearchHybrid(ctx context.Context, query string, topK int) ([]Result, error) {
	if r.db == nil {
		return r.primary.Search(ctx, query, topK)
	}
	if topK <= 0 {
		topK = 5
	}
	fetch := topK * 2

	var (
		vectorResults []Result
		ftsResults    []Result
		exactResults  []Result
		vectorErr     error
		ftsErr        error
	)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		vectorResults, vectorErr = r.primary.Search(ctx, query, fetch)
	}()
	go func() {
		defer wg.Done()
		ftsResults, ftsErr = ftsSearchDB(ctx, r.db, query, fetch, r.logger)
	}()
	go func() {
		defer wg.Done()
		exactResults = exactMatchDB(ctx, r.db, query, fetch, r.logger)
	}()
	wg.Wait()

	if vectorErr != nil {
		return nil, vectorErr
	}
	if ftsErr != nil {
		r.logger.Warn("hybrid wiki search: FTS5 failed; using vector-only", "error", ftsErr)
		return vectorResults, nil
	}
	return mergeHybridResults(query, topK, exactResults, ftsResults, vectorResults), nil
}

// ftsSearchDB queries the wiki_documents FTS5 mirror using BM25. Re-uses the
// escapeFTS5Query tokeniser already used by sqliteSearcher.
func ftsSearchDB(ctx context.Context, db *sql.DB, query string, topK int, logger *slog.Logger) ([]Result, error) {
	safeQuery := escapeFTS5Query(query)
	if safeQuery == "" {
		return nil, nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, content, metadata, title, rank
		FROM wiki_documents
		WHERE wiki_documents MATCH ?
		ORDER BY rank
		LIMIT ?
	`, safeQuery, topK)
	if err != nil {
		return nil, fmt.Errorf("hybrid FTS5 search: %w", err)
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var id, content, metaJSON, title string
		var rank float64
		if err := rows.Scan(&id, &content, &metaJSON, &title, &rank); err != nil {
			if logger != nil {
				logger.Warn("hybrid: scanning FTS5 result", "error", err)
			}
			continue
		}
		slug := extractMetaField(metaJSON, "slug")
		if slug == "" {
			slug = id
		}
		kind := extractMetaField(metaJSON, "kind")
		if kind == "" {
			kind = "wiki_page"
		}
		updatedAt, _ := parseSearchPayloadTime(extractMetaField(metaJSON, "updated_at"))
		createdAt, _ := parseSearchPayloadTime(extractMetaField(metaJSON, "created_at"))
		schemaVersion, _ := strconv.Atoi(strings.TrimSpace(extractMetaField(metaJSON, "schema_version")))
		size, _ := strconv.ParseInt(extractMetaField(metaJSON, "size"), 10, 64)
		results = append(results, Result{
			Kind:          kind,
			Slug:          slug,
			Title:         title,
			Content:       content,
			Score:         float32(-rank),
			UpdatedAt:     updatedAt,
			CreatedAt:     createdAt,
			SchemaVersion: schemaVersion,
			PromptVersion: extractMetaField(metaJSON, "prompt_version"),
			Unversioned:   parseSearchPayloadBool(extractMetaField(metaJSON, "unversioned")),
			FilePath:      extractMetaField(metaJSON, "filepath"),
			Category:      extractMetaField(metaJSON, "category"),
			Tags:          splitCSVPayloadField(extractMetaField(metaJSON, "tags")),
			Related:       splitCSVPayloadField(extractMetaField(metaJSON, "related")),
			Sources:       splitCSVPayloadField(extractMetaField(metaJSON, "sources")),
			SizeBytes:     size,
		})
	}
	return results, rows.Err()
}

// exactMatchDB returns pages from wiki_documents whose title contains any
// diacritic-stripped query token (case-insensitive substring match).
// ScoreExact is 1.0; missing pages get no entry (not score 0.0).
func exactMatchDB(ctx context.Context, db *sql.DB, query string, topK int, logger *slog.Logger) []Result {
	queryStripped := stringx.StripDiacritics(strings.ToLower(query))
	tokens := strings.Fields(queryStripped)
	if len(tokens) == 0 {
		return nil
	}

	// Fetch all page rows; ~60 pages, cheap.
	rows, err := db.QueryContext(ctx, `
		SELECT id, content, metadata, title
		FROM wiki_documents
	`)
	if err != nil {
		if logger != nil {
			logger.Warn("hybrid: exact match fetch failed", "error", err)
		}
		return nil
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var id, content, metaJSON, title string
		if err := rows.Scan(&id, &content, &metaJSON, &title); err != nil {
			continue
		}
		titleStripped := stringx.StripDiacritics(strings.ToLower(title))
		hit := false
		for _, tok := range tokens {
			if strings.Contains(titleStripped, tok) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		slug := extractMetaField(metaJSON, "slug")
		if slug == "" {
			slug = id
		}
		kind := extractMetaField(metaJSON, "kind")
		if kind == "" {
			kind = "wiki_page"
		}
		updatedAt, _ := parseSearchPayloadTime(extractMetaField(metaJSON, "updated_at"))
		createdAt, _ := parseSearchPayloadTime(extractMetaField(metaJSON, "created_at"))
		schemaVersion, _ := strconv.Atoi(strings.TrimSpace(extractMetaField(metaJSON, "schema_version")))
		size, _ := strconv.ParseInt(extractMetaField(metaJSON, "size"), 10, 64)
		results = append(results, Result{
			Kind:          kind,
			Slug:          slug,
			Title:         title,
			Content:       content,
			Score:         1.0,
			ScoreExact:    1.0,
			UpdatedAt:     updatedAt,
			CreatedAt:     createdAt,
			SchemaVersion: schemaVersion,
			PromptVersion: extractMetaField(metaJSON, "prompt_version"),
			Unversioned:   parseSearchPayloadBool(extractMetaField(metaJSON, "unversioned")),
			FilePath:      extractMetaField(metaJSON, "filepath"),
			Category:      extractMetaField(metaJSON, "category"),
			Tags:          splitCSVPayloadField(extractMetaField(metaJSON, "tags")),
			Related:       splitCSVPayloadField(extractMetaField(metaJSON, "related")),
			Sources:       splitCSVPayloadField(extractMetaField(metaJSON, "sources")),
			SizeBytes:     size,
		})
		if len(results) >= topK {
			break
		}
	}
	return results
}

func (r *qdrantRepository) IsIndexed() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.indexed
}

func (r *qdrantRepository) Index(ctx context.Context, id string, content string, metadata map[string]string) error {
	vector, err := r.embedFn(ctx, content)
	if err != nil {
		return fmt.Errorf("embedding %s: %w", id, err)
	}
	if len(vector) == 0 {
		return fmt.Errorf("embedding %s returned empty vector", id)
	}
	// WR-07: skip CreateCollection if the collection has already been
	// initialized in this process (either by a previous Index call or by
	// IndexWikiPages). Qdrant's CreateCollection is idempotent at the HTTP
	// level (200 if collection exists with same params), so the previous
	// behavior was correct but added a network round-trip per single-doc
	// upsert. The r.indexed flag persists for the process lifetime; on a
	// fresh process we still hit the create path on the first Index call.
	r.mu.RLock()
	alreadyIndexed := r.indexed
	r.mu.RUnlock()
	if !alreadyIndexed {
		if err := r.client.CreateCollection(ctx, r.collectionQdrant, len(vector)); err != nil {
			// WR-07: surface a clearer hint when CreateCollection fails with
			// a dimension mismatch (most likely cause: operator swapped
			// EMBEDDING_MODEL without rebuilding the collection — see T-01-24).
			lower := strings.ToLower(err.Error())
			if strings.Contains(lower, "dim") || strings.Contains(lower, "vector size") {
				r.logger.Warn("qdrant CreateCollection failed with dimension hint; embedding model may have changed since last rebuild",
					"collection", r.collectionQdrant, "new_vector_size", len(vector), "error", err)
			}
			return err
		}
		r.mu.Lock()
		r.indexed = true
		r.mu.Unlock()
	}
	payload := map[string]string{
		"doc_id":  id,
		"content": content,
	}
	for key, value := range metadata {
		payload[key] = value
	}
	if err := r.client.Upsert(ctx, r.collectionQdrant, []qdrant.Point{{
		ID:      qdrantPointID(id),
		Vector:  vector,
		Payload: payload,
	}}); err != nil {
		return err
	}
	return nil
}

func (r *qdrantRepository) IndexWikiPages(ctx context.Context) error {
	_, err := rebuildQdrantWikiDocumentsWithClient(ctx, r.wikiDir, r.embedFn, r.client, r.collectionQdrant, r.logger)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.indexed = true
	r.mu.Unlock()
	return nil
}

func (r *qdrantRepository) ReindexWikiPage(ctx context.Context, slug string) error {
	slug = strings.TrimSpace(slug)
	doc, found, err := loadWikiPageDocument(r.wikiDir, slug, r.logger)
	if err != nil {
		return err
	}
	if !found {
		return r.client.Delete(ctx, r.collectionQdrant, []string{
			qdrantPointID(slug),
			qdrantPointID("graph:node:" + slug),
		})
	}
	return r.Index(ctx, doc.ID, doc.Content, doc.Metadata)
}

// RebuildQdrantWikiDocuments recreates the configured collection from Aura's
// wiki pages and graph cards.
func RebuildQdrantWikiDocuments(ctx context.Context, wikiDir string, embedFn EmbeddingFunc, cfg QdrantConfig, logger *slog.Logger) (QdrantRebuildReport, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if embedFn == nil {
		return QdrantRebuildReport{}, fmt.Errorf("embedding function is required")
	}
	client, collection, err := newQdrantClientFromConfig(cfg)
	if err != nil {
		return QdrantRebuildReport{}, err
	}
	return rebuildQdrantWikiDocumentsWithClient(ctx, wikiDir, embedFn, client, collection, logger)
}

// rebuildQdrantWikiDocumentsWithClient implements the rebuild logic using an
// already-constructed qdrant.Client and collection name. This avoids needing to
// reconstruct credentials from the client interface.
func rebuildQdrantWikiDocumentsWithClient(ctx context.Context, wikiDir string, embedFn EmbeddingFunc, client qdrant.Client, collection string, logger *slog.Logger) (QdrantRebuildReport, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if embedFn == nil {
		return QdrantRebuildReport{}, fmt.Errorf("embedding function is required")
	}
	if err := client.Health(ctx); err != nil {
		return QdrantRebuildReport{}, err
	}

	// QDRANT-01 warm-cache short-circuit: if the collection already exists with
	// points, skip the rebuild and reuse the cached vectors.
	//
	// WR-03 / T-01-24 (accepted): CollectionInfo does not expose the existing
	// collection's vector size or distance metric, so a warm-cache hit cannot
	// detect schema drift from an EMBEDDING_MODEL swap (e.g. 1024 → 1536 dim).
	// Subsequent Search calls will surface the dimension mismatch as a clear
	// Qdrant error. Operator runbook: DELETE the collection (or set fresh
	// QDRANT_COLLECTION) before changing embedding models. Phase 2 hardening
	// candidate: extend CollectionInfo with vector_size and assert here.
	info, infoErr := client.CollectionInfo(ctx, collection)
	if infoErr != nil {
		// Defensive fallback: a transient probe failure must not block startup.
		// Continue with the full rebuild path -- correctness > performance.
		logger.Warn("qdrant collection info probe failed; proceeding with full rebuild", "collection", collection, "error", infoErr)
	} else if info.PointsCount > 0 {
		// Still load pages so PagesIndexed is meaningful in the report.
		// W2: surface the loadWikiDocuments error at warn level instead of swallowing it.
		_, pages, loadErr := loadWikiDocuments(wikiDir, logger)
		// WR-05: distinguish "load failed → count unknown" from "load succeeded →
		// 0 pages on disk". Use PagesIndexedUnknown (-1) as the sentinel so
		// downstream consumers (e.g. /api/health, debug_qdrant) do not silently
		// report 0 pages when the enumeration failed.
		pagesIndexed := pages
		if loadErr != nil {
			logger.Warn("warm-cache hit: pages_on_disk count unavailable", "error", loadErr, "collection", collection)
			pagesIndexed = PagesIndexedUnknown
		}
		// WR-01: saturate uint64 → int conversion. On 32-bit platforms the
		// naked int(info.PointsCount) would wrap for values above MaxInt32.
		// Aura is unlikely to ever index >2 billion points but the guard
		// keeps the report sane on every architecture.
		docsIndexed := saturateUint64ToInt(info.PointsCount)
		if loadErr != nil {
			logger.Info("qdrant warm-cache hit (pages_on_disk unavailable)", "collection", collection, "points_count", info.PointsCount)
		} else {
			logger.Info("qdrant warm-cache hit, skipping rebuild", "collection", collection, "points_count", info.PointsCount, "pages_on_disk", pages)
		}
		return QdrantRebuildReport{
			Collection:   collection,
			PagesIndexed: pagesIndexed,
			DocsIndexed:  docsIndexed, // W1: live points count, not 0
			VectorSize:   0,
		}, nil
	}

	docs, pages, err := loadWikiDocuments(wikiDir, logger)
	if err != nil {
		return QdrantRebuildReport{}, err
	}
	if len(docs) == 0 {
		return QdrantRebuildReport{Collection: collection, PagesIndexed: pages}, nil
	}

	points := make([]qdrant.Point, 0, len(docs))
	vectorSize := 0
	for _, doc := range docs {
		vector, err := embedFn(ctx, doc.Content)
		if err != nil {
			return QdrantRebuildReport{}, fmt.Errorf("embedding %s: %w", doc.ID, err)
		}
		if len(vector) == 0 {
			return QdrantRebuildReport{}, fmt.Errorf("embedding %s returned empty vector", doc.ID)
		}
		if vectorSize == 0 {
			vectorSize = len(vector)
		} else if len(vector) != vectorSize {
			return QdrantRebuildReport{}, fmt.Errorf("embedding %s returned vector size %d, want %d", doc.ID, len(vector), vectorSize)
		}
		payload := map[string]string{
			"doc_id":  doc.ID,
			"content": doc.Content,
		}
		for key, value := range doc.Metadata {
			payload[key] = value
		}
		points = append(points, qdrant.Point{
			ID:      qdrantPointID(doc.ID),
			Vector:  vector,
			Payload: payload,
		})
	}

	if err := client.DeleteCollection(ctx, collection); err != nil {
		return QdrantRebuildReport{}, err
	}
	if err := client.CreateCollection(ctx, collection, vectorSize); err != nil {
		return QdrantRebuildReport{}, err
	}
	if err := client.Upsert(ctx, collection, points); err != nil {
		return QdrantRebuildReport{}, err
	}
	return QdrantRebuildReport{
		Collection:   collection,
		DocsIndexed:  len(points),
		PagesIndexed: pages,
		VectorSize:   vectorSize,
	}, nil
}

// newQdrantClientFromConfig creates a qdrant.Client from QdrantConfig and returns
// the client, collection name, and any construction error.
func newQdrantClientFromConfig(cfg QdrantConfig) (qdrant.Client, string, error) {
	base := stringx.NormalizeBaseURL(cfg.BaseURL)
	if base == "" {
		return nil, "", fmt.Errorf("QDRANT_URL is required")
	}
	if _, err := url.ParseRequestURI(base); err != nil {
		return nil, "", fmt.Errorf("invalid QDRANT_URL: %w", err)
	}
	collection := strings.TrimSpace(cfg.Collection)
	if collection == "" {
		return nil, "", fmt.Errorf("QDRANT_COLLECTION is required")
	}
	timeout := 30 * time.Second
	if cfg.Client != nil && cfg.Client.Timeout > 0 {
		timeout = cfg.Client.Timeout
	}
	client, err := qdrant.NewClient(qdrant.Config{
		BaseURL: base,
		APIKey:  cfg.APIKey,
		Timeout: timeout,
	})
	if err != nil {
		return nil, "", err
	}
	return client, collection, nil
}

func (s *qdrantSearcher) Search(ctx context.Context, query string, topK int) ([]Result, error) {
	if topK <= 0 {
		topK = 5
	}
	vector, err := s.embedFn(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embedding qdrant query: %w", err)
	}
	if len(vector) == 0 {
		return nil, fmt.Errorf("embedding qdrant query returned empty vector")
	}
	points, err := s.client.Search(ctx, s.collection, vector, topK, true)
	if err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(points))
	for _, point := range points {
		payload := point.Payload
		kind := strings.TrimSpace(payload["kind"])
		if kind == "" {
			kind = "wiki_page"
		}
		slug := strings.TrimSpace(payload["slug"])
		if slug == "" {
			slug = strings.TrimSpace(payload["doc_id"])
		}
		updatedAt, _ := parseSearchPayloadTime(payload["updated_at"])
		createdAt, _ := parseSearchPayloadTime(payload["created_at"])
		schemaVersion, _ := strconv.Atoi(strings.TrimSpace(payload["schema_version"]))
		size, _ := strconv.ParseInt(strings.TrimSpace(payload["size"]), 10, 64)
		results = append(results, Result{
			Kind:          kind,
			Slug:          slug,
			Title:         payload["title"],
			Content:       payload["content"],
			Score:         point.Score,
			UpdatedAt:     updatedAt,
			CreatedAt:     createdAt,
			SchemaVersion: schemaVersion,
			PromptVersion: strings.TrimSpace(payload["prompt_version"]),
			Unversioned:   parseSearchPayloadBool(payload["unversioned"]),
			FilePath:      strings.TrimSpace(payload["filepath"]),
			Category:      strings.TrimSpace(payload["category"]),
			Tags:          splitCSVPayloadField(payload["tags"]),
			Related:       splitCSVPayloadField(payload["related"]),
			Sources:       splitCSVPayloadField(payload["sources"]),
			SizeBytes:     size,
		})
	}
	return results, nil
}

// saturateUint64ToInt converts a uint64 to int, clamping to math.MaxInt
// when the value exceeds the platform's int range. Used to safely surface
// Qdrant's PointsCount (uint64) into report fields typed as int (WR-01).
func saturateUint64ToInt(v uint64) int {
	if v > uint64(math.MaxInt) {
		return math.MaxInt
	}
	return int(v)
}

func qdrantPointID(docID string) string {
	sum := sha256.Sum256([]byte(docID))
	b := make([]byte, 16)
	copy(b, sum[:16])
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	hexed := hex.EncodeToString(b)
	return hexed[0:8] + "-" + hexed[8:12] + "-" + hexed[12:16] + "-" + hexed[16:20] + "-" + hexed[20:32]
}
