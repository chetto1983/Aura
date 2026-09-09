//go:build db_integration

// Integration tests for internal/db. Requires a running Postgres container:
//   make db-up
//   AURA_DB_URL + AURA_DB_MIGRATE_URL set in env
//   POSTGRES_PASSWORD set for EnsureRoles bootstrap.
//
// Run via:
//   go test -tags db_integration -race ./internal/db -count=1

package db

import (
	"context"
	"errors"
	"fmt"
	"github.com/chetto1983/aura/internal/dbtest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.uber.org/goleak"
)

// seededOperatorIdentity is the `local` operator seeded by migration 0004.
const seededOperatorIdentity = "00000000-0000-0000-0000-000000000001"

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// envOrSkip returns the env var value, or skips the test locally / fails it under
// CI when the var is unset. The fail-loud-under-CI branch is deliberate: a skipped
// integration test must never pass as green in the pipeline (see ci.yml — the
// integration job exports AURA_DB_URL/AURA_DB_MIGRATE_URL so this never fires there).
func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("integration test requires %s, but it is unset under CI — "+
				"a skipped integration test must not pass as green; wire it in ci.yml", key)
		}
		t.Skipf("integration test requires %s; set it and re-run (e.g. via .env + make db-up)", key)
	}
	return v
}

// bootstrapURL returns the superuser DSN for role/database creation.
// Conventionally `postgres://aura:${POSTGRES_PASSWORD}@127.0.0.1:5432/aura`.
func bootstrapURL(t *testing.T) string {
	t.Helper()
	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	return fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", pwd, host, port)
}

func TestEnsureRoles_CreatesBothRoles(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := EnsureRoles(ctx, bootstrapURL(t), os.Getenv("POSTGRES_PASSWORD")); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	migrateURL := dbtest.MigrateURL(t, envOrSkip(t, "AURA_DB_MIGRATE_URL"))

	// Roles must exist before m.Up runs.
	if err := EnsureRoles(ctx, bootstrapURL(t), os.Getenv("POSTGRES_PASSWORD")); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}

	n1, err := Migrate(ctx, migrateURL)
	if err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	t.Logf("first migrate applied %d migrations", n1)

	n2, err := Migrate(ctx, migrateURL)
	if err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if n2 != 0 {
		t.Errorf("second Migrate: want 0 newly-applied (idempotent), got %d", n2)
	}
}

