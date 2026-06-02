package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// defaultCacheTTL is the bounded fallback lifetime for a cached entry when the
// response carries no usable Cache-Control max-age (D-33). It is deliberately
// short — the cache exists to collapse duplicate calls within one task, not to act
// as a durable store.
const defaultCacheTTL = 5 * time.Minute

// cacheEntry is one stored value with its absolute expiry. The payload is opaque
// bytes so the same cache backs both the search-results JSON and the fetched
// markdown Page (each caller marshals/unmarshals its own shape).
type cacheEntry struct {
	Payload []byte    `json:"payload"`
	Expires time.Time `json:"expires"`
}

// cache is the in-memory TTL response cache (default) with an optional disk tier
// gated by AURA_WEB_CACHE_PERSISTENT (D-31/D-32). The disk path is a best-effort
// mirror: a write/read failure degrades to the in-memory map, never an error to
// the caller. The clock is injectable so the TTL test is deterministic.
type cache struct {
	mu         sync.Mutex
	m          map[string]cacheEntry
	defaultTTL time.Duration
	persistent bool
	dir        string
	now        func() time.Time
}

// newCache builds the cache. When persistent is true it resolves a per-user disk
// directory under the OS cache root; if that cannot be created it silently falls
// back to memory-only so a read-only home never breaks a fetch (D-32).
func newCache(persistent bool, defaultTTL time.Duration) *cache {
	c := &cache{
		m:          make(map[string]cacheEntry),
		defaultTTL: defaultTTL,
		persistent: persistent,
		now:        time.Now,
	}
	if persistent {
		if base, err := os.UserCacheDir(); err == nil {
			dir := filepath.Join(base, "aura", "web-cache")
			if mkErr := os.MkdirAll(dir, 0o700); mkErr == nil {
				c.dir = dir
			} else {
				c.persistent = false
			}
		} else {
			c.persistent = false
		}
	}
	return c
}

// cacheKey derives a filesystem-safe, collision-resistant key from a namespace and
// a raw key (a URL or an encoded query). The namespace keeps search and fetch
// entries from colliding on a shared raw string.
func cacheKey(namespace, raw string) string {
	sum := sha256.Sum256([]byte(namespace + "\x00" + raw))
	return hex.EncodeToString(sum[:])
}

// get returns the live payload for key, or (nil,false) on a miss or an expired
// entry. An expired in-memory entry is dropped; the disk tier is consulted only
// when the memory map misses (and a stale disk entry is treated as a miss).
func (c *cache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[key]; ok {
		if c.now().Before(e.Expires) {
			return e.Payload, true
		}
		delete(c.m, key)
	}
	if !c.persistent {
		return nil, false
	}
	return c.getDiskLocked(key)
}

// set stores payload under key with the given TTL. A non-positive TTL falls back
// to defaultTTL so an entry always has a bounded life (D-33). The disk mirror is
// best-effort.
func (c *cache) set(key string, payload []byte, ttl time.Duration) {
	if ttl <= 0 {
		ttl = c.defaultTTL
	}
	e := cacheEntry{Payload: payload, Expires: c.now().Add(ttl)}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = e
	if c.persistent {
		c.setDiskLocked(key, e)
	}
}

// getDiskLocked reads and validates a disk entry under the held mutex. A decode or
// I/O failure, or an expired entry, is a miss (the file is left for set to
// overwrite). The caller holds c.mu.
func (c *cache) getDiskLocked(key string) ([]byte, bool) {
	// key is always a sha256 hex digest from cacheKey (no caller-controlled path
	// component), so no traversal is possible here.
	b, err := os.ReadFile(filepath.Join(c.dir, key+".json")) // #nosec G304 -- key is a sha256 hex digest
	if err != nil {
		return nil, false
	}
	var e cacheEntry
	if json.Unmarshal(b, &e) != nil || !c.now().Before(e.Expires) {
		return nil, false
	}
	c.m[key] = e // promote to memory
	return e.Payload, true
}

// setDiskLocked writes the entry to disk best-effort under the held mutex. A
// failure is ignored — the in-memory copy already holds the value.
func (c *cache) setDiskLocked(key string, e cacheEntry) {
	if b, err := json.Marshal(e); err == nil {
		_ = os.WriteFile(filepath.Join(c.dir, key+".json"), b, 0o600)
	}
}
