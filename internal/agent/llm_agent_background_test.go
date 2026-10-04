package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

// heldTool answers once release is closed, or with its context's error.
type heldTool struct {
	foreground bool
	release    chan struct{}
}

func (h *heldTool) Spec() tools.Spec {
	return tools.Spec{Name: "held", Summary: "test", Foreground: h.foreground}
}

func (h *heldTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	select {
	case <-h.release:
		return tools.ToolResult{Preview: "late answer"}, nil
	case <-ctx.Done():
		return tools.ToolResult{}, ctx.Err()
	}
}

func heldCall() llm.ToolCall {
	call := llm.ToolCall{ID: "call-1", Type: "function"}
	call.Function.Name, call.Function.Arguments = "held", "{}"
	return call
}

// runHeld dispatches one held call through runTool with a one-second window, releasing
// the tool after releaseAfter.
func runHeld(t *testing.T, calls *tools.BackgroundCalls, tool *heldTool, releaseAfter time.Duration) toolRunResult {
	t.Helper()
	reg := tools.NewRegistry()
	reg.Register(tool)
	a := NewLlmAgent(LlmAgentConfig{
		Registry: reg, RunDir: t.TempDir(), SessionID: "sess-1", PreviewCap: 30000, BackgroundCalls: calls,
	})
	window, ceiling := 1, 30
	budget, err := NewBudget(BudgetOptions{BackgroundAfterSec: &window, BackgroundMaxSec: &ceiling})
	if err != nil {
		t.Fatal(err)
	}
	timer := time.AfterFunc(releaseAfter, func() { close(tool.release) })
	t.Cleanup(func() { timer.Stop() })
	return a.runTool(context.Background(), budget, heldCall(), time.Now())
}

func stopBackground(t *testing.T, calls *tools.BackgroundCalls) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := calls.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestRunToolMovesACallStillRunningAfterTheWindowToTheBackground(t *testing.T) {
	calls := tools.NewBackgroundCalls()
	defer stopBackground(t, calls)
	tool := &heldTool{release: make(chan struct{})}

	run := runHeld(t, calls, tool, 1500*time.Millisecond)
	if run.Err != "" || !strings.Contains(run.Preview, `"status":"in_progress"`) || !strings.Contains(run.Preview, `"tool":"held"`) {
		t.Fatalf("run = %+v, want the background notice in place of the held result", run)
	}
	if strings.Contains(run.Preview, "late answer") {
		t.Fatal("the turn waited for the call it was meant to leave behind")
	}
}

func TestRunToolKeepsAForegroundCallInItsTurn(t *testing.T) {
	calls := tools.NewBackgroundCalls()
	defer stopBackground(t, calls)
	tool := &heldTool{foreground: true, release: make(chan struct{})}

	if run := runHeld(t, calls, tool, 1200*time.Millisecond); run.Err != "" || !strings.Contains(run.Preview, "late answer") {
		t.Fatalf("run = %+v, want the foreground tool's own result", run)
	}
}

func TestRunToolWithoutABackgroundRegistryWaitsForTheCall(t *testing.T) {
	tool := &heldTool{release: make(chan struct{})}

	if run := runHeld(t, nil, tool, 1200*time.Millisecond); run.Err != "" || !strings.Contains(run.Preview, "late answer") {
		t.Fatalf("run = %+v, want the tool's own result", run)
	}
}

// A call that reaches execTool without runTool's limits (a reviewed message's dispatch)
// stays in its turn even when the agent has a registry.
func TestExecuteToolNeedsTheRunsLimitsToMoveACall(t *testing.T) {
	calls := tools.NewBackgroundCalls()
	defer stopBackground(t, calls)
	tool := &heldTool{release: make(chan struct{})}
	close(tool.release)

	res, err := (&LlmAgent{background: calls}).executeTool(context.Background(), tool, nil)
	if err != nil || res.Preview != "late answer" {
		t.Fatalf("executeTool = %+v, %v", res, err)
	}
}
