package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

func addCompactMemoryIndex(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS compact_memory_documents (
  id              TEXT PRIMARY KEY,
  kind            TEXT NOT NULL,
  title           TEXT NOT NULL DEFAULT '',
  body            TEXT NOT NULL,
  handle          TEXT NOT NULL DEFAULT '',
  source_id       TEXT NOT NULL DEFAULT '',
  page            INTEGER NOT NULL DEFAULT 0,
  chunk_index     INTEGER NOT NULL DEFAULT 0,
  byte_start      INTEGER NOT NULL DEFAULT 0,
  byte_end        INTEGER NOT NULL DEFAULT 0,
  chat_id         INTEGER NOT NULL DEFAULT 0,
  conversation_id INTEGER NOT NULL DEFAULT 0,
  proposal_id     INTEGER NOT NULL DEFAULT 0,
  status          TEXT NOT NULL DEFAULT '',
  priority        TEXT NOT NULL DEFAULT 'normal',
  last_recalled_at TEXT NOT NULL DEFAULT '',
  recall_count    INTEGER NOT NULL DEFAULT 0,
  entities_json   TEXT NOT NULL DEFAULT '[]',
  tags_json       TEXT NOT NULL DEFAULT '[]',
  updated_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_compact_memory_kind
  ON compact_memory_documents(kind, updated_at);
CREATE INDEX IF NOT EXISTS idx_compact_memory_chat
  ON compact_memory_documents(kind, chat_id, updated_at);
CREATE INDEX IF NOT EXISTS idx_compact_memory_source
  ON compact_memory_documents(source_id);
CREATE INDEX IF NOT EXISTS idx_compact_memory_proposal
  ON compact_memory_documents(proposal_id);
CREATE INDEX IF NOT EXISTS idx_compact_memory_priority
  ON compact_memory_documents(kind, priority, updated_at);
CREATE INDEX IF NOT EXISTS idx_compact_memory_recall_decay
  ON compact_memory_documents(kind, priority, status, last_recalled_at, updated_at);
CREATE VIRTUAL TABLE IF NOT EXISTS compact_memory_fts
USING fts5(id UNINDEXED, kind, title, body, handle, source_id, status, entities, tags);
`)
	if err != nil {
		return fmt.Errorf("migrations: add compact memory index: %w", err)
	}
	return nil
}
