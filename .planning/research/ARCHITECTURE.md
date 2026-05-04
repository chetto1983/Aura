# Architecture Research — v1.0 Hardening Integration

> **Superseded research note (2026-05-04):** This architecture research preserves the earlier broad hardening investigation. It is not the active v1.0 Production Readiness implementation plan. Use `.planning/REQUIREMENTS.md`, `.planning/ROADMAP.md`, `docs/superpowers/specs/2026-05-04-v1-production-readiness-design.md`, and `docs/superpowers/plans/2026-05-04-v1-production-readiness-plan.md` for current scope. Items below that are absent from those approved docs are v1.1+ or historical context.

**Domain:** Go monolith hardening (SQLite centralization, migrations, token expiry, secrets encryption, test coverage, release packaging)
**Researched:** 2026-05-04
**Confidence:** HIGH for historical observations; superseded for active v1.0 scope.

## Current Architecture Baseline

A single Go binary embeds a React 19 SPA. 30 internal packages. SQLite (`aura.db`) is accessed by 4 independent `*sql.DB` openers: `auth.OpenStore`, `settings.OpenStore`, `scheduler.OpenStore`, and `search.OpenEmbedCache`. The scheduler's `DB()` method serves as a de-facto shared connection for `swarm`, `conversation`, `summarizer`, and `issues` — but auth and settings both open their own connections to the same file. No migration framework exists: every store has an inline `CREATE TABLE IF NOT EXISTS` in its `migrate()` method, and the scheduler applies ad-hoc `ALTER TABLE ADD COLUMN` via `PRAGMA table_info` checks. No version tracking, no rollback.

Settings secrets (LLM_API_KEY, EMBEDDING_API_KEY, MISTRAL_API_KEY, OLLAMA_API_KEY) were stored in plain text in the research snapshot. Dashboard tokens were issued without expiry. Telegram test coverage, tray coverage, and file-generation tool layout were also surveyed as historical hardening candidates. Release packaging consisted of `go build -o aura.exe` with no runtime bundling. Current v1.0 blockers are only those in the approved planning docs.

Full architecture detail is at `.planning/codebase/ARCHITECTURE.md`.

## Hardening Item #1: Centralized SQLite (`internal/db` — NEW component)

### What It Touches

Every existing store that opens SQLite directly:
- `internal/auth/store.go:75` — `sql.Open("sqlite", path)`
- `internal/settings/store.go:57` — `sql.Open("sqlite", path)`
- `internal/scheduler/store.go:117` — `sql.Open("sqlite", path)`
- `internal/search/embed_cache.go:68` — `sql.Open("sqlite", dbPath)`
- `internal/search/sqlite.go:21` — `sql.Open("sqlite", dbPath)` (FTS5 fallback)

### New Component: `internal/db`

```
internal/db/
├── db.go          # Open(path) (*sql.DB, error) — single factory, WAL + busy_timeout
└── db_test.go     # open/pragma/memory-mode tests
```

- **`db.Open(path)`** applies `PRAGMA journal_mode=WAL`, `PRAGMA busy_timeout=5000`, `PRAGMA foreign_keys=ON`, and returns a single `*sql.DB`.
- **Ownership:** The caller (`cmd/aura/main.go`) opens once via `db.Open(cfg.DBPath)` and passes the `*sql.DB` to every store constructor. No more `owned bool` tracking per store — the DB lifecycle is `main.go`'s responsibility.
- **Backward compatibility:** `db.Open` mirrors the existing `sql.Open("sqlite", path)` + `Ping()` pattern that all stores use; the only new behavior is the PRAGMAs. Existing WAL files survive without conflict.

### Modified Components

