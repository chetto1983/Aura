package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

func addConversationCompactions(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS conversation_compactions (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id            TEXT NOT NULL DEFAULT '',
  chat_id           INTEGER NOT NULL,
  turn_index        INTEGER NOT NULL,
  iteration         INTEGER NOT NULL DEFAULT 0,
  messages_before   INTEGER NOT NULL DEFAULT 0,
  messages_after    INTEGER NOT NULL DEFAULT 0,
  tokens_before     INTEGER NOT NULL DEFAULT 0,
  tokens_after      INTEGER NOT NULL DEFAULT 0,
  threshold_tokens  INTEGER NOT NULL DEFAULT 0,
  cumulative_tokens INTEGER NOT NULL DEFAULT 0,
  focus_preview     TEXT NOT NULL DEFAULT '',
  elapsed_ms        INTEGER NOT NULL DEFAULT 0,
  created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_conversation_compactions_turn
  ON conversation_compactions(chat_id, turn_index, created_at);
CREATE INDEX IF NOT EXISTS idx_conversation_compactions_run
  ON conversation_compactions(run_id, created_at);
`); err != nil {
		return fmt.Errorf("migrations: add conversation compactions: %w", err)
	}
	return nil
}
