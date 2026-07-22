package activelearn

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/neostore"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func TestSeenSetTTLBoundaryAndRelease(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	seen := NewSeenSet(2, 30*24*time.Hour, clock.Now)
	hash := neostore.HashText("retry-secret-sentinel")

	if !seen.TryReserve(hash) {
		t.Fatal("initial reserve rejected")
	}
	seen.Commit(hash)
	clock.Advance(30 * 24 * time.Hour)
	if seen.TryReserve(hash) {
		t.Fatal("entry expired at equality; TTL is inclusive")
	}
	clock.Advance(time.Nanosecond)
	if !seen.TryReserve(hash) {
		t.Fatal("entry was not admissible immediately after TTL")
	}
	seen.Release(hash)
	if !seen.TryReserve(hash) {
		t.Fatal("released reservation was not retryable")
	}
}

func TestSeenSetHardCapAndDeterministicOldestEviction(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	seen := NewSeenSet(2, time.Hour, clock.Now)
	for _, hash := range []string{"old", "new"} {
		if !seen.TryReserve(hash) {
			t.Fatalf("reserve %q", hash)
		}
		seen.Commit(hash)
		clock.Advance(time.Second)
	}
	if !seen.TryReserve("third") {
		t.Fatal("third entry should evict the oldest committed entry")
	}
	if got := seen.Len(); got != 2 {
		t.Fatalf("Len = %d, want hard cap 2", got)
	}
	if seen.TryReserve("new") {
		t.Fatal("newer committed entry was evicted instead of oldest")
	}
	if !seen.TryReserve("old") {
		t.Fatal("oldest committed entry was not evicted")
	}
}

func TestSeenSetConcurrentAdmissionNeverExceedsCap(t *testing.T) {
	const capEntries = 128
	seen := NewSeenSet(capEntries, 30*24*time.Hour, time.Now)
	var wg sync.WaitGroup
	for i := 0; i < 2_048; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h := neostore.HashText(fmt.Sprintf("value-%d", i))
			if seen.TryReserve(h) {
				seen.Commit(h)
			}
		}(i)
	}
	wg.Wait()
	if got := seen.Len(); got > capEntries {
		t.Fatalf("Len = %d, exceeds hard cap %d", got, capEntries)
	}
}

func TestSeenSetSnapshotContainsHashesOnly(t *testing.T) {
	seen := NewSeenSet(1, time.Hour, time.Now)
	raw := "raw-learning-secret"
	hash := neostore.HashText(raw)
	if !seen.TryReserve(hash) {
		t.Fatal("reserve")
	}
	seen.Commit(hash)
	snapshot := seen.snapshot()
	if len(snapshot) != 1 || snapshot[0].Hash != hash {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot[0].Hash == raw {
		t.Fatal("raw observation retained in seen state")
	}
}
