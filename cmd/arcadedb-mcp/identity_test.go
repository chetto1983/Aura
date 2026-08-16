package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/arcadedb"
	auramcp "github.com/chetto1983/aura/internal/mcp"
)

// reqWithIdentity builds a *mcp.CallToolRequest carrying identity in
// _meta.aura.user_identifier — the shape every handler test in this package
// needs post-D-108, since UserIdentifier is no longer an input struct field on
// any of the nine input types. Every other test file in this package (same
// package, `main`) reuses this helper instead of hand-rolling the _meta shape.
func reqWithIdentity(identity string) *mcp.CallToolRequest {
	return reqWithMeta(map[string]any{
		auraMetaKey: map[string]any{metaFieldUserIdentifier: identity},
	})
}

// reqWithMeta builds a *mcp.CallToolRequest carrying an arbitrary _meta shape,
// for the refusal-path tests that need something OTHER than a valid identity
// (absent, wrong-typed, or a foreign namespace entirely).
func reqWithMeta(meta map[string]any) *mcp.CallToolRequest {
	params := &mcp.CallToolParamsRaw{}
	if meta != nil {
		params.SetMeta(meta)
	}
	return &mcp.CallToolRequest{Params: params}
}

// TestMetaKeyConstantsMatchClient pins identity.go's own consts to
// internal/mcp's exported ones BY VALUE. This binary is a separate `main`
// package and cannot import internal/mcp for a shared constant without pulling
// agent-side policy into the memory sidecar (D-103), so the two pairs are
// pinned equal here instead. A silent divergence would refuse every call
// (fail-closed) rather than cross a tenant — but it would also take memory
// down, so it is worth catching at test time rather than live.
func TestMetaKeyConstantsMatchClient(t *testing.T) {
	if auraMetaKey != auramcp.MetaNamespaceAura {
		t.Fatalf("auraMetaKey = %q, want internal/mcp.MetaNamespaceAura = %q", auraMetaKey, auramcp.MetaNamespaceAura)
	}
	if metaFieldUserIdentifier != auramcp.MetaFieldUserIdentifier {
		t.Fatalf("metaFieldUserIdentifier = %q, want internal/mcp.MetaFieldUserIdentifier = %q",
			metaFieldUserIdentifier, auramcp.MetaFieldUserIdentifier)
	}
}

