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

	"pgregory.net/rapid"
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

// testDeps builds an AuthDeps with auth ACTIVE (a configured secret) bound to the
// seeded local identity, with the local id holding "agent.run".
func testDeps(secret string) AuthDeps {
	return AuthDeps{
		Secret:           secret,
		SigningKey:       deriveSigningKey(secret),
		TTL:              defaultSessionTTL,
		SecretConfigured: secret != "",
		LocalIdentityID:  testLocalID,
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

func signedSessionFixture(key []byte, identityID string, issued time.Time) string {
	payload := identityID + "|" + strconv.FormatInt(issued.Unix(), 10)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// TestSignVerifyRoundtrip is the property: for any identity id + an issued time within
// TTL, a correctly signed fixture verifies to the same id with ok==true.
func TestSignVerifyRoundtrip(t *testing.T) {
	t.Parallel()
	key := deriveSigningKey("operator-secret")
	rapid.Check(t, func(rt *rapid.T) {
		id := rapid.StringMatching(`[a-zA-Z0-9._-]{1,64}`).Draw(rt, "id")
		// issued is within (now-TTL, now] so the cookie is unexpired at verify time.
		ageSec := rapid.Int64Range(0, int64(defaultSessionTTL.Seconds())-1).Draw(rt, "ageSec")
		now := time.Unix(1_700_000_000, 0)
		issued := now.Add(-time.Duration(ageSec) * time.Second)

		value := signedSessionFixture(key, id, issued)
		gotID, ok := verifySession(key, value, defaultSessionTTL, now)
		if !ok {
			rt.Fatalf("verifySession ok=false for fresh cookie id=%q age=%ds", id, ageSec)
		}
		if gotID != id {
			rt.Fatalf("verifySession id = %q, want %q", gotID, id)
		}
	})
}

// TestVerifyTamper is the property: any single-byte mutation of a valid cookie value
// makes verifySession reject (forgery rejected).
func TestVerifyTamper(t *testing.T) {
	t.Parallel()
	key := deriveSigningKey("operator-secret")
	now := time.Unix(1_700_000_000, 0)
	rapid.Check(t, func(rt *rapid.T) {
		id := rapid.StringMatching(`[a-zA-Z0-9._-]{1,32}`).Draw(rt, "id")
		value := signedSessionFixture(key, id, now)
		pos := rapid.IntRange(0, len(value)-1).Draw(rt, "pos")
		// Flip the byte at pos to a different printable rune.
		orig := value[pos]
		repl := byte('A')
		if orig == repl {
			repl = 'B'
		}
		tampered := value[:pos] + string(repl) + value[pos+1:]
		if tampered == value {
			rt.Skip("no-op mutation")
		}
		if _, ok := verifySession(key, tampered, defaultSessionTTL, now); ok {
			rt.Fatalf("verifySession accepted a tampered cookie (pos=%d): %q", pos, tampered)
		}
	})
}

// TestVerifyExpiry covers the absolute-TTL rejection and the malformed-value rejections
// (no panic on any shape).
func TestVerifyExpiry(t *testing.T) {
	t.Parallel()
	key := deriveSigningKey("operator-secret")
	now := time.Unix(1_700_000_000, 0)

	t.Run("expired beyond absolute TTL", func(t *testing.T) {
		issued := now.Add(-defaultSessionTTL - time.Second)
		value := signedSessionFixture(key, testLocalID, issued)
		if _, ok := verifySession(key, value, defaultSessionTTL, now); ok {
			t.Fatal("verifySession accepted an expired cookie")
		}
	})
	t.Run("wrong key rejects", func(t *testing.T) {
		value := signedSessionFixture(key, testLocalID, now)
		other := deriveSigningKey("different-secret")
		if _, ok := verifySession(other, value, defaultSessionTTL, now); ok {
			t.Fatal("verifySession accepted a cookie signed with a different key")
		}
	})
	malformed := []struct{ name, value string }{
		{"no separator", "deadbeef"},
		{"empty", ""},
		{"too many parts ok-split", "a.b.c"}, // SplitN(.,2) keeps "b.c" as part[1] → base64 fail
		{"bad base64 payload", "!!!.AAAA"},
		{"bad base64 sig", "AAAA.!!!"},
	}
	for _, m := range malformed {
		t.Run("malformed/"+m.name, func(t *testing.T) {
			if _, ok := verifySession(key, m.value, defaultSessionTTL, now); ok {
				t.Fatalf("verifySession accepted malformed value %q", m.value)
			}
		})
	}
}

// readSessionCookie returns the __Host-aura_session cookie from a recorded response,
// or nil if absent.
func readSessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}
	return nil
}

func TestLogout(t *testing.T) {
	t.Parallel()
	deps := testDeps("operator-secret")
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	rec := httptest.NewRecorder()

	deps.LogoutHandler()(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	c := readSessionCookie(rec)
	if c == nil {
		t.Fatal("logout did not emit a clearing Set-Cookie")
	}
	if c.MaxAge >= 0 {
		t.Errorf("logout cookie MaxAge = %d, want < 0 (delete)", c.MaxAge)
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
	value := signedSessionFixture(deps.SigningKey, deps.LocalIdentityID, time.Now())
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: value})
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

	t.Run("tampered cookie -> reject", func(t *testing.T) {
		next, hit := nextRecorder()
		value := signedSessionFixture(deps.SigningKey, deps.LocalIdentityID, time.Now())
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: value + "x"})
		rec := httptest.NewRecorder()
		RequireAuth(next, deps).ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 on tampered cookie", rec.Code)
		}
		if *hit {
			t.Fatal("next reached with a tampered cookie")
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

// deriveSigningKey turns the operator secret into a 32-byte HMAC key.
// It lives in the tests, not in auth_cookie.go, because PRODUCTION never derives
// one: buildAuthDeps sets SigningKey nil and Secret "", and validateSession
// dispatches to the Authula SessionValidator instead of the passphrase branch.
// The derivation contract this expresses is therefore a test fixture, not the
// runtime's behaviour — the fallback branch it feeds is only reachable from here.
func deriveSigningKey(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}
