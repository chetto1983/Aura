package agui

import (
	"context"
	"encoding/json"
	"errors"
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
	state      MCPAuthorization
	stateErr   error
	flow       mcp.Flow
	startErr   error
	flowErr    error
	removed    bool
	revokeErr  error
	completeAt string
	failedWith error
	sawOrigin  string
	sawOwner   string
}

func (f *fakeAuthProvider) AuthorizationState(context.Context, string) (MCPAuthorization, error) {
	return f.state, f.stateErr
}

func (f *fakeAuthProvider) StartAuthorization(_ context.Context, owner, _, origin string) (mcp.Flow, error) {
	f.sawOwner, f.sawOrigin = owner, origin
	return f.flow, f.startErr
}

func (f *fakeAuthProvider) AuthorizationFlow(owner, _ string) (mcp.Flow, error) {
	f.sawOwner = owner
	return f.flow, f.flowErr
}

func (f *fakeAuthProvider) CompleteAuthorization(state, _, _ string) (mcp.Flow, error) {
	f.completeAt = state
	if f.flowErr != nil {
		return mcp.Flow{}, f.flowErr
	}
	return f.flow, nil
}

func (f *fakeAuthProvider) FailAuthorization(state string, reason error) (mcp.Flow, error) {
	f.completeAt, f.failedWith = state, reason
	return f.flow, nil
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
		"state":    s.handleMCPAuthorizationState,
		"start":    s.handleMCPAuthorizationStart,
		"flow":     s.handleMCPAuthorizationFlow,
		"revoke":   s.handleMCPAuthorizationRevoke,
		"callback": s.handleMCPAuthorizationCallback,
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
	s.handleMCPAuthorizationStart(httptest.NewRecorder(), req)

	if provider.sawOrigin != "https://aura.lan:8443" {
		t.Fatalf("origin = %q, want the browser's own", provider.sawOrigin)
	}
	if provider.sawOwner != "id-1" {
		t.Fatalf("owner = %q, want the request identity", provider.sawOwner)
	}
}

// No public URL is needed, and none is assumed: with no Origin header the handler falls
// back to the address this request arrived on, which for a local cockpit is exactly
// http://localhost:<port>.
func TestOriginFallsBackToTheRequestsOwnAddress(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/x", nil)
	if got := cockpitOrigin(req); got != "http://localhost:8080" {
		t.Fatalf("origin = %q", got)
	}
}

// Behind a TLS-terminating proxy r.TLS is nil, so a scheme read from the connection would
// produce an http:// redirect that a provider rejects.
func TestOriginHonoursTheForwardedScheme(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "http://aura.example/x", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	if got := cockpitOrigin(req); got != "https://aura.example" {
		t.Fatalf("origin = %q, want the forwarded scheme honoured", got)
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

func TestCallbackCompletesTheFlow(t *testing.T) {
	t.Parallel()
	provider := &fakeAuthProvider{flow: mcp.Flow{ID: "flow-1", Status: mcp.FlowApproved}}
	s := serverWithAuth(provider)
	recorder := httptest.NewRecorder()
	s.handleMCPAuthorizationCallback(recorder, httptest.NewRequest(http.MethodGet, "/cb?state=st4te&code=abc&iss=https%3A%2F%2Fslack.com", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if provider.completeAt != "st4te" {
		t.Fatalf("completed state = %q", provider.completeAt)
	}
	// text/plain, because every value available to render was chosen by the
	// authorization server and none of it may become markup in the operator's origin.
	if ct := recorder.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content type = %q, want text/plain", ct)
	}
	if recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("the callback response may be sniffed into another type")
	}
}

// A refusal has to reach the flow, or the cockpit polls a spinner until the TTL expires
// and never says why.
func TestCallbackReportsAProviderRefusalToTheFlow(t *testing.T) {
	t.Parallel()
	provider := &fakeAuthProvider{}
	s := serverWithAuth(provider)
	recorder := httptest.NewRecorder()
	s.handleMCPAuthorizationCallback(recorder, httptest.NewRequest(http.MethodGet, "/cb?state=st4te&error=access_denied&error_description=nope", nil))

	if provider.failedWith == nil || !strings.Contains(provider.failedWith.Error(), "access_denied") {
		t.Fatalf("failure reported = %v", provider.failedWith)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want the human to get a readable page", recorder.Code)
	}
}

func TestCallbackRefusesARedirectItCannotMatch(t *testing.T) {
	t.Parallel()
	s := serverWithAuth(&fakeAuthProvider{})
	for name, target := range map[string]string{
		"no state": "/cb?code=abc",
		"no code":  "/cb?state=st4te",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			recorder := httptest.NewRecorder()
			s.handleMCPAuthorizationCallback(recorder, httptest.NewRequest(http.MethodGet, target, nil))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", recorder.Code)
			}
		})
	}
}

// A replayed redirect — a refresh of the callback tab, a browser prefetch — is a conflict,
// not a server error, and the human is told they can close the tab.
func TestCallbackOnAnAlreadyFinishedFlowIsAConflict(t *testing.T) {
	t.Parallel()
	s := serverWithAuth(&fakeAuthProvider{flowErr: errors.New("already completed")})
	recorder := httptest.NewRecorder()
	s.handleMCPAuthorizationCallback(recorder, httptest.NewRequest(http.MethodGet, "/cb?state=st4te&code=abc", nil))

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
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
