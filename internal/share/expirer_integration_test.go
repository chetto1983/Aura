//go:build db_integration

// Integration tests for Service.ExpireDue (the share_expiry_sweep seam target) and the D-15
// expiry-monotonicity property, against a real Postgres-backed aura.shared_links/
// aura.share_audit (migration 0040) and objectstore.NewFake() — no Garage. Reuses
// migratedPool/envOrSkip/seedIdentity/seedConversation/newLink/futurePtr/pastPtr and
// newShareTestService from store_integration_test.go/service_integration_test.go (same
// package, same build tag).
//
// These tests exercise Service.ExpireDue directly rather than routing through
// internal/cron/handlers.NewShareExpiryHandler: the handler is a ~20-LOC copy-and-rename shell
// over newCountingSweep (unit-proved in share_expiry_test.go) whose entire job is calling this
// exact method through the ShareExpirer seam, and internal/cron/handlers must never import
// internal/share (D-24) — so the seam-driving behavior this plan needs to prove lives here,
// where the real Service and a real database are both directly reachable. See the plan's
// SUMMARY for this choice.
//
// Run via:
//
//	go test -tags db_integration -race -p 1 -count=1 ./internal/share/
package share

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/objectstore"
	"pgregory.net/rapid"
)

// seedBlobUnderSnapshot places a minimal placeholder object at link's canonical snapshot key,
// so a test can prove ExpireDue's dropBlobs step actually reclaims REAL bytes rather than
// vacuously passing against an already-empty prefix.
func seedBlobUnderSnapshot(t *testing.T, deps shareTestDeps, link Link) {
	t.Helper()
	if _, err := deps.objects.Put(context.Background(), objectstore.ObjectRef{
		Bucket: deps.bucket, Key: objectstore.ShareSnapshotKey(link.ID, link.SnapshotID),
	}, bytes.NewReader([]byte("{}")), objectstore.PutOptions{MIMEType: "application/json", Size: 2}); err != nil {
		t.Fatalf("seed blob for share %s: %v", link.ID, err)
	}
}

