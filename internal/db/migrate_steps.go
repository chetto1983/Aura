//go:build db_integration

package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// MigrateSteps applies exactly n migration steps using the migrate role: n>0
// migrates up, n<0 migrates down (e.g. -1 reverses the most-recent migration).
// It is the reversibility seam the schema round-trip tests use to assert a
// migration's down + re-up is clean. migrateURL must be the aura_migrate DSN
// (DDL). ErrNoChange is swallowed (already at the target version is not an error).
func MigrateSteps(ctx context.Context, migrateURL string, n int) error {
	if migrateURL == "" {
		return errors.New(errMissingMigrateURL)
	}
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("migrate-steps source: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, migrateURL)
	if err != nil {
		return fmt.Errorf("migrate-steps new migrator to %s: %w", redactDSN(migrateURL), err)
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Steps(n); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate-steps %d against %s: %w", n, redactDSN(migrateURL), err)
	}
	return nil
}
