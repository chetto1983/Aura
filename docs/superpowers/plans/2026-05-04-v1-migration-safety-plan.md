# v1 Migration Safety Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add deterministic versioned SQLite migrations so fresh installs and upgraded `v3.0.2` databases converge to the same current schema, and production startup can rerun safely.

**Architecture:** Create a small `internal/db/migrations` package that owns schema DDL and records applied versions in `schema_migrations`. `cmd/aura/main.go` runs migrations immediately after `internal/db.Open` and before constructing settings, Telegram, auth, scheduler, search, or swarm stores. Domain stores keep explicit CRUD SQL, while production schema creation moves out of lazy constructors.

**Tech Stack:** Go, `database/sql`, `modernc.org/sqlite`, SQLite DDL, FTS5, existing `internal/db.Open`, PowerShell verification commands.

---

## Scope Check

This phase implements only migration safety:

- versioned migration runner;
- `schema_migrations` table with `version INTEGER PRIMARY KEY`, `name TEXT NOT NULL`, `applied_at TEXT NOT NULL`;
- ordered migration registration and duplicate/non-increasing version rejection;
- fresh database schema creation for current production tables and indexes;
- upgrade coverage from a minimal `v3.0.2` legacy schema;
- idempotent rerun coverage;
- store schema ownership cleanup after central startup migrations.

Do not implement dashboard token expiry, settings secret redaction, archive reliability, Telegram regression harnesses, release packaging gates, or broad refactors in this phase.

## File Structure

- Create: `internal/db/migrations/migrations.go`
  - Owns `Migration`, `Run`, `Registered`, `ensureMigrationTable`, registration validation, and current schema migration DDL.
- Create: `internal/db/migrations/migrations_test.go`
  - Covers migration ordering, duplicate/non-increasing registration validation, fresh schema creation, FTS5 transaction behavior, `v3.0.2` upgrade, representative row preservation, missing-column backfill, and idempotent reruns.
- Create: `internal/db/migrations/testdata/v302_schema.sql`
  - Minimal shipped legacy schema builder for `v3.0.2` upgrade tests when no fixture already exists.
- Modify: `cmd/aura/main.go`
  - Calls `migrations.Run(context.Background(), pool)` after `auradb.Open(cfg.DBPath)` and before `settings.NewStoreWithDB(pool)`.
- Modify: `cmd/debug_telegram_sandbox/main.go`
  - Calls `migrations.Run(context.Background(), pool)` after `auradb.Open(cfg.DBPath)` and before shared settings store or Telegram construction.
- Modify: `internal/auth/store.go`
  - Removes production schema bootstrapping from `NewStoreWithDB`; keeps `OpenStore` compatible by opening through `internal/db.Open`, running migrations, then returning a store.
- Modify: `internal/scheduler/store.go`
  - Moves scheduler, conversations, `wiki_issues`, and `proposed_updates` schema DDL/backfill ownership to migrations; keeps scheduler CRUD SQL and row scanning in place.
- Modify: `internal/settings/store.go`
  - Removes lazy `settings` table creation from shared-pool constructor; keeps key/value CRUD SQL.
- Modify: `internal/search/sqlite.go`
  - Removes `wiki_documents` FTS5 creation from searcher construction; searcher assumes migrations ran for shared production DB.
- Modify: `internal/search/embed_cache.go`
  - Removes `embedding_cache` table creation from shared-pool constructor; keeps lookup/write SQL.
- Modify: `internal/swarm/store.go`
  - Moves `swarm_runs`, `swarm_tasks`, indexes, and metric-column backfills to migrations.
- Modify tests only where constructor tests require explicit migrations or a helper.

## Migration Runner Contract

The implementation must expose this API from `internal/db/migrations`:

```go
package migrations

import (
	"context"
	"database/sql"
)

type Migration struct {
	Version int
	Name    string
	Up      func(context.Context, *sql.Tx) error
}

func Run(ctx context.Context, db *sql.DB) error
func Registered() []Migration
```

Implementation rules:

- `Run` rejects a nil `*sql.DB`.
- `Run` creates `schema_migrations` before applying registered migrations.
- `Run` validates registered migrations before touching user tables.
- `Run` applies missing migrations in ascending `Version` order.
- Each migration runs in its own transaction.
- The `schema_migrations` insert runs in the same transaction as the migration DDL.
- Applied migrations are inserted with UTC `time.RFC3339Nano` timestamps.
- A migration with a version already present in `schema_migrations` is skipped.
- A registered migration list containing duplicate versions, version `<= 0`, empty names, nil `Up`, or non-increasing versions returns an error.
- `Registered` returns a copy so tests cannot mutate package state.

## Task 1: Migration Runner Tests

**Files:**
- Create: `internal/db/migrations/migrations_test.go`
- Create: `internal/db/migrations/migrations.go`

- [ ] **Step 1: Add runner test scaffolding**

Create `internal/db/migrations/migrations_test.go` with package `migrations` and these helpers:

```go
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
```

- [ ] **Step 2: Test migration table creation**

Add this test:

```go
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
```

- [ ] **Step 3: Test idempotent rerun**

Add this test:

```go
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
```

- [ ] **Step 4: Test registration validation**

Add this test:

```go
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
```

- [ ] **Step 5: Run the new tests and capture the expected failure**

Run:

```powershell
go test ./internal/db/migrations
```

Expected: FAIL because `Run`, `Migration`, `Registered`, and `validateRegistered` are not implemented.

## Task 2: Migration Runner Implementation

**Files:**
- Create: `internal/db/migrations/migrations.go`
- Test: `internal/db/migrations/migrations_test.go`

- [ ] **Step 1: Implement the runner skeleton**

Create `internal/db/migrations/migrations.go`:

