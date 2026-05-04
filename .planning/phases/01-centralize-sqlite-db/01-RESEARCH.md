# Phase 1: Centralize SQLite DB - Research

**Researched:** 2026-05-04
**Domain:** SQLite connection pool consolidation, constructor injection refactoring
**Confidence:** HIGH

## Summary

The Aura codebase currently opens 6 independent `sql.Open("sqlite", ...)` connections to the same `aura.db` file. Several stores (auth, settings, swarm) use an `owned` bool guard to conditionally close; the scheduler always closes regardless; the FTS5 searcher never closes at all. The swarm store additionally calls `SetMaxOpenConns(1)` on any DB it receives, which would silently throttle a shared pool.

The migration path is straightforward mechanically (replace `OpenStore(path)` with `db.Open(path)` + `NewStoreWithDB(db)`) but has one non-trivial sequencing problem: `cmd/aura/main.go` currently opens settings before the bot exists, and the first-run wizard path also needs the DB available early. Four categories of cleanup risk must be addressed: the scheduler's unconditional `Close()`, the swarm's `SetMaxOpenConns(1)` poisoning the shared pool, the FTS5 searcher's missing `Close()`, and the embed cache's standalone `Close()` that must not double-close a shared connection.

**Primary recommendation:** Create `internal/db.Open(path)` with all PRAGMAs applied at open time, open the pool once at the top of `main.go` before any store construction, pass it to every store via `NewStoreWithDB`, remove `Close()` from all stores, and own the single `db.Close()` in `main.go`'s shutdown path.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| SQLite connection lifecycle | API / Backend (cmd/aura) | -- | Single process opens and closes the pool; stores are pure consumers |
| PRAGMA application | API / Backend (internal/db) | -- | Applied once at open time by the `db` package; no per-store tweaks |
| Schema migration (per-store) | API / Backend (each store) | -- | Stores still own their `CREATE TABLE IF NOT EXISTS` -- idempotent, runs on constructor injection |
| Connection injection | API / Backend (cmd/aura, telegram/setup) | -- | Entrypoints pass `*sql.DB` to constructors; stores never open their own connections |
| Shutdown ordering | API / Backend (cmd/aura) | -- | Close archiver/stop scheduler before closing DB; DB close is the last SQLite operation |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| modernc.org/sqlite | v1.38.2 [VERIFIED: go.mod] | Pure-Go SQLite driver | Already used by all stores; no CGo dependency |
| database/sql | stdlib | Connection pool, context propagation | Go standard library; all stores already use it |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| None | -- | -- | This phase is pure refactoring -- no new dependencies |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| modernc.org/sqlite | mattn/go-sqlite3 (CGo) | CGo requires C toolchain, breaks cross-compilation; modernc is already in use |

**Installation:**
No new dependencies. All imports already exist in go.mod.

## Architecture Patterns

### System Architecture Diagram

```
                          cmd/aura/main.go
                               |
                    internal/db.Open(dbPath)
                    (applies all PRAGMAs once)
                               |
                         *sql.DB pool
                               |
               +---------------+---------------+
               |               |               |
          settings          scheduler        embedCache
          (NewStoreWithDB)  (NewStoreWithDB)  (NewEmbedCacheWithDB)
               |               |               |
               |    +----------+----------+
               |    |          |          |
               |  swarm   auth      archive
               |  (NewStore  (NewStore   (NewArchive
               |   WithDB)   WithDB)     Store)
               |               |
               +-------+-------+--------+
                       |                |
                   API router      summarizer
                  (uses schedStore, (uses archive,
                   authStore,        summaries,
                   summariesStore,   review applier)
                   issuesStore,
                   swarmStore)

Shutdown: archiver.Close -> sched.Stop -> db.Close (in main.go, after bot.Stop)
```

