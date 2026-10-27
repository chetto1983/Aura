package agui

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testLocalID = "00000000-0000-0000-0000-000000000001"

// fakeIdentities is the injected identityChecker for the auth unit tests: it answers
// GetIdentityByID/HasCapability from in-memory maps so the middleware + capability gate
// are exercised without a DB (the real store is covered by the db_integration test).
type fakeIdentities struct {
	known        map[string]Identity // id -> identity (a missing id ⇒ ErrIdentityNotFound)
	capabilities map[string]bool     // "id|capability" -> granted
	getErr       error               // forced GetIdentityByID error (e.g. not-found)
	hasErr       error               // forced HasCapability error
}

func (f *fakeIdentities) GetIdentityByID(_ context.Context, id string) (Identity, error) {
	if f.getErr != nil {
		return Identity{}, f.getErr
	}
	idn, ok := f.known[id]
	if !ok {
		return Identity{}, errFakeNotFound
	}
	return idn, nil
}

func (f *fakeIdentities) HasCapability(_ context.Context, id, capability string) (bool, error) {
	if f.hasErr != nil {
		return false, f.hasErr
	}
	return f.capabilities[id+"|"+capability], nil
}

var errFakeNotFound = &fakeErr{"identity not found"}

type fakeErr struct{ s string }

func (e *fakeErr) Error() string { return e.s }

// testSessionCookie is the cookie testDeps' SessionValidator reads. Production's validator
// is Authula's (cmd/aura/serve_auth.go); RequireAuth only sees the identity it returns.
const testSessionCookie = "__Host-test_session"

// sessionCookieFor is a cookie testDeps' validator accepts as a session bound to id.
func sessionCookieFor(id string) *http.Cookie {
	return &http.Cookie{Name: testSessionCookie, Value: "valid:" + id}
}

// testDeps builds an AuthDeps with auth ACTIVE (a configured secret) bound to the
// seeded local identity, with the local id holding "agent.run". Its validator accepts
// exactly the cookies sessionCookieFor builds.
func testDeps(secret string) AuthDeps {
	return AuthDeps{
		SessionValidator: func(r *http.Request) (string, bool) {
			c, err := r.Cookie(testSessionCookie)
			if err != nil {
				return "", false
			}
			return strings.CutPrefix(c.Value, "valid:")
		},
		SessionCookieName: testSessionCookie,
		SecretConfigured:  secret != "",
		LocalIdentityID:   testLocalID,
		Identities: &fakeIdentities{
			known:        map[string]Identity{testLocalID: {ID: testLocalID, Name: "local", Kind: "user"}},
			capabilities: map[string]bool{testLocalID + "|agent.run": true},
		},
		LoginPath: "/login",
		// Fake the webui.IsPublicAsset predicate: the production wiring serves the shared
		// SPA bundle/styles + PWA + icons (under /assets/ and a few root files) pre-auth.
		PublicAsset: func(p string) bool {
			return strings.HasPrefix(p, "/assets/") ||
				p == "/registerSW.js" || p == "/manifest.webmanifest" || p == "/favicon.svg"
		},
	}
}

func TestIsLoopbackRemoteAddr(t *testing.T) {
	t.Parallel()
	cases := []struct {
		remoteAddr string
		want       bool
	}{
		{"127.0.0.1:51234", true},
		{"[::1]:51234", true},
		{"172.18.0.1:51234", false},
		{"not-an-addr", false},
	}
	for _, tc := range cases {
		t.Run(tc.remoteAddr, func(t *testing.T) {
			if got := isLoopbackRemoteAddr(tc.remoteAddr); got != tc.want {
				t.Fatalf("isLoopbackRemoteAddr(%q) = %v, want %v", tc.remoteAddr, got, tc.want)
			}
		})
	}
}

// nextRecorder is a sentinel `next` handler that records whether it was reached and
// answers 200. RequireAuth/requireCapability only call it when the gate passes.
func nextRecorder() (http.Handler, *bool) {
	hit := false
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	})
	return h, &hit
}

// validCookieReq builds a GET request to path carrying a valid session cookie for the
// local identity. acceptHTML toggles the browser-navigation Accept header.
func validCookieReq(deps AuthDeps, path string, acceptHTML bool) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if acceptHTML {
		req.Header.Set("Accept", "text/html")
	}
	req.AddCookie(sessionCookieFor(deps.LocalIdentityID))
	return req
}

