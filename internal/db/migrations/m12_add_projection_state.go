package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

func addProjectionState(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS projection_state (
  projection_id        TEXT PRIMARY KEY,
  kind                 TEXT NOT NULL,
  embedding_model_id   TEXT NOT NULL DEFAULT '',
  embedding_dim        INTEGER NOT NULL DEFAULT 0,
  index_build_id       TEXT NOT NULL DEFAULT '',
  schema_version       INTEGER NOT NULL DEFAULT 1,
  last_full_rebuild_at INTEGER NOT NULL DEFAULT 0,
  last_incremental_at  INTEGER,
  pending_count        INTEGER NOT NULL DEFAULT 0,
  completed_count      INTEGER NOT NULL DEFAULT 0,
  failed_count         INTEGER NOT NULL DEFAULT 0,
  status               TEXT NOT NULL DEFAULT 'fresh',
  health_reason        TEXT NOT NULL DEFAULT '',
  version              INTEGER NOT NULL DEFAULT 1,
  updated_at           INTEGER NOT NULL DEFAULT (unixepoch())
);
`)
	if err != nil {
		return fmt.Errorf("migrations: add projection_state: %w", err)
	}
	return nil
}
