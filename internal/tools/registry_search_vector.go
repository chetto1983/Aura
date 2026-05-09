package tools

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
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultVectorBatchSize = 32

type ToolVectorConfig struct {
	Backend        string
	TopK           int
	QdrantURL      string
	QdrantAPIKey   string
	Collection     string
	EmbedBaseURL   string
	EmbedAPIKey    string
	EmbedModel     string
}

type ToolVectorHealth struct {
	Backend      string  `json:"backend"`
	DocCount     int     `json:"doc_count"`
	LastRebuild  string  `json:"last_rebuild,omitempty"`
	LastError    string  `json:"last_error,omitempty"`
	EmbedModel   string  `json:"embed_model,omitempty"`
	Fallback     bool    `json:"fallback"`
}

type toolVectorDoc struct {
	name string
	text string
}

type toolVectorIndex struct {
	cfg        ToolVectorConfig
	http       *http.Client
	mu         sync.RWMutex
	docCount   int
	lastRebuild time.Time
	lastError  error
	logger     *slog.Logger
}

func NewToolVectorIndex(cfg ToolVectorConfig, logger *slog.Logger) *toolVectorIndex {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Collection == "" {
		cfg.Collection = "aura_tool_search"
	}
	if cfg.Backend == "" {
		cfg.Backend = "fts"
	}
	return &toolVectorIndex{
		cfg:    cfg,
		http:   &http.Client{Timeout: 30 * time.Second},
		logger: logger,
	}
}

