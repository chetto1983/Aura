package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

func addToolAttempts(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS tool_attempts (
  id               TEXT PRIMARY KEY,
  run_id           TEXT NOT NULL,
  tool_call_id     TEXT NOT NULL DEFAULT '',
  attempt_n        INTEGER NOT NULL DEFAULT 1,
  tool_name        TEXT NOT NULL,
  tool_kind        TEXT NOT NULL DEFAULT 'native',
  tool_schema_hash TEXT NOT NULL DEFAULT '',
  outcome          TEXT NOT NULL,
  class            TEXT NOT NULL DEFAULT '',
  reason           TEXT NOT NULL DEFAULT '',
  args_hash        TEXT NOT NULL DEFAULT '',
  arg_keys_json    TEXT NOT NULL DEFAULT '[]',
  error_redacted   TEXT NOT NULL DEFAULT '',
  elapsed_ms       INTEGER NOT NULL DEFAULT 0,
  started_at       TEXT NOT NULL,
  ended_at         TEXT NOT NULL,
  FOREIGN KEY(run_id) REFERENCES runs(id)
);
CREATE INDEX IF NOT EXISTS idx_tool_attempts_run_tool
  ON tool_attempts(run_id, tool_name, ended_at);
CREATE INDEX IF NOT EXISTS idx_tool_attempts_tool_outcome
  ON tool_attempts(tool_name, outcome, ended_at);
CREATE INDEX IF NOT EXISTS idx_tool_attempts_run_outcome
  ON tool_attempts(run_id, outcome);
CREATE INDEX IF NOT EXISTS idx_tool_attempts_signature
  ON tool_attempts(tool_name, args_hash, outcome);

CREATE VIEW IF NOT EXISTS tool_warnings AS
  SELECT tool_name, class, COUNT(*) AS n, MAX(ended_at) AS last_seen,
         SUM(CASE WHEN outcome='recoverable' THEN 1 ELSE 0 END) AS n_recoverable,
         SUM(CASE WHEN outcome='blocked'     THEN 1 ELSE 0 END) AS n_blocked,
         SUM(CASE WHEN outcome='fatal'       THEN 1 ELSE 0 END) AS n_fatal
  FROM tool_attempts
  WHERE outcome IN ('recoverable','blocked','fatal')
    AND ended_at > datetime('now','-7 days')
  GROUP BY tool_name, class;
`)
	if err != nil {
		return fmt.Errorf("migrations: add tool_attempts: %w", err)
	}
	return nil
}
