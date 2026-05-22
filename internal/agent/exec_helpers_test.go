package agent

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aura/aura/internal/agent/tools/attempts"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/testutil"
)

// stubToolRunner implements ToolRunner for testing.
type stubToolRunner struct {
	names  []string
	result string
	err    error
	calls  atomic.Int32
}

func (s *stubToolRunner) Names() []string { return s.names }

func (s *stubToolRunner) Execute(_ context.Context, _ string, _ map[string]any) (string, error) {
	s.calls.Add(1)
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
	if got := runner.calls.Load(); got != 1 {
		t.Fatalf("runner.calls = %d, want 1", got)
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

func TestExecuteToolCallsErrorAddsRetryHintWhenEnabled(t *testing.T) {
	t.Setenv("AURA_OP12B_RETRY_HINT_ENABLED", "true")
	runner := &stubToolRunner{names: []string{"mcp_fake"}, err: errors.New("mcp server unavailable")}
	convCtx := conversation.NewContext(conversation.Config{})
	calls := []llm.ToolCall{{ID: "call-1", Name: "mcp_fake"}}

	summary := ExecuteToolCalls(context.Background(), runner, convCtx, "user1", 0, calls, true, nil)

	if !strings.Contains(summary.LastResult, "[Analyze the error above and try a different approach.]") {
		t.Fatalf("LastResult = %q, want retry hint", summary.LastResult)
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
	if got := runner.calls.Load(); got != 2 {
		t.Fatalf("runner.calls = %d, want 2", got)
	}
}

func TestExecuteToolCallsRecordsToolAttempt(t *testing.T) {
	db := testutil.OpenTestDB(t, migrations.Run)
	const runID = "run-helper-attempt-test"
	seedExecutorRun(t, db, runID)

	repo := attempts.NewSQLiteRepo(db)
	runner := &stubToolRunner{names: []string{"search"}, result: "found memory"}
	convCtx := conversation.NewContext(conversation.Config{})
	calls := []llm.ToolCall{{ID: "call-1", Name: "search", Arguments: map[string]any{"action": "search", "query": "docs"}}}

	summary := ExecuteToolCalls(context.Background(), runner, convCtx, "user1", 42, calls, true, nil,
		WithToolAttemptRecording(runID, repo))

	if summary.Results["call-1"] != "found memory" {
		t.Fatalf("summary = %+v", summary)
	}
	var outcome, argKeys string
	err := db.QueryRowContext(context.Background(),
		`SELECT outcome, arg_keys_json FROM tool_attempts WHERE run_id = ? AND tool_name = ?`,
		runID, "search").Scan(&outcome, &argKeys)
	if err != nil {
		t.Fatalf("tool_attempts row missing: %v", err)
	}
	if outcome != "ok" {
		t.Fatalf("outcome = %q, want ok", outcome)
	}
	for _, want := range []string{"chat_id", "query"} {
		if !strings.Contains(argKeys, want) {
			t.Fatalf("arg_keys_json = %q, missing %q", argKeys, want)
		}
	}
}
