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
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/storage/qdrant"
	"github.com/aura/aura/internal/stringx"
)

type ToolVectorConfig struct {
	Backend      string
	TopK         int
	QdrantURL    string
	QdrantAPIKey string
	Collection   string
	EmbedBaseURL string
	EmbedAPIKey  string
	EmbedModel   string
	// EmbedOutputDim activates Matryoshka (MRL) truncation client-side when
	// > 0 and smaller than the model's native output dimension. The query
	// vector is sliced to the first EmbedOutputDim components and
	// re-normalized so cosine similarity stays equivalent to native-dim.
	// Must equal the dimension declared on the Qdrant collection — Aura's
	// production target is 256 (embeddinggemma-300m @ 256-d MRL).
	// Live debug 2026-05-17 found query mismatch (`expected dim: 256, got 768`)
	// because this field was missing and embed() was returning native 768-d.
	EmbedOutputDim int
}

type ToolVectorHealth struct {
	Backend     string `json:"backend"`
	DocCount    int    `json:"doc_count"`
	LastRebuild string `json:"last_rebuild,omitempty"`
	LastError   string `json:"last_error,omitempty"`
	EmbedModel  string `json:"embed_model,omitempty"`
	Fallback    bool   `json:"fallback"`
}

type ToolVectorIndex struct {
	qclient    qdrant.Client
	collection string
	cfg        ToolVectorConfig
	http       *http.Client

	// mu guards the optional health fields below. toolindex.Reconciler owns
	// the indexed state (count, last upsert time) since Wave 2.10.b; these
	// fields stay zero unless an external probe writes them.
	mu        sync.RWMutex
	lastError error

	logger *slog.Logger
}

// ToolSearchCollection is the canonical Qdrant collection name used by
// the toolindex.Reconciler writer and the ToolVectorIndex reader. Exported
// so boot wiring + tests reference the same constant.
const ToolSearchCollection = "aura_tool_search_v2"

// ToolVectorDim returns the embedding dimension to declare when creating
// the Qdrant collection. embedOutputDim is the cfg.EmbeddingOutputDim
// passed by the operator (0 = full native dim of the embedding model).
// Aura targets 256 in production (Matryoshka truncation of embeddinggemma).
func ToolVectorDim(embedOutputDim int) int {
	if embedOutputDim > 0 {
		return embedOutputDim
	}
	// Native embeddinggemma-300m dim. Falls back here when the operator
	// has not set EMBEDDING_OUTPUT_DIM (rare; compose ships 256).
	return 768
}

// SearchableEmbeddingTextForLLMDef is the public wrapper around the
// package-private renderer the Reconciler uses to compute the per-tool
// embedding input. Accepts llm.ToolDefinition so callers outside the
// tools package (toolindex.Reconciler in particular) don't need to depend
// on the internal tools.ToolDefinition type. The Examples field is empty
// at this boundary; the renderer treats that as a no-op.
func SearchableEmbeddingTextForLLMDef(def llm.ToolDefinition) string {
	return searchableToolEmbeddingText(ToolDefinition{
		Name:        def.Name,
		Description: def.Description,
		Parameters:  def.Parameters,
	})
}

func NewToolVectorIndex(cfg ToolVectorConfig, logger *slog.Logger) *ToolVectorIndex {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Collection == "" {
		cfg.Collection = ToolSearchCollection
	}
	if cfg.Backend == "" {
		cfg.Backend = "fts"
	}
	idx := &ToolVectorIndex{
		cfg:        cfg,
		collection: cfg.Collection,
		http:       &http.Client{Timeout: 30 * time.Second},
		logger:     logger,
	}
	// Create the shared Qdrant client if a URL is configured and backend is qdrant.
	// For fts backend, qclient stays nil — all Qdrant methods guard on cfg.Backend == "fts".
	if cfg.QdrantURL != "" && cfg.Backend != "fts" {
		if qc, err := qdrant.NewClient(qdrant.Config{
			BaseURL: cfg.QdrantURL,
			APIKey:  cfg.QdrantAPIKey,
		}); err == nil {
			idx.qclient = qc
		} else {
			logger.Warn("tool vector index: failed to create qdrant client", "error", err)
		}
	}
	return idx
}

