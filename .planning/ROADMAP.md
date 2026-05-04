# Roadmap: Aura v1.0 — Close Concern

**Created:** 2026-05-04
**Milestone:** v1.0 Close Concern
**Total phases:** 6

## Dependency Graph

```
Phase 1 (FIX-02: DB centralization)
  └→ Phase 2 (REFACTOR-02 + REFACTOR-01: migrations framework + scheduler extraction)
       └→ Phase 3 (FIX-03 + FIX-04: token expiry + secrets encryption)
            ├─ (parallel)
            └→ Phase 4 (FIX-01 + REFACTOR-03 + TEST-02: panic fix + file split + tray coverage)
                 ├─ (parallel)
                 └→ Phase 5 (TEST-01: telegram test coverage)
                      └→ Phase 6 (POLISH-01: telebot docs + final verification)
```

Critical path: Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5 → Phase 6

Phase 3 and Phase 4 can overlap once migrations land (Phase 2 complete). Phase 5 is independent of all DB work but gates final verification.

---

## Phases

### Phase 1: Centralize SQLite DB

**Addresses:** FIX-02
**Depends on:** —
**Rationale:** Critical path blocker. All migration, encryption, token expiry, and PRAGMA work requires a single `*sql.DB`. 6 of 10 hardening items depend on this completing.

**Deliverables:**
- `internal/db/` package with single `Open(path string) (*sql.DB, error)` factory
- WAL journal mode enabled at open (`PRAGMA journal_mode=WAL`)
- Busy timeout 5000ms (`PRAGMA busy_timeout=5000`)
- Foreign key enforcement (`PRAGMA foreign_keys=ON`)
- All 5 stores switched to `NewStoreWithDB(sharedDB)` constructor injection
- Per-store `Close()` methods removed; lifecycle owned by `internal/db`
- `cmd/aura/main.go` owns the single `*sql.DB` lifecycle

**Success criteria:**
1. A single `sql.Open("sqlite", path)` call exists in the entire codebase
2. WAL mode is active on startup (verified via `PRAGMA journal_mode` returning `wal`)
3. All existing store tests pass after refactoring to constructor injection
4. `aura.db` has `PRAGMA busy_timeout=5000` and `PRAGMA foreign_keys=ON` applied on open

---

### Phase 2: Versioned Migrations + Scheduler Extraction

**Addresses:** REFACTOR-02, REFACTOR-01
**Depends on:** Phase 1
**Rationale:** Must follow Phase 1 because it needs the shared `*sql.DB`. The migration framework gates all schema changes (token expiry, encryption). Scheduler extraction reduces `store.go` from 754 to ~400 lines.

**Deliverables:**
- `internal/db/migrations/` package with versioned migration runner
- `_migrations` tracking table (`version INTEGER, name TEXT, applied_at TEXT`)
- Ordered `[]Migration` slices applied in `BEGIN IMMEDIATE` transaction
- All per-store ad-hoc `CREATE TABLE` / `ALTER TABLE` consolidated into numbered migration files
- `internal/scheduler/migrations.go` extracted from `store.go` (schema definitions only)
- Idempotent `IF NOT EXISTS` for upgrade path from unmigrated databases

**Success criteria:**
1. Every `CREATE TABLE` and `ALTER TABLE` in the codebase lives in a numbered migration file
2. `_migrations` table tracks applied versions; re-running migrations on a clean DB is a no-op
3. `internal/scheduler/store.go` is ≤ 450 lines (down from 754)
4. Migrations applied atomically in a single `BEGIN IMMEDIATE` transaction; partial failure rolls back entirely
5. Upgrading from a pre-GSD `aura.db` (unmigrated) succeeds on first boot

---

### Phase 3: Token Expiry + Secrets Encryption

**Addresses:** FIX-03, FIX-04
**Depends on:** Phase 2
**Rationale:** Both security items depend on the migration framework (Phase 2) for schema changes. Independent of each other — both can be worked on in parallel within this phase after Phase 2 ships.

**Deliverables:**
- Token expiry:
  - `expires_at` column on `api_tokens` (nullable, added via migration)
  - Expiry enforcement in `auth.Lookup()` — expired tokens return distinct `ErrExpired`
  - `PurgeExpired()` background goroutine with configurable interval
  - `DASHBOARD_TOKEN_TTL_HOURS` env var (default 30 days)
  - 7-day grace period for already-issued tokens (nullable column → not retroactive)
- Secrets encryption:
  - `internal/settings/encrypt.go` with AES-256-GCM encrypt/decrypt
  - Auto-generated `SETTINGS_ENCRYPTION_KEY` (32 random bytes, base64) stored in `.env` on first boot
  - Transparent `enc:v1:` prefix protocol at `Get()`/`Set()` boundary
  - `_API_KEY` fields encrypted at rest; non-secret settings unchanged

**Success criteria:**
1. Issued dashboard tokens older than TTL days are rejected with `status: expired` in API responses
2. `SELECT value FROM settings WHERE key = 'LLM_API_KEY'` returns ciphertext (base64, not readable)
3. `Get("LLM_API_KEY")` returns plaintext at runtime for LLM client use
4. Missing `SETTINGS_ENCRYPTION_KEY` on first boot auto-generates and persists to `.env`
5. `PurgeExpired()` removes tokens past expiration from `api_tokens` at interval

---

### Phase 4: Bare Panic Fix + File Tool Split + Tray Coverage

