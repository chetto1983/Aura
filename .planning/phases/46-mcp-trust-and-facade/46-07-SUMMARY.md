---
phase: 46-mcp-trust-and-facade
plan: 07
subsystem: testing
tags: [mcp, calendar, e2e, ci, fixture]

requires:
  - phase: 46-06
    provides: "Calendar tracer slice wired end to end — curated tool, bridge multiplex, risk classification, CI tier"
provides:
  - "CI fixture calendar so the calendar_integration tier proves the MCP-05 round-trip without OAuth"
  - "Live E2E evidence deferred — see Deviations"
affects: [46-08, 46-09]

actuals:
  tokens: 55
  tasks: 1
  commits: 1

tech-stack:
  added: []
  patterns: ["json-provider fixture calendar for daemon-free CI coverage of MCP-05"]

key-files:
  created:
    - internal/mcp/testdata/fixture-calendar.json
  modified:
    - .github/workflows/ci.yml

key-decisions:
  - "CI fixture uses the fork's json provider (no OAuth, no secrets) to prove the MCP-05 round-trip in CI"
  - "Live E2E drive deferred to operator's manual close-out — stack-dependent live evidence not captured in this plan's scope"

patterns-established:
  - "Fixture-calendar pattern: seed a local JSON file via /admin/accounts for zero-credential CI coverage"

requirements-completed: []

coverage:
  - id: D1
    description: "CI fixture calendar seeded so calendar_integration tier proves MCP-05 round-trip (get_calendar_events → opaque eventId → get_calendar_event_details) without OAuth"
    requirement: "MCP-05"
    verification:
      - kind: integration
        ref: ".github/workflows/ci.yml — calendar_integration job seeds fixture, restarts sidecar, runs tier"
        status: pass
    human_judgment: false
  - id: D2
    description: "Live E2E drive of the agent on the running stack — manifest inspection, tool_invocations rows, scoring >9.8"
    requirement: "MCP-04"
    verification: []
    human_judgment: true
    rationale: "Core live E2E deferred — requires running stack and human-driven conversation; operator closed out plan manually"

duration: N/A
completed: 2026-08-24
status: complete
---

# Plan 46-07: Tracer Gate Summary

**CI fixture calendar seeded for MCP-05 round-trip proof; live E2E validation deferred to operator close-out**

## Performance

- **Duration:** N/A (CI infrastructure only; live E2E deferred)
- **Started:** 2026-08-23
- **Completed:** 2026-08-24 (manual close-out)
- **Tasks:** 1/2 completed
- **Files modified:** 2

## Accomplishments
- CI fixture calendar (two-event JSON file + /admin/accounts seed) so `calendar_integration` tier proves the MCP-05 round-trip without OAuth or secrets
- Proven locally: `get_calendar_events` mints an opaque eventId, `get_calendar_event_details` resolves it with no accountId, and a missing-eventId call is refused outright

## Task Commits

1. **Task 1 (partial): CI fixture seeding** - `044d8871e` (test)
2. **Task 2: Score scenario and fill VALIDATION.md** - deferred (operator manual close-out)

## Files Created/Modified
- `internal/mcp/testdata/fixture-calendar.json` - Two-event fixture calendar for CI
- `.github/workflows/ci.yml` - Seed step after health gate, restart, re-wait

## Decisions Made
- Used json provider (no OAuth) for CI fixture — same coverage as a connected Google account, zero credential handling
- Operator closed out the plan manually — live E2E validation (driving the real agent, capturing tool_invocations rows, scoring >9.8) deferred

## Deviations from Plan

The plan's core objective was a live E2E drive on the running stack. Only the CI infrastructure (Task 1's fixture seeding) was completed. The live drive, evidence capture, VALIDATION.md filling, and scoring (Task 1 core + Task 2) are deferred. The operator chose to close out manually and proceed with expansion (46-08, 46-09).

**What this does NOT prove:** SC#1/SC#2/SC#4 live evidence, manifest composition at runtime, approval gate firing for destructive actions, byte-identical reference round-trip. These remain owed to 46-VALIDATION.md.

## Issues Encountered
None — the CI fixture worked as designed.

## Next Phase Readiness
- 46-08 (WhatsApp sidecar) can proceed on the pattern established by 46-04/05/06
- Live E2E validation for the full phase (calendar + WhatsApp) is still owed before phase close

---
*Phase: 46-mcp-trust-and-facade*
*Completed: 2026-08-24 (manual close-out)*
