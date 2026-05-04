# v1.0 Production Readiness Design

Date: 2026-05-04
Status: approved for planning
Source of truth: `.planning/codebase/CONCERNS.md`

## Purpose

v1.0 makes Aura safe to run as the daily production build after the `v3.0.2`
Pyodide release. It is a hardening milestone, not a feature milestone.

The milestone closes the concerns that can affect data integrity, upgrade
safety, dashboard security, and release confidence. It defers broad polish and
low-risk refactors to v1.1.

## Milestone Boundary

v1.0 is in scope when the work protects production data, prevents silent memory
loss, reduces security exposure, or proves the release can be installed and run.

In scope:

- one shared SQLite connection pool with WAL, busy timeout, and foreign keys;
- deterministic versioned migrations and upgrade checks;
- observable conversation archive failures;
- dashboard token expiry;
- settings secret redaction in API/dashboard responses;
- focused Telegram critical-path tests;
- final release gates for Go, web, sandbox, packaging, and manual Windows smoke.

Deferred to v1.1:

- file tool split and other broad large-file refactors;
- tray coverage polish beyond any minimal safety fix needed for release;
- telebot beta monitoring docs;
- full settings at-rest encryption unless redaction proves insufficient;
- arbitrary package coverage targets outside Telegram critical paths.

## Architecture

v1.0 uses a small internal persistence layer. It does not introduce an ORM.

`internal/db` owns the production SQLite open path:

- `Open(path string) (*sql.DB, error)` opens the database;
- the package applies SQLite PRAGMAs in one place;
- the package is the only production code that imports the SQLite driver for
  database startup.

`cmd/aura/main.go` owns process-level database lifecycle:

- load config and `.env`;
- call `internal/db.Open(cfg.DBPath)`;
- run migrations;
- pass the shared `*sql.DB` to stores and runtime services;
- close workers first, then close the pool once during shutdown.

Domain stores keep explicit SQL. They receive the shared pool through
constructors such as `NewStoreWithDB`. Stores do not own `Close()` for the
shared pool, and migrations do not run lazily from random stores after startup.

## Data Flow

Startup flow:

1. Load config.
2. Open the shared DB pool through `internal/db.Open`.
3. Apply WAL, busy timeout, foreign keys, and other approved PRAGMAs.
4. Run versioned migrations.
5. Build stores from the shared pool.
6. Start Telegram, API, scheduler, search, settings, auth, and swarm services.

Request flow:

- Telegram and API handlers call domain services and stores.
- Stores execute explicit SQL through the shared pool.
- Archive writes return observable errors instead of silently losing memory.
- Settings responses redact secret values before they cross the API boundary.

Upgrade flow:

- A fresh database creates the complete schema.
- A real `v3.0.2` database upgrades successfully on first boot.
- Running migrations twice is a no-op.
- Fresh and upgraded schemas converge.
- Existing dashboard tokens keep the planned grace behavior until expiry rules
  take over.

## Phases

### 1. DB Foundation

Create the shared DB pool and move production startup to it. Apply SQLite
PRAGMAs once at open time and remove independent production connection pools.

Success means only one production `sql.Open("sqlite", ...)` path remains, WAL
is active, `busy_timeout` is set, and foreign keys are enabled.

### 2. Migration Safety

Add a versioned migration runner with deterministic ordering and idempotent
upgrade behavior. This phase owns fresh-install and upgrade safety.

Success means migrations are tracked, repeatable, and tested against fresh and
upgraded databases.

### 3. Memory Reliability

Make conversation archive failures visible and test the core memory write path.
Aura's product value depends on durable memory, so silent archive loss blocks
v1.0.

Success means failed archive appends are logged or returned through an observable
path, and critical archive behavior has focused tests.

### 4. Dashboard Security

Add dashboard token expiry and redact settings secrets from API/dashboard
responses. At-rest encryption stays deferred unless redaction leaves a v1.0
security gap.

Success means expired tokens are rejected with a distinct state, issued tokens
have expiry metadata, and API responses do not expose raw API keys.

### 5. Telegram Regression Harness

Add focused tests for the Telegram paths most likely to break production:
conversation handling, streaming edits, document/OCR trigger paths, auth access,
and archive failure behavior.

Success means critical flows are covered, not merely that an arbitrary coverage
number improves.

### 6. Release Gate

Run the final automated and manual checks before tagging v1.0.

Automated gates:

- `go fmt ./...`
- `go test ./...`
- `go vet ./...`
- `go build ./...`
- web install, lint, and build
- sandbox smoke, tool smoke, and artifact smoke

Migration gates:

- fresh DB creates full schema;
- `v3.0.2` DB upgrades cleanly;
- migrations are idempotent;
- PRAGMAs are active on startup;
- only one production SQLite open path remains.

Manual release gates:

- clean Windows unzip and first-run setup;
- Telegram `/start` and dashboard token login;
- PDF ingest/OCR/wiki path;
- reminder scheduling and firing;
- `execute_code` returns CSV/PNG artifacts;
- tray opens the dashboard on Windows;
- backup/restore of `.env`, `aura.db`, wiki, and source data works.

## Out Of Scope

v1.0 does not add new user-facing product features. It does not introduce
WebSockets, a mobile app, Postgres, sqlite-vector, installers, auto-update,
major dependency swaps, telebot replacement, full key rotation, or a larger
Pyodide package profile unless a release smoke fails.

## Open Decisions Resolved

- ORM: rejected for v1.0. Explicit SQL plus a lightweight internal DB/migration
  layer keeps the milestone smaller and easier to verify.
- Settings encryption: deferred unless secret redaction is insufficient.
- Current phase work: `.planning/` remains the active planning surface;
  historical `docs/` plans are not revived.

