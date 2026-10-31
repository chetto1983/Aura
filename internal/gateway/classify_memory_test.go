package gateway

import (
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/scoring"
)

// memoryForgetSpec is the spec the bridge builds for the memory recipe's memory_forget
// (mcptools.specFromToolDefWithPolicy): Destructive, with the recipe fields only the bridge assigns.
func memoryForgetSpec() tools.Spec {
	spec := destructiveMCPSpec("memory__memory_forget")
	spec.TrustedRecipeSource = mcp.SourceRecipeMemory
	spec.TrustedRecipeTool = "memory_forget"
	spec.MCPServer = "memory"
	return spec
}

func TestClassifyMemoryForgetDryRunIsARead(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]scoring.RiskTier{
		`{"entity":"x","dry_run":true}`:   scoring.Safe,
		`{"entity":"x"}`:                  scoring.Destructive,
		`{"entity":"x","dry_run":false}`:  scoring.Destructive,
		`{"entity":"x","dry_run":"true"}`: scoring.Destructive,
		`{"entity":"x","dry_run":1}`:      scoring.Destructive,
		`{"entity":"x","dry_run":null}`:   scoring.Destructive,
		// The server decodes case-sensitively, so it would not read DRY_RUN as a dry run.
		`{"entity":"x","DRY_RUN":true}`: scoring.Destructive,
		// The bridge keeps the last of two equal keys, and so does this read.
		`{"dry_run":true,"dry_run":false}`: scoring.Destructive,
		`{"dry_run":false,"dry_run":true}`: scoring.Safe,
		`{"dry_run":`:                      scoring.Destructive,
		``:                                 scoring.Destructive,
	} {
		if got := classify(memoryForgetSpec(), json.RawMessage(raw)); got != want {
			t.Errorf("classify(memory_forget, %s) = %s, want %s", raw, got, want)
		}
	}
}

func TestClassifyDryRunCarveOutTrustsOnlyTheMemoryRecipe(t *testing.T) {
	t.Parallel()
	dryRun := json.RawMessage(`{"entity":"x","dry_run":true}`)

	otherRecipe := memoryForgetSpec()
	otherRecipe.TrustedRecipeSource = "recipe:calendar"
	unmanaged := destructiveMCPSpec("memory__memory_forget")
	otherTool := memoryForgetSpec()
	otherTool.TrustedRecipeTool = "memory_batch"

	for name, spec := range map[string]tools.Spec{
		"another recipe's memory_forget":      otherRecipe,
		"an unmanaged server's memory_forget": unmanaged,
		"another memory tool":                 otherTool,
	} {
		if got := classify(spec, dryRun); got != scoring.Destructive {
			t.Errorf("%s with dry_run = %s, want %s", name, got, scoring.Destructive)
		}
	}
}
