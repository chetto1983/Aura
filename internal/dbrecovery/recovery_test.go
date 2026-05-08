package dbrecovery

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
)

func TestRecoverCopiesCanonicalTablesAndSkipsDerivedIndex(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	srcPath := filepath.Join(dir, "source.db")
	dstPath := filepath.Join(dir, "recovered.db")
	src := openMigratedDB(t, srcPath)
	seedRecoverableRows(t, src)
	if err := src.Close(); err != nil {
		t.Fatalf("close source: %v", err)
	}

	report, err := Recover(context.Background(), Options{SourcePath: srcPath, DestPath: dstPath})
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if report.IntegrityStatus != "ok" {
		t.Fatalf("integrity status = %q, want ok", report.IntegrityStatus)
	}
	if report.RowsCopied == 0 {
		t.Fatal("RowsCopied = 0, want canonical rows copied")
	}
	assertTableReport(t, report, "settings", 1, false)
	assertTableReport(t, report, "wiki_documents", 0, true)

	dst := openExistingDB(t, dstPath)
	defer dst.Close()
	assertScalar(t, dst, `SELECT value FROM settings WHERE key = 'LLM_MODEL'`, "deepseek-test")
	assertScalar(t, dst, `SELECT COUNT(*) FROM wiki_documents`, 0)
	assertScalar(t, dst, `SELECT COUNT(*) FROM schema_migrations`, len(migrations.Registered()))
}

func TestRecoverReplaceBacksUpCorruptSourcePath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	srcPath := filepath.Join(dir, "aura.db")
	backupPath := filepath.Join(dir, "aura.corrupt-test.db")
	src := openMigratedDB(t, srcPath)
	seedRecoverableRows(t, src)
	if err := src.Close(); err != nil {
		t.Fatalf("close source: %v", err)
	}

	report, err := Recover(context.Background(), Options{
		SourcePath: srcPath,
		Replace:    true,
		BackupPath: backupPath,
		Now:        func() time.Time { return time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("Recover replace: %v", err)
	}
	if !report.Replaced {
		t.Fatal("Replaced = false, want true")
	}
	if report.BackupPath != backupPath {
		t.Fatalf("BackupPath = %q, want %q", report.BackupPath, backupPath)
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("backup source missing: %v", err)
	}

	recovered := openExistingDB(t, srcPath)
	defer recovered.Close()
	assertScalar(t, recovered, `SELECT value FROM settings WHERE key = 'LLM_MODEL'`, "deepseek-test")
}

func TestRecoverReplaceRemovesStaleSourceSidecars(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	srcPath := filepath.Join(dir, "aura.db")
	backupPath := filepath.Join(dir, "aura.corrupt-test.db")
	src := openMigratedDB(t, srcPath)
	seedRecoverableRows(t, src)
	if err := src.Close(); err != nil {
		t.Fatalf("close source: %v", err)
	}
	writeFile(t, srcPath+"-wal", []byte("stale wal"))
	writeFile(t, srcPath+"-shm", []byte("stale shm"))

	report, err := Recover(context.Background(), Options{
		SourcePath: srcPath,
		Replace:    true,
		BackupPath: backupPath,
	})
	if err != nil {
		t.Fatalf("Recover replace: %v", err)
	}
	if !report.Replaced {
		t.Fatal("Replaced = false, want true")
	}
	assertNoFile(t, srcPath+"-wal")
	assertNoFile(t, srcPath+"-shm")

	recovered := openExistingDB(t, srcPath)
	defer recovered.Close()
	assertScalar(t, recovered, `SELECT value FROM settings WHERE key = 'LLM_MODEL'`, "deepseek-test")
}

func TestReplaceSourceWithRecoveredRemovesStaleSourceSidecars(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	srcPath := filepath.Join(dir, "aura.db")
	recoveredPath := filepath.Join(dir, "aura.db.recovered")
	backupPath := filepath.Join(dir, "aura.corrupt-test.db")
	writeFile(t, srcPath, []byte("old main"))
	writeFile(t, srcPath+"-wal", []byte("stale wal"))
	writeFile(t, srcPath+"-shm", []byte("stale shm"))
	writeFile(t, recoveredPath, []byte("fresh main"))

	gotBackup, err := replaceSourceWithRecovered(Options{
		SourcePath: srcPath,
		BackupPath: backupPath,
	}, recoveredPath)
	if err != nil {
		t.Fatalf("replaceSourceWithRecovered: %v", err)
	}
	if gotBackup != backupPath {
		t.Fatalf("backup path = %q, want %q", gotBackup, backupPath)
	}
	assertFileContent(t, srcPath, []byte("fresh main"))
	assertNoFile(t, srcPath+"-wal")
	assertNoFile(t, srcPath+"-shm")
	assertFileContent(t, backupPath+"-wal", []byte("stale wal"))
	assertFileContent(t, backupPath+"-shm", []byte("stale shm"))
}

func TestSkipTableSkipsFTSShadowsAndMigrationState(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"schema_migrations":   true,
		"wiki_documents":      true,
		"wiki_documents_data": true,
		"sqlite_sequence":     true,
		"settings":            false,
		"conversations":       false,
		"swarm_runs":          false,
		"swarm_tasks":         false,
		"embedding_cache":     false,
		"proposed_updates":    false,
		"allowed_users":       false,
		"pending_users":       false,
		"api_tokens":          false,
		"scheduled_tasks":     false,
		"wiki_issues":         false,
	}
	for table, wantSkip := range cases {
		got, _ := skipTable(table)
		if got != wantSkip {
			t.Fatalf("skipTable(%q) = %v, want %v", table, got, wantSkip)
		}
	}
}