func TestRequireAuth(t *testing.T) {
	t.Parallel()
	deps := testDeps("operator-secret")

	t.Run("valid cookie reaches next + binds principal", func(t *testing.T) {
		var gotPrincipal string
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPrincipal = principalFrom(r.Context())
			w.WriteHeader(http.StatusOK)
		})
		rec := httptest.NewRecorder()
		RequireAuth(next, deps).ServeHTTP(rec, validCookieReq(deps, "/", false))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if gotPrincipal != testLocalID {
			t.Fatalf("principal = %q, want %q", gotPrincipal, testLocalID)
		}
	})

	t.Run("no cookie on a browser GET -> 302 to login", func(t *testing.T) {
		next, hit := nextRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept", "text/html")
		rec := httptest.NewRecorder()
		RequireAuth(next, deps).ServeHTTP(rec, req)
		if rec.Code != http.StatusFound {
			t.Fatalf("status = %d, want 302", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/login" {
			t.Fatalf("Location = %q, want /login", loc)
		}
		if *hit {
			t.Fatal("next was reached without a session")
		}
	})

	t.Run("no cookie on an API request -> 401", func(t *testing.T) {
		next, hit := nextRecorder()
		req := httptest.NewRequest(http.MethodGet, "/threads/x/messages", nil) // no Accept html
		rec := httptest.NewRecorder()
		RequireAuth(next, deps).ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if *hit {
			t.Fatal("next was reached without a session")
		}
	})

	t.Run("cookie the validator refuses -> reject", func(t *testing.T) {
		next, hit := nextRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: testSessionCookie, Value: "forged:" + deps.LocalIdentityID})
		rec := httptest.NewRecorder()
		RequireAuth(next, deps).ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 on a refused cookie", rec.Code)
		}
		if *hit {
			t.Fatal("next reached with a refused cookie")
		}
	})

	// Until 2026-10-26 a nil validator fell back to the passphrase HMAC cookie, verified
	// under whatever SigningKey the deps carried. Production carried none, so a MAC under an
	// empty key, which anyone can compute, would have validated. Now nothing does.
	t.Run("no validator -> every session refused", func(t *testing.T) {
		bare := deps
		bare.SessionValidator = nil
		payload := deps.LocalIdentityID + "|" + strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, nil)
		_, _ = mac.Write([]byte(payload))
		forged := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
			base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
		next, hit := nextRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "__Host-aura_session", Value: forged})
		req.AddCookie(sessionCookieFor(deps.LocalIdentityID))
		rec := httptest.NewRecorder()
		RequireAuth(next, bare).ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized || *hit {
			t.Fatalf("status = %d, reached = %v: an AuthDeps without a validator must accept no session", rec.Code, *hit)
		}
	})

	t.Run("deleted bound identity -> reject", func(t *testing.T) {
		gone := testDeps("operator-secret")
		gone.Identities = &fakeIdentities{getErr: errFakeNotFound} // GetIdentityByID always not-found
		next, hit := nextRecorder()
		rec := httptest.NewRecorder()
		RequireAuth(next, gone).ServeHTTP(rec, validCookieReq(gone, "/threads/x", false))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 when bound identity is gone", rec.Code)
		}
		if *hit {
			t.Fatal("next reached with a deleted bound identity")
		}
	})

	t.Run("D-03 public-path table", func(t *testing.T) {
		cases := []struct {
			name       string
			method     string
			path       string
			acceptHTML bool
			public     bool // true => reachable WITHOUT a cookie
		}{
			{"GET /healthz public", http.MethodGet, "/healthz", false, true},
			{"login page public", http.MethodGet, "/login", true, true},
			{"SPA bundle asset public", http.MethodGet, "/assets/index-abc123.js", false, true},
			{"PWA service worker public", http.MethodGet, "/registerSW.js", false, true},
			{"webmanifest public", http.MethodGet, "/manifest.webmanifest", false, true},
			{"SPA shell gated", http.MethodGet, "/", true, false},
			{"/readyz gated from non-loopback client", http.MethodGet, "/readyz", false, false},
			{"/metrics gated", http.MethodGet, "/metrics", false, false},
			{"/debug/vars gated", http.MethodGet, "/debug/vars", false, false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				next, hit := nextRecorder()
				req := httptest.NewRequest(tc.method, tc.path, nil)
				if tc.acceptHTML {
					req.Header.Set("Accept", "text/html")
				}
				rec := httptest.NewRecorder()
				RequireAuth(next, deps).ServeHTTP(rec, req)
				if tc.public {
					if !*hit || rec.Code != http.StatusOK {
						t.Fatalf("public path %q: hit=%v code=%d, want hit=true code=200", tc.path, *hit, rec.Code)
					}
				} else {
					if *hit {
						t.Fatalf("gated path %q reached next without a cookie", tc.path)
					}
					if rec.Code == http.StatusOK {
						t.Fatalf("gated path %q returned 200 without a cookie", tc.path)
					}
				}
			})
		}
	})

	t.Run("loopback /readyz health probe bypasses session", func(t *testing.T) {
		next, hit := nextRecorder()
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		req.RemoteAddr = "127.0.0.1:51234"
		rec := httptest.NewRecorder()
		RequireAuth(next, deps).ServeHTTP(rec, req)
		if !*hit || rec.Code != http.StatusOK {
			t.Fatalf("loopback /readyz: hit=%v code=%d, want hit=true code=200", *hit, rec.Code)
		}
	})

	t.Run("non-loopback /readyz still requires a session", func(t *testing.T) {
		next, hit := nextRecorder()
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		req.RemoteAddr = "172.18.0.1:51234"
		rec := httptest.NewRecorder()
		RequireAuth(next, deps).ServeHTTP(rec, req)
		if *hit || rec.Code == http.StatusOK {
			t.Fatalf("non-loopback /readyz reached handler: hit=%v code=%d", *hit, rec.Code)
		}
	})

	t.Run("unconfigured secret -> pass-through no-op", func(t *testing.T) {
		open := testDeps("") // SecretConfigured=false
		next, hit := nextRecorder()
		// Even a normally-gated path with NO cookie reaches next when auth is disabled.
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rec := httptest.NewRecorder()
		RequireAuth(next, open).ServeHTTP(rec, req)
		if !*hit || rec.Code != http.StatusOK {
			t.Fatalf("loopback pass-through: hit=%v code=%d, want hit=true code=200", *hit, rec.Code)
		}
	})

	t.Run("RequireAuth reads no client auth header", func(t *testing.T) {
		next, hit := nextRecorder()
		// Forge every classic client-supplied auth/identity header; none must grant access.
		req := httptest.NewRequest(http.MethodGet, "/threads/x", nil)
		req.Header.Set("Authorization", "Bearer forged")
		req.Header.Set("X-Forwarded-User", testLocalID)
		req.Header.Set("X-Identity-Id", testLocalID)
		rec := httptest.NewRecorder()
		RequireAuth(next, deps).ServeHTTP(rec, req)
		if *hit || rec.Code == http.StatusOK {
			t.Fatal("a forged client auth header granted access — RequireAuth must read only the cookie")
		}
	})
}

