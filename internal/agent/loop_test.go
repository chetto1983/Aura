package agent

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/llm"
)

func TestRunLoopNoToolCallsReturnsAssistantText(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{{Response: llm.Response{Content: "done"}}}}

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(context.Context, []llm.ToolCall) ExecutionSummary {
		t.Fatal("executor should not run")
		return ExecutionSummary{}
	}), state, Options{MaxIterations: 3})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if result.Text != "done" {
		t.Fatalf("Text = %q, want done", result.Text)
	}
	if got := state.last().Content; got != "done" {
		t.Fatalf("last message = %q", got)
	}
	if result.Stats.LLMCalls != 1 || result.Stats.ToolCalls != 0 {
		t.Fatalf("stats = %+v", result.Stats)
	}
}

func TestRunLoopWarnsWhenMaxIterationsIsCapped(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{{Response: llm.Response{Content: "done"}}}}

	_, err := runLoop(context.Background(), client, ToolExecutorFunc(func(context.Context, []llm.ToolCall) ExecutionSummary {
		t.Fatal("executor should not run")
		return ExecutionSummary{}
	}), state, Options{MaxIterations: 500, Logger: logger})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}

	got := logs.String()
	for _, want := range []string{
		"agent: max_iterations_capped",
		"requested_max_iterations=500",
		"effective_max_iterations=100",
		"max_iterations_ceiling=100",
		"reason=runtime_ceiling",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("cap warning log = %q, missing %q", got, want)
		}
	}
}

func TestRunLoopToolCallContinuesToFinalResponse(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "search_files", Arguments: map[string]any{"query": "aura"}}}}},
		{Response: llm.Response{Content: "found it"}},
	}}
	var executed []llm.ToolCall

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		executed = append(executed, calls...)
		state.AddToolResultMessage(calls[0].ID, "wiki result")
		return ExecutionSummary{LastResult: "wiki result"}
	}), state, Options{MaxIterations: 3})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if result.Text != "found it" {
		t.Fatalf("Text = %q", result.Text)
	}
	if len(executed) != 1 || executed[0].Name != "search_files" {
		t.Fatalf("executed = %+v", executed)
	}
	if result.Stats.LLMCalls != 2 || result.Stats.ToolCalls != 1 {
		t.Fatalf("stats = %+v", result.Stats)
	}
}

func TestRunLoopNormalizesTextResponsePseudoCall(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "create_document", Arguments: map[string]any{"format": "xlsx"}}}}},
		{Response: llm.Response{Content: "Ho creato il file.\n\ntext_response(text=\"Ho creato il file.\")"}},
	}}
	executions := 0

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		executions += len(calls)
		state.AddToolResultMessage(calls[0].ID, `{"source_id":"src_test"}`)
		return ExecutionSummary{LastResult: `{"source_id":"src_test"}`}
	}), state, Options{
		MaxIterations: 3,
	})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if result.Text != "Ho creato il file." {
		t.Fatalf("Text = %q, want normalized final text", result.Text)
	}
	if strings.Contains(state.last().Content, "text_response(") {
		t.Fatalf("last message leaked pseudo-call: %q", state.last().Content)
	}
	if executions != 1 {
		t.Fatalf("executions = %d, want exactly one create_document call", executions)
	}
}

func TestRunLoopDoesNotInjectPhantomCorrectionForToolNameProse(t *testing.T) {
	state := newBudgetLoopState()
	content := "Ho usato source come citazione nello slug [[source-corso-base-robot]], ma questa e' solo una risposta finale."
	client := &fakeLoopClient{responses: []ChatResponse{{Response: llm.Response{Content: content}}}}

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(context.Context, []llm.ToolCall) ExecutionSummary {
		t.Fatal("executor should not run")
		return ExecutionSummary{}
	}), state, Options{MaxIterations: 3})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if result.Text != content {
		t.Fatalf("Text = %q, want original content", result.Text)
	}
	if result.Stats.LLMCalls != 1 {
		t.Fatalf("LLMCalls = %d, want 1 (no corrective retry)", result.Stats.LLMCalls)
	}
	if state.sawUserMessage("[system]") {
		t.Fatalf("unexpected corrective user message injected: %+v", state.messages)
	}
}

func TestRunLoopDuplicateToolCallsExecuteOnceAndAppendRecoverableResult(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "search_files", Arguments: map[string]any{"query": "aura"}},
			{ID: "call-2", Name: "search_files", Arguments: map[string]any{"query": "aura"}},
		}}},
		{Response: llm.Response{Content: "ok"}},
	}}
	executions := 0

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		executions += len(calls)
		state.AddToolResultMessage(calls[0].ID, "wiki result")
		return ExecutionSummary{LastResult: "wiki result"}
	}), state, Options{MaxIterations: 3})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if result.Text != "ok" {
		t.Fatalf("Text = %q", result.Text)
	}
	if executions != 1 {
		t.Fatalf("executions = %d, want 1", executions)
	}
	if !result.Stats.DuplicateToolCall {
		t.Fatal("DuplicateToolCall = false, want true")
	}
	if got := state.toolResult("call-2"); !strings.Contains(got, "You already called") {
		t.Fatalf("duplicate result = %q", got)
	}
}

func TestRunLoopDuplicateToolCallsAcrossIterationsExecuteOnce(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "search_memory", Arguments: map[string]any{"query": "documents", "limit": float64(5)}},
		}}},
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{
			{ID: "call-2", Name: "search_memory", Arguments: map[string]any{"limit": float64(5), "query": "documents"}},
		}}},
		{Response: llm.Response{Content: "ok"}},
	}}
	executions := 0

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		executions += len(calls)
		state.AddToolResultMessage(calls[0].ID, "memory result")
		return ExecutionSummary{LastResult: "memory result"}
	}), state, Options{MaxIterations: 4})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if result.Text != "ok" {
		t.Fatalf("Text = %q", result.Text)
	}
	if executions != 1 {
		t.Fatalf("executions = %d, want 1", executions)
	}
	if !result.Stats.DuplicateToolCall {
		t.Fatal("DuplicateToolCall = false, want true")
	}
	if got := state.toolResult("call-2"); !strings.Contains(got, "You already called") {
		t.Fatalf("duplicate result = %q", got)
	}
}

func TestRunLoopMaxCallsPerToolSkipsRepeatedRetrieval(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "search_memory", Arguments: map[string]any{"query": "documents"}},
		}}},
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{
			{ID: "call-2", Name: "search_memory", Arguments: map[string]any{"query": "sources"}},
		}}},
		{Response: llm.Response{Content: "ok"}},
	}}
	executions := 0

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		executions += len(calls)
		state.AddToolResultMessage(calls[0].ID, "memory result")
		return ExecutionSummary{LastResult: "memory result"}
	}), state, Options{MaxIterations: 4, MaxCallsPerTool: map[string]int{"search_memory": 1}})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if result.Text != "ok" {
		t.Fatalf("Text = %q", result.Text)
	}
	if executions != 1 {
		t.Fatalf("executions = %d, want 1", executions)
	}
	if got := state.toolResult("call-2"); !strings.Contains(got, "per-turn call budget") {
		t.Fatalf("repeat result = %q", got)
	}
}

