package mcp

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestManagedConfigPathUsesOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "servers.json")
	t.Setenv("AURA_MCP_CONFIG", want)

	got, err := ManagedConfigPath()
	if err != nil {
		t.Fatalf("ManagedConfigPath: %v", err)
	}
	if got != want {
		t.Fatalf("ManagedConfigPath = %q, want %q", got, want)
	}
}

func TestManagedConfigRoundTripFiltersDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	doc := ManagedConfig{MCPServers: map[string]ManagedServer{
		"calculator": {
			Command: "uvx",
			Args:    []string{"calculator-mcp-server", "--stdio"},
			Env:     []string{"PYTHONUNBUFFERED=1"},
		},
		"calendar": {
			Command: "dotnet",
			Args:    []string{"Calendar.Mcp.dll"},
			Enabled: boolPtr(false),
		},
	}}

	if err := SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("SaveManagedConfig: %v", err)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatalf("stat saved config: %v", err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("saved mode = %v, want 0600", info.Mode().Perm())
	}

	got, err := LoadManagedConfig(path)
	if err != nil {
		t.Fatalf("LoadManagedConfig: %v", err)
	}
	if !reflect.DeepEqual(got.MCPServers["calculator"].Args, doc.MCPServers["calculator"].Args) {
		t.Fatalf("calculator args = %#v, want %#v", got.MCPServers["calculator"].Args, doc.MCPServers["calculator"].Args)
	}
	enabled, err := got.EnabledServers()
	if err != nil {
		t.Fatalf("EnabledServers: %v", err)
	}
	if _, ok := enabled["calendar"]; ok {
		t.Fatal("disabled calendar server should not be returned")
	}
	if enabled["calculator"].Command != "uvx" {
		t.Fatalf("enabled calculator command = %q, want uvx", enabled["calculator"].Command)
	}
}

func TestManagedConfigDockerRuntimeDoesNotRequireLocalCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	doc := ManagedConfig{MCPServers: map[string]ManagedServer{
		"third-party": {
			Source: "manual",
			Trust:  ManagedTrust{Class: TrustSandboxedLocal},
			Runtime: ManagedRuntime{
				Kind:    RuntimeKindDocker,
				Image:   "example/mcp:1",
				Command: []string{"server", "--stdio"},
			},
		},
	}}

	if err := SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("SaveManagedConfig: %v", err)
	}
	got, err := LoadManagedConfig(path)
	if err != nil {
		t.Fatalf("LoadManagedConfig: %v", err)
	}
	if got.MCPServers["third-party"].Command != "" {
		t.Fatalf("local command = %q, want empty for docker runtime", got.MCPServers["third-party"].Command)
	}
}

func TestManagedConfigLegacyLoadsWithDefaultVersionAndProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	if err := os.WriteFile(path, []byte(`{
  "mcpServers": {
    "legacy": {
      "command": "node",
      "args": ["server.js"],
      "env": ["API_TOKEN=secret"]
    }
  }
}`), 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	got, err := LoadManagedConfig(path)
	if err != nil {
		t.Fatalf("LoadManagedConfig: %v", err)
	}
	if got.Version != 2 {
		t.Fatalf("Version = %d, want 2", got.Version)
	}
	if got.ActiveProfileName() != DefaultMCPProfile {
		t.Fatalf("ActiveProfileName = %q, want %q", got.ActiveProfileName(), DefaultMCPProfile)
	}
	if got.MCPServers["legacy"].Command != "node" {
		t.Fatalf("legacy command = %q, want node", got.MCPServers["legacy"].Command)
	}
}

func TestManagedConfigTrustDefaults(t *testing.T) {
	doc := ManagedConfig{MCPServers: map[string]ManagedServer{
		"recipe":  {Command: "uvx", Source: "recipe:mail"},
		"manual":  {Command: "npx", Source: "manual"},
		"trusted": {Command: "npx", Trust: ManagedTrust{Class: TrustTrustedLocal}},
	}}

	if got := doc.NormalizedTrust("recipe"); got != TrustTrustedRecipe {
		t.Fatalf("recipe trust = %q, want %q", got, TrustTrustedRecipe)
	}
	if got := doc.NormalizedTrust("manual"); got != TrustBlocked {
		t.Fatalf("manual trust = %q, want %q", got, TrustBlocked)
	}
	if got := doc.NormalizedTrust("trusted"); got != TrustTrustedLocal {
		t.Fatalf("trusted trust = %q, want %q", got, TrustTrustedLocal)
	}
}

func TestManagedConfigProfileMembership(t *testing.T) {
	doc := ManagedConfig{
		Profiles: map[string]ManagedProfile{
			"work": {Servers: []string{"calendar", "mail"}},
		},
		MCPServers: map[string]ManagedServer{
			"calendar": {Command: "calendar-mcp", Source: "recipe:calendar"},
			"mail":     {Command: "mail-mcp", Source: "recipe:mail"},
			"other":    {Command: "other-mcp", Source: "recipe:other"},
		},
	}

	got := doc.ProfileServerNames("work")
	want := []string{"calendar", "mail"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ProfileServerNames(work) = %#v, want %#v", got, want)
	}
}

func boolPtr(v bool) *bool { return &v }
