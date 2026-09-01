package mcptools

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The memory MCP serves two consumers with different needs: Aura's own Go code
// (onboarding, recall, the `aura memory` CLI) calls it directly through
// MountedServer.CallToolText, while the model's surface is built by this bridge.
// Removing a tool server-side to slim the model's menu breaks the host instead —
// that is what took onboarding down with "Unknown tool: memory_get_facts". The
// tools stay on the server; the bridge decides what the model sees.
func TestBridgeHidesNonModelFacingMemoryTools(t *testing.T) {
	t.Parallel()

	// The REAL surface of cmd/arcadedb-mcp.
	all := []string{
		// model-facing: one read plan plus two mutation intentions.
		"memory_recall", "memory_upsert_fact", "memory_batch",
		// host-facing: active CLI/context/graph/readiness operations, absent from the model.
		"memory_search", "memory_facts_about", "memory_entities", "memory_digest",
		"graph_schema", "memory_reembed", "memory_merge_entities", "memory_forget",
	}
	toolDefs := make([]*sdkmcp.Tool, 0, len(all))
	for _, n := range all {
		toolDefs = append(toolDefs, mustTool(n, "fixture", nil, nil))
	}

	srv, _ := newInMemoryMounted(t, toolDefs...)
	bridged, err := Bridge(context.Background(), "memory", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	got := make(map[string]struct{}, len(bridged))
	for _, tool := range bridged {
		got[tool.Spec().Name] = struct{}{}
	}

	for _, want := range []string{
		"memory__memory_recall", "memory__memory_upsert_fact", "memory__memory_batch",
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("%s must reach the model", want)
		}
	}
	for _, hidden := range []string{
		"memory__memory_search", "memory__memory_facts_about", "memory__memory_entities",
		"memory__memory_digest", "memory__graph_schema", "memory__memory_reembed",
		"memory__memory_merge_entities", "memory__memory_forget",
	} {
		if _, ok := got[hidden]; ok {
			t.Errorf("%s must NOT reach the model", hidden)
		}
	}
	if len(got) != 3 {
		t.Errorf("bridged %d memory tools, want 3", len(got))
	}
}

// Hiding is scoped to the namespace that asked for it. Another server's tools
// must pass through untouched, including any that happen to share a name.
func TestBridgeHidingIsNamespaceScoped(t *testing.T) {
	t.Parallel()

	srv, _ := newInMemoryMounted(t,
		mustTool("memory_get_facts", "same name, different server", nil, nil),
		mustTool("anything", "fixture", nil, nil),
	)
	bridged, err := Bridge(context.Background(), "pim", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	if len(bridged) != 2 {
		t.Fatalf("bridged %d tools from a non-memory namespace, want 2", len(bridged))
	}
}