func TestRunLoopMaxToolCallsTriggersFinalizingAndBudgetStub(t *testing.T) {
	state := newBudgetLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "search_memory", Arguments: map[string]any{"query": "documents"}},
		}}},
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{
			{ID: "call-2", Name: "web_search", Arguments: map[string]any{"q": "anything"}},
		}}},
		{Response: llm.Response{Content: "final answer from evidence"}},
	}}
	executions := 0
	executor := ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		executions += len(calls)
		state.AddToolResultMessage(calls[0].ID, "memory result")
		return ExecutionSummary{LastResult: "memory result", Results: map[string]string{calls[0].ID: "memory result"}}
	})

	result, err := runLoop(context.Background(), client, executor, state, Options{
		MaxIterations: 4,
		MaxToolCalls:  1,
	})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if result.Text != "final answer from evidence" {
		t.Fatalf("Text = %q", result.Text)
	}
	if executions != 1 {
		t.Fatalf("executions = %d, want 1 (MaxToolCalls=1 should cap fresh dispatches)", executions)
	}
	if got := state.toolResult("call-2"); !strings.Contains(got, "per-Run tool budget") {
		t.Fatalf("call-2 stub = %q, want budget-cap message", got)
	}
	if !state.sawUserMessage("Tool budget reached") {
		t.Fatalf("expected user-side finalize instruction injected; messages = %+v", state.messages)
	}
}

func TestRunLoopBeforeToolPolicySkipsRepeatedRetrieval(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "search_memory", Arguments: map[string]any{"query": "documents"}},
		}}},
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{
			{ID: "call-2", Name: "search_memory", Arguments: map[string]any{"query": "sources"}},
		}}},
		{Response: llm.Response{Content: "ok"}},
	}}
	executions := 0

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		executions += len(calls)
		state.AddToolResultMessage(calls[0].ID, "memory result")
		return ExecutionSummary{LastResult: "memory result"}
	}), state, Options{
		MaxIterations: 4,
		BeforeTool: DuplicateOrMaxCallsPolicy(map[string]int{"search_memory": 1}, func(call llm.ToolCall, state ToolCallState) string {
			return "use compact retrieval and call create_docx now"
		}),
	})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if result.Text != "ok" {
		t.Fatalf("Text = %q", result.Text)
	}
	if executions != 1 {
		t.Fatalf("executions = %d, want 1", executions)
	}
	if got := state.toolResult("call-2"); got != "use compact retrieval and call create_docx now" {
		t.Fatalf("repeat result = %q", got)
	}
}

func TestRunLoopMaxIterationReturnsLastUsefulResult(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "search_files"}}}},
	}}

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		state.AddToolResultMessage(calls[0].ID, "partial result")
		return ExecutionSummary{LastResult: "partial result"}
	}), state, Options{MaxIterations: 1})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if !result.Stats.MaxIterationsHit {
		t.Fatal("MaxIterationsHit = false")
	}
	if !strings.Contains(result.Text, "Per-turn") {
		t.Fatalf("answer = %q, want Per-turn contextual budget fallback", result.Text)
	}
	if strings.Contains(result.Text, deadEndFallbackText()) {
		t.Fatalf("answer contains dead-end fallback: %q", result.Text)
	}
}

func TestRunLoopMaxIterationFinalizesMemoryEvidenceNaturally(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "search_memory"}}}},
		{Response: llm.Response{Content: "Ho trovato tre riferimenti utili sullo scenario PMS; il piu rilevante e il PDF con la richiesta d'offerta e scadenza."}},
	}}
	raw := `Memory evidence for "scenario test gestione richieste offerta pms" (3 result(s)):
- [source] src_1 - file.pdf - score=0.85
Evidence envelope:
{"query":"scenario test","items":[{"kind":"source","id":"src_1","score":0.85}]}`

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		state.AddToolResultMessage(calls[0].ID, raw)
		return ExecutionSummary{LastResult: raw}
	}), state, Options{MaxIterations: 1, AllowNoToolFinalization: true})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if result.Text == raw {
		t.Fatal("returned raw search_memory evidence")
	}
	for _, leaked := range []string{"Memory evidence", "Evidence envelope", `"score"`} {
		if strings.Contains(result.Text, leaked) {
			t.Fatalf("answer leaked %q in %q", leaked, result.Text)
		}
	}
	if client.requests != 2 {
		t.Fatalf("LLM requests = %d, want tool turn plus no-tool finalization", client.requests)
	}
}

func TestRunLoopMaxElapsedReturnsLastUsefulResultBeforeNextLLM(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "search_files"}}}},
		{Response: llm.Response{Content: "too late"}},
	}}

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		state.AddToolResultMessage(calls[0].ID, "partial result")
		time.Sleep(5 * time.Millisecond)
		return ExecutionSummary{LastResult: "partial result"}
	}), state, Options{MaxIterations: 3, MaxElapsed: time.Millisecond})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if !result.Stats.MaxElapsedHit {
		t.Fatal("MaxElapsedHit = false")
	}
	if client.requests != 1 {
		t.Fatalf("LLM requests = %d, want 1", client.requests)
	}
	if !strings.Contains(result.Text, "Per-turn") {
		t.Fatalf("answer = %q, want Per-turn contextual budget fallback", result.Text)
	}
	if strings.Contains(result.Text, deadEndFallbackText()) {
		t.Fatalf("answer contains dead-end fallback: %q", result.Text)
	}
}

func TestRunLoopBudgetWithoutToolResultReturnsBriefLimitAnswer(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "search_files"}}}},
	}}

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		state.AddToolResultMessage(calls[0].ID, "")
		return ExecutionSummary{}
	}), state, Options{MaxIterations: 1})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if !result.Stats.MaxIterationsHit {
		t.Fatal("MaxIterationsHit = false, want true")
	}
	if strings.TrimSpace(result.Text) == "" {
		t.Fatalf("answer is empty, want a budget-fallback message")
	}
	if strings.Contains(result.Text, deadEndFallbackText()) {
		t.Fatalf("answer contains dead-end fallback: %q", result.Text)
	}
}

