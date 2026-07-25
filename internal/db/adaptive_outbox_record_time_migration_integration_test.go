//go:build db_integration

package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestAdaptiveOutboxRecordTimeMigrationLiveContract(t *testing.T) {
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
	database := "aura_migrate0074_" +
		strings.ReplaceAll(uuid.Must(uuid.NewV7()).String(), "-", "")
	dsn := func(role string) string {
		return fmt.Sprintf(
			"postgres://%s:%s@%s:%s/%s?sslmode=disable",
			role, password, host, port, database,
		)
	}
	admin, err := Open(ctx, &Config{URL: fmt.Sprintf(
		"postgres://aura:%s@%s:%s/aura?sslmode=disable",
		password, host, port,
	)})
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
	freshAdmin, err := Open(ctx, &Config{URL: dsn("aura")})
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

	migrateURL := dsn("aura_migrate")
	if err := MigrateSteps(ctx, migrateURL, 73); err != nil {
		t.Fatalf("migrate through 0073: %v", err)
	}
	migrate, err := Open(ctx, &Config{URL: migrateURL})
	if err != nil {
		t.Fatalf("open migration pool: %v", err)
	}
	t.Cleanup(migrate.Close)
	app, err := Open(ctx, &Config{URL: dsn("aura_app")})
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	t.Cleanup(app.Close)

	owner := uuid.Must(uuid.NewV7())
	if _, err := app.Exec(
		ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`,
		owner,
		"migrate-0074-"+owner.String(),
	); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	historicalID := uuid.Must(uuid.NewV7())
	insertRow := func(id uuid.UUID, suppliedRecordedAt *time.Time) (time.Time, time.Time) {
		t.Helper()
		tx, err := app.Begin(ctx)
		if err != nil {
			t.Fatalf("begin app insert transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		if _, err := tx.Exec(
			ctx,
			`SELECT set_config('app.current_identity', $1, true)`,
			owner.String(),
		); err != nil {
			t.Fatalf("scope app insert: %v", err)
		}
		var recordedAt, statementAt time.Time
		if suppliedRecordedAt == nil {
			err = tx.QueryRow(ctx, `
INSERT INTO aura.adaptive_outbox (
    id, owner_id, aggregate_id, sequence, decision_id, event_kind,
    payload, payload_hash, created_at
) VALUES ($1, $2, $3, 1, $4, 'decision',
          '{"schema_version":"1.0"}'::jsonb, decode(repeat('00', 32), 'hex'),
          statement_timestamp())
RETURNING created_at, statement_timestamp()`,
				id, owner, "recorded-time-"+id.String(), uuid.Must(uuid.NewV7()),
			).Scan(&recordedAt, &statementAt)
		} else {
			err = tx.QueryRow(ctx, `
INSERT INTO aura.adaptive_outbox (
    id, owner_id, aggregate_id, sequence, decision_id, event_kind,
    payload, payload_hash, created_at, recorded_at
) VALUES ($1, $2, $3, 1, $4, 'decision',
          '{"schema_version":"1.0"}'::jsonb, decode(repeat('00', 32), 'hex'),
          statement_timestamp(), $5)
RETURNING recorded_at, statement_timestamp()`,
				id, owner, "recorded-time-"+id.String(), uuid.Must(uuid.NewV7()),
				*suppliedRecordedAt,
			).Scan(&recordedAt, &statementAt)
		}
		if err != nil {
			t.Fatalf("insert adaptive outbox row: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit app insert transaction: %v", err)
		}
		return recordedAt, statementAt
	}
	insertRow(historicalID, nil)

	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0074 up: %v", err)
	}
	var historicalRecordedAt *time.Time
	if err := migrate.QueryRow(
		ctx,
		`SELECT recorded_at FROM aura.adaptive_outbox WHERE id=$1`,
		historicalID,
	).Scan(&historicalRecordedAt); err != nil {
		t.Fatalf("read historical record time: %v", err)
	}
	if historicalRecordedAt != nil {
		t.Fatalf("historical record time = %s, want NULL", *historicalRecordedAt)
	}

	var securityDefiner, appExecute, publicExecute bool
	var config string
	if err := migrate.QueryRow(ctx, `
SELECT p.prosecdef, COALESCE(p.proconfig::text, ''),
       has_function_privilege('aura_app', p.oid, 'EXECUTE'),
       EXISTS (
           SELECT 1
           FROM aclexplode(COALESCE(p.proacl, acldefault('f', p.proowner))) AS acl
           WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE'
       )
FROM pg_proc AS p
JOIN pg_namespace AS n ON n.oid=p.pronamespace
WHERE n.nspname='aura'
  AND p.proname='set_adaptive_outbox_recorded_at'
  AND p.proargtypes=''::oidvector`).Scan(
		&securityDefiner,
		&config,
		&appExecute,
		&publicExecute,
	); err != nil {
		t.Fatalf("read record-time trigger function security: %v", err)
	}
	if securityDefiner || config != "{search_path=pg_catalog}" || appExecute || publicExecute {
		t.Fatalf(
			"unsafe record-time trigger function: definer=%t path=%q app=%t public=%t",
			securityDefiner, config, appExecute, publicExecute,
		)
	}

	callerRecordedAt := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	newID := uuid.Must(uuid.NewV7())
	recordedAt, statementAt := insertRow(newID, &callerRecordedAt)
	if !recordedAt.Equal(statementAt) || recordedAt.Equal(callerRecordedAt) {
		t.Fatalf(
			"new record time = %s, statement time = %s, caller time = %s",
			recordedAt, statementAt, callerRecordedAt,
		)
	}

	tx, err := app.Begin(ctx)
	if err != nil {
		t.Fatalf("begin immutable update transaction: %v", err)
	}
	if _, err := tx.Exec(
		ctx,
		`SELECT set_config('app.current_identity', $1, true)`,
		owner.String(),
	); err != nil {
		t.Fatalf("scope immutable update: %v", err)
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE aura.adaptive_outbox SET recorded_at=$2 WHERE id=$1`,
		newID,
		callerRecordedAt,
	)
	_ = tx.Rollback(context.Background())
	if err == nil {
		t.Fatal("aura_app changed recorded_at; want column-grant rejection")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("aura_app recorded_at update error = %v, want SQLSTATE 42501", err)
	}
	_, err = migrate.Exec(
		ctx,
		`UPDATE aura.adaptive_outbox SET recorded_at=$2 WHERE id=$1`,
		newID,
		callerRecordedAt,
	)
	if err == nil {
		t.Fatal("migration role changed recorded_at; want immutable-fact rejection")
	}
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("migration-role recorded_at update error = %v, want SQLSTATE 23514", err)
	}

	tx, err = app.Begin(ctx)
	if err != nil {
		t.Fatalf("begin operational update transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(
		ctx,
		`SELECT set_config('app.current_identity', $1, true)`,
		owner.String(),
	); err != nil {
		t.Fatalf("scope operational update: %v", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE aura.adaptive_outbox
SET status='dead_letter', dead_letter_at=statement_timestamp(),
    last_error_class='record-time-test'
WHERE id=$1`, newID); err != nil {
		t.Fatalf("aura_app updates operational status: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit operational update: %v", err)
	}

	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0074 down: %v", err)
	}
	var recordedColumnExists bool
	if err := migrate.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema='aura' AND table_name='adaptive_outbox'
      AND column_name='recorded_at'
)`).Scan(&recordedColumnExists); err != nil {
		t.Fatalf("read recorded_at column after down: %v", err)
	}
	if recordedColumnExists {
		t.Fatal("0074 down retained recorded_at")
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0074 re-up: %v", err)
	}
	if err := migrate.QueryRow(
		ctx,
		`SELECT recorded_at FROM aura.adaptive_outbox WHERE id=$1`,
		historicalID,
	).Scan(&historicalRecordedAt); err != nil {
		t.Fatalf("read historical record time after re-up: %v", err)
	}
	if historicalRecordedAt != nil {
		t.Fatalf("historical record time after re-up = %s, want NULL", *historicalRecordedAt)
	}
}
