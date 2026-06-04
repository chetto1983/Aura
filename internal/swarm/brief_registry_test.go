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
