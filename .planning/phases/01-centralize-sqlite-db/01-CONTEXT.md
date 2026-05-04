# Phase 1: Centralize SQLite DB - Context

**Gathered:** 2026-05-04
**Status:** Ready for planning

<domain>
## Phase Boundary

Replace 5 independent `sql.Open` calls with a single `*sql.DB` pool managed by a new `internal/db` package. Apply WAL journal mode, busy timeout, foreign key enforcement, and performance PRAGMAs at open time. Remove `OpenStore(path)` from all stores — only `NewStoreWithDB(db)` constructor injection remains. The lifecycle (open/close) is owned by `cmd/aura/main.go`.

</domain>

<decisions>
## Implementation Decisions

### Connection Pool Tuning
- **D-01:** Apply full SQLite performance PRAGMAs at open time: `journal_mode=WAL`, `busy_timeout=5000`, `foreign_keys=ON`, `synchronous=NORMAL`, `cache_size=-20000` (20MB), `mmap_size=30000000000`, `temp_store=MEMORY`, `page_size=4096` (only for newly created databases — do not change on existing).
- **D-02:** No MaxOpenConns limit (SQLite WAL supports concurrent readers + one writer; let the driver manage connections).
- **D-03:** No connection lifetime limits (SQLite in WAL mode works fine with long-lived connections; no need to reap them).
- **D-04:** Only `internal/db` imports the `modernc.org/sqlite` driver. Stores receive a plain `*sql.DB`. Remove blank driver imports from all store packages.

### Constructor Migration Strategy
- **D-05:** Remove `OpenStore(path)` from every store (`auth`, `scheduler`, `settings`, `swarm`, `search.EmbedCache`, `search.sqliteSearcher`). Only `NewStoreWithDB(db)` constructors remain.
- **D-06:** Tests that create temp SQLite files migrate to using `db.Open(tmpFile)` + `NewStoreWithDB(db)` directly — no test-specific helper.
- **D-07:** `search.EmbedCache` and `search.sqliteSearcher` (FTS5) merge into the shared connection pool — no separate `*sql.DB` instances.
- **D-08:** `internal/db` package exposes `Open(path string) (*sql.DB, error)` — that's the API surface. No wrappers, no helpers. All PRAGMAs happen inside `Open()`.

### the agent's Discretion
- How `Close()` inconsistencies are cleaned up (auth/settings use `owned` bool; scheduler always closes; sqliteSearcher has no Close) — the agent decides the cleanest path as long as the shared DB isn't double-closed.
- Whether to keep `DB()` accessor methods on stores (currently scheduler and swarm expose it) or remove them once all consumers get the DB from the single pool.
- Exact test migration order and grouping — the agent decides the most efficient batch strategy for updating ~40 test files.
- How `settingsStore` in `main.go` accesses the DB during the early bootstrap phase before the bot is created — the agent decides whether to open the shared pool earlier or defer settings application.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Architecture & Patterns
- `.planning/codebase/CONVENTIONS.md` §Database Access — existing SQLite patterns (idempotent migrations, context propagation, transaction lifecycle, connection ownership)
- `.planning/codebase/CONCERNS.md` §Multiple SQLite connection pools + §No WAL mode configured — the problems this phase fixes
- `.planning/codebase/STRUCTURE.md` — full store layout and dependency graph

### Existing Stores (for constructor and pattern reference)
- `internal/auth/store.go` — auth store with `owned` guard, `NewStoreWithDB` already exists
- `internal/scheduler/store.go` — scheduler store, de facto migration authority, `DB()` accessor
- `internal/settings/store.go` — settings store, `NewStoreWithDB` already exists, first to open in main.go
- `internal/swarm/store.go` — swarm store, sets `MaxOpenConns(1)` and `PRAGMA busy_timeout`, `DB()` accessor
- `internal/search/embed_cache.go` — embedding cache with standalone `OpenEmbedCache`
- `internal/search/sqlite.go` — FTS5 fallback searcher, no Close()

### Entrypoint
- `cmd/aura/main.go` — DB lifecycle owner, settings bootstrap before bot creation

### Requirements
- `.planning/REQUIREMENTS.md` FIX-02 — authoritative requirement: WAL mode, busy timeout, foreign key enforcement, single `*sql.DB`

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `NewStoreWithDB(db)` pattern already exists on 5 stores (auth, scheduler, settings, swarm) — not building from scratch
- 5 stores already share `schedStore.DB()` (swarm, archive, summaries, review applier, issues) — proven pattern
- `auth.Store.owned bool` guard — established close ownership tracking pattern

### Established Patterns
- Schema as string constants with `CREATE TABLE IF NOT EXISTS` — idempotent (keep for backward compat until Phase 2)
- `defer tx.Rollback()` + explicit `tx.Commit()` — consistent transaction lifecycle
- Always `db.ExecContext(ctx, ...)` / `db.QueryRowContext(ctx, ...)` — context propagation

### Integration Points
- `cmd/aura/main.go` line 50: `settings.OpenStore(cfg.DBPath)` — must become `db.Open(cfg.DBPath)` + `NewStoreWithDB(db)`
- `internal/telegram/setup.go` line 216: `scheduler.OpenStore(schedDBPath)` — must become same injection
- `internal/telegram/setup.go` line 323: `auth.OpenStore(schedDBPath)` — must become same injection
- `internal/telegram/setup.go` line 84: `search.OpenEmbedCache(cfg.DBPath, ...)` — must become constructor injection
- `internal/search/search.go` line 70: `newSqliteSearcher(dbPath, ...)` — must become constructor injection

</code_context>

<specifics>
## Specific Ideas

None — the approach follows existing patterns and ROADMAP.md deliverables precisely.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 01-centralize-sqlite-db*
*Context gathered: 2026-05-04*
