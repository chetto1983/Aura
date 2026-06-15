---
phase: 14-onboarding-agent-md
plan: 03
subsystem: onboarding
tags: [agent-md, onboarding, loop-agent, profile-extraction, session-state]

requires:
  - phase: 14-onboarding-agent-md
    plan: 01
    provides: filesystem Agent.md profile store and render helpers
  - phase: 14-onboarding-agent-md
    plan: 02
    provides: identity-aware messages[1] profile injection
provides:
  - Channel-agnostic onboarding Session state machine
  - InterviewStepAgent usable through workflow.NewLoop
  - Confirm, skip, cancel, and edit terminal StateDelta contracts
  - Deterministic structured answer extraction into Agent.md and preferences.json
affects: [phase-14, telegram-onboarding, profile-store]

tech-stack:
  added: []
  patterns: [TDD, channel-agnostic state machine, one-event-per-loop-iteration, deterministic profile extraction]

key-files:
  created:
    - internal/onboarding/session.go
    - internal/onboarding/session_test.go
    - internal/onboarding/interview.go
    - internal/onboarding/interview_test.go
    - internal/onboarding/extractor.go
    - internal/onboarding/extractor_test.go
  modified: []

key-decisions:
  - "The onboarding package imports agent/workflow and profile, but has no Telegram dependency."
  - "InterviewStepAgent emits natural-language LLMResponse.Content events with StateDelta payloads and no tool calls."
  - "Confirm, skip, cancel, and edit-driven confirm terminate via Actions.Escalate=true so callers can resume normal chat or write Agent.md."
  - "Extraction is deterministic over structured answers; no LLM extraction runs in this plan."

patterns-established:
  - "Session.Queue lets tests and future channel adapters feed structured inputs while the LoopAgent emits one question/draft/final event per iteration."
  - "ExtractDraft bounds answer fields and final Agent.md size before returning profile.Store-compatible data."

requirements-completed: [UX-05]

duration: ~30 min
completed: 2026-06-11
---

# Phase 14 Plan 03: Channel-Agnostic Profile Onboarding Summary

**A reusable onboarding workflow now collects structured profile answers, drafts Agent.md, and terminates with state deltas Telegram can persist or skip.**

## Performance

- **Duration:** ~30 min
- **Started:** 2026-06-11T18:22:00+02:00
- **Completed:** 2026-06-11T18:37:00+02:00
- **Tasks:** 3
- **Files modified:** 6

## Accomplishments

- Added `internal/onboarding.Session` with explicit steps, statuses, intents, queued inputs, draft state, and terminal transitions.
- Added `InterviewStepAgent` plus `NewLoop(session, maxIter)` over `workflow.NewLoop(ProfileOnboardingLoop, ..., InterviewStepAgent)`.
- Added confirm/skip/edit tests adapted from spike 039, including terminal `Actions.Escalate=true` and state deltas.
- Added deterministic `ExtractDraft` for name, language, timezone, tone, response length, voice mode, proactive messaging, and custom instructions.
- Ensured onboarding remains channel-agnostic and emits no tool calls for ordinary interview steps.

## Task Commits

1. **Task 1: define onboarding session state and transitions** - `22ab7f3c`
2. **Task 2: implement InterviewStepAgent and Loop builder** - `9cc148a9` (included in concurrent Phase 20 commit; see deviation)
3. **Task 3: add profile extraction/render helpers** - `75355b65`

## Files Created/Modified

- `internal/onboarding/session.go` - Session state machine, intents, steps, transition state, and queued-input loop adapter.
- `internal/onboarding/session_test.go` - Confirm-before-draft, terminal skip/cancel, and edit-then-confirm tests.
- `internal/onboarding/interview.go` - `InterviewStepAgent`, `NewLoop`, and Event construction.
- `internal/onboarding/interview_test.go` - Confirm, skip, edit, no-tool-call, and LoopAgent scenario tests.
- `internal/onboarding/extractor.go` - Deterministic structured-answer extraction into Agent.md and `profile.Preferences`.
- `internal/onboarding/extractor_test.go` - Italian preference, Europe/Rome timezone, response length, voice mode, JSON, and bound tests.

## Decisions Made

- Kept profile onboarding separate from Telegram setup onboarding. Telegram will own buttons/rendering; this package owns workflow semantics.
- Used structured inputs rather than LLM extraction for first-run onboarding in this plan, matching the deterministic acceptance criteria.
- Represented terminal caller actions through `StateDelta` keys: `agent_md`, `preferences_json`, `onboarding_completed`, `skipped`, `canceled`, and `resume_chat`.

## Deviations from Plan

### Operational Deviations

**1. [GSD commit discipline] Task 2 files landed in a concurrent Phase 20 commit**
- **Found during:** Task 2 commit
- **Issue:** `HEAD` advanced while the pre-commit hook was running; the onboarding loop files were captured by concurrent commit `9cc148a9`.
- **Fix:** Did not rewrite the other session's commit. Reconciled the index, verified the onboarding tests, and continued with a scoped Task 3 commit.
- **Files modified:** `internal/onboarding/interview.go`, `internal/onboarding/interview_test.go`, `internal/onboarding/session.go`
- **Verification:** `go test ./internal/onboarding/ -run 'Interview|Loop|Confirm|Skip|Edit' -count=1`
- **Committed in:** `9cc148a9`

---

**Total deviations:** 1 operational deviation, 0 product-code deviations.
**Impact on plan:** Implementation scope and behavior remained correct; only task-commit attribution is interleaved.

## Verification

- `go test ./internal/onboarding/ -run Session -count=1`
- `go test ./internal/onboarding/ -run 'Interview|Loop|Confirm|Skip|Edit' -count=1`
- `go test ./internal/onboarding/ ./internal/profile/ -run 'Extract|Render|Preference' -count=1`
- `go test ./internal/onboarding/ -count=1`
- `go test ./internal/onboarding/ ./internal/profile/ -count=1`
- `go test ./internal/agent/workflow/ -run LoopAgent -count=1`
- `go build ./...`

## User Setup Required

None.

## Next Phase Readiness

Plan 14-04 can wire Telegram first-message and `/onboard` routing to `internal/onboarding.Session` and persist terminal `agent_md` plus `preferences_json` through `internal/profile.Store`.

---
*Phase: 14-onboarding-agent-md*
*Completed: 2026-06-11*
