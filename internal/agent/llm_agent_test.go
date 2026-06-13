package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

const sentinelKey = "sk-or-SENTINEL-AGENT-DO-NOT-LEAK-0123456789"

// testRegistry builds a registry with text_response + a recording echo tool so
// tool-dispatch paths are exercised deterministically.
func testRegistry() *tools.Registry {
	r := tools.NewRegistry()
	r.Register(tools.TextResponse{})
	r.Register(&echoTool{})
	return r
}

// echoTool is a minimal non-deferred tool whose Execute echoes its args. It lets
// the agent test drive a non-terminal tool call without touching the network.
type echoTool struct{}

func (echoTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "echo",
		Summary:     "Echo the provided text back.",
		Description: "Echo the provided text back.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"v":{"type":"string"}}}`),
		Deferred:    false,
	}
}

func (echoTool) Execute(ctx context.Context, raw json.RawMessage) (tools.ToolResult, error) {
	return tools.NewResult(ctx, "echo:"+string(raw))
}

// newAgent builds an LlmAgent with the given fake client, a one-user-turn history,
// and explicit budget options threaded through the InvocationContext.
func newAgent(t *testing.T, fc *agenttest.FakeClient, cfg llm.Config) *agent.LlmAgent {
	t.Helper()
	if cfg.Model == "" {
		cfg.Model = "test-model"
	}
	if cfg.Provider == "" {
		cfg.Provider = "test-provider"
	}
	if cfg.TotalTimeoutSec == 0 {
		cfg.TotalTimeoutSec = 30
	}
	return agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     fc,
		LLM:        cfg,
		Registry:   testRegistry(),
		PreviewCap: 2048,
		RunDir:     t.TempDir(),
		SessionID:  uuid.Must(uuid.NewV7()).String(),
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "ciao"}},
	})
}

// newIC builds a fresh root InvocationContext with the given budget options.
func newIC(t *testing.T, opts agent.BudgetOptions) agent.InvocationContext {
	t.Helper()
	b, err := agent.NewBudget(opts)
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	return agent.InvocationContext{
		Ctx:       context.Background(),
		RequestID: uuid.Must(uuid.NewV7()),
		Budget:    b,
	}
}

// textResponseCall builds a finalized text_response tool call.
func textResponseCall(id, text string) llm.ToolCall {
	args, _ := json.Marshal(map[string]string{"text": text})
	return agenttest.MakeToolCall(id, "text_response", string(args))
}

// recordingProvider installs an in-memory span recorder and shuts it on cleanup.
func recordingProvider(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return rec
}

// collect ranges over Run, returning the ordered events and the first error.
func collect(seq func(yield func(*agent.Event, error) bool)) ([]*agent.Event, error) {
	var evs []*agent.Event
	var firstErr error
	for ev, err := range seq {
		if err != nil {
			firstErr = err
			break
		}
		if ev != nil {
			evs = append(evs, ev)
		}
	}
	return evs, firstErr
}

func TestLlmAgent_StreamErrDoesNotFinalizePartialText(t *testing.T) {
	streamErr := errors.New("mid-stream reset")
	fc := agenttest.NewFakeClient(agenttest.TextThenErr(streamErr, "partial answer"))
	a := newAgent(t, fc, llm.Config{})

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: ptr(2)})))
	if !errors.Is(err, streamErr) {
		t.Fatalf("Run error = %v, want %v", err, streamErr)
	}
	for _, ev := range evs {
		if ev.LLMResponse != nil && ev.LLMResponse.FinishReason != "" {
			t.Fatalf("partial stream was emitted as terminal answer: %+v", ev.LLMResponse)
		}
	}
}

func TestLlmAgent_RetryableStreamErrSalvagesOnce(t *testing.T) {
	streamErr := errors.New("connection reset by peer")
	fc := agenttest.NewFakeClient(
		agenttest.TextThenErr(streamErr, "partial answer"),
		agenttest.TextChunks("stop", "clean answer"),
	)
	a := newAgent(t, fc, llm.Config{})

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: ptr(3)})))
	if err != nil {
		t.Fatalf("Run error = %v, want retry success", err)
	}
	if fc.CallCount() != 2 {
		t.Fatalf("stream should be retried once, calls=%d", fc.CallCount())
	}
	final := evs[len(evs)-1]
	if final.LLMResponse == nil || final.LLMResponse.FinishReason == "" || final.LLMResponse.Content != "clean answer" {
		t.Fatalf("final event = %+v, want clean retried answer", final.LLMResponse)
	}
	for _, ev := range evs[:len(evs)-1] {
		if ev.LLMResponse != nil && ev.LLMResponse.FinishReason != "" && strings.Contains(ev.LLMResponse.Content, "partial answer") {
			t.Fatalf("partial stream was emitted as terminal answer: %+v", ev.LLMResponse)
		}
	}
}

