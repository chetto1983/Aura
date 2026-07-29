package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// TestLlmAgent_Accessors covers the trivial Agent-interface accessors on a
// constructed LlmAgent: Name/Description default and explicit, SubAgents (leaf),
// and FindAgent self-match vs miss.
func TestLlmAgent_Accessors(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		a := agent.NewLlmAgent(agent.LlmAgentConfig{
			Client:   agenttest.NewFakeClient(),
			Registry: testRegistry(),
		})
		if a.Name() != "aura" {
			t.Errorf("Name() = %q, want the default 'aura'", a.Name())
		}
		if a.Description() == "" {
			t.Error("Description() defaulted to empty")
		}
		if a.SubAgents() != nil {
			t.Errorf("SubAgents() = %v, want nil (leaf)", a.SubAgents())
		}
		if a.FindAgent("aura") != agent.Agent(a) {
			t.Error("FindAgent(self-name) did not return self")
		}
		if a.FindAgent("other") != nil {
			t.Error("FindAgent(miss) did not return nil")
		}
	})

	t.Run("explicit_name_description", func(t *testing.T) {
		a := agent.NewLlmAgent(agent.LlmAgentConfig{
			Name:        "scout",
			Description: "a named worker",
			Client:      agenttest.NewFakeClient(),
			Registry:    testRegistry(),
		})
		if a.Name() != "scout" {
			t.Errorf("Name() = %q, want scout", a.Name())
		}
		if a.Description() != "a named worker" {
			t.Errorf("Description() = %q, want the explicit value", a.Description())
		}
		if a.FindAgent("scout") == nil {
			t.Error("FindAgent(explicit name) returned nil")
		}
		if a.FindAgent("aura") != nil {
			t.Error("FindAgent should not match the default name when one was set")
		}
	})
}

// erroringTool is a non-deferred tool whose Execute always fails, so the dispatch
// loop's runTool error-preview branch (D-15) is exercised: the failure becomes a
// RoleTool error message the loop continues from, never the iter.Seq2 error slot.
type erroringTool struct{}

func (erroringTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "boom",
		Summary:     "Always fails.",
		Description: "Always fails.",
		Parameters:  rawObject,
		Deferred:    false,
	}
}

func (erroringTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{}, errToolBoom
}

var rawObject = json.RawMessage(`{"type":"object"}`)

type toolBoom struct{}

func (toolBoom) Error() string { return "tool exploded" }

var errToolBoom = toolBoom{}

// TestLlmAgent_ToolExecuteError (D-15): a tool whose Execute returns an error
// becomes a RoleTool error preview the loop continues from; the next turn's
// text_response terminates normally. This drives dispatch's runTool error branch.
func TestLlmAgent_ToolExecuteError(t *testing.T) {
	recordingProvider(t)
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(erroringTool{})

	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "boom", `{}`)),
		agenttest.ToolCallTurn(textResponseCall("c2", "recovered")),
	)
	a := agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     fc,
		LLM:        llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 30},
		Registry:   reg,
		PreviewCap: 2048,
		RunDir:     t.TempDir(),
		SessionID:  uuid.Must(uuid.NewV7()).String(),
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "vai"}},
	})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: new(25)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("a tool Execute error must NOT surface as an error slot: %v", err)
	}
	last := evs[len(evs)-1]
	if last.LLMResponse == nil || last.LLMResponse.Content != "recovered" {
		t.Errorf("loop did not continue past the tool error: %+v", last.LLMResponse)
	}
	// The second request must carry a RoleTool message whose content is the error
	// preview the failing tool produced.
	second := fc.Requests[1]
	var sawErr bool
	for _, m := range second.Messages {
		if m.Role == llm.RoleTool && strings.HasPrefix(m.Content, "error:") {
			sawErr = true
		}
	}
	if !sawErr {
		t.Errorf("tool Execute error not threaded back as a RoleTool error preview: %+v", second.Messages)
	}
}

