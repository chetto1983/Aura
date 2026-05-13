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
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/qdrant"
)

const defaultVectorBatchSize = 32

type ToolVectorConfig struct {
	Backend      string
	TopK         int
	QdrantURL    string
	QdrantAPIKey string
	Collection   string
	EmbedBaseURL string
	EmbedAPIKey  string
	EmbedModel   string
}

type ToolVectorHealth struct {
	Backend     string `json:"backend"`
	DocCount    int    `json:"doc_count"`
	LastRebuild string `json:"last_rebuild,omitempty"`
	LastError   string `json:"last_error,omitempty"`
	EmbedModel  string `json:"embed_model,omitempty"`
	Fallback    bool   `json:"fallback"`
}

type toolVectorDoc struct {
	name string
	text string
}

type ToolVectorIndex struct {
	qclient    qdrant.Client
	collection string
	cfg        ToolVectorConfig
	http       *http.Client

	// mu guards the in-memory Build/Search state below. It is held only
	// for fast in-memory mutations / reads, never around HTTP I/O (WR-04).
	mu          sync.RWMutex
	docCount    int
	lastRebuild time.Time
	lastError   error

	// buildMu serializes concurrent Build calls so they do not race each
	// other on the Qdrant collection (delete/create/upsert). Search does
	// NOT take buildMu — it only takes mu.RLock for the in-memory state,
	// which means a Search initiated mid-Build returns the previous
	// build's docCount/lastError snapshot and proceeds without blocking
	// on the Build's HTTP calls (WR-04).
	buildMu sync.Mutex

	logger *slog.Logger
}

// ToolSearchCollection is the canonical Qdrant collection name used by
// both the legacy BuildVectorIndex reader and the Wave 2.10.b
// toolindex.Reconciler writer. Exported so boot wiring + tests reference
// the same constant.
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
// at this boundary; the renderer treats that as a no-op, so the
// resulting bytes are identical to what BuildVectorIndex hashed.
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

// Build (re)builds the Qdrant-backed tool index. WR-04: idx.mu is NOT held
// across the Qdrant/embedding HTTP calls. buildMu serializes concurrent
// Build calls; idx.mu is taken only briefly to publish the resulting
// in-memory state (docCount, lastRebuild, lastError). This means Search
// callers continue to see the previous snapshot during a long rebuild
// instead of blocking on the rebuild's HTTP round-trips.
func (idx *ToolVectorIndex) Build(ctx context.Context, docs []toolVectorDoc) error {
	if idx == nil || idx.cfg.Backend == "fts" {
		return nil
	}
	// Serialize Builds so two callers do not race the delete/create/upsert
	// sequence on the same Qdrant collection.
	idx.buildMu.Lock()
	defer idx.buildMu.Unlock()

	// publish writes the build outcome to the shared in-memory state.
	// Used in defer-style for early returns and at the end on success.
	publish := func(docCount int, buildErr error) {
		idx.mu.Lock()
		idx.docCount = docCount
		idx.lastError = buildErr
		idx.lastRebuild = time.Now()
		idx.mu.Unlock()
	}

	if len(docs) == 0 {
		publish(0, nil)
		return nil
	}

	if idx.qclient == nil {
		err := fmt.Errorf("qdrant client not initialized")
		idx.logger.Warn("tool vector index build: qdrant client unavailable", "error", err)
		publish(0, err)
		return nil
	}

	// QDRANT-01 warm-cache short-circuit: if the collection already exists with
	// points, skip the rebuild and reuse the cached vectors.
	//
	// WR-03 / T-01-24 (accepted): vector-size drift from EMBEDDING_MODEL swaps
	// is not detected here because CollectionInfo does not expose the stored
	// vector size. Search queries will fail loudly with a Qdrant dimension
	// error if the operator changes models without rebuilding the collection.
	info, infoErr := idx.qclient.CollectionInfo(ctx, idx.collection)
	if infoErr != nil {
		// Defensive fallback: a transient probe failure must not block startup.
		idx.logger.Warn("tool vector index build: collection info probe failed; proceeding with full rebuild", "collection", idx.collection, "error", infoErr)
	} else if info.PointsCount > 0 {
		idx.logger.Info("tool vector qdrant warm-cache hit, skipping rebuild", "collection", idx.collection, "points_count", info.PointsCount, "docs", len(docs))
		publish(len(docs), nil)
		return nil
	}

	texts := make([]string, len(docs))
	for i, doc := range docs {
		texts[i] = doc.text
	}

	vectors, err := idx.embed(ctx, texts)
	if err != nil {
		idx.logger.Warn("tool vector index build: embedding failed, falling back to fts", "error", err)
		publish(0, err)
		return nil
	}
	if len(vectors) != len(docs) {
		publish(0, fmt.Errorf("expected %d vectors, got %d", len(docs), len(vectors)))
		return nil
	}

	vectorSize := len(vectors[0])
	if err := idx.qclient.DeleteCollection(ctx, idx.collection); err != nil {
		idx.logger.Warn("tool vector index build: qdrant collection delete failed", "error", err)
		publish(0, err)
		return nil
	}
	if err := idx.qclient.CreateCollection(ctx, idx.collection, vectorSize); err != nil {
		idx.logger.Warn("tool vector index build: qdrant collection create failed", "error", err)
		publish(0, err)
		return nil
	}

	qpoints := make([]qdrant.Point, len(docs))
	for i, doc := range docs {
		qpoints[i] = qdrant.Point{
			ID:     toolQdrantPointID(doc.name),
			Vector: vectors[i],
			Payload: map[string]string{
				"name": doc.name,
				"text": doc.text,
			},
		}
	}

	if err := idx.qclient.Upsert(ctx, idx.collection, qpoints); err != nil {
		idx.logger.Warn("tool vector index build: qdrant upsert failed", "error", err)
		publish(0, err)
		return nil
	}

	publish(len(docs), nil)
	idx.logger.Info("tool vector index built", "docs", len(docs), "vector_size", vectorSize)
	return nil
}

func (idx *ToolVectorIndex) Search(ctx context.Context, query string, topK int, excluded ...string) ([]ToolSearchResult, error) {
	if idx == nil || idx.cfg.Backend == "fts" {
		return nil, nil
	}
	// Snapshot mutable state under the RLock, then release before any HTTP
	// I/O. Holding the lock across embed + Qdrant calls would starve rebuilds
	// (Build's idx.mu.Lock waits behind every in-flight search RTT).
	idx.mu.RLock()
	docCount := idx.docCount
	lastErr := idx.lastError
	qclient := idx.qclient
	collection := idx.collection
	idx.mu.RUnlock()

	if docCount == 0 || lastErr != nil {
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

func (idx *ToolVectorIndex) Health() ToolVectorHealth {
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

func (idx *ToolVectorIndex) embed(ctx context.Context, texts []string) ([][]float32, error) {
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

// ToolQdrantPointID is exported so toolindex.Reconciler can derive the
// SAME point ID for a given tool name as the legacy BuildVectorIndex
// path. Identical IDs mean the two paths upsert into the same row in
// Qdrant; without this alignment a fresh-boot install could leave two
// points per tool (one from each writer) until the next reconcile.
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
