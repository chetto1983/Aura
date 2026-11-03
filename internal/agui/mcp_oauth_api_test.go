package agui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
)

// fakeAuthProvider records what the handlers ask for. The provider itself is exercised in
// cmd/aura; what matters here is the HTTP contract — who may see what, what the redirect
// route does with a provider's answer, and which values never reach the wire.
type fakeAuthProvider struct {
	state         MCPAuthorization
	stateErr      error
	flow          mcp.Flow
	startErr      error
	flowErr       error
	removed       bool
	revokeErr     error
	sawOrigin     string
	sawOwner      string
	pastedAddress string
}

func (f *fakeAuthProvider) AuthorizationState(context.Context, string) (MCPAuthorization, error) {
	return f.state, f.stateErr
}

func (f *fakeAuthProvider) StartSignIn(_ context.Context, owner, _ string, request SignInRequest) (MCPSignInResult, error) {
	f.sawOwner, f.sawOrigin = owner, request.Origin
	return MCPSignInResult{Flow: f.flow}, f.startErr
}

func (f *fakeAuthProvider) AuthorizationFlow(owner, _ string) (mcp.Flow, error) {
	f.sawOwner = owner
	return f.flow, f.flowErr
}

func (f *fakeAuthProvider) RevokeAuthorization(context.Context, string) (bool, error) {
	return f.removed, f.revokeErr
}

func serverWithAuth(p MCPAuthorizationProvider) *Server {
	s := &Server{}
	s.SetMCPAuthorizations(p)
	return s
}

func authRequest(method, target string, identity string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	if identity != "" {
		req = req.WithContext(identityctx.WithIdentityID(req.Context(), identity))
	}
	req.SetPathValue("name", "notion")
	req.SetPathValue("id", "flow-1")
	return req
}

// A deployment with no Postgres (or no secret to derive the grant key from) must say so
// rather than reporting every server as needing no authorization — a 200 with
// supported:false would be a lie the operator acts on.
func TestAuthorizationRoutesAnswer503WhenUnwired(t *testing.T) {
	t.Parallel()
	s := &Server{}
	for name, call := range map[string]func(http.ResponseWriter, *http.Request){
		"state":  s.handleMCPAuthorizationState,
		"start":  s.handleMCPAuthorizationStart,
		"flow":   s.handleMCPAuthorizationFlow,
		"revoke": s.handleMCPAuthorizationRevoke,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			recorder := httptest.NewRecorder()
			call(recorder, authRequest(http.MethodGet, "/api/governance/mcp/notion/authorization", "id-1"))
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", recorder.Code)
			}
		})
	}
}

