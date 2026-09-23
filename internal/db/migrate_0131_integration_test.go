//go:build db_integration

package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrate0131PIMProviderAppFreshUpDownUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0131_pim_apps")

	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("migrate fresh database to head: %v", err)
	}
	assert0131Schema(t, ctx, admin)

	steps, err := MigrationStepsAbove(130)
	if err != nil {
		t.Fatalf("MigrationStepsAbove(130): %v", err)
	}
	if err := MigrateSteps(ctx, migrateURL, -steps); err != nil {
		t.Fatalf("MigrateSteps(%d) down to pre-0131: %v", -steps, err)
	}
	if regclass(t, ctx, admin, "pim_provider_app") != nil {
		t.Fatal("aura.pim_provider_app survived the down migration")
	}
	if err := MigrateSteps(ctx, migrateURL, steps); err != nil {
		t.Fatalf("migrate 0131 back up: %v", err)
	}
	assert0131Schema(t, ctx, admin)
}

func assert0131Schema(t *testing.T, ctx context.Context, admin *pgxpool.Pool) {
	t.Helper()
	if regclass(t, ctx, admin, "pim_provider_app") == nil {
		t.Fatal("aura.pim_provider_app missing after migrate")
	}
	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"google without a secret", `INSERT INTO aura.pim_provider_app (provider, client_id) VALUES ('google', 'cid')`},
		{"microsoft with a secret", `INSERT INTO aura.pim_provider_app (provider, client_id, tenant_id, client_secret_ciphertext) VALUES ('outlook.com', 'cid', 'consumers', '\x01')`},
		{"microsoft without a tenant", `INSERT INTO aura.pim_provider_app (provider, client_id) VALUES ('microsoft365', 'cid')`},
		{"google with a tenant", `INSERT INTO aura.pim_provider_app (provider, client_id, tenant_id, client_secret_ciphertext) VALUES ('google', 'cid', 't', '\x01')`},
		{"unmanaged provider", `INSERT INTO aura.pim_provider_app (provider, client_id, tenant_id) VALUES ('imap', 'cid', 't')`},
		{"empty client id", `INSERT INTO aura.pim_provider_app (provider, client_id, tenant_id) VALUES ('outlook.com', '', 'consumers')`},
	} {
		_, err := admin.Exec(ctx, tc.sql)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Errorf("%s: err = %v, want check_violation 23514", tc.name, err)
		}
	}
	if _, err := admin.Exec(ctx, `INSERT INTO aura.pim_provider_app (provider, client_id, tenant_id) VALUES ('outlook.com', 'cid', 'consumers')`); err != nil {
		t.Fatalf("a well-formed Microsoft row was rejected: %v", err)
	}
	if _, err := admin.Exec(ctx, `DELETE FROM aura.pim_provider_app`); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}