// countExpireAudit counts aura.share_audit "expire" rows for shareID — the per-link audit
// assertion TestShareExpirySweep needs.
func countExpireAudit(t *testing.T, deps shareTestDeps, shareID string) int {
	t.Helper()
	pool := deps.svc.store.pool
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM aura.share_audit WHERE share_link_id = $1 AND action = 'expire'`, shareID,
	).Scan(&n); err != nil {
		t.Fatalf("count expire audit for %s: %v", shareID, err)
	}
	return n
}

// TestShareExpirySweep seeds two due (already-expired, still-live) public links and one
// not-due public link, runs ExpireDue, and asserts: only the due links are stamped and have
// their blobs reclaimed; the not-due link is untouched (blob intact, still resolves); and an
// "expire" audit row lands for each expired link, none for the untouched one.
func TestShareExpirySweep(t *testing.T) {
	pool := migratedPool(t)
	deps := newShareTestService(t, pool, true)
	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)
	ctx := context.Background()

	mint := func(expiresAt *time.Time) (Link, string) {
		plaintext, hash, err := Mint()
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		link := newLink(owner, conv, string(TierPublic), expiresAt)
		if err := deps.svc.store.Insert(ctx, link, hash[:]); err != nil {
			t.Fatalf("insert: %v", err)
		}
		seedBlobUnderSnapshot(t, deps, link)
		return link, plaintext
	}

	due1, due1Tok := mint(pastPtr(2 * time.Hour))
	due2, due2Tok := mint(pastPtr(time.Hour))
	notDue, notDueTok := mint(futurePtr(24 * time.Hour))

	n, err := deps.svc.ExpireDue(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ExpireDue: %v", err)
	}
	if n != 2 {
		t.Fatalf("ExpireDue expired = %d, want 2", n)
	}

	for _, tc := range []struct {
		name string
		link Link
		tok  string
	}{{"due1", due1, due1Tok}, {"due2", due2, due2Tok}} {
		objs, err := deps.objects.List(ctx, objectstore.ListRequest{Bucket: deps.bucket, Prefix: objectstore.ShareKeyPrefix(tc.link.ID)})
		if err != nil {
			t.Fatalf("%s: list after sweep: %v", tc.name, err)
		}
		if len(objs) != 0 {
			t.Fatalf("%s: sweep left %d object(s) under the share prefix, want 0", tc.name, len(objs))
		}
		if _, _, err := deps.svc.ResolveByToken(ctx, tc.tok); !errors.Is(err, ErrShareNotFound) {
			t.Fatalf("%s: ResolveByToken(post-sweep) = %v, want ErrShareNotFound", tc.name, err)
		}
		if got := countExpireAudit(t, deps, tc.link.ID.String()); got != 1 {
			t.Fatalf("%s: expire audit rows = %d, want 1", tc.name, got)
		}
	}

	// The not-due link is untouched: its blob survives and it still resolves.
	objs, err := deps.objects.List(ctx, objectstore.ListRequest{Bucket: deps.bucket, Prefix: objectstore.ShareKeyPrefix(notDue.ID)})
	if err != nil {
		t.Fatalf("not-due: list after sweep: %v", err)
	}
	if len(objs) == 0 {
		t.Fatal("not-due link's blob was reclaimed by the sweep — it was not due")
	}
	if _, _, err := deps.svc.ResolveByToken(ctx, notDueTok); err != nil {
		t.Fatalf("not-due link ResolveByToken(post-sweep) = %v, want success", err)
	}
	if got := countExpireAudit(t, deps, notDue.ID.String()); got != 0 {
		t.Fatalf("not-due link expire audit rows = %d, want 0", got)
	}
}

// TestShareExpiryBlobDropFailureLeavesRowUnstamped proves the drop-before-stamp ordering
// under fault injection, not just by inspection: when dropBlobs fails, ExpireDue returns
// BEFORE calling RevokeForIdentity, so the row is still NOT revoked — a crash at exactly this
// point (this failure simulates it) leaves a still-due row DueForExpiry will pick up again on
// the next tick, never an orphaned, unreachable-forever stamped row.
func TestShareExpiryBlobDropFailureLeavesRowUnstamped(t *testing.T) {
	pool := migratedPool(t)
	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)
	ctx := context.Background()

	store := New(pool)
	audit := NewAuditWriter(pool)
	const bucket = "share-expiry-fail-test"
	fake := objectstore.NewFake()

	_, hash, err := Mint()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	link := newLink(owner, conv, string(TierPublic), pastPtr(time.Hour))
	if err := store.Insert(ctx, link, hash[:]); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := fake.Put(ctx, objectstore.ObjectRef{Bucket: bucket, Key: objectstore.ShareSnapshotKey(link.ID, link.SnapshotID)},
		bytes.NewReader([]byte("{}")), objectstore.PutOptions{MIMEType: "application/json", Size: 2}); err != nil {
		t.Fatalf("seed blob: %v", err)
	}

	injectedErr := errors.New("injected object-store failure")
	failing := &failingListStore{FakeStore: fake, failPrefix: objectstore.ShareKeyPrefix(link.ID), err: injectedErr}
	svc := NewService(store, audit, failing, bucket, nil, nil, nil, 90, true)

	n, err := svc.ExpireDue(ctx, time.Now().UTC())
	if err == nil {
		t.Fatal("want ExpireDue to surface the dropBlobs failure")
	}
	if !errors.Is(err, injectedErr) {
		t.Fatalf("ExpireDue error does not wrap the injected failure: %v", err)
	}
	if n != 0 {
		t.Fatalf("expired = %d, want 0 (the failing link must not count as expired)", n)
	}

	var revokedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT revoked_at FROM aura.shared_links WHERE id = $1`, link.ID).Scan(&revokedAt); err != nil {
		t.Fatalf("query revoked_at: %v", err)
	}
	if revokedAt != nil {
		t.Fatal("row was stamped despite the dropBlobs failure — drop-before-stamp ordering violated")
	}
}

// TestShareExpirySweepIdempotent proves re-running ExpireDue over an already-swept link is a
// no-op, not an error: the second run expires 0 with no error, and the state left by the first
// run (blob gone, one audit row) is unchanged.
func TestShareExpirySweepIdempotent(t *testing.T) {
	pool := migratedPool(t)
	deps := newShareTestService(t, pool, true)
	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)
	ctx := context.Background()

	_, hash, err := Mint()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	link := newLink(owner, conv, string(TierPublic), pastPtr(time.Hour))
	if err := deps.svc.store.Insert(ctx, link, hash[:]); err != nil {
		t.Fatalf("insert: %v", err)
	}
	seedBlobUnderSnapshot(t, deps, link)

	n1, err := deps.svc.ExpireDue(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("first ExpireDue: %v", err)
	}
	if n1 != 1 {
		t.Fatalf("first ExpireDue expired = %d, want 1", n1)
	}

	n2, err := deps.svc.ExpireDue(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("second ExpireDue: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("second ExpireDue expired = %d, want 0 (already swept)", n2)
	}
	if got := countExpireAudit(t, deps, link.ID.String()); got != 1 {
		t.Fatalf("expire audit rows after two sweeps = %d, want 1 (no duplicate audit)", got)
	}
}

