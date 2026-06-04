package mcptools

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

// mailDefs models the mail-mcp footgun census (spike 001): two v1-allowed tools
// plus one destructive tool the D-20 allowlist must drop before adaptation.
func mailDefs() []mcp.ToolDef {
	return []mcp.ToolDef{
		{Name: "send_email", Description: "Send an email."},
		{Name: "fetch_emails", Description: "Fetch recent emails."},
		{Name: "delete_mailbox", Description: "Permanently delete a mailbox."},
	}
}

// TestMountAllowlistDeferred locks the two D-20 invariants the swarm relies on:
//  1. every bridged tool's Spec is Deferred==true after the flip (a 16+12-tool MCP
//     surface no longer floods the manifest into the 30-50-tool degradation zone);
//  2. a non-empty per-server allowlist drops footgun tools before adaptation, while
//     a nil/empty allowlist mounts all advertised tools (calculator back-compat).
func TestMountAllowlistDeferred(t *testing.T) {
	t.Run("allowlist drops footgun tools and bridged specs are Deferred", func(t *testing.T) {
		reg := tools.NewRegistry()
		srv := &fakeServer{defs: mailDefs()}
		names, err := Mount(context.Background(), reg, "mail", srv, []string{"send_email", "fetch_emails"})
		if err != nil {
			t.Fatalf("Mount: %v", err)
		}
		if len(names) != 2 {
			t.Fatalf("allowlist must register exactly the 2 allowed tools, got %v", names)
		}
		if _, ok := reg.Get("mail__delete_mailbox"); ok {
			t.Fatal("delete_mailbox must be dropped by the allowlist (D-20 footgun census)")
		}
		for _, want := range []string{"mail__send_email", "mail__fetch_emails"} {
			tool, ok := reg.Get(want)
			if !ok {
				t.Fatalf("%s not registered under the allowlist", want)
			}
			if !tool.Spec().Deferred {
				t.Errorf("%s must be Deferred:true after the D-20 flip", want)
			}
		}
	})

	t.Run("nil allowlist mounts all advertised tools", func(t *testing.T) {
		reg := tools.NewRegistry()
		srv := &fakeServer{defs: mailDefs()}
		names, err := Mount(context.Background(), reg, "mail", srv, nil)
		if err != nil {
			t.Fatalf("Mount: %v", err)
		}
		if len(names) != 3 {
			t.Fatalf("nil allowlist must mount all 3 tools (back-compat), got %v", names)
		}
		if _, ok := reg.Get("mail__delete_mailbox"); !ok {
			t.Fatal("nil allowlist must NOT filter — delete_mailbox should register")
		}
	})

	t.Run("empty allowlist mounts all advertised tools", func(t *testing.T) {
		reg := tools.NewRegistry()
		srv := &fakeServer{defs: mailDefs()}
		names, err := Mount(context.Background(), reg, "mail", srv, []string{})
		if err != nil {
			t.Fatalf("Mount: %v", err)
		}
		if len(names) != 3 {
			t.Fatalf("empty allowlist must mount all 3 tools (back-compat), got %v", names)
		}
	})
}

// TestMountServer_SpawnFailureLeavesRegistryClean asserts that when the MCP server
// command cannot be spawned, MountServer returns an error and registers nothing —
// a misconfigured server never half-wires the agent registry.
func TestMountServer_SpawnFailureLeavesRegistryClean(t *testing.T) {
	reg := tools.NewRegistry()
	closer, names, err := MountServer(context.Background(), reg, "bad",
		mcp.ServerConfig{Command: "aura-nonexistent-mcp-binary-xyz"}, nil)
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
