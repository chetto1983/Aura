package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/agent/tools/attempts"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/identity"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/testutil"
)

// seedExecutorRun inserts a minimal runs row to satisfy the FK on tool_attempts.run_id.
func seedExecutorRun(t *testing.T, db *sql.DB, runID string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO runs (id, channel, status, started_at, updated_at) VALUES (?, 'test', 'running', ?, ?)`,
		runID,
		time.Now().UTC().Format(time.RFC3339Nano),
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		t.Fatalf("seedExecutorRun: %v", err)
	}
}

// authorizedExecCtx injects an actor + authorizer so identity checks inside tools pass.
func authorizedExecCtx() context.Context {
	ctx := identity.WithAuthorizer(context.Background(), allowRunTaskAuthorizer{})
	return identity.WithActorID(ctx, "actor:executor-test")
}

// TestExecutorRecordsAttemptOnToolError verifies that executeOneTool writes a
// row to tool_attempts with the correct outcome and class when the tool returns
// an error, and that the tool result is still surfaced to the caller.
func TestExecutorRecordsAttemptOnToolError(t *testing.T) {
	db := testutil.OpenTestDB(t, migrations.Run)
	const runID = "run-exec-error-test"
	seedExecutorRun(t, db, runID)

	repo := attempts.NewSQLiteRepo(db)
	reg := tools.NewRegistry(nil)
	// Use an error that ClassifyToolError maps to "io" (message contains "i/o").
	reg.Register(&fakeTool{name: "web_fetch", err: errors.New("i/o error: connection refused")})

	state := newAgentState([]llm.Message{{Role: "user", Content: "test"}})
	exec := newAgentExecutor(reg, state, nil, []string{"web_fetch"}, "", runID, 0, 0, repo, false, "", nil, nil)

	calls := []llm.ToolCall{{ID: "call_err", Name: "web_fetch", Arguments: map[string]any{"url": "http://example.com"}}}
	summary := exec.ExecuteToolCalls(authorizedExecCtx(), calls)

	// The executor must still return a (error-formatted) result to the caller.
	if summary.Results["call_err"] == "" {
		t.Fatal("expected a non-empty tool result even on error")
	}

	// A row must exist in tool_attempts.
	var outcome, class, toolKind string
	err := db.QueryRowContext(context.Background(),
		`SELECT outcome, class, tool_kind FROM tool_attempts WHERE run_id = ?`, runID).
		Scan(&outcome, &class, &toolKind)
	if err != nil {
		t.Fatalf("no tool_attempts row after error: %v", err)
	}
	// "i/o error" → class="io" → outcome="recoverable".
	if outcome != "recoverable" {
		t.Errorf("outcome = %q, want 'recoverable'", outcome)
	}
	if class != "io" {
		t.Errorf("class = %q, want 'io'", class)
	}
	if toolKind != "native" {
		t.Errorf("tool_kind = %q, want 'native'", toolKind)
	}
}

// TestExecutorRecordsAttemptOnSuccess verifies that a successful tool call also
// produces a tool_attempts row with outcome=ok.
func TestExecutorRecordsAttemptOnSuccess(t *testing.T) {
	db := testutil.OpenTestDB(t, migrations.Run)
	const runID = "run-exec-success-test"
	seedExecutorRun(t, db, runID)

	repo := attempts.NewSQLiteRepo(db)
	reg := tools.NewRegistry(nil)
	reg.Register(&fakeTool{name: "lookup", result: "found it"})

	state := newAgentState([]llm.Message{{Role: "user", Content: "test"}})
	exec := newAgentExecutor(reg, state, nil, []string{"lookup"}, "", runID, 0, 0, repo, false, "", nil, nil)

	calls := []llm.ToolCall{{ID: "call_ok", Name: "lookup", Arguments: map[string]any{}}}
	exec.ExecuteToolCalls(authorizedExecCtx(), calls)

	var outcome string
	err := db.QueryRowContext(context.Background(),
		`SELECT outcome FROM tool_attempts WHERE run_id = ?`, runID).Scan(&outcome)
	if err != nil {
		t.Fatalf("no tool_attempts row after success: %v", err)
	}
	if outcome != "ok" {
		t.Errorf("outcome = %q, want 'ok'", outcome)
	}
}

// TestExecutorMCPToolKindPersisted verifies that a tool whose name starts with
// "mcp_" is stored with tool_kind='mcp' in the persisted row.
func TestExecutorMCPToolKindPersisted(t *testing.T) {
	db := testutil.OpenTestDB(t, migrations.Run)
	const runID = "run-exec-mcp-test"
	seedExecutorRun(t, db, runID)

	repo := attempts.NewSQLiteRepo(db)
	reg := tools.NewRegistry(nil)
	// mcp_test_echo exercises the mcp_ prefix detection path in ToolKindOf.
	reg.Register(&fakeTool{name: "mcp_test_echo", err: errors.New("server unavailable")})

	state := newAgentState([]llm.Message{{Role: "user", Content: "test"}})
	exec := newAgentExecutor(reg, state, nil, []string{"mcp_test_echo"}, "", runID, 0, 0, repo, false, "", nil, nil)

	calls := []llm.ToolCall{{ID: "call_mcp", Name: "mcp_test_echo", Arguments: map[string]any{}}}
	exec.ExecuteToolCalls(authorizedExecCtx(), calls)

	var toolKind string
	err := db.QueryRowContext(context.Background(),
		`SELECT tool_kind FROM tool_attempts WHERE run_id = ?`, runID).Scan(&toolKind)
	if err != nil {
		t.Fatalf("no tool_attempts row for mcp tool: %v", err)
	}
	if toolKind != "mcp" {
		t.Errorf("tool_kind = %q, want 'mcp'", toolKind)
	}
}

// TestExecutorNilRepoSkipsSilently confirms that a nil repo does not cause a
// panic and that the tool result is still returned to the caller.
func TestExecutorNilRepoSkipsSilently(t *testing.T) {
	reg := tools.NewRegistry(nil)
	reg.Register(&fakeTool{name: "lookup", result: "ok"})

	state := newAgentState([]llm.Message{{Role: "user", Content: "test"}})
	exec := newAgentExecutor(reg, state, nil, []string{"lookup"}, "", "run-nil-repo", 0, 0, nil, false, "", nil, nil)

	calls := []llm.ToolCall{{ID: "c1", Name: "lookup", Arguments: map[string]any{}}}
	summary := exec.ExecuteToolCalls(authorizedExecCtx(), calls)
	if summary.Results["c1"] == "" {
		t.Fatal("expected result even with nil repo")
	}
}

// TestExecutorRecordFailureDoesNotPropagateToResult checks the degradation
// guarantee: even if the repo.Record call returns an error, the tool result
// still reaches the caller without an error.
func TestExecutorRecordFailureDoesNotPropagateToResult(t *testing.T) {
	reg := tools.NewRegistry(nil)
	reg.Register(&fakeTool{name: "lookup", result: "found"})

	state := newAgentState([]llm.Message{{Role: "user", Content: "test"}})
	exec := newAgentExecutor(reg, state, nil, []string{"lookup"}, "", "run-repo-err", 0, 0, alwaysErrRepo{}, false, "", nil, nil)

	calls := []llm.ToolCall{{ID: "c1", Name: "lookup", Arguments: map[string]any{}}}
	summary := exec.ExecuteToolCalls(authorizedExecCtx(), calls)
	if summary.Results["c1"] == "" {
		t.Fatal("expected non-empty result even when repo.Record fails")
	}
}

// TestExecutorTokenJuicePreservesStructuredToolPayload verifies TokenJuice does
// not compact fresh structured evidence such as file reads. The runtime budget
// may later cap oversized results, but the terminal-log reducer must not drop
// the content before the next LLM round can inspect it.
func TestExecutorTokenJuicePreservesStructuredToolPayload(t *testing.T) {
	bigContent := "LOAD_BEARING_SENTINEL-" + strings.Repeat("x", 4800)
	bigOutput := `{"path":"workspace/TOOLS.md","type":"file","bytes":5000,"total_bytes":5000,"truncated":false,"encoding":"utf-8","content":"` + bigContent + `"}`

	reg := tools.NewRegistry(nil)
	reg.Register(&fakeTool{name: "file", result: bigOutput})
	call := llm.ToolCall{ID: "c1", Name: "file", Arguments: map[string]any{"action": "read", "path": "workspace/TOOLS.md"}}

	state := newAgentState([]llm.Message{{Role: "user", Content: "test"}})
	exec := newAgentExecutor(reg, state, nil, []string{"file"}, "", "run-tj-on", 0, 0, nil, true, "", nil, nil)
	exec.ExecuteToolCalls(authorizedExecCtx(), []llm.ToolCall{call})

	var toolMsg *llm.Message
	for i := range state.messages {
		if state.messages[i].Role == "tool" {
			toolMsg = &state.messages[i]
			break
		}
	}
	if toolMsg == nil {
		t.Fatal("tokenjuice=true: no tool result message in state")
	}
	if !strings.Contains(toolMsg.Content, `"content":"LOAD_BEARING_SENTINEL-`) {
		t.Fatalf("tokenjuice=true: file content was not preserved: %q", toolMsg.Content[:min(len(toolMsg.Content), 240)])
	}
	if !strings.Contains(toolMsg.Content, `"path":"workspace/TOOLS.md"`) {
		t.Fatalf("tokenjuice=true: file metadata was not preserved: %q", toolMsg.Content[:min(len(toolMsg.Content), 240)])
	}
}

