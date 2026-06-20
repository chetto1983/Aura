package main

import (
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

func TestGovernanceMCPBoardIncludesApplianceSidecars(t *testing.T) {
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
			t.Fatalf("MCP board missing appliance sidecar recipe %q: %#v", name, doc.MCPServers)
		}
		if server.Source != "recipe:"+name {
			t.Fatalf("%s source = %q, want recipe:%s", name, server.Source, name)
		}
	}
}
