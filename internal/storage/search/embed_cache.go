package search

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync/atomic"
	"time"

	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/stringx"
)

const embedCacheBatchSize = 8

// EmbedCacheNamespace returns the provider-scoped cache namespace for
// embedding vectors. Model alone is not enough: OpenAI-compatible endpoints can
// expose the same model name with different dimensions or semantics.
func EmbedCacheNamespace(baseURL, model string) string {
	baseURL = stringx.NormalizeBaseURL(baseURL)
	model = strings.TrimSpace(model)
	if baseURL == "" {
		return model
	}
	if model == "" {
		return baseURL
	}
	return baseURL + "|" + model
}

// EmbedCache wraps a EmbeddingFunc with a SQLite-backed
// content-addressed cache. The same fn serves both wiki indexing and
// query embedding, so repeat queries hit the cache too.
//
// Why content-addressed: embedding APIs (Mistral here) are slow
// (~500ms-2s per call) and remote. A wiki of 50 pages re-embedded on
// every restart wastes 30+ seconds and a chunk of API budget for no
// benefit when the content hasn't changed. SHA-256 of the full input
// uniquely identifies the document version, so a cache miss happens
// only when content (or model) actually changed.
//
// The cache key is (content_sha, model). Callers should pass
// EmbedCacheNamespace(baseURL, model) as model so changing either
// EMBEDDING_BASE_URL or EMBEDDING_MODEL invalidates entries automatically.
// Stale entries linger but cost nothing — pruning is a manual op.
type EmbedCache struct {
	db         *sql.DB
	model      string
	inner      EmbeddingFunc
	batchInner BatchEmbeddingFunction
	logger     *slog.Logger
	owned      bool
	hits       atomic.Uint64
	misses     atomic.Uint64
}

// EmbedCacheStatsReader is the read-only diagnostics boundary for cache health.
type EmbedCacheStatsReader interface {
	Stats() (hits, misses uint64)
}

// OpenEmbedCache opens (or creates) the cache table on dbPath. If
// inner is nil, the cache short-circuits to an error on miss — useful
// for tests where we want to verify cache hits without spinning up a
// real embedding provider.
func OpenEmbedCache(dbPath, model string, inner EmbeddingFunc, logger *slog.Logger) (*EmbedCache, error) {
	if dbPath == "" {
		return nil, errors.New("embed cache: dbPath required")
	}
	db, err := auradb.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("embed cache: open %q: %w", dbPath, err)
	}
	if err := migrations.Run(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("embed cache: migrate: %w", err)
	}
	c, err := NewEmbedCacheWithDB(db, model, inner, logger)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	c.owned = true
	return c, nil
}

// NewEmbedCacheWithDB wraps a caller-owned migrated SQLite pool.
func NewEmbedCacheWithDB(db *sql.DB, model string, inner EmbeddingFunc, logger *slog.Logger) (*EmbedCache, error) {
	return NewEmbedCacheWithBatchWithDB(db, model, inner, nil, logger)
}

func NewEmbedCacheWithBatchWithDB(db *sql.DB, model string, inner EmbeddingFunc, batchInner BatchEmbeddingFunction, logger *slog.Logger) (*EmbedCache, error) {
	if db == nil {
		return nil, errors.New("embed cache: db required")
	}
	return &EmbedCache{db: db, model: model, inner: inner, batchInner: batchInner, logger: logger, owned: false}, nil
}

// Close releases the SQLite handle. Idempotent.
func (c *EmbedCache) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	if !c.owned {
		return nil
	}
	return c.db.Close()
}

// Stats returns hit/miss counters for diagnostics.
func (c *EmbedCache) Stats() (hits, misses uint64) {
	return c.hits.Load(), c.misses.Load()
}

// Embed satisfies EmbeddingFunc. Cache hit short-circuits the
// upstream API call. Miss falls through to inner, then writes the
// result. Write failures are logged but never propagated — a degraded
// cache must not break embedding.
func (c *EmbedCache) Embed(ctx context.Context, text string) ([]float32, error) {
	if c == nil {
		return nil, errors.New("embed cache: nil receiver")
	}
	if vec, ok, err := c.cached(ctx, text); ok || err != nil {
		return vec, err
	}
	c.misses.Add(1)
	if c.inner == nil {
		return nil, errors.New("embed cache: miss with no upstream embedFn")
	}
	vec, err := c.inner(ctx, text)
	if err != nil {
		return nil, err
	}
	c.store(ctx, text, vec)
	return vec, nil
}

