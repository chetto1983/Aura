package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agui"
)

// wiringIdentities is a DB-free identityChecker for the auth-wiring test: it satisfies
// the agui consumer-side seam (exported methods returning agui.Identity) so a concrete
// AuthDeps can be built in cmd/aura without a Postgres pool. The real *identity.Store is
// exercised end-to-end by internal/agui/auth_capability_integration_test.go.
type wiringIdentities struct{ id string }

func (w wiringIdentities) GetIdentityByID(_ context.Context, id string) (agui.Identity, error) {
	if id != w.id {
		return agui.Identity{}, errWiringNotFound
	}
	return agui.Identity{ID: id, Name: "local", Kind: "user"}, nil
}

func (w wiringIdentities) HasCapability(_ context.Context, id, capability string) (bool, error) {
	return id == w.id && capability == agentRunCapability, nil // local holds agent.run
}

var errWiringNotFound = errors.New("identity not found")

type fakeAuthulaProvider struct {
	hits []string
}

func (f *fakeAuthulaProvider) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits = append(f.hits, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"authula":true}`)
	})
}

// TestServeWebuiAuthulaSubtreePublic pins the Option-A2 mount: Authula credential
// routes under /auth/* must be reachable before a session exists, while adjacent
// /authx paths remain gated and never leak to the credential provider.
func TestServeWebuiAuthulaSubtreePublic(t *testing.T) {
	const localID = "00000000-0000-0000-0000-000000000001"
	auth := agui.AuthDeps{
		Secret:           "operator-secret",
		SecretConfigured: true,
		LocalIdentityID:  localID,
		Identities:       wiringIdentities{id: localID},
		LoginPath:        "/login",
		SessionValidator: func(*http.Request) (string, bool) { return "", false },
	}
	var aguiHits []string
	aguiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aguiHits = append(aguiHits, r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	authula := &fakeAuthulaProvider{}
	handler, err := newServeHandler(aguiHandler, auth, authula)
	if err != nil {
		t.Fatalf("newServeHandler: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/email-password/sign-in", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/auth credential route status = %d, want 200", rec.Code)
	}
	if len(authula.hits) != 1 || authula.hits[0] != "/auth/email-password/sign-in" {
		t.Fatalf("Authula provider hits = %v, want credential route", authula.hits)
	}
	if len(aguiHits) != 0 {
		t.Fatalf("credential route leaked to AG-UI handler: %v", aguiHits)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/authx", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/authx status = %d, want 401", rec.Code)
	}
	if len(authula.hits) != 1 {
		t.Fatalf("/authx must not reach Authula provider; hits=%v", authula.hits)
	}
}

func TestServeWebuiAuthConfigPublic(t *testing.T) {
	const localID = "00000000-0000-0000-0000-000000000001"
	for _, tc := range []struct {
		name        string
		authula     *fakeAuthulaProvider
		wantAuthula bool
	}{
		{name: "passphrase provider"},
		{name: "authula provider", authula: &fakeAuthulaProvider{}, wantAuthula: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth := agui.AuthDeps{
				Secret:           "operator-secret",
				SecretConfigured: true,
				LocalIdentityID:  localID,
				Identities:       wiringIdentities{id: localID},
				LoginPath:        "/login",
				SessionValidator: func(*http.Request) (string, bool) { return "", false },
			}
			var aguiHits []string
			aguiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				aguiHits = append(aguiHits, r.URL.Path)
				w.WriteHeader(http.StatusOK)
			})
			handler, err := newServeHandler(aguiHandler, auth, tc.authula)
			if err != nil {
				t.Fatalf("newServeHandler: %v", err)
			}

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/auth/config", nil)
			handler.ServeHTTP(rec, req)
			raw := rec.Body.String()
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /api/auth/config = %d, want 200: %s", rec.Code, raw)
			}
			if len(aguiHits) != 0 {
				t.Fatalf("auth config leaked to AG-UI handler: %v", aguiHits)
			}
			if tc.authula != nil && len(tc.authula.hits) != 0 {
				t.Fatalf("auth config leaked to Authula provider: %v", tc.authula.hits)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", got)
			}

			if tc.wantAuthula {
				if !strings.Contains(raw, `"provider":"authula"`) {
					t.Fatalf("authula config body missing provider: %s", raw)
				}
				if !strings.Contains(raw, `"auth_base_path":"/auth"`) {
					t.Fatalf("authula config body missing auth_base_path: %s", raw)
				}
				if !strings.Contains(raw, `"csrf_header_name":"X-AUTHULA-CSRF-TOKEN"`) {
					t.Fatalf("authula config body missing csrf_header_name: %s", raw)
				}
				if rec.Header().Get("X-AUTHULA-CSRF-TOKEN") == "" {
					t.Fatal("authula config did not return the CSRF token response header")
				}
				var csrfCookie *http.Cookie
				for _, c := range rec.Result().Cookies() {
					if c.Name == "__Host-authula_csrf_token" {
						csrfCookie = c
						break
					}
				}
				if csrfCookie == nil {
					t.Fatal("authula config did not set the CSRF cookie")
				}
				if csrfCookie.Value == "" || csrfCookie.Value != rec.Header().Get("X-AUTHULA-CSRF-TOKEN") {
					t.Fatalf("csrf cookie/header mismatch: cookie=%q header=%q", csrfCookie.Value, rec.Header().Get("X-AUTHULA-CSRF-TOKEN"))
				}
				if !csrfCookie.Secure || !csrfCookie.HttpOnly || csrfCookie.Path != "/" || csrfCookie.SameSite != http.SameSiteStrictMode {
					t.Fatalf("csrf cookie attrs = Secure:%v HttpOnly:%v Path:%q SameSite:%v", csrfCookie.Secure, csrfCookie.HttpOnly, csrfCookie.Path, csrfCookie.SameSite)
				}
				return
			}

			if !strings.Contains(raw, `"provider":"passphrase"`) {
				t.Fatalf("passphrase config body missing provider: %s", raw)
			}
			if rec.Header().Get("X-AUTHULA-CSRF-TOKEN") != "" {
				t.Fatal("passphrase config must not return an Authula CSRF header")
			}
			if len(rec.Result().Cookies()) != 0 {
				t.Fatalf("passphrase config set unexpected cookies: %v", rec.Result().Cookies())
			}
		})
	}
}

// TestServeWebuiAuthWiring pins the WEB-03 wiring inside newServeHandler: with a
// configured secret, RequireAuth gates the whole origin (an API request with no cookie
// is 401'd before reaching the AG-UI handler), GET /healthz stays public, a valid
// session reaches the AG-UI handler, the POST /login route is public, and POST
// /agent/run flows through the capability gate. It uses a real session cookie minted
// from the wired signing key and a DB-free identityChecker.
func TestServeWebuiAuthWiring(t *testing.T) {
	const localID = "00000000-0000-0000-0000-000000000001"
	const secret = "operator-secret"
	key := sha256.Sum256([]byte(secret))
	auth := agui.AuthDeps{
		Secret:           secret,
		SigningKey:       key[:],
		SecretConfigured: true,
		LocalIdentityID:  localID,
		Identities:       wiringIdentities{id: localID},
		LoginPath:        "/login",
	}

	var aguiHits []string
	aguiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aguiHits = append(aguiHits, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"agui":true}`)
	})

	handler, err := newServeHandler(aguiHandler, auth, nil)
	if err != nil {
		t.Fatalf("newServeHandler: %v", err)
	}

	// A valid session cookie is minted by exercising the wired POST /login.
	loginRec := httptest.NewRecorder()
	form := strings.NewReader("passphrase=" + secret)
	loginReq := httptest.NewRequest(http.MethodPost, "/login", form)
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusSeeOther {
		t.Fatalf("POST /login (wired) = %d, want 303", loginRec.Code)
	}
	var sessionCookie *http.Cookie
	for _, c := range loginRec.Result().Cookies() {
		if strings.HasPrefix(c.Name, "__Host-") {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("wired POST /login did not mint a session cookie")
	}

	t.Run("no cookie API request -> 401, AG-UI never reached (gate active)", func(t *testing.T) {
		aguiHits = nil
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/threads/abc/messages", nil) // no html accept, no cookie
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("gated request status = %d, want 401", rec.Code)
		}
		if len(aguiHits) != 0 {
			t.Fatalf("gated request leaked to the AG-UI handler: %v", aguiHits)
		}
	})

	t.Run("GET /healthz stays public under the gate", func(t *testing.T) {
		aguiHits = nil
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if rec.Code != http.StatusOK || len(aguiHits) != 1 {
			t.Fatalf("/healthz code=%d hits=%v, want 200 + AG-UI reached", rec.Code, aguiHits)
		}
	})

	// CHAT-02 (Phase 25): /api/conversations inherits the whole-origin gate - a request
	// with no cookie is 401'd before reaching the AG-UI handler (no second auth check on
	// the new subtree; RequireAuth wrapping the whole mux is the sole gate).
	t.Run("no cookie /api/conversations -> 401 (RequireAuth inherited)", func(t *testing.T) {
		aguiHits = nil
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/conversations", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("/api/conversations status = %d, want 401 (gate inherited)", rec.Code)
		}
		if len(aguiHits) != 0 {
			t.Fatalf("unauthenticated /api/conversations leaked to the AG-UI handler: %v", aguiHits)
		}
	})

	t.Run("valid cookie /api/conversations reaches the AG-UI handler", func(t *testing.T) {
		aguiHits = nil
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/conversations", nil)
		req.AddCookie(sessionCookie)
		handler.ServeHTTP(rec, req)
		if len(aguiHits) != 1 || aguiHits[0] != "/api/conversations" {
			t.Fatalf("valid-session /api/conversations did not reach the AG-UI handler: hits=%v code=%d", aguiHits, rec.Code)
		}
	})

	t.Run("no cookie /api/assets -> 401 (RequireAuth inherited)", func(t *testing.T) {
		aguiHits = nil
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/assets", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("/api/assets status = %d, want 401 (gate inherited)", rec.Code)
		}
		if len(aguiHits) != 0 {
			t.Fatalf("unauthenticated /api/assets leaked to the AG-UI handler: %v", aguiHits)
		}
	})

	t.Run("valid cookie /api/assets reads reach the AG-UI handler", func(t *testing.T) {
		for _, route := range []string{"/api/assets", "/api/assets/asset-1"} {
			aguiHits = nil
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, route, nil)
			req.AddCookie(sessionCookie)
			handler.ServeHTTP(rec, req)
			if len(aguiHits) != 1 || aguiHits[0] != route {
				t.Fatalf("valid-session GET %s did not reach the AG-UI handler: hits=%v code=%d", route, aguiHits, rec.Code)
			}
		}
	})

	t.Run("asset mutations with a valid capable cookie pass the capability gate", func(t *testing.T) {
		for _, tc := range []struct {
			method string
			path   string
			body   string
		}{
			{http.MethodPost, "/api/assets/presign", `{}`},
			{http.MethodPost, "/api/assets/asset-1/finalize", `{}`},
			{http.MethodPost, "/api/assets/asset-1/promote", `{}`},
			{http.MethodPost, "/api/assets/asset-1/retry", `{}`},
			{http.MethodDelete, "/api/assets/asset-1", ``},
		} {
			aguiHits = nil
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.AddCookie(sessionCookie)
			handler.ServeHTTP(rec, req)
			if len(aguiHits) != 1 || aguiHits[0] != tc.path {
				t.Fatalf("%s %s did not flow through RequireCapability to the AG-UI handler: hits=%v code=%d", tc.method, tc.path, aguiHits, rec.Code)
			}
		}
	})

	t.Run("valid cookie reaches the AG-UI handler", func(t *testing.T) {
		aguiHits = nil
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/threads/abc/messages", nil)
		req.AddCookie(sessionCookie)
		handler.ServeHTTP(rec, req)
		if len(aguiHits) != 1 {
			t.Fatalf("valid-session request did not reach the AG-UI handler: hits=%v code=%d", aguiHits, rec.Code)
		}
	})

	t.Run("POST /agent/run with a valid cookie passes the capability gate", func(t *testing.T) {
		aguiHits = nil
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/agent/run", strings.NewReader("{}"))
		req.AddCookie(sessionCookie)
		handler.ServeHTTP(rec, req)
		if len(aguiHits) != 1 || aguiHits[0] != "/agent/run" {
			t.Fatalf("POST /agent/run did not flow through RequireCapability to the AG-UI handler: hits=%v code=%d", aguiHits, rec.Code)
		}
	})

	// APRV (Phase 25): the read inherits the whole-origin gate (no second auth check on
	// the new route) - a no-cookie GET /api/approvals is 401'd before the AG-UI handler.
	t.Run("no cookie /api/approvals -> 401 (RequireAuth inherited)", func(t *testing.T) {
		aguiHits = nil
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/approvals", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("/api/approvals status = %d, want 401 (gate inherited)", rec.Code)
		}
		if len(aguiHits) != 0 {
			t.Fatalf("unauthenticated /api/approvals leaked to the AG-UI handler: %v", aguiHits)
		}
	})

	// APRV-02: the mutating resolve flows through RequireCapability to the AG-UI handler
	// with a valid, capable cookie (the wired local identity holds agent.run).
	t.Run("POST /api/approvals/{token}/resolve with a valid capable cookie passes the gate", func(t *testing.T) {
		aguiHits = nil
		const token = "11111111-1111-1111-1111-111111111111"
		wantPath := "/api/approvals/" + token + "/resolve"
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, wantPath, strings.NewReader(`{"action":"accept"}`))
		req.AddCookie(sessionCookie)
		handler.ServeHTTP(rec, req)
		if len(aguiHits) != 1 || aguiHits[0] != wantPath {
			t.Fatalf("POST resolve did not flow through RequireCapability to the AG-UI handler: hits=%v code=%d", aguiHits, rec.Code)
		}
	})
}

