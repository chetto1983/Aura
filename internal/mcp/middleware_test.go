package mcp

import (
	"context"
	"sync"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
)

// echoMetaServer builds a real in-memory MCP server with one tool whose handler
// records the _meta the SERVER received, so the middleware is asserted on what
// crossed the wire, not on what Aura's own struct holds.
func echoMetaServer(t *testing.T) (*sdkmcp.Server, *sync.Mutex, *map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var seen map[string]any
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	server.AddTool(&sdkmcp.Tool{
		Name:        "echo",
		Description: "echo",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		mu.Lock()
		seen = req.Params.GetMeta()
		mu.Unlock()
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "ok"}}}, nil
	})
	return server, &mu, &seen
}

// TestOperationMetaMiddleware_StampsOperationOnTheWire drives a real in-memory
// client/server pair: the assertion is on what the SERVER observed in _meta.aura,
// not on what the client's own CallToolParams struct holds — a middleware that
// mutates a copy would pass a struct-level test and fail here.
func TestOperationMetaMiddleware_StampsOperationOnTheWire(t *testing.T) {
	server, mu, seen := echoMetaServer(t)
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Wait() })

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "aura-test", Version: "0.0.1"}, nil)
	client.AddSendingMiddleware(OperationMetaMiddleware())
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	op := idempotency.Operation{
		Key:         idempotency.OperationKey{IdentityID: identityctx.LocalOperatorIdentity, Scope: idempotency.ScopeMCPTool, Key: "op-1"},
		Fingerprint: [32]byte{7},
	}
	opCtx, err := idempotency.WithOperation(identityctx.WithIdentityID(ctx, identityctx.LocalOperatorIdentity), op)
	if err != nil {
		t.Fatalf("WithOperation: %v", err)
	}

	if _, err := session.CallTool(opCtx, &sdkmcp.CallToolParams{Name: "echo"}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	aura, _ := (*seen)[MetaNamespaceAura].(map[string]any)
	if aura == nil {
		t.Fatal("server observed no _meta.aura namespace")
	}
	if aura[MetaFieldOperationKey] != "op-1" {
		t.Fatalf("operation_key = %v, want op-1", aura[MetaFieldOperationKey])
	}
	if aura[MetaFieldOperationScope] != string(idempotency.ScopeMCPTool) {
		t.Fatalf("operation_scope = %v, want %q", aura[MetaFieldOperationScope], idempotency.ScopeMCPTool)
	}
	want := idempotency.FingerprintHex(op.Fingerprint)
	if aura[MetaFieldOperationFingerprint] != want {
		t.Fatalf("operation_fingerprint = %v, want %q", aura[MetaFieldOperationFingerprint], want)
	}
}

// TestOperationMetaMiddleware_NoOperationLeavesMetaUntouched covers the no-op
// branch: a tools/call with no idempotency operation on its context reaches the
// server with no aura namespace at all.
func TestOperationMetaMiddleware_NoOperationLeavesMetaUntouched(t *testing.T) {
	server, mu, seen := echoMetaServer(t)
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Wait() })

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "aura-test", Version: "0.0.1"}, nil)
	client.AddSendingMiddleware(OperationMetaMiddleware())
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	if _, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "echo"}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if aura, ok := (*seen)[MetaNamespaceAura]; ok {
		t.Fatalf("no operation on ctx must leave _meta.aura absent, got %v", aura)
	}
}

// TestOperationMetaMiddleware_PassesThroughNonCallToolMethod covers the
// method!="tools/call" short-circuit directly against the exported Middleware
// type, without needing a live session.
func TestOperationMetaMiddleware_PassesThroughNonCallToolMethod(t *testing.T) {
	called := false
	next := func(ctx context.Context, method string, req sdkmcp.Request) (sdkmcp.Result, error) {
		called = true
		return nil, nil
	}
	mw := OperationMetaMiddleware()(next)
	if _, err := mw(context.Background(), "tools/list", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("a non tools/call method must pass through to next")
	}
}

// TestAuraMeta_MergesWithoutClobberingOtherKeys pre-seeds a foreign _meta key
// (the SDK's own io.modelcontextprotocol/protocolVersion shape) and asserts it
// survives the merge — proving the aura namespace is additive, not a replace.
func TestAuraMeta_MergesWithoutClobberingOtherKeys(t *testing.T) {
	params := &sdkmcp.CallToolParams{Name: "echo"}
	params.SetMeta(map[string]any{"io.modelcontextprotocol/protocolVersion": "2026-07-28"})

	aura := auraMeta(params)
	aura["operation_key"] = "k1"

	m := params.GetMeta()
	if m["io.modelcontextprotocol/protocolVersion"] != "2026-07-28" {
		t.Fatalf("foreign _meta key did not survive the merge, got %v", m)
	}
	got, _ := m[MetaNamespaceAura].(map[string]any)
	if got["operation_key"] != "k1" {
		t.Fatalf("aura namespace missing operation_key, got %v", got)
	}
}
