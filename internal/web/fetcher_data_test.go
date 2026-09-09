//go:build !web_integration

package web

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

// TestFetchData_Success: a public host serving application/json streams back the
// bytes and the canonical media type through the SSRF-hardened transport.
func TestFetchData_Success(t *testing.T) {
	payload := []byte(`{"price":78511.27}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()
	_, port := hostPort(t, srv.URL)

	c := fetchClient(t, map[string][]netip.Addr{"api.test": {publicIP}})
	body, ct, err := c.FetchData(context.Background(), "conv1", "http://api.test:"+port+"/v1/price")
	if err != nil {
		t.Fatalf("FetchData: %v", err)
	}
	if ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("body mismatch: got %q", body)
	}
}

// TestFetchData_BlocksPrivateAndMetadata: the data proxy is gated by the SAME
// validateAndPin guard as web_fetch — a host resolving to a private, loopback or
// cloud-metadata address is refused before the dial, with a sanitized WebError.
func TestFetchData_BlocksPrivateAndMetadata(t *testing.T) {
	cases := map[string]netip.Addr{
		"metadata": netip.MustParseAddr("169.254.169.254"),
		"loopback": netip.MustParseAddr("127.0.0.1"),
		"private":  netip.MustParseAddr("10.0.0.5"),
	}
	for name, ip := range cases {
		t.Run(name, func(t *testing.T) {
			c := fetchClient(t, map[string][]netip.Addr{"evil.test": {ip}})
			_, _, err := c.FetchData(context.Background(), "conv1", "http://evil.test:80/x.json")
			we, ok := AsWebError(err)
			if !ok {
				t.Fatalf("want WebError, got %T: %v", err, err)
			}
			if we.Code != CodeBlockedURL {
				t.Fatalf("want blocked_url, got %s/%s", we.Code, we.Reason)
			}
		})
	}
}

// TestFetchData_RejectsHTML: text/html is deliberately absent from the data
// allowlist. The proxy is same-origin with the cockpit, so relaying a document
// would hand an attacker-controlled page Aura's own origin.
func TestFetchData_RejectsHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<script>alert(1)</script>"))
	}))
	defer srv.Close()
	_, port := hostPort(t, srv.URL)

	c := fetchClient(t, map[string][]netip.Addr{"api.test": {publicIP}})
	_, _, err := c.FetchData(context.Background(), "conv1", "http://api.test:"+port+"/page")
	we, ok := AsWebError(err)
	if !ok {
		t.Fatalf("want WebError, got %T: %v", err, err)
	}
	if we.Code != CodeUnsupportedContent {
		t.Fatalf("want unsupported_content, got %s", we.Code)
	}
}

// TestFetchData_RedirectNotFollowed: a 3xx is refused outright rather than chased,
// because a redirect target can rebind to a private host between hops.
func TestFetchData_RedirectNotFollowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, "http://127.0.0.1/secret", http.StatusFound)
	}))
	defer srv.Close()
	_, port := hostPort(t, srv.URL)

	c := fetchClient(t, map[string][]netip.Addr{"api.test": {publicIP}})
	_, _, err := c.FetchData(context.Background(), "conv1", "http://api.test:"+port+"/r")
	we, ok := AsWebError(err)
	if !ok {
		t.Fatalf("want WebError, got %T: %v", err, err)
	}
	if we.Reason != "redirect_not_followed" {
		t.Fatalf("want redirect_not_followed, got %s/%s", we.Code, we.Reason)
	}
}

// TestFetchData_SizeCap: a body past the configured ceiling is refused rather than
// truncated, so the cockpit never renders a half-document as if it were whole.
func TestFetchData_SizeCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[" + strings.Repeat("1,", 1<<20) + "1]"))
	}))
	defer srv.Close()
	_, port := hostPort(t, srv.URL)

	c := fetchClient(t, map[string][]netip.Addr{"api.test": {publicIP}})
	c.cfg.WebFetchMaxBodyBytes = 1024
	_, _, err := c.FetchData(context.Background(), "conv1", "http://api.test:"+port+"/big")
	we, ok := AsWebError(err)
	if !ok {
		t.Fatalf("want WebError, got %T: %v", err, err)
	}
	if we.Code != CodeResponseTooLarge {
		t.Fatalf("want response_too_large, got %s", we.Code)
	}
}

// TestFetchData_RejectsNonHTTPScheme: nothing but http(s) reaches the dialer. A
// hostless URL (file:///etc/passwd) is refused earlier still, by the empty-host
// check, so both refusal paths are pinned here.
func TestFetchData_RejectsNonHTTPScheme(t *testing.T) {
	cases := map[string]struct {
		rawURL string
		want   string
	}{
		"ftp_with_host": {"ftp://api.test/x.json", CodeUnsupportedScheme},
		"hostless_file": {"file:///etc/passwd", CodeHTTPError},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := fetchClient(t, map[string][]netip.Addr{})
			_, _, err := c.FetchData(context.Background(), "conv1", tc.rawURL)
			we, ok := AsWebError(err)
			if !ok {
				t.Fatalf("want WebError, got %T: %v", err, err)
			}
			if we.Code != tc.want {
				t.Fatalf("want %s, got %s/%s", tc.want, we.Code, we.Reason)
			}
		})
	}
}
