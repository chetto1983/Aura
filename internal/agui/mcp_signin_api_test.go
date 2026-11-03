package agui

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

type signInResponseProvider struct {
	fakeAuthProvider
	answer  MCPSignInResult
	request SignInRequest
}

func (p *signInResponseProvider) StartSignIn(_ context.Context, _, _ string, request SignInRequest) (MCPSignInResult, error) {
	p.request = request
	return p.answer, nil
}

func TestMCPStartReturnsDesktopPreparationAndFallbackMetadata(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result MCPSignInResult
		field  string
		value  any
	}{
		{"desktop", MCPSignInResult{SignInURL: "/signing-in/session?flow=f1"}, "signInUrl", "/signing-in/session?flow=f1"},
		{"prepare", MCPSignInResult{Preparing: true, Reason: "Download first"}, "preparing", true},
		{"fallback", MCPSignInResult{Via: "paste", Reason: "Use your own browser"}, "via", "paste"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &signInResponseProvider{answer: tc.result}
			r := authRequest("POST", "https://aura.test/api/governance/mcp/canva/authorization", "alice")
			r.Body = io.NopCloser(strings.NewReader(`{}`))
			r.Header.Set("X-Aura-Remote-Ingress", "tunnel")
			w := httptest.NewRecorder()
			serverWithAuth(p).handleMCPAuthorizationStart(w, r)
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 200 || body[tc.field] != tc.value {
				t.Fatalf("response=%s, err=%v", w.Body, err)
			}
			if p.request.Origin != "https://aura.test" || !p.request.Remote {
				t.Fatalf("request=%+v", p.request)
			}
			if tc.result.Preparing && body["flowId"] != nil {
				t.Fatalf("preparation invented a flow: %s", w.Body)
			}
		})
	}
}

func TestTheCockpitCallbackIsGone(t *testing.T) {
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	s.SetMCPAuthorizations(&fakeAuthProvider{})
	rr := httptest.NewRecorder()
	s.Mux().ServeHTTP(rr, httptest.NewRequest("GET", "https://aura.test/api/governance/mcp/authorization/callback?state=fixture&code=fixture", nil))
	if rr.Code != 404 {
		t.Fatalf("callback status=%d body=%s", rr.Code, rr.Body)
	}
}