### Recommended Project Structure
```
internal/
├── db/
│   └── db.go                    # Open(path string) (*sql.DB, error)
├── auth/
│   └── store.go                 # Remove OpenStore, keep NewStoreWithDB, remove Close
├── scheduler/
│   ├── store.go                 # Remove OpenStore, keep NewStoreWithDB, remove Close
│   └── testdb.go                # Replace sql.Open -> db.Open, remove blank driver import
├── settings/
│   └── store.go                 # Remove OpenStore, keep NewStoreWithDB, remove Close
├── swarm/
│   └── store.go                 # Remove OpenStore, keep NewStoreWithDB, remove Close, remove SetMaxOpenConns(1)
├── search/
│   ├── embed_cache.go           # Remove OpenEmbedCache, add NewEmbedCacheWithDB, remove Close
│   └── sqlite.go                # Replace newSqliteSearcher with newSqliteSearcherWithDB(db, logger)
```

### Pattern 1: Constructor Injection (Existing, Preserved)
**What:** Stores expose `NewStoreWithDB(db *sql.DB) (*Store, error)` that calls idempotent `migrate()`.
**When to use:** Every store in this phase. Already exists on auth, settings, scheduler, swarm.
**Example:**
```go
// Source: internal/settings/store.go (existing pattern, kept)
func NewStoreWithDB(db *sql.DB) (*Store, error) {
    s := &Store{db: db, now: time.Now, owned: false}
    if err := s.migrate(); err != nil {
        return nil, err
    }
    return s, nil
}
```

### Pattern 2: Centralized DB Open Factory (NEW)
**What:** `internal/db.Open(path)` opens the SQLite file once, applies all PRAGMAs, registers the driver.
**When to use:** Called exactly once at the top of `cmd/aura/main.go` and in tests that need independent temp DBs.
**Example:**
```go
// NEW: internal/db/db.go
package db

import (
    "database/sql"
    "fmt"
    _ "modernc.org/sqlite"
)

func Open(path string) (*sql.DB, error) {
    db, err := sql.Open("sqlite", path)
    if err != nil {
        return nil, fmt.Errorf("db: open %q: %w", path, err)
    }
    if err := db.Ping(); err != nil {
        db.Close()
        return nil, fmt.Errorf("db: ping %q: %w", path, err)
    }
    pragmas := []string{
        "PRAGMA journal_mode=WAL",
        "PRAGMA busy_timeout=5000",
        "PRAGMA foreign_keys=ON",
        "PRAGMA synchronous=NORMAL",
        "PRAGMA cache_size=-20000",
        "PRAGMA mmap_size=30000000000",
        "PRAGMA temp_store=MEMORY",
    }
    for _, p := range pragmas {
        if _, err := db.Exec(p); err != nil {
            db.Close()
            return nil, fmt.Errorf("db: %s: %w", p, err)
        }
    }
    // page_size cannot be changed on existing databases; only warn
    if _, err := db.Exec("PRAGMA page_size=4096"); err != nil {
        // Silently ignore -- page_size is set at DB creation time
    }
    // D-02: No MaxOpenConns limit
    // D-03: No connection lifetime limits
    return db, nil
}
```

### Pattern 3: Store Receives Plain *sql.DB (Existing, Enforced)
**What:** Stores receive a plain `*sql.DB` interface. No import of the SQLite driver in store packages.
**When to use:** All stores after this phase. Only `internal/db` imports `modernc.org/sqlite`.
**Example:**
```go
// Store does NOT import _ "modernc.org/sqlite"
// Store receives *sql.DB from external caller
func NewStoreWithDB(db *sql.DB) (*Store, error) {
    s := &Store{db: db}
    if err := s.migrate(); err != nil {
        return nil, err
    }
    return s, nil
}
```

