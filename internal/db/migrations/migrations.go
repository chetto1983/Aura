package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Migration describes a single database schema change.
type Migration struct {
	Version int
	Name    string
	Up      func(context.Context, *sql.Tx) error
}

var registered = []Migration{
	{Version: 1, Name: "create_current_schema", Up: createCurrentSchema},
	{Version: 2, Name: "backfill_current_columns", Up: backfillCurrentColumns},
	{Version: 3, Name: "add_api_token_expiry", Up: addAPITokenExpiry},
	{Version: 4, Name: "add_compact_memory_index", Up: addCompactMemoryIndex},
	{Version: 5, Name: "add_tool_index_state", Up: addToolIndexState},
	{Version: 6, Name: "add_run_event_foundation", Up: addRunEventFoundation},
	{Version: 7, Name: "add_identity_capability_grants", Up: addIdentityCapabilityGrants},
	{Version: 8, Name: "backfill_allowed_users_identity", Up: backfillAllowedUsersIdentity},
	{Version: 9, Name: "add_secrets_table", Up: addSecretsTable},
	{Version: 10, Name: "add_tool_attempts", Up: addToolAttempts},
	{Version: 11, Name: "add_chat_questions", Up: addChatQuestions},
	{Version: 12, Name: "add_projection_state", Up: addProjectionState},
	{Version: 13, Name: "add_compact_memory_freshness_columns", Up: addCompactMemoryFreshnessColumns},
	{Version: 14, Name: "add_operational_memory_proposal_columns", Up: addOperationalMemoryProposalColumns},
}

type columnDef struct {
	Name string
	SQL  string
}

