//go:build !web_integration

package web

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
)

// fetchClient builds a Client whose hardened transport maps each fake host to a
// classification IP (public → allowed dial; link-local/loopback → blocked at the
// gate). The injected dialer rewrites an ALLOWED pinned IP to the real loopback the
// httptest server listens on, so the SSRF classifier runs for real while the test
// still connects to the local server. A blocked IP never reaches the dialer.
func fetchClient(t *testing.T, hostIPs map[string][]netip.Addr) *Client {
	t.Helper()
	cfg := &config.Config{
		WebDNSPinTTLSec:      60,
		WebFetchMaxBodyBytes: 5_000_000,
		WebFetchTimeoutSec:   30,
		WebUserAgent:         "Aura/test web",
	}
	c := NewClient(cfg)
	c.transport = newHardenedTransport(
		newGuard(stubResolver{answers: hostIPs}, c.pin, nil),
		func(ctx context.Context, network, addr string) (net.Conn, error) {
			// addr is the pinned public ip:port (the gate already approved it). The
			// port is the real httptest port; dial loopback on that port.
			_, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort("127.0.0.1", port))
		},
		cfg.WebUserAgent,
	)
	return c
}

// publicIP is the classification stand-in for an allowed host: it passes classify
// (TEST-NET-3, not private/loopback/link-local) so the gate approves the dial.
var publicIP = netip.MustParseAddr("203.0.113.10")

func hostPort(t *testing.T, rawURL string) (host, port string) {
	t.Helper()
	u := mustParse(t, rawURL)
	h, p, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("split %q: %v", u.Host, err)
	}
	return h, p
}

func TestFetch_Readability(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(richHTML))
	}))
	defer srv.Close()

	_, port := hostPort(t, srv.URL)
	c := fetchClient(t, map[string][]netip.Addr{"page.test": {publicIP}})
	page, err := c.Fetch(context.Background(), "conv1", "http://page.test:"+port+"/article")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.Contains(page.ContentMD, "genuine readable article body") {
		t.Errorf("markdown missing body: %q", page.ContentMD)
	}
	if strings.Contains(page.ContentMD, "FOOTER_FIXTURE_MARKER") {
		t.Errorf("footer boilerplate leaked: %q", page.ContentMD)
	}
	if page.Title != "The Real Title" {
		t.Errorf("title = %q", page.Title)
	}
}

func TestFetch_RedirectRevalidate(t *testing.T) {
	var metadataHits atomic.Int32
	metaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		metadataHits.Add(1)
		_, _ = w.Write([]byte("SECRET METADATA"))
	}))
	defer metaSrv.Close()
	_, metaPort := hostPort(t, metaSrv.URL)

	var publicHits atomic.Int32
	pubSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		publicHits.Add(1)
		w.Header().Set("Location", "http://metadata.test:"+metaPort+"/latest")
		w.WriteHeader(http.StatusFound)
	}))
	defer pubSrv.Close()
	_, pubPort := hostPort(t, pubSrv.URL)

	meta := netip.MustParseAddr("169.254.169.254") // link-local → blocked by classify
	c := fetchClient(t, map[string][]netip.Addr{
		"public.test":   {publicIP},
		"metadata.test": {meta},
	})
	_, err := c.Fetch(context.Background(), "conv1", "http://public.test:"+pubPort+"/start")
	we, ok := AsWebError(err)
	if !ok {
		t.Fatalf("want WebError, got %T: %v", err, err)
	}
	if we.Code != CodeBlockedURL || we.Reason != ReasonRedirectToBlocked {
		t.Errorf("want blocked_url/redirect_to_blocked_target, got %s/%s", we.Code, we.Reason)
	}
	if publicHits.Load() != 1 {
		t.Errorf("first public hop should be dialed once, got %d", publicHits.Load())
	}
	if metadataHits.Load() != 0 {
		t.Errorf("blocked metadata target must NOT be dialed, got %d hits", metadataHits.Load())
	}
}

