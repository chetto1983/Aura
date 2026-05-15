package attempts_test

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aura/aura/internal/agent/tools/attempts"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/testutil"
)

func openMigratedDB(t *testing.T) *sql.DB {
	t.Helper()
	return testutil.OpenTestDB(t, migrations.Run)
}

// seedRun inserts a minimal runs row so FK constraints on tool_attempts are satisfied.
func seedRun(t *testing.T, db *sql.DB, runID string) {
	t.Helper()
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `INSERT INTO runs (id, channel, status, started_at, updated_at)
		VALUES (?, 'telegram', 'running', ?, ?)`,
		runID,
		time.Now().UTC().Format(time.RFC3339Nano),
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		t.Fatalf("seedRun: %v", err)
	}
}

func makeObs(runID, toolName, class string, offset time.Duration) tools.ToolObservation {
	argsHash, argKeys := tools.ObservationArgsHash(map[string]any{"query": "test_value", "limit": 10})
	return tools.ToolObservation{
		RunID:         runID,
		ToolName:      toolName,
		ToolKind:      tools.ToolKindOf(toolName),
		AttemptN:      1,
		Outcome:       tools.BucketOf(class),
		Class:         class,
		ArgsHash:      argsHash,
		ArgKeys:       argKeys,
		ErrorRedacted: "",
		StartedAt:     time.Now().Add(offset).UTC(),
		ElapsedMS:     50,
	}
}

// --- Tests ---

func TestNilRepo(t *testing.T) {
	repo := attempts.NewSQLiteRepo(nil)
	ctx := context.Background()

	if err := repo.Record(ctx, tools.ToolObservation{}); err != attempts.ErrRepoUnavailable {
		t.Errorf("Record with nil db: got %v, want ErrRepoUnavailable", err)
	}
	if _, err := repo.Recent(ctx, "run", "tool", 5); err != attempts.ErrRepoUnavailable {
		t.Errorf("Recent with nil db: got %v, want ErrRepoUnavailable", err)
	}
}

func TestRecord_Roundtrip(t *testing.T) {
	db := openMigratedDB(t)
	runID := "run_roundtrip"
	seedRun(t, db, runID)
	repo := attempts.NewSQLiteRepo(db)
	ctx := context.Background()

	obs := makeObs(runID, "web_fetch", "io", 0)
	if err := repo.Record(ctx, obs); err != nil {
		t.Fatalf("Record: %v", err)
	}

	var outcome, class, argKeysJSON, argsHash, toolKind string
	err := db.QueryRowContext(ctx,
		`SELECT outcome, class, arg_keys_json, args_hash, tool_kind FROM tool_attempts WHERE run_id = ?`, runID).
		Scan(&outcome, &class, &argKeysJSON, &argsHash, &toolKind)
	if err != nil {
		t.Fatalf("query row: %v", err)
	}
	if outcome != "recoverable" {
		t.Errorf("outcome: got %q, want %q", outcome, "recoverable")
	}
	if class != "io" {
		t.Errorf("class: got %q, want %q", class, "io")
	}
	if toolKind != "native" {
		t.Errorf("tool_kind: got %q, want %q", toolKind, "native")
	}
	// Arg values must NOT appear — only key names.
	if strings.Contains(argKeysJSON, "test_value") {
		t.Errorf("arg_keys_json must not contain arg value 'test_value', got: %s", argKeysJSON)
	}
	if !strings.Contains(argKeysJSON, "query") || !strings.Contains(argKeysJSON, "limit") {
		t.Errorf("arg_keys_json should contain key names, got: %s", argKeysJSON)
	}
}

func TestRecord_NoSecretValueInRow(t *testing.T) {
	db := openMigratedDB(t)
	runID := "run_secret"
	seedRun(t, db, runID)
	repo := attempts.NewSQLiteRepo(db)
	ctx := context.Background()

	const secret = "SUPER_SECRET_TOKEN_THAT_MUST_NOT_APPEAR"
	argsHash, argKeys := tools.ObservationArgsHash(map[string]any{
		"api_key": secret,
		"url":     "https://example.com",
	})
	obs := tools.ToolObservation{
		RunID:         runID,
		ToolName:      "web_fetch",
		ToolKind:      "native",
		AttemptN:      1,
		Outcome:       tools.OutcomeRecoverable,
		Class:         "io",
		ArgsHash:      argsHash,
		ArgKeys:       argKeys,
		ErrorRedacted: "",
		StartedAt:     time.Now().UTC(),
		ElapsedMS:     10,
	}
	if err := repo.Record(ctx, obs); err != nil {
		t.Fatalf("Record: %v", err)
	}

	rows, err := db.QueryContext(ctx,
		`SELECT id, run_id, tool_name, arg_keys_json, args_hash, error_redacted, reason FROM tool_attempts WHERE run_id = ?`, runID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		found = true
		var id, runIDGot, toolName, argKeysJSON, argsHashVal, errRed, reason string
		if err := rows.Scan(&id, &runIDGot, &toolName, &argKeysJSON, &argsHashVal, &errRed, &reason); err != nil {
			t.Fatalf("scan: %v", err)
		}
		for _, field := range []string{id, runIDGot, toolName, argKeysJSON, argsHashVal, errRed, reason} {
			if strings.Contains(field, secret) {
				t.Errorf("secret value found in row field: %q", field)
			}
		}
	}
	if !found {
		t.Fatal("no row found after Record")
	}
}

