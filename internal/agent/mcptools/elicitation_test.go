package mcptools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/elicit"
	"github.com/chetto1983/aura/internal/mcp"
)

// fakeConsent is the table-driven stand-in for the composition root's surface.
// Every field is a distinct failure mode the handler must resolve to a
// non-accept action.
type fakeConsent struct {
	action  string
	content map[string]any
	err     error
	panics  bool
	block   <-chan struct{}
	seen    chan elicit.Question
}

func (f *fakeConsent) AskOperator(ctx context.Context, req elicit.Question) (string, map[string]any, error) {
	if f.seen != nil {
		select {
		case f.seen <- req:
		default:
		}
	}
	if f.panics {
		panic("consent surface blew up")
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return "", nil, ctx.Err()
		}
	}
	return f.action, f.content, f.err
}

func callHandler(t *testing.T, server string, consent ElicitationConsent, params *sdkmcp.ElicitParams) *sdkmcp.ElicitResult {
	t.Helper()
	res, err := NewElicitationHandler(server, consent)(context.Background(), &sdkmcp.ElicitRequest{Params: params})
	if err != nil {
		t.Fatalf("handler returned a non-nil error (%v); an error here fails the whole CallTool through fulfillInputRequests instead of giving the server an answer", err)
	}
	if res == nil {
		t.Fatal("handler returned a nil result")
	}
	return res
}

// TestElicitationNeverAcceptsWithoutAnOperator enumerates every path that does
// NOT reach an operator decision and pins each to decline or cancel. This is the
// plan's central prohibition: a server must never obtain an accept by exploiting
// a failure mode.
// Not parallel: two cases drive the disable path through t.Setenv, which the
// testing package forbids under t.Parallel anywhere in the chain.
func TestElicitationNeverAcceptsWithoutAnOperator(t *testing.T) {
	blocked := make(chan struct{})
	t.Cleanup(func() { close(blocked) }) // let the blocked goroutine exit, so goleak stays green

	tests := []struct {
		name    string
		consent ElicitationConsent
		params  *sdkmcp.ElicitParams
		env     string
		want    string
	}{
		{
			name:    "no consent surface wired",
			consent: nil,
			params:  &sdkmcp.ElicitParams{Message: "who are you"},
			want:    elicit.ActionDecline,
		},
		{
			name:    "url mode is opt-out and never consults the surface",
			consent: &fakeConsent{action: elicit.ActionAccept, content: map[string]any{"leaked": true}},
			params:  &sdkmcp.ElicitParams{Mode: "url", URL: "https://evil.example/phish", Message: "click here"},
			want:    elicit.ActionDecline,
		},
		{
			name:    "url mode is matched case-insensitively",
			consent: &fakeConsent{action: elicit.ActionAccept},
			params:  &sdkmcp.ElicitParams{Mode: "URL", URL: "https://evil.example/phish"},
			want:    elicit.ActionDecline,
		},
		{
			name:    "surface returns an error",
			consent: &fakeConsent{err: errors.New("channel down")},
			params:  &sdkmcp.ElicitParams{Message: "hi"},
			want:    elicit.ActionDecline,
		},
		{
			name:    "surface panics",
			consent: &fakeConsent{panics: true},
			params:  &sdkmcp.ElicitParams{Message: "hi"},
			want:    elicit.ActionDecline,
		},
		{
			name:    "surface returns an unrecognised action",
			consent: &fakeConsent{action: "sure-why-not"},
			params:  &sdkmcp.ElicitParams{Message: "hi"},
			want:    elicit.ActionDecline,
		},
		{
			name:    "surface returns empty action",
			consent: &fakeConsent{action: ""},
			params:  &sdkmcp.ElicitParams{Message: "hi"},
			want:    elicit.ActionDecline,
		},
		{
			name:    "nil params",
			consent: &fakeConsent{action: elicit.ActionAccept},
			params:  nil,
			want:    elicit.ActionDecline,
		},
		{
			name:    "timeout <= 0 disables elicitation rather than waiting forever",
			consent: &fakeConsent{action: elicit.ActionAccept, block: blocked},
			params:  &sdkmcp.ElicitParams{Message: "hi"},
			env:     "0",
			want:    elicit.ActionDecline,
		},
		{
			name:    "negative timeout also disables",
			consent: &fakeConsent{action: elicit.ActionAccept, block: blocked},
			params:  &sdkmcp.ElicitParams{Message: "hi"},
			env:     "-5",
			want:    elicit.ActionDecline,
		},
		{
			name:    "surface declines",
			consent: &fakeConsent{action: elicit.ActionDecline},
			params:  &sdkmcp.ElicitParams{Message: "hi"},
			want:    elicit.ActionDecline,
		},
		{
			name:    "surface cancels",
			consent: &fakeConsent{action: elicit.ActionCancel},
			params:  &sdkmcp.ElicitParams{Message: "hi"},
			want:    elicit.ActionCancel,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				t.Setenv(envMCPElicitationTimeoutSec, tt.env)
			}
			res := callHandler(t, "fixture", tt.consent, tt.params)
			if res.Action != tt.want {
				t.Fatalf("action = %q, want %q", res.Action, tt.want)
			}
			if res.Action != elicit.ActionAccept && res.Content != nil {
				t.Fatalf("non-accept action %q carried content %v; content must only ride an accept", res.Action, res.Content)
			}
		})
	}
}

