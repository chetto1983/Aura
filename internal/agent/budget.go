// Budget is the cornerstone resource-exhaustion control (a DoS-prevention
// mechanism, ASVS V11): a single shared *atomic.Int32 step counter bounds the
// WHOLE agent tree, a wallclock deadline bounds total time, and a two-phase
// dedup ring (budget_dedup.go) bounds repeated tool calls.
//
// Pattern derivato da google/adk-go v1.4.0 agent/workflowagents/loopagent/agent.go
// (Apache 2.0). Adattato per Aura con SC#2 budget exhaustion + SC#3
// child-inherits-remaining + SC#4 UUIDv7 OTel-compat.
//
// The shared-atomic design (D-10) is what makes SC#3 true: a depth-3 fan-3 tree
// consumes ≤ max_steps total, NOT max_steps³ — it avoids the fresh-per-child
// explosion that nanobot-style designs fall into. Budget.Child shares the SAME
// counter by pointer and never forks it.
package agent

import (
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

// AURA_LOOP_* env var names (AURA_<DOMAIN>_<UNIT> convention). The hard 3-cap
// contract is steps + wallclock + dedup; the rest are hardening knobs (A7).
const (
	envMaxSteps           = "AURA_LOOP_MAX_STEPS"
	envMaxWallclockSec    = "AURA_LOOP_MAX_WALLCLOCK_SEC"
	envDedupWindow        = "AURA_LOOP_DEDUP_WINDOW" //nolint:gosec // G101 false positive: env var NAME, not a credential
	envBranchSoftFraction = "AURA_LOOP_BRANCH_SOFT_FRACTION"
	envNodeTimeoutSec     = "AURA_LOOP_NODE_TIMEOUT_SEC"
	envDedupExemptTools   = "AURA_LOOP_DEDUP_EXEMPT_TOOLS"
	envDedupResultCap     = "AURA_LOOP_DEDUP_RESULT_CAP"
)

// Builtin defaults (D-06 precedence: CLI flag > env > builtin default).
const (
	defaultBudgetMaxSteps     = 25   // AURA_LOOP_MAX_STEPS
	defaultBudgetWallclockSec = 300  // AURA_LOOP_MAX_WALLCLOCK_SEC
	defaultDedupWindow        = 3    // AURA_LOOP_DEDUP_WINDOW (D-20)
	defaultBranchSoftFrac     = 1.0  // AURA_LOOP_BRANCH_SOFT_FRACTION: 1.0 → softCap = ceil(remaining/fanout)
	defaultDedupResultCap     = 2048 // AURA_LOOP_DEDUP_RESULT_CAP: result-preview byte cap (A7)
)

// Budget bounds one agent run. The steps counter is shared by pointer across the
// whole tree (D-10); deadlineWallclock and now are shared by value; the dedup
// ring is per-branch (forked by Child, D-09).
type Budget struct {
	steps             *atomic.Int32       // shared step counter (D-10), decrement-then-check-then-restore (D-11)
	deadlineWallclock time.Time           // hard wallclock cap; ConsumeStep refuses new steps past it (D-13)
	now               func() time.Time    // injectable clock (W8): tests drive the deadline deterministically; default time.Now
	dedupWindow       int                 // consecutive-repeat threshold (default 3, D-20)
	dedupRing         *dedupRing          // per-branch two-phase dedup state (budget_dedup.go); distinct per Child (D-09)
	branchSoftCap     int                 // passive per-branch fair-share advisory (D-12); 0 = unset (root)
	branchConsumed    atomic.Int32        // steps this branch has consumed, for the passive soft-cap check (D-12)
	exemptTools       map[string]struct{} // AURA_LOOP_DEDUP_EXEMPT_TOOLS allowlist (D-19)
	resultCap         int                 // dedup result-preview byte cap (A7)
	nodeTimeout       time.Duration       // optional per-node soft timeout (D-13); 0 = disabled
	softFrac          float64             // AURA_LOOP_BRANCH_SOFT_FRACTION, feeds Child's softCap (D-12)
}

// now defaults to time.Now via the W8 injectable-clock field. W8 RATIONALE:
// wallclock uses an injectable clock, NOT Go 1.26 testing/synctest. synctest
// spawns background goroutines that can trip the workflow_test.go
// goleak.VerifyTestMain (SC#1); a plain func field keeps wallclock tests
// deterministic with zero goroutine cost.

// errMalformed is the verbatim fail-fast error for a non-parseable AURA_LOOP_*
// value (D-06). Tests assert this string byte-for-byte, so do not paraphrase.
func errMalformed(key, val string) error {
	return fmt.Errorf("%s=%q: not a valid value", key, val)
}

// NewBudgetFromEnv builds a Budget from the AURA_LOOP_* environment, fail-fast on
// any malformed value (D-06) — it never silently falls back to a default on a
// parse error (the opposite of internal/config's silent-absorb int helper). Unset vars use
// the builtin defaults; only a SET-but-malformed value is an error.
func NewBudgetFromEnv() (*Budget, error) {
	maxSteps, err := envIntFailFast(envMaxSteps, defaultBudgetMaxSteps)
	if err != nil {
		return nil, err
	}
	wallclockSec, err := envIntFailFast(envMaxWallclockSec, defaultBudgetWallclockSec)
	if err != nil {
		return nil, err
	}
	dedupWindow, err := envIntFailFast(envDedupWindow, defaultDedupWindow)
	if err != nil {
		return nil, err
	}
	softFrac, err := envFloatFailFast(envBranchSoftFraction, defaultBranchSoftFrac)
	if err != nil {
		return nil, err
	}
	if softFrac <= 0 || softFrac > 1 {
		return nil, fmt.Errorf("%s=%q: must be in (0,1]", envBranchSoftFraction, os.Getenv(envBranchSoftFraction))
	}
	resultCap, err := envIntFailFast(envDedupResultCap, defaultDedupResultCap)
	if err != nil {
		return nil, err
	}
	nodeTimeoutSec, err := envIntFailFast(envNodeTimeoutSec, 0)
	if err != nil {
		return nil, err
	}

	var steps atomic.Int32
	steps.Store(int32(maxSteps))

	b := &Budget{
		steps:             &steps,
		deadlineWallclock: time.Now().Add(time.Duration(wallclockSec) * time.Second),
		now:               time.Now,
		dedupWindow:       dedupWindow,
		dedupRing:         newDedupRing(dedupWindow),
		exemptTools:       parseExemptTools(os.Getenv(envDedupExemptTools)),
		resultCap:         resultCap,
		nodeTimeout:       time.Duration(nodeTimeoutSec) * time.Second,
		softFrac:          softFrac,
	}
	return b, nil
}

// envIntFailFast reads key as an int, returning fallback when unset/empty and a
// verbatim errMalformed when set-but-unparseable (D-06).
func envIntFailFast(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, errMalformed(key, v)
	}
	return n, nil
}

