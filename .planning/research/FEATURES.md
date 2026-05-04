# Feature Research

**Domain:** Go codebase hardening — bug fixes, test coverage, security, architecture, release packaging
**Researched:** 2026-05-04
**Confidence:** HIGH (all features sourced from audited CONCERNS.md with explicit file/line references)

## Feature Landscape

### Table Stakes (Users Expect These)

Features users assume exist in a production Go application. Missing these = app feels fragile and unshippable.

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Fix bare panic → error return in `MustResolveProfiles` | Go apps should never crash the process for recoverable input errors | LOW | Replace `panic(err)` with `error` return; callers already handle errors from other functions in the same file (`toolsets/toolsets.go:128`) |
| Log all ignored/discarded errors in production paths | Silent failures are invisible to operators; shutdown corruption, orphaned messages, stale timestamps all degrade reliability | LOW | Add `slog.Warn`/`slog.Error` calls at 7 discard sites: shutdown close (4x `bot.go:144,150,153,156`), tray browser open (`tray_windows.go:50`), placeholder deletion (`conversation.go:137`), token last_used update (`auth/store.go:174`) |
| Centralize SQLite DB connection to single pool | Multiple pools to same file cause `database is locked` errors under concurrent writes | MEDIUM | Create one `*sql.DB` in a shared `db.Open()` and pass to all stores; enable WAL mode, `busy_timeout=5000`, `foreign_keys=ON` at connection time. Impacts 5 stores and their migration chains. |
| Add dashboard token expiration (`expires_at` with TTL) | Tokens that never expire are a security liability; a leaked token is permanently valid | LOW | Add `expires_at` column to `api_tokens`, enforce in `Lookup`; default 30-day TTL configurable via `DASHBOARD_TOKEN_TTL_HOURS` env var |
| Boost `internal/telegram` test coverage 22.1% → 55%+ | Core conversation orchestration, streaming, document handling, access control at 22.1% means most user-facing paths are untested | HIGH | Add integration tests for `handleConversation` with fake telebot context; exercise streaming + tool-call loop + archiving + summarization triggers. This is the largest single effort in the milestone. |
| Extract `scheduler/store.go` migrations to `scheduler/migrations.go` | 754-line file mixes schema, CRUD, and migration logic — single responsibility violated | MEDIUM | Move `migrate()`, `dropLegacyConversations()`, and column-addition helpers to a dedicated file; keep CRUD in `store.go` |
| Split `tools/files.go` into per-format files | 599 lines mixing XLSX/DOCX/PDF generation; each format's `Execute` is 100+ lines | LOW | Create `tools/files_xlsx.go`, `tools/files_docx.go`, `tools/files_pdf.go`; no logic changes, pure file-split |
| Enable SQLite PRAGMAs: WAL mode + busy_timeout + foreign_keys | SQLite defaults serialize writes, fail immediately on lock, and skip referential integrity checks | LOW (must pair with #3) | Three PRAGMAs executed once after DB open; zero code impact on existing queries |
| Add tray unit test coverage + cross-platform `openBrowser` | Zero tray coverage means Windows systray init paths are never exercised; browser-open is Windows-only | MEDIUM | Write tests for Windows/non-Windows tray paths; extract `openBrowser(url)` using `xdg-open` (Linux), `open` (macOS), `rundll32` (Windows) with URL scheme validation |

### Differentiators (Competitive Advantage)

Features that move Aura from "hobby project" to "production-grade tool." Not required for basic operation, but signal engineering maturity.

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Versioned migration framework with `schema_versions` table | Eliminates invisible failed migrations, inconsistent DB states, and manual PRAGMA-based column checks across 5 stores | MEDIUM | Run all migrations once at startup in a single transaction before any store init. Replace ad-hoc `PRAGMA table_info` checks with sequential up-only migrations keyed by version number. Enables reproducible `aura.db` state across deployments. |
| Encrypt API keys at rest in settings store | Protects secrets from anyone with file access to `aura.db` (backups, copies, sharing) | MEDIUM | Derive encryption key from `TELEGRAM_TOKEN` (already excluded from settings); store only ciphertext for `LLM_API_KEY`, `MISTRAL_API_KEY`, `EMBEDDING_API_KEY`; requires versioned migrations first (schema change for ciphertext columns) |
| Automated test coverage enforcement per-package | Prevents regression after raising `internal/telegram` to 55%+; catches untested paths before they reach production | LOW | Add `go test -coverprofile` to CI (if exists) or a `make coverage` target with per-package thresholds; integrates with existing `go test ./...` patterns |
| Structured release artifact with Pyodide bundled | Eliminates "works on my machine" runtime dependency; users get a single zip with everything needed | MEDIUM | Bundle `runtime/pyodide/` into Windows release zip; smoke-test that Pyodide execution works post-unzip before publishing |

### Anti-Features (Commonly Requested, Often Problematic)

Features that seem good during hardening but create unnecessary risk or scope creep. Documented to prevent well-intentioned additions from derailing the milestone.

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| Replace SQLite with PostgreSQL | "Production databases use Postgres" | SQLite is explicitly chosen for local-first simplicity (PROJECT.md:60-61); replacing it requires rewrite of 5 stores, breaks single-binary deployment, and adds Docker dependency | Keep SQLite; add WAL + centralized pool + versioned migrations to close the reliability gaps without changing DB engine |
| Replace chromem-go with sqlite-vector for embeddings | "Native vector extension is faster" | Already evaluated and rejected in slice 11h (PROJECT.md:48); requires CGO/native extension loading which breaks single-binary portability | chromem-go gets ~99% of the win with zero CGO risk (PROJECT.md:72) |
| Real-time dashboard via WebSocket | "Dashboard should update live" | Adds significant complexity to API, auth, and frontend; not needed for hardening — the dashboard already refreshes on page load | Keep polling-based refresh; WebSocket is a feature-development concern, not hardening |
| Mobile app | "Users want push notifications" | Requires a completely separate client, push infrastructure, and cross-platform testing matrix | Web dashboard is sufficient (PROJECT.md:50); Telegram already delivers notifications |
| Distributed multi-process migration support | "What about horizontal scaling?" | Aura is a single-binary local app; distributed migration patterns (leader election, migration locks) add zero value and significant complexity | Single-process sequential migrations are correct for this architecture |
| Replace `telebot` beta with stable alternative | "Beta dependency is risky" | There is no Go Telegram Bot API library with comparable streaming and conversation support; alternatives either lack features or are abandoned | Pin to commit hash, monitor upstream for stable release, contribute back (PROJECT.md:186) |
| Full end-to-end tests with real Telegram API | "Integration tests should use real services" | Requires a real Telegram bot token, real Telegram API calls, and network access; flaky, slow, and couples CI to external service availability | Use fake telebot context + recorded LLM responses for integration tests; keep real-API testing as a manual smoke step before release |

## Feature Dependencies

```
Fix Bare Panic (Low, independent)
    └──no deps──>

Log Ignored Errors (Low, independent)
    └──no deps──>

Boost Telegram Coverage (High, independent)
    └──no deps──>

Tray Tests + openBrowser (Medium, independent)
    └──no deps──>

Document telebot Beta (Low, independent)
    └──no deps──>

Split tools/files.go (Low, independent)
    └──no deps──>

──────────────────────────────────────
Centralize SQLite DB Connection (Medium)
    ├──blocks nothing, but required before any migration work──>
    │
    ├──required by──> Versioned Migration Framework (Medium)
    │                      ├──required by──> Extract Scheduler Migrations (Medium)
    │                      ├──required by──> Add Token Expiration (Low)
    │                      │                      [adds expires_at via framework]
    │                      │
    │                      └──required by──> Encrypt Secrets at Rest (Medium)
    │                                             [adds ciphertext columns via framework]
    │
    └──required by──> Enable SQLite PRAGMAs (Low)
                           [WAL + busy_timeout + foreign_keys — runs immediately after DB open]

──────────────────────────────────────
Bundle Pyodide + Smoke Test (Medium)
    └──should run after──> Telegram Coverage Boost
    └──should run after──> Tray Tests + openBrowser
    [depends on stable tests to validate release artifact]
```

### Dependency Notes

- **Centralize SQLite DB Connection is the critical path blocker:** All migration work (versioned framework, scheduler extraction, token expiry, encryption, PRAGMAs) requires a single `*sql.DB` as input. This must be the first implementation task in the milestone.
- **Versioned Migration Framework gates all schema changes:** Token expiry (`expires_at`), encryption (ciphertext columns), and extracted scheduler migrations all produce schema changes. Running them through the framework ensures they're versioned, transactional, and recoverable.
- **Encrypt Secrets requires both Centralized DB AND Versioned Migrations:** Encryption needs the centralized DB to access settings store, and versioned migrations to add ciphertext columns safely.
- **Bundle Pyodide + Smoke is the final task:** Release packaging should run after test coverage is raised and all fixes are verified. Smoke test depends on stable code, not on specific fixes.
- **All `LOW` independent items can run in parallel:** Fix bare panic, log ignored errors, split tools/files.go, document telebot — these have zero dependencies on each other or on the DB centralization chain.

## MVP Definition

### Launch With (v1.0 Close Concern — All 13 Required)

The milestone is a close-concern hardening pass, not a product launch. Every item closes a documented concern from CONCERNS.md. Nothing is optional — each has a matching concern to resolve.

- [ ] **Fix bare panic in `MustResolveProfiles`** — P0 crash risk; replaces `panic(err)` with error return. Closes CONCERNS.md:31-38.
- [ ] **Log ignored errors across 7 discard sites** — P1 observability gap; adds `slog` calls to shutdown close, tray browser, placeholder deletion, token last_used. Closes CONCERNS.md:7-29.
- [ ] **Centralize SQLite DB connection to single pool** — P1 reliability gap; one `*sql.DB` for all 5 stores, WAL mode, busy_timeout, foreign_keys. Closes CONCERNS.md:90-94, 123-138.
- [ ] **Add dashboard token expiration (`expires_at` with TTL)** — P1 security gap; tokens expire after configurable TTL. Closes CONCERNS.md:98-102.
- [ ] **Boost `internal/telegram` test coverage 22.1% → 55%+** — P0 coverage gap; integration tests for main conversation handler. Closes CONCERNS.md:41-59.
- [ ] **Extract scheduler migrations to `scheduler/migrations.go`** — P1 code-quality gap; splits 754-line monolithic store. Closes CONCERNS.md:63-77.
- [ ] **Versioned migration framework with `schema_versions` table** — P2 architecture gap; centralized, transactional, up-only migrations for all stores. Closes CONCERNS.md:78-88.
- [ ] **Split `tools/files.go` into per-format files** — P2 code-quality gap; `files_xlsx.go`, `files_docx.go`, `files_pdf.go`. Closes CONCERNS.md:64-65.
- [ ] **Encrypt secrets at rest in settings store** — P2 security gap; AES-GCM encryption of API keys with key derived from `TELEGRAM_TOKEN`. Closes CONCERNS.md:110-115.
- [ ] **Tray unit test coverage + cross-platform `openBrowser`** — P3 coverage + platform gap; tests for Windows/non-Windows paths, abstraction for browser-open. Closes CONCERNS.md:46-48, 165-177.
- [ ] **Document telebot beta dependency risk** — P3 dependency gap; pin to commit hash, document monitoring plan, commit to contributing upstream. Closes CONCERNS.md:179-187.
- [ ] **Bundle Pyodide runtime into Windows release artifact** — P3 release gap; include `runtime/pyodide/` in release zip. Closes implicit packaging concern: no release artifact exists.
- [ ] **Smoke test before publish** — P3 release gap; verify Pyodide execution works post-extract before publishing release. Closes CONCERNS.md implicit quality concern.

### Add After Validation (v1.0.1+)

Features that build on successful v1.0 hardening but aren't required to close concerns.

- [ ] **Automated coverage scorecard in CI** — Trigger: once telegram coverage hits 55%+, add a coverage gate to prevent regression
- [ ] **Rate limiting on dashboard auth endpoints** — Trigger: if brute-force attempts appear in logs (currently theoretical — 256-bit random tokens)
- [ ] **`errors.Join` for multi-close shutdown** — Trigger: if close-error logs become noisy enough to warrant aggregation
- [ ] **Domain-grouped tool sub-packages (`tools/wiki/`, `tools/source/`, `tools/files/`)** — Trigger: any new tool development requires touching the monolithic package again
- [ ] **Shared `EnsureColumn` migration helper** — Trigger: next time a column addition is needed (5+ stores currently duplicate the PRAGMA pattern)

### Future Consideration (v2+)

Features deferred until hardening is complete and validated.

- [ ] **Contribute to telebot v4 upstream** — Reason: beta dependency is working; contribution effort is better spent after Aura itself is stable
- [ ] **Cross-platform tray** — Reason: Windows tray is sufficient; Linux/macOS tray requires systray library evaluation; non-Windows currently degrades gracefully with logged warning
- [ ] **Git-backed wiki** — Reason: already optional (PROJECT.md:56); hardening milestone doesn't touch wiki filesystem layer

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority | Notes |
|---------|------------|---------------------|----------|-------|
| Fix bare panic → error return | HIGH (crashes kill user sessions) | LOW (1 line change + caller adaptation) | P0 | Immediate safety win |
| Boost telegram test coverage 22.1% → 55%+ | HIGH (catches regression in most critical package) | HIGH (383+342+634+507 = 1866 lines to test) | P0 | Largest single effort; plan for integration tests not unit-level mocks |
| Centralize SQLite DB connection | HIGH (prevents lock errors, enables all migration work) | MEDIUM (touches 5 store init paths) | P1 | Critical path blocker — must ship first |
| Add dashboard token expiration | HIGH (closes the largest security gap) | LOW (1 column + Lookup check + config) | P1 | Quick win after centralized DB |
| Extract scheduler migrations to separate file | MEDIUM (improves maintainability) | MEDIUM (refactor, no logic change) | P1 | De-risked by versioned migrations framework |
| Versioned migration framework | HIGH (eliminates fragile, invisible migration failures) | MEDIUM (new schema_versions table + loader) | P2 | Required by 3 downstream items |
| Split `tools/files.go` into per-format files | MEDIUM (cleaner codebase) | LOW (file-split, no logic change) | P2 | Independent — can ship in parallel with migration work |
| Encrypt secrets at rest | HIGH (closes the second security gap) | MEDIUM (AES-GCM + key derivation + schema change) | P2 | Requires versioned migrations first |
| Tray unit test coverage + cross-platform browser | LOW (tray is thin wrapper) | MEDIUM (test infrastructure + platform abstraction) | P3 | Platform work — Windows-first with non-Windows graceful degradation |
| Document telebot beta dependency risk | MEDIUM (risk awareness) | LOW (docs only) | P3 | No code change, pure documentation |
| Bundle Pyodide into Windows release | MEDIUM (eliminates runtime dependency) | MEDIUM (packaging + smoke) | P3 | Final deliverable; gates on test coverage passing |

**Priority key:**
- P0: Must ship immediately — crash risk or critical untested paths
- P1: Required for the milestone — reliability and security fundamentals
- P2: Required for the milestone — architecture and code quality
- P3: Required for the milestone — platform polish and release readiness

All items are required — this is a close-concern milestone, not a feature-prioritized launch. Priority tiers reflect implementation ordering (P0 first, P3 last) rather than optionality.

## Competitor Feature Analysis

Since this is a hardening-only milestone on an existing app, competitor analysis maps to "what production Go projects do for these concerns" rather than feature competition.

| Concern | Industry Standard | OpenClaude/MCP Ecosystem | Our Approach |
|---------|-------------------|--------------------------|--------------|
| Error handling | slog with structured errors; `errcheck` linters; no bare panics | Variable — some tools panic, some return errors | Fix bare panic; log all 7 discard sites with slog |
| DB connection pooling | Single `*sql.DB` with `SetMaxOpenConns`, WAL mode, busy_timeout | SQLite is common; connection patterns vary widely | Centralize to single pool; WAL + 5s busy_timeout + foreign_keys ON |
| Database migrations | `golang-migrate/migrate` OR custom `schema_versions` table; up-only, transactional | Most Go apps use golang-migrate or homegrown | Custom `schema_versions` approach — avoids external dependency, simpler for single-DB SQLite, fits "no new deps" constraint |
| Token management | JWT or opaque tokens with TTL, refresh, revocation lists | Opaque tokens are standard in CLI tools | Add `expires_at` to existing opaque tokens; configurable TTL, no JWT needed |
| Secret encryption | AES-GCM with derived key (crypto/aes + crypto/cipher from stdlib); NaCl secretbox | Varies — `.env` only, OS keychain, or at-rest encryption | Pure-Go AES-GCM from stdlib; key derived from TELEGRAM_TOKEN (already excluded from settings); no external lib needed |
| Test coverage gates | 50-80% per package with CI enforcement | 0-30% typical for hobby projects | 55% target for `internal/telegram` (from 22.1%); per-package thresholds as future post-validation |
| Release packaging | goreleaser for Go binaries; platform-specific installers (NSIS/MSI for Windows) | Manual build or simple zip | Bundled Pyodide runtime in release zip; smoke test gate before publish |

## Sources

- **CONCERNS.md** — Full codebase audit with file/line references for every concern
- **PROJECT.md** — Active requirements list (11 items) and Key Decisions that constrain approach
- **go.mod** — Current dependencies: `modernc.org/sqlite` v1.50.0 (pure-Go SQLite driver), `golang.org/x/crypto` v0.48.0 (already available transitively for key derivation)
- **Industry references:**
  - Michael Nygard ADR template (2011) — foundation for Architecture Decision Records used at AWS, Google, Spotify
  - Kubernetes Enhancement Proposal (KEP) template — yaml metadata + README pattern for versioned proposals
  - Rust RFC template (already fetched earlier) — clean, minimal RFC structure
  - golang-migrate/migrate — most popular Go migration library; rejected in favor of custom `schema_versions` for simplicity

---

*Feature research for: Go codebase hardening — v1.0 Close Concern milestone*
*Researched: 2026-05-04*
