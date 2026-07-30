// Package rerank is Aura's fail-soft client for the optional GPU cross-encoder
// reranker sidecar (aura-rerank — Qwen3-Reranker-0.6B Q4_K_M on llama.cpp). It
// mirrors internal/documents.EmbeddingClient: stdlib net/http only, no
// third-party rerank/HTTP SDK and no native graph driver.
//
// Contract (spike 070 / RET-01): Rerank reorders documents by descending
// relevance on success, and on EVERY failure mode — empty base URL, transport
// error, non-2xx, decode error, result/doc length mismatch, or an out-of-range
// index — returns the input order unchanged (identity) with a nil error. A
// missing or GPU-absent sidecar therefore degrades retrieval to the upstream
// RRF/vector order and never blocks the caller. The degradation is logged at
// most once per process, and the log never carries document text or secrets.
package rerank

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// maxRerankDocChars caps each document's on-the-wire length in runes. The spike
// fast-path truncation (~480) keeps each query/doc pair within the sidecar's
// -c 2048 context window; the returned Scored.Document always carries the
// ORIGINAL untruncated text.
const maxRerankDocChars = 480

// defaultRerankModel is sent when RerankClient.Model is empty. llama.cpp serves
// whatever model was loaded and ignores an unknown id, mirroring the embedder's
// inputModel default.
const defaultRerankModel = "aura-rerank"

// rerankTimeout is the default client deadline when RerankClient.Client is nil.
// GPU rerank is sub-second (spike 070: ~300ms for 10 short docs); 30s is ample
// headroom for a cold first call without wedging a caller.
const rerankTimeout = 30 * time.Second

// Successful reranks are deterministic for the endpoint/model/query/document
// payload. Keeping a short, bounded score cache avoids repeating seconds of GPU
// work when a user retries the exact same retrieval request. Fail-soft responses
// are deliberately never cached.
const (
	rerankCacheTTL        = 5 * time.Minute
	rerankCacheMaxEntries = 256
)

// warnOnce guards the single degraded-path warning per process so a sidecar that
// is down for an entire run logs once, not once per retrieval.
var warnOnce sync.Once

// Scored is one reranked document. Degraded distinguishes fail-soft identity
// output from a successful sidecar response whose genuine relevance score is zero.
type Scored struct {
	Index    int
	Document string
	Score    float64
	Degraded bool
}

type cachedScore struct {
	Index int
	Score float64
}

type rerankCacheEntry struct {
	Scores  []cachedScore
	Expires time.Time
	Access  uint64
}

// RerankClient calls Aura's optional OpenAI-style rerank endpoint (/v1/rerank),
// mirroring documents.EmbeddingClient construction. A nil Client uses a default
// http.Client with rerankTimeout. APIKey is the config-only local↔cloud swap
// (D-28, mirroring the vision route): empty for the local sidecar (no auth), or a
// Bearer key for a cloud reranker (e.g. OpenRouter's cohere/rerank-4-fast). It is
// set ONLY on the Authorization header at request-build time, never logged.
type RerankClient struct {
	BaseURL string
	Model   string
	APIKey  string
	Client  *http.Client

	cacheMu       sync.Mutex
	cache         map[[sha256.Size]byte]rerankCacheEntry
	cacheSequence uint64
	now           func() time.Time
}

