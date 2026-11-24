//go:build db_integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 0158 adds the record of Go data upgrades that have run. The migrate role writes it; the
// application role, which 0001's default privileges would otherwise serve, cannot touch it.
func TestMigrate0158AddsTheDataUpgradeRecord(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, migrateURL, appURL := fresh0093Database(t, ctx, "aura_migrate0158_upgrades")
	migrateToVersion(t, ctx, migrateURL, admin, 157)
	if regclass(t, ctx, admin, "data_upgrades") != nil {
		t.Fatal("aura.data_upgrades exists before 0158")
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0158 up: %v", err)
	}
	assert0158Roles(t, ctx, migrateURL, appURL)

	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0158 down: %v", err)
	}
	if regclass(t, ctx, admin, "data_upgrades") != nil {
		t.Fatal("aura.data_upgrades survived 0158 down")
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0158 up again: %v", err)
	}
	assert0158Roles(t, ctx, migrateURL, appURL)
}

func assert0158Roles(t *testing.T, ctx context.Context, migrateURL, appURL string) {
	t.Helper()
	migrate := openPool(t, ctx, migrateURL)
	if _, err := migrate.Exec(ctx, `INSERT INTO aura.data_upgrades (name) VALUES ('probe')`); err != nil {
		t.Fatalf("the migrate role cannot record an upgrade: %v", err)
	}
	var doneAt time.Time
	if err := migrate.QueryRow(ctx, `SELECT done_at FROM aura.data_upgrades WHERE name = 'probe'`).Scan(&doneAt); err != nil || doneAt.IsZero() {
		t.Fatalf("done_at = %v, %v; want it stamped", doneAt, err)
	}
	if _, err := migrate.Exec(ctx, `INSERT INTO aura.data_upgrades (name) VALUES ('probe')`); err == nil {
		t.Fatal("an upgrade was recorded twice")
	}
	app := openPool(t, ctx, appURL)
	if _, err := app.Exec(ctx, `SELECT 1 FROM aura.data_upgrades`); err == nil {
		t.Fatal("the application role reads aura.data_upgrades")
	}
	if _, err := app.Exec(ctx, `INSERT INTO aura.data_upgrades (name) VALUES ('app')`); err == nil {
		t.Fatal("the application role writes aura.data_upgrades")
	}
}

func openPool(t *testing.T, ctx context.Context, dsn string) *pgxpool.Pool {
	t.Helper()
	pool, err := Open(ctx, &Config{URL: dsn})
	if err != nil {
		t.Fatalf("open %s: %v", redactDSN(dsn), err)
	}
	t.Cleanup(pool.Close)
	return pool
}
