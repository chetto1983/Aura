package api

import (
	"context"
	"database/sql"
	"net/http"
	"time"
)

// ToolAttemptsReader aggregates tool_attempts for the operator dashboard.
type ToolAttemptsReader interface {
	GetToolAttemptsStats(ctx context.Context, window time.Duration) (AttemptsStats, error)
}

// AttemptsStats is the aggregated view returned by GET /maintenance/tool-attempts.
type AttemptsStats struct {
	PerTool                map[string]PerToolCounts
	RetryBudgetConsumption []BudgetRow
}

// PerToolCounts holds 24h outcome bucket counts for one tool.
type PerToolCounts struct {
	OK          int
	Recoverable int
	Blocked     int
	Fatal       int
	Cancelled   int
}

// BudgetRow is one tool's daily attempt budget summary.
type BudgetRow struct {
	ToolName      string
	AttemptsToday int
	Budget        int
	Percent       float64
}

// dailyToolBudget is the static daily cap used to compute percent budget consumption.
const dailyToolBudget = 100

// SQLiteToolAttemptsReader reads tool_attempts from a *sql.DB.
type SQLiteToolAttemptsReader struct {
	db *sql.DB
}

// NewSQLiteToolAttemptsReader wraps an existing connection pool.
func NewSQLiteToolAttemptsReader(db *sql.DB) *SQLiteToolAttemptsReader {
	return &SQLiteToolAttemptsReader{db: db}
}

func (r *SQLiteToolAttemptsReader) GetToolAttemptsStats(ctx context.Context, window time.Duration) (AttemptsStats, error) {
	cutoff := time.Now().UTC().Add(-window).Format(time.RFC3339)

	// per_tool — outcome bucket counts per tool, uses idx_tool_attempts_tool_outcome.
	rows, err := r.db.QueryContext(ctx, `
SELECT tool_name,
  SUM(CASE WHEN outcome = 'ok'          THEN 1 ELSE 0 END) AS ok,
  SUM(CASE WHEN outcome = 'recoverable' THEN 1 ELSE 0 END) AS recoverable,
  SUM(CASE WHEN outcome = 'blocked'     THEN 1 ELSE 0 END) AS blocked,
  SUM(CASE WHEN outcome = 'fatal'       THEN 1 ELSE 0 END) AS fatal,
  SUM(CASE WHEN outcome = 'cancelled'   THEN 1 ELSE 0 END) AS cancelled
FROM tool_attempts
WHERE ended_at >= ?
GROUP BY tool_name
ORDER BY tool_name`, cutoff)
	if err != nil {
		return AttemptsStats{}, err
	}
	defer rows.Close()

	perTool := map[string]PerToolCounts{}
	for rows.Next() {
		var name string
		var counts PerToolCounts
		if err := rows.Scan(&name, &counts.OK, &counts.Recoverable, &counts.Blocked, &counts.Fatal, &counts.Cancelled); err != nil {
			return AttemptsStats{}, err
		}
		perTool[name] = counts
	}
	if err := rows.Err(); err != nil {
		return AttemptsStats{}, err
	}

	// retry_budget_consumption — total attempts per tool sorted busiest first.
	budgetRows, err := r.db.QueryContext(ctx, `
SELECT tool_name, COUNT(*) AS attempts_today
FROM tool_attempts
WHERE ended_at >= ?
GROUP BY tool_name
ORDER BY attempts_today DESC`, cutoff)
	if err != nil {
		return AttemptsStats{}, err
	}
	defer budgetRows.Close()

	var budget []BudgetRow
	for budgetRows.Next() {
		var row BudgetRow
		row.Budget = dailyToolBudget
		if err := budgetRows.Scan(&row.ToolName, &row.AttemptsToday); err != nil {
			return AttemptsStats{}, err
		}
		row.Percent = float64(row.AttemptsToday) / float64(row.Budget) * 100
		budget = append(budget, row)
	}
	if err := budgetRows.Err(); err != nil {
		return AttemptsStats{}, err
	}

	return AttemptsStats{
		PerTool:                perTool,
		RetryBudgetConsumption: budget,
	}, nil
}

func handleMaintenanceToolAttempts(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.ToolAttemptsStats == nil {
			writeJSON(w, deps.Logger, http.StatusOK, ToolAttemptsResponse{
				PerTool:                map[string]ToolOutcomeCounts{},
				RetryBudgetConsumption: []ToolBudgetRow{},
			})
			return
		}

		stats, err := deps.ToolAttemptsStats.GetToolAttemptsStats(r.Context(), 24*time.Hour)
		if err != nil {
			deps.Logger.Warn("api: tool attempts stats query", "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to query tool attempts")
			return
		}

		perTool := make(map[string]ToolOutcomeCounts, len(stats.PerTool))
		for name, c := range stats.PerTool {
			perTool[name] = ToolOutcomeCounts{
				OK:          c.OK,
				Recoverable: c.Recoverable,
				Blocked:     c.Blocked,
				Fatal:       c.Fatal,
				Cancelled:   c.Cancelled,
			}
		}

		budget := make([]ToolBudgetRow, 0, len(stats.RetryBudgetConsumption))
		for _, b := range stats.RetryBudgetConsumption {
			budget = append(budget, ToolBudgetRow{
				ToolName:      b.ToolName,
				AttemptsToday: b.AttemptsToday,
				Budget:        b.Budget,
				Percent:       b.Percent,
			})
		}

		writeJSON(w, deps.Logger, http.StatusOK, ToolAttemptsResponse{
			PerTool:                perTool,
			RetryBudgetConsumption: budget,
		})
	}
}