const currentSchemaSQL = `
CREATE TABLE IF NOT EXISTS settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS api_tokens (
  token_hash TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL,
  issued_at  TEXT NOT NULL,
  expires_at TEXT,
  last_used  TEXT,
  revoked_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_api_tokens_user ON api_tokens(user_id);

CREATE TABLE IF NOT EXISTS allowed_users (
  user_id    TEXT PRIMARY KEY,
  source     TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pending_users (
  user_id      TEXT PRIMARY KEY,
  username     TEXT NOT NULL,
  requested_at TEXT NOT NULL,
  decided_at   TEXT,
  decision     TEXT
);
CREATE INDEX IF NOT EXISTS idx_pending_users_decision ON pending_users(decision);

CREATE TABLE IF NOT EXISTS scheduled_tasks (
  id                     INTEGER PRIMARY KEY AUTOINCREMENT,
  name                   TEXT NOT NULL UNIQUE,
  kind                   TEXT NOT NULL,
  payload                TEXT NOT NULL DEFAULT '',
  recipient_id           TEXT NOT NULL DEFAULT '',
  schedule_kind          TEXT NOT NULL,
  schedule_at            TEXT,
  schedule_daily         TEXT,
  schedule_weekdays      TEXT NOT NULL DEFAULT '',
  schedule_every_minutes INTEGER NOT NULL DEFAULT 0,
  next_run_at            TEXT NOT NULL,
  last_run_at            TEXT,
  last_error             TEXT NOT NULL DEFAULT '',
  last_output            TEXT NOT NULL DEFAULT '',
  last_metrics_json      TEXT NOT NULL DEFAULT '',
  wake_signature         TEXT NOT NULL DEFAULT '',
  status                 TEXT NOT NULL DEFAULT 'active',
  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_due
  ON scheduled_tasks(status, next_run_at);

CREATE TABLE IF NOT EXISTS conversations (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  chat_id           INTEGER NOT NULL,
  user_id           INTEGER NOT NULL,
  turn_index        INTEGER NOT NULL,
  role              TEXT NOT NULL,
  content           TEXT NOT NULL,
  tool_calls        TEXT,
  tool_call_id      TEXT,
  llm_calls         INTEGER NOT NULL DEFAULT 0,
  tool_calls_count  INTEGER NOT NULL DEFAULT 0,
  elapsed_ms        INTEGER NOT NULL DEFAULT 0,
  tokens_in         INTEGER NOT NULL DEFAULT 0,
  tokens_out        INTEGER NOT NULL DEFAULT 0,
  created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(chat_id, turn_index)
);
CREATE INDEX IF NOT EXISTS idx_conv_chat ON conversations(chat_id, turn_index);
CREATE INDEX IF NOT EXISTS idx_conv_user ON conversations(user_id, created_at);

CREATE TABLE IF NOT EXISTS proposed_updates (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  chat_id         INTEGER NOT NULL,
  fact            TEXT NOT NULL,
  action          TEXT NOT NULL,
  target_slug     TEXT NOT NULL DEFAULT '',
  similarity      REAL NOT NULL DEFAULT 0,
  source_turn_ids TEXT NOT NULL DEFAULT '',
  category        TEXT NOT NULL DEFAULT '',
  related_slugs   TEXT NOT NULL DEFAULT '',
  provenance_json TEXT NOT NULL DEFAULT '{}',
  status          TEXT NOT NULL DEFAULT 'pending',
  kind            TEXT NOT NULL DEFAULT 'wiki',
  signature_hash  TEXT NOT NULL DEFAULT '',
  created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS wiki_issues (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  kind        TEXT NOT NULL,
  severity    TEXT NOT NULL,
  slug        TEXT NOT NULL DEFAULT '',
  broken_link TEXT NOT NULL DEFAULT '',
  message     TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL DEFAULT 'open',
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  resolved_at DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_wiki_issues_key
  ON wiki_issues(kind, slug, broken_link);

CREATE TABLE IF NOT EXISTS embedding_cache (
  content_sha TEXT NOT NULL,
  model       TEXT NOT NULL,
  embedding   BLOB NOT NULL,
  created_at  TIMESTAMP NOT NULL,
  PRIMARY KEY (content_sha, model)
);

CREATE VIRTUAL TABLE IF NOT EXISTS wiki_documents
USING fts5(id, content, metadata, title);

CREATE TABLE IF NOT EXISTS compact_memory_documents (
  id                 TEXT PRIMARY KEY,
  kind               TEXT NOT NULL,
  title              TEXT NOT NULL DEFAULT '',
  body               TEXT NOT NULL,
  handle             TEXT NOT NULL DEFAULT '',
  source_id          TEXT NOT NULL DEFAULT '',
  page               INTEGER NOT NULL DEFAULT 0,
  chat_id            INTEGER NOT NULL DEFAULT 0,
  conversation_id    INTEGER NOT NULL DEFAULT 0,
  proposal_id        INTEGER NOT NULL DEFAULT 0,
  status             TEXT NOT NULL DEFAULT '',
  entities_json      TEXT NOT NULL DEFAULT '[]',
  tags_json          TEXT NOT NULL DEFAULT '[]',
  updated_at         TEXT NOT NULL,
  content_hash       TEXT NOT NULL DEFAULT '',
  embedding_model_id TEXT NOT NULL DEFAULT '',
  index_build_id     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_compact_memory_kind
  ON compact_memory_documents(kind, updated_at);
CREATE INDEX IF NOT EXISTS idx_compact_memory_chat
  ON compact_memory_documents(kind, chat_id, updated_at);
CREATE INDEX IF NOT EXISTS idx_compact_memory_source
  ON compact_memory_documents(source_id);
CREATE INDEX IF NOT EXISTS idx_compact_memory_proposal
  ON compact_memory_documents(proposal_id);

CREATE VIRTUAL TABLE IF NOT EXISTS compact_memory_fts
USING fts5(id UNINDEXED, kind, title, body, handle, source_id, status, entities, tags);

CREATE TABLE IF NOT EXISTS principals (
  id            TEXT PRIMARY KEY,
  kind          TEXT NOT NULL,
  display_name  TEXT NOT NULL DEFAULT '',
  status        TEXT NOT NULL DEFAULT 'active',
  created_at    TEXT NOT NULL,
  metadata_json TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_principals_kind_status
  ON principals(kind, status);

CREATE TABLE IF NOT EXISTS channel_accounts (
  id            TEXT PRIMARY KEY,
  principal_id  TEXT NOT NULL,
  provider      TEXT NOT NULL,
  external_id   TEXT NOT NULL,
  display_name  TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL,
  last_seen_at  TEXT,
  metadata_json TEXT NOT NULL DEFAULT '{}',
  UNIQUE(provider, external_id),
  FOREIGN KEY(principal_id) REFERENCES principals(id)
);
CREATE INDEX IF NOT EXISTS idx_channel_accounts_principal
  ON channel_accounts(principal_id);

CREATE TABLE IF NOT EXISTS actors (
  id                 TEXT PRIMARY KEY,
  principal_id       TEXT NOT NULL,
  actor_type         TEXT NOT NULL,
  parent_actor_id    TEXT,
  channel_account_id TEXT,
  run_id             TEXT NOT NULL DEFAULT '',
  created_at         TEXT NOT NULL,
  expires_at         TEXT,
  metadata_json      TEXT NOT NULL DEFAULT '{}',
  FOREIGN KEY(principal_id) REFERENCES principals(id),
  FOREIGN KEY(parent_actor_id) REFERENCES actors(id),
  FOREIGN KEY(channel_account_id) REFERENCES channel_accounts(id)
);
CREATE INDEX IF NOT EXISTS idx_actors_principal_created
  ON actors(principal_id, created_at);
CREATE INDEX IF NOT EXISTS idx_actors_parent
  ON actors(parent_actor_id);
CREATE INDEX IF NOT EXISTS idx_actors_run
  ON actors(run_id);

CREATE TABLE IF NOT EXISTS capability_grants (
  id                  TEXT PRIMARY KEY,
  subject_type        TEXT NOT NULL,
  subject_id          TEXT NOT NULL,
  capability          TEXT NOT NULL,
  resource_type       TEXT NOT NULL DEFAULT '',
  resource_id         TEXT NOT NULL DEFAULT '',
  constraints_json    TEXT NOT NULL DEFAULT '{}',
  granted_by_actor_id TEXT,
  created_at          TEXT NOT NULL,
  expires_at          TEXT,
  revoked_at          TEXT,
  FOREIGN KEY(granted_by_actor_id) REFERENCES actors(id)
);
CREATE INDEX IF NOT EXISTS idx_capability_grants_subject
  ON capability_grants(subject_type, subject_id, capability);
CREATE INDEX IF NOT EXISTS idx_capability_grants_capability_resource
  ON capability_grants(capability, resource_type, resource_id);
CREATE INDEX IF NOT EXISTS idx_capability_grants_revoked
  ON capability_grants(revoked_at, expires_at);

CREATE TABLE IF NOT EXISTS authz_decisions (
  id            TEXT PRIMARY KEY,
  actor_id      TEXT NOT NULL,
  capability    TEXT NOT NULL,
  resource_type TEXT NOT NULL DEFAULT '',
  resource_id   TEXT NOT NULL DEFAULT '',
  decision      TEXT NOT NULL,
  reason        TEXT NOT NULL DEFAULT '',
  run_id        TEXT NOT NULL DEFAULT '',
  event_id      TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL,
  FOREIGN KEY(actor_id) REFERENCES actors(id)
);
CREATE INDEX IF NOT EXISTS idx_authz_decisions_actor_created
  ON authz_decisions(actor_id, created_at);
CREATE INDEX IF NOT EXISTS idx_authz_decisions_run_created
  ON authz_decisions(run_id, created_at);
CREATE INDEX IF NOT EXISTS idx_authz_decisions_capability_created
  ON authz_decisions(capability, created_at);

CREATE TABLE IF NOT EXISTS swarm_runs (
  id           TEXT PRIMARY KEY,
  goal         TEXT NOT NULL,
  status       TEXT NOT NULL,
  created_by   TEXT NOT NULL,
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL,
  completed_at TEXT,
  last_error   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS swarm_tasks (
  id                TEXT PRIMARY KEY,
  run_id            TEXT NOT NULL,
  parent_id         TEXT NOT NULL DEFAULT '',
  role              TEXT NOT NULL,
  subject           TEXT NOT NULL,
  prompt            TEXT NOT NULL,
  tool_allowlist    TEXT NOT NULL DEFAULT '[]',
  status            TEXT NOT NULL,
  depth             INTEGER NOT NULL DEFAULT 0,
  attempts          INTEGER NOT NULL DEFAULT 0,
  blocked_by        TEXT NOT NULL DEFAULT '[]',
  result            TEXT NOT NULL DEFAULT '',
  tool_calls        INTEGER NOT NULL DEFAULT 0,
  llm_calls         INTEGER NOT NULL DEFAULT 0,
  tokens_prompt     INTEGER NOT NULL DEFAULT 0,
  tokens_completion INTEGER NOT NULL DEFAULT 0,
  tokens_total      INTEGER NOT NULL DEFAULT 0,
  elapsed_ms        INTEGER NOT NULL DEFAULT 0,
  created_at        TEXT NOT NULL,
  started_at        TEXT,
  completed_at      TEXT,
  last_error        TEXT NOT NULL DEFAULT '',
  FOREIGN KEY(run_id) REFERENCES swarm_runs(id)
);
CREATE INDEX IF NOT EXISTS idx_swarm_tasks_run ON swarm_tasks(run_id, status, created_at);
CREATE INDEX IF NOT EXISTS idx_swarm_tasks_status ON swarm_tasks(status, created_at);

CREATE TABLE IF NOT EXISTS runs (
  id                 TEXT PRIMARY KEY,
  parent_run_id      TEXT NOT NULL DEFAULT '',
  thread_id          TEXT NOT NULL DEFAULT '',
  principal_id       TEXT NOT NULL DEFAULT '',
  actor_id           TEXT NOT NULL DEFAULT '',
  channel            TEXT NOT NULL,
  status             TEXT NOT NULL,
  model              TEXT NOT NULL DEFAULT '',
  started_at         TEXT NOT NULL,
  updated_at         TEXT NOT NULL,
  completed_at       TEXT,
  cancelled_at       TEXT,
  last_error         TEXT NOT NULL DEFAULT '',
  current_seq        INTEGER NOT NULL DEFAULT 0,
  idempotency_key    TEXT NOT NULL DEFAULT '',
  correlation_id     TEXT NOT NULL DEFAULT '',
  trace_id           TEXT NOT NULL DEFAULT '',
  span_id            TEXT NOT NULL DEFAULT '',
  final_text_preview TEXT NOT NULL DEFAULT '',
  stats_json         TEXT NOT NULL DEFAULT '{}',
  metadata_json      TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_runs_status_updated
  ON runs(status, updated_at);
CREATE INDEX IF NOT EXISTS idx_runs_thread_updated
  ON runs(thread_id, updated_at);
CREATE INDEX IF NOT EXISTS idx_runs_actor_updated
  ON runs(actor_id, updated_at);
CREATE INDEX IF NOT EXISTS idx_runs_parent
  ON runs(parent_run_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_runs_idempotency
  ON runs(idempotency_key)
  WHERE idempotency_key <> '';

CREATE TABLE IF NOT EXISTS run_events (
  id              TEXT PRIMARY KEY,
  run_id          TEXT NOT NULL,
  parent_run_id   TEXT NOT NULL DEFAULT '',
  seq             INTEGER NOT NULL,
  type            TEXT NOT NULL,
  schema_version  INTEGER NOT NULL DEFAULT 1,
  actor_id        TEXT NOT NULL DEFAULT '',
  causation_id    TEXT NOT NULL DEFAULT '',
  correlation_id  TEXT NOT NULL DEFAULT '',
  idempotency_key TEXT NOT NULL DEFAULT '',
  payload_json    TEXT NOT NULL DEFAULT '{}',
  redaction_level TEXT NOT NULL DEFAULT 'metadata',
  created_at      TEXT NOT NULL,
  FOREIGN KEY(run_id) REFERENCES runs(id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_run_events_run_seq
  ON run_events(run_id, seq);
CREATE INDEX IF NOT EXISTS idx_run_events_type_created
  ON run_events(type, created_at);
CREATE INDEX IF NOT EXISTS idx_run_events_correlation
  ON run_events(correlation_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_run_events_idempotency
  ON run_events(idempotency_key)
  WHERE idempotency_key <> '';

CREATE TABLE IF NOT EXISTS chat_questions (
  id                  TEXT PRIMARY KEY,
  run_id              TEXT NOT NULL,
  event_id            TEXT NOT NULL,
  answer_run_id       TEXT NOT NULL DEFAULT '',
  answer_event_id     TEXT NOT NULL DEFAULT '',
  thread_id           TEXT NOT NULL DEFAULT '',
  actor_id            TEXT NOT NULL DEFAULT '',
  channel             TEXT NOT NULL DEFAULT '',
  kind                TEXT NOT NULL DEFAULT 'clarification',
  status              TEXT NOT NULL DEFAULT 'waiting',
  question_text       TEXT NOT NULL DEFAULT '',
  options_json        TEXT NOT NULL DEFAULT '[]',
  answer_preview      TEXT NOT NULL DEFAULT '',
  answer_json         TEXT NOT NULL DEFAULT '{}',
  requested_at        TEXT NOT NULL,
  answered_at         TEXT,
  expires_at          TEXT,
  producer_json       TEXT NOT NULL DEFAULT '{}',
  metadata_json       TEXT NOT NULL DEFAULT '{}',
  FOREIGN KEY(run_id) REFERENCES runs(id),
  FOREIGN KEY(event_id) REFERENCES run_events(id)
);
CREATE INDEX IF NOT EXISTS idx_chat_questions_thread_status
  ON chat_questions(thread_id, channel, status, requested_at);
CREATE INDEX IF NOT EXISTS idx_chat_questions_run
  ON chat_questions(run_id, status, requested_at);

CREATE TABLE IF NOT EXISTS run_outbox (
  id              TEXT PRIMARY KEY,
  run_id          TEXT NOT NULL,
  event_id        TEXT NOT NULL DEFAULT '',
  target          TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  payload_json    TEXT NOT NULL DEFAULT '{}',
  status          TEXT NOT NULL,
  attempts        INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TEXT,
  last_error      TEXT NOT NULL DEFAULT '',
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL,
  FOREIGN KEY(run_id) REFERENCES runs(id)
);
CREATE INDEX IF NOT EXISTS idx_run_outbox_status_next
  ON run_outbox(status, next_attempt_at, created_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_run_outbox_target_idempotency
  ON run_outbox(target, idempotency_key);

CREATE TABLE IF NOT EXISTS run_idempotency_keys (
  scope      TEXT NOT NULL,
  key        TEXT NOT NULL,
  run_id     TEXT NOT NULL,
  event_id   TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  PRIMARY KEY(scope, key),
  FOREIGN KEY(run_id) REFERENCES runs(id)
);
CREATE INDEX IF NOT EXISTS idx_run_idempotency_run
  ON run_idempotency_keys(run_id);

CREATE TABLE IF NOT EXISTS audit_events (
  id              TEXT PRIMARY KEY,
  run_id          TEXT NOT NULL DEFAULT '',
  event_id        TEXT NOT NULL DEFAULT '',
  type            TEXT NOT NULL,
  actor_id        TEXT NOT NULL DEFAULT '',
  target_type     TEXT NOT NULL DEFAULT '',
  target_id       TEXT NOT NULL DEFAULT '',
  payload_json    TEXT NOT NULL DEFAULT '{}',
  redaction_level TEXT NOT NULL DEFAULT 'metadata',
  created_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_events_type_created
  ON audit_events(type, created_at);
CREATE INDEX IF NOT EXISTS idx_audit_events_actor_created
  ON audit_events(actor_id, created_at);

CREATE TABLE IF NOT EXISTS projection_state (
  projection_id        TEXT PRIMARY KEY,
  kind                 TEXT NOT NULL,
  embedding_model_id   TEXT NOT NULL DEFAULT '',
  embedding_dim        INTEGER NOT NULL DEFAULT 0,
  index_build_id       TEXT NOT NULL DEFAULT '',
  schema_version       INTEGER NOT NULL DEFAULT 1,
  last_full_rebuild_at INTEGER NOT NULL DEFAULT 0,
  last_incremental_at  INTEGER,
  pending_count        INTEGER NOT NULL DEFAULT 0,
  completed_count      INTEGER NOT NULL DEFAULT 0,
  failed_count         INTEGER NOT NULL DEFAULT 0,
  status               TEXT NOT NULL DEFAULT 'fresh',
  health_reason        TEXT NOT NULL DEFAULT '',
  version              INTEGER NOT NULL DEFAULT 1,
  updated_at           INTEGER NOT NULL DEFAULT (unixepoch())
);
`

