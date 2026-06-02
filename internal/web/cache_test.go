//go:build !web_integration

package web

import (
	"net/url"
	"testing"
	"time"
)

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

func TestCache_TTL(t *testing.T) {
	clock := time.Unix(1000, 0)
	c := newCache(false, time.Minute)
	c.now = func() time.Time { return clock }

	key := cacheKey("fetch", "https://x.test/a")
	c.set(key, []byte("payload"), 30*time.Second)

	if got, ok := c.get(key); !ok || string(got) != "payload" {
		t.Fatalf("fresh entry should hit: ok=%v got=%q", ok, got)
	}

	clock = clock.Add(31 * time.Second) // past the entry TTL
	if _, ok := c.get(key); ok {
		t.Errorf("expired entry must miss")
	}
}

func TestCache_DefaultTTLFallback(t *testing.T) {
	clock := time.Unix(2000, 0)
	c := newCache(false, 10*time.Second)
	c.now = func() time.Time { return clock }

	key := cacheKey("fetch", "https://y.test/b")
	c.set(key, []byte("v"), 0) // non-positive → defaultTTL (10s)

	clock = clock.Add(5 * time.Second)
	if _, ok := c.get(key); !ok {
		t.Errorf("within default TTL should hit")
	}
	clock = clock.Add(6 * time.Second) // total 11s > 10s default
	if _, ok := c.get(key); ok {
		t.Errorf("past default TTL should miss")
	}
}