```go
package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Migration struct {
	Version int
	Name    string
	Up      func(context.Context, *sql.Tx) error
}

var registered = []Migration{
	{Version: 1, Name: "create_current_schema", Up: createCurrentSchema},
}

func Registered() []Migration {
	out := make([]Migration, len(registered))
	copy(out, registered)
	return out
}

func Run(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("migrations: db required")
	}
	migrations := Registered()
	if err := validateRegistered(migrations); err != nil {
		return err
	}
	if err := ensureMigrationTable(ctx, db); err != nil {
		return err
	}
	applied, err := appliedMap(ctx, db)
	if err != nil {
		return err
	}
	for _, migration := range migrations {
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
	last := 0
	seen := map[int]bool{}
	for _, migration := range migrations {
		if migration.Version <= 0 {
			return fmt.Errorf("migrations: invalid version %d", migration.Version)
		}
		if migration.Version <= last {
			return fmt.Errorf("migrations: versions must increase: %d after %d", migration.Version, last)
		}
		if seen[migration.Version] {
			return fmt.Errorf("migrations: duplicate version %d", migration.Version)
		}
		if migration.Name == "" {
			return fmt.Errorf("migrations: version %d has empty name", migration.Version)
		}
		if migration.Up == nil {
			return fmt.Errorf("migrations: version %d has nil Up", migration.Version)
		}
		seen[migration.Version] = true
		last = migration.Version
	}
	return nil
}

func ensureMigrationTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version    INTEGER PRIMARY KEY,
  name       TEXT NOT NULL,
  applied_at TEXT NOT NULL
);
`)
	if err != nil {
		return fmt.Errorf("migrations: create schema_migrations: %w", err)
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
		return nil, fmt.Errorf("migrations: applied rows: %w", err)
	}
	return applied, nil
}

