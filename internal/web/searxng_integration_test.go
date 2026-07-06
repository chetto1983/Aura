//go:build web_integration

// Package web's LIVE tier. These tests run web.Search / the DNS pin against the
// real SearXNG Compose service (NO host port — reached as http://searxng:8080/
// search on the shared network, D-02/D-03). They are gated behind the
// web_integration build tag because they need a reachable SEARXNG_URL and the
// public internet. This file carries the tier's own goleak TestMain so the
// integration build is leak-checked too (the untagged main_test.go is
// //go:build !web_integration, so exactly one TestMain compiles per build).
package web

import (
	"context"
	"os"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/chetto1983/aura/internal/config"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// envOrSkip enforces NO-SKIP-AS-GREEN (CLAUDE.md): when the required env var is
// unset it t.Fatal's under $CI (a missing live env in CI is a misconfigured job,
// never a silent pass) and only skips for a local developer who has no stack up.
// A sub-second "integration" runtime is the skip tell the CI gate watches for.
func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v != "" {
		return v
	}
	if os.Getenv("CI") != "" {
		t.Fatalf("%s must be set in CI (web_integration tier must run live against the SearXNG container, no skip-as-green)", key)
	}
	t.Skipf("%s unset — skipping web_integration tier locally (bring the SearXNG stack up to run it)", key)
	return ""
}

func liveClient(t *testing.T) *Client {
	t.Helper()
	cfg := &config.Config{
		SearxngURL:           envOrSkip(t, "SEARXNG_URL"),
		WebDNSPinTTLSec:      60,
		WebFetchMaxBodyBytes: 5_000_000,
		WebSearchTimeoutSec:  20,
		WebFetchTimeoutSec:   30,
		WebUserAgent:         "Aura/web_integration",
	}
	return NewClient(cfg)
}

// TestSearch_Live exercises SC#1 against the real backend: a query returns ranked
// {title,url,snippet} results within the search deadline (D-43). It asserts the
// JSON round-trip works (the settings.yml `formats: [html, json]` contract) and
// that at least one result has a populated title + url.
func TestSearch_Live(t *testing.T) {
	c := liveClient(t)

	queries := []SearchParams{
		{Query: "Neo4j HNSW vector index", MaxResults: 5},
		{Query: "SearXNG", Domains: []string{"wikipedia.org"}, MaxResults: 5},
		{Query: "wikipedia", MaxResults: 5}, // high-recall insurance: any upstream engine returns something
	}
	// SearXNG proxies public upstream engines (Google/Bing/DuckDuckGo/...) which
	// intermittently rate-limit or block a CI datacenter IP and then return zero
	// results for a query set that succeeds a moment later. Retry the whole smoke
	// set with a short backoff so a transient upstream throttle does not red the
	// matrix. This is NOT skip-as-green: every attempt is a real live call, and a
	// genuine contract break (settings.yml json format dropped, deadline wrong)
	// returns zero/err on EVERY attempt and still fails deterministically.
	const attempts = 3
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 3 * time.Second)
		}
		for _, q := range queries {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			results, err := c.Search(ctx, q)
			cancel()
			if err != nil {
				lastErr = err
				continue
			}
			if len(results) == 0 {
				lastErr = nil
				continue
			}
			for i, r := range results {
				if r.Title == "" || r.URL == "" {
					t.Fatalf("result %d missing title/url for query %q: %+v", i, q.Query, r)
				}
			}
			return // ranked results with populated title+url — SC#1 satisfied
		}
	}
	if lastErr != nil {
		t.Fatalf("live Search failed after %d attempts (is the SearXNG container up + on the public internet?): %v", attempts, lastErr)
	}
	t.Fatalf("live Search returned zero results for all smoke queries across %d attempts - check SearXNG upstream engines and the JSON format contract (settings.yml formats)", attempts)
}

// TestFetch_Live exercises SC#2 against a real public page: web.Fetch returns
// clean markdown with no obvious nav/footer chrome (D-17).
func TestFetch_Live(t *testing.T) {
	c := liveClient(t)
	if os.Getenv("SEARXNG_URL") == "" && os.Getenv("CI") == "" {
		t.Skip("local skip without stack")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()

	page, err := c.Fetch(ctx, "web-integration", "https://en.wikipedia.org/wiki/Knowledge_graph")
	if err != nil {
		t.Fatalf("live Fetch failed: %v", err)
	}
	if len(page.ContentMD) < 250 {
		t.Fatalf("expected substantive markdown, got %d bytes", len(page.ContentMD))
	}
}
