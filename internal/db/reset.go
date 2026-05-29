package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// Reset rolls every migration Down then re-applies them Up. Dev-only — the
// `aura db reset` CLI handler guards entry with --yes and AURA_RESET_YES=1.
// migrateURL must be the aura_migrate DSN (Reset is DDL).
func Reset(ctx context.Context, migrateURL string) error {
	if migrateURL == "" {
		return errors.New(errMissingMigrateURL)
	}
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("reset source: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, migrateURL)
	if err != nil {
		return fmt.Errorf("reset new migrator to %s: %w", redactDSN(migrateURL), err)
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("reset down against %s: %w", redactDSN(migrateURL), err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("reset up against %s: %w", redactDSN(migrateURL), err)
	}
	return nil
}
