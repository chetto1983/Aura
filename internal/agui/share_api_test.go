//go:build db_integration

// share_api_test.go — shared test infrastructure (fakes/adapters/env builder/helpers) for the
// WEBSHARE-02/03 HTTP surface across all three trust boundaries (plan 37F-10), plus the
// owner-scoped CRUD tests that exercise share_api.go's own routes directly (list/audit-ledger/
// body-cap/foreign-owner). D-10 bearer-within-auth internal-route tests live in
// share_api_internal_test.go; unauthenticated public-token-route tests live in
// share_api_public_test.go — this three-file split mirrors the production handler split
// (share_api.go/share_api_internal.go/share_api_public.go) and keeps each file under the
// CLAUDE.md 600-LOC cap (37F-13 refactor-on-touch: this file grew past 600 lines when the
// share.public capability check landed, so it was split, not just trimmed).
//
// Every identity is fresh + non-wildcard (R-13) via seedShareExportIdentity
// (share_export_test.go, same package/tag). Run via:
// go test -tags db_integration -race -p 1 -count=1 -run 'TestShare' ./internal/agui/
package agui

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/chetto1983/aura/internal/share"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// shareAPITestBucket is the fake bucket every env/adapter/hand-built Link in this file family shares.
const shareAPITestBucket = "share-api-test-bucket"

// shareConvAdapter satisfies share.ConversationReader over a real *conversations.Store.
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

// shareTestAdapter satisfies agui.ShareService over a real Service+Store+objectstore.Store.
type shareTestAdapter struct {
	svc     *share.Service
	store   *share.Store
	objects objectstore.Store
	bucket  string
}

func (a *shareTestAdapter) Create(ctx context.Context, req share.CreateRequest) (share.CreateResult, error) {
	return a.svc.Create(ctx, req)
}
func (a *shareTestAdapter) ListForIdentity(ctx context.Context, id string, limit, offset int) ([]share.Link, error) {
	return a.store.ListForIdentity(ctx, id, limit, offset)
}
func (a *shareTestAdapter) ListForConversation(ctx context.Context, convID, id string) ([]share.Link, error) {
	return a.store.ListForConversation(ctx, convID, id)
}
func (a *shareTestAdapter) UpdateSnapshot(ctx context.Context, shareID, id string) (share.Link, error) {
	return a.svc.Update(ctx, shareID, id)
}
func (a *shareTestAdapter) Revoke(ctx context.Context, shareID, id string) error {
	return a.svc.Revoke(ctx, shareID, id)
}
func (a *shareTestAdapter) ResolveByToken(ctx context.Context, token string) (share.Snapshot, share.Link, error) {
	return a.svc.ResolveByToken(ctx, token)
}
func (a *shareTestAdapter) ResolveInternal(ctx context.Context, shareID, id string) (share.Snapshot, share.Link, error) {
	return a.svc.ResolveInternal(ctx, shareID, id)
}

// OpenArtifact reads the token/snapshot-scoped object-store key directly (copy-never-reference).
func (a *shareTestAdapter) OpenArtifact(ctx context.Context, shareID, snapshotID uuid.UUID, assetID string) (io.ReadCloser, error) {
	aid, err := uuid.Parse(assetID)
	if err != nil {
		return nil, err
	}
	rc, _, err := a.objects.Get(ctx, objectstore.ObjectRef{Bucket: a.bucket, Key: objectstore.ShareArtifactKey(shareID, snapshotID, aid)})
	return rc, err
}

type shareAPIEnv struct {
	server    *Server
	adapter   *shareTestAdapter
	assetsF   *fakeAssetService
	convStore *conversations.Store
	pool      *pgxpool.Pool
}

