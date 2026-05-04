# Phase 1 PLAN: Centralize SQLite DB

**Phase:** 1 of 6
**Addresses:** FIX-02
**Depends on:** —
**Created:** 2026-05-04

## Goal

Replace 6 independent `sql.Open("sqlite", ...)` calls with one `*sql.DB` pool managed by `internal/db.Open()`. Apply WAL, busy_timeout, foreign_keys, and performance PRAGMAs at open time. Remove `Close()` from all stores — lifecycle owned by `cmd/aura/main.go`.

## Threat Model

- **Double-close:** Removing per-store `Close()` is the safest path. Shared pool closed once in `main.go` defer.
- **Settings bootstrap:** Currently opens first. Must not race with later pool open.
- **Swarm MaxOpenConns(1):** Must be removed — per-store connection limit on shared pool blocks concurrent readers.

## Task Breakdown

### Wave 1: internal/db package (no deps)

#### Task 1.1 — Create internal/db/db.go

Create the single DB factory. All PRAGMAs in one place.

**File:** `internal/db/db.go` (new)
**Changes:**
- Package `db` with single exported `func Open(path string) (*sql.DB, error)`
- `sql.Open("sqlite", path)` with `modernc.org/sqlite` driver
- Execute in order: `PRAGMA journal_mode=WAL`, `PRAGMA busy_timeout=5000`, `PRAGMA foreign_keys=ON`, `PRAGMA synchronous=NORMAL`, `PRAGMA cache_size=-20000`, `PRAGMA temp_store=MEMORY`, `PRAGMA mmap_size=30000000000`
- `PRAGMA page_size=4096` only for new DBs (skip if file exists)
- `db.Ping()` after PRAGMAs
- No `MaxOpenConns` limit, no connection lifetime limits
- Only package importing `modernc.org/sqlite` driver
- `go doc`-style package comment

**Verification:** `go build ./internal/db/`

---

#### Task 1.2 — Create internal/db/db_test.go

**File:** `internal/db/db_test.go` (new)
**Tests:**
- `TestOpen_WALEnabled` — opens temp DB, verifies `PRAGMA journal_mode` returns `wal`
- `TestOpen_BusyTimeout` — verifies `PRAGMA busy_timeout` = 5000
- `TestOpen_ForeignKeys` — verifies `PRAGMA foreign_keys` = 1
- `TestOpen_EmptyPath` — returns error
- `TestOpen_PingFailure` — non-writable path returns error
- `TestOpen_SchemaCreate` — `CREATE TABLE` + `INSERT` + `SELECT` round-trip works

**Verification:** `go test ./internal/db/`

---

### Wave 2: Store constructors (depends on Wave 1)

#### Task 2.1 — Refactor auth.Store

**File:** `internal/auth/store.go`
**Changes:**
- Remove `import _ "modernc.org/sqlite"`
- Remove `OpenStore(path string) (*Store, error)` function
- Remove `owned bool` field from `Store`
- Remove `Close()` method
- Keep `NewStoreWithDB(db *sql.DB) (*Store, error)` — unchanged
- Keep all CRUD methods, `migrate()`, schema — unchanged
- Update package doc to note that DB lifecycle is external

**Verification:** `go build ./internal/auth/`

---

#### Task 2.2 — Refactor scheduler.Store

**File:** `internal/scheduler/store.go`
**Changes:**
- Remove `import _ "modernc.org/sqlite"`
- Remove `OpenStore(path string) (*Store, error)` function
- Remove `Close()` method
- Keep `NewStoreWithDB(db *sql.DB) (*Store, error)` — unchanged
- Keep `DB() *sql.DB` accessor (Phase 2 may use it)
- Keep all CRUD, migrations, schema — unchanged

**Verification:** `go build ./internal/scheduler/`

---

#### Task 2.3 — Refactor settings.Store

**File:** `internal/settings/store.go`
**Changes:**
- Remove `import _ "modernc.org/sqlite"`
- Remove `OpenStore(path string) (*Store, error)` function
- Remove `owned bool` field from `Store`
- Remove `Close()` method
- Keep `NewStoreWithDB(db *sql.DB) (*Store, error)` — unchanged
- Keep all getter/setter methods — unchanged