// Registered returns the migrations known to the runner.
func Registered() []Migration {
	out := make([]Migration, len(registered))
	copy(out, registered)
	return out
}

// Run applies all registered migrations that have not yet been recorded.
func Run(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("migrations: nil db")
	}
	if err := validateRegistered(registered); err != nil {
		return err
	}
	if err := ensureMigrationTable(ctx, db); err != nil {
		return err
	}
	applied, err := appliedMap(ctx, db)
	if err != nil {
		return err
	}
	for _, migration := range registered {
		if applied[migration.Version] {
			continue
		}
		if err := runOne(ctx, db, migration); err != nil {
			return err
		}
	}
	return nil
}

func validateRegistered(migrations []Migration) error {
	seen := make(map[int]struct{}, len(migrations))
	lastVersion := 0
	for i, migration := range migrations {
		if migration.Version <= 0 {
			return fmt.Errorf("migrations: migration %d has non-positive version %d", i, migration.Version)
		}
		if migration.Name == "" {
			return fmt.Errorf("migrations: migration %d has empty name", migration.Version)
		}
		if migration.Up == nil {
			return fmt.Errorf("migrations: migration %d has nil Up", migration.Version)
		}
		if _, ok := seen[migration.Version]; ok {
			return fmt.Errorf("migrations: duplicate version %d", migration.Version)
		}
		if migration.Version <= lastVersion {
			return fmt.Errorf("migrations: versions must be strictly increasing")
		}
		seen[migration.Version] = struct{}{}
		lastVersion = migration.Version
	}
	return nil
}

func ensureMigrationTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  applied_at TEXT NOT NULL
);`)
	if err != nil {
		return fmt.Errorf("migrations: ensure schema_migrations: %w", err)
	}
	return nil
}

func appliedMap(ctx context.Context, db *sql.DB) (map[int]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("migrations: read applied versions: %w", err)
	}
	defer rows.Close()

	applied := map[int]bool{}
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("migrations: scan applied version: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("migrations: iterate applied versions: %w", err)
	}
	return applied, nil
}

func runOne(ctx context.Context, db *sql.DB, migration Migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migrations: begin migration %d: %w", migration.Version, err)
	}
	defer tx.Rollback()

	if err := migration.Up(ctx, tx); err != nil {
		return fmt.Errorf("migrations: apply migration %d %q: %w", migration.Version, migration.Name, err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		migration.Version,
		migration.Name,
		time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("migrations: record migration %d: %w", migration.Version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrations: commit migration %d: %w", migration.Version, err)
	}
	return nil
}

func createCurrentSchema(ctx context.Context, tx *sql.Tx) error {
	if err := dropLegacyConversationsWithoutChatID(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, currentSchemaSQL); err != nil {
		return fmt.Errorf("migrations: create current schema: %w", err)
	}
	return nil
}

func dropLegacyConversationsWithoutChatID(ctx context.Context, tx *sql.Tx) error {
	cols, err := txTableColumns(ctx, tx, "conversations")
	if err != nil {
		return err
	}
	if len(cols) == 0 {
		return nil
	}
	if _, ok := cols["chat_id"]; ok {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE conversations`); err != nil {
		return fmt.Errorf("migrations: drop retired conversations table: %w", err)
	}
	return nil
}

