//go:build web_integration

package web

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"
)

// TestDNSRebind is the SC#4 belt-and-suspenders against the live fixture (the
// primary deterministic SC#4 test is the injectable-resolver unit test
// TestDNSPin_TTL in dnspin_test.go). It fetches a real public host twice within
// the pin TTL through the SAME conversation and asserts the second fetch reuses
// the IP pinned by the first — so a rebind that flips the host's A record between
// the two fetches cannot redirect the second connection to a private target
// (D-25/T-07-12). It runs live, so it is gated on SEARXNG_URL the same way as the
// rest of the web_integration tier (the env presence is the "stack is up" signal;
// a real SearXNG instance is not required for the fetch path, but the tier as a
// whole is CI-gated by SEARXNG_URL — no-skip-as-green).
func TestDNSRebind(t *testing.T) {
	_ = envOrSkip(t, "SEARXNG_URL") // gate the whole tier on the stack being up
	if os.Getenv("CI") == "" && os.Getenv("SEARXNG_URL") == "" {
		t.Skip("local skip without stack")
	}

	c := liveClient(t)
	const conv = "rebind-conv"
	target := "https://en.wikipedia.org/wiki/Knowledge_graph"
	host := mustHost(t, target)

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()

	if _, err := c.Fetch(ctx, conv, target); err != nil {
		t.Fatalf("first live fetch failed: %v", err)
	}
	first, ok := c.pin.Pinned(conv, host)
	if !ok {
		t.Fatalf("expected a DNS pin for (%q,%q) after the first fetch", conv, host)
	}

	// A second fetch within TTL must reuse the same pinned IP.
	if _, err := c.Fetch(ctx, conv, target); err != nil {
		t.Fatalf("second live fetch failed: %v", err)
	}
	second, ok := c.pin.Pinned(conv, host)
	if !ok {
		t.Fatalf("pin disappeared between fetches for (%q,%q)", conv, host)
	}
	if first != second {
		t.Fatalf("pin not reused within TTL: first=%v second=%v (a rebind could redirect the second connection)", first, second)
	}
	// The pin must still be live well inside the 60s TTL.
	time.Sleep(50 * time.Millisecond)
	if _, ok := c.pin.Pinned(conv, host); !ok {
		t.Fatal("pin expired far too early")
	}
}

func mustHost(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u.Hostname()
}
