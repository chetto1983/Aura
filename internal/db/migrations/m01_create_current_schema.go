package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

const currentSchemaSQL = `
CREATE TABLE IF NOT EXISTS settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
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
  channel           TEXT NOT NULL DEFAULT 'telegram',
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

CREATE TABLE IF NOT EXISTS conversation_compactions (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id            TEXT NOT NULL DEFAULT '',
  chat_id           INTEGER NOT NULL,
  turn_index        INTEGER NOT NULL,
  iteration         INTEGER NOT NULL DEFAULT 0,
  messages_before   INTEGER NOT NULL DEFAULT 0,
  messages_after    INTEGER NOT NULL DEFAULT 0,
  tokens_before     INTEGER NOT NULL DEFAULT 0,
  tokens_after      INTEGER NOT NULL DEFAULT 0,
  threshold_tokens  INTEGER NOT NULL DEFAULT 0,
  cumulative_tokens INTEGER NOT NULL DEFAULT 0,
  focus_preview     TEXT NOT NULL DEFAULT '',
  elapsed_ms        INTEGER NOT NULL DEFAULT 0,
  created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_conversation_compactions_turn
  ON conversation_compactions(chat_id, turn_index, created_at);
CREATE INDEX IF NOT EXISTS idx_conversation_compactions_run
  ON conversation_compactions(run_id, created_at);

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
  output_dim  INTEGER NOT NULL DEFAULT 0,
  embedding   BLOB NOT NULL,
  created_at  TIMESTAMP NOT NULL,
  PRIMARY KEY (content_sha, model, output_dim)
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
  chunk_index        INTEGER NOT NULL DEFAULT 0,
  byte_start         INTEGER NOT NULL DEFAULT 0,
  byte_end           INTEGER NOT NULL DEFAULT 0,
  chat_id            INTEGER NOT NULL DEFAULT 0,
  conversation_id    INTEGER NOT NULL DEFAULT 0,
  proposal_id        INTEGER NOT NULL DEFAULT 0,
  status             TEXT NOT NULL DEFAULT '',
  priority           TEXT NOT NULL DEFAULT 'normal',
  last_recalled_at   TEXT NOT NULL DEFAULT '',
  recall_count       INTEGER NOT NULL DEFAULT 0,
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
CREATE INDEX IF NOT EXISTS idx_compact_memory_priority
  ON compact_memory_documents(kind, priority, updated_at);
CREATE INDEX IF NOT EXISTS idx_compact_memory_recall_decay
  ON compact_memory_documents(kind, priority, status, last_recalled_at, updated_at);

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
  run_origin      TEXT NOT NULL DEFAULT 'user',
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

CREATE TABLE IF NOT EXISTS agent_notes (
  conversation_id TEXT PRIMARY KEY,
  content         TEXT NOT NULL DEFAULT '',
  updated_at      INTEGER NOT NULL DEFAULT (unixepoch())
);
`

func createCurrentSchema(ctx context.Context, tx *sql.Tx) error {
	if err := dropLegacyConversationsWithoutChatID(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, currentSchemaSQL); err != nil {
		return fmt.Errorf("migrations: create current schema: %w", err)
	}
	if err := ensureConversationChannelColumn(ctx, tx); err != nil {
		return err
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

func ensureConversationChannelColumn(ctx context.Context, tx *sql.Tx) error {
	cols, err := txTableColumns(ctx, tx, "conversations")
	if err != nil {
		return err
	}
	if _, ok := cols["channel"]; ok {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE conversations ADD COLUMN channel TEXT NOT NULL DEFAULT 'telegram'`); err != nil {
		return fmt.Errorf("migrations: add conversations.channel: %w", err)
	}
	return nil
}
