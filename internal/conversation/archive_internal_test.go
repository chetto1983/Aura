package conversation

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// openTestDB opens a fresh SQLite DB, applies the conversations migration, and
// registers cleanup. Used by internal tests that need to bypass the scheduler
// package to avoid an import cycle.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("openTestDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(migrationCreateConversationsTable); err != nil {
		t.Fatalf("openTestDB migrate: %v", err)
	}
	return db
}

// TestIsDuplicateError_Nil covers the nil-guard branch in isDuplicateError.
func TestIsDuplicateError_Nil(t *testing.T) {
	if isDuplicateError(nil) {
		t.Fatal("isDuplicateError(nil) should return false")
	}
}

// TestIsDuplicateError_NonDuplicate covers the false branch for a plain error.
func TestIsDuplicateError_NonDuplicate(t *testing.T) {
	if isDuplicateError(sql.ErrNoRows) {
		t.Fatal("isDuplicateError(sql.ErrNoRows) should return false")
	}
}

// TestScanTurn_RFC3339Fallback covers the RFC3339 success branch in scanTurn.
func TestScanTurn_RFC3339Fallback(t *testing.T) {
	row := &fakeScanner{
		values: []any{
			int64(1), int64(10), int64(5), int64(0),
			"user", "hello",
			sql.NullString{Valid: false}, sql.NullString{Valid: false},
			0, 0, int64(0), 0, 0,
			"2026-05-02T11:00:00Z", // RFC3339 format
		},
	}
	turn, err := scanTurn(row)
	if err != nil {
		t.Fatalf("scanTurn with RFC3339 timestamp: %v", err)
	}
	if turn.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should not be zero")
	}
	if turn.CreatedAt.Year() != 2026 {
		t.Fatalf("unexpected year: %d", turn.CreatedAt.Year())
	}
}

// TestScanTurn_BadTimestamp covers the parse-failure branch in scanTurn.
func TestScanTurn_BadTimestamp(t *testing.T) {
	row := &fakeScanner{
		values: []any{
			int64(1), int64(10), int64(5), int64(0),
			"user", "hello",
			sql.NullString{Valid: false}, sql.NullString{Valid: false},
			0, 0, int64(0), 0, 0,
			"not-a-timestamp",
		},
	}
	_, err := scanTurn(row)
	if err == nil {
		t.Fatal("scanTurn with invalid timestamp should return error")
	}
}

// TestListByChat_ScanError covers the scan-error path in ListByChat by
// inserting a row with a corrupt created_at value via raw SQL.
func TestListByChat_ScanError(t *testing.T) {
	db := openTestDB(t)
	store := &ArchiveStore{db: db}

	// Insert a row with an unparseable created_at directly.
	_, err := db.Exec(`INSERT INTO conversations
		(chat_id, user_id, turn_index, role, content, llm_calls, tool_calls_count, elapsed_ms, tokens_in, tokens_out, created_at)
		VALUES (99, 1, 0, 'user', 'hello', 0, 0, 0, 0, 0, 'BADTIME')`)
	if err != nil {
		t.Fatalf("raw insert: %v", err)
	}

	_, err = store.ListByChat(context.Background(), 99, 10)
	if err == nil {
		t.Fatal("want error from unparseable created_at, got nil")
	}
}

// fakeScanner implements turnScanner by scanning from a pre-populated slice.
type fakeScanner struct {
	values []any
}

func (f *fakeScanner) Scan(dest ...any) error {
	for i, d := range dest {
		if i >= len(f.values) {
			break
		}
		switch ptr := d.(type) {
		case *int64:
			switch v := f.values[i].(type) {
			case int64:
				*ptr = v
			case int:
				*ptr = int64(v)
			}
		case *int:
			switch v := f.values[i].(type) {
			case int:
				*ptr = v
			case int64:
				*ptr = int(v)
			}
		case *string:
			*ptr = f.values[i].(string)
		case *sql.NullString:
			*ptr = f.values[i].(sql.NullString)
		}
	}
	return nil
}
