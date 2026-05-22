package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

func addVoiceDispatches(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS voice_dispatches (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  chat_id    TEXT NOT NULL,
  bytes      INTEGER NOT NULL,
  voice      TEXT NOT NULL,
  language   TEXT NOT NULL,
  elapsed_ms INTEGER NOT NULL,
  at         TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_voice_dispatches_chat_at ON voice_dispatches(chat_id, at);
`)
	if err != nil {
		return fmt.Errorf("migrations: add voice_dispatches: %w", err)
	}
	return nil
}
