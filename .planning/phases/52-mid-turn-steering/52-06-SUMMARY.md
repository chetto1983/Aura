---
phase: 52-mid-turn-steering
plan: 06
subsystem: channels
tags: [telegram, steer, mid-turn, queue, go, concurrency]

# Dependency graph
requires:
  - phase: 52-mid-turn-steering (plan 02)
    provides: "internal/steer.Inbox (Push/Drain/Close, its own sentinels), the conversation-id key"
  - phase: 52-mid-turn-steering (plan 04)
    provides: "cmd/aura's newSteerInbox singleton (chat.steer), the aura.steer echo payload key set, config.AGUISteer caps"
  - phase: 52-mid-turn-steering (plan 05)
    provides: "the leftover-auto-delivery chain-cap pattern (steerAutoDeliverMaxChain=1) and the byte-stable notice-line pattern this plan's media queue mirrors"
provides:
  - "telegram.Deps.Steer (*steer.Inbox) wired from cmd/aura's chat.steer singleton — the SAME inbox instance the cockpit route drains now also serves Telegram"
  - "bot_dispatch_steer.go: the plain-text busy-turn redirect (steerBusyTurn), rendering internal/steer's own Push sentinels, no re-validation"
  - "bot_dispatch_queue.go: the per-chat pending-turn slot D-05 required (which had no shared-seam equivalent) — enqueueBusyTurn / takePendingTurns / deliverPendingTurn, capped at mediaQueueMaxPerChat=4 messages and mediaQueueMaxChain=1 delivery hop"
  - "cmd/aura/steer_wiring_test.go: the one-inbox-two-consumers pointer-identity proof and the AURA_AGUI_RUN_STEER=false rollback proof"
affects: [52-07, 52-08]

actuals:
  tokens: 15600
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Busy-turn routing predicate lives in a NEW sibling file (bot_dispatch_steer.go / bot_dispatch_queue.go), never in bot_dispatch_turn.go itself — the call site (runTurnWithAssets/onBusyRedirect) stays a thin dispatcher, the classification logic stays in the sibling."
    - "Raw-text-before-composition capture: rawText is snapshotted BEFORE composeTurnContext reassigns the local text variable, so a steer carries the operator's words and never the attachment block/knowledge catalog (T-52-33) — the queued MEDIA turn deliberately takes the opposite path (it stores the ALREADY-COMPOSED text, because a queued message IS a fresh turn, not a steer joining one)."
    - "Delivery-before-unregister: the queued turn's delivery call sits between handleTurn and the function's return, so it runs strictly before the deferred unregisterTurn fires — the SAME per-chat registration and the SAME scoped turnCtx a live turn used, with no second registerTurn call needed."
    - "Chain cap via `for range N`: mediaQueueMaxChain=1 bounds delivery to exactly one hop per startTurn goroutine, mirroring runner_steer.go's steerAutoDeliverMaxChain — a message queued during the delivered turn is left queued for the NEXT turn's own delivery check, never chased recursively."
    - "Gated turnDriver test double: each driver invocation blocks on its OWN per-call channel (not a single shared one), because the media queue's delivered turn re-invokes the SAME driver from WITHIN the same goroutine — a single shared release channel could not distinguish which hop is currently live, which the chain-cap test needs to control precisely."

key-files:
  created:
    - internal/channels/telegram/bot_dispatch_steer.go
    - internal/channels/telegram/bot_dispatch_steer_test.go
    - internal/channels/telegram/bot_dispatch_queue.go
    - internal/channels/telegram/bot_dispatch_queue_test.go
    - cmd/aura/steer_wiring_test.go
  modified:
    - internal/channels/telegram/bot.go
    - internal/channels/telegram/bot_dispatch_turn.go
    - cmd/aura/serve_channels.go

