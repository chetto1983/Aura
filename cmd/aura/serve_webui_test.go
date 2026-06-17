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

// TestServeWebui pins FND-02 + WEB-01: newServeHandler mounts the embedded operator
// SPA at "/" additively while the AG-UI route prefixes and the integrations proxy
// keep priority, and the "/" catch-all is now an SPA-fallback (not a bare static
// tree). Stdlib testing + httptest only (no testify — the agui/webui HTTP surface
// convention).
//
// A FAKE AG-UI handler stands in for agui.Server.Mux so the precedence assertion is
// genuine: it records every path it receives and answers a sentinel body. If the
// parent mux gives "/" priority over a registered AG-UI prefix, the AG-UI handler
// would never be reached and the assertion fails — proving the Go 1.22 ServeMux
// longest-pattern-wins precedence, not just that both handlers exist.
func TestServeWebui(t *testing.T) {
	var aguiHits []string
	aguiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aguiHits = append(aguiHits, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"agui":true}`)
	})

	// A zero AuthDeps has SecretConfigured=false, so agui.RequireAuth is a no-op
	// pass-through — this test pins the loopback (unauthenticated) routing behavior,
	// exactly as the daemon serves when AURA_WEB_AUTH_SECRET is unset. The auth-active
	// gating is covered by internal/agui auth_test.go (RequireAuth) and the wiring is
	// pinned below by TestServeWebuiAuthWiring.
	handler, err := newServeHandler(aguiHandler, agui.AuthDeps{})
	if err != nil {
		t.Fatalf("newServeHandler: %v", err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	const indexMarker = `<div id="root"`

	t.Run("GET / -> 200 text/html embedded shell", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/")
		if err != nil {
			t.Fatalf("GET /: %v", err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, raw)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("Content-Type = %q, want text/html...", ct)
		}
		body := string(raw)
		// theme-before-paint contract: the root carries the dark theme on the first
		// response HTML so there is no flash (SC2).
		if !strings.Contains(body, "data-theme") {
			t.Fatalf("served shell missing data-theme marker: %s", body)
		}
		// brand contract (SC4): the shell identifies as Aura.
		if !strings.Contains(strings.ToLower(body), "aura") {
			t.Fatalf("served shell missing brand: %s", body)
		}
	})

	t.Run("GET /healthz -> AG-UI handler keeps priority", func(t *testing.T) {
		aguiHits = nil
		resp, err := http.Get(srv.URL + "/healthz")
		if err != nil {
			t.Fatalf("GET /healthz: %v", err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, raw)
		}
		if !strings.Contains(string(raw), `"agui":true`) {
			t.Fatalf("/healthz did not route to the AG-UI handler: %s", raw)
		}
		if len(aguiHits) != 1 || aguiHits[0] != "/healthz" {
			t.Fatalf("AG-UI handler hits = %v, want [/healthz] (precedence over the / catch-all)", aguiHits)
		}
	})

	t.Run("GET /threads/{id}/messages -> AG-UI subtree keeps priority", func(t *testing.T) {
		aguiHits = nil
		resp, err := http.Get(srv.URL + "/threads/abc/messages")
		if err != nil {
			t.Fatalf("GET /threads/...: %v", err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(raw), `"agui":true`) {
			t.Fatalf("/threads/ subtree did not route to the AG-UI handler: %s", raw)
		}
		if len(aguiHits) != 1 {
			t.Fatalf("AG-UI handler hits = %v, want 1 (/threads/ subtree precedence)", aguiHits)
		}
	})

	// WEB-01 contract change (sanctioned test rewrite, not a test-massage): under the
	// Phase-23 placeholder a missing path under "/" 404'd; WEB-01/SC1 now serves the
	// SPA shell for an unknown CLIENT route so React Router resolves deep links.
	t.Run("GET unknown client route -> SPA-fallback index.html (200, WEB-01)", func(t *testing.T) {
		aguiHits = nil
		resp, err := http.Get(srv.URL + "/investigations/42")
		if err != nil {
			t.Fatalf("GET client route: %v", err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200 (deep client link -> SPA shell): %s", resp.StatusCode, raw)
		}
		if !strings.Contains(string(raw), indexMarker) {
			t.Fatalf("unknown client route did not serve the SPA shell (%q): %s", indexMarker, raw)
		}
		if len(aguiHits) != 0 {
			t.Fatalf("a client-route fallback leaked to the AG-UI handler: %v", aguiHits)
		}
	})

	// WEB-01/SC1: an excluded API/agent prefix returns a real 404, NEVER the SPA shell.
	t.Run("GET /api/nope + /agent/typo -> real 404 (never the SPA shell)", func(t *testing.T) {
		for _, route := range []string{"/api/nope", "/agent/typo"} {
			aguiHits = nil
			resp, err := http.Get(srv.URL + route)
			if err != nil {
				t.Fatalf("GET %s: %v", route, err)
			}
			raw, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("%s: status = %d, want 404 (excluded prefix is a real API error): %s", route, resp.StatusCode, raw)
			}
			if strings.Contains(string(raw), indexMarker) {
				t.Fatalf("%s: 404 leaked the SPA shell (%q): %s", route, indexMarker, raw)
			}
		}
	})

	// CHAT-02 (Phase 25): the new /api/conversations/ subtree reaches the AG-UI handler
	// — both the exact list GET (no trailing slash) and a {id} subtree path — proving the
	// specific mount (NOT a bare /api/) dispatches to aguiHandler over the "/" catch-all.
	t.Run("GET /api/conversations* -> AG-UI handler (CHAT-02 mount)", func(t *testing.T) {
		for _, route := range []string{"/api/conversations", "/api/conversations/abc"} {
			aguiHits = nil
			resp, err := http.Get(srv.URL + route)
			if err != nil {
				t.Fatalf("GET %s: %v", route, err)
			}
			raw, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if len(aguiHits) != 1 || aguiHits[0] != route {
				t.Fatalf("%s did not route to the AG-UI handler: hits=%v body=%s", route, aguiHits, raw)
			}
			if strings.Contains(string(raw), indexMarker) {
				t.Fatalf("%s leaked the SPA shell instead of reaching the AG-UI handler", route)
			}
		}
	})

	// APRV-01/02 (Phase 25): the approval-center routes reach the AG-UI handler — the
	// exact read GET /api/approvals AND the POST /api/approvals/{token}/resolve — proving
	// the specific mounts dispatch to aguiHandler over the "/" catch-all (NOT a bare
	// /api/). With no secret configured RequireCapability is a pass-through, so the resolve
	// POST flows straight to the handler here; the gate-active path is TestServeWebuiAuthWiring.
	t.Run("/api/approvals + resolve -> AG-UI handler (APRV mount)", func(t *testing.T) {
		aguiHits = nil
		resp, err := http.Get(srv.URL + "/api/approvals")
		if err != nil {
			t.Fatalf("GET /api/approvals: %v", err)
		}
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if len(aguiHits) != 1 || aguiHits[0] != "/api/approvals" {
			t.Fatalf("GET /api/approvals did not route to the AG-UI handler: hits=%v body=%s", aguiHits, raw)
		}
		if strings.Contains(string(raw), indexMarker) {
			t.Fatalf("/api/approvals leaked the SPA shell instead of reaching the AG-UI handler")
		}

		aguiHits = nil
		const token = "11111111-1111-1111-1111-111111111111"
		wantPath := "/api/approvals/" + token + "/resolve"
		presp, err := http.Post(srv.URL+wantPath, "application/json", strings.NewReader(`{"action":"accept"}`))
		if err != nil {
			t.Fatalf("POST resolve: %v", err)
		}
		praw, _ := io.ReadAll(presp.Body)
		_ = presp.Body.Close()
		if len(aguiHits) != 1 || aguiHits[0] != wantPath {
			t.Fatalf("POST resolve did not route through to the AG-UI handler: hits=%v body=%s", aguiHits, praw)
		}
	})

	// Precedence unbroken: /api/integrations/ still reaches the integrations proxy
	// (its own 404 body, NOT the SPA fallback and NOT the /api/ carve-out swallowing it).
	// Asserted AFTER the /api/conversations/ AND /api/approvals mounts above to prove
	// neither new route shadows the integrations proxy (T-25-05).
	t.Run("GET /api/integrations/<unknown> -> integrations proxy (precedence)", func(t *testing.T) {
		aguiHits = nil
		resp, err := http.Get(srv.URL + "/api/integrations/does-not-exist")
		if err != nil {
			t.Fatalf("GET integrations: %v", err)
		}
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 from the integrations proxy", resp.StatusCode)
		}
		// The integrations proxy 404s an unknown name with its own distinctive body —
		// proves the request reached the proxy, not the /api/ fallback exclusion.
		if !strings.Contains(string(raw), "unknown integration") {
			t.Fatalf("/api/integrations/ did not reach the integrations proxy (body=%q)", raw)
		}
		if strings.Contains(string(raw), indexMarker) {
			t.Fatalf("/api/integrations/ leaked the SPA shell instead of reaching the proxy")
		}
	})
}

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

	handler, err := newServeHandler(aguiHandler, auth)
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

	// CHAT-02 (Phase 25): /api/conversations inherits the whole-origin gate — a request
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
	// the new route) — a no-cookie GET /api/approvals is 401'd before the AG-UI handler.
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
// denies every capability — the negative half of the capability gate.
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

// TestServeWebuiApprovalsCapabilityGate pins the negative half of T-25-07: with a
// configured secret and a principal that holds NO capability, the mutating resolve
// (and POST /agent/run) are 403'd by RequireCapability and never reach the AG-UI handler,
// even with a valid session cookie. This proves the resolve route is genuinely gated on
// the capability, not merely on authentication.
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

	handler, err := newServeHandler(aguiHandler, auth)
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

	for _, route := range []string{"/api/approvals/11111111-1111-1111-1111-111111111111/resolve", "/agent/run"} {
		aguiHits = nil
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, route, strings.NewReader(`{"action":"accept"}`))
		req.AddCookie(sessionCookie)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("POST %s with an uncapable principal = %d, want 403", route, rec.Code)
		}
		if len(aguiHits) != 0 {
			t.Fatalf("POST %s reached the AG-UI handler despite a denied capability: %v", route, aguiHits)
		}
	}
}