### Anti-Patterns to Avoid
- **Store owning lifecycle after injection:** After this phase, NO store closes `db`. Only `main.go` closes the single pool. Any store that still calls `db.Close()` is a bug -- it will close the shared connection while other stores are still using it.
- **SetMaxOpenConns on shared pool:** The current `swarm/newStore()` unconditionally calls `db.SetMaxOpenConns(1)`. When the pool is shared, this throttles ALL stores to a single connection. Remove this call entirely.
- **Multiple db.Open calls in production path:** `main.go` must call `db.Open` exactly once. All stores get the same `*sql.DB` reference. Debug tools and tests call `db.Open` independently for their temp databases.
- **Blank driver imports in store packages:** After this phase, `_ "modernc.org/sqlite"` appears only in `internal/db/db.go`. Remove it from all store packages (`auth`, `scheduler`, `settings`, `swarm`, `search`).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| SQLite connection pool | Custom pool manager | database/sql built-in pool | Go's sql.DB already handles connection pooling, concurrent reads, and lifecycle; no wrapper needed |
| Connection lifecycle tracking | Per-store `owned bool` + `Close()` | Single owner in main.go | One `defer db.Close()` in main.go is simpler and prevents double-close bugs |
| Schema migrations | Custom migration runner | Per-store `migrate()` called by constructor | Phase 2 introduces versioned migrations; keep per-store idempotent CREATE TABLE for now |
| Driver registration | Multiple `_ "modernc.org/sqlite"` imports | Single import in `internal/db` | Go registers drivers globally; importing once is sufficient and prevents confusion about ownership |

**Key insight:** The existing `owned` guard pattern in auth/settings is already correct for non-owned stores -- they correctly skip Close(). The problem is the *inconsistency*: scheduler always closes, swarm poisons MaxOpenConns, and sqliteSearcher never closes. Centralizing lifecycle in main.go eliminates all these edge cases.

## Common Pitfalls

### Pitfall 1: Scheduler.Close() Always Closes the DB
**What goes wrong:** `scheduler.Store.Close()` unconditionally calls `s.db.Close()` with no ownership guard. After switching to `NewStoreWithDB`, calling `bot.Stop()` would close the shared pool while auth/settings/swarm still hold references.
**Why it happens:** The scheduler was historically the first store to open and the de facto connection owner. Other stores already shared via `DB()`.
**How to avoid:** Remove `Close()` from scheduler.Store entirely. The single pool lifecycle moves to main.go. The `bot.Stop()` method must NOT close schedDB -- only close archiver and stop the scheduler goroutine.
**Warning signs:** `bot.Stop()` calls `schedDB.Close()`. This line must be removed.

### Pitfall 2: Swarm Store Calls SetMaxOpenConns(1) on Any DB
**What goes wrong:** `swarm.newStore()` unconditionally calls `db.SetMaxOpenConns(1)`. When using `NewStoreWithDB`, this mutates the shared `*sql.DB` pool, limiting ALL stores (scheduler, auth, settings, embed cache, FTS5) to one concurrent connection.
**Why it happens:** Swarm was originally designed for a standalone DB with serialized access patterns. The `SetMaxOpenConns(1)` was intended to prevent concurrent writes on swarm tables, but it was set on the pool rather than enforced per-transaction.
**How to avoid:** Remove the `db.SetMaxOpenConns(1)` call from `newStore()`. Per D-02, no MaxOpenConns limit is applied. SQLite WAL mode handles concurrent readers + one writer natively.
**Warning signs:** `internal/swarm/store.go:97` -- `db.SetMaxOpenConns(1)` must be deleted.

### Pitfall 3: Settings Bootstrap Before Bot Creation
**What goes wrong:** `main.go:50` opens settings to apply the settings overlay before the bot is created. The first-run wizard (line 62-90) also needs settingsStore available. If `db.Open` is deferred until after bot creation, settings application and the wizard break.
**Why it happens:** Settings must be applied to config before the bot can be constructed, because config values like LLM models affect bot behavior.
**How to avoid:** Open the shared pool at line ~47 BEFORE `settings.OpenStore` on line 50. Replace `settings.OpenStore(cfg.DBPath)` with `db.Open(cfg.DBPath)` followed by `settings.NewStoreWithDB(pool)`.
**Warning signs:** Cannot move `db.Open` after bot creation -- must be before line 50.

