//go:build db_integration

// Authula's CSRF plugin checks a request only on routes mapped to "csrf.protect". Until
// 2026-10-02 none was, so its double-submit token and its origin check ran nowhere.
//
//	go test -tags db_integration ./internal/webauth -run TestAuthulaCSRF -count=1 -p 1

package webauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// Walking the registered routes, not a list, is what fails this test when an Authula upgrade
// adds a state-changing route that csrfProtectedRoutes does not name.
func TestAuthulaCSRFRefusesEveryStateChangeWithoutThePair(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	provider, _, _ := newMultiUserProvider(t, ctx)
	handler := provider.Handler()

	var unsafe []string
	err := chi.Walk(provider.auth.Router().Get(), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
			unsafe = append(unsafe, method+" "+route)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk Authula's routes: %v", err)
	}
	// sign-in, four more email-password routes, five TOTP routes and sign-out; sign-up is
	// disabled. Fewer means the walk missed routes, not that they are safe.
	const knownStateChanges = 11
	if len(unsafe) < knownStateChanges {
		t.Fatalf("walked %d state-changing routes %q, want at least %d", len(unsafe), unsafe, knownStateChanges)
	}
	for _, route := range unsafe {
		method, path, _ := strings.Cut(route, " ")
		req := httptest.NewRequest(method, path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s without the CSRF pair answered %d %s, want 403", route, rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	}
}

func TestAuthulaCSRFRefusesAForgedSignOutAndKeepsTheSession(t *testing.T) {
	cases := map[string]func(*http.Request){
		"from another site": func(req *http.Request) {
			req.Header.Set("Origin", "https://attacker.example")
			req.Header.Set("Sec-Fetch-Site", "cross-site")
		},
		"with a header that is not the cookie": func(req *http.Request) {
			req.Header.Set(CSRFHeaderName, "forged-token")
		},
		"without the CSRF cookie": func(req *http.Request) {
			var kept []string
			for _, c := range req.Cookies() {
				if c.Name != CSRFCookieName {
					kept = append(kept, c.Name+"="+c.Value)
				}
			}
			req.Header.Set("Cookie", strings.Join(kept, "; "))
		},
	}
	for name, forge := range cases {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			provider, _, _ := newMultiUserProvider(t, ctx)
			rawToken, hashed := mintSession(t, ctx, provider, "csrf")
			req := cockpitSignOut(rawToken)
			forge(req)

			rec := httptest.NewRecorder()
			provider.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Errorf("forged sign-out answered %d %s, want 403", rec.Code, strings.TrimSpace(rec.Body.String()))
			}
			if sess, err := provider.CoreServices().SessionService.GetByToken(ctx, hashed); err != nil || sess == nil {
				t.Errorf("session after a forged sign-out = %v (err %v), want it alive", sess, err)
			}
		})
	}
}

// The cockpit's sign-in carries the pair, so a wrong password is refused for the password.
func TestAuthulaCSRFLetsTheCockpitSignInReachTheCredentialCheck(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	provider, _, _ := newMultiUserProvider(t, ctx)
	const csrfToken = "sign-in-test-csrf-token"
	req := httptest.NewRequest(http.MethodPost, "/auth/email-password/sign-in",
		strings.NewReader(`{"email":"nobody@aura.local","password":"not-the-password"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:9080")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set(CSRFHeaderName, csrfToken)
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: csrfToken})

	rec := httptest.NewRecorder()
	provider.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("cockpit sign-in with a wrong password answered %d %s, want 401", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
}
