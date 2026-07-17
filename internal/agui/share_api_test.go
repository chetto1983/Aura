//go:build db_integration

// share_api_test.go — integration tests for the WEBSHARE-02/03 HTTP surface across all three
// trust boundaries (plan 37F-10): owner-scoped CRUD, D-10 bearer-within-auth internal routes,
// and unauthenticated public token routes. Every identity is fresh + non-wildcard (R-13) via
// seedShareExportIdentity (share_export_test.go, same package/tag). RequireAuth/RequireCapability
// are exercised for real only in TestShareInternalAnonymous401/TestSharePublicMintWithCapability/
// TestSharePublicOrgKillSwitch. Run via:
// go test -tags db_integration -race -p 1 -count=1 -run 'TestShare' ./internal/agui/
package agui

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
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

// shareAPITestBucket is the fake bucket every env/adapter/hand-built Link in this file shares.
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
func createShare(t *testing.T, env *shareAPIEnv, owner, convID, tier string, wantStatus int) shareLinkResponse {
	t.Helper()
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

// publicToken extracts the plaintext token from a ShareLink.url of the form "/s/{token}".
func publicToken(t *testing.T, url string) string {
	t.Helper()
	const prefix = "/s/"
	if !strings.HasPrefix(url, prefix) {
		t.Fatalf("public link url %q missing /s/ prefix", url)
	}
	return strings.TrimPrefix(url, prefix)
}

func assertAuditRow(t *testing.T, pool *pgxpool.Pool, shareID, action string) {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM aura.share_audit WHERE share_link_id = $1 AND action = $2",
		uuid.MustParse(shareID), action).Scan(&count); err != nil {
		t.Fatalf("count share_audit(%s): %v", action, err)
	}
	if count < 1 {
		t.Fatalf("share_audit has no %q row for share %s", action, shareID)
	}
}

