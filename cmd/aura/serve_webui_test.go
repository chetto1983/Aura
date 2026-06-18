package main

import (
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
// tree). Stdlib testing + httptest only (no testify).
func TestServeWebui(t *testing.T) {
	var aguiHits []string
	aguiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aguiHits = append(aguiHits, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"agui":true}`)
	})

	// A zero AuthDeps has SecretConfigured=false, so agui.RequireAuth is a no-op
	// pass-through. Auth-active wiring is covered in serve_webui_auth_test.go.
	handler, err := newServeHandler(aguiHandler, agui.AuthDeps{}, nil)
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
		if !strings.Contains(body, "data-theme") {
			t.Fatalf("served shell missing data-theme marker: %s", body)
		}
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

	t.Run("GET /api/auth/config -> json with explicit utf-8 charset", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/auth/config")
		if err != nil {
			t.Fatalf("GET /api/auth/config: %v", err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, raw)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
			t.Fatalf("Content-Type = %q, want application/json; charset=utf-8", ct)
		}
	})

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

	t.Run("branch routes -> AG-UI handler (D-09 mount, no new bare /api/)", func(t *testing.T) {
		const cid = "/api/conversations/22222222-2222-2222-2222-222222222222"
		aguiHits = nil
		resp, err := http.Get(srv.URL + cid + "/branches")
		if err != nil {
			t.Fatalf("GET branches: %v", err)
		}
		_ = resp.Body.Close()
		if len(aguiHits) != 1 || aguiHits[0] != cid+"/branches" {
			t.Fatalf("GET branches did not route to the AG-UI handler: hits=%v", aguiHits)
		}
		for _, route := range []string{cid + "/edit", cid + "/branches/3/select"} {
			aguiHits = nil
			presp, err := http.Post(srv.URL+route, "application/json", strings.NewReader(`{"diverge_seq":2}`))
			if err != nil {
				t.Fatalf("POST %s: %v", route, err)
			}
			_ = presp.Body.Close()
			if len(aguiHits) != 1 || aguiHits[0] != route {
				t.Fatalf("POST %s did not route through to the AG-UI handler: hits=%v", route, aguiHits)
			}
		}
	})

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

	t.Run("/api/assets + mutations -> AG-UI handler (asset mount)", func(t *testing.T) {
		for _, route := range []string{"/api/assets", "/api/assets/asset-1"} {
			aguiHits = nil
			resp, err := http.Get(srv.URL + route)
			if err != nil {
				t.Fatalf("GET %s: %v", route, err)
			}
			raw, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if len(aguiHits) != 1 || aguiHits[0] != route {
				t.Fatalf("GET %s did not route to the AG-UI handler: hits=%v body=%s", route, aguiHits, raw)
			}
			if strings.Contains(string(raw), indexMarker) {
				t.Fatalf("%s leaked the SPA shell instead of reaching the AG-UI handler", route)
			}
		}

		for _, route := range []string{
			"/api/assets/presign",
			"/api/assets/asset-1/finalize",
			"/api/assets/asset-1/promote",
			"/api/assets/asset-1/retry",
		} {
			aguiHits = nil
			presp, err := http.Post(srv.URL+route, "application/json", strings.NewReader(`{}`))
			if err != nil {
				t.Fatalf("POST %s: %v", route, err)
			}
			praw, _ := io.ReadAll(presp.Body)
			_ = presp.Body.Close()
			if len(aguiHits) != 1 || aguiHits[0] != route {
				t.Fatalf("POST %s did not route to the AG-UI handler: hits=%v body=%s", route, aguiHits, praw)
			}
		}

		aguiHits = nil
		req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/assets/asset-1", nil)
		if err != nil {
			t.Fatalf("new DELETE: %v", err)
		}
		dresp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("DELETE asset: %v", err)
		}
		draw, _ := io.ReadAll(dresp.Body)
		_ = dresp.Body.Close()
		if len(aguiHits) != 1 || aguiHits[0] != "/api/assets/asset-1" {
			t.Fatalf("DELETE asset did not route to the AG-UI handler: hits=%v body=%s", aguiHits, draw)
		}
	})

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
		if !strings.Contains(string(raw), "unknown integration") {
			t.Fatalf("/api/integrations/ did not reach the integrations proxy (body=%q)", raw)
		}
		if strings.Contains(string(raw), indexMarker) {
			t.Fatalf("/api/integrations/ leaked the SPA shell instead of reaching the proxy")
		}
	})
}