func backfillCurrentColumns(ctx context.Context, tx *sql.Tx) error {
	if err := addMissingColumns(ctx, tx, "scheduled_tasks", []columnDef{
		{Name: "schedule_weekdays", SQL: "TEXT NOT NULL DEFAULT ''"},
		{Name: "schedule_every_minutes", SQL: "INTEGER NOT NULL DEFAULT 0"},
		{Name: "last_output", SQL: "TEXT NOT NULL DEFAULT ''"},
		{Name: "last_metrics_json", SQL: "TEXT NOT NULL DEFAULT ''"},
		{Name: "wake_signature", SQL: "TEXT NOT NULL DEFAULT ''"},
	}); err != nil {
		return err
	}
	if err := addMissingColumns(ctx, tx, "proposed_updates", []columnDef{
		{Name: "category", SQL: "TEXT NOT NULL DEFAULT ''"},
		{Name: "related_slugs", SQL: "TEXT NOT NULL DEFAULT ''"},
		{Name: "provenance_json", SQL: "TEXT NOT NULL DEFAULT '{}'"},
	}); err != nil {
		return err
	}
	if err := addMissingColumns(ctx, tx, "swarm_tasks", []columnDef{
		{Name: "tokens_prompt", SQL: "INTEGER NOT NULL DEFAULT 0"},
		{Name: "tokens_completion", SQL: "INTEGER NOT NULL DEFAULT 0"},
		{Name: "tokens_total", SQL: "INTEGER NOT NULL DEFAULT 0"},
	}); err != nil {
		return err
	}
	return nil
}

func addAPITokenExpiry(ctx context.Context, tx *sql.Tx) error {
	if err := addMissingColumns(ctx, tx, "api_tokens", []columnDef{
		{Name: "expires_at", SQL: "TEXT"},
	}); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT token_hash, issued_at FROM api_tokens WHERE expires_at IS NULL OR expires_at = ''`)
	if err != nil {
		return fmt.Errorf("migrations: query api token expiry backfill: %w", err)
	}
	defer rows.Close()

	type tokenExpiry struct {
		hash      string
		expiresAt string
	}
	var updates []tokenExpiry
	for rows.Next() {
		var hash, issuedAtRaw string
		if err := rows.Scan(&hash, &issuedAtRaw); err != nil {
			return fmt.Errorf("migrations: scan api token expiry backfill: %w", err)
		}
		issuedAt, err := parseStoredTime(issuedAtRaw)
		if err != nil {
			return fmt.Errorf("migrations: parse api_tokens.issued_at %q: %w", issuedAtRaw, err)
		}
		updates = append(updates, tokenExpiry{
			hash:      hash,
			expiresAt: issuedAt.Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339),
		})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("migrations: iterate api token expiry backfill: %w", err)
	}
	for _, update := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE api_tokens SET expires_at = ? WHERE token_hash = ?`, update.expiresAt, update.hash); err != nil {
			return fmt.Errorf("migrations: update api token expiry: %w", err)
		}
	}
	return nil
}

// addToolIndexState creates the tool_index_state table used by
// internal/toolindex.Reconciler to diff the live tool registry against the
// indexed set in Qdrant. Qdrant has no scroll API, so this table is the
// authoritative "what is currently indexed" set; reconcile reads it,
// computes the upsert/delete buckets, and writes back atomically.
func addToolIndexState(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS tool_index_state (
  tool_name    TEXT PRIMARY KEY,
  content_hash TEXT NOT NULL,
  point_id     TEXT NOT NULL,
  embed_model  TEXT NOT NULL,
  indexed_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tool_index_state_hash
  ON tool_index_state(content_hash);
`)
	if err != nil {
		return fmt.Errorf("migrations: add tool_index_state: %w", err)
	}
	return nil
}

func addRunEventFoundation(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS runs (
  id                 TEXT PRIMARY KEY,
  parent_run_id      TEXT NOT NULL DEFAULT '',
  thread_id          TEXT NOT NULL DEFAULT '',
  principal_id       TEXT NOT NULL DEFAULT '',
  channel            TEXT NOT NULL,
  status             TEXT NOT NULL,
  model              TEXT NOT NULL DEFAULT '',
  started_at         TEXT NOT NULL,
  updated_at         TEXT NOT NULL,
  completed_at       TEXT,
  cancelled_at       TEXT,
  last_error         TEXT NOT NULL DEFAULT '',
  current_seq        INTEGER NOT NULL DEFAULT 0,
  idempotency_key    TEXT NOT NULL DEFAULT '',
  correlation_id     TEXT NOT NULL DEFAULT '',
  trace_id           TEXT NOT NULL DEFAULT '',
  span_id            TEXT NOT NULL DEFAULT '',
  final_text_preview TEXT NOT NULL DEFAULT '',
  stats_json         TEXT NOT NULL DEFAULT '{}',
  metadata_json      TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_runs_status_updated
  ON runs(status, updated_at);
CREATE INDEX IF NOT EXISTS idx_runs_thread_updated
  ON runs(thread_id, updated_at);
CREATE INDEX IF NOT EXISTS idx_runs_parent
  ON runs(parent_run_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_runs_idempotency
  ON runs(idempotency_key)
  WHERE idempotency_key <> '';

CREATE TABLE IF NOT EXISTS run_events (
  id              TEXT PRIMARY KEY,
  run_id          TEXT NOT NULL,
  parent_run_id   TEXT NOT NULL DEFAULT '',
  seq             INTEGER NOT NULL,
  type            TEXT NOT NULL,
  schema_version  INTEGER NOT NULL DEFAULT 1,
  actor_id        TEXT NOT NULL DEFAULT '',
  causation_id    TEXT NOT NULL DEFAULT '',
  correlation_id  TEXT NOT NULL DEFAULT '',
  idempotency_key TEXT NOT NULL DEFAULT '',
  payload_json    TEXT NOT NULL DEFAULT '{}',
  redaction_level TEXT NOT NULL DEFAULT 'metadata',
  created_at      TEXT NOT NULL,
  FOREIGN KEY(run_id) REFERENCES runs(id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_run_events_run_seq
  ON run_events(run_id, seq);
CREATE INDEX IF NOT EXISTS idx_run_events_type_created
  ON run_events(type, created_at);
CREATE INDEX IF NOT EXISTS idx_run_events_correlation
  ON run_events(correlation_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_run_events_idempotency
  ON run_events(idempotency_key)
  WHERE idempotency_key <> '';

CREATE TABLE IF NOT EXISTS run_outbox (
  id              TEXT PRIMARY KEY,
  run_id          TEXT NOT NULL,
  event_id        TEXT NOT NULL DEFAULT '',
  target          TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  payload_json    TEXT NOT NULL DEFAULT '{}',
  status          TEXT NOT NULL,
  attempts        INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TEXT,
  last_error      TEXT NOT NULL DEFAULT '',
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL,
  FOREIGN KEY(run_id) REFERENCES runs(id)
);
CREATE INDEX IF NOT EXISTS idx_run_outbox_status_next
  ON run_outbox(status, next_attempt_at, created_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_run_outbox_target_idempotency
  ON run_outbox(target, idempotency_key);

CREATE TABLE IF NOT EXISTS run_idempotency_keys (
  scope      TEXT NOT NULL,
  key        TEXT NOT NULL,
  run_id     TEXT NOT NULL,
  event_id   TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  PRIMARY KEY(scope, key),
  FOREIGN KEY(run_id) REFERENCES runs(id)
);
CREATE INDEX IF NOT EXISTS idx_run_idempotency_run
  ON run_idempotency_keys(run_id);

CREATE TABLE IF NOT EXISTS audit_events (
  id              TEXT PRIMARY KEY,
  run_id          TEXT NOT NULL DEFAULT '',
  event_id        TEXT NOT NULL DEFAULT '',
  type            TEXT NOT NULL,
  actor_id        TEXT NOT NULL DEFAULT '',
  target_type     TEXT NOT NULL DEFAULT '',
  target_id       TEXT NOT NULL DEFAULT '',
  payload_json    TEXT NOT NULL DEFAULT '{}',
  redaction_level TEXT NOT NULL DEFAULT 'metadata',
  created_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_events_type_created
  ON audit_events(type, created_at);
CREATE INDEX IF NOT EXISTS idx_audit_events_actor_created
  ON audit_events(actor_id, created_at);
`)
	if err != nil {
		return fmt.Errorf("migrations: add run event foundation: %w", err)
	}
	return nil
}

