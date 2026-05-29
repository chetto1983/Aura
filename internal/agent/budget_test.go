package agent

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fixedClock returns a now-func pinned at t for deterministic wallclock tests (W8).
func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// newTestBudget builds a Budget directly (bypassing env) for unit tests: a far
// future deadline so wallclock never trips unless a test injects its own clock.
func newTestBudget(maxSteps int32, window int) *Budget {
	var steps atomic.Int32
	steps.Store(maxSteps)
	return &Budget{
		steps:             &steps,
		deadlineWallclock: time.Now().Add(time.Hour),
		now:               time.Now,
		dedupWindow:       window,
		dedupRing:         newDedupRing(window),
		resultCap:         defaultDedupResultCap,
		softFrac:          defaultBranchSoftFrac,
	}
}

func TestBudget_ConsumeStep_AtomicDecrement_NoRace(t *testing.T) {
	b := newTestBudget(1000, 3)
	var ok int64
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if got, _ := b.ConsumeStep(); got {
					atomic.AddInt64(&ok, 1)
				}
			}
		}()
	}
	wg.Wait()
	if ok != 1000 {
		t.Fatalf("successful decrements: want exactly 1000, got %d", ok)
	}
	if rem := b.Remaining(); rem != 0 {
		t.Fatalf("remaining after exhaustion: want 0, got %d", rem)
	}
}

func TestBudget_ConsumeStep_ExactlyOneWinner_When_CounterIsOne(t *testing.T) {
	b := newTestBudget(1, 3)
	var winners int64
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := b.ConsumeStep(); ok {
				atomic.AddInt64(&winners, 1)
			}
		}()
	}
	wg.Wait()
	if winners != 1 {
		t.Fatalf("TOCTOU: want exactly one winner, got %d", winners)
	}
	if rem := b.Remaining(); rem != 0 {
		t.Fatalf("remaining: want 0 after the single win, got %d", rem)
	}
}

func TestBudget_ConsumeStep_OverspendReason(t *testing.T) {
	b := newTestBudget(1, 3)
	if ok, _ := b.ConsumeStep(); !ok {
		t.Fatal("first consume should succeed")
	}
	ok, reason := b.ConsumeStep()
	if ok {
		t.Fatal("second consume should fail (counter at 0)")
	}
	if reason != "max_steps" {
		t.Fatalf("reason: want max_steps, got %q", reason)
	}
}

func TestBudget_Child_SharesStepsCounter(t *testing.T) {
	b := newTestBudget(25, 3)
	for i := 0; i < 5; i++ {
		if ok, _ := b.ConsumeStep(); !ok {
			t.Fatalf("parent consume %d should succeed", i)
		}
	}
	child := b.Child(1)
	if rem := child.Remaining(); rem != 20 {
		t.Fatalf("child shares counter: want Remaining()==20 after parent consumes 5, got %d", rem)
	}
	// A child decrement is visible to the parent (shared *atomic.Int32, D-10).
	if ok, _ := child.ConsumeStep(); !ok {
		t.Fatal("child consume should succeed")
	}
	if rem := b.Remaining(); rem != 19 {
		t.Fatalf("parent sees child decrement: want 19, got %d", rem)
	}
}

func TestBudget_Wallclock_TerminatesAtDeadline(t *testing.T) {
	var steps atomic.Int32
	steps.Store(100)
	deadline := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	b := &Budget{
		steps:             &steps,
		deadlineWallclock: deadline,
		now:               fixedClock(deadline.Add(-time.Second)), // before deadline
		dedupWindow:       3,
		dedupRing:         newDedupRing(3),
	}
	if ok, reason := b.ConsumeStep(); !ok {
		t.Fatalf("before deadline should succeed, got reason %q", reason)
	}
	// Advance the injected clock past the deadline.
	b.now = fixedClock(deadline.Add(time.Second))
	ok, reason := b.ConsumeStep()
	if ok {
		t.Fatal("past deadline should fail")
	}
	if reason != "wallclock" {
		t.Fatalf("reason past deadline: want wallclock, got %q", reason)
	}
}

func TestBudget_Wallclock_CheckedBeforeSteps(t *testing.T) {
	// With steps exhausted AND past deadline, wallclock wins (checked first, D-13).
	var steps atomic.Int32
	steps.Store(0)
	deadline := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	b := &Budget{
		steps:             &steps,
		deadlineWallclock: deadline,
		now:               fixedClock(deadline.Add(time.Second)),
		dedupWindow:       3,
		dedupRing:         newDedupRing(3),
	}
	_, reason := b.ConsumeStep()
	if reason != "wallclock" {
		t.Fatalf("wallclock checked before steps: want wallclock, got %q", reason)
	}
}

