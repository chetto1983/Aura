package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

func (e *Env) toolAttemptOKCountsSince(since time.Time, toolNames ...string) (map[string]int, error) {
	toolNames = nonEmptyStrings(toolNames)
	if len(toolNames) == 0 {
		return map[string]int{}, nil
	}
	if e.dockerDBReady() {
		query := `
SELECT tool_name, COUNT(*) AS count
FROM tool_attempts
WHERE started_at >= ` + sqliteQuote(since.UTC().Format(time.RFC3339Nano)) + `
  AND outcome = 'ok'
  AND tool_name IN (` + sqliteStringList(toolNames) + `)
GROUP BY tool_name;`
		return e.dockerCountRows(query, "tool_name", "tool_attempts")
	}

	args := []any{since.UTC().Format(time.RFC3339Nano)}
	placeholders := make([]string, 0, len(toolNames))
	for _, name := range toolNames {
		placeholders = append(placeholders, "?")
		args = append(args, name)
	}
	return countRowsSQL(e.DB, `
SELECT tool_name, COUNT(*)
FROM tool_attempts
WHERE started_at >= ?
  AND outcome = 'ok'
  AND tool_name IN (`+strings.Join(placeholders, ",")+`)
GROUP BY tool_name
`, args...)
}

func (e *Env) toolAttemptOutcomeCounts(since time.Time, toolName string) (map[string]int, error) {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return map[string]int{}, nil
	}
	if e.dockerDBReady() {
		query := `
SELECT outcome, COUNT(*) AS count
FROM tool_attempts
WHERE started_at >= ` + sqliteQuote(since.UTC().Format(time.RFC3339Nano)) + `
  AND tool_name = ` + sqliteQuote(toolName) + `
GROUP BY outcome;`
		return e.dockerCountRows(query, "outcome", "tool_attempt outcomes")
	}
	return countRowsSQL(e.DB, `
SELECT outcome, COUNT(*)
FROM tool_attempts
WHERE started_at >= ?
  AND tool_name = ?
GROUP BY outcome
`, since.UTC().Format(time.RFC3339Nano), toolName)
}

func (e *Env) dockerCountRows(query, keyName, label string) (map[string]int, error) {
	raw, err := e.dockerSQLite(true, true, query)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		raw = []byte("[]")
	}
	out, err := decodeCountRows(raw, keyName)
	if err != nil {
		return nil, fmt.Errorf("decode docker %s: %w", label, err)
	}
	return out, nil
}

func decodeCountRows(raw []byte, keyName string) (map[string]int, error) {
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, row := range rows {
		key, _ := row[keyName].(string)
		if key == "" {
			continue
		}
		count, ok := jsonNumberAsInt(row["count"])
		if !ok {
			continue
		}
		out[key] = count
	}
	return out, nil
}

func jsonNumberAsInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if math.Trunc(n) != n {
			return 0, false
		}
		return int(n), true
	case int:
		return n, true
	default:
		return 0, false
	}
}

func countRowsSQL(db *sql.DB, query string, args ...any) (out map[string]int, err error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	out = map[string]int{}
	for rows.Next() {
		var key string
		var count int
		if err := rows.Scan(&key, &count); err != nil {
			return nil, err
		}
		out[key] = count
	}
	return out, rows.Err()
}
