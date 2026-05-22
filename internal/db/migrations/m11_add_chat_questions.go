package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

func addChatQuestions(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS chat_questions (
  id                  TEXT PRIMARY KEY,
  run_id              TEXT NOT NULL,
  event_id            TEXT NOT NULL,
  answer_run_id       TEXT NOT NULL DEFAULT '',
  answer_event_id     TEXT NOT NULL DEFAULT '',
  thread_id           TEXT NOT NULL DEFAULT '',
  actor_id            TEXT NOT NULL DEFAULT '',
  channel             TEXT NOT NULL DEFAULT '',
  kind                TEXT NOT NULL DEFAULT 'clarification',
  status              TEXT NOT NULL DEFAULT 'waiting',
  question_text       TEXT NOT NULL DEFAULT '',
  options_json        TEXT NOT NULL DEFAULT '[]',
  answer_preview      TEXT NOT NULL DEFAULT '',
  answer_json         TEXT NOT NULL DEFAULT '{}',
  requested_at        TEXT NOT NULL,
  answered_at         TEXT,
  expires_at          TEXT,
  producer_json       TEXT NOT NULL DEFAULT '{}',
  metadata_json       TEXT NOT NULL DEFAULT '{}',
  FOREIGN KEY(run_id) REFERENCES runs(id),
  FOREIGN KEY(event_id) REFERENCES run_events(id)
);
CREATE INDEX IF NOT EXISTS idx_chat_questions_thread_status
  ON chat_questions(thread_id, channel, status, requested_at);
CREATE INDEX IF NOT EXISTS idx_chat_questions_run
  ON chat_questions(run_id, status, requested_at);
`)
	if err != nil {
		return fmt.Errorf("migrations: add chat questions: %w", err)
	}
	return nil
}