// countRevokeAudit counts aura.share_audit "revoke" rows for shareID.
func countRevokeAudit(t *testing.T, deps shareTestDeps, shareID string) int {
	t.Helper()
	pool := deps.svc.store.pool
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM aura.share_audit WHERE share_link_id = $1 AND action = 'revoke'`, shareID,
	).Scan(&n); err != nil {
		t.Fatalf("count revoke audit for %s: %v", shareID, err)
	}
	return n
}

// TestRevokeConversationShares proves the cascade.go seam target directly (complementing
// internal/runner's end-to-end TestDeleteLifecycleRevokesShares, which proves the OTHER half —
// that the runner actually drives this method before its own persistence delete): two live
// public shares on the same conversation both get revoked + blob-dropped in one call, while an
// already-revoked THIRD share on the same conversation is skipped (no duplicate revoke, no
// duplicate audit row) — the `link.RevokedAt != nil` continue branch.
func TestRevokeConversationShares(t *testing.T) {
	pool := migratedPool(t)
	deps := newShareTestService(t, pool, true)
	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)
	ctx := context.Background()

	mintLive := func() (Link, string) {
		plaintext, hash, err := Mint()
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		link := newLink(owner, conv, string(TierPublic), futurePtr(24*time.Hour))
		if err := deps.svc.store.Insert(ctx, link, hash[:]); err != nil {
			t.Fatalf("insert: %v", err)
		}
		seedBlobUnderSnapshot(t, deps, link)
		return link, plaintext
	}

	live1, live1Tok := mintLive()
	live2, live2Tok := mintLive()

	// A third share on the SAME conversation, already revoked before this call — proves the
	// skip branch (never double-revoked, never double-audited).
	alreadyRevoked, _ := mintLive()
	if err := deps.svc.Revoke(ctx, alreadyRevoked.ID.String(), owner); err != nil {
		t.Fatalf("pre-revoke third share: %v", err)
	}

	if err := deps.svc.RevokeConversationShares(ctx, conv, owner); err != nil {
		t.Fatalf("RevokeConversationShares: %v", err)
	}

	for _, tc := range []struct {
		name string
		link Link
		tok  string
	}{{"live1", live1, live1Tok}, {"live2", live2, live2Tok}} {
		objs, err := deps.objects.List(ctx, objectstore.ListRequest{Bucket: deps.bucket, Prefix: objectstore.ShareKeyPrefix(tc.link.ID)})
		if err != nil {
			t.Fatalf("%s: list after cascade: %v", tc.name, err)
		}
		if len(objs) != 0 {
			t.Fatalf("%s: cascade left %d object(s) under the share prefix, want 0", tc.name, len(objs))
		}
		if _, _, err := deps.svc.ResolveByToken(ctx, tc.tok); !errors.Is(err, ErrShareNotFound) {
			t.Fatalf("%s: ResolveByToken(post-cascade) = %v, want ErrShareNotFound", tc.name, err)
		}
		if got := countRevokeAudit(t, deps, tc.link.ID.String()); got != 1 {
			t.Fatalf("%s: revoke audit rows = %d, want 1", tc.name, got)
		}
	}

	// The already-revoked share was skipped: exactly one revoke audit row (from the pre-revoke
	// above), never two.
	if got := countRevokeAudit(t, deps, alreadyRevoked.ID.String()); got != 1 {
		t.Fatalf("already-revoked share revoke audit rows = %d, want 1 (no duplicate)", got)
	}
}

// TestRevokeConversationSharesNoShares proves an empty result is not an error: a conversation
// with zero live shares revokes zero, successfully.
func TestRevokeConversationSharesNoShares(t *testing.T) {
	pool := migratedPool(t)
	deps := newShareTestService(t, pool, true)
	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)

	if err := deps.svc.RevokeConversationShares(context.Background(), conv, owner); err != nil {
		t.Fatalf("RevokeConversationShares(no shares): %v", err)
	}
}

// failingListStore wraps a *objectstore.FakeStore and fails List for exactly one prefix — the
// fault-injection TestRevokeConversationSharesPerLinkFailureContinues needs to force a single
// link's Revoke to fail without touching any other link.
type failingListStore struct {
	*objectstore.FakeStore
	failPrefix string
	err        error
}

func (f *failingListStore) List(ctx context.Context, req objectstore.ListRequest) ([]objectstore.ObjectInfo, error) {
	if req.Prefix == f.failPrefix {
		return nil, f.err
	}
	return f.FakeStore.List(ctx, req)
}

// TestRevokeConversationSharesPerLinkFailureContinues proves a single link's Revoke failure
// does not abandon its siblings: the OTHER live link on the same conversation is still
// revoked and blob-dropped, and the failure is joined into the returned error (never silently
// swallowed) for the caller — the runner lifecycle — to WARN on.
func TestRevokeConversationSharesPerLinkFailureContinues(t *testing.T) {
	pool := migratedPool(t)
	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)
	ctx := context.Background()

	store := New(pool)
	audit := NewAuditWriter(pool)
	const bucket = "share-cascade-fail-test"
	fake := objectstore.NewFake()

	mintLive := func() Link {
		_, hash, err := Mint()
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		link := newLink(owner, conv, string(TierPublic), futurePtr(24*time.Hour))
		if err := store.Insert(ctx, link, hash[:]); err != nil {
			t.Fatalf("insert: %v", err)
		}
		if _, err := fake.Put(ctx, objectstore.ObjectRef{Bucket: bucket, Key: objectstore.ShareSnapshotKey(link.ID, link.SnapshotID)},
			bytes.NewReader([]byte("{}")), objectstore.PutOptions{MIMEType: "application/json", Size: 2}); err != nil {
			t.Fatalf("seed blob: %v", err)
		}
		return link
	}

	okLink := mintLive()
	failLink := mintLive()

	injectedErr := errors.New("injected object-store failure")
	failing := &failingListStore{FakeStore: fake, failPrefix: objectstore.ShareKeyPrefix(failLink.ID), err: injectedErr}
	svc := NewService(store, audit, failing, bucket, nil, nil, nil, 90, true)

	err := svc.RevokeConversationShares(ctx, conv, owner)
	if err == nil {
		t.Fatal("want a joined error surfacing the failing link's Revoke failure")
	}
	if !errors.Is(err, injectedErr) {
		t.Fatalf("joined error does not wrap the injected failure: %v", err)
	}

	objs, lerr := fake.List(ctx, objectstore.ListRequest{Bucket: bucket, Prefix: objectstore.ShareKeyPrefix(okLink.ID)})
	if lerr != nil {
		t.Fatalf("list ok link: %v", lerr)
	}
	if len(objs) != 0 {
		t.Fatalf("ok link still has %d object(s), want 0 (it must still be revoked despite the sibling failure)", len(objs))
	}
}

// TestPropertyExpiryMonotonicity proves the VALIDATION.md expiry-monotonicity property: for
// every t, once resolve(l,t) 404s for expiry, resolve(l,t') 404s for every t' > t — an expired
// link never resurrects. Driven over a RANGE of past expires_at offsets (rapid draws a random
// past duration per check, spanning one second to two years) rather than one fixed value, so a
// clock-skew-class regression at any magnitude is caught, not just the one offset a hand-picked
// fixture happened to use. No sweep runs anywhere in this test — the lazy resolve predicate
// alone (plan 37F-07) must close the window.
func TestPropertyExpiryMonotonicity(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)

	rapid.Check(t, func(rt *rapid.T) {
		pastSeconds := rapid.Int64Range(1, 2*365*24*3600).Draw(rt, "pastSeconds")
		expiresAt := time.Now().UTC().Add(-time.Duration(pastSeconds) * time.Second)

		_, hash, err := Mint()
		if err != nil {
			rt.Fatalf("mint: %v", err)
		}
		link := newLink(owner, conv, string(TierPublic), &expiresAt)
		if err := store.Insert(context.Background(), link, hash[:]); err != nil {
			rt.Fatalf("insert: %v", err)
		}

		if _, err := store.ResolveByToken(context.Background(), hash[:]); !errors.Is(err, ErrShareNotFound) {
			rt.Fatalf("ResolveByToken(expired %s ago) = %v, want ErrShareNotFound", time.Duration(pastSeconds)*time.Second, err)
		}
	})
}
