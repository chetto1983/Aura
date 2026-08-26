package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/chetto1983/aura/internal/mcp"
)

// With nothing configured the port must be ephemeral: dynamic registration registers the
// redirect URI during the very flow that uses it, so picking a fixed port would only
// create a collision to debug.
func TestLoopbackFlowBindsAnEphemeralPortByDefault(t *testing.T) {
	t.Parallel()
	flow, err := newLoopbackFlow(io.Discard, "")
	if err != nil {
		t.Fatalf("newLoopbackFlow: %v", err)
	}
	defer flow.Close()

	if !strings.HasPrefix(flow.redirect, "http://127.0.0.1:") {
		t.Fatalf("redirect = %q, want a loopback address", flow.redirect)
	}
	if strings.HasSuffix(flow.redirect, ":0"+mcpOAuthCallbackPath) {
		t.Fatalf("redirect kept the placeholder port: %q", flow.redirect)
	}
	if !strings.HasSuffix(flow.redirect, mcpOAuthCallbackPath) {
		t.Fatalf("redirect = %q, want it to end in the callback path", flow.redirect)
	}
}

// A pre-registered client's redirect URI has to match what the operator registered with
// the provider byte for byte. Rewriting it — even to an equivalent form — fails at the
// provider with an error that names nothing useful.
func TestLoopbackFlowUsesAConfiguredRedirectVerbatim(t *testing.T) {
	t.Parallel()
	const configured = "http://127.0.0.1:47653/slack/callback"
	flow, err := newLoopbackFlow(io.Discard, configured)
	if err != nil {
		t.Fatalf("newLoopbackFlow: %v", err)
	}
	defer flow.Close()

	if flow.redirect != configured {
		t.Fatalf("redirect = %q, want it unchanged", flow.redirect)
	}
	if got := flow.listener.Addr().String(); !strings.HasSuffix(got, ":47653") {
		t.Fatalf("listening on %q, want the configured port", got)
	}
}

// Without a port there is nothing to listen on, and the flow would print a URL whose
// redirect can never arrive. Failing before the browser opens is the whole point.
func TestLoopbackFlowRejectsARedirectItCannotReceive(t *testing.T) {
	t.Parallel()
	for name, configured := range map[string]string{
		"no port":     "http://127.0.0.1/callback",
		"not a url":   "ht tp://nope",
		"bare string": "callback",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			flow, err := newLoopbackFlow(io.Discard, configured)
			if err == nil {
				flow.Close()
				t.Fatalf("%q was accepted as a redirect", configured)
			}
		})
	}
}

// The SDK generates the state and compares what comes back, so the fetcher's only job is
// to relay it. Dropping it turns CSRF protection into a "state mismatch" bug report.
func TestCallbackRelaysTheCodeAndStateExactly(t *testing.T) {
	t.Parallel()
	results := make(chan *auth.AuthorizationResult, 1)
	failures := make(chan error, 1)
	flow := &loopbackFlow{out: io.Discard}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/cb?code=abc123&state=xyz&iss=https%3A%2F%2Fslack.com", nil)
	flow.handleCallback(results, failures)(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	select {
	case got := <-results:
		if got.Code != "abc123" || got.State != "xyz" || got.Iss != "https://slack.com" {
			t.Fatalf("result = %+v", got)
		}
	default:
		t.Fatal("the callback produced no result")
	}
}

// A provider that refuses must surface as an error naming the refusal, not as a timeout
// five minutes later.
func TestCallbackSurfacesAProviderRefusal(t *testing.T) {
	t.Parallel()
	results := make(chan *auth.AuthorizationResult, 1)
	failures := make(chan error, 1)
	flow := &loopbackFlow{out: io.Discard}

	request := httptest.NewRequest(http.MethodGet, "/cb?error=access_denied&error_description=user+said+no", nil)
	flow.handleCallback(results, failures)(httptest.NewRecorder(), request)

	select {
	case err := <-failures:
		if !strings.Contains(err.Error(), "access_denied") || !strings.Contains(err.Error(), "user said no") {
			t.Fatalf("error = %v, want the provider's own words", err)
		}
	default:
		t.Fatal("a refusal produced no error")
	}
}

// A browser fetching /favicon.ico must not be mistaken for a failed authorization.
func TestCallbackIgnoresRequestsThatAreNotTheRedirect(t *testing.T) {
	t.Parallel()
	results := make(chan *auth.AuthorizationResult, 1)
	failures := make(chan error, 1)
	flow := &loopbackFlow{out: io.Discard}

	recorder := httptest.NewRecorder()
	flow.handleCallback(results, failures)(recorder, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if len(results) != 0 || len(failures) != 0 {
		t.Fatal("a stray request was treated as an authorization outcome")
	}
}

func TestNoOAuthReasonNamesTheActualCause(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		server   mcp.ManagedServer
		settings mcp.OAuthSettings
		want     string
	}{
		"opted out": {
			mcp.ManagedServer{Type: mcp.ServerTypeStreamableHTTP, URL: "https://mcp.notion.com/mcp"},
			mcp.OAuthSettings{Disabled: true}, "MCP_OAUTH_DISABLED",
		},
		"stdio": {
			mcp.ManagedServer{Command: "notes-mcp"}, mcp.OAuthSettings{}, "stdio",
		},
		"static bearer": {
			mcp.ManagedServer{Type: mcp.ServerTypeStreamableHTTP, URL: "https://mcp.notion.com/mcp", Env: []string{"MCP_BEARER_TOKEN=t"}},
			mcp.OAuthSettings{}, "MCP_BEARER_TOKEN",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := noOAuthReason(tc.server, tc.settings); !strings.Contains(got, tc.want) {
				t.Fatalf("reason = %q, want it to mention %q", got, tc.want)
			}
		})
	}
}

// OAuth diagnostics need the pool too: without an operator identity they falsely report
// that a persisted grant does not exist.
func TestOAuthSubcommandsGetAPool(t *testing.T) {
	t.Parallel()
	for _, verb := range []string{"login", "logout", "authorizations", "status", "doctor", "tools"} {
		if !mcpCommandNeedsPool([]string{verb}) {
			t.Errorf("aura mcp %s would run without a Postgres pool", verb)
		}
	}
	// And they must NOT be routed through the managed-config audit trail: what they
	// change is one identity's credentials, not the server inventory.
	for _, verb := range []string{"login", "logout", "authorizations"} {
		if mcpMutatingSubcommands[verb] {
			t.Errorf("%q was added to the managed-config mutating set", verb)
		}
	}
}

func TestUsageAdvertisesTheAuthorizationVerbs(t *testing.T) {
	t.Parallel()
	for _, verb := range []string{"login", "logout", "authorizations"} {
		if !strings.Contains(mcpUsage, verb) {
			t.Errorf("usage does not mention %q, so nobody can discover it", verb)
		}
	}
}
