---
phase: 20-scheduler-hardening-full-implementation
plan: 01
subsystem: channels
tags: [scheduler, delivery, telegram, identity-routing, deliverer, fan-out, pgx]

# Dependency graph
requires:
  - phase: 13-telegram
    provides: "Telegram channel (channels.Channel), Store over telegram_accounts, GetTelegramAccountByIdentity sqlc query, botSender seam, tele.ChatID Recipient"
  - phase: 10-scheduler
    provides: "channels.Registry started-map fan-out target, cron.Task carries IdentityID/OriginConversationID"
provides:
  - "channels.Deliverer optional-capability interface with the load-bearing tri-state contract ((false,nil)=try-next / (true,nil)=stop-delivered / (false,err)=owns-but-failed-stop)"
  - "Registry.DeliverToIdentity deterministic sorted-by-name fan-out over started Deliverer channels"
  - "telegram.Store.GetAccountByIdentity (wrapper over the existing sqlc query; non-UUID 'local' → wrapped pgx.ErrNoRows)"
  - "telegram.Telegram.Deliver (identity→chat resolution + bot.Send under t.mu) satisfying channels.Deliverer"
affects: [20-02, 20-03, 20-04, scheduler-dispatch, reminder-agnostic-channel]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Optional-capability interface (Deliverer) sibling to a lifecycle interface (Channel) — runtime-asserted ch.(Deliverer), zero registry change for a new channel"
    - "Deterministic sorted-by-name fan-out (sort.Strings over a snapshot, never Go map iteration order — Fork 4 / D-05)"
    - "Snapshot-under-lock then iterate unlocked (Deliver can block on network — mirrors StopAll)"
    - "Consumer-declared accountResolver seam + unexported test-override fields (deliverBot/deliverResolver) for DB-free, API-free unit tests"
    - "parseUUID failure mapped to wrapped pgx.ErrNoRows so a single errors.Is branch means not-my-user for both no-account AND non-UUID 'local' (Pitfall 6)"

key-files:
  created:
    - internal/channels/deliver.go
    - internal/channels/telegram/deliver.go
    - internal/channels/telegram/deliver_test.go
  modified:
    - internal/channels/registry.go
    - internal/channels/registry_test.go
    - internal/channels/telegram/store.go
    - internal/channels/telegram/bot.go

key-decisions:
  - "Deterministic order mechanism = sort-by-name (sort.Strings over a snapshot of r.started), NOT an explicit Priority() interface method — lower LOC, zero new interface method (RESEARCH OQ2 / Claude's Discretion). With one Deliverer the order is unobservable; the sort is the regression guard the moment a 2nd Deliverer lands."
  - "Telegram.Deliver testability via two unexported Telegram-struct fields (deliverBot botSender, deliverResolver accountResolver) mirroring the existing consumerFactory unexported-seam precedent — production reads the live *tele.Bot under t.mu and t.deps.Store; tests inject a recording sendRecorder + a stubResolver with no live API/DB."
  - "accountResolver declared consumer-side in telegram/deliver.go (the *Store satisfies it) so Deliver is unit-testable without a Store/pool — matches the package's commands.go/onboarding.go consumer-declared-interface idiom."

patterns-established:
  - "Deliverer tri-state contract: (false,err) owns-but-failed must STOP the fan-out, never fall through to a sibling (double-delivery guard) — the contract 20-03 dispatch precedence depends on"

requirements-completed: [R2, R3]

# Metrics
duration: 11 min
completed: 2026-06-11
---

# Phase 20 Plan 01: Identity-Keyed Delivery Seam Summary

**channels.Deliverer optional-capability interface + deterministic sorted-by-name Registry.DeliverToIdentity fan-out + Telegram.Deliver (identity→telegram_user_id→bot.Send) with the tri-state contract, all behind 10 unit cases under -race.**

## Performance

- **Duration:** ~11 min
- **Started:** 2026-06-11T15:43Z
- **Completed:** 2026-06-11T15:54Z
- **Tasks:** 2 (both TDD)
- **Files created:** 3 | **Files modified:** 4

## Accomplishments

