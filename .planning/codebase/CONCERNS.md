# Codebase Concerns

**Analysis Date:** 2026-05-04

## Missing Production Blockers Found During v1.0 Design

**Conversation archive failures can be silent:**
- Issue: archive append failures in the Telegram conversation path are not surfaced strongly enough for a memory-first product.
- Impact: Aura can appear to answer correctly while losing durable conversation evidence.
- Fix approach: make archive append failures observable through logging and focused tests.

**Settings API can expose secrets:**
- Issue: dashboard/settings responses can return raw API key values to any holder of a valid dashboard token.
- Impact: a stolen dashboard token can exfiltrate LLM, OCR, or embedding credentials.
- Fix approach: redact secret settings in API responses and UI state while keeping write/test-connection paths working.

## Error Handling Gaps

**Ignored errors in production paths:**
- Issue: `exec.Command().Start()` error silently discarded when opening browser from tray.
- Files: `internal/tray/tray_windows.go` (line 50)
- Impact: Browser-open failures are invisible to the user; no fallback or log warning.
- Fix approach: Log the error at `warn` level so the operator can diagnose when dashboard doesn't open.

**Shutdown close errors discarded:**
- Issue: `Close()` errors on archiver, scheduler DB, auth DB, and telebot client are all discarded with `_ =` during bot shutdown.
- Files: `internal/telegram/bot.go` (lines 144, 150, 153, 156)
- Impact: Corrupted database state on shutdown is never surfaced. Clean shutdown failures are invisible.
- Fix approach: Log each close error at `error` level. Consider a `errors.Join` approach for multi-close.

**Placeholder message deletion errors ignored:**
- Issue: `Delete(placeholder)` error discarded during streaming conversation.
- Files: `internal/telegram/conversation.go` (line 137)
- Impact: Orphaned placeholder messages clutter the chat silently.
- Fix approach: Log at `debug` level; the message is cosmetic, but the absence should be observable.

**Token last_used update inline error swallowed:**
- Issue: `Lookup` in auth store updates `last_used` as a fire-and-forget — the `ExecContext` result is discarded.
- Files: `internal/auth/store.go` (line 174)
- Impact: If the write fails (e.g., DB locked), the token still authenticates but the last-seen timestamp is stale. Security auditing based on last_used becomes unreliable.
- Fix approach: Either log the error or move to a buffered background writer pattern (already noted in the design comment on line 12-13).

## Bare Panic Risk

**`MustResolveProfiles` panics on invalid profile names:**
- Issue: `panic(err)` is the only error-handling path when toolset profile resolution fails.
- Files: `internal/toolsets/toolsets.go` (line 128)
- Impact: Any caller that passes an invalid profile name to `MustResolveProfiles` crashes the entire bot process — no recovery, no graceful degradation, no error logged before the panic.
- Fix approach: Replace with an `error` return and let callers decide the failure mode. If "must" semantics are needed, use `zap.Fatal` or an explicit crash after structured logging, not a bare panic. At minimum, wrap in a `slog.Fatal` so the reason appears in logs.

## Test Coverage Gaps

**Low-coverage packages (below 55%):**

| Package | Coverage | Risk |
|---------|----------|------|
| `internal/telegram` | 22.1% | **Critical** — all conversation orchestration, streaming, document handling, and access control. Most user-facing code is essentially untested. |
| `internal/tray` | 0.0% | Low — tray is a thin wrapper, but zero coverage means platform-specific startup paths (Windows systray init, non-Windows channel blocking) are never exercised. |
| `internal/setup` | 43.5% | High — first-run setup wizard, LLM probing, Telegram token minting. Failure here means the bot can't bootstrap. |
| `internal/skill` | 53.0% | Medium — skill execution runner used by agent jobs. Failures here cascade into scheduled job failures. |
| `internal/logging` | 55.2% | Low — zap wrapper, but log format changes could break structured logging contracts. |

**Why the `internal/telegram` gap matters:**
The `internal/telegram` package contains `conversation.go` (383 lines), `documents.go` (342 lines), `setup.go` (634 lines), and `scheduler_handlers.go` (507 lines). These are the tightest integration points: they coordinate LLM streaming, tool-call loops, OCR pipeline triggers, scheduler bootstrapping, auth checks, and user-facing Telegram message rendering. At 22.1% coverage, the bulk of execution paths are untested — bugs surface only when users hit them in production.

