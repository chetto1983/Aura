# Project Research Summary

**Project:** Aura — Go Telegram assistant with embedded React dashboard
**Domain:** Go monolith hardening (SQLite reliability, security, test coverage, release packaging)
**Researched:** 2026-05-04
**Confidence:** HIGH

## Executive Summary

Aura is a single-binary Go application with an embedded React 19 SPA and SQLite persistence. The v1.0 "Close Concern" hardening milestone targets 13 documented concerns from CONCERNS.md across reliability, security, code quality, test coverage, and release packaging. This research confirms a pure-Go approach throughout: no new dependencies are required for any of the hardening work.

The critical finding is that **SQLite connection centralization is the single-path dependency for all database reliability and security work**. Five independent `sql.Open("sqlite", path)` calls against the same `aura.db` file cause `SQLITE_BUSY` under concurrent writes and prevent any coherent migration or encryption story. Creating an `internal/db` package with a single `Open()` factory that applies WAL mode, busy_timeout, foreign_keys, and a versioned migration runner is the prerequisite for 6 of the 13 hardening items.

The recommended approach is a 7-phase implementation plan with a hard dependency chain: centralized DB → versioned migrations → token expiry + secrets encryption (parallel) → test coverage + file split + tray tests (parallel) → release packaging. Independent low-risk items (bare panic fix, error logging, file split) can run in parallel with the DB chain. All 13 items are required — this is a close-concern milestone, not an MVP with optional features.

## Key Findings

### Recommended Stack

