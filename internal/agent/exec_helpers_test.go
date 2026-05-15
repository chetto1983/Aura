package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
)

// stubToolRunner implements ToolRunner for testing.
type stubToolRunner struct {
	names  []string
	result string
	err    error
	calls  int
}

func (s *stubToolRunner) Names() []string { return s.names }

func (s *stubToolRunner) Execute(_ context.Context, _ string, _ map[string]any) (string, error) {
	s.calls++
	return s.result, s.err
}

func TestExecuteToolCallsSuccessPath(t *testing.T) {
	runner := &stubToolRunner{names: []string{"search_memory"}, result: "found memory"}
	convCtx := conversation.NewContext(conversation.Config{})
	calls := []llm.ToolCall{{ID: "call-1", Name: "search_memory"}}

	summary := ExecuteToolCalls(context.Background(), runner, convCtx, "user1", 0, calls, true, nil)

	if summary.LastResult != "found memory" {
		t.Fatalf("LastResult = %q, want found memory", summary.LastResult)
	}
	if summary.Results["call-1"] != "found memory" {
		t.Fatalf("Results[call-1] = %q, want found memory", summary.Results["call-1"])
	}
	if runner.calls != 1 {
		t.Fatalf("runner.calls = %d, want 1", runner.calls)
	}
}

func TestExecuteToolCallsErrorPropagation(t *testing.T) {
	runner := &stubToolRunner{names: []string{"search_memory"}, err: errors.New("backend unavailable")}
	convCtx := conversation.NewContext(conversation.Config{})
	calls := []llm.ToolCall{{ID: "call-1", Name: "search_memory"}}

	summary := ExecuteToolCalls(context.Background(), runner, convCtx, "user1", 0, calls, true, nil)

	if summary.LastResult == "" {
		t.Fatal("LastResult is empty after error, want error message")
	}
	if !strings.HasPrefix(summary.LastResult, "Error: ") {
		t.Fatalf("LastResult = %q, want FormatToolError-wrapped (Error: prefix)", summary.LastResult)
	}
}

func TestExecuteToolCallsSummaryAggregation(t *testing.T) {
	runner := &stubToolRunner{names: []string{"tool_a", "tool_b"}, result: "ok"}
	convCtx := conversation.NewContext(conversation.Config{})
	calls := []llm.ToolCall{
		{ID: "call-1", Name: "tool_a"},
		{ID: "call-2", Name: "tool_b"},
	}

	summary := ExecuteToolCalls(context.Background(), runner, convCtx, "user1", 0, calls, true, nil)

	if len(summary.Results) != 2 {
		t.Fatalf("Results len = %d, want 2", len(summary.Results))
	}
	if summary.Results["call-1"] != "ok" {
		t.Fatalf("Results[call-1] = %q, want ok", summary.Results["call-1"])
	}
	if summary.Results["call-2"] != "ok" {
		t.Fatalf("Results[call-2] = %q, want ok", summary.Results["call-2"])
	}
	msgs := convCtx.Messages()
	if len(msgs) != 2 {
		t.Fatalf("convCtx messages len = %d, want 2", len(msgs))
	}
	if runner.calls != 2 {
		t.Fatalf("runner.calls = %d, want 2", runner.calls)
	}
}