func (c *EmbedCache) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	if c == nil {
		return nil, errors.New("embed cache: nil receiver")
	}
	if len(texts) == 0 {
		return nil, nil
	}
	out := make([][]float32, len(texts))
	var missTexts []string
	var missIndexes []int
	for i, text := range texts {
		vec, ok, err := c.cached(ctx, text)
		if err != nil {
			return nil, err
		}
		if ok {
			out[i] = vec
			continue
		}
		c.misses.Add(1)
		missTexts = append(missTexts, text)
		missIndexes = append(missIndexes, i)
	}
	if len(missTexts) == 0 {
		return out, nil
	}
	var vectors [][]float32
	var err error
	if c.batchInner != nil {
		vectors, err = c.embedBatchAdaptive(ctx, missTexts)
	} else if c.inner != nil {
		vectors = make([][]float32, 0, len(missTexts))
		for _, text := range missTexts {
			vec, embedErr := c.inner(ctx, text)
			if embedErr != nil {
				err = embedErr
				break
			}
			vectors = append(vectors, vec)
		}
	} else {
		err = errors.New("embed cache: miss with no upstream embedFn")
	}
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(missTexts) {
		return nil, fmt.Errorf("embed cache: batch returned %d vectors for %d inputs", len(vectors), len(missTexts))
	}
	for i, vec := range vectors {
		if len(vec) == 0 {
			return nil, fmt.Errorf("embed cache: batch returned empty vector at index %d", i)
		}
		target := missIndexes[i]
		out[target] = vec
		c.store(ctx, missTexts[i], vec)
	}
	return out, nil
}

func (c *EmbedCache) embedBatchAdaptive(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += embedCacheBatchSize {
		end := start + embedCacheBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		vectors, err := c.embedBatchSplitOnFailure(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vectors...)
	}
	return out, nil
}

func (c *EmbedCache) embedBatchSplitOnFailure(ctx context.Context, texts []string) ([][]float32, error) {
	vectors, err := c.batchInner(ctx, texts)
	if err == nil {
		return vectors, nil
	}
	if len(texts) <= 1 {
		return nil, err
	}
	mid := len(texts) / 2
	left, leftErr := c.embedBatchSplitOnFailure(ctx, texts[:mid])
	if leftErr != nil {
		return nil, leftErr
	}
	right, rightErr := c.embedBatchSplitOnFailure(ctx, texts[mid:])
	if rightErr != nil {
		return nil, rightErr
	}
	return append(left, right...), nil
}

func (c *EmbedCache) cached(ctx context.Context, text string) ([]float32, bool, error) {
	key := contentSHA(text)

	var blob []byte
	row := c.db.QueryRowContext(ctx, "SELECT embedding FROM embedding_cache WHERE content_sha = ? AND model = ?", key, c.model)
	switch err := row.Scan(&blob); {
	case err == nil:
		c.hits.Add(1)
		vec, decErr := decodeFloats(blob)
		if decErr == nil {
			return vec, true, nil
		}
		// Corrupt entry: log + delete + fall through to fresh embed.
		c.logger.Warn("embed cache: corrupt blob, refetching", "sha", key, "error", decErr)
		_, _ = c.db.ExecContext(ctx, "DELETE FROM embedding_cache WHERE content_sha = ? AND model = ?", key, c.model)
	case errors.Is(err, sql.ErrNoRows):
		// proceed to miss
	default:
		c.logger.Warn("embed cache: lookup failed, embedding fresh", "error", err)
	}
	return nil, false, nil
}

func (c *EmbedCache) store(ctx context.Context, text string, vec []float32) {
	key := contentSHA(text)
	if _, err := c.db.ExecContext(ctx,
		"INSERT OR REPLACE INTO embedding_cache (content_sha, model, embedding, created_at) VALUES (?, ?, ?, ?)",
		key, c.model, encodeFloats(vec), time.Now().UTC(),
	); err != nil {
		c.logger.Warn("embed cache: write failed", "error", err)
	}
}

// EmbedFunc returns the cache's Embed method as a chromem-compatible
// closure. Convenience for wiring into NewEngineWithFallback.
func (c *EmbedCache) EmbedFunc() EmbeddingFunc {
	return c.Embed
}

func contentSHA(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// encodeFloats packs []float32 to little-endian bytes for SQLite BLOB
// storage. 4 bytes per float; for a 1024-dim Mistral vector that's 4
// KiB, comfortably within SQLite's per-row budget.
func encodeFloats(v []float32) []byte {
	out := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(f))
	}
	return out
}

func decodeFloats(b []byte) ([]float32, error) {
	if len(b)%4 != 0 {
		return nil, fmt.Errorf("embed cache: blob length %d not multiple of 4", len(b))
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out, nil
}