// TestLlmAgent_MalformedTextResponse (D-15): a text_response call with invalid /
// empty args is NOT terminal — the parse error is fed back as a RoleTool error and
// the loop continues to a well-formed terminal call. Drives parseTextResponse's
// error branches and dispatch's appendToolError path.
func TestLlmAgent_MalformedTextResponse(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		// invalid JSON args for the terminal tool
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "text_response", `{not json}`)),
		// then an empty-text terminal call (validation error)
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c2", "text_response", `{"text":"   "}`)),
		// finally a valid terminal call
		agenttest.ToolCallTurn(textResponseCall("c3", "ecco la risposta")),
	)
	a := newAgentCover(t, fc)
	ic := newIC(t, agent.BudgetOptions{MaxSteps: new(25)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("malformed terminal args must NOT error the loop: %v", err)
	}
	last := evs[len(evs)-1]
	if last.LLMResponse == nil || last.LLMResponse.Content != "ecco la risposta" {
		t.Errorf("loop did not self-correct to the valid terminal answer: %+v", last.LLMResponse)
	}
	// Both malformed turns must have appended a RoleTool error message (history grew
	// past the system+user seed with error previews before the valid answer).
	third := fc.Requests[2]
	var errCount int
	for _, m := range third.Messages {
		if m.Role == llm.RoleTool && strings.HasPrefix(m.Content, "error:") {
			errCount++
		}
	}
	if errCount < 2 {
		t.Errorf("saw %d RoleTool error messages before the valid call, want >=2 (parse + validation)", errCount)
	}
}

// TestLlmAgent_NonJSONToolArgs drives canonicaljson.CanonicalArgs' fallback
// branches: a tool call whose arguments are not valid JSON dedups on the raw bytes.
// Two identical malformed-arg calls trip the dedup window rather than crashing.
func TestLlmAgent_NonJSONToolArgs(t *testing.T) {
	recordingProvider(t)
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(&echoTool{})
	turns := make([]agenttest.FakeTurn, 0, 6)
	for range 6 {
		// Non-JSON arguments: CanonicalArgs falls back to the raw bytes; identical
		// raw bytes each turn still produce a stable dedup fingerprint.
		turns = append(turns, agenttest.ToolCallTurn(agenttest.MakeToolCall("c", "echo", `<<not-json>>`)))
	}
	fc := agenttest.NewFakeClient(turns...)
	a := agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     fc,
		LLM:        llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 30},
		Registry:   reg,
		PreviewCap: 2048,
		RunDir:     t.TempDir(),
		SessionID:  uuid.Must(uuid.NewV7()).String(),
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "vai"}},
	})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: new(50), DedupWindow: new(2)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("non-JSON tool args must not error the loop: %v", err)
	}
	last := evs[len(evs)-1]
	if last.Actions.StateDelta["limit_hit"] != "dedup" {
		t.Errorf("non-JSON identical args did not trip dedup: %v", last.Actions.StateDelta)
	}
}

// TestLlmAgent_UsageCostPresent drives usageStateDelta's Cost-set branch: when the
// provider sent a usage.cost, the final Event's StateDelta carries cost_usd. The
// existing event tests only cover the Cost==nil (key-absent) path.
func TestLlmAgent_UsageCostPresent(t *testing.T) {
	recordingProvider(t)
	cost := 0.004567
	fc := agenttest.NewFakeClient(
		agenttest.WithUsage(
			agenttest.ToolCallTurn(textResponseCall("c1", "fatto")),
			llm.Usage{PromptTokens: 10, CompletionTokens: 4, CachedTokens: 6, Cost: &cost},
		),
	)
	a := newAgentCover(t, fc)
	ic := newIC(t, agent.BudgetOptions{MaxSteps: new(25)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	last := evs[len(evs)-1]
	got, ok := last.Actions.StateDelta["cost_usd"]
	if !ok {
		t.Fatalf("final StateDelta missing cost_usd when usage.Cost was set: %v", last.Actions.StateDelta)
	}
	if got != cost {
		t.Errorf("cost_usd = %v, want %v", got, cost)
	}
}

// TestNewTracerProvider_Exported drives the exported NewTracerProvider for each
// mode (none/stdout/otlp), asserting construction succeeds and Shutdown is clean.
func TestNewTracerProvider_Exported(t *testing.T) {
	for _, mode := range []string{"none", "stdout", "otlp"} {
		t.Run(mode, func(t *testing.T) {
			endpoint := ""
			if mode == "otlp" {
				endpoint = "127.0.0.1:14317" // nothing listening — must silent-drop
			}
			tp, err := agent.NewTracerProvider(context.Background(), mode, endpoint)
			if err != nil {
				t.Fatalf("NewTracerProvider(%s): %v", mode, err)
			}
			if tp == nil {
				t.Fatalf("NewTracerProvider(%s) returned nil", mode)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := tp.Shutdown(ctx); err != nil {
				t.Logf("Shutdown(%s) returned %v (acceptable for otlp without a collector)", mode, err)
			}
		})
	}
}

// TestLlmAgent_DispatchStopsOnToolResult drives dispatch's yield-false branch on a
// tool-result Event (D-14): the consumer stops exactly as the first tool result is
// yielded, so dispatch returns done=true and Run returns cleanly without a further
// LLM call. This is a SAFE stop point (unlike a mid-stream consume stop) because
// dispatch's terminal `return true` is the last yield of the turn.
func TestLlmAgent_DispatchStopsOnToolResult(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "echo", `{"v":"x"}`)),
		agenttest.ToolCallTurn(textResponseCall("c2", "unreached")),
	)
	a := newAgentCover(t, fc)
	ic := newIC(t, agent.BudgetOptions{MaxSteps: new(25)})

	n := 0
	for _, err := range a.Run(ic) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		n++
		if n == 2 { // event 1 = tool_call, event 2 = tool_result -> stop here
			break
		}
	}
	if fc.CallCount() != 1 {
		t.Errorf("client called %d times, want 1 (consumer stopped at the tool result)", fc.CallCount())
	}
}

