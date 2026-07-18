//go:build db_integration

// share_api_internal_test.go — integration tests for the D-10 bearer-within-auth internal
// share routes (share_api_internal.go): GET /api/shares/{id}/data and
// GET /api/shares/{id}/asset/{assetID}. Split out of share_api_test.go (37F-13 refactor-on-touch,
// CLAUDE.md 600-LOC cap) — that file's own header carries the shared test infrastructure
// (shareAPIEnv/newShareAPIEnv/createShare/shareReq/etc.) every test below depends on.
// RequireAuth is exercised for real only in TestShareInternalAnonymous401. Run via:
// go test -tags db_integration -race -p 1 -count=1 -run 'TestShareInternal' ./internal/agui/
package agui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/share"
	"github.com/google/uuid"
)

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