**Untested critical paths:**
- `handleConversation` — the main message handler, including streaming, archiving, and summarization triggers (`internal/telegram/conversation.go`)
- Browser-open in tray (`internal/tray/tray_windows.go` — no test file exists at all)
- Skill execution pipeline (`internal/skill`)

**Recommendation:** Prioritize `internal/telegram` coverage to at least 55%. Add an integration test that exercises a full conversation round-trip through a fake telebot context and captures LLM tool-call loop behavior.

## Large Files (High Complexity Risk)

**Files exceeding 400 lines (beyond tests):**

| File | Lines | Concern |
|------|-------|---------|
| `internal/scheduler/store.go` | 754 | Schema definitions + all CRUD for 5 tables + migration logic + dropLegacyConversations. Single responsibility is violated — this is a schema registry, migration engine, and data access layer in one file. |
| File-generation tool module | 599 | XLSX, DOCX, and PDF generation all in one file. Each format's `Execute` method is 100+ lines. |
| `internal/tools/source.go` | 488 | Five tool implementations (store_source, ocr_source, read_source, list_sources, lint_sources) in one file. |
| `internal/tools/memory_search.go` | 461 | Semantic search, source search, archive search, snippet formatting — all in one file. |
| `internal/api/types.go` | 401 | Every API response struct in one file. Grows with every new dashboard endpoint. |
| `internal/llm/openai.go` | 385 | OpenAI-compatible HTTP client with streaming, tool-call fragment accumulation, retry logic — all in one file. |
| `internal/tools/scheduler.go` | 473 | Multiple scheduler tool implementations in one file. |
| `internal/conversation/context.go` | 385 | Conversation context management, message trimming, summarization, and tool-result pairing. |

**Recommendation:** Defer the file-generation tool split to v1.1 Hardening Polish. Split `tools/source.go` into one file per tool in a later broad large-file refactor. Keep v1.0 focused on production blockers.

## Database Migration Strategy (Fragile)

**Ad-hoc per-store migration pattern:**
- Issue: Each store (`scheduler.Store`, `auth.Store`, `settings.Store`, `search.EmbedCache`, `swarm.Store`) manages its own `migrate()` method independently. There is no versioned migration framework, no transaction wrapping, and no rollback capability.
- Files: `internal/scheduler/store.go` (lines 156-184), `internal/auth/store.go`, `internal/settings/store.go` (lines 92-97), `internal/search/embed_cache.go` (line 72), `internal/swarm/store.go` (lines 117-140)
- Impact:
  1. If one store's migration fails mid-sequence, the DB is in an inconsistent state that other stores may or may not handle.
  2. No version tracking — there is no way to know what state a given `aura.db` is in.
  3. The `scheduler.Store` is the de facto migration authority for tables it doesn't own (`conversations`, `proposed_updates`, `wiki_issues`). If `scheduler.Store` is opened after `ArchiveStore`, the conversations table may not exist yet, leading to subtle failures.
  4. `ALTER TABLE` operations are not wrapped in transactions — if the process crashes between adding `schedule_every_minutes` and adding `schedule_weekdays`, the next startup will re-attempt the first ALTER (which fails because the column already exists) and the second won't run. The PRAGMA table_info check prevents crashes but masks the missed migration.
- Fix approach: Adopt a versioned migration system (e.g., `golang-migrate` or a simple `schema_versions` table with sequential up-only migrations). Run all migrations once at startup in a single transaction before any store is initialized.

**Multiple SQLite connection pools:**
- Issue: Multiple stores open their own `sql.Open("sqlite", path)` connections to the same `DB_PATH`. Although SQLite supports multiple readers, only one writer at a time — and multiple connection pools create independent WAL/journal management.
- Files: `internal/scheduler/store.go` (line 117), `internal/auth/store.go` (line 75), `internal/settings/store.go` (line 57), `internal/search/embed_cache.go` (line 68), `internal/swarm/store.go` (line 69)
- Impact: Potential `database is locked` errors under concurrent write load (e.g., scheduler ticks during conversation archive writes). No WAL mode is explicitly configured, so the default rollback journal serializes all writes.
- Fix approach: Centralize the `*sql.DB` creation in one place and pass it to all stores via `NewStoreWithDB`. Enable WAL mode (`PRAGMA journal_mode=WAL`) and set a busy timeout (`PRAGMA busy_timeout=5000`) at connection open time.

## Security Considerations

