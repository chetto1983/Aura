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
// that is what took onboarding down with "Unknown tool: memory_get_facts".
//
// The bridge no longer decides what the model SEES, only what rides in every turn's
// manifest. It used to skip eight of the eleven, and skipping is not deferral: those
// tools were absent from the model's world and tool_search could not reach them, so the
// memory-aura skill's instruction to read memory_entities before writing named a tool
// that did not exist. Read back from the live database on 2026-09-03, the agent had
// answered every memory question with memory_recall because that was one of the only
// three it held.
func TestBridgeMountsEveryMemoryToolAndDefersAllButTheCore(t *testing.T) {
	// Not parallel, and the budget is reset: this asserts which tools ride in the
	// manifest, which depends on the mount winning one of the global slots.
	resetLoadedSlotBudgetForTest()

	// The REAL surface of cmd/arcadedb-mcp.
	all := []string{
		// the manifest core: the four with no substitute among the rest.
		"memory_recall", "memory_upsert_fact", "memory_batch", "memory_entities",
		// deferred, not absent: recall answers what search and facts_about answer,
		// batch subsumes forget and merge, the runner injects the digest, and the
		// last three are operator maintenance.
		"memory_search", "memory_facts_about", "memory_digest",
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
	manifest := make(map[string]bool, 4)
	for _, tool := range bridged {
		got[tool.Spec().Name] = struct{}{}
		if !tool.Spec().Deferred {
			manifest[tool.Spec().Name] = true
		}
	}

	if len(got) != len(all) {
		t.Errorf("bridged %d memory tools, want all %d — a tool the model cannot see is "+
			"worse than one it has to search for", len(got), len(all))
	}
	for _, name := range all {
		if _, ok := got["memory__"+name]; !ok {
			t.Errorf("memory__%s never reached the model", name)
		}
	}
	if len(manifest) != 4 {
		t.Errorf("always-loaded memory surface = %v, want exactly the four-tool core", manifest)
	}
	for _, want := range []string{
		"memory__memory_recall", "memory__memory_upsert_fact",
		"memory__memory_batch", "memory__memory_entities",
	} {
		if !manifest[want] {
			t.Errorf("%s must ride in every turn's manifest", want)
		}
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
