---
phase: 13-channels-telegram-multimodal
plan: 07
subsystem: api
tags: [setup-wizard, http, sse, telegram, onboarding, qrterminal, token-gate, goleak]

# Dependency graph
requires:
  - phase: 13-01
    provides: "telegram.Store (InsertPending/PendingConsumed/CountAccounts) + migration 0012 telegram_setup_pending + the internal/setup goleak main_test.go"
  - phase: 13-04
    provides: "config.Config AURA_SETUP_BIND (127.0.0.1:9081) + AURA_SETUP_TOKEN knobs"
  - phase: 12
    provides: "internal/agui/server.go — the loopback-HTTP + SSE Mux/header pattern mirrored here"
provides:
  - "internal/setup: an isolated loopback :9081 HTTP server gating four /setup/* endpoints with a one-time in-memory token"
  - "POST /setup/token — getMe-validate the bot token (BotProbe seam), never logs it"
  - "POST /setup/onboard-link — mint a 1h single-use telegram_setup_pending row + deep_link + terminal ASCII QR (qrterminal); qr_svg empty (OQ4)"
  - "GET /setup/status — bot_configured + account_count"
  - "GET /setup/events — SSE poll-2s of consumed_at → onboarding_completed, goleak-clean pump"
  - "requireSetupToken middleware (constant-time, query+header, invalidated after onboarding)"
affects: [13-09, packaging-17, onboarding-14]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Isolated loopback HTTP subsystem mirroring agui.Server: NewServer(Deps) → Mux (Go-1.22 method-pattern) → HTTPServer(bind) the composition root runs"
    - "One-time in-memory credential: generate-and-print-on-empty, constant-time compare, Invalidate() burns it (no disk)"
    - "SSE pump that selects on r.Context().Done() — client disconnect AND server shutdown both cancel the request ctx (goleak-clean)"
    - "Consumer-side seams (Store interface + BotProbe func) keep the whole package unit-testable with no DB/network/telebot"
    - "Injectable io.Writer sinks (TokenOut/QROut) default os.Stdout, discardable in tests"

key-files:
  created:
    - "internal/setup/server.go — :9081 loopback server + Deps + requireSetupToken gate + Mux + HTTPServer"
    - "internal/setup/token.go — in-memory one-time Token (gen/print/Valid/Invalidate)"
    - "internal/setup/types.go — the request/response + SSE event DTOs"
    - "internal/setup/handlers.go — the four endpoint bodies + SSE pump"
    - "internal/setup/qr.go — the deferred qrSVG stub (OQ4)"
    - "internal/setup/server_test.go — gate + token-print + bind tests"
    - "internal/setup/handlers_test.go — getMe-validate + token-never-logged + onboard-link + status tests"
    - "internal/setup/events_test.go — SSE emit + pump-exits-on-disconnect (goleak-defining)"
  modified:
    - "internal/channels/deps.go — dropped the now-redundant qrterminal blank-import anchor (qr.go's sibling handlers.go is its real consumer)"

key-decisions:
  - "onboarding_completed SSE event keyed purely on consumed_at — the 0012 schema has no token→account link, so telegram_user_id/username stay omitempty rather than adding a migration column (avoids Rule 4)"
  - "qrterminal.Generate is called inline in handleOnboardLink (satisfies the acceptance grep + the plan action verbatim); qr.go holds only the deferred qrSVG stub"
  - "qr_svg returns the empty string this phase (OQ4 / D-03 frontend deferral); the JSON field stays for forward-compat"
  - "TokenOut/QROut injectable io.Writer sinks default os.Stdout — prod path identical, tests pass io.Discard to keep output quiet"

patterns-established:
  - "Isolated loopback HTTP subsystem with a mandatory one-time token gate (mirrors agui but with auth, distinct port :9081)"
  - "SSE poll-DB pump that is provably goleak-clean via httptest + ctx-cancel (the disconnect test never lets the token flip, so only ctx-cancel ends the pump)"

requirements-completed: [UX-03]

# Metrics
duration: 13min
completed: 2026-06-08
---

# Phase 13 Plan 07: Setup Wizard Backend Summary

**An isolated loopback :9081 HTTP/SSE setup API gated by a one-time in-memory AURA_SETUP_TOKEN — getMe-validate the bot token (never logged), mint a 1h single-use onboarding token with a terminal ASCII QR (qr_svg deferred), and stream onboarding_completed over a goleak-clean 2s consumed_at poll.**

## Performance

- **Duration:** 13 min
- **Started:** 2026-06-08T09:34:50Z
- **Completed:** 2026-06-08T09:48:13Z
- **Tasks:** 2 (both TDD `type="auto"`)
- **Files modified:** 9 (8 created in internal/setup + 1 anchor edit in internal/channels)

