package agui

import (
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

// returnBaseSeenBySidecar starts the Google flow through the proxy and reports the returnBase the
// sidecar received — the origin the relay will send the browser back to.
func returnBaseSeenBySidecar(t *testing.T, publicURL string, header http.Header) string {
	t.Helper()
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetCalendarMCP(sidecar.URL, publicURL, staticMCPAccessTokenProvider{token: fakePIMToken})

	req := httptest.NewRequest(http.MethodGet, "http://192.168.101.158/api/connect/pim/accounts/work/google/start", nil)
	maps.Copy(req.Header, header)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, withPrincipal(req, fakePIMIdentity))
	if rec.Code != http.StatusOK {
		t.Fatalf("google/start = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if p.gotPath != "/admin/auth/work/google/start" {
		t.Fatalf("forwarded path = %q", p.gotPath)
	}
	q, err := url.ParseQuery(p.gotQuery)
	if err != nil {
		t.Fatalf("forwarded query %q: %v", p.gotQuery, err)
	}
	return q.Get("returnBase")
}

func TestPIMGoogleStartSendsTheCockpitOriginAsReturnBase(t *testing.T) {
	if got := returnBaseSeenBySidecar(t, "", nil); got != "http://192.168.101.158" {
		t.Fatalf("returnBase = %q, want the origin the cockpit request arrived on", got)
	}
}

func TestPIMGoogleStartHonoursTheTLSTerminatingProxy(t *testing.T) {
	got := returnBaseSeenBySidecar(t, "", http.Header{"X-Forwarded-Proto": {"https"}})
	if got != "https://192.168.101.158" {
		t.Fatalf("returnBase = %q, want https from X-Forwarded-Proto", got)
	}
}

func TestPIMGoogleStartPrefersThePinnedPublicURLOrigin(t *testing.T) {
	got := returnBaseSeenBySidecar(t, "https://aura.example.com/cockpit/", nil)
	if got != "https://aura.example.com" {
		t.Fatalf("returnBase = %q, want only the origin of AURA_WEB_PUBLIC_URL", got)
	}
}

// The sidecar answers a write only after its registry has reloaded (fork commit 529a101), so the
// proxy no longer retries a 404 after create: a missing account is reported at once.
func TestPIMGoogleStartPassesA404ThroughWithoutRetrying(t *testing.T) {
	var calls atomic.Int32
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"Account 'work' not found."}`)
	}))
	defer sidecar.Close()
	srv := connectPIMServer(sidecar.URL, fakePIMToken)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/connect/pim/accounts/work/google/start")
	if err != nil {
		t.Fatalf("GET google/start: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("google/start = %d, want the sidecar's 404", resp.StatusCode)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("sidecar called %d times, want exactly 1", got)
	}
}

func TestPIMGoogleCallbackForwardsWithoutAToken(t *testing.T) {
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetCalendarMCP(sidecar.URL, "", staticMCPAccessTokenProvider{token: fakePIMToken})

	const query = "state=5d96.aHR0cHM6Ly8x&code=4%2F0Ab&scope=https%3A%2F%2Fwww.googleapis.com%2Fauth%2Fcalendar.readonly"
	// No principal on the request: the browser arrives without its session cookie.
	req := httptest.NewRequest(http.MethodGet, "http://192.168.101.158"+PIMGoogleCallbackPath+"?"+query, nil)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("callback = %d, want the sidecar's 200", rec.Code)
	}
	if p.gotPath != PIMGoogleCallbackPath || p.gotQuery != query {
		t.Fatalf("forwarded %s?%s, want %s?%s", p.gotPath, p.gotQuery, PIMGoogleCallbackPath, query)
	}
	if p.gotAuth != "" {
		t.Fatalf("Authorization %q forwarded; the callback must carry no token", p.gotAuth)
	}
	if !strings.Contains(rec.Body.String(), "Connected") || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("sidecar page not passed through: %q %q", rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("callback page must not leak or cache the code: %v", rec.Header())
	}
}

func TestPIMGoogleCallbackUnwired503(t *testing.T) {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, PIMGoogleCallbackPath+"?state=x&code=y", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unwired callback = %d, want 503", rec.Code)
	}
}

func TestPIMGoogleCallbackSidecarUnreachable502(t *testing.T) {
	sidecar := httptest.NewServer(http.NotFoundHandler())
	sidecarURL := sidecar.URL
	sidecar.Close()
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetCalendarMCP(sidecarURL, "", staticMCPAccessTokenProvider{token: fakePIMToken})

	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, PIMGoogleCallbackPath+"?state=x&code=y", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("callback with sidecar down = %d, want 502", rec.Code)
	}
	if strings.Contains(rec.Body.String(), sidecarURL) {
		t.Fatalf("502 body leaks the sidecar address: %q", rec.Body.String())
	}
}
