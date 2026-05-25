package agentdef

import (
	"context"
	"testing"
	"testing/fstest"
)

func TestRegistry_GetReturnsCopy(t *testing.T) {
	reg, err := NewRegistry(context.Background(), &Loader{BuiltinFS: fstest.MapFS{
		"planner/agent.json": {Data: []byte(`{
			"id":"planner",
			"tier":"chat",
			"when_to_use":"plan work",
			"prompt":"plan",
			"tools":{"named":["search"]},
			"subagents":[{"id":"worker","delegate_name":"worker"}]
		}`)},
		"worker/agent.json": {Data: []byte(`{
			"id":"worker",
			"tier":"worker",
			"when_to_use":"do work",
			"prompt":"work"
		}`)},
	}}, &Validator{})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	def, ok := reg.Get("planner")
	if !ok {
		t.Fatal("planner not found")
	}
	def.Tools.Named[0] = "mutated"
	def.Subagents[0].ID = "mutated"

	again, ok := reg.Get("planner")
	if !ok {
		t.Fatal("planner not found on second get")
	}
	if again.Tools.Named[0] != "search" || again.Subagents[0].ID != "worker" {
		t.Fatalf("registry returned aliased definition: %+v", again)
	}
}