// TestMigrate_Phase4_AppliesAndSeeds proves the Phase-4 substrate: starting from a
// fresh database, every embedded up migration applies; a re-run applies zero
// (idempotent, including the CONCURRENTLY index in 0006); and 0004's seed lands
// exactly one (local/system) identity with the fixed UUID plus one (...001, '*')
// capability grant. Uses a throwaway database so the shared `aura` DB is untouched.
func TestMigrate_Phase4_AppliesAndSeeds(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}

	// Roles live cluster-wide; ensure they exist before any migrate.
	if err := EnsureRoles(ctx, bootstrapURL(t), pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}

	const freshDB = "aura_phase4_migrate_drill"
	dsn := func(role, db string) string {
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", role, pwd, host, port, db)
	}

	admin, err := Open(ctx, &Config{URL: dsn("aura", "aura")})
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+freshDB+" WITH (FORCE)"); err != nil {
		t.Fatalf("pre-drop fresh db: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+freshDB); err != nil {
		t.Fatalf("create fresh db: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+freshDB+" WITH (FORCE)")
	})

	// aura_migrate needs CREATE on the fresh DB + its public schema for the
	// golang-migrate schema_migrations tracker (Postgres 17+ default-revokes CREATE
	// on public from non-owners; mirrors EnsureRoles' grants on the primary DB). The
	// public-schema grant must run on a connection INTO the fresh DB, not the admin
	// pool (which is connected to `aura`).
	if _, err := admin.Exec(ctx, "GRANT CREATE ON DATABASE "+freshDB+" TO aura_migrate"); err != nil {
		t.Fatalf("grant create on fresh db: %v", err)
	}
	freshAdmin, err := Open(ctx, &Config{URL: dsn("aura", freshDB)})
	if err != nil {
		t.Fatalf("open fresh-db admin pool: %v", err)
	}
	defer freshAdmin.Close()
	if _, err := freshAdmin.Exec(ctx, "GRANT CREATE ON SCHEMA public TO aura_migrate"); err != nil {
		t.Fatalf("grant create on public schema of fresh db: %v", err)
	}

	migrateURL := dsn("aura_migrate", freshDB)

	n1, err := Migrate(ctx, migrateURL)
	if err != nil {
		t.Fatalf("first Migrate on fresh db: %v", err)
	}
	shippedMigrations := shippedMigrationCount(t)
	if n1 != shippedMigrations {
		t.Errorf("first Migrate on fresh db: want %d applied (all embedded *.up.sql migrations), got %d", shippedMigrations, n1)
	}

	n2, err := Migrate(ctx, migrateURL)
	if err != nil {
		t.Fatalf("second Migrate on fresh db: %v", err)
	}
	if n2 != 0 {
		t.Errorf("second Migrate on fresh db: want 0 (idempotent incl. CONCURRENTLY 0006), got %d", n2)
	}

	app, err := Open(ctx, &Config{URL: dsn("aura_app", freshDB)})
	if err != nil {
		t.Fatalf("open app pool on fresh db: %v", err)
	}
	defer app.Close()

	var idCount int
	if err := app.QueryRow(ctx,
		"SELECT count(*) FROM aura.identities WHERE id = '"+seededOperatorIdentity+"' AND name = 'local' AND kind = 'system'",
	).Scan(&idCount); err != nil {
		t.Fatalf("count seeded identity: %v", err)
	}
	if idCount != 1 {
		t.Errorf("seeded `local`/system identity: want exactly 1 row, got %d", idCount)
	}

	// This asserts a SEEDING fact ("what did the migrations leave the operator holding?"),
	// so it reads with app.current_identity bound to the seeded identity.
	// aura.capability_grants is fail-closed as of migration 0087: on a bare pool connection
	// the rows are correctly invisible, and counting 0 there would say nothing about what
	// was seeded.
	//
	// Migration 0004 seeded a single literal '*' row here. Migration 0121 retired the
	// wildcard: it rewrites every '*' row — the seeded operator's included — into the six
	// explicit names, so a migrate-to-head database has no '*' row at all. The names are
	// spelled out rather than imported from internal/identity, which imports internal/db
	// and would close a cycle; scripts/check_capability_declaration.sh (RBAC-02) excludes
	// _test.go files for exactly this case.
	wantCaps := []string{
		"identity.create", "identity.delete",
		"agent.run", "governance.read", "governance.write", "share.public",
	}

	var gotCaps []string
	var wildcardCount int
	if err := WithIdentityTxRaw(ctx, app, seededOperatorIdentity, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			"SELECT count(*) FROM aura.capability_grants WHERE capability = '*'",
		).Scan(&wildcardCount); err != nil {
			return err
		}
		rows, err := tx.Query(ctx,
			"SELECT capability FROM aura.capability_grants WHERE identity_id = $1::uuid ORDER BY capability",
			seededOperatorIdentity,
		)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				return err
			}
			gotCaps = append(gotCaps, c)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("read seeded capability grants: %v", err)
	}

	// The whole point of 0121: not "the operator gained six names" but "the wildcard is
	// gone", so a capability added later is granted to nobody until someone grants it.
	if wildcardCount != 0 {
		t.Errorf("wildcard grants after 0121: want 0 rows repo-wide, got %d", wildcardCount)
	}

	slices.Sort(wantCaps)
	if !slices.Equal(gotCaps, wantCaps) {
		t.Errorf("seeded operator grants: want %v, got %v", wantCaps, gotCaps)
	}

	// Role separation on a Phase-4 table: aura_app must be denied DDL.
	if _, err := app.Exec(ctx, "DROP TABLE aura.conversations"); err == nil {
		t.Error("aura_app: DROP TABLE aura.conversations unexpectedly succeeded — role separation broken")
	} else {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Errorf("aura_app DROP: want SQLSTATE 42501 (insufficient_privilege), got %v", err)
		}
	}
}

func shippedMigrationCount(t *testing.T) int {
	t.Helper()
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	n := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".up.sql") {
			n++
		}
	}
	if n == 0 {
		t.Fatal("read embedded migrations: found no *.up.sql files")
	}
	return n
}

func TestPing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg := &Config{URL: envOrSkip(t, "AURA_DB_URL")}
	pool, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer pool.Close()

	latency, err := Ping(ctx, pool)
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if latency <= 0 {
		t.Errorf("Ping latency: want > 0, got %s", latency)
	}
	t.Logf("ping latency: %s", latency)
}

