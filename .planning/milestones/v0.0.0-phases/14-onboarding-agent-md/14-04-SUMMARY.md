---
phase: 14-onboarding-agent-md
plan: 04
subsystem: telegram
tags: [agent-md, onboarding, telegram, routing, profile-store]

requires:
  - phase: 14-onboarding-agent-md
    plan: 01
    provides: filesystem Agent.md profile store and render helpers
  - phase: 14-onboarding-agent-md
    plan: 02
    provides: identity-aware profile context injection
  - phase: 14-onboarding-agent-md
    plan: 03
    provides: channel-agnostic onboarding Session and InterviewStepAgent
provides:
  - Telegram profile onboarding adapter with confirm, edit, skip, and stale-callback handling
  - Telegram Deps wiring for internal/profile.Store from AURA_PROFILE_DIR
  - OnText routing for first linked no-profile messages and explicit /onboard restarts
affects: [phase-14, telegram-onboarding, profile-store]

tech-stack:
  added: []
  patterns: [TDD, Telegram inline callbacks, profile Store boundary, route-before-turn guard]

key-files:
  created:
    - internal/channels/telegram/profile_onboarding.go
    - internal/channels/telegram/profile_onboarding_test.go
  modified:
    - internal/channels/telegram/bot.go
    - internal/channels/telegram/bot_dispatch.go
    - internal/channels/telegram/bot_dispatch_test.go
    - internal/channels/telegram/commands.go
    - internal/channels/telegram/commands_test.go
    - cmd/aura/serve_channels.go

key-decisions:
  - "Kept Telegram setup onboarding (/start token) separate and first in the text route."
  - "Added /onboard as a Telegram command surface, but channel OnText handles the actual restart before generic command dispatch."
  - "A linked user with no Agent.md starts profile onboarding before a normal LLM turn; unlinked users continue to normal chat."
  - "Confirm and skip persist through internal/profile.Store; edit updates the active draft before confirm."

patterns-established:
  - "profileOnboarding owns per-chat Session state, Telegram copy, and bounded inline callback payloads."
  - "profileForDispatch reuses Store as the production Telegram account resolver while tests can inject a resolver seam."

requirements-completed: [UX-05]

duration: ~20 min
completed: 2026-06-11
---

# Phase 14 Plan 04: Telegram Profile Onboarding Summary

**Telegram now makes first-run Agent.md onboarding reachable for linked users, with confirm/edit/skip flows that do not drive duplicate LLM turns.**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-06-11T18:39:00+02:00
- **Completed:** 2026-06-11T18:59:00+02:00
- **Tasks:** 3
- **Files modified:** 8

## Accomplishments

- Added `profileOnboarding`, a Telegram adapter over `internal/onboarding.Session`.
- Added inline draft buttons for confirm, edit, and skip with callback payloads under Telegram's 64-byte limit.
- Wired `telegram.Deps.Profile` and `profile.NewStore(chat.cfg.ProfileDir)` through `aura serve`.
- Routed `/onboard` before generic command dispatch so it restarts profile onboarding explicitly.
- Routed linked first-time text messages into profile onboarding before normal chat.
- Preserved setup-token `/start <token>` precedence and HITL/command routing.
- Added focused routing tests proving onboarding-consumed messages never call the turn driver.

## Task Commits

1. **Task 2: implement Telegram profile onboarding adapter** - `6e68f3ce`
2. **Task 1: add profile onboarding deps to Telegram channel** - `ebb2e6a8`
3. **Task 3: route first normal message and /onboard before LLM turn** - `04fc636f`

## Files Created/Modified

- `internal/channels/telegram/profile_onboarding.go` - Adapter, session map, Telegram copy/buttons, confirm/edit/skip writes, stale callback handling.
- `internal/channels/telegram/profile_onboarding_test.go` - Confirm write, skip metadata, edit revision, stale callback, no-store degradation tests.
- `internal/channels/telegram/bot.go` - `Deps.Profile`, profile resolver test seam, `Telegram.profile`, `/onboard` menu command.
- `internal/channels/telegram/bot_dispatch.go` - Profile manager construction, text route gates, profile callback handler, profile reply helper.
- `internal/channels/telegram/bot_dispatch_test.go` - First linked no-profile route, active profile reply route, `/onboard`, and `/start token` precedence tests.
- `internal/channels/telegram/commands.go` - `/onboard` command copy and help text.
- `internal/channels/telegram/commands_test.go` - Updated command/menu mirror tests for the 11-command Telegram surface.
- `cmd/aura/serve_channels.go` - `profile.NewStore(chat.cfg.ProfileDir)` passed into Telegram deps.

## Decisions Made

- Kept profile onboarding deterministic and channel-local: Telegram parses simple user answers and reuses the Phase 14 session/extractor rather than adding an LLM extraction pass here.
- Treated a missing profile store as a clear profile-onboarding degradation only when a linked account can be resolved or `/onboard` is explicit.
- Disarmed old inline keyboards after profile callbacks to avoid stale button reuse; stale/forged callbacks ack without writing.
- Added a per-session mutex around profile onboarding state because text and callback updates can arrive concurrently.

## Deviations from Plan

No product-code deviations.

Task order differed slightly from the written plan: the adapter was implemented first to let TDD drive the Telegram behavior, then the dependency wiring and routing were added. The final scope and acceptance criteria are unchanged.

## Verification

- `go test ./internal/channels/telegram/ -run ProfileOnboarding -count=1`
- `go test ./internal/channels/telegram/ ./cmd/aura/ -run 'Profile|Serve|Deps' -count=1`
- `go test ./internal/channels/telegram/ -run 'ProfileOnboarding|StartPayload|Command|HITL' -count=1`
- `go test -race ./internal/channels/telegram/`
- `go build ./...`

## User Setup Required

None.

## Next Phase Readiness

Plan 14-05 can validate the complete Agent.md onboarding path end to end and decide whether any manual UX checkpoint is needed before Phase 14 closes.

---
*Phase: 14-onboarding-agent-md*
*Completed: 2026-06-11*
