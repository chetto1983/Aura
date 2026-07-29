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

	all := []string{
		// model-facing: one verb per intention
		"memory_search", "memory_add_fact", "memory_add_preference",
		"memory_get_entity", "memory_update", "memory_forget",
		// hidden: still served, still callable by Aura, absent from the manifest
		"memory_store_message", "memory_get_context", "memory_get_conversation",
		"memory_list_sessions", "memory_add_entity", "memory_store_profile",
		"memory_create_relationship",
		"memory_get_facts",
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
		"memory__memory_search", "memory__memory_add_fact", "memory__memory_add_preference",
		"memory__memory_get_entity", "memory__memory_update", "memory__memory_forget",
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("%s must reach the model", want)
		}
	}
	for _, unwanted := range []string{
		"memory__memory_store_message", "memory__memory_get_context",
		"memory__memory_get_conversation", "memory__memory_list_sessions",
		"memory__memory_add_entity", "memory__memory_store_profile",
		"memory__memory_create_relationship",
		"memory__memory_get_facts",
	} {
		if _, ok := got[unwanted]; ok {
			t.Errorf("%s must NOT reach the model", unwanted)
		}
	}
	if len(got) != 6 {
		t.Errorf("bridged %d memory tools, want 6", len(got))
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
