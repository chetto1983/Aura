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

// TestRequireAuth_SessionRenewerOrder pins that a session is slid forward only once the
// request is fully authenticated: never for a missing session, never for a deactivated
// identity, and before next so the refreshed cookie rides the response headers.
func TestRequireAuth_SessionRenewerOrder(t *testing.T) {
	cases := []struct {
		name      string
		valid     bool
		identity  Identity
		wantRenew bool
		wantCode  int
	}{
		{"authenticated request renews", true, Identity{ID: testLocalID, Kind: "user"}, true, http.StatusOK},
		{"missing session does not renew", false, Identity{ID: testLocalID, Kind: "user"}, false, http.StatusUnauthorized},
		{"deactivated identity does not renew", true, Identity{ID: testLocalID, Kind: "user", Deactivated: true}, false, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			renewed := false
			deps := AuthDeps{
				SecretConfigured: true,
				Identities:       &fakeIdentities{known: map[string]Identity{testLocalID: tc.identity}},
				LoginPath:        "/login",
				SessionValidator: func(*http.Request) (string, bool) { return testLocalID, tc.valid },
				SessionRenewer: func(w http.ResponseWriter, _ *http.Request) {
					renewed = true
					w.Header().Set("Set-Cookie", "renewed=1")
				},
			}
			h := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if !renewed {
					t.Error("next ran before the session was renewed")
				}
				w.WriteHeader(http.StatusOK)
			}), deps)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/me", nil))
			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantCode)
			}
			if renewed != tc.wantRenew {
				t.Fatalf("renewed = %v, want %v", renewed, tc.wantRenew)
			}
			if tc.wantRenew && rec.Header().Get("Set-Cookie") != "renewed=1" {
				t.Error("renewed cookie did not reach the response")
			}
		})
	}
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
