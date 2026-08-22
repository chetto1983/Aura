package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/scoring"
)

// fakeTool is a test fixture implementing tools.Tool with a caller-chosen Spec so the
// boot-guard can be exercised against a deliberately-misregistered multiplexed tool.
type fakeTool struct{ spec tools.Spec }

func (f fakeTool) Spec() tools.Spec { return f.spec }
func (f fakeTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

// TestValidateClassifiablePanicsOnUnwiredMultiplexed proves the fail-loud wiring guard:
// a Mutating+Multiplexed tool with no per-action classifier in multiplexedClassifiers
// panics at boot (RESEARCH Pitfall 2), rather than silently under-gating its actions.
func TestValidateClassifiablePanicsOnUnwiredMultiplexed(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(fakeTool{spec: tools.Spec{
		Name:                "ghost_multiplex",
		Summary:             "unwired",
		Mutating:            true,
		Multiplexed:         true,
		OperationScope:      tools.OperationScopeAgent,
		OperationNormalizer: tools.OperationNormalizerCanonical,
		ReplayPolicy:        tools.ReplayToolResult,
	}})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("ValidateClassifiable did not panic on an unwired multiplexed tool")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "ghost_multiplex") ||
			!strings.Contains(msg, "no per-action classifier") {
			t.Fatalf("panic message = %q, want it to name the tool and the missing classifier", r)
		}
	}()

	ValidateClassifiable(reg)
}

// TestValidateClassifiableAcceptsWiredTools proves the guard is quiet for the real
// wiring: the three live multiplexed tools all have classifiers, and non-multiplexed
// mutating tools (shell_exec-shaped) are ignored by the classifier check. All four
// mutating fixtures declare complete operation metadata (D-09) — this test proves the
// classifier-wiring guard, not the metadata guard, so it must not trip the latter.
func TestValidateClassifiableAcceptsWiredTools(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(fakeTool{spec: tools.Spec{Name: "skill_manage", Mutating: true, Multiplexed: true, OperationScope: tools.OperationScopeAgent, OperationNormalizer: tools.OperationNormalizerCanonical, ReplayPolicy: tools.ReplayToolResult}})
	reg.Register(fakeTool{spec: tools.Spec{Name: "task", Mutating: true, Multiplexed: true, OperationScope: tools.OperationScopeAgent, OperationNormalizer: tools.OperationNormalizerCanonical, ReplayPolicy: tools.ReplayToolResult}})
	reg.Register(fakeTool{spec: tools.Spec{Name: "swarm_spawn", Mutating: true, Multiplexed: true, Deferred: true, OperationScope: tools.OperationScopeAgent, OperationNormalizer: tools.OperationNormalizerCanonical, ReplayPolicy: tools.ReplayToolResult}})
	reg.Register(fakeTool{spec: tools.Spec{Name: "shell_exec", Mutating: true, OperationScope: tools.OperationScopeAgent, OperationNormalizer: tools.OperationNormalizerCanonical, ReplayPolicy: tools.ReplayToolResult}})
	reg.Register(fakeTool{spec: tools.Spec{Name: "fs_read"}})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ValidateClassifiable panicked on correctly-wired tools: %v", r)
		}
	}()

	ValidateClassifiable(reg)
}

// TestValidateClassifiableIgnoresNonMutatingMultiplexed documents that the guard only
// fires for the Mutating floor: a (hypothetical) read-only multiplexed tool without a
// classifier is not a wiring bug because classify's non-mutating fallback returns Safe.
func TestValidateClassifiableIgnoresNonMutatingMultiplexed(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(fakeTool{spec: tools.Spec{Name: "readonly_multiplex", Multiplexed: true}})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("guard fired on a non-mutating multiplexed tool: %v", r)
		}
	}()

	ValidateClassifiable(reg)
}

// TestValidateClassifiablePanicsOnEmptyOperationScope proves the second boot guard
// (D-09): a Mutating tool with an empty OperationScope panics, naming the tool.
func TestValidateClassifiablePanicsOnEmptyOperationScope(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(fakeTool{spec: tools.Spec{
		Name:                "half_wired_scope",
		Mutating:            true,
		OperationNormalizer: tools.OperationNormalizerCanonical,
		ReplayPolicy:        tools.ReplayToolResult,
	}})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("ValidateClassifiable did not panic on a mutating tool with an empty OperationScope")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "half_wired_scope") ||
			!strings.Contains(msg, "OperationScope") {
			t.Fatalf("panic message = %q, want it to name the tool and the missing field", r)
		}
	}()

	ValidateClassifiable(reg)
}