func runOne(ctx context.Context, db *sql.DB, migration Migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migrations: begin %d %s: %w", migration.Version, migration.Name, err)
	}
	defer tx.Rollback()
	if err := migration.Up(ctx, tx); err != nil {
		return fmt.Errorf("migrations: apply %d %s: %w", migration.Version, migration.Name, err)
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO schema_migrations (version, name, applied_at)
VALUES (?, ?, ?)
`, migration.Version, migration.Name, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("migrations: record %d %s: %w", migration.Version, migration.Name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrations: commit %d %s: %w", migration.Version, migration.Name, err)
	}
	return nil
}

func createCurrentSchema(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, currentSchemaSQL)
	if err != nil {
		return fmt.Errorf("create current schema: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: Add a temporary minimal current schema constant**

Append this constant to `internal/db/migrations/migrations.go` so the runner compiles before Task 4 expands the schema:

```go
const currentSchemaSQL = `
CREATE TABLE IF NOT EXISTS settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
`
```

- [ ] **Step 3: Run the focused runner tests**

Run:

```powershell
go test ./internal/db/migrations
```

Expected: PASS for runner behavior.

- [ ] **Step 4: Commit the runner slice**

Run:

```powershell
git add internal/db/migrations/migrations.go internal/db/migrations/migrations_test.go
git commit -m "db: add sqlite migration runner"
```

Expected: commit succeeds with only migration runner files staged.

## Task 3: FTS5 Transaction Proof

**Files:**
- Modify: `internal/db/migrations/migrations_test.go`
- Modify: `internal/db/migrations/migrations.go`

- [ ] **Step 1: Add an explicit FTS5 transaction test before registering the full schema**

Add this test to `internal/db/migrations/migrations_test.go`:

```go
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
```

- [ ] **Step 2: Run the FTS5 proof**

Run:

```powershell
go test ./internal/db/migrations -run TestFTS5CreateVirtualTableWorksInsideTransaction -count=1
```

Expected: PASS on the repository's current `modernc.org/sqlite` version. If this command fails, stop this phase and replace the runner API with an explicit `NoTx bool` field before any schema migration is registered; the FTS5 migration must then run without a transaction and still record `schema_migrations` only after successful table creation.

- [ ] **Step 3: Keep the proof test**

Leave `TestFTS5CreateVirtualTableWorksInsideTransaction` in `migrations_test.go` so future SQLite driver upgrades prove the transaction strategy remains valid.

- [ ] **Step 4: Commit the FTS5 proof**

Run:

```powershell
git add internal/db/migrations/migrations_test.go
git commit -m "db: verify fts5 migration transaction behavior"
```

Expected: commit succeeds with only the FTS5 proof test staged.

## Task 4: Register Current Fresh Schema

**Files:**
- Modify: `internal/db/migrations/migrations.go`
- Modify: `internal/db/migrations/migrations_test.go`

- [ ] **Step 1: Replace `currentSchemaSQL` with the current production schema**

Replace the temporary `currentSchemaSQL` constant in `internal/db/migrations/migrations.go` with this DDL:

```go
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
```

- [ ] **Step 2: Add a fresh schema test**

Add this test to `internal/db/migrations/migrations_test.go`:

```go
func TestRunCreatesCurrentFreshSchema(t *testing.T) {
	db := openTestDB(t)
	if err := Run(context.Background(), db); err != nil {
		t.Fatalf("Run: %v", err)
	}

	requiredTables := []string{
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
	for _, table := range requiredTables {
		t.Run(table, func(t *testing.T) {
			var name string
			err := db.QueryRow(`SELECT name FROM sqlite_schema WHERE name = ?`, table).Scan(&name)
			if err != nil {
				t.Fatalf("missing table %s: %v", table, err)
			}
		})
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

func assertColumns(t *testing.T, db *sql.DB, table string, names []string) {
	t.Helper()
	cols := tableColumns(t, db, table)
	for _, name := range names {
		if !cols[name] {
			t.Fatalf("%s missing column %s; cols=%v", table, name, cols)
		}
	}
}

func tableColumns(t *testing.T, db *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("table info %s: %v", table, err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan table info %s: %v", table, err)
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table info rows %s: %v", table, err)
	}
	return cols
}
```

- [ ] **Step 3: Add an FTS5 behavior assertion to the fresh schema test file**

Add this test to prove `wiki_documents` is usable:

```go
func TestRunCreatesUsableWikiDocumentsFTS5Table(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := Run(ctx, db); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO wiki_documents (id, content, metadata, title)
VALUES ('alpha', 'Aura remembers durable context', '{"slug":"alpha"}', 'Alpha')
`); err != nil {
		t.Fatalf("insert wiki_documents: %v", err)
	}
	var id string
	if err := db.QueryRowContext(ctx, `
SELECT id FROM wiki_documents WHERE wiki_documents MATCH 'durable'
`).Scan(&id); err != nil {
		t.Fatalf("search wiki_documents: %v", err)
	}
	if id != "alpha" {
		t.Fatalf("id = %q, want alpha", id)
	}
}
```

- [ ] **Step 4: Run schema tests**

Run:

```powershell
go test ./internal/db/migrations
```

Expected: PASS.

- [ ] **Step 5: Commit the schema registration slice**

Run:

```powershell
git add internal/db/migrations/migrations.go internal/db/migrations/migrations_test.go
git commit -m "db: register current sqlite schema migration"
```

Expected: commit succeeds with migration schema files staged.

## Task 5: v3.0.2 Upgrade Fixture and Backfill Migration

**Files:**
- Create: `internal/db/migrations/testdata/v302_schema.sql`
- Modify: `internal/db/migrations/migrations.go`
- Modify: `internal/db/migrations/migrations_test.go`

- [ ] **Step 1: Add the minimal `v3.0.2` fixture**

Create `internal/db/migrations/testdata/v302_schema.sql`:

```sql
CREATE TABLE settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE api_tokens (
  token_hash TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL,
  issued_at  TEXT NOT NULL,
  last_used  TEXT,
  revoked_at TEXT
);
CREATE INDEX idx_api_tokens_user ON api_tokens(user_id);

CREATE TABLE allowed_users (
  user_id    TEXT PRIMARY KEY,
  source     TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE pending_users (
  user_id      TEXT PRIMARY KEY,
  username     TEXT NOT NULL,
  requested_at TEXT NOT NULL,
  decided_at   TEXT,
  decision     TEXT
);
CREATE INDEX idx_pending_users_decision ON pending_users(decision);

CREATE TABLE scheduled_tasks (
  id                     INTEGER PRIMARY KEY AUTOINCREMENT,
  name                   TEXT NOT NULL UNIQUE,
  kind                   TEXT NOT NULL,
  payload                TEXT NOT NULL DEFAULT '',
  recipient_id           TEXT NOT NULL DEFAULT '',
  schedule_kind          TEXT NOT NULL,
  schedule_at            TEXT,
  schedule_daily         TEXT,
  schedule_every_minutes INTEGER NOT NULL DEFAULT 0,
  next_run_at            TEXT NOT NULL,
  last_run_at            TEXT,
  last_error             TEXT NOT NULL DEFAULT '',
  status                 TEXT NOT NULL DEFAULT 'active',
  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL
);
CREATE INDEX idx_scheduled_tasks_due ON scheduled_tasks(status, next_run_at);

CREATE TABLE conversations (
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
CREATE INDEX idx_conv_chat ON conversations(chat_id, turn_index);
CREATE INDEX idx_conv_user ON conversations(user_id, created_at);

CREATE TABLE proposed_updates (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  chat_id         INTEGER NOT NULL,
  fact            TEXT NOT NULL,
  action          TEXT NOT NULL,
  target_slug     TEXT NOT NULL DEFAULT '',
  similarity      REAL NOT NULL DEFAULT 0,
  source_turn_ids TEXT NOT NULL DEFAULT '',
  status          TEXT NOT NULL DEFAULT 'pending',
  created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE wiki_issues (
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
CREATE UNIQUE INDEX idx_wiki_issues_key ON wiki_issues(kind, slug, broken_link);

CREATE TABLE embedding_cache (
  content_sha TEXT NOT NULL,
  model       TEXT NOT NULL,
  embedding   BLOB NOT NULL,
  created_at  TIMESTAMP NOT NULL,
  PRIMARY KEY (content_sha, model)
);

CREATE VIRTUAL TABLE wiki_documents
USING fts5(id, content, metadata, title);

CREATE TABLE swarm_runs (
  id           TEXT PRIMARY KEY,
  goal         TEXT NOT NULL,
  status       TEXT NOT NULL,
  created_by   TEXT NOT NULL,
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL,
  completed_at TEXT,
  last_error   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE swarm_tasks (
  id             TEXT PRIMARY KEY,
  run_id         TEXT NOT NULL,
  parent_id      TEXT NOT NULL DEFAULT '',
  role           TEXT NOT NULL,
  subject        TEXT NOT NULL,
  prompt         TEXT NOT NULL,
  tool_allowlist TEXT NOT NULL DEFAULT '[]',
  status         TEXT NOT NULL,
  depth          INTEGER NOT NULL DEFAULT 0,
  attempts       INTEGER NOT NULL DEFAULT 0,
  blocked_by     TEXT NOT NULL DEFAULT '[]',
  result         TEXT NOT NULL DEFAULT '',
  tool_calls     INTEGER NOT NULL DEFAULT 0,
  llm_calls      INTEGER NOT NULL DEFAULT 0,
  elapsed_ms     INTEGER NOT NULL DEFAULT 0,
  created_at     TEXT NOT NULL,
  started_at     TEXT,
  completed_at   TEXT,
  last_error     TEXT NOT NULL DEFAULT '',
  FOREIGN KEY(run_id) REFERENCES swarm_runs(id)
);
CREATE INDEX idx_swarm_tasks_run ON swarm_tasks(run_id, status, created_at);
CREATE INDEX idx_swarm_tasks_status ON swarm_tasks(status, created_at);
```

- [ ] **Step 2: Add upgrade migration registration**

Change `registered` in `internal/db/migrations/migrations.go` to:

```go
var registered = []Migration{
	{Version: 1, Name: "create_current_schema", Up: createCurrentSchema},
	{Version: 2, Name: "backfill_current_columns", Up: backfillCurrentColumns},
}
```

- [ ] **Step 3: Implement table-column helpers and backfills**

Append this code to `internal/db/migrations/migrations.go`:

```go
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

type columnDef struct {
	Name string
	SQL  string
}

func addMissingColumns(ctx context.Context, tx *sql.Tx, table string, columns []columnDef) error {
	existing, err := txTableColumns(ctx, tx, table)
	if err != nil {
		return fmt.Errorf("migrations: inspect %s: %w", table, err)
	}
	for _, column := range columns {
		if existing[column.Name] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column.Name+` `+column.SQL); err != nil {
			return fmt.Errorf("migrations: add %s.%s: %w", table, column.Name, err)
		}
	}
	return nil
}

func txTableColumns(ctx context.Context, tx *sql.Tx, table string) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return cols, nil
}
```

- [ ] **Step 4: Add upgrade test helpers**

Add these helpers to `internal/db/migrations/migrations_test.go`:

```go
func loadSQLFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func seedV302Rows(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
INSERT INTO settings (key, value, updated_at)
VALUES ('LLM_MODEL', 'claude-test', '2026-05-04T00:00:00Z');

INSERT INTO api_tokens (token_hash, user_id, issued_at, last_used, revoked_at)
VALUES ('hash-1', 'user-1', '2026-05-04T00:00:00Z', NULL, NULL);

INSERT INTO allowed_users (user_id, source, created_at)
VALUES ('user-1', 'telegram_bootstrap', '2026-05-04T00:00:00Z');

INSERT INTO pending_users (user_id, username, requested_at, decided_at, decision)
VALUES ('user-2', 'candidate', '2026-05-04T00:01:00Z', NULL, NULL);

INSERT INTO scheduled_tasks (
  name, kind, payload, recipient_id, schedule_kind, schedule_daily,
  schedule_every_minutes, next_run_at, last_error, status, created_at, updated_at
) VALUES (
  'daily review', 'briefing', '{}', 'user-1', 'daily', '09:00',
  0, '2026-05-05T09:00:00Z', '', 'active', '2026-05-04T00:00:00Z', '2026-05-04T00:00:00Z'
);

INSERT INTO conversations (chat_id, user_id, turn_index, role, content)
VALUES (42, 7, 1, 'user', 'remember this');

INSERT INTO proposed_updates (chat_id, fact, action, target_slug, similarity, source_turn_ids)
VALUES (42, 'Aura has a legacy fact', 'new', '', 0.5, '[1]');

INSERT INTO wiki_issues (kind, severity, slug, broken_link, message)
VALUES ('broken_link', 'warning', 'alpha', 'missing', 'Missing link');

INSERT INTO embedding_cache (content_sha, model, embedding, created_at)
VALUES ('sha-1', 'mistral-embed', X'00000000', '2026-05-04T00:00:00Z');

INSERT INTO wiki_documents (id, content, metadata, title)
VALUES ('alpha', 'legacy searchable memory', '{"slug":"alpha"}', 'Alpha');

INSERT INTO swarm_runs (id, goal, status, created_by, created_at, updated_at, last_error)
VALUES ('swarm_1', 'legacy run', 'pending', 'user-1', '2026-05-04T00:00:00Z', '2026-05-04T00:00:00Z', '');

INSERT INTO swarm_tasks (
  id, run_id, role, subject, prompt, status, created_at
) VALUES (
  'task_1', 'swarm_1', 'worker', 'subject', 'prompt', 'pending', '2026-05-04T00:00:00Z'
);
`)
	if err != nil {
		t.Fatalf("seed v302 rows: %v", err)
	}
}

func assertScalar[T comparable](t *testing.T, db *sql.DB, query string, want T) {
	t.Helper()
	var got T
	if err := db.QueryRow(query).Scan(&got); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("query %q = %v, want %v", query, got, want)
	}
}
```

Add `os` to the imports.

- [ ] **Step 5: Add the `v3.0.2` upgrade test**

Add this test to `internal/db/migrations/migrations_test.go`:

```go
func TestRunUpgradesV302SchemaPreservesRowsAndIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, loadSQLFile(t, filepath.Join("testdata", "v302_schema.sql"))); err != nil {
		t.Fatalf("create v302 schema: %v", err)
	}
	seedV302Rows(t, db)

	if err := Run(ctx, db); err != nil {
		t.Fatalf("Run upgrade: %v", err)
	}

	assertScalar(t, db, `SELECT COUNT(*) FROM settings WHERE key = 'LLM_MODEL' AND value = 'claude-test'`, 1)
	assertScalar(t, db, `SELECT COUNT(*) FROM api_tokens WHERE token_hash = 'hash-1'`, 1)
	assertScalar(t, db, `SELECT COUNT(*) FROM allowed_users WHERE user_id = 'user-1'`, 1)
	assertScalar(t, db, `SELECT COUNT(*) FROM pending_users WHERE user_id = 'user-2'`, 1)
	assertScalar(t, db, `SELECT COUNT(*) FROM scheduled_tasks WHERE name = 'daily review'`, 1)
	assertScalar(t, db, `SELECT COUNT(*) FROM conversations WHERE chat_id = 42 AND turn_index = 1`, 1)
	assertScalar(t, db, `SELECT COUNT(*) FROM proposed_updates WHERE fact = 'Aura has a legacy fact'`, 1)
	assertScalar(t, db, `SELECT COUNT(*) FROM wiki_issues WHERE slug = 'alpha'`, 1)
	assertScalar(t, db, `SELECT COUNT(*) FROM embedding_cache WHERE content_sha = 'sha-1'`, 1)
	assertScalar(t, db, `SELECT COUNT(*) FROM wiki_documents WHERE wiki_documents MATCH 'searchable'`, 1)
	assertScalar(t, db, `SELECT COUNT(*) FROM swarm_runs WHERE id = 'swarm_1'`, 1)
	assertScalar(t, db, `SELECT COUNT(*) FROM swarm_tasks WHERE id = 'task_1'`, 1)

	assertColumns(t, db, "scheduled_tasks", []string{"schedule_weekdays", "last_output", "last_metrics_json", "wake_signature"})
	assertColumns(t, db, "proposed_updates", []string{"category", "related_slugs", "provenance_json"})
	assertColumns(t, db, "swarm_tasks", []string{"tokens_prompt", "tokens_completion", "tokens_total"})

	first := appliedVersions(t, db)
	if err := Run(ctx, db); err != nil {
		t.Fatalf("Run rerun: %v", err)
	}
	second := appliedVersions(t, db)
	if len(first) != len(second) {
		t.Fatalf("migration count changed after rerun: first=%v second=%v", first, second)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("migration versions changed after rerun: first=%v second=%v", first, second)
		}
	}
}
```

- [ ] **Step 6: Run upgrade tests**

Run:

```powershell
go test ./internal/db/migrations -run "TestRunUpgradesV302SchemaPreservesRowsAndIsIdempotent|TestRunIsIdempotent" -count=1
```

Expected: PASS; existing representative rows survive, current columns exist, and rerun records no duplicate migrations.

- [ ] **Step 7: Run all migration tests**

Run:

```powershell
go test ./internal/db/migrations
```

Expected: PASS.

- [ ] **Step 8: Commit the upgrade slice**

Run:

```powershell
git add internal/db/migrations/migrations.go internal/db/migrations/migrations_test.go internal/db/migrations/testdata/v302_schema.sql
git commit -m "db: verify v302 migration upgrade"
```

Expected: commit succeeds with migration package files staged.

## Task 6: Wire Production Startup Through Migrations

**Files:**
- Modify: `cmd/aura/main.go`
- Modify: `cmd/debug_telegram_sandbox/main.go`
- Test: `cmd/aura/main_test.go` or existing command package test file
- Test: `cmd/debug_telegram_sandbox/main_test.go` or existing command package test file

- [ ] **Step 1: Import migrations in production startup**

In `cmd/aura/main.go` and `cmd/debug_telegram_sandbox/main.go`, add this import:

```go
	"github.com/aura/aura/internal/db/migrations"
