package mcptools

import (
	"context"
	"encoding/json"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
)

// TestBridgedMemoryToolInjectsContextUserIdentifier proves the identity the
// bridge forwards is the authenticated ctx identity, stamped in
// _meta.aura.user_identifier (D-108) — not a caller-supplied ARGUMENT, which no
// longer exists on any memory tool's schema and is never inspected by the bridge.
func TestBridgedMemoryToolInjectsContextUserIdentifier(t *testing.T) {
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
		Sending:         sendingMiddleware(bridgePolicy{memory: true}),
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

	// A stale/spoofed user_identifier in the ARGUMENTS (rehydrated history, or an
	// adversarial payload) must never override the authenticated _meta identity —
	// the bridge no longer reads or filters this argument at all; it is inert.
	if _, err := got[0].Execute(callCtx, json.RawMessage(`{"subject":"a","predicate":"b","object_value":"c","user_identifier":"spoofed"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	aura, _ := capturedMeta[mcp.MetaNamespaceAura].(map[string]any)
	if aura == nil || aura[mcp.MetaFieldUserIdentifier] != "identity-1" {
		t.Fatalf("_meta.aura.user_identifier = %v, want identity-1 (the authenticated ctx identity)", aura)
	}
}

// TestBridgedMemoryToolFallsBackToOperatorWhenNoPrincipal guards the fail-open
// fix, preserved verbatim from withMemoryUserIdentifier's era: a no-principal
// (CLI/unauthenticated) memory call is scoped to the seeded local operator
// identity — never carried unscoped — but now via _meta rather than an argument.
func TestBridgedMemoryToolFallsBackToOperatorWhenNoPrincipal(t *testing.T) {
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
		Sending:         sendingMiddleware(bridgePolicy{memory: true}),
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

	if _, err := got[0].Execute(callCtx, json.RawMessage(`{"query":"anything"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	aura, _ := capturedMeta[mcp.MetaNamespaceAura].(map[string]any)
	if aura == nil || aura[mcp.MetaFieldUserIdentifier] != identityctx.LocalOperatorIdentity {
		t.Fatalf("no-principal _meta.aura.user_identifier = %v, want operator fallback %q (never unscoped)",
			aura, identityctx.LocalOperatorIdentity)
	}
}

// TestBridgedNonMemoryToolDoesNotInjectUserIdentifier proves a non-memory bridge
// mount never stamps an identity anywhere: not in _meta (no IdentityMetaMiddleware
// on its Sending slice at all) and not in Arguments (the bridge never touches them).
func TestBridgedNonMemoryToolDoesNotInjectUserIdentifier(t *testing.T) {
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
		Sending:         sendingMiddleware(bridgePolicy{memory: false}),
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
	if aura, ok := capturedMeta[mcp.MetaNamespaceAura]; ok {
		t.Fatalf("non-memory tool received a _meta.aura namespace at all: %v", aura)
	}
}
