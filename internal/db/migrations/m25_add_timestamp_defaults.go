package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

// addTimestampDefaults adds DEFAULT CURRENT_TIMESTAMP to timestamp columns on
// operator-facing tables where a missing default forces callers to spell out the
// timestamp explicitly on every INSERT.  Triggered by the 2026-05-22 manual
// INSERT into settings failing with "NOT NULL constraint failed: settings.updated_at".
//
// Tables fixed here: settings, secrets, chat_settings.
//
// Audit — tables with NOT NULL timestamp columns and no DEFAULT that are
// scope-deferred (Go code always provides the value; no bare operator INSERT path):
//   api_tokens.issued_at, allowed_users.created_at, pending_users.requested_at,
//   scheduled_tasks.created_at/.updated_at, compact_memory_documents.updated_at,
//   principals.created_at, channel_accounts.created_at, actors.created_at,
//   capability_grants.created_at, authz_decisions.created_at,
//   swarm_runs.created_at/.updated_at, swarm_tasks.created_at,
//   runs.started_at/.updated_at, run_events.created_at, chat_questions.requested_at,
//   run_outbox.created_at/.updated_at, run_idempotency_keys.created_at,
//   audit_events.created_at, embedding_cache.created_at, tool_index_state.indexed_at,
//   tool_attempts.started_at/.ended_at, voice_dispatches.at.
func addTimestampDefaults(ctx context.Context, tx *sql.Tx) error {
	type tableSpec struct {
		name       string
		newDDL     string
		backupName string
	}
	tables := []tableSpec{
		{
			name:       "settings",
			backupName: "_settings_bak_mig25",
			newDDL: `CREATE TABLE settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`,
		},
		{
			name:       "secrets",
			backupName: "_secrets_bak_mig25",
			newDDL: `CREATE TABLE secrets (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`,
		},
		{
			name:       "chat_settings",
			backupName: "_chat_settings_bak_mig25",
			newDDL: `CREATE TABLE chat_settings (
  chat_id    TEXT PRIMARY KEY,
  voice_mode TEXT NOT NULL DEFAULT 'off' CHECK(voice_mode IN ('off','voice_only','all')),
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`,
		},
	}

	for _, spec := range tables {
		exists, err := txTableExists(ctx, tx, spec.name)
		if err != nil {
			return fmt.Errorf("migrations: check %s exists: %w", spec.name, err)
		}
		if !exists {
			continue
		}
		if _, err := tx.ExecContext(ctx, `ALTER TABLE `+spec.name+` RENAME TO `+spec.backupName); err != nil {
			return fmt.Errorf("migrations: rename %s: %w", spec.name, err)
		}
		if _, err := tx.ExecContext(ctx, spec.newDDL); err != nil {
			return fmt.Errorf("migrations: recreate %s: %w", spec.name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO `+spec.name+` SELECT * FROM `+spec.backupName); err != nil {
			return fmt.Errorf("migrations: copy %s rows: %w", spec.name, err)
		}
		if _, err := tx.ExecContext(ctx, `DROP TABLE `+spec.backupName); err != nil {
			return fmt.Errorf("migrations: drop %s backup: %w", spec.name, err)
		}
	}
	return nil
}
