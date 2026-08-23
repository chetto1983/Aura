package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

const (
	testViewURI  = "ui://fixture/thread.html"
	testViewHTML = `<!doctype html><html><head><title>t</title></head><body>view</body></html>`
	cockpitHost  = "aura.test"
	sandboxOK    = "https://aura.test:8444"
)

type stubViewCaller struct {
	structured json.RawMessage
	text       string
	err        error
	sawServer  string
	sawTool    string
}

func (s *stubViewCaller) CallForView(_ context.Context, server, tool string, _ map[string]any) (mcp.ToolPayload, error) {
	s.sawServer, s.sawTool = server, tool
	return mcp.ToolPayload{Structured: s.structured, Text: s.text}, s.err
}

func newViewServer(t *testing.T, origin string, caller MCPViewToolCaller) http.Handler {
	t.Helper()
	catalog := mcp.NewViewCatalog()
	if err := catalog.Put(mcp.ViewDocument{
		Server: "fixture", ResourceURI: testViewURI, HTML: testViewHTML,
		Policy: mcp.ViewPolicy{DeclaredCSP: true, FrameDomains: []string{"https://learn-video.azurefd.net"}},
	}); err != nil {
		t.Fatalf("seed catalog: %v", err)
	}
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	s.SetMCPViews(catalog, origin, caller)
	return s.Mux()
}

