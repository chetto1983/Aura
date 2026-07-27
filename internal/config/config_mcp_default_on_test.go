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

// TestCalculatorContainerDefaultOn proves the Aura appliance mounts the
// warm-cached calculator recipe by default in-container, without requiring a
// prior `aura mcp install calculator`.
func TestCalculatorContainerDefaultOn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	clearMCPEnv(t, path)
	t.Setenv("AURA_IN_CONTAINER", "1")

	cfg := LoadDB()
	if cfg.MCPServersErr != nil {
		t.Fatalf("loadMCPServers err = %v, want nil", cfg.MCPServersErr)
	}
	got, ok := cfg.MCPPolicies["calculator"]
	if !ok {
		t.Fatalf("MCPPolicies missing calculator in Aura container: %#v", cfg.MCPPolicies)
	}
	want, ok := mcpmanager.LookupCatalog("calculator")
	if !ok {
		t.Fatal("LookupCatalog(\"calculator\") not found")
	}
	if !reflect.DeepEqual(got, want.Server) {
		t.Fatalf("calculator policy = %#v, want LookupCatalog(\"calculator\").Server %#v", got, want.Server)
	}
}

func TestCalculatorDefaultOn_RespectsDisable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	clearMCPEnv(t, path)
	t.Setenv("AURA_IN_CONTAINER", "1")

	disabled := false
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"calculator": {
			Command: "uvx",
			Args:    []string{"calculator-mcp-server", "--stdio"},
			Source:  "recipe:calculator",
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
	if _, ok := cfg.MCPPolicies["calculator"]; ok {
		t.Fatalf("calculator mounted despite explicit disable: %#v", cfg.MCPPolicies)
	}
}

// TestCalculatorContainerDefaultOn_EnvServersOverrideWins proves an
// AURA_MCP_SERVERS_JSON entry named "calculator" wins in-container — the inject
// lands AFTER the envServers delete loop and must not re-add the recipe default.
func TestCalculatorContainerDefaultOn_EnvServersOverrideWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	clearMCPEnv(t, path)
	t.Setenv("AURA_IN_CONTAINER", "1")
	t.Setenv(
		"AURA_MCP_SERVERS_JSON",
		`{"mcpServers":{"calculator":{"command":"my-calc","args":["--stdio"]}}}`,
	)

	cfg := LoadDB()
	if cfg.MCPServersErr != nil {
		t.Fatalf("loadMCPServers err = %v, want nil", cfg.MCPServersErr)
	}
	if _, ok := cfg.MCPPolicies["calculator"]; ok {
		t.Fatalf("calculator should not be a policy when overridden by AURA_MCP_SERVERS_JSON: %#v", cfg.MCPPolicies)
	}
	got, ok := cfg.MCPServers["calculator"]
	if !ok {
		t.Fatalf("AURA_MCP_SERVERS_JSON calculator override not in MCPServers: %#v", cfg.MCPServers)
	}
	if got.Command != "my-calc" {
		t.Fatalf("calculator command = %q, want my-calc (env override wins)", got.Command)
	}
}

