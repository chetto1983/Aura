---
phase: 42-llm-conversation-compaction
plan: 07
subsystem: operations-ui
tags: [go, react, telegram, ag-ui, accessibility, compaction, i18n]
requires:
  - phase: 42-06
    provides: durable privacy-safe compaction and memory foundations
provides:
  - shared-coordinator CLI, REPL, Telegram, and AG-UI compaction operations
  - accessible checkpoint history, preview, loss distinction, and safe-point restore UX
  - localized React and operator messaging with stable API error codes
affects: [42-08, compaction-rollout, operator-recovery]
tech-stack:
  added: []
  patterns: [single coordinator surface parity, owner-scoped bounded operations, accessible recovery controls]
key-files:
  created: [cmd/aura/chat_compact.go, internal/channels/telegram/compact.go, web/src/conversations/CompactionHistory.tsx, web/src/conversations/useCompactions.ts]
  modified: [internal/agui/conversations_api.go, internal/conversations/store_compaction.go]
key-decisions:
  - "All operator surfaces consume one shadow-only CompactCoordinator; surfaces never duplicate activation policy."
  - "Restore is accepted only at an explicit safe point and is blocked while a model response is in flight."
patterns-established:
  - "Surface parity: compact, history, preview, diff, and restore share the same bounded coordinator outcome vocabulary."
  - "Recovery UX names reversible compaction, L1 offload, and lossy L2.5 distinctly for visual and assistive-technology users."
requirements-completed: [IC-12, IC-14]
coverage:
  - id: D1
    description: Authorized CLI, Telegram, and AG-UI compaction operations share the common coordinator and safe-point rules.
    requirement: IC-12
    verification:
      - kind: integration
        ref: "CGO_ENABLED=1 go test -race ./cmd/aura ./internal/channels/telegram ./internal/agui -run 'Compact|Compaction|Restore|IDOR' -count=1"
        status: pass
    human_judgment: false
  - id: D2
    description: Accessible history distinguishes reversible reduction from loss and supports keyboard preview and restore.
    requirement: IC-14
    verification:
      - kind: automated_ui
        ref: "web/src/conversations/__tests__/CompactionHistory.test.tsx"
        status: pass
      - kind: unit
        ref: "web/src/conversations/__tests__/useCompactions.test.ts"
        status: pass
    human_judgment: false
duration: 25min
completed: 2026-07-13
status: complete
---

# Phase 42 Plan 07: Operator Surfaces and Accessible Recovery Summary

**Owner-gated common-coordinator operations across CLI, Telegram, and AG-UI with an accessible checkpoint history that clearly distinguishes reversible compaction from information loss**

## Performance

- **Duration:** 25 min
- **Started:** 2026-07-13T15:29:00+02:00
- **Completed:** 2026-07-13T15:57:00+02:00
- **Tasks:** 2
- **Files modified:** 19

## Accomplishments

- Added pre-model CLI/REPL and localized Telegram commands for compact, history, preview, diff, and restore through one shared coordinator.
- Added bounded owner-scoped AG-UI routes and a production store adapter for active checkpoint preview and audited restore.
- Added typed React Query operations and keyboard/screen-reader recovery UX with explicit semantic, L1 offload, L2.5 loss, degraded, corrupt, and quarantined states.
- Localized the new UI through existing English/Italian i18next resources, formatted token deltas by locale, and moved Go surface text into catalogs without adding a localization dependency.

## Task Commits

1. **Task 1: Expose common coordinator operations on CLI, REPL, Telegram, and AG-UI** - `4a1a1cf5b`
2. **Task 2: Add accessible compaction history and recovery controls** - `e3ff86004`
3. **Corrective: Localize compaction surfaces and API errors** - `b6cd7d9c3`

## Decisions Made

- Kept rollout shadow-only and projected every manual read/action through `CompactCoordinator` rather than placing policy in adapters.
- Added the smallest store preview projection required for real composition; checkpoint summary data remains bounded and owner-gated before coordinator access.
- Used native buttons, named regions and alerts, focus return, explicit keyboard activation, and reduced-motion styling without adding dependencies.
- Preserved Telegram's fixed-Italian convention, selected CLI catalogs from `LANG`, and exposed machine-readable API error codes for client localization.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Wired the existing durable store to the common coordinator**

- **Found during:** Task 1
- **Issue:** The coordinator interface existed, but no production backend adapter exposed the active checkpoint preview to CLI, Telegram, or AG-UI.
- **Fix:** Added a bounded `PreviewCompaction` store projection and a composition-root adapter; restore continues through the existing audited transaction.
- **Files modified:** `internal/conversations/store_compaction.go`, `cmd/aura/chat_compact.go`, composition-root files
- **Verification:** Full relevant Go package suites and exact WSL/CGO race gate passed.
- **Committed in:** `4a1a1cf5b`

**2. [Rule 2 - Missing Critical] Localized every new user-facing surface**

- **Found during:** Completion audit
- **Issue:** New React and Go operator messages were embedded in presentation logic, and API failures returned English prose.
- **Fix:** Added English/Italian React resources and locale-aware number formatting, Go message catalogs using existing surface conventions, and stable API error codes.
- **Verification:** React locale tests, web lint/typecheck, and the exact WSL/CGO race gate passed.
- **Committed in:** `b6cd7d9c3`

**Total deviations:** 2 auto-fixed missing critical integrations. **Impact:** Required for production-ready operator surfaces; no new dependencies or rollout-policy expansion.

## Issues Encountered

- A focused Vitest invocation passed its four tests but correctly failed the repository-wide coverage threshold because it measured only selected files. The exact full plan gate then passed 1,284 tests with 92.61% statement and 86.63% branch coverage.
- One unrelated full-suite `DocumentsWorkspace` test timed out under parallel load; its isolated rerun passed 4/4 in 3.92 seconds.
- The corrective commit's first hook attempt rejected `resources.ts` at 601 lines. After confirming no hook process remained, imports were compacted to the 600-line cap and the single retry passed all hooks in 142 seconds.

## Known Stubs

None. Empty checkpoint history is a valid no-active-checkpoint state, not mock data.

## Threat Flags

| Flag | File | Description |
|------|------|-------------|
| threat_flag: privileged_restore_endpoint | internal/agui/conversations_api.go | New restore and checkpoint-operation routes mutate the active pointer only after owner scoping and an explicit safe-point declaration. |

## Verification

- WSL/CGO race gate: 3 packages passed with explicit `GATE_OK`.
- Native full relevant Go packages: passed.
- Web lint: passed with zero issues.
- Web typecheck: passed.
- Focused localized history suite: 3/3 passed.
- Full web suite: 158 files / 1,284 tests passed; 92.61% statements, 86.63% branches, 92.71% functions, 94.33% lines.

## User Setup Required

None.

## Next Phase Readiness

- Plan 42-08 can add evaluation/rollout visibility while reusing the stable coordinator outcomes and accessible recovery vocabulary.
- Activation remains shadow-only by design until rollout gates authorize it.

## Self-Check: PASSED

- All four key created files exist.
- Task commits `4a1a1cf5b` and `e3ff86004` exist in history.
- Unrelated `.planning/graphs/` dirt remains unstaged.

---
*Phase: 42-llm-conversation-compaction*
*Completed: 2026-07-13*
