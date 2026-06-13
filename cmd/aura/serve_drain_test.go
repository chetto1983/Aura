package main

import (
	"sync"
	"testing"
	"time"
)

// TestDrainWithGrace_FinishesBeforeGrace proves an in-flight turn that completes
// inside the grace window is allowed to run to its terminal frame: drainWithGrace
// returns drainResultDrained and the drain func ran fully (the turn's completion
// observed), never cancelled early (AP-17 bounded drain, O-06).
func TestDrainWithGrace_FinishesBeforeGrace(t *testing.T) {
	var completed bool
	drain := func() {
		// A short in-flight turn: well under the grace window.
		time.Sleep(20 * time.Millisecond)
		completed = true
	}

	res := drainWithGrace(drain, 500*time.Millisecond)

	if res != drainResultDrained {
		t.Fatalf("drainWithGrace = %v, want drainResultDrained (turn finished before grace)", res)
	}
	if !completed {
		t.Fatal("the in-flight drain must have run to completion, not been cut short")
	}
}

// TestDrainWithGrace_ExceedsGrace proves an in-flight turn that overruns the grace
// window does NOT wedge shutdown forever: drainWithGrace returns drainResultTimedOut
// within a bounded margin of the grace (so the caller proceeds to the hard-cancel
// backstop), rather than blocking on the still-running turn.
func TestDrainWithGrace_ExceedsGrace(t *testing.T) {
	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	drain := func() {
		defer wg.Done()
		<-release // a turn that overruns the grace window
	}

	const grace = 50 * time.Millisecond
	start := time.Now()
	res := drainWithGrace(drain, grace)
	elapsed := time.Since(start)

	if res != drainResultTimedOut {
		t.Fatalf("drainWithGrace = %v, want drainResultTimedOut (turn overran grace)", res)
	}
	// Bounded: it returned at ~grace, not blocked on the still-running drain.
	if elapsed > grace+200*time.Millisecond {
		t.Fatalf("drainWithGrace blocked %s, must return within ~grace (%s) — it must NOT hang on the overrunning turn", elapsed, grace)
	}
	// The backstop hard-cancel happens AFTER timeout; release the straggler so the
	// test's goroutine unwinds cleanly (mirrors workCancel firing).
	close(release)
	wg.Wait()
}