```

- [ ] **Step 2: Run migrations after `auradb.Open` in `cmd/aura`**

Immediately after:

```go
	pool, err := auradb.Open(cfg.DBPath)
	if err != nil {
		logger.Error("failed to open database", "error", err, "db_path", cfg.DBPath)
		os.Exit(1)
	}
	defer pool.Close()
```

add:

```go
	if err := migrations.Run(context.Background(), pool); err != nil {
		logger.Error("failed to migrate database", "error", err, "db_path", cfg.DBPath)
		os.Exit(1)
	}
```

- [ ] **Step 3: Run migrations after `auradb.Open` in `cmd/debug_telegram_sandbox`**

In `cmd/debug_telegram_sandbox/main.go`, immediately after:

```go
	pool, err := auradb.Open(cfg.DBPath)
	if err != nil {
		logger.Error("failed to open database", "error", err, "db_path", cfg.DBPath)
		os.Exit(1)
	}
	defer pool.Close()
```

add:

```go
	if err := migrations.Run(context.Background(), pool); err != nil {
		logger.Error("failed to migrate database", "error", err, "db_path", cfg.DBPath)
		os.Exit(1)
	}
```

This call must stay before `settings.NewStoreWithDB(pool)`, any shared Telegram store construction, and `telegram.New`.

- [ ] **Step 4: Add a static startup-order test for `cmd/aura`**

Create or update `cmd/aura/main_test.go` with this test:

```go
func TestMainRunsMigrationsBeforeStoreConstruction(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(data)
	openIdx := strings.Index(source, "auradb.Open(cfg.DBPath)")
	migrateIdx := strings.Index(source, "migrations.Run(context.Background(), pool)")
	settingsIdx := strings.Index(source, "settings.NewStoreWithDB(pool)")
	telegramIdx := strings.Index(source, "telegram.New(cfg, settingsStore, pool, logger)")

	if openIdx < 0 || migrateIdx < 0 || settingsIdx < 0 || telegramIdx < 0 {
		t.Fatalf("startup markers missing: open=%d migrate=%d settings=%d telegram=%d", openIdx, migrateIdx, settingsIdx, telegramIdx)
	}
	if !(openIdx < migrateIdx && migrateIdx < settingsIdx && settingsIdx < telegramIdx) {
		t.Fatalf("startup order invalid: open=%d migrate=%d settings=%d telegram=%d", openIdx, migrateIdx, settingsIdx, telegramIdx)
	}
}
```

Add `os`, `strings`, and `testing` imports if `cmd/aura/main_test.go` is new.

- [ ] **Step 5: Add a static startup-order test for `cmd/debug_telegram_sandbox`**

Create or update `cmd/debug_telegram_sandbox/main_test.go` with this test:

```go
func TestMainRunsMigrationsBeforeSharedStoreConstruction(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(data)
	openIdx := strings.Index(source, "auradb.Open(cfg.DBPath)")
	migrateIdx := strings.Index(source, "migrations.Run(context.Background(), pool)")
	settingsIdx := strings.Index(source, "settings.NewStoreWithDB(pool)")
	telegramIdx := strings.Index(source, "telegram.New(")

	if openIdx < 0 || migrateIdx < 0 || settingsIdx < 0 || telegramIdx < 0 {
		t.Fatalf("startup markers missing: open=%d migrate=%d settings=%d telegram=%d", openIdx, migrateIdx, settingsIdx, telegramIdx)
	}
	if !(openIdx < migrateIdx && migrateIdx < settingsIdx && settingsIdx < telegramIdx) {
		t.Fatalf("startup order invalid: open=%d migrate=%d settings=%d telegram=%d", openIdx, migrateIdx, settingsIdx, telegramIdx)
	}
}
```

Add `os`, `strings`, and `testing` imports if `cmd/debug_telegram_sandbox/main_test.go` is new.

- [ ] **Step 6: Run command package tests**

Run:

```powershell
go test ./cmd/aura
go test ./cmd/debug_telegram_sandbox
```

Expected: PASS; the static checks prove production startup and the debug Telegram sandbox run migrations before shared-store construction.

- [ ] **Step 7: Build command packages**

Run:

```powershell
go build ./cmd/aura
go build ./cmd/debug_telegram_sandbox
```

Expected: both command packages build successfully.

- [ ] **Step 8: Commit startup wiring**

Run:

```powershell
git add cmd/aura/main.go cmd/aura/main_test.go cmd/debug_telegram_sandbox/main.go cmd/debug_telegram_sandbox/main_test.go
git commit -m "db: run migrations during aura startup"
```

Expected: commit succeeds with only startup wiring files staged.

## Task 7: Store Schema Ownership Cleanup

**Files:**
- Modify: `internal/auth/store.go`
- Modify: `internal/scheduler/store.go`
- Modify: `internal/settings/store.go`
- Modify: `internal/search/sqlite.go`
- Modify: `internal/search/embed_cache.go`
- Modify: `internal/swarm/store.go`
- Modify: tests in `internal/auth`, `internal/scheduler`, `internal/settings`, `internal/search`, `internal/swarm`, and `internal/telegram` only where constructors need explicit migrations.
- Modify: `internal/telegram/bot_test.go`
- Modify: `internal/telegram/scheduler_handlers_test.go`

- [ ] **Step 1: Add migrations imports where `OpenStore` needs compatibility**

In `internal/auth/store.go`, `internal/scheduler/store.go`, `internal/settings/store.go`, and `internal/swarm/store.go`, add:

```go
	"github.com/aura/aura/internal/db/migrations"