**Verification:** `go build ./internal/settings/`

---

#### Task 2.4 — Refactor swarm.Store

**File:** `internal/swarm/store.go`
**Changes:**
- Remove `import _ "modernc.org/sqlite"`
- Remove `OpenStore(path string) (*Store, error)` function
- Remove `owned bool` field from `Store`
- Remove `Close()` method
- Remove `db.SetMaxOpenConns(1)` from `newStore()` — shared pool must not be restricted
- Remove `PRAGMA busy_timeout = 5000` from `migrate()` — `internal/db` handles this
- Keep `NewStoreWithDB(db *sql.DB) (*Store, error)` — unchanged
- Keep `DB() *sql.DB` — unchanged

**Verification:** `go build ./internal/swarm/`

---

#### Task 2.5 — Refactor EmbedCache

**File:** `internal/search/embed_cache.go`
**Changes:**
- Remove `import _ "modernc.org/sqlite"`
- Remove `OpenEmbedCache(dbPath, model, inner, logger)` function
- Add `NewEmbedCacheWithDB(db *sql.DB, model string, inner chromem.EmbeddingFunc, logger *slog.Logger) (*EmbedCache, error)`
- Remove `Close()` method
- Schema creation `CREATE TABLE IF NOT EXISTS embedding_cache` moves into new constructor
- Keep `Embed`, `Stats`, `EmbedFunc` — unchanged

**Verification:** `go build ./internal/search/`

---

#### Task 2.6 — Refactor sqliteSearcher

**File:** `internal/search/sqlite.go`
**Changes:**
- Remove `import _ "modernc.org/sqlite"`
- Remove `newSqliteSearcher(dbPath string, logger)` function
- Add `newSqliteSearcherWithDB(db *sql.DB, logger *slog.Logger) (*sqliteSearcher, error)`
- Schema `CREATE VIRTUAL TABLE IF NOT EXISTS wiki_documents` moves into new constructor
- Remove `Ping`/`Close` from old constructor
- Keep `search`, `indexDocument`, `clear`, `escapeFTS5Query` — unchanged

**Verification:** `go build ./internal/search/`

---

### Wave 3: Search engine wiring (depends on Wave 2)

#### Task 3.1 — Update NewEngineWithFallback

**File:** `internal/search/search.go`
**Changes:**
- `NewEngineWithFallback` signature changes from `(wikiDir, embedFn, dbPath, logger)` to `(wikiDir, embedFn, db *sql.DB, logger)`
- Replace `newSqliteSearcher(dbPath, logger)` → `newSqliteSearcherWithDB(db, logger)`
- Remove the fallback path that opens its own connection — DB already open

**Verification:** `go build ./internal/search/`

---

### Wave 4: Entrypoint wiring (depends on Waves 1-3)

#### Task 4.1 — Refactor cmd/aura/main.go

**File:** `cmd/aura/main.go`
**Changes:**
- Add `import "github.com/aura/aura/internal/db"`
- After `config.Load()` (line 39), add:
  ```go
  pool, err := db.Open(cfg.DBPath)
  if err != nil {
      logger.Error("failed to open database", "error", err)
      os.Exit(1)
  }
  defer pool.Close()
  ```
- Replace `settings.OpenStore(cfg.DBPath)` → `settings.NewStoreWithDB(pool)` (line 50)
- Remove `defer settingsStore.Close()` (line 55) — settings no longer owns close
- Remove settings error nil-check for setup wizard (settings always succeeds if pool opened)
- Pass `pool` to `telegram.New(cfg, settingsStore, pool, logger)` (need to update signature)

**Verification:** `go build ./cmd/aura/`

---

#### Task 4.2 — Update telegram.New signature

