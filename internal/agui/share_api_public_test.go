//go:build db_integration

// share_api_public_test.go — integration tests for the unauthenticated public token routes
// (share_api_public.go): GET /s/{token}/data and GET /s/{token}/asset/{id}, plus the
// capability/kill-switch gate over POST /api/shares' public tier. Split out of
// share_api_test.go (37F-13 refactor-on-touch, CLAUDE.md 600-LOC cap) — that file's own header
// carries the shared test infrastructure (shareAPIEnv/newShareAPIEnv/createShare/shareReq/etc.)
// every test below depends on. RequireCapability is exercised for real only in
// TestSharePublicMintWithCapability/TestSharePublicOrgKillSwitch. Run via:
// go test -tags db_integration -race -p 1 -count=1 -run 'TestSharePublic|TestShareToken|TestShareRevoke' ./internal/agui/
package agui

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/share"
	"github.com/google/uuid"
)

// publicToken extracts the plaintext token from a ShareLink.url of the form "/s/{token}".
func publicToken(t *testing.T, url string) string {
	t.Helper()
	const prefix = "/s/"
	if !strings.HasPrefix(url, prefix) {
		t.Fatalf("public link url %q missing /s/ prefix", url)
	}
	return strings.TrimPrefix(url, prefix)
}

// TestSharePublicMintWithCapability wraps the real RequireCapability chain: capability + kill-switch on ⇒ 201, token once.
func TestSharePublicMintWithCapability(t *testing.T) {
	pool := migratedPool(t)
	env := newShareAPIEnv(t, pool, true)
	owner, convID := seedOwnerAndConversation(t, env, oneTurn())
	if err := identity.New(pool).GrantCapability(ownerCtx(), owner, "share.public"); err != nil {
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
	if err := identity.New(pool).GrantCapability(ownerCtx(), owner, "share.public"); err != nil {
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
	if err := env.convStore.SetTitleIfNull(ownerCtx(), convID, "Secret Title"); err != nil {
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
	if err := env.adapter.store.Insert(ownerCtx(), expired, hash[:]); err != nil {
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
	rows, err := pool.Query(ownerCtx(),
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
	if err := pool.QueryRow(ownerCtx(),
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
