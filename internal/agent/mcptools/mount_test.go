package mcptools

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

// TestMount_ThreeToolServerEarnsAlwaysLoadedSlot was
// TestMountAllAdvertisedToolsDeferred before D-27 (bridge_deferral.go,
// PRD amendment #123): this fixture's 3 tools are exactly at
// maxAlwaysLoadedMCPTools, so on a fresh global slot budget the mount now earns
// an always-loaded slot and every one of its tools bridges Deferred:false,
// instead of the pre-amendment unconditional Deferred:true. Mount still
// registers all advertised tools unconditionally — that part of the name no
// longer matches what the test proves, hence the rename.
func TestMount_ThreeToolServerEarnsAlwaysLoadedSlot(t *testing.T) {
	resetLoadedSlotBudgetForTest()
	reg := tools.NewRegistry()
	srv, _ := newInMemoryMounted(t,
		mustTool("send_email", "Send an email.", nil, nil),
		mustTool("fetch_emails", "Fetch recent emails.", nil, nil),
		mustTool("delete_mailbox", "Permanently delete a mailbox.", nil, nil),
	)

	names, err := Mount(context.Background(), reg, "mail", srv)
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if len(names) != 3 {
		t.Fatalf("Mount must register all advertised tools, got %v", names)
	}
	for _, want := range []string{"mail__send_email", "mail__fetch_emails", "mail__delete_mailbox"} {
		tool, ok := reg.Get(want)
		if !ok {
			t.Fatalf("%s not registered", want)
		}
		if tool.Spec().Deferred {
			t.Errorf("%s: a 3-tool server (at the ceiling) on a fresh budget must be Deferred:false (D-27)", want)
		}
	}
}

// TestMountServer_SpawnFailureLeavesRegistryClean asserts that when the MCP
// server command cannot be spawned, MountServer returns an error and registers
// nothing: a misconfigured server never half-wires the agent registry.
func TestMountServer_SpawnFailureLeavesRegistryClean(t *testing.T) {
	reg := tools.NewRegistry()
	closer, names, err := MountServer(context.Background(), context.Background(), reg, "bad",
		mcp.ServerConfig{Command: "aura-nonexistent-mcp-binary-xyz"})
	if err == nil {
		t.Fatal("want spawn error for a missing binary")
	}
	if closer != nil {
		t.Fatal("on failure closer must be nil")
	}
	if names != nil {
		t.Fatalf("on failure names must be nil, got %v", names)
	}
	if len(reg.All()) != 0 {
		t.Fatalf("registry must stay empty on spawn failure, got %d tools", len(reg.All()))
	}
}