func addIdentityCapabilityGrants(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS principals (
  id            TEXT PRIMARY KEY,
  kind          TEXT NOT NULL,
  display_name  TEXT NOT NULL DEFAULT '',
  status        TEXT NOT NULL DEFAULT 'active',
  created_at    TEXT NOT NULL,
  metadata_json TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_principals_kind_status
  ON principals(kind, status);

CREATE TABLE IF NOT EXISTS channel_accounts (
  id            TEXT PRIMARY KEY,
  principal_id  TEXT NOT NULL,
  provider      TEXT NOT NULL,
  external_id   TEXT NOT NULL,
  display_name  TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL,
  last_seen_at  TEXT,
  metadata_json TEXT NOT NULL DEFAULT '{}',
  UNIQUE(provider, external_id),
  FOREIGN KEY(principal_id) REFERENCES principals(id)
);
CREATE INDEX IF NOT EXISTS idx_channel_accounts_principal
  ON channel_accounts(principal_id);

CREATE TABLE IF NOT EXISTS actors (
  id                 TEXT PRIMARY KEY,
  principal_id       TEXT NOT NULL,
  actor_type         TEXT NOT NULL,
  parent_actor_id    TEXT,
  channel_account_id TEXT,
  run_id             TEXT NOT NULL DEFAULT '',
  created_at         TEXT NOT NULL,
  expires_at         TEXT,
  metadata_json      TEXT NOT NULL DEFAULT '{}',
  FOREIGN KEY(principal_id) REFERENCES principals(id),
  FOREIGN KEY(parent_actor_id) REFERENCES actors(id),
  FOREIGN KEY(channel_account_id) REFERENCES channel_accounts(id)
);
CREATE INDEX IF NOT EXISTS idx_actors_principal_created
  ON actors(principal_id, created_at);
CREATE INDEX IF NOT EXISTS idx_actors_parent
  ON actors(parent_actor_id);
CREATE INDEX IF NOT EXISTS idx_actors_run
  ON actors(run_id);

CREATE TABLE IF NOT EXISTS capability_grants (
  id                  TEXT PRIMARY KEY,
  subject_type        TEXT NOT NULL,
  subject_id          TEXT NOT NULL,
  capability          TEXT NOT NULL,
  resource_type       TEXT NOT NULL DEFAULT '',
  resource_id         TEXT NOT NULL DEFAULT '',
  constraints_json    TEXT NOT NULL DEFAULT '{}',
  granted_by_actor_id TEXT,
  created_at          TEXT NOT NULL,
  expires_at          TEXT,
  revoked_at          TEXT,
  FOREIGN KEY(granted_by_actor_id) REFERENCES actors(id)
);
CREATE INDEX IF NOT EXISTS idx_capability_grants_subject
  ON capability_grants(subject_type, subject_id, capability);
CREATE INDEX IF NOT EXISTS idx_capability_grants_capability_resource
  ON capability_grants(capability, resource_type, resource_id);
CREATE INDEX IF NOT EXISTS idx_capability_grants_revoked
  ON capability_grants(revoked_at, expires_at);

CREATE TABLE IF NOT EXISTS authz_decisions (
  id            TEXT PRIMARY KEY,
  actor_id      TEXT NOT NULL,
  capability    TEXT NOT NULL,
  resource_type TEXT NOT NULL DEFAULT '',
  resource_id   TEXT NOT NULL DEFAULT '',
  decision      TEXT NOT NULL,
  reason        TEXT NOT NULL DEFAULT '',
  run_id        TEXT NOT NULL DEFAULT '',
  event_id      TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL,
  FOREIGN KEY(actor_id) REFERENCES actors(id)
);
CREATE INDEX IF NOT EXISTS idx_authz_decisions_actor_created
  ON authz_decisions(actor_id, created_at);
CREATE INDEX IF NOT EXISTS idx_authz_decisions_run_created
  ON authz_decisions(run_id, created_at);
CREATE INDEX IF NOT EXISTS idx_authz_decisions_capability_created
  ON authz_decisions(capability, created_at);
`)
	if err != nil {
		return fmt.Errorf("migrations: add identity capability grants: %w", err)
	}
	if err := addMissingColumns(ctx, tx, "runs", []columnDef{
		{Name: "actor_id", SQL: "TEXT NOT NULL DEFAULT ''"},
	}); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_runs_actor_updated ON runs(actor_id, updated_at)`); err != nil {
		return fmt.Errorf("migrations: add runs actor index: %w", err)
	}
	return nil
}

