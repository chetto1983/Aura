package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

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