// TestElicitationAcceptPassesContentThrough is the one path that DOES reach an
// operator decision, so it must survive intact.
func TestElicitationAcceptPassesContentThrough(t *testing.T) {
	t.Parallel()
	want := map[string]any{"token": "abc"}
	res := callHandler(t, "fixture", &fakeConsent{action: elicit.ActionAccept, content: want}, &sdkmcp.ElicitParams{Message: "token?"})
	if res.Action != elicit.ActionAccept {
		t.Fatalf("action = %q, want accept", res.Action)
	}
	if res.Content["token"] != "abc" {
		t.Fatalf("content = %v, want %v", res.Content, want)
	}
}

// TestElicitationTimesOutToCancel pins T-45.1-30: a blocked surface cannot hold
// the call open beyond the one-second question bound (with scheduling slack).
func TestElicitationTimesOutToCancel(t *testing.T) {
	blocked := make(chan struct{})
	t.Cleanup(func() { close(blocked) })
	t.Setenv(envMCPElicitationTimeoutSec, "1")

	// No operator answer arrives before the timeout.
	consent := &fakeConsent{action: elicit.ActionAccept, block: blocked}
	start := time.Now()
	res, err := NewElicitationHandler("fixture", consent)(context.Background(), &sdkmcp.ElicitRequest{Params: &sdkmcp.ElicitParams{Message: "hi"}})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("handler returned a non-nil error: %v", err)
	}
	if res.Action != elicit.ActionCancel {
		t.Fatalf("action = %q, want cancel on timeout", res.Action)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("handler took %v; the timeout did not bound it", elapsed)
	}
}

