package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/qdrant"
	"github.com/philippgille/chromem-go"
)

// QdrantConfig describes Aura's external vector index.
type QdrantConfig struct {
	BaseURL    string
	Collection string
	APIKey     string
	BatchSize  int
	Client     *http.Client
}

type QdrantRebuildReport struct {
	Collection   string `json:"collection"`
	DocsIndexed  int    `json:"docs_indexed"`
	PagesIndexed int    `json:"pages_indexed"`
	VectorSize   int    `json:"vector_size"`
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

func (r *qdrantRepository) Search(ctx context.Context, query string, topK int) ([]Result, error) {
	return r.primary.Search(ctx, query, topK)
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
	if err := r.client.CreateCollection(ctx, r.collectionQdrant, len(vector)); err != nil {
		return err
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
	r.mu.Lock()
	r.indexed = true
	r.mu.Unlock()
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
	_ = slug
	return r.IndexWikiPages(ctx)
}

// RebuildQdrantWikiDocuments recreates the configured collection from Aura's
// wiki pages and graph cards.
func RebuildQdrantWikiDocuments(ctx context.Context, wikiDir string, embedFn chromem.EmbeddingFunc, cfg QdrantConfig, logger *slog.Logger) (QdrantRebuildReport, error) {
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
func rebuildQdrantWikiDocumentsWithClient(ctx context.Context, wikiDir string, embedFn chromem.EmbeddingFunc, client qdrant.Client, collection string, logger *slog.Logger) (QdrantRebuildReport, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if embedFn == nil {
		return QdrantRebuildReport{}, fmt.Errorf("embedding function is required")
	}
	if err := client.Health(ctx); err != nil {
		return QdrantRebuildReport{}, err
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
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
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
		results = append(results, Result{
			Kind:    kind,
			Slug:    slug,
			Title:   payload["title"],
			Content: payload["content"],
			Score:   point.Score,
		})
	}
	return results, nil
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
