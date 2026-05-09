package search

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/philippgille/chromem-go"
)

const defaultQdrantBatchSize = 64

// QdrantConfig describes Aura's optional external vector index. SQLite FTS
// remains the local fallback; Qdrant is rebuilt from the wiki manifest.
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

type qdrantClient struct {
	baseURL    string
	collection string
	apiKey     string
	batchSize  int
	http       *http.Client
}

type qdrantSearcher struct {
	client  *qdrantClient
	embedFn EmbeddingFunction
}

type qdrantRepository struct {
	primary  *qdrantSearcher
	fallback Repository
	logger   *slog.Logger
}

// CheckQdrantReady probes the Qdrant REST service without mutating data.
func CheckQdrantReady(ctx context.Context, cfg QdrantConfig) error {
	client, err := newQdrantClient(cfg)
	if err != nil {
		return err
	}
	return client.ready(ctx)
}

// NewQdrantSearcher creates a read-only Qdrant-backed semantic searcher.
// It uses Qdrant's Query Points endpoint with Aura-owned embeddings and
// payloads created by RebuildQdrantWikiDocuments.
func NewQdrantSearcher(cfg QdrantConfig, embedFn EmbeddingFunction) (*qdrantSearcher, error) {
	if embedFn == nil {
		return nil, fmt.Errorf("embedding function is required")
	}
	client, err := newQdrantClient(cfg)
	if err != nil {
		return nil, err
	}
	return &qdrantSearcher{client: client, embedFn: embedFn}, nil
}

// NewQdrantRepository wraps the existing local search repository with an
// opt-in Qdrant query path. Writes and reindexing still go to the local
// repository; Qdrant remains a rebuildable sidecar until promotion is complete.
func NewQdrantRepository(cfg QdrantConfig, embedFn EmbeddingFunction, fallback Repository, logger *slog.Logger) (Repository, error) {
	if fallback == nil {
		return nil, fmt.Errorf("fallback search repository is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	primary, err := NewQdrantSearcher(cfg, embedFn)
	if err != nil {
		return nil, err
	}
	return &qdrantRepository{primary: primary, fallback: fallback, logger: logger}, nil
}

func (r *qdrantRepository) Search(ctx context.Context, query string, topK int) ([]Result, error) {
	primaryResults, primaryErr := r.primary.Search(ctx, query, topK)
	fallbackResults, fallbackErr := r.fallback.Search(ctx, query, topK)
	merged := mergeHybridResults(query, topK, fallbackResults, primaryResults)
	if len(merged) > 0 {
		if primaryErr != nil && r.logger != nil {
			r.logger.Warn("qdrant search failed; returned local hybrid results", "error", primaryErr)
		}
		if fallbackErr != nil && r.logger != nil {
			r.logger.Warn("local hybrid search failed; returned qdrant results", "error", fallbackErr)
		}
		return merged, nil
	}
	if r.logger != nil {
		if primaryErr != nil {
			r.logger.Warn("qdrant search failed", "error", primaryErr)
		}
		if fallbackErr != nil {
			r.logger.Warn("local hybrid search failed", "error", fallbackErr)
		}
	}
	if primaryErr != nil {
		return nil, primaryErr
	}
	return nil, fallbackErr
}

func (r *qdrantRepository) IsIndexed() bool {
	return r.fallback.IsIndexed()
}

func (r *qdrantRepository) Index(ctx context.Context, id string, content string, metadata map[string]string) error {
	return r.fallback.Index(ctx, id, content, metadata)
}

func (r *qdrantRepository) IndexWikiPages(ctx context.Context) error {
	return r.fallback.IndexWikiPages(ctx)
}

func (r *qdrantRepository) ReindexWikiPage(ctx context.Context, slug string) error {
	return r.fallback.ReindexWikiPage(ctx, slug)
}

// RebuildQdrantWikiDocuments recreates the configured collection from Aura's
// wiki pages and graph cards. It intentionally does not replace the primary
// runtime index yet; this command gives us a deterministic sidecar rebuild.
func RebuildQdrantWikiDocuments(ctx context.Context, wikiDir string, embedFn chromem.EmbeddingFunc, cfg QdrantConfig, logger *slog.Logger) (QdrantRebuildReport, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if embedFn == nil {
		return QdrantRebuildReport{}, fmt.Errorf("embedding function is required")
	}
	client, err := newQdrantClient(cfg)
	if err != nil {
		return QdrantRebuildReport{}, err
	}
	if err := client.ready(ctx); err != nil {
		return QdrantRebuildReport{}, err
	}
	docs, pages, err := loadWikiDocuments(wikiDir, logger)
	if err != nil {
		return QdrantRebuildReport{}, err
	}
	if len(docs) == 0 {
		return QdrantRebuildReport{Collection: client.collection, PagesIndexed: pages}, nil
	}

	points := make([]qdrantPoint, 0, len(docs))
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
		points = append(points, qdrantPoint{
			ID:      qdrantPointID(doc.ID),
			Vector:  vector,
			Payload: payload,
		})
	}

	if err := client.recreateCollection(ctx, vectorSize); err != nil {
		return QdrantRebuildReport{}, err
	}
	if err := client.upsertPoints(ctx, points); err != nil {
		return QdrantRebuildReport{}, err
	}
	return QdrantRebuildReport{
		Collection:   client.collection,
		DocsIndexed:  len(points),
		PagesIndexed: pages,
		VectorSize:   vectorSize,
	}, nil
}

func newQdrantClient(cfg QdrantConfig) (*qdrantClient, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("QDRANT_URL is required")
	}
	if _, err := url.ParseRequestURI(base); err != nil {
		return nil, fmt.Errorf("invalid QDRANT_URL: %w", err)
	}
	collection := strings.TrimSpace(cfg.Collection)
	if collection == "" {
		return nil, fmt.Errorf("QDRANT_COLLECTION is required")
	}
	httpClient := cfg.Client
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = defaultQdrantBatchSize
	}
	return &qdrantClient{
		baseURL:    base,
		collection: collection,
		apiKey:     cfg.APIKey,
		batchSize:  batchSize,
		http:       httpClient,
	}, nil
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
	points, err := s.client.queryPoints(ctx, vector, topK)
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