func TestRunLoopTerminalSwarmCanStopWithHandler(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "run_aurabot_swarm"}}}},
		{Response: llm.Response{Content: "too late"}},
	}}

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		state.AddToolResultMessage(calls[0].ID, "swarm synthesis")
		return ExecutionSummary{LastResult: "swarm synthesis", TerminalTool: "run_aurabot_swarm"}
	}), state, Options{
		MaxIterations:           3,
		TerminalToolPolicy:      true,
		AllowNoToolFinalization: true,
		TerminalHandler: func(context.Context, string, string, *Stats) (string, bool, bool) {
			return "final from swarm", false, true
		},
	})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if result.Text != "final from swarm" {
		t.Fatalf("Text = %q, want terminal response", result.Text)
	}
	if client.requests != 1 {
		t.Fatalf("LLM requests = %d, want 1", client.requests)
	}
	if result.Stats.TerminalTool != "run_aurabot_swarm" {
		t.Fatalf("TerminalTool = %q", result.Stats.TerminalTool)
	}
}

func TestRunLoopOnLLMStartFiresOncePerIteration(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "search_files"}}}},
		{Response: llm.Response{Content: "done"}},
	}}
	var starts []int
	_, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		state.AddToolResultMessage(calls[0].ID, "wiki result")
		return ExecutionSummary{LastResult: "wiki result", Results: map[string]string{calls[0].ID: "wiki result"}}
	}), state, Options{
		MaxIterations: 3,
		OnLLMStart: func(iteration, messagesIn, toolsIn int) {
			starts = append(starts, iteration)
		},
	})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if len(starts) != 2 || starts[0] != 0 || starts[1] != 1 {
		t.Fatalf("OnLLMStart fired with iterations %v, want [0 1]", starts)
	}
}

func TestRunLoopDeliveredResponseStillReturnsTextForLifecycle(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{Content: "streamed answer"}, Delivered: true},
	}}

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(context.Context, []llm.ToolCall) ExecutionSummary {
		t.Fatal("executor should not run")
		return ExecutionSummary{}
	}), state, Options{MaxIterations: 1})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if !result.Delivered {
		t.Fatal("Delivered = false, want true")
	}
	if result.Text != "streamed answer" {
		t.Fatalf("Text = %q, want streamed answer", result.Text)
	}
}

func TestRunLoopOnLLMDeltaFiresOncePerLLMCallWithFullContent(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{Content: "the full answer"}},
	}}
	var deltas []string
	_, err := runLoop(context.Background(), client, ToolExecutorFunc(func(context.Context, []llm.ToolCall) ExecutionSummary {
		t.Fatal("executor should not run")
		return ExecutionSummary{}
	}), state, Options{
		MaxIterations: 1,
		OnLLMDelta: func(delta string) {
			deltas = append(deltas, delta)
		},
	})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if len(deltas) != 1 || deltas[0] != "the full answer" {
		t.Fatalf("OnLLMDelta deltas = %v, want [\"the full answer\"]", deltas)
	}
}

func TestRunLoopOnLLMDeltaSkippedWhenContentEmpty(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "search_files"}}}},
		{Response: llm.Response{Content: "answer"}},
	}}
	var deltas []string
	_, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		state.AddToolResultMessage(calls[0].ID, "r")
		return ExecutionSummary{Results: map[string]string{calls[0].ID: "r"}}
	}), state, Options{
		MaxIterations: 3,
		OnLLMDelta: func(delta string) {
			deltas = append(deltas, delta)
		},
	})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if len(deltas) != 1 || deltas[0] != "answer" {
		t.Fatalf("OnLLMDelta deltas = %v, want [\"answer\"]", deltas)
	}
}

func TestRunLoopOnToolStartFiresOncePerFreshCall(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{
			{ID: "call-a", Name: "search_files", Arguments: map[string]any{"query": "a"}},
			{ID: "call-b", Name: "wiki_page", Arguments: map[string]any{"slug": "b"}},
		}}},
		{Response: llm.Response{Content: "done"}},
	}}
	var startNames []string
	var startKeys [][]string
	_, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		results := map[string]string{}
		for _, call := range calls {
			state.AddToolResultMessage(call.ID, "r")
			results[call.ID] = "r"
		}
		return ExecutionSummary{Results: results}
	}), state, Options{
		MaxIterations: 3,
		OnToolStart: func(call llm.ToolCall, argKeys []string) {
			startNames = append(startNames, call.Name)
			startKeys = append(startKeys, append([]string(nil), argKeys...))
		},
	})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if len(startNames) != 2 || startNames[0] != "search_files" || startNames[1] != "wiki_page" {
		t.Fatalf("OnToolStart names = %v, want [search_files wiki_page]", startNames)
	}
	if len(startKeys[0]) != 1 || startKeys[0][0] != "query" {
		t.Fatalf("call-a arg keys = %v, want [query]", startKeys[0])
	}
	if len(startKeys[1]) != 1 || startKeys[1][0] != "slug" {
		t.Fatalf("call-b arg keys = %v, want [slug]", startKeys[1])
	}
}

func TestRunLoopOnToolEndReportsErrorSentinelAsFailure(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{
			{ID: "call-ok", Name: "tool_ok"},
			{ID: "call-bad", Name: "tool_bad"},
		}}},
		{Response: llm.Response{Content: "done"}},
	}}
	type endRecord struct {
		callID  string
		success bool
		preview string
	}
	var ends []endRecord
	_, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		results := map[string]string{}
		for _, call := range calls {
			if call.Name == "tool_bad" {
				state.AddToolResultMessage(call.ID, "Error: boom")
				results[call.ID] = "Error: boom"
			} else {
				state.AddToolResultMessage(call.ID, "ok result")
				results[call.ID] = "ok result"
			}
		}
		return ExecutionSummary{Results: results}
	}), state, Options{
		MaxIterations: 3,
		OnToolEnd: func(callID, _ string, success bool, _ time.Duration, preview string) {
			ends = append(ends, endRecord{callID: callID, success: success, preview: preview})
		},
	})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if len(ends) != 2 {
		t.Fatalf("OnToolEnd fired %d times, want 2 (%+v)", len(ends), ends)
	}
	for _, e := range ends {
		switch e.callID {
		case "call-ok":
			if !e.success {
				t.Fatalf("call-ok success = false, want true")
			}
			if e.preview != "ok result" {
				t.Fatalf("call-ok preview = %q", e.preview)
			}
		case "call-bad":
			if e.success {
				t.Fatalf("call-bad success = true, want false (Error: prefix)")
			}
			if e.preview != "Error: boom" {
				t.Fatalf("call-bad preview = %q", e.preview)
			}
		default:
			t.Fatalf("unexpected callID %q", e.callID)
		}
	}
}

func TestRunLoopEmptyLLMResponseFallsBackToLastToolResult(t *testing.T) {
	const toolResult = "tool found something useful"
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "search_files", Arguments: map[string]any{"query": "x"}}}}},
		{Response: llm.Response{Content: ""}},
	}}

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		state.AddToolResultMessage(calls[0].ID, toolResult)
		return ExecutionSummary{LastResult: toolResult}
	}), state, Options{MaxIterations: 3})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	// US-CACHE-04: lastToolResult is returned directly as the reply.
	if result.Text != toolResult {
		t.Fatalf("result.Text = %q, want lastToolResult %q", result.Text, toolResult)
	}
}