func TestRequireCapability(t *testing.T) {
	t.Parallel()
	deps := testDeps("operator-secret")

	t.Run("unconfigured secret -> pass-through", func(t *testing.T) {
		next, hit := nextRecorder()
		req := httptest.NewRequest(http.MethodPost, "/agent/run", nil)
		rec := httptest.NewRecorder()
		RequireCapability(next, AuthDeps{}, "agent.run").ServeHTTP(rec, req)
		if !*hit || rec.Code != http.StatusOK {
			t.Fatalf("open loopback capability gate: hit=%v code=%d, want hit=true code=200", *hit, rec.Code)
		}
	})

	t.Run("local principal with agent.run -> next (wildcard)", func(t *testing.T) {
		next, hit := nextRecorder()
		req := httptest.NewRequest(http.MethodPost, "/agent/run", nil)
		req = withPrincipal(req, testLocalID)
		rec := httptest.NewRecorder()
		RequireCapability(next, deps, "agent.run").ServeHTTP(rec, req)
		if !*hit || rec.Code != http.StatusOK {
			t.Fatalf("authorized capability: hit=%v code=%d, want hit=true code=200", *hit, rec.Code)
		}
	})

	t.Run("forbidden capability -> 403", func(t *testing.T) {
		next, hit := nextRecorder()
		req := httptest.NewRequest(http.MethodPost, "/agent/run", nil)
		req = withPrincipal(req, testLocalID)
		rec := httptest.NewRecorder()
		RequireCapability(next, deps, "governance.write").ServeHTTP(rec, req) // not granted
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
		if *hit {
			t.Fatal("next reached without the capability")
		}
	})

	t.Run("no principal -> 403", func(t *testing.T) {
		next, hit := nextRecorder()
		req := httptest.NewRequest(http.MethodPost, "/agent/run", nil) // no principal on ctx
		rec := httptest.NewRecorder()
		RequireCapability(next, deps, "agent.run").ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
		if *hit {
			t.Fatal("next reached with no principal")
		}
	})

	t.Run("store error -> 403", func(t *testing.T) {
		errDeps := testDeps("operator-secret")
		errDeps.Identities = &fakeIdentities{hasErr: errFakeNotFound}
		next, hit := nextRecorder()
		req := httptest.NewRequest(http.MethodPost, "/agent/run", nil)
		req = withPrincipal(req, testLocalID)
		rec := httptest.NewRecorder()
		RequireCapability(next, errDeps, "agent.run").ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 on store error", rec.Code)
		}
		if *hit {
			t.Fatal("next reached despite a store error")
		}
	})
}
