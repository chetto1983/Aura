package main

import (
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/mcp"
)

func TestGovernanceMCPBoardIncludesContainerRecipes(t *testing.T) {
	t.Setenv("AURA_IN_CONTAINER", "1")
	withMemoryMCPRegistry(t)

	providers := buildGovernanceProviders(&config.Config{}, nil, nil)
	if providers.MCP == nil {
		t.Fatal("MCP governance provider is nil")
	}

	doc := providers.MCP.Servers()
	for _, name := range []string{"calendar", "whatsapp"} {
		server, ok := doc.MCPServers[name]
		if !ok {
			t.Fatalf("MCP board missing container recipe %q: %#v", name, doc.MCPServers)
		}
		if server.Source != "recipe:"+name {
			t.Fatalf("%s source = %q, want recipe:%s", name, server.Source, name)
		}
	}
}

// A server installed AFTER boot must appear on the board without a restart. The adapter used
// to answer from a snapshot taken at start-up, so the cockpit's own Install button wrote a
// server the cockpit then refused to list — the operator's only way out was restarting the
// daemon, and nothing said so.
func TestGovernanceMCPBoardSeesAServerInstalledAfterBoot(t *testing.T) {
	t.Setenv("AURA_IN_CONTAINER", "1")
	withMemoryMCPRegistry(t)

	providers := buildGovernanceProviders(&config.Config{}, nil, nil)
	if providers.MCP == nil {
		t.Fatal("MCP governance provider is nil")
	}
	if _, ok := providers.MCP.Servers().MCPServers["slack"]; ok {
		t.Fatal("slack is on the board before it was installed")
	}

	seedMCPRegistry(t, mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"slack": {Source: "custom", Type: mcp.ServerTypeStreamableHTTP, URL: "https://mcp.slack.com/mcp"},
	}})

	server, ok := providers.MCP.Servers().MCPServers["slack"]
	if !ok {
		t.Fatal("board does not show a server installed after boot")
	}
	if server.URL != "https://mcp.slack.com/mcp" {
		t.Fatalf("slack url = %q, want the installed one", server.URL)
	}
}

// A default-on recipe has no registry row until somebody customizes it, so a board that
// listed only installed servers dropped memory off the cockpit entirely while it went on
// mounting at every boot. The board answers from the same resolution the mount does.
func TestGovernanceMCPBoardShowsDefaultOnMemory(t *testing.T) {
	withMemoryMCPRegistry(t)

	providers := buildGovernanceProviders(&config.Config{}, nil, nil)
	server, ok := providers.MCP.Servers().MCPServers["memory"]
	if !ok {
		t.Fatalf("memory is missing from the board: %#v", providers.MCP.Servers().MCPServers)
	}
	if server.Source != mcp.SourceRecipeMemory {
		t.Fatalf("memory source = %q, want %q", server.Source, mcp.SourceRecipeMemory)
	}
}
