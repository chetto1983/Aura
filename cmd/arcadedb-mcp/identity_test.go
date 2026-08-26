package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/arcadedb"
)

func reqWithIdentity(identity string) *mcp.CallToolRequest {
	return &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{},
		Extra:  &mcp.RequestExtra{TokenInfo: &auth.TokenInfo{UserID: identity}},
	}
}

func TestIdentityFromToken(t *testing.T) {
	cases := []struct {
		name   string
		req    *mcp.CallToolRequest
		wantID string
	}{
		{name: "nil request"},
		{name: "nil extra", req: &mcp.CallToolRequest{}},
		{name: "nil token", req: &mcp.CallToolRequest{Extra: &mcp.RequestExtra{}}},
		{name: "empty subject", req: reqWithIdentity("")},
		{name: "whitespace subject", req: reqWithIdentity("   ")},
		{name: "valid subject", req: reqWithIdentity(testIdentity), wantID: testIdentity},
		{name: "surrounding whitespace", req: reqWithIdentity("  " + testIdentity + "  "), wantID: testIdentity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := identityFromToken(tc.req)
			if tc.wantID == "" {
				if !errors.Is(err, errMissingOAuthSubject) {
					t.Fatalf("error = %v, want errMissingOAuthSubject", err)
				}
				return
			}
			if err != nil || got != tc.wantID {
				t.Fatalf("identityFromToken = (%q, %v), want (%q, nil)", got, err, tc.wantID)
			}
		})
	}
}

func TestIdentityFromTokenIgnoresClientMetadata(t *testing.T) {
	req := reqWithIdentity(testIdentity)
	req.Params.SetMeta(map[string]any{"tenant": "00000000-0000-0000-0000-000000000099"})
	got, err := identityFromToken(req)
	if err != nil || got != testIdentity {
		t.Fatalf("identityFromToken = (%q, %v), want authenticated subject %q", got, err, testIdentity)
	}
}

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
		if text, ok := part.(*mcp.TextContent); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}

func TestMemoryToolRefusesMissingOAuthSubject(t *testing.T) {
	spy := &spyTenantResolver{}
	server := newServer(&tenants{resolver: spy}, testClock, "")
	session := inMemoryIdentityServer(t, server)
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "memory_search", Arguments: map[string]any{"query": "q"},
	})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !res.IsError || !strings.Contains(resultText(res), "missing authenticated OAuth subject") {
		t.Fatalf("result = (isError=%v) %q", res.IsError, resultText(res))
	}
	if got := spy.callCount(); got != 0 {
		t.Fatalf("tenants.For called %d times, want 0", got)
	}
}

func TestMemoryToolResolvesTokenSubjectNotStaleArgument(t *testing.T) {
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
		t.Fatalf("resolved identity = %q, want token subject %q", captured.lastIdentity(), testIdentity)
	}
}

func TestMemoryToolRefusesStaleArgumentWithoutToken(t *testing.T) {
	spy := &spyTenantResolver{}
	server := newServer(&tenants{resolver: spy}, testClock, "")
	session := inMemoryIdentityServer(t, server)
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "memory_search", Arguments: map[string]any{"query": "q", "user_identifier": testIdentity},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("stale argument rescued a call with no OAuth subject")
	}
	if got := spy.callCount(); got != 0 {
		t.Fatalf("tenants.For called %d times, want 0", got)
	}
}

type capturingTenantResolver struct {
	client *arcadedb.Client
	mu     sync.Mutex
	last   string
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
