package agui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRequireAuthRejectsBlankPrincipal locks LO-02: an authenticated session that validates
// to a BLANK identity ("", ok=true) MUST be rejected at RequireAuth — never scoped to the
// seeded `local` operator. The existence re-check (GetIdentityByID("")) misses, so the
// request is bounced to login (302 for a browser GET, 401 for an XHR) and the wrapped
// handler is never reached. This guards the invariant RequireAuth + session_validate already
// uphold against a future change that would turn the no-principal `local` fallback
// (scopedIdentityID) into a blank-principal impersonation (cf. HI-03).
func TestRequireAuthRejectsBlankPrincipal(t *testing.T) {
	t.Parallel()

	deps := testDeps("operator-secret")
	// The anti-case: an authenticated-but-blank session. A validator returning ("", true)
	// simulates the edge where the session verifies but binds no principal.
	deps.SessionValidator = func(_ *http.Request) (string, bool) { return "", true }
	// A real store WOULD resolve testLocalID but NOT the empty id — proving the rejection is
	// specific to the blank principal, not a blanket store failure.
	deps.Identities = &fakeIdentities{
		known: map[string]Identity{testLocalID: {ID: testLocalID, Name: "local", Kind: "user"}},
	}

	t.Run("browser GET -> 302 login, next not reached", func(t *testing.T) {
		next, hit := nextRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept", "text/html")
		rec := httptest.NewRecorder()

		RequireAuth(next, deps).ServeHTTP(rec, req)

		if rec.Code != http.StatusFound {
			t.Fatalf("status = %d, want 302 (blank principal rejected)", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/login" {
			t.Fatalf("Location = %q, want /login", loc)
		}
		if *hit {
			t.Fatal("next was reached with a BLANK principal — LO-02 impersonation risk")
		}
	})

	t.Run("XHR -> 401, next not reached", func(t *testing.T) {
		next, hit := nextRecorder()
		req := httptest.NewRequest(http.MethodGet, "/threads/x/messages", nil) // no Accept: text/html
		rec := httptest.NewRecorder()

		RequireAuth(next, deps).ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (blank principal rejected)", rec.Code)
		}
		if *hit {
			t.Fatal("next was reached with a BLANK principal on an API request")
		}
	})
}