**No token expiration for dashboard bearer tokens:**
- Issue: Tokens in `api_tokens` have `issued_at` and `last_used` but no `expires_at`. Once issued, a token is valid forever (or until revoked).
- Files: `internal/auth/store.go` (schema on lines 38-45)
- Risk: A leaked token remains valid indefinitely. There is no automatic rotation or TTL.
- Recommendation: Add an `expires_at` column and enforce expiry in `Lookup`. Default to 30 days with a configurable `DASHBOARD_TOKEN_TTL_HOURS` environment variable.

**Tray browser-open via `rundll32`:**
- Issue: `exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()` passes a URL directly to the Windows shell.
- Files: `internal/tray/tray_windows.go` (line 50)
- Risk: If `DashboardURL` is ever user-controlled or derived from config that an attacker can modify, this becomes a command injection vector. In its current form (hardcoded from `HTTP_PORT`), the risk is low but the pattern is fragile.
- Recommendation: Validate the URL scheme before passing it to the shell. Reject anything that isn't `http:` or `https:`.

**Settings store holds secrets in plain text:**
- Issue: API keys (`LLM_API_KEY`, `MISTRAL_API_KEY`, `EMBEDDING_API_KEY`) can be stored in the `settings` table as plain text.
- Files: `internal/settings/store.go`, `internal/settings/types.go`
- Risk: Anyone with read access to `aura.db` can extract all API keys. The design doc acknowledges this (line 11-12: "OS-level file permissions are the security boundary"), but SQLite files are frequently backed up, copied, and shared.
- Current mitigation: TELEGRAM_TOKEN is excluded from settings and stays in `.env`.
- Recommendation: Encrypt secret values at rest using a derivation of a bootstrap master key (e.g., from `TELEGRAM_TOKEN`) and store only ciphertext in settings.

**No rate limiting on dashboard login:**
- Issue: The `POST /api/auth/logout` and `GET /api/auth/whoami` endpoints with invalid bearer tokens return 401 but there's no throttle on failed attempts.
- Files: `internal/auth/middleware.go`, `internal/api/router.go`
- Risk: Brute-force token guessing is possible. Tokens are 32 bytes of random → 256 bits of entropy, making this largely theoretical, but an attacker could still fill logs with auth failure noise.
- Current mitigation: `ErrInvalid` is generic — doesn't distinguish wrong/reused/revoked tokens.

## SQLite Configuration Gaps

**No WAL mode configured:**
- Issue: SQLite defaults to rollback journal mode. WAL is never explicitly enabled.
- Impact: All writes serialize. During concurrent usage (scheduler ticks + conversation archiving + dashboard writes), `database is locked` errors can occur.
- Fix approach: Execute `PRAGMA journal_mode=WAL;` once after opening the DB connection.

**No busy timeout configured:**
- Issue: No `PRAGMA busy_timeout` is set. Default is 0 (immediate failure on lock contention).
- Impact: Any write contention immediately fails instead of waiting briefly.
- Fix approach: `PRAGMA busy_timeout=5000;` (5-second wait before giving up).

**No foreign key enforcement:**
- Issue: `PRAGMA foreign_keys=ON` is not set.
- Impact: Referential integrity is not enforced at the database level (e.g., deleting an allowed user doesn't cascade to revoke their tokens — the auth store handles this in Go code).
- Current mitigation: Referential integrity is enforced in application code, but this is fragile.

## Code Duplication

**Repeated PRAGMA table_info pattern:**
- Issue: The migration pattern of "check PRAGMA table_info, ALTER TABLE if column missing" is repeated across multiple stores.
- Files: `internal/scheduler/store.go` (addEveryMinutesColumn, addScheduleWeekdaysColumn, addAgentJobResultColumns, addProposedUpdateReviewColumns), `internal/conversation/summarizer/applier.go`, `internal/swarm/store.go`
- Recommendation: Extract a shared `EnsureColumn(db, table, column, type, default)` helper.

**Repeated store initialization pattern:**
- Issue: Every store repeats the `sql.Open` → `Ping` → `migrate` → `Close on error` sequence.
- Files: `internal/auth/store.go`, `internal/scheduler/store.go`, `internal/settings/store.go`, `internal/search/embed_cache.go`, `internal/swarm/store.go`
- Recommendation: Centralize in a `db.Open(path)` that returns a migrated and pinged `*sql.DB`.

## Architecture Smells

**scheduler.Store as the de facto DB migration authority:**
- Issue: Tables that belong to other packages (`conversations`, `proposed_updates`, `wiki_issues`) are created by `scheduler.Store.migrate()` because it's guaranteed to run first during bot startup.
- Files: `internal/scheduler/store.go` (lines 169-183)
- Impact: If startup order changes or `scheduler.Store` is initialized lazily, these tables may not exist when their owning packages try to use them. This is a hidden temporal coupling.
- Fix approach: Either a) run all schema in a dedicated migration step before any store initialization, or b) make each owning package's store responsible for its own schema (and accept that they'll all run `CREATE TABLE IF NOT EXISTS` idempotently).

