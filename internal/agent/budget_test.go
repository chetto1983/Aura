package agent

import (
	"context"
	"os"
	"strconv"
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
	for range 10 {
		wg.Go(func() {
			for range 100 {
				if got, _ := b.ConsumeStep(); got {
					atomic.AddInt64(&ok, 1)
				}
			}
		})
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
	for range 100 {
		wg.Go(func() {
			if ok, _ := b.ConsumeStep(); ok {
				atomic.AddInt64(&winners, 1)
			}
		})
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

func TestBudget_ConsumeStep_OverspendRestoresCounter(t *testing.T) {
	b := newTestBudget(0, 3)
	ok, reason := b.ConsumeStep()
	if ok {
		t.Fatal("consume should fail when counter starts at 0")
	}
	if reason != "max_steps" {
		t.Fatalf("reason: want max_steps, got %q", reason)
	}
	if raw := b.steps.Load(); raw != 0 {
		t.Fatalf("overspend must restore the raw shared counter to 0, got %d", raw)
	}
}

func TestBudget_Child_SharesStepsCounter(t *testing.T) {
	b := newTestBudget(25, 3)
	for i := range 5 {
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

func TestNewBudget_InjectedClock_AnchorsDeadlineThroughConstructor(t *testing.T) {
	// WR-03: the deadline anchor and the ConsumeStep gate must share ONE time source.
	// With an injected clock the constructor must anchor deadlineWallclock at
	// injectedNow + wallclock — NOT at real time.Now — so a caller can drive wallclock
	// behavior end-to-end through the constructor instead of building a Budget literal.
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clk := base
	maxSteps, wallSec := 100, 30
	b, err := NewBudget(BudgetOptions{
		MaxSteps:        &maxSteps,
		MaxWallclockSec: &wallSec,
		Now:             func() time.Time { return clk },
	})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}

	wantDeadline := base.Add(30 * time.Second)
	if !b.deadlineWallclock.Equal(wantDeadline) {
		t.Fatalf("deadline anchored off injected clock: want %v, got %v", wantDeadline, b.deadlineWallclock)
	}

	// Still inside the window → a step succeeds.
	clk = base.Add(29 * time.Second)
	if ok, reason := b.ConsumeStep(); !ok {
		t.Fatalf("before injected deadline should succeed, got reason %q", reason)
	}
	// Advance the SAME injected clock past the deadline → wallclock trips.
	clk = base.Add(31 * time.Second)
	ok, reason := b.ConsumeStep()
	if ok {
		t.Fatal("past injected deadline should fail")
	}
	if reason != "wallclock" {
		t.Fatalf("reason past injected deadline: want wallclock, got %q", reason)
	}
}

func TestNewBudget_NilClock_DefaultsToTimeNow(t *testing.T) {
	// The default (Now nil) production path must still anchor a future deadline off
	// real time.Now so an immediate step succeeds — WR-03 must not regress the default.
	maxSteps, wallSec := 5, 300
	b, err := NewBudget(BudgetOptions{MaxSteps: &maxSteps, MaxWallclockSec: &wallSec})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	if !b.deadlineWallclock.After(time.Now()) {
		t.Fatalf("default clock must anchor a future deadline, got %v", b.deadlineWallclock)
	}
	if ok, reason := b.ConsumeStep(); !ok {
		t.Fatalf("default path step should succeed, got reason %q", reason)
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

func TestNewBudget_Options_OverrideEnvWithoutMutatingEnv(t *testing.T) {
	// CLI > env > default (D-06) resolved via BudgetOptions, no os.Setenv (WR-04):
	// an explicit override wins over the env value; the env is never mutated.
	t.Setenv(envMaxSteps, "9")
	t.Setenv(envDedupWindow, "2")
	override := 25
	b, err := NewBudget(BudgetOptions{MaxSteps: &override})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	if rem := b.Remaining(); rem != 25 {
		t.Fatalf("MaxSteps override must win over env=9: want 25, got %d", rem)
	}
	if b.dedupWindow != 2 {
		t.Fatalf("unset DedupWindow must fall through to env=2: got %d", b.dedupWindow)
	}
	if got := os.Getenv(envMaxSteps); got != "9" {
		t.Fatalf("NewBudget must NOT mutate the env (WR-04), AURA_LOOP_MAX_STEPS=%q", got)
	}
}

func TestNewBudget_Options_NilFallsThroughToDefault(t *testing.T) {
	for _, k := range []string{envMaxSteps, envDedupWindow} {
		t.Setenv(k, "")
	}
	b, err := NewBudget(BudgetOptions{})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	if rem := b.Remaining(); rem != defaultBudgetMaxSteps {
		t.Fatalf("nil MaxSteps + unset env → builtin default %d, got %d", defaultBudgetMaxSteps, rem)
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

func TestNewBudgetFromEnv_FailFast_MaxStepsOutOfInt32Range(t *testing.T) {
	if strconv.IntSize <= 32 {
		t.Skip("int cannot exceed int32 on this platform")
	}
	for _, v := range []string{"2147483648", "-2147483649"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv(envMaxSteps, v)
			t.Setenv(envMaxWallclockSec, "")
			t.Setenv(envDedupWindow, "")
			t.Setenv(envBranchSoftFraction, "")
			t.Setenv(envNodeTimeoutSec, "")
			t.Setenv(envDedupResultCap, "")

			_, err := NewBudgetFromEnv()
			if err == nil {
				t.Fatalf("%s=%s must fail-fast as out of int32 range", envMaxSteps, v)
			}
			if !strings.Contains(err.Error(), "must fit int32") {
				t.Fatalf("range error should require int32, got %q", err.Error())
			}
		})
	}
}

func TestNewBudgetFromEnv_FailFast_MaxStepsMustBePositive(t *testing.T) {
	for _, v := range []string{"0", "-1"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv(envMaxSteps, v)
			t.Setenv(envMaxWallclockSec, "")
			t.Setenv(envDedupWindow, "")
			t.Setenv(envBranchSoftFraction, "")
			t.Setenv(envNodeTimeoutSec, "")
			t.Setenv(envDedupResultCap, "")

			_, err := NewBudgetFromEnv()
			if err == nil {
				t.Fatalf("%s=%s must fail-fast as non-positive", envMaxSteps, v)
			}
			if !strings.Contains(err.Error(), "must be >= 1") {
				t.Fatalf("positive-range error should require >= 1, got %q", err.Error())
			}
		})
	}
}

func TestNewBudget_Options_MaxStepsRejectsInt32Overflow(t *testing.T) {
	if strconv.IntSize <= 32 {
		t.Skip("int cannot exceed int32 on this platform")
	}
	tooLarge64 := int64(2147483647) + 1
	tooLarge := int(tooLarge64)
	t.Setenv(envMaxWallclockSec, "")
	t.Setenv(envDedupWindow, "")
	t.Setenv(envBranchSoftFraction, "")
	t.Setenv(envNodeTimeoutSec, "")
	t.Setenv(envDedupResultCap, "")

	_, err := NewBudget(BudgetOptions{MaxSteps: &tooLarge})
	if err == nil {
		t.Fatal("MaxSteps override above int32 range must fail-fast")
	}
	if !strings.Contains(err.Error(), "must fit int32") {
		t.Fatalf("range error should require int32, got %q", err.Error())
	}
}

func TestNewBudget_Options_MaxStepsMustBePositive(t *testing.T) {
	for _, maxSteps := range []int{0, -1} {
		t.Run(strconv.Itoa(maxSteps), func(t *testing.T) {
			t.Setenv(envMaxWallclockSec, "")
			t.Setenv(envDedupWindow, "")
			t.Setenv(envBranchSoftFraction, "")
			t.Setenv(envNodeTimeoutSec, "")
			t.Setenv(envDedupResultCap, "")

			_, err := NewBudget(BudgetOptions{MaxSteps: &maxSteps})
			if err == nil {
				t.Fatalf("MaxSteps override %d must fail-fast as non-positive", maxSteps)
			}
			if !strings.Contains(err.Error(), "must be >= 1") {
				t.Fatalf("positive-range error should require >= 1, got %q", err.Error())
			}
		})
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

func TestNewBudgetFromEnv_FailFast_WallclockMustBePositive(t *testing.T) {
	for _, v := range []string{"0", "-1"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv(envMaxWallclockSec, v)
			t.Setenv(envMaxSteps, "")
			t.Setenv(envDedupWindow, "")
			t.Setenv(envBranchSoftFraction, "")
			t.Setenv(envNodeTimeoutSec, "")
			t.Setenv(envDedupResultCap, "")

			_, err := NewBudgetFromEnv()
			if err == nil {
				t.Fatalf("%s=%s must fail-fast as non-positive", envMaxWallclockSec, v)
			}
			if !strings.Contains(err.Error(), "must be >= 1") {
				t.Fatalf("positive-range error should require >= 1, got %q", err.Error())
			}
		})
	}
}

func TestNewBudget_Options_WallclockMustBePositive(t *testing.T) {
	for _, wallclockSec := range []int{0, -1} {
		t.Run(strconv.Itoa(wallclockSec), func(t *testing.T) {
			t.Setenv(envMaxSteps, "")
			t.Setenv(envDedupWindow, "")
			t.Setenv(envBranchSoftFraction, "")
			t.Setenv(envNodeTimeoutSec, "")
			t.Setenv(envDedupResultCap, "")

			_, err := NewBudget(BudgetOptions{MaxWallclockSec: &wallclockSec})
			if err == nil {
				t.Fatalf("MaxWallclockSec override %d must fail-fast as non-positive", wallclockSec)
			}
			if !strings.Contains(err.Error(), "must be >= 1") {
				t.Fatalf("positive-range error should require >= 1, got %q", err.Error())
			}
		})
	}
}

func TestNewBudgetFromEnv_FailFast_MalformedDedupWindow(t *testing.T) {
	t.Setenv(envDedupWindow, "nope")
	_, err := NewBudgetFromEnv()
	if err == nil {
		t.Fatal("malformed AURA_LOOP_DEDUP_WINDOW must fail-fast")
	}
	want := errMalformed(envDedupWindow, "nope")
	if err.Error() != want.Error() {
		t.Fatalf("fail-fast error string:\n want %q\n got  %q", want.Error(), err.Error())
	}
}

func TestNewBudgetFromEnv_FailFast_MalformedSoftFraction(t *testing.T) {
	t.Setenv(envBranchSoftFraction, "huge")
	_, err := NewBudgetFromEnv()
	if err == nil {
		t.Fatal("malformed AURA_LOOP_BRANCH_SOFT_FRACTION must fail-fast")
	}
	want := errMalformed(envBranchSoftFraction, "huge")
	if err.Error() != want.Error() {
		t.Fatalf("fail-fast error string:\n want %q\n got  %q", want.Error(), err.Error())
	}
}

func TestNewBudgetFromEnv_FailFast_SoftFractionOutOfRange(t *testing.T) {
	for _, v := range []string{"0", "1.01"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv(envBranchSoftFraction, v)
			_, err := NewBudgetFromEnv()
			if err == nil {
				t.Fatalf("%s=%s must fail-fast as out of range", envBranchSoftFraction, v)
			}
			if !strings.Contains(err.Error(), "must be in (0,1]") {
				t.Fatalf("range error should name the allowed interval, got %q", err.Error())
			}
		})
	}
}

func TestNewBudgetFromEnv_FailFast_MalformedResultCap(t *testing.T) {
	t.Setenv(envDedupResultCap, "wide")
	_, err := NewBudgetFromEnv()
	if err == nil {
		t.Fatal("malformed AURA_LOOP_DEDUP_RESULT_CAP must fail-fast")
	}
	want := errMalformed(envDedupResultCap, "wide")
	if err.Error() != want.Error() {
		t.Fatalf("fail-fast error string:\n want %q\n got  %q", want.Error(), err.Error())
	}
}

func TestNewBudgetFromEnv_FailFast_MalformedNodeTimeout(t *testing.T) {
	t.Setenv(envNodeTimeoutSec, "slow")
	_, err := NewBudgetFromEnv()
	if err == nil {
		t.Fatal("malformed AURA_LOOP_NODE_TIMEOUT_SEC must fail-fast")
	}
	want := errMalformed(envNodeTimeoutSec, "slow")
	if err.Error() != want.Error() {
		t.Fatalf("fail-fast error string:\n want %q\n got  %q", want.Error(), err.Error())
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
	for range 10 {
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

func TestBudget_SoftCapExceeded_AtThreshold(t *testing.T) {
	b := newTestBudget(20, 3)
	child := b.Child(4) // branchSoftCap = 5
	for i := 0; i < child.branchSoftCap-1; i++ {
		if ok, reason := child.ConsumeStep(); !ok {
			t.Fatalf("consume before soft cap failed with %q", reason)
		}
	}
	if child.SoftCapExceeded() {
		t.Fatal("soft cap must not report true before the threshold")
	}
	if ok, reason := child.ConsumeStep(); !ok {
		t.Fatalf("consume at soft cap failed with %q", reason)
	}
	if !child.SoftCapExceeded() {
		t.Fatal("soft cap must report true exactly at the threshold")
	}
}

func TestSoftCap_ZeroFanoutAndMinimumShare(t *testing.T) {
	if got := softCap(10, 0, 1); got != 10 {
		t.Fatalf("zero fanout should fall back to one branch: want 10, got %d", got)
	}
	if got := softCap(0, 4, 1); got != 1 {
		t.Fatalf("soft cap should have a minimum share of 1, got %d", got)
	}
}
