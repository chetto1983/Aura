package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

// addToolIndexState creates the tool_index_state table used by
// internal/toolindex.Reconciler to diff the live tool registry against the
// indexed set in Qdrant. Qdrant has no scroll API, so this table is the
// authoritative "what is currently indexed" set; reconcile reads it,
// computes the upsert/delete buckets, and writes back atomically.
func addToolIndexState(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS tool_index_state (
  tool_name    TEXT PRIMARY KEY,
  content_hash TEXT NOT NULL,
  point_id     TEXT NOT NULL,
  embed_model  TEXT NOT NULL,
  indexed_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tool_index_state_hash
  ON tool_index_state(content_hash);
`)
	if err != nil {
		return fmt.Errorf("migrations: add tool_index_state: %w", err)
	}
	return nil
}
