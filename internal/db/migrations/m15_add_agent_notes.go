package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

func addAgentNotes(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS agent_notes (
  conversation_id TEXT PRIMARY KEY,
  content         TEXT NOT NULL DEFAULT '',
  updated_at      INTEGER NOT NULL DEFAULT (unixepoch())
);
`)
	if err != nil {
		return fmt.Errorf("migrations: add agent_notes: %w", err)
	}
	return nil
}
