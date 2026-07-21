package activelearn

import (
	"sort"
	"sync"
	"time"
)

const expirationSweepLimit = 64

type seenState uint8

const (
	seenReserved seenState = iota
	seenCommitted
)

type seenRecord struct {
	touched time.Time
	state   seenState
	seq     uint64
}

type reserveOutcome uint8

const (
	reserveAdmitted reserveOutcome = iota
	reserveDuplicate
	reserveAfterExpiry
	reserveAfterEviction
	reserveAtCapacity
)

// SeenEntry is the hash-only diagnostic view used by package tests.
type SeenEntry struct {
	Hash      string
	TouchedAt time.Time
	Reserved  bool
}

// SeenSet atomically bounds hash-only learning admission by capacity and TTL.
type SeenSet struct {
	mu         sync.Mutex
	entries    map[string]seenRecord
	maxEntries int
	ttl        time.Duration
	now        func() time.Time
	sequence   uint64
}

// NewSeenSet constructs a seen set with the requested capacity and TTL.
func NewSeenSet(maxEntries int, ttl time.Duration, now func() time.Time) *SeenSet {
	if maxEntries <= 0 {
		maxEntries = defaultSeenMaxEntries
	}
	if ttl <= 0 {
		ttl = defaultSeenTTL
	}
	if now == nil {
		now = time.Now
	}
	return &SeenSet{
		entries:    make(map[string]seenRecord, min(maxEntries, 1024)),
		maxEntries: maxEntries,
		ttl:        ttl,
		now:        now,
	}
}

// TryReserve atomically admits hash when it is not already retained.
func (s *SeenSet) TryReserve(hash string) bool {
	outcome := s.tryReserve(hash)
	return outcome != reserveDuplicate && outcome != reserveAtCapacity
}

func (s *SeenSet) tryReserve(hash string) reserveOutcome {
	if s == nil || hash == "" {
		return reserveAtCapacity
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if record, ok := s.entries[hash]; ok {
		if now.Sub(record.touched) <= s.ttl {
			return reserveDuplicate
		}
		delete(s.entries, hash)
		s.insert(hash, now, seenReserved)
		return reserveAfterExpiry
	}

	s.expireSome(now, expirationSweepLimit)
	outcome := reserveAdmitted
	if len(s.entries) >= s.maxEntries {
		oldest, ok := s.oldestCommitted()
		if !ok {
			return reserveAtCapacity
		}
		delete(s.entries, oldest)
		outcome = reserveAfterEviction
	}
	s.insert(hash, now, seenReserved)
	return outcome
}

func (s *SeenSet) insert(hash string, now time.Time, state seenState) {
	s.sequence++
	s.entries[hash] = seenRecord{touched: now, state: state, seq: s.sequence}
}

func (s *SeenSet) expireSome(now time.Time, limit int) {
	removed := 0
	for hash, record := range s.entries {
		if removed >= limit {
			return
		}
		if now.Sub(record.touched) > s.ttl {
			delete(s.entries, hash)
			removed++
		}
	}
}

func (s *SeenSet) oldestCommitted() (string, bool) {
	var oldestHash string
	var oldest seenRecord
	found := false
	for hash, record := range s.entries {
		if record.state != seenCommitted {
			continue
		}
		if !found || record.touched.Before(oldest.touched) ||
			(record.touched.Equal(oldest.touched) && (record.seq < oldest.seq ||
				record.seq == oldest.seq && hash < oldestHash)) {
			oldestHash, oldest, found = hash, record, true
		}
	}
	return oldestHash, found
}

// Commit marks a reservation as durably learned without retaining content.
func (s *SeenSet) Commit(hash string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.entries[hash]
	if !ok || record.state != seenReserved {
		return
	}
	record.state = seenCommitted
	record.touched = s.now()
	s.entries[hash] = record
}

// Release removes an unsuccessful reservation so it can be retried.
func (s *SeenSet) Release(hash string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if record, ok := s.entries[hash]; ok && record.state == seenReserved {
		delete(s.entries, hash)
	}
}

// Len returns the current number of retained hashes.
func (s *SeenSet) Len() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

func (s *SeenSet) oldestAge() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	var oldest time.Time
	for _, record := range s.entries {
		if oldest.IsZero() || record.touched.Before(oldest) {
			oldest = record.touched
		}
	}
	if oldest.IsZero() || now.Before(oldest) {
		return 0
	}
	return now.Sub(oldest)
}

func (s *SeenSet) snapshot() []SeenEntry {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SeenEntry, 0, len(s.entries))
	for hash, record := range s.entries {
		out = append(out, SeenEntry{Hash: hash, TouchedAt: record.touched, Reserved: record.state == seenReserved})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hash < out[j].Hash })
	return out
}
