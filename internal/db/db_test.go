package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenRejectsEmptyPath(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"", "   ", "\t\n"} {
		t.Run("path="+path, func(t *testing.T) {
			if _, err := Open(path); err == nil {
				t.Fatalf("Open(%q) error = nil, want error", path)
			}
		})
	}
}

func TestOpenFreshDatabaseUsesWALJournalMode(t *testing.T) {
	t.Parallel()

	db := openTempDB(t)

	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if strings.ToLower(mode) != "wal" {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
}

func TestOpenFreshDatabaseSetsBusyTimeout(t *testing.T) {
	t.Parallel()

	db := openTempDB(t)

	var timeout int
	if err := db.QueryRow("PRAGMA busy_timeout").Scan(&timeout); err != nil {
		t.Fatalf("query busy_timeout: %v", err)
	}
	if timeout != 5000 {
		t.Fatalf("busy_timeout = %d, want 5000", timeout)
	}
}

func TestOpenFreshDatabaseEnablesForeignKeys(t *testing.T) {
	t.Parallel()

	db := openTempDB(t)

	var enabled int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&enabled); err != nil {
		t.Fatalf("query foreign_keys: %v", err)
	}
	if enabled != 1 {
		t.Fatalf("foreign_keys = %d, want 1", enabled)
	}
}

func TestOpenAppliesConnectionPragmasToNewPooledConnections(t *testing.T) {
	t.Parallel()

	db := openTempDB(t)
	db.SetMaxOpenConns(2)

	ctx := context.Background()
	conn1, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("get first connection: %v", err)
	}
	defer conn1.Close()

	conn2, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("get second connection: %v", err)
	}
	defer conn2.Close()

	for name, conn := range map[string]*sql.Conn{
		"first":  conn1,
		"second": conn2,
	} {
		var foreignKeys int
		if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			t.Fatalf("%s connection query foreign_keys: %v", name, err)
		}
		if foreignKeys != 1 {
			t.Fatalf("%s connection foreign_keys = %d, want 1", name, foreignKeys)
		}

		var busyTimeout int
		if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
			t.Fatalf("%s connection query busy_timeout: %v", name, err)
		}
		if busyTimeout != 5000 {
			t.Fatalf("%s connection busy_timeout = %d, want 5000", name, busyTimeout)
		}
	}
}

func TestOpenAllowsCreateInsertSelectRoundTrip(t *testing.T) {
	t.Parallel()

	db := openTempDB(t)

	if _, err := db.Exec("CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT NOT NULL)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.Exec("INSERT INTO notes (body) VALUES (?)", "hello"); err != nil {
		t.Fatalf("insert note: %v", err)
	}

	var body string
	if err := db.QueryRow("SELECT body FROM notes WHERE id = ?", 1).Scan(&body); err != nil {
		t.Fatalf("select note: %v", err)
	}
	if body != "hello" {
		t.Fatalf("body = %q, want hello", body)
	}
}

func openTempDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "aura.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close temp db: %v", err)
		}
	})
	return db
}
