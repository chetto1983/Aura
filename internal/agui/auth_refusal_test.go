package agui

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureRefusals routes the default logger into a buffer at debug, so a test can read
// which refusals were logged and at what level.
func captureRefusals(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// The refusal reason is what an appliance's log needs to say why someone was sent back to
// the login page: before 2026-10-04 it said nothing, and an iPad report could not tell a
// cookie the browser never sent from one the server refused.
func TestRequireAuthLogsWhySessionWasRefused(t *testing.T) {
	deps := testDeps("operator-secret")
	stale := httptest.NewRequest(http.MethodGet, "/chat/abc", nil)
	stale.Header.Set("Accept", "text/html")
	stale.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "forged"})
	for _, tc := range []struct {
		name  string
		req   *http.Request
		want  string
		level string
	}{
		{"navigation without a cookie", navigation("/chat/abc"), "reason=no_cookie", "level=INFO"},
		{"api call without a cookie", httptest.NewRequest(http.MethodGet, "/api/me", nil), "reason=no_cookie", "level=DEBUG"},
		{"navigation with a refused cookie", stale, "reason=session_refused", "level=INFO"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureRefusals(t)
			next, hit := nextRecorder()
			RequireAuth(next, deps).ServeHTTP(httptest.NewRecorder(), tc.req)
			if *hit {
				t.Fatal("next was reached without a session")
			}
			line := logs.String()
			for _, want := range []string{`msg="web session refused"`, tc.want, tc.level, "path=" + tc.req.URL.Path} {
				if !strings.Contains(line, want) {
					t.Errorf("log %q is missing %q", line, want)
				}
			}
		})
	}
}

func TestRequireAuthLogsAnIdentityThatIsGone(t *testing.T) {
	deps := testDeps("operator-secret")
	deps.Identities = &fakeIdentities{known: map[string]Identity{}}
	logs := captureRefusals(t)
	next, _ := nextRecorder()
	RequireAuth(next, deps).ServeHTTP(httptest.NewRecorder(), validCookieReq(deps, "/", true))
	if !strings.Contains(logs.String(), "reason=identity_gone") {
		t.Fatalf("log %q does not name the gone identity", logs.String())
	}
}

// The provider's cookie name decides "no cookie" versus "refused": Authula's cookie is not
// the passphrase one, and reading the wrong name would call every refused Authula session
// a missing cookie.
func TestMissingOrRefusedReadsTheProvidersCookie(t *testing.T) {
	deps := AuthDeps{SessionCookieName: "__Host-authula_session"}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "passphrase-cookie"})
	if got := deps.missingOrRefused(req); got != refusedNoCookie {
		t.Fatalf("passphrase cookie under Authula = %q, want %q", got, refusedNoCookie)
	}
	req.AddCookie(&http.Cookie{Name: "__Host-authula_session", Value: "x"})
	if got := deps.missingOrRefused(req); got != refusedSession {
		t.Fatalf("Authula cookie = %q, want %q", got, refusedSession)
	}
}

func TestRequireAuthTruncatesTheLoggedUserAgent(t *testing.T) {
	logs := captureRefusals(t)
	req := navigation("/chat/abc")
	req.Header.Set("User-Agent", strings.Repeat("a", 4*userAgentLogCap))
	next, _ := nextRecorder()
	RequireAuth(next, testDeps("operator-secret")).ServeHTTP(httptest.NewRecorder(), req)
	if strings.Contains(logs.String(), strings.Repeat("a", userAgentLogCap+1)) {
		t.Fatal("the logged user agent is not capped")
	}
	if !strings.Contains(logs.String(), strings.Repeat("a", userAgentLogCap)) {
		t.Fatal("the logged user agent lost its first bytes")
	}
}

// A refused navigation used to land on a bare /login, so signing in again dropped the
// person on the home page whatever they had open.
func TestRefusedNavigationKeepsItsPage(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{"/", "/login"},
		{"/chat/abc?branch=2", "/login?next=%2Fchat%2Fabc%3Fbranch%3D2"},
		{"/api/assets/a1/download", "/login"},
	} {
		rec := httptest.NewRecorder()
		next, _ := nextRecorder()
		RequireAuth(next, testDeps("operator-secret")).ServeHTTP(rec, navigation(tc.path))
		if rec.Code != http.StatusFound {
			t.Fatalf("%s: status = %d, want 302", tc.path, rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != tc.want {
			t.Errorf("%s: Location = %q, want %q", tc.path, loc, tc.want)
		}
	}
}

func navigation(target string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Accept", "text/html")
	return req
}
