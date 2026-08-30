---
phase: 51-durable-delegation
plan: 12b
subsystem: ui
tags: [swarm, react, sse, playwright, telegram, llamacpp]

requires:
  - phase: 51-durable-delegation
    provides: "51-11 durable cards, report artifacts, fan-out notification, child ids and swarm_status"
  - phase: 51-durable-delegation
    provides: "51-12a identity-scoped child transcript SSE and multiplexed worker status events"
provides:
  - "A read-only worker pane sharing the cockpit right rail with Artifacts, plus a keyboard worker picker"
  - "Push-only per-worker rows with live status, duration, bounded summary, Watch and View report controls"
  - "A six-verdict live delivery-envelope acceptance at 9.9/10 on local gemma-4-12b"
  - "PRD Amendment #183 recording measured wire contracts, numbers and limitations"
affects: [51-08, phase-51-verification, swarm-ui, delegation-delivery]

actuals:
  tokens: 1010784
  tasks: 4
  commits: 24

tech-stack:
  added: []
  patterns:
    - "ReadonlyThreadProvider renders worker events through the main thread's existing assistant-ui toolkit"
    - "One status EventSource per conversation and one child EventSource per watched worker; the browser never polls"
    - "Live defects follow measure, amend, RED, fix, rebuild and repeat before acceptance"

key-files:
  created:
    - web/src/chat/workers/workerStream.ts
    - web/src/chat/workers/WorkerPane.tsx
    - web/src/chat/workers/WorkerPicker.tsx
    - web/src/chat/workers/useWorkerStatuses.ts
    - web/src/chat/workers/WorkerWatchProvider.tsx
    - web/src/shell/WorkerPaneShell.tsx
    - web/src/shell/useWorkerPane.ts
    - .planning/phases/51-durable-delegation/live-check/cockpit/RESULTS.md
  modified:
    - web/src/AppShell.tsx
    - web/src/chat/displays/SwarmReportTable.tsx
    - web/src/chat/displays/swarmRow.ts
    - web/src/chat/displays/types.ts
    - web/src/i18n/resources.display.ts
    - internal/swarm/delegation_delivery.go
    - internal/agent/display/preview.go
    - internal/agent/tools/swarm_status.go
    - prd.md

key-decisions:
  - "The worker pane and Artifacts remain mutually exclusive views of one right-rail slot on desktop and one Drawer slot on mobile"
  - "Native named EventSource records are subscribed explicitly; the default message event remains only a compatibility fallback"
  - "A terminal RUN_FINISHED or RUN_ERROR closes the child EventSource successfully; only a pre-terminal error selects the artifact fallback"
  - "The full report stays in the Markdown artifact while the steer rail carries a rune-bounded projection"
  - "Amendment #177 supersedes the plan's stale metric-neutral quality-ledger update and deleted freshness script"

patterns-established:
  - "Worker watch controls cross the display/shell boundary through a small context controller, not prop drilling"
  - "Queued and terminal swarm rows share one display vocabulary and one row layout"
  - "Private channel verification records structural counts and timing only, never message text or identifiers"

requirements-completed: [SWARM-12, SWARM-10]

