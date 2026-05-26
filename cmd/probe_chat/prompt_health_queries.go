package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

func (e *Env) promptHealthViewsPresent() (bool, error) {
	rows, err := e.promptHealthQueryRows(`SELECT COUNT(*) AS count FROM sqlite_master WHERE type = 'view' AND name IN (` + sqliteStringList(promptHealthViews) + `);`)
	if err != nil {
		return false, err
	}
	if len(rows) == 0 {
		return false, nil
	}
	return rowInt64(rows[0], "count") == int64(len(promptHealthViews)), nil
}

func (e *Env) promptHealthRollup() (PromptHealthRollup, error) {
	rows, err := e.promptHealthQueryRowsFallback(
		`SELECT turn_rows, distinct_chats, assistant_turns, llm_calls, tool_calls, max_llm_calls, max_tool_calls, max_prompt_tokens, prompt_tokens, completion_tokens, max_elapsed_ms, elapsed_ms, compactions, max_compaction_tokens_before, max_compaction_tokens_after FROM prompt_health_rollup_24h;`,
		`WITH turns AS (
  SELECT * FROM conversations WHERE datetime(created_at) >= datetime('now', '-24 hours')
)
SELECT
  COUNT(*) AS turn_rows,
  COUNT(DISTINCT chat_id) AS distinct_chats,
  SUM(CASE WHEN role = 'assistant' THEN 1 ELSE 0 END) AS assistant_turns,
  COALESCE(SUM(llm_calls), 0) AS llm_calls,
  COALESCE(SUM(tool_calls_count), 0) AS tool_calls,
  COALESCE(MAX(llm_calls), 0) AS max_llm_calls,
  COALESCE(MAX(tool_calls_count), 0) AS max_tool_calls,
  COALESCE(MAX(tokens_in), 0) AS max_prompt_tokens,
  COALESCE(SUM(tokens_in), 0) AS prompt_tokens,
  COALESCE(SUM(tokens_out), 0) AS completion_tokens,
  COALESCE(MAX(elapsed_ms), 0) AS max_elapsed_ms,
  COALESCE(SUM(elapsed_ms), 0) AS elapsed_ms,
  (SELECT COUNT(*) FROM conversation_compactions WHERE datetime(created_at) >= datetime('now', '-24 hours')) AS compactions,
  COALESCE((SELECT MAX(tokens_before) FROM conversation_compactions WHERE datetime(created_at) >= datetime('now', '-24 hours')), 0) AS max_compaction_tokens_before,
  COALESCE((SELECT MAX(tokens_after) FROM conversation_compactions WHERE datetime(created_at) >= datetime('now', '-24 hours')), 0) AS max_compaction_tokens_after
FROM turns;`,
	)
	if err != nil {
		return PromptHealthRollup{}, fmt.Errorf("prompt health rollup: %w", err)
	}
	if len(rows) == 0 {
		return PromptHealthRollup{}, nil
	}
	row := rows[0]
	return PromptHealthRollup{
		TurnRows:                  rowInt64(row, "turn_rows"),
		AssistantTurns:            rowInt64(row, "assistant_turns"),
		DistinctChats:             rowInt64(row, "distinct_chats"),
		LLMCalls:                  rowInt64(row, "llm_calls"),
		ToolCalls:                 rowInt64(row, "tool_calls"),
		Compactions:               rowInt64(row, "compactions"),
		PromptTokens:              rowInt64(row, "prompt_tokens"),
		CompletionTokens:          rowInt64(row, "completion_tokens"),
		MaxPromptTokens:           rowInt64(row, "max_prompt_tokens"),
		MaxLLMCalls:               rowInt64(row, "max_llm_calls"),
		MaxToolCalls:              rowInt64(row, "max_tool_calls"),
		MaxElapsedMS:              rowInt64(row, "max_elapsed_ms"),
		MaxCompactionTokensBefore: rowInt64(row, "max_compaction_tokens_before"),
		MaxCompactionTokensAfter:  rowInt64(row, "max_compaction_tokens_after"),
	}, nil
}

