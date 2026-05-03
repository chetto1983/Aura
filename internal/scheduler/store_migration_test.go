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

func TestMigrateAddsScheduleWeekdaysColumn(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy-scheduler.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	_, err = db.Exec(`
CREATE TABLE scheduled_tasks (
    id                     INTEGER PRIMARY KEY AUTOINCREMENT,
    name                   TEXT NOT NULL UNIQUE,
    kind                   TEXT NOT NULL,
    payload                TEXT NOT NULL DEFAULT '',
    recipient_id           TEXT NOT NULL DEFAULT '',
    schedule_kind          TEXT NOT NULL,
    schedule_at            TEXT,
    schedule_daily         TEXT,
    schedule_every_minutes INTEGER NOT NULL DEFAULT 0,
    next_run_at            TEXT NOT NULL,
    last_run_at            TEXT,
    last_error             TEXT NOT NULL DEFAULT '',
    status                 TEXT NOT NULL DEFAULT 'active',
    created_at             TEXT NOT NULL,
    updated_at             TEXT NOT NULL
);
`)
	if err != nil {
		t.Fatalf("create legacy scheduled_tasks: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	cols, err := tableInfoColumns(store.DB(), "scheduled_tasks")
	if err != nil {
		t.Fatalf("tableInfoColumns: %v", err)
	}
	if !cols["schedule_weekdays"] {
		t.Fatal("missing migrated schedule_weekdays column")
	}
}