func TestLlmAgent_ForwardsSessionID(t *testing.T) {
	fc := agenttest.NewFakeClient(agenttest.TextChunks("stop", "ciao"))
	const sessionID = "conv-session-123"
	a := agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:    fc,
		LLM:       llm.Config{Model: "m", Provider: "openrouter", TotalTimeoutSec: 30},
		Registry:  testRegistry(),
		RunDir:    t.TempDir(),
		SessionID: sessionID,
		UserTurns: []llm.Message{{Role: llm.RoleUser, Content: "ciao"}},
	})

	if _, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: ptr(1)}))); err != nil {
		t.Fatalf("Run errored: %v", err)
	}
	if got := fc.LastRequest().SessionID; got != sessionID {
		t.Fatalf("request SessionID = %q, want %q", got, sessionID)
	}
}

// TestLlmAgent_EventOrder (Req#9): a tool call then a text_response yields ordered
// Events (tool_call -> tool_result -> final) and terminates.
func TestLlmAgent_EventOrder(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "echo", `{"v":"hi"}`)),
		agenttest.WithUsage(agenttest.ToolCallTurn(textResponseCall("c2", "Fatto.")), llm.Usage{PromptTokens: 5, CompletionTokens: 2}),
	)
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(25)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("Run errored: %v", err)
	}
	if len(evs) < 3 {
		t.Fatalf("got %d events, want >=3 (tool_call, tool_result, final)", len(evs))
	}
	last := evs[len(evs)-1]
	if last.LLMResponse == nil || last.LLMResponse.Content != "Fatto." {
		t.Errorf("final event content = %+v, want 'Fatto.'", last.LLMResponse)
	}
	for _, ev := range evs {
		if ev.Timestamp.IsZero() {
			t.Error("event carries a zero Timestamp (Req#14)")
		}
	}
}

func TestLlmAgent_EmitsToolInvocationStartAndEndMetadata(t *testing.T) {
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "echo", `{"v":"hi"}`)),
		agenttest.ToolCallTurn(textResponseCall("c2", "done")),
	)
	a := newAgent(t, fc, llm.Config{})
	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: ptr(4)})))
	if err != nil {
		t.Fatalf("Run errored: %v", err)
	}

	var start, end *agent.ToolInvocation
	for _, ev := range evs {
		if ev.Actions.ToolInvocation == nil {
			continue
		}
		switch ev.Actions.ToolInvocation.Event {
		case agent.ToolInvocationStart:
			start = ev.Actions.ToolInvocation
		case agent.ToolInvocationEnd:
			end = ev.Actions.ToolInvocation
		}
	}
	if start == nil {
		t.Fatal("missing tool invocation start event")
	}
	if end == nil {
		t.Fatal("missing tool invocation end event")
	}

	if start.ToolCallID != "c1" || start.ToolName != "echo" || start.Arguments != `{"v":"hi"}` {
		t.Fatalf("start metadata = %+v", start)
	}
	if start.ArgsBytes != len(`{"v":"hi"}`) || start.StartedAt == nil {
		t.Fatalf("start byte/timestamp metadata = %+v", start)
	}

	wantPreview := `echo:{"v":"hi"}`
	if end.ToolCallID != "c1" || end.ToolName != "echo" || end.Status != "ok" {
		t.Fatalf("end identity/status metadata = %+v", end)
	}
	if end.StartedAt == nil || end.EndedAt == nil || end.EndedAt.Before(*end.StartedAt) {
		t.Fatalf("end timestamps are not coherent: %+v", end)
	}
	if end.ResultPreview != wantPreview || end.ResultBytes != len(wantPreview) ||
		end.PreviewBytes != len(wantPreview) || end.ResultTruncated {
		t.Fatalf("end result metadata = %+v", end)
	}
}