func (e *Env) promptHealthPercentiles() (PromptHealthPercentiles, error) {
	rows, err := e.promptHealthQueryRowsFallback(
		`SELECT tokens_in FROM prompt_health_turns_24h WHERE role = 'assistant' AND tokens_in > 0 ORDER BY tokens_in ASC;`,
		`SELECT tokens_in FROM conversations WHERE datetime(created_at) >= datetime('now', '-24 hours') AND role = 'assistant' AND tokens_in > 0 ORDER BY tokens_in ASC;`,
	)
	if err != nil {
		return PromptHealthPercentiles{}, fmt.Errorf("prompt health percentiles: %w", err)
	}
	values := make([]int64, 0, len(rows))
	for _, row := range rows {
		values = append(values, rowInt64(row, "tokens_in"))
	}
	return PromptHealthPercentiles{
		PromptTokensP50: percentileNearest(values, 0.50),
		PromptTokensP95: percentileNearest(values, 0.95),
	}, nil
}

func (e *Env) promptHealthOutliers() ([]PromptHealthTurn, error) {
	rows, err := e.promptHealthQueryRowsFallback(
		`SELECT id, channel, chat_id, turn_index, role, created_at, llm_calls, tool_calls_count, elapsed_ms, tokens_in, tokens_out
FROM prompt_health_turns_24h
WHERE role = 'assistant'
ORDER BY tokens_in DESC, llm_calls DESC, tool_calls_count DESC, elapsed_ms DESC
LIMIT 10;`,
		`SELECT id, channel, chat_id, turn_index, role, created_at, llm_calls, tool_calls_count, elapsed_ms, tokens_in, tokens_out
FROM conversations
WHERE datetime(created_at) >= datetime('now', '-24 hours') AND role = 'assistant'
ORDER BY tokens_in DESC, llm_calls DESC, tool_calls_count DESC, elapsed_ms DESC
LIMIT 10;`,
	)
	if err != nil {
		return nil, fmt.Errorf("prompt health outliers: %w", err)
	}
	out := make([]PromptHealthTurn, 0, len(rows))
	for _, row := range rows {
		out = append(out, PromptHealthTurn{
			ID:             rowInt64(row, "id"),
			Channel:        rowString(row, "channel"),
			ChatHash:       shortHash(fmt.Sprintf("%d", rowInt64(row, "chat_id"))),
			TurnIndex:      rowInt64(row, "turn_index"),
			Role:           rowString(row, "role"),
			CreatedAt:      rowString(row, "created_at"),
			LLMCalls:       rowInt64(row, "llm_calls"),
			ToolCallsCount: rowInt64(row, "tool_calls_count"),
			ElapsedMS:      rowInt64(row, "elapsed_ms"),
			TokensIn:       rowInt64(row, "tokens_in"),
			TokensOut:      rowInt64(row, "tokens_out"),
		})
	}
	return out, nil
}

func (e *Env) promptHealthToolOutcomes() ([]PromptHealthToolBucket, error) {
	rows, err := e.promptHealthQueryRowsFallback(
		`SELECT tool_name, outcome, class, count, max_elapsed_ms, last_seen FROM prompt_health_tool_outcomes_24h ORDER BY count DESC, tool_name ASC LIMIT 20;`,
		`SELECT tool_name, outcome, class, COUNT(*) AS count, COALESCE(MAX(elapsed_ms), 0) AS max_elapsed_ms, MAX(ended_at) AS last_seen
FROM tool_attempts
WHERE datetime(ended_at) >= datetime('now', '-24 hours')
GROUP BY tool_name, outcome, class
ORDER BY count DESC, tool_name ASC
LIMIT 20;`,
	)
	if err != nil {
		return nil, fmt.Errorf("prompt health tool outcomes: %w", err)
	}
	out := make([]PromptHealthToolBucket, 0, len(rows))
	for _, row := range rows {
		out = append(out, PromptHealthToolBucket{
			ToolName:     rowString(row, "tool_name"),
			Outcome:      rowString(row, "outcome"),
			Class:        rowString(row, "class"),
			Count:        rowInt64(row, "count"),
			MaxElapsedMS: rowInt64(row, "max_elapsed_ms"),
			LastSeen:     rowString(row, "last_seen"),
		})
	}
	return out, nil
}

func (e *Env) promptHealthMemoryKinds() ([]PromptHealthMemoryKind, error) {
	rows, err := e.promptHealthMetricRows(promptHealthMemoryKindsViewSQL, promptHealthMemoryKindsFallbackSQL, "memory kinds")
	if err != nil {
		return nil, err
	}
	return promptHealthRows(rows, promptHealthMemoryKindFromRow), nil
}