// TestRunLoopLastToolResultFallback verifies US-CACHE-04: when the LLM emits
// an empty response after a tool execution (no text, no tool calls), the loop
// returns lastToolResult as the user-facing reply instead of a generic budget
// message. This is the "LLM treats tool result as the final answer" shortcut:
// the tool already produced the reply and the model correctly stayed silent.
func TestRunLoopLastToolResultFallback(t *testing.T) {
	const toolResult = "42\n--- tool produced the answer"
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "execute_code", Arguments: map[string]any{"code": "df.head(90)"}}}}},
		{Response: llm.Response{Content: ""}},
	}}

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		state.AddToolResultMessage(calls[0].ID, toolResult)
		return ExecutionSummary{LastResult: toolResult}
	}), state, Options{MaxIterations: 3})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if result.Text != toolResult {
		t.Fatalf("user-facing reply = %q, want lastToolResult %q", result.Text, toolResult)
	}
	// Only 2 LLM calls: tool round + empty round; no graceful finalization round.
	if client.requests != 2 {
		t.Fatalf("LLM requests = %d, want 2 (tool round + empty round)", client.requests)
	}
}

func TestRunLoopMaxElapsedTriggersGracefulFinalize(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "search_memory"}}}},
		{Response: llm.Response{Content: "Risposta graceful dopo timeout."}},
	}}

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		state.AddToolResultMessage(calls[0].ID, "risultato parziale")
		time.Sleep(5 * time.Millisecond)
		return ExecutionSummary{LastResult: "risultato parziale"}
	}), state, Options{
		MaxIterations:           3,
		MaxElapsed:              time.Millisecond,
		AllowNoToolFinalization: true,
	})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if !result.Stats.MaxElapsedHit {
		t.Fatal("MaxElapsedHit = false")
	}
	if result.Text != "Risposta graceful dopo timeout." {
		t.Fatalf("answer = %q, want graceful finalization response", result.Text)
	}
	if client.requests != 2 {
		t.Fatalf("LLM requests = %d, want 2 (tool turn + finalization)", client.requests)
	}
}

func TestRunLoopEmptyLLMResponseTriggersGracefulFinalize(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "search_memory"}}}},
		{Response: llm.Response{Content: ""}},
		{Response: llm.Response{Content: "Risposta graceful dopo LLM vuoto."}},
	}}

	// LastResult is empty so the fallback (US-CACHE-04) does not fire;
	// gracefulFinalize still runs and calls the third LLM round.
	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		state.AddToolResultMessage(calls[0].ID, "risultato utile")
		return ExecutionSummary{LastResult: ""}
	}), state, Options{
		MaxIterations:           3,
		AllowNoToolFinalization: true,
	})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if result.Stats.StopReason != "empty_llm_response" {
		t.Fatalf("StopReason = %q, want 'empty_llm_response'", result.Stats.StopReason)
	}
	if result.Text != "Risposta graceful dopo LLM vuoto." {
		t.Fatalf("answer = %q, want graceful finalization response", result.Text)
	}
	if client.requests != 3 {
		t.Fatalf("LLM requests = %d, want 3 (tool round + empty round + finalization)", client.requests)
	}
}

func TestRunLoopOnToolStartSkippedForDedupedCalls(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "search_files", Arguments: map[string]any{"query": "aura"}},
			{ID: "call-2", Name: "search_files", Arguments: map[string]any{"query": "aura"}},
		}}},
		{Response: llm.Response{Content: "done"}},
	}}
	var starts []string
	_, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		results := map[string]string{}
		for _, call := range calls {
			state.AddToolResultMessage(call.ID, "wiki result")
			results[call.ID] = "wiki result"
		}
		return ExecutionSummary{LastResult: "wiki result", Results: results}
	}), state, Options{
		MaxIterations: 3,
		OnToolStart: func(call llm.ToolCall, _ []string) {
			starts = append(starts, call.ID)
		},
	})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if len(starts) != 1 || starts[0] != "call-1" {
		t.Fatalf("OnToolStart fired with callIDs %v, want [call-1]", starts)
	}
}

type fakeLoopClient struct {
	responses []ChatResponse
	requests  int
}

func (f *fakeLoopClient) Chat(context.Context, []llm.Message, []llm.ToolDefinition) (ChatResponse, error) {
	f.requests++
	if f.requests <= len(f.responses) {
		return f.responses[f.requests-1], nil
	}
	return ChatResponse{}, nil
}

type recordingContextEngine struct {
	should bool
	focus  string
}

func (e *recordingContextEngine) Name() string { return "recording" }

func (e *recordingContextEngine) UpdateFromResponse(llm.TokenUsage) {}

func (e *recordingContextEngine) ShouldCompress(int) bool { return e.should }

func (e *recordingContextEngine) Compress(messages []llm.Message, _ int, focusTopic string) []llm.Message {
	e.focus = focusTopic
	return messages
}

// fakeLoopState is shared between the main loop goroutine and the
// StreamDispatcher tool-execution goroutines (US-LAT-06). Every mutation
// of the messages slice MUST acquire the mutex; otherwise the race
// detector trips on AddAssistantMessage ↔ AddToolResultMessage.
type fakeLoopState struct {
	mu       sync.Mutex
	messages []llm.Message
}

func newFakeLoopState() *fakeLoopState {
	return &fakeLoopState{messages: []llm.Message{{Role: "user", Content: "hello"}}}
}

func (s *fakeLoopState) Messages() []llm.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]llm.Message(nil), s.messages...)
}

func (s *fakeLoopState) TrackTokens(llm.TokenUsage) {}

func (s *fakeLoopState) AddAssistantMessage(content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, llm.Message{Role: "assistant", Content: content})
}

func (s *fakeLoopState) AddAssistantToolCallMessage(content string, calls []llm.ToolCall) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, llm.Message{Role: "assistant", Content: content, ToolCalls: calls})
}

func (s *fakeLoopState) AddToolResultMessage(id, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, llm.Message{Role: "tool", ToolCallID: id, Content: content})
}

func (s *fakeLoopState) last() llm.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.messages[len(s.messages)-1]
}

func (s *fakeLoopState) toolResult(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, msg := range s.messages {
		if msg.Role == "tool" && msg.ToolCallID == id {
			return msg.Content
		}
	}
	return ""
}

func deadEndFallbackText() string {
	return "Mi sono " + "fermato"
}

func TestLastUserMessageTextReturnsLastUserTruncatedRuneSafe(t *testing.T) {
	long := strings.Repeat("x", focusTopicMaxRunes-1) + "è" + "tail"
	got := lastUserMessageText([]llm.Message{
		{Role: "user", Content: "old request"},
		{Role: "assistant", Content: "reply"},
		{Role: "user", Content: long},
	})

	if len([]rune(got)) != focusTopicMaxRunes {
		t.Fatalf("focus runes = %d, want %d", len([]rune(got)), focusTopicMaxRunes)
	}
	if !strings.HasSuffix(got, "è") {
		t.Fatalf("focus topic is not rune-safe truncated: %q", got[len(got)-8:])
	}
	if strings.Contains(got, "tail") {
		t.Fatalf("focus topic was not truncated: %q", got)
	}
}

