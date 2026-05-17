package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/testutil"
)

func newAttemptsTestReader(t *testing.T) *SQLiteToolAttemptsReader {
	t.Helper()
	db := testutil.OpenTestDB(t, migrations.Run)
	return NewSQLiteToolAttemptsReader(db)
}

// seedRunForAttempts inserts a minimal runs row to satisfy the tool_attempts FK.
func seedRunForAttempts(t *testing.T, r *SQLiteToolAttemptsReader, runID string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.ExecContext(context.Background(),
		`INSERT OR IGNORE INTO runs (id, channel, status, started_at, updated_at)
		 VALUES (?, 'test', 'completed', ?, ?)`,
		runID, now, now,
	)
	if err != nil {
		t.Fatalf("seedRunForAttempts: %v", err)
	}
}

// seedAttempt inserts one tool_attempts row.
func seedAttempt(t *testing.T, r *SQLiteToolAttemptsReader, id, runID, toolName, outcome string, endedAt time.Time) {
	t.Helper()
	now := endedAt.UTC().Format(time.RFC3339)
	_, err := r.db.ExecContext(context.Background(),
		`INSERT INTO tool_attempts
		 (id, run_id, tool_name, tool_kind, outcome, started_at, ended_at)
		 VALUES (?, ?, ?, 'native', ?, ?, ?)`,
		id, runID, toolName, outcome, now, now,
	)
	if err != nil {
		t.Fatalf("seedAttempt: %v", err)
	}
}

func TestHandleMaintenanceToolAttempts_ShapeAndCounts(t *testing.T) {
	reader := newAttemptsTestReader(t)
	now := time.Now().UTC()
	runID := "run-t05-test"
	seedRunForAttempts(t, reader, runID)

	// Seed web_search: 4 ok, 2 fatal — all within 24h.
	for i := 0; i < 4; i++ {
		seedAttempt(t, reader, fmt.Sprintf("ws-ok-%d", i), runID, "web_search", "ok", now.Add(-time.Duration(i)*time.Hour))
	}
	for i := 0; i < 2; i++ {
		seedAttempt(t, reader, fmt.Sprintf("ws-fatal-%d", i), runID, "web_search", "fatal", now.Add(-time.Duration(i)*time.Hour))
	}

	// Seed memory_search: 3 ok — within 24h.
	for i := 0; i < 3; i++ {
		seedAttempt(t, reader, fmt.Sprintf("ms-ok-%d", i), runID, "memory_search", "ok", now.Add(-time.Duration(i)*time.Hour))
	}

	router := NewRouter(Deps{ToolAttemptsStats: reader})
	req := httptest.NewRequest("GET", "/maintenance/tool-attempts", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var body ToolAttemptsResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// per_tool must have 2 tools.
	if len(body.PerTool) != 2 {
		t.Fatalf("want 2 tools in per_tool, got %d: %+v", len(body.PerTool), body.PerTool)
	}

	ws := body.PerTool["web_search"]
	if ws.OK != 4 {
		t.Errorf("web_search ok = %d, want 4", ws.OK)
	}
	if ws.Fatal != 2 {
		t.Errorf("web_search fatal = %d, want 2", ws.Fatal)
	}

	ms := body.PerTool["memory_search"]
	if ms.OK != 3 {
		t.Errorf("memory_search ok = %d, want 3", ms.OK)
	}

	// retry_budget_consumption must have 2 rows, busiest first.
	if len(body.RetryBudgetConsumption) != 2 {
		t.Fatalf("want 2 budget rows, got %d", len(body.RetryBudgetConsumption))
	}

	// web_search has 6 attempts — should be first.
	if body.RetryBudgetConsumption[0].ToolName != "web_search" {
		t.Errorf("first budget row = %q, want web_search", body.RetryBudgetConsumption[0].ToolName)
	}
	if body.RetryBudgetConsumption[0].AttemptsToday != 6 {
		t.Errorf("web_search attempts_today = %d, want 6", body.RetryBudgetConsumption[0].AttemptsToday)
	}
	if body.RetryBudgetConsumption[0].Budget != dailyToolBudget {
		t.Errorf("budget = %d, want %d", body.RetryBudgetConsumption[0].Budget, dailyToolBudget)
	}
	wantPct := float64(6) / float64(dailyToolBudget) * 100
	if got := body.RetryBudgetConsumption[0].Percent; got < wantPct-0.001 || got > wantPct+0.001 {
		t.Errorf("percent = %f, want %f", got, wantPct)
	}
}

func TestHandleMaintenanceToolAttempts_NilReader(t *testing.T) {
	router := NewRouter(Deps{ToolAttemptsStats: nil})
	req := httptest.NewRequest("GET", "/maintenance/tool-attempts", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 with nil reader, got %d", w.Code)
	}

	var body ToolAttemptsResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.PerTool) != 0 {
		t.Errorf("want empty per_tool, got %d entries", len(body.PerTool))
	}
	if len(body.RetryBudgetConsumption) != 0 {
		t.Errorf("want empty retry_budget_consumption, got %d entries", len(body.RetryBudgetConsumption))
	}
}

func TestHandleMaintenanceToolAttempts_OldRowsExcluded(t *testing.T) {
	reader := newAttemptsTestReader(t)
	now := time.Now().UTC()
	runID := "run-t05-old"
	seedRunForAttempts(t, reader, runID)

	// 1 attempt within 24h.
	seedAttempt(t, reader, "new-ok", runID, "web_fetch", "ok", now.Add(-1*time.Hour))
	// 5 old attempts outside the window (should not appear).
	for i := 0; i < 5; i++ {
		seedAttempt(t, reader, fmt.Sprintf("old-%d", i), runID, "web_fetch", "fatal", now.Add(-48*time.Hour))
	}

	router := NewRouter(Deps{ToolAttemptsStats: reader})
	req := httptest.NewRequest("GET", "/maintenance/tool-attempts", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var body ToolAttemptsResponse
	json.NewDecoder(w.Body).Decode(&body)

	wf := body.PerTool["web_fetch"]
	if wf.OK != 1 {
		t.Errorf("web_fetch ok = %d, want 1 (old rows excluded)", wf.OK)
	}
	if wf.Fatal != 0 {
		t.Errorf("web_fetch fatal = %d, want 0 (old rows excluded)", wf.Fatal)
	}
	if len(body.RetryBudgetConsumption) != 1 {
		t.Fatalf("want 1 budget row, got %d", len(body.RetryBudgetConsumption))
	}
	if body.RetryBudgetConsumption[0].AttemptsToday != 1 {
		t.Errorf("attempts_today = %d, want 1", body.RetryBudgetConsumption[0].AttemptsToday)
	}
}
