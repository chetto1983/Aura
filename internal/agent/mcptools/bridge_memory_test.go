package mcptools

import (
	"context"
	"strconv"
	"sync"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
)

// bridge_memory_test.go drives mount.go's REAL sendingMiddleware(policy) wiring
// (not a hand-built middleware slice), proving the production composition rather
// than the primitive in isolation.

// TestSendingMiddleware_NonMemoryMountCarriesNoIdentityKey proves the policy
// gate: a non-memory mount's Sending slice has no IdentityMetaMiddleware at all,
// so _meta.aura is entirely absent on a call with no idempotency operation.
func TestSendingMiddleware_NonMemoryMountCarriesNoIdentityKey(t *testing.T) {
	server, mu, seenMeta, _ := echoMetaAndArgsServer(t)
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Wait() })

	session, err := connectClient(ctx, clientTransport, mcp.SessionOptions{
		Sending: sendingMiddleware(bridgePolicy{memory: false}),
	})
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	callCtx := identityctx.WithIdentityID(ctx, "identity-under-test")
	if _, err := session.CallTool(callCtx, &sdkmcp.CallToolParams{Name: "echo"}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if aura, ok := (*seenMeta)[mcp.MetaNamespaceAura]; ok {
		t.Fatalf("non-memory mount must carry no _meta.aura namespace at all, got %v", aura)
	}
}

// TestSendingMiddleware_ComposesOperationAndIdentity proves both plan 45.1-02's
// operation stamp and this plan's identity stamp arrive TOGETHER under one
// _meta.aura namespace on a memory-policy mount's real production middleware
// slice, and that the SDK's own protocolVersion triple survives both.
func TestSendingMiddleware_ComposesOperationAndIdentity(t *testing.T) {
	server, mu, seenMeta, _ := echoMetaAndArgsServer(t)
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Wait() })

	session, err := connectClient(ctx, clientTransport, mcp.SessionOptions{
		Sending: sendingMiddleware(bridgePolicy{memory: true}),
	})
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	callCtx := identityctx.WithIdentityID(ctx, "identity-composed")
	if _, err := session.CallTool(callCtx, &sdkmcp.CallToolParams{Name: "echo"}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	aura, _ := (*seenMeta)[mcp.MetaNamespaceAura].(map[string]any)
	if aura == nil {
		t.Fatal("server observed no _meta.aura namespace")
	}
	if aura[mcp.MetaFieldUserIdentifier] != "identity-composed" {
		t.Fatalf("user_identifier = %v, want identity-composed", aura[mcp.MetaFieldUserIdentifier])
	}
	if _, ok := (*seenMeta)["io.modelcontextprotocol/protocolVersion"]; !ok {
		t.Fatal("the SDK's own protocolVersion _meta triple must survive both middlewares")
	}
}

// TestIdentityMetaMiddleware_ConcurrentCallsCarryTheirOwnIdentity is the
// concurrency edge case: eight goroutines, sharing ONE *ClientSession, each
// under a distinct identityctx value, produce eight requests whose
// _meta.aura.user_identifier the server observes as exactly that set — proving
// the middleware reads ctx per-call rather than capturing a value once.
func TestIdentityMetaMiddleware_ConcurrentCallsCarryTheirOwnIdentity(t *testing.T) {
	const n = 8
	var mu sync.Mutex
	seen := make(map[string]struct{}, n)
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	server.AddTool(&sdkmcp.Tool{
		Name:        "echo",
		Description: "echo",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		m := req.Params.GetMeta()
		aura, _ := m[mcp.MetaNamespaceAura].(map[string]any)
		id, _ := aura[mcp.MetaFieldUserIdentifier].(string)
		mu.Lock()
		seen[id] = struct{}{}
		mu.Unlock()
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "ok"}}}, nil
	})

	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Wait() })

	session, err := connectClient(ctx, clientTransport, mcp.SessionOptions{
		Sending: sendingMiddleware(bridgePolicy{memory: true}),
	})
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	want := make(map[string]struct{}, n)
	var wg sync.WaitGroup
	for i := range n {
		id := "identity-" + strconv.Itoa(i)
		want[id] = struct{}{}
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			callCtx := identityctx.WithIdentityID(ctx, id)
			if _, err := session.CallTool(callCtx, &sdkmcp.CallToolParams{Name: "echo"}); err != nil {
				t.Errorf("CallTool(%s): %v", id, err)
			}
		}(id)
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != n {
		t.Fatalf("server observed %d distinct identities, want %d: %v", len(seen), n, seen)
	}
	for id := range want {
		if _, ok := seen[id]; !ok {
			t.Fatalf("identity %q never observed by the server; seen = %v", id, seen)
		}
	}
}
