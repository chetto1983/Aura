//go:build db_integration

// service_integration_edge_test.go carries the coverage-floor deviation tests added on top of
// the plan's 9 named tests in service_integration_test.go (same package, same tag, same
// helpers/adapters/fakes — split into its own file purely to stay under the 600-LOC file-size
// cap). Every test here targets a Service branch the named tests do not otherwise reach:
// owner-gate misses on Update/Revoke (Create's own owner-gate miss too), a malformed
// ResolveInternal id, an unknown ResolveByToken token, a missing/corrupt snapshot blob (both
// via the shared getSnapshot helper directly and via each public resolver), an artifact-bundle
// failure inside Create AND Update, and a second Revoke on an already-revoked link.
package share

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/google/uuid"
)

// TestShareCreateOwnerGate proves Create's owner gate runs first: a caller who does not own
// (or whose id is absent from) the target conversation gets ErrShareNotFound before anything
// is minted, built, or written.
func TestShareCreateOwnerGate(t *testing.T) {
	pool := migratedPool(t)
	deps := newShareTestService(t, pool, true)
	ownerA := seedIdentity(t, pool)
	strangerB := seedIdentity(t, pool)
	conv := seedConversation(t, pool, ownerA)

	if _, err := deps.svc.Create(context.Background(), CreateRequest{
		ConversationID: conv, OwnerIdentityID: strangerB, Tier: TierInternal,
	}); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("Create(foreign conversation) = %v, want ErrShareNotFound", err)
	}
}

// TestShareUpdateOwnerGate proves Update's owner gate runs first: a non-owner caller gets
// ErrShareNotFound before any re-snapshot work happens.
func TestShareUpdateOwnerGate(t *testing.T) {
	pool := migratedPool(t)
	deps := newShareTestService(t, pool, true)
	owner := seedIdentity(t, pool)
	stranger := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)
	appendTurn(t, deps.convStore, owner, conv, llm.RoleUser, "hi")

	res, err := deps.svc.Create(context.Background(), CreateRequest{ConversationID: conv, OwnerIdentityID: owner, Tier: TierInternal})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := deps.svc.Update(context.Background(), res.Link.ID.String(), stranger); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("Update(foreign identity) = %v, want ErrShareNotFound", err)
	}
}

// TestShareRevokeOwnerGate proves Revoke's owner gate runs first: a non-owner caller gets
// ErrShareNotFound and no blobs/row are touched.
func TestShareRevokeOwnerGate(t *testing.T) {
	pool := migratedPool(t)
	deps := newShareTestService(t, pool, true)
	owner := seedIdentity(t, pool)
	stranger := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)
	appendTurn(t, deps.convStore, owner, conv, llm.RoleUser, "hi")

	res, err := deps.svc.Create(context.Background(), CreateRequest{ConversationID: conv, OwnerIdentityID: owner, Tier: TierInternal})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := deps.svc.Revoke(context.Background(), res.Link.ID.String(), stranger); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("Revoke(foreign identity) = %v, want ErrShareNotFound", err)
	}
}

// TestShareResolveInternalInvalidID proves a malformed (non-UUID) share id 404s through the
// same sentinel as every other miss — no distinguishable "bad id" error.
func TestShareResolveInternalInvalidID(t *testing.T) {
	pool := migratedPool(t)
	deps := newShareTestService(t, pool, true)
	if _, _, err := deps.svc.ResolveInternal(context.Background(), "not-a-uuid", "whoever"); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("ResolveInternal(malformed id) = %v, want ErrShareNotFound", err)
	}
}

// TestShareResolveByTokenUnknown proves a token that was never minted 404s through
// ErrShareNotFound at the service layer (distinct from the revoked-token case already
// covered by TestShareRevokeDropsBlobs).
func TestShareResolveByTokenUnknown(t *testing.T) {
	pool := migratedPool(t)
	deps := newShareTestService(t, pool, true)
	if _, _, err := deps.svc.ResolveByToken(context.Background(), "never-minted-token"); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("ResolveByToken(unknown) = %v, want ErrShareNotFound", err)
	}
}

// TestServiceGetSnapshotMissingBlob and TestServiceGetSnapshotCorruptJSON prove the shared
// resolve-time helper surfaces both a missing blob and a corrupted one, rather than a panic
// or a silently-empty Snapshot.
func TestServiceGetSnapshotMissingBlob(t *testing.T) {
	pool := migratedPool(t)
	deps := newShareTestService(t, pool, true)
	link := Link{ID: uuid.Must(uuid.NewV7()), SnapshotID: uuid.Must(uuid.NewV7())}
	if _, err := deps.svc.getSnapshot(context.Background(), link); err == nil {
		t.Fatal("getSnapshot(missing blob) succeeded, want an error")
	}
}

func TestServiceGetSnapshotCorruptJSON(t *testing.T) {
	pool := migratedPool(t)
	deps := newShareTestService(t, pool, true)
	link := Link{ID: uuid.Must(uuid.NewV7()), SnapshotID: uuid.Must(uuid.NewV7())}
	if _, err := deps.objects.Put(context.Background(), objectstore.ObjectRef{
		Bucket: deps.bucket, Key: objectstore.ShareSnapshotKey(link.ID, link.SnapshotID),
	}, bytes.NewReader([]byte("not json")), objectstore.PutOptions{}); err != nil {
		t.Fatalf("seed corrupt blob: %v", err)
	}
	if _, err := deps.svc.getSnapshot(context.Background(), link); err == nil {
		t.Fatal("getSnapshot(corrupt JSON) succeeded, want an error")
	}
}

