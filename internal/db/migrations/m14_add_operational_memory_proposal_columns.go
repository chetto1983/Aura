package migrations

import (
	"context"
	"database/sql"
)

func addOperationalMemoryProposalColumns(ctx context.Context, tx *sql.Tx) error {
	return addMissingColumns(ctx, tx, "proposed_updates", []columnDef{
		{Name: "kind", SQL: "TEXT NOT NULL DEFAULT 'wiki'"},
		{Name: "signature_hash", SQL: "TEXT NOT NULL DEFAULT ''"},
	})
}
