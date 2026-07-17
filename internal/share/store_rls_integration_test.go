//go:build db_integration

// store_rls_integration_test.go proves the migration 0041 RLS backstop on
// aura.shared_links actually bites at the DATABASE layer — closing the 37F-07 gap (see
// 0041_shared_links_rls.up.sql + docs/adr/0039-conversation-sharing-vs-identity-isolation.md
// for the design). Split from store_integration_test.go / store_mutations_integration_test.go
// (CLAUDE.md NO GOD CLASS / file-size cap); shares those files' migratedPool/seedIdentity/
// seedConversation/newLink/futurePtr helpers (same package, same build tag).
//
// Run via:
//
//	go test -tags db_integration -race -run RLS ./internal/share/ -count=1
package share

import (
	"context"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/jackc/pgx/v5"
)

// TestShareStoreRLSDeniesCrossIdentityRead is the migration 0041 backstop proof: a raw,
// deliberately unfiltered `SELECT id FROM aura.shared_links WHERE id = $1` — no owner
// predicate anywhere in the query text — run inside WithIdentityTxRaw for a FOREIGN
// identity returns ZERO rows for another identity's share link. The database itself
// denies it; the query never had a chance to.
func TestShareStoreRLSDeniesCrossIdentityRead(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx := context.Background()

	ownerA := seedIdentity(t, pool)
	ownerB := seedIdentity(t, pool)
	convA := seedConversation(t, pool, ownerA)

	link := newLink(ownerA, convA, "internal", nil)
	if err := store.Insert(ctx, link, nil); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var seenRows int
	if err := db.WithIdentityTxRaw(ctx, pool, ownerB, func(tx pgx.Tx) error {
		rows, qErr := tx.Query(ctx, `SELECT id FROM aura.shared_links WHERE id = $1`, link.ID)
		if qErr != nil {
			return qErr
		}
		defer rows.Close()
		for rows.Next() {
			seenRows++
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("WithIdentityTxRaw(foreign identity) unfiltered select: %v", err)
	}
	if seenRows != 0 {
		t.Fatalf("RLS backstop LEAKED a foreign share link (isolation broken): got %d rows under the foreign identity, want 0", seenRows)
	}
}

// TestShareStoreRLSOwnerStillSeesOwnRow is the mandatory self-lockout guard (see plan
// notes): without it, a policy of USING (false) would pass
// TestShareStoreRLSDeniesCrossIdentityRead vacuously while breaking the entire feature.
// The identical unfiltered query (no owner predicate) run under the TRUE owner's
// identity must return exactly the one row that owner created.
func TestShareStoreRLSOwnerStillSeesOwnRow(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx := context.Background()

	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)

	link := newLink(owner, conv, "internal", nil)
	if err := store.Insert(ctx, link, nil); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var seenRows int
	if err := db.WithIdentityTxRaw(ctx, pool, owner, func(tx pgx.Tx) error {
		rows, qErr := tx.Query(ctx, `SELECT id FROM aura.shared_links WHERE id = $1`, link.ID)
		if qErr != nil {
			return qErr
		}
		defer rows.Close()
		for rows.Next() {
			seenRows++
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("WithIdentityTxRaw(true owner) unfiltered select: %v", err)
	}
	if seenRows != 1 {
		t.Fatalf("RLS backstop hid the true owner's OWN share link (self-lockout): got %d rows under the owning identity, want 1", seenRows)
	}
}

// TestShareStoreRLSPublicLaneUnaffected proves migration 0041's permissive-on-unset
// branch keeps both plain-pool resolvers working exactly as before: ResolveByToken
// (public /s/{token}) and ResolveLiveByID (internal /shared/{id} bearer-within-auth,
// D-10) never set app.current_identity, so the new policy must stay permissive for both
// after this migration — a fail-closed-on-unset policy would silently 404 every public
// and internal-bearer share.
func TestShareStoreRLSPublicLaneUnaffected(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx := context.Background()

	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)

	_, hash, err := Mint()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	publicLink := newLink(owner, conv, "public", futurePtr(24*time.Hour))
	if err := store.Insert(ctx, publicLink, hash[:]); err != nil {
		t.Fatalf("insert public link: %v", err)
	}
	gotPublic, err := store.ResolveByToken(ctx, hash[:])
	if err != nil {
		t.Fatalf("ResolveByToken after migration 0041: %v", err)
	}
	if gotPublic.ID != publicLink.ID {
		t.Fatalf("ResolveByToken returned %s, want %s", gotPublic.ID, publicLink.ID)
	}

	internalLink := newLink(owner, conv, "internal", nil)
	if err := store.Insert(ctx, internalLink, nil); err != nil {
		t.Fatalf("insert internal link: %v", err)
	}
	gotInternal, err := store.ResolveLiveByID(ctx, internalLink.ID)
	if err != nil {
		t.Fatalf("ResolveLiveByID after migration 0041: %v", err)
	}
	if gotInternal.ID != internalLink.ID {
		t.Fatalf("ResolveLiveByID returned %s, want %s", gotInternal.ID, internalLink.ID)
	}
}
