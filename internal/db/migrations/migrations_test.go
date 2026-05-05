package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

func loadSQLFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read SQL file %s: %v", path, err)
	}
	return string(content)
}

func seedV302Rows(t *testing.T, db *sql.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO settings (key, value, updated_at) VALUES ('timezone', 'Europe/Rome', '2026-01-02T03:04:05Z')`,
		`INSERT INTO api_tokens (token_hash, user_id, issued_at, last_used, revoked_at) VALUES ('hash-1', 'user-1', '2026-01-02T03:04:05Z', NULL, NULL)`,
		`INSERT INTO allowed_users (user_id, source, created_at) VALUES ('user-1', 'manual', '2026-01-02T03:04:05Z')`,
		`INSERT INTO pending_users (user_id, username, requested_at, decided_at, decision) VALUES ('user-2', 'pending_user', '2026-01-02T03:04:05Z', NULL, NULL)`,
		`INSERT INTO scheduled_tasks (name, kind, payload, recipient_id, schedule_kind, schedule_at, schedule_daily, schedule_every_minutes, next_run_at, last_run_at, last_error, status, created_at, updated_at) VALUES ('daily-review', 'telegram', '{}', 'chat-1', 'daily', NULL, '09:00', 0, '2026-01-03T09:00:00Z', NULL, '', 'active', '2026-01-02T03:04:05Z', '2026-01-02T03:04:05Z')`,
		`INSERT INTO conversations (chat_id, user_id, turn_index, role, content, tool_calls, tool_call_id, llm_calls, tool_calls_count, elapsed_ms, tokens_in, tokens_out, created_at) VALUES (1001, 2002, 1, 'user', 'remember migration safety', NULL, NULL, 1, 0, 123, 10, 20, '2026-01-02T03:04:05Z')`,
		`INSERT INTO proposed_updates (chat_id, fact, action, target_slug, similarity, source_turn_ids, status, created_at) VALUES (1001, 'Aura has migration tests', 'create', 'migration-tests', 0.75, '1', 'pending', '2026-01-02T03:04:05Z')`,
		`INSERT INTO wiki_issues (kind, severity, slug, broken_link, message, status, created_at, resolved_at) VALUES ('broken_link', 'medium', 'migration-tests', 'missing-page', 'Missing wiki page', 'open', '2026-01-02T03:04:05Z', NULL)`,
		`INSERT INTO embedding_cache (content_sha, model, embedding, created_at) VALUES ('sha-1', 'mistral-embed', x'010203', '2026-01-02T03:04:05Z')`,
		`INSERT INTO wiki_documents (id, content, metadata, title) VALUES ('migration-tests', 'Aura preserves searchable legacy wiki content', '{"slug":"migration-tests"}', 'Migration Tests')`,
		`INSERT INTO swarm_runs (id, goal, status, created_by, created_at, updated_at, completed_at, last_error) VALUES ('run-1', 'verify migrations', 'running', 'test', '2026-01-02T03:04:05Z', '2026-01-02T03:04:05Z', NULL, '')`,
		`INSERT INTO swarm_tasks (id, run_id, parent_id, role, subject, prompt, tool_allowlist, status, depth, attempts, blocked_by, result, tool_calls, llm_calls, elapsed_ms, created_at, started_at, completed_at, last_error) VALUES ('task-1', 'run-1', '', 'tester', 'migration', 'verify upgrade', '[]', 'pending', 0, 0, '[]', '', 0, 0, 0, '2026-01-02T03:04:05Z', NULL, NULL, '')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed v302 row with %q: %v", statement, err)
		}
	}
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

func TestRunRepairsLegacyConversationsTableWithoutChatID(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `
CREATE TABLE conversations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  role TEXT NOT NULL,
  content TEXT NOT NULL,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO conversations (user_id, role, content) VALUES (2002, 'user', 'legacy row');
`); err != nil {
		t.Fatalf("create legacy conversations table: %v", err)
	}

	if err := Run(ctx, db); err != nil {
		t.Fatalf("Run: %v", err)
	}

	assertColumns(t, db, "conversations", []string{
		"id",
		"chat_id",
		"user_id",
		"turn_index",
		"role",
		"content",
		"tool_calls",
		"tool_call_id",
		"llm_calls",
		"tool_calls_count",
		"elapsed_ms",
		"tokens_in",
		"tokens_out",
		"created_at",
	})
	assertIndexes(t, db, "conversations", []string{
		"idx_conv_chat",
		"idx_conv_user",
	})
	assertScalar(t, db, `SELECT COUNT(*) FROM conversations`, 0)
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

