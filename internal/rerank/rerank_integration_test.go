//go:build rerank_integration

// Package rerank's LIVE tier. Runs RerankClient.Rerank against the real
// aura-rerank GPU sidecar (Qwen3-Reranker-0.6B Q4_K_M on llama.cpp). Gated behind
// the rerank_integration build tag because it needs a reachable
// AURA_RERANK_BASE_URL and a GPU. It enforces NO-SKIP-AS-GREEN (CLAUDE.md): when
// the base URL is unset it t.Fatal's under $CI and only skips for a local
// developer with no GPU sidecar up. The sidecar-down identity fallback is covered
// by the unit tests (client_test.go), not here.
package rerank

import (
	"os"
	"slices"
	"testing"
	"time"
)

// envOrSkipCI returns the env value, t.Fatal's under $CI when it is unset (a
// missing live env in CI is a misconfigured job, never a silent green pass), and
// skips locally otherwise. GitHub Actions always sets CI=true.
func envOrSkipCI(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v != "" {
		return v
	}
	if os.Getenv("CI") != "" {
		t.Fatalf("%s must be set in CI (rerank_integration tier must run live against the aura-rerank sidecar; a skipped tier must never pass as green)", key)
	}
	t.Skipf("%s unset — skipping rerank_integration tier locally (bring the aura-rerank GPU sidecar up to run it)", key)
	return ""
}

// TestRerankLive exercises RET-01 against the live sidecar on spike 070's
// injected-answer case (the torque TOC/lexical false-positive): pure vector
// retrieval surfaces the table-of-contents line, and the cross-encoder must lift
// the actual instruction to rank #1. It also asserts the p95 latency budget.
func TestRerankLive(t *testing.T) {
	baseURL := envOrSkipCI(t, "AURA_RERANK_BASE_URL")
	c := &RerankClient{BaseURL: baseURL}

	const query = "tightening torque specification for the terminals"
	answer := "Mount the terminal covers. Tighten the screws with a tightening torque of 2 Nm."
	docs := []string{
		"4.11.18.4 Load monitoring for torque ......................... 623",
		"Table of contents: safety information, commissioning, maintenance.",
		answer,
		"The ambient operating temperature must be between -10 and +40 C.",
		"Connect the line supply mains cables to terminals L1, L2, L3.",
	}

	ctx := t.Context()
	got, err := c.Rerank(ctx, query, docs)
	if err != nil {
		t.Fatalf("live Rerank err = %v, want nil", err)
	}
	if len(got) != len(docs) {
		t.Fatalf("live Rerank returned %d scored, want %d", len(got), len(docs))
	}
	if got[0].Document != answer {
		t.Fatalf("live rerank #1 = %q (score %.4f), want the instruction doc; full order: %#v", got[0].Document, got[0].Score, got)
	}

	// p95 over N>=5 short-doc reranks must beat 400ms on GPU (spike 070 Q4).
	const n = 8
	lat := make([]time.Duration, 0, n)
	for range n {
		start := time.Now()
		if _, err := c.Rerank(ctx, query, docs); err != nil {
			t.Fatalf("live Rerank (timing) err = %v", err)
		}
		lat = append(lat, time.Since(start))
	}
	p95 := percentile95(lat)
	t.Logf("live rerank p95 over %d runs = %s (top1 score %.4f)", n, p95, got[0].Score)
	if p95 >= 400*time.Millisecond {
		t.Fatalf("live rerank p95 %s exceeds the 400ms budget", p95)
	}
}

func percentile95(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), values...)
	slices.Sort(sorted)
	return sorted[int(float64(len(sorted)-1)*0.95)]
}
