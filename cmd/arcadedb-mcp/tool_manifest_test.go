package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestToolManifestMatchesServer(t *testing.T) {
	tools := listServerTools(t)
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})
	got, err := json.MarshalIndent(tools, "", "  ")
	if err != nil {
		t.Fatalf("encode tools: %v", err)
	}
	got = append(got, '\n')

	path := filepath.Join("..", "..", "docs", "arcadedb-mcp-live-tools.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("update %s: %v", path, err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s does not match the live MCP surface; run UPDATE_GOLDEN=1 go test ./cmd/arcadedb-mcp", path)
	}

	assertNoUserIdentifierProperty(t, tools)
	assertErasingToolsAreDestructive(t, tools)
}

// assertErasingToolsAreDestructive pins the hint a client other than Aura reads: both tools
// that can remove facts declare it, so neither looks safer than the other.
func assertErasingToolsAreDestructive(t *testing.T, tools []*mcp.Tool) {
	t.Helper()
	erasing := map[string]bool{"memory_forget": false, "memory_batch": false}
	for _, tool := range tools {
		if _, ok := erasing[tool.Name]; !ok {
			continue
		}
		erasing[tool.Name] = true
		if a := tool.Annotations; a == nil || a.DestructiveHint == nil || !*a.DestructiveHint {
			t.Errorf("%s removes facts but does not declare DestructiveHint: true", tool.Name)
		}
	}
	for name, seen := range erasing {
		if !seen {
			t.Errorf("%s is not advertised", name)
		}
	}
}

// assertNoUserIdentifierProperty iterates every advertised tool's input schema
// — never a hand-picked list of tool names, so a tenth tool added later is
// covered automatically — and fails if any declares a user_identifier
// property. The calling identity comes from the OAuth subject; the model must
// have no schema slot to fill with one, and the server ignores an invented
// argument because identityFromToken never reads Arguments.
func assertNoUserIdentifierProperty(t *testing.T, tools []*mcp.Tool) {
	t.Helper()
	if len(tools) == 0 {
		t.Fatal("no tools advertised; nothing to check")
	}
	for _, tool := range tools {
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			continue
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			continue
		}
		if _, present := props["user_identifier"]; present {
			t.Errorf("tool %q still advertises a user_identifier input property", tool.Name)
		}
	}
}
