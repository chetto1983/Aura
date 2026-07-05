package handlers

import (
	"context"
	"fmt"
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

// IdentityPurgeHandler is the grace-window purge sweep (D-27). It scans for identities whose
// soft-delete grace window has elapsed and runs the de-provisioning purge saga on each. A
// missed sweep does NOT reschedule (the saga is idempotent — the next tick re-evaluates the
// same purge_after set and resumes any partially-purged identity).
type IdentityPurgeHandler struct {
	Purger IdentityPurger
	// now is injectable for tests; nil → time.Now().UTC().
	now func() time.Time
}

// Meta declares the sweep contract: a 5-minute budget, no reschedule-on-recovery (the purge
// saga is idempotent + resumable, so re-running the same due set is safe).
func (IdentityPurgeHandler) Meta() HandlerMeta {
	return HandlerMeta{Kind: KindIdentityPurge, MaxDuration: identityPurgeMaxDuration, ReschedulesOnRecovery: false}
}

// Run purges every identity past its grace window and returns a count summary. A nil purger
// is a no-op success (the sweep is harmlessly disabled, not an error). A scan/purge error is
// terminal for this run (the dispatcher records + notifies it); already-purged identities
// stay purged and the next tick resumes the rest.
func (h IdentityPurgeHandler) Run(ctx context.Context, _ Job) (string, error) {
	if h.Purger == nil {
		return "identity purge: disabled (no purger)", nil
	}
	now := time.Now().UTC()
	if h.now != nil {
		now = h.now()
	}
	purged, err := h.Purger.PurgeExpired(ctx, now)
	if err != nil {
		return "", fmt.Errorf("identity purge: %w", err)
	}
	return fmt.Sprintf("identity purge ok: purged %d expired identit(y/ies)", purged), nil
}
