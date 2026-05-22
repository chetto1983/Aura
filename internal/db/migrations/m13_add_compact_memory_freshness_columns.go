package migrations

import (
	"context"
	"database/sql"
)

func addCompactMemoryFreshnessColumns(ctx context.Context, tx *sql.Tx) error {
	return addMissingColumns(ctx, tx, "compact_memory_documents", []columnDef{
		{Name: "content_hash", SQL: "TEXT NOT NULL DEFAULT ''"},
		{Name: "embedding_model_id", SQL: "TEXT NOT NULL DEFAULT ''"},
		{Name: "index_build_id", SQL: "TEXT NOT NULL DEFAULT ''"},
	})
}
