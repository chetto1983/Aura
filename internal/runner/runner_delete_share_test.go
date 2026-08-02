//go:build db_integration

// Integration tests for the D-15 conversation-delete cascade (runner_delete.go step 4.5)
// against a real Postgres-backed aura.shared_links (migration 0040) and objectstore.NewFake()
// — no Garage. Reuses migratedRunnerPool/envOrSkip/localIdentityID/newIntegrationConversation
// from integration_helpers_test.go and newTestRunner/newConvID from runner_test.go, and
// stepLog/recordingConvStore/seedOwnedConversation from conversation_delete_lifecycle_test.go
// (all same package, same build tag).
//
// internal/runner never imports internal/share in its PRODUCTION code (runner_delete.go
// declares the local ShareRevoker seam instead) — that import boundary is asserted by
// `go list -deps ./internal/runner/` (which excludes _test.go dependencies by design), so this
// file constructing a real *share.Service to prove the seam's live-wiring shape does not
// violate it.
//
// Run via:
//
//	go test -tags db_integration -race ./internal/runner/ -count=1
package runner

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/chetto1983/aura/internal/share"
)

// shareConvReaderAdapter satisfies share.ConversationReader over a real *conversations.Store —
// the same test-side stand-in internal/share/service_integration_test.go uses (a later plan
// wires the production equivalent). Go test helpers do not cross package boundaries, so this
// file carries its own copy rather than importing internal/share's unexported test glue.
type shareConvReaderAdapter struct {
	store *conversations.Store
}

func (a shareConvReaderAdapter) GetForIdentity(ctx context.Context, conversationID, identityID string) (share.ConvMeta, error) {
	conv, err := a.store.GetForIdentity(ctx, conversationID, identityID)
	if err != nil {
		return share.ConvMeta{}, err
	}
	createdAt, _ := time.Parse(time.RFC3339, conv.CreatedAt)
	return share.ConvMeta{Title: conv.Title, Model: conv.Model, CreatedAt: createdAt}, nil
}

func (a shareConvReaderAdapter) LoadHistory(ctx context.Context, conversationID string) ([]llm.Message, error) {
	return a.store.LoadHistory(ctx, conversationID)
}

// emptyArtifactLister/emptyArtifactOpener satisfy share.ArtifactLister/share.ArtifactOpener
// with no candidate artifacts — this test's share carries none, so OpenForIdentity is never
// actually called.
type emptyArtifactLister struct{}

func (emptyArtifactLister) ListForThread(context.Context, string, string) ([]assets.Asset, error) {
	return nil, nil
}

type emptyArtifactOpener struct{}

func (emptyArtifactOpener) OpenForIdentity(context.Context, string, string) (io.ReadCloser, assets.Asset, error) {
	return nil, assets.Asset{}, errors.New("emptyArtifactOpener: no artifacts in this test")
}

// recordingRealConvStore records the persistence-delete step (the LAST lifecycle step) over a
// REAL *conversations.Store — the TestDeleteLifecycleRevokesShares analog of
// conversation_delete_lifecycle_test.go's recordingConvStore (which wraps the in-memory fake).
// This test's conversation must be real: the share service reads its history over the same
// pool.
type recordingRealConvStore struct {
	*conversations.Store
	steps *stepLog
}

func (r *recordingRealConvStore) DeleteForIdentity(ctx context.Context, id, identityID string) (int64, error) {
	r.steps.add("delete")
	return r.Store.DeleteForIdentity(ctx, id, identityID)
}

// recordingShareRevoker records the share-revoke step over a real ShareRevoker (a
// *share.Service in TestDeleteLifecycleRevokesShares), so the test can assert its firing ORDER
// relative to the persistence delete — not just the end state, which a stamp-then-delete bug
// would also satisfy.
type recordingShareRevoker struct {
	inner ShareRevoker
	steps *stepLog
}

func (r *recordingShareRevoker) RevokeConversationShares(ctx context.Context, conversationID, identityID string) error {
	err := r.inner.RevokeConversationShares(ctx, conversationID, identityID)
	r.steps.add("share-revoke")
	return err
}

// erroringShareRevoker always fails — TestDeleteLifecycleShareRevokeFailureDoesNotBlock's fault
// injection for the best-effort WARN-and-continue path.
type erroringShareRevoker struct{ err error }

func (e *erroringShareRevoker) RevokeConversationShares(context.Context, string, string) error {
	return e.err
}

