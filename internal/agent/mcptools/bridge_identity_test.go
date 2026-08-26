package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
)

// TestBridgedOAuthToolUsesNoProprietaryTenantMetadata proves tenant selection
// is no longer encoded in tool arguments or MCP metadata.
func TestBridgedOAuthToolUsesNoProprietaryTenantMetadata(t *testing.T) {
	var capturedMeta map[string]any
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	memTool := mustTool("memory_upsert_fact", "Store a fact.",
		map[string]any{"type": "object", "properties": map[string]any{
			"subject": map[string]any{"type": "string"}, "predicate": map[string]any{"type": "string"},
			"object_value": map[string]any{"type": "string"},
		}}, nil)
	server.AddTool(memTool, func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		capturedMeta = req.Params.GetMeta()
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
	session, err := connectClient(ctx, clientTransport, mcp.SessionOptions{
		Sending:         sendingMiddleware(bridgePolicy{identityScoped: true}, "identity-1"),
		ToolListChanged: srv.onToolListChanged,
	})
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

	// A stale argument from rehydrated history is inert. The OAuth session bearer,
	// bound to identity-1 by the sending middleware, selects the remote subject.
	if _, err := got[0].Execute(callCtx, json.RawMessage(`{"subject":"a","predicate":"b","object_value":"c","user_identifier":"spoofed"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if aura, ok := capturedMeta["aura"]; ok {
		t.Fatalf("OAuth tool received proprietary Aura metadata: %v", aura)
	}
}

// TestBridgedRemoteToolRejectsNoPrincipal proves a missing authenticated owner
// fails before the remote handler is invoked.
func TestBridgedRemoteToolRejectsNoPrincipal(t *testing.T) {
	var capturedMeta map[string]any
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	recallTool := mustTool("memory_recall", "Recall memory.",
		map[string]any{"type": "object", "properties": map[string]any{
			"query": map[string]any{"type": "string"},
		}}, nil)
	server.AddTool(recallTool, func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		capturedMeta = req.Params.GetMeta()
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
	session, err := connectClient(ctx, clientTransport, mcp.SessionOptions{
		Sending:         sendingMiddleware(bridgePolicy{identityScoped: true}, "identity-1"),
		ToolListChanged: srv.onToolListChanged,
	})
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

	if _, err := got[0].Execute(callCtx, json.RawMessage(`{"query":"anything"}`)); !errors.Is(err, errMissingRemoteIdentity) {
		t.Fatalf("Execute error = %v, want %v", err, errMissingRemoteIdentity)
	}
	if capturedMeta != nil {
		t.Fatalf("remote handler ran without a principal: meta = %v", capturedMeta)
	}
}

// TestBridgedUnscopedToolDoesNotInjectTenantData proves an unscoped bridge
// never stamps an identity in metadata or arguments.
func TestBridgedUnscopedToolDoesNotInjectTenantData(t *testing.T) {
	var capturedArgs map[string]any
	var capturedMeta map[string]any
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	server.AddTool(sandboxTools()[0], func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		_ = json.Unmarshal(req.Params.Arguments, &capturedArgs)
		capturedMeta = req.Params.GetMeta()
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
	session, err := connectClient(ctx, clientTransport, mcp.SessionOptions{
		Sending:         sendingMiddleware(bridgePolicy{}, ""),
		ToolListChanged: srv.onToolListChanged,
	})
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
		t.Fatalf("non-memory tool received user_identifier in Arguments: %+v", capturedArgs)
	}
	if aura, ok := capturedMeta["aura"]; ok {
		t.Fatalf("non-memory tool received a _meta.aura namespace at all: %v", aura)
	}
}
