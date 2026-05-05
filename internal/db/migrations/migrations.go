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
	if _, err := tx.ExecContext(ctx, currentSchemaSQL); err != nil {
		return fmt.Errorf("migrations: create current schema: %w", err)
	}
	return nil
}
