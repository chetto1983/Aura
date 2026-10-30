package mcptools

import (
	"encoding/json"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/mcp"
)

func memoryRecipePolicy() bridgePolicy {
	return managedBridgePolicy(mcp.ManagedServer{Source: mcp.SourceRecipeMemory, Trust: mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe}})
}

// forgetTool is memory_forget as cmd/arcadedb-mcp declares it.
func forgetTool(destructiveHint bool) *sdkmcp.Tool {
	return &sdkmcp.Tool{
		Name:        "memory_forget",
		InputSchema: map[string]any{"type": "object"},
		Annotations: &sdkmcp.ToolAnnotations{DestructiveHint: &destructiveHint, IdempotentHint: true},
	}
}

func TestTheBridgedMemoryForgetDryRunIsRecognised(t *testing.T) {
	t.Parallel()
	dryRun := json.RawMessage(`{"entity":"x","dry_run":true}`)

	spec := specFromToolDefWithPolicy("memory", forgetTool(true), memoryRecipePolicy())
	if !MemoryForgetDryRun(spec, dryRun) {
		t.Errorf("the memory recipe's memory_forget dry run is not recognised: recipe %q/%q",
			spec.TrustedRecipeSource, spec.TrustedRecipeTool)
	}
	if MemoryForgetDryRun(spec, json.RawMessage(`{"entity":"x"}`)) {
		t.Error("a memory_forget without dry_run is taken for a dry run")
	}

	unmanaged := specFromToolDefWithPolicy("memory", forgetTool(true), defaultBridgePolicy("memory"))
	if MemoryForgetDryRun(unmanaged, dryRun) {
		t.Error("an unmanaged server's memory_forget is trusted as the recipe's")
	}
}