func TestRunUpgradesV302SchemaPreservesRowsAndIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if _, err := db.Exec(loadSQLFile(t, filepath.Join("testdata", "v302_schema.sql"))); err != nil {
		t.Fatalf("create v302 schema: %v", err)
	}
	seedV302Rows(t, db)

	if err := Run(ctx, db); err != nil {
		t.Fatalf("Run: %v", err)
	}

	assertScalar(t, db, `SELECT value FROM settings WHERE key = 'timezone'`, "Europe/Rome")
	assertScalar(t, db, `SELECT COUNT(*) FROM api_tokens WHERE token_hash = 'hash-1'`, 1)
	assertScalar(t, db, `SELECT COUNT(*) FROM allowed_users WHERE user_id = 'user-1'`, 1)
	assertScalar(t, db, `SELECT COUNT(*) FROM pending_users WHERE user_id = 'user-2'`, 1)
	assertScalar(t, db, `SELECT COUNT(*) FROM scheduled_tasks WHERE name = 'daily-review'`, 1)
	assertScalar(t, db, `SELECT content FROM conversations WHERE chat_id = 1001 AND turn_index = 1`, "remember migration safety")
	assertScalar(t, db, `SELECT fact FROM proposed_updates WHERE target_slug = 'migration-tests'`, "Aura has migration tests")
	assertScalar(t, db, `SELECT COUNT(*) FROM wiki_issues WHERE slug = 'migration-tests'`, 1)
	assertScalar(t, db, `SELECT COUNT(*) FROM embedding_cache WHERE content_sha = 'sha-1'`, 1)
	assertScalar(t, db, `SELECT COUNT(*) FROM swarm_runs WHERE id = 'run-1'`, 1)
	assertScalar(t, db, `SELECT COUNT(*) FROM swarm_tasks WHERE id = 'task-1'`, 1)

	assertScalar(t, db, `SELECT id FROM wiki_documents WHERE wiki_documents MATCH 'searchable'`, "migration-tests")

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
	assertScalar(t, db, `SELECT schedule_weekdays FROM scheduled_tasks WHERE name = 'daily-review'`, "")
	assertScalar(t, db, `SELECT provenance_json FROM proposed_updates WHERE target_slug = 'migration-tests'`, "{}")
	assertScalar(t, db, `SELECT tokens_total FROM swarm_tasks WHERE id = 'task-1'`, 0)

	first := appliedVersions(t, db)
	if err := Run(ctx, db); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	second := appliedVersions(t, db)

	if len(first) != len(second) {
		t.Fatalf("applied migration count changed after rerun: first=%v second=%v", first, second)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("applied versions changed after rerun: first=%v second=%v", first, second)
		}
	}
	if len(first) != 2 || first[0] != 1 || first[1] != 2 {
		t.Fatalf("applied versions = %v, want [1 2]", first)
	}
}

func TestFreshAndUpgradedSchemasConverge(t *testing.T) {
	ctx := context.Background()

	fresh := openTestDB(t)
	if err := Run(ctx, fresh); err != nil {
		t.Fatalf("fresh Run: %v", err)
	}

	upgraded := openTestDB(t)
	if _, err := upgraded.Exec(loadSQLFile(t, filepath.Join("testdata", "v302_schema.sql"))); err != nil {
		t.Fatalf("create v302 schema: %v", err)
	}
	if err := Run(ctx, upgraded); err != nil {
		t.Fatalf("upgraded Run: %v", err)
	}

	freshSignature := schemaSignature(t, fresh)
	upgradedSignature := schemaSignature(t, upgraded)
	if len(freshSignature) != len(upgradedSignature) {
		t.Fatalf(
			"schema signature length mismatch: fresh=%d upgraded=%d\nfresh=%v\nupgraded=%v",
			len(freshSignature),
			len(upgradedSignature),
			freshSignature,
			upgradedSignature,
		)
	}
	for i := range freshSignature {
		if freshSignature[i] != upgradedSignature[i] {
			t.Fatalf(
				"schema signature mismatch at %d:\nfresh:    %s\nupgraded: %s\nfresh all=%v\nupgraded all=%v",
				i,
				freshSignature[i],
				upgradedSignature[i],
				freshSignature,
				upgradedSignature,
			)
		}
	}

	assertFTSContentBehavior(t, fresh)
	assertFTSContentBehavior(t, upgraded)
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func schemaSignature(t *testing.T, db *sql.DB) []string {
	t.Helper()
	signature := tableSignatures(t, db)
	signature = append(signature, indexSignatures(t, db)...)
	signature = append(signature, ftsSignatures(t, db)...)
	sort.Strings(signature)
	return signature
}

func tableSignatures(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`
SELECT name
FROM sqlite_master
WHERE type = 'table'
  AND name NOT LIKE 'sqlite_%'
  AND name NOT LIKE 'wiki_documents_%'
ORDER BY name
`)
	if err != nil {
		t.Fatalf("query tables: %v", err)
	}
	defer rows.Close()

	var signatures []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatalf("scan table: %v", err)
		}
		signatures = append(signatures, fmt.Sprintf("table|%s|", table))
		for _, column := range columnSignatures(t, db, table) {
			signatures = append(signatures, fmt.Sprintf("column|%s|%s", table, column))
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table rows: %v", err)
	}
	return signatures
}

