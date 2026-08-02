//go:build db_integration

// Integration tests for internal/share.Service against a real Postgres-backed
// aura.shared_links/aura.conversations (migrations 0040 + 0005) and objectstore.NewFake() —
// no Garage, no additional build tag beyond the one on this file's own first line
// (VALIDATION.md: 37F has zero container/daemon-gated code, and that is a design property to
// protect). Reuses migratedPool/envOrSkip/seedIdentity/seedConversation/newLink/futurePtr/
// pastPtr/pgErrCode from store_integration_test.go (same package, same build tag).
//
// This file carries the plan's 9 NAMED security-property tests plus the shared
// helpers/adapters/fakes every share integration test file in this package depends on.
// service_integration_edge_test.go (same package, same tag) carries the coverage-floor
// deviation tests added to clear the 85% floor — split there, not here, to stay under the
// 600-LOC file cap.
//
// Run via:
//
//	go test -tags db_integration -race -p 1 -count=1 ./internal/share/
package share

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// convReaderAdapter satisfies ConversationReader over the real *conversations.Store — the
// composition-root-shaped adapter this plan's seam is designed for (a later plan wires the
// production equivalent; this is the test-side stand-in over a live conversations.Store).
type convReaderAdapter struct {
	store *conversations.Store
}

func (a convReaderAdapter) GetForIdentity(ctx context.Context, conversationID, identityID string) (ConvMeta, error) {
	conv, err := a.store.GetForIdentity(ctx, conversationID, identityID)
	if err != nil {
		return ConvMeta{}, err
	}
	createdAt, _ := time.Parse(time.RFC3339, conv.CreatedAt)
	return ConvMeta{Title: conv.Title, Model: conv.Model, CreatedAt: createdAt}, nil
}

func (a convReaderAdapter) LoadHistory(ctx context.Context, conversationID string) ([]llm.Message, error) {
	return a.store.LoadHistory(ctx, conversationID)
}

// fakeShareArtifactLister is a test-only ArtifactLister keyed by conversation (thread) id.
// Named distinctly from bundle_test.go's fakeArtifactOpener/fakeArtifactLister-shaped doubles
// to avoid a duplicate-declaration conflict: bundle_test.go carries no build tag, so it
// compiles alongside this db_integration-tagged file whenever this tag is set.
type fakeShareArtifactLister struct {
	byThread map[string][]assets.Asset
}

func (f *fakeShareArtifactLister) ListForThread(_ context.Context, _, threadID string) ([]assets.Asset, error) {
	return f.byThread[threadID], nil
}

// fakeShareArtifactOpener is a test-only ArtifactOpener that counts every open, so tests can
// assert the resolve path (ResolveByToken/ResolveInternal) NEVER calls it — proving copy,
// never reference, at the service layer (T-37F-08 / R-11).
type fakeShareArtifactOpener struct {
	bodies map[string][]byte
	opens  int
}

func (f *fakeShareArtifactOpener) OpenForIdentity(_ context.Context, assetID, _ string) (io.ReadCloser, assets.Asset, error) {
	f.opens++
	b, ok := f.bodies[assetID]
	if !ok {
		return nil, assets.Asset{}, errors.New("fakeShareArtifactOpener: unknown asset " + assetID)
	}
	return io.NopCloser(bytes.NewReader(b)), assets.Asset{ID: assetID}, nil
}

// shareTestDeps bundles one Service instance with the fakes/helpers its tests need to seed
// artifacts and append turns directly (bypassing the service, to simulate "a turn was added
// after mint" / "an artifact exists before Create runs"). Shared by this file and
// service_integration_edge_test.go.
type shareTestDeps struct {
	svc       *Service
	convStore *conversations.Store
	artifacts *fakeShareArtifactLister
	opener    *fakeShareArtifactOpener
	objects   *objectstore.FakeStore
	bucket    string
}