// TestDeleteLifecycleRevokesShares is the D-15 core: a conversation with an active public
// share; DeleteConversationLifecycle is called; and (a) the share's blobs are gone from the
// fake object store, (b) the token no longer resolves, and (c) — the ordering claim — the blob
// drop happened BEFORE the persistence delete. (c) is proved by recording firing order, not by
// checking only the end state: a stamp-then-delete implementation that orphans bytes would
// still pass (a) and (b) here.
func TestDeleteLifecycleRevokesShares(t *testing.T) {
	pool := migratedRunnerPool(t)
	convStore := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	convID := newIntegrationConversation(t, pool, convStore)
	ctx := ownerCtx()

	if err := convStore.AppendTurn(ctx, conversations.AppendTurnParams{
		ConversationID: convID, Role: llm.RoleUser, Content: "hello",
	}); err != nil {
		t.Fatalf("seed turn: %v", err)
	}

	shareStore := share.New(pool)
	audit := share.NewAuditWriter(pool)
	objects := objectstore.NewFake()
	const bucket = "runner-share-cascade-test"
	svc := share.NewService(shareStore, audit, objects, bucket,
		shareConvReaderAdapter{store: convStore}, emptyArtifactLister{}, emptyArtifactOpener{}, 90, true)

	res, err := svc.Create(ctx, share.CreateRequest{
		ConversationID: convID, OwnerIdentityID: localIdentityID,
		Tier: share.TierPublic, ExpiryOption: share.ExpiryDefault,
	})
	if err != nil {
		t.Fatalf("seed share: %v", err)
	}

	objsBefore, err := objects.List(ctx, objectstore.ListRequest{Bucket: bucket, Prefix: objectstore.ShareKeyPrefix(res.Link.ID)})
	if err != nil {
		t.Fatalf("sanity list before delete: %v", err)
	}
	if len(objsBefore) == 0 {
		t.Fatal("sanity: seeded share has no blob under its prefix before delete")
	}

	steps := &stepLog{}
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	r.Conv = &recordingRealConvStore{Store: convStore, steps: steps}
	r.shareRevoker = &recordingShareRevoker{inner: svc, steps: steps}

	affected, err := r.DeleteConversationLifecycle(ctx, localIdentityID, convID)
	if err != nil {
		t.Fatalf("lifecycle: %v", err)
	}
	if affected != 1 {
		t.Fatalf("rows-affected = %d, want 1", affected)
	}

	// (a) blobs gone.
	objsAfter, err := objects.List(ctx, objectstore.ListRequest{Bucket: bucket, Prefix: objectstore.ShareKeyPrefix(res.Link.ID)})
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(objsAfter) != 0 {
		t.Fatalf("delete lifecycle left %d object(s) under the share prefix, want 0", len(objsAfter))
	}

	// (b) the token no longer resolves.
	if _, _, err := svc.ResolveByToken(ctx, res.Token); !errors.Is(err, share.ErrShareNotFound) {
		t.Fatalf("ResolveByToken(post-delete) = %v, want ErrShareNotFound", err)
	}

	// (c) the blob drop fired BEFORE the persistence delete.
	got := steps.snapshot()
	if len(got) != 2 || got[0] != "share-revoke" || got[1] != "delete" {
		t.Fatalf("lifecycle order = %v, want [share-revoke delete]", got)
	}
}

// TestDeleteLifecycleShareRevokerNil proves a nil revoker (share never mounted in this
// deployment) does not block or panic the delete — step 4.5 is a silent skip.
func TestDeleteLifecycleShareRevokerNil(t *testing.T) {
	r, _, steps := wireLifecycleRecorder(t)
	if r.shareRevoker != nil {
		t.Fatal("sanity: shareRevoker must be nil by default (Deps never set it)")
	}
	owner := resolveOwnerIdentity("")
	convID := newConvID(t)
	seedOwnedConversation(t, r, owner, convID)

	affected, err := r.DeleteConversationLifecycle(ownerCtx(), "", convID)
	if err != nil {
		t.Fatalf("lifecycle with nil share revoker: %v", err)
	}
	if affected != 1 {
		t.Fatalf("rows-affected = %d, want 1", affected)
	}
	// The rest of the ordered teardown still ran (nil revoker only skips step 4.5).
	if got := steps.snapshot(); len(got) == 0 {
		t.Fatal("nil share revoker must not skip the rest of the lifecycle")
	}
}

// TestDeleteLifecycleShareRevokeFailureDoesNotBlock proves a revoker error is best-effort
// (WARN and continue, mirroring step 2): the conversation IS still deleted despite the
// failure, because the FK backstops the row even when this step could not reach the bytes.
func TestDeleteLifecycleShareRevokeFailureDoesNotBlock(t *testing.T) {
	r, _, _ := wireLifecycleRecorder(t)
	r.shareRevoker = &erroringShareRevoker{err: errors.New("garage unreachable")}
	owner := resolveOwnerIdentity("")
	convID := newConvID(t)
	seedOwnedConversation(t, r, owner, convID)

	affected, err := r.DeleteConversationLifecycle(ownerCtx(), "", convID)
	if err != nil {
		t.Fatalf("lifecycle: %v", err)
	}
	if affected != 1 {
		t.Fatalf("rows-affected = %d, want 1 (delete must proceed despite the revoke failure)", affected)
	}
}
