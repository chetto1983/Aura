//go:build !web_integration

package web

import (
	"net/url"
	"os"
	"path/filepath"
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

// diskCache builds a persistent cache pointed at a temp dir with a frozen clock so
// the disk tier (getDiskLocked/setDiskLocked) round-trips deterministically. It
// constructs via the real newCache so the persistent flag and JSON shape are
// exercised, then redirects dir/now for hermeticity (newCache's own dir is
// os.UserCacheDir which is neither hermetic nor TTL-deterministic).
func diskCache(t *testing.T, ttl time.Duration, clock *time.Time) *cache {
	t.Helper()
	c := newCache(true, ttl)
	if !c.persistent {
		t.Fatalf("persistent cache should not have degraded to memory-only in test env")
	}
	c.dir = t.TempDir()
	c.now = func() time.Time { return *clock }
	return c
}

func TestCache_DiskRoundTrip(t *testing.T) {
	clock := time.Unix(3000, 0)
	c := diskCache(t, time.Minute, &clock)

	key := cacheKey("fetch", "https://disk.test/a")
	c.set(key, []byte("disk-payload"), 30*time.Second)

	// setDiskLocked must have mirrored the entry to a <key>.json file.
	path := filepath.Join(c.dir, key+".json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("disk mirror file should exist: %v", err)
	}

	// In-memory hit first (does not touch disk).
	if got, ok := c.get(key); !ok || string(got) != "disk-payload" {
		t.Fatalf("in-memory hit: ok=%v got=%q", ok, got)
	}

	// Fresh cache instance pointed at the SAME dir reads the entry back off disk
	// (getDiskLocked promotes it to memory) — the persistence round-trip.
	c2 := newCache(true, time.Minute)
	c2.dir = c.dir
	c2.now = func() time.Time { return clock }
	got, ok := c2.get(key)
	if !ok || string(got) != "disk-payload" {
		t.Fatalf("fresh instance disk read: ok=%v got=%q", ok, got)
	}
	if _, inMem := c2.m[key]; !inMem {
		t.Errorf("getDiskLocked should promote the entry into the memory map")
	}
}

func TestCache_DiskTTLExpiry(t *testing.T) {
	clock := time.Unix(4000, 0)
	c := diskCache(t, time.Minute, &clock)

	key := cacheKey("fetch", "https://disk.test/b")
	c.set(key, []byte("v"), 30*time.Second)

	// A fresh instance reading past the disk entry's expiry must treat it as a miss
	// (getDiskLocked's TTL check), even though the file is still present.
	expired := clock.Add(31 * time.Second)
	c2 := newCache(true, time.Minute)
	c2.dir = c.dir
	c2.now = func() time.Time { return expired }
	if _, ok := c2.get(key); ok {
		t.Errorf("expired disk entry must miss")
	}
	if _, err := os.Stat(filepath.Join(c.dir, key+".json")); err != nil {
		t.Errorf("expired disk file is left for overwrite, not deleted: %v", err)
	}
}

func TestCache_DiskMissOnAbsentFile(t *testing.T) {
	clock := time.Unix(5000, 0)
	c := diskCache(t, time.Minute, &clock)
	// Nothing written: getDiskLocked hits os.ReadFile's error path → clean miss.
	if _, ok := c.get(cacheKey("fetch", "https://disk.test/never")); ok {
		t.Errorf("absent disk file must be a miss, not a hit")
	}
}