**Addresses:** FIX-01, REFACTOR-03, TEST-02
**Depends on:** Phase 2 (migrations framework available), can run parallel with Phase 3
**Rationale:** Three independent hardening items with no mutual dependencies. All are pure refactor/test additions with no behavioral changes. Can run in parallel once base infrastructure (Phase 1–2) exists.

**Deliverables:**
- Bare panic fix:
  - `MustResolveProfiles` → `ResolveProfiles` returning `([]Profile, error)`
  - All callers updated to handle error return (log + graceful degrade, not panic)
- File tool split:
  - `tools/files.go` (599 lines) split into:
    - `tools/files_xlsx.go` — XLSX generation
    - `tools/files_docx.go` — DOCX generation
    - `tools/files_pdf.go` — PDF generation
    - `tools/files_types.go` — shared types and constants
  - No behavioral change, no new tests needed beyond existing suite passing
- Tray coverage + cross-platform browser:
  - `internal/tray/tray_windows_test.go` — startup, browser-open validation, error paths
  - `internal/tray/tray_other_test.go` — channel-block with logged warning
  - Cross-platform `openBrowser(url string) error` abstraction:
    - Windows: `rundll32 url.dll,FileProtocolHandler` with `http://`/`https://` validation
    - Linux: `xdg-open`
    - macOS: `open`
  - Non-Windows tray logs warning instead of silently blocking

**Success criteria:**
1. Passing an invalid toolset profile name returns an error; bot does not crash
2. `internal/tools/files.go` no longer exists; 4 focused files pass all existing tests
3. `internal/tray` package coverage ≥ 50%
4. `openBrowser("file://etc/passwd")` returns an error (URL scheme validation)
5. Non-Windows `tray_other.go` logs a warning-level message on startup

---

### Phase 5: Telegram Test Coverage

**Addresses:** TEST-01
**Depends on:** Phase 4 (hardened codebase available for testing), can run parallel with Phase 3–4
**Rationale:** Largest single effort in the milestone. Tests validate the hardened codebase. Uses hermetic fixtures — no real network I/O. Independent of all DB work.

**Deliverables:**
- ~24 new test functions across 4 new test files:
  - `internal/telegram/conversation_test.go` — `handleConversation` with stub LLM, fake telebot context
  - `internal/telegram/streaming_test.go` — streaming edit loop, tool-call fragment accumulation
  - `internal/telegram/documents_test.go` — document handling + OCR pipeline trigger paths
  - `internal/telegram/access_test.go` — auth middleware, approved user checks, /start approval flow
- Hermetic test fixtures: temp SQLite files, canned LLM responses, fake `*telebot.Context`
- Package coverage: 22.1% → ≥55%

**Success criteria:**
1. `go test -cover ./internal/telegram/` reports ≥55% coverage
2. `handleConversation` exercised with at least 3 distinct conversation paths (text, document, tool-call loop)
3. Streaming edit loop tested with progressive message edits (placeholder → partial → final)
4. Auth middleware tested: approved user, pending user, unknown user scenarios
5. No test creates a real network connection or requires `TELEGRAM_TOKEN`

---

### Phase 6: Telebot Documentation + Final Verification

**Addresses:** POLISH-01
**Depends on:** Phase 5
**Rationale:** Final cleanup phase. Documents the telebot beta risk and verifies all hardening items are stable before declaring the milestone complete.

**Deliverables:**
- `docs/telebot-beta.md` documenting:
  - Current pinned commit hash
  - Known gaps vs stable v3 API
  - Monitoring plan (watch releases, subscribe to changelog)
  - Migration path to stable v4 when available
- Full test suite pass verification
- Go build verification (`go build ./...` and `go vet ./...`)
- ROADMAP.md updated with final status: all phases complete

**Success criteria:**
1. `docs/telebot-beta.md` exists with pinned commit hash, risk assessment, and monitoring plan
2. `go test ./...` passes cleanly with no skipped tests
3. `go build ./...` and `go vet ./...` pass with zero warnings
4. All 10 requirements marked complete in REQUIREMENTS.md traceability table

---

## Phase Ordering Summary

| Phase | Requirements | Depends On | Parallel With | Target Deliverable |
|-------|-------------|------------|---------------|--------------------|
| 1 | FIX-02 | — | — | `internal/db` package, single connection pool |
| 2 | REFACTOR-02, REFACTOR-01 | 1 | — | Migration framework + scheduler extraction |
| 3 | FIX-03, FIX-04 | 2 | 4 | Token expiry + secrets encryption |
| 4 | FIX-01, REFACTOR-03, TEST-02 | 2 | 3 | Panic fix + file split + tray coverage |
| 5 | TEST-01 | 4 | 3 | Telegram test coverage ≥55% |
| 6 | POLISH-01 | 5 | — | Telebot docs + final verification |

## Requirement Coverage

| Requirement | Phase | Category |
|-------------|-------|----------|
| FIX-01 | Phase 4 | Fix & Secure |
| FIX-02 | Phase 1 | Fix & Secure |
| FIX-03 | Phase 3 | Fix & Secure |
| FIX-04 | Phase 3 | Fix & Secure |
| TEST-01 | Phase 5 | Test Coverage |
| TEST-02 | Phase 4 | Test Coverage |
| REFACTOR-01 | Phase 2 | Refactor |
| REFACTOR-02 | Phase 2 | Refactor |
| REFACTOR-03 | Phase 4 | Refactor |
| POLISH-01 | Phase 6 | Polish |

**Coverage:** 10/10 requirements mapped to phases. 0 unmapped. ✓

---

*Roadmap defined: 2026-05-04*
*Last updated: 2026-05-04*
