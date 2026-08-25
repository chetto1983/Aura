---
phase: 52-mid-turn-steering
plan: 05
subsystem: agent-runtime
tags: [steer, runner, agui, sse, wire-protocol, go, concurrency]

# Dependency graph
requires:
  - phase: 52-mid-turn-steering (plan 02)
    provides: "internal/steer.Inbox, agent.SteerInbox interface, drainSteer, Actions.SteerDelta wire shape"
  - phase: 52-mid-turn-steering (plan 04)
    provides: "POST /agent/runs/{runID}/steer route, SteerEventName = \"aura.steer\" CUSTOM frame, persistSteerTurn drain-time persistence keyed on delivery form (already reserved and skipped the auto_delivery_next_turn value this plan fills in)"
provides:
  - "(*Runner).deliverLeftoverSteer: bounded (steerAutoDeliverMaxChain=1) leftover-steer auto-delivery wrap around every turn's iterator"
  - "steerAutoDeliveryNotice: the byte-stable visible line an auto-delivered leftover carries"
  - "410-Gone terminal-run branch in handleRunSteer, sharing one writeSteerGone call site with the inbox-closed sentinel"
  - "TestSteerHasNoFourthOutcome: FA-3's closure, a table proving exactly three outcomes for a steer POST"
affects: [52-06, 52-07, 52-08]

actuals:
  tokens: 0
  tasks: 2
  commits: 2

tech-stack:
  added: []
  patterns:
    - "Sibling-file wrap, one-line call site: runTurn gained exactly one line (a call to deliverLeftoverSteer); all new logic lives in runner_steer.go, not runner.go — the same discipline runner_reasoning.go already established."
    - "Re-entrant turn under an already-held lock: the follow-on turn calls turnLocked directly (never Turn/runTurn), reusing the SAME lock hold and the SAME scoped ctx the finished turn ran on, rather than re-acquiring (which would deadlock) or deriving a fresh unscoped context."
    - "One aura.steer wire branch for every delivery form: the auto-delivery reuses 52-02/52-04's exact SteerDelta payload keys and translator.go's existing emission branch — no second branch, verified structurally via a git-scoped criterion, not by inspection."
    - "Structural bound over probabilistic one: steerAutoDeliverMaxChain is a named constant (=1), making a second auto-delivery hop within one Turn call impossible by construction rather than merely unlikely."
    - "Row-count proof over status-code proof for a repudiation threat: TestLeftoverSteerPersistsExactlyOneTurn counts aura.conversation_turns rows rather than asserting existence, because a status code or an existence check both pass on the double-write T-52-26 warns against."

key-files:
  created:
    - internal/runner/runner_deps.go
    - internal/runner/runner_steer_leftover_test.go
  modified:
    - internal/runner/runner.go
    - internal/runner/runner_steer.go
    - internal/agui/server_run_steer.go
    - internal/agui/server_run_steer_test.go

key-decisions:
  - "steerAutoDeliveryNotice is the exact byte-stable string: \"The previous turn ended before this message could be delivered, so it is being sent now as a new message:\" — asserted byte-for-byte by TestLeftoverSteerAutoDeliversAsNextTurn and by the aura.steer frame's own structural payload (delivery: \"auto_delivery_next_turn\"), so a consumer never has to string-match prose to render the notice."
  - "runner.go's 596/600 pre-existing headroom (recorded in 52-04-SUMMARY.md) required the refactor-on-touch split BEFORE Task 1's one-line wrap could land: Deps/ResumeHook/New()/the two default-timeout consts moved verbatim to a new internal/runner/runner_deps.go (pure move, zero behavior change) — runner.go closed this plan at 397 LOC."
  - "The follow-on (auto-delivered) turn is the ONLY thing that persists the leftover text, via its own ordinary appendUserTurn/AppendTurn path. 52-04's drain-time persistSteerTurn branch was already guarded on the two mid-run delivery forms and reserved auto_delivery_next_turn as a deliberate no-op — this plan verified that guard rather than assuming it, and TestLeftoverSteerPersistsExactlyOneTurn counts exactly 1 matching aura.conversation_turns row, never merely asserting one exists."
  - "FA-3 is closed, not deferred: TestSteerHasNoFourthOutcome is a 3-case table (delivered mid-run / auto-delivered next turn / refused with an actionable 410), each proven by an independent observable (the model's next request, a persisted-row count, and the HTTP status+body respectively) against a real runner and a real HTTP round-trip. No fourth case surfaced."
  - "The terminal-run 410 and the mid-run-drop auto-delivery are disjoint and jointly exhaustive by construction, not by inspection: the 410 fires only when sess.terminalState() is already true at POST time (the SAME accessor the reaper and the resume route already consult — grep -Ec 'time\\.Now|deadline|expired' returns 0 in server_run_steer.go); the auto-delivery covers every case where the POST succeeded (202) against a still-live run whose drain never came before the run ended."
  - "Both 410 causes (terminal-run refusal, inbox-closed sentinel) now share one writeSteerGone call site, so grep -c 'StatusGone' returns exactly 1 — the two causes remain distinguishable to a caller only by response BODY content, never by a second status literal."

