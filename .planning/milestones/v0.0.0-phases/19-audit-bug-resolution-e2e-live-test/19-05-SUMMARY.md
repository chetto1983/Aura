---
phase: 19-audit-bug-resolution-e2e-live-test
plan: 05
subsystem: ui
tags: [telegram, agui, sanitize, hitl, ask_user, error-handling, run-error, document-convert]

# Dependency graph
requires:
  - phase: 19-04
    provides: exported agui.SanitizeString (the shared redaction chokepoint H2 routes RunErrorEvent.Message through)
  - phase: 19-09
    provides: serve_channels.go MCP-cluster context (same file the D-04 stale comment lives in)
provides:
  - "H2: Telegram renderer surfaces a sanitized turn-failure reason (RunErrorEvent case) instead of dropping it"
  - "H3: async 5-50MB document-conversion failures notify the user via convertFailMessage instead of silence"
  - "M-e: /cancel during a pending ask_user pause cancels the pause (SubmitAnswer ActionCancel) + clears the inline keyboard"
  - "D-04: corrected the stale serve_channels.go ensuringTurn comment that claimed a string rendering nowhere"
affects: [telegram, e2e-live-test, agui]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "User-facing error rendering routes through the one agui.SanitizeString redaction contract (no second sanitizer)"
    - "Per-chat pause-prompt message tracking (pausePrompts map) so a /cancel text command can disarm a keyboard the button path disarms via its own callback message"

key-files:
  created:
    - internal/channels/telegram/renderer_error_test.go
  modified:
    - internal/channels/telegram/renderer.go
    - internal/channels/telegram/bot_dispatch.go
    - internal/channels/telegram/bot_dispatch_hitl.go
    - internal/channels/telegram/hitl.go
    - internal/channels/telegram/bot.go
    - cmd/aura/serve_channels.go

key-decisions:
  - "H2 sends the error as a FRESH message (forceNew) so the streamed answer msg #2 is never overwritten by the failure notice"
  - "M-e routes /cancel at the dispatch seam (step 0 in onText, before the command intercept) so a pause-cancel never falls through to the no-op turn-cancel"
  - "Channel tracks the last pause-prompt message per chat (pausePrompts) to clear the keyboard for a /cancel that arrives with no handle on the prompt"

patterns-established:
  - "Pattern 1: user-facing error strings on the Telegram surface go through agui.SanitizeString (DSN/token redaction), mirroring the HTTP RUN_ERROR path"
  - "Pattern 2: the channel stays render-only over the Runner — reads PendingFor, resolves via SubmitAnswer, never writes paused_states (T-13-06-PauseHijack)"

requirements-completed: [H2, H3, M-e]

# Metrics
duration: ~40min
completed: 2026-06-10
---

# Phase 19 Plan 05: Telegram user-facing error + orphaned-pause fixes Summary

**The Telegram renderer now surfaces a sanitized turn-failure reason (RunErrorEvent), async document-conversion failures notify the user, and `/cancel` during an ask_user pause cancels the pause and clears the keyboard.**

## Performance

- **Duration:** ~40 min
- **Started:** 2026-06-10T11:21Z
- **Completed:** 2026-06-10
- **Tasks:** 2
- **Files modified:** 6 (1 created)

## Accomplishments

- **H2** — Added a `case *events.RunErrorEvent:` to the renderer's `consume` switch. A turn that errors now shows the user the failure reason routed through `agui.SanitizeString` (the same redaction chokepoint the HTTP RUN_ERROR path uses — a DSN/token in the error never reaches the user, T-19-11), instead of dropping it and leaving only the status pane's bare "Stato: errore" glyph. Sent as a fresh message so the streamed answer msg #2 is never overwritten.
- **H3** — The async (5-50MB) document-conversion callback now sends `convertFailMessage` via the captured sender on `convErr != nil`, mirroring the sync ≤5MB sibling, instead of logging + returning silently after "📄 …elaborando…" (which stranded the user forever).
- **M-e** — `/cancel` during a pending ask_user pause now routes through `SubmitAnswer(…ActionCancel)` (resolving the `paused_states` row) and clears the rendered prompt's inline keyboard, matching the "Annulla" button. With no pending pause the existing in-flight-turn ctx-cancel is preserved.
- **D-04** — Corrected the stale `serve_channels.go` `ensuringTurn` comment that claimed the create-failure "the user sees a generic ❌ Errore" (a string that rendered nowhere); it now describes the actual RUN_ERROR → renderer path.