// TestShareInternalBearerWithinAuth is SC4 row 3: B, a NON-owner, resolves A's internal link (D-10).
func TestShareInternalBearerWithinAuth(t *testing.T) {
	pool := migratedPool(t)
	env := newShareAPIEnv(t, pool, true)
	owner, convID := seedOwnerAndConversation(t, env, oneTurn())
	bearerB := seedShareExportIdentity(t, pool)
	link := createShare(t, env, owner, convID, "internal", http.StatusCreated)
	rec := shareReq(env.server, http.MethodGet, "/api/shares/"+link.ID+"/data", bearerB, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("bearer B resolve status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

// TestShareInternalAnonymous401 exercises the REAL RequireAuth chain (SC4 row 4): not on any public allowlist.
func TestShareInternalAnonymous401(t *testing.T) {
	pool := migratedPool(t)
	env := newShareAPIEnv(t, pool, true)
	owner, convID := seedOwnerAndConversation(t, env, oneTurn())
	link := createShare(t, env, owner, convID, "internal", http.StatusCreated)
	deps := AuthDeps{SecretConfigured: true, SigningKey: []byte("0123456789abcdef0123456789abcdef")}
	gated := RequireAuth(env.server.Mux(), deps)
	req := httptest.NewRequest(http.MethodGet, "/api/shares/"+link.ID+"/data", nil)
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusFound {
		t.Fatalf("anonymous internal resolve status = %d, want 401 or 302", rec.Code)
	}
}

// TestShareInternalRejectsPublicTierID: a public share's id on the internal route 404s byte-identically to an unknown id.
func TestShareInternalRejectsPublicTierID(t *testing.T) {
	pool := migratedPool(t)
	env := newShareAPIEnv(t, pool, true)
	owner, convID := seedOwnerAndConversation(t, env, oneTurn())
	link := createShare(t, env, owner, convID, "public", http.StatusCreated)
	got := shareReq(env.server, http.MethodGet, "/api/shares/"+link.ID+"/data", owner, "")
	unknown := shareReq(env.server, http.MethodGet, "/api/shares/"+uuid.Must(uuid.NewV7()).String()+"/data", owner, "")
	if got.Code != http.StatusNotFound || unknown.Code != http.StatusNotFound {
		t.Fatalf("public-tier id = %d, unknown id = %d, want both 404", got.Code, unknown.Code)
	}
	if got.Body.String() != unknown.Body.String() {
		t.Fatalf("public-tier id body %q != unknown id body %q (tier oracle)", got.Body.String(), unknown.Body.String())
	}
}

// TestShareInternalRevokedExpired404: revoked + expired (inserted directly) internal links both 404 like an unknown id.
func TestShareInternalRevokedExpired404(t *testing.T) {
	pool := migratedPool(t)
	env := newShareAPIEnv(t, pool, true)
	owner, convID := seedOwnerAndConversation(t, env, oneTurn())
	revokedLink := createShare(t, env, owner, convID, "internal", http.StatusCreated)
	if rec := shareReq(env.server, http.MethodDelete, "/api/shares/"+revokedLink.ID, owner, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	expired := share.Link{
		ID: uuid.Must(uuid.NewV7()), OwnerIdentityID: uuid.MustParse(owner), ConversationID: uuid.MustParse(convID),
		Tier: "internal", SnapshotID: uuid.Must(uuid.NewV7()), SnapshotBucket: shareAPITestBucket,
		ExpiresAt: pastShareTime(time.Hour),
	}
	if err := env.adapter.store.Insert(context.Background(), expired, nil); err != nil {
		t.Fatalf("insert expired link: %v", err)
	}
	unknown := shareReq(env.server, http.MethodGet, "/api/shares/"+uuid.Must(uuid.NewV7()).String()+"/data", owner, "")
	revoked := shareReq(env.server, http.MethodGet, "/api/shares/"+revokedLink.ID+"/data", owner, "")
	exp := shareReq(env.server, http.MethodGet, "/api/shares/"+expired.ID.String()+"/data", owner, "")
	for name, rec := range map[string]*httptest.ResponseRecorder{"revoked": revoked, "expired": exp} {
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", name, rec.Code)
		}
		if rec.Body.String() != unknown.Body.String() {
			t.Fatalf("%s body %q != unknown body %q", name, rec.Body.String(), unknown.Body.String())
		}
	}
}

// TestShareInternalAssetSnapshotScoped is SC4 row 9: a link authenticates ONE snapshot, never any asset id.
func TestShareInternalAssetSnapshotScoped(t *testing.T) {
	pool := migratedPool(t)
	env := newShareAPIEnv(t, pool, true)
	owner := seedShareExportIdentity(t, pool)
	conv1 := seedExportConversation(t, env.convStore, owner, "C1", oneTurn())
	conv2 := seedExportConversation(t, env.convStore, owner, "C2", oneTurn())
	seedBundledArtifact(env, "one.txt", []byte("one"))
	link1 := createShare(t, env, owner, conv1, "internal", http.StatusCreated)
	asset2 := seedBundledArtifact(env, "two.txt", []byte("two"))
	link2 := createShare(t, env, owner, conv2, "internal", http.StatusCreated)
	rec := shareReq(env.server, http.MethodGet, "/api/shares/"+link1.ID+"/asset/"+asset2, owner, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign snapshot asset status = %d, want 404", rec.Code)
	}
	if rec2 := shareReq(env.server, http.MethodGet, "/api/shares/"+link2.ID+"/asset/"+asset2, owner, ""); rec2.Code != http.StatusOK {
		t.Fatalf("own snapshot asset status = %d, want 200: %s", rec2.Code, rec2.Body.String())
	}
}

// TestShareInternalAssetContentType: the inert-bytes rule is not a public-tier special case.
func TestShareInternalAssetContentType(t *testing.T) {
	pool := migratedPool(t)
	env := newShareAPIEnv(t, pool, true)
	owner, convID := seedOwnerAndConversation(t, env, oneTurn())
	assetID := seedBundledArtifact(env, "page.html", []byte("<html><script>evil()</script></html>"))
	link := createShare(t, env, owner, convID, "internal", http.StatusCreated)
	rec := shareReq(env.server, http.MethodGet, "/api/shares/"+link.ID+"/asset/"+assetID, owner, "")
	assertInertAttachment(t, rec)
}

// TestSharePublicMintWithCapability wraps the real RequireCapability chain: capability + kill-switch on ⇒ 201, token once.
func TestSharePublicMintWithCapability(t *testing.T) {
	pool := migratedPool(t)
	env := newShareAPIEnv(t, pool, true)
	owner, convID := seedOwnerAndConversation(t, env, oneTurn())
	if err := identity.New(pool).GrantCapability(context.Background(), owner, "share.public"); err != nil {
		t.Fatalf("grant share.public: %v", err)
	}
	gated := RequireCapability(env.server.Mux(), AuthDeps{SecretConfigured: true, Identities: storeChecker{store: identity.New(pool)}}, "share.public")
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/shares", strings.NewReader(
		`{"conversation_id":"`+convID+`","tier":"public"}`)), owner)
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if n := strings.Count(rec.Body.String(), `"url":"/s/`); n != 1 {
		t.Fatalf("plaintext token url appears %d times in the body, want exactly 1: %s", n, rec.Body.String())
	}
}