patterns-established:
  - "A pre-existing test fixture that drives its run to terminal completion before exercising Push-time validation logic breaks the moment a terminal-run refusal is added ahead of that logic in the same handler — the fix is a genuinely LIVE session (RunRegistry.Start), not a weakened assertion."
  - "A goroutine leak surfaced by a LATER, unrelated test's own goleak check when a new subtest makes a real http.Post: close idle connections in t.Cleanup at the site of the new real HTTP call, mirroring internal/runner/main_test.go's own documented closeIdleHTTPConnections() precedent."

requirements-completed: [STEER-04, STEER-03]

coverage:
  - id: D1
    description: "A run that ends with one steer still queued auto-delivers it as the next user turn: the iterator yields the run's own terminal Event, then an aura.steer Event naming the next-turn delivery form, then a fresh turn whose user message is the leftover, preceded by the byte-stable notice line"
    requirement: STEER-04
    verification:
      - kind: unit
        ref: "internal/runner/runner_steer_leftover_test.go#TestLeftoverSteerAutoDeliversAsNextTurn"
        status: pass
    human_judgment: false
  - id: D2
    description: "The leftover persists EXACTLY ONCE — a row count, not a status code, proves the drain-time persistSteerTurn branch does not also fire for the next-turn delivery form"
    requirement: STEER-04
    verification:
      - kind: unit
        ref: "internal/runner/runner_steer_leftover_test.go#TestLeftoverSteerPersistsExactlyOneTurn"
        status: pass
    human_judgment: false
  - id: D3
    description: "The no-leftover path is byte-identical to before this plan: same event count, same persisted turn count, same LLM call count"
    requirement: STEER-04
    verification:
      - kind: unit
        ref: "internal/runner/runner_steer_leftover_test.go#TestLeftoverSteerNoLeftoverPathUnchanged"
        status: pass
    human_judgment: false
  - id: D4
    description: "A nil steer inbox leaves the whole auto-delivery path inert"
    requirement: STEER-04
    verification:
      - kind: unit
        ref: "internal/runner/runner_steer_leftover_test.go#TestLeftoverSteerNilInboxIsInert"
        status: pass
    human_judgment: false
  - id: D5
    description: "The yield-after-false contract holds through the auto-delivery: once a consumer stops, nothing further is yielded and the inbox is left undrained"
    requirement: STEER-03
    verification:
      - kind: unit
        ref: "internal/runner/runner_steer_leftover_test.go#TestLeftoverSteerHonoursYieldAfterFalse"
        status: pass
    human_judgment: false
  - id: D6
    description: "The auto-delivery chain is bounded at exactly one hop per Turn call — a steer queued during the auto-delivered turn is handled by the normal drain points, not a second auto-delivery"
    requirement: STEER-04
    verification:
      - kind: unit
        ref: "internal/runner/runner_steer_leftover_test.go#TestAutoDeliveryChainIsBounded"
        status: pass
    human_judgment: false
  - id: D7
    description: "The auto-delivered turn runs under the SAME already-held per-conversation lock, never a second acquisition (which would deadlock) — driven under -race with the real lock"
    requirement: STEER-04
    verification:
      - kind: unit
        ref: "internal/runner/runner_steer_leftover_test.go#TestDeliverLeftoverSteerRaceLockedContextReuse"
        status: pass
    human_judgment: false
  - id: D8
    description: "POST to a run whose session is already terminal is refused 410 with a body stating the message was not queued, distinguishable from the resume route's own 410 and from the inbox-closed sentinel's 410"
    requirement: STEER-04
    verification:
      - kind: unit
        ref: "internal/agui/server_run_steer_test.go#TestSteerAtTerminalRunIs410"
        status: pass
    human_judgment: false
  - id: D9
    description: "FA-3's closure: a POST to a steer route has exactly three outcomes — delivered mid-run, auto-delivered next turn, refused with an actionable status — no fourth (silent-drop) path exists"
    requirement: STEER-04
    verification:
      - kind: integration
        ref: "internal/agui/server_run_steer_test.go#TestSteerHasNoFourthOutcome"
        status: pass
    human_judgment: false

