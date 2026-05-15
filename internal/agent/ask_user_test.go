package agent

import (
	"context"
	"testing"

	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/llm"
)

// --- AskUserTool unit tests ---

func TestAskUserToolDefinition(t *testing.T) {
	tool := &tools.AskUserTool{}
	if tool.Name() != "ask_user" {
		t.Fatalf("Name = %q, want ask_user", tool.Name())
	}
	if tool.Description() == "" {
		t.Fatal("Description is empty")
	}
	params := tool.Parameters()
	if params == nil {
		t.Fatal("Parameters is nil")
	}
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("parameters.properties missing or wrong type: %T", params["properties"])
	}
	for _, field := range []string{"question", "options", "kind"} {
		if _, ok := props[field]; !ok {
			t.Errorf("parameters.properties missing field %q", field)
		}
	}
	req, ok := params["required"].([]string)
	if !ok || len(req) != 1 || req[0] != "question" {
		t.Fatalf("required = %v, want [question]", params["required"])
	}
}

func TestAskUserToolExecuteReturnsSentinel(t *testing.T) {
	tool := &tools.AskUserTool{}
	_, err := tool.Execute(context.Background(), map[string]any{
		"question": "Which file should I update?",
		"options":  []any{"main.go", "config.go", "other"},
		"kind":     "clarification",
	})
	if err == nil {
		t.Fatal("Execute returned nil error, want ErrAwaitingUserInput")
	}
	var awaitErr *tools.ErrAwaitingUserInput
	if !isErrAwaitingUserInput(err, &awaitErr) {
		t.Fatalf("error is %T (%v), want *ErrAwaitingUserInput", err, err)
	}
	if awaitErr.Question != "Which file should I update?" {
		t.Errorf("Question = %q, want 'Which file should I update?'", awaitErr.Question)
	}
	if len(awaitErr.Options) != 3 {
		t.Errorf("Options = %v, want 3 items", awaitErr.Options)
	}
	if awaitErr.Kind != "clarification" {
		t.Errorf("Kind = %q, want clarification", awaitErr.Kind)
	}
}

func TestAskUserToolExecuteDefaultsKindToClarification(t *testing.T) {
	tool := &tools.AskUserTool{}
	_, err := tool.Execute(context.Background(), map[string]any{
		"question": "Should I proceed?",
	})
	var awaitErr *tools.ErrAwaitingUserInput
	if !isErrAwaitingUserInput(err, &awaitErr) {
		t.Fatalf("error is %T, want *ErrAwaitingUserInput", err)
	}
	if awaitErr.Kind != "clarification" {
		t.Errorf("Kind = %q, want clarification (default)", awaitErr.Kind)
	}
	if len(awaitErr.Options) != 0 {
		t.Errorf("Options = %v, want empty (no options supplied)", awaitErr.Options)
	}
}

func TestAskUserToolExecuteApprovalKind(t *testing.T) {
	tool := &tools.AskUserTool{}
	_, err := tool.Execute(context.Background(), map[string]any{
		"question": "Delete all logs from last week?",
		"kind":     "approval",
	})
	var awaitErr *tools.ErrAwaitingUserInput
	if !isErrAwaitingUserInput(err, &awaitErr) {
		t.Fatalf("error is %T, want *ErrAwaitingUserInput", err)
	}
	if awaitErr.Kind != "approval" {
		t.Errorf("Kind = %q, want approval", awaitErr.Kind)
	}
}

func TestAskUserToolExecuteEmptyQuestionReturnsError(t *testing.T) {
	tool := &tools.AskUserTool{}
	_, err := tool.Execute(context.Background(), map[string]any{"question": ""})
	if err == nil {
		t.Fatal("Execute with empty question should return error")
	}
	var awaitErr *tools.ErrAwaitingUserInput
	if isErrAwaitingUserInput(err, &awaitErr) {
		t.Fatal("empty question should NOT produce ErrAwaitingUserInput")
	}
}

// --- loop pause behavior tests ---

func TestRunLoopPausesOnAskUserSentinel(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{
			HasToolCalls: true,
			ToolCalls: []llm.ToolCall{
				{ID: "ask-1", Name: "ask_user", Arguments: map[string]any{
					"question": "Which format do you prefer?",
					"options":  []any{"JSON", "CSV"},
				}},
			},
		}},
	}}

	result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		return ExecutionSummary{
			AwaitingUserInput: &tools.ErrAwaitingUserInput{
				Question: "Which format do you prefer?",
				Options:  []string{"JSON", "CSV"},
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
	// loop must have stopped after 1 LLM call (not continued to more iterations)
	if result.Stats.LLMCalls != 1 {
		t.Errorf("LLMCalls = %d, want 1 (should not have made further LLM calls)", result.Stats.LLMCalls)
	}
}

func TestRunLoopAskUserExclusiveDiscardsOtherBatchedCalls(t *testing.T) {
	state := newFakeLoopState()
	var executedNames []string
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{
			HasToolCalls: true,
			ToolCalls: []llm.ToolCall{
				{ID: "search-1", Name: "search_memory", Arguments: map[string]any{"query": "docs"}},
				{ID: "ask-1", Name: "ask_user", Arguments: map[string]any{"question": "Confirm?"}},
				{ID: "search-2", Name: "web_search", Arguments: map[string]any{"q": "go"}},
			},
		}},
	}}

	_, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		for _, call := range calls {
			executedNames = append(executedNames, call.Name)
		}
		return ExecutionSummary{
			AwaitingUserInput: &tools.ErrAwaitingUserInput{Question: "Confirm?", Kind: "approval"},
		}
	}), state, Options{MaxIterations: 5})
	if err != nil {
		t.Fatalf("runLoop error: %v", err)
	}
	// Only ask_user should have been dispatched to the executor.
	if len(executedNames) != 1 || executedNames[0] != "ask_user" {
		t.Errorf("executor received %v, want [ask_user]", executedNames)
	}
}

