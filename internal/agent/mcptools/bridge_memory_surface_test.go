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

func TestMemorySurfacePolicy_AliasKeepsIsolationAndHiddenSurface(t *testing.T) {
	// D-27 (bridge_deferral.go): this fixture exposes exactly 1 model-facing
	// tool (memory_recall; the other 3 are hidden by bridgePolicy.modelFacing),
	// which is <= maxAlwaysLoadedMCPTools, so on a fresh budget it now earns an
	// always-loaded slot instead of the pre-amendment #123 unconditional
	// Deferred:true.
	resetLoadedSlotBudgetForTest()
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
	// Identity scope and surface curation are explicit and independent of the
	// alias, matching the managed mount path.
	session, err := connectClient(ctx, clientTransport, mcp.SessionOptions{
		Sending:         sendingMiddleware(bridgePolicy{identityScoped: true}),
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
	// string alone (defaultBridgePolicy: memorySurface = namespace=="memory"), which
	// "mem" fails â€” the alias case this test exists for needs the memory surface
	// EXPLICITLY, independent of the namespace label, so it goes through
	// bridgeFromAdvertisedWithPolicy directly instead of Bridge.
	bridged, err := bridgeFromAdvertisedWithPolicy("mem", srv, advertised, bridgePolicy{identityScoped: true, memorySurface: true})
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
	if recall.Spec().Deferred {
		t.Fatal("memory alias recall exposes only 1 model-facing tool (<= the 3-tool ceiling) and must earn an always-loaded slot on a fresh budget: Deferred must be false (D-27)")
	}

	callCtx := identityctx.WithIdentityID(context.Background(), "tenant-a")
	callCtx = tools.WithToolCallContext(callCtx, "session", "call", t.TempDir(), 2048)
	// A stale/spoofed user_identifier in the ARGUMENTS must not affect which
	// identity reaches _meta â€” the bridge never reads or filters this argument.
	if _, err := recall.Execute(callCtx, json.RawMessage(`{"query":"marker","user_identifier":"tenant-b"}`)); err != nil {
		t.Fatalf("execute aliased memory recall: %v", err)
	}
	aura, _ := capturedMeta[mcp.MetaNamespaceAura].(map[string]any)
	if aura == nil || aura[mcp.MetaFieldUserIdentifier] != "tenant-a" {
		t.Fatalf("forwarded _meta.aura.user_identifier = %v, want authenticated tenant-a", aura)
	}
}
