package scheduler

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrateAddsProposedUpdateReviewColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	_, err = db.Exec(`
CREATE TABLE proposed_updates (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  chat_id         INTEGER NOT NULL,
  fact            TEXT    NOT NULL,
  action          TEXT    NOT NULL,
  target_slug     TEXT    NOT NULL DEFAULT '',
  similarity      REAL    NOT NULL DEFAULT 0,
  source_turn_ids TEXT    NOT NULL DEFAULT '',
  status          TEXT    NOT NULL DEFAULT 'pending',
  created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`)
	if err != nil {
		t.Fatalf("create legacy proposed_updates: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	cols, err := tableInfoColumns(store.DB(), "proposed_updates")
	if err != nil {
		t.Fatalf("tableInfoColumns: %v", err)
	}
	for _, col := range []string{"category", "related_slugs"} {
		if !cols[col] {
			t.Fatalf("missing migrated column %q", col)
		}
	}
}