### Pitfall 4: FTS5 sqliteSearcher Has No Close() Method
**What goes wrong:** `sqliteSearcher` opens its own `*sql.DB` via `newSqliteSearcher()` but never exposes a Close method. After switching to shared pool, we no longer need to worry about closing it, but the existing leak should be noted.
**Why it happens:** FTS5 was added as a fallback; the original author likely assumed the process lifecycle would handle cleanup.
**How to avoid:** No Close needed after this phase -- the shared pool handles it. Just remove the raw `sql.Open` from `newSqliteSearcher` and replace with `newSqliteSearcherWithDB(db *sql.DB, logger)`.
**Warning signs:** `internal/search/sqlite.go:21` -- the `sql.Open` and `defer db.Close()` on error paths must be removed.

### Pitfall 5: Embed Cache Persists-Across-Opens Test
**What goes wrong:** `TestEmbedCache_PersistsAcrossOpens` in `embed_cache_test.go` explicitly opens two separate `*sql.DB` instances on the same file to verify persistence. This is a valid test pattern that uses `OpenEmbedCache` which internally calls `sql.Open`.
**Why it happens:** The test verifies that embedding cache rows written by one process are visible to a second process (simulated by closing and reopening).
**How to avoid:** Keep the test pattern but change it to call `db.Open(dbPath)` + `NewEmbedCacheWithDB(db, ...)` for both instances. Each call to `db.Open` creates its own connection pool, which is the correct behavior for simulating separate processes.
**Warning signs:** This is the only test that intentionally opens two pools on the same file. Don't try to "optimize" it into sharing -- the separate pools ARE the test.

### Pitfall 6: Double-Close in Bot.Stop()
**What goes wrong:** `bot.Stop()` currently calls `schedDB.Close()` and `authDB.Close()`. After this phase, both stores use the shared pool. If their Close() methods are not removed, the shared pool gets closed twice.
**Why it happens:** Historical: each store opened its own connection and was responsible for closing it.
**How to avoid:** Remove both Close() calls from bot.Stop(). Only close the shared pool in main.go's deferred cleanup, after bot.Stop() returns. Remove Close() methods from all stores.
**Warning signs:** `internal/telegram/bot.go:190-192` (schedDB.Close) and `193-195` (authDB.Close) must be removed.

## Code Examples

Verified patterns from the current codebase:

### Settings Bootstrap (main.go:47-56 -- BEFORE change)
```go
// Source: D:\Aura\cmd\aura\main.go lines 47-56
// CURRENT pattern -- settings opens its own connection
settingsStore, err := settings.OpenStore(cfg.DBPath)
if err != nil {
    logger.Warn("settings store unavailable, using env only", "error", err)
} else {
    settings.ApplyToConfig(context.Background(), settingsStore, cfg)
    defer settingsStore.Close()
}
```

### Settings Bootstrap (main.go -- AFTER change)
```go
// AFTER: shared pool opened once, settings injected
pool, err := db.Open(cfg.DBPath)
if err != nil {
    logger.Error("failed to open database", "error", err)
    os.Exit(1)
}
defer pool.Close()

settingsStore, err := settings.NewStoreWithDB(pool)
if err != nil {
    logger.Warn("settings store unavailable, using env only", "error", err)
} else {
    settings.ApplyToConfig(context.Background(), settingsStore, cfg)
    // No defer settingsStore.Close() -- pool.Close handles it
}
```

### Store Constructor (auth -- BEFORE change)
```go
// Source: D:\Aura\internal\auth\store.go lines 74-99
// OpenStore is REMOVED
func OpenStore(path string) (*Store, error) {
    db, err := sql.Open("sqlite", path)
    // ... ping, migrate, return Store{owned: true}
}

// NewStoreWithDB is KEPT (with modifications)
func NewStoreWithDB(db *sql.DB) (*Store, error) {
    s := &Store{db: db, now: time.Now, owned: false}
    if err := s.migrate(); err != nil {
        return nil, err
    }
    return s, nil
}
```

### Store Constructor (auth -- AFTER change)
```go
// AFTER: only NewStoreWithDB remains; owned field and Close() removed
func NewStoreWithDB(db *sql.DB) (*Store, error) {
    s := &Store{db: db, now: time.Now}
    if err := s.migrate(); err != nil {
        return nil, err
    }
    return s, nil
}
// Close() method REMOVED entirely
// owned field REMOVED from Store struct
```