// TestLlmAgent_StepCap_Trips (Req#10 + Req#3/#4): distinct-arg tool calls exhaust
// max_steps. The FIRST trip recovers (one bypass turn), whose distinct-arg echo
// loops to the SECOND trip, which finalizes — the terminal Event carries
// limit_hit=max_steps. The finalize synthesis answers on its first call, so the
// ceiling is MaxSteps+2 = 5, never a runaway.
func TestLlmAgent_StepCap_Trips(t *testing.T) {
	recordingProvider(t)
	const maxSteps = 3
	turns := make([]agenttest.FakeTurn, 0, maxSteps+2)
	// maxSteps+1 distinct-arg echoes: maxSteps budgeted + 1 recovery turn (the
	// recovery turn re-enters the loop and re-trips the exhausted budget).
	for i := 0; i < maxSteps+1; i++ {
		turns = append(turns, agenttest.ToolCallTurn(agenttest.MakeToolCall("c", "echo", `{"v":"`+string(rune('a'+i))+`"}`)))
	}
	turns = append(turns, agenttest.TextChunks("stop", "finale")) // finalize synthesis (first-try)
	fc := agenttest.NewFakeClient(turns...)
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(maxSteps)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("step-cap surfaced an error slot (must be a terminal Event): %v", err)
	}
	last := evs[len(evs)-1]
	if last.Actions.StateDelta["limit_hit"] != "max_steps" {
		t.Errorf("terminal limit_hit = %v, want max_steps", last.Actions.StateDelta["limit_hit"])
	}
	if fc.CallCount() > maxSteps+2 {
		t.Errorf("client called %d times, want <= %d (3 step cap + 1 recovery + 1 forced finalize)", fc.CallCount(), maxSteps+2)
	}
}

// TestLlmAgent_WallclockCap_Trips (Req#10 + Req#3 + Req#4): an injected clock past
// the deadline trips wallclock on every ConsumeStep. The FIRST trip recovers (one
// nudge + one bypass turn outside the budget); the recovery turn issues a tool call
// so the loop re-enters and the SECOND wallclock trip routes to finalize — the
// terminal Event carries limit_hit=wallclock. Calls: recovery turn (1) + finalize
// (1) = 2, never the old prose-less terminalBudgetEvent.
func TestLlmAgent_WallclockCap_Trips(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c", "echo", `{"v":"x"}`)), // recovery turn (bypass)
		agenttest.TextChunks("stop", "finale"),                                   // finalize synthesis
	)
	a := newAgent(t, fc, llm.Config{})

	base := time.Now()
	calls := 0
	clock := func() time.Time {
		calls++
		if calls == 1 {
			return base // construction anchor
		}
		return base.Add(2 * time.Hour) // every ConsumeStep check is past the deadline
	}
	b, err := agent.NewBudget(agent.BudgetOptions{MaxWallclockSec: ptr(1), Now: clock})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	ic := agent.InvocationContext{Ctx: context.Background(), RequestID: uuid.Must(uuid.NewV7()), Budget: b}

	evs, rerr := collect(a.Run(ic))
	if rerr != nil {
		t.Fatalf("wallclock surfaced an error slot: %v", rerr)
	}
	last := evs[len(evs)-1]
	if last.Actions.StateDelta["limit_hit"] != "wallclock" {
		t.Errorf("limit_hit = %v, want wallclock", last.Actions.StateDelta["limit_hit"])
	}
	if fc.CallCount() != 2 {
		t.Errorf("client called %d times, want 2 (recovery turn + forced finalize)", fc.CallCount())
	}
}

// TestLlmAgent_DedupWindow_Trips (Req#10): a repeated identical tool call trips
// dedup as a terminal Event.
func TestLlmAgent_DedupWindow_Trips(t *testing.T) {
	recordingProvider(t)
	turns := make([]agenttest.FakeTurn, 0, 10)
	for i := 0; i < 10; i++ {
		turns = append(turns, agenttest.ToolCallTurn(agenttest.MakeToolCall("c", "echo", `{"v":"same"}`)))
	}
	fc := agenttest.NewFakeClient(turns...)
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(50), DedupWindow: ptr(3)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("dedup surfaced an error slot: %v", err)
	}
	last := evs[len(evs)-1]
	if last.Actions.StateDelta["limit_hit"] != "dedup" {
		t.Errorf("limit_hit = %v, want dedup", last.Actions.StateDelta["limit_hit"])
	}
}

