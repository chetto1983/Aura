package agui

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidSignInOrigins(t *testing.T) {
	for _, tc := range []struct {
		cockpit, stream string
		want            bool
	}{
		{"https://aura.test", "https://aura.test:8444", true},
		{"http://aura.test", "http://aura.test:8444", true},
		{"https://aura.test", "https://AURA.test:8444", true},
		{"https://aura.test", "https://aura.test", false},
		{"https://aura.test", "https://aura.test:443", false},
		{"https://aura.test", "http://aura.test:8444", false},
		{"https://aura.test", "https://other.test:8444", false},
		{"ftp://aura.test", "ftp://aura.test:8444", false},
		{"https://", "https://aura.test:8444", false},
		{"https://aura.test", "https://:8444", false},
		{"https://user@aura.test", "https://aura.test:8444", false},
		{"https://aura.test/", "https://aura.test:8444", false},
		{"https://aura.test", "https://aura.test:8444/x", false},
		{"https://aura.test:bad", "https://aura.test:8444", false},
		{"https://aura.test", "https://aura.test:bad", false},
		{"https://*.aura.test", "https://*.aura.test:8444", false},
		{"https://aura.test", "https://aura.test:8444\"", false},
	} {
		if got := validSignInOrigins(tc.cockpit, tc.stream); got != tc.want {
			t.Errorf("validSignInOrigins(%q, %q) = %v, want %v", tc.cockpit, tc.stream, got, tc.want)
		}
	}
}

func TestSignInMalformedSessionNeverReachesTheBrowser(t *testing.T) {
	b := &proxySignInBrowser{}
	h := signInProxyHandler(b)
	for _, target := range []string{"https://aura.test:8444/sign-in/bad/", "https://aura.test/api/sign-in/signin-0123"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, signInProxyRequest(target, "alice"))
		if rr.Code != http.StatusNotFound || b.checks.Load() != 0 {
			t.Fatalf("%s status=%d ownership checks=%d", target, rr.Code, b.checks.Load())
		}
	}
}

func TestSignInRoutesWithoutABrowserAreNotFound(t *testing.T) {
	h := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{}).Mux()
	for _, target := range []string{"https://aura.test/api/sign-in/" + proxySignInSession, "https://aura.test:8444" + proxySignInPath} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, signInProxyRequest(target, "alice"))
		if rr.Code != http.StatusNotFound {
			t.Fatalf("%s status=%d", target, rr.Code)
		}
	}
}

func TestSignInRefusesUnusableOrigins(t *testing.T) {
	for name, b := range map[string]*proxySignInBrowser{
		"none":   {noOrigins: true},
		"shared": {origins: &[2]string{"https://aura.test:8444", "https://aura.test:8444"}},
	} {
		h := signInProxyHandler(b)
		for _, target := range []string{"https://aura.test/api/sign-in/" + proxySignInSession, "https://aura.test:8444" + proxySignInPath} {
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, signInProxyRequest(target, "alice"))
			if rr.Code != http.StatusNotFound || b.dials.Load() != 0 {
				t.Fatalf("%s %s status=%d dials=%d", name, target, rr.Code, b.dials.Load())
			}
		}
	}
}

func TestSignInMetadataAndCancelRefuseTheStreamOrigin(t *testing.T) {
	b := &proxySignInBrowser{}
	h := signInProxyHandler(b)
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		r := signInProxyRequest("https://aura.test:8444/api/sign-in/"+proxySignInSession, "alice")
		r.Method = method
		r.Header.Set("Idempotency-Key", "stream-origin")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, r)
		if rr.Code != http.StatusNotFound || strings.Contains(rr.Body.String(), "8444") {
			t.Fatalf("%s from the stream origin: %d %s", method, rr.Code, rr.Body)
		}
	}
	if b.cancels.Load() != 0 {
		t.Fatal("the stream origin cancelled a sign-in")
	}
}

func TestSignInCancelFailureIsReported(t *testing.T) {
	b := &proxySignInBrowser{cancelErr: errors.New("browser stuck")}
	r := signInProxyRequest("https://aura.test/api/sign-in/"+proxySignInSession, "alice")
	r.Method = http.MethodDelete
	r.Header.Set("Idempotency-Key", "cancel-failure")
	rr := httptest.NewRecorder()
	signInProxyHandler(b).ServeHTTP(rr, r)
	if rr.Code != http.StatusServiceUnavailable || b.cancels.Load() != 1 || strings.Contains(rr.Body.String(), "stuck") {
		t.Fatalf("cancel failure: %d %q calls=%d", rr.Code, rr.Body, b.cancels.Load())
	}
}

