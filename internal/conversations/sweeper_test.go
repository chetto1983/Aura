// Unit tier (no build tag): the periodic sidecar-sweep worker lifecycle + reclaim
// semantics for audit finding M-06 part 2. The worker drives the boot sweep on an
// interval so a long-running `serve` daemon reclaims aged/orphaned sidecars WITHOUT a
// restart. These tests need no DB: the worker takes an injectable sweep func, and the
// reclaim-semantics test exercises the pool-independent tmp/* TTL path directly.
package conversations

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
)

// TestSweeper_TicksThenStopsClean: with a short interval the worker runs the injected
// sweep on each tick; Stop joins the goroutine with no leak (goleak.VerifyNone).
func TestSweeper_TicksThenStopsClean(t *testing.T) {
	defer goleak.VerifyNone(t)

	var calls atomic.Int64
	sw := NewSweeper(SweeperConfig{
		Interval: 10 * time.Millisecond,
		Sweep:    func(context.Context) { calls.Add(1) },
	})

	ctx := t.Context()
	sw.Start(ctx)

	// Wait for at least a couple of ticks to land.
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("worker did not tick: got %d calls", calls.Load())
		}
		time.Sleep(2 * time.Millisecond)
	}
	sw.Stop()

	// After Stop the worker must be quiescent — no further ticks.
	settled := calls.Load()
	time.Sleep(40 * time.Millisecond)
	if got := calls.Load(); got != settled {
		t.Errorf("worker kept ticking after Stop: %d -> %d", settled, got)
	}
}

// TestSweeper_StopBeforeStartIsNoop: Stop on a never-started worker is safe (the
// serve shutdown path may Stop a worker whose Start was skipped by a disabled knob).
func TestSweeper_StopBeforeStartIsNoop(t *testing.T) {
	defer goleak.VerifyNone(t)
	sw := NewSweeper(SweeperConfig{Interval: time.Hour, Sweep: func(context.Context) {}})
	sw.Stop() // must not panic or block
}

// TestSweeper_DisabledIntervalNeverTicks: a non-positive interval disables the periodic
// worker entirely (the boot sweep still runs elsewhere); Start launches nothing to join.
func TestSweeper_DisabledIntervalNeverTicks(t *testing.T) {
	defer goleak.VerifyNone(t)

	var calls atomic.Int64
	sw := NewSweeper(SweeperConfig{
		Interval: 0,
		Sweep:    func(context.Context) { calls.Add(1) },
	})
	sw.Start(context.Background())
	time.Sleep(30 * time.Millisecond)
	if got := calls.Load(); got != 0 {
		t.Errorf("disabled worker must never tick, got %d calls", got)
	}
	sw.Stop()
}

// TestSweeper_CancelStopsWorker: cancelling the Start ctx stops the worker even without
// an explicit Stop — Stop afterward is still a clean join (goleak).
func TestSweeper_CancelStopsWorker(t *testing.T) {
	defer goleak.VerifyNone(t)

	var calls atomic.Int64
	sw := NewSweeper(SweeperConfig{
		Interval: 10 * time.Millisecond,
		Sweep:    func(context.Context) { calls.Add(1) },
	})
	ctx, cancel := context.WithCancel(context.Background())
	sw.Start(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("worker did not tick before cancel")
		}
		time.Sleep(2 * time.Millisecond)
	}
	cancel() // ctx-cancel must wind the goroutine down
	sw.Stop()
}

// TestSweeper_ReclaimsAgedKeepsFresh: the worker, driven over a real TempDir using the
// boot sweep's tmp/* TTL semantics (sweepTmp), reclaims an aged sidecar on a tick and
// keeps a fresh one — proving the periodic path reuses the existing age/orphan rules.
func TestSweeper_ReclaimsAgedKeepsFresh(t *testing.T) {
	defer goleak.VerifyNone(t)

	runDir := t.TempDir()
	tmpRoot := filepath.Join(runDir, "tmp")
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	aged := filepath.Join(tmpRoot, "aged.scratch")
	fresh := filepath.Join(tmpRoot, "fresh.scratch")
	for _, p := range []string{aged, fresh} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %q: %v", p, err)
		}
	}
	// Backdate the aged entry past the boot sweep's tmpTTL.
	stale := time.Now().Add(-tmpTTL - time.Hour)
	if err := os.Chtimes(aged, stale, stale); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	sw := NewSweeper(SweeperConfig{
		Interval: 10 * time.Millisecond,
		// Reuse the boot sweep's pool-independent tmp TTL path (the periodic worker in
		// serve wires the full ScanOrphans; this asserts the reclaim semantics it runs).
		Sweep: func(context.Context) { _ = sweepTmp(runDir) },
	})
	ctx := t.Context()
	sw.Start(ctx)
	// defer, NOT t.Cleanup: Cleanup functions run AFTER the test function returns, so
	// they fire after `defer goleak.VerifyNone(t)` above and the worker is still alive
	// when goleak counts. As a defer registered later it wins the LIFO order and stops
	// the worker first. t.Context() has the same hazard — it is cancelled just before
	// Cleanup, i.e. also too late — so the explicit Stop is what makes this correct.
	defer sw.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(aged); os.IsNotExist(err) {
			break // reclaimed
		}
		if time.Now().After(deadline) {
			t.Fatal("aged sidecar was not reclaimed by the periodic sweep")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh sidecar must survive the periodic sweep: %v", err)
	}
}
