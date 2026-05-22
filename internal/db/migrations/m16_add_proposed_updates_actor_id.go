package migrations

import (
	"context"
	"database/sql"
)

func addProposedUpdatesActorID(ctx context.Context, tx *sql.Tx) error {
	return addMissingColumns(ctx, tx, "proposed_updates", []columnDef{
		{Name: "actor_id", SQL: "TEXT NOT NULL DEFAULT ''"},
	})
}