**`internal/tools` package is a monolithic catch-all:**
- Issue: Every built-in tool implementation lives in one flat package, regardless of domain (web, wiki, source, files, scheduler, memory, auth). The package has 488, 461, 599, 473-line files.
- Impact: Adding a new tool means touching an already-large package. Dependencies are broad (the package imports conversation, search, source, ocr, files, scheduler, skills, llm, wiki — nearly every other internal package).
- Recommendation: Consider domain-grouped tool sub-packages: `tools/wiki/`, `tools/source/`, `tools/files/`, `tools/web/`.

## Platform-Specific Code Concerns

**Windows tray no-op on non-Windows:**
- Issue: `tray_other.go` blocks indefinitely on a channel. On Linux/macOS, the tray "runs" but does nothing useful — it just blocks a goroutine.
- Files: `internal/tray/tray_other.go` (line 14-17)
- Impact: Minor — the goroutine is harmless, but it's a wasted resource and a misleading API (the caller expects a tray, gets a channel-block).
- Recommendation: Add a logged warning on non-Windows platforms: "tray: platform not supported, running headless."

**Windows-specific `rundll32` for browser open:**
- Issue: Hard dependency on Windows shell for opening a URL. No cross-platform `openBrowser` abstraction exists.
- Files: `internal/tray/tray_windows.go` (line 50)
- Impact: If tray support is ever added for macOS/Linux, the browser-open logic can't be shared.
- Recommendation: Extract an `openBrowser` function that uses `xdg-open` on Linux, `open` on macOS, and the current `rundll32` on Windows.

## Dependency Risks

**Beta Telegram library:**
- Issue: `gopkg.in/telebot.v4 v4.0.0-beta.7` — the core dependency that powers all user interaction is a beta release.
- Files: `go.mod` (line 5)
- Risk: API-breaking changes in the telebot library could require significant refactoring with no deprecation period. Beta releases may have undiscovered bugs.
- Recommendation: Pin to a specific commit hash rather than a beta tag, or contribute upstream to help move the library toward a stable release.

**Indirect dependencies from the full Go module graph include packages like `cloud.google.com/go/*` and `github.com/DataDog/datadog-go` that appear to be pulled in transitively but are unused by Aura's code — likely artifacts of the full module resolution graph.**

## Summary of Priority

v1.0 scope triage: the priority table remains the concern audit severity, not the active milestone boundary. v1.0 closes production-readiness blockers: shared SQLite ownership and PRAGMAs, migration safety, dashboard token expiry, settings API secret redaction, observable archive failures, Telegram critical-path tests, and release gates. v1.1 Hardening Polish defers the MustResolveProfiles panic fix unless production reachability promotes it back to a blocker, file-generation split, broad large-file refactors, tray coverage/browser polish, telebot beta monitoring docs, full settings at-rest encryption unless redaction proves insufficient, and arbitrary package-wide coverage targets.

| Priority | Area | Recommendation |
|----------|------|----------------|
| **P0** | `internal/telegram` 22.1% coverage | Add integration tests for main conversation handler |
| **P0** | Bare panic in `MustResolveProfiles` | Replace with error return or logged fatal |
| **P1** | Multiple SQLite connection pools | Centralize DB connection, enable WAL + busy_timeout |
| **P1** | No token expiration | Add `expires_at` to `api_tokens` |
| **P1** | `scheduler/store.go` (754 lines) | Extract migrations to separate file |
| **P2** | Ad-hoc per-store migrations | Versioned migration framework |
| **P2** | File-generation tool module (599 lines) | Split by file format |
| **P2** | Secrets in plain text in settings | At-rest encryption for API keys |
| **P3** | `tray` 0% coverage | Basic unit test for Windows/non-Windows paths |
| **P3** | Deprecated/unsupported beta dependency | Monitor telebot v4 for stable release |

---

*Concerns audit: 2026-05-04*