// newShareAPIEnv wires a Server + ShareService over a real pool, a fake object store, and a
// fake artifact source (fakeAssetService — satisfies both ArtifactLister/ArtifactOpener).
func newShareAPIEnv(t *testing.T, pool *pgxpool.Pool, publicEnabled bool) *shareAPIEnv {
	t.Helper()
	store := share.New(pool)
	objects := objectstore.NewFake()
	convStore := newTestStore(t, pool)
	fakeAssets := &fakeAssetService{}
	svc := share.NewService(store, share.NewAuditWriter(pool), objects, shareAPITestBucket,
		shareConvAdapter{convStore}, fakeAssets, fakeAssets, 90, publicEnabled)
	adapter := &shareTestAdapter{svc: svc, store: store, objects: objects, bucket: shareAPITestBucket}
	s := NewServer(&scriptedRunner{}, convStore, ServerConfig{SharePublicEnabled: publicEnabled})
	s.SetShareService(adapter)
	// 37F-13/WEBSHARE-04 SC4 row 8: handleShareCreate's public-tier mint now checks
	// share.public via s.idAdmin — wire the SAME real *identity.Store the production
	// composition root uses (cmd/aura/serve.go's SetIdentityAdmin(chat.identity)) so
	// createShare's auto-grant (below) is honored and every pre-existing caller of this env
	// keeps minting successfully.
	s.SetIdentityAdmin(identity.New(pool))
	return &shareAPIEnv{server: s, adapter: adapter, assetsF: fakeAssets, convStore: convStore, pool: pool}
}

// shareReq drives one request through env.server.Mux() (mirrors exportRequest); "" = anonymous.
func shareReq(s *Server, method, path, identityID, body string) *httptest.ResponseRecorder {
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if identityID != "" {
		req = withPrincipal(req, identityID)
	}
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	return rec
}

func pastShareTime(d time.Duration) *time.Time {
	tm := time.Now().UTC().Add(-d)
	return &tm
}

func oneTurn() []conversations.AppendTurnParams {
	return []conversations.AppendTurnParams{{Seq: 1, Role: llm.RoleUser, Content: "hi"}}
}

// seedOwnerAndConversation seeds a fresh non-wildcard identity (R-13) + one owned conversation.
func seedOwnerAndConversation(t *testing.T, env *shareAPIEnv, turns []conversations.AppendTurnParams) (string, string) {
	t.Helper()
	owner := seedShareExportIdentity(t, env.pool)
	convID := seedExportConversation(t, env.convStore, owner, "Share Test", turns)
	return owner, convID
}

// createShare POSTs /api/shares as owner, asserts wantStatus, and decodes the 201 body.
// 37F-13: a public-tier mint now requires share.public (WEBSHARE-04 SC4 row 8); this helper
// auto-grants it to owner so every PRE-EXISTING caller (none of which exercise the capability
// gate itself) keeps minting successfully. TestSharePublicMintWithCapability/
// TestSharePublicOrgKillSwitch test the gate directly with their own explicit grant + wrap and
// do NOT call this helper; share_cross_identity_test.go's row 8 also bypasses this helper
// (a bare shareReq) specifically so A is never granted the capability.
func createShare(t *testing.T, env *shareAPIEnv, owner, convID, tier string, wantStatus int) shareLinkResponse {
	t.Helper()
	if tier == "public" {
		if err := identity.New(env.pool).GrantCapability(ownerCtx(), owner, "share.public"); err != nil {
			t.Fatalf("grant share.public for createShare(tier=public): %v", err)
		}
	}
	rec := shareReq(env.server, http.MethodPost, "/api/shares", owner,
		`{"conversation_id":"`+convID+`","tier":"`+tier+`"}`)
	if rec.Code != wantStatus {
		t.Fatalf("create(tier=%s) status = %d, want %d: %s", tier, rec.Code, wantStatus, rec.Body.String())
	}
	var link shareLinkResponse
	if rec.Code == http.StatusCreated {
		if err := json.Unmarshal(rec.Body.Bytes(), &link); err != nil {
			t.Fatalf("unmarshal create response: %v", err)
		}
	}
	return link
}

// seedBundledArtifact configures env.assetsF for the NEXT createShare call; returns the asset id.
func seedBundledArtifact(env *shareAPIEnv, name string, body []byte) string {
	id := uuid.Must(uuid.NewV7()).String()
	asset := assets.Asset{
		ID: id, FileName: name, MIMEType: "text/plain", SizeBytes: int64(len(body)),
		SourceKind: assets.SourceAgent, Status: assets.StatusComplete,
	}
	env.assetsF.listResp = []assets.Asset{asset}
	env.assetsF.openResp = io.NopCloser(bytes.NewReader(body))
	env.assetsF.openAsset = asset
	return id
}