func TestAuthorizationStateIsRendered(t *testing.T) {
	t.Parallel()
	s := serverWithAuth(&fakeAuthProvider{state: MCPAuthorization{Supported: true, Authorized: true}})
	recorder := httptest.NewRecorder()
	s.handleMCPAuthorizationState(recorder, authRequest(http.MethodGet, "/x", "id-1"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var body MCPAuthorization
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Supported || !body.Authorized {
		t.Fatalf("body = %+v", body)
	}
}

// The origin is what the authorization server sends the human back to, so the handler
// must pass the browser's own view of this cockpit — not a guess, and not nothing.
func TestStartPassesTheBrowsersOwnOrigin(t *testing.T) {
	t.Parallel()
	provider := &fakeAuthProvider{flow: mcp.Flow{ID: "flow-1", Status: mcp.FlowAuthorizationRequired, AuthorizationURL: "https://slack.com/authorize?state=s"}}
	s := serverWithAuth(provider)

	req := authRequest(http.MethodPost, "/x", "id-1")
	req.Header.Set("Origin", "https://aura.lan:8443")
	req.Host = "aura.lan:8443"
	req.Header.Set("X-Forwarded-Proto", "https")
	s.handleMCPAuthorizationStart(httptest.NewRecorder(), req)

	if provider.sawOrigin != "https://aura.lan:8443" {
		t.Fatalf("origin = %q, want the browser's own", provider.sawOrigin)
	}
	if provider.sawOwner != "id-1" {
		t.Fatalf("owner = %q, want the request identity", provider.sawOwner)
	}
}

// The state is what COMPLETES an authorization. A status endpoint that returned it would
// let anyone who could read one finish somebody else's flow.
func TestFlowResponseNeverCarriesTheState(t *testing.T) {
	t.Parallel()
	provider := &fakeAuthProvider{flow: mcp.Flow{
		ID: "flow-1", ServerName: "notion", Status: mcp.FlowAuthorizationRequired,
		AuthorizationURL: "https://slack.com/authorize?state=super-secret-state",
	}}
	s := serverWithAuth(provider)
	recorder := httptest.NewRecorder()
	s.handleMCPAuthorizationFlow(recorder, authRequest(http.MethodGet, "/x", "id-1"))

	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, present := body["state"]; present {
		t.Fatal("the flow's state reached the wire")
	}
	// The authorization URL legitimately contains it — that is the URL the human opens —
	// but no field of our own may.
	if body["flowId"] != "flow-1" || body["status"] != mcp.FlowAuthorizationRequired {
		t.Fatalf("body = %+v", body)
	}
}

func TestStartAndFlowRequireAnIdentity(t *testing.T) {
	t.Parallel()
	s := serverWithAuth(&fakeAuthProvider{})
	for name, call := range map[string]func(http.ResponseWriter, *http.Request){
		"start": s.handleMCPAuthorizationStart,
		"flow":  s.handleMCPAuthorizationFlow,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			recorder := httptest.NewRecorder()
			call(recorder, authRequest(http.MethodPost, "/x", ""))
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", recorder.Code)
			}
		})
	}
}

func TestRevokeReportsWhetherAnythingWasRemoved(t *testing.T) {
	t.Parallel()
	for name, removed := range map[string]bool{"had a grant": true, "had none": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := serverWithAuth(&fakeAuthProvider{removed: removed})
			recorder := httptest.NewRecorder()
			s.handleMCPAuthorizationRevoke(recorder, authRequest(http.MethodDelete, "/x", "id-1"))

			var body map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body["removed"] != removed {
				t.Fatalf("removed = %v, want %v", body["removed"], removed)
			}
		})
	}
}

// A server with no authorization flow gets no button, so a revoke that reaches it anyway is
// the caller's mistake: a 400 with the reason, not a 500 that sends someone reading logs.
func TestRevokeOfAServerWithNoAuthorizationFlowIsABadRequest(t *testing.T) {
	t.Parallel()
	refusal := fmt.Errorf("%w: Aura issues this sidecar's credential itself", ErrNoAuthorizationFlow)
	s := serverWithAuth(&fakeAuthProvider{revokeErr: refusal})
	recorder := httptest.NewRecorder()
	s.handleMCPAuthorizationRevoke(recorder, authRequest(http.MethodDelete, "/x", "id-1"))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("revoke status = %d, want 400", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "Aura issues") {
		t.Fatalf("body = %q, want the reason", recorder.Body.String())
	}
}

// An unknown server is a 404, and a start the provider refuses is a 400 carrying the
// reason — a cockpit that only saw "500" would send someone reading server logs.
func TestProviderErrorsBecomeReadableStatuses(t *testing.T) {
	t.Parallel()
	unknown := serverWithAuth(&fakeAuthProvider{stateErr: errors.New("not configured")})
	recorder := httptest.NewRecorder()
	unknown.handleMCPAuthorizationState(recorder, authRequest(http.MethodGet, "/x", "id-1"))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("state status = %d, want 404", recorder.Code)
	}

	refused := serverWithAuth(&fakeAuthProvider{startErr: errors.New("takes no authorization flow")})
	recorder = httptest.NewRecorder()
	refused.handleMCPAuthorizationStart(recorder, authRequest(http.MethodPost, "/x", "id-1"))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("start status = %d, want 400", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "takes no authorization flow") {
		t.Fatalf("body = %q, want the reason", recorder.Body.String())
	}
}