// Rerank reorders docs by descending relevance for query, POSTing to
// BaseURL+"/v1/rerank". It is FAIL-SOFT: any failure (empty BaseURL, transport
// error, non-2xx, decode error, result/doc length mismatch, or an out-of-range
// index) returns identity(docs) with a nil error and logs once. An empty docs
// slice returns an empty result with no HTTP call. The error result is reserved
// for future use; the current implementation never returns a non-nil error so a
// caller can treat rerank as strictly best-effort.
func (c *RerankClient) Rerank(ctx context.Context, query string, docs []string) ([]Scored, error) {
	if len(docs) == 0 {
		return identity(docs), nil
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		return degrade(docs, "base URL empty (rerank disabled)"), nil
	}

	body, err := json.Marshal(map[string]any{
		"model":     rerankModel(c.Model),
		"query":     query,
		"documents": truncatedDocs(docs),
	})
	if err != nil {
		return degrade(docs, "request marshal failed"), nil
	}
	cacheMaterial := append(
		[]byte(strings.TrimRight(c.BaseURL, "/")+"\x00"),
		body...,
	)
	cacheKey := sha256.Sum256(cacheMaterial)
	if cached, ok := c.cached(cacheKey, docs); ok {
		return cached, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/v1/rerank", bytes.NewReader(body))
	if err != nil {
		return degrade(docs, "request build failed"), nil
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey) // cloud rerank route only (D-28)
	}

	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: rerankTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return degrade(docs, "sidecar transport error"), nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return degrade(docs, "sidecar non-2xx status"), nil
	}

	var decoded struct {
		Results []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
		} `json:"results"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return degrade(docs, "sidecar decode error"), nil
	}
	if len(decoded.Results) != len(docs) {
		return degrade(docs, "result/doc length mismatch"), nil
	}

	out := make([]Scored, 0, len(decoded.Results))
	for _, r := range decoded.Results {
		if r.Index < 0 || r.Index >= len(docs) {
			return degrade(docs, "out-of-range result index"), nil
		}
		out = append(out, Scored{Index: r.Index, Document: docs[r.Index], Score: r.RelevanceScore})
	}
	slices.SortStableFunc(out, func(a, b Scored) int { return cmp.Compare(b.Score, a.Score) })
	c.storeCached(cacheKey, out)
	return out, nil
}

// cached reconstructs documents from the current call: the bounded cache retains
// only indices and scores, never full user document text.
func (c *RerankClient) cached(
	key [sha256.Size]byte,
	docs []string,
) ([]Scored, bool) {
	now := c.cacheNow()
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	entry, ok := c.cache[key]
	if !ok {
		return nil, false
	}
	if !entry.Expires.After(now) || len(entry.Scores) != len(docs) {
		delete(c.cache, key)
		return nil, false
	}
	c.cacheSequence++
	entry.Access = c.cacheSequence
	c.cache[key] = entry

	out := make([]Scored, len(entry.Scores))
	for i, cached := range entry.Scores {
		if cached.Index < 0 || cached.Index >= len(docs) {
			delete(c.cache, key)
			return nil, false
		}
		out[i] = Scored{
			Index: cached.Index, Document: docs[cached.Index], Score: cached.Score,
		}
	}
	return out, true
}

func (c *RerankClient) storeCached(
	key [sha256.Size]byte,
	scored []Scored,
) {
	now := c.cacheNow()
	scores := make([]cachedScore, len(scored))
	for i, score := range scored {
		scores[i] = cachedScore{Index: score.Index, Score: score.Score}
	}

	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	if c.cache == nil {
		c.cache = make(map[[sha256.Size]byte]rerankCacheEntry)
	}
	for candidate, entry := range c.cache {
		if !entry.Expires.After(now) {
			delete(c.cache, candidate)
		}
	}
	if _, exists := c.cache[key]; !exists && len(c.cache) >= rerankCacheMaxEntries {
		var victim [sha256.Size]byte
		var oldest uint64
		first := true
		for candidate, entry := range c.cache {
			if first || entry.Access < oldest {
				victim, oldest, first = candidate, entry.Access, false
			}
		}
		delete(c.cache, victim)
	}
	c.cacheSequence++
	c.cache[key] = rerankCacheEntry{
		Scores: scores, Expires: now.Add(rerankCacheTTL), Access: c.cacheSequence,
	}
}

func (c *RerankClient) cacheNow() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// identity returns docs in input order with score 0 — the fail-soft no-op
// ordering, which IS the upstream RRF/vector order the caller already had.
func identity(docs []string) []Scored {
	out := make([]Scored, 0, len(docs))
	for i, d := range docs {
		out = append(out, Scored{
			Index: i, Document: d, Score: 0, Degraded: true,
		})
	}
	return out
}

// degrade logs the fail-soft reason once per process and returns identity order.
// reason is a short static string — never document text, query, or secrets.
func degrade(docs []string, reason string) []Scored {
	warnOnce.Do(func() {
		slog.Warn("rerank: degraded to identity order (further occurrences suppressed)", "reason", reason)
	})
	return identity(docs)
}

// truncatedDocs caps each document to maxRerankDocChars runes for the wire body
// only; the original text is preserved for the returned Scored.Document.
func truncatedDocs(docs []string) []string {
	out := make([]string, len(docs))
	for i, d := range docs {
		out[i] = truncateRunes(d, maxRerankDocChars)
	}
	return out
}

// truncateRunes caps s to max runes for the wire body (no ellipsis — the sidecar only
// needs the prefix within its context window). It is a near-duplicate of
// internal/assets/context.go truncateRunes, which appends "..." for human-readable
// summaries — folding the two into a shared internal/strutil is deferred (QUAL-02 T8 /
// OQ#2): they differ by that suffix, and a new package for a 5-liner would itself need
// coverage-gate registration, so the audit accepts the dup at this scale.
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

func rerankModel(model string) string {
	if strings.TrimSpace(model) == "" {
		return defaultRerankModel
	}
	return model
}