func newShareTestService(t *testing.T, pool *pgxpool.Pool, publicEnabled bool) shareTestDeps {
	t.Helper()
	store := New(pool)
	audit := NewAuditWriter(pool)
	objects := objectstore.NewFake()
	const bucket = "share-test-bucket"
	convStore := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	artifacts := &fakeShareArtifactLister{byThread: map[string][]assets.Asset{}}
	opener := &fakeShareArtifactOpener{bodies: map[string][]byte{}}
	svc := NewService(store, audit, objects, bucket, convReaderAdapter{store: convStore}, artifacts, opener, 90, publicEnabled)
	return shareTestDeps{svc: svc, convStore: convStore, artifacts: artifacts, opener: opener, objects: objects, bucket: bucket}
}

// appendTurn takes the OWNER as well as the conversation because aura.conversation_turns is
// fail-closed since migration 0089: a write on a bare context does not append to the wrong
// conversation, it appends to none, and the turn simply is not there when the snapshot is
// built.
func appendTurn(t *testing.T, conv *conversations.Store, ownerID, convID, role, content string) {
	t.Helper()
	ctx := identityctx.WithIdentityID(context.Background(), ownerID)
	if err := conv.AppendTurn(ctx, conversations.AppendTurnParams{
		ConversationID: convID, Role: role, Content: content,
	}); err != nil {
		t.Fatalf("append turn: %v", err)
	}
}

// TestShareSnapshotFrozen proves the D-06 core / SC3 leak-prevention property: a turn
// appended AFTER mint never appears when the link is resolved again — the snapshot is a
// static copy taken once at Create, not a live view.
func TestShareSnapshotFrozen(t *testing.T) {
	pool := migratedPool(t)
	deps := newShareTestService(t, pool, true)
	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)
	appendTurn(t, deps.convStore, owner, conv, llm.RoleUser, "turn before mint")

	res, err := deps.svc.Create(context.Background(), CreateRequest{ConversationID: conv, OwnerIdentityID: owner, Tier: TierInternal})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	appendTurn(t, deps.convStore, owner, conv, llm.RoleUser, "turn added AFTER mint — must never leak")

	snap, _, err := deps.svc.ResolveInternal(context.Background(), res.Link.ID.String(), owner)
	if err != nil {
		t.Fatalf("resolve internal: %v", err)
	}
	if len(snap.Turns) != 1 {
		t.Fatalf("frozen snapshot has %d turns, want 1 (only the pre-mint turn)", len(snap.Turns))
	}
	for _, turn := range snap.Turns {
		if turn.Text == "turn added AFTER mint — must never leak" {
			t.Fatalf("frozen snapshot leaked a post-mint turn: %+v", snap.Turns)
		}
	}
}