func (idx *toolVectorIndex) Ready(ctx context.Context) error {
	if idx == nil || idx.cfg.Backend == "fts" {
		return nil
	}
	if idx.cfg.QdrantURL == "" {
		return fmt.Errorf("QDRANT_URL is required for vector tool search")
	}
	if idx.cfg.EmbedBaseURL == "" || idx.cfg.EmbedAPIKey == "" {
		return fmt.Errorf("embedding config required for vector tool search")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, idx.qdrantBase()+"/readyz", nil)
	if err != nil {
		return err
	}
	idx.authorizeQdrant(req)
	resp, err := idx.http.Do(req)
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

func (idx *toolVectorIndex) Build(ctx context.Context, docs []toolVectorDoc) error {
	if idx == nil || idx.cfg.Backend == "fts" {
		return nil
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.lastError = nil
	if len(docs) == 0 {
		idx.docCount = 0
		idx.lastRebuild = time.Now()
		return nil
	}

	texts := make([]string, len(docs))
	for i, doc := range docs {
		texts[i] = doc.text
	}

	vectors, err := idx.embed(ctx, texts)
	if err != nil {
		idx.lastError = err
		idx.logger.Warn("tool vector index build: embedding failed, falling back to fts", "error", err)
		return nil
	}
	if len(vectors) != len(docs) {
		idx.lastError = fmt.Errorf("expected %d vectors, got %d", len(docs), len(vectors))
		return nil
	}

	vectorSize := len(vectors[0])
	if err := idx.recreateQdrantCollection(ctx, vectorSize); err != nil {
		idx.lastError = err
		idx.logger.Warn("tool vector index build: qdrant collection recreate failed", "error", err)
		return nil
	}

	points := make([]map[string]any, len(docs))
	for i, doc := range docs {
		points[i] = map[string]any{
			"id":     toolQdrantPointID(doc.name),
			"vector": vectors[i],
			"payload": map[string]string{
				"name": doc.name,
				"text": doc.text,
			},
		}
	}

	if err := idx.upsertQdrantPoints(ctx, points); err != nil {
		idx.lastError = err
		idx.logger.Warn("tool vector index build: qdrant upsert failed", "error", err)
		return nil
	}

	idx.docCount = len(docs)
	idx.lastRebuild = time.Now()
	idx.logger.Info("tool vector index built", "docs", idx.docCount, "vector_size", vectorSize)
	return nil
}

func (idx *toolVectorIndex) Search(ctx context.Context, query string, topK int, excluded ...string) ([]ToolSearchResult, error) {
	if idx == nil || idx.cfg.Backend == "fts" {
		return nil, nil
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if idx.docCount == 0 || idx.lastError != nil {
		return nil, idx.lastError
	}

	vectors, err := idx.embed(ctx, []string{query})
	if err != nil {
		idx.logger.Warn("tool vector search: embedding failed", "error", err)
		return nil, err
	}
	if len(vectors) == 0 || len(vectors[0]) == 0 {
		return nil, fmt.Errorf("empty embedding for query")
	}

	exclude := make(map[string]bool, len(excluded))
	for _, name := range excluded {
		name = strings.TrimSpace(name)
		if name != "" {
			exclude[name] = true
		}
	}

	points, err := idx.queryQdrantPoints(ctx, vectors[0], topK*3)
	if err != nil {
		idx.logger.Warn("tool vector search: qdrant query failed", "error", err)
		return nil, err
	}

	results := make([]ToolSearchResult, 0, len(points))
	for _, pt := range points {
		name := strings.TrimSpace(pt["name"])
		if name == "" || exclude[name] {
			continue
		}
		score, _ := strconv.ParseFloat(strings.TrimSpace(pt["score"]), 64)
		results = append(results, ToolSearchResult{
			Name:        name,
			Description: strings.TrimSpace(pt["text"]),
			Score:       int(score * 100),
		})
		if len(results) >= topK {
			break
		}
	}
	return results, nil
}

func (idx *toolVectorIndex) Health() ToolVectorHealth {
	if idx == nil {
		return ToolVectorHealth{Backend: "fts", Fallback: true}
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	h := ToolVectorHealth{
		Backend:    idx.cfg.Backend,
		DocCount:   idx.docCount,
		EmbedModel: idx.cfg.EmbedModel,
	}
	if idx.lastError != nil {
		h.LastError = idx.lastError.Error()
		h.Fallback = true
	}
	if !idx.lastRebuild.IsZero() {
		h.LastRebuild = idx.lastRebuild.Format(time.RFC3339)
	}
	return h
}

func (idx *toolVectorIndex) embed(ctx context.Context, texts []string) ([][]float32, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(idx.cfg.EmbedBaseURL), "/")
	body := map[string]any{
		"model": idx.cfg.EmbedModel,
		"input": texts,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/embeddings", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+idx.cfg.EmbedAPIKey)
	resp, err := idx.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings request: %w", err)
	}
	defer resp.Body.Close()
	respBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if readErr != nil {
		return nil, fmt.Errorf("embeddings read response: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embeddings returned %s: %s", resp.Status, strings.TrimSpace(string(respBytes)))
	}
	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBytes, &out); err != nil {
		return nil, fmt.Errorf("embeddings decode response: %w", err)
	}
	vectors := make([][]float32, len(texts))
	for i, item := range out.Data {
		index := item.Index
		if index < 0 || index >= len(vectors) {
			index = i
		}
		if index >= 0 && index < len(vectors) {
			vectors[index] = item.Embedding
		}
	}
	for i, vector := range vectors {
		if len(vector) == 0 {
			return nil, fmt.Errorf("embeddings missing vector at index %d", i)
		}
	}
	return vectors, nil
}

func (idx *toolVectorIndex) qdrantBase() string {
	return strings.TrimRight(strings.TrimSpace(idx.cfg.QdrantURL), "/")
}

func (idx *toolVectorIndex) collectionPath() string {
	return idx.qdrantBase() + "/collections/" + url.PathEscape(idx.cfg.Collection)
}

func (idx *toolVectorIndex) authorizeQdrant(req *http.Request) {
	if idx.cfg.QdrantAPIKey != "" {
		req.Header.Set("api-key", idx.cfg.QdrantAPIKey)
	}
}

func (idx *toolVectorIndex) recreateQdrantCollection(ctx context.Context, vectorSize int) error {
	if err := idx.deleteQdrantCollection(ctx); err != nil {
		return err
	}
	body := map[string]any{
		"vectors": map[string]any{
			"size":     vectorSize,
			"distance": "Cosine",
		},
	}
	return idx.doQdrantJSON(ctx, http.MethodPut, idx.collectionPath(), body, http.StatusOK, http.StatusConflict)
}

func (idx *toolVectorIndex) deleteQdrantCollection(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, idx.collectionPath(), nil)
	if err != nil {
		return err
	}
	idx.authorizeQdrant(req)
	resp, err := idx.http.Do(req)
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

func (idx *toolVectorIndex) upsertQdrantPoints(ctx context.Context, points []map[string]any) error {
	for start := 0; start < len(points); start += defaultVectorBatchSize {
		end := start + defaultVectorBatchSize
		if end > len(points) {
			end = len(points)
		}
		body := map[string]any{"points": points[start:end]}
		if err := idx.doQdrantJSON(ctx, http.MethodPut, idx.collectionPath()+"/points?wait=true", body, http.StatusOK); err != nil {
			return err
		}
	}
	return nil
}

func (idx *toolVectorIndex) queryQdrantPoints(ctx context.Context, vector []float32, topK int) ([]map[string]string, error) {
	body := map[string]any{
		"query":        vector,
		"limit":        topK,
		"with_payload": true,
	}
	var out struct {
		Status string `json:"status"`
		Result struct {
			Points []struct {
				Score   float32           `json:"score"`
				Payload map[string]string `json:"payload"`
			} `json:"points"`
		} `json:"result"`
	}
	if err := idx.doQdrantJSONDecode(ctx, http.MethodPost, idx.collectionPath()+"/points/query", body, &out, http.StatusOK); err != nil {
		return nil, err
	}
	if out.Status != "" && out.Status != "ok" {
		return nil, fmt.Errorf("qdrant query returned status %q", out.Status)
	}
	results := make([]map[string]string, 0, len(out.Result.Points))
	for _, pt := range out.Result.Points {
		m := map[string]string{
			"name":  strings.TrimSpace(pt.Payload["name"]),
			"text":  strings.TrimSpace(pt.Payload["text"]),
			"score": fmt.Sprintf("%.4f", pt.Score),
		}
		results = append(results, m)
	}
	return results, nil
}

func (idx *toolVectorIndex) doQdrantJSON(ctx context.Context, method, endpoint string, body any, accepted ...int) error {
	return idx.doQdrantJSONDecode(ctx, method, endpoint, body, nil, accepted...)
}

func (idx *toolVectorIndex) doQdrantJSONDecode(ctx context.Context, method, endpoint string, body any, out any, accepted ...int) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	idx.authorizeQdrant(req)
	resp, err := idx.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, endpoint, err)
	}
	defer resp.Body.Close()
	respBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if readErr != nil {
		return fmt.Errorf("%s %s read response: %w", method, endpoint, readErr)
	}
	for _, code := range accepted {
		if resp.StatusCode == code {
			if out != nil && len(respBytes) > 0 {
				if err := json.Unmarshal(respBytes, out); err != nil {
					return fmt.Errorf("%s %s decode response: %w", method, endpoint, err)
				}
			}
			return nil
		}
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(respBytes) > 0 {
			if err := json.Unmarshal(respBytes, out); err != nil {
				return fmt.Errorf("%s %s decode response: %w", method, endpoint, err)
			}
		}
		return nil
	}
	return fmt.Errorf("%s %s returned %s: %s", method, endpoint, resp.Status, strings.TrimSpace(string(respBytes)))
}

func toolQdrantPointID(name string) string {
	sum := sha256.Sum256([]byte("tool:" + name))
	b := make([]byte, 16)
	copy(b, sum[:16])
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	hexed := hex.EncodeToString(b)
	return hexed[0:8] + "-" + hexed[8:12] + "-" + hexed[12:16] + "-" + hexed[16:20] + "-" + hexed[20:32]
}
