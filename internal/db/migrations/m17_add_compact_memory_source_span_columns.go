package migrations

import (
	"context"
	"database/sql"
)

func addCompactMemorySourceSpanColumns(ctx context.Context, tx *sql.Tx) error {
	return addMissingColumns(ctx, tx, "compact_memory_documents", []columnDef{
		{Name: "chunk_index", SQL: "INTEGER NOT NULL DEFAULT 0"},
		{Name: "byte_start", SQL: "INTEGER NOT NULL DEFAULT 0"},
		{Name: "byte_end", SQL: "INTEGER NOT NULL DEFAULT 0"},
	})
}
