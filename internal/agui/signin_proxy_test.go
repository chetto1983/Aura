package agui

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/coder/websocket"
)

const proxySignInSession = "signin-0123456789abcdef01234567"
const proxySignInPath = "/sign-in/" + proxySignInSession + "/"

type proxySignInBrowser struct {
	ended   bool
	target  string
	dials   atomic.Int32
	cancels atomic.Int32
}

func (b *proxySignInBrowser) Owns(ctx context.Context, owner, session string) bool {
	return !b.ended && owner == "alice" && identityctx.IdentityID(ctx) == owner && session == proxySignInSession
}
func (*proxySignInBrowser) Origins(string) (string, string, bool) {
	return "https://aura.test", "https://aura.test:8444", true
}
func (b *proxySignInBrowser) Dial(ctx context.Context, _ string) (net.Conn, error) {
	b.dials.Add(1)
	if b.target == "" {
		return nil, errors.New("unavailable")
	}
	return (&net.Dialer{}).DialContext(ctx, "tcp", b.target)
}
func (b *proxySignInBrowser) Cancel(context.Context, string, string) error {
	b.cancels.Add(1)
	return nil
}

func signInProxyHandler(b *proxySignInBrowser) http.Handler {
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	s.SetSignInBrowser(b)
	return s.Mux()
}

func signInProxyRequest(target, owner string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	return r.WithContext(identityctx.WithIdentityID(r.Context(), owner))
}

func TestSignInProxyRefusesBeforeItDials(t *testing.T) {
	for _, tc := range []struct {
		owner, path string
		ended       bool
	}{
		{"", proxySignInPath, false}, {"bob", proxySignInPath, false}, {"alice", proxySignInPath, true}, {"alice", "/sign-in/bad/", false},
	} {
		b := &proxySignInBrowser{ended: tc.ended}
		rr := httptest.NewRecorder()
		signInProxyHandler(b).ServeHTTP(rr, signInProxyRequest("https://aura.test:8444"+tc.path, tc.owner))
		if rr.Code != 404 || b.dials.Load() != 0 {
			t.Fatalf("status=%d dials=%d", rr.Code, b.dials.Load())
		}
	}
}

func TestSignInProxyNeverForwardsTheCockpitCookie(t *testing.T) {
	seen := make(chan http.Header, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Clone()
		w.Header().Add("Set-Cookie", "aura_session=stolen; Path=/")
		w.Header().Set("Clear-Site-Data", `"cookies"`)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(200)
	}))
	defer upstream.Close()
	b := &proxySignInBrowser{target: strings.TrimPrefix(upstream.URL, "http://")}
	r := signInProxyRequest("https://aura.test:8444"+proxySignInPath, "alice")
	for _, h := range []string{"Cookie", "Authorization", "Proxy-Authorization", "Forwarded", "X-Forwarded-For", "X-Forwarded-User", "X-Aura-Identity"} {
		r.Header.Set(h, "private")
	}
	rr := httptest.NewRecorder()
	signInProxyHandler(b).ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Fatalf("status=%d: %s", rr.Code, rr.Body)
	}
	for h, v := range <-seen {
		if h == "Cookie" || strings.Contains(h, "Authorization") || h == "Forwarded" || strings.HasPrefix(h, "X-Forwarded-") || strings.HasPrefix(h, "X-Aura-") {
			t.Errorf("forwarded %s=%v", h, v)
		}
	}
	if rr.Header().Get("Set-Cookie") != "" || rr.Header().Get("Access-Control-Allow-Origin") != "" || rr.Header().Get("Clear-Site-Data") != "" {
		t.Fatal("upstream could set credentials or CORS")
	}
}

func TestSignInProxyIsolatesEveryResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "frame-ancestors *; sandbox allow-forms")
		if strings.HasSuffix(r.URL.Path, "error") {
			w.WriteHeader(500)
		}
		_, _ = io.WriteString(w, "body")
	}))
	defer upstream.Close()
	for _, path := range []string{"", "client.js", "error", "offline"} {
		b := &proxySignInBrowser{target: strings.TrimPrefix(upstream.URL, "http://")}
		if path == "offline" {
			b.target = ""
		}
		rr := httptest.NewRecorder()
		signInProxyHandler(b).ServeHTTP(rr, signInProxyRequest("https://aura.test:8444"+proxySignInPath+path, "alice"))
		csp := rr.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "sandbox allow-scripts allow-same-origin") || !strings.Contains(csp, "frame-ancestors https://aura.test;") || strings.Contains(csp, "allow-forms") {
			t.Fatalf("%s: %s", path, csp)
		}
		if rr.Header().Get("Cache-Control") != "no-store" || rr.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Fatal("private stream headers absent")
		}
	}
}