func TestFetch_ContentGate(t *testing.T) {

	t.Run("unsupported_content_type", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.4 ..."))
		}))
		defer srv.Close()
		_, port := hostPort(t, srv.URL)
		c := fetchClient(t, map[string][]netip.Addr{"page.test": {publicIP}})
		_, err := c.Fetch(context.Background(), "c", "http://page.test:"+port+"/x")
		assertWebErr(t, err, CodeUnsupportedContent, "")
	})

	t.Run("response_too_large", func(t *testing.T) {
		// Inject a TINY raw-body ceiling so a small fixture still trips the gate
		// cheaply — the cap governs the raw HTTP body, not the markdown, so a
		// realistic 5MB fixture is unnecessary (and would be slow).
		body := strings.Repeat("<p>padding padding padding</p>", 50)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html><body>" + body + "</body></html>"))
		}))
		defer srv.Close()
		_, port := hostPort(t, srv.URL)
		c := fetchClient(t, map[string][]netip.Addr{"page.test": {publicIP}})
		c.cfg.WebFetchMaxBodyBytes = 128 // far below the fixture body size
		_, err := c.Fetch(context.Background(), "c", "http://page.test:"+port+"/big")
		assertWebErr(t, err, CodeResponseTooLarge, "")
	})
}

func TestFetch_LowContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><p>hi</p></body></html>"))
	}))
	defer srv.Close()
	_, port := hostPort(t, srv.URL)
	c := fetchClient(t, map[string][]netip.Addr{"page.test": {publicIP}})
	page, err := c.Fetch(context.Background(), "c", "http://page.test:"+port+"/thin")
	if err != nil {
		t.Fatalf("low content must not error: %v", err)
	}
	if page.Warning != "low_content" {
		t.Errorf("warning = %q, want low_content", page.Warning)
	}
}

func TestFetch_UnsupportedScheme(t *testing.T) {
	c := fetchClient(t, nil)
	_, err := c.Fetch(context.Background(), "c", "ftp://example.com/file")
	assertWebErr(t, err, CodeUnsupportedScheme, "")
}

func TestFetch_PerHostConcurrencyCap(t *testing.T) {
	var inFlight atomic.Int32
	var maxObserved atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := inFlight.Add(1)
		for {
			old := maxObserved.Load()
			if n <= old || maxObserved.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(40 * time.Millisecond)
		inFlight.Add(-1)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(richHTML))
	}))
	defer srv.Close()
	_, port := hostPort(t, srv.URL)
	c := fetchClient(t, map[string][]netip.Addr{"page.test": {publicIP}})

	var wg sync.WaitGroup
	for i := 0; i < perHostLimit+3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.Fetch(context.Background(), "c", "http://page.test:"+port+"/p")
		}()
	}
	wg.Wait()
	if maxObserved.Load() > int32(perHostLimit) {
		t.Errorf("per-host concurrency exceeded cap: observed %d > %d", maxObserved.Load(), perHostLimit)
	}
}

func TestRetry_Policy(t *testing.T) {

	t.Run("retry_on_503", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(richHTML))
		}))
		defer srv.Close()
		_, port := hostPort(t, srv.URL)
		c := fetchClient(t, map[string][]netip.Addr{"page.test": {publicIP}})
		_, err := c.Fetch(context.Background(), "c", "http://page.test:"+port+"/r")
		if err != nil {
			t.Fatalf("Fetch after retry: %v", err)
		}
		if calls.Load() != 2 {
			t.Errorf("want 2 calls (1 retry), got %d", calls.Load())
		}
	})

	t.Run("no_retry_on_404", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		_, port := hostPort(t, srv.URL)
		c := fetchClient(t, map[string][]netip.Addr{"page.test": {publicIP}})
		_, err := c.Fetch(context.Background(), "c", "http://page.test:"+port+"/nf")
		if err == nil {
			t.Fatal("want error on 404")
		}
		if calls.Load() != 1 {
			t.Errorf("want 1 call (no retry on 404), got %d", calls.Load())
		}
	})
}
