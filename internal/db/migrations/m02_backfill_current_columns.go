package migrations

import (
	"context"
	"database/sql"
)

func backfillCurrentColumns(ctx context.Context, tx *sql.Tx) error {
	if err := addMissingColumns(ctx, tx, "scheduled_tasks", []columnDef{
		{Name: "schedule_weekdays", SQL: "TEXT NOT NULL DEFAULT ''"},
		{Name: "schedule_every_minutes", SQL: "INTEGER NOT NULL DEFAULT 0"},
		{Name: "last_output", SQL: "TEXT NOT NULL DEFAULT ''"},
		{Name: "last_metrics_json", SQL: "TEXT NOT NULL DEFAULT ''"},
		{Name: "wake_signature", SQL: "TEXT NOT NULL DEFAULT ''"},
	}); err != nil {
		return err
	}
	if err := addMissingColumns(ctx, tx, "proposed_updates", []columnDef{
		{Name: "category", SQL: "TEXT NOT NULL DEFAULT ''"},
		{Name: "related_slugs", SQL: "TEXT NOT NULL DEFAULT ''"},
		{Name: "provenance_json", SQL: "TEXT NOT NULL DEFAULT '{}'"},
	}); err != nil {
		return err
	}
	if err := addMissingColumns(ctx, tx, "swarm_tasks", []columnDef{
		{Name: "tokens_prompt", SQL: "INTEGER NOT NULL DEFAULT 0"},
		{Name: "tokens_completion", SQL: "INTEGER NOT NULL DEFAULT 0"},
		{Name: "tokens_total", SQL: "INTEGER NOT NULL DEFAULT 0"},
	}); err != nil {
		return err
	}
	return nil
}
