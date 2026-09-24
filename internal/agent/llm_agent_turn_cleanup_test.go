package agent_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// leaveFileTool stands in for an MCP tool that materialized a file: it registers a
// cleanup step with its turn and counts how often that step runs.
type leaveFileTool struct{ cleaned *atomic.Int32 }

func (leaveFileTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "leave_file",
		Summary:     "Leave a file for the turn to clean up.",
		Description: "Leave a file for the turn to clean up.",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Deferred:    false,
	}
}

func (l leaveFileTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	tools.TurnCleanupFromContext(ctx).Add("file", func(context.Context) error {
		l.cleaned.Add(1)
		return nil
	})
	return tools.NewResult(ctx, "left a file")
}

func newCleanupAgent(t *testing.T, client llm.Client, cleaned *atomic.Int32) *agent.LlmAgent {
	t.Helper()
	r := tools.NewRegistry()
	r.Register(tools.TextResponse{})
	r.Register(tools.AskUser{})
	r.Register(leaveFileTool{cleaned: cleaned})
	return agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     client,
		LLM:        llm.Config{Model: "test-model", Provider: "test", TotalTimeoutSec: 30},
		Registry:   r,
		PreviewCap: 2048,
		RunDir:     t.TempDir(),
		SessionID:  uuid.Must(uuid.NewV7()).String(),
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "go"}},
	})
}

func TestLlmAgent_TurnCleanupRunsOnceWhenTheTurnEnds(t *testing.T) {
	var cleaned atomic.Int32
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "leave_file", `{}`)),
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c2", "leave_file", `{}`)),
		agenttest.ToolCallTurn(textResponseCall("c3", "done")),
	)

	if _, err := collect(newCleanupAgent(t, fc, &cleaned).Run(newIC(t, agent.BudgetOptions{MaxSteps: new(5)}))); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := cleaned.Load(); got != 1 {
		t.Fatalf("cleanup ran %d times, want once for the one key two calls registered", got)
	}
}

func TestLlmAgent_TurnCleanupRunsWhenTheTurnPausesForTheUser(t *testing.T) {
	var cleaned atomic.Int32
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "leave_file", `{}`)),
		agenttest.ToolCallTurn(askUserCall("c2", `{"question":"deploy now?","kind":"approval","priority":40}`)),
	)

	if _, err := collect(newCleanupAgent(t, fc, &cleaned).Run(newPauseIC(t))); err != nil {
		t.Fatalf("a pause is Event-only; Run: %v", err)
	}
	if got := cleaned.Load(); got != 1 {
		t.Fatalf("cleanup ran %d times across an ask_user pause, want 1", got)
	}
}

// panicOnSecondStream answers the first model call from its script and panics on the
// second: a crash after a tool has already left something in the box.
type panicOnSecondStream struct {
	script *agenttest.FakeClient
	calls  int
}

func (p *panicOnSecondStream) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	p.calls++
	if p.calls == 2 {
		panic("model client crashed")
	}
	return p.script.Stream(ctx, req)
}

func TestLlmAgent_TurnCleanupRunsWhenTheTurnPanics(t *testing.T) {
	var cleaned atomic.Int32
	client := &panicOnSecondStream{script: agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "leave_file", `{}`)),
	)}

	if _, err := collect(newCleanupAgent(t, client, &cleaned).Run(newIC(t, agent.BudgetOptions{MaxSteps: new(5)}))); err == nil {
		t.Fatal("a panicking model client must surface as the Run error")
	}
	if got := cleaned.Load(); got != 1 {
		t.Fatalf("cleanup ran %d times after a panic, want 1", got)
	}
}
