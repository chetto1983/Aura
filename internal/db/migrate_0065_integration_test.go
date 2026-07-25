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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrate0065OwnerLockCleanInstallDownUpAndPrivileges(t *testing.T) {
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
	database := "aura_migrate0065_" + strings.ReplaceAll(uuid.Must(uuid.NewV7()).String(), "-", "")
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
	applied, err := Migrate(ctx, migrateURL)
	if err != nil {
		t.Fatalf("clean migration install: %v", err)
	}
	if want := shippedMigrationCount(t); applied != want {
		t.Fatalf("clean migration install applied %d, want %d", applied, want)
	}
	migratePool, err := Open(ctx, &Config{URL: migrateURL})
	if err != nil {
		t.Fatalf("open disposable migration pool: %v", err)
	}
	t.Cleanup(migratePool.Close)
	// A clean install lands on whatever the shipped head is, NOT on 0065. Hardcoding 65
	// here made the test assert that no migration had ever been added after it: it passed
	// only while 0065 was head, and 0066-0074 broke it the first time CI saw them.
	head, err := MigrationHead()
	if err != nil {
		t.Fatalf("read embedded migration head: %v", err)
	}
	assertMigration0065Version(t, ctx, migratePool, int(head))

	app, err := Open(ctx, &Config{URL: dsn("aura_app", database)})
	if err != nil {
		t.Fatalf("open disposable app pool: %v", err)
	}
	t.Cleanup(app.Close)
	var securityDefiner, fixedSearchPath, appCanExecute, publicCanExecute bool
	if err := migratePool.QueryRow(
		ctx,
		`SELECT p.prosecdef,
		        p.proconfig @> ARRAY['search_path=pg_catalog'],
		        has_function_privilege('aura_app', p.oid, 'EXECUTE'),
		        EXISTS (
		            SELECT 1
		            FROM aclexplode(
		                COALESCE(p.proacl, acldefault('f', p.proowner))
		            ) AS acl
		            WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE'
		        )
		 FROM pg_proc AS p
		 JOIN pg_namespace AS n ON n.oid=p.pronamespace
		 WHERE n.nspname='aura'
		   AND p.proname='fence_adaptive_identity'
		   AND p.proargtypes='2950'::oidvector`,
	).Scan(
		&securityDefiner,
		&fixedSearchPath,
		&appCanExecute,
		&publicCanExecute,
	); err != nil {
		t.Fatalf("read fence function privileges: %v", err)
	}
	if !securityDefiner || !fixedSearchPath || !appCanExecute || publicCanExecute {
		t.Fatalf(
			"fence function security = definer:%t search_path:%t app_execute:%t public_execute:%t",
			securityDefiner,
			fixedSearchPath,
			appCanExecute,
			publicCanExecute,
		)
	}

	owner := seedMigration0065Owner(t, ctx, app)
	assertMigration0065FenceOwnerLock(t, ctx, app, migratePool, owner, true)
	var fenced bool
	if err := app.QueryRow(
		ctx,
		`SELECT aura.fence_adaptive_identity($1)`,
		owner,
	).Scan(&fenced); err != nil {
		t.Fatalf("aura_app execute fence function: %v", err)
	}
	if !fenced {
		t.Fatal("fence existing owner returned false")
	}
	if _, err := app.Exec(
		ctx,
		`DELETE FROM aura.adaptive_identity_tombstones WHERE owner_id=$1`,
		owner,
	); err == nil {
		t.Fatal("aura_app directly deleted a tombstone")
	} else {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("direct tombstone delete error = %v, want SQLSTATE 42501", err)
		}
	}
	missingOwner := uuid.Must(uuid.NewV7())
	if err := app.QueryRow(
		ctx,
		`SELECT aura.fence_adaptive_identity($1)`,
		missingOwner,
	).Scan(&fenced); err != nil {
		t.Fatalf("fence missing owner: %v", err)
	}
	if fenced {
		t.Fatal("fence missing owner returned true")
	}

	lockOwner := seedMigration0065Owner(t, ctx, app)
	// Step down to 0064 so the boundary under test is 0065's OWN, whatever else has shipped
	// since. A bare -1 walked back the newest migration instead — with head at 0074 it
	// removed adaptive_outbox.recorded_at and asserted 0065's fence against a schema 0065
	// never described.
	if err := MigrateSteps(ctx, migrateURL, -(int(head) - 64)); err != nil {
		t.Fatalf("migrate 0065 down: %v", err)
	}
	assertMigration0065Version(t, ctx, migratePool, 64)
	assertMigration0065FenceOwnerLock(t, ctx, app, migratePool, lockOwner, false)
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0065 up: %v", err)
	}
	assertMigration0065Version(t, ctx, migratePool, 65)
	assertMigration0065FenceOwnerLock(t, ctx, app, migratePool, lockOwner, true)
}

func seedMigration0065Owner(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) uuid.UUID {
	t.Helper()
	owner := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1,$2,'user')`,
		owner,
		"migrate-0065-"+owner.String(),
	); err != nil {
		t.Fatalf("seed migration 0065 owner: %v", err)
	}
	return owner
}

func assertMigration0065Version(
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

func assertMigration0065FenceOwnerLock(
	t *testing.T,
	ctx context.Context,
	app *pgxpool.Pool,
	migrate *pgxpool.Pool,
	owner uuid.UUID,
	wantLocked bool,
) {
	t.Helper()
	fenceTx, err := app.Begin(ctx)
	if err != nil {
		t.Fatalf("begin fence transaction: %v", err)
	}
	defer rollbackMigration0065Tx(t, fenceTx)
	var fenced bool
	if err := fenceTx.QueryRow(
		ctx,
		`SELECT aura.fence_adaptive_identity($1)`,
		owner,
	).Scan(&fenced); err != nil {
		t.Fatalf("fence owner in transaction: %v", err)
	}
	if !fenced {
		t.Fatal("fence owner in transaction returned false")
	}
	contender, err := migrate.Begin(ctx)
	if err != nil {
		t.Fatalf("begin owner lock contender: %v", err)
	}
	defer rollbackMigration0065Tx(t, contender)
	_, lockErr := contender.Exec(
		ctx,
		`SELECT id FROM aura.identities WHERE id=$1 FOR KEY SHARE NOWAIT`,
		owner,
	)
	var pgErr *pgconn.PgError
	locked := errors.As(lockErr, &pgErr) && pgErr.Code == "55P03"
	if locked != wantLocked {
		t.Fatalf("owner lock blocked = %t (error %v), want %t", locked, lockErr, wantLocked)
	}
}

func rollbackMigration0065Tx(t *testing.T, tx pgx.Tx) {
	t.Helper()
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cleanupCancel()
	if err := tx.Rollback(cleanupCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Errorf("rollback migration 0065 transaction: %v", err)
	}
}
