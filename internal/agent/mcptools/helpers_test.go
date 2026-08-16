package mcptools

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/mcp"
)

// helpers_test.go is the shared harness every test file in this package uses.
// sdkmcp.NewInMemoryTransports() yields a REAL *ClientSession over a net.Pipe(),
// so newInMemoryMounted binds a real client to a real in-process server — no
// hand-written mcptools test double exists anywhere in this package
// (D-103/RESEARCH Open Question #1, resolved by the operator).

// mustTool builds an *sdkmcp.Tool for a fixture server. A nil schema defaults to
// the empty-object schema every AddTool call requires.
func mustTool(name, description string, schema map[string]any, ann *sdkmcp.ToolAnnotations) *sdkmcp.Tool {
	if schema == nil {
		schema = map[string]any{"type": "object"}
	}
	return &sdkmcp.Tool{Name: name, Description: description, InputSchema: schema, Annotations: ann}
}

// trivialToolHandler answers every call with the tool's name and its raw
// arguments echoed back, which is enough for a caller to assert routing (which
// tool, which args) without any tool-specific scripting. Tests that need scripted
// behaviour register their own handler on the *sdkmcp.Server this helper returns
// (AddTool replaces an existing registration by name).
func trivialToolHandler(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{
		Text: req.Params.Name + ":" + string(req.Params.Arguments),
	}}}, nil
}

// connectClient builds an SDK client over transport using o exactly as
// mount.go's real production path does (mcp.SDKClientOptions + AddSendingMiddleware),
// so a fixture-driven test exercises the same construction shape production uses.
func connectClient(ctx context.Context, transport sdkmcp.Transport, o mcp.SessionOptions) (*sdkmcp.ClientSession, error) {
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "aura-test", Version: "0.0.1"}, mcp.SDKClientOptions(o))
	if len(o.Sending) > 0 {
		client.AddSendingMiddleware(o.Sending...)
	}
	return client.Connect(ctx, transport, nil)
}

// mcpSessionOptionsFor builds the SessionOptions a manually-constructed
// MountedServer needs wired before Attach, for tests that build their own
// server/client pair instead of going through newInMemoryMounted.
func mcpSessionOptionsFor(srv *MountedServer) mcp.SessionOptions {
	return mcp.SessionOptions{ToolListChanged: srv.onToolListChanged}
}

// newInMemoryMounted builds a real sdkmcp.NewServer, registers toolDefs with the
// trivial handler, pairs it over sdkmcp.NewInMemoryTransports(), connects a real
// client wrapped in a *MountedServer, and registers cleanup for both halves.
// Servers must connect before clients (net.Pipe has no listen queue), so the
// server side connects first.
func newInMemoryMounted(t *testing.T, toolDefs ...*sdkmcp.Tool) (*MountedServer, *sdkmcp.Server) {
	t.Helper()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	for _, tool := range toolDefs {
		server.AddTool(tool, trivialToolHandler)
	}

	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	var mounted *MountedServer
	// This fixture's transport is single-use (one net.Pipe half): a redial
	// attempt against it fails naturally rather than silently reconnecting to a
	// stale pipe. Tests exercising death/redial build their own richer fixture
	// (bridge_supervisor_test.go) that can produce a fresh transport pair.
	open := func(_, handshakeCtx context.Context, o mcp.SessionOptions) (*sdkmcp.ClientSession, error) {
		return connectClient(handshakeCtx, clientTransport, o)
	}
	mounted = NewMountedServer("fixture", open)
	session, err := open(ctx, ctx, mcp.SessionOptions{ToolListChanged: mounted.onToolListChanged})
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	mounted.Attach(session)
	return mounted, server
}
