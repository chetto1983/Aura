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
	defer m.Close()
	pre, _, _ := m.Version()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return 0, fmt.Errorf("migrate up against %s: %w", redactDSN(migrateURL), err)
	}
	post, _, _ := m.Version()
	return int(post) - int(pre), nil
}

// EnsureRoles bootstraps the aura_app + aura_migrate Postgres roles using
// parametrized DO blocks. Run by `aura db migrate` BEFORE Migrate() so the
// 0001_init grants land on existing roles (golang-migrate iofs cannot do
// psql variable substitution; this is the Go-side substitute).
//
// migrateURL must point at a role with CREATE ROLE privilege (typically the
// superuser, e.g. the `aura` POSTGRES_USER set by compose). The passwords
// are passed via pgx Exec parameters so they never appear in error strings
// (T-1.05-06 mitigation — asserted by TestEnsureRoles_NoPlaintextInError).
func EnsureRoles(ctx context.Context, bootstrapURL, appPassword, migratePassword string) error {
	if bootstrapURL == "" {
		return errors.New("EnsureRoles: bootstrapURL is empty")
	}
	if appPassword == "" || migratePassword == "" {
		return errors.New("EnsureRoles: appPassword and migratePassword must be non-empty")
	}
	pool, err := pgxpool.New(ctx, bootstrapURL)
	if err != nil {
		return fmt.Errorf("bootstrap pool %s: %w", redactDSN(bootstrapURL), err)
	}
	defer pool.Close()

	// Parametrized DO blocks. PL/pgSQL accepts parameters via the FORMAT
	// statement; we use $1/$2 binding via EXECUTE to keep the password out of
	// the literal SQL text (and out of any error string Postgres echoes).
	const ensureAppRole = `
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'aura_app') THEN
        EXECUTE format('CREATE ROLE aura_app WITH LOGIN PASSWORD %L', $1);
    ELSE
        EXECUTE format('ALTER ROLE aura_app WITH LOGIN PASSWORD %L', $1);
    END IF;
END $$;
`
	const ensureMigrateRole = `
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'aura_migrate') THEN
        EXECUTE format('CREATE ROLE aura_migrate WITH LOGIN PASSWORD %L', $1);
    ELSE
        EXECUTE format('ALTER ROLE aura_migrate WITH LOGIN PASSWORD %L', $1);
    END IF;
END $$;
`
	if _, err := pool.Exec(ctx, ensureAppRole, appPassword); err != nil {
		return fmt.Errorf("ensure aura_app role: %w", redactErrorPassword(err, appPassword, migratePassword))
	}
	if _, err := pool.Exec(ctx, ensureMigrateRole, migratePassword); err != nil {
		return fmt.Errorf("ensure aura_migrate role: %w", redactErrorPassword(err, appPassword, migratePassword))
	}
	return nil
}

// redactErrorPassword scrubs known passwords from an error message before it
// is wrapped. Defense-in-depth on top of parametrized queries — if a Postgres
// error message ever echoes a literal substring of the password (e.g. via
// an unexpected upstream code path), this strips it.
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