| File | Change |
|------|--------|
| `cmd/aura/main.go` | Add `db.Open(cfg.DBPath)` before `settings.OpenStore`; pass `*sql.DB` downstream |
| `internal/telegram/setup.go` | Accept `*sql.DB` in `New()`; pass to `auth.NewStoreWithDB`, `settings.NewStoreWithDB`, `scheduler.NewStoreWithDB`, `search.OpenEmbedCacheWithDB`, `search.NewFallbackStoreWithDB` |
| `internal/auth/store.go` | **Remove** `OpenStore(path)` — keep only `NewStoreWithDB(db)`. Remove `owned`/`Close()` logic. `migrate()` stays (called once, idempotent). |
| `internal/settings/store.go` | Same as auth: remove `OpenStore(path)`, keep `NewStoreWithDB(db)`. Remove `owned`/`Close()`. |
| `internal/scheduler/store.go` | Same: remove `OpenStore(path)`, keep `NewStoreWithDB(db)`. Remove `Close()` from Store struct. |
| `internal/search/embed_cache.go` | Add `OpenEmbedCacheWithDB(db)` constructor. Remove `sql.Open` from existing. |
| `internal/search/sqlite.go` | Same — add `NewFallbackStoreWithDB(db)`, remove `sql.Open`. |

### Integration Points

```
cmd/aura/main.go startup sequence (before/after):
  BEFORE: settings.OpenStore(cfg.DBPath) → settings.ApplyToConfig() → ...
  AFTER:  db.Open(cfg.DBPath) → settings.NewStoreWithDB(sharedDB) → settings.ApplyToConfig() → ...
```

**Key point:** `db.Open` runs **before** `settings.OpenStore` in main.go:60. The first-run wizard path (main.go:62-90) also needs the shared DB available before the wizard launches. Settings store changes from `OpenStore(path)` → `NewStoreWithDB(sharedDB)` — same semantics, shared connection.

In `telegram/setup.go`, the existing pattern of `scheduler.OpenStore(schedDBPath)` (line 215) and `auth.OpenStore(schedDBPath)` (line 323) changes to `scheduler.NewStoreWithDB(sharedDB)` and `auth.NewStoreWithDB(sharedDB)` — both are already supported via the `NewStoreWithDB` constructors that exist today. No new interfaces needed.

## Hardening Item #2: Versioned Migration Framework (`internal/db/migrations/` — NEW component)

### What It Touches

The scattered `migrate()` methods in:
- `internal/scheduler/store.go:156-185` — 5 CREATE TABLE statements + 4 ALTER TABLE column additions
- `internal/auth/store.go:109-113` — 3 CREATE TABLE statements
- `internal/settings/store.go:92-96` — 1 CREATE TABLE statement
- `internal/search/embed_cache.go` — 1 CREATE TABLE

### New Component: `internal/db/migrations`

```
internal/db/migrations/
├── runner.go     # Run(db) error — reads migrations table, applies pending
├── migration.go  # Migration struct {Version int, Name string, Up string}
├── registry.go   # all() []Migration — ordered slice of every migration
├── 001_auth.go   # Version 1: api_tokens + allowed_users + pending_users
├── 002_scheduler.go # Version 2: scheduled_tasks + index
├── 003_conv.go   # Version 3: conversations table
├── 004_proposals.go # Version 4: proposed_updates + wiki_issues
├── 005_settings.go  # Version 5: settings table
├── 006_embed.go  # Version 6: embedding cache table
├── 007_search.go # Version 7: FTS5 fallback tables
├── 008_token_expiry.go # Version 8: expires_at column on api_tokens (see #3)
└── runner_test.go
```

### Modified Components

| File | Change |
|------|--------|
| `internal/auth/store.go` | **Remove** `schemaSQL` const and `migrate()` method entirely. Schema is now owned by `db/migrations/001_auth.go`. |
| `internal/settings/store.go` | **Remove** `schemaSQL` and `migrate()`. Schema moves to `db/migrations/005_settings.go`. |
| `internal/scheduler/store.go` | **Remove** `schemaSQL`, `conversationsSchemaSQL`, `wikiIssuesSchemaSQL`, `proposedUpdatesSchemaSQL`, and all `add*Column` helpers. Move to `db/migrations/002–004`. |
| `internal/search/embed_cache.go` | **Remove** inline schema. Move to `db/migrations/006_embed.go`. |
| `internal/search/sqlite.go` | **Remove** inline schema. Move to `db/migrations/007_search.go`. |