// TestCalculatorContainerDefaultOn_RespectsExplicitInstall proves an
// operator-customized calculator entry wins in-container — the default-on inject
// sees the existing managed/policy row and does not overwrite the custom args.
func TestCalculatorContainerDefaultOn_RespectsExplicitInstall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	clearMCPEnv(t, path)
	t.Setenv("AURA_IN_CONTAINER", "1")

	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"calculator": {
			Command: "uvx",
			Args:    []string{"calculator-mcp-server", "--stdio", "--operator-flag"},
			Source:  "recipe:calculator",
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
	got, ok := cfg.MCPPolicies["calculator"]
	if !ok {
		t.Fatalf("explicit calculator install not mounted: %#v", cfg.MCPPolicies)
	}
	if len(got.Args) != 3 || got.Args[2] != "--operator-flag" {
		t.Fatalf("calculator args = %#v, want operator-customized (explicit wins)", got.Args)
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

// TestMemoryDefaultOn_RespectsProfileExclusion proves the CR-01 fix: an explicit
// memory entry (custom URL) excluded by the ACTIVE profile must stay unmounted —
// the inject must consult the unfiltered managed doc, never re-add memory at the
// catalog URL behind the operator's profile selection.
func TestMemoryDefaultOn_RespectsProfileExclusion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	clearMCPEnv(t, path)

	doc := mcp.ManagedConfig{
		ActiveProfile: "work",
		Profiles: map[string]mcp.ManagedProfile{
			"work": {Servers: []string{"other"}},
		},
		MCPServers: map[string]mcp.ManagedServer{
			"memory": {
				Type:   mcp.ServerTypeStreamableHTTP,
				URL:    "http://127.0.0.1:18091/mcp/",
				Source: "recipe:memory",
				Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
			},
			"other": {
				Type:   mcp.ServerTypeStreamableHTTP,
				URL:    "http://127.0.0.1:19000/mcp/",
				Source: "recipe:other",
				Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
			},
		},
	}
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("save managed config: %v", err)
	}

	cfg := LoadDB()
	if cfg.MCPServersErr != nil {
		t.Fatalf("loadMCPServers err = %v, want nil", cfg.MCPServersErr)
	}
	if got, ok := cfg.MCPPolicies["memory"]; ok {
		t.Fatalf("memory mounted (URL %q) despite active-profile exclusion (CR-01): %#v", got.URL, cfg.MCPPolicies)
	}
	if _, ok := cfg.MCPPolicies["other"]; !ok {
		t.Fatalf("profile-selected server missing: %#v", cfg.MCPPolicies)
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

// TestSidecarRecipesContainerDefaultOn proves the appliance mounts calendar and whatsapp
// out of the box, like calculator and memory already did.
//
// The regression it guards was invisible from the operator's seat: the cockpit's Connect
// panel pairs the device — scan the QR, WhatsApp reports linked — but pairing is not
// mounting, and nothing in that flow ran `aura mcp install whatsapp`. A live appliance sat
// with `paired: true` on the bridge and ZERO WhatsApp tools in the agent, with no error on
// either side to explain it. Calendar had the identical gap.
func TestSidecarRecipesContainerDefaultOn(t *testing.T) {
	for _, name := range []string{"calendar", "whatsapp"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "servers.json")
			clearMCPEnv(t, path)
			t.Setenv("AURA_IN_CONTAINER", "1")

			cfg := LoadDB()
			if cfg.MCPServersErr != nil {
				t.Fatalf("loadMCPServers err = %v, want nil", cfg.MCPServersErr)
			}
			got, ok := cfg.MCPPolicies[name]
			if !ok {
				t.Fatalf("MCPPolicies missing %s in the appliance container — a paired device with no tools is exactly the trap this closes: %#v", name, cfg.MCPPolicies)
			}
			want, ok := mcpmanager.LookupCatalog(name)
			if !ok {
				t.Fatalf("LookupCatalog(%q) not found", name)
			}
			if !reflect.DeepEqual(got, want.Server) {
				t.Fatalf("%s policy = %#v, want LookupCatalog(%q).Server %#v", name, got, name, want.Server)
			}
		})
	}
}

// TestSidecarRecipesNotDefaultOnOutsideContainer keeps a dev host clean: these recipes
// point at compose sidecars that only exist in the appliance stack, so mounting them on a
// laptop would just log a failed dial every boot.
func TestSidecarRecipesNotDefaultOnOutsideContainer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	clearMCPEnv(t, path)
	t.Setenv("AURA_IN_CONTAINER", "")

	cfg := LoadDB()
	if cfg.MCPServersErr != nil {
		t.Fatalf("loadMCPServers err = %v, want nil", cfg.MCPServersErr)
	}
	for _, name := range []string{"calendar", "whatsapp", "calculator"} {
		if _, ok := cfg.MCPPolicies[name]; ok {
			t.Fatalf("%s default-on outside the container: %#v", name, cfg.MCPPolicies)
		}
	}
	// Memory is the deliberate exception: a core capability on every host.
	if _, ok := cfg.MCPPolicies["memory"]; !ok {
		t.Fatalf("memory must stay default-on everywhere: %#v", cfg.MCPPolicies)
	}
}

// TestWhatsAppDefaultOn_RespectsDisable proves default-on never overrides the operator:
// an explicit `aura mcp disable whatsapp` still keeps it unmounted.
func TestWhatsAppDefaultOn_RespectsDisable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	clearMCPEnv(t, path)
	t.Setenv("AURA_IN_CONTAINER", "1")

	disabled := false
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"whatsapp": {
			Type:    mcp.ServerTypeStreamableHTTP,
			URL:     "http://whatsapp:8080/mcp",
			Source:  "recipe:whatsapp",
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
	if _, ok := cfg.MCPPolicies["whatsapp"]; ok {
		t.Fatalf("whatsapp mounted despite explicit disable: %#v", cfg.MCPPolicies)
	}
}
