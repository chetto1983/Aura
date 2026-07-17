//go:build db_integration

// store_mutations_integration_test.go covers the remaining Store methods not exercised by
// store_integration_test.go's security-property tests: the plain listing/update/sweep paths
// (ListForIdentity, ListForConversation, UpdateSnapshot, RevokeForConversation,
// DueForExpiry). Split into its own file (CLAUDE.md NO GOD CLASS / file-size cap) rather than
// growing store_integration_test.go past 600 LOC; shares that file's envOrSkip/migratedPool/
// seedIdentity/seedConversation/newLink/futurePtr/pastPtr helpers (same package, same tag).
package share

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestShareStoreListForIdentity proves newest-first ordering (shared_links_owner_idx) and
// that a foreign identity's list never includes another owner's links.
func TestShareStoreListForIdentity(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx := context.Background()

	owner := seedIdentity(t, pool)
	other := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)
	convOther := seedConversation(t, pool, other)

	first := newLink(owner, conv, "internal", nil)
	if err := store.Insert(ctx, first, nil); err != nil {
		t.Fatalf("insert first: %v", err)
	}
	time.Sleep(10 * time.Millisecond) // created_at DESC needs distinguishable timestamps
	second := newLink(owner, conv, "internal", nil)
	if err := store.Insert(ctx, second, nil); err != nil {
		t.Fatalf("insert second: %v", err)
	}
	foreign := newLink(other, convOther, "internal", nil)
	if err := store.Insert(ctx, foreign, nil); err != nil {
		t.Fatalf("insert foreign: %v", err)
	}

	got, err := store.ListForIdentity(ctx, owner, 10, 0)
	if err != nil {
		t.Fatalf("ListForIdentity: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListForIdentity = %d links, want 2 (owner's own only)", len(got))
	}
	if got[0].ID != second.ID || got[1].ID != first.ID {
		t.Fatalf("ListForIdentity order = [%s, %s], want newest-first [%s, %s]", got[0].ID, got[1].ID, second.ID, first.ID)
	}
	for _, l := range got {
		if l.OwnerIdentityID.String() != owner {
			t.Fatalf("ListForIdentity leaked a foreign link: %s owned by %s", l.ID, l.OwnerIdentityID)
		}
	}

	// limit/offset pagination.
	page, err := store.ListForIdentity(ctx, owner, 1, 1)
	if err != nil {
		t.Fatalf("ListForIdentity (paginated): %v", err)
	}
	if len(page) != 1 || page[0].ID != first.ID {
		t.Fatalf("ListForIdentity(limit=1,offset=1) = %+v, want [%s]", page, first.ID)
	}
}

// TestShareStoreListForConversation proves the conversation-scoped, owner-gated listing
// that drives the "Condiviso" section.
func TestShareStoreListForConversation(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx := context.Background()

	owner := seedIdentity(t, pool)
	other := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)

	link := newLink(owner, conv, "internal", nil)
	if err := store.Insert(ctx, link, nil); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := store.ListForConversation(ctx, conv, owner)
	if err != nil {
		t.Fatalf("ListForConversation(owner): %v", err)
	}
	if len(got) != 1 || got[0].ID != link.ID {
		t.Fatalf("ListForConversation(owner) = %+v, want [%s]", got, link.ID)
	}

	gotForeign, err := store.ListForConversation(ctx, conv, other)
	if err != nil {
		t.Fatalf("ListForConversation(foreign): %v", err)
	}
	if len(gotForeign) != 0 {
		t.Fatalf("ListForConversation(foreign identity) = %d links, want 0 (owner-scoped)", len(gotForeign))
	}
}

// TestShareStoreUpdateSnapshot proves D-06 "Update": a re-pointed snapshot_id + bumped
// updated_at persist, owner-scoped, and a foreign/absent id is ErrShareNotFound.
func TestShareStoreUpdateSnapshot(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx := context.Background()

	owner := seedIdentity(t, pool)
	other := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)

	link := newLink(owner, conv, "internal", nil)
	if err := store.Insert(ctx, link, nil); err != nil {
		t.Fatalf("insert: %v", err)
	}
	before, err := store.GetForIdentity(ctx, link.ID.String(), owner)
	if err != nil {
		t.Fatalf("GetForIdentity before update: %v", err)
	}

	newSnapshot := uuid.Must(uuid.NewV7())
	at := before.UpdatedAt.Add(time.Second) // strictly after the DB-assigned updated_at default
	if err := store.UpdateSnapshot(ctx, link.ID.String(), owner, newSnapshot, at); err != nil {
		t.Fatalf("UpdateSnapshot: %v", err)
	}

	got, err := store.GetForIdentity(ctx, link.ID.String(), owner)
	if err != nil {
		t.Fatalf("GetForIdentity after update: %v", err)
	}
	if got.SnapshotID != newSnapshot {
		t.Fatalf("SnapshotID after update = %s, want %s", got.SnapshotID, newSnapshot)
	}
	if !got.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("UpdatedAt was not bumped: before=%s after=%s", before.UpdatedAt, got.UpdatedAt)
	}

	if err := store.UpdateSnapshot(ctx, link.ID.String(), other, newSnapshot, at); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("UpdateSnapshot(foreign owner) = %v, want ErrShareNotFound", err)
	}
}

