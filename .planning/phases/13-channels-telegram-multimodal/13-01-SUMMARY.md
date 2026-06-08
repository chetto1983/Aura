---
phase: 13-channels-telegram-multimodal
plan: 01
subsystem: database
tags: [telegram, telebot, postgres, sqlc, pgx, migration, goleak, x-image, qrterminal, onboarding]

# Dependency graph
requires:
  - phase: 04-conversations-identity-pause
    provides: "aura.identities table (FK parent) + the canonical askuser/identity Store pattern (Store{pool,q}, SQLSTATE classification, db.WithTx atomic writes)"
  - phase: 12-agui-gateway
    provides: "internal/agui/fanout.go seam the Telegram channel consumes per-turn (downstream plans)"
provides:
  - "Three external deps pinned + anchored: gopkg.in/telebot.v4 v4.0.0-beta.9, golang.org/x/image v0.41.0 (direct), github.com/mdp/qrterminal/v3 v3.2.1"
  - "Migration 0012: aura.telegram_accounts + aura.telegram_setup_pending (single-use onboarding token, 1h TTL, partial active index)"
  - "sqlc queries for both tables (atomic consume-and-return) + regenerated sqlc client"
  - "internal/channels/telegram.Store — atomic onboarding consume-and-INSERT, single-use enforcement, SQLSTATE sentinels"
  - "Three goleak TestMain harnesses (internal/channels, internal/channels/telegram, internal/setup) for the downstream Wave plans"
affects: [13-02-send-file-artifact, 13-03-telegram-channel, 13-04-tables-rendering, 13-07-setup-wizard, 13-08-multimodal]

# Tech tracking
tech-stack:
  added: ["gopkg.in/telebot.v4 v4.0.0-beta.9", "golang.org/x/image v0.41.0 (direct)", "github.com/mdp/qrterminal/v3 v3.2.1", "rsc.io/qr (indirect)", "golang.org/x/term (indirect)"]
  patterns: ["blank-import dep anchor (internal/channels/deps.go) keeps not-yet-consumed pins through go mod tidy", "telegram.Store copies the canonical askuser/identity Store pattern", "atomic single-use onboarding via db.WithTx consume-then-INSERT"]

key-files:
  created:
    - internal/channels/deps.go
    - internal/db/migrations/0012_telegram.up.sql
    - internal/db/migrations/0012_telegram.down.sql
    - internal/db/queries/telegram_accounts.sql
    - internal/db/queries/telegram_setup_pending.sql
    - internal/db/sqlc/telegram_accounts.sql.go
    - internal/db/sqlc/telegram_setup_pending.sql.go
    - internal/channels/telegram/store.go
    - internal/channels/telegram/store_integration_test.go
    - internal/channels/main_test.go
    - internal/channels/telegram/main_test.go
    - internal/setup/main_test.go
  modified:
    - go.mod
    - go.sum
    - internal/db/sqlc/models.go
    - internal/db/sqlc/querier.go

key-decisions:
  - "Pinned deps need a blank-import anchor (internal/channels/deps.go): go mod tidy drops not-yet-imported pins, which would break the amendment-#58 CI pin gate and re-demote x/image to indirect. Mirrors internal/cron/tzdata.go."
  - "ConsumeOnboarding collapses unknown/spent/expired tokens to ErrTokenConsumed on the consume path (single-use chokepoint); PendingConsumed distinguishes unknown via ErrTokenNotFound."
  - "internal/setup is a test-only package this plan (only main_test.go); server.go/handlers.go land in Plan 13-07. The goleak harness must exist first so 13-07's SSE-pump tests inherit the leak check."

patterns-established:
  - "Dep anchor: blank-import the actual subpackages a Wave consumer will use, with a doc-comment explaining why tidy would otherwise drop the pin."
  - "telegram.Store: Store{pool,q}, sentinels (ErrTokenConsumed/ErrTokenNotFound/ErrAccountExists), SQLSTATE 23505 via errors.As+pgErr.Code (never message-match), pgtype at the boundary, db.WithTx for the atomic consume-and-INSERT."

requirements-completed: [UX-02, UX-03]

# Metrics
duration: ~22min
completed: 2026-06-08
---

# Phase 13 Plan 01: Channels + Telegram Data Substrate Summary

**Phase-13 dependency floor + migration 0012 (telegram_accounts + single-use telegram_setup_pending) + telegram.Store atomic onboarding consume-and-INSERT + three goleak TestMain harnesses for the downstream Wave packages.**