// TestSpan_PerCall (Req#13): exactly one llm.request span per LLM call with a
// stable, valid span_id; and req.Messages is byte-identical pre/post the call.
func TestSpan_PerCall(t *testing.T) {
	rec := recordingProvider(t)
	fc := agenttest.NewFakeClient(
		agenttest.WithUsage(agenttest.ToolCallTurn(textResponseCall("c1", "ok")), llm.Usage{PromptTokens: 9, CompletionTokens: 3, CachedTokens: 4}),
	)
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(25)})

	if _, err := collect(a.Run(ic)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Exactly one llm.request span per LLM call (one call here). The run also records
	// an agent.turn wrapper span (O-08), so count the llm.request spans specifically
	// rather than the total — the per-call contract is what this test guards.
	var llmSpans int
	for _, s := range rec.Ended() {
		if s.Name() != "llm.request" {
			continue
		}
		llmSpans++
		if !s.SpanContext().SpanID().IsValid() {
			t.Errorf("llm.request span_id invalid: %v", s.SpanContext().SpanID())
		}
	}
	if llmSpans != 1 {
		t.Fatalf("recorded %d llm.request spans, want exactly 1 (one call)", llmSpans)
	}
}

// TestMessagesImmutable (Req#13): the request the client received carries the
// system prompt at messages[0] and the agent never mutated a prior snapshot.
func TestMessagesImmutable(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "echo", `{"v":"a"}`)),
		agenttest.ToolCallTurn(textResponseCall("c2", "done")),
	)
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(25)})
	if _, err := collect(a.Run(ic)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The first recorded request's messages[0] must equal the system prompt and be
	// untouched by the later in-place history append.
	first := fc.Requests[0]
	if first.Messages[0].Role != llm.RoleSystem {
		t.Fatalf("messages[0].Role = %q, want system", first.Messages[0].Role)
	}
	if first.Messages[0].Content != agent.SystemPrompt {
		t.Error("messages[0] is not the byte-stable system prompt")
	}
	// The conversation prefix is system+user; live volatile hints (07.1-04, Req#6)
	// tail-inject a trailing user message to a COPY, so the first request is
	// system+user+hints = 3 messages. The block must be the LAST message (never the
	// prefix) so messages[0] stays cache-stable.
	if len(first.Messages) != 3 {
		t.Errorf("first call had %d messages, want 3 (system+user+hints) — prefix mutated?", len(first.Messages))
	}
	tail := first.Messages[len(first.Messages)-1]
	if tail.Role != llm.RoleUser || !strings.HasPrefix(tail.Content, "<budget>") {
		t.Errorf("last message = {%q,%q}, want the trailing volatile user hint block", tail.Role, tail.Content)
	}
	if !strings.Contains(tail.Content, "<current_time>") || !strings.Contains(tail.Content, "<today>") {
		t.Errorf("trailing hint block missing current time/today tags: %q", tail.Content)
	}
	if first.Messages[1].Content != "ciao" {
		t.Errorf("messages[1] = %q, want the original user turn (hints must not displace it)", first.Messages[1].Content)
	}
}

// TestLlmAgent_ToolError (D-15): an unknown tool becomes a RoleTool error message
// the loop continues from; a real Stream error surfaces in the error slot.
func TestLlmAgent_ToolError(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "no_such_tool", `{}`)),
		agenttest.ToolCallTurn(textResponseCall("c2", "recovered")),
	)
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(25)})
	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("unknown tool must NOT surface as an error slot: %v", err)
	}
	last := evs[len(evs)-1]
	if last.LLMResponse == nil || last.LLMResponse.Content != "recovered" {
		t.Errorf("loop did not continue past the unknown tool: %+v", last.LLMResponse)
	}

	// Real infra failure → error slot.
	infraErr := errors.New("wire dead")
	fc2 := agenttest.NewFakeClient(agenttest.FakeTurn{Err: infraErr})
	a2 := newAgent(t, fc2, llm.Config{})
	ic2 := newIC(t, agent.BudgetOptions{MaxSteps: ptr(25)})
	if _, rerr := collect(a2.Run(ic2)); !errors.Is(rerr, infraErr) {
		t.Errorf("Stream error = %v, want it surfaced in the iter.Seq2 error slot", rerr)
	}
}

