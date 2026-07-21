package db

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MigrationHead returns the highest embedded PostgreSQL up-migration version.
func MigrationHead() (int64, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return 0, fmt.Errorf("read embedded migrations: %w", err)
	}
	var head int64
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		prefix, _, ok := strings.Cut(name, "_")
		if !ok {
			return 0, fmt.Errorf("invalid migration filename %q", name)
		}
		version, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse migration version %q: %w", name, err)
		}
		if version > head {
			head = version
		}
	}
	if head == 0 {
		return 0, errors.New("no embedded PostgreSQL migrations")
	}
	return head, nil
}

// CheckMigrationHead verifies that the database tracker is clean and exactly at
// the highest embedded migration. It never applies migrations.
func CheckMigrationHead(ctx context.Context, migrateURL string) error {
	if migrateURL == "" {
		return errors.New(errMissingMigrateURL)
	}
	pool, err := pgxpool.New(ctx, migrateURL)
	if err != nil {
		return fmt.Errorf("migration-head pool %s: %w", redactDSN(migrateURL), err)
	}
	defer pool.Close()

	rows, err := Status(ctx, pool)
	if err != nil {
		return fmt.Errorf("migration-head status: %w", err)
	}
	want, err := MigrationHead()
	if err != nil {
		return err
	}
	if len(rows) != 1 {
		return fmt.Errorf("migration tracker row count %d, want 1 at version %d", len(rows), want)
	}
	got := rows[0]
	if got.Dirty || got.Version != want {
		return fmt.Errorf("migration tracker incompatible: version=%d dirty=%t want=%d", got.Version, got.Dirty, want)
	}
	return nil
}