- `channels.Deliverer` optional-capability interface (new `internal/channels/deliver.go`) carrying the verbatim tri-state doc contract that 20-03's dispatch precedence and the registry fan-out both depend on.
- `Registry.DeliverToIdentity` — snapshots `r.started` under `r.mu`, sorts names with `sort.Strings` (deterministic, never map order), runs `Deliver` unlocked, returns on first-delivers-wins / owns-but-fails; a started channel that is not a `Deliverer` is silently skipped.
- `telegram.Store.GetAccountByIdentity` — thin wrapper over the EXISTING `GetTelegramAccountByIdentity` sqlc query (no new SQL), reusing the existing `parseUUID` helper; a non-UUID identity (`'local'`) maps to wrapped `pgx.ErrNoRows`.
- `telegram.Telegram.Deliver` — satisfies `channels.Deliverer` (compile assertion `var _ channels.Deliverer = (*Telegram)(nil)`), reads the live bot under `t.mu` (nil-bot short-circuit), resolves identity→account, sends to `tele.ChatID(acct.TelegramUserID)`.
- 10 unit cases (5 registry fan-out + 5 telegram deliver, plus a Store-boundary test) green under `-race`; `go vet` clean; `golangci-lint run ./internal/channels/...` 0 issues; `go build ./...` succeeds; all files ≤600 LOC.

## Task Commits

Each task was committed atomically:

1. **Task 1: channels.Deliverer + Registry.DeliverToIdentity** - `55887022` (feat) — test+impl combined (the project pre-commit `vet` hook rejects a non-compiling RED-only commit; RED was confirmed via the failing-to-compile test run before the impl landed).
2. **Task 2: Telegram.Deliver + Store.GetAccountByIdentity** - `d8e01844` (feat) — test+impl combined (same hook constraint).

**Plan metadata:** committed with this SUMMARY (`docs(20-01): complete identity-keyed delivery seam plan`).

## Files Created/Modified

- `internal/channels/deliver.go` (new, 21 LOC) — `Deliverer` interface + tri-state doc contract.
- `internal/channels/registry.go` (+`sort` import, +`DeliverToIdentity`) — deterministic sorted fan-out.
- `internal/channels/registry_test.go` (+`fakeDeliverer`, +`TestRegistryDeliverToIdentity`) — 5 sub-cases.
- `internal/channels/telegram/store.go` (+`GetAccountByIdentity`) — wrapper over the existing sqlc query.
- `internal/channels/telegram/deliver.go` (new, 86 LOC) — `Telegram.Deliver`, `accountResolver` seam, compile assertion.
- `internal/channels/telegram/deliver_test.go` (new) — `sendRecorder` + `stubResolver` doubles, `TestDeliver` (5 cases) + `TestGetAccountByIdentityLocalMapsToNotFound`.
- `internal/channels/telegram/bot.go` (+`deliverBot`/`deliverResolver` unexported test-seam fields on `*Telegram`).

## Test Symbols Added (20-03 source-grounding pass: EXCLUDE these — newly created here)

- Interface/method: `channels.Deliverer`, `(*channels.Registry).DeliverToIdentity`, `(*telegram.Store).GetAccountByIdentity`, `(*telegram.Telegram).Deliver`, `telegram.accountResolver` (unexported seam), `var _ channels.Deliverer = (*telegram.Telegram)(nil)`.
- Struct fields: `telegram.Telegram.deliverBot`, `telegram.Telegram.deliverResolver` (unexported test seams).
- Test symbols: `channels.fakeDeliverer`, `channels.TestRegistryDeliverToIdentity`; `telegram.sendRecorder`, `telegram.recordedSend`, `telegram.stubResolver`, `telegram.TestDeliver`, `telegram.TestGetAccountByIdentityLocalMapsToNotFound`.

## Decisions Made