// TestShareStoreRevokeForConversation proves the D-15 cascade seam: every still-live link
// for a conversation is revoked in one call and returned (snapshot fields intact) so the
// caller can drop their Garage bytes; an already-revoked link is untouched and not returned.
func TestShareStoreRevokeForConversation(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx := context.Background()

	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)

	live1 := newLink(owner, conv, "internal", nil)
	if err := store.Insert(ctx, live1, nil); err != nil {
		t.Fatalf("insert live1: %v", err)
	}
	live2 := newLink(owner, conv, "internal", nil)
	if err := store.Insert(ctx, live2, nil); err != nil {
		t.Fatalf("insert live2: %v", err)
	}
	alreadyRevoked := newLink(owner, conv, "internal", nil)
	if err := store.Insert(ctx, alreadyRevoked, nil); err != nil {
		t.Fatalf("insert alreadyRevoked: %v", err)
	}
	if err := store.RevokeForIdentity(ctx, alreadyRevoked.ID.String(), owner, time.Now().UTC()); err != nil {
		t.Fatalf("pre-revoke alreadyRevoked: %v", err)
	}

	revoked, err := store.RevokeForConversation(ctx, conv, owner, time.Now().UTC())
	if err != nil {
		t.Fatalf("RevokeForConversation: %v", err)
	}
	if len(revoked) != 2 {
		t.Fatalf("RevokeForConversation returned %d links, want 2 (only the previously-live ones)", len(revoked))
	}
	seen := map[uuid.UUID]bool{}
	for _, l := range revoked {
		seen[l.ID] = true
		if l.RevokedAt == nil {
			t.Fatalf("revoked link %s has nil RevokedAt", l.ID)
		}
		if l.SnapshotBucket != "share-test-bucket" {
			t.Fatalf("revoked link %s lost its snapshot bucket: %q", l.ID, l.SnapshotBucket)
		}
	}
	if !seen[live1.ID] || !seen[live2.ID] {
		t.Fatalf("RevokeForConversation did not return both live links: %+v", revoked)
	}
	if seen[alreadyRevoked.ID] {
		t.Fatalf("RevokeForConversation re-returned an already-revoked link: %s", alreadyRevoked.ID)
	}
}

// TestShareStoreDueForExpiry proves the sweep's due-set query: a still-live link past its
// expiry is due; a revoked link and a not-yet-expired link are both excluded.
func TestShareStoreDueForExpiry(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx := context.Background()

	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)

	_, hashDue, err := Mint()
	if err != nil {
		t.Fatalf("mint due: %v", err)
	}
	due := newLink(owner, conv, "public", pastPtr(time.Hour))
	if err := store.Insert(ctx, due, hashDue[:]); err != nil {
		t.Fatalf("insert due: %v", err)
	}

	_, hashRevoked, err := Mint()
	if err != nil {
		t.Fatalf("mint revoked: %v", err)
	}
	revokedButExpired := newLink(owner, conv, "public", pastPtr(time.Hour))
	if err := store.Insert(ctx, revokedButExpired, hashRevoked[:]); err != nil {
		t.Fatalf("insert revokedButExpired: %v", err)
	}
	if err := store.RevokeForIdentity(ctx, revokedButExpired.ID.String(), owner, time.Now().UTC()); err != nil {
		t.Fatalf("revoke revokedButExpired: %v", err)
	}

	_, hashNotYet, err := Mint()
	if err != nil {
		t.Fatalf("mint notYet: %v", err)
	}
	notYetDue := newLink(owner, conv, "public", futurePtr(24*time.Hour))
	if err := store.Insert(ctx, notYetDue, hashNotYet[:]); err != nil {
		t.Fatalf("insert notYetDue: %v", err)
	}

	got, err := store.DueForExpiry(ctx, time.Now().UTC(), 50)
	if err != nil {
		t.Fatalf("DueForExpiry: %v", err)
	}
	var sawDue, sawRevoked, sawNotYet bool
	for _, l := range got {
		switch l.ID {
		case due.ID:
			sawDue = true
		case revokedButExpired.ID:
			sawRevoked = true
		case notYetDue.ID:
			sawNotYet = true
		}
	}
	if !sawDue {
		t.Fatalf("DueForExpiry missed the due-and-live link %s", due.ID)
	}
	if sawRevoked {
		t.Fatalf("DueForExpiry included a revoked link %s (revoked_at IS NULL predicate)", revokedButExpired.ID)
	}
	if sawNotYet {
		t.Fatalf("DueForExpiry included a not-yet-expired link %s", notYetDue.ID)
	}
}
