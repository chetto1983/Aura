package handlers

import (
	"context"
	"time"
)

// KindIdentityPurge is the system-seeded soft-delete grace-window purge TaskKind (Phase 36
// D-27). Like skill_ttl_sweep it is NOT model-schedulable — the composition root seeds it,
// and the dispatcher routes it here. It is defined in this package (not the shared handler.go
// list) because its scheduler seeding is wired at the cutover; the handler + its seam ship
// and unit-prove here.
const KindIdentityPurge TaskKind = "identity_purge"

// identityPurgeMaxDuration bounds one purge sweep. The scan is a small indexed query and the
// per-identity teardown is a handful of idempotent store deletes, so a 5-minute budget is
// generous even when several identities fall due in the same tick.
const identityPurgeMaxDuration = 5 * time.Minute

// IdentityPurger is the consumer-declared seam the purge handler drives (the SnippetSweeper
// pattern): the live *agui.Deprovisioner satisfies it via PurgeExpired, so this package does
// NOT import internal/agui (D-24, and it avoids the reverse-import cycle). PurgeExpired finds
// every identity past its purge_after grace window and runs the symmetric de-provisioning
// saga on each (idempotent + resumable), returning the count purged.
type IdentityPurger interface {
	PurgeExpired(ctx context.Context, now time.Time) (purged int, err error)
}

// NewIdentityPurgeHandler builds the grace-window purge sweep (D-27) over purger: it scans for
// identities past their soft-delete grace window and runs the de-provisioning purge saga on
// each, reporting the count. A nil purger yields the disabled no-op sweep (harmlessly off, not
// an error). The missed sweep never reschedules (the saga is idempotent + resumable — the next
// tick re-evaluates the same purge_after set and resumes any partially-purged identity).
func NewIdentityPurgeHandler(purger IdentityPurger) Handler {
	var seam sweepFn
	if purger != nil {
		seam = purger.PurgeExpired
	}
	return newCountingSweep(KindIdentityPurge, identityPurgeMaxDuration, seam,
		"identity purge: disabled (no purger)", "identity purge", "identity purge ok: purged %d expired identit(y/ies)")
}