```

These `OpenStore` helpers can run migrations because they open their own DB outside production startup. Shared-pool constructors must assume `cmd/aura/main.go` already ran migrations.

- [ ] **Step 2: Update auth store construction**

In `internal/auth/store.go`:

- remove the `schemaSQL` constant;
- remove `func (s *Store) migrate() error`;
- in `OpenStore`, replace `s.migrate()` with `migrations.Run(context.Background(), db)`;
- in `NewStoreWithDB`, remove the migration call and return the store after nil validation.

The resulting constructor bodies must be:

```go
func OpenStore(path string) (*Store, error) {
	db, err := auradb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open auth db: %w", err)
	}
	if err := migrations.Run(context.Background(), db); err != nil {
		db.Close()
		return nil, fmt.Errorf("auth migrate: %w", err)
	}
	return &Store{db: db, now: time.Now, owned: true}, nil
}

func NewStoreWithDB(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("auth: db required")
	}
	return &Store{db: db, now: time.Now, owned: false}, nil
}
```

- [ ] **Step 3: Update settings store construction**

In `internal/settings/store.go`:

- remove the `schemaSQL` constant;
- remove `func (s *Store) migrate() error`;
- in `OpenStore`, replace `s.migrate()` with `migrations.Run(context.Background(), db)`;
- in `NewStoreWithDB`, remove the migration call.

The resulting constructor bodies must be:

```go
func OpenStore(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("settings: db path required")
	}
	db, err := auradb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("settings: open db: %w", err)
	}
	if err := migrations.Run(context.Background(), db); err != nil {
		db.Close()
		return nil, fmt.Errorf("settings: migrate: %w", err)
	}
	return &Store{db: db, now: time.Now, owned: true}, nil
}