func TestCompactToolOutputCompactsExecuteShellOutput(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&sb, "line %03d\n", i)
	}
	sb.WriteString("MARKER-END\n")
	raw := "exit_code: 0\nelapsed_ms: 7\n\n" + sb.String()

	got := CompactToolOutput(nil, "execute_shell", map[string]any{"command": "printf lines"}, raw)
	if len(got) >= len(raw) {
		t.Fatalf("shell output was not compacted: got %d bytes, raw %d", len(got), len(raw))
	}
	if !strings.Contains(got, "MARKER-END") {
		t.Fatalf("shell compaction lost tail marker: %q", got)
	}
}

// TestExecutorBudgetCapBlocksFourthWebCall verifies the probe from US-OUT-07:
// a turn that calls web_search 4 times (cap=3) has the 4th call blocked with
// a budget-exhausted error returned inline as the tool result.
// Distinct queries are used so the repeated-lookup guard (US-OUT-03) does not
// fire before the budget check.
func TestExecutorBudgetCapBlocksFourthWebCall(t *testing.T) {
	reg := tools.NewRegistry(nil)
	reg.Register(&fakeTool{name: "web_search", result: "some results"})

	state := newAgentState([]llm.Message{{Role: "user", Content: "search"}})
	// Default caps: web=3. Pass nil to use defaults.
	exec := newAgentExecutor(reg, state, nil, []string{"web_search"}, "", "run-budget-test", 0, 0, nil, false, "", nil, nil)

	// Calls 1–3 use distinct queries to avoid repeatedLookup guard; all must succeed.
	for i := range 3 {
		calls := []llm.ToolCall{{ID: fmt.Sprintf("c%d", i+1), Name: "web_search", Arguments: map[string]any{"query": fmt.Sprintf("query-%d", i+1)}}}
		summary := exec.ExecuteToolCalls(authorizedExecCtx(), calls)
		res := summary.Results[fmt.Sprintf("c%d", i+1)]
		if strings.Contains(res, "budget exhausted") {
			t.Fatalf("call %d should succeed, got blocked: %s", i+1, res)
		}
	}
	// 4th call with a new unique query — blocked by budget, not repeated-lookup.
	calls4 := []llm.ToolCall{{ID: "c4", Name: "web_search", Arguments: map[string]any{"query": "query-4"}}}
	summary := exec.ExecuteToolCalls(authorizedExecCtx(), calls4)
	res := summary.Results["c4"]
	if !strings.Contains(res, "budget exhausted") {
		t.Errorf("4th call should be blocked by budget, got: %s", res)
	}
	if !strings.Contains(res, "web") {
		t.Errorf("error should name the class, got: %s", res)
	}
}