### Scheduler Consumer Chain (setup.go -- BEFORE change)
```go
// Source: D:\Aura\internal\telegram\setup.go lines 211-230
schedDBPath := cfg.DBPath
if schedDBPath == "" {
    schedDBPath = "./aura.db"
}
schedStore, err := scheduler.OpenStore(schedDBPath)  // OPENS OWN CONNECTION
// ...
summariesStore := summarizer.NewSummariesStore(schedStore.DB())  // SHARES via DB()
swarmStore, err := swarm.NewStoreWithDB(schedStore.DB())         // SHARES via DB()
```

### Scheduler Consumer Chain (setup.go -- AFTER change)
```go
// AFTER: pool arrives from New() parameter; schedDBPath variable removed
schedStore, err := scheduler.NewStoreWithDB(pool)       // INJECTED
// ...
summariesStore := summarizer.NewSummariesStore(pool)     // DIRECT from pool
swarmStore, err := swarm.NewStoreWithDB(pool)             // DIRECT from pool
// All consumers get pool directly -- DB() accessor REMOVED from scheduler.Store
```

### Test Migration Pattern (settings -- BEFORE)
```go
// Source: D:\Aura\internal\settings\store_test.go line 12
func TestSettings_SetAndGet(t *testing.T) {
    dir := t.TempDir()
    s, err := OpenStore(filepath.Join(dir, "settings.db"))
    // ...
    defer s.Close()
}
```

### Test Migration Pattern (settings -- AFTER)
```go
// AFTER: uses db.Open + NewStoreWithDB
func TestSettings_SetAndGet(t *testing.T) {
    db, err := db.Open(filepath.Join(t.TempDir(), "settings.db"))
    if err != nil {
        t.Fatalf("db.Open: %v", err)
    }
    defer db.Close()
    s, err := NewStoreWithDB(db)
    // ...
    // No s.Close() -- defer db.Close() handles it
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Per-store `sql.Open` + `owned` guard | Single `db.Open` + constructor injection | This phase | Eliminates 6 independent connection pools, inconsistent lifecycle management |
| Swarm `SetMaxOpenConns(1)` | No MaxOpenConns limit (WAL native) | This phase | Removes global throttle on shared pool |
| `scheduler.DB()` accessor chain | Direct pool injection to all consumers | This phase | Simplifies dependency graph; all consumers get same `*sql.DB` |
| Per-store `Close()` with mixed semantics | Single `db.Close()` in main.go | This phase | Single responsibility, no double-close risk |

**Deprecated/outdated (after this phase):**
- `OpenStore(path)` on all stores: removed; use `db.Open(path)` + `NewStoreWithDB(db)`
- `owned bool` field on auth/settings/swarm stores: removed; lifecycle owned by main.go
- `Close()` on all stores: removed; single pool close in main.go
- `SetMaxOpenConns(1)` in swarm: removed; conflicts with shared pool
- `_ "modernc.org/sqlite"` blank import in store packages: removed; only in `internal/db`
- `DB()` accessor on scheduler.Store: removed; pool injected directly to all consumers

## Runtime State Inventory

> This phase is NOT a rename/refactor/migration phase -- it does not change stored data, service config, OS registrations, secrets, or build artifacts. It refactors code-level connection management only. The same `aura.db` file is used with the same schema and data. No runtime state inventory is needed.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `page_size=4096` PRAGMA can fail silently on existing databases without side effects | Architecture Patterns | If the driver throws a hard error rather than a warnable one, the PRAGMA loop needs try/catch. MEDIUM risk -- testing needed. |
| A2 | All cmd/debug_* tools are out of scope for this phase and can be updated in a follow-up or left broken temporarily | Common Pitfalls | If debug tools must remain compilable throughout the phase, they need updating too. LOW risk -- they're development-only tools. |
| A3 | `search.EmbedCache` embedded function signature won't change when moving to constructor injection | Architecture Patterns | The cache itself needs no behavioral change; only its constructor changes. LOW risk. |

## Open Questions

1. **Should `scheduler.DB()` accessor be removed in this phase or deferred?**
   - What we know: 6 production consumers in `telegram/setup.go` use `schedStore.DB()`, plus several test helpers. If removed, all consumers must switch to receiving `*sql.DB` directly from the pool.
   - What's unclear: Whether removing it is in scope for Phase 1 or should wait for Phase 2 when migrations are restructured.
   - Recommendation: Remove it in Phase 1. The CONTEXT.md discretion section explicitly asks the agent to decide whether to keep or remove `DB()`. Removing it simplifies the architecture: no more "backdoor" access to the pool through a store wrapper. All consumers get the pool directly. This is a clean boundary.

2. **How to handle `scheduler.NewTestDB(t)` helper?**
   - What we know: It is used by `conversation/archive_test.go`, `conversation/summarizer/applier_test.go`, and several scheduler internal tests. It calls `sql.Open("sqlite", ...)` directly and applies migrations manually.
   - What's unclear: Whether it should switch to `db.Open` or remain as a scheduler-internal concern.
   - Recommendation: Switch `NewTestDB` to call `db.Open(dbPath)` instead of `sql.Open("sqlite", dbPath)`. Remove the `_ "modernc.org/sqlite"` import from `testdb.go`. This is one of the raw sql.Open calls that the phase must eliminate.

3. **Should `conversation/archive_internal_test.go` duplicate schema or import scheduler?**
   - What we know: Currently duplicates `testConversationsSchema` and uses raw `sql.Open` to avoid import cycles with scheduler.
   - What's unclear: Whether to refactor the schema sharing or keep the duplication.
   - Recommendation: Keep the duplication. Switch `sql.Open` to `db.Open`. This is an internal test helper, not production code. Phase 2 (versioned migrations) will eliminate the duplication naturally.

## Environment Availability

> This phase has no external dependencies beyond the Go toolchain and the existing `modernc.org/sqlite` driver already in `go.mod`. No environment audit needed.

## Validation Architecture

> Treating as enabled: `nyquist_validation` flag is absent from `.planning/config.json` (no file exists), so default behavior is to include validation.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` package |
| Config file | none -- standard `go test` |
| Quick run command | `go test ./internal/db/... -count=1` |
| Full suite command | `go test ./... -count=1` |

