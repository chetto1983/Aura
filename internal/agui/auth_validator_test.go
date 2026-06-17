package agui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRequireAuth_SessionValidatorSeam pins the Option-A2 seam: when a SessionValidator
// is wired (AURA_WEB_AUTH_PROVIDER=authula), RequireAuth uses it instead of the HMAC
// cookie path, then applies the SAME existence re-check + withPrincipal contract.
func TestRequireAuth_SessionValidatorSeam(t *testing.T) {
	const validatedID = testLocalID
	deps := AuthDeps{
		SecretConfigured: true,
		Identities: &fakeIdentities{
			known: map[string]Identity{validatedID: {ID: validatedID, Name: "local", Kind: "user"}},
		},
		LoginPath: "/login",
	}

	t.Run("validator hit binds principal", func(t *testing.T) {
		d := deps
		d.SessionValidator = func(_ *http.Request) (string, bool) { return validatedID, true }
		var seen string
		h := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = principalFrom(r.Context())
			w.WriteHeader(http.StatusOK)
		}), d)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/threads/x", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if seen != validatedID {
			t.Errorf("principal = %q, want %q", seen, validatedID)
		}
	})

	t.Run("validator miss redirects to login", func(t *testing.T) {
		d := deps
		d.SessionValidator = func(_ *http.Request) (string, bool) { return "", false }
		h := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Fatal("handler must not run on validator miss")
		}), d)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept", "text/html")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusFound {
			t.Errorf("status = %d, want 302 redirect", rec.Code)
		}
	})

	t.Run("validated-but-deleted identity 401s (existence re-check)", func(t *testing.T) {
		d := deps
		d.Identities = &fakeIdentities{getErr: errFakeNotFound} // identity gone
		d.SessionValidator = func(_ *http.Request) (string, bool) { return validatedID, true }
		h := RequireAuth(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatal("handler must not run for a deleted identity")
		}), d)
		req := httptest.NewRequest(http.MethodGet, "/threads/x", nil) // XHR (no html)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})
}

// TestIsPublicPath_AuthBasePath pins that the Authula credential subtree is reachable
// without a session (login/TOTP happen pre-session) while a non-/auth path stays gated.
func TestIsPublicPath_AuthBasePath(t *testing.T) {
	d := AuthDeps{LoginPath: "/login", AuthBasePath: "/auth"}
	public := []string{"/auth", "/auth/email-password/sign-in", "/auth/totp/verify", "/login"}
	for _, p := range public {
		if !d.isPublicPath(p) {
			t.Errorf("%q should be public", p)
		}
	}
	gated := []string{"/", "/agent/run", "/api/conversations", "/authx", "/auths"}
	for _, p := range gated {
		if d.isPublicPath(p) {
			t.Errorf("%q should be gated, not public", p)
		}
	}

	// Without AuthBasePath set (passphrase path) nothing under /auth is public.
	none := AuthDeps{LoginPath: "/login"}
	if none.isPublicPath("/auth/email-password/sign-in") {
		t.Error("/auth/* must be gated when AuthBasePath is unset (passphrase path)")
	}
}
