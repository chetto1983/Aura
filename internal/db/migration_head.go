package db

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationVersions returns every embedded PostgreSQL up-migration version, unsorted.
func migrationVersions() ([]int64, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	versions := make([]int64, 0, len(entries)/2)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		prefix, _, ok := strings.Cut(name, "_")
		if !ok {
			return nil, fmt.Errorf("invalid migration filename %q", name)
		}
		version, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse migration version %q: %w", name, err)
		}
		versions = append(versions, version)
	}
	if len(versions) == 0 {
		return nil, errors.New("no embedded PostgreSQL migrations")
	}
	return versions, nil
}

// MigrationHead returns the highest embedded PostgreSQL up-migration version.
func MigrationHead() (int64, error) {
	versions, err := migrationVersions()
	if err != nil {
		return 0, err
	}
	head := versions[0]
	for _, v := range versions[1:] {
		if v > head {
			head = v
		}
	}
	return head, nil
}

// MigrationStepsAbove counts the embedded migrations strictly newer than version —
// which is exactly the MigrateSteps(-n) distance from head back to that version.
//
// The sequence has GAPS, so that distance is not `head - version`. Twenty-five
// migrations built an adaptive-learning plane and one later dropped it whole; all
// twenty-six were removed on 2026-08-03 once a fresh Migrate was proven to produce a
// byte-identical schema without them. golang-migrate's Steps() counts MIGRATIONS, not
// version numbers, so every caller that subtracted version numbers started overshooting
// the moment the first gap appeared, and golang-migrate reported it as an opaque
// "limit N short". Ask the embedded catalog instead of doing arithmetic on it.
func MigrationStepsAbove(version int64) (int, error) {
	versions, err := migrationVersions()
	if err != nil {
		return 0, err
	}
	steps := 0
	for _, v := range versions {
		if v > version {
			steps++
		}
	}
	return steps, nil
}

// MigrationStepDelta returns the signed MigrateSteps argument that moves a database
// from one version to another — negative to roll back, positive to roll forward.
//
// It is `stepsAbove(from) - stepsAbove(to)` because what Steps() counts is the number
// of migrations strictly between the two versions, which the gapped sequence makes
// different from `to - from`.
func MigrationStepDelta(from, to int64) (int, error) {
	aboveFrom, err := MigrationStepsAbove(from)
	if err != nil {
		return 0, err
	}
	aboveTo, err := MigrationStepsAbove(to)
	if err != nil {
		return 0, err
	}
	return aboveFrom - aboveTo, nil
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