key-decisions:
  - "telegram.Deps.Steer is typed *steer.Inbox (the concrete pointer), never an interface — matching agui.Server.SetSteerInbox's own signature. This sidesteps the classic Go nil-in-interface trap runner.go's steerInboxOrNil exists to fix: an interface-typed field would turn a nil *steer.Inbox into a non-nil interface, silently breaking the `t.deps.Steer == nil` rollback check."
  - "The media queue combines N pending messages into ONE follow-on turn (joinQueuedTurns, FIFO-joined by a blank line), never N separate turns — mirroring runner_steer.go's joinSteerLeftovers for the analogous steer case. mediaQueueMaxPerChat=4 is an explicit, small, named cap (not measured/tuned — a T-52-36 DoS mitigation, same spirit as the AGUISteer.Max=8/MaxBytes=16384 caps 52-04 documented as untested placeholders)."
  - "onBusyRedirect (bot_dispatch_turn.go) is the single classification point evolving across Task 1 (steer-vs-busy) and Task 2 (steer-vs-queue-vs-busy) — the routing logic itself never moved into bot_dispatch_steer.go/bot_dispatch_queue.go, which stay pure 'push and render a reply' wrappers. This differs slightly from the plan's literal phrasing ('put the routing logic in bot_dispatch_steer.go') because Task 2 needed the SAME predicate to grow a third arm without bot_dispatch_steer.go depending on bot_dispatch_queue.go's enqueueBusyTurn or vice versa — keeping the decision in bot_dispatch_turn.go (the call site) avoids a cross-file dependency between the two sibling files."
  - "TestOneInboxServesBothSurfaces cannot read agui.Server's unexported steer field back (no getter exists, by design — the daemon never reads its own wiring). The test instead executes buildTelegramDeps verbatim (the real Telegram-side production call) and mirrors serve_agui.go's own one-line SetSteerInbox gate over the SAME chat.steer field, with a negative subtest proving two independently-constructed *steer.Inbox values are never pointer-equal (so the positive assertion is a genuine discriminating check, not a tautology). This is lighter than replaying 52-04/52-05's full HTTP e2e infra from cmd/aura, which would have required implementing agui.Runner's 7-method interface and a detached-run HTTP round-trip purely to prove a pointer assignment."

patterns-established:
  - "A channel-lifecycle concern with no shared-seam equivalent (the pending-turn slot) gets its OWN mutex, never bolted onto an existing map whose lifetime differs (cancels is cleared the instant a turn ends; pendingTurns must survive exactly that moment)."

requirements-completed: [STEER-05]

