// expirer.go implements ExpireDue — the batch expiry the cron share_expiry_sweep handler
// (internal/cron/handlers/share_expiry.go) drives via the ShareExpirer seam. It is GARBAGE
// COLLECTION only, never the security gate: ResolveByToken/ResolveLiveByID's lazy fail-closed
// liveness predicate (plan 37F-07) already denies an expired-but-unswept link, sweep or no
// sweep. Without this sweep an expired link is merely unreachable — its Garage bytes live on
// unbounded, which is the data-retention gap this file closes (37F-RESEARCH.md OQ3).
package share

import (
	"context"
	"fmt"
	"time"
)

// expireDueBatchLimit bounds one ExpireDue tick's DueForExpiry scan — a small indexed query
// over shared_links_expiry_idx plus idempotent per-link teardown, so a generous batch keeps one
// tick's Postgres round trip cheap even when many links fall due in the same window. A backlog
// beyond this limit is picked up by the NEXT tick (the sweep is idempotent + resumable).
const expireDueBatchLimit = 200

// ExpireDue drops the Garage bytes of every still-live link whose expires_at has passed now,
// THEN stamps each one's row, auditing "expire" per link. The per-link order is the whole
// correctness argument, matching runner_delete.go's numbered-steps discipline:
//
//  1. drop the blobs (dropBlobs, idempotent on an already-empty prefix)
//  2. stamp the row (RevokeForIdentity, reused rather than a bespoke expiry-only stamp — an
//     expired link and a revoked link are the SAME "closed" row state; only the AUDITED
//     action differs)
//  3. audit "expire"
//
// Never stamp-then-drop: a crash between 1 and 2 re-runs the idempotent delete on the next
// tick (DueForExpiry re-selects the same still-live, still-due row; dropBlobs on an
// already-empty prefix is a no-op). A stamp-then-drop order would instead orphan the bytes
// permanently the moment the stamp lands and a crash follows — DueForExpiry's own WHERE clause
// (revoked_at IS NULL) excludes a stamped row from every future scan, so nothing would ever
// retry the drop.
//
// A per-link failure is terminal for this tick (the count achieved so far is returned
// alongside the wrapped error): the next tick re-evaluates the remaining due set, so ExpireDue
// itself needs no in-process retry loop. Re-running over an already-swept link is a no-op, not
// an error — DueForExpiry simply stops returning it once revoked_at is stamped.
func (svc *Service) ExpireDue(ctx context.Context, now time.Time) (int, error) {
	due, err := svc.store.DueForExpiry(ctx, now, expireDueBatchLimit)
	if err != nil {
		return 0, fmt.Errorf("expire due shares: %w", err)
	}
	expired := 0
	for _, link := range due {
		// 1. Drop blobs, THEN stamp — never the reverse (see doc comment above).
		if err := dropBlobs(ctx, svc.objects, svc.bucket, link.ID); err != nil {
			return expired, fmt.Errorf("expire due share %s: drop blobs: %w", link.ID, err)
		}
		// 2. Stamp the row via the existing owner-scoped revoke primitive.
		owner := link.OwnerIdentityID.String()
		if err := svc.store.RevokeForIdentity(ctx, link.ID.String(), owner, now); err != nil {
			return expired, fmt.Errorf("expire due share %s: stamp: %w", link.ID, err)
		}
		// 3. Audit "expire" (D-14 names expire as an audited action).
		if err := svc.audit.Append(ctx, owner, link.ID, link.ConversationID, AuditExpire, link.Tier, ""); err != nil {
			return expired, fmt.Errorf("expire due share %s: audit: %w", link.ID, err)
		}
		expired++
	}
	return expired, nil
}
