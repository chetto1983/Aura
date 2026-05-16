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

	"github.com/aura/aura/internal/testutil"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	return testutil.OpenTestDB(t, nil)
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
		`INSERT INTO wiki_documents (id, content, metadata, title) VALUES ('migration-tests', 'Aura preserves searchable retired wiki content', '{"slug":"migration-tests"}', 'Migration Tests')`,
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
		"compact_memory_documents",
		"compact_memory_fts",
		"principals",
		"channel_accounts",
		"actors",
		"capability_grants",
		"authz_decisions",
		"swarm_runs",
		"swarm_tasks",
		"runs",
		"run_events",
		"run_outbox",
		"run_idempotency_keys",
		"audit_events",
		"secrets",
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
	assertColumns(t, db, "api_tokens", []string{
		"expires_at",
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
	assertColumns(t, db, "compact_memory_documents", []string{
		"id",
		"kind",
		"title",
		"body",
		"handle",
		"source_id",
		"page",
		"chat_id",
		"conversation_id",
		"proposal_id",
		"status",
		"entities_json",
		"tags_json",
		"updated_at",
	})
	assertColumns(t, db, "principals", []string{
		"id",
		"kind",
		"display_name",
		"status",
		"created_at",
		"metadata_json",
	})
	assertColumns(t, db, "channel_accounts", []string{
		"id",
		"principal_id",
		"provider",
		"external_id",
		"display_name",
		"created_at",
		"last_seen_at",
		"metadata_json",
	})
	assertColumns(t, db, "actors", []string{
		"id",
		"principal_id",
		"actor_type",
		"parent_actor_id",
		"channel_account_id",
		"run_id",
		"created_at",
		"expires_at",
		"metadata_json",
	})
	assertColumns(t, db, "capability_grants", []string{
		"id",
		"subject_type",
		"subject_id",
		"capability",
		"resource_type",
		"resource_id",
		"constraints_json",
		"granted_by_actor_id",
		"created_at",
		"expires_at",
		"revoked_at",
	})
	assertColumns(t, db, "authz_decisions", []string{
		"id",
		"actor_id",
		"capability",
		"resource_type",
		"resource_id",
		"decision",
		"reason",
		"run_id",
		"event_id",
		"created_at",
	})
	assertColumns(t, db, "runs", []string{
		"id",
		"parent_run_id",
		"thread_id",
		"principal_id",
		"actor_id",
		"channel",
		"status",
		"current_seq",
		"idempotency_key",
		"correlation_id",
		"final_text_preview",
		"stats_json",
		"metadata_json",
	})
	assertColumns(t, db, "run_events", []string{
		"id",
		"run_id",
		"parent_run_id",
		"seq",
		"type",
		"schema_version",
		"idempotency_key",
		"payload_json",
		"redaction_level",
	})
	assertColumns(t, db, "run_outbox", []string{
		"id",
		"run_id",
		"event_id",
		"target",
		"idempotency_key",
		"payload_json",
		"status",
		"attempts",
		"next_attempt_at",
		"last_error",
	})
	assertColumns(t, db, "run_idempotency_keys", []string{
		"scope",
		"key",
		"run_id",
		"event_id",
		"created_at",
	})
	assertColumns(t, db, "audit_events", []string{
		"id",
		"run_id",
		"event_id",
		"type",
		"actor_id",
		"target_type",
		"target_id",
		"payload_json",
		"redaction_level",
	})
	assertIndexes(t, db, "runs", []string{
		"idx_runs_status_updated",
		"idx_runs_thread_updated",
		"idx_runs_actor_updated",
		"idx_runs_parent",
		"idx_runs_idempotency",
	})
	assertIndexes(t, db, "run_events", []string{
		"idx_run_events_run_seq",
		"idx_run_events_type_created",
		"idx_run_events_correlation",
		"idx_run_events_idempotency",
	})
	assertIndexes(t, db, "run_outbox", []string{
		"idx_run_outbox_status_next",
		"idx_run_outbox_target_idempotency",
	})
	assertIndexes(t, db, "audit_events", []string{
		"idx_audit_events_type_created",
		"idx_audit_events_actor_created",
	})
	assertIndexes(t, db, "principals", []string{
		"idx_principals_kind_status",
	})
	assertIndexes(t, db, "channel_accounts", []string{
		"idx_channel_accounts_principal",
	})
	assertIndexes(t, db, "actors", []string{
		"idx_actors_principal_created",
		"idx_actors_parent",
		"idx_actors_run",
	})
	assertIndexes(t, db, "capability_grants", []string{
		"idx_capability_grants_subject",
		"idx_capability_grants_capability_resource",
		"idx_capability_grants_revoked",
	})
	assertIndexes(t, db, "authz_decisions", []string{
		"idx_authz_decisions_actor_created",
		"idx_authz_decisions_run_created",
		"idx_authz_decisions_capability_created",
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
INSERT INTO conversations (user_id, role, content) VALUES (2002, 'user', 'retired row');
`); err != nil {
		t.Fatalf("create retired conversations table: %v", err)
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
	assertScalar(t, db, `SELECT kind FROM principals WHERE id = 'principal:telegram:user-1'`, "owner")
	assertScalar(t, db, `SELECT principal_id FROM channel_accounts WHERE provider = 'telegram' AND external_id = 'user-1'`, "principal:telegram:user-1")
	assertScalar(t, db, `SELECT actor_type FROM actors WHERE id = 'actor:telegram:session:user-1'`, "session")
	assertScalar(t, db, `SELECT COUNT(*) FROM capability_grants WHERE subject_id = 'principal:telegram:user-1'`, 12)
	assertScalar(t, db, `SELECT COUNT(*) FROM capability_grants WHERE subject_id = 'principal:telegram:user-1' AND capability = 'skills.install'`, 1)
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
	assertColumns(t, db, "api_tokens", []string{
		"expires_at",
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
	assertScalar(t, db, `SELECT expires_at FROM api_tokens WHERE token_hash = 'hash-1'`, "2026-02-01T03:04:05Z")
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
	if len(first) != 11 || first[0] != 1 || first[1] != 2 || first[2] != 3 || first[3] != 4 || first[4] != 5 || first[5] != 6 || first[6] != 7 || first[7] != 8 || first[8] != 9 || first[9] != 10 || first[10] != 11 {
		t.Fatalf("applied versions = %v, want [1 2 3 4 5 6 7 8 9 10 11]", first)
	}
}

func TestChatQuestionsTableIsUsable(t *testing.T) {
	db := openTestDB(t)
	if err := Run(context.Background(), db); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := db.Exec(`
INSERT INTO runs (id, channel, status, started_at, updated_at)
VALUES ('run-question', 'telegram', 'waiting_for_user', '2026-05-16T08:00:00Z', '2026-05-16T08:00:00Z');
INSERT INTO run_events (id, run_id, seq, type, payload_json, created_at)
VALUES ('evt-question', 'run-question', 1, 'question_requested', '{}', '2026-05-16T08:00:00Z');
INSERT INTO chat_questions (
  id, run_id, event_id, thread_id, actor_id, channel, kind, status,
  question_text, options_json, requested_at
) VALUES (
  'evt-question', 'run-question', 'evt-question', 'thread-1', 'actor-1',
  'telegram', 'approval', 'waiting', 'Approve write?', '["approve_once","deny"]',
  '2026-05-16T08:00:00Z'
);
`); err != nil {
		t.Fatalf("insert chat question: %v", err)
	}
	assertScalar(t, db, `SELECT status FROM chat_questions WHERE id = 'evt-question'`, "waiting")
	assertScalar(t, db, `SELECT kind FROM chat_questions WHERE thread_id = 'thread-1' AND channel = 'telegram'`, "approval")
}

func TestIdentityCapabilityTablesAreUsable(t *testing.T) {
	db := openTestDB(t)
	if err := Run(context.Background(), db); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := db.Exec(`
INSERT INTO principals (id, kind, display_name, status, created_at, metadata_json)
VALUES ('principal-1', 'human', 'Owner', 'active', '2026-05-15T08:00:00Z', '{}');
INSERT INTO channel_accounts (id, principal_id, provider, external_id, display_name, created_at, metadata_json)
VALUES ('acct-1', 'principal-1', 'telegram', '123', 'Owner TG', '2026-05-15T08:00:00Z', '{}');
INSERT INTO actors (id, principal_id, actor_type, channel_account_id, run_id, created_at, metadata_json)
VALUES ('actor-1', 'principal-1', 'session', 'acct-1', 'run-1', '2026-05-15T08:00:00Z', '{}');
INSERT INTO capability_grants (
  id, subject_type, subject_id, capability, resource_type, resource_id,
  constraints_json, granted_by_actor_id, created_at
) VALUES (
  'grant-1', 'principal', 'principal-1', 'api.chat', 'thread', 'thread-1',
  '{}', 'actor-1', '2026-05-15T08:00:00Z'
);
INSERT INTO authz_decisions (
  id, actor_id, capability, resource_type, resource_id, decision, reason,
  run_id, event_id, created_at
) VALUES (
  'decision-1', 'actor-1', 'api.chat', 'thread', 'thread-1', 'allow',
  'active_grant', 'run-1', 'event-1', '2026-05-15T08:00:01Z'
);
`); err != nil {
		t.Fatalf("insert identity rows: %v", err)
	}

	assertScalar(t, db, `SELECT principal_id FROM channel_accounts WHERE id = 'acct-1'`, "principal-1")
	assertScalar(t, db, `SELECT decision FROM authz_decisions WHERE id = 'decision-1'`, "allow")

	if _, err := db.Exec(`
INSERT INTO channel_accounts (id, principal_id, provider, external_id, created_at, metadata_json)
VALUES ('acct-bad', 'missing-principal', 'telegram', '404', '2026-05-15T08:00:00Z', '{}')
`); err == nil {
		t.Fatal("expected foreign-key failure for missing principal")
	}
}

func TestRunEventFoundationTablesAreUsable(t *testing.T) {
	db := openTestDB(t)
	if err := Run(context.Background(), db); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := db.Exec(`
INSERT INTO runs (
  id, parent_run_id, thread_id, principal_id, channel, status, started_at,
  updated_at, idempotency_key, correlation_id, stats_json, metadata_json
) VALUES (
  'run-1', 'parent-1', 'thread-1', 'principal-1', 'web', 'running',
  '2026-05-15T08:00:00Z', '2026-05-15T08:00:00Z', 'inbound-1',
  'corr-1', '{}', '{}'
);
INSERT INTO run_events (
  id, run_id, parent_run_id, seq, type, schema_version, correlation_id,
  idempotency_key, payload_json, redaction_level, created_at
) VALUES (
  'event-1', 'run-1', 'parent-1', 1, 'run_started', 1, 'corr-1',
  'event-1-key', '{"arg_keys":["q"]}', 'metadata', '2026-05-15T08:00:01Z'
);
INSERT INTO run_outbox (
  id, run_id, event_id, target, idempotency_key, payload_json, status,
  attempts, created_at, updated_at
) VALUES (
  'outbox-1', 'run-1', 'event-1', 'web:sse', 'deliver-1', '{}',
  'pending', 0, '2026-05-15T08:00:01Z', '2026-05-15T08:00:01Z'
);
INSERT INTO run_idempotency_keys (scope, key, run_id, event_id, created_at)
VALUES ('inbound', 'inbound-1', 'run-1', '', '2026-05-15T08:00:00Z');
INSERT INTO audit_events (
  id, run_id, event_id, type, actor_id, target_type, target_id, payload_json,
  redaction_level, created_at
) VALUES (
  'audit-1', 'run-1', 'event-1', 'privileged_payload_read', 'principal-1',
  'run', 'run-1', '{}', 'metadata', '2026-05-15T08:00:02Z'
);
`); err != nil {
		t.Fatalf("insert run event foundation rows: %v", err)
	}

	assertScalar(t, db, `SELECT parent_run_id FROM run_events WHERE id = 'event-1'`, "parent-1")
	assertScalar(t, db, `SELECT status FROM run_outbox WHERE id = 'outbox-1'`, "pending")
	assertScalar(t, db, `SELECT run_id FROM run_idempotency_keys WHERE scope = 'inbound' AND key = 'inbound-1'`, "run-1")
	assertScalar(t, db, `SELECT type FROM audit_events WHERE id = 'audit-1'`, "privileged_payload_read")
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
	tables := schemaTables(t, db)

	var signatures []string
	for _, table := range tables {
		signatures = append(signatures, fmt.Sprintf("table|%s|", table))
		for _, column := range columnSignatures(t, db, table) {
			signatures = append(signatures, fmt.Sprintf("column|%s|%s", table, column))
		}
	}
	return signatures
}

func schemaTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`
SELECT name
FROM sqlite_master
WHERE type = 'table'
  AND name NOT LIKE 'sqlite_%'
  AND name NOT LIKE 'wiki_documents_%'
  AND name NOT LIKE 'compact_memory_fts_%'
ORDER BY name
`)
	if err != nil {
		t.Fatalf("query tables: %v", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatalf("scan table: %v", err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table rows: %v", err)
	}
	return tables
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
	var signatures []string
	for _, table := range schemaTables(t, db) {
		rows, err := db.Query(`PRAGMA index_list(` + quoteIdent(table) + `)`)
		if err != nil {
			t.Fatalf("index list %s: %v", table, err)
		}

		for rows.Next() {
			var seq, unique, partial int
			var name, origin string
			if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
				rows.Close()
				t.Fatalf("scan index list %s: %v", table, err)
			}
			identity := name
			if strings.HasPrefix(name, "sqlite_autoindex_") {
				identity = origin
			}
			signatures = append(
				signatures,
				fmt.Sprintf(
					"index|%s|%s|unique=%d|origin=%s|partial=%d|columns=%s|sql=%s",
					table,
					identity,
					unique,
					origin,
					partial,
					strings.Join(indexColumns(t, db, name), ","),
					indexSQL(t, db, name),
				),
			)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatalf("index list rows %s: %v", table, err)
		}
		rows.Close()
	}
	sort.Strings(signatures)
	return signatures
}

func indexColumns(t *testing.T, db *sql.DB, index string) []string {
	t.Helper()
	rows, err := db.Query(`PRAGMA index_info(` + quoteIdent(index) + `)`)
	if err != nil {
		t.Fatalf("index info %s: %v", index, err)
	}
	defer rows.Close()

	type indexedColumn struct {
		seq  int
		name string
	}
	var columns []indexedColumn
	for rows.Next() {
		var seq, cid int
		var name sql.NullString
		if err := rows.Scan(&seq, &cid, &name); err != nil {
			t.Fatalf("scan index info %s: %v", index, err)
		}
		columnName := fmt.Sprintf("#cid%d", cid)
		if name.Valid {
			columnName = name.String
		}
		columns = append(columns, indexedColumn{seq: seq, name: columnName})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("index info rows %s: %v", index, err)
	}

	sort.Slice(columns, func(i, j int) bool {
		return columns[i].seq < columns[j].seq
	})
	names := make([]string, 0, len(columns))
	for _, column := range columns {
		names = append(names, column.name)
	}
	return names
}

func indexSQL(t *testing.T, db *sql.DB, index string) string {
	t.Helper()
	var sqlText sql.NullString
	if err := db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?`,
		index,
	).Scan(&sqlText); err != nil {
		if err == sql.ErrNoRows {
			return ""
		}
		t.Fatalf("query index SQL %s: %v", index, err)
	}
	if !sqlText.Valid {
		return ""
	}
	return strings.Join(strings.Fields(sqlText.String), " ")
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
	var compactExists int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM sqlite_master
WHERE type = 'table'
  AND name = 'compact_memory_fts'
`).Scan(&compactExists); err != nil {
		t.Fatalf("query compact_memory_fts table: %v", err)
	}
	out := []string{"fts|wiki_documents|present"}
	if compactExists > 0 {
		out = append(out, "fts|compact_memory_fts|present")
	}
	return out
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
	assertScalar(t, db, `SELECT COUNT(*) FROM wiki_documents WHERE wiki_documents MATCH 'convergence'`, 0)
	if _, err := db.Exec(
		`INSERT INTO compact_memory_documents (id, kind, title, body, handle, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		probeID,
		"source",
		"Compact Probe",
		"compact memory convergence verifies FTS content behavior",
		"source:probe",
		"2026-05-08T00:00:00Z",
	); err != nil {
		t.Fatalf("insert compact_memory_documents probe: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO compact_memory_fts (id, kind, title, body, handle, source_id, status, entities, tags) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		probeID,
		"source",
		"Compact Probe",
		"compact memory convergence verifies FTS content behavior",
		"source:probe",
		"",
		"",
		"",
		"",
	); err != nil {
		t.Fatalf("insert compact_memory_fts probe: %v", err)
	}
	assertScalar(t, db, `SELECT COUNT(*) FROM compact_memory_fts WHERE compact_memory_fts MATCH 'compact'`, 1)
	if _, err := db.Exec(`DELETE FROM compact_memory_fts WHERE id = ?`, probeID); err != nil {
		t.Fatalf("delete compact_memory_fts probe: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM compact_memory_documents WHERE id = ?`, probeID); err != nil {
		t.Fatalf("delete compact_memory_documents probe: %v", err)
	}
	assertScalar(t, db, `SELECT COUNT(*) FROM compact_memory_fts WHERE compact_memory_fts MATCH 'compact'`, 0)
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
