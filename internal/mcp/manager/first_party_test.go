package manager

import (
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

func TestFirstPartyRecipeAcceptsEveryShippedSidecar(t *testing.T) {
	t.Setenv("AURA_ARCADEDB_MCP_PORT", "")
	t.Setenv("AURA_WHATSAPP_MCP_PORT", "")
	t.Setenv("AURA_PIM_MCP_PORT", "")
	entries := BuiltInCatalog()
	if len(entries) != 3 {
		t.Fatalf("catalog has %d entries; the first-party set is meant to be the whole catalog", len(entries))
	}
	for _, entry := range entries {
		if !FirstPartyRecipe(entry.Server) {
			t.Errorf("FirstPartyRecipe(%q) = false, want true", entry.Name)
		}
	}
}

// Renaming a recipe is supported (`aura mcp install memory mymem`), so the predicate must
// not key on the server name.
func TestFirstPartyRecipeSurvivesARename(t *testing.T) {
	memory, ok := LookupCatalog(memoryRecipeName)
	if !ok {
		t.Fatal("memory recipe missing from the catalog")
	}
	renamed := memory.Server
	renamed.Env = append(renamed.Env, "MCP_HEADER_X_TEAM=acme")
	if !FirstPartyRecipe(renamed) {
		t.Fatal("a renamed, env-extended memory recipe must stay first-party")
	}
}

func TestFirstPartyRecipeRefusesEverythingElse(t *testing.T) {
	memory, ok := LookupCatalog(memoryRecipeName)
	if !ok {
		t.Fatal("memory recipe missing from the catalog")
	}
	retargeted := memory.Server
	retargeted.URL = "http://attacker.example/mcp/"
	borrowedSource := mcp.ManagedServer{
		Type: mcp.ServerTypeStreamableHTTP, URL: "http://attacker.example/mcp/",
		Source: mcp.SourceRecipeMemory, Trust: mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
	}
	for name, server := range map[string]mcp.ManagedServer{
		// What `aura mcp add` writes.
		"operator added": {Command: "npx", Args: []string{"some-server"}, Source: "manual"},
		"remote http": {
			Type: mcp.ServerTypeStreamableHTTP, URL: "https://mcp.linear.app/mcp",
			Source: "manual", Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP},
		},
		"no source at all":                  {Type: mcp.ServerTypeStreamableHTTP, URL: memory.Server.URL},
		"catalog url no source":             {Type: mcp.ServerTypeStreamableHTTP, URL: memory.Server.URL, Source: ""},
		"recipe source retargeted off-host": retargeted,
		"hand-planted borrowed source":      borrowedSource,
		"empty":                             {},
	} {
		if FirstPartyRecipe(server) {
			t.Errorf("FirstPartyRecipe(%s) = true, want false — a non-recipe server must never be auto-granted", name)
		}
	}
}
