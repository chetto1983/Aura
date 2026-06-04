package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeSwarmRunner records whether Run was invoked so the goals-cap test can assert
// the over-cap path short-circuits BEFORE any engine call.
type fakeSwarmRunner struct {
	called   bool
	gotGoals []string
}

func (f *fakeSwarmRunner) Run(ctx context.Context, goals []string) (ToolResult, error) {
	f.called = true
	f.gotGoals = goals
	return NewResult(ctx, `[]`)
}

// TestSwarmSpawnDescriptionLiteral asserts the Deferred Description carries the four
// D-24 anti-over-spawn phrases (the finalizeNudge substring-assert idiom). These are
// load-bearing: BM25 indexes the description, and the model relies on them to avoid
// over-spawning trivial work.
func TestSwarmSpawnDescriptionLiteral(t *testing.T) {
	desc := (&SwarmSpawn{}).Spec().Description
	phrases := []string{
		"2 or more independent, self-contained subtasks",  // >=2 independent self-contained subtasks
		"answer it directly yourself instead of spawning", // simple single task = answer directly
		"complete, standalone brief",                      // each goal a complete brief (objective + output + boundaries)
		"the worker cannot see the conversation",          // worker cannot see the conversation
	}
	for _, p := range phrases {
		if !strings.Contains(desc, p) {
			t.Errorf("swarm_spawn Description missing D-24 phrase %q\nfull: %s", p, desc)
		}
	}
}

// TestSwarmSpawnGoalsCap asserts an over-cap call returns a model-readable error
// string and NEVER reaches the runner (D-13).
func TestSwarmSpawnGoalsCap(t *testing.T) {
	r := &fakeSwarmRunner{}
	tool := &SwarmSpawn{Runner: r, MaxGoals: 2}
	ctx := ctxWith(t, "sess-swarm", "call-swarm")

	res, err := tool.Execute(ctx, json.RawMessage(`{"goals":["a","b","c"]}`))
	if err != nil {
		t.Fatalf("over-cap goals must be a model-readable inline error, not a Go error: %v", err)
	}
	if r.called {
		t.Fatal("the runner must NOT be invoked when goals exceed the cap (D-13)")
	}
	if !strings.HasPrefix(res.Preview, "error: too many goals") {
		t.Fatalf("preview = %q, want an `error: too many goals` string", res.Preview)
	}
	if !strings.Contains(res.Preview, "AURA_SWARM_MAX_GOALS") {
		t.Errorf("the cap error should name AURA_SWARM_MAX_GOALS for the operator: %q", res.Preview)
	}
}

// TestSwarmSpawnDelegatesUnderCap asserts a valid goals array reaches the runner
// verbatim (the happy path the engine drives).
func TestSwarmSpawnDelegatesUnderCap(t *testing.T) {
	r := &fakeSwarmRunner{}
	tool := &SwarmSpawn{Runner: r, MaxGoals: 8}
	ctx := ctxWith(t, "sess-swarm", "call-swarm")

	if _, err := tool.Execute(ctx, json.RawMessage(`{"goals":["x","y"]}`)); err != nil {
		t.Fatalf("under-cap Execute: %v", err)
	}
	if !r.called {
		t.Fatal("a valid goals array must be delegated to the runner")
	}
	if len(r.gotGoals) != 2 || r.gotGoals[0] != "x" || r.gotGoals[1] != "y" {
		t.Fatalf("runner got goals %v, want [x y]", r.gotGoals)
	}
}

// TestSwarmSpawnSchema asserts Deferred:true, a goals array with required:["goals"],
// and NO tier key (D-03).
func TestSwarmSpawnSchema(t *testing.T) {
	s := (&SwarmSpawn{}).Spec()
	if s.Name != "swarm_spawn" {
		t.Fatalf("name = %q, want swarm_spawn", s.Name)
	}
	if !s.Deferred {
		t.Fatal("swarm_spawn must be Deferred:true (big Description — manifest protection)")
	}

	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(s.Parameters, &schema); err != nil {
		t.Fatalf("unmarshal Parameters: %v", err)
	}
	if _, ok := schema.Properties["goals"]; !ok {
		t.Fatal("Parameters must declare a goals property")
	}
	if _, ok := schema.Properties["tier"]; ok {
		t.Fatal("Parameters must NOT declare a tier property (D-03 — no tier in v1)")
	}
	if len(schema.Required) != 1 || schema.Required[0] != "goals" {
		t.Fatalf("required = %v, want [goals]", schema.Required)
	}
}

// TestSwarmSpawnMissingRunner asserts a nil runner (composition-root wiring bug) is a
// real Go error, distinct from the inline domain errors.
func TestSwarmSpawnMissingRunner(t *testing.T) {
	tool := &SwarmSpawn{Runner: nil, MaxGoals: 8}
	ctx := ctxWith(t, "sess-swarm", "call-swarm")
	if _, err := tool.Execute(ctx, json.RawMessage(`{"goals":["x"]}`)); err == nil {
		t.Fatal("a nil runner must surface as a Go error (wiring bug), not a silent no-op")
	}
}
