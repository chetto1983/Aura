package search

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"

	auradb "github.com/aura/aura/internal/db"
	"github.com/philippgille/chromem-go"
)

// counterFn is a stub embedFn that returns a deterministic vector and
// counts how many times it was actually invoked. The counter lets tests
// verify cache hits skip the upstream call entirely.
func counterFn(invocations *atomic.Uint64, vec []float32) chromem.EmbeddingFunc {
	return func(_ context.Context, _ string) ([]float32, error) {
		invocations.Add(1)
		out := make([]float32, len(vec))
		copy(out, vec)
		return out, nil
	}
}

func newCache(t *testing.T, model string, inner chromem.EmbeddingFunc) *EmbedCache {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "cache.db")
	c, err := OpenEmbedCache(dbPath, model, inner, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("OpenEmbedCache: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestEmbedCacheNamespace(t *testing.T) {
	got := EmbedCacheNamespace(" https://api.mistral.ai/v1/ ", " mistral-embed ")
	want := "https://api.mistral.ai/v1|mistral-embed"
	if got != want {
		t.Fatalf("EmbedCacheNamespace() = %q, want %q", got, want)
	}
	if got := EmbedCacheNamespace("", "mistral-embed"); got != "mistral-embed" {
		t.Fatalf("empty base URL namespace = %q", got)
	}
	if got := EmbedCacheNamespace("https://api.mistral.ai/v1", ""); got != "https://api.mistral.ai/v1" {
		t.Fatalf("empty model namespace = %q", got)
	}
}

func TestNewEmbedCacheWithDBCloseDoesNotCloseSharedDB(t *testing.T) {
	db, err := auradb.Open(filepath.Join(t.TempDir(), "shared-cache.db"))
	if err != nil {
		t.Fatalf("open shared db: %v", err)
	}
	defer db.Close()

	c, err := NewEmbedCacheWithDB(db, "mistral-embed", nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewEmbedCacheWithDB: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("cache Close: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("shared db was closed by cache Close: %v", err)
	}
}

func TestEmbedCache_HitSkipsUpstream(t *testing.T) {
	var invocations atomic.Uint64
	c := newCache(t, "mistral-embed", counterFn(&invocations, []float32{1, 2, 3}))

	// First call → miss → upstream called.
	v1, err := c.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("first Embed: %v", err)
	}
	if invocations.Load() != 1 {
		t.Fatalf("first call: invocations = %d, want 1", invocations.Load())
	}

	// Second call → hit → upstream NOT called again.
	v2, err := c.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("second Embed: %v", err)
	}
	if invocations.Load() != 1 {
		t.Fatalf("cache hit failed: invocations = %d, want still 1", invocations.Load())
	}
	if len(v1) != len(v2) {
		t.Fatalf("vector length mismatch: %d vs %d", len(v1), len(v2))
	}
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Errorf("v1[%d]=%v v2[%d]=%v", i, v1[i], i, v2[i])
		}
	}
	hits, misses := c.Stats()
	if hits != 1 || misses != 1 {
		t.Errorf("stats: hits=%d misses=%d, want 1/1", hits, misses)
	}
}

func TestEmbedCache_DifferentTextMisses(t *testing.T) {
	var invocations atomic.Uint64
	c := newCache(t, "mistral-embed", counterFn(&invocations, []float32{1}))

	if _, err := c.Embed(context.Background(), "alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Embed(context.Background(), "beta"); err != nil {
		t.Fatal(err)
	}
	if invocations.Load() != 2 {
		t.Fatalf("two distinct inputs should miss twice, got %d invocations", invocations.Load())
	}
}