// TestExecutorBudgetCounterResetsPerTurn verifies that two sequential turns
// (= two separate executors) each get a fresh budget: 3+3=6 web calls total,
// all succeed.
func TestExecutorBudgetCounterResetsPerTurn(t *testing.T) {
	reg := tools.NewRegistry(nil)
	reg.Register(&fakeTool{name: "web_search", result: "ok"})

	makeExec := func(id string) *agentExecutor {
		state := newAgentState([]llm.Message{{Role: "user", Content: "search"}})
		return newAgentExecutor(reg, state, nil, []string{"web_search"}, "", id, 0, 0, nil, false, "", nil, nil)
	}

	for turn := range 2 {
		exec := makeExec(fmt.Sprintf("run-budget-reset-turn%d", turn))
		for i := range 3 {
			// Use distinct queries per call to avoid the repeated-lookup guard.
			q := fmt.Sprintf("turn%d-query%d", turn, i)
			calls := []llm.ToolCall{{ID: fmt.Sprintf("c%d", i+1), Name: "web_search", Arguments: map[string]any{"query": q}}}
			summary := exec.ExecuteToolCalls(authorizedExecCtx(), calls)
			res := summary.Results[fmt.Sprintf("c%d", i+1)]
			if strings.Contains(res, "budget exhausted") {
				t.Errorf("turn%d call%d should succeed, got blocked: %s", turn+1, i+1, res)
			}
		}
	}
}

