package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// deferredOnlyTool is a deferred capability fixture for Validate() coverage: it is
// NOT tool_search, so a registry of these alone must fail the ≥1-non-deferred guard.
type deferredOnlyTool struct{ name string }

func (d deferredOnlyTool) Spec() Spec {
	return Spec{
		Name: d.name, Summary: "deferred " + d.name, Description: "full desc " + d.name,
		Parameters: json.RawMessage(`{"type":"object"}`), Deferred: true,
	}
}
func (deferredOnlyTool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	return ToolResult{}, nil
}

// activeTool is a non-deferred capability fixture (NOT tool_search) — exactly one
// of these is enough to satisfy Validate().
type activeTool struct{ name string }

func (a activeTool) Spec() Spec {
	return Spec{
		Name: a.name, Summary: "active " + a.name, Description: "full desc " + a.name,
		Parameters: json.RawMessage(`{"type":"object"}`), Deferred: false,
	}
}
func (activeTool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	return ToolResult{}, nil
}

// TestNonDeferredGuard: a registry holding ONLY tool_search + all-deferred tools
// fails Validate() (mirroring Anthropic's hard 400 "at least one tool must be
// non-deferred"); adding one non-deferred capability tool makes it pass. Kills the
// "Validate always returns nil" mutant.
func TestNonDeferredGuard(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&ToolSearch{Registry: reg})
	reg.Register(deferredOnlyTool{name: "deferred_a"})
	reg.Register(deferredOnlyTool{name: "deferred_b"})

	err := reg.Validate()
	if err == nil {
		t.Fatal("Validate() returned nil for an all-deferred registry (only tool_search non-deferred)")
	}
	if !strings.Contains(err.Error(), "non-deferred") {
		t.Errorf("Validate() error = %q, want it to mention 'non-deferred'", err.Error())
	}

	reg.Register(activeTool{name: "act"})
	if err := reg.Validate(); err != nil {
		t.Fatalf("Validate() failed after adding a non-deferred tool: %v", err)
	}
}

// TestValidate_ExcludesToolSearch: a registry whose ONLY non-deferred tool is
// tool_search itself still FAILS Validate() — tool_search must be excluded from the
// non-deferred count (RESEARCH Pitfall 5). Kills the mutant that drops the
// `Name != "tool_search"` exclusion (which would let a search-only registry pass).
func TestValidate_ExcludesToolSearch(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&ToolSearch{Registry: reg})
	reg.Register(deferredOnlyTool{name: "deferred_only"})

	if err := reg.Validate(); err == nil {
		t.Fatal("Validate() passed a registry whose only non-deferred tool is tool_search — the exclusion was dropped")
	}
}

// TestValidate_EmptyRegistry: an empty registry (no tools at all) fails Validate()
// — there is no actionable tool. Guards the count-from-zero boundary.
func TestValidate_EmptyRegistry(t *testing.T) {
	if err := NewRegistry().Validate(); err == nil {
		t.Fatal("Validate() passed an empty registry")
	}
}

// TestMultiplexedToolsMutatingFloor pins the D-02/D-02d fail-closed floor: the three
// action-multiplexed tools each report Spec().Mutating == true (so the policy gateway
// reserves them) AND Spec().Multiplexed == true (so its boot-guard requires a concrete
// tier for them). Kills the mutant that drops either flag on any of the three.
func TestMultiplexedToolsMutatingFloor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		spec Spec
	}{
		{"skill_manage", (&SkillManageTool{}).Spec()},
		{"task", (&TaskTool{}).Spec()},
		{"swarm_spawn", (&SwarmSpawn{}).Spec()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.spec.Mutating {
				t.Errorf("%s Spec().Mutating = false, want true (fail-closed floor)", tc.name)
			}
			if !tc.spec.Multiplexed {
				t.Errorf("%s Spec().Multiplexed = false, want true (boot-guard hint)", tc.name)
			}
		})
	}
	// swarm_spawn must stay Deferred:true even after the Mutating/Multiplexed flip (D-01).
	if !(&SwarmSpawn{}).Spec().Deferred {
		t.Error("swarm_spawn Spec().Deferred = false, want true (deferred-tool contract preserved)")
	}
}
