package mcptools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/gateway"
)

// curatedTool is the smallest tools.Tool that carries a spec through a registry.
type curatedTool struct{ spec tools.Spec }

func (c curatedTool) Spec() tools.Spec { return c.spec }

func (curatedTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

// TestEveryCuratedMultiplexedToolHasAGatewayClassifier walks the multiplexedMCPTools
// table itself, so a curated tool added there without a per-action classifier in
// internal/gateway fails here, in CI, with no list in a test to edit. classify
// (gateway/classify.go) would otherwise grade every action of such a tool at one
// flat tier taken from its spec bits, losing the per-action grading.
//
// The spec is built Mutating and Multiplexed, the way the curated calendar tool
// bridges: ValidateClassifiable skips a non-mutating tool, and a curated tool whose
// raw name misses the risk table falls through to the fail-closed mutating default
// (bridge_risk.go, classifyToolRisk), so assuming Mutating is the safe direction.
func TestEveryCuratedMultiplexedToolHasAGatewayClassifier(t *testing.T) {
	names := mcptools.MultiplexedMCPToolNamesForTest()
	if len(names) == 0 {
		t.Fatal("the curated multiplexed tool table is empty: this test would pass vacuously")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			reg := tools.NewRegistry()
			reg.Register(curatedTool{spec: tools.Spec{
				Name:                name,
				Mutating:            true,
				Multiplexed:         true,
				OperationScope:      tools.OperationScopeMCP,
				OperationNormalizer: tools.OperationNormalizerCanonical,
				ReplayPolicy:        tools.ReplayToolResult,
			}})
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("curated multiplexed tool %q has no per-action classifier in internal/gateway (multiplexedClassifiers): %v", name, r)
				}
			}()
			gateway.ValidateClassifiable(reg)
		})
	}
}
