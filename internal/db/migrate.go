// Migrate runner for Aura — golang-migrate v4 with iofs source pointed at the
// embedded migrations/*.sql files. Roles `aura_app` and `aura_migrate` are
// created BY GO (EnsureRoles, parametrized DO blocks via pgxpool) BEFORE
// m.Up() runs, because golang-migrate iofs does NOT support psql variable
// substitution (`:'var'`). See RESEARCH §Code Examples note line 788 +
// T-1.05-06 mitigation.
//
// The literal error string in MissingURLFailsFast is LOAD-BEARING — asserted
// verbatim by TestMigrate_MissingURLFailsFast per D-07. Do not paraphrase.
package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// errMissingMigrateURL is the exact D-07 error string. Tests assert it byte-for-byte.
const errMissingMigrateURL = "AURA_DB_MIGRATE_URL required for DDL operations — see prd.md amendment #17"

// Migrate applies all pending migrations using the migrate role.
// Returns the count of newly-applied migrations (0 = "no pending").
// Empty migrateURL is a fail-fast error matching the D-07 literal.
//
// golang-migrate's schema_migrations tracker lives in `public`; the
// aura_migrate role gets CREATE on public from EnsureRoles so the tracker
// can be bootstrapped on a fresh database (Postgres 17+ default-revokes that
// grant from non-owners). All application tables live under `aura.*`.
func Migrate(ctx context.Context, migrateURL string) (int, error) {
	if migrateURL == "" {
		return 0, errors.New(errMissingMigrateURL)
	}
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return 0, fmt.Errorf("migrate source: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, migrateURL)
	if err != nil {
		return 0, fmt.Errorf("new migrator to %s: %w", redactDSN(migrateURL), err)
	}
	defer func() { _, _ = m.Close() }()
	applied, err := migrateAllCountingSteps(m)
	if err != nil {
		return 0, fmt.Errorf("migrate up against %s: %w", redactDSN(migrateURL), err)
	}
	return applied, nil
}

type migrationStepper interface {
	Steps(int) error
}

func migrateAllCountingSteps(m migrationStepper) (int, error) {
	applied := 0
	for {
		if err := m.Steps(1); err != nil {
			if errors.Is(err, migrate.ErrNoChange) || errors.Is(err, os.ErrNotExist) {
				return applied, nil
			}
			return applied, err
		}
		applied++
	}
}

// EnsureRoles bootstraps the aura_app + aura_migrate Postgres roles AND the
// aura schema (owner aura_migrate). Run by `aura db migrate` BEFORE Migrate()
// so the 0001_init grants land on existing roles AND so golang-migrate's
// schema_migrations tracking table has a home inside `aura.*` (Postgres 17
// revokes CREATE on `public` from non-owners; without aura ownership the
// migrate role can't even create the tracker table).
//
// bootstrapURL must point at a role with CREATE ROLE + CREATE SCHEMA privilege
// (typically the POSTGRES_USER superuser set by compose). The single
// `password` is applied to both aura_app and aura_migrate for local-dev
// ergonomics; production environments use AURA_DB_URL / AURA_DB_MIGRATE_URL
// overrides with per-role credentials and skip EnsureRoles entirely.
//
// Passwords are inlined as SQL literals (Postgres rejects bound params in
// CREATE/ALTER ROLE PASSWORD clauses) and immediately scrubbed from every
// error wrap via redactErrorPassword — T-1.05-06 defense-in-depth, asserted
// by TestEnsureRoles_NoPlaintextInError.
func EnsureRoles(ctx context.Context, bootstrapURL, password string) error {
	if bootstrapURL == "" {
		return errors.New("EnsureRoles: bootstrapURL is empty")
	}
	if password == "" {
		return errors.New("EnsureRoles: password must be non-empty")
	}
	pool, err := pgxpool.New(ctx, bootstrapURL)
	if err != nil {
		return fmt.Errorf("bootstrap pool %s: %w", redactDSN(bootstrapURL), err)
	}
	defer pool.Close()

	// Postgres rejects bound parameters in CREATE/ALTER ROLE PASSWORD clauses,
	// so the password must be inlined as a SQL literal. We escape it via the
	// standard PL/pgSQL `'` → `''` rule and immediately scrub it from any
	// error wrap (redactErrorPassword) so Postgres echo paths can't leak it.
	// Role names are hardcoded constants — no identifier-injection surface.
	const checkRoleSQL = `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = $1)`
	escapedPwd := strings.ReplaceAll(password, "'", "''")
	for _, role := range []string{"aura_app", "aura_migrate"} {
		var exists bool
		if err := pool.QueryRow(ctx, checkRoleSQL, role).Scan(&exists); err != nil {
			return fmt.Errorf("check role %s: %w", role, redactErrorPassword(err, password))
		}
		qualifier := "CREATE"
		if exists {
			qualifier = "ALTER"
		}
		stmt := fmt.Sprintf("%s ROLE %s WITH LOGIN PASSWORD '%s'", qualifier, role, escapedPwd)
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("ensure role %s: %w", role, redactErrorPassword(err, password))
		}
	}
	// Database-level CREATE for aura_migrate — required by 0001_init's
	// `CREATE SCHEMA IF NOT EXISTS aura` (Postgres still checks the privilege
	// even when the schema already exists).
	dbname, err := databaseNameFromDSN(bootstrapURL)
	if err != nil {
		return fmt.Errorf("grant CREATE on database: %w", redactErrorPassword(err, password))
	}
	if _, err := pool.Exec(ctx, grantCreateDatabaseSQL(dbname)); err != nil {
		return fmt.Errorf("grant CREATE on database %s: %w", dbname, redactErrorPassword(err, password))
	}
	// Pre-create the aura schema with aura_migrate as owner. 0001_init's
	// `CREATE SCHEMA IF NOT EXISTS aura` becomes a documented no-op once this
	// bootstrap path has run; 0001_init still owns the schema-level grants
	// for aura_app.
	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS aura AUTHORIZATION aura_migrate"); err != nil {
		return fmt.Errorf("ensure schema aura: %w", redactErrorPassword(err, password))
	}
	// golang-migrate's schema_migrations tracker lives in `public` by default
	// (the lib/pq-based driver does not support a custom tracker schema).
	// Postgres 17+ default-revokes CREATE on `public` from non-owners, so we
	// explicitly re-grant it to aura_migrate. The tracker is metadata, not
	// domain data; aura_app is NOT granted CREATE on public — role separation
	// stays intact. `aura db status` reads the tracker via the migrate role.
	if _, err := pool.Exec(ctx, "GRANT CREATE ON SCHEMA public TO aura_migrate"); err != nil {
		return fmt.Errorf("grant CREATE on public to aura_migrate: %w", redactErrorPassword(err, password))
	}
	return nil
}

// redactErrorPassword scrubs known passwords from an error message before it
// is wrapped. Defense-in-depth on top of parametrized queries — if a Postgres
// error message ever echoes a literal substring of the password (e.g. via
// an unexpected upstream code path), this strips it.
func databaseNameFromDSN(dsn string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	dbname := strings.TrimPrefix(u.Path, "/")
	if dbname == "" {
		return "", errors.New("database name is empty")
	}
	return dbname, nil
}

func grantCreateDatabaseSQL(dbname string) string {
	return "GRANT CREATE ON DATABASE " + quoteIdent(dbname) + " TO aura_migrate"
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func redactErrorPassword(err error, passwords ...string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	for _, p := range passwords {
		if p == "" {
			continue
		}
		msg = strings.ReplaceAll(msg, p, "***")
	}
	if msg == err.Error() {
		return err
	}
	return errors.New(msg)
}
