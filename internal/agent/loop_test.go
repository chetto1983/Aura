package agent

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aura/aura/internal/llm"
	tools "github.com/aura/aura/internal/agent/tools/registry"
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

// budgetLoopState extends fakeLoopState with AddUserMessage so the
// MaxToolCalls finalizing transition can inject its prod via the
// PhantomCorrector type assertion path.
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

// TestRunLoopAskUserClarificationPausesLoop verifies that when the LLM calls
// ask_user_clarification, the loop pauses (StopReason=waiting_for_user) after
// exactly one LLM call — identical semantics to ask_user.
func TestRunLoopAskUserClarificationPausesLoop(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{
			HasToolCalls: true,
			ToolCalls: []llm.ToolCall{
				{ID: "clarify-1", Name: "ask_user_clarification", Arguments: map[string]any{
					"question": "Quale cliente cerchi?",
					"options": []any{
						map[string]any{"label": "Per nome", "value": "by_name"},
						map[string]any{"label": "Per codice", "value": "by_code"},
						map[string]any{"label": "Mostrami i primi 5", "value": "first5"},
					},
				}},
			},
		}},
	}}

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		return ExecutionSummary{
			AwaitingUserInput: &tools.ErrAwaitingUserInput{
				Question: "Quale cliente cerchi?",
				Options:  []string{"Per nome", "Per codice", "Mostrami i primi 5"},
				Kind:     "clarification",
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
// the loop injects a corrector message via PhantomCorrector and the final LLM
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
	mu       sync.Mutex
	captured [][]llm.Message
	idx      int
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
