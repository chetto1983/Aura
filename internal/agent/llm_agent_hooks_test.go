package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

type recordingHook struct {
	events []string

	beforeModel func(context.Context, *llm.Request) (*agent.ModelHookResult, error)
	beforeTool  func(context.Context, llm.ToolCall) (*agent.ToolHookResult, error)
	afterTool   func(context.Context, llm.ToolCall, tools.ToolResult) (*agent.ToolResultHookResult, error)
}

func (h *recordingHook) OnTurnStart(context.Context, agent.HookTurn) error {
	h.events = append(h.events, "turn_start")
	return nil
}

func (h *recordingHook) BeforeModel(ctx context.Context, req *llm.Request) (*agent.ModelHookResult, error) {
	h.events = append(h.events, "before_model")
	if h.beforeModel == nil {
		return nil, nil
	}
	return h.beforeModel(ctx, req)
}

func (h *recordingHook) BeforeTool(ctx context.Context, call llm.ToolCall) (*agent.ToolHookResult, error) {
	h.events = append(h.events, "before_tool:"+call.Function.Arguments)
	if h.beforeTool == nil {
		return nil, nil
	}
	return h.beforeTool(ctx, call)
}

func (h *recordingHook) AfterTool(ctx context.Context, call llm.ToolCall, res tools.ToolResult) (*agent.ToolResultHookResult, error) {
	h.events = append(h.events, "after_tool:"+res.Preview)
	if h.afterTool == nil {
		return nil, nil
	}
	return h.afterTool(ctx, call, res)
}

func (h *recordingHook) OnTurnEnd(context.Context, agent.HookTurn) error {
	h.events = append(h.events, "turn_end")
	return nil
}

func newHookAgent(t *testing.T, fc *agenttest.FakeClient, hooks *agent.HookManager) *agent.LlmAgent {
	t.Helper()
	return agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:      fc,
		LLM:         llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 30},
		Registry:    testRegistry(),
		PreviewCap:  2048,
		RunDir:      t.TempDir(),
		SessionID:   "hook-session",
		UserTurns:   []llm.Message{{Role: llm.RoleUser, Content: "vai"}},
		HookManager: hooks,
	})
}

func TestLlmAgentHooks_NilManagerNoop(t *testing.T) {
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "echo", `{"v":"a"}`)),
		agenttest.ToolCallTurn(textResponseCall("c2", "done")),
	)
	a := newHookAgent(t, fc, nil)

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: ptr(25)})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := evs[len(evs)-1].LLMResponse.Content; got != "done" {
		t.Fatalf("final content = %q, want done", got)
	}
	if fc.CallCount() != 2 {
		t.Fatalf("CallCount = %d, want 2", fc.CallCount())
	}
}

func TestLlmAgentHooks_BeforeModelShortCircuitsAfterBudget(t *testing.T) {
	h := &recordingHook{
		beforeModel: func(context.Context, *llm.Request) (*agent.ModelHookResult, error) {
			return &agent.ModelHookResult{Content: "synthetic answer", FinishReason: "hook"}, nil
		},
	}
	a := newHookAgent(t, agenttest.NewFakeClient(agenttest.TextChunks("stop", "unreached")), agent.NewHookManager(h))
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(1)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := evs[len(evs)-1].LLMResponse.Content; got != "synthetic answer" {
		t.Fatalf("final content = %q, want hook answer", got)
	}
	if got := ic.Budget.Remaining(); got != 0 {
		t.Fatalf("budget remaining = %d, want 0 (ConsumeStep fires before hook)", got)
	}
}

func TestLlmAgentHooks_BeforeToolCanRewriteArgsAndAuditUsesRewrite(t *testing.T) {
	h := &recordingHook{
		beforeTool: func(_ context.Context, call llm.ToolCall) (*agent.ToolHookResult, error) {
			call.Function.Arguments = `{"v":"rewritten"}`
			return &agent.ToolHookResult{Call: &call}, nil
		},
	}
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "echo", `{"v":"original"}`)),
		agenttest.ToolCallTurn(textResponseCall("c2", "done")),
	)
	a := newHookAgent(t, fc, agent.NewHookManager(h))

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: ptr(25)})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var start, end *agent.ToolInvocation
	for _, ev := range evs {
		ti := ev.Actions.ToolInvocation
		if ti == nil {
			continue
		}
		if ti.Event == agent.ToolInvocationStart {
			start = ti
		}
		if ti.Event == agent.ToolInvocationEnd {
			end = ti
		}
	}
	if start == nil || end == nil {
		t.Fatalf("missing tool invocation start/end: start=%v end=%v", start, end)
	}
	if start.Arguments != `{"v":"rewritten"}` || end.Arguments != `{"v":"rewritten"}` {
		t.Fatalf("audit args = start %q end %q, want rewritten", start.Arguments, end.Arguments)
	}
	if !strings.Contains(end.ResultPreview, "rewritten") {
		t.Fatalf("tool result preview = %q, want rewritten args echoed", end.ResultPreview)
	}
}

