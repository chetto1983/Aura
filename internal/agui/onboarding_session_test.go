package agui

import (
	"sync"
	"testing"
	"time"
)

// onboarding_session_test.go covers the goroutine-free TTL session store (ONBD-02 /
// RESEARCH §Hard Problem 4 + the threat T-28-05-07 DoS row): put/get round-trip, the
// per-step idle-TTL refresh, the lazy sweep on access (no background goroutine — the
// package goleak TestMain proves no leaked reaper), and concurrent access under -race.

// fixedClock is an injectable monotonic clock for deterministic TTL assertions.
type fixedClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fixedClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fixedClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newTestSession() *sessionEntry {
	return &sessionEntry{
		creatorIdentityID: "00000000-0000-0000-0000-000000000001",
		capabilityOptions: []string{"agent.run"},
	}
}

// TestSessionTTL is the goleak/-race anchor (the plan verify runs -run TestSessionTTL):
// it proves put→get round-trips, an idle session past the TTL is swept on access (gone),
// a per-step access before the TTL refreshes the deadline (survives a later access), and
// the whole store needs NO background goroutine (goleak TestMain).
func TestSessionTTL(t *testing.T) {
	clk := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	st := newSessionStore(15 * time.Minute)
	st.now = clk.now

	t.Run("put and get round-trip", func(t *testing.T) {
		token, err := st.put(newTestSession())
		if err != nil {
			t.Fatalf("put: %v", err)
		}
		if token == "" {
			t.Fatal("put returned an empty token")
		}
		got, ok := st.get(token)
		if !ok || got == nil {
			t.Fatalf("get(%q) miss after put", token)
		}
		if got.creatorIdentityID != "00000000-0000-0000-0000-000000000001" {
			t.Errorf("session creator = %q, want the stored creator", got.creatorIdentityID)
		}
	})

	t.Run("idle past TTL is swept on access", func(t *testing.T) {
		token, err := st.put(newTestSession())
		if err != nil {
			t.Fatalf("put: %v", err)
		}
		clk.advance(15*time.Minute + time.Second) // idle past the 15-min window
		if _, ok := st.get(token); ok {
			t.Fatal("expired session must be swept (got a hit)")
		}
	})

	t.Run("per-step access refreshes the idle deadline", func(t *testing.T) {
		token, err := st.put(newTestSession())
		if err != nil {
			t.Fatalf("put: %v", err)
		}
		// Two accesses 10 min apart: each refreshes the deadline, so the entry survives a
		// total elapsed time well past the 15-min idle TTL (idle, not absolute).
		clk.advance(10 * time.Minute)
		if _, ok := st.get(token); !ok {
			t.Fatal("session must survive within the idle window")
		}
		clk.advance(10 * time.Minute)
		if _, ok := st.get(token); !ok {
			t.Fatal("a refreshed session must survive a second sub-TTL idle gap")
		}
	})

	t.Run("unknown token is a clean miss", func(t *testing.T) {
		if _, ok := st.get("deadbeef"); ok {
			t.Fatal("unknown token must miss")
		}
	})

	t.Run("opaque tokens are unique", func(t *testing.T) {
		a, _ := st.put(newTestSession())
		b, _ := st.put(newTestSession())
		if a == b {
			t.Fatal("two puts minted the same token (entropy/collision bug)")
		}
		if len(a) != sessionTokenBytes*2 { // hex
			t.Errorf("token length = %d, want %d hex chars", len(a), sessionTokenBytes*2)
		}
	})
}

// TestSessionStoreLazySweepReclaims proves the sweep reclaims abandoned entries on access
// of ANY session (the lazy-GC-no-goroutine guarantee): an idle entry is dropped from the
// live count once a later put/get sweeps it.
func TestSessionStoreLazySweepReclaims(t *testing.T) {
	clk := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	st := newSessionStore(15 * time.Minute)
	st.now = clk.now

	for i := range 5 {
		if _, err := st.put(newTestSession()); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	if n := st.len(); n != 5 {
		t.Fatalf("live entries = %d, want 5", n)
	}
	clk.advance(15*time.Minute + time.Second)
	// A single access triggers the sweep across all siblings.
	if _, err := st.put(newTestSession()); err != nil {
		t.Fatalf("put after expiry: %v", err)
	}
	if n := st.len(); n != 1 {
		t.Fatalf("after sweep live entries = %d, want 1 (only the fresh put)", n)
	}
}

// TestSessionStoreConcurrent hammers the store from many goroutines so the race detector
// proves the mutex discipline (no data race) and goleak proves no leaked goroutine.
func TestSessionStoreConcurrent(t *testing.T) {
	st := newSessionStore(time.Hour)
	var wg sync.WaitGroup
	for range 64 {
		wg.Go(func() {
			token, err := st.put(newTestSession())
			if err != nil {
				t.Errorf("put: %v", err)
				return
			}
			for range 8 {
				if _, ok := st.get(token); !ok {
					t.Errorf("get(%q) miss under concurrency", token)
					return
				}
			}
		})
	}
	wg.Wait()
	if n := st.len(); n != 64 {
		t.Errorf("live entries = %d, want 64", n)
	}
}

// len reports the number of live (un-swept) entries — a test-support accessor that also
// triggers a sweep so expiry assertions observe the reclaimed count.
func (st *sessionStore) len() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.sweepLocked()
	return len(st.entries)
}
