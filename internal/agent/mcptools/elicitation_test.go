package mcptools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

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
	seen    chan ElicitationRequest
}

func (f *fakeConsent) AskOperator(ctx context.Context, req ElicitationRequest) (string, map[string]any, error) {
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
			want:    elicitActionDecline,
		},
		{
			name:    "url mode is opt-out and never consults the surface",
			consent: &fakeConsent{action: elicitActionAccept, content: map[string]any{"leaked": true}},
			params:  &sdkmcp.ElicitParams{Mode: "url", URL: "https://evil.example/phish", Message: "click here"},
			want:    elicitActionDecline,
		},
		{
			name:    "url mode is matched case-insensitively",
			consent: &fakeConsent{action: elicitActionAccept},
			params:  &sdkmcp.ElicitParams{Mode: "URL", URL: "https://evil.example/phish"},
			want:    elicitActionDecline,
		},
		{
			name:    "surface returns an error",
			consent: &fakeConsent{err: errors.New("channel down")},
			params:  &sdkmcp.ElicitParams{Message: "hi"},
			want:    elicitActionDecline,
		},
		{
			name:    "surface panics",
			consent: &fakeConsent{panics: true},
			params:  &sdkmcp.ElicitParams{Message: "hi"},
			want:    elicitActionDecline,
		},
		{
			name:    "surface returns an unrecognised action",
			consent: &fakeConsent{action: "sure-why-not"},
			params:  &sdkmcp.ElicitParams{Message: "hi"},
			want:    elicitActionDecline,
		},
		{
			name:    "surface returns empty action",
			consent: &fakeConsent{action: ""},
			params:  &sdkmcp.ElicitParams{Message: "hi"},
			want:    elicitActionDecline,
		},
		{
			name:    "nil params",
			consent: &fakeConsent{action: elicitActionAccept},
			params:  nil,
			want:    elicitActionDecline,
		},
		{
			name:    "timeout <= 0 disables elicitation rather than waiting forever",
			consent: &fakeConsent{action: elicitActionAccept, block: blocked},
			params:  &sdkmcp.ElicitParams{Message: "hi"},
			env:     "0",
			want:    elicitActionDecline,
		},
		{
			name:    "negative timeout also disables",
			consent: &fakeConsent{action: elicitActionAccept, block: blocked},
			params:  &sdkmcp.ElicitParams{Message: "hi"},
			env:     "-5",
			want:    elicitActionDecline,
		},
		{
			name:    "surface declines",
			consent: &fakeConsent{action: elicitActionDecline},
			params:  &sdkmcp.ElicitParams{Message: "hi"},
			want:    elicitActionDecline,
		},
		{
			name:    "surface cancels",
			consent: &fakeConsent{action: elicitActionCancel},
			params:  &sdkmcp.ElicitParams{Message: "hi"},
			want:    elicitActionCancel,
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
			if res.Action != elicitActionAccept && res.Content != nil {
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
	res := callHandler(t, "fixture", &fakeConsent{action: elicitActionAccept, content: want}, &sdkmcp.ElicitParams{Message: "token?"})
	if res.Action != elicitActionAccept {
		t.Fatalf("action = %q, want accept", res.Action)
	}
	if res.Content["token"] != "abc" {
		t.Fatalf("content = %v, want %v", res.Content, want)
	}
}

// TestElicitationTimesOutToCancel pins T-45.1-30: a surface that ignores ctx
// cannot hold the in-flight agent turn open. 50ms bound, asserted to return well
// inside 200ms.
func TestElicitationTimesOutToCancel(t *testing.T) {
	blocked := make(chan struct{})
	t.Cleanup(func() { close(blocked) })
	t.Setenv(envMCPElicitationTimeoutSec, "1")

	// A surface that ignores ctx entirely — the worst case the bound exists for.
	consent := &fakeConsent{action: elicitActionAccept, block: blocked}
	start := time.Now()
	res, err := NewElicitationHandler("fixture", consent)(context.Background(), &sdkmcp.ElicitRequest{Params: &sdkmcp.ElicitParams{Message: "hi"}})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("handler returned a non-nil error: %v", err)
	}
	if res.Action != elicitActionCancel {
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
	res, err := NewElicitationHandler("fixture", &fakeConsent{action: elicitActionAccept, block: blocked})(ctx, &sdkmcp.ElicitRequest{Params: &sdkmcp.ElicitParams{Message: "hi"}})
	if err != nil {
		t.Fatalf("handler returned a non-nil error: %v", err)
	}
	if res.Action != elicitActionCancel {
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

// TestSummariseElicitationSchemaIsSortedAndCapped pins the two properties an
// operator-facing rendering needs: a stable order (map iteration is random, and
// a prompt that reorders every ask is unreadable) and a bound on server-authored
// description text (T-45.1-29).
func TestSummariseElicitationSchemaIsSortedAndCapped(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", maxMCPArgDescBytes*3)
	fields := summariseElicitationSchema(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"zebra":  map[string]any{"type": "string", "description": long},
			"alpha":  map[string]any{"type": "number", "description": "first"},
			"middle": map[string]any{"type": "boolean"},
		},
		"required": []any{"alpha", "zebra"},
	})
	if len(fields) != 3 {
		t.Fatalf("got %d fields, want 3: %+v", len(fields), fields)
	}
	if fields[0].Name != "alpha" || fields[1].Name != "middle" || fields[2].Name != "zebra" {
		t.Fatalf("fields are not sorted by name: %+v", fields)
	}
	if !fields[0].Required || fields[1].Required || !fields[2].Required {
		t.Fatalf("required flags wrong: %+v", fields)
	}
	if fields[1].Type != "boolean" {
		t.Fatalf("type = %q, want boolean", fields[1].Type)
	}
	if len(fields[2].Description) > maxMCPArgDescBytes {
		t.Fatalf("description is %d bytes, want <= %d", len(fields[2].Description), maxMCPArgDescBytes)
	}
}

func TestSummariseElicitationSchemaDegradesToNoFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  any
	}{
		{"nil schema", nil},
		{"not an object", "just a string"},
		{"no properties", map[string]any{"type": "object"}},
		{"empty properties", map[string]any{"type": "object", "properties": map[string]any{}}},
		{"over the byte cap", map[string]any{"type": "object", "properties": map[string]any{
			"padded": map[string]any{"type": "string", "description": strings.Repeat("y", maxMCPSchemaBytes+1)},
		}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := summariseElicitationSchema(tt.raw); got != nil {
				t.Fatalf("got %d fields, want none: %+v", len(got), got)
			}
		})
	}
}