func TestLlmAgentHooks_BeforeToolVetoSkipsExecution(t *testing.T) {
	h := &recordingHook{
		beforeTool: func(context.Context, llm.ToolCall) (*agent.ToolHookResult, error) {
			res := tools.ToolResult{Preview: "synthetic veto", Bytes: len("synthetic veto")}
			return &agent.ToolHookResult{Result: &res}, nil
		},
	}
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "echo", `{"v":"real"}`)),
		agenttest.ToolCallTurn(textResponseCall("c2", "done")),
	)
	a := newHookAgent(t, fc, agent.NewHookManager(h))

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: ptr(25)})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var end *agent.ToolInvocation
	for _, ev := range evs {
		if ti := ev.Actions.ToolInvocation; ti != nil && ti.Event == agent.ToolInvocationEnd {
			end = ti
		}
	}
	if end == nil {
		t.Fatal("missing vetoed tool end event")
	}
	if end.ResultPreview != "synthetic veto" {
		t.Fatalf("result preview = %q, want synthetic veto", end.ResultPreview)
	}
	if strings.Contains(end.ResultPreview, "real") {
		t.Fatalf("veto did not skip real tool execution: %q", end.ResultPreview)
	}
}

func TestLlmAgentHooks_AfterToolCanRewriteResult(t *testing.T) {
	h := &recordingHook{
		afterTool: func(context.Context, llm.ToolCall, tools.ToolResult) (*agent.ToolResultHookResult, error) {
			res := tools.ToolResult{Preview: "after rewrite", Bytes: len("after rewrite")}
			return &agent.ToolResultHookResult{Result: &res}, nil
		},
	}
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "echo", `{"v":"real"}`)),
		agenttest.ToolCallTurn(textResponseCall("c2", "done")),
	)
	a := newHookAgent(t, fc, agent.NewHookManager(h))

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: ptr(25)})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var end *agent.ToolInvocation
	for _, ev := range evs {
		if ti := ev.Actions.ToolInvocation; ti != nil && ti.Event == agent.ToolInvocationEnd {
			end = ti
		}
	}
	if end == nil || end.ResultPreview != "after rewrite" {
		t.Fatalf("tool end = %+v, want after rewrite", end)
	}
	second := fc.Requests[1]
	var saw bool
	for _, m := range second.Messages {
		if m.Role == llm.RoleTool && m.Content == "after rewrite" {
			saw = true
		}
	}
	if !saw {
		b, _ := json.Marshal(second.Messages)
		t.Fatalf("rewritten tool result not threaded to next request: %s", b)
	}
}

func TestHookManagerFirstNonNilWins(t *testing.T) {
	first := &recordingHook{
		beforeModel: func(context.Context, *llm.Request) (*agent.ModelHookResult, error) {
			return &agent.ModelHookResult{Content: "first", FinishReason: "hook"}, nil
		},
	}
	second := &recordingHook{
		beforeModel: func(context.Context, *llm.Request) (*agent.ModelHookResult, error) {
			return &agent.ModelHookResult{Content: "second", FinishReason: "hook"}, nil
		},
	}
	a := newHookAgent(t, agenttest.NewFakeClient(agenttest.TextChunks("stop", "unreached")), agent.NewHookManager(first, second))
	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: ptr(25)})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := evs[len(evs)-1].LLMResponse.Content; got != "first" {
		t.Fatalf("final content = %q, want first hook result", got)
	}
	for _, ev := range second.events {
		if ev == "before_model" {
			t.Fatalf("second hook was consulted after first non-nil result: %v", second.events)
		}
	}
}