func TestRecord_MCPToolKind(t *testing.T) {
	db := openMigratedDB(t)
	runID := "run_mcp"
	seedRun(t, db, runID)
	repo := attempts.NewSQLiteRepo(db)
	ctx := context.Background()

	obs := makeObs(runID, "mcp_github_search", "rate_limited", 0)
	if obs.ToolKind != "mcp" {
		t.Fatalf("ToolKindOf should produce 'mcp' for mcp_* name, got %q", obs.ToolKind)
	}
	if err := repo.Record(ctx, obs); err != nil {
		t.Fatalf("Record: %v", err)
	}

	var toolKind string
	err := db.QueryRowContext(ctx,
		`SELECT tool_kind FROM tool_attempts WHERE run_id = ?`, runID).Scan(&toolKind)
	if err != nil {
		t.Fatalf("query tool_kind: %v", err)
	}
	if toolKind != "mcp" {
		t.Errorf("tool_kind: got %q, want 'mcp'", toolKind)
	}
}

func TestRecord_Concurrent4Goroutines(t *testing.T) {
	db := openMigratedDB(t)
	runID := "run_concurrent"
	seedRun(t, db, runID)
	repo := attempts.NewSQLiteRepo(db)
	ctx := context.Background()

	const n = 4
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			obs := makeObs(runID, "web_search", "io", time.Duration(i)*time.Millisecond)
			errs[i] = repo.Record(ctx, obs)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d Record error: %v", i, err)
		}
	}

	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tool_attempts WHERE run_id = ?`, runID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != n {
		t.Errorf("concurrent Record: got %d rows, want %d distinct rows", count, n)
	}
}

func TestRecent_NewestFirst(t *testing.T) {
	db := openMigratedDB(t)
	runID := "run_recent_order"
	seedRun(t, db, runID)
	repo := attempts.NewSQLiteRepo(db)
	ctx := context.Background()

	// Insert 3 observations with ascending start times (100ms apart).
	for i := range 3 {
		obs := makeObs(runID, "web_fetch", "io", time.Duration(i)*100*time.Millisecond)
		if err := repo.Record(ctx, obs); err != nil {
			t.Fatalf("Record[%d]: %v", i, err)
		}
	}

	got, err := repo.Recent(ctx, runID, "web_fetch", 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Recent returned %d rows, want 3", len(got))
	}
	// Each EndedAt must be >= the next (newest first).
	for i := 1; i < len(got); i++ {
		if got[i-1].EndedAt.Before(got[i].EndedAt) {
			t.Errorf("not newest-first: row[%d].EndedAt %v < row[%d].EndedAt %v",
				i-1, got[i-1].EndedAt, i, got[i].EndedAt)
		}
	}
}

func TestRecent_Limit(t *testing.T) {
	db := openMigratedDB(t)
	runID := "run_recent_limit"
	seedRun(t, db, runID)
	repo := attempts.NewSQLiteRepo(db)
	ctx := context.Background()

	for i := range 5 {
		obs := makeObs(runID, "web_fetch", "io", time.Duration(i)*10*time.Millisecond)
		if err := repo.Record(ctx, obs); err != nil {
			t.Fatalf("Record[%d]: %v", i, err)
		}
	}

	got, err := repo.Recent(ctx, runID, "web_fetch", 3)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("Recent(limit=3) returned %d rows, want 3", len(got))
	}
}

func TestMigration_FreshDB(t *testing.T) {
	db := openMigratedDB(t)
	ctx := context.Background()

	// tool_attempts table must exist.
	var tableName string
	err := db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name='tool_attempts'`).Scan(&tableName)
	if err != nil {
		t.Fatalf("tool_attempts table missing after migration: %v", err)
	}

	// All 4 indexes must exist.
	for _, idx := range []string{
		"idx_tool_attempts_run_tool",
		"idx_tool_attempts_tool_outcome",
		"idx_tool_attempts_run_outcome",
		"idx_tool_attempts_signature",
	} {
		var idxName string
		if err := db.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&idxName); err != nil {
			t.Errorf("index %q missing after migration: %v", idx, err)
		}
	}

	// tool_warnings view must exist.
	var viewName string
	if err := db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='view' AND name='tool_warnings'`).Scan(&viewName); err != nil {
		t.Fatalf("tool_warnings view missing after migration: %v", err)
	}
}

func TestMigration_NoFKOnToolName_FKOnRunID(t *testing.T) {
	db := openMigratedDB(t)
	ctx := context.Background()

	rows, err := db.QueryContext(ctx, `PRAGMA foreign_key_list(tool_attempts)`)
	if err != nil {
		t.Fatalf("pragma foreign_key_list: %v", err)
	}
	defer rows.Close()

	var fkColumns []string
	for rows.Next() {
		var id, seq int
		var table, from, to, onUpdate, onDelete, match string
		if err := rows.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			t.Fatalf("scan fk: %v", err)
		}
		fkColumns = append(fkColumns, from)
	}

	for _, col := range fkColumns {
		if col == "tool_name" {
			t.Error("tool_name must NOT have a FK constraint (MCP servers come and go)")
		}
	}

	hasRunFK := false
	for _, col := range fkColumns {
		if col == "run_id" {
			hasRunFK = true
		}
	}
	if !hasRunFK {
		t.Error("run_id must have FK constraint to runs(id)")
	}
}
