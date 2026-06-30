package web

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestThrottleSemLazyCreatesPerHostBoundedChannel pins the semaphore seam: sem
// lazily creates ONE buffered channel of perHostLimit tokens per host and returns
// the SAME channel on repeat lookups, while distinct hosts get distinct channels
// (per-host isolation lives in the channel identity).
func TestThrottleSemLazyCreatesPerHostBoundedChannel(t *testing.T) {
	ht := newHostThrottle()

	a1 := ht.sem("a")
	a2 := ht.sem("a")
	if a1 != a2 {
		t.Fatal("sem(\"a\") returned a different channel on the second call — it must be created once and reused")
	}
	if cap(a1) != perHostLimit {
		t.Fatalf("sem cap = %d, want perHostLimit = %d", cap(a1), perHostLimit)
	}
	if b := ht.sem("b"); b == a1 {
		t.Fatal("distinct hosts must get distinct semaphores (no per-host isolation otherwise)")
	}
}

// TestThrottleAcquireBlocksAtLimitReleaseFrees proves acquire holds up to
// perHostLimit tokens, a further acquire blocks while the host is saturated, and a
// release unblocks the waiter.
func TestThrottleAcquireBlocksAtLimitReleaseFrees(t *testing.T) {
	ht := newHostThrottle()

	r1, ok := ht.acquire(context.Background(), "h")
	if !ok {
		t.Fatal("first acquire failed on an empty throttle")
	}
	r2, ok := ht.acquire(context.Background(), "h")
	if !ok {
		t.Fatal("second acquire failed below perHostLimit")
	}

	// The third acquire must block while the host holds perHostLimit tokens. Use a
	// cancellable ctx so the goroutine is guaranteed to unwind even if the assertion
	// path is not reached (goleak-clean).
	thirdCtx, cancelThird := context.WithCancel(context.Background())
	defer cancelThird()
	got := make(chan bool, 1)
	go func() {
		rel, ok := ht.acquire(thirdCtx, "h")
		if ok {
			rel()
		}
		got <- ok
	}()

	select {
	case <-got:
		t.Fatal("third acquire returned while the host was at perHostLimit — it must block")
	case <-time.After(50 * time.Millisecond):
		// Expected: still blocked.
	}

	// Releasing one held token must hand it to the blocked waiter.
	r1()
	select {
	case ok := <-got:
		if !ok {
			t.Fatal("third acquire returned ok=false after a token was released")
		}
	case <-time.After(time.Second):
		t.Fatal("third acquire did not unblock after a release freed a token")
	}
	r2()
}

// TestThrottleCtxCancelNoOpRelease is the subtle correctness case (RESEARCH): when
// acquire loses the race to ctx cancellation it returns ok=false AND a no-op
// release. Calling that release must NOT free a token it never took — otherwise a
// cancelled waiter would silently widen the per-host budget.
func TestThrottleCtxCancelNoOpRelease(t *testing.T) {
	ht := newHostThrottle()

	r1, ok := ht.acquire(context.Background(), "h")
	if !ok {
		t.Fatal("first acquire failed")
	}
	r2, ok := ht.acquire(context.Background(), "h")
	if !ok {
		t.Fatal("second acquire failed")
	}

	// Host is saturated. A pre-cancelled ctx forces acquire down the <-ctx.Done()
	// arm (the only ready case while the sem is full).
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	rel, ok := ht.acquire(cancelled, "h")
	if ok {
		rel()
		t.Fatal("acquire returned ok=true on a cancelled ctx while the host was at limit")
	}

	// Fire the no-op release. If it wrongly returned a token, the host would now
	// have a free slot.
	rel()

	// Prove the budget is still saturated: a short-deadline acquire must still fail
	// because the no-op release freed nothing.
	deadline, dcancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer dcancel()
	if extra, ok := ht.acquire(deadline, "h"); ok {
		extra()
		t.Fatal("no-op release wrongly freed a token — the semaphore should still be full")
	}

	r1()
	r2()
}

// TestThrottlePerHostIsolation proves a host pinned at its limit does not starve a
// different host (each host owns an independent semaphore).
func TestThrottlePerHostIsolation(t *testing.T) {
	ht := newHostThrottle()

	ra1, ok := ht.acquire(context.Background(), "a")
	if !ok {
		t.Fatal("acquire a#1 failed")
	}
	ra2, ok := ht.acquire(context.Background(), "a")
	if !ok {
		t.Fatal("acquire a#2 failed")
	}
	// Host "a" is now saturated. Host "b" must still acquire promptly; a bounded ctx
	// makes a broken isolation fail fast instead of hanging.
	bCtx, bCancel := context.WithTimeout(context.Background(), time.Second)
	defer bCancel()
	rb, ok := ht.acquire(bCtx, "b")
	if !ok {
		t.Fatal("host b was blocked by host a at its limit — per-host isolation is broken")
	}
	rb()
	ra1()
	ra2()
}

// TestThrottleConcurrentRace hammers acquire/release across several hosts from many
// goroutines so the race detector exercises the lazy map-creation mutex and the
// token handoff. Every goroutine releases what it acquires, so the pool always
// makes progress and the goleak TestMain (main_test.go) sees no survivor.
func TestThrottleConcurrentRace(t *testing.T) {
	ht := newHostThrottle()
	hosts := []string{"h1", "h2", "h3"}

	var wg sync.WaitGroup
	for i := 0; i < 256; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			host := hosts[i%len(hosts)]
			rel, ok := ht.acquire(context.Background(), host)
			if !ok {
				return
			}
			rel()
		}(i)
	}
	wg.Wait()
}
