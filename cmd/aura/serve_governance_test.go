package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

func TestGovernanceMCPBoardIncludesContainerRecipes(t *testing.T) {
	t.Setenv("AURA_IN_CONTAINER", "1")
	t.Setenv("AURA_MCP_CONFIG", filepath.Join(t.TempDir(), "servers.json"))
	t.Setenv("AURA_MCP_SERVERS_JSON", "")

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
	path := filepath.Join(t.TempDir(), "servers.json")
	t.Setenv("AURA_IN_CONTAINER", "1")
	t.Setenv("AURA_MCP_CONFIG", path)
	t.Setenv("AURA_MCP_SERVERS_JSON", "")

	providers := buildGovernanceProviders(&config.Config{}, nil, nil)
	if providers.MCP == nil {
		t.Fatal("MCP governance provider is nil")
	}
	if _, ok := providers.MCP.Servers().MCPServers["slack"]; ok {
		t.Fatal("slack is on the board before it was installed")
	}

	installed := `{"version":2,"mcpServers":{"slack":{"source":"custom","type":"streamable_http",` +
		`"url":"https://mcp.slack.com/mcp","trust":{"class":"blocked"}}}}`
	if err := os.WriteFile(path, []byte(installed), 0o600); err != nil {
		t.Fatalf("write managed config: %v", err)
	}

	server, ok := providers.MCP.Servers().MCPServers["slack"]
	if !ok {
		t.Fatal("board does not show a server installed after boot")
	}
	if server.URL != "https://mcp.slack.com/mcp" {
		t.Fatalf("slack url = %q, want the installed one", server.URL)
	}
}

// A managed config that cannot be read must not blank a board that was rendering a moment
// ago: the reload falls back to the boot snapshot rather than dropping every row.
func TestGovernanceMCPBoardFallsBackToBootWhenTheReloadFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "servers.json")
	t.Setenv("AURA_IN_CONTAINER", "1")
	t.Setenv("AURA_MCP_CONFIG", path)
	t.Setenv("AURA_MCP_SERVERS_JSON", "")

	providers := buildGovernanceProviders(&config.Config{}, nil, nil)
	if providers.MCP == nil {
		t.Fatal("MCP governance provider is nil")
	}
	if err := os.WriteFile(path, []byte("{ not json"), 0o600); err != nil {
		t.Fatalf("write broken config: %v", err)
	}

	if _, ok := providers.MCP.Servers().MCPServers["calendar"]; !ok {
		t.Fatal("a broken managed config emptied the board instead of serving the boot snapshot")
	}
}