func NewStoreWithDB(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("settings: db required")
	}
	return &Store{db: db, now: time.Now, owned: false}, nil
}
```

- [ ] **Step 4: Update scheduler store construction**

In `internal/scheduler/store.go`:

- remove `conversationsSchemaSQL`, `wikiIssuesSchemaSQL`, `proposedUpdatesSchemaSQL`, and `schemaSQL`;
- remove `func (s *Store) migrate() error`;
- remove `addProposedUpdateReviewColumns`, `tableInfoColumns`, `addEveryMinutesColumn`, `addScheduleWeekdaysColumn`, `addAgentJobResultColumns`, and `dropLegacyConversations`;
- in `OpenStore`, run `migrations.Run(context.Background(), db)` before returning the store;
- in `NewStoreWithDB`, remove the migration call.

The resulting constructor bodies must be:

```go
func OpenStore(path string) (*Store, error) {
	db, err := auradb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open scheduler db: %w", err)
	}
	if err := migrations.Run(context.Background(), db); err != nil {
		db.Close()
		return nil, fmt.Errorf("scheduler migrate: %w", err)
	}
	return &Store{db: db, owned: true}, nil
}

func NewStoreWithDB(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("scheduler: db required")
	}
	return &Store{db: db, owned: false}, nil
}
```

- [ ] **Step 5: Update search constructors**

In `internal/search/sqlite.go`:

- remove `setupSqliteSchema`;
- remove the call to `setupSqliteSchema(db)` from `newSqliteSearcherWithDB`;
- keep `newSqliteSearcher` as an owned helper, but run `migrations.Run(context.Background(), db)` after `auradb.Open(dbPath)` and before `newSqliteSearcherWithDB(db, logger)`.

The owned opener body must include:

```go
	if err := migrations.Run(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrating sqlite schema: %w", err)
	}
```

In `internal/search/embed_cache.go`:

- remove the `CREATE TABLE IF NOT EXISTS embedding_cache` block from `NewEmbedCacheWithDB`;
- keep cache lookup and writes unchanged;
- in `OpenEmbedCache`, run `migrations.Run(context.Background(), db)` after `auradb.Open(dbPath)` and before `NewEmbedCacheWithDB`.

- [ ] **Step 6: Update swarm store construction**

In `internal/swarm/store.go`:

- remove `schemaSQL`;
- remove `func (s *Store) migrate() error`;
- remove `addSwarmTaskMetricColumns` and `tableColumns`;
- in `OpenStore`, run `migrations.Run(context.Background(), db)` before `newStore(db, true)`;
- in `NewStoreWithDB`, remove the migration call.

The resulting constructor bodies must be:

```go
func OpenStore(path string) (*Store, error) {
	db, err := auradb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open swarm db: %w", err)
	}
	if err := migrations.Run(context.Background(), db); err != nil {
		db.Close()
		return nil, fmt.Errorf("swarm migrate: %w", err)
	}
	return newStore(db, true), nil
}

func NewStoreWithDB(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("swarm store: db required")
	}
	return newStore(db, false), nil
}
```

- [ ] **Step 7: Update tests that construct shared-pool stores**

For tests that call `auradb.Open` and then `NewStoreWithDB`, add:

```go
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("migrations.Run: %v", err)
	}
```

Add the import:

```go
	"github.com/aura/aura/internal/db/migrations"