- **Deterministic order = sort-by-name**, not a `Priority()` method (RESEARCH OQ2). Lowest LOC, zero new interface surface; the sort is the testable regression guard for when a 2nd Deliverer channel lands.
- **`Deliver` test seam = two unexported `*Telegram` fields** (`deliverBot botSender`, `deliverResolver accountResolver`) mirroring the existing `consumerFactory` unexported-Deps precedent. Production reads the live `*tele.Bot` under `t.mu` + `t.deps.Store`; tests inject a `sendRecorder` + `stubResolver`. This keeps `Deliver` reading the live bot under the lock (Pitfall 4) while remaining DB/API-free in tests.
- **`accountResolver` declared consumer-side** in `telegram/deliver.go` (`*Store` satisfies it) so the deliver branches are unit-testable without a pool — consistent with the package's `commands.go`/`onboarding.go` consumer-declared-interface idiom.

## Deviations from Plan

None - plan executed exactly as written.

The plan anticipated the test seam ("inject a `docBot`-style recorder + a fake/stub `*Store`") and Claude's Discretion covered the precise mechanism (unexported struct fields vs a new Deps field); the chosen `deliverBot`/`deliverResolver` fields are the minimal idiomatic realization and not a scope change.

## Known Stubs

None — both methods are fully wired to their real backends in production (`Registry.DeliverToIdentity` over real started channels; `Telegram.Deliver` over the live `*tele.Bot` + `*Store`). The `deliverBot`/`deliverResolver` fields are nil on the production path (test-only overrides). This plan is the leaf of the dependency graph; the dispatch wiring that consumes `DeliverToIdentity` is created by 20-03 (named, not expected yet).

## Threat Flags

None — the only security-relevant surface (identity→chat resolution in `Telegram.Deliver`) is exactly the surface the plan's `<threat_model>` enumerated (T-20-01/T-20-02/T-20-03), all mitigated and pinned by tests: ErrNoRows→(false,nil) delivers to nobody (T-20-01), non-UUID 'local'→(false,nil) no stray send (T-20-02), (false,err) stops fan-out with no sibling attempt (T-20-03). No new endpoint, auth path, or schema change was introduced (no new SQL query; SQL files unchanged).

## Issues Encountered

- A stale `.git/index.lock` from a prior interrupted process blocked the first Task 2 commit attempt. Verified no live git process was running (a clean `git status` succeeded), removed the stale lock, re-staged only the Task 2 files (excluding the unrelated `.planning/STATE.md` modification), and committed cleanly.
- The project pre-commit `vet` hook rejects a non-compiling RED-only commit (a `test(...)` commit referencing an undefined method fails `go vet`). RED was still established (the failing-to-compile test run before the impl), and test+impl were committed together per the TDD protocol's "TDD tasks may have multiple commits / combined commit" allowance.

## Next Phase Readiness

- The identity-keyed delivery seam is complete and is the leaf of the Phase 20 dependency graph. 20-03 can now wire `cron.DispatchDeps.ChannelDeliverer` to `*channels.Registry` (which satisfies the `DeliverToIdentity(ctx, identityID, text) (bool, error)` shape) and route `deliverToOrigin` through it, honoring the tri-state contract documented on `channels.Deliverer`.
- No blockers. No new env vars, no migration, no new dependency in this plan.

## TDD Gate Compliance

Both tasks are `tdd="true"`. RED was confirmed (the registry test failed to compile — `DeliverToIdentity undefined` — before the implementation; the telegram deliver branches were authored against the not-yet-implemented method). The project's pre-commit `vet` hook forbids a standalone non-compiling `test(...)` commit, so each task's RED test and GREEN implementation were committed together as one `feat(...)` commit (the TDD reference explicitly allows combined commits when a non-compiling intermediate cannot be committed). No `feat` landed without its test in the same commit.

## Self-Check: PASSED

- Created files exist on disk: `internal/channels/deliver.go`, `internal/channels/telegram/deliver.go`, `internal/channels/telegram/deliver_test.go`, `20-01-SUMMARY.md` — all FOUND.
- Task commits exist: `55887022` (Task 1), `d8e01844` (Task 2) — both FOUND.
- Acceptance: `TestRegistryDeliverToIdentity` (5/5) + `TestDeliver` (5/5) green under `-race`; `go vet ./internal/channels/...` clean; `golangci-lint run ./internal/channels/...` 0 issues; `go build ./...` succeeds; all files ≤600 LOC.

---
*Phase: 20-scheduler-hardening-full-implementation*
*Completed: 2026-06-11*
