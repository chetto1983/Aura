package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

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
