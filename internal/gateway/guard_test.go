package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
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
		Name:        "ghost_multiplex",
		Summary:     "unwired",
		Mutating:    true,
		Multiplexed: true,
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
// mutating tools (shell_exec-shaped) are ignored.
func TestValidateClassifiableAcceptsWiredTools(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(fakeTool{spec: tools.Spec{Name: "skill", Mutating: true, Multiplexed: true}})
	reg.Register(fakeTool{spec: tools.Spec{Name: "task", Mutating: true, Multiplexed: true}})
	reg.Register(fakeTool{spec: tools.Spec{Name: "swarm_spawn", Mutating: true, Multiplexed: true, Deferred: true}})
	reg.Register(fakeTool{spec: tools.Spec{Name: "shell_exec", Mutating: true}})
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