// TestExecutorNilPayloadSummarizerPassesThrough verifies the recursive-dispatch
// prevention wiring (US-CTX-03 R1): when payloadSummarizer is nil (as it must be
// for the summarizer sub-agent), large tool results pass through without any
// summarization call — preventing recursive dispatch.
func TestExecutorNilPayloadSummarizerPassesThrough(t *testing.T) {
	// A large result that would trigger a real summarizer.
	bigResult := strings.Repeat("x", 5000)
	reg := tools.NewRegistry(nil)
	reg.Register(&fakeTool{name: "search", result: bigResult})

	state := newAgentState([]llm.Message{{Role: "user", Content: "find"}})
	// payloadSummarizer = nil (last arg) — simulates summarizer sub-agent wiring.
	exec := newAgentExecutor(reg, state, nil, []string{"search"}, "", "run-nil-ps", 0, 0, nil, false, "", nil, nil)

	calls := []llm.ToolCall{{ID: "c1", Name: "search", Arguments: map[string]any{"query": "x"}}}
	summary := exec.ExecuteToolCalls(authorizedExecCtx(), calls)
	// Result should pass through; bigResult content preserved (after wrapping).
	if !strings.Contains(summary.Results["c1"], "x") {
		t.Fatal("expected large result to pass through unchanged with nil payloadSummarizer")
	}
}

func TestAgentExecutorAppliesFreshToolResultBudgetHeadOnly(t *testing.T) {
	raw := "HEAD-" + strings.Repeat("x", 2000) + "-TAIL"
	reg := tools.NewRegistry(nil)
	reg.Register(&fakeTool{name: "search", result: raw})

	state := newAgentState([]llm.Message{{Role: "user", Content: "inspect graph evidence"}})
	exec := newAgentExecutor(reg, state, nil, []string{"search"}, "", "run-budget-head", 512, 0, nil, false, "", nil, nil)

	summary := exec.ExecuteToolCalls(authorizedExecCtx(), []llm.ToolCall{
		{ID: "call-1", Name: "search", Arguments: map[string]any{"action": "subgraph", "query": "aura"}},
	})

	got := summary.Results["call-1"]
	if !strings.Contains(got, "HEAD-") {
		t.Fatalf("summary result missing preserved head: %q", got)
	}
	if strings.Contains(got, "-TAIL") {
		t.Fatalf("summary result kept truncated tail")
	}
	if !strings.Contains(got, "bytes truncated by tool_result_budget") {
		t.Fatalf("summary result missing budget trailer: %q", got)
	}

	msgs := state.Messages()
	if len(msgs) != 2 || msgs[1].Role != "tool" {
		t.Fatalf("messages = %+v, want one tool result after user", msgs)
	}
	if msgs[1].Content != got {
		t.Fatalf("history result diverged from summary result")
	}
}

// alwaysErrRepo satisfies attempts.Repo and always returns an error from every method.
type alwaysErrRepo struct{}

func (alwaysErrRepo) Record(_ context.Context, _ tools.ToolObservation) error {
	return errors.New("repo always fails")
}

func (alwaysErrRepo) Recent(_ context.Context, _, _ string, _ int) ([]attempts.ToolAttempt, error) {
	return nil, errors.New("repo always fails")
}

func (alwaysErrRepo) CountOutcome(_ context.Context, _, _ string, _ tools.Outcome) (int, error) {
	return 0, errors.New("repo always fails")
}

func (alwaysErrRepo) AggregateForPromotion(_ context.Context, _, _ int) ([]attempts.LessonCandidate, error) {
	return nil, errors.New("repo always fails")
}
