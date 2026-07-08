package handlers

import (
	"context"
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

// NewSandboxReapHandler builds the idle-suspend reap sweep (D-08) over reaper: it scans for
// per-identity boxes past their idle TTL and suspends each, reporting the count. A nil reaper
// yields the disabled no-op sweep (harmlessly off, not an error) — exactly the identity_purge
// nil-Purger posture. The missed sweep never reschedules (it is idempotent — the next tick
// re-evaluates the same idle set).
func NewSandboxReapHandler(reaper SandboxReaper) Handler {
	var seam sweepFn
	if reaper != nil {
		seam = reaper.SuspendIdle
	}
	return newCountingSweep(KindSandboxReap, sandboxReapMaxDuration, seam,
		"sandbox reap: disabled (no reaper)", "sandbox reap", "sandbox reap ok: suspended %d idle box(es)")
}
