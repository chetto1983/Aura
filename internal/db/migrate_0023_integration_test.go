//go:build db_integration

package db

import (
	"context"
	"testing"
)

func TestMigration0023IdentityRecoveryRoundTrip(t *testing.T) {
	ctx := context.Background()
	migrateURL := envOrSkip(t, "AURA_DB_MIGRATE_URL")

	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	pool, err := Open(ctx, &Config{URL: migrateURL})
	if err != nil {
		t.Fatalf("open migrate pool: %v", err)
	}
	t.Cleanup(pool.Close)

	for _, table := range []string{
		"aura.identity_recovery",
		"aura.password_reset_challenges",
		"aura.password_reset_tokens",
		"aura.identity_recovery_audit",
	} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatalf("check %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("%s missing after migrate up", table)
		}
	}

	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate down 0023: %v", err)
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate re-up 0023: %v", err)
	}
}
