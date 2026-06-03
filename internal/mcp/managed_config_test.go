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

func boolPtr(v bool) *bool { return &v }