coverage:
  - id: D1
    description: "A watched worker renders as a read-only parallel thread and can be switched from the keyboard-accessible picker."
    requirement: SWARM-12
    verification:
      - kind: unit
        ref: "web/src/chat/workers/WorkerPane.test.tsx and WorkerPicker.test.tsx"
        status: pass
      - kind: automated_ui
        ref: "live-check/cockpit/RESULTS.md verdict 4"
        status: pass
    human_judgment: false
  - id: D2
    description: "The chip shows independently updating worker rows from one push status stream, with no polling route requests."
    requirement: SWARM-12
    verification:
      - kind: unit
        ref: "web/src/chat/workers/useWorkerStatuses.test.ts and workerStream.test.ts"
        status: pass
      - kind: automated_ui
        ref: "live-check/cockpit/RESULTS.md verdicts 4 and 6"
        status: pass
    human_judgment: false
  - id: D3
    description: "Each worker produces one card and one full report artifact, while Telegram emits one notification per terminal fan-out and none mid-flight."
    requirement: SWARM-12
    verification:
      - kind: e2e
        ref: "live-check/cockpit/RESULTS.md verdicts 1 through 3"
        status: pass
    human_judgment: false
  - id: D4
    description: "swarm_status supplies a fact-based progress answer containing worker id, status, elapsed duration and recent activity."
    requirement: SWARM-10
    verification:
      - kind: unit
        ref: "internal/agent/tools/swarm_status_test.go"
        status: pass
      - kind: e2e
        ref: "live-check/cockpit/RESULTS.md verdict 5"
        status: pass
    human_judgment: false

duration: ~9h38m
completed: 2026-08-30
status: complete
---

# Phase 51 Plan 12b: Live Worker Cockpit Summary

**Aura now exposes background workers as live read-only cockpit threads and the complete card, artifact, Telegram and progress envelope passes a real local-model drive at 9.9/10.**

## Accomplishments

- Added the mutually exclusive worker right rail and mobile drawer, a picker that supports arrows, Home/End, Enter and Space, and one stable row layout for running and terminal workers.
- Reused the shipped AG-UI reducer and assistant-ui read-only provider, preserving the main chat's tool renderers without adding a composer, branch control, polling loop or dependency.
- Drove two real two-worker fan-outs on the rebuilt stack: per-worker cards and Markdown artifacts, one terminal Telegram notification per fan-out, live tool cards, picker switching and a fact-based `swarm_status` answer all passed.
- Recorded the final six verdicts in the redacted live report and the measured product contract in PRD Amendment #183.

## Performance

- **Duration:** approximately 9h38m, including five measure/fix/rebuild live loops and the device checkpoint
- **Started:** 2026-08-29T23:05:22+02:00
- **Completed:** 2026-08-30T08:43:44+02:00
- **Tasks:** 4
- **Commits:** 24 before this summary
- **Files touched:** 209 including the generated embedded web bundle

## Task Commits

| Task | Commits | Outcome |
|---|---|---|
| 1, pane tracer | `c94b719bb`, `5e281c5c9` | RED then live read-only pane, shell slot and watch controller |
| 2, status rows and picker | `2233ebdcc`, `aff73bd55`, `9dc3ddfb3` | RED then status subscription, picker and row controls; strict E2E selector repaired |
| 3, deploy and live drive | `42efbff30`, `605377187`, `d0c728f75`, `2caedee2a`, `68b178035`, `3b51d00a5`, `ae0593fed`, `011fb16e4`, `4f7a1a6b5`, `e8ba08281`, `82f35670b`, `d6a99e9d2`, `82f853b37`, `1ccc16d24`, `723daa60b`, `e17e7b552`, `33a8e0bb2`, `5a52bda03` | Docker bundle, five measured remediations, 6/6 evidence and device confirmation |
| 4, record measurement | `2ae258aa1` | PRD Amendment #183; quality ledger correctly left unchanged under #177 |

## Decisions Made

- Opening Watch closes Artifacts and opening Artifacts closes Watch; a four-column cockpit is not supported.
- Worker event rendering consumes the same `reduceFrame` mapping as the parent thread and explicitly subscribes to native named SSE records.
- The Telegram unit remains one whole fan-out, backed by the deterministic job key and `steer_queue.fanout_key`; the device proof used aggregate structure and timing only.
- Local `gemma-4-12b` through llama.cpp is the routed model for the live drive. No OpenRouter route was used.

## Deviations From Plan

### Auto-fixed Issues

**1. [Rule 1 - Correctness] Full reports exceeded the steer message boundary**

