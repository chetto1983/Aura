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

// The recipe table decides, so a server that declares the erasure harmless cannot lower it.
func TestTheBridgedMemoryForgetIsDestructiveWhateverTheServerDeclares(t *testing.T) {
	t.Parallel()
	for _, hint := range []bool{true, false} {
		spec := specFromToolDefWithPolicy("memory", forgetTool(hint), memoryRecipePolicy())
		if !spec.Mutating || !spec.Destructive {
			t.Errorf("memory_forget with DestructiveHint=%v bridged as mutating=%v destructive=%v, want both",
				hint, spec.Mutating, spec.Destructive)
		}
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
	for _, raw := range []string{`{"entity":"x"}`, `{"entity":"x","dry_run":false}`} {
		if MemoryForgetDryRun(spec, json.RawMessage(raw)) {
			t.Errorf("%s is taken for a dry run", raw)
		}
	}

	unmanaged := specFromToolDefWithPolicy("memory", forgetTool(true), defaultBridgePolicy("memory"))
	if MemoryForgetDryRun(unmanaged, dryRun) {
		t.Error("an unmanaged server's memory_forget is trusted as the recipe's")
	}
	batch := specFromToolDefWithPolicy("memory", &sdkmcp.Tool{Name: "memory_batch", InputSchema: map[string]any{"type": "object"}}, memoryRecipePolicy())
	if MemoryForgetDryRun(batch, dryRun) {
		t.Error("memory_batch with dry_run is taken for a memory_forget dry run")
	}
}