// TestElicitationMessageIsByteCapped pins the other half of T-45.1-29: the
// server's free-text message is bounded before it can reach a human.
func TestElicitationMessageIsByteCapped(t *testing.T) {
	t.Parallel()
	seen := make(chan ElicitationRequest, 1)
	consent := &fakeConsent{action: elicitActionDecline, seen: seen}
	callHandler(t, "fixture", consent, &sdkmcp.ElicitParams{Message: strings.Repeat("z", maxMCPSummaryBytes*4)})
	req := <-seen
	if len(req.Message) > maxMCPSummaryBytes {
		t.Fatalf("message is %d bytes, want <= %d", len(req.Message), maxMCPSummaryBytes)
	}
	if req.Server != "fixture" {
		t.Fatalf("server = %q, want fixture — every operator-facing projection must name the asking server", req.Server)
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
	seen := make(chan ElicitationRequest, 1)
	consent := &fakeConsent{action: elicitActionDecline, seen: seen}
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
		if len(req.Fields) != 1 || req.Fields[0].Name != "name" || !req.Fields[0].Required {
			t.Fatalf("fields = %+v, want one required field named 'name'", req.Fields)
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
	if got := elicitationHandlerFor("fixture", &fakeConsent{action: elicitActionDecline}); got == nil {
		t.Fatal("elicitationHandlerFor with a real surface returned nil")
	}
}
