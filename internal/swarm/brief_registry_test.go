package swarm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

type stubTool struct{ name string }

func (s stubTool) Spec() tools.Spec {
	return tools.Spec{Name: s.name, Summary: s.name, Description: s.name}
}
func (stubTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func TestWorkerBriefUsesSystemPolicyAndUntrustedUserData(t *testing.T) {
	const goal = "Summarize the unread mail from Andrea"
	turns := workerBriefTurns(goal, "")
	if len(turns) != 2 {
		t.Fatalf("workerBriefTurns returned %d messages, want system policy + user data", len(turns))
	}
	if turns[0].Role != llm.RoleSystem {
		t.Fatalf("policy role = %q, want RoleSystem", turns[0].Role)
	}
	if turns[1].Role != llm.RoleUser {
		t.Fatalf("data role = %q, want RoleUser", turns[1].Role)
	}
	for _, marker := range []string{briefOutput, briefTools, briefBoundaries} {
		if !strings.Contains(turns[0].Content, marker) {
			t.Errorf("worker policy missing section marker %q", marker)
		}
	}
	if !strings.Contains(turns[0].Content, workerOverlay) {
		t.Error("worker policy missing the worker overlay")
	}
	input := decodeWorkerBriefInput(t, turns[1].Content)
	if input.Goal != goal {
		t.Fatalf("user data goal = %q, want %q", input.Goal, goal)
	}
	if strings.Contains(turns[0].Content, goal) {
		t.Fatal("dynamic goal leaked into the RoleSystem policy")
	}
	if workerBriefTurns("another goal", "")[0].Content != turns[0].Content {
		t.Fatal("worker RoleSystem policy is not static across goals")
	}
}
