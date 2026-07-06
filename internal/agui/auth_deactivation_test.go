package agui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// auth_deactivation_test.go is the HI-02 gate coverage: a soft-deleted (deactivated_at IS
// NOT NULL) principal whose Authula token / HMAC cookie is still valid MUST be denied at the
// auth boundary so it cannot re-authenticate during the de-provisioning grace window, while a
// live identity keeps passing unchanged. The identity→Deactivated projection is proven in
// internal/identity (store_deactivated_test.go); here we prove RequireAuth acts on it.

// TestRequireAuthDeniesDeactivatedIdentity: a valid session bound to a deactivated identity
// is refused (302 for a browser GET, 401 for an XHR) and next.ServeHTTP is NOT called.
func TestRequireAuthDeniesDeactivatedIdentity(t *testing.T) {
	t.Parallel()

	deactivatedDeps := func() AuthDeps {
		d := testDeps("operator-secret")
		d.Identities = &fakeIdentities{known: map[string]Identity{
			testLocalID: {ID: testLocalID, Name: "local", Kind: "user", Deactivated: true},
		}}
		return d
	}

	t.Run("deactivated on an API request -> 401, next not reached", func(t *testing.T) {
		deps := deactivatedDeps()
		next, hit := nextRecorder()
		rec := httptest.NewRecorder()
		RequireAuth(next, deps).ServeHTTP(rec, validCookieReq(deps, "/threads/x", false))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 for a deactivated principal", rec.Code)
		}
		if *hit {
			t.Fatal("next reached with a deactivated identity")
		}
	})

	t.Run("deactivated on a browser GET -> 302 to login, next not reached", func(t *testing.T) {
		deps := deactivatedDeps()
		next, hit := nextRecorder()
		rec := httptest.NewRecorder()
		RequireAuth(next, deps).ServeHTTP(rec, validCookieReq(deps, "/", true))
		if rec.Code != http.StatusFound {
			t.Fatalf("status = %d, want 302 for a deactivated principal on a browser GET", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/login" {
			t.Fatalf("Location = %q, want /login", loc)
		}
		if *hit {
			t.Fatal("next reached with a deactivated identity")
		}
	})
}

// TestRequireAuthAllowsLiveIdentity: a live (Deactivated:false) principal with a valid
// session passes and is stamped — the deactivation gate must not regress the normal path.
func TestRequireAuthAllowsLiveIdentity(t *testing.T) {
	t.Parallel()
	deps := testDeps("operator-secret")
	deps.Identities = &fakeIdentities{known: map[string]Identity{
		testLocalID: {ID: testLocalID, Name: "local", Kind: "user", Deactivated: false},
	}}
	var gotPrincipal string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPrincipal = principalFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	RequireAuth(next, deps).ServeHTTP(rec, validCookieReq(deps, "/", false))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a live identity", rec.Code)
	}
	if gotPrincipal != testLocalID {
		t.Fatalf("principal = %q, want %q", gotPrincipal, testLocalID)
	}
}
