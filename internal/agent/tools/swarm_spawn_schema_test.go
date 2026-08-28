package tools

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// swarmSpawnParametersJSON decodes Spec().Parameters as generic JSON — the same
// json.Unmarshal-into-map idiom internal/agent/tools/skill_test.go already uses
// for another tool's Spec().Parameters (reused per the plan's own instruction to
// grep for an existing decode helper before writing a new one).
func swarmSpawnParametersJSON(t *testing.T, tool *SwarmSpawn) map[string]any {
	t.Helper()
	var schema map[string]any
	if err := json.Unmarshal(tool.Spec().Parameters, &schema); err != nil {
		t.Fatalf("swarm_spawn Parameters not valid JSON: %v\nraw: %s", err, tool.Spec().Parameters)
	}
	return schema
}

// TestSwarmSpawnSpecReflectsConfig is the SWARM-02 live-cap render test: the
// rendered JSON schema carries a context property beside goals, names all four
// live caps by VALUE (never a hardcoded env var name), and two tools constructed
// with different caps render two different schemas from the same source code —
// proving nothing is a static literal.
func TestSwarmSpawnSpecReflectsConfig(t *testing.T) {
	capsA := SwarmCaps{MaxGoals: 8, MaxConcurrent: 3, ChildTimeoutSec: 120, MaxDepth: 2}
	capsB := SwarmCaps{MaxGoals: 3, MaxConcurrent: 5, ChildTimeoutSec: 90, MaxDepth: 4}

	toolA := &SwarmSpawn{Caps: capsA}
	toolB := &SwarmSpawn{Caps: capsB}

	rawA := string(toolA.Spec().Parameters)
	rawB := string(toolB.Spec().Parameters)

	if rawA == rawB {
		t.Fatalf("two tools constructed with different caps rendered the SAME schema — not reading the injected caps:\n%s", rawA)
	}

	// goals + context both declared as properties.
	schemaA := swarmSpawnParametersJSON(t, toolA)
	props, _ := schemaA["properties"].(map[string]any)
	if props == nil {
		t.Fatalf("Parameters missing properties: %s", rawA)
	}
	if _, ok := props["goals"]; !ok {
		t.Fatal("Parameters must declare a goals property")
	}
	if _, ok := props["context"]; !ok {
		t.Fatal("Parameters must declare a context property beside goals (SWARM-01)")
	}

	// All four live caps are named BY VALUE in the rendered schema text.
	for _, want := range []string{
		strconv.Itoa(capsA.MaxGoals),
		strconv.Itoa(capsA.MaxConcurrent),
		strconv.Itoa(capsA.ChildTimeoutSec),
		strconv.Itoa(capsA.MaxDepth),
	} {
		if !strings.Contains(rawA, want) {
			t.Errorf("rendered schema missing live cap value %q:\n%s", want, rawA)
		}
	}

	// The env var NAME must never appear in the rendered schema — only the value
	// (plan 51-09 retires AURA_SWARM_CHILD_TIMEOUT_SEC; a hardcoded name would go
	// stale silently).
	for _, forbidden := range []string{"AURA_SWARM_MAX_GOALS", "AURA_SWARM_MAX_CONCURRENT", "AURA_SWARM_CHILD_TIMEOUT_SEC", "AURA_SWARM_MAX_DEPTH"} {
		if strings.Contains(rawA, forbidden) {
			t.Errorf("rendered schema hardcodes env var name %q — must render the VALUE, not the name:\n%s", forbidden, rawA)
		}
	}

	// Spec() is deterministic — called twice, same output (render happens at
	// call time from the injected struct, not from a package-init side effect).
	if string(toolA.Spec().Parameters) != rawA {
		t.Error("Spec().Parameters is not deterministic across repeated calls")
	}
}

// TestSwarmSpawnSpecNoStaticParamsLiteral pins the "no static json.RawMessage
// literal" prohibition at the source level, beyond the two-caps-differ test
// above: renderSwarmSpawnParams must exist as the one production path building
// Parameters.
func TestSwarmSpawnSpecNoStaticParamsLiteral(t *testing.T) {
	tool := &SwarmSpawn{Caps: SwarmCaps{MaxGoals: 5, MaxConcurrent: 2, ChildTimeoutSec: 60, MaxDepth: 2}}
	params := renderSwarmSpawnParams(tool.Caps)
	if len(params) == 0 {
		t.Fatal("renderSwarmSpawnParams returned empty output")
	}
	if string(params) != string(tool.Spec().Parameters) {
		t.Error("Spec().Parameters must be built from renderSwarmSpawnParams(caps), not a separate literal")
	}
}