## Performance

- **Duration:** ~22 min
- **Started:** 2026-06-08
- **Completed:** 2026-06-08
- **Tasks:** 3
- **Files modified:** 16 (12 created, 4 modified)

## Accomplishments
- Three external deps installed behind the operator-approved legitimacy gate and pinned at the exact amendment-#58 versions, kept durable through `go mod tidy` via a blank-import anchor (`internal/channels/deps.go`).
- Migration 0012 ships `aura.telegram_accounts` (PK `telegram_user_id`, FK `identity_id → aura.identities`) and `aura.telegram_setup_pending` (single-use `consumed_at`, 1h `expires_at`, partial active index `WHERE consumed_at IS NULL`), with `aura_app` DML grants + `aura_migrate GRANT ALL` + COMMENT, mirroring 0009/0004.
- `telegram.Store` is the sole DB seam for both onboarding (UX-03) and the channel (UX-02): `ConsumeOnboarding` is the single-use chokepoint — one `db.WithTx` marks the token consumed and INSERTs the account atomically; re-consume/expired → `ErrTokenConsumed`, duplicate account → `ErrAccountExists` via SQLSTATE 23505.
- Three `goleak.VerifyTestMain` harnesses created so the Wave plans (13-03 bot polling, 13-04 async convert, 13-07 SSE pump) inherit the amendment-#15 leak gate.

## Task Commits

Each task was committed atomically:

1. **Task 1: Package legitimacy gate + add deps** - `05eeca32` (feat)
2. **Task 2: Migration 0012 + sqlc queries + telegram.Store** - `3ec353e8` (feat, TDD: migration + queries + Store + db_integration test in one atomic commit)
3. **Task 3: goleak TestMain harnesses** - `1606a077` (test)

**Plan metadata:** _(this SUMMARY + STATE + ROADMAP commit follows)_

## Files Created/Modified
- `internal/channels/deps.go` - Blank-import anchor keeping telebot.v4 / x/image / qrterminal pinned through `go mod tidy` until their Wave consumers land.
- `internal/db/migrations/0012_telegram.{up,down}.sql` - telegram_accounts + telegram_setup_pending DDL with grants/COMMENT/FK/partial index.
- `internal/db/queries/telegram_accounts.sql` - 6 sqlc queries (insert/get-by-tg-id/get-by-identity/touch-last-seen/count/list).
- `internal/db/queries/telegram_setup_pending.sql` - 3 sqlc queries incl. atomic `ConsumeTelegramSetupPending` (UPDATE...RETURNING with consumed_at IS NULL + expires_at > now() guards) and `:execrows` cleanup.
- `internal/db/sqlc/telegram_accounts.sql.go` + `telegram_setup_pending.sql.go` + `models.go` + `querier.go` - Regenerated sqlc client (two new structs + querier entries; no other tables touched).
- `internal/channels/telegram/store.go` - Store{pool,q} adapter: InsertPending, ConsumeOnboarding (atomic), CleanupExpired, PendingConsumed, GetAccountByTelegramID, TouchLastSeen, CountAccounts, ListAccounts; sentinels + SQLSTATE classification + pgtype boundary helpers.
- `internal/channels/telegram/store_integration_test.go` - db_integration tier: onboarding round-trip, single-use re-consume, expired/unknown token, duplicate-account classification, cleanup-expired (no-skip-as-green env gate copied from askuser).
- `internal/channels/main_test.go`, `internal/channels/telegram/main_test.go`, `internal/setup/main_test.go` - goleak TestMain harnesses.
- `go.mod` / `go.sum` - Three pins (telebot.v4 literal, x/image direct, qrterminal) + transitive (rsc.io/qr, x/term).