// TestSharePublicOrgKillSwitch is R-08: kill-switch off ⇒ 403 even with the capability granted and SecretConfigured=false.
func TestSharePublicOrgKillSwitch(t *testing.T) {
	pool := migratedPool(t)
	env := newShareAPIEnv(t, pool, false) // kill-switch OFF
	owner, convID := seedOwnerAndConversation(t, env, oneTurn())
	if err := identity.New(pool).GrantCapability(context.Background(), owner, "share.public"); err != nil {
		t.Fatalf("grant share.public: %v", err)
	}
	gated := RequireCapability(env.server.Mux(), AuthDeps{SecretConfigured: false}, "share.public")
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/shares", strings.NewReader(
		`{"conversation_id":"`+convID+`","tier":"public"}`)), owner)
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (R-08 regression)", rec.Code)
	}
}

// TestShareRevokeThen404 proves the 404 after revoke is never a stale render: no title, a short body.
func TestShareRevokeThen404(t *testing.T) {
	pool := migratedPool(t)
	env := newShareAPIEnv(t, pool, true)
	owner, convID := seedOwnerAndConversation(t, env, oneTurn())
	if err := env.convStore.SetTitleIfNull(context.Background(), convID, "Secret Title"); err != nil {
		t.Fatalf("set title: %v", err)
	}
	link := createShare(t, env, owner, convID, "public", http.StatusCreated)
	token := publicToken(t, link.URL)
	if rec := shareReq(env.server, http.MethodDelete, "/api/shares/"+link.ID, owner, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	rec := shareReq(env.server, http.MethodGet, "/s/"+token+"/data", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("revoked token status = %d, want 404", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, "Secret Title") || len(body) > 64 {
		t.Fatalf("404 body is not a short, title-free response: %q", body)
	}
}

// TestShareTokenNoOracle proves unknown/revoked/expired tokens produce byte-identical 404s.
func TestShareTokenNoOracle(t *testing.T) {
	pool := migratedPool(t)
	env := newShareAPIEnv(t, pool, true)
	owner, convID := seedOwnerAndConversation(t, env, oneTurn())
	revokedLink := createShare(t, env, owner, convID, "public", http.StatusCreated)
	revokedToken := publicToken(t, revokedLink.URL)
	if rec := shareReq(env.server, http.MethodDelete, "/api/shares/"+revokedLink.ID, owner, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204", rec.Code)
	}
	plaintext, hash, err := share.Mint()
	if err != nil {
		t.Fatalf("mint expired token: %v", err)
	}
	expired := share.Link{
		ID: uuid.Must(uuid.NewV7()), OwnerIdentityID: uuid.MustParse(owner), ConversationID: uuid.MustParse(convID),
		Tier: "public", SnapshotID: uuid.Must(uuid.NewV7()), SnapshotBucket: shareAPITestBucket,
		ExpiresAt: pastShareTime(time.Hour),
	}
	if err := env.adapter.store.Insert(context.Background(), expired, hash[:]); err != nil {
		t.Fatalf("insert expired link: %v", err)
	}
	unknown := shareReq(env.server, http.MethodGet, "/s/"+uuid.Must(uuid.NewV7()).String()+"/data", "", "")
	revoked := shareReq(env.server, http.MethodGet, "/s/"+revokedToken+"/data", "", "")
	exp := shareReq(env.server, http.MethodGet, "/s/"+plaintext+"/data", "", "")
	for name, rec := range map[string]*httptest.ResponseRecorder{"revoked": revoked, "expired": exp} {
		if rec.Code != http.StatusNotFound || rec.Body.String() != unknown.Body.String() {
			t.Fatalf("%s: status=%d body=%q, want 404 identical to unknown's %q", name, rec.Code, rec.Body.String(), unknown.Body.String())
		}
	}
}

// TestShareTokenEnumeration proves 1000 random tokens all 404 identically.
func TestShareTokenEnumeration(t *testing.T) {
	pool := migratedPool(t)
	env := newShareAPIEnv(t, pool, true)
	baseline := shareReq(env.server, http.MethodGet, "/s/"+uuid.Must(uuid.NewV7()).String()+"/data", "", "")
	if baseline.Code != http.StatusNotFound {
		t.Fatalf("baseline status = %d, want 404", baseline.Code)
	}
	for i := 0; i < 1000; i++ {
		token, _, err := share.Mint()
		if err != nil {
			t.Fatalf("mint random token %d: %v", i, err)
		}
		rec := shareReq(env.server, http.MethodGet, "/s/"+token+"/data", "", "")
		if rec.Code != http.StatusNotFound || rec.Body.String() != baseline.Body.String() {
			t.Fatalf("token %d: status=%d body=%q, want 404 identical to baseline", i, rec.Code, rec.Body.String())
		}
	}
}

// TestSharePublicOpenAuditNoPII: a distinctive X-Forwarded-For/User-Agent never lands in share_audit (D-14).
func TestSharePublicOpenAuditNoPII(t *testing.T) {
	pool := migratedPool(t)
	env := newShareAPIEnv(t, pool, true)
	owner, convID := seedOwnerAndConversation(t, env, oneTurn())
	link := createShare(t, env, owner, convID, "public", http.StatusCreated)
	token := publicToken(t, link.URL)
	req := httptest.NewRequest(http.MethodGet, "/s/"+token+"/data", nil)
	const distinctiveIP, distinctiveUA = "203.0.113.66", "AuraProbe/PII-canary-1.0"
	req.Header.Set("X-Forwarded-For", distinctiveIP)
	req.Header.Set("User-Agent", distinctiveUA)
	rec := httptest.NewRecorder()
	env.server.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	rows, err := pool.Query(context.Background(),
		"SELECT identity_id, detail FROM aura.share_audit WHERE share_link_id = $1", uuid.MustParse(link.ID))
	if err != nil {
		t.Fatalf("query share_audit: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var identityID, detail string
		if err := rows.Scan(&identityID, &detail); err != nil {
			t.Fatalf("scan share_audit row: %v", err)
		}
		if strings.Contains(identityID, distinctiveIP) || strings.Contains(detail, distinctiveIP) ||
			strings.Contains(identityID, distinctiveUA) || strings.Contains(detail, distinctiveUA) {
			t.Fatalf("share_audit row leaked PII: identity_id=%q detail=%q", identityID, detail)
		}
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

// TestShareTokenNeverLogged: across mint->open->revoke, the plaintext token appears in no log line or share_audit row.
func TestShareTokenNeverLogged(t *testing.T) {
	pool := migratedPool(t)
	env := newShareAPIEnv(t, pool, true)
	owner, convID := seedOwnerAndConversation(t, env, oneTurn())
	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	link := createShare(t, env, owner, convID, "public", http.StatusCreated)
	token := publicToken(t, link.URL)
	if rec := shareReq(env.server, http.MethodGet, "/s/"+token+"/data", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("open status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec := shareReq(env.server, http.MethodDelete, "/api/shares/"+link.ID, owner, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(logBuf.String(), token) {
		t.Fatalf("plaintext token appeared in slog output: %s", logBuf.String())
	}
	var detailCount int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM aura.share_audit WHERE detail LIKE $1", "%"+token+"%").Scan(&detailCount); err != nil {
		t.Fatalf("scan share_audit for token leak: %v", err)
	}
	if detailCount != 0 {
		t.Fatalf("plaintext token appeared in %d share_audit row(s)", detailCount)
	}
}

// TestSharePublicAssetContentType: a text/html artifact still serves neutral-typed over the public lane.
func TestSharePublicAssetContentType(t *testing.T) {
	pool := migratedPool(t)
	env := newShareAPIEnv(t, pool, true)
	owner, convID := seedOwnerAndConversation(t, env, oneTurn())
	assetID := seedBundledArtifact(env, "page.html", []byte("<html><script>evil()</script></html>"))
	link := createShare(t, env, owner, convID, "public", http.StatusCreated)
	token := publicToken(t, link.URL)
	rec := shareReq(env.server, http.MethodGet, "/s/"+token+"/asset/"+assetID, "", "")
	assertInertAttachment(t, rec)
}

// TestSharePublicAssetHeaderInjection: contentDisposition percent-escapes a hostile filename; no X-Evil header lands.
func TestSharePublicAssetHeaderInjection(t *testing.T) {
	pool := migratedPool(t)
	env := newShareAPIEnv(t, pool, true)
	owner, convID := seedOwnerAndConversation(t, env, oneTurn())
	assetID := seedBundledArtifact(env, "a\"; rm -rf /\r\nX-Evil: 1", []byte("payload"))
	link := createShare(t, env, owner, convID, "public", http.StatusCreated)
	token := publicToken(t, link.URL)
	rec := shareReq(env.server, http.MethodGet, "/s/"+token+"/asset/"+assetID, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Evil") != "" {
		t.Fatalf("injected X-Evil header is present: %q", rec.Header().Get("X-Evil"))
	}
	if strings.ContainsAny(rec.Header().Get("Content-Disposition"), "\r\n") {
		t.Fatalf("Content-Disposition carries a raw CR/LF: %q", rec.Header().Get("Content-Disposition"))
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
	links, err := env.adapter.store.ListForConversation(context.Background(), convID, owner)
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
