package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Migration describes a single database schema change.
type Migration struct {
	Version int
	Name    string
	Up      func(context.Context, *sql.Tx) error
}

var registered = []Migration{
	{Version: 1, Name: "create_current_schema", Up: createCurrentSchema},
}

const currentSchemaSQL = `
CREATE TABLE IF NOT EXISTS settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
`

// Registered returns the migrations known to the runner.
func Registered() []Migration {
	out := make([]Migration, len(registered))
	copy(out, registered)
	return out
}

// Run applies all registered migrations that have not yet been recorded.
func Run(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("migrations: nil db")
	}
	if err := validateRegistered(registered); err != nil {
		return err
	}
	if err := ensureMigrationTable(ctx, db); err != nil {
		return err
	}
	applied, err := appliedMap(ctx, db)
	if err != nil {
		return err
	}
	for _, migration := range registered {
		if applied[migration.Version] {
			continue
		}
		if err := runOne(ctx, db, migration); err != nil {
			return err
		}
	}
	return nil
}

func validateRegistered(migrations []Migration) error {
	seen := make(map[int]struct{}, len(migrations))
	lastVersion := 0
	for i, migration := range migrations {
		if migration.Version <= 0 {
			return fmt.Errorf("migrations: migration %d has non-positive version %d", i, migration.Version)
		}
		if migration.Name == "" {
			return fmt.Errorf("migrations: migration %d has empty name", migration.Version)
		}
		if migration.Up == nil {
			return fmt.Errorf("migrations: migration %d has nil Up", migration.Version)
		}
		if _, ok := seen[migration.Version]; ok {
			return fmt.Errorf("migrations: duplicate version %d", migration.Version)
		}
		if migration.Version <= lastVersion {
			return fmt.Errorf("migrations: versions must be strictly increasing")
		}
		seen[migration.Version] = struct{}{}
		lastVersion = migration.Version
	}
	return nil
}

func ensureMigrationTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  applied_at TEXT NOT NULL
);`)
	if err != nil {
		return fmt.Errorf("migrations: ensure schema_migrations: %w", err)
	}
	return nil
}

func appliedMap(ctx context.Context, db *sql.DB) (map[int]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("migrations: read applied versions: %w", err)
	}
	defer rows.Close()

	applied := map[int]bool{}
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("migrations: scan applied version: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("migrations: iterate applied versions: %w", err)
	}
	return applied, nil
}

func runOne(ctx context.Context, db *sql.DB, migration Migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migrations: begin migration %d: %w", migration.Version, err)
	}
	defer tx.Rollback()

	if err := migration.Up(ctx, tx); err != nil {
		return fmt.Errorf("migrations: apply migration %d %q: %w", migration.Version, migration.Name, err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		migration.Version,
		migration.Name,
		time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("migrations: record migration %d: %w", migration.Version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrations: commit migration %d: %w", migration.Version, err)
	}
	return nil
}

func createCurrentSchema(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, currentSchemaSQL); err != nil {
		return fmt.Errorf("migrations: create current schema: %w", err)
	}
	return nil
}
