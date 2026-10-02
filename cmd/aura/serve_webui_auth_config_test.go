package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chetto1983/aura/internal/webauth"
)

// Authula compares the CSRF header with the CSRF cookie, and a browser keeps only the
// newest cookie: a token minted per call would make the first of two login tabs fail.
func TestAuthConfigKeepsTheCSRFTokenTheBrowserHolds(t *testing.T) {
	handler := newAuthConfigHandler(nil)
	held := authConfigCSRFCookie(t, serveAuthConfig(handler, nil))

	rec := serveAuthConfig(handler, held)

	if got := authConfigCSRFCookie(t, rec).Value; got != held.Value {
		t.Errorf("second call set the CSRF cookie to %q, want the held %q", got, held.Value)
	}
	if got := rec.Header().Get(webauth.CSRFHeaderName); got != held.Value {
		t.Errorf("second call's CSRF header = %q, want the held %q", got, held.Value)
	}
	if got := authConfigBodyToken(t, rec); got != held.Value {
		t.Errorf("second call's csrf_token = %q, want the held %q", got, held.Value)
	}
}

func TestAuthConfigMintsATokenForACookieItDidNotIssue(t *testing.T) {
	for name, value := range map[string]string{
		"empty":            "",
		"not base64url":    "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!",
		"too short":        base64.RawURLEncoding.EncodeToString(make([]byte, 16)),
		"padded base64std": base64.StdEncoding.EncodeToString(make([]byte, 32)),
	} {
		t.Run(name, func(t *testing.T) {
			rec := serveAuthConfig(newAuthConfigHandler(nil), &http.Cookie{Name: webauth.CSRFCookieName, Value: value})

			got := authConfigCSRFCookie(t, rec).Value
			if got == value {
				t.Fatalf("kept the cookie %q, want a freshly minted token", value)
			}
			if raw, err := base64.RawURLEncoding.DecodeString(got); err != nil || len(raw) != 32 {
				t.Errorf("minted token %q is not 32 random bytes in base64url (err %v)", got, err)
			}
			if body := authConfigBodyToken(t, rec); body != got {
				t.Errorf("csrf_token = %q, cookie = %q, want them equal", body, got)
			}
		})
	}
}

func serveAuthConfig(handler http.HandlerFunc, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, authConfigRoute, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func authConfigCSRFCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == webauth.CSRFCookieName {
			return c
		}
	}
	t.Fatalf("no %s cookie in %q", webauth.CSRFCookieName, rec.Header().Values("Set-Cookie"))
	return nil
}

func authConfigBodyToken(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var cfg frontendAuthConfig
	if err := json.NewDecoder(rec.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode auth config: %v", err)
	}
	return cfg.CSRFToken
}
