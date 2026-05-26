package agent

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aura/aura/internal/agent/tools/attempts"
	tools "github.com/aura/aura/internal/agent/tools/registry"
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

type contextCaptureRunner struct {
	names          []string
	userID         chan string
	conversation   chan string
	allowedTools   chan []string
	searchArgsSeen chan map[string]any
}

func newContextCaptureRunner(names ...string) *contextCaptureRunner {
	return &contextCaptureRunner{
		names:          names,
		userID:         make(chan string, 1),
		conversation:   make(chan string, 1),
		allowedTools:   make(chan []string, 1),
		searchArgsSeen: make(chan map[string]any, 1),
	}
}

func (r *contextCaptureRunner) Names() []string { return r.names }

func (r *contextCaptureRunner) Execute(ctx context.Context, _ string, args map[string]any) (string, error) {
	r.userID <- tools.UserIDFromContext(ctx)
	r.conversation <- tools.ConversationIDFromContext(ctx)
	r.allowedTools <- tools.AllowedToolNamesFromContext(ctx)
	r.searchArgsSeen <- args
	return "ok", nil
}

type blockingToolRunner struct{}

func (blockingToolRunner) Names() []string { return []string{"slow"} }

func (blockingToolRunner) Execute(ctx context.Context, _ string, _ map[string]any) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
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

// Retry-hint feature retired 2026-05-24; the dormant AURA_OP12B_RETRY_HINT
// suite was deleted after probe data showed zero observable benefit.

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

func TestExecuteToolCallsUsesConversationIDOption(t *testing.T) {
	runner := newContextCaptureRunner("search", "context_probe")
	convCtx := conversation.NewContext(conversation.Config{})
	calls := []llm.ToolCall{{ID: "call-1", Name: "search", Arguments: map[string]any{"query": "docs"}}}

	summary := ExecuteToolCalls(context.Background(), runner, convCtx, "alice", 42, calls, true, nil,
		WithConversationID("web-thread-7"))

	if summary.Results["call-1"] != "ok" {
		t.Fatalf("summary = %+v", summary)
	}
	if got := <-runner.userID; got != "alice" {
		t.Fatalf("userID = %q, want alice", got)
	}
	if got := <-runner.conversation; got != "web-thread-7" {
		t.Fatalf("conversationID = %q, want web-thread-7", got)
	}
	if got := <-runner.allowedTools; !containsString(got, "search") || !containsString(got, "context_probe") {
		t.Fatalf("allowed tools = %+v", got)
	}
	args := <-runner.searchArgsSeen
	if args["chat_id"] != float64(42) {
		t.Fatalf("chat_id = %#v, want float64(42)", args["chat_id"])
	}
}

func TestExecuteToolCallsAppliesToolTimeout(t *testing.T) {
	convCtx := conversation.NewContext(conversation.Config{})
	calls := []llm.ToolCall{{ID: "call-1", Name: "slow"}}

	summary := ExecuteToolCalls(context.Background(), blockingToolRunner{}, convCtx, "alice", 0, calls, true, nil,
		WithToolTimeout(10*time.Millisecond))

	if !strings.Contains(summary.Results["call-1"], "deadline exceeded") {
		t.Fatalf("summary result = %q, want deadline exceeded", summary.Results["call-1"])
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

func TestExecuteToolCallsAppliesFreshToolResultBudgetToHistory(t *testing.T) {
	raw := "HEAD-" + strings.Repeat("x", 40000) + "-TAIL"
	runner := &stubToolRunner{names: []string{"search"}, result: raw}
	convCtx := conversation.NewContext(conversation.Config{})
	calls := []llm.ToolCall{{
		ID:        "call-1",
		Name:      "search",
		Arguments: map[string]any{"action": "subgraph", "query": "aura"},
	}}

	summary := ExecuteToolCalls(context.Background(), runner, convCtx, "user1", 0, calls, true, nil)

	if summary.Results["call-1"] != raw {
		t.Fatalf("summary result should remain raw for caller-side handling")
	}
	msgs := convCtx.Messages()
	if len(msgs) != 1 || msgs[0].Role != "tool" {
		t.Fatalf("messages = %+v, want one tool result", msgs)
	}
	history := msgs[0].Content
	if !strings.Contains(history, "HEAD-") {
		t.Fatalf("history missing preserved head: %q", history[:min(len(history), 80)])
	}
	if strings.Contains(history, "-TAIL") {
		t.Fatalf("history kept truncated tail")
	}
	if !strings.Contains(history, "bytes truncated by tool_result_budget") {
		t.Fatalf("history missing budget trailer: %q", history)
	}
}

func TestExecuteToolCallsTokenJuicePreservesStructuredToolPayload(t *testing.T) {
	raw := `{"path":"AGENT.md","content":"LOAD_BEARING_SENTINEL-` + strings.Repeat("x", 2048) + `","bytes":2048}`
	runner := &stubToolRunner{names: []string{"file"}, result: raw}
	convCtx := conversation.NewContext(conversation.Config{})
	calls := []llm.ToolCall{{
		ID:        "call-1",
		Name:      "file",
		Arguments: map[string]any{"action": "read", "path": "AGENT.md"},
	}}

	summary := ExecuteToolCalls(context.Background(), runner, convCtx, "user1", 0, calls, true, nil,
		WithTokenJuice(true))

	if summary.Results["call-1"] != raw {
		t.Fatalf("summary result changed by TokenJuice")
	}
	msgs := convCtx.Messages()
	if len(msgs) != 1 || msgs[0].Role != "tool" {
		t.Fatalf("messages = %+v, want one tool result", msgs)
	}
	if !strings.Contains(msgs[0].Content, "LOAD_BEARING_SENTINEL-") {
		t.Fatalf("history lost structured payload content: %q", msgs[0].Content[:min(len(msgs[0].Content), 160)])
	}
	if !strings.Contains(msgs[0].Content, `"path":"AGENT.md"`) {
		t.Fatalf("history lost structured payload metadata: %q", msgs[0].Content[:min(len(msgs[0].Content), 160)])
	}
}