```

Apply this only to tests that perform CRUD after `NewStoreWithDB`. Tests that only assert nil DB rejection do not need migrations.

Include Telegram tests that open raw DB pools and then construct shared-pool stores through bot or scheduler handler setup. In particular, update `internal/telegram/bot_test.go` and `internal/telegram/scheduler_handlers_test.go` so any test path that opens a database and reaches `NewStoreWithDB` runs `migrations.Run` first.

- [ ] **Step 8: Move old store migration tests into migration package**

Delete `internal/scheduler/store_migration_test.go` after confirming the coverage exists in `internal/db/migrations/migrations_test.go`:

- proposed_updates review columns;
- scheduled_tasks `schedule_weekdays`;
- scheduled_tasks `last_output`, `last_metrics_json`, `wake_signature`.

Do not delete scheduler CRUD tests.

- [ ] **Step 9: Run focused store tests**

Run:

```powershell
go test ./internal/db ./internal/auth ./internal/scheduler ./internal/settings ./internal/search ./internal/swarm ./internal/telegram
```

Expected: PASS; store constructors no longer lazily create production schemas from shared-pool constructors.

- [ ] **Step 10: Commit store cleanup**

Run:

```powershell
git status --short -uall
git add internal/auth/store.go internal/scheduler/store.go internal/settings/store.go internal/search/sqlite.go internal/search/embed_cache.go internal/swarm/store.go internal/telegram/bot_test.go internal/telegram/scheduler_handlers_test.go
git commit -m "db: move store schema ownership to migrations"
```

Expected: `git status --short -uall` is inspected before staging; stage only the changed files from this task, using explicit file paths and omitting any unrelated user edits. If constructor tests outside Telegram changed, add those exact test file paths from the inspected status output. Commit succeeds with only migration-related store and test changes staged.

## Task 8: Fresh/Upgrade Schema Convergence Gate

**Files:**
- Modify: `internal/db/migrations/migrations_test.go`

- [ ] **Step 1: Add schema signature helpers**

Add these helpers to `internal/db/migrations/migrations_test.go`:

```go
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func schemaSignature(t *testing.T, db *sql.DB) []string {
	t.Helper()
	var out []string

	out = append(out, tableSignatures(t, db)...)
	out = append(out, indexSignatures(t, db)...)
	out = append(out, ftsSignatures(t, db)...)
	sort.Strings(out)
	return out
}

func tableSignatures(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`
SELECT name
FROM sqlite_schema
WHERE type = 'table'
  AND name NOT LIKE 'sqlite_%'
  AND name NOT LIKE 'wiki_documents_%'
ORDER BY name
`)
	if err != nil {
		t.Fatalf("table signature query: %v", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatalf("table signature scan: %v", err)
		}
		out = append(out, "table|"+table+"|"+strings.Join(columnSignatures(t, db, table), ","))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table signature rows: %v", err)
	}
	return out
}