## Accomplishments
- A separate-port `:9081` loopback HTTP server (`internal/setup`) distinct from the AG-UI `:9080`, mirroring the `agui.Server` Mux/header posture but adding a mandatory auth gate.
- The mandatory `requireSetupToken` middleware on every `/setup/*` route — constant-time compare, `?token=` query OR `X-Aura-Setup-Token` header, **401 without / 200 with / 401 after onboarding** (the one-time token is invalidated when the SSE pump observes a consumed row).
- An in-memory one-time token: an empty `AURA_SETUP_TOKEN` generates a UUIDv4 and prints exactly one parseable `AURA_SETUP_TOKEN=<value>` line to stdout, held in memory only (no disk); an operator-set token is never echoed.
- The four endpoints: `/setup/token` getMe-validates via the `BotProbe` seam (the bot token is **never** logged or echoed — proven by a slog+body log-scan across success AND failure), `/setup/onboard-link` mints a 1h single-use `telegram_setup_pending` row + `{deep_link, qr_svg:""}` + a terminal ASCII QR (`qrterminal.Generate`), `/setup/status` reports `{bot_configured, account_count}`, and `/setup/events` SSE polls `consumed_at` every 2s and emits `onboarding_completed`.
- The SSE pump is provably **goleak-clean**: the package `goleak.VerifyTestMain` harness plus a disconnect test where the token never flips, so the only way the poll goroutine exits is `r.Context().Done()` (client disconnect / server shutdown). Race-clean under `-race -count=10`.

## Task Commits

Each task was committed atomically:

1. **Task 1: server.go + token.go + types.go (isolated :9081 server + one-time token gate)** — `2a2f8935` (feat)
2. **Task 2: handlers.go + qr.go (token getMe / onboard-link / status / events SSE)** — `3f311092` (feat)

_Note: this plan's two TDD tasks each landed as a single feat commit (the greenfield package builds + the test passes in the same step; no separate RED commit was warranted for a net-new file pair where the failing-then-passing cycle was internal to the task)._

## Files Created/Modified
- `internal/setup/server.go` — the `:9081` loopback `http.Server` builder (`HTTPServer`), `Deps`, `NewServer`, Go-1.22 method-pattern `Mux`, and the `requireSetupToken` gate.
- `internal/setup/token.go` — the in-memory one-time `Token`: gen+print on empty, constant-time `Valid`, `Invalidate`.
- `internal/setup/types.go` — `TokenRequest/Response`, `OnboardLinkResponse{deep_link,qr_svg}`, `StatusResponse`, `SetupEvent`.
- `internal/setup/handlers.go` — the four endpoint bodies + the 2s SSE consumed_at pump.
- `internal/setup/qr.go` — the deferred `qrSVG` stub returning `""` (OQ4).
- `internal/setup/{server,handlers,events}_test.go` — 13 unit tests (gate, token-print, getMe-validate, token-never-logged, onboard-link, status, SSE emit, SSE pump-exit-on-disconnect).
- `internal/channels/deps.go` — dropped the now-redundant `qrterminal` blank-import anchor.

## Decisions Made
- **onboarding_completed carries only `type`.** The 0012 `telegram_setup_pending` schema records `consumed_at` but has no column linking a consumed token to the `telegram_user_id` that consumed it (a consumed token shares only the single `local` identity_id). The SSE event is therefore keyed purely on the `consumed_at` signal; `telegram_user_id`/`username` stay `omitempty` and unset. Inventing a `consumed_by` column would be a schema migration = Rule 4 (out of this plan's scope); the completion **signal** (the documented poll) is fully satisfied.
- **qr_svg deferred to the empty string** (OQ4 / D-03 — the HTML frontend is deferred to the next milestone). The JSON field remains for forward-compat; the terminal ASCII QR (`qrterminal`) is the real onboarding path this phase. No `skip2/go-qrcode`/`boombuler/barcode` added.
- **`qrterminal.Generate` inline in `handleOnboardLink`** (not a `qr.go` helper) to satisfy both the acceptance grep (`grep qrterminal internal/setup/handlers.go`) and the plan action verbatim; the QR destination is an injectable `io.Writer` (defaults `os.Stdout`) so tests stay quiet.
- **`local` identity FK.** `/setup/onboard-link` FKs the minted pending row to `Deps.IdentityID` (the `local` seed `00000000-0000-0000-0000-000000000001`, which the composition root resolves in 13-09).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Schema-constraint] onboarding_completed event keyed on consumed_at only (no token→account link)**
- **Found during:** Task 2 (handlers.go / events SSE)
- **Issue:** The plan's SSE event shape is `{type, telegram_user_id, username}`, but the shipped 0012 `telegram_setup_pending` schema has no column mapping a consumed token to the consuming `telegram_user_id`. There is no honest data source for those fields.
- **Fix:** Emit `{type:"onboarding_completed"}` keyed on the `consumed_at` signal (the documented 2s poll); leave `telegram_user_id`/`username` as `omitempty` unset rather than inventing a schema column (that would be a migration = Rule 4). Dropped the speculative `AccountForToken` Store method.
- **Files modified:** internal/setup/server.go (Store seam), internal/setup/handlers.go (pollCompletion)
- **Verification:** `TestEventsSSEEmitsOnboardingCompleted` asserts the frame carries `onboarding_completed` and the stream terminates; documented in the Store interface doc-comment.
- **Committed in:** `3f311092` (Task 2 commit)

