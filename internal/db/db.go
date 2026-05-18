// Package db provides Aura's shared production SQLite open path.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const sqliteJournalModeEnv = "AURA_SQLITE_JOURNAL_MODE"

var fixedPragmas = []string{
	"busy_timeout=10000",
	"foreign_keys=ON",
	"synchronous=NORMAL",
	"cache_size=-20000",
	"temp_store=MEMORY",
}

// Open opens a SQLite database configured with Aura's production PRAGMAs.
func Open(path string) (*sql.DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("db path is required")
	}

	db, err := sql.Open("sqlite", withPragmas(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}

	return db, nil
}

// OpenReadOnly opens a SQLite database for repair/recovery reads. It disables
// foreign key checks and marks the connection query-only so recovery tooling can
// inspect damaged local databases without mutating them.
func OpenReadOnly(path string) (*sql.DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("db path is required")
	}

	values := url.Values{}
	values.Add("_pragma", "busy_timeout=5000")
	values.Add("_pragma", "foreign_keys=OFF")
	values.Add("_pragma", "query_only=ON")
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	db, err := sql.Open("sqlite", path+separator+values.Encode())
	if err != nil {
		return nil, fmt.Errorf("open sqlite database read-only: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite database read-only: %w", err)
	}
	return db, nil
}

// CheckIntegrity runs SQLite's integrity check on the shared database pool.
func CheckIntegrity(ctx context.Context, db *sql.DB) (string, error) {
	if db == nil {
		return "", errors.New("db is required")
	}
	var status string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&status); err != nil {
		return "", fmt.Errorf("sqlite integrity check: %w", err)
	}
	return status, nil
}

// JournalMode reports the active SQLite journal mode for health checks and
// Docker diagnostics.
func JournalMode(ctx context.Context, db *sql.DB) (string, error) {
	if db == nil {
		return "", errors.New("db is required")
	}
	var mode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return "", fmt.Errorf("sqlite journal mode: %w", err)
	}
	return strings.ToLower(strings.TrimSpace(mode)), nil
}

// Snapshot writes a transactionally consistent copy of sourcePath to destPath.
// It is intended for backups of live databases; callers should archive the
// snapshot rather than copying aura.db, aura.db-wal, and aura.db-shm directly.
//
// When called against a live database that is already opened by Aura's main
// pool, prefer SnapshotFromPool which reuses the existing connection and
// avoids the dual-pool SQLITE_CANTOPEN (error 14) that occurs in DELETE-mode
// when two independent sql.DB pools contend on the rollback journal lock.
func Snapshot(ctx context.Context, sourcePath, destPath string) error {
	if strings.TrimSpace(sourcePath) == "" {
		return errors.New("source path is required")
	}
	if strings.TrimSpace(destPath) == "" {
		return errors.New("destination path is required")
	}
	sourcePath = filepath.Clean(sourcePath)
	destPath = filepath.Clean(destPath)
	if samePath(sourcePath, destPath) {
		return errors.New("snapshot destination must differ from source")
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create snapshot directory: %w", err)
	}
	if err := removeSQLiteFiles(destPath); err != nil {
		return err
	}

	db, err := openForSnapshot(sourcePath)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "VACUUM INTO "+quoteSQLiteString(destPath)); err != nil {
		_ = db.Close()
		_ = removeSQLiteFiles(destPath)
		return fmt.Errorf("sqlite snapshot: %w", err)
	}
	if err := db.Close(); err != nil {
		_ = removeSQLiteFiles(destPath)
		return fmt.Errorf("close snapshot source: %w", err)
	}
	return verifySnapshotAt(ctx, destPath)
}

// SnapshotFromPool writes a transactionally consistent copy of the database
// backing src into destPath, reusing src's existing connection instead of
// opening a second pool. This is the safe path for live backups while Aura
// is running: opening a second sql.DB pool on the same file fails with
// SQLITE_CANTOPEN (error 14) in DELETE-mode rollback-journal contention.
//
// src is not closed by this function — caller retains ownership.
func SnapshotFromPool(ctx context.Context, src *sql.DB, destPath string) error {
	if src == nil {
		return errors.New("source db pool is required")
	}
	if strings.TrimSpace(destPath) == "" {
		return errors.New("destination path is required")
	}
	destPath = filepath.Clean(destPath)
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create snapshot directory: %w", err)
	}
	if err := removeSQLiteFiles(destPath); err != nil {
		return err
	}
	if _, err := src.ExecContext(ctx, "VACUUM INTO "+quoteSQLiteString(destPath)); err != nil {
		_ = removeSQLiteFiles(destPath)
		return fmt.Errorf("sqlite snapshot: %w", err)
	}
	return verifySnapshotAt(ctx, destPath)
}

// verifySnapshotAt opens destPath read-only and runs an integrity check.
// Used by both Snapshot and SnapshotFromPool after the VACUUM INTO step.
func verifySnapshotAt(ctx context.Context, destPath string) error {
	snapshot, err := OpenReadOnly(destPath)
	if err != nil {
		_ = removeSQLiteFiles(destPath)
		return fmt.Errorf("open snapshot: %w", err)
	}
	status, checkErr := CheckIntegrity(ctx, snapshot)
	closeErr := snapshot.Close()
	if checkErr != nil {
		_ = removeSQLiteFiles(destPath)
		return fmt.Errorf("check snapshot integrity: %w", checkErr)
	}
	if closeErr != nil {
		_ = removeSQLiteFiles(destPath)
		return fmt.Errorf("close snapshot: %w", closeErr)
	}
	if status != "ok" {
		_ = removeSQLiteFiles(destPath)
		return fmt.Errorf("snapshot integrity check failed: %s", status)
	}
	return nil
}

func withPragmas(path string) string {
	values := url.Values{}
	values.Add("_pragma", "journal_mode="+sqliteJournalMode())
	for _, pragma := range fixedPragmas {
		values.Add("_pragma", pragma)
	}

	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return path + separator + values.Encode()
}

func openForSnapshot(path string) (*sql.DB, error) {
	values := url.Values{}
	values.Add("_pragma", "busy_timeout=5000")
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	db, err := sql.Open("sqlite", path+separator+values.Encode())
	if err != nil {
		return nil, fmt.Errorf("open sqlite database for snapshot: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite database for snapshot: %w", err)
	}
	return db, nil
}

func quoteSQLiteString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func removeSQLiteFiles(path string) error {
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(candidate); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", candidate, err)
		}
	}
	return nil
}

func samePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA == nil && errB == nil {
		a, b = absA, absB
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func sqliteJournalMode() string {
	switch strings.ToUpper(strings.TrimSpace(os.Getenv(sqliteJournalModeEnv))) {
	case "":
		return "WAL"
	case "WAL":
		return "WAL"
	case "DELETE":
		return "DELETE"
	case "TRUNCATE":
		return "TRUNCATE"
	case "PERSIST":
		return "PERSIST"
	default:
		return "WAL"
	}
}
