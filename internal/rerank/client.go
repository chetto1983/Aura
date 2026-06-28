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

// warnOnce guards the single degraded-path warning per process so a sidecar that
// is down for an entire run logs once, not once per retrieval.
var warnOnce sync.Once

// Scored is one reranked document: its original Index in the input slice, the
// original (untruncated) Document text, and the sidecar relevance Score.
type Scored struct {
	Index    int
	Document string
	Score    float64
}

// RerankClient calls Aura's optional OpenAI-style rerank sidecar (/v1/rerank),
// mirroring documents.EmbeddingClient construction. A nil Client uses a default
// http.Client with rerankTimeout.
type RerankClient struct {
	BaseURL string
	Model   string
	Client  *http.Client
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/v1/rerank", bytes.NewReader(body))
	if err != nil {
		return degrade(docs, "request build failed"), nil
	}
	req.Header.Set("Content-Type", "application/json")

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
	return out, nil
}

// identity returns docs in input order with score 0 — the fail-soft no-op
// ordering, which IS the upstream RRF/vector order the caller already had.
func identity(docs []string) []Scored {
	out := make([]Scored, 0, len(docs))
	for i, d := range docs {
		out = append(out, Scored{Index: i, Document: d, Score: 0})
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