coverage:
  - id: D1
    description: "A plain-text Telegram message arriving during a live turn pushes the operator's RAW text onto the shared steer inbox under convID(chatID) and echoes turnSteeredMessage, replacing today's busy reply"
    requirement: STEER-05
    verification:
      - kind: unit
        ref: "internal/channels/telegram/bot_dispatch_steer_test.go#TestPlainTextDuringLiveTurnSteers"
        status: pass
      - kind: unit
        ref: "internal/channels/telegram/bot_dispatch_steer_test.go#TestSteerCarriesRawTextNotComposedContext"
        status: pass
      - kind: unit
        ref: "internal/channels/telegram/bot_dispatch_steer_test.go#TestTelegramConvIDIsTheInboxKey"
        status: pass
    human_judgment: false
  - id: D2
    description: "An ask_user-paused run's continuation (HITL resume) is terminal, not steerable — the resume path keeps sendBusy's byte-identical turnBusyMessage even with a wired steer inbox, never pushing"
    requirement: STEER-05
    verification:
      - kind: unit
        ref: "internal/channels/telegram/bot_dispatch_steer_test.go#TestHitlResumeIsNotASteer"
        status: pass
    human_judgment: false
  - id: D3
    description: "With Steer unwired (AURA_AGUI_RUN_STEER=false), Telegram degrades to today's turnBusyMessage for both plain text and media — no panic, no half-live branch"
    requirement: STEER-05
    verification:
      - kind: unit
        ref: "internal/channels/telegram/bot_dispatch_steer_test.go#TestNilSteerInboxDegradesToBusy"
        status: pass
      - kind: unit
        ref: "internal/channels/telegram/bot_dispatch_queue_test.go#TestMediaQueueNilSteerDegradesToBusy"
        status: pass
    human_judgment: false
  - id: D4
    description: "A non-text message (photo/voice/document) arriving during a live turn is QUEUED (turnQueuedForNextTurnMessage) and DELIVERED as its own turn once the live turn ends, under the same registration/ctx, exactly once — proven by counting turn invocations, never merely by observing a reply, so the proof cannot pass against today's silent drop"
    requirement: STEER-05
    verification:
      - kind: unit
        ref: "internal/channels/telegram/bot_dispatch_queue_test.go#TestNonTextDuringLiveTurnIsQueuedNotDropped"
        status: pass
      - kind: unit
        ref: "internal/channels/telegram/bot_dispatch_queue_test.go#TestQueuedTurnDeliveredAfterLiveTurnEnds"
        status: pass
    human_judgment: false
  - id: D5
    description: "If the live turn is cancelled before delivery, the queued message is NOT delivered silently: the pending slot is cleared and the operator is told it did not run (turnQueuedNotDeliveredMessage) — a /cancel never swallows a message the bot said it had accepted"
    requirement: STEER-05
    verification:
      - kind: unit
        ref: "internal/channels/telegram/bot_dispatch_queue_test.go#TestQueuedTurnNotDeliveredOnCancelIsAnnounced"
        status: pass
    human_judgment: false
  - id: D6
    description: "The media-queue delivery chain is bounded at exactly one hop per startTurn goroutine (mediaQueueMaxChain=1) — a message queued during the delivered turn queues again for the NEXT turn rather than being chased recursively"
    requirement: STEER-05
    verification:
      - kind: unit
        ref: "internal/channels/telegram/bot_dispatch_queue_test.go#TestQueueChainIsBounded"
        status: pass
    human_judgment: false
  - id: D7
    description: "Stop drains cleanly (goleak-clean, no panic) even with a chat's pending slot still holding an undelivered media message"
    requirement: STEER-05
    verification:
      - kind: unit
        ref: "internal/channels/telegram/bot_dispatch_queue_test.go#TestStopIsGoleakCleanWithOutstandingPendingSlot"
        status: pass
    human_judgment: false
  - id: D8
    description: "One process-wide *steer.Inbox instance serves BOTH the cockpit route (agui.Server.SetSteerInbox) and Telegram (telegram.Deps.Steer), pinned by pointer identity with a negative subtest proving the check is discriminating; the AURA_AGUI_RUN_STEER=false rollback darkens both surfaces together, never one alone"
    requirement: STEER-05
    verification:
      - kind: unit
        ref: "cmd/aura/steer_wiring_test.go#TestOneInboxServesBothSurfaces"
        status: pass
      - kind: unit
        ref: "cmd/aura/steer_wiring_test.go#TestSteerRollbackDarkensBothSurfaces"
        status: pass
    human_judgment: false

duration: ~53 min (approximate — measured from the prior plan's closing commit cadbf2fb5 at 21:19:07+02:00 to this plan's final task commit 9a9714b1d at 22:07:03+02:00; does not include SUMMARY authoring time)
completed: 2026-08-25
status: complete
---

# Phase 52 Plan 06: Mid-turn steering — Telegram channel parity Summary

**A plain-text Telegram message during a live turn now steers the running turn instead of replying busy, and a photo/voice/document during a live turn is held in a new per-chat pending slot and delivered as its own turn when the live turn ends — closing D-05, which today drops it silently (`startTurn` calls `onBusy()` and returns, with no queue anywhere on that path).**

## Performance

