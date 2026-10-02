//go:build db_integration

// A cockpit sign-out must end the server session, not only leave the page. Measured
// 2026-10-01 on the lab VM: POST /auth/sign-out answered 401 and the same session cookie
// went on answering 200, so every logout left a live session until its 12 h expiry.
//
//	go test -tags db_integration ./internal/webauth -run TestAuthulaSignOut -count=1 -p 1

package webauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuthulaSignOutRevokesTheSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	provider, _, _ := newMultiUserProvider(t, ctx)
	userID := enrollUser(t, ctx, provider, "signout")
	core := provider.CoreServices()
	rawToken, err := core.TokenService.Generate()
	if err != nil {
		t.Fatalf("token generate: %v", err)
	}
	hashed := core.TokenService.Hash(rawToken)
	if _, err := core.SessionService.Create(ctx, userID, hashed, nil, nil, time.Hour); err != nil {
		t.Fatalf("session create: %v", err)
	}

	rec := httptest.NewRecorder()
	provider.Handler().ServeHTTP(rec, cockpitSignOut(rawToken))

	if rec.Code != http.StatusOK {
		t.Fatalf("sign-out answered %d %s, want 200", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	if sess, err := core.SessionService.GetByToken(ctx, hashed); err != nil || sess != nil {
		t.Fatalf("session after sign-out = %+v (err %v), want none", sess, err)
	}
	if !clearsCookie(rec.Result().Cookies(), SessionCookieName) {
		t.Errorf("sign-out did not clear %s; Set-Cookie: %q", SessionCookieName, rec.Header().Values("Set-Cookie"))
	}
}

// cockpitSignOut is the request web/src/shell/useLogoutSession.ts sends: an empty JSON
// body, the session cookie and the double-submit CSRF pair, from a trusted origin.
func cockpitSignOut(sessionToken string) *http.Request {
	const csrfToken = "sign-out-test-csrf-token"
	req := httptest.NewRequest(http.MethodPost, "/auth/sign-out", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:9080")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set(CSRFHeaderName, csrfToken)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sessionToken})
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: csrfToken})
	return req
}

func clearsCookie(cookies []*http.Cookie, name string) bool {
	for _, c := range cookies {
		if c.Name == name && c.Value == "" && c.MaxAge < 0 {
			return true
		}
	}
	return false
}
