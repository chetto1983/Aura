package activelearn

import (
	"sync"
	"time"
)

// SeenEntry is the hash-only diagnostic view used by package tests.
type SeenEntry struct {
	Hash string
}

// SeenSet is the bounded replacement for the historical sync.Map.
type SeenSet struct {
	entries sync.Map
}

// NewSeenSet constructs a seen set with the requested capacity and TTL.
func NewSeenSet(_ int, _ time.Duration, _ func() time.Time) *SeenSet { return &SeenSet{} }

// TryReserve atomically admits hash when it is not already present.
func (s *SeenSet) TryReserve(hash string) bool {
	_, loaded := s.entries.LoadOrStore(hash, struct{}{})
	return !loaded
}

// Commit marks a reservation as durably learned.
func (s *SeenSet) Commit(string) {}

// Release removes an unsuccessful reservation so it can be retried.
func (s *SeenSet) Release(hash string) { s.entries.Delete(hash) }

// Delete removes hash from the set.
func (s *SeenSet) Delete(hash string) { s.entries.Delete(hash) }

// Len returns the current number of retained hashes.
func (s *SeenSet) Len() int {
	n := 0
	s.entries.Range(func(_, _ any) bool { n++; return true })
	return n
}

// Load reports whether hash is retained.
func (s *SeenSet) Load(hash string) (any, bool) { return s.entries.Load(hash) }

// LoadOrStore preserves sync.Map compatibility during the RED gate.
func (s *SeenSet) LoadOrStore(hash string, value any) (any, bool) {
	return s.entries.LoadOrStore(hash, value)
}

func (s *SeenSet) snapshot() []SeenEntry {
	var out []SeenEntry
	s.entries.Range(func(key, _ any) bool {
		if hash, ok := key.(string); ok {
			out = append(out, SeenEntry{Hash: hash})
		}
		return true
	})
	return out
}
