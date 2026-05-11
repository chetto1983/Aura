package agentloop

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/llm"
)

func TestRunNoToolCallsReturnsAssistantText(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{{Response: llm.Response{Content: "done"}}}}

	result, err := Run(context.Background(), client, ToolExecutorFunc(func(context.Context, []llm.ToolCall) ExecutionSummary {
		t.Fatal("executor should not run")
		return ExecutionSummary{}
	}), state, Options{MaxIterations: 3})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
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

func TestRunToolCallContinuesToFinalResponse(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "search_files", Arguments: map[string]any{"query": "aura"}}}}},
		{Response: llm.Response{Content: "found it"}},
	}}
	var executed []llm.ToolCall

	result, err := Run(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		executed = append(executed, calls...)
		state.AddToolResultMessage(calls[0].ID, "wiki result")
		return ExecutionSummary{LastResult: "wiki result"}
	}), state, Options{MaxIterations: 3})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
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

func TestRunDuplicateToolCallsExecuteOnceAndAppendRecoverableResult(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "search_files", Arguments: map[string]any{"query": "aura"}},
			{ID: "call-2", Name: "search_files", Arguments: map[string]any{"query": "aura"}},
		}}},
		{Response: llm.Response{Content: "ok"}},
	}}
	executions := 0

	result, err := Run(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		executions += len(calls)
		state.AddToolResultMessage(calls[0].ID, "wiki result")
		return ExecutionSummary{LastResult: "wiki result"}
	}), state, Options{MaxIterations: 3})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
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

func TestRunDuplicateToolCallsAcrossIterationsExecuteOnce(t *testing.T) {
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

	result, err := Run(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		executions += len(calls)
		state.AddToolResultMessage(calls[0].ID, "memory result")
		return ExecutionSummary{LastResult: "memory result"}
	}), state, Options{MaxIterations: 4})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
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

func TestRunMaxCallsPerToolSkipsRepeatedRetrieval(t *testing.T) {
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

	result, err := Run(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		executions += len(calls)
		state.AddToolResultMessage(calls[0].ID, "memory result")
		return ExecutionSummary{LastResult: "memory result"}
	}), state, Options{MaxIterations: 4, MaxCallsPerTool: map[string]int{"search_memory": 1}})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
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

func TestRunBeforeToolPolicySkipsRepeatedRetrieval(t *testing.T) {
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

	result, err := Run(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
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
		t.Fatalf("Run returned error: %v", err)
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

func TestRunMaxIterationReturnsLastUsefulResult(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "search_files"}}}},
	}}

	result, err := Run(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		state.AddToolResultMessage(calls[0].ID, "partial result")
		return ExecutionSummary{LastResult: "partial result"}
	}), state, Options{MaxIterations: 1})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
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

func TestRunMaxIterationFinalizesMemoryEvidenceNaturally(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "search_memory"}}}},
		{Response: llm.Response{Content: "Ho trovato tre riferimenti utili sullo scenario PMS; il piu rilevante e il PDF con la richiesta d'offerta e scadenza."}},
	}}
	raw := `Memory evidence for "scenario test gestione richieste offerta pms" (3 result(s)):
- [source] src_1 - file.pdf - score=0.85
Evidence envelope:
{"query":"scenario test","items":[{"kind":"source","id":"src_1","score":0.85}]}`

	result, err := Run(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		state.AddToolResultMessage(calls[0].ID, raw)
		return ExecutionSummary{LastResult: raw}
	}), state, Options{MaxIterations: 1, AllowNoToolFinalization: true})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
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

// Removed: TestRunEmptyFinalDoesNotReturnRawShellResult,
// TestRunSanitizesRawModelFinalAnswer,
// TestRunMaxIterationDoesNotFallbackToRawMemoryEvidence.
//
// These asserted the legacy looksLikeRawToolEvidence scrubber, which
// regex-detected "exit_code:", "Evidence envelope:", "Memory evidence for"
// etc. in either the model's reply or the budget fallback, then replaced
// them with a canned Italian message. The fix moved upstream: tools render
// human-readable text, the system prompt instructs the model not to echo
// raw output. The loop no longer post-processes assistant content.

func TestRunMaxElapsedReturnsLastUsefulResultBeforeNextLLM(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "search_files"}}}},
		{Response: llm.Response{Content: "too late"}},
	}}

	result, err := Run(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		state.AddToolResultMessage(calls[0].ID, "partial result")
		time.Sleep(5 * time.Millisecond)
		return ExecutionSummary{LastResult: "partial result"}
	}), state, Options{MaxIterations: 3, MaxElapsed: time.Millisecond})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
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

func TestRunBudgetWithoutToolResultReturnsBriefLimitAnswer(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "search_files"}}}},
	}}

	result, err := Run(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		state.AddToolResultMessage(calls[0].ID, "")
		return ExecutionSummary{}
	}), state, Options{MaxIterations: 1})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
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

func TestRunTerminalSwarmCanStopWithHandler(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "run_aurabot_swarm"}}}},
		{Response: llm.Response{Content: "too late"}},
	}}

	result, err := Run(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
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
		t.Fatalf("Run returned error: %v", err)
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

// Tests for legacy defensive primitives (SpiralBreaker, TieredBudget,
// MaxRetryNudgesPerTurn, HiddenRejected) were removed when those primitives
// were deleted from Options/Stats. The loop now treats tool failures as
// regular tool messages and trusts the model to recover. See loop.go header
// for the rationale.

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