// uncapableIdentities resolves the local identity (so RequireAuth binds a principal) but
// denies every capability - the negative half of the capability gate.
type uncapableIdentities struct{ id string }

func (u uncapableIdentities) GetIdentityByID(_ context.Context, id string) (agui.Identity, error) {
	if id != u.id {
		return agui.Identity{}, errWiringNotFound
	}
	return agui.Identity{ID: id, Name: "local", Kind: "user"}, nil
}

func (u uncapableIdentities) HasCapability(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

// TestServeWebuiApprovalsCapabilityGate pins the negative half of T-25-07 and the
// asset mutation gate: with a configured secret and a principal that holds NO
// capability, mutating routes are 403'd by RequireCapability and never reach the
// AG-UI handler, even with a valid session cookie. This proves those routes are
// genuinely gated on the capability, not merely on authentication.
func TestServeWebuiApprovalsCapabilityGate(t *testing.T) {
	const localID = "00000000-0000-0000-0000-000000000001"
	const secret = "operator-secret"
	key := sha256.Sum256([]byte(secret))
	auth := agui.AuthDeps{
		Secret:           secret,
		SigningKey:       key[:],
		SecretConfigured: true,
		LocalIdentityID:  localID,
		Identities:       uncapableIdentities{id: localID},
		LoginPath:        "/login",
	}

	var aguiHits []string
	aguiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aguiHits = append(aguiHits, r.URL.Path)
		_, _ = io.WriteString(w, `{"agui":true}`)
	})

	handler, err := newServeHandler(aguiHandler, auth, nil)
	if err != nil {
		t.Fatalf("newServeHandler: %v", err)
	}

	loginRec := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("passphrase="+secret))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusSeeOther {
		t.Fatalf("POST /login = %d, want 303", loginRec.Code)
	}
	var sessionCookie *http.Cookie
	for _, c := range loginRec.Result().Cookies() {
		if strings.HasPrefix(c.Name, "__Host-") {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("POST /login did not mint a session cookie")
	}

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/approvals/11111111-1111-1111-1111-111111111111/resolve", `{"action":"accept"}`},
		{http.MethodPost, "/agent/run", `{}`},
		{http.MethodPost, "/api/assets/presign", `{}`},
		{http.MethodPost, "/api/assets/asset-1/finalize", `{}`},
		{http.MethodPost, "/api/assets/asset-1/promote", `{}`},
		{http.MethodPost, "/api/assets/asset-1/retry", `{}`},
		{http.MethodDelete, "/api/assets/asset-1", ``},
	} {
		aguiHits = nil
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.AddCookie(sessionCookie)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s with an uncapable principal = %d, want 403", tc.method, tc.path, rec.Code)
		}
		if len(aguiHits) != 0 {
			t.Fatalf("%s %s reached the AG-UI handler despite a denied capability: %v", tc.method, tc.path, aguiHits)
		}
	}
}
