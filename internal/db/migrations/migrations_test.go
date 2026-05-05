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
