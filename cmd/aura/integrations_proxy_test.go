package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// capturedReq records what the fake sidecar received so the proxy's rewrite + auth
// injection can be asserted from the far side of the forward.
type capturedReq struct {
	method, path, query, adminToken, body string
}

func startFakeSidecar(t *testing.T, captured *capturedReq) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*captured = capturedReq{
			method:     r.Method,
			path:       r.URL.Path,
			query:      r.URL.RawQuery,
			adminToken: r.Header.Get("X-Admin-Token"),
			body:       string(body),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// pointEnvAtSidecar makes mcpmanager.PIMSidecarBaseURL() resolve to the fake server's
// loopback port (forces the non-container branch).
func pointEnvAtSidecar(t *testing.T, srv *httptest.Server) {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse fake sidecar URL: %v", err)
	}
	t.Setenv("AURA_IN_CONTAINER", "")
	t.Setenv("AURA_PIM_MCP_PORT", u.Port())
}

// reapProxyIdleConns returns the ReverseProxy's DefaultTransport keep-alive conns to
// the fake sidecar so the cmd/aura goleak TestMain stays clean.
func reapProxyIdleConns(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		if tr, ok := http.DefaultTransport.(*http.Transport); ok {
			tr.CloseIdleConnections()
		}
		time.Sleep(100 * time.Millisecond)
	})
}

func TestIntegrationsProxyForwardsCalendarAdminWithToken(t *testing.T) {
	reapProxyIdleConns(t)
	var got capturedReq
	sidecar := startFakeSidecar(t, &got)
	pointEnvAtSidecar(t, sidecar)
	t.Setenv("AURA_PIM_MCP_ADMIN_TOKEN", "secret-tok")

	ts := httptest.NewServer(newIntegrationsProxy())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/integrations/calendar/accounts")
	if err != nil {
		t.Fatalf("GET via proxy: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), `"ok":true`) {
		t.Fatalf("proxy did not return the sidecar body: %s", raw)
	}
	if got.method != http.MethodGet || got.path != "/admin/accounts" {
		t.Fatalf("sidecar saw %s %q, want GET /admin/accounts", got.method, got.path)
	}
	if got.adminToken != "secret-tok" {
		t.Fatalf("sidecar X-Admin-Token = %q, want injected secret-tok (cockpit never holds it)", got.adminToken)
	}
}

func TestIntegrationsProxyPreservesMethodBodyQueryAndDefaultToken(t *testing.T) {
	reapProxyIdleConns(t)
	var got capturedReq
	sidecar := startFakeSidecar(t, &got)
	pointEnvAtSidecar(t, sidecar)
	t.Setenv("AURA_PIM_MCP_ADMIN_TOKEN", "") // force the compose-matching default

	ts := httptest.NewServer(newIntegrationsProxy())
	t.Cleanup(ts.Close)

	resp, err := http.Post(
		ts.URL+"/api/integrations/calendar/auth/outlook-personal/start?wait=1",
		"application/json",
		strings.NewReader(`{"x":1}`),
	)
	if err != nil {
		t.Fatalf("POST via proxy: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)

	if got.method != http.MethodPost || got.path != "/admin/auth/outlook-personal/start" {
		t.Fatalf("sidecar saw %s %q, want POST /admin/auth/outlook-personal/start", got.method, got.path)
	}
	if got.query != "wait=1" {
		t.Fatalf("sidecar query = %q, want wait=1 (preserved)", got.query)
	}
	if got.body != `{"x":1}` {
		t.Fatalf("sidecar body = %q, want the forwarded JSON", got.body)
	}
	if got.adminToken != pimAdminTokenDefault {
		t.Fatalf("sidecar X-Admin-Token = %q, want default %q", got.adminToken, pimAdminTokenDefault)
	}
}

func TestIntegrationsProxyForwardsWhatsAppNoToken(t *testing.T) {
	reapProxyIdleConns(t)
	var got capturedReq
	bridge := startFakeSidecar(t, &got)
	u, err := url.Parse(bridge.URL)
	if err != nil {
		t.Fatalf("parse fake bridge URL: %v", err)
	}
	t.Setenv("AURA_IN_CONTAINER", "")
	t.Setenv("AURA_WHATSAPP_BRIDGE_PORT", u.Port())

	ts := httptest.NewServer(newIntegrationsProxy())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/integrations/whatsapp/status")
	if err != nil {
		t.Fatalf("GET via proxy: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)

	if got.method != http.MethodGet || got.path != "/api/status" {
		t.Fatalf("bridge saw %s %q, want GET /api/status", got.method, got.path)
	}
	if got.adminToken != "" {
		t.Fatalf("whatsapp leg injected a token (%q); the bridge REST is unauthenticated", got.adminToken)
	}
}

func TestIntegrationsProxyUnknownIntegrationIs404(t *testing.T) {
	var got capturedReq
	sidecar := startFakeSidecar(t, &got)
	pointEnvAtSidecar(t, sidecar)

	ts := httptest.NewServer(newIntegrationsProxy())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/integrations/bogus/x")
	if err != nil {
		t.Fatalf("GET unknown integration: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for an unwired integration", resp.StatusCode)
	}
	if got.method != "" {
		t.Fatalf("unknown integration leaked to a sidecar: %+v", got)
	}
}

func TestIntegrationsProxySidecarUnreachableIs502(t *testing.T) {
	reapProxyIdleConns(t)
	// Grab a port, then free it so the forward target refuses the connection.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	u, err := url.Parse(dead.URL)
	if err != nil {
		t.Fatalf("parse dead URL: %v", err)
	}
	t.Setenv("AURA_IN_CONTAINER", "")
	t.Setenv("AURA_PIM_MCP_PORT", u.Port())
	dead.Close() // nothing listens on that port now

	ts := httptest.NewServer(newIntegrationsProxy())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/integrations/calendar/accounts")
	if err != nil {
		t.Fatalf("GET via proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 when the sidecar is unreachable", resp.StatusCode)
	}
}