func columnSignatures(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(` + quoteIdent(table) + `)`)
	if err != nil {
		t.Fatalf("table info %s: %v", table, err)
	}
	defer rows.Close()

	var signatures []string
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan table info %s: %v", table, err)
		}
		defaultSignature := ""
		if defaultValue.Valid {
			defaultSignature = strings.Join(strings.Fields(defaultValue.String), " ")
		}
		signatures = append(
			signatures,
			fmt.Sprintf(
				"%s:%s:%d:%s:%d",
				name,
				strings.ToUpper(typ),
				notNull,
				defaultSignature,
				pk,
			),
		)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table info rows %s: %v", table, err)
	}
	sort.Strings(signatures)
	return signatures
}

func indexSignatures(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`
SELECT name, tbl_name, sql
FROM sqlite_master
WHERE type = 'index'
  AND name NOT LIKE 'sqlite_%'
ORDER BY name
`)
	if err != nil {
		t.Fatalf("query indexes: %v", err)
	}
	defer rows.Close()

	var signatures []string
	for rows.Next() {
		var name, table string
		var sqlText sql.NullString
		if err := rows.Scan(&name, &table, &sqlText); err != nil {
			t.Fatalf("scan index: %v", err)
		}
		normalizedSQL := ""
		if sqlText.Valid {
			normalizedSQL = strings.Join(strings.Fields(sqlText.String), " ")
		}
		signatures = append(signatures, fmt.Sprintf("index|%s|%s|%s", name, table, normalizedSQL))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("index rows: %v", err)
	}
	return signatures
}

func ftsSignatures(t *testing.T, db *sql.DB) []string {
	t.Helper()
	var exists int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM sqlite_master
WHERE type = 'table'
  AND name = 'wiki_documents'
`).Scan(&exists); err != nil {
		t.Fatalf("query wiki_documents FTS table: %v", err)
	}
	if exists == 0 {
		return nil
	}
	return []string{"fts|wiki_documents|present"}
}

func assertFTSContentBehavior(t *testing.T, db *sql.DB) {
	t.Helper()
	const probeID = "__schema_convergence_probe__"
	if _, err := db.Exec(`DELETE FROM wiki_documents WHERE id = ?`, probeID); err != nil {
		t.Fatalf("delete stale wiki_documents probe: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO wiki_documents (id, content, metadata, title) VALUES (?, ?, ?, ?)`,
		probeID,
		"schema convergence verifies FTS content behavior",
		`{"slug":"schema-convergence-probe"}`,
		"Schema Convergence Probe",
	); err != nil {
		t.Fatalf("insert wiki_documents probe: %v", err)
	}
	assertScalar(t, db, `SELECT COUNT(*) FROM wiki_documents WHERE wiki_documents MATCH 'convergence'`, 1)
	if _, err := db.Exec(`DELETE FROM wiki_documents WHERE id = ?`, probeID); err != nil {
		t.Fatalf("delete wiki_documents probe: %v", err)
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

func assertIndexes(t *testing.T, db *sql.DB, table string, names []string) {
	t.Helper()
	for _, name := range names {
		var got string
		if err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = ? AND name = ?`,
			table,
			name,
		).Scan(&got); err != nil {
			t.Fatalf("%s missing index %s: %v", table, name, err)
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