// TestElicitationCancelledParentCancels asserts the ask dies with its caller —
// the ctx handed to the surface is derived from the SDK's.
func TestElicitationCancelledParentCancels(t *testing.T) {
	t.Parallel()
	blocked := make(chan struct{})
	t.Cleanup(func() { close(blocked) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := NewElicitationHandler("fixture", &fakeConsent{action: elicit.ActionAccept, block: blocked})(ctx, &sdkmcp.ElicitRequest{Params: &sdkmcp.ElicitParams{Message: "hi"}})
	if err != nil {
		t.Fatalf("handler returned a non-nil error: %v", err)
	}
	if res.Action != elicit.ActionCancel {
		t.Fatalf("action = %q, want cancel when the parent ctx is already cancelled", res.Action)
	}
}

func TestConfiguredElicitationTimeout(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want time.Duration
	}{
		{"unset uses the recorded default", "", defaultElicitationTimeout},
		{"explicit seconds", "45", 45 * time.Second},
		{"zero disables, it does not mean infinite", "0", 0},
		{"negative disables", "-1", 0},
		{"malformed falls back to the default rather than failing the mount", "banana", defaultElicitationTimeout},
		{"whitespace is trimmed", "  30  ", 30 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(envMCPElicitationTimeoutSec, tt.env)
			if got := configuredElicitationTimeout(); got != tt.want {
				t.Fatalf("configuredElicitationTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestAnOverCapFormReachesTheFallbackAsARefusal pins T-45.1-29 on the new path:
// an over-cap message is not cut down and shown, it is refused, and the fallback
// learns why without any of the server's text.
func TestAnOverCapFormReachesTheFallbackAsARefusal(t *testing.T) {
	t.Parallel()
	seen := make(chan elicit.Question, 1)
	res := callHandler(t, "fixture", &fakeConsent{action: elicit.ActionAccept, seen: seen},
		&sdkmcp.ElicitParams{Message: strings.Repeat("z", elicit.MaxMessageBytes+1)})
	if res.Action != elicit.ActionDecline {
		t.Fatalf("action = %q, want decline even though the fallback would accept", res.Action)
	}
	if q := <-seen; q.Refusal != elicit.RefusalUnrenderable || q.Message != "" || q.Server != "fixture" {
		t.Fatalf("fallback saw %+v", q)
	}
}

// elicitingServer builds an in-memory pair whose one tool asks for input on its
// FIRST call and answers plainly thereafter.
//
// It does not call ServerSession.Elicit. On protocol 2026-07-28 that is refused
// outright — "elicitation/create cannot be sent while serving a request ...
// return an InputRequests map instead (multi round-trip requests, SEP-2322)".
// The live path is the one the plan named: the server returns InputRequests, and
// clientMultiRoundTripMiddleware calls fulfillInputRequests (go-sdk@v1.7.0
// mcp/mrtr.go:233-268), which dispatches *ElicitParams to the client's handler.
func elicitingServer(t *testing.T, opts mcp.SessionOptions) *sdkmcp.ClientSession {
	t.Helper()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	asked := false
	tool := mustTool("needs_input", "Asks for input once.",
		map[string]any{"type": "object", "properties": map[string]any{}}, nil)
	server.AddTool(tool, func(_ context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		if asked {
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "done"}}}, nil
		}
		asked = true
		return &sdkmcp.CallToolResult{InputRequests: sdkmcp.InputRequestMap{
			"who": &sdkmcp.ElicitParams{
				Mode:    "form",
				Message: "what is your name",
				RequestedSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{"name": map[string]any{"type": "string", "description": "your name"}},
					"required":   []any{"name"},
				},
			},
		}}, nil
	})

	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	session, err := connectClient(ctx, clientTransport, opts)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// TestElicitationReachesHandlerOverARealSession drives a real multi-round-trip
// tool call through an in-memory pair, so the capability advertisement and the
// SDK's MRTR path are exercised rather than assumed.
func TestElicitationReachesHandlerOverARealSession(t *testing.T) {
	seen := make(chan elicit.Question, 1)
	consent := &fakeConsent{action: elicit.ActionDecline, seen: seen}
	session := elicitingServer(t, mcp.SessionOptions{
		Elicitation: NewElicitationHandler("fixture", consent),
	})

	if _, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "needs_input"}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	select {
	case req := <-seen:
		if req.Server != "fixture" {
			t.Fatalf("server = %q, want fixture — every operator-facing projection names the asking server", req.Server)
		}
		if len(req.Fields) != 1 || req.Fields[0].Name != "name" || !req.Fields[0].Required || req.Fields[0].Kind != elicit.KindString {
			t.Fatalf("fields = %+v, want one required string field named 'name'", req.Fields)
		}
		if req.Message != "what is your name" {
			t.Fatalf("message = %q, want the server's own text", req.Message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the consent surface was never reached over a real session")
	}
}

// TestNilElicitationHandlerDoesNotAdvertiseTheCapability is the other half of
// the posture: a mount with no consent surface keeps today's honest refusal,
// where the SDK answers CodeInvalidParams on Aura's behalf and the call fails
// rather than silently proceeding.
func TestNilElicitationHandlerDoesNotAdvertiseTheCapability(t *testing.T) {
	session := elicitingServer(t, mcp.SessionOptions{})
	_, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "needs_input"})
	if err == nil {
		t.Fatal("CallTool succeeded with no elicitation handler; without a consent surface the ask must fail, not be silently fulfilled")
	}
	if !strings.Contains(err.Error(), "elicitation") {
		t.Fatalf("error = %v, want one naming elicitation", err)
	}
}

// TestElicitationHandlerForNilConsentReturnsNil pins the mount-side posture: a
// mount with no consent surface must produce a NIL handler, because the SDK
// tests ClientOptions.ElicitationHandler != nil to decide whether to advertise
// the capability. A non-nil closure wrapping a nil surface would advertise a
// capability that always declines — the lie plan 45.1-06 rejected as option C.
func TestElicitationHandlerForNilConsentReturnsNil(t *testing.T) {
	t.Parallel()
	if got := elicitationHandlerFor("fixture", nil); got != nil {
		t.Fatal("elicitationHandlerFor(nil) returned a non-nil handler; the capability would be advertised with nothing behind it")
	}
	if got := elicitationHandlerFor("fixture", &fakeConsent{action: elicit.ActionDecline}); got == nil {
		t.Fatal("elicitationHandlerFor with a real surface returned nil")
	}
}