// envFloatFailFast reads key as a float, fail-fast on a set-but-unparseable value.
func envFloatFailFast(key string, fallback float64) (float64, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, errMalformed(key, v)
	}
	return f, nil
}

// ConsumeStep is the TOCTOU-safe (D-11) gate every agent step passes through.
// It checks the wallclock FIRST (D-13), then does an atomic
// decrement-then-check-then-restore so N concurrent goroutines can never
// over-spend the shared cap: the goroutines that overshoot below zero all add 1
// back. Only HARD terminal reasons are returned ("max_steps" | "wallclock") —
// the per-branch soft cap is NON-terminal and surfaced via SoftCapExceeded (D-12).
func (b *Budget) ConsumeStep() (ok bool, reason string) {
	if b.now().After(b.deadlineWallclock) {
		return false, "wallclock"
	}
	if n := b.steps.Add(-1); n < 0 {
		b.steps.Add(1) // restore — every overshooting goroutine undoes its decrement
		return false, "max_steps"
	}
	b.branchConsumed.Add(1)
	return true, ""
}

// Remaining is the current shared step balance (never negative once restored).
func (b *Budget) Remaining() int {
	n := b.steps.Load()
	if n < 0 {
		return 0
	}
	return int(n)
}

// SetMaxSteps overrides the shared step counter (CLI flag precedence, D-06).
// It resets the WHOLE tree's remaining budget; intended only at boot before any
// child is spawned.
func (b *Budget) SetMaxSteps(n int32) {
	b.steps.Store(n)
}

// SoftCapExceeded reports whether THIS branch has consumed at least its passive
// fair-share soft cap (D-12). It is a non-terminal scheduling/fairness signal:
// the hard total bound (steps) stays authoritative, the branch may still consume
// against the shared pool. A root budget (branchSoftCap==0) never reports true.
func (b *Budget) SoftCapExceeded() bool {
	if b.branchSoftCap <= 0 {
		return false
	}
	return int(b.branchConsumed.Load()) >= b.branchSoftCap
}

// Child forks a budget for a PARALLEL branch (D-09): it shares the SAME
// *atomic.Int32 step counter and the same wallclock + clock (so the hard total
// bound is preserved across the tree), but gets a DISTINCT dedup ring (no
// cross-branch false positives) and a PASSIVE per-branch soft cap (D-12).
// Sequential/Loop sub-agents instead use InvocationContext.WithSubAgent, which
// shares the ring for cross-iteration repeat detection.
func (b *Budget) Child(fanout int) *Budget {
	c := &Budget{
		steps:             b.steps, // SHARED pointer (D-10) — total bound preserved
		deadlineWallclock: b.deadlineWallclock,
		now:               b.now,
		dedupWindow:       b.dedupWindow,
		dedupRing:         newDedupRing(b.dedupWindow), // DISTINCT ring (D-09)
		branchSoftCap:     softCap(b.Remaining(), fanout, b.softFrac),
		exemptTools:       b.exemptTools,
		resultCap:         b.resultCap,
		nodeTimeout:       b.nodeTimeout,
		softFrac:          b.softFrac,
	}
	return c
}

// softCap computes a passive per-branch fair share: ceil(remaining * frac / fanout),
// at least 1 (D-12). frac (AURA_LOOP_BRANCH_SOFT_FRACTION) defaults to 1.0, so the
// default cap is ceil(remaining/fanout) — a branch's even slice of the shared pool.
func softCap(remaining, fanout int, frac float64) int {
	if fanout <= 0 {
		fanout = 1
	}
	cap := int(math.Ceil(float64(remaining) * frac / float64(fanout)))
	if cap < 1 {
		cap = 1
	}
	return cap
}

// WithDeadline derives a context bounded by the budget's wallclock deadline so
// in-flight LLM/tool calls are cancelled end-to-end, not just blocked from new
// steps (D-13). The caller owns the returned CancelFunc.
func (b *Budget) WithDeadline(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithDeadline(parent, b.deadlineWallclock)
}

// NodeTimeout is the optional per-node soft timeout (AURA_LOOP_NODE_TIMEOUT_SEC,
// D-13); zero means disabled.
func (b *Budget) NodeTimeout() time.Duration { return b.nodeTimeout }