func TestNewBudgetFromEnv_Defaults(t *testing.T) {
	// Clear all AURA_LOOP_* so defaults apply.
	for _, k := range []string{
		envMaxSteps, envMaxWallclockSec, envDedupWindow,
		envBranchSoftFraction, envNodeTimeoutSec, envDedupExemptTools, envDedupResultCap,
	} {
		t.Setenv(k, "")
	}
	b, err := NewBudgetFromEnv()
	if err != nil {
		t.Fatalf("NewBudgetFromEnv defaults: unexpected error %v", err)
	}
	if rem := b.Remaining(); rem != defaultBudgetMaxSteps {
		t.Fatalf("default max steps: want %d, got %d", defaultBudgetMaxSteps, rem)
	}
	if b.dedupWindow != defaultDedupWindow {
		t.Fatalf("default dedup window: want %d, got %d", defaultDedupWindow, b.dedupWindow)
	}
}

func TestNewBudgetFromEnv_FailFast_MalformedMaxSteps(t *testing.T) {
	t.Setenv(envMaxSteps, "abc")
	_, err := NewBudgetFromEnv()
	if err == nil {
		t.Fatal("malformed AURA_LOOP_MAX_STEPS must fail-fast, got nil error")
	}
	want := errMalformed(envMaxSteps, "abc")
	if err.Error() != want.Error() {
		t.Fatalf("fail-fast error string:\n want %q\n got  %q", want.Error(), err.Error())
	}
}

func TestNewBudgetFromEnv_FailFast_MalformedWallclock(t *testing.T) {
	t.Setenv(envMaxWallclockSec, "not-a-number")
	_, err := NewBudgetFromEnv()
	if err == nil {
		t.Fatal("malformed AURA_LOOP_MAX_WALLCLOCK_SEC must fail-fast")
	}
	if !strings.Contains(err.Error(), envMaxWallclockSec) {
		t.Fatalf("error should name the offending var, got %q", err.Error())
	}
}

func TestNewBudgetFromEnv_FailFast_MalformedSoftFraction(t *testing.T) {
	t.Setenv(envBranchSoftFraction, "huge")
	_, err := NewBudgetFromEnv()
	if err == nil {
		t.Fatal("malformed AURA_LOOP_BRANCH_SOFT_FRACTION must fail-fast")
	}
}

func TestBudget_SetMaxSteps_Override(t *testing.T) {
	b := newTestBudget(25, 3)
	b.SetMaxSteps(5)
	if rem := b.Remaining(); rem != 5 {
		t.Fatalf("SetMaxSteps override: want 5, got %d", rem)
	}
}

func TestBudget_Remaining_ClampsNegative(t *testing.T) {
	var steps atomic.Int32
	steps.Store(-5) // forced negative
	b := &Budget{steps: &steps, deadlineWallclock: time.Now().Add(time.Hour), now: time.Now}
	if rem := b.Remaining(); rem != 0 {
		t.Fatalf("Remaining clamps negative to 0, got %d", rem)
	}
}

func TestBudget_SoftCapExceeded_RootIsAlwaysFalse(t *testing.T) {
	b := newTestBudget(10, 3) // root: branchSoftCap == 0
	for i := 0; i < 10; i++ {
		b.ConsumeStep()
	}
	if b.SoftCapExceeded() {
		t.Fatal("root budget (branchSoftCap==0) must never report SoftCapExceeded")
	}
}

func TestBudget_WithDeadline_PropagatesCancellation(t *testing.T) {
	var steps atomic.Int32
	steps.Store(10)
	deadline := time.Now().Add(50 * time.Millisecond)
	b := &Budget{steps: &steps, deadlineWallclock: deadline, now: time.Now}
	ctx, cancel := b.WithDeadline(context.Background())
	defer cancel()
	dl, ok := ctx.Deadline()
	if !ok || !dl.Equal(deadline) {
		t.Fatalf("WithDeadline should set the budget deadline, got %v ok=%v", dl, ok)
	}
}

func TestBudget_NodeTimeout(t *testing.T) {
	t.Setenv(envNodeTimeoutSec, "7")
	b, err := NewBudgetFromEnv()
	if err != nil {
		t.Fatalf("NewBudgetFromEnv: %v", err)
	}
	if got := b.NodeTimeout(); got != 7*time.Second {
		t.Fatalf("NodeTimeout: want 7s, got %v", got)
	}
}

func TestBudget_SoftCap_PassiveAdvisory(t *testing.T) {
	// Soft cap is non-terminal: ConsumeStep never returns "branch_soft_cap";
	// SoftCapExceeded reports fairness separately (D-12).
	b := newTestBudget(20, 3)
	child := b.Child(4) // softCap ~= ceil(20*fraction / 4)
	if child.branchSoftCap <= 0 {
		t.Fatalf("child should carry a positive soft cap, got %d", child.branchSoftCap)
	}
	// Consume up to and beyond the soft cap; ConsumeStep stays hard-only.
	for i := 0; i < child.branchSoftCap+2; i++ {
		ok, reason := child.ConsumeStep()
		if !ok {
			t.Fatalf("hard pool not exhausted yet; ConsumeStep failed at %d with %q", i, reason)
		}
		if reason == "branch_soft_cap" {
			t.Fatal("ConsumeStep must never return branch_soft_cap (soft cap is non-terminal, D-12)")
		}
	}
	if !child.SoftCapExceeded() {
		t.Fatal("SoftCapExceeded should report true after exceeding the branch soft cap")
	}
}
