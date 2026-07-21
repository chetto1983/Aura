package main

import (
	"context"
	"io"
	"time"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/chetto1983/aura/internal/share"
	"github.com/google/uuid"
)

// share_service_wiring.go is the WEBSHARE-02/03 composition root (plans 37F-08/10/11/12
// declared the seams; nothing ever called them here — a gap deadcode surfaced at 37F-18).
// Without buildShareService's two call sites (bootServe wiring chat.run.SetShareRevoker +
// aguiServer.SetShareService, serve.go) the entire share feature was permanently dead in the
// deployed binary: every /api/shares* route answered 503 (s.share == nil guards in
// share_api*.go), D-15's revoke-on-delete cascade silently no-opped (r.shareRevoker == nil),
// and the share_expiry_sweep cron kind was never dispatched — exactly the SetApprovalStore /
// SetAssetService precedent documented in serve.go (SetApprovalStore's own comment: "the live
// daemon shipped with s.approvals == nil"). buildShareService fixes all three at once by
// returning ONE *share.Service every seam structurally satisfies directly (runner.ShareRevoker,
// handlers.ShareExpirer) plus the one adapter agui.ShareService needs (Store reads it does not
// otherwise expose).

// buildShareService constructs the live WEBSHARE-02/03 share lifecycle over chat's already-
// composed pool/conversations/assets — call ONLY after chat.assets is built (buildAssetService,
// serve.go), since artifacts/opener below are chat.assets itself: agui.AssetService's
// ListForThread/OpenForIdentity structurally satisfy share.ArtifactLister/ArtifactOpener with no
// adapter (the same structural fit SetAssetService(chat.assets) already relies on). The returned
// *share.Service is handed to THREE composition-root call sites: aguiServer.SetShareService
// (via the adapter below), chat.run.SetShareRevoker (runner_delete.go step 4.5, D-15), and
// buildDispatch's share_expiry_sweep registration (serve_dispatch.go, OQ3) — one instance, three
// seams, mirroring how chat.assets itself backs several independent wirings.
func buildShareService(chat *chatEnv, objects objectstore.Store) (*share.Service, agui.ShareService) {
	store := share.New(chat.pool)
	svc := share.NewService(
		store,
		share.NewAuditWriter(chat.pool),
		objects,
		chat.cfg.ObjectStoreBucket,
		shareConvAdapter{chat.conv},
		chat.assets,
		chat.assets,
		chat.cfg.Share.MaxExpiryDays,
		chat.cfg.Share.PublicEnabled,
	)
	adapter := &shareServiceAdapter{svc: svc, store: store, objects: objects, bucket: chat.cfg.ObjectStoreBucket}
	return svc, adapter
}

// shareConvAdapter satisfies share.ConversationReader over the live *conversations.Store —
// internal/share imports neither internal/agui nor internal/conversations (the IdentityPurger
// pattern), so this adapter is built here at the composition root instead. Mirrors the
// db_integration-only test double of the identical name in internal/agui/share_api_test.go
// (that copy cannot be reused from production code — it lives behind a test build tag).
type shareConvAdapter struct{ store *conversations.Store }

func (a shareConvAdapter) GetForIdentity(ctx context.Context, convID, identityID string) (share.ConvMeta, error) {
	conv, err := a.store.GetForIdentity(ctx, convID, identityID)
	if err != nil {
		return share.ConvMeta{}, err
	}
	createdAt, _ := time.Parse(time.RFC3339, conv.CreatedAt)
	return share.ConvMeta{Title: conv.Title, Model: conv.Model, CreatedAt: createdAt}, nil
}

func (a shareConvAdapter) LoadHistory(ctx context.Context, convID string) ([]llm.Message, error) {
	return a.store.LoadHistory(ctx, convID)
}

// shareServiceAdapter satisfies agui.ShareService over the real *share.Service + *share.Store +
// objectstore.Store (the composition root's thin adapter share_service.go's own doc comment
// names in advance). ListForIdentity/ListForConversation read the Store directly — Service does
// not expose them — and OpenArtifact opens the token-scoped share/ key directly (copy-never-
// reference, D-09), never through the identity-scoped assets.Service.
type shareServiceAdapter struct {
	svc     *share.Service
	store   *share.Store
	objects objectstore.Store
	bucket  string
}

func (a *shareServiceAdapter) Create(ctx context.Context, req share.CreateRequest) (share.CreateResult, error) {
	return a.svc.Create(ctx, req)
}

func (a *shareServiceAdapter) ListForIdentity(ctx context.Context, id string, limit, offset int) ([]share.Link, error) {
	return a.store.ListForIdentity(ctx, id, limit, offset)
}

func (a *shareServiceAdapter) ListForConversation(ctx context.Context, convID, id string) ([]share.Link, error) {
	return a.store.ListForConversation(ctx, convID, id)
}

func (a *shareServiceAdapter) UpdateSnapshot(ctx context.Context, shareID, id string) (share.Link, error) {
	return a.svc.Update(ctx, shareID, id)
}

func (a *shareServiceAdapter) Revoke(ctx context.Context, shareID, id string) error {
	return a.svc.Revoke(ctx, shareID, id)
}

func (a *shareServiceAdapter) ResolveByToken(ctx context.Context, token string) (share.Snapshot, share.Link, error) {
	return a.svc.ResolveByToken(ctx, token)
}

func (a *shareServiceAdapter) ResolveInternal(ctx context.Context, shareID, id string) (share.Snapshot, share.Link, error) {
	return a.svc.ResolveInternal(ctx, shareID, id)
}

// OpenArtifact reads the token/snapshot-scoped object-store key directly (D-09 copy-never-
// reference): the caller (share_api_public.go, SC4 row 9) has already confirmed assetID belongs
// to the resolved snapshot before calling this.
func (a *shareServiceAdapter) OpenArtifact(ctx context.Context, shareID, snapshotID uuid.UUID, assetID string) (io.ReadCloser, error) {
	aid, err := uuid.Parse(assetID)
	if err != nil {
		return nil, err
	}
	rc, _, err := a.objects.Get(ctx, objectstore.ObjectRef{
		Bucket: a.bucket,
		Key:    objectstore.ShareArtifactKey(shareID, snapshotID, aid),
	})
	return rc, err
}