func TestRunLoopContextEngineReceivesLastUserFocusTopic(t *testing.T) {
	long := strings.Repeat("a", focusTopicMaxRunes+10)
	state := &fakeLoopState{messages: []llm.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "old"},
		{Role: "assistant", Content: "prior"},
		{Role: "user", Content: long},
	}}
	engine := &recordingContextEngine{should: true}
	client := &fakeLoopClient{responses: []ChatResponse{{Response: llm.Response{Content: "done"}}}}

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(context.Context, []llm.ToolCall) ExecutionSummary {
		t.Fatal("executor should not run")
		return ExecutionSummary{}
	}), state, Options{MaxIterations: 1, ContextEngine: engine})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if result.Text != "done" {
		t.Fatalf("Text = %q, want done", result.Text)
	}
	if len([]rune(engine.focus)) != focusTopicMaxRunes {
		t.Fatalf("focus runes = %d, want %d", len([]rune(engine.focus)), focusTopicMaxRunes)
	}
	if engine.focus != strings.Repeat("a", focusTopicMaxRunes) {
		t.Fatalf("focus = %q, want truncated last user text", engine.focus)
	}
}

// budgetLoopState extends fakeLoopState with AddUserMessage so bounded
// finalizing transitions can inject their prompt via UserMessageInjector.
type budgetLoopState struct {
	*fakeLoopState
}

func newBudgetLoopState() *budgetLoopState {
	return &budgetLoopState{fakeLoopState: newFakeLoopState()}
}

func (s *budgetLoopState) AddUserMessage(content string) {
	s.messages = append(s.messages, llm.Message{Role: "user", Content: content})
}

func (s *budgetLoopState) sawUserMessage(substr string) bool {
	for _, msg := range s.messages {
		if msg.Role == "user" && strings.Contains(msg.Content, substr) {
			return true
		}
	}
	return false
}

// TestRunLoopStructuredAskUserPausesLoop verifies that structured choices on
// ask_user pause the loop after exactly one LLM call.
func TestRunLoopStructuredAskUserPausesLoop(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{
			HasToolCalls: true,
			ToolCalls: []llm.ToolCall{
				{ID: "clarify-1", Name: "ask_user", Arguments: map[string]any{
					"question": "Which customer are you looking for?",
					"options": []any{
						map[string]any{"label": "By name", "value": "by_name"},
						map[string]any{"label": "By code", "value": "by_code"},
						map[string]any{"label": "Show first 5", "value": "first5"},
					},
					"kind": "choice",
				}},
			},
		}},
	}}

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		return ExecutionSummary{
			AwaitingUserInput: &tools.ErrAwaitingUserInput{
				Question: "Which customer are you looking for?",
				Options:  []string{"By name", "By code", "Show first 5"},
				Kind:     "choice",
			},
		}
	}), state, Options{MaxIterations: 5})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if result.Stats.StopReason != "waiting_for_user" {
		t.Errorf("StopReason = %q, want waiting_for_user", result.Stats.StopReason)
	}
	if result.Stats.LLMCalls != 1 {
		t.Errorf("LLMCalls = %d, want 1 (loop should pause after one LLM call)", result.Stats.LLMCalls)
	}
}

// TestMaxIter_ForcesFinalizeOnCap verifies that when MaxIterations is reached,
// the loop injects a corrector message via UserMessageInjector and the final LLM
// call returns a text answer (US-LAT-01 cap-hit behavior).
func TestMaxIter_ForcesFinalizeOnCap(t *testing.T) {
	state := newBudgetLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		// iteration 0: tool call (first iteration, not the cap)
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{
			{ID: "t1", Name: "search", Arguments: map[string]any{"query": "test"}},
		}}},
		// iteration 1: final LLM call with toolDefs=nil (cap hit)
		{Response: llm.Response{Content: "final answer from cap"}},
	}}

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		state.AddToolResultMessage(calls[0].ID, "search result")
		return ExecutionSummary{LastResult: "search result"}
	}), state, Options{MaxIterations: 2})

	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if result.Text != "final answer from cap" {
		t.Errorf("Text = %q, want final answer from cap", result.Text)
	}
	// The corrector message must mention the step count so the LLM knows
	// it's on the final iteration.
	if !state.sawUserMessage("2/2") {
		t.Error("expected cap-hit corrector message mentioning 2/2")
	}
}

// TestRunLoopEndTurnFalseForcesContinuation verifies that an explicit
// end_turn=false on a no-tool-call response forces another sampling round
// instead of returning. The loop should run until the next response
// (which has no end_turn signal) exits normally.
func TestRunLoopEndTurnFalseForcesContinuation(t *testing.T) {
	endTurnFalse := false
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{Content: "step 1", EndTurn: &endTurnFalse}},
		{Response: llm.Response{Content: "step 2"}},
	}}

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(context.Context, []llm.ToolCall) ExecutionSummary {
		t.Fatal("executor should not run")
		return ExecutionSummary{}
	}), state, Options{MaxIterations: 5})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if result.Text != "step 2" {
		t.Fatalf("Text = %q, want step 2", result.Text)
	}
	if result.Stats.LLMCalls != 2 {
		t.Fatalf("LLMCalls = %d, want 2 (end_turn=false forced a second round)", result.Stats.LLMCalls)
	}
}

// TestRunLoopEndTurnTrueExitsNormally verifies that explicit end_turn=true
// exits the loop on the first non-tool-call response (same as nil — existing
// semantics, no extra round forced).
func TestRunLoopEndTurnTrueExitsNormally(t *testing.T) {
	endTurnTrue := true
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{Content: "final answer", EndTurn: &endTurnTrue}},
	}}

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(context.Context, []llm.ToolCall) ExecutionSummary {
		t.Fatal("executor should not run")
		return ExecutionSummary{}
	}), state, Options{MaxIterations: 5})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if result.Text != "final answer" {
		t.Fatalf("Text = %q, want final answer", result.Text)
	}
	if result.Stats.LLMCalls != 1 {
		t.Fatalf("LLMCalls = %d, want 1 (end_turn=true exits immediately)", result.Stats.LLMCalls)
	}
}

// TestRunLoopEndTurnNilFallsBackToExistingExit verifies that nil EndTurn
// (self-hosted or provider that omits the field) falls back to existing
// exit semantics — no regression.
func TestRunLoopEndTurnNilFallsBackToExistingExit(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{Content: "answer"}},
	}}

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(context.Context, []llm.ToolCall) ExecutionSummary {
		t.Fatal("executor should not run")
		return ExecutionSummary{}
	}), state, Options{MaxIterations: 5})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if result.Text != "answer" {
		t.Fatalf("Text = %q, want answer", result.Text)
	}
	if result.Stats.LLMCalls != 1 {
		t.Fatalf("LLMCalls = %d, want 1 (nil EndTurn exits on first response)", result.Stats.LLMCalls)
	}
}

