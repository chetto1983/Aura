package migrations

import (
	"context"
	"database/sql"
)

func addTokenJuiceRunsColumns(ctx context.Context, tx *sql.Tx) error {
	return addMissingColumns(ctx, tx, "runs", []columnDef{
		{Name: "tokenjuice_bytes_saved", SQL: "INTEGER NOT NULL DEFAULT 0"},
		{Name: "tokenjuice_compactions_applied", SQL: "INTEGER NOT NULL DEFAULT 0"},
	})
}