// TestValidateClassifiablePanicsOnEmptyOperationNormalizer mirrors the OperationScope
// case for OperationNormalizer (D-09).
func TestValidateClassifiablePanicsOnEmptyOperationNormalizer(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(fakeTool{spec: tools.Spec{
		Name:           "half_wired_normalizer",
		Mutating:       true,
		OperationScope: tools.OperationScopeAgent,
		ReplayPolicy:   tools.ReplayToolResult,
	}})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("ValidateClassifiable did not panic on a mutating tool with an empty OperationNormalizer")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "half_wired_normalizer") ||
			!strings.Contains(msg, "OperationNormalizer") {
			t.Fatalf("panic message = %q, want it to name the tool and the missing field", r)
		}
	}()

	ValidateClassifiable(reg)
}

// TestValidateClassifiablePanicsOnEmptyReplayPolicy mirrors the OperationScope case
// for ReplayPolicy (D-09). It asserts emptiness only — it must NOT compare against
// tools.ReplayToolResult, which would resurrect the withdrawn second ReplayPolicy
// value the boot guard is standing in for (D-07).
func TestValidateClassifiablePanicsOnEmptyReplayPolicy(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(fakeTool{spec: tools.Spec{
		Name:                "half_wired_replay",
		Mutating:            true,
		OperationScope:      tools.OperationScopeAgent,
		OperationNormalizer: tools.OperationNormalizerCanonical,
	}})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("ValidateClassifiable did not panic on a mutating tool with an empty ReplayPolicy")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "half_wired_replay") ||
			!strings.Contains(msg, "ReplayPolicy") {
			t.Fatalf("panic message = %q, want it to name the tool and the missing field", r)
		}
	}()

	ValidateClassifiable(reg)
}

// TestValidateClassifiableAcceptsCompleteMutatingTool proves the positive case
// distinct from the wiring tests above: a single non-multiplexed mutating tool that
// declares all three operation-metadata fields does not panic (D-09).
func TestValidateClassifiableAcceptsCompleteMutatingTool(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(fakeTool{spec: tools.Spec{
		Name:                "fully_wired_mutation",
		Mutating:            true,
		OperationScope:      tools.OperationScopeAgent,
		OperationNormalizer: tools.OperationNormalizerCanonical,
		ReplayPolicy:        tools.ReplayToolResult,
	}})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ValidateClassifiable panicked on a complete mutating tool: %v", r)
		}
	}()

	ValidateClassifiable(reg)
}

// TestValidateClassifiableIgnoresNonMutatingEmptyOperationMetadata proves the guard is
// scoped to Mutating tools only (D-09): a non-mutating tool with all three operation
// fields empty must not panic, or the guard would become a de facto requirement on
// every registered tool rather than only the mutating ones.
func TestValidateClassifiableIgnoresNonMutatingEmptyOperationMetadata(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(fakeTool{spec: tools.Spec{Name: "readonly_unwired"}})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("guard fired on a non-mutating tool with empty operation metadata: %v", r)
		}
	}()

	ValidateClassifiable(reg)
}

// TestMultiplexedNotInferredFromSchemaShape pins D-34's fail-closed-not-panic
// promise: a Mutating tool whose schema carries an `action` property, but whose
// Multiplexed is deliberately false (no classifier entry), boots cleanly and
// classifies at the generic Mutating/Destructive floor — never a panic, never a
// silent promotion. This is what lets a stranger's server mount safely even
// though its schema happens to look like Aura's own multiplexed tools.
func TestMultiplexedNotInferredFromSchemaShape(t *testing.T) {
	reg := tools.NewRegistry()
	spec := tools.Spec{
		Name:                "stranger__do_thing",
		Parameters:          json.RawMessage(`{"type":"object","properties":{"action":{"type":"string"}}}`),
		Mutating:            true,
		Destructive:         true,
		Multiplexed:         false,
		OperationScope:      tools.OperationScopeMCP,
		OperationNormalizer: tools.OperationNormalizerCanonical,
		ReplayPolicy:        tools.ReplayToolResult,
	}
	reg.Register(fakeTool{spec: spec})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ValidateClassifiable panicked on a non-multiplexed tool whose schema merely has an action property: %v", r)
		}
	}()
	ValidateClassifiable(reg)

	if got := classify(spec, json.RawMessage(`{"action":"whatever"}`)); got != scoring.Destructive {
		t.Fatalf("classify = %q, want the generic Destructive floor (spec.Destructive=true)", got)
	}
}
