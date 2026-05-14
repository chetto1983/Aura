package cron

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
)

// NewTestDB opens a fresh SQLite database in t.TempDir(), applies all
// scheduler migrations, and registers a cleanup to close it. Exported so
// other packages (e.g. internal/conversation) can share the same migrated
// DB in their tests.
func NewTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := auradb.Open(dbPath)
	if err != nil {
		t.Fatalf("NewTestDB open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("NewTestDB migrate: %v", err)
	}
	return db
}
