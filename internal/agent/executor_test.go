package agent

import (
	"context"
	"database/sql"
	"errors"
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
	exec := newAgentExecutor(reg, state, nil, []string{"web_fetch"}, "", runID, 0, 0, repo)

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
	exec := newAgentExecutor(reg, state, nil, []string{"lookup"}, "", runID, 0, 0, repo)

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
	exec := newAgentExecutor(reg, state, nil, []string{"mcp_test_echo"}, "", runID, 0, 0, repo)

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
	exec := newAgentExecutor(reg, state, nil, []string{"lookup"}, "", "run-nil-repo", 0, 0, nil)

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
	exec := newAgentExecutor(reg, state, nil, []string{"lookup"}, "", "run-repo-err", 0, 0, alwaysErrRepo{})

	calls := []llm.ToolCall{{ID: "c1", Name: "lookup", Arguments: map[string]any{}}}
	summary := exec.ExecuteToolCalls(authorizedExecCtx(), calls)
	if summary.Results["c1"] == "" {
		t.Fatal("expected non-empty result even when repo.Record fails")
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