func TestRunLoopAskUserExclusiveStateHasOnlyAskUserCall(t *testing.T) {
	state := newFakeLoopState()
	client := &fakeLoopClient{responses: []ChatResponse{
		{Response: llm.Response{
			HasToolCalls: true,
			ToolCalls: []llm.ToolCall{
				{ID: "other-1", Name: "wiki_page", Arguments: map[string]any{"action": "read"}},
				{ID: "ask-1", Name: "ask_user", Arguments: map[string]any{"question": "Which page?"}},
			},
		}},
	}}

	_, err := runLoop(context.Background(), client, ToolExecutorFunc(func(_ context.Context, _ []llm.ToolCall) ExecutionSummary {
		return ExecutionSummary{
			AwaitingUserInput: &tools.ErrAwaitingUserInput{Question: "Which page?", Kind: "clarification"},
		}
	}), state, Options{MaxIterations: 5})
	if err != nil {
		t.Fatalf("runLoop error: %v", err)
	}
	// The assistant message in state should contain ONLY the ask_user call.
	msgs := state.Messages()
	var assistantMsg *llm.Message
	for i := range msgs {
		if msgs[i].Role == "assistant" && len(msgs[i].ToolCalls) > 0 {
			assistantMsg = &msgs[i]
			break
		}
	}
	if assistantMsg == nil {
		t.Fatal("no assistant tool-call message in state")
	}
	if len(assistantMsg.ToolCalls) != 1 || assistantMsg.ToolCalls[0].Name != "ask_user" {
		t.Errorf("assistant ToolCalls = %+v, want only [ask_user]", assistantMsg.ToolCalls)
	}
	// No tool_result message should have been added (ask_user pause).
	for _, msg := range msgs {
		if msg.Role == "tool" {
			t.Errorf("unexpected tool_result message in state: %+v", msg)
		}
	}
}

// --- PendingAskUserCall tests ---

func TestPendingAskUserCallFindsPending(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: "do the thing"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{
			{ID: "ask-1", Name: "ask_user", Arguments: map[string]any{
				"question": "Which format?",
				"options":  []any{"JSON", "CSV"},
				"kind":     "clarification",
			}},
		}},
		// No tool_result for ask-1 yet — it is pending.
	}

	id, opts, kind, ok := PendingAskUserCall(msgs)
	if !ok {
		t.Fatal("PendingAskUserCall returned ok=false, want true")
	}
	if id != "ask-1" {
		t.Errorf("toolCallID = %q, want ask-1", id)
	}
	if len(opts) != 2 || opts[0] != "JSON" || opts[1] != "CSV" {
		t.Errorf("options = %v, want [JSON CSV]", opts)
	}
	if kind != "clarification" {
		t.Errorf("kind = %q, want clarification", kind)
	}
}

func TestPendingAskUserCallReturnsFalseWhenResolved(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: "do the thing"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{
			{ID: "ask-1", Name: "ask_user", Arguments: map[string]any{"question": "Which?"}},
		}},
		{Role: "tool", ToolCallID: "ask-1", Content: "JSON"},
	}

	_, _, _, ok := PendingAskUserCall(msgs)
	if ok {
		t.Fatal("PendingAskUserCall returned ok=true, want false (call is resolved)")
	}
}

func TestPendingAskUserCallReturnsFalseWhenAbsent(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	}

	_, _, _, ok := PendingAskUserCall(msgs)
	if ok {
		t.Fatal("PendingAskUserCall returned ok=true, want false (no ask_user call)")
	}
}

func TestPendingAskUserCallReturnsFalseOnEmptyHistory(t *testing.T) {
	_, _, _, ok := PendingAskUserCall(nil)
	if ok {
		t.Fatal("PendingAskUserCall returned ok=true on nil slice, want false")
	}
}

func TestPendingAskUserCallDefaultsKindToClarification(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{
			{ID: "ask-1", Name: "ask_user", Arguments: map[string]any{"question": "confirm?"}},
			// No "kind" arg — should default to clarification.
		}},
	}
	_, _, kind, ok := PendingAskUserCall(msgs)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if kind != "clarification" {
		t.Errorf("kind = %q, want clarification (default)", kind)
	}
}

// isErrAwaitingUserInput is a helper since errors.As needs a pointer-to-pointer.
func isErrAwaitingUserInput(err error, target **tools.ErrAwaitingUserInput) bool {
	if err == nil {
		return false
	}
	awErr, ok := err.(*tools.ErrAwaitingUserInput)
	if ok && target != nil {
		*target = awErr
	}
	return ok
}
