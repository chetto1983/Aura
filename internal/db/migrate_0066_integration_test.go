//go:build db_integration

package db

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrate0066TypedOutcomesCleanInstallDownUpAndPrivileges(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	password := envOrSkip(t, "POSTGRES_PASSWORD")
	if err := EnsureRoles(ctx, bootstrapURL(t), password); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	database := "aura_migrate0066_" +
		strings.ReplaceAll(uuid.Must(uuid.NewV7()).String(), "-", "")
	dsn := func(role, name string) string {
		return fmt.Sprintf(
			"postgres://%s:%s@%s:%s/%s?sslmode=disable",
			role,
			password,
			host,
			port,
			name,
		)
	}
	admin, err := Open(ctx, &Config{URL: dsn("aura", "aura")})
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(admin.Close)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+database); err != nil {
		t.Fatalf("create disposable database: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(
			cleanupCtx,
			"DROP DATABASE IF EXISTS "+database+" WITH (FORCE)",
		); err != nil {
			t.Errorf("drop disposable database: %v", err)
		}
	})
	if _, err := admin.Exec(
		ctx,
		"GRANT CREATE ON DATABASE "+database+" TO aura_migrate",
	); err != nil {
		t.Fatalf("grant database create: %v", err)
	}
	freshAdmin, err := Open(ctx, &Config{URL: dsn("aura", database)})
	if err != nil {
		t.Fatalf("open disposable admin pool: %v", err)
	}
	t.Cleanup(freshAdmin.Close)
	if _, err := freshAdmin.Exec(
		ctx,
		"GRANT CREATE ON SCHEMA public TO aura_migrate",
	); err != nil {
		t.Fatalf("grant public schema create: %v", err)
	}
	migrateURL := dsn("aura_migrate", database)
	if err := MigrateSteps(ctx, migrateURL, 66); err != nil {
		t.Fatalf("clean migration install through 0066: %v", err)
	}
	migratePool, err := Open(ctx, &Config{URL: migrateURL})
	if err != nil {
		t.Fatalf("open migration pool: %v", err)
	}
	t.Cleanup(migratePool.Close)
	assertMigration0066Version(t, ctx, migratePool, 66)
	assertMigration0066FunctionPrivileges(t, ctx, migratePool)

	app, err := Open(ctx, &Config{URL: dsn("aura_app", database)})
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	t.Cleanup(app.Close)
	var valid bool
	if err := app.QueryRow(
		ctx,
		`SELECT aura.adaptive_schema2_evaluator_valid(
		    '{"kind":"deterministic","id":"checker","version":"v1",
		      "rubric_id":"binary-v1","calibration_id":"heldout-v1",
		      "provenance_sha256":
		      "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}'
		    ::jsonb
		)`,
	).Scan(&valid); err != nil {
		t.Fatalf("aura_app execute evaluator validator: %v", err)
	}
	if !valid {
		t.Fatal("valid evaluator registration was rejected")
	}
	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0066 down: %v", err)
	}
	assertMigration0066Version(t, ctx, migratePool, 65)
	var exists bool
	if err := migratePool.QueryRow(
		ctx,
		`SELECT to_regprocedure(
		    'aura.adaptive_schema2_outcome_payload_valid(jsonb,uuid)'
		) IS NOT NULL`,
	).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("0066 down retained outcome validator")
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0066 up: %v", err)
	}
	assertMigration0066Version(t, ctx, migratePool, 66)
	assertMigration0066FunctionPrivileges(t, ctx, migratePool)
}

func assertMigration0066Version(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	want int,
) {
	t.Helper()
	var version int
	var dirty bool
	if err := pool.QueryRow(
		ctx,
		`SELECT version, dirty FROM public.schema_migrations`,
	).Scan(&version, &dirty); err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if version != want || dirty {
		t.Fatalf("migration state = %d dirty:%t, want %d clean", version, dirty, want)
	}
}

func assertMigration0066FunctionPrivileges(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	var functionCount int
	var appCanExecute, publicCanExecute bool
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*),
		        bool_and(has_function_privilege('aura_app', p.oid, 'EXECUTE')),
		        bool_or(EXISTS (
		            SELECT 1
		            FROM aclexplode(
		                COALESCE(p.proacl, acldefault('f', p.proowner))
		            ) AS acl
		            WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE'
		        ))
		 FROM pg_proc AS p
		 JOIN pg_namespace AS n ON n.oid=p.pronamespace
		 WHERE n.nspname='aura'
		   AND p.proname IN (
		       'adaptive_schema2_evaluator_valid',
		       'adaptive_schema2_outcome_measures_valid',
		       'adaptive_schema2_outcome_payload_valid',
		       'adaptive_schema2_correction_payload_valid',
		       'adaptive_schema2_sourced_event_id',
		       'adaptive_schema2_outcome_row_valid',
		       'enforce_adaptive_schema2_outcome_chain'
		   )`,
	).Scan(&functionCount, &appCanExecute, &publicCanExecute); err != nil {
		t.Fatalf("read 0066 function privileges: %v", err)
	}
	if functionCount != 7 || !appCanExecute || publicCanExecute {
		t.Fatalf(
			"0066 privileges = count:%d app:%t public:%t, want 7/true/false",
			functionCount,
			appCanExecute,
			publicCanExecute,
		)
	}
}
