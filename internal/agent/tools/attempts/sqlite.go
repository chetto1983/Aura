package attempts

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	tools "github.com/aura/aura/internal/agent/tools/registry"
)

// SQLiteRepo is the production Repo backed by Aura's shared SQLite pool.
// When db is nil every method returns ErrRepoUnavailable.
type SQLiteRepo struct {
	db *sql.DB
}

// NewSQLiteRepo creates a SQLiteRepo wrapping the given pool.
func NewSQLiteRepo(db *sql.DB) *SQLiteRepo {
	return &SQLiteRepo{db: db}
}

// Record writes one tool observation atomically inside BEGIN IMMEDIATE.
// On success the row is immediately visible to concurrent readers (WAL mode).
func (r *SQLiteRepo) Record(ctx context.Context, obs tools.ToolObservation) error {
	if r.db == nil {
		return ErrRepoUnavailable
	}

	argKeysJSON, err := json.Marshal(obs.ArgKeys)
	if err != nil || argKeysJSON == nil {
		argKeysJSON = []byte("[]")
	}
	argsHashHex := hex.EncodeToString(obs.ArgsHash[:])
	schemaHashHex := hex.EncodeToString(obs.SchemaHash[:])
	endedAt := obs.StartedAt.Add(time.Duration(obs.ElapsedMS) * time.Millisecond)
	id := newAttemptID()

	conn, err := r.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("attempts: acquire conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("attempts: begin immediate: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			conn.ExecContext(ctx, "ROLLBACK") //nolint:errcheck
		}
	}()

	_, err = conn.ExecContext(ctx, `INSERT INTO tool_attempts (
		id, run_id, tool_call_id, attempt_n, tool_name, tool_kind, tool_schema_hash,
		outcome, class, reason, args_hash, arg_keys_json, error_redacted,
		elapsed_ms, started_at, ended_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		obs.RunID,
		"",
		int(obs.AttemptN),
		obs.ToolName,
		obs.ToolKind,
		schemaHashHex,
		obs.Outcome.String(),
		obs.Class,
		obs.Reason,
		argsHashHex,
		string(argKeysJSON),
		obs.ErrorRedacted,
		obs.ElapsedMS,
		obs.StartedAt.UTC().Format(time.RFC3339Nano),
		endedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("attempts: insert tool_attempt: %w", err)
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("attempts: commit: %w", err)
	}
	committed = true
	return nil
}

// CountOutcome returns the count of tool_attempts rows matching (runID, toolName, outcome).
func (r *SQLiteRepo) CountOutcome(ctx context.Context, runID, toolName string, outcome tools.Outcome) (int, error) {
	if r.db == nil {
		return 0, ErrRepoUnavailable
	}
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tool_attempts WHERE run_id = ? AND tool_name = ? AND outcome = ?`,
		runID, toolName, outcome.String(),
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("attempts: count outcome: %w", err)
	}
	return count, nil
}

// Recent returns up to n most-recent attempts for toolName within runID, newest first.
func (r *SQLiteRepo) Recent(ctx context.Context, runID, toolName string, n int) ([]ToolAttempt, error) {
	if r.db == nil {
		return nil, ErrRepoUnavailable
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id, run_id, tool_name, tool_kind, outcome, class, reason,
       error_redacted, elapsed_ms, started_at, ended_at, attempt_n
FROM tool_attempts
WHERE run_id = ? AND tool_name = ?
ORDER BY ended_at DESC
LIMIT ?`, runID, toolName, n)
	if err != nil {
		return nil, fmt.Errorf("attempts: query recent: %w", err)
	}
	defer rows.Close()

	var attempts []ToolAttempt
	for rows.Next() {
		var ta ToolAttempt
		var startedAtStr, endedAtStr string
		if err := rows.Scan(
			&ta.ID, &ta.RunID, &ta.ToolName, &ta.ToolKind, &ta.Outcome,
			&ta.Class, &ta.Reason, &ta.ErrorRedacted, &ta.ElapsedMS,
			&startedAtStr, &endedAtStr, &ta.AttemptN,
		); err != nil {
			return nil, fmt.Errorf("attempts: scan recent: %w", err)
		}
		ta.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAtStr)
		ta.EndedAt, _ = time.Parse(time.RFC3339Nano, endedAtStr)
		attempts = append(attempts, ta)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("attempts: iterate recent: %w", err)
	}
	return attempts, nil
}

// newAttemptID generates a unique row identifier for tool_attempts.
func newAttemptID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "atm_0000000000000000"
	}
	return "atm_" + hex.EncodeToString(buf[:])
}

// Warnings returns aggregated failure counts for the last `days` days,
// grouped by (tool_name, tool_kind, class), ordered by count descending.
// Only recoverable, blocked, and fatal outcomes are included.
func (r *SQLiteRepo) Warnings(ctx context.Context, days int) ([]WarningRow, error) {
	if r.db == nil {
		return nil, ErrRepoUnavailable
	}
	offset := fmt.Sprintf("-%d days", days)
	rows, err := r.db.QueryContext(ctx, `
SELECT tool_name, tool_kind, class, COUNT(*) AS n, MAX(ended_at) AS last_seen
FROM tool_attempts
WHERE outcome IN ('recoverable','blocked','fatal')
  AND ended_at >= datetime('now', ?)
GROUP BY tool_name, tool_kind, class
ORDER BY n DESC`, offset)
	if err != nil {
		return nil, fmt.Errorf("attempts: query warnings: %w", err)
	}
	defer rows.Close()

	var result []WarningRow
	for rows.Next() {
		var wr WarningRow
		var lastSeenStr string
		if err := rows.Scan(&wr.ToolName, &wr.ToolKind, &wr.Class, &wr.N, &lastSeenStr); err != nil {
			return nil, fmt.Errorf("attempts: scan warning: %w", err)
		}
		wr.LastSeen, _ = time.Parse(time.RFC3339Nano, lastSeenStr)
		result = append(result, wr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("attempts: iterate warnings: %w", err)
	}
	return result, nil
}

// CanonicalArgsJSON marshals args with sorted keys for a stable SHA-256 hash
// across LLM re-orderings. Values are consumed only for hashing and are never
// stored in the DB — only the key names and the resulting hash are persisted.
func CanonicalArgsJSON(args map[string]any) []byte {
	if len(args) == 0 {
		return []byte("{}")
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make(map[string]any, len(args))
	for _, k := range keys {
		ordered[k] = args[k]
	}
	b, err := json.Marshal(ordered)
	if err != nil {
		return []byte("{}")
	}
	return b
}