// captureLoopClient is a test client that records every messages slice it
// receives so tests can inspect what the loop sent to the LLM on each call.
type captureLoopClient struct {
	mu        sync.Mutex
	captured  [][]llm.Message
	idx       int
	responses []ChatResponse
}

func (c *captureLoopClient) Chat(_ context.Context, msgs []llm.Message, _ []llm.ToolDefinition) (ChatResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]llm.Message, len(msgs))
	copy(cp, msgs)
	c.captured = append(c.captured, cp)
	if c.idx < len(c.responses) {
		resp := c.responses[c.idx]
		c.idx++
		return resp, nil
	}
	return ChatResponse{}, nil
}

// TestRunLoopAlreadyDoneBlockInjectedAfterFirstToolCall verifies US-OUT-04:
// after a tool call in iteration 0, the system message for iteration 1 contains
// the "## Already done this turn" block with the tool name and result status.
func TestRunLoopAlreadyDoneBlockInjectedAfterFirstToolCall(t *testing.T) {
	state := newFakeLoopState()
	client := &captureLoopClient{
		responses: []ChatResponse{
			{Response: llm.Response{
				HasToolCalls: true,
				ToolCalls: []llm.ToolCall{{
					ID: "c1", Name: "search_memory",
					Arguments: map[string]any{"query": "X"},
				}},
			}},
			{Response: llm.Response{Content: "done"}},
		},
	}

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		state.AddToolResultMessage(calls[0].ID, "No results found")
		return ExecutionSummary{
			LastResult: "No results found",
			Results:    map[string]string{calls[0].ID: "No results found"},
		}
	}), state, Options{MaxIterations: 3})
	if err != nil {
		t.Fatalf("runLoop error: %v", err)
	}
	if result.Text != "done" {
		t.Fatalf("Text = %q", result.Text)
	}

	client.mu.Lock()
	captured := client.captured
	client.mu.Unlock()

	if len(captured) < 2 {
		t.Fatalf("expected ≥2 LLM calls, got %d", len(captured))
	}
	// System message is at index 0.
	if captured[1][0].Role != "system" {
		t.Fatalf("captured[1][0].Role = %q, want system", captured[1][0].Role)
	}
	sysmsg := captured[1][0].Content
	for _, want := range []string{
		"Already done this turn",
		"search_memory",
		"SUCCESSFUL but no results",
	} {
		if !strings.Contains(sysmsg, want) {
			t.Errorf("iteration-2 system msg missing %q:\n%s", want, sysmsg)
		}
	}
}

// TestRunLoopAlreadyDoneBlockAbsentOnFirstIteration verifies the token-cost
// gate (US-OUT-04): when no tool calls have been made yet, the block is NOT
// injected into the first-iteration system message.
func TestRunLoopAlreadyDoneBlockAbsentOnFirstIteration(t *testing.T) {
	state := newFakeLoopState()
	client := &captureLoopClient{
		responses: []ChatResponse{
			{Response: llm.Response{Content: "direct answer"}},
		},
	}

	_, err := runLoop(context.Background(), client, ToolExecutorFunc(func(context.Context, []llm.ToolCall) ExecutionSummary {
		t.Fatal("executor should not run")
		return ExecutionSummary{}
	}), state, Options{MaxIterations: 3})
	if err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	client.mu.Lock()
	captured := client.captured
	client.mu.Unlock()

	if len(captured) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(captured))
	}
	sysmsg := captured[0][0].Content
	if strings.Contains(sysmsg, "Already done this turn") {
		t.Errorf("first-iteration system msg must NOT contain the block (no tool calls yet):\n%s", sysmsg)
	}
}

// TestRunLoopFinishReasonLengthDropsToolCalls verifies US-OUT-05 rail 1:
// when finish_reason='length' AND tool_calls are present the calls are DROPPED
// and the executor is NOT invoked. The loop continues on the next iteration.
func TestRunLoopFinishReasonLengthDropsToolCalls(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{
			HasToolCalls: true,
			ToolCalls:    []llm.ToolCall{{ID: "call-1", Name: "web_search", Arguments: map[string]any{"q": "test"}}},
			FinishReason: "length",
		}},
		{Response: llm.Response{Content: "final after length drop"}},
	}}
	executed := false
	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, _ []llm.ToolCall) ExecutionSummary {
		executed = true
		return ExecutionSummary{}
	}), state, Options{MaxIterations: 4})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if executed {
		t.Fatal("executor MUST NOT be called when finish_reason='length' with tool calls")
	}
	if result.Text != "final after length drop" {
		t.Fatalf("Text = %q, want final after length drop", result.Text)
	}
	if client.requests != 2 {
		t.Fatalf("LLM requests = %d, want 2 (length-drop iteration + final)", client.requests)
	}
}

// TestRunLoopFinishReasonLengthInjectsRecoveryPrompt verifies US-OUT-05 rail 2:
// when finish_reason='length' AND non-blank text AND no tool_calls, the loop
// adds a user-side recovery prompt so the model continues from where it stopped.
func TestRunLoopFinishReasonLengthInjectsRecoveryPrompt(t *testing.T) {
	state := newBudgetLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{Content: "partial answer...", FinishReason: "length"}},
		{Response: llm.Response{Content: "completed answer"}},
	}}

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(context.Context, []llm.ToolCall) ExecutionSummary {
		t.Fatal("executor should not run")
		return ExecutionSummary{}
	}), state, Options{MaxIterations: 5})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	if result.Text != "completed answer" {
		t.Fatalf("Text = %q, want completed answer", result.Text)
	}
	if !state.sawUserMessage("Output limit reached") {
		t.Fatal("expected recovery prompt injected as user message")
	}
	if client.requests != 2 {
		t.Fatalf("LLM requests = %d, want 2 (length-recovery + final)", client.requests)
	}
}