func (e *Env) promptHealthRunEvents() ([]PromptHealthEventBucket, error) {
	rows, err := e.promptHealthMetricRows(promptHealthRunEventsViewSQL, promptHealthRunEventsFallbackSQL, "run events")
	if err != nil {
		return nil, err
	}
	return promptHealthRows(rows, promptHealthEventBucketFromRow), nil
}

const promptHealthMemoryKindsViewSQL = `SELECT kind, status, count, last_updated FROM prompt_health_memory_kinds ORDER BY count DESC, kind ASC LIMIT 20;`

const promptHealthMemoryKindsFallbackSQL = `SELECT kind, status, COUNT(*) AS count, MAX(updated_at) AS last_updated
FROM compact_memory_documents
GROUP BY kind, status
ORDER BY count DESC, kind ASC
LIMIT 20;`

const promptHealthRunEventsViewSQL = `SELECT type, run_origin, count, last_seen FROM prompt_health_run_events_24h ORDER BY count DESC, type ASC LIMIT 20;`

const promptHealthRunEventsFallbackSQL = `SELECT type, run_origin, COUNT(*) AS count, MAX(created_at) AS last_seen
FROM run_events
WHERE datetime(created_at) >= datetime('now', '-24 hours')
GROUP BY type, run_origin
ORDER BY count DESC, type ASC
LIMIT 20;`

func (e *Env) promptHealthMetricRows(viewQuery, fallbackQuery, label string) ([]map[string]any, error) {
	rows, err := e.promptHealthQueryRowsFallback(viewQuery, fallbackQuery)
	if err != nil {
		return nil, fmt.Errorf("prompt health %s: %w", label, err)
	}
	return rows, nil
}

func promptHealthMemoryKindFromRow(row map[string]any) PromptHealthMemoryKind {
	return PromptHealthMemoryKind{
		Kind:        rowString(row, "kind"),
		Status:      rowString(row, "status"),
		Count:       rowInt64(row, "count"),
		LastUpdated: rowString(row, "last_updated"),
	}
}

func promptHealthEventBucketFromRow(row map[string]any) PromptHealthEventBucket {
	return PromptHealthEventBucket{
		Type:      rowString(row, "type"),
		RunOrigin: rowString(row, "run_origin"),
		Count:     rowInt64(row, "count"),
		LastSeen:  rowString(row, "last_seen"),
	}
}

func promptHealthRows[T any](rows []map[string]any, build func(map[string]any) T) []T {
	out := make([]T, 0, len(rows))
	for _, row := range rows {
		out = append(out, build(row))
	}
	return out
}

func (e *Env) promptHealthQueryRowsFallback(viewQuery, fallbackQuery string) ([]map[string]any, error) {
	rows, err := e.promptHealthQueryRows(viewQuery)
	if err == nil {
		return rows, nil
	}
	if !isMissingSQLiteObject(err) {
		return nil, err
	}
	return e.promptHealthQueryRows(fallbackQuery)
}

func (e *Env) promptHealthQueryRows(query string) ([]map[string]any, error) {
	if e.dockerDBReady() {
		raw, err := e.dockerSQLite(true, true, query)
		if err != nil {
			return nil, err
		}
		if len(raw) == 0 {
			raw = []byte("[]")
		}
		var rows []map[string]any
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, err
		}
		return rows, nil
	}
	return queryRowsMap(e.DB, query)
}

func queryRowsMap(db *sql.DB, query string) (out []map[string]any, err error) {
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		raw := make([]any, len(cols))
		dest := make([]any, len(cols))
		for i := range raw {
			dest[i] = &raw[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			switch v := raw[i].(type) {
			case []byte:
				row[col] = string(v)
			default:
				row[col] = v
			}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func isMissingSQLiteObject(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such table") || strings.Contains(msg, "no such view")
}

func rowString(row map[string]any, key string) string {
	switch v := row[key].(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func rowInt64(row map[string]any, key string) int64 {
	switch v := row[key].(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		var n int64
		_, _ = fmt.Sscan(v, &n)
		return n
	default:
		return 0
	}
}

func percentileNearest(values []int64, p float64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