// TestShareUpdateResnapshot proves Update mints a NEW snapshot_id, keeps the SAME token
// (resolve with the original plaintext still succeeds), and the new turn now appears.
func TestShareUpdateResnapshot(t *testing.T) {
	pool := migratedPool(t)
	deps := newShareTestService(t, pool, true)
	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)
	appendTurn(t, deps.convStore, owner, conv, llm.RoleUser, "turn one")

	res, err := deps.svc.Create(context.Background(), CreateRequest{
		ConversationID: conv, OwnerIdentityID: owner, Tier: TierPublic, ExpiryOption: ExpiryDefault,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if res.Token == "" {
		t.Fatal("create(public): empty plaintext token")
	}

	appendTurn(t, deps.convStore, owner, conv, llm.RoleUser, "turn two, added before update")

	updated, err := deps.svc.Update(context.Background(), res.Link.ID.String(), owner)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.SnapshotID == res.Link.SnapshotID {
		t.Fatal("update did not mint a new snapshot_id")
	}

	snap, _, err := deps.svc.ResolveByToken(context.Background(), res.Token)
	if err != nil {
		t.Fatalf("resolve by the ORIGINAL token after update: %v (token must be unchanged)", err)
	}
	if len(snap.Turns) != 2 {
		t.Fatalf("post-update snapshot has %d turns, want 2", len(snap.Turns))
	}
}

// TestShareRevokeDropsBlobs proves Revoke drops every byte under the share's prefix AND
// closes the resolve path in the same call.
func TestShareRevokeDropsBlobs(t *testing.T) {
	pool := migratedPool(t)
	deps := newShareTestService(t, pool, true)
	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)
	appendTurn(t, deps.convStore, owner, conv, llm.RoleUser, "hello")

	res, err := deps.svc.Create(context.Background(), CreateRequest{
		ConversationID: conv, OwnerIdentityID: owner, Tier: TierPublic, ExpiryOption: ExpiryDefault,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := deps.svc.Revoke(context.Background(), res.Link.ID.String(), owner); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	objs, err := deps.objects.List(context.Background(), objectstore.ListRequest{
		Bucket: deps.bucket, Prefix: objectstore.ShareKeyPrefix(res.Link.ID),
	})
	if err != nil {
		t.Fatalf("list after revoke: %v", err)
	}
	if len(objs) != 0 {
		t.Fatalf("revoke left %d object(s) under the share prefix, want 0", len(objs))
	}

	if _, _, err := deps.svc.ResolveByToken(context.Background(), res.Token); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("ResolveByToken(revoked) = %v, want ErrShareNotFound", err)
	}
}

// TestShareBundledArtifactTokenScoped proves an artifact is copied to
// ShareArtifactKey(shareID, snapshotID, assetID) at Create time, AND that the resolve path
// (ResolveInternal) never calls the identity-scoped ArtifactOpener again — copy, never
// reference (D-09/R-11/T-37F-08).
func TestShareBundledArtifactTokenScoped(t *testing.T) {
	pool := migratedPool(t)
	deps := newShareTestService(t, pool, true)
	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)
	appendTurn(t, deps.convStore, owner, conv, llm.RoleUser, "please attach the report")

	assetID := uuid.Must(uuid.NewV7())
	body := []byte("pdf bytes")
	deps.artifacts.byThread[conv] = []assets.Asset{
		{
			ID: assetID.String(), FileName: "report.pdf", MIMEType: "application/pdf",
			SizeBytes: int64(len(body)), SourceKind: assets.SourceAgent, Status: assets.StatusComplete,
		},
	}
	deps.opener.bodies[assetID.String()] = body

	res, err := deps.svc.Create(context.Background(), CreateRequest{ConversationID: conv, OwnerIdentityID: owner, Tier: TierInternal})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	rc, _, err := deps.objects.Get(context.Background(), objectstore.ObjectRef{
		Bucket: deps.bucket, Key: objectstore.ShareArtifactKey(res.Link.ID, res.Link.SnapshotID, assetID),
	})
	if err != nil {
		t.Fatalf("get bundled artifact: %v", err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("read bundled artifact: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("bundled artifact bytes = %q, want %q", got, body)
	}

	opensAfterCreate := deps.opener.opens
	snap, _, err := deps.svc.ResolveInternal(context.Background(), res.Link.ID.String(), owner)
	if err != nil {
		t.Fatalf("resolve internal: %v", err)
	}
	if deps.opener.opens != opensAfterCreate {
		t.Fatalf("ResolveInternal called the identity-scoped opener %d more time(s) — resolve must copy, never reference",
			deps.opener.opens-opensAfterCreate)
	}
	if len(snap.Artifacts) != 1 || snap.Artifacts[0].AssetID != assetID.String() {
		t.Fatalf("snapshot artifacts = %+v, want one descriptor for asset %s", snap.Artifacts, assetID)
	}
}

// TestShareResolveInternalBearer proves the D-10 core property at the service layer: B, a
// DIFFERENT provisioned identity who is NOT the owner, successfully resolves A's internal
// link. Resolving as A would pass vacuously — this test MUST resolve as B (SC4 row 3).
func TestShareResolveInternalBearer(t *testing.T) {
	pool := migratedPool(t)
	deps := newShareTestService(t, pool, true)
	ownerA := seedIdentity(t, pool)
	bearerB := seedIdentity(t, pool)
	conv := seedConversation(t, pool, ownerA)
	appendTurn(t, deps.convStore, ownerA, conv, llm.RoleUser, "hi")

	res, err := deps.svc.Create(context.Background(), CreateRequest{ConversationID: conv, OwnerIdentityID: ownerA, Tier: TierInternal})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	link, err := deps.svc.store.ResolveLiveByID(context.Background(), res.Link.ID)
	if err != nil {
		t.Fatalf("sanity ResolveLiveByID before bearer resolve: %v", err)
	}
	if link.OwnerIdentityID.String() != ownerA {
		t.Fatalf("sanity: link owner = %s, want %s", link.OwnerIdentityID, ownerA)
	}

	if _, _, err := deps.svc.ResolveInternal(context.Background(), res.Link.ID.String(), bearerB); err != nil {
		t.Fatalf("ResolveInternal(bearer B, NOT the owner) = %v, want success (D-10)", err)
	}
}

// TestShareResolveInternalRejectsPublicTier proves a public share's id passed to
// ResolveInternal returns ErrShareNotFound, never the snapshot: the public tier is
// token-addressed only.
func TestShareResolveInternalRejectsPublicTier(t *testing.T) {
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

	if _, _, err := deps.svc.ResolveInternal(context.Background(), res.Link.ID.String(), owner); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("ResolveInternal(public tier id) = %v, want ErrShareNotFound", err)
	}
}

// TestShareResolveInternalLazyLiveness proves a revoked internal link and an expired
// internal link both 404 from ResolveInternal with the expiry sweep never run —
// indistinguishable from the unknown-id case.
func TestShareResolveInternalLazyLiveness(t *testing.T) {
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
		t.Fatalf("revoke: %v", err)
	}
	if _, _, err := deps.svc.ResolveInternal(context.Background(), res.Link.ID.String(), owner); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("ResolveInternal(revoked) = %v, want ErrShareNotFound", err)
	}

	// A separately-seeded, already-expired internal link (Service.Create has no path to set
	// an internal-tier expiry — D-10 leaves it optional and unused by this plan — so this row
	// is inserted directly via the store, mirroring TestShareResolveLiveByIDLazyLiveness's own
	// store-level fixture in store_integration_test.go).
	expired := newLink(owner, conv, "internal", pastPtr(time.Hour))
	if err := deps.svc.store.Insert(context.Background(), expired, nil); err != nil {
		t.Fatalf("insert expired internal link: %v", err)
	}
	if _, _, err := deps.svc.ResolveInternal(context.Background(), expired.ID.String(), owner); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("ResolveInternal(expired, no sweep) = %v, want ErrShareNotFound", err)
	}
}