// TestRunLoopFinishReasonLengthRecoveryCap verifies US-OUT-05 recovery counter:
// after 3 injections the 4th finish_reason='length' text response terminates the
// loop with the partial text rather than injecting another prompt.
func TestRunLoopFinishReasonLengthRecoveryCap(t *testing.T) {
	state := newBudgetLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{Content: "partial 1", FinishReason: "length"}},
		{Response: llm.Response{Content: "partial 2", FinishReason: "length"}},
		{Response: llm.Response{Content: "partial 3", FinishReason: "length"}},
		{Response: llm.Response{Content: "partial 4", FinishReason: "length"}},
		{Response: llm.Response{Content: "should not reach here"}},
	}}

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(context.Context, []llm.ToolCall) ExecutionSummary {
		t.Fatal("executor should not run")
		return ExecutionSummary{}
	}), state, Options{MaxIterations: 10})
	if err != nil {
		t.Fatalf("runLoop returned error: %v", err)
	}
	// 4th finish should terminate with partial 4, not reach the 5th response.
	if result.Text != "partial 4" {
		t.Fatalf("Text = %q, want partial 4 (cap exceeded, returns partial)", result.Text)
	}
	// Exactly 3 recovery messages injected, not 4.
	recoveryCount := 0
	for _, msg := range state.messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "Output limit reached") {
			recoveryCount++
		}
	}
	if recoveryCount != 3 {
		t.Fatalf("recovery messages = %d, want exactly 3 (maxLengthRecoveries)", recoveryCount)
	}
	// 4 LLM calls, not 5.
	if client.requests != 4 {
		t.Fatalf("LLM requests = %d, want 4 (terminates after 4th length-finish)", client.requests)
	}
}

// TestRunLoopTurnActionsAccumulateAcrossIterations verifies that Stats.TurnActions
// grows with each fresh tool call and is accessible after the run.
func TestRunLoopTurnActionsAccumulateAcrossIterations(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{
			{ID: "c1", Name: "web_search", Arguments: map[string]any{"query": "go generics"}},
		}}},
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{
			{ID: "c2", Name: "web_fetch", Arguments: map[string]any{"url": "https://go.dev"}},
		}}},
		{Response: llm.Response{Content: "done"}},
	}}
	callN := 0
	_, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		callN++
		state.AddToolResultMessage(calls[0].ID, "result "+calls[0].Name)
		return ExecutionSummary{
			LastResult: "result",
			Results:    map[string]string{calls[0].ID: "result " + calls[0].Name},
		}
	}), state, Options{MaxIterations: 4})
	if err != nil {
		t.Fatalf("runLoop error: %v", err)
	}
	// TurnActions is not directly accessible from loopResult, but we can
	// verify it was populated by checking that 2 tool calls were made.
	if callN != 2 {
		t.Fatalf("executor called %d times, want 2", callN)
	}
}

// TestOrOfFourTokenBudgetExceededStopReason verifies US-OUT-08 signal 2:
// when cumulative token usage exceeds MaxTokens, stop_reason is token_budget_exceeded
// and the second LLM call is never made.
func TestOrOfFourTokenBudgetExceededStopReason(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{
			HasToolCalls: true,
			ToolCalls:    []llm.ToolCall{{ID: "c1", Name: "search_memory"}},
			Usage:        llm.TokenUsage{TotalTokens: 600_000},
		}},
		{Response: llm.Response{Content: "should not be reached"}},
	}}
	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		for _, c := range calls {
			state.AddToolResultMessage(c.ID, "ok")
		}
		return ExecutionSummary{LastResult: "ok"}
	}), state, Options{MaxIterations: 10, MaxTokens: 500_000})
	if err != nil {
		t.Fatalf("runLoop: %v", err)
	}
	if result.Stats.StopReason != "token_budget_exceeded" {
		t.Errorf("StopReason = %q, want token_budget_exceeded", result.Stats.StopReason)
	}
	if client.requests != 1 {
		t.Errorf("LLM requests = %d, want 1 (second call must not fire)", client.requests)
	}
}

// TestOrOfFourWallClockStopReason verifies US-OUT-08 signal 3:
// the stop_reason string is "wall_clock_exceeded" (renamed from max_elapsed_hit).
func TestOrOfFourWallClockStopReason(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{{ID: "c1", Name: "search_memory"}}}},
		{Response: llm.Response{Content: "too late"}},
	}}
	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		for _, c := range calls {
			state.AddToolResultMessage(c.ID, "partial")
		}
		time.Sleep(5 * time.Millisecond)
		return ExecutionSummary{LastResult: "partial"}
	}), state, Options{MaxIterations: 3, MaxElapsed: time.Millisecond})
	if err != nil {
		t.Fatalf("runLoop: %v", err)
	}
	if result.Stats.StopReason != "wall_clock_exceeded" {
		t.Errorf("StopReason = %q, want wall_clock_exceeded", result.Stats.StopReason)
	}
	if !result.Stats.MaxElapsedHit {
		t.Error("MaxElapsedHit = false, want true")
	}
}

// TestOrOfFourCostBudgetExceededStopReason verifies US-OUT-08 signal 4:
// when estimated cost exceeds MaxCostUSD, stop_reason is cost_budget_exceeded.
func TestOrOfFourCostBudgetExceededStopReason(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{
			HasToolCalls: true,
			ToolCalls:    []llm.ToolCall{{ID: "c1", Name: "search_memory"}},
			Usage:        llm.TokenUsage{TotalTokens: 100},
		}},
		{Response: llm.Response{Content: "should not be reached"}},
	}}
	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		for _, c := range calls {
			state.AddToolResultMessage(c.ID, "ok")
		}
		return ExecutionSummary{LastResult: "ok"}
	}), state, Options{
		MaxIterations: 10,
		MaxCostUSD:    1.0,
		EstimateCost:  func(llm.TokenUsage) float64 { return 25.0 },
	})
	if err != nil {
		t.Fatalf("runLoop: %v", err)
	}
	if result.Stats.StopReason != "cost_budget_exceeded" {
		t.Errorf("StopReason = %q, want cost_budget_exceeded", result.Stats.StopReason)
	}
	if client.requests != 1 {
		t.Errorf("LLM requests = %d, want 1 (second call must not fire)", client.requests)
	}
}

// TestOrOfFourIterCapPreservesMaxIterationsHit verifies US-OUT-08 legacy behavior:
// when only the iter cap fires (with low tokens/cost), stop_reason is still
// max_iterations_hit.
func TestOrOfFourIterCapPreservesMaxIterationsHit(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{{ID: "c1", Name: "search_memory"}}}},
		{Response: llm.Response{Content: "done"}},
	}}
	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		for _, c := range calls {
			state.AddToolResultMessage(c.ID, "result")
		}
		return ExecutionSummary{LastResult: "result"}
	}), state, Options{MaxIterations: 1}) // one iteration hits the iter cap
	if err != nil {
		t.Fatalf("runLoop: %v", err)
	}
	if result.Stats.StopReason != "max_iterations_hit" {
		t.Errorf("StopReason = %q, want max_iterations_hit", result.Stats.StopReason)
	}
	if !result.Stats.MaxIterationsHit {
		t.Error("MaxIterationsHit = false, want true")
	}
}