// TestShareCreateArtifactBundleFailure proves Create surfaces a bundling failure (here: the
// opener has no bytes for a listed candidate) as an error, rather than silently minting a
// share with a missing artifact.
func TestShareCreateArtifactBundleFailure(t *testing.T) {
	pool := migratedPool(t)
	deps := newShareTestService(t, pool, true)
	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)
	appendTurn(t, deps.convStore, owner, conv, llm.RoleUser, "hi")

	assetID := uuid.Must(uuid.NewV7()).String()
	deps.artifacts.byThread[conv] = []assets.Asset{
		{ID: assetID, FileName: "x", MIMEType: "text/plain", SizeBytes: 1, SourceKind: assets.SourceAgent, Status: assets.StatusComplete},
	}
	// deps.opener.bodies has no entry for assetID -> OpenForIdentity errors inside bundleArtifacts.

	if _, err := deps.svc.Create(context.Background(), CreateRequest{ConversationID: conv, OwnerIdentityID: owner, Tier: TierInternal}); err == nil {
		t.Fatal("Create with a failing artifact bundle succeeded, want an error")
	}
}

// TestShareUpdateArtifactBundleFailure proves Update surfaces a bundling failure the same
// way Create does (TestShareCreateArtifactBundleFailure) — a candidate the opener has no
// bytes for fails the whole Update, never silently re-snapshotting without it.
func TestShareUpdateArtifactBundleFailure(t *testing.T) {
	pool := migratedPool(t)
	deps := newShareTestService(t, pool, true)
	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)
	appendTurn(t, deps.convStore, owner, conv, llm.RoleUser, "hi")

	res, err := deps.svc.Create(context.Background(), CreateRequest{ConversationID: conv, OwnerIdentityID: owner, Tier: TierInternal})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	assetID := uuid.Must(uuid.NewV7()).String()
	deps.artifacts.byThread[conv] = []assets.Asset{
		{ID: assetID, FileName: "x", MIMEType: "text/plain", SizeBytes: 1, SourceKind: assets.SourceAgent, Status: assets.StatusComplete},
	}
	// deps.opener.bodies has no entry for assetID -> OpenForIdentity errors inside bundleArtifacts.

	if _, err := deps.svc.Update(context.Background(), res.Link.ID.String(), owner); err == nil {
		t.Fatal("Update with a failing artifact bundle succeeded, want an error")
	}
}

// TestShareRevokeAlreadyRevoked proves a SECOND Revoke on an already-revoked link surfaces
// the store's ErrShareNotFound (rows-affected 0) rather than succeeding silently — exercising
// Revoke's own store-failure wrap branch (distinct from the owner-gate miss).
func TestShareRevokeAlreadyRevoked(t *testing.T) {
	pool := migratedPool(t)
	deps := newShareTestService(t, pool, true)
	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)
	appendTurn(t, deps.convStore, owner, conv, llm.RoleUser, "hi")

	res, err := deps.svc.Create(context.Background(), CreateRequest{ConversationID: conv, OwnerIdentityID: owner, Tier: TierInternal})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := deps.svc.Revoke(context.Background(), res.Link.ID.String(), owner); err != nil {
		t.Fatalf("first revoke: %v", err)
	}
	if err := deps.svc.Revoke(context.Background(), res.Link.ID.String(), owner); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("second revoke (already revoked) = %v, want ErrShareNotFound", err)
	}
}

// TestShareResolveByTokenBlobMissing and TestShareResolveInternalBlobMissing prove BOTH
// resolvers collapse a missing-blob failure (a live row whose snapshot bytes are gone —
// e.g. a partial delete) to the SAME ErrShareNotFound sentinel, exercising the
// getSnapshot-failure branch INSIDE each resolver (as opposed to TestServiceGetSnapshot*
// above, which call getSnapshot directly).
func TestShareResolveByTokenBlobMissing(t *testing.T) {
	pool := migratedPool(t)
	deps := newShareTestService(t, pool, true)
	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)
	appendTurn(t, deps.convStore, owner, conv, llm.RoleUser, "hi")

	res, err := deps.svc.Create(context.Background(), CreateRequest{
		ConversationID: conv, OwnerIdentityID: owner, Tier: TierPublic, ExpiryOption: ExpiryDefault,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := deps.objects.Delete(context.Background(), objectstore.ObjectRef{
		Bucket: deps.bucket, Key: objectstore.ShareSnapshotKey(res.Link.ID, res.Link.SnapshotID),
	}); err != nil {
		t.Fatalf("delete snapshot blob out from under the live row: %v", err)
	}

	if _, _, err := deps.svc.ResolveByToken(context.Background(), res.Token); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("ResolveByToken(missing blob) = %v, want ErrShareNotFound", err)
	}
}

func TestShareResolveInternalBlobMissing(t *testing.T) {
	pool := migratedPool(t)
	deps := newShareTestService(t, pool, true)
	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)
	appendTurn(t, deps.convStore, owner, conv, llm.RoleUser, "hi")

	res, err := deps.svc.Create(context.Background(), CreateRequest{ConversationID: conv, OwnerIdentityID: owner, Tier: TierInternal})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := deps.objects.Delete(context.Background(), objectstore.ObjectRef{
		Bucket: deps.bucket, Key: objectstore.ShareSnapshotKey(res.Link.ID, res.Link.SnapshotID),
	}); err != nil {
		t.Fatalf("delete snapshot blob out from under the live row: %v", err)
	}

	if _, _, err := deps.svc.ResolveInternal(context.Background(), res.Link.ID.String(), owner); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("ResolveInternal(missing blob) = %v, want ErrShareNotFound", err)
	}
}