All hardening work uses existing dependencies or Go stdlib. No `go get` commands are needed. The stack research verified that `modernc.org/sqlite` v1.50.0 (already the project's SQLite driver), `database/sql` (stdlib connection pooling), `crypto/aes` + `crypto/cipher` (stdlib AES-256-GCM), and `crypto/sha256` (stdlib key derivation) cover every requirement. A custom `schema_versions` table (~80 lines of Go) replaces per-store ad-hoc migrations. goreleaser v2 (already configured in `.goreleaser.yml`) handles the release pipeline with Pyodide bundling.

**Core technologies:**
- `modernc.org/sqlite` v1.50.0 + `database/sql`: Pure-Go SQLite with WAL mode and connection sharing — already in use, no CGO
- `crypto/aes` + `crypto/cipher` + `crypto/sha256` (stdlib): AES-256-GCM authenticated encryption for settings secrets — zero dependencies, already available
- Custom `schema_versions` table: Lightweight versioned migration runner — simpler than `golang-migrate`/`goose` for single-file SQLite, avoids external framework risk
- goreleaser v2: Cross-platform binary releases with embedded Pyodide runtime — already configured, smoke-tested in `before` hooks

### Expected Features

13 items across P0 (crash risk, critical coverage gaps) through P3 (platform polish, release readiness). DB centralization is the critical dependency path: it gates versioned migrations, which in turn gate token expiry, scheduler migration extraction, and secrets encryption.

**Must have (P0 — crash risk / critical untested paths):**
- Fix bare `panic(err)` in `MustResolveProfiles` — P0 crash risk; replaces panic with error return
- Boost `internal/telegram` test coverage from 22.1% to 55%+ — P0 coverage gap; 1866 lines of untested conversation orchestration

**Must have (P1 — reliability and security fundamentals):**
- Centralize SQLite DB connection to single pool — P1 reliability gap; gates all migration and schema work
- Add dashboard token expiration (`expires_at` with TTL) — P1 security gap; 30-day default, configurable
- Extract scheduler migrations to `scheduler/migrations.go` — P1 code-quality gap; de-monoliths 754-line store

**Must have (P2 — architecture and code quality):**
- Versioned migration framework with `schema_versions` table — P2 architecture gap; transactional, up-only, reproducible
- Encrypt secrets at rest (API keys in settings store) — P2 security gap; AES-256-GCM with auto-generated encryption key
- Split `tools/files.go` into per-format files — P2 code-quality gap; 650-line monolith → 4 focused files

**Must have (P3 — platform polish and release readiness):**
- Add tray unit test coverage + cross-platform `openBrowser` — P3 coverage + platform gap
- Document telebot beta dependency risk — P3 dependency gap; pin to commit hash, monitoring plan
- Bundle Pyodide runtime into Windows release artifact — P3 release gap
- Smoke test before publish — P3 quality gate; verifies Pyodide execution post-extract

**Defer (v1.0.1+ — post-validation):**
- Automated coverage scorecard in CI
- Rate limiting on dashboard auth endpoints
- Domain-grouped tool sub-packages
- Shared `EnsureColumn` migration helper

### Architecture Approach

The hardening introduces 4 new components (`internal/db/`, `internal/db/migrations/`, `internal/settings/encrypt.go`, `scripts/package.ps1`) and modifies 11 existing files. The central pattern is constructor injection: all stores switch from `OpenStore(path)` to `NewStoreWithDB(sharedDB)`, with `main.go` owning the single `*sql.DB` lifecycle via `db.Open()`. The migration runner applies pending migrations in a single `BEGIN IMMEDIATE` transaction before any store initializes, making all schema changes atomic and reproducible.

**Major components:**
1. `internal/db/` — Single `db.Open()` factory with WAL + busy_timeout + foreign_keys pragmas; sole owner of `*sql.DB` lifecycle
2. `internal/db/migrations/` — Versioned migration runner with `_migrations` tracking table; consolidates all per-store `CREATE TABLE` and `ALTER TABLE` into 8 numbered migration files
3. `internal/settings/encrypt.go` — AES-256-GCM encrypt/decrypt for `_API_KEY` fields; transparent at `Get()/Set()` boundary with `enc:v1:` prefix protocol
4. Modified `internal/auth/store.go` — `expires_at` column with expiry enforcement, `PurgeExpired()` background goroutine
5. Modified `internal/telegram/*_test.go` — ~24 new test functions across 4 new test files, targeting 55%+ coverage
6. Refactored `internal/tools/files{,_xlsx,_docx,_pdf}.go` — Monolith split into per-format files
7. `scripts/package.ps1` — Release archive builder with Pyodide bundle + smoke check

### Critical Pitfalls

Six critical pitfalls were identified, each mapped to a specific phase with prevention strategies and recovery procedures.

1. **Breaking existing stores with shared DB** — Stores that currently own their `*sql.DB` may double-close or panic when the connection is injected. Avoid by removing `Close()` methods from all 5 stores and delegating lifetime to `internal/db`. Phase 1.

2. **Partially-migrated databases on upgrade** — Users upgrading from pre-migration Aura have tables from ad-hoc `PRAGMA table_info` checks. Avoid with idempotent `IF NOT EXISTS` migrations and `_migrations` tracking table that records applied versions. Phase 2.

3. **Losing the encryption key due to TELEGRAM_TOKEN rotation** — Deriving the encryption key from a mutable secret creates permanent data loss risk. Avoid by storing a separate random `SETTINGS_ENCRYPTION_KEY` in `.env` on first boot, independent of the bot token. Phase 4.

4. **Expiring tokens for active dashboard sessions** — Adding `expires_at` enforcement immediately kills existing sessions. Avoid with a 7-day grace period for already-expired tokens, nullable migration column, and distinct `ErrExpired` return so the frontend can show "Session expired" instead of "Invalid token." Phase 3.

5. **Tests depending on real infrastructure** — Telegram integration tests that connect to real APIs become slow and flaky. Avoid by using hermetic temp SQLite files, stub `llm.Client`, and fake telebot context — following the established pattern from `setup_test.go`. Phase 5.

6. **Pyodide release bundling breaking non-Windows builds** — Pyodide runtime files in goreleaser cross-compilation artifacts. Avoid with `builder: windows` filter in goreleaser config and fail-closed sandbox runtime on non-Windows platforms. Phase 7.

## Implications for Roadmap

Based on research, suggested phase structure:

### Phase 1: DB Centralization
**Rationale:** Critical path blocker — all migration, encryption, token expiry, and PRAGMA work requires a single `*sql.DB`. Must ship first; nothing depends on anything else, but 6 items depend on this.
**Delivers:** `internal/db` package with `Open()` factory; all 5 stores switched to `NewStoreWithDB`; WAL + busy_timeout + foreign_keys enabled.
**Addresses:** Fix bare panic, centralize SQLite, enable PRAGMAs.
**Avoids:** Breaking stores with shared DB (Pitfall 1) — remove all per-store `Close()` methods.

### Phase 2: Versioned Migrations
**Rationale:** Gates all schema changes (token expiry, encryption, scheduler extraction). Must follow Phase 1 because it needs the shared `*sql.DB`.
**Delivers:** `internal/db/migrations/` with migration runner, `_migrations` tracking table, 8 numbered migration files consolidating all per-store schema.
**Uses:** Custom `schema_versions` table pattern from STACK.md.
**Implements:** Migration runner component from ARCHITECTURE.md.
**Avoids:** Partially-migrated DBs on upgrade (Pitfall 2) — idempotent migrations + version tracking.

### Phase 3: Token Expiry
**Rationale:** Quick security win after migrations land. Independent of encryption — can parallel with Phase 4.
**Delivers:** `expires_at` column on `api_tokens`, expiry enforcement in `Lookup()`, `PurgeExpired()` goroutine, `DASHBOARD_TOKEN_TTL_HOURS` env var.
**Uses:** Migration 008 from Phase 2.
**Avoids:** Expiring active sessions (Pitfall 4) — 7-day grace period, nullable column, distinct `ErrExpired`.

### Phase 4: Secrets Encryption
**Rationale:** Second security gap. Requires migrations (Phase 2) for settings schema. Can parallel with Phase 3.
**Delivers:** `internal/settings/encrypt.go` with AES-256-GCM encrypt/decrypt; auto-generated `SETTINGS_ENCRYPTION_KEY` in `.env`; transparent `enc:v1:` prefix protocol.
**Uses:** `crypto/aes` + `crypto/cipher` (stdlib) from STACK.md.
**Avoids:** Losing encryption key (Pitfall 3) — separate random key, not derived from TELEGRAM_TOKEN.

### Phase 5: Telegram Test Coverage
**Rationale:** Largest single effort. Independent of all DB work — can start anytime, but tests validate the hardened codebase.
**Delivers:** ~24 new test functions across 4 new test files; coverage from 22.1% to 55%+; hermetic fixtures, stub LLM client, fake telebot context.
**Avoids:** Tests with real infrastructure (Pitfall 5) — temp SQLite, canned responses, no real API keys.

### Phase 6: Code Quality (File Split + Tray Tests + Scheduler Extraction + Error Logging)
**Rationale:** Independent items that can run in parallel with Phases 3–5. Pure refactor/test additions, no behavioral changes.
**Delivers:** Split `tools/files.go` into per-format files; tray test coverage + cross-platform `openBrowser`; extracted `scheduler/migrations.go`; slog logging at 7 discard sites.

### Phase 7: Release Packaging
**Rationale:** Final deliverable — packages everything once all hardening is verified. Gates on stable test suite.
**Delivers:** `scripts/package.ps1`, `make release` target, Pyodide-bundled Windows zip, smoke test gate.
**Avoids:** Pyodide breaking non-Windows builds (Pitfall 6) — `builder: windows` filter, fail-closed sandbox.

### Phase Ordering Rationale

- **DB centralization must ship first** — it is the prerequisite for 6 of 13 items. Without it, no migration framework, no encryption, no token expiry, no PRAGMAs.
- **Versioned migrations must follow immediately** — it gates token expiry, encryption, and scheduler extraction which all produce schema changes.
- **Token expiry and encryption can parallel** — both depend on migrations but not on each other. Together they close both security gaps.
- **Test coverage, file split, and tray tests can parallel with everything** — they have zero dependencies on the DB chain. Running them concurrently with Phases 3–4 maximizes throughput.
- **Error logging and bare panic fix can ship in parallel with Phase 1** — they're independent low-risk items that improve observability immediately.
- **Release packaging is the final task** — it packages the hardened binary. Must run after all fixes and tests are verified stable.

### Research Flags

Phases needing deeper research during planning:
- **Phase 5 (Telegram coverage):** Complex — requires determining which conversation paths are critical vs edge cases, building reusable test harness, deciding mock vs stub vs fixture approach for LLM responses.
- **Phase 7 (Release packaging):** Cross-platform — needs verification on a clean Windows VM that Pyodide works post-extract; smoke test must cover all sandbox runtime modes.

Phases with standard patterns (skip research-phase):
- **Phases 1–2 (DB centralization + migrations):** Well-established Go patterns — `database/sql` connection pooling, `CREATE TABLE IF NOT EXISTS` idempotency, ordered migration slices. Pattern already partially implemented via `NewStoreWithDB` constructors.
- **Phase 3 (Token expiry):** Standard auth pattern — expiry timestamp, background purge, configurable TTL. No novel design work.
- **Phase 4 (Secrets encryption):** Standard crypto — AES-256-GCM key derivation + authenticated encryption. Go stdlib reference implementation available.
- **Phase 6 (File split + tray tests + error logging):** Pure refactor — no design decisions, follow existing patterns.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | All technologies verified against `go.mod`, `.goreleaser.yml`, and source code. No new dependencies required; all capabilities exist in stdlib or current modules. |
| Features | HIGH | All 13 items sourced from audited CONCERNS.md with explicit file/line references. Feature landscape classified by industry-standard table-stakes/differentiator/anti-feature model. |
| Architecture | HIGH | All integration points verified against live codebase (`cmd/aura/main.go`, `internal/telegram/setup.go`, 5 store files). Modified components and wire paths traced end-to-end. |
| Pitfalls | HIGH | All 6 pitfalls correspond to verified architecture risks. Each mapped to a specific phase with concrete prevention strategy and recoverable failure mode. Recovery cost is LOW for all except partial migrations (MEDIUM). |

**Overall confidence:** HIGH

### Gaps to Address

- **Encryption key lifecycle beyond initial generation:** Research covers generation and single-key encryption. Full key rotation (re-encrypting all settings with a new key) is deferred to v1.0.1+. Recovery is documented as "re-enter keys manually" — sufficient for v1.0.
- **Telegram integration test harness complexity:** Research identifies the approach (hermetic SQLite, stub LLM, fake context) but the exact harness design needs decisions during Phase 5 planning — specifically fixture format for conversation responses and how to exercise the streaming edit loop without real network I/O.
- **Pyodide smoke test on clean Windows:** Research assumes smoke test passes on a clean install. Actual verification on a Windows VM without pre-existing Pyodide files is needed during Phase 7 execution.

## Sources

### Primary (HIGH confidence)
- `.planning/codebase/CONCERNS.md` — full codebase audit with file/line references for all 13 hardening items
- `.planning/codebase/ARCHITECTURE.md` — current system architecture, startup sequence, component wiring
- `go.mod` — verified dependencies: `modernc.org/sqlite` v1.50.0, `golang.org/x/crypto` v0.48.0 (indirect)
- `.goreleaser.yml` — verified release pipeline: before hooks, CGO_ENABLED=0, GOOS/GOARCH matrix
- Source code: `cmd/aura/main.go`, `internal/telegram/setup.go`, `internal/auth/store.go`, `internal/settings/store.go`, `internal/scheduler/store.go`, `internal/search/embed_cache.go`, `internal/tools/files.go`, `internal/tray/`

### Secondary (MEDIUM confidence)
- Go stdlib documentation (`crypto/aes`, `crypto/cipher`, `crypto/sha256`, `crypto/rand`, `database/sql`) — verified AES-256-GCM authenticated encryption and connection pool patterns
- SQLite documentation (WAL mode, busy_timeout, foreign_keys pragmas) — verified correctness of PRAGMA settings
- goreleaser v2 documentation — verified `extra_files`, `builder` filter, and archive pattern

### Tertiary (LOW confidence)
- golang-migrate vs goose comparison — evaluated for alternatives section; both rejected in favor of custom `schema_versions` for simplicity at current scale

---

*Research completed: 2026-05-04*
*Ready for roadmap: yes*