### Phase Requirements -- Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| FIX-02-WAL | WAL journal mode active after `db.Open` | unit | `go test ./internal/db/ -run TestOpen_WALMode -count=1` | No -- Wave 0 |
| FIX-02-PRAGMA | All PRAGMAs applied on open | unit | `go test ./internal/db/ -run TestOpen_Pragmas -count=1` | No -- Wave 0 |
| FIX-02-SINGLE | No `sql.Open` outside `internal/db` in non-test code | static | `rg 'sql\.Open\(' --glob '!*_test.go' --glob '!internal/db/*'` | N/A |
| FIX-02-INJECT | All stores constructable via `NewStoreWithDB` | unit | `go test ./internal/auth/... ./internal/settings/... ./internal/scheduler/... ./internal/swarm/... ./internal/search/... -count=1` | Partially -- existing store tests exist, need migration |
| FIX-02-CLOSE | Per-store Close() removed; lifecycle in main.go | integration | Manual verification: `rg 'func \(.*\) Close\(\) error' internal/{auth,scheduler,settings,swarm}/store.go` should return 0 matches | N/A |
| FIX-02-DRIVER | Only `internal/db` imports modernc.org/sqlite | static | `rg '_ "modernc.org/sqlite"' --glob '!internal/db/*'` should return 0 matches | N/A |

### Sampling Rate
- **Per task commit:** `go test ./internal/db/... -count=1`
- **Per wave merge:** `go test ./internal/{db,auth,scheduler,settings,swarm,search}/... -count=1`
- **Phase gate:** Full `go test ./... -count=1` green before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `internal/db/db_test.go` -- covers `TestOpen_WALMode`, `TestOpen_Pragmas`, `TestOpen_NonexistentPath`, `TestOpen_EmptyPath`
- [ ] `internal/db/db_test.go` -- covers `TestOpen_ExistingDB` (no migration, no data loss)
- [ ] Framework install: none needed -- standard `go test`