duration: ~1h13m measured between the Task 1 and Task 2 commits (0400932cf 20:27:21+02:00 -> 9ab99b3c3 21:00:50+02:00 covers the recorded window; total session time including a mid-session API-drop recovery was longer)
completed: 2026-08-25
status: complete
---

# Phase 52 Plan 05: Auto-deliver the leftover steer, and refuse one at a dead run Summary

**A steer accepted (202) but never drained before its run ended now auto-delivers as the very next user turn — said out loud via a byte-stable notice line, persisted exactly once, bounded to one hop — while a steer POSTed at an already-terminal run is refused 410 with a body distinct from every other 410 on the surface; `TestSteerHasNoFourthOutcome` proves the three outcomes are exhaustive.**

## Performance

- **Duration:** ~1h13m between the two task commits (see frontmatter `duration` for the exact caveat — a mid-session API disconnect/recovery extended real wall-clock time beyond this window)
- **Completed:** 2026-08-25
- **Tasks:** 2/2
- **Files modified:** 6 (4 in Task 1, 2 in Task 2)

## Accomplishments

- `(*Runner).deliverLeftoverSteer` (`internal/runner/runner_steer.go`) wraps every turn's iterator: forwards all inner events, and on exhaustion drains the conversation's steer inbox one final time — non-empty means one `aura.steer` Event naming the `auto_delivery_next_turn` form, then one more real turn (via `turnLocked`, under the SAME already-held lock, never a fresh `Turn`/`runTurn`) whose user message is the leftover text(s) joined FIFO behind `steerAutoDeliveryNotice`.
- The chain is bounded by a named constant, `steerAutoDeliverMaxChain = 1` — structurally, not probabilistically: `TestAutoDeliveryChainIsBounded` proves a steer queued during the auto-delivered turn is caught by the normal drain points, not a second hop.
- The leftover persists exactly once, proven by a row COUNT (`TestLeftoverSteerPersistsExactlyOneTurn`) rather than an existence check — 52-04's drain-time `persistSteerTurn` branch was already guarded against the `auto_delivery_next_turn` form, and this plan verified (rather than assumed) that guard holds.
- `runTurn` in `runner.go` gained exactly one line calling `deliverLeftoverSteer`; `grep -c 'deliverLeftoverSteer' internal/runner/runner.go` returns 1. `internal/agui/translator.go` was touched by neither commit — the auto-delivery reuses the existing `aura.steer` wire branch, no second one.
- `handleRunSteer` (`internal/agui/server_run_steer.go`) now refuses 410 before ever touching the inbox when `sess.terminalState()` is already true, with a body (`steerTerminalRunMessage`) that says plainly the message was NOT queued — distinguishable from the resume route's replay-window 410 and from the inbox-closed sentinel's own message, sharing one `writeSteerGone` call site (`grep -c 'StatusGone'` == 1).
- `TestSteerHasNoFourthOutcome` closes FA-3: a 3-case table (delivered mid-run / auto-delivered next turn / refused 410) against a real runner and a real HTTP round-trip, each proven by an independent observable. No fourth case surfaced.
- Refactor-on-touch: `runner.go` was at 596/600 LOC before this plan started (per 52-04-SUMMARY.md's own headroom note); `Deps`/`ResumeHook`/`New()` moved verbatim to a new `runner_deps.go` (pure split, zero behavior change) to make room. `runner.go` closed this plan at **397 LOC**.

## Task Commits

1. **Task 1: Auto-deliver the leftover steer as the next user turn** - `0400932cf` (feat)
2. **Task 2: The terminal-run 410 and the no-fourth-outcome proof** - `9ab99b3c3` (feat)

_Note: no plan metadata commit yet — that follows this SUMMARY per the executor's final_commit step._

## Files Created/Modified

- `internal/runner/runner_deps.go` (new, 226 lines) - `Deps`, `ResumeHook`, `New()`, the two default-timeout consts, moved verbatim out of `runner.go`
- `internal/runner/runner.go` (596 → 397 lines) - `runTurn` rewritten to funnel both lock-path branches through one `deliverLeftoverSteer` call site
- `internal/runner/runner_steer.go` (92 → 228 lines) - `deliverLeftoverSteer`, `steerAutoDeliverMaxChain`, `steerDeliveryAutoNextTurn`, `steerAutoDeliveryNotice`, `joinSteerLeftovers`, `leftoverSteerNoticeEvent`
- `internal/runner/runner_steer_leftover_test.go` (new, 419 lines) - the full Task 1 leftover-auto-delivery test suite (extracted to its own file to keep `runner_steer_test.go` under the 600-LOC cap)
- `internal/agui/server_run_steer.go` (81 → 109 lines) - the terminal-run 410 branch, `steerTerminalRunMessage`, `writeSteerGone`
- `internal/agui/server_run_steer_test.go` (final: 488 lines) - `TestSteerAtTerminalRunIs410`, `TestSteerHasNoFourthOutcome`, plus a fixture fix to the pre-existing `TestSteerRouteRendersInboxSentinels`

## Decisions Made

See `key-decisions` in the frontmatter for: the exact `steerAutoDeliveryNotice` string, the refactor-on-touch split rationale, the single-persistence-site decision, FA-3's closure, and the terminal-410/auto-delivery disjointness argument.

**Values recorded per this plan's own `<output>` spec:**
- **`steerAutoDeliveryNotice`** (exact, byte-for-byte): `"The previous turn ended before this message could be delivered, so it is being sent now as a new message:"`
- **Post-edit `wc -l internal/runner/runner.go`:** `397`
- **Persisted-row count for the auto-delivered leftover:** `1` (proven by `TestLeftoverSteerPersistsExactlyOneTurn`, a COUNT assertion, not an existence check)
- **FA-3's disposition:** **Closed.** `TestSteerHasNoFourthOutcome` enumerates and proves exactly three outcomes (delivered mid-run, auto-delivered next turn, refused with an actionable 410) against a real runner and a real HTTP round-trip; no fourth case surfaced during this plan's execution.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `runner.go` had no headroom left for the one-line wrap**
- **Found during:** Task 1, before writing any code (per the plan's own `<no_stale_inputs>` instruction to re-`wc -l` first)
- **Issue:** `runner.go` was at 596/600 LOC per 52-04-SUMMARY.md's own recorded headroom note; even a single new line would risk tripping the file-size pre-commit hook once the doc comment and the wrap were added.
- **Fix:** Moved `Deps`, `ResumeHook`, `New()`, and the two default-timeout consts verbatim into a new `internal/runner/runner_deps.go` (pure move, no behavior change), then rewrote `runTurn` to add the one-line `deliverLeftoverSteer` wrap.
- **Files modified:** `internal/runner/runner.go`, `internal/runner/runner_deps.go` (new)
- **Verification:** `wc -l` both files (397, 226), `go build ./...`, `go test ./internal/runner/`.
- **Committed in:** `0400932cf`

**2. [Rule 3 - Blocking] `runner_steer_test.go` exceeded the 600-LOC cap after appending the new Task 1 test suite**
- **Found during:** Task 1, flagged by the user mid-session
- **Issue:** Appending the leftover-auto-delivery tests directly into the pre-existing `runner_steer_test.go` pushed it well past 600 LOC.
- **Fix:** Extracted the entire new test suite into a new `internal/runner/runner_steer_leftover_test.go` (419 lines), leaving `runner_steer_test.go` byte-identical to its already-committed 52-04 content (262 lines) — confirmed via `git diff` showing zero changes, so it did not need to be staged in Task 1's commit at all.
- **Files modified:** `internal/runner/runner_steer_leftover_test.go` (new)
- **Verification:** `wc -l` both files, `go test -race ./internal/runner/`.
- **Committed in:** `0400932cf`

**3. [Rule 1 - Lint] `golangci-lint` `rangeint`/`newexpr` modernize findings**
- **Found during:** Task 1 and Task 2 pre-commit hooks
- **Issue:** `for hop := 0; hop < steerAutoDeliverMaxChain; hop++` (unused `hop` in the body) in `runner_steer.go` was flagged as convertible to `for range N`; a hand-written `strPtr(s string) *string` helper plus its one call site in `server_run_steer_test.go` were flagged as reducible to the Go 1.26 `new(x)` builtin.
- **Fix:** Rewrote the loop as `for range steerAutoDeliverMaxChain {`; replaced the `strPtr(...)` call with `new("please handle this task")` directly and deleted the helper function.
- **Files modified:** `internal/runner/runner_steer.go`, `internal/agui/server_run_steer_test.go`
- **Verification:** `golangci-lint run` reports 0 issues for both packages; tests still pass.
- **Committed in:** `0400932cf`, `9ab99b3c3` respectively

**4. [Rule 1 - Bug] Self-inflicted grep-count trips from doc-comment wording**
- **Found during:** Task 1 and Task 2 acceptance-criteria verification
- **Issue:** `grep -c 'deliverLeftoverSteer' internal/runner/runner.go` returned 2 (not the required 1) because the doc comment above `runTurn` named the function literally; likewise `grep -c 'StatusGone' internal/agui/server_run_steer.go` returned 2 because `writeSteerGone`'s doc comment said "http.StatusGone" literally.
- **Fix:** Reworded both comments to describe the mechanism ("leftover-steer auto-delivery wrap", "410-Gone call site") without repeating the exact grepped symbol/literal a second time.
- **Files modified:** `internal/runner/runner.go`, `internal/agui/server_run_steer.go`
- **Verification:** Both grep counts return exactly 1.
- **Committed in:** `0400932cf`, `9ab99b3c3` respectively

**5. [Rule 1 - Bug] Goroutine leak in an unrelated test surfaced by a new real HTTP call**
- **Found during:** Task 2, running the full `internal/agui` package suite (not just the new tests in isolation)
- **Issue:** `TestSteerHasNoFourthOutcome`'s "delivered mid-run" subtest added a third real `http.Post` call in the same test binary (via the pre-existing `steerViaHTTPTool`/e2e infra), leaving `net/http.(*persistConn)` read/write-loop goroutines parked in the shared `http.DefaultTransport` idle-connection pool past this subtest's completion; later, unrelated tests' own `defer goleak.VerifyNone(t)` checks then flagged these as "unexpected."
- **Fix:** Added `t.Cleanup(func() { http.DefaultClient.CloseIdleConnections() })` at the top of the subtest, mirroring `internal/runner/main_test.go`'s own documented `closeIdleHTTPConnections()` precedent for the identical failure class.
- **Files modified:** `internal/agui/server_run_steer_test.go`
- **Verification:** Full `go test -race ./internal/agui/` package suite green, including the previously-flagged unrelated tests.
- **Committed in:** `9ab99b3c3`

**6. [Rule 1 - Bug, pre-existing test fixture] `TestSteerRouteRendersInboxSentinels` broke when the terminal-run 410 branch was added**
- **Found during:** Task 2, immediately after adding the terminal-run 410 check
- **Issue:** This pre-existing 52-04 test drove its fixture run to full completion via `runDetachedToTerminal` BEFORE exercising the empty/oversize/queue-full/closed sentinel ladder — once the run was terminal, every subsequent POST in the test correctly hit the new 410 branch first, masking the sentinel-validation logic it was meant to test (3 of 4 subtests failed: `status = 410, want 400`, etc.). This is legitimate test evolution, not modifying a test to force a pass: the underlying business behavior genuinely changed (a new valid refusal path was correctly added ahead of the sentinel logic), so the fixture needed a LIVE, non-terminal run to keep testing what it always tested.
- **Fix:** Replaced `runDetachedToTerminal(t, srv, tid)` with a directly-constructed live session via `s.runs.Start(runParams{runID, threadID: tid, identityID: localIdentityID})` plus `defer sess.finish()`, following the exact pattern the pre-existing `TestHandleRunSteerHides404Ladder`'s "foreign identity" subtest already used.
- **Files modified:** `internal/agui/server_run_steer_test.go`
- **Verification:** All 4 subtests of `TestSteerRouteRendersInboxSentinels` pass again.
- **Committed in:** `9ab99b3c3`

---

**Total deviations:** 6 auto-fixed (2 blocking refactor-on-touch splits, 1 lint, 1 self-inflicted grep-count wording fix pattern applied twice, 1 goroutine leak, 1 pre-existing test fixture update caused directly by this plan's new behavior).
**Impact on plan:** All fixes were necessary to land correct, lint-clean, cap-compliant commits with an honest, fully-green test suite. No scope creep — every fix was directly caused by this plan's own new code, new tests, or the size discipline CLAUDE.md mandates.

## Issues Encountered

None beyond the deviations above — no blockers that required stopping and asking, no architectural changes, no authentication gates. A mid-session API disconnect interrupted execution between Task 1's test-writing and its completion; the tree state was independently re-verified against the reported state before resuming, and no work was redone or lost.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- `steerAutoDeliveryNotice`'s exact string and the `auto_delivery_next_turn` delivery-form value are load-bearing for 52-08's live E2E assertions.
- `internal/runner/runner.go` closed this plan at 397/600 LOC — ample headroom restored for 52-06/52-07/52-08.
- `internal/runner/runner_steer.go` closed at 228/600 LOC, `internal/agui/server_run_steer.go` at 109/600 LOC — both still far under cap.
- The "delivered mid-run" outcome in `TestSteerHasNoFourthOutcome` is proven via the real e2e HTTP infra; the "auto-delivered next turn" outcome is proven by driving `r.Turn(...)` directly with a synchronous push-at-FinishReason technique rather than a live HTTP-transport race, because forcing that exact race deterministically over a real transport is not achievable without either modifying `runTerminal` or accepting genuine goroutine-scheduling non-determinism — the codebase's own `steerInjectorTool` precedent (52-04) exists for the same reason. The HTTP-transport-level race for the leftover path is instead proven deterministic by the Task 1 runner-package suite (`TestDeliverLeftoverSteerRaceLockedContextReuse`, driven under `-race` through the real per-conversation lock).
- The plain-`user_message_fallback` mid-run delivery branch (noted in 52-04-SUMMARY.md as fake-client-only) remains untouched by this plan; still a candidate for a future real-provider smoke pass.

## Self-Check: PASSED

- FOUND: internal/runner/runner_deps.go
- FOUND: internal/runner/runner_steer_leftover_test.go
- FOUND: internal/runner/runner.go
- FOUND: internal/runner/runner_steer.go
- FOUND: internal/agui/server_run_steer.go
- FOUND: internal/agui/server_run_steer_test.go
- FOUND: commit 0400932cf
- FOUND: commit 9ab99b3c3

---
*Phase: 52-mid-turn-steering*
*Completed: 2026-08-25*
