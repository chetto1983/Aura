package mcptools

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

// mailDefs models a mixed MCP server surface. Aura mounts all advertised tools and
// keeps them deferred in the agent registry.
func mailDefs() []mcp.ToolDef {
	return []mcp.ToolDef{
		{Name: "send_email", Description: "Send an email."},
		{Name: "fetch_emails", Description: "Fetch recent emails."},
		{Name: "delete_mailbox", Description: "Permanently delete a mailbox."},
	}
}

func TestMountAllAdvertisedToolsDeferred(t *testing.T) {
	reg := tools.NewRegistry()
	srv := &fakeServer{defs: mailDefs()}

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
		if !tool.Spec().Deferred {
			t.Errorf("%s must be Deferred:true", want)
		}
	}
}

// TestMountServer_SpawnFailureLeavesRegistryClean asserts that when the MCP server
// command cannot be spawned, MountServer returns an error and registers nothing: a
// misconfigured server never half-wires the agent registry.
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
