package tools

import (
	"context"
	"encoding/json"
	"testing"
)

// TestRenderToolDefs asserts RenderToolDefs returns []llm.ToolDef in the same
// alphabetical order as Render(), one per tool, with Name/Description/Parameters
// carried (deferred tools fall back to Summary and carry no Parameters).
func TestRenderToolDefs(t *testing.T) {
	r := NewRegistry()
	// Registered out of alphabetical order on purpose.
	r.Register(zebraTool{})
	r.Register(alphaTool{})
	r.Register(deferredMidTool{})

	entries := r.Render()
	defs := r.RenderToolDefs()

	if len(defs) != len(entries) {
		t.Fatalf("RenderToolDefs len = %d, Render len = %d — must be 1:1", len(defs), len(entries))
	}
	// Same alphabetical order as Render (no re-sort).
	for i := range entries {
		if defs[i].Function.Name != entries[i].Name {
			t.Errorf("def[%d].Name = %q, Render[%d].Name = %q — order diverged", i, defs[i].Function.Name, i, entries[i].Name)
		}
		if defs[i].Type != "function" {
			t.Errorf("def[%d].Type = %q, want function", i, defs[i].Type)
		}
	}
	// Names are alpha-sorted: alpha, mid_deferred, zebra.
	want := []string{"alpha", "mid_deferred", "zebra"}
	for i, w := range want {
		if defs[i].Function.Name != w {
			t.Errorf("defs[%d].Name = %q, want %q", i, defs[i].Function.Name, w)
		}
	}

	// Non-deferred tool carries its full Description + Parameters.
	alpha := defs[0]
	if alpha.Function.Description != "alpha full description" {
		t.Errorf("alpha Description = %q, want full description", alpha.Function.Description)
	}
	if len(alpha.Function.Parameters) == 0 {
		t.Error("alpha Parameters empty, want the JSON schema carried")
	}

	// Deferred tool falls back to Summary and carries no Parameters (hidden until
	// tool_search promotes it).
	mid := defs[1]
	if mid.Function.Description != "mid summary" {
		t.Errorf("deferred Description = %q, want the Summary fallback", mid.Function.Description)
	}
	if len(mid.Function.Parameters) != 0 {
		t.Errorf("deferred Parameters = %q, want empty (hidden until tool_search)", mid.Function.Parameters)
	}
}

type alphaTool struct{}

func (alphaTool) Spec() Spec {
	return Spec{
		Name: "alpha", Summary: "alpha summary", Description: "alpha full description",
		Parameters: json.RawMessage(`{"type":"object"}`), Deferred: false,
	}
}
func (alphaTool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	return ToolResult{}, nil
}

type zebraTool struct{}

func (zebraTool) Spec() Spec {
	return Spec{Name: "zebra", Summary: "zebra summary", Description: "zebra desc", Deferred: false}
}
func (zebraTool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	return ToolResult{}, nil
}

type deferredMidTool struct{}

func (deferredMidTool) Spec() Spec {
	return Spec{
		Name: "mid_deferred", Summary: "mid summary", Description: "mid full hidden",
		Parameters: json.RawMessage(`{"type":"object"}`), Deferred: true,
	}
}
func (deferredMidTool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	return ToolResult{}, nil
}