// get issues a request that looks like one arriving through the appliance proxy:
// the browser's authority in Host, the scheme in X-Forwarded-Proto.
func get(t *testing.T, handler http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Host = cockpitHost
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestHandleMCPView_ServesTheArmedDocument(t *testing.T) {
	rec := get(t, newViewServer(t, sandboxOK, nil), "/api/mcp/view?server=fixture&uri="+testViewURI)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	var dto struct {
		HTML          string `json:"html"`
		SandboxOrigin string `json:"sandbox_origin"`
		AppliedCSP    string `json:"applied_csp"`
		Declared      struct {
			FrameDomains []string `json:"frame_domains"`
			Sealed       bool     `json:"sealed"`
		} `json:"declared"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The policy travels WITH the bytes: the cockpit renders this from a srcdoc
	// frame, where no response header of ours would follow it.
	if !strings.Contains(dto.HTML, "Content-Security-Policy") {
		t.Errorf("the served document must carry its policy: %q", dto.HTML)
	}
	if !strings.Contains(dto.AppliedCSP, "frame-src https://learn-video.azurefd.net") {
		t.Errorf("applied csp = %q", dto.AppliedCSP)
	}
	if dto.SandboxOrigin != sandboxOK {
		t.Errorf("sandbox_origin = %q", dto.SandboxOrigin)
	}
	if len(dto.Declared.FrameDomains) != 1 || dto.Declared.Sealed {
		t.Errorf("declared = %#v", dto.Declared)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("HTML inside a JSON body must be served nosniff")
	}
}

// The load-bearing refusal. A view needs allow-same-origin to run; giving it the
// cockpit's origin would hand a mounted server the operator's session. A host
// that cannot isolate a view must not render one — degrading is not an option.
func TestHandleMCPView_RefusesWhenTheSandboxIsNotAnotherOrigin(t *testing.T) {
	for name, origin := range map[string]string{
		"unset":                    "",
		"same host and port":       "https://" + cockpitHost,
		"same, port made explicit": "https://" + cockpitHost + ":443",
		"same, different case":     "https://AURA.TEST",
	} {
		t.Run(name, func(t *testing.T) {
			rec := get(t, newViewServer(t, origin, nil), "/api/mcp/view?server=fixture&uri="+testViewURI)
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503; body %q", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "<html") {
				t.Fatal("a refusal must not carry the document")
			}
		})
	}
	// A genuinely different port IS a different origin, which is the deployment
	// the appliance ships.
	rec := get(t, newViewServer(t, sandboxOK, nil), "/api/mcp/view?server=fixture&uri="+testViewURI)
	if rec.Code != http.StatusOK {
		t.Fatalf("a second port must be accepted, got %d", rec.Code)
	}
}

func TestHandleMCPView_RejectsBadRequests(t *testing.T) {
	handler := newViewServer(t, sandboxOK, nil)
	for name, tc := range map[string]struct {
		target string
		want   int
	}{
		"no server":       {"/api/mcp/view?uri=" + testViewURI, http.StatusBadRequest},
		"not a ui:// uri": {"/api/mcp/view?server=fixture&uri=https://evil.test/a.html", http.StatusBadRequest},
		"unknown view":    {"/api/mcp/view?server=fixture&uri=ui://fixture/other.html", http.StatusNotFound},
		"unknown server":  {"/api/mcp/view?server=other&uri=" + testViewURI, http.StatusNotFound},
		"empty uri":       {"/api/mcp/view?server=fixture&uri=", http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			if rec := get(t, handler, tc.target); rec.Code != tc.want {
				t.Fatalf("status = %d, want %d; body %q", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestHandleMCPView_UnwiredAnswers503(t *testing.T) {
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	if rec := get(t, s.Mux(), "/api/mcp/view?server=fixture&uri="+testViewURI); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestHandleMCPSandbox(t *testing.T) {
	handler := newViewServer(t, sandboxOK, nil)

	if rec := get(t, handler, "/mcp-sandbox"); rec.Code != http.StatusBadRequest {
		t.Fatalf("a relay nobody claimed must be refused, got %d", rec.Code)
	}

	rec := get(t, handler, "/mcp-sandbox?host=https%3A%2F%2Faura.test")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors https://aura.test") {
		t.Errorf("only the naming origin may embed the relay: %q", csp)
	}
	if !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("the relay loads nothing: %q", csp)
	}
	// A picture that arrived inside a tool result may be painted; a picture the
	// frame would have to go and fetch may not. The view inherits this policy —
	// it is mounted as srcdoc — so without this directive a rendered panel could
	// not show an image at all, whatever its server returned.
	if !strings.Contains(csp, "img-src data:") {
		t.Errorf("a view must be able to paint the bytes it was handed: %q", csp)
	}
	// The relay must never grow a network reach: it carries data it was handed.
	for _, forbidden := range []string{"connect-src", "img-src http", "img-src *"} {
		if strings.Contains(csp, forbidden) {
			t.Errorf("the relay policy must not open %q: %q", forbidden, csp)
		}
	}
	if !strings.Contains(rec.Body.String(), "from_view") {
		t.Error("the relay document must be served")
	}
}

// The host parameter is written into a CSP directive, so its SHAPE is the thing
// that has to be enforced: an origin names an ancestor, anything longer starts
// writing policy.
func TestHandleMCPSandbox_TakesOnlyABareOrigin(t *testing.T) {
	handler := newViewServer(t, sandboxOK, nil)

	for _, claimed := range []string{
		"https://aura.test/path",
		"https://aura.test; script-src *",
		"https://aura.test?x=1",
		"https://user:pw@aura.test",
		"javascript:alert(1)",
		"aura.test",
		"   ",
	} {
		rec := get(t, handler, "/mcp-sandbox?host="+url.QueryEscape(claimed))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q must be refused, got %d (CSP %q)",
				claimed, rec.Code, rec.Header().Get("Content-Security-Policy"))
		}
	}

	// A port is part of an origin, and the sandbox is reached by one.
	rec := get(t, handler, "/mcp-sandbox?host="+url.QueryEscape("https://aura.test:8444"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors https://aura.test:8444") {
		t.Errorf("the port must survive into the directive: %q", csp)
	}
}

func TestHandleMCPViewCall(t *testing.T) {
	post := func(handler http.Handler, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/mcp/view/call", strings.NewReader(body))
		req.Host = cockpitHost
		req.Header.Set("X-Forwarded-Proto", "https")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	caller := &stubViewCaller{structured: json.RawMessage(`{"rows":[1,2]}`)}
	handler := newViewServer(t, sandboxOK, caller)

	rec := post(handler, `{"server":"fixture","tool":"list_messages","arguments":{"page":1}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	if caller.sawServer != "fixture" || caller.sawTool != "list_messages" {
		t.Fatalf("caller saw %q/%q", caller.sawServer, caller.sawTool)
	}
	if !strings.Contains(rec.Body.String(), `"rows":[1,2]`) {
		t.Fatalf("body = %q", rec.Body.String())
	}

	// A server that never served a document has no view to be calling back from,
	// so a valid session cannot use this route to drive an arbitrary mount.
	if rec := post(handler, `{"server":"memory","tool":"memory_recall"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	for name, body := range map[string]string{
		"no tool":   `{"server":"fixture"}`,
		"no server": `{"tool":"list_messages"}`,
		"not json":  `nope`,
	} {
		t.Run(name, func(t *testing.T) {
			if rec := post(handler, body); rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
		})
	}

	// The gate lives in the caller; the route must surface its refusal, and must
	// not leak the reason to a document that could be probing for tool names.
	refusing := newViewServer(t, sandboxOK, &stubViewCaller{err: errors.New("tool send_message is not callable from a view")})
	rec = post(refusing, `{"server":"fixture","tool":"send_message"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "send_message") {
		t.Fatalf("the refusal must not echo the tool: %q", rec.Body.String())
	}

	// No isolation, no calls either: in that state no view should be rendering.
	if rec := post(newViewServer(t, "", caller), `{"server":"fixture","tool":"list_messages"}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
