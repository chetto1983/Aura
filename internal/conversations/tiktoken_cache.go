package conversations

import (
	"hash/maphash"
	"sync"
)

// tokenCounts is the process's memory of what countTokens computed. The ladder recounts
// the replayed history on every turn of every conversation, and since the history only grows
// at its end, a turn costs a hash per old message instead of a full encode.
//
// A count is keyed by a 64-bit hash of the text and its length, not by the text, so the cache
// holds no conversation content. Two texts of one length colliding would give one of them
// the other's count: at the cache's size that is about one chance in 10^9 over its whole life,
// for a number that is already an estimate.
var tokenCounts = newTokenCountCache(1 << 15)

type tokenCountKey struct {
	hash uint64
	size int
}

// tokenCountCache keeps two generations of at most size counts each. A full generation
// becomes the previous one and the one before it is dropped, so the memory is bounded and a
// count still in use is carried forward when it is read.
type tokenCountCache struct {
	mu       sync.Mutex
	seed     maphash.Seed
	size     int
	current  map[tokenCountKey]int
	previous map[tokenCountKey]int
}

func newTokenCountCache(size int) *tokenCountCache {
	return &tokenCountCache{
		seed:    maphash.MakeSeed(),
		size:    size,
		current: make(map[tokenCountKey]int, size),
	}
}

func (c *tokenCountCache) key(text string) tokenCountKey {
	return tokenCountKey{hash: maphash.String(c.seed, text), size: len(text)}
}

func (c *tokenCountCache) get(text string) (int, bool) {
	key := c.key(text)
	c.mu.Lock()
	defer c.mu.Unlock()
	if n, ok := c.current[key]; ok {
		return n, true
	}
	n, ok := c.previous[key]
	if ok {
		c.store(key, n)
	}
	return n, ok
}

func (c *tokenCountCache) put(text string, n int) {
	key := c.key(text)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store(key, n)
}

func (c *tokenCountCache) store(key tokenCountKey, n int) {
	if len(c.current) >= c.size {
		c.previous, c.current = c.current, make(map[tokenCountKey]int, c.size)
	}
	c.current[key] = n
}
