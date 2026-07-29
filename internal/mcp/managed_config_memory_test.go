package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedConfig_RejectsDuplicateMemorySourcesOnLoadAndSave(t *testing.T) {
	servers := map[string]ManagedServer{
		"memory": {
			Type: ServerTypeStreamableHTTP, URL: "http://127.0.0.1:8091/mcp/",
			Source: SourceRecipeMemory, Trust: ManagedTrust{Class: TrustTrustedRecipe},
		},
		"mem": {
			Type: ServerTypeStreamableHTTP, URL: "http://127.0.0.1:8092/mcp/",
			Source: SourceRecipeMemory, Trust: ManagedTrust{Class: TrustTrustedRecipe},
		},
	}
	path := filepath.Join(t.TempDir(), "servers.json")
	if err := SaveManagedConfig(path, ManagedConfig{MCPServers: servers}); err == nil ||
		!strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("SaveManagedConfig duplicate memory error = %v", err)
	}

	raw := `{"version":2,"mcpServers":{` +
		`"memory":{"type":"streamable_http","url":"http://127.0.0.1:8091/mcp/","source":"recipe:memory","trust":{"class":"trusted_recipe"}},` +
		`"mem":{"type":"streamable_http","url":"http://127.0.0.1:8092/mcp/","source":"recipe:memory","trust":{"class":"trusted_recipe"}}}}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := LoadManagedConfig(path); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("LoadManagedConfig duplicate memory error = %v", err)
	}
}