// TestLlmAgent_SequentialToolCalls (D-14): two tool calls in one assistant message
// dispatch sequentially, two RoleTool results appended in order, then one next call.
func TestLlmAgent_SequentialToolCalls(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(
			agenttest.MakeToolCall("c1", "echo", `{"v":"one"}`),
			agenttest.MakeToolCall("c2", "echo", `{"v":"two"}`),
		),
		agenttest.ToolCallTurn(textResponseCall("c3", "done")),
	)
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(25)})
	if _, err := collect(a.Run(ic)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The SECOND call's request must contain both RoleTool results in c1,c2 order.
	second := fc.Requests[1]
	var toolMsgs []llm.Message
	for _, m := range second.Messages {
		if m.Role == llm.RoleTool {
			toolMsgs = append(toolMsgs, m)
		}
	}
	if len(toolMsgs) != 2 {
		t.Fatalf("second call saw %d tool results, want 2", len(toolMsgs))
	}
	if toolMsgs[0].ToolCallID != "c1" || toolMsgs[1].ToolCallID != "c2" {
		t.Errorf("tool result order = (%q,%q), want (c1,c2)", toolMsgs[0].ToolCallID, toolMsgs[1].ToolCallID)
	}
	if fc.CallCount() != 2 {
		t.Errorf("client called %d times, want 2 (one batch dispatch then one next call)", fc.CallCount())
	}
}

// TestPrefixStable (Req#14): messages[0] is byte-identical across two turns.
func TestPrefixStable(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "echo", `{"v":"a"}`)),
		agenttest.ToolCallTurn(textResponseCall("c2", "done")),
	)
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(25)})
	if _, err := collect(a.Run(ic)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fc.Requests) < 2 {
		t.Fatalf("want >=2 requests, got %d", len(fc.Requests))
	}
	first, _ := json.Marshal(fc.Requests[0].Messages[0])
	second, _ := json.Marshal(fc.Requests[1].Messages[0])
	if string(first) != string(second) {
		t.Errorf("messages[0] drifted across turns:\n turn1: %s\n turn2: %s", first, second)
	}
}

// TestLlmAgent_SecretRedaction (D-28, release-blocking): the sentinel key appears
// in NO emitted Event, recorded span, or error.
func TestLlmAgent_SecretRedaction(t *testing.T) {
	rec := recordingProvider(t)
	fc := agenttest.NewFakeClient(
		agenttest.WithUsage(agenttest.ToolCallTurn(textResponseCall("c1", "ok")), llm.Usage{PromptTokens: 1}),
	)
	cfg := llm.Config{APIKey: sentinelKey, Model: "m", Provider: "p"}
	a := newAgent(t, fc, cfg)
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(25)})

	evs, err := collect(a.Run(ic))
	if err != nil && strings.Contains(err.Error(), sentinelKey) {
		t.Fatal("API key leaked into the error slot")
	}
	for _, ev := range evs {
		blob, _ := json.Marshal(ev)
		if strings.Contains(string(blob), sentinelKey) {
			t.Fatalf("API key leaked into an Event: %s", blob)
		}
	}
	for _, s := range rec.Ended() {
		for _, kv := range s.Attributes() {
			if strings.Contains(kv.Value.AsString(), sentinelKey) || strings.Contains(string(kv.Key), sentinelKey) {
				t.Fatalf("API key leaked into a span attribute: %s=%v", kv.Key, kv.Value)
			}
		}
	}
}

// TestLlmAgent_LengthTruncation (D-21): finish_reason "length" with content (no
// tool call) → the final answer carries the truncation notice, no auto-continue.
func TestLlmAgent_LengthTruncation(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(agenttest.TextChunks("length", "parziale"))
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(25)})
	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	last := evs[len(evs)-1]
	if last.LLMResponse == nil || !strings.Contains(last.LLMResponse.Content, "[risposta troncata: max_tokens]") {
		t.Errorf("final content = %+v, want the truncation notice", last.LLMResponse)
	}
	if fc.CallCount() != 1 {
		t.Errorf("client called %d times, want 1 (no auto-continue)", fc.CallCount())
	}
}

// TestLlmAgent_ContentStopTextResponsePayloadUnwrapped hardens the fallback path
// seen with live providers that occasionally emit text_response args as content.
func TestLlmAgent_ContentStopTextResponsePayloadUnwrapped(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(agenttest.TextChunks("stop", `{"text":"risposta pulita"}`))
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(25)})
	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	last := evs[len(evs)-1]
	if last.LLMResponse == nil {
		t.Fatalf("last event missing LLMResponse: %+v", last)
	}
	if got := last.LLMResponse.Content; got != "risposta pulita" {
		t.Fatalf("final content = %q, want unwrapped text_response.text", got)
	}
}

// ptr is a tiny int-pointer helper for BudgetOptions.
func ptr(n int) *int { return &n }