func backfillAllowedUsersIdentity(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
WITH allowlisted AS (
  SELECT
    user_id,
    source,
    created_at,
    CASE
      WHEN source IN ('telegram_bootstrap', 'manual') THEN 'owner'
      ELSE 'human'
    END AS principal_kind
  FROM allowed_users
  WHERE source <> 'e2e_bootstrap'
)
INSERT OR IGNORE INTO principals (
  id, kind, display_name, status, created_at, metadata_json
)
SELECT
  'principal:telegram:' || user_id,
  principal_kind,
  'Telegram ' || user_id,
  'active',
  created_at,
  '{}'
FROM allowlisted;

WITH allowlisted AS (
  SELECT user_id, created_at
  FROM allowed_users
  WHERE source <> 'e2e_bootstrap'
)
INSERT OR IGNORE INTO channel_accounts (
  id, principal_id, provider, external_id, display_name, created_at,
  metadata_json
)
SELECT
  'acct:telegram:' || user_id,
  'principal:telegram:' || user_id,
  'telegram',
  user_id,
  'Telegram ' || user_id,
  created_at,
  '{}'
FROM allowlisted;

WITH allowlisted AS (
  SELECT user_id, created_at
  FROM allowed_users
  WHERE source <> 'e2e_bootstrap'
)
INSERT OR IGNORE INTO actors (
  id, principal_id, actor_type, channel_account_id, run_id, created_at,
  metadata_json
)
SELECT
  'actor:telegram:session:' || user_id,
  'principal:telegram:' || user_id,
  'session',
  'acct:telegram:' || user_id,
  '',
  created_at,
  '{}'
FROM allowlisted;

WITH allowlisted AS (
  SELECT
    user_id,
    source,
    created_at,
    CASE
      WHEN source IN ('telegram_bootstrap', 'manual') THEN 1
      ELSE 0
    END AS is_owner
  FROM allowed_users
  WHERE source <> 'e2e_bootstrap'
),
caps(capability, owner_only) AS (
  VALUES
    ('api.chat', 0),
    ('dashboard.read', 0),
    ('dashboard.write', 0),
    ('tool.execute', 0),
    ('memory.user.write', 0),
    ('wiki.write', 0),
    ('skills.install', 1),
    ('skills.delete', 1),
    ('settings.write', 1),
    ('cron.create', 1),
    ('cron.run', 1),
    ('swarm.spawn', 1)
)
INSERT OR IGNORE INTO capability_grants (
  id, subject_type, subject_id, capability, resource_type, resource_id,
  constraints_json, granted_by_actor_id, created_at
)
SELECT
  'grant:telegram:' || allowlisted.user_id || ':' || caps.capability,
  'principal',
  'principal:telegram:' || allowlisted.user_id,
  caps.capability,
  '',
  '',
  '{}',
  NULL,
  allowlisted.created_at
FROM allowlisted
JOIN caps
WHERE caps.owner_only = 0 OR allowlisted.is_owner = 1;
`)
	if err != nil {
		return fmt.Errorf("migrations: backfill allowed users identity: %w", err)
	}
	return nil
}

func addCompactMemoryIndex(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS compact_memory_documents (
  id              TEXT PRIMARY KEY,
  kind            TEXT NOT NULL,
  title           TEXT NOT NULL DEFAULT '',
  body            TEXT NOT NULL,
  handle          TEXT NOT NULL DEFAULT '',
  source_id       TEXT NOT NULL DEFAULT '',
  page            INTEGER NOT NULL DEFAULT 0,
  chat_id         INTEGER NOT NULL DEFAULT 0,
  conversation_id INTEGER NOT NULL DEFAULT 0,
  proposal_id     INTEGER NOT NULL DEFAULT 0,
  status          TEXT NOT NULL DEFAULT '',
  entities_json   TEXT NOT NULL DEFAULT '[]',
  tags_json       TEXT NOT NULL DEFAULT '[]',
  updated_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_compact_memory_kind
  ON compact_memory_documents(kind, updated_at);
CREATE INDEX IF NOT EXISTS idx_compact_memory_chat
  ON compact_memory_documents(kind, chat_id, updated_at);
CREATE INDEX IF NOT EXISTS idx_compact_memory_source
  ON compact_memory_documents(source_id);
CREATE INDEX IF NOT EXISTS idx_compact_memory_proposal
  ON compact_memory_documents(proposal_id);
CREATE VIRTUAL TABLE IF NOT EXISTS compact_memory_fts
USING fts5(id UNINDEXED, kind, title, body, handle, source_id, status, entities, tags);
`)
	if err != nil {
		return fmt.Errorf("migrations: add compact memory index: %w", err)
	}
	return nil
}

