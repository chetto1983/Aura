package mcptools

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

// The memory MCP serves two consumers with different needs: Aura's own Go code
// (onboarding, recall, the `aura memory` CLI) calls it directly through
// mcp.Transport.CallTool, while the model's surface is built by this bridge.
// Removing a tool server-side to slim the model's menu breaks the host instead —
// that is what took onboarding down with "Unknown tool: memory_get_facts". The
// tools stay on the server; the bridge decides what the model sees.
func TestBridgeHidesNonModelFacingMemoryTools(t *testing.T) {
	t.Parallel()

	// The REAL surface of cmd/arcadedb-mcp. This fixture used to enumerate the previous
	// sidecar's tools — memory_add_fact, memory_get_entity, memory_update,
	// memory_create_relationship, memory_store_message and friends — none of which the
	// ArcadeDB server implements. A hiding test whose hidden names can never arrive
	// proves the filter compiles, not that it filters.
	all := []string{
		// model-facing: one read plan plus three mutation intentions.
		"memory_recall", "memory_upsert_fact", "memory_merge_entities", "memory_forget",
		// host-facing: active CLI/context/graph/readiness operations, absent from the model.
		"memory_search", "memory_facts_about", "memory_entities", "memory_digest",
		"graph_schema", "memory_reembed",
	}
	defs := make([]mcp.ToolDef, 0, len(all))
	for _, n := range all {
		defs = append(defs, mcp.ToolDef{Name: n, Description: "fixture"})
	}

	bridged, err := Bridge(context.Background(), "memory", &fakeServer{defs: defs})
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	got := make(map[string]struct{}, len(bridged))
	for _, tool := range bridged {
		got[tool.Spec().Name] = struct{}{}
	}

	for _, want := range []string{
		"memory__memory_recall", "memory__memory_upsert_fact",
		"memory__memory_merge_entities", "memory__memory_forget",
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("%s must reach the model", want)
		}
	}
	for _, hidden := range []string{
		"memory__memory_search", "memory__memory_facts_about", "memory__memory_entities",
		"memory__memory_digest", "memory__graph_schema", "memory__memory_reembed",
	} {
		if _, ok := got[hidden]; ok {
			t.Errorf("%s must NOT reach the model", hidden)
		}
	}
	if len(got) != 4 {
		t.Errorf("bridged %d memory tools, want 4", len(got))
	}
}

// Hiding is scoped to the namespace that asked for it. Another server's tools must
// pass through untouched, including any that happen to share a name.
func TestBridgeHidingIsNamespaceScoped(t *testing.T) {
	t.Parallel()

	defs := []mcp.ToolDef{
		{Name: "memory_get_facts", Description: "same name, different server"},
		{Name: "anything", Description: "fixture"},
	}
	bridged, err := Bridge(context.Background(), "pim", &fakeServer{defs: defs})
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	if len(bridged) != 2 {
		t.Fatalf("bridged %d tools from a non-memory namespace, want 2", len(bridged))
	}
}
