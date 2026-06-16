package webui

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandler pins SC2 for the Phase-23 static host: //go:embed all:dist ->
// Handler() serves the placeholder index.html at "/" (200, text/html, with the
// theme-before-paint + brand markers), and a missing path 404s. Stdlib testing +
// httptest only — the agui/webui HTTP surface does not use testify.
func TestHandler(t *testing.T) {
	h, err := Handler()
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

	t.Run("GET /no-such-asset -> 404", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/no-such-asset.js")
		if err != nil {
			t.Fatalf("GET missing: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	})
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