func TestOpen_PoolTuning_AppliedAndDefaulted(t *testing.T) {
	// Asserts that Open's pool-tuning block actually takes effect on the live
	// pool — both the explicit branch (cfg.* > 0) and the default branch
	// (cfg.* == 0 falls back to the package defaults). Without inspecting
	// pool.Config() here, the assignments are executed but never verified, so a
	// mutant that drops them (or flips `> 0` to `>= 0`) survives despite 90%
	// line coverage.
	base := envOrSkip(t, "AURA_DB_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Run("explicit", func(t *testing.T) {
		pool, err := Open(ctx, &Config{URL: base, MaxConns: 7, MinConns: 3, MaxConnIdleTime: 15 * time.Second})
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer pool.Close()
		pc := pool.Config()
		if pc.MaxConns != 7 {
			t.Errorf("MaxConns: want 7 (explicit), got %d", pc.MaxConns)
		}
		if pc.MinConns != 3 {
			t.Errorf("MinConns: want 3 (explicit), got %d", pc.MinConns)
		}
		if pc.MaxConnIdleTime != 15*time.Second {
			t.Errorf("MaxConnIdleTime: want 15s (explicit), got %s", pc.MaxConnIdleTime)
		}
	})

	t.Run("defaulted", func(t *testing.T) {
		// Zero fields must resolve to the package defaults, NOT pgx's own
		// (MaxConns=NumCPU, MinConns=0, MaxConnIdleTime=30m) — which is what a
		// dropped assignment or a `>= 0` boundary flip would leave behind.
		pool, err := Open(ctx, &Config{URL: base})
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer pool.Close()
		pc := pool.Config()
		if pc.MaxConns != defaultMaxConns {
			t.Errorf("MaxConns: want default %d, got %d", defaultMaxConns, pc.MaxConns)
		}
		if pc.MinConns != defaultMinConns {
			t.Errorf("MinConns: want default %d, got %d", defaultMinConns, pc.MinConns)
		}
		if pc.MaxConnIdleTime != defaultMaxConnIdleTime {
			t.Errorf("MaxConnIdleTime: want default %s, got %s", defaultMaxConnIdleTime, pc.MaxConnIdleTime)
		}
	})
}

func TestRoleSeparation_AppDenied(t *testing.T) {
	// T-1.05-02 mitigation. Connect as aura_app, attempt TRUNCATE + DROP on
	// aura.knowledge_migrations; both must return "permission denied".
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	migrateURL := dbtest.MigrateURL(t, envOrSkip(t, "AURA_DB_MIGRATE_URL"))
	// Ensure roles + migrations are applied first.
	if err := EnsureRoles(ctx, bootstrapURL(t), os.Getenv("POSTGRES_PASSWORD")); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	cfg := &Config{URL: envOrSkip(t, "AURA_DB_URL")} // role aura_app
	pool, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("Open as aura_app: %v", err)
	}
	defer pool.Close()

	// Every statement here must name an object that still EXISTS, or the test asserts
	// nothing: a missing relation raises 42P01, not 42501, and 42P01 tells you only that
	// you deleted a table. Two of these named aura.knowledge_migrations, which migration
	// 0084 dropped when the Cypher ledger was retired — so this gate had been reporting
	// a dropped table as a broken privilege since then.
	denied := []string{
		"TRUNCATE aura.conversations",
		"DROP TABLE aura.conversations",
		"CREATE TABLE aura.t_role_test (id int)",
	}
	// The security property is "aura_app is denied DDL", not a specific message.
	// Postgres reports privilege failures with SQLSTATE 42501 (insufficient_privilege)
	// regardless of whether the wording is "permission denied for ..." (TRUNCATE /
	// CREATE) or "must be owner of table ..." (DROP). Asserting the SQLSTATE is both
	// the actual property under test and stable across operations and PG versions.
	const insufficientPrivilege = "42501"
	for _, stmt := range denied {
		_, err := pool.Exec(ctx, stmt)
		if err == nil {
			t.Errorf("aura_app: %q unexpectedly succeeded — role separation broken", stmt)
			continue
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != insufficientPrivilege {
			t.Errorf("aura_app: %q error = %q, want SQLSTATE %s (insufficient_privilege)",
				stmt, err.Error(), insufficientPrivilege)
		}
	}
}

func TestEnsureRoles_NonPrivilegedBootstrapDenied(t *testing.T) {
	// Driving EnsureRoles with the aura_app DSN (no CREATEROLE) must surface a
	// privilege error from the ALTER/CREATE ROLE Exec, with the password redacted.
	// Passing the shared password keeps aura_app's own credential unchanged.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	appURL := envOrSkip(t, "AURA_DB_URL")

	err := EnsureRoles(ctx, appURL, pwd)
	if err == nil {
		t.Fatal("EnsureRoles via aura_app: want privilege error, got nil")
	}
	if strings.Contains(err.Error(), pwd) {
		t.Errorf("EnsureRoles error leaked password: %q", err.Error())
	}
}

func TestReset_DownUpRoundTrip(t *testing.T) {
	// Reset rolls every migration Down then back Up. A follow-up Migrate must be
	// a no-op (0 newly-applied), proving the schema was fully re-materialized.
	// Relies on 0001_init.down.sql NOT dropping roles (role lifecycle is owned by
	// EnsureRoles) so the Up's GRANTs land on the still-present roles.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	migrateURL := dbtest.MigrateURL(t, envOrSkip(t, "AURA_DB_MIGRATE_URL"))
	if err := EnsureRoles(ctx, bootstrapURL(t), os.Getenv("POSTGRES_PASSWORD")); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("pre-Reset Migrate: %v", err)
	}

	if err := Reset(ctx, migrateURL); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	n, err := Migrate(ctx, migrateURL)
	if err != nil {
		t.Fatalf("post-Reset Migrate: %v", err)
	}
	if n != 0 {
		t.Errorf("post-Reset Migrate: want 0 pending (schema fully re-applied), got %d", n)
	}
}