func TestSignInProxyRefusesTheCockpitOrigin(t *testing.T) {
	b := &proxySignInBrowser{}
	for _, path := range []string{"", "client.js", "websocket"} {
		r := signInProxyRequest("https://aura.test"+proxySignInPath+path, "alice")
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Origin", "https://aura.test")
		rr := httptest.NewRecorder()
		signInProxyHandler(b).ServeHTTP(rr, r)
		if rr.Code != 404 || b.dials.Load() != 0 {
			t.Fatalf("status=%d dials=%d", rr.Code, b.dials.Load())
		}
	}
}

func TestSignInProxyKeepsThePath(t *testing.T) {
	seen := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen <- r.URL.RequestURI() }))
	defer upstream.Close()
	b := &proxySignInBrowser{target: strings.TrimPrefix(upstream.URL, "http://")}
	rr := httptest.NewRecorder()
	signInProxyHandler(b).ServeHTTP(rr, signInProxyRequest("https://aura.test:8444"+proxySignInPath+"x?q=1", "alice"))
	if rr.Code != 200 || <-seen != proxySignInPath+"x?q=1" {
		t.Fatal("SUBFOLDER path changed")
	}
}

func TestSignInProxyRefusesAnUpgradeFromAnotherOrigin(t *testing.T) {
	for _, origin := range []string{"https://evil.example", "https://aura.test", "null", ""} {
		b := &proxySignInBrowser{}
		r := signInProxyRequest("https://aura.test:8444"+proxySignInPath, "alice")
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Origin", origin)
		rr := httptest.NewRecorder()
		signInProxyHandler(b).ServeHTTP(rr, r)
		if rr.Code != 403 || b.dials.Load() != 0 {
			t.Fatalf("%q status=%d dials=%d", origin, rr.Code, b.dials.Load())
		}
	}
}

func TestSignInProxyCarriesAWebsocket(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"aura.test:8444"}})
		if err != nil {
			return
		}
		defer ws.CloseNow()
		kind, data, err := ws.Read(r.Context())
		if err == nil {
			_ = ws.Write(r.Context(), kind, append([]byte("echo:"), data...))
		}
	}))
	defer upstream.Close()
	b := &proxySignInBrowser{target: strings.TrimPrefix(upstream.URL, "http://")}
	h := signInProxyHandler(b)
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = "aura.test:8444"
		r.Header.Set("X-Forwarded-Proto", "https")
		h.ServeHTTP(w, r.WithContext(identityctx.WithIdentityID(r.Context(), "alice")))
	}))
	defer front.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(front.URL, "http")+proxySignInPath, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"https://aura.test:8444"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	if err = ws.Write(ctx, websocket.MessageText, []byte("input")); err != nil {
		t.Fatal(err)
	}
	_, data, err := ws.Read(ctx)
	if err != nil || string(data) != "echo:input" {
		t.Fatalf("%q %v", data, err)
	}
}

func TestSignInReadinessIsOnlyReadableByTheCockpit(t *testing.T) {
	for _, origin := range []string{"https://aura.test", "https://evil.example", "https://aura.test:8444", ""} {
		b := &proxySignInBrowser{}
		r := signInProxyRequest("https://aura.test:8444"+proxySignInPath+".aura-ready", "alice")
		r.Header.Set("Origin", origin)
		rr := httptest.NewRecorder()
		signInProxyHandler(b).ServeHTTP(rr, r)
		if origin == "https://aura.test" {
			if rr.Code != 204 || rr.Header().Get("Access-Control-Allow-Origin") != origin || rr.Header().Get("Access-Control-Allow-Credentials") != "true" {
				t.Fatalf("readiness refused: %d %v", rr.Code, rr.Header())
			}
		} else if rr.Code != 403 || rr.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("unexpected readiness grant: %d", rr.Code)
		}
		if b.dials.Load() != 0 {
			t.Fatal("readiness dialed the desktop")
		}
	}
}

func TestSignInMetadataAndCancelStayWithTheirOwner(t *testing.T) {
	for _, who := range []string{"alice", "bob", ""} {
		b := &proxySignInBrowser{}
		h := signInProxyHandler(b)
		path := "https://aura.test/api/sign-in/" + proxySignInSession
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, signInProxyRequest(path, who))
		if who == "alice" {
			if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"streamOrigin":"https://aura.test:8444"`) {
				t.Fatalf("metadata=%d %s", rr.Code, rr.Body)
			}
		} else if rr.Code != 404 || strings.Contains(rr.Body.String(), "8444") {
			t.Fatal("metadata leaked")
		}
		r := signInProxyRequest(path, who)
		r.Method = http.MethodDelete
		r.Header.Set("Idempotency-Key", "cancel-fixture")
		rr = httptest.NewRecorder()
		h.ServeHTTP(rr, r)
		if who == "alice" {
			if rr.Code != 204 || b.cancels.Load() != 1 {
				t.Fatalf("cancel=%d calls=%d", rr.Code, b.cancels.Load())
			}
		} else if rr.Code != 404 || b.cancels.Load() != 0 {
			t.Fatal("cross-owner cancellation")
		}
		if b.dials.Load() != 0 {
			t.Fatal("metadata or cancel dialed desktop")
		}
	}
}