- **Duration:** ~53 min (see frontmatter `duration` for the exact measurement window and caveat)
- **Completed:** 2026-08-25
- **Tasks:** 3/3
- **Files modified:** 8 (5 created, 3 modified)

## Accomplishments

- `telegram.Deps.Steer` (`*steer.Inbox`) is wired from `cmd/aura`'s `chat.steer` singleton in `buildTelegramDeps` — the SAME instance the cockpit route drains, closing T-52-31 for the channel side.
- Plain text arriving on a busy chat pushes the operator's RAW text (captured before `composeTurnContext` reassigns it) onto the inbox under `convID(chatID)` and echoes `turnSteeredMessage`, replacing today's busy copy — D-03/D-04.
- A photo, voice note or document arriving on a busy chat is stored in a NEW per-chat pending slot (`bot_dispatch_queue.go`, its own `pendingMu`, never the `cancels` map) and the operator is told `turnQueuedForNextTurnMessage` — D-05, fixing the measured defect that today's `startTurn` drops it with no queue at all.
- The queued turn is delivered inside `startTurn`'s own goroutine, immediately after `handleTurn` returns and strictly before the deferred `unregisterTurn` fires — so it runs under the exact same per-chat registration and the exact same scoped `turnCtx` the live turn used, with no second `registerTurn` call.
- A cancelled live turn (`/cancel`) does NOT deliver the pending message: the slot is cleared and `turnQueuedNotDeliveredMessage` is sent, so a cancel never silently swallows a message the bot said it had accepted (T-52-35).
- `mediaQueueMaxChain = 1` bounds delivery to one follow-on turn per goroutine (mirroring `runner_steer.go`'s `steerAutoDeliverMaxChain`); `mediaQueueMaxPerChat = 4` bounds how many messages one chat may hold, past which the operator is told the message was refused rather than dropped or queued unbounded (T-52-36).
- `bot_dispatch_hitl.go` and `commands.go` remain byte-identical across all three commits — an `ask_user`-paused continuation stays terminal, not steerable (#132 item 7), asserted per-commit via the HEAD-scoped `git show --name-only` criterion (never `git diff --quiet`, which cannot fail post-commit).
- `cmd/aura/steer_wiring_test.go` pins the one-inbox-two-consumers invariant (pointer identity, with a negative subtest proving the comparison discriminates) and the `AURA_AGUI_RUN_STEER=false` rollback darkening both surfaces together.

## Task Commits

1. **Task 1: Route a plain-text message on a busy chat to the inbox and echo the redirect** - `68dd40585` (feat)
2. **Task 2: Build the queue D-05 requires** - `bab6a478b` (feat)
3. **Task 3: One inbox, two consumers — prove the composition root wires the same singleton to both surfaces** - `9a9714b1d` (test)

**Plan metadata:** pending — this SUMMARY commit follows, per the executor's `git_commit_metadata` step.

_Note: each task's `<action>` specified a single named commit rather than a separate RED/GREEN pair; tests were written and run to failing-then-passing before each commit, matching the shape 52-04/52-05 already established in this same phase._

## Files Created/Modified

- `internal/channels/telegram/bot_dispatch_steer.go` (new, 39 lines) - `turnSteeredMessage`, `turnSteerRefusedMessage`, `steerBusyTurn` (Push + render, no validation of its own)
- `internal/channels/telegram/bot_dispatch_steer_test.go` (new, 259 lines) - the Task 1 test suite plus `blockingTurnDriver`/`composedTextAssets`/`driveBusyScenario` fixtures Task 2 reuses
- `internal/channels/telegram/bot_dispatch_queue.go` (new, 141 lines) - `pendingTurn`, `enqueueBusyTurn`, `takePendingTurns`, `deliverPendingTurn`, `joinQueuedTurns`, `sendPlain`, the three queue message constants, `mediaQueueMaxChain`, `mediaQueueMaxPerChat`
- `internal/channels/telegram/bot_dispatch_queue_test.go` (new, 308 lines) - the Task 2 test suite plus the `gatedTurnDriver`/`gatedCall` fixture the chain-cap test needs
- `internal/channels/telegram/bot.go` (379 → 401 lines) - `Steer *steer.Inbox` field (Task 1), `pendingMu`/`pendingTurns` fields (Task 2)
- `internal/channels/telegram/bot_dispatch_turn.go` (151 → 181 lines) - raw-text capture, `onBusyRedirect` (grown across Task 1 and Task 2), the `deliverPendingTurn` call site in `startTurn`'s goroutine
- `cmd/aura/serve_channels.go` (+1 line) - `Steer: chat.steer` in `buildTelegramDeps`
- `cmd/aura/steer_wiring_test.go` (new, 80 lines) - `TestOneInboxServesBothSurfaces`, `TestSteerRollbackDarkensBothSurfaces`

## Decisions Made

See `key-decisions` in the frontmatter for: `Deps.Steer`'s concrete-pointer typing (avoiding the nil-interface trap), the media queue's FIFO-join-into-one-turn design (mirroring `joinSteerLeftovers`), the single-classification-point placement of `onBusyRedirect` (a deliberate small deviation from the plan's literal "put the routing logic in bot_dispatch_steer.go" phrasing, explained below), and `TestOneInboxServesBothSurfaces`'s lighter-than-e2e proof strategy.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Lint] Misspelling false-positive in the Italian redirect-echo string**
- **Found during:** Task 1, first commit attempt
- **Issue:** `golangci-lint`'s `misspell` linter flagged "applicato" (Italian, correct) as a misspelling of the English word "application" in both `turnSteeredMessage` and `turnSteerRefusedMessage`, blocking the commit.
- **Fix:** Reworded both constants to avoid the `applic*` root entirely (`turnSteeredMessage` now reads "Ho girato la tua correzione..."; `turnSteerRefusedMessage` now reads "...a inoltrare la correzione..."). No behavior change; wording is Claude's Discretion per the plan.
- **Files modified:** `internal/channels/telegram/bot_dispatch_steer.go`
- **Verification:** `golangci-lint` (via the pre-commit hook) reports 0 issues; the 5 Task 1 tests still pass unchanged (they assert against the `turnSteeredMessage` symbol, never a hardcoded literal).
- **Committed in:** `68dd40585`

---

**Total deviations:** 1 auto-fixed (Rule 1, a linter false-positive on correct Italian text).
**Impact on plan:** None on scope or correctness — a wording-only fix required to pass the pre-commit lint gate. No scope creep.

### Design note (not a Rule 1-4 deviation, disclosed per CLAUDE.md's NEVER SUPPOSE)

The plan's Task 1 `<action>` says "Put the routing logic in a NEW sibling file `bot_dispatch_steer.go`." The text-vs-attachment CLASSIFICATION (which of the three arms — steer / queue / busy — a busy-turn message takes) was kept in `bot_dispatch_turn.go`'s `onBusyRedirect`, grown incrementally across Task 1 and Task 2, rather than moved into `bot_dispatch_steer.go`. `bot_dispatch_steer.go` and `bot_dispatch_queue.go` each contain only their own "push/enqueue and render a reply" logic — the two files never import or call each other. This keeps the plan's own `<no_redundancy>` invariant (the two sibling files touch nothing outside their own concern) while avoiding a cross-file dependency Task 2 would otherwise have needed (either `bot_dispatch_steer.go` depending on Task 2's not-yet-written `enqueueBusyTurn`, or vice versa). The plan's own text anticipates exactly this shape one paragraph later: "Build the `onBusy` closure at the `runTurnWithAssets` call site from three inputs... (The attachments arm is Task 2's; until Task 2 lands, that arm keeps today's `turnBusyMessage`.)" — which places the closure-building at the call site, matching what was implemented.

## Issues Encountered

None beyond the deviation above — no blockers requiring a stop-and-ask, no architectural changes, no authentication gates. No concurrent unrelated commits landed on `master` during this plan's execution (verified via `git log` immediately before each commit).

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- The exact load-bearing strings 52-08's live Telegram E2E will assert:
  - `turnSteeredMessage` = `"↩️ Ho girato la tua correzione al turno in corso: la userò dal prossimo passaggio."`
  - `turnQueuedForNextTurnMessage` = `"📎 Ho ricevuto l'allegato: lo elaborerò appena finisce la richiesta in corso."`
  - `turnQueuedNotDeliveredMessage` = `"⚠️ La richiesta in corso è stata annullata: l'allegato che avevi inviato NON è stato elaborato, rimandalo pure."`
  - (also present, not named in the plan's output spec but load-bearing for the cap/refusal paths: `turnSteerRefusedMessage` and `turnQueueFullMessage`, and the two named caps `mediaQueueMaxChain = 1` / `mediaQueueMaxPerChat = 4`.)
- `mediaQueueMaxPerChat = 4` is an explicit, deliberately small, untested placeholder cap (same spirit as 52-04's documented `AGUISteer.Max=8`/`MaxBytes=16384`) — a future live-traffic measurement may want to tune it, but the mechanism (a named constant with a distinct refusal reply past it) is what T-52-36 required, not a specific number.
- All touched files have ample LOC headroom for 52-07/52-08: `bot_dispatch_turn.go` 181/600, `bot.go` 401/600, `bot_dispatch_queue.go` 141/600, `bot_dispatch_steer.go` 39/600, `serve_channels.go` 385/600.
- STEER-05 is declared only by this plan in the phase's requirements (no shared-ID gate wait needed).

## Self-Check: PASSED

- FOUND: internal/channels/telegram/bot_dispatch_steer.go
- FOUND: internal/channels/telegram/bot_dispatch_steer_test.go
- FOUND: internal/channels/telegram/bot_dispatch_queue.go
- FOUND: internal/channels/telegram/bot_dispatch_queue_test.go
- FOUND: cmd/aura/steer_wiring_test.go
- FOUND: commit 68dd40585 (git log --oneline --all)
- FOUND: commit bab6a478b (git log --oneline --all)
- FOUND: commit 9a9714b1d (git log --oneline --all)
- `go build ./...`, `go vet ./...` — PASS (Windows host)
- `go test ./internal/channels/telegram/ ./cmd/aura/` — PASS (Windows host, no -race: CGO unavailable)
- `go test -race -count=1 ./internal/channels/telegram/... ./cmd/aura/ ./internal/steer/` — PASS (WSL, canonical race environment)
- HEAD-scoped `git show --name-only --format= HEAD -- internal/channels/telegram/bot_dispatch_hitl.go internal/channels/telegram/commands.go` — empty (untouched) on all three commits
- `wc -l` on every touched file — all ≤ 600 (see Next Phase Readiness for exact figures)
- All 11 named plan tests (`TestPlainTextDuringLiveTurnSteers`, `TestSteerCarriesRawTextNotComposedContext`, `TestHitlResumeIsNotASteer`, `TestNilSteerInboxDegradesToBusy`, `TestTelegramConvIDIsTheInboxKey`, `TestNonTextDuringLiveTurnIsQueuedNotDropped`, `TestQueuedTurnDeliveredAfterLiveTurnEnds`, `TestQueuedTurnNotDeliveredOnCancelIsAnnounced`, `TestQueueChainIsBounded`, `TestOneInboxServesBothSurfaces`, `TestSteerRollbackDarkensBothSurfaces`) plus 3 additional tests the acceptance criteria required by prose (`TestMediaQueueNilSteerDegradesToBusy`, `TestStopIsGoleakCleanWithOutstandingPendingSlot`) — all PASS.

---
*Phase: 52-mid-turn-steering*
*Completed: 2026-08-25*