func (c *qdrantClient) queryPoints(ctx context.Context, vector []float32, topK int) ([]qdrantScoredPoint, error) {
	body := map[string]any{
		"query":        vector,
		"limit":        topK,
		"with_payload": true,
	}
	var out qdrantQueryResponse
	if err := c.doJSONResponse(ctx, http.MethodPost, c.collectionPath()+"/points/query", body, &out, http.StatusOK); err != nil {
		return nil, err
	}
	if out.Status != "" && out.Status != "ok" {
		return nil, fmt.Errorf("qdrant query returned status %q", out.Status)
	}
	return out.Result.Points, nil
}

func (c *qdrantClient) ready(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/readyz", nil)
	if err != nil {
		return err
	}
	c.authorize(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant ready check: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("qdrant ready check returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *qdrantClient) recreateCollection(ctx context.Context, vectorSize int) error {
	if vectorSize <= 0 {
		return fmt.Errorf("vector size must be positive")
	}
	if err := c.deleteCollection(ctx); err != nil {
		return err
	}
	return c.createCollection(ctx, vectorSize)
}

func (c *qdrantClient) createCollection(ctx context.Context, vectorSize int) error {
	if vectorSize <= 0 {
		return fmt.Errorf("vector size must be positive")
	}
	body := map[string]any{
		"vectors": map[string]any{
			"size":     vectorSize,
			"distance": "Cosine",
		},
	}
	return c.doJSON(ctx, http.MethodPut, c.collectionPath(), body, http.StatusOK, http.StatusConflict)
}

func (c *qdrantClient) deleteCollection(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.collectionPath(), nil)
	if err != nil {
		return err
	}
	c.authorize(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("delete qdrant collection: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("delete qdrant collection returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *qdrantClient) upsertPoints(ctx context.Context, points []qdrantPoint) error {
	for start := 0; start < len(points); start += c.batchSize {
		end := start + c.batchSize
		if end > len(points) {
			end = len(points)
		}
		body := map[string]any{"points": points[start:end]}
		if err := c.doJSON(ctx, http.MethodPut, c.collectionPath()+"/points?wait=true", body, http.StatusOK); err != nil {
			return err
		}
	}
	return nil
}

func (c *qdrantClient) deletePoints(ctx context.Context, ids []string) error {
	for start := 0; start < len(ids); start += c.batchSize {
		end := start + c.batchSize
		if end > len(ids) {
			end = len(ids)
		}
		body := map[string]any{"points": ids[start:end]}
		if err := c.doJSON(ctx, http.MethodPost, c.collectionPath()+"/points/delete?wait=true", body, http.StatusOK); err != nil {
			if isQdrantNotFound(err) {
				return nil
			}
			return err
		}
	}
	return nil
}

func isQdrantNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "404") || strings.Contains(msg, "not found")
}

func (c *qdrantClient) doJSON(ctx context.Context, method, endpoint string, body any, accepted ...int) error {
	return c.doJSONResponse(ctx, method, endpoint, body, nil, accepted...)
}

func (c *qdrantClient) doJSONResponse(ctx context.Context, method, endpoint string, body any, out any, accepted ...int) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.authorize(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, endpoint, err)
	}
	defer resp.Body.Close()
	bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if readErr != nil {
		return fmt.Errorf("%s %s read response: %w", method, endpoint, readErr)
	}
	for _, code := range accepted {
		if resp.StatusCode == code {
			if out != nil && len(bodyBytes) > 0 {
				if err := json.Unmarshal(bodyBytes, out); err != nil {
					return fmt.Errorf("%s %s decode response: %w", method, endpoint, err)
				}
			}
			return nil
		}
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(bodyBytes) > 0 {
			if err := json.Unmarshal(bodyBytes, out); err != nil {
				return fmt.Errorf("%s %s decode response: %w", method, endpoint, err)
			}
		}
		return nil
	}
	return fmt.Errorf("%s %s returned %s: %s", method, endpoint, resp.Status, strings.TrimSpace(string(bodyBytes)))
}

func (c *qdrantClient) collectionPath() string {
	return c.baseURL + "/collections/" + url.PathEscape(c.collection)
}

func (c *qdrantClient) authorize(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("api-key", c.apiKey)
	}
}

type qdrantPoint struct {
	ID      string            `json:"id"`
	Vector  []float32         `json:"vector"`
	Payload map[string]string `json:"payload"`
}

type qdrantScoredPoint struct {
	ID      any               `json:"id"`
	Score   float32           `json:"score"`
	Payload map[string]string `json:"payload"`
}

type qdrantQueryResponse struct {
	Status string `json:"status"`
	Result struct {
		Points []qdrantScoredPoint `json:"points"`
	} `json:"result"`
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
