package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
)

// bridge_user_identifier_test.go used to pin acceptsUserIdentifier's schema-probe
// rule, which withMemoryUserIdentifier needed to decide whether to inject the
// user_identifier ARGUMENT. D-108 deletes both: identity now travels only in
// _meta, unconditionally, on every identity-scoped remote mount — there is no schema to
// probe any more. These tests assert on what the SERVER received, not on what
// Aura's own struct holds (a middleware that mutates a copy would pass a
// struct-level assertion and fail on the wire).

// TestIdentityMetaMiddleware_PassesThroughNonCallToolMethod covers the
// method != "tools/call" short-circuit directly against the exported Middleware
// type, without needing a live session (mirrors middleware_test.go's identical
// coverage for OperationMetaMiddleware).
func TestIdentityMetaMiddleware_PassesThroughNonCallToolMethod(t *testing.T) {
	called := false
	next := func(ctx context.Context, method string, req sdkmcp.Request) (sdkmcp.Result, error) {
		called = true
		return nil, nil
	}
	mw := IdentityMetaMiddleware()(next)
	if _, err := mw(context.Background(), "tools/list", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("a non tools/call method must pass through to next")
	}
}

// TestIdentityMetaMiddleware_StampsCtxIdentityOnTheWire drives a real in-memory
// client/server pair: a call under identityctx.WithIdentityID arrives with
// exactly that identity in _meta.aura.user_identifier, and NO user_identifier
// key anywhere in the arguments.
func TestIdentityMetaMiddleware_StampsCtxIdentityOnTheWire(t *testing.T) {
	server, mu, seenMeta, seenArgs := echoMetaAndArgsServer(t)
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Wait() })

	session, err := connectClient(ctx, clientTransport, mcp.SessionOptions{
		Sending: sendingMiddleware(bridgePolicy{identityScoped: true}),
	})
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	callCtx := identityctx.WithIdentityID(ctx, "identity-under-test")
	if _, err := session.CallTool(callCtx, &sdkmcp.CallToolParams{
		Name: "echo", Arguments: map[string]any{"query": "q"},
	}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	aura, _ := (*seenMeta)[mcp.MetaNamespaceAura].(map[string]any)
	if aura == nil {
		t.Fatal("server observed no _meta.aura namespace")
	}
	if aura[mcp.MetaFieldUserIdentifier] != "identity-under-test" {
		t.Fatalf("user_identifier = %v, want identity-under-test", aura[mcp.MetaFieldUserIdentifier])
	}
	if _, ok := (*seenArgs)["user_identifier"]; ok {
		t.Fatalf("user_identifier leaked into params.Arguments: %#v", *seenArgs)
	}
}

// TestIdentityMetaMiddleware_RejectsCallWithNoCtxIdentity proves an unowned
// remote call fails before the server sees it.
func TestIdentityMetaMiddleware_RejectsCallWithNoCtxIdentity(t *testing.T) {
	server, mu, seenMeta, _ := echoMetaAndArgsServer(t)
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Wait() })

	session, err := connectClient(ctx, clientTransport, mcp.SessionOptions{
		Sending: sendingMiddleware(bridgePolicy{identityScoped: true}),
	})
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	if _, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "echo"}); !errors.Is(err, errMissingRemoteIdentity) {
		t.Fatalf("CallTool error = %v, want %v", err, errMissingRemoteIdentity)
	}

	mu.Lock()
	defer mu.Unlock()
	if *seenMeta != nil {
		t.Fatalf("server handled an unowned remote call: meta = %v", *seenMeta)
	}
}

// echoMetaAndArgsServer builds a real in-memory MCP server with one "echo" tool
// whose handler records both the _meta AND the arguments the SERVER received,
// so a test can assert identity arrives in _meta and NOWHERE in Arguments.
func echoMetaAndArgsServer(t *testing.T) (*sdkmcp.Server, *sync.Mutex, *map[string]any, *map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var seenMeta map[string]any
	var seenArgs map[string]any
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	server.AddTool(&sdkmcp.Tool{
		Name:        "echo",
		Description: "echo",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		mu.Lock()
		seenMeta = req.Params.GetMeta()
		var args map[string]any
		_ = json.Unmarshal(req.Params.Arguments, &args)
		seenArgs = args
		mu.Unlock()
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "ok"}}}, nil
	})
	return server, &mu, &seenMeta, &seenArgs
}
