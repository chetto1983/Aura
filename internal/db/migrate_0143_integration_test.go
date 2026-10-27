//go:build db_integration

package db

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var tablesDroppedBy0143 = []string{
	"benchmark_settings_overrides", "benchmark_settings_override_rows", "asset_events", "audit_logs",
}

// 0143 drops four tables and three pipeline_generation columns that nothing reads or writes.
// A table that still holds a row is not emptied: the migration stops, and the recovery its
// message names -- keep the rows, delete them, mark the tracker clean at 142 -- is what this
// test follows. The row is seeded in asset_events, the one table of the four under
// row-level security, so the refusal also proves the migrate role's count sees through it.
func TestMigrate0143DropsUnusedSchemaAndStopsOnHeldRows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0143_drop")
	migrateToVersion(t, ctx, migrateURL, admin, 142)

	asset := seed0132Asset(t, ctx, admin, "complete", "succeeded")
	if _, err := admin.Exec(ctx, `INSERT INTO aura.asset_events (asset_id, seq, to_status) VALUES ($1, 1, 'complete')`, asset); err != nil {
		t.Fatalf("seed asset event: %v", err)
	}
	err := MigrateSteps(ctx, migrateURL, 1)
	if err == nil || !strings.Contains(err.Error(), "migration 0143: rows in tables it would drop: 1 aura.asset_events") {
		t.Fatalf("migrate 0143 over a held row = %v, want the held-row refusal", err)
	}
	assertTablesExist(t, ctx, admin, tablesDroppedBy0143, true)
	assertColumnExists(t, ctx, admin, "assets", "pipeline_generation", true)

	if _, err := admin.Exec(ctx, `DELETE FROM aura.asset_events`); err != nil {
		t.Fatalf("delete held row: %v", err)
	}
	if _, err := admin.Exec(ctx, `UPDATE schema_migrations SET version = 142, dirty = false`); err != nil {
		t.Fatalf("mark tracker clean: %v", err)
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0143 up after deleting the row: %v", err)
	}
	assertTablesExist(t, ctx, admin, tablesDroppedBy0143, false)
	for _, table := range []string{"assets", "ingestion_jobs", "ingestion_events"} {
		assertColumnExists(t, ctx, admin, table, "pipeline_generation", false)
	}
	// The restated CHECK still fences the attempt and lease generations.
	for _, set := range []string{"attempt_generation = 0", "lease_generation = -1"} {
		if _, err := admin.Exec(ctx, `UPDATE aura.ingestion_jobs SET `+set+` WHERE asset_id = $1`, asset); err == nil {
			t.Errorf("ingestion_jobs admits %s after 0143", set)
		}
	}

	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0143 down: %v", err)
	}
	assertTablesExist(t, ctx, admin, tablesDroppedBy0143, true)
	for _, table := range []string{"assets", "ingestion_jobs", "ingestion_events"} {
		assertColumnExists(t, ctx, admin, table, "pipeline_generation", true)
	}
	if _, err := admin.Exec(ctx, `UPDATE aura.ingestion_jobs SET pipeline_generation = -1 WHERE asset_id = $1`, asset); err == nil {
		t.Error("0143 down did not restore the pipeline_generation floor")
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0143 up again: %v", err)
	}
}

func assertTablesExist(t *testing.T, ctx context.Context, admin *pgxpool.Pool, tables []string, want bool) {
	t.Helper()
	for _, table := range tables {
		var exists bool
		if err := admin.QueryRow(ctx, `SELECT to_regclass('aura.' || $1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatalf("read table %s: %v", table, err)
		}
		if exists != want {
			t.Errorf("table aura.%s exists = %v, want %v", table, exists, want)
		}
	}
}
