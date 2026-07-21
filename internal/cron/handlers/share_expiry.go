package handlers

import (
	"context"
	"time"
)

// KindShareExpirySweep is the system-seeded share-link expiry TaskKind (Phase 37F D-15/OQ3),
// added to the scheduler_tasks kind CHECK by migration 0040. Like identity_purge and
// sandbox_reap it is NOT model-schedulable — the composition root seeds it, and the dispatcher
// routes it here. It is defined in this package (not the shared handler.go list) because its
// scheduler seeding is wired at a later cutover; the handler + its seam ship and unit-prove
// here. It is GARBAGE COLLECTION only: the security gate is the lazy fail-closed liveness
// predicate already in ResolveByToken/ResolveLiveByID (plan 37F-07), which denies an
// expired-but-unswept link even if this sweep never runs.
const KindShareExpirySweep TaskKind = "share_expiry_sweep"

// shareExpiryMaxDuration bounds one expiry sweep. The scan is a small indexed query
// (shared_links_expiry_idx) plus idempotent per-link teardown, so a 5-minute budget is
// generous even when several links fall due in the same tick — mirroring
// identityPurgeMaxDuration's identical reasoning.
const shareExpiryMaxDuration = 5 * time.Minute

// ShareExpirer is the consumer-declared seam the sweep handler drives (the IdentityPurger
// pattern): the live *share.Service satisfies it via ExpireDue, so this package does NOT
// import internal/share (D-24, and it avoids the reverse-import cycle). ExpireDue drops the
// Garage bytes of every still-live link whose expires_at has passed, THEN stamps its row,
// auditing "expire" per link (idempotent + resumable), and returns the count expired.
type ShareExpirer interface {
	ExpireDue(ctx context.Context, now time.Time) (expired int, err error)
}

// NewShareExpiryHandler builds the share-link expiry sweep (D-15/OQ3) over expirer: it scans
// for still-live links past their expires_at and reclaims each one's Garage bytes, reporting
// the count. A nil expirer yields the disabled no-op sweep (harmlessly off, not an error) —
// exactly the identity_purge nil-Purger posture. The missed sweep never reschedules (the sweep
// is idempotent — the next tick re-evaluates the same due set).
func NewShareExpiryHandler(expirer ShareExpirer) Handler {
	var seam sweepFn
	if expirer != nil {
		seam = expirer.ExpireDue
	}
	return newCountingSweep(KindShareExpirySweep, shareExpiryMaxDuration, seam,
		"share expiry: disabled (no expirer)", "share expiry", "share expiry ok: expired %d link(s)")
}