**File:** `internal/telegram/setup.go`
**Changes:**
- Change signature: `func New(cfg *config.Config, settingsStore *settings.Store, pool *sql.DB, logger *slog.Logger) (*Bot, error)`
- Replace `auth.OpenStore(schedDBPath)` → `auth.NewStoreWithDB(pool)` (line 323)
- Replace `scheduler.OpenStore(schedDBPath)` → `scheduler.NewStoreWithDB(pool)` (line 215, remove schedDBPath variable)
- Replace `search.OpenEmbedCache(cfg.DBPath, ...)` → `search.NewEmbedCacheWithDB(pool, ...)` (line 84)
- Replace `search.NewEngineWithFallback(..., cfg.DBPath, ...)` → `search.NewEngineWithFallback(..., pool, ...)` (line 95)
- Remove `schedDBPath` variable entirely (lines 211-214)
- Keep all tool registration, swarm wiring — unchanged (they get DB from `schedStore.DB()` already)

**Verification:** `go build ./internal/telegram/`

---

#### Task 4.3 — Update Bot struct and shutdown

**File:** `internal/telegram/bot.go`
**Changes:**
- In `Bot.Stop()`):
  - Replace `schedDB.Close()` + `authDB.Close()` with single `pool.Close()` (but DO NOT close — pool owned by main.go)
  - Remove `schedDB.Close()` and `authDB.Close()` calls entirely
  - If embed cache has no Close() anymore, remove that too
- `schedDB` field in Bot struct still needed for `DB()` accessor (other consumers use it) — keep
- `authDB` field still referenced by API deps — keep

**Note:** `pool` not stored on Bot struct. Main.go owns lifecycle. Bot.Stop() cleans up archiver, scheduler, MCP only.

**Verification:** `go build ./internal/telegram/`

---

### Wave 5: Test migration (depends on Waves 1-4)

#### Task 5.1 — Migrate internal/db tests (already done in 1.2)

No action needed.

---

#### Task 5.2 — Migrate search tests

**Files:** `internal/search/embed_cache_test.go`
**Changes:**
- Replace `OpenEmbedCache(dbPath, ...)` → `db.Open(dbPath)` + `NewEmbedCacheWithDB(db, ...)`
- Add `defer db.Close()` after each `db.Open()` call
- 8 call sites across 4 test functions

**Verification:** `go test ./internal/search/`

---

#### Task 5.3 — Migrate scheduler tests

**Files:** `internal/scheduler/scheduler_test.go`, `internal/scheduler/store_migration_test.go`
**Changes:**
- Replace `OpenStore(dbPath)` → `db.Open(dbPath)` + `NewStoreWithDB(db)`
- Replace `sql.Open("sqlite", dbPath)` → `db.Open(dbPath)`
- Remove `_ "modernc.org/sqlite"` imports
- 8 call sites across 2 test files

**Verification:** `go test ./internal/scheduler/`

---

#### Task 5.4 — Migrate auth tests

**Files:** `internal/auth/store_test.go`
**Changes:**
- Replace `OpenStore(path)` → `db.Open(path)` + `NewStoreWithDB(db)`
- 1 call site

**Verification:** `go test ./internal/auth/`

---

#### Task 5.5 — Migrate settings tests

**Files:** `internal/settings/store_test.go`
**Changes:**
- Replace `OpenStore(path)` → `db.Open(path)` + `NewStoreWithDB(db)`
- Update `OpenStore("")` / `OpenStore("   ")` error tests → test `db.Open("")` / `db.Open("   ")` errors
- 4 call sites

**Verification:** `go test ./internal/settings/`

---

#### Task 5.6 — Migrate swarm tests

**Files:** `internal/swarm/store_test.go`, `internal/swarmtools/tools_test.go`
**Changes:**
- Replace `OpenStore(path)` → `db.Open(path)` + `NewStoreWithDB(db)`
- Replace `sql.Open("sqlite", path)` → `db.Open(path)`
- 7 call sites across 2 test files

**Verification:** `go test ./internal/swarm/ ./internal/swarmtools/`

---

#### Task 5.7 — Migrate API tests

**Files:** `internal/api/router_test.go`, `internal/api/auth_test.go`, `internal/api/pending_test.go`, `internal/api/swarm_test.go`, `internal/api/settings_test.go`, `internal/api/summaries_test.go`
**Changes:**
- Replace per-store `OpenStore(path)` → single `db.Open(path)` + inject to all stores
- Replace `sql.Open("sqlite", dbPath)` → `db.Open(dbPath)`
- `router_test.go`: scheduler and auth stores on same DB → open once, inject twice
- 7 call sites across 6 test files