- **Found during:** Task 3 live drive.
- **Issue:** The artifact persisted, but the same uncapped report made `DeliverReport` retry after the steer store rejected the oversized body.
- **Fix:** Kept the artifact authoritative and rune-capped only the steer projection.
- **Verification:** Oversized-report RED tests, focused swarm tests and successful rebuilt-image fan-outs.
- **Committed in:** `605377187`, `d0c728f75`, `2caedee2a`.

**2. [Rule 1 - Correctness] The queued preview expected a boolean producer field that is numeric**

- **Found during:** Task 3 browser replay.
- **Issue:** JSON decoding failed before the worker display could normalize the valid queued result.
- **Fix:** Decode only the `workers` member consumed by the display and ignore wrapper metadata.
- **Verification:** Public preview RED fixture plus two visible dispatch rows in Playwright.
- **Committed in:** `68b178035`, `3b51d00a5`, `ae0593fed`.

**3. [Rule 1 - Correctness] EventSource clients ignored named AG-UI records**

- **Found during:** Task 3 live pane drive.
- **Issue:** Native EventSource does not deliver named records to the default `message` listener.
- **Fix:** Subscribe to reducer-relevant names and `CUSTOM`, with complete teardown and a compatibility fallback.
- **Verification:** Native-shaped RED tests and live running `shell_exec` plus status updates.
- **Committed in:** `011fb16e4`, `4f7a1a6b5`, `e8ba08281`.

**4. [Rule 1 - Correctness] Terminal EOF selected the error fallback**

- **Found during:** Task 3 terminal replay.
- **Issue:** Browser `error` after a complete terminal stream was treated as transport failure.
- **Fix:** Close successfully after `RUN_FINISHED` or `RUN_ERROR`; retain fallback for pre-terminal errors.
- **Verification:** RED lifecycle pair and complete terminal transcript replay.
- **Committed in:** `82f35670b`, `d6a99e9d2`, `82f853b37`.

**5. [Rule 1 - Correctness] The parent omitted elapsed time from its progress answer**

- **Found during:** Task 3 local-model progress question.
- **Issue:** `swarm_status` returned `elapsed_sec`, but its Deferred guidance did not require that fact in the final answer.
- **Fix:** Require id, status, elapsed duration and one recent activity fact in both discovery layers.
- **Verification:** RED guidance test and repeated live answer-shape assertion.
- **Committed in:** `1ccc16d24`, `723daa60b`, `e17e7b552`.

---

**Total deviations:** 5 correctness defects found and fixed by the real live drive.

**Impact on plan:** Task 3 necessarily touched three Go files despite the plan's pre-drive prohibition. Each edit followed a measured PRD amendment and RED test; leaving the defects in place would have made the blocking acceptance fail. No unrelated product scope was added.

## Issues Encountered

- The full Windows `internal/agent/tools` package still has an unrelated NTFS mode assertion (`0600` observed as `0666`). The focused `swarm_status` suite, WSL race run, `go vet ./...` and `go build ./...` passed.
- The old Task 4 quality-freshness command cannot run because Amendment #177 deleted that prose gate. No current quality-row metric was remeasured, so the snapshot was not rewritten.
- Telegram was inspected through the repository Playwright package attached to the operator's existing local browser. No screenshot, trace, private identifier, message body or session data was retained.

## User Setup Required

None. The model route is already persisted through the cockpit and the rebuilt stack remains running.

## Next Phase Readiness

- Plan 51-12b is complete and satisfies the live `>9.8` Definition of Done.
- Plan 51-08 remains the final phase-level retry/dead-letter and whole-envelope closure before code review and phase verification.

## Self-Check: PASSED

- All four task outcomes and both declared requirements are represented above.
- The final live report is 6/6 at 9.9/10, and its commit precedes Amendment #183.
- Coverage classification reports four auto-covered deliverables and zero schema errors.
- The stale no-Go-edit and quality-freshness criteria are accounted for as measured plan deviations rather than reported as passing.

---
*Phase: 51-durable-delegation*
*Completed: 2026-08-30*
