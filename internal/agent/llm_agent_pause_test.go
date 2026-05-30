package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// pauseRegistry has the ask_user pause primitive plus text_response and a benign
// sibling (a read_tool_output stub) so intra-turn exclusivity (ask_user wins,
// siblings dropped) is observable.
func pauseRegistry() *tools.Registry {
	r := tools.NewRegistry()
	r.Register(tools.AskUser{})
	r.Register(tools.TextResponse{})
	r.Register(pauseSiblingTool{})
	return r
}

// pauseSiblingTool stands in for a non-ask_user tool batched alongside a pause.
// If it ever runs it records "sibling-ran" — but on a pause it must be DROPPED,
// so a RoleTool with this content appearing is the failure signal.
type pauseSiblingTool struct{}

func (pauseSiblingTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "read_tool_output",
		Summary:     "stub sibling",
		Description: "stub sibling",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Deferred:    false,
	}
}

func (pauseSiblingTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{Preview: "sibling-ran"}, nil
}

func newPauseAgent(t *testing.T, fc *agenttest.FakeClient) *agent.LlmAgent {
	t.Helper()
	return agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     fc,
		LLM:        llm.Config{Model: "test-model", Provider: "test", TotalTimeoutSec: 30},
		Registry:   pauseRegistry(),
		PreviewCap: 2048,
		RunDir:     t.TempDir(),
		SessionID:  uuid.Must(uuid.NewV7()).String(),
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "go"}},
	})
}

func newPauseIC(t *testing.T) agent.InvocationContext {
	t.Helper()
	steps := 25
	b, err := agent.NewBudget(agent.BudgetOptions{MaxSteps: &steps})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	return agent.InvocationContext{Ctx: context.Background(), RequestID: uuid.Must(uuid.NewV7()), Budget: b}
}

func countRoleTool(history []llm.Message) int {
	n := 0
	for _, m := range history {
		if m.Role == llm.RoleTool {
			n++
		}
	}
	return n
}

// lastAssistantToolCalls returns the tool_calls of the last assistant message
// that carried any (the rewritten pause message).
func lastAssistantToolCalls(history []llm.Message) []llm.ToolCall {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == llm.RoleAssistant && len(history[i].ToolCalls) > 0 {
			return history[i].ToolCalls
		}
	}
	return nil
}

func askUserCall(id, args string) llm.ToolCall {
	return agenttest.MakeToolCall(id, "ask_user", args)
}

// TestPause_SingleAskUser_EmitsEventNoRoleTool: a lone ask_user(kind=approval)
// pauses with exactly one Actions.AwaitingInput Event, appends NO RoleTool for
// it, and never uses the iter error slot (D-A1-03, Event-only termination).
func TestPause_SingleAskUser_EmitsEventNoRoleTool(t *testing.T) {
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(askUserCall("c1", `{"question":"deploy now?","kind":"approval","priority":40}`)),
	)
	a := newPauseAgent(t, fc)

	evs, err := collect(a.Run(newPauseIC(t)))
	if err != nil {
		t.Fatalf("pause must be Event-only, never the error slot; got err=%v", err)
	}

	var pauses []*agent.Event
	for _, ev := range evs {
		if ev.Actions.AwaitingInput != nil {
			pauses = append(pauses, ev)
		}
	}
	if len(pauses) != 1 {
		t.Fatalf("want exactly 1 pause Event, got %d (of %d events)", len(pauses), len(evs))
	}
	ai := pauses[0].Actions.AwaitingInput
	if ai.Question != "deploy now?" || ai.Kind != "approval" || ai.Priority != 40 || ai.ToolCallID != "c1" {
		t.Fatalf("pause payload wrong: %+v", ai)
	}
	if ai.OriginAgent == "" {
		t.Errorf("OriginAgent must be stamped for swarm forward-compat (D-A1-08)")
	}

	hist := a.HistoryForTest()
	if n := countRoleTool(hist); n != 0 {
		t.Errorf("no RoleTool may be appended for a pause, got %d", n)
	}
	tc := lastAssistantToolCalls(hist)
	if len(tc) != 1 || tc[0].ID != "c1" {
		t.Errorf("assistant message tool_calls = %+v, want exactly [c1]", tc)
	}
}

// TestPause_IntraTurnExclusivity: 2×ask_user + 1×sibling in one turn → exactly 2
// pause payloads, the sibling dropped (never executed → no RoleTool), and the
// assistant message rewritten to the 2 ask_user tool_calls only (D-A1-07).
func TestPause_IntraTurnExclusivity(t *testing.T) {
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(
			askUserCall("p1", `{"question":"q1","kind":"clarification"}`),
			agenttest.MakeToolCall("s1", "read_tool_output", `{"tool_call_id":"x"}`),
			askUserCall("p2", `{"question":"q2","kind":"approval","priority":10}`),
		),
	)
	a := newPauseAgent(t, fc)

	evs, err := collect(a.Run(newPauseIC(t)))
	if err != nil {
		t.Fatalf("error slot must stay nil on a pause, got %v", err)
	}

	var pauseIDs []string
	for _, ev := range evs {
		if ev.Actions.AwaitingInput != nil {
			pauseIDs = append(pauseIDs, ev.Actions.AwaitingInput.ToolCallID)
		}
	}
	if len(pauseIDs) != 2 || pauseIDs[0] != "p1" || pauseIDs[1] != "p2" {
		t.Fatalf("want 2 pause payloads in emit order [p1 p2], got %v", pauseIDs)
	}

	hist := a.HistoryForTest()
	if n := countRoleTool(hist); n != 0 {
		t.Errorf("sibling must be DROPPED, not executed; want 0 RoleTool, got %d", n)
	}
	tc := lastAssistantToolCalls(hist)
	if len(tc) != 2 || tc[0].ID != "p1" || tc[1].ID != "p2" {
		t.Errorf("assistant tool_calls must be ask_user-only [p1 p2], got %+v", tc)
	}
}

// TestPause_MalformedAskUser_NotAPause: an ask_user that fails validation is NOT
// a pause — it flows through normal dispatch as a RoleTool error and the loop is
// not suspended (no AwaitingInput Event).
func TestPause_MalformedAskUser_NotAPause(t *testing.T) {
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(askUserCall("c1", `{"question":"","kind":"approval"}`)),
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c2", "text_response", `{"text":"done"}`)),
	)
	a := newPauseAgent(t, fc)

	evs, err := collect(a.Run(newPauseIC(t)))
	if err != nil {
		t.Fatalf("Run errored: %v", err)
	}
	for _, ev := range evs {
		if ev.Actions.AwaitingInput != nil {
			t.Fatalf("a validation-failed ask_user must NOT pause, got %+v", ev.Actions.AwaitingInput)
		}
	}
	if n := countRoleTool(a.HistoryForTest()); n == 0 {
		t.Errorf("a malformed ask_user must surface as a RoleTool error, got 0 RoleTool messages")
	}
}
