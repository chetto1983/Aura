package webui

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// excludedPrefixes mirrors the segment-level exclusion set the caller
// (cmd/aura/serve_webui.go) derives and passes in: the AG-UI route segments,
// the health endpoints, and the forward-compat /api/ carve-out (which also
// subsumes /api/integrations/). Kept here only so the unit tests can exercise
// the 404-vs-fallback split without importing cmd/aura.
var excludedPrefixes = []string{
	"/healthz",
	"/readyz",
	"/debug/vars",
	"/metrics",
	"/agent/",
	"/threads/",
	"/api/",
}

// TestHandler pins SC2 for the embedded host: //go:embed all:dist -> Handler()
// serves index.html at "/" (200, text/html, with the theme-before-paint + brand
// markers), and a real embedded asset resolves. Stdlib testing + httptest only —
// the agui/webui HTTP surface does not use testify.
func TestHandler(t *testing.T) {
	h, err := Handler(excludedPrefixes)
	if err != nil {
		t.Fatalf("Handler(): %v", err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	t.Run("GET / -> 200 text/html branded shell", func(t *testing.T) {
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
		// theme-before-paint contract: the root must carry the dark theme on the
		// first response HTML so there is no flash (SC2).
		if !strings.Contains(body, `data-theme`) {
			t.Fatalf("index.html missing data-theme marker: %s", body)
		}
		// brand contract (SC4): the shell identifies as Aura.
		if !strings.Contains(strings.ToLower(body), "aura") {
			t.Fatalf("index.html missing brand: %s", body)
		}
	})
}

// indexMarker is a substring unique to the SPA shell (index.html). A real API 404
// MUST NOT contain it, or the shell is leaking HTML to an API client (WEB-01/SC1).
const indexMarker = `<div id="root"`

// TestSPAFallback pins WEB-01 / SC1: the catch-all serves the SPA shell for unknown
// client routes (deep links resolve to index.html) but returns a real 404 for any
// excluded API/agent/health prefix — the shell is never leaked to an API client.
func TestSPAFallback(t *testing.T) {
	h, err := Handler(excludedPrefixes)
	if err != nil {
		t.Fatalf("Handler(): %v", err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	get := func(t *testing.T, path string) (*http.Response, string) {
		t.Helper()
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return resp, string(raw)
	}

	t.Run("unknown client route -> index.html (200)", func(t *testing.T) {
		for _, route := range []string{"/investigations/42", "/some/client/route", "/login"} {
			resp, body := get(t, route)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("%s: status = %d, want 200 (deep link -> SPA shell)", route, resp.StatusCode)
			}
			if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Fatalf("%s: Content-Type = %q, want text/html", route, ct)
			}
			if !strings.Contains(body, indexMarker) {
				t.Fatalf("%s: did not serve the SPA shell (no %q): %s", route, indexMarker, body)
			}
			// theme-before-paint must survive the fallback path too.
			if !strings.Contains(body, "data-theme") {
				t.Fatalf("%s: SPA shell missing data-theme marker", route)
			}
		}
	})

	t.Run("excluded prefix -> real 404 (never the SPA shell)", func(t *testing.T) {
		for _, route := range []string{
			"/api/nope",
			"/agent/typo",
			"/threads/bogus",
			"/healthz-typo-but-prefix",
			"/readyz",
			"/metrics",
			"/debug/vars",
			"/api/integrations/x",
		} {
			resp, body := get(t, route)
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("%s: status = %d, want 404 (excluded prefix is a real API error)", route, resp.StatusCode)
			}
			if strings.Contains(body, indexMarker) {
				t.Fatalf("%s: 404 body leaked the SPA shell (%q): %s", route, indexMarker, body)
			}
		}
	})

	t.Run("real embedded asset -> 200 served as-is", func(t *testing.T) {
		// Discover the hashed JS bundle from the embedded FS rather than pinning a
		// content-hash filename — Vite re-hashes every rebuild, so a literal name
		// goes stale on each `npm run build` (it did when 24-04 rebuilt dist).
		asset := firstDistJSAsset(t)
		resp, body := get(t, "/assets/"+asset)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("asset %s: status = %d, want 200", asset, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "javascript") {
			t.Fatalf("asset %s Content-Type = %q, want javascript", asset, ct)
		}
		if strings.Contains(body, indexMarker) {
			t.Fatalf("asset request fell back to index.html instead of serving the JS bundle")
		}
	})

	t.Run("web manifest carries explicit utf-8 content type", func(t *testing.T) {
		resp, body := get(t, "/manifest.webmanifest")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("manifest status = %d, want 200: %s", resp.StatusCode, body)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/manifest+json; charset=utf-8" {
			t.Fatalf("manifest Content-Type = %q, want application/manifest+json; charset=utf-8", ct)
		}
	})

	t.Run("missing asset under a client path -> index.html (200)", func(t *testing.T) {
		resp, body := get(t, "/assets/does-not-exist.js")
		// A bogus path that is NOT an excluded prefix is treated as a client route:
		// it falls back to the SPA shell (React Router resolves / 404s in-app).
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("missing asset: status = %d, want 200 (client-route fallback)", resp.StatusCode)
		}
		if !strings.Contains(body, indexMarker) {
			t.Fatalf("missing asset did not fall back to the SPA shell: %s", body)
		}
	})
}

// firstDistJSAsset returns the name of a hashed JS bundle under dist/assets in the
// embedded tree (e.g. "index-CNlksIPz.js"). The content hash changes every Vite
// rebuild, so the asset-serving test discovers the real name instead of pinning a
// literal that goes stale on each `npm run build`.
func firstDistJSAsset(t *testing.T) string {
	t.Helper()
	sub, err := Sub()
	if err != nil {
		t.Fatalf("Sub(): %v", err)
	}
	entries, err := fs.ReadDir(sub, "assets")
	if err != nil {
		t.Fatalf("read embedded dist/assets: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".js") {
			return e.Name()
		}
	}
	t.Fatalf("no .js bundle found under embedded dist/assets")
	return ""
}

// TestIsPublicAsset pins the D-03 web-auth public-asset predicate: a real hashed
// /assets/* bundle is public (the login page needs it pre-session), the SPA shell
// (index.html / "/") is NOT (it stays gated), and an unknown client route is NOT (it
// would fall back to the shell). A single shared bundle means the asset set is public.
func TestIsPublicAsset(t *testing.T) {
	asset := firstDistJSAsset(t)
	cases := []struct {
		path string
		want bool
	}{
		{"/assets/" + asset, true},    // real hashed bundle — public
		{"assets/" + asset, true},     // leading slash is optional
		{"/", false},                  // SPA shell root — gated
		{"/index.html", false},        // the shell itself is not an asset
		{"index.html", false},         // (same, no leading slash)
		{"/threads/x", false},         // an API/client route is not a real asset
		{"/does-not-exist.js", false}, // missing file — falls back to the shell, gated
	}
	for _, tc := range cases {
		if got := IsPublicAsset(tc.path); got != tc.want {
			t.Errorf("IsPublicAsset(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestSub asserts the embedded tree is rooted at dist/ so index.html resolves
// from the FS root (the reason Handler serves it at "/").
func TestSub(t *testing.T) {
	sub, err := Sub()
	if err != nil {
		t.Fatalf("Sub(): %v", err)
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		t.Fatalf("index.html not at embedded root: %v", err)
	}
}