// TestSharePublicRequiresExpiryService proves the SERVICE (not just the DB CHECK) refuses a
// public mint whose expiry resolves to a rejection (a non-positive custom day count) BEFORE
// any row is written.
func TestSharePublicRequiresExpiryService(t *testing.T) {
	pool := migratedPool(t)
	deps := newShareTestService(t, pool, true)
	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)
	appendTurn(t, deps.convStore, owner, conv, llm.RoleUser, "hi")

	_, err := deps.svc.Create(context.Background(), CreateRequest{
		ConversationID: conv, OwnerIdentityID: owner, Tier: TierPublic,
		ExpiryOption: ExpiryCustom, CustomExpiryDays: 0,
	})
	if !errors.Is(err, ErrNonPositiveCustomExpiry) {
		t.Fatalf("Create(public, non-positive custom expiry) = %v, want ErrNonPositiveCustomExpiry", err)
	}

	links, err := deps.svc.store.ListForConversation(context.Background(), conv, owner)
	if err != nil {
		t.Fatalf("list shares for conversation: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("Create(rejected expiry) still wrote %d shared_links row(s), want 0", len(links))
	}
}

// TestSharePublicDeniedWhenKillSwitchOff proves publicEnabled=false denies a public Create
// even when every other input (capability, expiry) would otherwise be valid.
func TestSharePublicDeniedWhenKillSwitchOff(t *testing.T) {
	pool := migratedPool(t)
	deps := newShareTestService(t, pool, false)
	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)
	appendTurn(t, deps.convStore, owner, conv, llm.RoleUser, "hi")

	_, err := deps.svc.Create(context.Background(), CreateRequest{
		ConversationID: conv, OwnerIdentityID: owner, Tier: TierPublic, ExpiryOption: ExpiryDefault,
	})
	if !errors.Is(err, ErrSharePublicDisabled) {
		t.Fatalf("Create(public, kill-switch off) = %v, want ErrSharePublicDisabled", err)
	}
}
