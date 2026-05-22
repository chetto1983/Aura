package migrations

import (
	"context"
	"database/sql"
)

func addRunEventOrigin(ctx context.Context, tx *sql.Tx) error {
	return addMissingColumns(ctx, tx, "run_events", []columnDef{
		{Name: "run_origin", SQL: "TEXT NOT NULL DEFAULT 'user'"},
	})
}
