package handlers

import (
	"context"
	"fmt"
	"time"
)

// KindSandboxReap is the system-seeded idle-suspend reaper TaskKind (Phase 37 D-08). Like
// identity_purge and skill_ttl_sweep it is NOT model-schedulable — the composition root seeds
// it and the dispatcher routes it here. It is defined in this package (not the shared
// handler.go list) because its scheduler seeding is wired at the cutover (plan 37-05); the
// handler + its seam ship and unit-prove here.
const KindSandboxReap TaskKind = "sandbox_reap"

// sandboxReapMaxDuration bounds one reap sweep. Suspending an idle box is a single moby stop
// per box, so a 5-minute budget is generous even when several boxes fall idle in the same tick.
const sandboxReapMaxDuration = 5 * time.Minute

// SandboxReaper is the consumer-declared seam the reap handler drives (the IdentityPurger
// pattern): the live per-identity sandbox router satisfies it via SuspendIdle (wired in plan
// 37-05), so this package does NOT import the sandbox runtime package — avoiding the
// reverse-import cycle exactly as identity_purge avoids importing internal/agui (D-08).
// SuspendIdle suspends every box past its idle TTL and returns the count suspended.
type SandboxReaper interface {
	SuspendIdle(ctx context.Context, now time.Time) (suspended int, err error)
}

// SandboxReapHandler is the idle-suspend reap sweep (D-08). It scans for per-identity boxes
// whose idle TTL has elapsed and suspends each. A missed sweep does NOT reschedule (the sweep
// is idempotent — the next tick re-evaluates the same idle set).
type SandboxReapHandler struct {
	Reaper SandboxReaper
	// now is injectable for tests; nil → time.Now().UTC().
	now func() time.Time
}

// Meta declares the sweep contract: a 5-minute budget, no reschedule-on-recovery (the reap
// sweep is idempotent, so re-running the same idle set is safe).
func (SandboxReapHandler) Meta() HandlerMeta {
	return HandlerMeta{Kind: KindSandboxReap, MaxDuration: sandboxReapMaxDuration, ReschedulesOnRecovery: false}
}

// Run suspends every box past its idle TTL and returns a count summary. A nil reaper is a
// no-op success (the sweep is harmlessly disabled, not an error). A scan/suspend error is
// terminal for this run (the dispatcher records + notifies it); already-suspended boxes stay
// suspended and the next tick resumes the rest.
func (h SandboxReapHandler) Run(ctx context.Context, _ Job) (string, error) {
	if h.Reaper == nil {
		return "sandbox reap: disabled (no reaper)", nil
	}
	now := time.Now().UTC()
	if h.now != nil {
		now = h.now()
	}
	suspended, err := h.Reaper.SuspendIdle(ctx, now)
	if err != nil {
		return "", fmt.Errorf("sandbox reap: %w", err)
	}
	return fmt.Sprintf("sandbox reap ok: suspended %d idle box(es)", suspended), nil
}
