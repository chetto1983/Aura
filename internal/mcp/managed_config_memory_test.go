package mcp

import (
	"strings"
	"testing"
)

// Two servers both claiming to be THE memory recipe is a configuration with no correct
// answer — the agent's long-term memory would resolve to whichever one the map iterated
// first. The write path refuses it.
func TestManagedConfig_RejectsDuplicateMemorySources(t *testing.T) {
	doc := ManagedConfig{MCPServers: map[string]ManagedServer{
		"memory": {
			Type: ServerTypeStreamableHTTP, URL: "http://127.0.0.1:8096/mcp/",
			Source: SourceRecipeMemory, Trust: ManagedTrust{Class: TrustTrustedRecipe},
		},
		"mem": {
			Type: ServerTypeStreamableHTTP, URL: "http://127.0.0.1:8097/mcp/",
			Source: SourceRecipeMemory, Trust: ManagedTrust{Class: TrustTrustedRecipe},
		},
	}}
	err := PrepareForWrite(&doc)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("PrepareForWrite duplicate memory error = %v", err)
	}
}
