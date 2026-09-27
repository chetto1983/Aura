package agent_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/pausable"
)

// heldTool holds its context's clocks for longer than the node timeout, the way
// an MCP call does while the operator fills in a form.
type heldTool struct{ hold time.Duration }

func (heldTool) Spec() tools.Spec {
	return tools.Spec{Name: "held", Summary: "holds its clocks", Parameters: json.RawMessage(`{"type":"object"}`)}
}

func (h heldTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	release := pausable.Hold(ctx)
	time.Sleep(h.hold)
	release()
	if err := ctx.Err(); err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Preview: "answered"}, nil
}

func TestRunToolNodeTimeoutStopsWhileHeld(t *testing.T) {
	recordingProvider(t)
	t.Setenv("AURA_LOOP_NODE_TIMEOUT_SEC", "1")

	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(heldTool{hold: 1500 * time.Millisecond})
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "held", `{}`)),
		agenttest.ToolCallTurn(textResponseCall("c2", "done")),
	)
	a := agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     fc,
		LLM:        llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 30},
		Registry:   reg,
		PreviewCap: 2048,
		RunDir:     t.TempDir(),
		SessionID:  uuid.Must(uuid.NewV7()).String(),
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "go"}},
	})

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: new(5)})))
	if err != nil {
		t.Fatalf("run errored: %v", err)
	}
	for _, ev := range evs {
		if ti := ev.Actions.ToolInvocation; ti != nil && ti.Event == agent.ToolInvocationEnd && ti.Error != "" {
			t.Fatalf("the 1s node timeout cut a tool that held its clock for 1.5s: %s", ti.Error)
		}
	}
}
