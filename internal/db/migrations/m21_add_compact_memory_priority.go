package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

func addCompactMemoryPriority(ctx context.Context, tx *sql.Tx) error {
	if err := addMissingColumns(ctx, tx, "compact_memory_documents", []columnDef{
		{Name: "priority", SQL: "TEXT NOT NULL DEFAULT 'normal'"},
	}); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
CREATE INDEX IF NOT EXISTS idx_compact_memory_priority
  ON compact_memory_documents(kind, priority, updated_at);
`); err != nil {
		return fmt.Errorf("migrations: add compact memory priority index: %w", err)
	}
	return nil
}
