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
	// OutputDim, when > 0, enables dim-mismatch detection at boot: if the
	// existing collection's vector size differs from OutputDim the collection
	// is dropped and rebuilt unless SkipDimMismatchRebuild is true.
	OutputDim              int
	SkipDimMismatchRebuild bool
}

// PagesIndexedUnknown is the sentinel value for QdrantRebuildReport.PagesIndexed
// when the on-disk page count could not be determined (e.g. loadWikiDocuments
// failed during a warm-cache hit). Consumers should treat this as "unavailable",
// not as "zero pages on disk" (WR-05).
const PagesIndexedUnknown = -1

// SkippedDoc records a wiki document that could not be embedded during a rebuild.
type SkippedDoc struct {
	DocID  string `json:"doc_id"`
	Reason string `json:"reason"`
}

type QdrantRebuildReport struct {
	Collection  string `json:"collection"`
	DocsIndexed int    `json:"docs_indexed"`
	// PagesIndexed is the number of wiki pages enumerated on disk during the
	// rebuild. The value PagesIndexedUnknown (-1) means the disk enumeration
	// failed during a warm-cache hit; callers should not interpret it as zero.
	PagesIndexed int `json:"pages_indexed"`
	VectorSize   int `json:"vector_size"`
	// PriorVectorSize is the collection's vector dimension before this rebuild.
	// Zero when no prior collection existed or the dim was not exposed by the
	// Qdrant API. Useful to detect whether a rebuild changed the dimension.
	PriorVectorSize int          `json:"prior_vector_size,omitempty"`
	SkippedDocs     []SkippedDoc `json:"skipped_docs,omitempty"`
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
	// expectedDim and skipDimRebuild control boot-time dim-mismatch detection.
	// When expectedDim > 0 and the existing collection's vector size differs,
	// the collection is auto-rebuilt unless skipDimRebuild is true.
	expectedDim    int
	skipDimRebuild bool
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
		expectedDim:      cfg.OutputDim,
		skipDimRebuild:   cfg.SkipDimMismatchRebuild,
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
		expectedDim:      cfg.OutputDim,
		skipDimRebuild:   cfg.SkipDimMismatchRebuild,
	}, nil
}

func (r *qdrantRepository) Search(ctx context.Context, query string, topK int) ([]Result, error) {
	return r.primary.Search(ctx, query, topK)
}

func (r *qdrantRepository) IsIndexed() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.indexed
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
