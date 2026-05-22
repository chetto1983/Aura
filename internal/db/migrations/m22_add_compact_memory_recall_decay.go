package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

func addCompactMemoryRecallDecay(ctx context.Context, tx *sql.Tx) error {
	if err := addMissingColumns(ctx, tx, "compact_memory_documents", []columnDef{
		{Name: "last_recalled_at", SQL: "TEXT NOT NULL DEFAULT ''"},
		{Name: "recall_count", SQL: "INTEGER NOT NULL DEFAULT 0"},
	}); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
CREATE INDEX IF NOT EXISTS idx_compact_memory_recall_decay
  ON compact_memory_documents(kind, priority, status, last_recalled_at, updated_at);
`); err != nil {
		return fmt.Errorf("migrations: add compact memory recall decay index: %w", err)
	}
	return nil
}
