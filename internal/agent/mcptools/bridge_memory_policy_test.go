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

func TestMemoryBridgePolicy_AliasKeepsIsolationAndHiddenSurface(t *testing.T) {
	var capturedMeta map[string]any
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	// Raw path-specific reads remain available to the host and CLI, but the
	// model receives one deterministic read contract.
	for _, name := range []string{"memory_reembed", "memory_search", "memory_digest"} {
		server.AddTool(mustTool(name, "fixture", nil, nil), trivialToolHandler)
	}
	recallSchema := map[string]any{"type": "object", "properties": map[string]any{
		"query": map[string]any{"type": "string"},
	}}
	server.AddTool(mustTool("memory_recall", "Recall memory.", recallSchema, &sdkmcp.ToolAnnotations{ReadOnlyHint: true}),
		func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
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
	// The alias case ("mem" != "memory") needs the memory-policy Sending
	// middleware EXPLICITLY, independent of the namespace label the SessionOptions
	// helper below has no way to infer — mirrors bridgeFromAdvertisedWithPolicy's
	// own explicit-policy call a few lines down.
	session, err := connectClient(ctx, clientTransport, mcp.SessionOptions{
		Sending:         sendingMiddleware(bridgePolicy{memory: true}),
		ToolListChanged: srv.onToolListChanged,
	})
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	srv.Attach(session)

	advertised, err := srv.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	// Bridge(ctx, "mem", srv) would derive bridgePolicy from the namespace
	// string alone (defaultBridgePolicy: memory = namespace=="memory"), which
	// "mem" fails — the alias case this test exists for needs the memory policy
	// EXPLICITLY, independent of the namespace label, so it goes through
	// bridgeFromAdvertisedWithPolicy directly instead of Bridge.
	bridged, err := bridgeFromAdvertisedWithPolicy("mem", srv, advertised, bridgePolicy{memory: true})
	if err != nil {
		t.Fatalf("bridgeFromAdvertisedWithPolicy: %v", err)
	}
	if len(bridged) != 1 {
		t.Fatalf("memory alias mounted %d tools, want only the unified recall tool", len(bridged))
	}
	recall := bridged[0]
	if got := recall.Spec().Name; got != "mem__memory_recall" {
		t.Fatalf("aliased model name = %q, want mem__memory_recall", got)
	}
	if !recall.Spec().Deferred {
		t.Fatal("memory alias recall must be deferred like every other bridged tool")
	}

	callCtx := identityctx.WithIdentityID(context.Background(), "tenant-a")
	callCtx = tools.WithToolCallContext(callCtx, "session", "call", t.TempDir(), 2048)
	// A stale/spoofed user_identifier in the ARGUMENTS must not affect which
	// identity reaches _meta — the bridge never reads or filters this argument.
	if _, err := recall.Execute(callCtx, json.RawMessage(`{"query":"marker","user_identifier":"tenant-b"}`)); err != nil {
		t.Fatalf("execute aliased memory recall: %v", err)
	}
	aura, _ := capturedMeta[mcp.MetaNamespaceAura].(map[string]any)
	if aura == nil || aura[mcp.MetaFieldUserIdentifier] != "tenant-a" {
		t.Fatalf("forwarded _meta.aura.user_identifier = %v, want authenticated tenant-a", aura)
	}
}
