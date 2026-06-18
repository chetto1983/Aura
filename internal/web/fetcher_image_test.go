//go:build !web_integration

package web

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

// pngBytes is a tiny valid PNG-magic body (the proxy gates on the Content-Type
// header, not the magic, but a realistic body exercises the size-cap path).
var pngBytes = []byte("\x89PNG\r\n\x1a\nfake-image-bytes")

// TestFetchImage_Success: a public host serving image/png streams back the bytes +
// the canonical media type through the SSRF-hardened transport.
func TestFetchImage_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	}))
	defer srv.Close()
	_, port := hostPort(t, srv.URL)

	c := fetchClient(t, map[string][]netip.Addr{"img.test": {publicIP}})
	body, ct, err := c.FetchImage(context.Background(), "conv1", "http://img.test:"+port+"/thumb.png")
	if err != nil {
		t.Fatalf("FetchImage: %v", err)
	}
	if ct != "image/png" {
		t.Fatalf("content-type = %q, want image/png", ct)
	}
	if !bytes.Equal(body, pngBytes) {
		t.Fatalf("body mismatch: got %d bytes", len(body))
	}
}

// TestFetchImage_BlocksPrivateAndMetadata (D-09): a host resolving to a private /
// metadata / loopback IP is rejected by the SAME validateAndPin guard web_fetch uses,
// with a sanitized WebError carrying no IP/host detail. The target is never dialed.
func TestFetchImage_BlocksPrivateAndMetadata(t *testing.T) {
	cases := map[string]netip.Addr{
		"metadata": netip.MustParseAddr("169.254.169.254"), // link-local (cloud metadata)
		"loopback": netip.MustParseAddr("127.0.0.1"),
		"private":  netip.MustParseAddr("10.0.0.5"),
	}
	for name, ip := range cases {
		t.Run(name, func(t *testing.T) {
			c := fetchClient(t, map[string][]netip.Addr{"evil.test": {ip}})
			_, _, err := c.FetchImage(context.Background(), "conv1", "http://evil.test:80/x.png")
			we, ok := AsWebError(err)
			if !ok {
				t.Fatalf("want WebError, got %T: %v", err, err)
			}
			if we.Code != CodeBlockedURL {
				t.Fatalf("want blocked_url, got %s/%s", we.Code, we.Reason)
			}
			// No sensitive field leaks (the WebError is the sanitized shape).
			if msg := we.Error(); bytes.Contains([]byte(msg), []byte(ip.String())) {
				t.Fatalf("blocked WebError leaked the IP: %q", msg)
			}
		})
	}
}

// TestFetchImage_RejectsNonImageContentType (D-09): a non-image content-type and an
// SVG (script-bearing, excluded per A5) are both rejected with unsupported_content_type.
func TestFetchImage_RejectsNonImageContentType(t *testing.T) {
	for _, ct := range []string{"text/html", "application/json", "image/svg+xml"} {
		t.Run(ct, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", ct)
				_, _ = w.Write([]byte("<svg/>"))
			}))
			defer srv.Close()
			_, port := hostPort(t, srv.URL)

			c := fetchClient(t, map[string][]netip.Addr{"img.test": {publicIP}})
			_, _, err := c.FetchImage(context.Background(), "conv1", "http://img.test:"+port+"/x")
			we, ok := AsWebError(err)
			if !ok {
				t.Fatalf("want WebError, got %T: %v", err, err)
			}
			if we.Code != CodeUnsupportedContent {
				t.Fatalf("content-type %q: want unsupported_content_type, got %s", ct, we.Code)
			}
		})
	}
}

// TestFetchImage_SizeCap (D-09): a body exceeding WebFetchMaxBodyBytes is rejected as
// response_too_large (the io.LimitReader+1 sentinel byte trips it).
func TestFetchImage_SizeCap(t *testing.T) {
	big := bytes.Repeat([]byte("A"), 200)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(big)
	}))
	defer srv.Close()
	_, port := hostPort(t, srv.URL)

	c := fetchClient(t, map[string][]netip.Addr{"img.test": {publicIP}})
	c.cfg.WebFetchMaxBodyBytes = 100 // smaller than the 200-byte body
	_, _, err := c.FetchImage(context.Background(), "conv1", "http://img.test:"+port+"/big.jpg")
	we, ok := AsWebError(err)
	if !ok {
		t.Fatalf("want WebError, got %T: %v", err, err)
	}
	if we.Code != CodeResponseTooLarge {
		t.Fatalf("want response_too_large, got %s", we.Code)
	}
}

// TestFetchImage_RedirectNotFollowed (D-09): a 3xx is rejected outright (never chased
// to a possibly-private hop), unlike web_fetch which re-validates each hop.
func TestFetchImage_RedirectNotFollowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "http://elsewhere.test/x.png")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	_, port := hostPort(t, srv.URL)

	c := fetchClient(t, map[string][]netip.Addr{"img.test": {publicIP}})
	_, _, err := c.FetchImage(context.Background(), "conv1", "http://img.test:"+port+"/redir")
	we, ok := AsWebError(err)
	if !ok {
		t.Fatalf("want WebError, got %T: %v", err, err)
	}
	if we.Code != CodeHTTPError || we.Reason != "redirect_not_followed" {
		t.Fatalf("want http_error/redirect_not_followed, got %s/%s", we.Code, we.Reason)
	}
}

// TestFetchImage_RejectsBadScheme: a non-http(s) scheme is rejected before any dial.
func TestFetchImage_RejectsBadScheme(t *testing.T) {
	c := fetchClient(t, nil)
	_, _, err := c.FetchImage(context.Background(), "conv1", "ftp://files.test/secret.png")
	we, ok := AsWebError(err)
	if !ok {
		t.Fatalf("want WebError, got %T: %v", err, err)
	}
	if we.Code != CodeUnsupportedScheme {
		t.Fatalf("want unsupported_scheme, got %s", we.Code)
	}
}
