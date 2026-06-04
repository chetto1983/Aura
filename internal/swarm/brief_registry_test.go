package swarm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// stubTool is a minimal non-deferred tool for registry-filtering assertions.
type stubTool struct{ name string }

func (s stubTool) Spec() tools.Spec {
	return tools.Spec{Name: s.name, Summary: s.name, Description: s.name}
}
func (stubTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

// TestWithout asserts D-08/D-10: the derived registry drops the named tool while
// the parent keeps it (parent is never mutated).
func TestWithout(t *testing.T) {
	parent := tools.NewRegistry()
	parent.Register(stubTool{name: "swarm_spawn"})
	parent.Register(stubTool{name: "text_response"})
	parent.Register(stubTool{name: "web_search"})

	child := Without(parent, "swarm_spawn")

	if _, ok := child.Get("swarm_spawn"); ok {
		t.Error("child registry must NOT contain swarm_spawn")
	}
	if _, ok := child.Get("text_response"); !ok {
		t.Error("child registry must retain text_response")
	}
	if _, ok := child.Get("web_search"); !ok {
		t.Error("child registry must retain web_search")
	}
	// Parent is unmodified (immutable per run).
	if _, ok := parent.Get("swarm_spawn"); !ok {
		t.Error("parent registry must STILL contain swarm_spawn (no mutation)")
	}
	if len(child.All()) != 2 || len(parent.All()) != 3 {
		t.Errorf("child=%d parent=%d, want child=2 parent=3", len(child.All()), len(parent.All()))
	}
}

// TestStructuredBrief asserts the D-07 four section markers are present plus the
// D-06 worker overlay and the goal text (the finalizeNudge substring-assert idiom).
func TestStructuredBrief(t *testing.T) {
	const goal = "Summarize the unread mail from Andrea"
	b := structuredBrief(goal)

	for _, marker := range []string{briefObjective, briefOutput, briefTools, briefBoundaries} {
		if !strings.Contains(b, marker) {
			t.Errorf("structuredBrief missing D-07 section marker %q", marker)
		}
	}
	if !strings.Contains(b, workerOverlay) {
		t.Error("structuredBrief missing the D-06 worker overlay")
	}
	if !strings.Contains(b, goal) {
		t.Error("structuredBrief missing the goal text")
	}
	// Deterministic for a given goal (KV-cache friendly).
	if structuredBrief(goal) != b {
		t.Error("structuredBrief is not deterministic for the same goal")
	}
}
