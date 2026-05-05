package migrations

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	auradb "github.com/aura/aura/internal/db"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := auradb.Open(filepath.Join(t.TempDir(), "aura.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestFTS5CreateVirtualTableWorksInsideTransaction(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	_, err = tx.ExecContext(ctx, `
CREATE VIRTUAL TABLE wiki_documents_tx_probe
USING fts5(id, content, metadata, title)
`)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("CREATE VIRTUAL TABLE inside transaction failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM wiki_documents_tx_probe`).Scan(&count); err != nil {
		t.Fatalf("query FTS5 probe: %v", err)
	}
}

func appliedVersions(t *testing.T, db *sql.DB) []int {
	t.Helper()
	rows, err := db.Query(`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	defer rows.Close()
	var versions []int
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			t.Fatalf("scan version: %v", err)
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return versions
}

func TestRunCreatesSchemaMigrationsTable(t *testing.T) {
	db := openTestDB(t)

	if err := Run(context.Background(), db); err != nil {
		t.Fatalf("Run: %v", err)
	}

	rows, err := db.Query(`PRAGMA table_info(schema_migrations)`)
	if err != nil {
		t.Fatalf("table info: %v", err)
	}
	defer rows.Close()

	cols := map[string]string{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan table info: %v", err)
		}
		cols[name] = typ
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	want := map[string]string{
		"version":    "INTEGER",
		"name":       "TEXT",
		"applied_at": "TEXT",
	}
	for name, typ := range want {
		if cols[name] != typ {
			t.Fatalf("schema_migrations.%s type = %q, want %q; cols=%v", name, cols[name], typ, cols)
		}
	}
}

func TestRunCreatesCurrentFreshSchema(t *testing.T) {
	db := openTestDB(t)

	if err := Run(context.Background(), db); err != nil {
		t.Fatalf("Run: %v", err)
	}

	tables := []string{
		"settings",
		"api_tokens",
		"allowed_users",
		"pending_users",
		"scheduled_tasks",
		"conversations",
		"proposed_updates",
		"wiki_issues",
		"embedding_cache",
		"wiki_documents",
		"swarm_runs",
		"swarm_tasks",
	}
	for _, table := range tables {
		var name string
		if err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type IN ('table', 'virtual table') AND name = ?`,
			table,
		).Scan(&name); err != nil {
			t.Fatalf("table %s does not exist: %v", table, err)
		}
	}

	assertColumns(t, db, "scheduled_tasks", []string{
		"schedule_weekdays",
		"schedule_every_minutes",
		"last_output",
		"last_metrics_json",
		"wake_signature",
	})
	assertColumns(t, db, "proposed_updates", []string{
		"category",
		"related_slugs",
		"provenance_json",
	})
	assertColumns(t, db, "swarm_tasks", []string{
		"tokens_prompt",
		"tokens_completion",
		"tokens_total",
	})
}

func TestRunCreatesUsableWikiDocumentsFTS5Table(t *testing.T) {
	db := openTestDB(t)

	if err := Run(context.Background(), db); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := db.Exec(
		`INSERT INTO wiki_documents (id, content, metadata, title) VALUES (?, ?, ?, ?)`,
		"alpha",
		"Aura remembers durable context",
		`{"slug":"alpha"}`,
		"Alpha",
	); err != nil {
		t.Fatalf("insert wiki_documents: %v", err)
	}

	var id string
	if err := db.QueryRow(
		`SELECT id FROM wiki_documents WHERE wiki_documents MATCH 'durable'`,
	).Scan(&id); err != nil {
		t.Fatalf("query wiki_documents MATCH: %v", err)
	}
	if id != "alpha" {
		t.Fatalf("matched wiki_documents id = %q, want alpha", id)
	}
}

func TestRunIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := Run(ctx, db); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	first := appliedVersions(t, db)

	if err := Run(ctx, db); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	second := appliedVersions(t, db)

	if len(first) == 0 {
		t.Fatal("no migrations were recorded")
	}
	if len(first) != len(second) {
		t.Fatalf("applied migration count changed after rerun: first=%v second=%v", first, second)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("applied versions changed after rerun: first=%v second=%v", first, second)
		}
	}
}

func assertColumns(t *testing.T, db *sql.DB, table string, names []string) {
	t.Helper()
	cols := tableColumns(t, db, table)
	for _, name := range names {
		if _, ok := cols[name]; !ok {
			t.Fatalf("%s missing column %s; columns=%v", table, name, cols)
		}
	}
}

func tableColumns(t *testing.T, db *sql.DB, table string) map[string]struct{} {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("table info %s: %v", table, err)
	}
	defer rows.Close()

	cols := map[string]struct{}{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan table info %s: %v", table, err)
		}
		cols[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows %s: %v", table, err)
	}
	return cols
}

func TestRegisteredReturnsCopy(t *testing.T) {
	migrations := Registered()
	if len(migrations) == 0 {
		t.Fatal("Registered returned no migrations")
	}

	first := migrations[0]
	migrations[0] = Migration{}

	again := Registered()
	if len(again) == 0 {
		t.Fatal("Registered returned no migrations after mutation")
	}
	if again[0].Version != first.Version || again[0].Name != first.Name {
		t.Fatalf("Registered returned shared slice: first=%+v again=%+v", first, again[0])
	}
}

func TestValidateRegisteredRejectsInvalidLists(t *testing.T) {
	ok := func(context.Context, *sql.Tx) error { return nil }
	cases := map[string][]Migration{
		"zero version": {
			{Version: 0, Name: "bad", Up: ok},
		},
		"empty name": {
			{Version: 1, Name: "", Up: ok},
		},
		"nil up": {
			{Version: 1, Name: "bad", Up: nil},
		},
		"duplicate version": {
			{Version: 1, Name: "one", Up: ok},
			{Version: 1, Name: "again", Up: ok},
		},
		"decreasing version": {
			{Version: 2, Name: "two", Up: ok},
			{Version: 1, Name: "one", Up: ok},
		},
	}

	for name, migrations := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validateRegistered(migrations); err == nil {
				t.Fatal("validateRegistered error = nil")
			}
		})
	}
}