**Verification:** `go test ./internal/api/`

---

#### Task 5.8 — Migrate telegram tests

**Files:** `internal/telegram/bot_test.go`, `internal/telegram/scheduler_handlers_test.go`
**Changes:**
- Replace `auth.OpenStore(path)` → `db.Open(path)` + `auth.NewStoreWithDB(db)`
- Replace `scheduler.OpenStore(path)` → `db.Open(path)` + `scheduler.NewStoreWithDB(db)`
- 4 call sites across 2 test files

**Verification:** `go test ./internal/telegram/`

---

#### Task 5.9 — Migrate tools tests

**Files:** `internal/tools/auth_test.go`, `internal/tools/scheduler_test.go`
**Changes:**
- Replace `auth.OpenStore(path)` → `db.Open(path)` + `auth.NewStoreWithDB(db)`
- Replace `scheduler.OpenStore(path)` → `db.Open(path)` + `scheduler.NewStoreWithDB(db)`
- 2 call sites across 2 test files

**Verification:** `go test ./internal/tools/`

---

#### Task 5.10 — Migrate summarizer tests

**Files:** `internal/conversation/summarizer/applier_test.go`, `internal/conversation/summarizer/proposals_test.go`
**Changes:**
- Replace `sql.Open("sqlite", ...)` → `db.Open(path)`
- 2 call sites across 2 test files

**Verification:** `go test ./internal/conversation/summarizer/`

---

#### Task 5.11 — Migrate conversation tests

**Files:** `internal/conversation/archive_internal_test.go`
**Changes:**
- Replace `sql.Open("sqlite", dbPath)` → `db.Open(dbPath)`
- 1 call site

**Verification:** `go test ./internal/conversation/`

---

#### Task 5.12 — Migrate cmd tests

**Files:** `cmd/debug_agent_jobs/main_test.go`
**Changes:**
- Replace `scheduler.OpenStore(rep.DBPath)` → `db.Open(rep.DBPath)` + `scheduler.NewStoreWithDB(db)`
- 1 call site

**Verification:** `go test ./cmd/debug_agent_jobs/`

---

#### Task 5.13 — Remove blank driver imports

After all tests pass, remove any remaining `_ "modernc.org/sqlite"` blank imports from non-db packages. The only file importing `modernc.org/sqlite` should be `internal/db/db.go`.

**Verification:** `rg "_ \"modernc.org/sqlite\"" --type go` returns only `internal/db/db.go`

---

### Wave 6: Full verification

#### Task 6.1 — Full build and test

```bash
go fmt ./...
go build ./...
go vet ./...
go test ./...
```

All must pass with zero errors.

---

#### Task 6.2 — Verify success criteria from ROADMAP.md

1. **Single `sql.Open`**: `rg "sql\.Open\(\"sqlite\"" --type go` returns only `internal/db/db.go`
2. **WAL mode active**: `db_test.go` verifies via `PRAGMA journal_mode`
3. **All store tests pass**: `go test ./internal/auth/ ./internal/scheduler/ ./internal/settings/ ./internal/swarm/ ./internal/search/`
4. **PRAGMAs applied**: verified in `db_test.go`

---

## Dependency Order

```
Wave 1 (db package)
  └→ Wave 2 (store constructors)
       └→ Wave 3 (search engine)
            └→ Wave 4 (entrypoints)
                 └→ Wave 5 (test migration, can be parallel per package)
                      └→ Wave 6 (verification)
```

Wave 5 tasks can run partially in parallel since they're per-package and independent once constructors exist.

## Risk Register

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Swarm `SetMaxOpenConns(1)` removal causes swarm write contention | Low | Medium | Swarm uses `BEGIN IMMEDIATE` — serial already; concurrent readers fine with WAL |
| Settings bootstrap breaks first-run wizard | Medium | High | Settings constructor unchanged except no owned bool; wizard path tested in setup_test.go |
| Test that shares DB across stores fails from WAL contention | Low | Low | Tests use temp dirs, no concurrent access |
| Forgotten `_ "modernc.org/sqlite"` import breaks a debug tool build | Low | Low | Task 5.13 catches; debug tools import `db` package now |
