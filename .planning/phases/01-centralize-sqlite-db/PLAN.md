# Phase 1 PLAN: DB Foundation

**Phase:** 1 of 6
**Addresses:** FIX-02
**Depends on:** none
**Created:** 2026-05-04

## Goal

Create the database foundation for Aura production startup by routing production SQLite access through a shared `*sql.DB` pool opened by `internal/db.Open`. Phase 1 owns the pool factory, PRAGMAs, constructor injection, shutdown ownership, and tests proving the open path.

The existing store schema creation and package-local migrate methods may remain unchanged in Phase 1. Phase 2 owns schema versioning and upgrade orchestration.

## Scope

Production DB surfaces in scope:
- `cmd/aura/main.go`
- `internal/telegram/setup.go`
- `internal/auth`
- `internal/scheduler`
- `internal/settings`
- `internal/search` embed cache
- `internal/search` SQLite FTS searcher
- `internal/swarm`

Debug commands and tests may create temp SQLite databases, but production startup must create the Aura database through `internal/db.Open`.

## Non-Goals

- Do not build a migration framework in Phase 1.
- Do not introduce schema version tables or upgrade orchestration in Phase 1.
- Do not rewrite existing schemas unless constructor injection requires a narrow adjustment.
- Do not force debug commands or tests to share the production startup pool.

## Work

### 1. Shared DB Pool

Create `internal/db` with a single exported `Open(path string) (*sql.DB, error)` factory for Aura's production database.

Requirements:
- Validate empty or whitespace-only paths.
- Open SQLite with the `modernc.org/sqlite` driver.
- Apply `PRAGMA journal_mode=WAL`.
- Apply `PRAGMA busy_timeout=5000`.
- Apply `PRAGMA foreign_keys=ON`.
- Apply existing performance PRAGMAs that are safe at open time.
- Ping after PRAGMAs so startup fails fast.
- Keep the SQLite driver import localized to `internal/db`.

### 2. Constructor Injection

Update production stores/search components so they accept the shared pool instead of opening their own production SQLite connections.

Targets:
- `internal/auth`
- `internal/scheduler`
- `internal/settings`
- `internal/search` embed cache
- `internal/search` SQLite FTS searcher
- `internal/swarm`

Keep existing schema and migrate methods in their current packages for this phase. The required behavior is that production wiring injects the shared pool.

### 3. Entrypoint Wiring

Update production startup to open the Aura database once through `internal/db.Open`.

Requirements:
- `cmd/aura/main.go` owns opening and closing the shared pool.
- `internal/telegram/setup.go` receives the shared pool and passes it to auth, scheduler, search, and swarm-related constructors.
- Stores and bot shutdown must not close the shared pool independently.

### 4. Tests

Add focused tests proving the open path and the affected constructor paths.

Required proof:
- A fresh temp DB reports `journal_mode=wal`.
- A fresh temp DB reports `busy_timeout=5000`.
- A fresh temp DB reports `foreign_keys=1`.
- Production packages in scope build/test with constructor injection.
- Static search shows production `sql.Open("sqlite"` calls have been removed from `internal` and `cmd` outside the DB foundation path.

## Risk Register

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Shared pool shutdown ownership becomes ambiguous | Medium | High | `cmd/aura/main.go` owns `Close`; stores and bot shutdown do not close the pool |
| Settings bootstrap breaks first-run startup | Medium | High | Open DB before settings construction and cover `cmd/aura` plus telegram setup tests |
| Swarm connection settings restrict the shared pool | Low | Medium | Remove per-store connection limits from production swarm wiring |
| Existing schema methods are mistaken for Phase 2 migration work | Medium | Medium | Keep schema/migrate methods unchanged unless needed for injection; defer schema versioning and upgrade orchestration to Phase 2 |

## Success Criteria

- Production startup opens the Aura database once with `internal/db.Open`.
- Production DB consumers in scope receive the shared pool by constructor injection.
- The shared pool has WAL, busy timeout, and foreign keys enabled on fresh DBs.
- Shutdown closes the shared pool once from `cmd/aura/main.go`.
- Phase 2 remains responsible for schema versioning and upgrade orchestration.
