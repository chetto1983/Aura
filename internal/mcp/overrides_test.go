package mcp

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadToolDescriptionOverridesBareMap(t *testing.T) {
	path := filepath.Join(t.TempDir(), ToolOverridesFilename)
	if err := os.WriteFile(path, []byte(`{"mcp_srv_search":"Search with project context"," ":"ignored","empty":""}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadToolDescriptionOverrides(path)
	if err != nil {
		t.Fatalf("LoadToolDescriptionOverrides: %v", err)
	}
	want := map[string]string{"mcp_srv_search": "Search with project context"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("overrides = %#v, want %#v", got, want)
	}
}

func TestLoadToolDescriptionOverridesWrappedDescriptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), ToolOverridesFilename)
	if err := os.WriteFile(path, []byte(`{"descriptions":{"srv_search":"Search with wrapper"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadToolDescriptionOverrides(path)
	if err != nil {
		t.Fatalf("LoadToolDescriptionOverrides: %v", err)
	}
	want := map[string]string{"srv_search": "Search with wrapper"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("overrides = %#v, want %#v", got, want)
	}
}

func TestLoadToolDescriptionOverridesMissingFileIsEmpty(t *testing.T) {
	got, err := LoadToolDescriptionOverrides(filepath.Join(t.TempDir(), ToolOverridesFilename))
	if err != nil {
		t.Fatalf("LoadToolDescriptionOverrides: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("overrides = %#v, want empty", got)
	}
}

func TestApplyToolDescriptionOverridesUpdatesClientsAndSurfacesTypos(t *testing.T) {
	client := &Client{name: "srv1", transportKind: TransportHTTP}
	client.setDiscoveredTools([]Tool{
		{Name: "search", Description: "Upstream search"},
		{Name: "list", Description: "Upstream list"},
	})

	unmatched := ApplyToolDescriptionOverrides([]ConnectedClient{client}, map[string]string{
		"mcp_srv1_search": "Search with local operator context",
		"missing_tool":    "Typo should warn",
	})
	if !reflect.DeepEqual(unmatched, []string{"missing_tool"}) {
		t.Fatalf("unmatched = %#v", unmatched)
	}
	if !reflect.DeepEqual(client.OverrideWarnings(), []string{"missing_tool"}) {
		t.Fatalf("warnings = %#v", client.OverrideWarnings())
	}
	tools := client.Tools()
	if tools[0].Description != "Search with local operator context" {
		t.Fatalf("description = %q", tools[0].Description)
	}

	ApplyToolDescriptionOverrides([]ConnectedClient{client}, nil)
	tools = client.Tools()
	if tools[0].Description != "Upstream search" {
		t.Fatalf("description after reset = %q", tools[0].Description)
	}
	if len(client.OverrideWarnings()) != 0 {
		t.Fatalf("warnings after reset = %#v", client.OverrideWarnings())
	}
}

func TestToolOverridesPathPrefersRuntimeWorkspace(t *testing.T) {
	got := ToolOverridesPath(filepath.Join("runtime", "workspace"), filepath.Join("data", "mcp.json"))
	want := filepath.Join("runtime", "workspace", ToolOverridesFilename)
	if got != want {
		t.Fatalf("ToolOverridesPath = %q, want %q", got, want)
	}

	got = ToolOverridesPath("", filepath.Join("data", "mcp.json"))
	want = filepath.Join("data", ToolOverridesFilename)
	if got != want {
		t.Fatalf("ToolOverridesPath fallback = %q, want %q", got, want)
	}
}