The migration runner writes a `migrations` table:
```sql
CREATE TABLE IF NOT EXISTS _migrations (
    version   INTEGER PRIMARY KEY,
    name      TEXT NOT NULL,
    applied_at TEXT NOT NULL
);
```
On startup, `migrations.Run(db)` reads existing applied versions, skips them, and applies pending migrations in order within a single `BEGIN IMMEDIATE` transaction.

### Integration Points

```
cmd/aura/main.go startup (after db.Open):
  db.Open(cfg.DBPath) → migrations.Run(sharedDB) → settings.NewStoreWithDB(sharedDB) → ...
```

**Migration numbering strategy:** Start with version 1, consolidating all existing `CREATE TABLE IF NOT EXISTS` statements into numbered files. Existing databases have their tables already — migration versions 1–7 are no-ops (table already exists) because the migration framework uses `IF NOT EXISTS` guards. Future migrations (8+) add new columns/tables.

**Critical: backward compatibility.** Users who upgrade from the current codebase have a healthy `aura.db` with all tables. When the new migration runner executes versions 1–7, they're all idempotent no-ops. The `_migrations` table doesn't exist yet — `migrations.Run` creates it, inserts rows for versions 1–7, and the next startup skips them.

## Hardening Item #3: Dashboard Token Expiry (`internal/auth` — MODIFIED component)

### What It Touches

- `internal/auth/store.go` — `Issue()`, `Lookup()`, schema
- `internal/tools/auth.go` — `request_dashboard_token` tool (no schema work, just passes through)

### New Behavior

| Method | Change |
|--------|--------|
| `Issue()` | Adds `expires_at` column (DEFAULT `datetime('now', '+30 days')`). Returns token + expiry time in result so the LLM can tell the user. |
| `Lookup()` | Before the `revoked_at` check, verifies `expires_at > datetime('now')`. Expired tokens return `ErrInvalid` — indistinguishable from revoked to a client. |
| `Revoke()` | No change. |
| **NEW:** `PurgeExpired(ctx)` | Background method called by a goroutine. `DELETE FROM api_tokens WHERE expires_at <= datetime('now')`. Runs every 6 hours. |

### Modified Components

| File | Change |
|------|--------|
| `internal/auth/store.go` | `expires_at` column in Issue insert; expiry check in Lookup; `PurgeExpired()` method |
| `db/migrations/008_token_expiry.go` | `ALTER TABLE api_tokens ADD COLUMN expires_at TEXT NOT NULL DEFAULT ''` + backfill for existing rows: `UPDATE api_tokens SET expires_at = datetime(issued_at, '+30 days') WHERE expires_at = ''` |
| `internal/telegram/setup.go` | Launch `go authStore.PurgeExpiredLoop(ctx)` goroutine after auth store is wired |
| `internal/tools/auth.go` | Return expiry info in response message (no schema change) |

### Integration Points