func (idx *ToolVectorIndex) Ready(ctx context.Context) error {
	if idx == nil || idx.cfg.Backend == "fts" {
		return nil
	}
	if idx.cfg.QdrantURL == "" {
		return fmt.Errorf("QDRANT_URL is required for vector tool search")
	}
	if idx.cfg.EmbedBaseURL == "" || idx.cfg.EmbedAPIKey == "" {
		return fmt.Errorf("embedding config required for vector tool search")
	}
	if idx.qclient == nil {
		return fmt.Errorf("qdrant client not initialized")
	}
	return idx.qclient.Health(ctx)
}

func (idx *ToolVectorIndex) Search(ctx context.Context, query string, topK int, excluded ...string) ([]ToolSearchResult, error) {
	if idx == nil || idx.cfg.Backend == "fts" {
		return nil, nil
	}
	// Snapshot mutable state under the RLock, then release before any HTTP
	// I/O. Holding the lock across embed + Qdrant calls would block any
	// concurrent health write.
	idx.mu.RLock()
	lastErr := idx.lastError
	qclient := idx.qclient
	collection := idx.collection
	idx.mu.RUnlock()

	if lastErr != nil {
		return nil, lastErr
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

	if qclient == nil {
		return nil, fmt.Errorf("qdrant client not initialized")
	}
	points, err := qclient.Search(ctx, collection, vectors[0], topK*3, true)
	if err != nil {
		idx.logger.Warn("tool vector search: qdrant query failed", "error", err)
		return nil, err
	}

	results := make([]ToolSearchResult, 0, len(points))
	for _, pt := range points {
		name := strings.TrimSpace(pt.Payload["name"])
		if name == "" || exclude[name] {
			continue
		}
		results = append(results, ToolSearchResult{
			Name:        name,
			Description: strings.TrimSpace(pt.Payload["text"]),
			Score:       int(pt.Score * 100),
		})
		if len(results) >= topK {
			break
		}
	}
	return results, nil
}

// Health reports the reader's observable state. DocCount and LastRebuild
// are owned by toolindex.Reconciler post-Wave 2.10.b and stay zero/empty
// in this view; callers needing those values should query the reconciler
// or the manual /api/tools/reindex endpoint.
func (idx *ToolVectorIndex) Health() ToolVectorHealth {
	if idx == nil {
		return ToolVectorHealth{Backend: "fts", Fallback: true}
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	h := ToolVectorHealth{
		Backend:    idx.cfg.Backend,
		EmbedModel: idx.cfg.EmbedModel,
	}
	if idx.lastError != nil {
		h.LastError = idx.lastError.Error()
		h.Fallback = true
	}
	return h
}

func (idx *ToolVectorIndex) embed(ctx context.Context, texts []string) ([][]float32, error) {
	baseURL := stringx.NormalizeBaseURL(idx.cfg.EmbedBaseURL)
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
	// MRL truncation + renormalization. The Qdrant collection is created
	// with EmbedOutputDim components (e.g. 256 for embeddinggemma-300m @
	// 256-d MRL); if the embedding endpoint returns the model's native
	// dimension (768), slice and renormalize so cosine similarity stays
	// equivalent. Matches the writer-side behavior in
	// internal/storage/search/embed_http.go.
	if idx.cfg.EmbedOutputDim > 0 {
		for i, vector := range vectors {
			if len(vector) <= idx.cfg.EmbedOutputDim {
				continue
			}
			truncated := vector[:idx.cfg.EmbedOutputDim]
			var sumSquares float32
			for _, v := range truncated {
				sumSquares += v * v
			}
			if sumSquares > 0 {
				inv := float32(1.0 / math.Sqrt(float64(sumSquares)))
				renormed := make([]float32, idx.cfg.EmbedOutputDim)
				for j, v := range truncated {
					renormed[j] = v * inv
				}
				vectors[i] = renormed
			} else {
				vectors[i] = append([]float32(nil), truncated...)
			}
		}
	}
	return vectors, nil
}

// ToolQdrantPointID derives a stable Qdrant point ID from a tool name.
// Exported so toolindex.Reconciler can compute it the same way as the
// search-side reader.
func ToolQdrantPointID(name string) string {
	return toolQdrantPointID(name)
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
