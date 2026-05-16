package agent

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

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
	if result.Text != "partial result" {
		t.Fatalf("answer = %q, want last useful tool result", result.Text)
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
	if result.Text != "partial result" {
		t.Fatalf("answer = %q, want last useful tool result", result.Text)
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

type fakeLoopState struct {
	messages []llm.Message
}

func newFakeLoopState() *fakeLoopState {
	return &fakeLoopState{messages: []llm.Message{{Role: "user", Content: "hello"}}}
}

func (s *fakeLoopState) Messages() []llm.Message {
	return append([]llm.Message(nil), s.messages...)
}

func (s *fakeLoopState) TrackTokens(llm.TokenUsage) {}

func (s *fakeLoopState) AddAssistantMessage(content string) {
	s.messages = append(s.messages, llm.Message{Role: "assistant", Content: content})
}

func (s *fakeLoopState) AddAssistantToolCallMessage(content string, calls []llm.ToolCall) {
	s.messages = append(s.messages, llm.Message{Role: "assistant", Content: content, ToolCalls: calls})
}

func (s *fakeLoopState) AddToolResultMessage(id, content string) {
	s.messages = append(s.messages, llm.Message{Role: "tool", ToolCallID: id, Content: content})
}

func (s *fakeLoopState) last() llm.Message {
	return s.messages[len(s.messages)-1]
}

func (s *fakeLoopState) toolResult(id string) string {
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