## Decisions Made
- **Dep anchor required (deviation Rule 3 — blocking fix):** `go mod tidy` removed all three new pins because nothing in source imports them yet (their consumers are Plans 13-02..13-08). This would break the amendment-#58 CI pin literal grep and re-demote `golang.org/x/image` to `// indirect`. Fixed by adding `internal/channels/deps.go` — a blank-import anchor over the exact subpackages the Wave consumers will use (telebot.v4 root, x/image/font/opentype + gofont/gomono, qrterminal/v3), mirroring the project's `internal/cron/tzdata.go` idiom. This is the durable way to satisfy the Task-1 acceptance (pins survive `tidy`).
- **ConsumeOnboarding single-use semantics:** the consume path collapses unknown/already-consumed/expired tokens to `ErrTokenConsumed` (the SQL `consumed_at IS NULL AND expires_at > now()` guards make all three match no row); `PendingConsumed` separately distinguishes a genuinely unknown token via `ErrTokenNotFound` for the setup-status path.
- **CleanupExpired uses `:execrows`** (first use in the codebase) to return the deleted count per the behavior block; sqlc generated it cleanly.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Added internal/channels/deps.go blank-import anchor**
- **Found during:** Task 1 (add deps)
- **Issue:** `go mod tidy` (mandated by the Task-1 action) dropped all three new pins from go.mod because no source file imports them yet — their consumers land in Wave Plans 13-02..13-08. This directly breaks the Task-1 acceptance criteria (the `gopkg.in/telebot.v4 v4.0.0-beta.9` literal grep, the `golang.org/x/image v0.41.0` direct-require promotion) and the amendment-#58 CI pin gate.
- **Fix:** Created `internal/channels/deps.go` blank-importing the exact subpackages the Wave consumers use, then re-ran `go get` + `go mod tidy`. All three pins now persist in the DIRECT require block; x/image is direct (no `// indirect`). Mirrors the existing `internal/cron/tzdata.go` anchor idiom.
- **Files modified:** internal/channels/deps.go, go.mod, go.sum
- **Verification:** `grep 'gopkg.in/telebot.v4 v4.0.0-beta.9' go.mod` ✓; `golang.org/x/image v0.41.0` direct ✓; `github.com/mdp/qrterminal/v3 v3.2.1` ✓; `go build ./...` exit 0.
- **Committed in:** `05eeca32` (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** The anchor is necessary to satisfy the Task-1 acceptance itself (pins surviving `go mod tidy`). It is removed-by-replacement as the real consumers import these packages in later Wave plans. No scope creep — the file is pure dependency-pinning scaffolding with a documented removal path.

## Issues Encountered
- The plan's Task-1 `read_first` claimed `golang.org/x/image` was "already an indirect dep to promote." It was not present in go.mod at all (it had been a v0.0.0 pseudo-version pulled transitively and previously pruned). `go get golang.org/x/image@v0.41.0` added it fresh, and the anchor promotes it to direct. No impact — outcome matches the acceptance (x/image direct at v0.41.0).

## Live Test Delegation
- The `db_integration`-tagged `store_integration_test.go` is the compile-and-assert contract. It **compiles and vets clean** under `go vet -tags db_integration ./internal/channels/telegram/` and `go build -tags db_integration ./...`. Per the execution prompt, the **authoritative live PG round-trip is delegated to the orchestrator's Wave-1 post-build gate** — a local skip here is not a false-green (the env gate `t.Fatal`s under `$CI`, and the orchestrator runs it live against the up Postgres stack).

## Known Stubs
- `internal/setup` is a **test-only package** this plan (only `main_test.go`); `server.go`/`handlers.go` land in Plan 13-07. The goleak harness intentionally exists first so 13-07's SSE-pump tests inherit the leak check. Not goal-blocking — this is Wave-0 substrate scaffolding with an explicit resolving plan.
- `internal/channels/deps.go` is an intentional pinning anchor (not a functional stub); replaced by real imports as Wave consumers land.

## User Setup Required
**External service required for the live integration tiers.** Per the plan's `user_setup`:
- `TELEGRAM_BOT_TOKEN` — Telegram @BotFather `/newbot` HTTP API token (already in `.env` per spike-017). Needed only for the future `telegram_integration` tier (Plan 13-03+), not for this plan's `db_integration` tier.

## Next Phase Readiness
- The data substrate (migration 0012 + telegram.Store) and dependency floor are in place; every later Wave plan now compiles against the pinned deps and the Store seam.
- The three goleak TestMains exist, so 13-03 (bot polling), 13-04 (async convert), and 13-07 (SSE pump) drop tests in and get leak detection for free.
- No blockers. The live `db_integration` round-trip is queued for the orchestrator Wave-1 gate.

## Self-Check: PASSED

All 13 created files verified present on disk; all 3 task commits (`05eeca32`, `3ec353e8`, `1606a077`) verified in git history.

---
*Phase: 13-channels-telegram-multimodal*
*Completed: 2026-06-08*