// assertInertAttachment is shared by both asset-serving tiers (internal and public) — the
// inert-bytes rule is not a public-tier special case.
func assertInertAttachment(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("asset status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", ct)
	}
	if ns := rec.Header().Get("X-Content-Type-Options"); ns != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", ns)
	}
}

func assertAuditRow(t *testing.T, pool *pgxpool.Pool, shareID, action string) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ownerCtx(),
		"SELECT count(*) FROM aura.share_audit WHERE share_link_id = $1 AND action = $2",
		uuid.MustParse(shareID), action).Scan(&count); err != nil {
		t.Fatalf("count share_audit(%s): %v", action, err)
	}
	if count < 1 {
		t.Fatalf("share_audit has no %q row for share %s", action, shareID)
	}
}

// TestShareAuditLedger proves create/update/revoke each write their expected share_audit row.
func TestShareAuditLedger(t *testing.T) {
	pool := migratedPool(t)
	env := newShareAPIEnv(t, pool, true)
	owner, convID := seedOwnerAndConversation(t, env, oneTurn())
	link := createShare(t, env, owner, convID, "internal", http.StatusCreated)
	assertAuditRow(t, pool, link.ID, "create")
	if rec := shareReq(env.server, http.MethodPatch, "/api/shares/"+link.ID+"/snapshot", owner, ""); rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	assertAuditRow(t, pool, link.ID, "update")
	if rec := shareReq(env.server, http.MethodDelete, "/api/shares/"+link.ID, owner, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	assertAuditRow(t, pool, link.ID, "revoke")
}

// TestShareList proves GET /api/shares lists the owner's links, and ?conversation_id= scopes.
func TestShareList(t *testing.T) {
	pool := migratedPool(t)
	env := newShareAPIEnv(t, pool, true)
	owner, convID := seedOwnerAndConversation(t, env, oneTurn())
	createShare(t, env, owner, convID, "internal", http.StatusCreated)
	var links []shareLinkResponse
	all := shareReq(env.server, http.MethodGet, "/api/shares", owner, "")
	if err := json.Unmarshal(all.Body.Bytes(), &links); all.Code != http.StatusOK || err != nil || len(links) != 1 {
		t.Fatalf("list = %d %v (err=%v), want 200 with exactly 1 link", all.Code, links, err)
	}
	var scoped []shareLinkResponse
	rec := shareReq(env.server, http.MethodGet, "/api/shares?conversation_id="+convID, owner, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &scoped); err != nil || len(scoped) != 1 {
		t.Fatalf("scoped list = %v (err=%v), want exactly 1 link", scoped, err)
	}
}

// TestShareCreateBodyCap: an over-cap POST /api/shares body is rejected outright, never written.
func TestShareCreateBodyCap(t *testing.T) {
	pool := migratedPool(t)
	env := newShareAPIEnv(t, pool, true)
	owner, convID := seedOwnerAndConversation(t, env, oneTurn())
	oversized := `{"conversation_id":"` + convID + `","tier":"internal","padding":"` + strings.Repeat("x", 2<<20) + `"}`
	rec := shareReq(env.server, http.MethodPost, "/api/shares", owner, oversized)
	if rec.Code == http.StatusCreated {
		t.Fatalf("oversized create body was accepted (201) instead of rejected")
	}
	links, err := env.adapter.store.ListForConversation(ownerCtx(), convID, owner)
	if err != nil {
		t.Fatalf("list shares for conversation: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("oversized create body still wrote %d row(s), want 0", len(links))
	}
}

// TestShareForeignOwnerGets404: a non-owner's PATCH/DELETE on another's share is 404, never 403.
func TestShareForeignOwnerGets404(t *testing.T) {
	pool := migratedPool(t)
	env := newShareAPIEnv(t, pool, true)
	owner, convID := seedOwnerAndConversation(t, env, oneTurn())
	other := seedShareExportIdentity(t, pool)
	link := createShare(t, env, owner, convID, "internal", http.StatusCreated)
	if rec := shareReq(env.server, http.MethodPatch, "/api/shares/"+link.ID+"/snapshot", other, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign PATCH status = %d, want 404", rec.Code)
	}
	if rec := shareReq(env.server, http.MethodDelete, "/api/shares/"+link.ID, other, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign DELETE status = %d, want 404", rec.Code)
	}
}
