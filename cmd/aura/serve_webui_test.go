package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

	handler, err := newServeHandler(aguiHandler)
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

	// Precedence unbroken: /api/integrations/ still reaches the integrations proxy
	// (its own 404 body, NOT the SPA fallback and NOT the /api/ carve-out swallowing it).
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
