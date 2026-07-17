// cascade.go implements the D-15 conversation-delete cascade: RevokeConversationShares is the
// concrete method the consumer-declared ShareRevoker seam in internal/runner/runner_delete.go
// (step 4.5) drives through the live *Service at the composition root, so internal/runner never
// imports this package. R-10 is the reason this file exists at all:
// aura.shared_links.conversation_id ON DELETE CASCADE removes the ROW when the conversation is
// deleted, but the Garage BYTES survive with nothing left pointing at them — unreclaimable even
// by the expiry sweep (expirer.go), which only ever scans rows that still exist. FK = belt,
// this hook = suspenders.
package share

import (
	"context"
	"errors"
	"fmt"
)

// RevokeConversationShares revokes every still-live share link for conversationID, owner-
// scoped to identityID, dropping each one's Garage bytes — the D-15 cascade
// Runner.DeleteConversationLifecycle step 4.5 drives BEFORE the persistence delete removes the
// conversation row (and, via FK CASCADE, every shared_links row with it).
//
// It revokes each live link through Revoke (drop-blobs-then-stamp, R-10's mandatory ordering,
// already audited "revoke") rather than a bespoke bulk stamp: reusing Revoke means this cascade
// can never drift from the single already-tested crash-safe ordering every other revoke path
// uses — a bulk stamp-then-drop (the one aura.shared_links.RevokeForConversation alone would
// produce) would instead orphan bytes permanently on a crash between the two, exactly the R-10
// trap this whole plan exists to close.
//
// A per-link failure is collected and processing continues to the next link — a transient
// object-store error on one share must not abandon cleanup of the conversation's other shares
// — and the joined error (nil if every link succeeded) is returned for the caller (the runner
// lifecycle) to WARN on, best-effort.
func (svc *Service) RevokeConversationShares(ctx context.Context, conversationID, identityID string) error {
	links, err := svc.store.ListForConversation(ctx, conversationID, identityID)
	if err != nil {
		return fmt.Errorf("revoke conversation shares %s: %w", conversationID, err)
	}
	var errs []error
	for _, link := range links {
		if link.RevokedAt != nil {
			continue // already revoked — Revoke is not idempotent-silent (ErrShareNotFound), so skip it here
		}
		if err := svc.Revoke(ctx, link.ID.String(), identityID); err != nil {
			errs = append(errs, fmt.Errorf("revoke share %s: %w", link.ID, err))
		}
	}
	return errors.Join(errs...)
}
