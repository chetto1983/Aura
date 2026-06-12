package config

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

// clearMCPEnv zeroes the MCP env knobs so the default-on seam is exercised from a
// known baseline regardless of the host shell, then points AURA_MCP_CONFIG at path.
func clearMCPEnv(t *testing.T, path string) {
	t.Helper()
	t.Setenv("AURA_MCP_SERVERS_JSON", "")
	t.Setenv("AURA_AGENT_MEMORY_MCP_PORT", "")
	t.Setenv("AURA_MCP_CONFIG", path)
}

// TestMemoryDefaultOn proves a fresh machine (empty/absent AURA_MCP_CONFIG) mounts
// the memory recipe by default — no prior `aura mcp install` (D-08).
func TestMemoryDefaultOn(t *testing.T) {
	// Absent file: AURA_MCP_CONFIG points at a path that does not exist.
	path := filepath.Join(t.TempDir(), "servers.json")
	clearMCPEnv(t, path)

	cfg := LoadDB()
	if cfg.MCPServersErr != nil {
		t.Fatalf("loadMCPServers err = %v, want nil", cfg.MCPServersErr)
	}
	got, ok := cfg.MCPPolicies["memory"]
	if !ok {
		t.Fatalf("MCPPolicies missing memory on a fresh machine (default-on, D-08): %#v", cfg.MCPPolicies)
	}
	want, ok := mcpmanager.LookupCatalog("memory")
	if !ok {
		t.Fatal("LookupCatalog(\"memory\") not found — recipe must exist (Task 1)")
	}
	if !reflect.DeepEqual(got, want.Server) {
		t.Fatalf("memory policy = %#v, want LookupCatalog(\"memory\").Server %#v", got, want.Server)
	}
}

// TestMemoryDefaultOn_RespectsDisable proves `aura mcp disable memory`
// (Enabled=false in servers.json) keeps memory unmounted (D-09 respect disable).
func TestMemoryDefaultOn_RespectsDisable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	clearMCPEnv(t, path)

	disabled := false
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"memory": {
			Type:    mcp.ServerTypeStreamableHTTP,
			URL:     "http://127.0.0.1:8091/mcp/",
			Source:  "recipe:memory",
			Enabled: &disabled,
			Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
		},
	}}
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("save managed config: %v", err)
	}

	cfg := LoadDB()
	if cfg.MCPServersErr != nil {
		t.Fatalf("loadMCPServers err = %v, want nil", cfg.MCPServersErr)
	}
	if _, ok := cfg.MCPPolicies["memory"]; ok {
		t.Fatalf("memory mounted despite explicit disable: %#v", cfg.MCPPolicies)
	}
}

// TestMemoryDefaultOn_RespectsExplicitInstall proves an operator-customized memory
// server wins — the default-on inject does not override an explicit URL (D-08).
func TestMemoryDefaultOn_RespectsExplicitInstall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	clearMCPEnv(t, path)

	const customURL = "http://127.0.0.1:18091/mcp/"
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"memory": {
			Type:   mcp.ServerTypeStreamableHTTP,
			URL:    customURL,
			Source: "recipe:memory",
			Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
		},
	}}
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("save managed config: %v", err)
	}

	cfg := LoadDB()
	if cfg.MCPServersErr != nil {
		t.Fatalf("loadMCPServers err = %v, want nil", cfg.MCPServersErr)
	}
	got, ok := cfg.MCPPolicies["memory"]
	if !ok {
		t.Fatalf("explicit memory install not mounted: %#v", cfg.MCPPolicies)
	}
	if got.URL != customURL {
		t.Fatalf("memory URL = %q, want operator-customized %q (explicit wins)", got.URL, customURL)
	}
}

// TestMemoryDefaultOn_EnvServersOverrideWins proves an AURA_MCP_SERVERS_JSON entry
// named "memory" still wins — the inject lands AFTER the envServers delete loop.
func TestMemoryDefaultOn_EnvServersOverrideWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	clearMCPEnv(t, path)
	t.Setenv("AURA_MCP_SERVERS_JSON", `{"mcpServers":{"memory":{"command":"my-memory-binary","args":["--stdio"]}}}`)

	cfg := LoadDB()
	if cfg.MCPServersErr != nil {
		t.Fatalf("loadMCPServers err = %v, want nil", cfg.MCPServersErr)
	}
	if _, ok := cfg.MCPPolicies["memory"]; ok {
		t.Fatalf("memory should not be in policies when overridden by AURA_MCP_SERVERS_JSON: %#v", cfg.MCPPolicies)
	}
	got, ok := cfg.MCPServers["memory"]
	if !ok {
		t.Fatalf("AURA_MCP_SERVERS_JSON memory override not in MCPServers: %#v", cfg.MCPServers)
	}
	if got.Command != "my-memory-binary" {
		t.Fatalf("memory command = %q, want my-memory-binary (env override wins)", got.Command)
	}
}
