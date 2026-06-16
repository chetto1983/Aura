package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestServeWebui pins FND-02: newServeHandler mounts the embedded operator shell at
// "/" additively while the AG-UI route prefixes keep priority. Stdlib testing +
// httptest only (no testify — the agui/webui HTTP surface convention).
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

	t.Run("GET bogus asset -> 404 (no SPA-fallback, Phase 24)", func(t *testing.T) {
		aguiHits = nil
		resp, err := http.Get(srv.URL + "/no-such-asset.js")
		if err != nil {
			t.Fatalf("GET missing: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (a missing asset under / is a plain 404, no index.html fallback)", resp.StatusCode)
		}
		if len(aguiHits) != 0 {
			t.Fatalf("a static-tree miss leaked to the AG-UI handler: %v", aguiHits)
		}
	})
}
