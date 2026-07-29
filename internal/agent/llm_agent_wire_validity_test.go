package agent_test

import (
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
)

func assertHistoryToolCallsAnswered(t *testing.T, hist []llm.Message) {
	t.Helper()
	for i, msg := range hist {
		if msg.Role != llm.RoleAssistant || len(msg.ToolCalls) == 0 {
			continue
		}
		pending := map[string]string{}
		for _, call := range msg.ToolCalls {
			pending[call.ID] = call.Function.Name
		}
		for j := i + 1; j < len(hist) && hist[j].Role == llm.RoleTool; j++ {
			delete(pending, hist[j].ToolCallID)
		}
		if len(pending) > 0 {
			t.Fatalf("assistant history[%d] has unanswered tool calls before the next non-tool message: pending=%v history=%#v",
				i, pending, hist)
		}
	}
}

func assertRequestsToolCallsAnswered(t *testing.T, requests []llm.Request) {
	t.Helper()
	for i, req := range requests {
		t.Run("request_"+string(rune('A'+i)), func(t *testing.T) {
			assertHistoryToolCallsAnswered(t, req.Messages)
		})
	}
}

func TestLlmAgent_DedupRecoveryKeepsSiblingToolCallsAnswered(t *testing.T) {
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "echo", `{"v":"same"}`)),
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c2", "echo", `{"v":"same"}`)),
		agenttest.ToolCallTurn(
			agenttest.MakeToolCall("c3", "echo", `{"v":"same"}`),
			agenttest.MakeToolCall("c4", "echo", `{"v":"later"}`),
		),
		agenttest.ToolCallTurn(textResponseCall("c5", "recovered")),
	)
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: new(50), DedupWindow: new(3)})

	if _, err := collect(a.Run(ic)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertRequestsToolCallsAnswered(t, fc.Requests)
}

func TestCompletionGateVetoKeepsSiblingToolCallsAnswered(t *testing.T) {
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(mutatingCall("c1")),
		agenttest.ToolCallTurn(
			textResponseCall("c2", "I wrote the script, you run it"),
			echoCall("c3"),
		),
		agenttest.TextChunks("stop", "NOT_DONE: the output was never produced"),
		agenttest.ToolCallTurn(textResponseCall("c4", "file produced and verified")),
	)
	a := newGateAgent(t, fc, true)

	if _, err := collect(a.Run(newIC(t, agent.BudgetOptions{}))); err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertRequestsToolCallsAnswered(t, fc.Requests)
}
