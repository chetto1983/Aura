package mcptools

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
)

func TestMemorySurfacePolicy_AliasKeepsIsolationAndHiddenSurface(t *testing.T) {
	// D-27 (bridge_deferral.go): this fixture mirrors all 11 server operations
	// and exposes exactly 3 model-facing tools. Only memory_recall is retrieval;
	// memory_upsert_fact and memory_batch remain separately classified writes.
	// The count is <= maxAlwaysLoadedMCPTools, so on a fresh budget it now earns an
	// always-loaded slot instead of the pre-amendment #123 unconditional
	// Deferred:true.
	resetLoadedSlotBudgetForTest()
	var capturedMeta map[string]any
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	// Raw path-specific reads and hygiene operations remain available to the
	// host and CLI, but the model receives one deterministic retrieval contract.
	for _, name := range []string{
		"graph_schema", "memory_digest", "memory_entities", "memory_facts_about",
		"memory_forget", "memory_merge_entities", "memory_reembed", "memory_search",
	} {
		server.AddTool(mustTool(name, "fixture", nil, &sdkmcp.ToolAnnotations{ReadOnlyHint: true}), trivialToolHandler)
	}
	server.AddTool(mustTool("memory_upsert_fact", "Store a fact.", nil, nil), trivialToolHandler)
	destructive := true
	server.AddTool(mustTool("memory_batch", "Apply memory changes atomically.", nil, &sdkmcp.ToolAnnotations{
		ReadOnlyHint: false, DestructiveHint: &destructive, IdempotentHint: true,
	}), trivialToolHandler)
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
		Sending:         sendingMiddleware(bridgePolicy{identityScoped: true}, "tenant-a"),
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
	// Every advertised tool mounts under the alias too: the memory surface controls
	// which tools ride in a manifest, never which ones exist.
	if len(bridged) != len(advertised) {
		t.Fatalf("memory alias mounted %d of %d advertised tools", len(bridged), len(advertised))
	}
	byName := make(map[string]tools.Tool, len(bridged))
	for _, tool := range bridged {
		byName[tool.Spec().Name] = tool
	}
	recall := byName["mem__memory_recall"]
	if recall == nil {
		t.Fatal("unified memory_recall retrieval tool is absent")
	}
	if got := recall.Spec().Name; got != "mem__memory_recall" {
		t.Fatalf("aliased model name = %q, want mem__memory_recall", got)
	}
	if recall.Spec().Mutating {
		t.Fatal("memory_recall is retrieval and must not be classified as a mutation")
	}
	for _, name := range []string{"mem__memory_upsert_fact", "mem__memory_batch"} {
		tool := byName[name]
		if tool == nil || !tool.Spec().Mutating {
			t.Fatalf("%s must remain a separately classified mutation, got %#v", name, tool)
		}
	}
	batch := byName["mem__memory_batch"]
	if !batch.Spec().Destructive || batch.Spec().ReplayPolicy != tools.ReplayToolResult {
		t.Fatalf("memory_batch risk/replay = destructive:%v replay:%q", batch.Spec().Destructive, batch.Spec().ReplayPolicy)
	}
	// Every read is mounted; the invariant worth keeping is which ones ride in EVERY
	// turn's manifest. Two do: memory_recall, which is the whole deep-read surface, and
	// memory_entities, the vocabulary listing a write has to consult first. The other
	// reads are deferred behind one tool_search rather than absent.
	retrievalOperations := make([]string, 0, 2)
	for _, tool := range bridged {
		if !tool.Spec().Mutating && !tool.Spec().Deferred {
			retrievalOperations = append(retrievalOperations, strings.TrimPrefix(tool.Spec().Name, "mem__"))
		}
	}
	sort.Strings(retrievalOperations)
	if !slices.Equal(retrievalOperations, []string{"memory_entities", "memory_recall"}) {
		t.Fatalf("always-loaded memory retrieval operations = %v, want memory_entities and memory_recall",
			retrievalOperations)
	}
	surfaceEvidence, err := json.Marshal(map[string]any{"retrieval_operations": retrievalOperations})
	if err != nil {
		t.Fatalf("encode memory surface evidence: %v", err)
	}
	t.Logf("AURA_AGENT_MEMORY_SURFACE_JSON=%s", surfaceEvidence)
	if recall.Spec().Deferred {
		t.Fatal("memory_recall is in the four-tool manifest core and must not be deferred (D-27)")
	}

	callCtx := identityctx.WithIdentityID(context.Background(), "tenant-a")
	callCtx = tools.WithToolCallContext(callCtx, "session", "call", t.TempDir(), 2048)
	// A stale argument is inert. The identity-bound OAuth session determines the
	// subject without proprietary metadata.
	if _, err := recall.Execute(callCtx, json.RawMessage(`{"query":"marker","user_identifier":"tenant-b"}`)); err != nil {
		t.Fatalf("execute aliased memory recall: %v", err)
	}
	if aura, ok := capturedMeta["aura"]; ok {
		t.Fatalf("aliased OAuth tool received proprietary Aura metadata: %v", aura)
	}
}

func TestMemoryBatchRisk(t *testing.T) {
	destructive := true
	mutating, gotDestructive := mcpToolRisk(
		bridgePolicy{recipeSource: mcp.SourceRecipeMemory},
		mustTool("memory_batch", "batch", nil, &sdkmcp.ToolAnnotations{
			ReadOnlyHint: false, DestructiveHint: &destructive, IdempotentHint: true,
		}),
	)
	if !mutating || !gotDestructive {
		t.Fatalf("memory_batch risk = (%v, %v), want mutating and destructive", mutating, gotDestructive)
	}
}