func TestSignInReadinessRefusesWrites(t *testing.T) {
	b := &proxySignInBrowser{}
	r := signInProxyRequest("https://aura.test:8444"+proxySignInPath+".aura-ready", "alice")
	r.Method = http.MethodPost
	r.Header.Set("Origin", "https://aura.test")
	rr := httptest.NewRecorder()
	signInProxyHandler(b).ServeHTTP(rr, r)
	if rr.Code != http.StatusForbidden || rr.Header().Get("Access-Control-Allow-Origin") != "" || b.dials.Load() != 0 {
		t.Fatalf("readiness POST: %d %v dials=%d", rr.Code, rr.Header(), b.dials.Load())
	}
}

func TestSignInProxyRechecksTheOwnerBeforeDialing(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer upstream.Close()
	b := &proxySignInBrowser{target: strings.TrimPrefix(upstream.URL, "http://"), revokeAfter: 1}
	rr := httptest.NewRecorder()
	signInProxyHandler(b).ServeHTTP(rr, signInProxyRequest("https://aura.test:8444"+proxySignInPath, "alice"))
	if rr.Code != http.StatusBadGateway || b.dials.Load() != 0 || b.checks.Load() != 2 {
		t.Fatalf("revoked owner: status=%d dials=%d checks=%d", rr.Code, b.dials.Load(), b.checks.Load())
	}
}

func TestSignInProxyRewritesHostAndDropsBrowserState(t *testing.T) {
	dropped := []string{"Set-Cookie2", "Access-Control-Allow-Credentials", "Content-Security-Policy-Report-Only", "Refresh", "Reporting-Endpoints", "Report-To", "NEL"}
	type seenRequest struct {
		host   string
		header http.Header
	}
	seen := make(chan seenRequest, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- seenRequest{r.Host, r.Header.Clone()}
		for _, h := range dropped {
			w.Header().Set(h, "upstream")
		}
		w.Header().Set("Cache-Control", "public, max-age=600")
		w.Header().Set("Referrer-Policy", "unsafe-url")
	}))
	defer upstream.Close()
	b := &proxySignInBrowser{target: strings.TrimPrefix(upstream.URL, "http://")}
	r := signInProxyRequest("https://aura.test:8444"+proxySignInPath, "alice")
	r.Header.Set("Referer", "https://aura.test/chat/secret")
	r.Header.Set("Cookie2", "private")
	rr := httptest.NewRecorder()
	signInProxyHandler(b).ServeHTTP(rr, r)
	got := <-seen
	if got.host != "aura.test:8444" || got.header.Get("Referer") != "" || got.header.Get("Cookie2") != "" {
		t.Fatalf("upstream saw host=%q referer=%q cookie2=%q", got.host, got.header.Get("Referer"), got.header.Get("Cookie2"))
	}
	for _, h := range dropped {
		if v := rr.Header().Get(h); v != "" {
			t.Errorf("response kept %s=%q", h, v)
		}
	}
	if rr.Header().Get("Cache-Control") != "no-store" || rr.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("upstream overrode privacy headers: %v", rr.Header())
	}
}

func TestSignInRefusalsStayIsolated(t *testing.T) {
	h := signInProxyHandler(&proxySignInBrowser{})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, signInProxyRequest("https://aura.test:8444"+proxySignInPath, "bob"))
	if rr.Code != http.StatusNotFound || rr.Header().Get("Content-Security-Policy") != "sandbox; frame-ancestors 'none'" ||
		rr.Header().Get("Cache-Control") != "no-store" || rr.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("proxy refusal: %d %v", rr.Code, rr.Header())
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, signInProxyRequest("https://aura.test/api/sign-in/"+proxySignInSession, "bob"))
	if rr.Code != http.StatusNotFound || rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("metadata refusal: %d %v", rr.Code, rr.Header())
	}
}

func TestSignInProxyMatchesTheUpgradeHeaderCaseInsensitively(t *testing.T) {
	b := &proxySignInBrowser{}
	r := signInProxyRequest("https://aura.test:8444"+proxySignInPath, "alice")
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Upgrade", "WebSocket")
	r.Header.Set("Origin", "https://evil.example")
	rr := httptest.NewRecorder()
	signInProxyHandler(b).ServeHTTP(rr, r)
	if rr.Code != http.StatusForbidden || b.dials.Load() != 0 {
		t.Fatalf("mixed-case upgrade: status=%d dials=%d", rr.Code, b.dials.Load())
	}
}
