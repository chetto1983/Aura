package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

func addChatSettingsVoiceMode(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS chat_settings (
  chat_id    TEXT PRIMARY KEY,
  voice_mode TEXT NOT NULL DEFAULT 'off' CHECK(voice_mode IN ('off','voice_only','all')),
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`)
	if err != nil {
		return fmt.Errorf("migrations: add chat_settings: %w", err)
	}
	return nil
}
