package scheduler

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// NewTestDB opens a fresh SQLite database in t.TempDir(), applies all
// scheduler migrations, and registers a cleanup to close it. Exported so
// other packages (e.g. internal/conversation) can share the same migrated
// DB in their tests.
func NewTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("NewTestDB open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Apply all migrations (idempotent).
	if _, err := db.Exec(schemaSQL); err != nil {
		t.Fatalf("NewTestDB migrate scheduler: %v", err)
	}
	if _, err := db.Exec(conversationsSchemaSQL); err != nil {
		t.Fatalf("NewTestDB migrate conversations: %v", err)
	}
	if _, err := db.Exec(proposedUpdatesSchemaSQL); err != nil {
		t.Fatalf("NewTestDB migrate proposed_updates: %v", err)
	}
	if err := addProposedUpdateReviewColumns(db); err != nil {
		t.Fatalf("NewTestDB migrate proposed_updates review columns: %v", err)
	}
	if _, err := db.Exec(wikiIssuesSchemaSQL); err != nil {
		t.Fatalf("NewTestDB migrate wiki_issues: %v", err)
	}
	return db
}
