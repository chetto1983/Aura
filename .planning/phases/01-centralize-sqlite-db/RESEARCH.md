# Phase 1 Research: Centralize SQLite DB

**Date:** 2026-05-04

## Current State

### SQLite Connection Inventory

| # | Location | Owner | Close guarded? | Notes |
|---|----------|-------|----------------|-------|
| 1 | `auth.OpenStore` | `telegram/setup.go:323` | Yes (`owned` bool) | `NewStoreWithDB` exists |
| 2 | `scheduler.OpenStore` | `telegram/setup.go:215` | No (always closes via `Close()`) | Has `DB()` accessor used by 5 consumers |
| 3 | `settings.OpenStore` | `cmd/aura/main.go:50` | Yes (`owned` bool) | Opens before bot exists |
| 4 | `swarm.OpenStore` | N/A (uses `NewStoreWithDB`) | Yes (`owned` bool) | Calls `SetMaxOpenConns(1)` + own `PRAGMA busy_timeout` |
| 5 | `search.OpenEmbedCache` | `telegram/setup.go:84` | N/A (has `Close()` but used standalone) | No ownership tracking |
| 6 | `search.newSqliteSearcher` | `search/search.go:70` | N/A (no `Close()` at all) | FTS5 virtual table |
| 7 | `swarm.NewStoreWithDB(schedStore.DB())` | `telegram/setup.go:230` | Not owned (passthrough) | Gets DB from scheduler pool |

**Total:** 6 `sql.Open` calls for same `DBPath`. swarm shares scheduler's connection via `DB()` accessor.

### Close Lifecycle Problems

Current `Bot.Stop()` shutdown order (bot.go:183-201):
1. Close archiver
2. Stop scheduler
3. `schedDB.Close()` — closes scheduler's pool
4. `authDB.Close()` — closes auth's pool
5. Close MCP clients

Missing: settings, embed cache, sqliteSearcher never explicitly closed. `main.go:55` defers `settingsStore.Close()` which races with `Bot.Stop()` since settings was opened on same file.

### Test Call Sites (25 files)

Tests that create their own SQLite connections:
- **auth**: `auth/store_test.go`, `tools/auth_test.go`, `api/auth_test.go`, `api/pending_test.go`, `telegram/bot_test.go`
- **scheduler**: `scheduler/scheduler_test.go`, `tools/scheduler_test.go`, `api/router_test.go`, `telegram/scheduler_handlers_test.go`, `scheduler/store_migration_test.go`
- **settings**: `settings/store_test.go`, `api/settings_test.go`
- **swarm**: `swarm/store_test.go`, `swarmtools/tools_test.go`, `api/swarm_test.go`
- **search**: `search/embed_cache_test.go`
- **summarizer**: `summarizer/applier_test.go`, `summarizer/proposals_test.go`
- **conversation**: `conversation/archive_internal_test.go`
- **api**: `api/summaries_test.go` (raw `sql.Open`)
- **cmd**: `cmd/debug_agent_jobs/main_test.go`

Pattern: Every test uses `filepath.Join(t.TempDir(), "mystore.db")` + `OpenStore(path)`.

### Swarm PRAGMA Conflict

`swarm/store.go:118` executes `PRAGMA busy_timeout = 5000` in `migrate()`. Once `internal/db` handles PRAGMAs, this duplicates work. `SetMaxOpenConns(1)` in `swarm/newStore` is aggressive — shared pool should not be limited per-store.

## Design Decisions

### D-05 (close cleanup strategy)

Each store currently handles `Close()` differently:
- **auth**: `owned` guard → safe to keep if we want tests that isolate auth, but production will use shared pool
- **scheduler**: always closes → dangerous; will be first store to open in `setup.go`, must NOT close shared pool
- **settings**: `owned` guard → same as auth
- **swarm**: `owned` guard → safe
- **sqliteSearcher**: no close → must not close shared pool

**Decision:** Remove `Close()` from all stores except via interface cleanup where needed for backward compat. Shared pool closed once by `main.go`.

### DB() Accessor Strategy

`scheduler.DB()` used by: swarm, summaries, proposals, archive, issues stores. After centralization, all get `*sql.DB` directly at construction — `DB()` accessor becomes redundant.

**Decision:** Keep `DB()` for migration compatibility (Phase 2 migrations framework may use it). Remove after Phase 2 if unused.

### Settings Bootstrap Order

`main.go:50` opens settings before bot exists to apply env overlay. After centralization, flow becomes:
1. `db.Open(cfg.DBPath)` — single pool, all PRAGMAs applied
2. `settings.NewStoreWithDB(db)` — no second open
3. `settings.ApplyToConfig(...)` — overlay env
4. If not bootstrapped, run setup wizard
5. Pass `db` to `telegram.New()` which passes to all stores

**Decision:** Open pool once at `main.go:46` (before current settings open). Pass DB to both settings and telegram.New.

## Test Migration Strategy

Each test type needs specific migration:

1. **Store unit tests** (auth, scheduler, settings, swarm): Replace `OpenStore(path)` → `db.Open(path)` + `NewStoreWithDB(db)`. Pattern is straightforward.

2. **Embed cache tests**: `OpenEmbedCache(dbPath, ...)` → `db.Open(dbPath)` + new constructor. Cache tests use multiple independent DBs per test, so constructor injection works.

3. **Raw sql.Open tests** (summaries, conversation): Replace `sql.Open("sqlite", path)` → `db.Open(path)`.

4. **Integration tests** (api/router, api/auth, telegram/bot): These wire multiple stores on same DB. Switch to shared `db.Open` + inject to all stores.

**Batch strategy:** Group by package. Do search → scheduler → auth → settings → swarm → api → telegram → cmd. Each batch can be committed independently.