func addToolAttempts(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS tool_attempts (
  id               TEXT PRIMARY KEY,
  run_id           TEXT NOT NULL,
  tool_call_id     TEXT NOT NULL DEFAULT '',
  attempt_n        INTEGER NOT NULL DEFAULT 1,
  tool_name        TEXT NOT NULL,
  tool_kind        TEXT NOT NULL DEFAULT 'native',
  tool_schema_hash TEXT NOT NULL DEFAULT '',
  outcome          TEXT NOT NULL,
  class            TEXT NOT NULL DEFAULT '',
  reason           TEXT NOT NULL DEFAULT '',
  args_hash        TEXT NOT NULL DEFAULT '',
  arg_keys_json    TEXT NOT NULL DEFAULT '[]',
  error_redacted   TEXT NOT NULL DEFAULT '',
  elapsed_ms       INTEGER NOT NULL DEFAULT 0,
  started_at       TEXT NOT NULL,
  ended_at         TEXT NOT NULL,
  FOREIGN KEY(run_id) REFERENCES runs(id)
);
CREATE INDEX IF NOT EXISTS idx_tool_attempts_run_tool
  ON tool_attempts(run_id, tool_name, ended_at);
CREATE INDEX IF NOT EXISTS idx_tool_attempts_tool_outcome
  ON tool_attempts(tool_name, outcome, ended_at);
CREATE INDEX IF NOT EXISTS idx_tool_attempts_run_outcome
  ON tool_attempts(run_id, outcome);
CREATE INDEX IF NOT EXISTS idx_tool_attempts_signature
  ON tool_attempts(tool_name, args_hash, outcome);

CREATE VIEW IF NOT EXISTS tool_warnings AS
  SELECT tool_name, class, COUNT(*) AS n, MAX(ended_at) AS last_seen,
         SUM(CASE WHEN outcome='recoverable' THEN 1 ELSE 0 END) AS n_recoverable,
         SUM(CASE WHEN outcome='blocked'     THEN 1 ELSE 0 END) AS n_blocked,
         SUM(CASE WHEN outcome='fatal'       THEN 1 ELSE 0 END) AS n_fatal
  FROM tool_attempts
  WHERE outcome IN ('recoverable','blocked','fatal')
    AND ended_at > datetime('now','-7 days')
  GROUP BY tool_name, class;
`)
	if err != nil {
		return fmt.Errorf("migrations: add tool_attempts: %w", err)
	}
	return nil
}

func addChatQuestions(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS chat_questions (
  id                  TEXT PRIMARY KEY,
  run_id              TEXT NOT NULL,
  event_id            TEXT NOT NULL,
  answer_run_id       TEXT NOT NULL DEFAULT '',
  answer_event_id     TEXT NOT NULL DEFAULT '',
  thread_id           TEXT NOT NULL DEFAULT '',
  actor_id            TEXT NOT NULL DEFAULT '',
  channel             TEXT NOT NULL DEFAULT '',
  kind                TEXT NOT NULL DEFAULT 'clarification',
  status              TEXT NOT NULL DEFAULT 'waiting',
  question_text       TEXT NOT NULL DEFAULT '',
  options_json        TEXT NOT NULL DEFAULT '[]',
  answer_preview      TEXT NOT NULL DEFAULT '',
  answer_json         TEXT NOT NULL DEFAULT '{}',
  requested_at        TEXT NOT NULL,
  answered_at         TEXT,
  expires_at          TEXT,
  producer_json       TEXT NOT NULL DEFAULT '{}',
  metadata_json       TEXT NOT NULL DEFAULT '{}',
  FOREIGN KEY(run_id) REFERENCES runs(id),
  FOREIGN KEY(event_id) REFERENCES run_events(id)
);
CREATE INDEX IF NOT EXISTS idx_chat_questions_thread_status
  ON chat_questions(thread_id, channel, status, requested_at);
CREATE INDEX IF NOT EXISTS idx_chat_questions_run
  ON chat_questions(run_id, status, requested_at);
`)
	if err != nil {
		return fmt.Errorf("migrations: add chat questions: %w", err)
	}
	return nil
}

func addSecretsTable(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS secrets (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
`)
	if err != nil {
		return fmt.Errorf("migrations: add secrets table: %w", err)
	}
	return nil
}

func addProjectionState(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS projection_state (
  projection_id        TEXT PRIMARY KEY,
  kind                 TEXT NOT NULL,
  embedding_model_id   TEXT NOT NULL DEFAULT '',
  embedding_dim        INTEGER NOT NULL DEFAULT 0,
  index_build_id       TEXT NOT NULL DEFAULT '',
  schema_version       INTEGER NOT NULL DEFAULT 1,
  last_full_rebuild_at INTEGER NOT NULL DEFAULT 0,
  last_incremental_at  INTEGER,
  pending_count        INTEGER NOT NULL DEFAULT 0,
  completed_count      INTEGER NOT NULL DEFAULT 0,
  failed_count         INTEGER NOT NULL DEFAULT 0,
  status               TEXT NOT NULL DEFAULT 'fresh',
  health_reason        TEXT NOT NULL DEFAULT '',
  version              INTEGER NOT NULL DEFAULT 1,
  updated_at           INTEGER NOT NULL DEFAULT (unixepoch())
);
`)
	if err != nil {
		return fmt.Errorf("migrations: add projection_state: %w", err)
	}
	return nil
}

func addCompactMemoryFreshnessColumns(ctx context.Context, tx *sql.Tx) error {
	return addMissingColumns(ctx, tx, "compact_memory_documents", []columnDef{
		{Name: "content_hash", SQL: "TEXT NOT NULL DEFAULT ''"},
		{Name: "embedding_model_id", SQL: "TEXT NOT NULL DEFAULT ''"},
		{Name: "index_build_id", SQL: "TEXT NOT NULL DEFAULT ''"},
	})
}

func addOperationalMemoryProposalColumns(ctx context.Context, tx *sql.Tx) error {
	return addMissingColumns(ctx, tx, "proposed_updates", []columnDef{
		{Name: "kind", SQL: "TEXT NOT NULL DEFAULT 'wiki'"},
		{Name: "signature_hash", SQL: "TEXT NOT NULL DEFAULT ''"},
	})
}

func parseStoredTime(raw string) (time.Time, error) {
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts, nil
	}
	if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return ts, nil
	}
	if ts, err := time.Parse("2006-01-02 15:04:05", raw); err == nil {
		return ts.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unsupported timestamp")
}

func addMissingColumns(ctx context.Context, tx *sql.Tx, table string, columns []columnDef) error {
	existing, err := txTableColumns(ctx, tx, table)
	if err != nil {
		return err
	}
	for _, column := range columns {
		if _, ok := existing[column.Name]; ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column.Name+` `+column.SQL); err != nil {
			return fmt.Errorf("migrations: add column %s.%s: %w", table, column.Name, err)
		}
	}
	return nil
}

func txTableColumns(ctx context.Context, tx *sql.Tx, table string) (map[string]struct{}, error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return nil, fmt.Errorf("migrations: table info %s: %w", table, err)
	}
	defer rows.Close()

	cols := map[string]struct{}{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, fmt.Errorf("migrations: scan table info %s: %w", table, err)
		}
		cols[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("migrations: iterate table info %s: %w", table, err)
	}
	return cols, nil
}
