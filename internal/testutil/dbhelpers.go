package testutil

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	auradb "github.com/aura/aura/internal/db"
)

// TempDBPath returns a unique temp-file path for a test SQLite DB.
func TempDBPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.db")
}

// OpenTestDB opens a SQLite test DB via auradb.Open and registers cleanup.
// Pass nil for migrateFunc to skip migrations (e.g. when testing migrations
// themselves). Pass migrations.Run to apply the full schema.
func OpenTestDB(t *testing.T, migrateFunc func(context.Context, *sql.DB) error) *sql.DB {
	t.Helper()
	db, err := auradb.Open(TempDBPath(t))
	if err != nil {
		t.Fatalf("OpenTestDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if migrateFunc != nil {
		if err := migrateFunc(context.Background(), db); err != nil {
			t.Fatalf("OpenTestDB migrate: %v", err)
		}
	}
	return db
}
