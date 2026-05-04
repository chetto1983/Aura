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

	"github.com/philippgille/chromem-go"
	_ "modernc.org/sqlite" // SQLite driver
)

// EmbedCacheNamespace returns the provider-scoped cache namespace for
// embedding vectors. Model alone is not enough: OpenAI-compatible endpoints can
// expose the same model name with different dimensions or semantics.
func EmbedCacheNamespace(baseURL, model string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	model = strings.TrimSpace(model)
	if baseURL == "" {
		return model
	}
	if model == "" {
		return baseURL
	}
	return baseURL + "|" + model
}

// EmbedCache wraps a chromem.EmbeddingFunc with a SQLite-backed
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
	db     *sql.DB
	model  string
	inner  chromem.EmbeddingFunc
	logger *slog.Logger
	hits   atomic.Uint64
	misses atomic.Uint64
}

// OpenEmbedCache opens (or creates) the cache table on dbPath. If
// inner is nil, the cache short-circuits to an error on miss — useful
// for tests where we want to verify cache hits without spinning up a
// real embedding provider.
func OpenEmbedCache(dbPath, model string, inner chromem.EmbeddingFunc, logger *slog.Logger) (*EmbedCache, error) {
	if dbPath == "" {
		return nil, errors.New("embed cache: dbPath required")
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("embed cache: open %q: %w", dbPath, err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS embedding_cache (
		content_sha TEXT NOT NULL,
		model       TEXT NOT NULL,
		embedding   BLOB NOT NULL,
		created_at  TIMESTAMP NOT NULL,
		PRIMARY KEY (content_sha, model)
	)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("embed cache: create table: %w", err)
	}
	return &EmbedCache{db: db, model: model, inner: inner, logger: logger}, nil
}

// Close releases the SQLite handle. Idempotent.
func (c *EmbedCache) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

// Stats returns hit/miss counters for diagnostics.
func (c *EmbedCache) Stats() (hits, misses uint64) {
	return c.hits.Load(), c.misses.Load()
}

// Embed satisfies chromem.EmbeddingFunc. Cache hit short-circuits the
// upstream API call. Miss falls through to inner, then writes the
// result. Write failures are logged but never propagated — a degraded
// cache must not break embedding.
func (c *EmbedCache) Embed(ctx context.Context, text string) ([]float32, error) {
	if c == nil {
		return nil, errors.New("embed cache: nil receiver")
	}
	key := contentSHA(text)

	var blob []byte
	row := c.db.QueryRowContext(ctx, "SELECT embedding FROM embedding_cache WHERE content_sha = ? AND model = ?", key, c.model)
	switch err := row.Scan(&blob); {
	case err == nil:
		c.hits.Add(1)
		vec, decErr := decodeFloats(blob)
		if decErr == nil {
			return vec, nil
		}
		// Corrupt entry: log + delete + fall through to fresh embed.
		c.logger.Warn("embed cache: corrupt blob, refetching", "sha", key, "error", decErr)
		_, _ = c.db.ExecContext(ctx, "DELETE FROM embedding_cache WHERE content_sha = ? AND model = ?", key, c.model)
	case errors.Is(err, sql.ErrNoRows):
		// proceed to miss
	default:
		c.logger.Warn("embed cache: lookup failed, embedding fresh", "error", err)
	}

	c.misses.Add(1)
	if c.inner == nil {
		return nil, errors.New("embed cache: miss with no upstream embedFn")
	}
	vec, err := c.inner(ctx, text)
	if err != nil {
		return nil, err
	}
	if _, err := c.db.ExecContext(ctx,
		"INSERT OR REPLACE INTO embedding_cache (content_sha, model, embedding, created_at) VALUES (?, ?, ?, ?)",
		key, c.model, encodeFloats(vec), time.Now().UTC(),
	); err != nil {
		c.logger.Warn("embed cache: write failed", "error", err)
	}
	return vec, nil
}

// EmbedFunc returns the cache's Embed method as a chromem-compatible
// closure. Convenience for wiring into NewEngineWithFallback.
func (c *EmbedCache) EmbedFunc() chromem.EmbeddingFunc {
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