// TestLlmAgent_DispatchStopsOnParseError drives dispatch's yield-false branch on
// the malformed-terminal "parse error" tool-result (D-15): a text_response with
// invalid args yields a parse-error tool result; the consumer stops there and
// dispatch returns done=true cleanly.
func TestLlmAgent_DispatchStopsOnParseError(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "text_response", `{not json}`)),
		agenttest.ToolCallTurn(textResponseCall("c2", "unreached")),
	)
	a := newAgentCover(t, fc)
	ic := newIC(t, agent.BudgetOptions{MaxSteps: new(25)})

	n := 0
	for ev, err := range a.Run(ic) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// The malformed text_response yields a single "parse error" tool-result
		// Event first (no preceding tool_call Event for the terminal tool).
		if ev != nil && ev.LLMResponse != nil && ev.LLMResponse.Content == "parse error" {
			n++
			break
		}
	}
	if n != 1 {
		t.Fatalf("did not observe the parse-error tool-result Event")
	}
	if fc.CallCount() != 1 {
		t.Errorf("client called %d times, want 1 (consumer stopped at the parse-error result)", fc.CallCount())
	}
}

// TestLlmAgent_ConsumeStopMidStream is the regression for the iter.Seq2 stop-
// propagation bug in consume(): a consumer that breaks MID-STREAM (during the
// chunk/tool-call yields, not at a dispatch boundary) must not panic. Before the
// fix, consume ignored the false yield and returned the same tuple as a clean
// close, so Run proceeded to re-yield the final/tool Event after the range body
// had already returned false -> "range function continued iteration after for
// loop body returned false". The fix returns stopped=true so Run returns at once.
// Reaching the end of each sub-test without a panic IS the assertion.
func TestLlmAgent_ConsumeStopMidStream(t *testing.T) {
	t.Run("content_chunk", func(t *testing.T) {
		recordingProvider(t)
		fc := agenttest.NewFakeClient(
			agenttest.TextChunks("stop", "primo ", "secondo ", "terzo"),
		)
		a := newAgentCover(t, fc)
		ic := newIC(t, agent.BudgetOptions{MaxSteps: new(25)})

		n := 0
		for ev, err := range a.Run(ic) {
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ev != nil && ev.LLMResponse != nil && ev.LLMResponse.Content != "" {
				n++
				break // break on the FIRST streamed chunk — the dangerous mid-stream point
			}
		}
		if n != 1 {
			t.Fatalf("expected to break after the first streamed chunk, saw %d", n)
		}
	})

	t.Run("tool_call_chunk", func(t *testing.T) {
		recordingProvider(t)
		fc := agenttest.NewFakeClient(
			agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "echo", `{"v":"x"}`)),
		)
		a := newAgentCover(t, fc)
		ic := newIC(t, agent.BudgetOptions{MaxSteps: new(25)})

		for ev, err := range a.Run(ic) {
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ev != nil && ev.LLMResponse != nil && len(ev.LLMResponse.ToolCalls) > 0 {
				break // break on the tool_call Event, inside consume (before dispatch)
			}
		}
		if fc.CallCount() != 1 {
			t.Errorf("client called %d times, want 1 (consumer stopped inside consume)", fc.CallCount())
		}
	})
}

// newAgentCover builds a default LlmAgent over the echo+text_response registry for
// the coverage tests that don't need a custom registry.
func newAgentCover(t *testing.T, fc *agenttest.FakeClient) *agent.LlmAgent {
	t.Helper()
	return agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     fc,
		LLM:        llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 30},
		Registry:   testRegistry(),
		PreviewCap: 2048,
		RunDir:     t.TempDir(),
		SessionID:  uuid.Must(uuid.NewV7()).String(),
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "vai"}},
	})
}