- **Migration 008** (in `db/migrations/`) adds `expires_at` column atomically after the migration framework lands (build order dependency: #1 + #2 before #3).
- **PurgeExpired goroutine** starts in `telegram/setup.go` after `auth.NewStoreWithDB()` completes — same place where the auth store is wired today.
- **Graceful shutdown:** `Bot.Stop()` already closes `authDB`; the purge goroutine exits when `ctx` is cancelled.

## Hardening Item #4: Secrets Encryption in Settings Store (`internal/settings` — MODIFIED component)

### What It Touches

- `internal/settings/store.go` — `Get()`, `Set()`, `All()`
- `internal/config/config.go` — `LLMAPIKey`, `EmbeddingAPIKey`, `MistralAPIKey`, `OllamaAPIKey` fields
- `internal/settings/applier.go` — reads settings, writes to `*config.Config`

### New Component: `internal/settings/encrypt.go`

```
internal/settings/
├── store.go       # (modified) — Get/Set transparently encrypt/decrypt secret keys
├── encrypt.go     # encrypt/decrypt helpers, key derivation, env-key marker
└── encrypt_test.go
```

### Design

- **Secret key set:** `LLM_API_KEY`, `EMBEDDING_API_KEY`, `MISTRAL_API_KEY`, `OLLAMA_API_KEY`, and any future key ending in `_API_KEY` or `_SECRET`.
- **Encryption key derivation:** Aura generates a 32-byte random key on first boot, writes it to `ENCRYPTION_KEY` in `.env`, and never logs it. If `ENCRYPTION_KEY` is absent, encryption is a no-op (plaintext store — same as today). This avoids a chicken-and-egg problem where the encrypted value can't be read without a key that hasn't been loaded yet.
- **Storage format:** Encrypted values are base64-encoded and prefixed with `enc:v1:` so plaintext rows remain readable and the store can distinguish encrypted vs legacy values.
- **Transparent at API boundary:** `Get(ctx, "LLM_API_KEY")` detects `enc:v1:` prefix, decrypts, returns plaintext. `Set(ctx, "LLM_API_KEY", value)` detects key is in secret set, encrypts, stores with `enc:v1:` prefix. `All()` returns decrypted values. Dashboard forms see plaintext — no UI changes needed.

### Modified Components

| File | Change |
|------|--------|
| `internal/settings/store.go` | `Set()` auto-encrypts secret keys; `Get()` auto-decrypts `enc:v1:` values; `All()` decrypts before returning |
| `internal/settings/encrypt.go` | NEW: `isSecretKey(key) bool`, `encrypt(value, key) string`, `decrypt(value, key) string` |
| `config.Load()` / `.env.example` | Add `ENCRYPTION_KEY` env var (auto-generated, optional) |
| `cmd/aura/main.go` | Generate `ENCRYPTION_KEY` on first boot if absent, write to `.env`, reload config |

### Integration Points

- **Encryption key generation** happens at the same point as the first-run wizard in main.go: if `ENCRYPTION_KEY` is blank after loading `.env`, generate it, write to `.env`, and reload.
- **Settings migration:** Existing plaintext rows are re-encrypted on first read-then-write. No bulk migration needed — each key is encrypted when the user next saves the settings form. Plaintext rows still work because the `enc:` prefix check fails gracefully (value returned as-is).
- **Backward compatibility:** If `ENCRYPTION_KEY` is unset (legacy installs), encryption is a no-op. Aura runs identically to today.

## Historical Hardening Item #5: Test Coverage + File Split + Tray Tests

### 5a: Telegram Test Coverage Research — Historical Only

| Existing test file | Current tests | Target additions |
|---|---|---|
| `bot_test.go` | 9 tests (bot structure, allowlist, tools availability) | Add `TestNewBotFailsGracefullyOnMissingConfig`, `TestSendToUserInvalidID`, `TestStopDrainsArchiver`, `TestStartLaunchesScheduler` |
| `documents_test.go` | 9 tests (PDF validation, naming, formatting) | Add `TestDocHandlerRejectsNonAllowlisted`, `TestDocHandlerProgressEdit`, `TestDocHandlerOCRTrigger` |
| `markdown_test.go` | 2 tests (render, double-escape) | Add table rendering, code blocks, nested bold/italic, link stripping, HTML entity pass-through |
| `setup_sandbox_test.go` | 5 tests (runtime config) | Already solid — leave as-is |
| `sandbox_integration_test.go` | 3 tests | Already solid |
| `scheduler_handlers_test.go` | 6 tests (agent job dispatch) | Already solid |
| **NEW:** `conversation_test.go` | — | `TestHandleConversationBudgetExhausted`, `TestToolCallLoopHitsMaxIterations`, `TestStreamingEditBatching` |
| **NEW:** `access_test.go` | — | `TestStartHandler_NotAllowlisted_Rejected`, `TestLoginHandler_ExistingUser`, `TestLoginHandler_NewPending` |
| **NEW:** `setup_test.go` | — | `TestCreateLLMClient_OpenAIOnly`, `TestCreateLLMClient_Failover`, `TestNewBot_NilSettingsStore` |

Integration: No new components. Pure test additions following existing patterns (table-driven tests, `TestXxx` naming). Use existing mock stubs where available.

### 5b: File-Generation Tool Split Research — Historical Only

```
internal/tools/
├── files_xlsx.go  # CreateXLSXTool, parseCreateXLSXArgs, (extracted from files.go:29-200ish)
├── files_docx.go  # CreateDOCXTool, parseCreateDOCXArgs
├── files_pdf.go   # CreatePDFTool, parseCreatePDFArgs
├── files.go       # Keep: DocumentSender interface, common helpers (sanitizeFilename, etc.)
└── files_test.go  # Split into files_xlsx_test.go / files_docx_test.go / files_pdf_test.go
```

Integration: Pure file-split refactor. No API changes. `telegram/setup.go` imports already use `tools.NewCreateXLSXTool` which stays in the same package. No callers change.

### 5c: Tray Tests — NEW test file + modified `tray.go`

```
internal/tray/
├── tray.go            # (MODIFIED) — add openBrowser(URL) abstraction
├── tray_windows.go    # (modified) — use openBrowser
├── tray_other.go      # (modified) — use openBrowser
├── tray_test.go       # NEW — TestOptionsValidation, TestStopSafeFromAnyGoroutine
└── browser_test.go    # NEW — TestOpenBrowserMock (cross-platform abstraction test)
```

Integration: `openBrowser` is extracted from `tray_windows.go`'s current inline browser-launch code. `tray.go` exports `var OpenBrowser = defaultOpenBrowser` for test substitution. `tray_test.go` verifies `Stop()` doesn't panic when called uninitialized (Windows edge case).

## Hardening Item #6: Release Packaging — NEW Makefile target only

### What It Touches

- `Makefile` — add `release` target
- `runtime/pyodide/` — existing folder, already committed

### New Files

```
scripts/
└── package.ps1       # Release packaging script (Windows)
```

### Makefile Target

```makefile
.PHONY: release
release: web-build build
  @echo "Packaging release for $(TAG)..."
  powershell -File scripts/package.ps1 -Tag "$(TAG)"
```

### Package Contents

```
aura_v1.0.0_windows_amd64.zip
├── aura.exe           # Built binary (with embedded SPA + resource.syso)
├── runtime/
│   └── pyodide/       # Bundled Pyodide 0.29.3 runtime
├── .env.example       # Template env file
└── README.txt         # Quick-start instructions
```

Integration: No code changes. `package.ps1` copies the build artifact + runtime directory into a staging folder, creates the zip with `Compress-Archive`, and `Test-Archive` verifies file count. Smoke test runs `aura.exe --help` before packaging (exits 0 if Pyodide bundle is reachable).

## Recommended Project Structure (Post-Hardening)

```
internal/
├── db/                      # NEW — centralized SQLite (hardening #1)
│   ├── db.go
│   ├── db_test.go
│   └── migrations/          # NEW — versioned migrations (hardening #2)
│       ├── runner.go
│       ├── migration.go
│       ├── registry.go
│       ├── 001_auth.go
│       ├── 002_scheduler.go
│       ├── 003_conv.go
│       ├── 004_proposals.go
│       ├── 005_settings.go
│       ├── 006_embed.go
│       ├── 007_search.go
│       ├── 008_token_expiry.go
│       └── runner_test.go
├── auth/                    # MODIFIED — token expiry (hardening #3)
│   ├── store.go             # removes OpenStore, migrate; adds expires_at, PurgeExpired
│   └── store_test.go        # adds expiry tests
├── settings/                # MODIFIED — secrets encryption (hardening #4)
│   ├── store.go             # removes OpenStore, migrate; adds enc: handling
│   ├── encrypt.go           # NEW — encrypt/decrypt helpers
│   ├── applier.go           # unchanged
│   └── *test.go
├── scheduler/
│   ├── store.go             # MODIFIED — removes OpenStore, migrate, schema consts
│   └── ...
├── search/
│   ├── embed_cache.go       # MODIFIED — adds NewStoreWithDB constructor
│   ├── sqlite.go            # MODIFIED — adds NewStoreWithDB constructor
│   └── ...
├── telegram/
│   ├── setup.go             # MODIFIED — accepts *sql.DB; launches purge goroutine
│   ├── bot.go               # unchanged
│   ├── *_test.go            # +10 new test files/functions (hardening #5a)
│   └── ...
├── tools/
│   ├── files_xlsx.go        # NEW — extracted (hardening #5b)
│   ├── files_docx.go        # NEW — extracted
│   ├── files_pdf.go         # NEW — extracted
│   ├── files.go             # MODIFIED — stripped to interface + helpers
│   └── files_*_test.go      # NEW — split tests
└── tray/
    ├── tray.go              # MODIFIED — openBrowser abstraction (hardening #5c)
    ├── tray_test.go         # NEW
    ├── browser_test.go      # NEW
    └── ...
scripts/
└── package.ps1              # NEW — release packaging (hardening #6)
```

## Component Responsibilities Table

| Component | Current | Post-Hardening | New or Modified |
|-----------|---------|----------------|-----------------|
| `cmd/aura/main.go` | Bootstraps config, launches wizard, wires subsystems | Opens `db.Open()`, generates encryption key, runs migrations before store init | Modified |
| `internal/db/` | — | Centralized `*sql.DB` factory with WAL + busy_timeout pragmas | **NEW** |
| `internal/db/migrations/` | — | Versioned, ordered, idempotent migration runner with `_migrations` tracking table | **NEW** |
| `internal/auth/store.go` | Opens own `*sql.DB`, inline `CREATE TABLE`, no expiry | Shares `*sql.DB` via `NewStoreWithDB`, schema from migrations, `expires_at` column, `PurgeExpired()` goroutine | Modified |
| `internal/settings/store.go` | Opens own `*sql.DB`, inline `CREATE TABLE`, plaintext secrets | Shares `*sql.DB`, schema from migrations, transparent enc:v1 encrypt/decrypt for `_API_KEY` fields | Modified |
| `internal/settings/encrypt.go` | — | AES-256-GCM encrypt/decrypt, secret key detection, enc:v1: prefix protocol | **NEW** |
| `internal/scheduler/store.go` | Opens own `*sql.DB`, 5 CREATE TABLEs + 4 ALTER TABLE helpers in migrate() | Shares `*sql.DB`, schema entirely from migrations, removes ~80 lines of migration code | Modified |
| `internal/search/embed_cache.go` | Opens own `*sql.DB`, inline schema | `OpenEmbedCacheWithDB(db)` constructor | Modified |
| `internal/search/sqlite.go` | Opens own `*sql.DB`, inline schema | `NewFallbackStoreWithDB(db)` constructor | Modified |
| `internal/telegram/setup.go` | Calls `scheduler.OpenStore(path)`, `auth.OpenStore(path)` | Receives shared `*sql.DB`, uses `NewStoreWithDB` constructors, launches `PurgeExpired` goroutine | Modified |
| `internal/telegram/*_test.go` | 36 tests across 6 files, 23.6% coverage | Historical research target: broader test suite | Historical research |
| `internal/tools/files{,_xlsx,_docx,_pdf}` | Historical mixed file-generation implementation | Historical research target: interface plus split implementations | Historical research |
| `internal/tray/tray.go` | Inline browser-open in platform files | `openBrowser` abstraction, test seam | Modified |
| `internal/tray/tray_test.go` + `browser_test.go` | — | Stop safety, options validation, cross-platform browser abstraction | **NEW** |
| `scripts/package.ps1` | — | Release archive builder with Pyodide bundle + smoke check | **NEW** |
| `Makefile` | `build`, `test`, `web-build` | Adds `release` target | Modified |

## Build Order & Dependencies

```
Phase 1 (unlocks everything):
  ┌─ db.Open() central factory ─┐
  └─ migrations.Run() runner    ┘

Phase 2 (can parallel):
  ┌─ Token expiry (auth + migration 008) ─┐
  │                                         │
  └─ Secrets encryption (settings/encrypt) ┘

Phase 3 (can parallel):
  ┌─ Telegram test coverage ──┐
  ├─ File split (tools/files) │
  └─ Tray tests + browser     ┘

Phase 4 (last):
  └─ Release packaging (needs build artifact from all of above)
```

**Dependency graph:**
- Items #3 and #4 both need items #1 + #2 (migrations framework and shared DB must exist before adding `expires_at` column or encrypting stored values).
- Items #5a, #5b, #5c are independent of everything else — pure test/refactor additions.
- Item #6 needs everything else to be stable (it packages the built binary, which must include all hardening changes).

## Key Architectural Patterns Preserved

All existing patterns remain intact:

1. **No global state** — `*sql.DB` is passed via constructor injection like every other dependency.
2. **Graceful shutdown** — `Bot.Stop()` already tears down stores; new goroutines (PurgeExpired) respect context cancellation.
3. **Backward compatibility** — `NewStoreWithDB(db)` constructors already exist for auth, settings, scheduler, swarm. No schema changes that require user migration scripts.
4. **No new dependencies** — `crypto/aes`, `crypto/cipher`, `crypto/rand` are all stdlib. No CGO. No new Go modules.
5. **Settings overlay** — `settings.ApplyToConfig` continues working identically; the encrypt/decrypt layer is transparent at the `Get()/Set()` boundary.

## Anti-Patterns Avoided

| Anti-Pattern | Why It's Wrong | What We Do Instead |
|-------------|----------------|---------------------|
| Each store managing its own DB lifecycle | Multiple `*sql.DB` on same file with mismatched PRAGMAs causes `SQLITE_BUSY` and lost WAL benefits | Single `db.Open()` factory with shared connection pool; `main.go` owns close lifecycle |
| Ad-hoc column additions without version tracking | Impossible to know migration state across deployments; ordering bugs when two stores try to ALTER same table | Versioned migrations in numbered files, `_migrations` tracking table, single linear sequence |
| Plaintext API keys in SQLite | DB file leak = all credentials leaked | AES-256-GCM with `enc:v1:` prefix; transparent at API boundary |
| No token expiry | Compromised tokens grant permanent access | `expires_at` with 30-day default TTL; background purge goroutine |
| Monolithic tool file | 650-line single file with 3 unrelated tools | Each tool in its own file; shared interface in files.go |

## Sources

- `.planning/codebase/ARCHITECTURE.md` — current system architecture (2026-05-04)
- `cmd/aura/main.go` — startup sequence and component wiring
- `internal/telegram/setup.go` — subsystem initialization (720 lines)
- `internal/auth/store.go` — current token store (423 lines, no expiry)
- `internal/settings/store.go` — current settings store (223 lines, plaintext secrets)
- `internal/scheduler/store.go` — current migration pattern (ad-hoc ALTER TABLE)
- `internal/telegram/*_test.go` — 6 test files, 36 test functions, 23.6% coverage
- `internal/tools/files` — historical file-generation tool research context
- `internal/tray/` — zero tests, inline browser-open logic

---

*Architecture research for: Aura v1.0 hardening milestone*
*Researched: 2026-05-04*
