package mcptools

import (
	"context"
	"encoding/json"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
)

func TestBridgedMemoryToolInjectsContextUserIdentifier(t *testing.T) {
	var capturedArgs map[string]any
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	memTool := mustTool("memory_add_fact", "Store a fact.",
		map[string]any{"type": "object", "properties": map[string]any{
			"subject": map[string]any{"type": "string"}, "predicate": map[string]any{"type": "string"},
			"object_value": map[string]any{"type": "string"}, "user_identifier": map[string]any{"type": "string"},
		}}, nil)
	server.AddTool(memTool, func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		_ = json.Unmarshal(req.Params.Arguments, &capturedArgs)
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "ok"}}}, nil
	})
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	srv := NewMountedServer("fixture", nil)
	session, err := connectClient(ctx, clientTransport, mcpSessionOptionsFor(srv))
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	srv.Attach(session)

	got, err := Bridge(ctx, "memory", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	callCtx := identityctx.WithIdentityID(context.Background(), "identity-1")
	callCtx = tools.WithToolCallContext(callCtx, "sess", "tc1", t.TempDir(), 2048)

	if _, err := got[0].Execute(callCtx, json.RawMessage(`{"subject":"a","predicate":"b","object_value":"c","user_identifier":"spoofed"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if capturedArgs["user_identifier"] != "identity-1" {
		t.Fatalf("user_identifier = %v, want authenticated identity", capturedArgs["user_identifier"])
	}
}

// TestBridgedMemoryToolFallsBackToOperatorWhenNoPrincipal guards the fail-open
// fix: the memory server runs an unscoped GLOBAL query when a call carries no
// user_identifier, so a no-principal (CLI/unauthenticated) memory call must be
// scoped to the seeded local operator identity rather than forwarded bare.
func TestBridgedMemoryToolFallsBackToOperatorWhenNoPrincipal(t *testing.T) {
	var capturedArgs map[string]any
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	recallTool := mustTool("memory_recall", "Recall memory.",
		map[string]any{"type": "object", "properties": map[string]any{
			"query": map[string]any{"type": "string"}, "user_identifier": map[string]any{"type": "string"},
		}}, nil)
	server.AddTool(recallTool, func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		_ = json.Unmarshal(req.Params.Arguments, &capturedArgs)
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "ok"}}}, nil
	})
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	srv := NewMountedServer("fixture", nil)
	session, err := connectClient(ctx, clientTransport, mcpSessionOptionsFor(srv))
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	srv.Attach(session)

	got, err := Bridge(ctx, "memory", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	// No identityctx.WithIdentityID — the no-principal path.
	callCtx := tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), 2048)

	if _, err := got[0].Execute(callCtx, json.RawMessage(`{"query":"anything"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if capturedArgs["user_identifier"] != identityctx.LocalOperatorIdentity {
		t.Fatalf("no-principal user_identifier = %v, want operator fallback %q (never unscoped)",
			capturedArgs["user_identifier"], identityctx.LocalOperatorIdentity)
	}
}

func TestBridgedNonMemoryToolDoesNotInjectUserIdentifier(t *testing.T) {
	var capturedArgs map[string]any
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	server.AddTool(sandboxTools()[0], func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		_ = json.Unmarshal(req.Params.Arguments, &capturedArgs)
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "ok"}}}, nil
	})
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	srv := NewMountedServer("fixture", nil)
	session, err := connectClient(ctx, clientTransport, mcpSessionOptionsFor(srv))
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	srv.Attach(session)

	got, err := Bridge(ctx, "sb", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	callCtx := identityctx.WithIdentityID(context.Background(), "identity-1")
	callCtx = tools.WithToolCallContext(callCtx, "sess", "tc1", t.TempDir(), 2048)

	if _, err := got[0].Execute(callCtx, json.RawMessage(`{"container_id":"abc"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, ok := capturedArgs["user_identifier"]; ok {
		t.Fatalf("non-memory tool received user_identifier: %+v", capturedArgs)
	}
}