func TestEmbedCache_ProviderNamespaceIsolation(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cache.db")
	model := "mistral-embed"
	nsA := EmbedCacheNamespace("https://api.mistral.ai/v1", model)
	nsB := EmbedCacheNamespace("https://example.test/v1", model)

	var invocationsA atomic.Uint64
	cA, err := OpenEmbedCache(dbPath, nsA, counterFn(&invocationsA, []float32{1, 2}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cA.Embed(context.Background(), "shared text"); err != nil {
		t.Fatal(err)
	}
	_ = cA.Close()

	var invocationsB atomic.Uint64
	cB, err := OpenEmbedCache(dbPath, nsB, counterFn(&invocationsB, []float32{3, 4}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer cB.Close()
	if _, err := cB.Embed(context.Background(), "shared text"); err != nil {
		t.Fatal(err)
	}
	if invocationsB.Load() != 1 {
		t.Fatalf("different provider namespace should miss, got %d invocations", invocationsB.Load())
	}
}

func TestEmbedCache_ModelKeyIsolation(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cache.db")
	var invocationsA atomic.Uint64
	cA, err := OpenEmbedCache(dbPath, "model-a", counterFn(&invocationsA, []float32{1, 2}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cA.Embed(context.Background(), "shared text"); err != nil {
		t.Fatal(err)
	}
	_ = cA.Close()

	// Reopen with a different model — must NOT hit the cached row.
	var invocationsB atomic.Uint64
	cB, err := OpenEmbedCache(dbPath, "model-b", counterFn(&invocationsB, []float32{1, 2}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer cB.Close()
	if _, err := cB.Embed(context.Background(), "shared text"); err != nil {
		t.Fatal(err)
	}
	if invocationsB.Load() != 1 {
		t.Fatalf("different model should miss, got %d invocations", invocationsB.Load())
	}
}

func TestEmbedCache_PersistsAcrossOpens(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cache.db")
	var invocations1 atomic.Uint64
	c1, err := OpenEmbedCache(dbPath, "mistral-embed", counterFn(&invocations1, []float32{1, 2, 3}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c1.Embed(context.Background(), "persisted"); err != nil {
		t.Fatal(err)
	}
	_ = c1.Close()

	// Second process — fresh in-memory state — must hit the row written above.
	var invocations2 atomic.Uint64
	c2, err := OpenEmbedCache(dbPath, "mistral-embed", counterFn(&invocations2, []float32{1, 2, 3}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	if _, err := c2.Embed(context.Background(), "persisted"); err != nil {
		t.Fatal(err)
	}
	if invocations2.Load() != 0 {
		t.Fatalf("warm-restart should hit cache without invoking upstream, got %d", invocations2.Load())
	}
	hits, _ := c2.Stats()
	if hits != 1 {
		t.Errorf("hits=%d, want 1", hits)
	}
}

func TestEmbedCache_FloatRoundTrip(t *testing.T) {
	cases := [][]float32{
		{0, 1, -1, 3.14159, -2.71828},
		{1e-30, 1e30, -1e-30, -1e30},
		{},
	}
	for i, c := range cases {
		blob := encodeFloats(c)
		if len(blob) != 4*len(c) {
			t.Errorf("case %d: blob len = %d, want %d", i, len(blob), 4*len(c))
		}
		got, err := decodeFloats(blob)
		if err != nil {
			t.Fatalf("case %d: decode: %v", i, err)
		}
		if len(got) != len(c) {
			t.Errorf("case %d: round-trip len = %d, want %d", i, len(got), len(c))
		}
		for j := range c {
			if got[j] != c[j] {
				t.Errorf("case %d idx %d: got %v want %v", i, j, got[j], c[j])
			}
		}
	}
}

func TestEmbedCache_CorruptBlobIsRecovered(t *testing.T) {
	var invocations atomic.Uint64
	c := newCache(t, "mistral-embed", counterFn(&invocations, []float32{1, 2, 3}))
	// Inject a bogus row whose blob length isn't a multiple of 4.
	if _, err := c.db.Exec(
		`INSERT INTO embedding_cache(content_sha, model, embedding, created_at) VALUES (?, ?, ?, ?)`,
		contentSHA("hi"), "mistral-embed", []byte{0x01, 0x02, 0x03}, "2026-01-01T00:00:00Z",
	); err != nil {
		t.Fatal(err)
	}
	v, err := c.Embed(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Embed should recover from corrupt blob: %v", err)
	}
	if len(v) != 3 || invocations.Load() != 1 {
		t.Errorf("recovery path didn't re-embed correctly: vec=%v invocations=%d", v, invocations.Load())
	}
}

func TestEmbedCache_UpstreamErrorIsReturned(t *testing.T) {
	c := newCache(t, "mistral-embed", func(_ context.Context, _ string) ([]float32, error) {
		return nil, errors.New("api down")
	})
	if _, err := c.Embed(context.Background(), "anything"); err == nil {
		t.Fatal("expected upstream error to propagate")
	}
}

func TestEmbedCache_NilUpstreamErrorsOnMiss(t *testing.T) {
	c := newCache(t, "mistral-embed", nil)
	if _, err := c.Embed(context.Background(), "miss"); err == nil {
		t.Fatal("expected error on miss with nil upstream")
	}
}