// TestIdentityFromMeta covers every <behavior> bullet the plan names for the
// fail-closed read itself, exhaustively and directly — before any server
// round-trip is involved.
func TestIdentityFromMeta(t *testing.T) {
	cases := []struct {
		name   string
		req    *mcp.CallToolRequest
		wantID string
	}{
		{"nil request", nil, ""},
		{"nil params", &mcp.CallToolRequest{}, ""},
		{"no _meta at all", reqWithMeta(nil), ""},
		{"_meta present, no aura namespace", reqWithMeta(map[string]any{"other": "x"}), ""},
		{"aura namespace wrong type", reqWithMeta(map[string]any{"aura": "not-a-map"}), ""},
		{"aura present, no user_identifier key", reqWithMeta(map[string]any{"aura": map[string]any{}}), ""},
		{"empty identity", reqWithMeta(map[string]any{"aura": map[string]any{"user_identifier": ""}}), ""},
		{"whitespace-only identity", reqWithMeta(map[string]any{"aura": map[string]any{"user_identifier": "   "}}), ""},
		{"non-string identity", reqWithMeta(map[string]any{"aura": map[string]any{"user_identifier": 42}}), ""},
		{"valid identity", reqWithIdentity(testIdentity), testIdentity},
		{"valid identity with surrounding whitespace trims", reqWithMeta(map[string]any{
			"aura": map[string]any{"user_identifier": "  " + testIdentity + "  "},
		}), testIdentity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := identityFromMeta(tc.req)
			if tc.wantID == "" {
				if err == nil {
					t.Fatalf("identityFromMeta(%s) = %q, nil; want errMissingIdentityMeta", tc.name, got)
				}
				if !errors.Is(err, errMissingIdentityMeta) {
					t.Fatalf("err = %v, want errMissingIdentityMeta", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("identityFromMeta(%s): unexpected error %v", tc.name, err)
			}
			if got != tc.wantID {
				t.Fatalf("identity = %q, want %q", got, tc.wantID)
			}
		})
	}
}

// spyTenantResolver counts For calls so "never resolved" is asserted rather
// than assumed (RESEARCH Pitfall 2 / the plan's threat register T-45.1-22): a
// refusal must never reach tenant resolution at all.
type spyTenantResolver struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (s *spyTenantResolver) For(_ context.Context, _ string) (*arcadedb.Client, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	return nil, errors.New("spyTenantResolver: no client configured")
}

func (s *spyTenantResolver) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// inMemoryIdentityServer connects srv to a real client over an in-memory
// transport pair and returns the client session — the vehicle every refusal
// test in this file uses to prove the WIRE outcome (IsError:true + text), not
// merely a Go-level return value a caller could get by skipping AddTool's
// generated wrapper.
func inMemoryIdentityServer(t *testing.T, srv *mcp.Server) *mcp.ClientSession {
	t.Helper()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "identity-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func resultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, part := range res.Content {
		if tc, ok := part.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// TestMemoryToolsRefuseMissingIdentity drives a REAL in-memory server (through
// mcp.AddTool's generated ToolHandlerFor wrapper, per this plan's
// <falsified_premise>) and proves every one of the four refusal shapes reaches
// the wire as IsError:true with the model-visible "missing required identity"
// text — and that tenants.For is NEVER called on any refusal path.
func TestMemoryToolsRefuseMissingIdentity(t *testing.T) {
	spy := &spyTenantResolver{}
	server := newServer(&tenants{resolver: spy}, testClock, "")
	session := inMemoryIdentityServer(t, server)
	ctx := context.Background()

	cases := []struct {
		name string
		meta map[string]any
	}{
		{"no _meta at all", nil},
		{"empty identity", map[string]any{"aura": map[string]any{"user_identifier": ""}}},
		{"whitespace-only identity", map[string]any{"aura": map[string]any{"user_identifier": "   "}}},
		{"non-string identity", map[string]any{"aura": map[string]any{"user_identifier": 7}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := &mcp.CallToolParams{Name: "memory_search", Arguments: map[string]any{"query": "q"}}
			if tc.meta != nil {
				params.SetMeta(tc.meta)
			}
			res, err := session.CallTool(ctx, params)
			if err != nil {
				t.Fatalf("CallTool transport error: %v", err)
			}
			if !res.IsError {
				t.Fatalf("IsError = false, want true (refusal); content = %s", resultText(res))
			}
			text := resultText(res)
			if !strings.Contains(text, "missing required identity") {
				t.Fatalf("text = %q, want it to contain %q", text, "missing required identity")
			}
		})
	}
	if got := spy.callCount(); got != 0 {
		t.Fatalf("tenants.For called %d times across refusal paths, want 0", got)
	}
}

// TestMemoryToolResolvesMetaIdentityNotStaleArgument is the adjacency edge
// case: a call whose ARGUMENTS carry a stale user_identifier (rehydrated
// history) and whose _meta carries a DIFFERENT, valid identity resolves the
// _meta identity — the argument is ignored outright, never merged, never
// preferred, never a fallback.
//
// Driven at the handler level, not through a full session.CallTool round trip:
// every memory tool's advertised schema now declares additionalProperties:false
// (mcp.AddTool's reflection-derived default) and no longer declares
// user_identifier at all, so a REAL wire call carrying that extra property is
// refused by SCHEMA VALIDATION before identityFromMeta or tenants.For ever run
// — an even stronger guarantee than "ignored", proven separately by
// TestMemoryToolRefusesStaleArgumentWithNoMeta's IsError:true. What this test
// isolates is identityFromMeta's OWN contract: it never reads req.Params.
// Arguments at all, so a stale value sitting there — however it got there — has
// nothing to override.
func TestMemoryToolResolvesMetaIdentityNotStaleArgument(t *testing.T) {
	client, _ := newRecordingDB(t, `{"result":[]}`)
	captured := &capturingTenantResolver{client: client}

	req := reqWithIdentity(testIdentity)
	req.Params.Arguments = []byte(`{"query":"q","user_identifier":"11111111-1111-1111-1111-111111111111"}`)

	_, _, err := memorySearchHandler(&tenants{resolver: captured})(
		context.Background(), req, MemorySearchInput{Query: "q"})
	if err != nil {
		t.Fatalf("memorySearchHandler: %v", err)
	}
	if captured.lastIdentity() != testIdentity {
		t.Fatalf("resolved identity = %q, want the _meta identity %q — a stale argument must never win",
			captured.lastIdentity(), testIdentity)
	}
}

// TestMemoryToolRefusesStaleArgumentWithNoMeta: arguments carrying
// user_identifier and _meta carrying NONE is refused — the stale argument does
// not rescue the call.
func TestMemoryToolRefusesStaleArgumentWithNoMeta(t *testing.T) {
	spy := &spyTenantResolver{}
	server := newServer(&tenants{resolver: spy}, testClock, "")
	session := inMemoryIdentityServer(t, server)
	ctx := context.Background()

	params := &mcp.CallToolParams{Name: "memory_search", Arguments: map[string]any{
		"query": "q", "user_identifier": testIdentity,
	}}
	res, err := session.CallTool(ctx, params)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true: a stale argument must not rescue a call with no _meta identity")
	}
	if got := spy.callCount(); got != 0 {
		t.Fatalf("tenants.For called %d times, want 0", got)
	}
}

// capturingTenantResolver records the identity it was resolved with and
// answers every call with the same recording client — used to prove WHICH
// identity a call actually resolved, not merely that resolution succeeded.
type capturingTenantResolver struct {
	client *arcadedb.Client

	mu   sync.Mutex
	last string
}

func (c *capturingTenantResolver) For(_ context.Context, identityID string) (*arcadedb.Client, error) {
	c.mu.Lock()
	c.last = identityID
	c.mu.Unlock()
	return c.client, nil
}

func (c *capturingTenantResolver) lastIdentity() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}
