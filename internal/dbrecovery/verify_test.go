package dbrecovery

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyBackup_HappyPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "good.db")

	db := openMigratedDB(t, dbPath)
	// Register cleanup before any Fatalf so the DB is closed even on early exit.
	// LIFO order: this runs before t.TempDir's cleanup, which avoids Windows
	// file-lock failures when the TempDir tries to remove the SQLite WAL/SHM files.
	t.Cleanup(func() {
		db.Close()
		_ = os.Remove(dbPath + "-wal")
		_ = os.Remove(dbPath + "-shm")
	})

	if _, err := db.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('test_key', 'test_val', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Close explicitly so VerifyBackup can open its own read-only connection.
	db.Close()

	report, err := VerifyBackup(context.Background(), dbPath, map[string]int{
		"settings":        1,
		"scheduled_tasks": 0,
	})
	if err != nil {
		t.Fatalf("VerifyBackup happy path: %v", err)
	}
	if report.IntegrityStatus != "ok" {
		t.Errorf("IntegrityStatus = %q, want ok", report.IntegrityStatus)
	}
	if report.RowCounts["settings"] < 1 {
		t.Errorf("settings row count = %d, want >= 1", report.RowCounts["settings"])
	}
}

func TestVerifyBackup_TruncatedDB(t *testing.T) {
	t.Parallel()

	f, err := os.CreateTemp(t.TempDir(), "bad-*.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("this is not a valid sqlite database file")); err != nil {
		t.Fatal(err)
	}
	f.Close()

	_, err = VerifyBackup(context.Background(), f.Name(), nil)
	if err == nil {
		t.Fatal("VerifyBackup(truncated): want error, got nil")
	}
}

func TestVerifyBackup_RowCountShortfall(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "empty.db")

	db := openMigratedDB(t, dbPath)
	t.Cleanup(func() {
		db.Close()
		_ = os.Remove(dbPath + "-wal")
		_ = os.Remove(dbPath + "-shm")
	})
	db.Close()

	// scheduled_tasks is empty on a fresh DB — requesting >= 100 must fail.
	_, err := VerifyBackup(context.Background(), dbPath, map[string]int{
		"scheduled_tasks": 100,
	})
	if err == nil {
		t.Fatal("VerifyBackup(row shortfall): want error, got nil")
	}
}

func TestVerifyBackup_EmptyPath(t *testing.T) {
	_, err := VerifyBackup(context.Background(), "", nil)
	if err == nil {
		t.Fatal("VerifyBackup(empty path): want error, got nil")
	}
}
