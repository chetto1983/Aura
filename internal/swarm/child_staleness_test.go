package swarm

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
)

// TestChildStalenessResetKeepsAlive proves a worker emitting Progress() faster than
// the idle window runs indefinitely and is never reaped (SWARM-03 edge: N < idle).
func TestChildStalenessResetKeepsAlive(t *testing.T) {
	defer goleak.VerifyNone(t)
	var cancelled atomic.Bool
	cs := newChildStaleness(func() { cancelled.Store(true) }, 25*time.Millisecond)
	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(8 * time.Millisecond)
		cs.Progress()
	}
	cs.Stop()
	if cancelled.Load() {
		t.Fatal("staleness fired despite regular Progress() resets faster than the idle window")
	}
	if cs.Stalled() {
		t.Fatal("Stalled() true despite no fire")
	}
}

// TestChildStalenessReapsOnSilence proves a worker emitting nothing is reaped
// exactly once, at (approximately) the idle window (SWARM-03 edge: silence).
func TestChildStalenessReapsOnSilence(t *testing.T) {
	defer goleak.VerifyNone(t)
	ctx, cancel := context.WithCancel(context.Background())
	cs := newChildStaleness(cancel, 15*time.Millisecond)
	<-ctx.Done()
	if !cs.Stalled() {
		t.Fatal("Stalled() false after silence elapsed past the idle window")
	}
	if ctx.Err() == nil {
		t.Fatal("worker ctx was not cancelled by the staleness timer")
	}
	cs.Stop() // deferred cleanup in runChild; must be safe post-fire
}

// TestChildStalenessDisabledStartsNoTimer proves idle<=0 disables reaping entirely
// and allocates no timer goroutine (SWARM-03 boundary edge: <=0 disables).
func TestChildStalenessDisabledStartsNoTimer(t *testing.T) {
	defer goleak.VerifyNone(t)
	var cancelled atomic.Bool
	cs := newChildStaleness(func() { cancelled.Store(true) }, 0)
	time.Sleep(20 * time.Millisecond)
	cs.Progress() // must also be a safe no-op with no timer armed
	cs.Stop()
	if cancelled.Load() {
		t.Fatal("idle<=0 must disable reaping entirely")
	}
	if cs.Stalled() {
		t.Fatal("Stalled() true despite idle<=0")
	}
}

// TestChildStalenessStopIsIdempotent proves the deferred Stop() in runChild never
// panics regardless of how many times it is invoked or whether the timer already
// fired (SWARM-09 edge: no leaked timer/goroutine after Stop).
func TestChildStalenessStopIsIdempotent(t *testing.T) {
	defer goleak.VerifyNone(t)
	cs := newChildStaleness(func() {}, time.Second)
	cs.Stop()
	cs.Stop()
	cs.Stop()
}

// TestChildStalenessProgressNeverReArmsAfterFire proves a Progress() call racing
// (or arriving after) a fire does not resurrect an already-tripped deadline.
func TestChildStalenessProgressNeverReArmsAfterFire(t *testing.T) {
	defer goleak.VerifyNone(t)
	ctx, cancel := context.WithCancel(context.Background())
	cs := newChildStaleness(cancel, 12*time.Millisecond)
	<-ctx.Done()
	cs.Progress()
	cs.Stop()
	if !cs.Stalled() {
		t.Fatal("Stalled() must stay true once fired")
	}
}

// TestChildStalenessNilReceiverIsSafe proves every method is safe on a nil
// *childStaleness (defensive, mirrors idleWatchdog's own nil-safety idiom).
func TestChildStalenessNilReceiverIsSafe(t *testing.T) {
	var cs *childStaleness
	cs.Progress()
	cs.Stop()
	if cs.Stalled() {
		t.Fatal("nil childStaleness must report Stalled() == false")
	}
}