func columnSignatures(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(` + quoteIdent(table) + `)`)
	if err != nil {
		t.Fatalf("table info %s: %v", table, err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("table info scan %s: %v", table, err)
		}
		def := "<nil>"
		if defaultValue.Valid {
			def = strings.Join(strings.Fields(defaultValue.String), " ")
		}
		out = append(out, fmt.Sprintf("%s:%s:notnull=%d:default=%s:pk=%d", name, strings.ToUpper(typ), notNull, def, pk))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table info rows %s: %v", table, err)
	}
	sort.Strings(out)
	return out
}

func indexSignatures(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`
SELECT name, tbl_name, sql
FROM sqlite_schema
WHERE type = 'index'
  AND name NOT LIKE 'sqlite_%'
ORDER BY name
`)
	if err != nil {
		t.Fatalf("index signature query: %v", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name, table string
		var sqlText sql.NullString
		if err := rows.Scan(&name, &table, &sqlText); err != nil {
			t.Fatalf("index signature scan: %v", err)
		}
		sqlSig := ""
		if sqlText.Valid {
			sqlSig = strings.Join(strings.Fields(sqlText.String), " ")
		}
		out = append(out, "index|"+name+"|"+table+"|"+sqlSig)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("index signature rows: %v", err)
	}
	return out
}

func ftsSignatures(t *testing.T, db *sql.DB) []string {
	t.Helper()
	var name string
	if err := db.QueryRow(`SELECT name FROM sqlite_schema WHERE type = 'table' AND name = 'wiki_documents'`).Scan(&name); err != nil {
		t.Fatalf("wiki_documents FTS table missing: %v", err)
	}
	return []string{"fts|wiki_documents|present"}
}

func assertFTSContentBehavior(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	if _, err := db.Exec(`
INSERT INTO wiki_documents(id, content, metadata, title)
VALUES (?, 'convergence probe text', '{}', 'Convergence Probe')
`, id); err != nil {
		t.Fatalf("insert FTS probe: %v", err)
	}
	assertScalar(t, db, `SELECT COUNT(*) FROM wiki_documents WHERE wiki_documents MATCH 'convergence'`, 1)
	if _, err := db.Exec(`DELETE FROM wiki_documents WHERE id = ?`, id); err != nil {
		t.Fatalf("delete FTS probe: %v", err)
	}
}
```

Add `fmt`, `sort`, and `strings` to the imports. This semantic signature intentionally avoids comparing raw DDL text: table comparison is by column name/type/notnull/default/pk sets so upgraded tables with `ALTER TABLE ... ADD COLUMN` converge even when fresh DDL places the same column in a different ordinal position. Index comparison is by name, owning table, and normalized SQL where SQLite preserves meaningful SQL. FTS comparison is by virtual table presence and a write/search/delete behavior probe.

- [ ] **Step 2: Add convergence test**

Add this test:

```go
func TestFreshAndUpgradedSchemasConverge(t *testing.T) {
	ctx := context.Background()

	fresh := openTestDB(t)
	if err := Run(ctx, fresh); err != nil {
		t.Fatalf("fresh Run: %v", err)
	}

	upgraded := openTestDB(t)
	if _, err := upgraded.ExecContext(ctx, loadSQLFile(t, filepath.Join("testdata", "v302_schema.sql"))); err != nil {
		t.Fatalf("create v302 schema: %v", err)
	}
	if err := Run(ctx, upgraded); err != nil {
		t.Fatalf("upgrade Run: %v", err)
	}

	freshSig := schemaSignature(t, fresh)
	upgradedSig := schemaSignature(t, upgraded)
	if len(freshSig) != len(upgradedSig) {
		t.Fatalf("schema length mismatch\nfresh=%v\nupgraded=%v", freshSig, upgradedSig)
	}
	for i := range freshSig {
		if freshSig[i] != upgradedSig[i] {
			t.Fatalf("schema mismatch at %d\nfresh=%s\nupgraded=%s\nfreshAll=%v\nupgradedAll=%v", i, freshSig[i], upgradedSig[i], freshSig, upgradedSig)
		}
	}
	assertFTSContentBehavior(t, fresh, "fresh-convergence-probe")
	assertFTSContentBehavior(t, upgraded, "upgraded-convergence-probe")
}
```

- [ ] **Step 3: Run convergence test**

Run:

```powershell
go test ./internal/db/migrations -run TestFreshAndUpgradedSchemasConverge -count=1
```

Expected: PASS; fresh and upgraded schema signatures match for Aura-owned tables and indexes.

- [ ] **Step 4: Commit convergence gate**

Run:

```powershell
git add internal/db/migrations/migrations_test.go
git commit -m "db: verify fresh and upgraded schema convergence"
```

Expected: commit succeeds with only migration tests staged.

## Task 9: Full Verification and Static Behavior Checks

**Files:**
- No code changes expected.

- [ ] **Step 1: Format Go code**

Run:

```powershell
go fmt ./...
```

Expected: command exits 0.

- [ ] **Step 2: Run focused migration/store packages**

Run:

```powershell
go test ./internal/db ./internal/auth ./internal/scheduler ./internal/settings ./internal/search ./internal/swarm
```

Expected: PASS.

- [ ] **Step 3: Run startup and Telegram packages**

Run:

```powershell
go test ./internal/telegram
go test ./cmd/aura
go test ./cmd/debug_telegram_sandbox
```

Expected: PASS, including `TestMainRunsMigrationsBeforeStoreConstruction` and `TestMainRunsMigrationsBeforeSharedStoreConstruction`.

- [ ] **Step 4: Build startup command packages**

Run:

```powershell
go build ./cmd/aura
go build ./cmd/debug_telegram_sandbox
```

Expected: PASS; both startup commands compile after migration wiring.

- [ ] **Step 5: Run all Go tests**

Run:

```powershell
go test ./...
```

Expected: PASS.

- [ ] **Step 6: Run static startup-order check from the shell**

Run:

```powershell
@'
from pathlib import Path
checks = {
    "cmd/aura/main.go": [
        "auradb.Open(cfg.DBPath)",
        "migrations.Run(context.Background(), pool)",
        "settings.NewStoreWithDB(pool)",
        "telegram.New(cfg, settingsStore, pool, logger)",
    ],
    "cmd/debug_telegram_sandbox/main.go": [
        "auradb.Open(cfg.DBPath)",
        "migrations.Run(context.Background(), pool)",
        "settings.NewStoreWithDB(pool)",
        "telegram.New(",
    ],
}
for path, markers in checks.items():
    s = Path(path).read_text()
    positions = [s.index(m) for m in markers]
    print(path, positions)
    if positions != sorted(positions):
        raise SystemExit(f"startup order invalid: {path}")
'@ | python -
```

Expected: prints increasing byte offsets for both startup commands and exits 0.

- [ ] **Step 7: Confirm no lazy production schema DDL remains in shared-pool constructors**

Run:

```powershell
rg -n "CREATE TABLE|CREATE INDEX|CREATE VIRTUAL TABLE|ALTER TABLE|schemaSQL|migrate\\(" internal/auth internal/scheduler internal/settings internal/search internal/swarm
```

Expected: DDL appears in tests or owned compatibility helpers only where they call `migrations.Run`; production shared-pool constructors `NewStoreWithDB`, `newSqliteSearcherWithDB`, and `NewEmbedCacheWithDB` contain no schema DDL.

- [ ] **Step 8: Confirm migration table name**

Run:

```powershell
rg -n "schema_migrations" internal/db/migrations
```

Expected: matches in `migrations.go` and `migrations_test.go`; table columns are `version`, `name`, and `applied_at`.

- [ ] **Step 9: Confirm debug Telegram sandbox migration wiring**

Run:

```powershell
rg -n "auradb.Open\(cfg.DBPath\)|migrations.Run\(context.Background\(\), pool\)|settings.NewStoreWithDB\(pool\)|telegram.New\(" cmd/debug_telegram_sandbox/main.go
```

Expected: all four markers are present, and the line numbers show `migrations.Run(context.Background(), pool)` after `auradb.Open(cfg.DBPath)` and before shared-store construction.

- [ ] **Step 10: Inspect working tree before final commit**

Run:

```powershell
git status --short -uall
```

Expected: only intended migration-safety files are modified or untracked.

- [ ] **Step 11: Commit verification polish if formatting changed files**

Run only if `go fmt ./...` changed files after the previous commits:

```powershell
git status --short -uall
```

After inspecting status, run `git add` with explicit file paths for only the formatting or test-polish files changed in this phase, then run:

```powershell
git commit -m "db: finalize migration safety verification"
```

Expected: unrelated user edits remain unstaged.

## Implementation Commit Sequence

Use these commits in order:

1. `db: add sqlite migration runner`
2. `db: verify fts5 migration transaction behavior`
3. `db: register current sqlite schema migration`
4. `db: verify v302 migration upgrade`
5. `db: run migrations during aura startup`
6. `db: move store schema ownership to migrations`
7. `db: verify fresh and upgraded schema convergence`
8. `db: finalize migration safety verification` only if formatting or verification polish changes files

## Final Acceptance

Migration Safety is complete when:

- `schema_migrations` exists with `version INTEGER PRIMARY KEY`, `name TEXT NOT NULL`, and `applied_at TEXT NOT NULL`.
- `Registered()` returns ordered migrations and validation rejects invalid registration.
- Fresh databases create every current production table/index listed in this plan.
- The `v3.0.2` fixture upgrade preserves representative rows.
- Missing scheduler, proposed update, and swarm metric columns are backfilled.
- `Run` can execute twice without duplicating `schema_migrations` rows.
- `cmd/aura/main.go` runs migrations before settings store construction and before `telegram.New`.
- `cmd/debug_telegram_sandbox/main.go` runs migrations before settings store construction and before `telegram.New`, so the debug Telegram sandbox smoke command does not bypass migrations.
- Store constructors no longer lazily create production schemas from shared-pool constructors.
- Verification commands in Task 9 pass.