## Security Domain

> `security_enforcement` flag is absent from config (no `.planning/config.json` exists), so security domain is enabled per default behavior.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|------------------|
| V2 Authentication | no | Auth tokens are managed by `internal/auth` -- no change in this phase |
| V3 Session Management | no | Sessions are stateless bearer tokens -- no change |
| V4 Access Control | no | Allowlist enforcement unchanged |
| V5 Input Validation | yes | `db.Open(path)` must validate path is non-empty and file is accessible; the existing stores already validate inputs |
| V6 Cryptography | no | No new cryptographic operations; auth tokens use SHA-256 (existing, unchanged) |

### Known Threat Patterns for SQLite Connection Pool

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Path traversal in DB path | Tampering | `db.Open` should use `filepath.Clean` and reject paths with `..` segments [CITED: OWASP Path Traversal] |
| Shared DB handle double-close | Denial of Service | Single `defer db.Close()` in main.go; remove Close from all stores |
| WAL file accumulation without checkpointing | Denial of Service | WAL auto-checkpointing is default in SQLite; no manual intervention needed at this phase |
| Unencrypted DB file containing secrets | Information Disclosure | Settings store holds API keys in plaintext. This is the existing threat model (OS file permissions as boundary). Phase 3 adds AES-256-GCM encryption. Not in scope for Phase 1. |

## Sources

### Primary (HIGH confidence)
- Live codebase at `D:\Aura` -- all source files read and verified:
  - `internal/auth/store.go` -- auth store with `OpenStore`, `NewStoreWithDB`, `owned` guard, `Close()`
  - `internal/settings/store.go` -- settings store with `OpenStore`, `NewStoreWithDB`, `owned` guard, `Close()`
  - `internal/scheduler/store.go` -- scheduler store with `OpenStore` (no ownership guard), `NewStoreWithDB`, `DB()` accessor, unconditional `Close()`
  - `internal/swarm/store.go` -- swarm store with `OpenStore`, `NewStoreWithDB`, `SetMaxOpenConns(1)`, `owned` guard, `DB()` accessor
  - `internal/search/embed_cache.go` -- embed cache with `OpenEmbedCache`, standalone `Close()`, no `owned` concept
  - `internal/search/sqlite.go` -- FTS5 searcher with `newSqliteSearcher`, internal `sql.Open`, NO `Close()` method
  - `internal/search/search.go` -- Engine wiring, `NewEngineWithFallback` calls `newSqliteSearcher`
  - `cmd/aura/main.go` -- entrypoint, settings bootstrap, first-run wizard, bot creation, shutdown
  - `internal/telegram/setup.go` -- store wiring, `scheduler.OpenStore`, `auth.OpenStore`, `DB()` consumer chain
  - `internal/telegram/bot.go` -- Bot struct, `Stop()` method with `schedDB.Close()` + `authDB.Close()`
  - `internal/scheduler/testdb.go` -- test helper with raw `sql.Open`
  - All test files enumerated in the grep results above

### Secondary (MEDIUM confidence)
- `.planning/CONTEXT.md` Phase 1 decisions -- verified against live code (one discrepancy: 6 sql.Open calls, not 5)
- `.planning/codebase/CONCERNS.md` -- confirmed multiple connection pools and missing WAL mode as the problems this phase fixes

### Tertiary (LOW confidence)
- None. All findings verified against live code.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH -- no new dependencies; `database/sql` + `modernc.org/sqlite` are already in go.mod
- Architecture: HIGH -- all patterns verified against live code; constructor injection already exists on 5/6 stores
- Pitfalls: HIGH -- identified via direct code audit of all Close() methods, SetMaxOpenConns call, and shutdown sequence

**Research date:** 2026-05-04
**Valid until:** 2026-05-18 (30 days -- stable domain, no SDK changes expected)

---

*Phase: 01-centralize-sqlite-db*
*Research completed: 2026-05-04*