## Task Commits

Each task was committed atomically:

1. **Task 1: Render turn errors (H2) + correct the stale serve_channels comment (D-04) + async doc-convert notice (H3)** — `658069cc` (fix)
2. **Task 2: /cancel during a pending pause cancels the pause (M-e)** — `772f771a` (fix)

_Note: an unrelated parallel-session commit (`d25a5891 test: cover bounded buffer`) landed between the two task commits; it is not part of this plan._

## Files Created/Modified

- `internal/channels/telegram/renderer.go` — new `RunErrorEvent` consume case + `sendError` method (sanitized via `agui.SanitizeString`, sent forceNew); `agui` import added.
- `internal/channels/telegram/renderer_error_test.go` — **created**: H2 regression tests (sanitized reason renders, no DSN leak; empty-message error still notifies). Split out to keep `renderer_test.go` under the 600-LOC cap (refactor-on-touch).
- `internal/channels/telegram/bot_dispatch.go` — H3 async convert-fail notice via the captured sender; M-e step-0 `/cancel` intercept in `onText`; clear tracked prompt on button resolution.
- `internal/channels/telegram/bot_dispatch_hitl.go` — `cancelPendingPause` (M-e cancel path) + `trackPausePrompt`/`takePausePrompt` helpers; `promptPendingPause` now tracks the rendered prompt.
- `internal/channels/telegram/hitl.go` — `cancel` method (ActionCancel submit); `prompt`/`send` now return the sent `*tele.Message` for keyboard tracking.
- `internal/channels/telegram/bot.go` — `pausePrompts map[int64]*tele.Message` field on the Telegram struct.
- `cmd/aura/serve_channels.go` — corrected the stale `ensuringTurn` D-04 comment.

## Decisions Made

- **H2 sends a fresh message (forceNew=true)** rather than editing the streamed content msg #2, so a partial streamed answer plus the failure reason both survive.
- **M-e seam = step 0 in `onText`** (before the command intercept). `/cancel` is a command, so the only correct ordering is to intercept it for a pending-pause chat ahead of `dispatchRich`; otherwise it falls through to the no-op turn-cancel.
- **Per-chat pause-prompt tracking (`pausePrompts`)** is required because the "Annulla" button disarms via its own callback message, but a `/cancel` text command arrives as a fresh message with no handle on the prompt. The button-resolution path clears the tracked handle so a later `/cancel` never disarms an already-resolved prompt.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Split renderer tests into renderer_error_test.go to satisfy the 600-LOC cap**
- **Found during:** Task 1 (committing the H2 tests)
- **Issue:** Appending the two H2 regression tests pushed `renderer_test.go` to 618 LOC, over the CLAUDE.md / pre-commit 600-LOC cap (the file-size hook rejected the commit).
- **Fix:** Moved the two H2 RunErrorEvent tests into a new `renderer_error_test.go` (refactor-on-touch, split by concern). Both files are now under the cap (565 + 61).
- **Files modified:** internal/channels/telegram/renderer_test.go, internal/channels/telegram/renderer_error_test.go
- **Verification:** file-size hook passes; `go test -run TestRendererRunError` green.
- **Committed in:** 658069cc (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking).
**Impact on plan:** The split is a mechanical file-size compliance move (no behavior change, no scope creep) mandated by the project's no-god-class rule.

## Issues Encountered

None — both tasks executed as planned. The status pane's existing `RunErrorEvent` case (which already flips to "Stato: errore" but swallows the reason) was confirmed to be a separate consumer from the content renderer; the golden `testdata/statuspane_run_error.golden` belongs to the status pane and was left unchanged (the H2 fix lives in the renderer, msg #2).

## User Setup Required

None — no external service configuration required. No new packages (go.mod unchanged), no migrations, no env vars.

## Next Phase Readiness

- The three Telegram user-facing findings (H2/H3/M-e) + the D-04 stale comment are closed and unit-regression-covered.
- These fixes are part of the broader Phase 19 audit-resolution + E2E live-test effort; the live Telegram E2E pass should confirm the RunErrorEvent render, the async convert-fail notice, and the `/cancel`-pause routing against a real bot.
- No blockers introduced for downstream plans.

---
*Phase: 19-audit-bug-resolution-e2e-live-test*
*Completed: 2026-06-10*

## Self-Check: PASSED

- All 8 created/modified files verified on disk.
- Both task commits verified in git history (`658069cc`, `772f771a`).