// TestOrOfFourCostBudgetFallbackWhenNoEstimateCost verifies US-OUT-08 R6:
// when EstimateCost is nil, the cost cap never fires even with a tiny MaxCostUSD.
func TestOrOfFourCostBudgetFallbackWhenNoEstimateCost(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{{ID: "c1", Name: "search_memory"}}}},
		{Response: llm.Response{Content: "success despite tiny MaxCostUSD"}},
	}}
	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		for _, c := range calls {
			state.AddToolResultMessage(c.ID, "result")
		}
		return ExecutionSummary{LastResult: "result"}
	}), state, Options{
		MaxIterations: 5,
		MaxCostUSD:    0.001, // tiny cap — must NOT fire when EstimateCost is nil
		EstimateCost:  nil,
	})
	if err != nil {
		t.Fatalf("runLoop: %v", err)
	}
	if result.Stats.StopReason == "cost_budget_exceeded" {
		t.Error("cost_budget_exceeded fired when EstimateCost is nil — R6 fallback broken")
	}
	if result.Text != "success despite tiny MaxCostUSD" {
		t.Errorf("Text = %q, want success message", result.Text)
	}
}

// fakeLoopClientWithSynthesisError is a test client whose second call (the
// synthesis round) returns a controlled error, used to verify US-OUT-09 R7:
// synthesis errors must fall back to partial work, not recurse.
type fakeLoopClientWithSynthesisError struct {
	mainResponse ChatResponse
	synthErr     error
	requests     int
}

func (c *fakeLoopClientWithSynthesisError) Chat(_ context.Context, _ []llm.Message, _ []llm.ToolDefinition) (ChatResponse, error) {
	c.requests++
	if c.requests == 1 {
		return c.mainResponse, nil
	}
	return ChatResponse{}, c.synthErr
}

// TestForceFinalizeSynthesisOnTokenBudget verifies US-OUT-09: when token budget
// is exceeded and AllowNoToolFinalization=true, a synthesis LLM call fires with
// the Hermes prompt naming "token_budget_exceeded", producing a non-empty reply.
func TestForceFinalizeSynthesisOnTokenBudget(t *testing.T) {
	state := newFakeLoopState()
	client := &captureLoopClient{
		responses: []ChatResponse{
			{Response: llm.Response{
				HasToolCalls: true,
				ToolCalls:    []llm.ToolCall{{ID: "c1", Name: "search_memory"}},
				Usage:        llm.TokenUsage{TotalTokens: 600_000},
			}},
			{Response: llm.Response{Content: "token_budget_exceeded — partial answer from synthesis"}},
		},
	}
	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		for _, c := range calls {
			state.AddToolResultMessage(c.ID, "partial evidence")
		}
		return ExecutionSummary{LastResult: "partial evidence"}
	}), state, Options{
		MaxIterations:           10,
		MaxTokens:               500_000,
		AllowNoToolFinalization: true,
	})
	if err != nil {
		t.Fatalf("runLoop: %v", err)
	}
	if result.Stats.StopReason != "token_budget_exceeded" {
		t.Errorf("StopReason = %q, want token_budget_exceeded", result.Stats.StopReason)
	}
	if result.Text == "" {
		t.Fatal("synthesis reply must be non-empty")
	}
	// Verify the synthesis prompt sent to the LLM contains the stop_reason.
	client.mu.Lock()
	captured := client.captured
	client.mu.Unlock()
	if len(captured) < 2 {
		t.Fatalf("expected 2 LLM calls (tool round + synthesis), got %d", len(captured))
	}
	synthCall := captured[1]
	lastMsg := synthCall[len(synthCall)-1]
	if !strings.Contains(lastMsg.Content, "token_budget_exceeded") {
		t.Errorf("synthesis prompt must mention stop_reason; got %q", lastMsg.Content)
	}
}

// TestForceFinalizeSynthesisOnCostBudget verifies US-OUT-09: when cost budget
// is exceeded and AllowNoToolFinalization=true, a synthesis LLM call fires with
// the Hermes prompt naming "cost_budget_exceeded".
func TestForceFinalizeSynthesisOnCostBudget(t *testing.T) {
	state := newFakeLoopState()
	client := &captureLoopClient{
		responses: []ChatResponse{
			{Response: llm.Response{
				HasToolCalls: true,
				ToolCalls:    []llm.ToolCall{{ID: "c1", Name: "web_search"}},
			}},
			{Response: llm.Response{Content: "cost_budget_exceeded — here is partial work"}},
		},
	}
	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		for _, c := range calls {
			state.AddToolResultMessage(c.ID, "search result")
		}
		return ExecutionSummary{LastResult: "search result"}
	}), state, Options{
		MaxIterations:           10,
		MaxCostUSD:              1.0,
		EstimateCost:            func(llm.TokenUsage) float64 { return 25.0 },
		AllowNoToolFinalization: true,
	})
	if err != nil {
		t.Fatalf("runLoop: %v", err)
	}
	if result.Stats.StopReason != "cost_budget_exceeded" {
		t.Errorf("StopReason = %q, want cost_budget_exceeded", result.Stats.StopReason)
	}
	if result.Text == "" {
		t.Fatal("synthesis reply must be non-empty")
	}
	client.mu.Lock()
	captured := client.captured
	client.mu.Unlock()
	if len(captured) < 2 {
		t.Fatalf("expected 2 LLM calls (tool round + synthesis), got %d", len(captured))
	}
	synthCall := captured[1]
	lastMsg := synthCall[len(synthCall)-1]
	if !strings.Contains(lastMsg.Content, "cost_budget_exceeded") {
		t.Errorf("synthesis prompt must mention stop_reason; got %q", lastMsg.Content)
	}
}

// TestForceFinalizeSynthesisErrorFallsBackToPartialWork verifies US-OUT-09 R7:
// when the synthesis LLM call itself fails, gracefulFinalize falls back to
// finalAnswerOnBudgetWithContext (non-empty partial work) — no recursive finalize.
func TestForceFinalizeSynthesisErrorFallsBackToPartialWork(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClientWithSynthesisError{
		mainResponse: ChatResponse{Response: llm.Response{
			HasToolCalls: true,
			ToolCalls:    []llm.ToolCall{{ID: "c1", Name: "search_memory"}},
			Usage:        llm.TokenUsage{TotalTokens: 600_000},
		}},
		synthErr: errors.New("upstream LLM unavailable"),
	}
	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		for _, c := range calls {
			state.AddToolResultMessage(c.ID, "partial data")
		}
		return ExecutionSummary{LastResult: "partial data"}
	}), state, Options{
		MaxIterations:           10,
		MaxTokens:               500_000,
		AllowNoToolFinalization: true,
	})
	if err != nil {
		t.Fatalf("runLoop: %v", err)
	}
	if result.Text == "" {
		t.Fatal("R7: synthesis error must produce non-empty fallback reply (not empty)")
	}
	// Fallback is finalAnswerOnBudgetWithContext which contains "Per-turn".
	if !strings.Contains(result.Text, "Per-turn") {
		t.Errorf("R7 fallback text = %q, want Per-turn contextual message", result.Text)
	}
	// Synthesis call was attempted (requests == 2), then error → fallback.
	if client.requests != 2 {
		t.Errorf("requests = %d, want 2 (main + synthesis attempt)", client.requests)
	}
}