**2. [Rule 3 - Blocking/redundant-anchor] Dropped the qrterminal blank-import anchor in internal/channels/deps.go**
- **Found during:** Task 2 (qr.go/handlers.go genuinely import qrterminal)
- **Issue:** `internal/setup/handlers.go` now imports `github.com/mdp/qrterminal/v3` as a real consumer, making the `deps.go` blank-import anchor (added in 13-01 to keep the pin DIRECT before any consumer existed) redundant dead weight.
- **Fix:** Removed the qrterminal blank import (mirrors the 13-03 x/image anchor removal); verified `go mod tidy` produces no go.mod/go.sum diff and qrterminal stays DIRECT at v3.2.1. The telebot anchor is retained as the amendment-#58 CI pin gate's stable grep target.
- **Files modified:** internal/channels/deps.go
- **Verification:** `go mod tidy` no-diff; `go build ./...` green.
- **Committed in:** `3f311092` (Task 2 commit)

---

**Total deviations:** 2 auto-fixed (1 Rule 1 schema-constraint, 1 Rule 3 redundant anchor)
**Impact on plan:** Both necessary for correctness (no inventing schema columns) and hygiene (no dead anchor). No scope creep — the four endpoints, the gate, the SSE pump, and the goleak guarantee all land exactly as specified.

## Known Stubs

| Stub | File:line | Reason |
|------|-----------|--------|
| `qrSVG(deepLink) string { return "" }` | internal/setup/qr.go | **Intentional (OQ4 / D-03).** The HTML frontend is deferred to the next milestone, so the SVG body is not generated this phase; the `qr_svg` JSON field stays for forward-compat returning empty. The terminal ASCII QR (`qrterminal`) is the real onboarding path. Resolved when the setup frontend lands (next milestone). |
| `SetupEvent.{TelegramUserID,Username}` unset (omitempty) | internal/setup/handlers.go pollCompletion | **Intentional (schema constraint, see Deviation 1).** No token→account link in 0012; the completion signal (consumed_at) is what the SSE delivers. A future plan that adds a `consumed_by` column would populate them. |

Neither stub prevents UX-03's goal (a token-gated setup API that issues single-use onboarding tokens + surfaces completion): the deep-link + ASCII QR onboarding path is fully functional, and the completion event fires on consume.

## Issues Encountered
None — both TDD tasks executed cleanly. The parallel Codex session's Phase-15 spike edits (`compose.yaml`, `.planning/spikes/*`) never touched the setup files; every commit was staged with explicit `git add <paths>` and verified with `git show --stat` to contain only this plan's files.

## User Setup Required
None — no external service configuration required this plan. The live `:9081` boot + the `BotProbe`/`Store` composition-root wiring (resolving the `local` identity + the real `telegram.Store` + the telebot getMe probe) land in plan 13-09; the live bot Gate-3 is 13-09.

## Next Phase Readiness
- The setup API is unit-complete and ready for the 13-09 composition-root mount: `NewServer(Deps{Store: telegramStore, Probe: telebotGetMe, Bind: cfg.SetupBind, Token: cfg.SetupToken, IdentityID: localID})` → `HTTPServer(cfg.SetupBind)` run as a fail-soft goroutine alongside the AG-UI server + scheduler (mirrors `serve.go`'s AG-UI mount), with a graceful `Shutdown` on ctx-cancel.
- The `telegram.Store` already satisfies the `setup.Store` seam by method set (InsertPending/PendingConsumed/CountAccounts) — the only adapter the root needs is mapping `setup.InsertPendingParams` ↔ `telegram.InsertPendingParams` (field-identical) and a `tele.NewBot(Settings{Token}).Me.Username` BotProbe.
- Blocker/concern: the live :9081 boot + the getMe BotProbe are not exercised here (no live Telegram in the unit tier) — that is 13-09's mount + Gate-3.

## Self-Check: PASSED

---
*Phase: 13-channels-telegram-multimodal*
*Completed: 2026-06-08*