func TestCommonColumnsKeepsDestinationOrder(t *testing.T) {
	t.Parallel()

	got := commonColumns([]string{"id", "name", "created_at", "new_col"}, []string{"created_at", "id", "name"})
	want := []string{"id", "name", "created_at"}
	if len(got) != len(want) {
		t.Fatalf("common columns = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("common columns = %#v, want %#v", got, want)
		}
	}
}

func openMigratedDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db := openExistingDB(t, path)
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	return db
}

func openExistingDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := auradb.Open(path)
	if err != nil {
		t.Fatalf("open db %s: %v", path, err)
	}
	return db
}

func seedRecoverableRows(t *testing.T, db *sql.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO settings (key, value, updated_at) VALUES ('LLM_MODEL', 'deepseek-test', '2026-05-08T10:00:00Z')`,
		`INSERT INTO allowed_users (user_id, source, created_at) VALUES ('1148481707', 'manual', '2026-05-08T10:00:00Z')`,
		`INSERT INTO conversations (chat_id, user_id, turn_index, role, content) VALUES (1, 2, 1, 'user', 'hello')`,
		`INSERT INTO wiki_documents (id, content, metadata, title) VALUES ('derived', 'derived content', '{}', 'Derived')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed %q: %v", statement, err)
		}
	}
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertNoFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("%s exists, want removed", path)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", path, err)
	}
}

func assertFileContent(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

func assertTableReport(t *testing.T, report Report, table string, rows int, skipped bool) {
	t.Helper()
	for _, tr := range report.Tables {
		if tr.Name != table {
			continue
		}
		if tr.Rows != rows || tr.Skipped != skipped || tr.Error != "" {
			t.Fatalf("table report %s = %+v, want rows=%d skipped=%v no error", table, tr, rows, skipped)
		}
		return
	}
	t.Fatalf("table report %s missing in %+v", table, report.Tables)
}

func assertScalar[T comparable](t *testing.T, db *sql.DB, query string, want T) {
	t.Helper()
	var got T
	if err := db.QueryRow(query).Scan(&got); err != nil {
		t.Fatalf("query scalar %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("query scalar %q = %v, want %v", query, got, want)
	}
}
