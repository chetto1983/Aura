---
phase: 26-typed-display-protocol-router
plan: 02
subsystem: ui
tags: [react, assistant-ui, sse, display-router, typescript, vite, i18n, a11y, pagination, replay]

# Dependency graph
requires:
  - phase: 26-typed-display-protocol-router (plan 01)
    provides: "display.Payload Go union + aura.display CUSTOM event + URL-keyed source registry — the wire contract this plan mirrors in TS"
  - phase: 25-chat-approval-center
    provides: "the kept-verbatim sseAdapter reducer, ExternalStoreChat tools.Fallback seam + history-rehydration fetch, ToolActivityCard raw card (D-02)"
  - phase: 23-frontend-infrastructure-industrial-foundation
    provides: "committed design tokens (readability refactor), Vitest >=85% gate, react-i18next en/it bundles"
provides:
  - "web/src/chat/displays/types.ts: DisplayPayload TS wire mirror of the Go union + isDisplayPayload() guard"
  - "sseAdapter CUSTOM/aura.display frame: attaches the typed payload to the tool part by toolCallId (live + replay)"
  - "DisplayRouter shell: switch(payload.type) with a default -> raw ToolActivityCard (never null, D-FALLBACK)"
  - "DisplayPagination: in-card 'X-Y of N' + prev/next + per-page, default 3/page, all client-side"
  - "snapshotToMessages: the display-aware MESSAGES_SNAPSHOT -> ThreadMessageLike replay home (D-06)"
affects: [26-04 per-type display cards, 26-05 source explorer + citations, 26-06 image-proxy + dist rebuild]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "DisplayRouter switch(payload.type) with a default -> escaped raw card (NEVER null) — the trust-boundary render rule (HARDEN-08): rich render only for a trusted-normalizer payload"
    - "Additive CUSTOM-frame extension of the kept-verbatim reducer: aura.display attaches by toolCallId; unknown CUSTOM names return state unchanged"
    - "ToolFallback reads the custom .display off the stored part via useAuiState(s => s.part) — survives the external-store identity convertMessage; D-15 progressive swap"
    - "Single snapshot-projection source of truth (snapshotToThreadMessages); snapshotToMessages is the named display-aware re-export, no duplication"

key-files:
  created:
    - web/src/chat/displays/types.ts
    - web/src/chat/displays/snapshotToMessages.ts
    - web/src/chat/displays/DisplayRouter.tsx
    - web/src/chat/displays/DisplayPagination.tsx
    - web/src/chat/displays/__tests__/snapshotToMessages.test.ts
    - web/src/chat/displays/__tests__/DisplayRouter.test.tsx
    - web/src/chat/displays/__tests__/DisplayPagination.test.tsx
  modified:
    - web/src/chat/sseAdapter.ts
    - web/src/chat/ExternalStoreChat.tsx
    - web/src/chat/__tests__/sseAdapter.test.ts
    - web/src/chat/__tests__/ExternalStoreChat.test.tsx
    - web/src/i18n/resources.ts

key-decisions:
  - "Task 1's D-06 rehydration FETCH already shipped (commit 4d248cb4); this plan added the missing .display projection + the named snapshotToMessages home, not a new fetch"
  - "DisplayRouter's default returns the raw ToolActivityCard, NEVER null — explicitly overrides elysia RenderDisplay's `default: return null` (D-FALLBACK / HARDEN-08)"
  - "ToolFallback reads .display via useAuiState(s => s.part), NOT the deprecated useMessagePart — the external-store identity convert keeps our custom field on the part"
  - "Micro-labels use text-[0.75rem] (12px), not the UI-SPEC's stale text-[0.6875rem] (11px) — the committed readability tokens use a 15.5px operator base + an enforced readabilityTokens gate"

patterns-established:
  - "Pattern 1: a typed display upgrades a tool turn in place via the tools.Fallback seam — part.display ? <DisplayRouter/> : <ToolActivityCard/>"
  - "Pattern 2: the DisplayRouter extension point — 26-04/26-05 add `case '<kind>': return <XDisplay payload={payload}/>;` above the default"

requirements-completed: [DISP-01, DISP-02]

# Metrics
duration: ~70min
completed: 2026-06-18
---

# Phase 26 Plan 02: Typed-Display Frontend Spine Summary

**The cockpit now recognizes the backend `aura.display` CUSTOM frame (attaching the typed `DisplayPayload` to a tool part by `toolCallId`, live and on replay), routes it through a `switch(payload.type)` `DisplayRouter` shell that defaults to the escaped raw card (never null), and ships in-card `DisplayPagination` — the Wave-1 foundation the per-type cards (26-04/26-05) slot into.**

## Performance

- **Duration:** ~70 min
- **Started:** 2026-06-18T14:45:00Z
- **Completed:** 2026-06-18T15:55:00Z
- **Tasks:** 3
- **Files modified:** 12 (7 created + 5 modified)

## Accomplishments
- `web/src/chat/displays/types.ts`: the `DisplayPayload` TS wire union field-for-field with the Go `display.Payload` (26-01), per-type interfaces, `DisplayKind`, and an `isDisplayPayload()` runtime guard.
- The kept-verbatim `sseAdapter` reducer extended additively: a `CustomFrame` in the `AguiFrame` union + a `case 'CUSTOM'` that attaches `aura.display` to the tool part by `toolCallId` (tolerant via `ensureTool`); any other CUSTOM name is a no-op. `ToolPart.display?` + a `display`-aware snapshot projection (D-06 replay carries the re-derived payload).
- `DisplayRouter` shell: `switch(payload.type)` with no per-type cases yet and a `default:` returning the escaped raw `ToolActivityCard` — never null (D-FALLBACK / HARDEN-08), the security-critical contract pinned by an XSS-shaped test.
- `DisplayPagination`: ported elysia logic, native-element chrome on the committed tokens — "X–Y of N", prev/next (aria-labels, 44px targets), per-page select (default 3), accent only on the active page, polite live region.
- `ExternalStoreChat` `tools.Fallback` now branches `part.display ? <DisplayRouter/> : <ToolActivityCard/>` via `ToolFallback` (reads the custom `.display` off the stored part; D-15 progressive swap).
- i18n `display.*` + `display.pagination.*` keys in **en + it**. Frontend gate green: typecheck + lint clean, 294 Vitest tests pass, coverage 93.0% stmts / 85.4% branches / 95.0% funcs / 95.1% lines (≥85% floor held).

## Task Commits

Each task was committed atomically (TDD: test + impl folded per task — these pin pure-data + render contracts):

1. **Task 1: History-rehydration display projection + snapshotToMessages mapper (D-06)** - `9cda1a48` (feat)
2. **Task 2: DisplayPayload wire types + sseAdapter CUSTOM/aura.display frame** - `14dc480b` (feat)
3. **Task 3: DisplayRouter shell + DisplayPagination + Fallback branch + i18n** - `5df7ee15` (feat)

## Files Created/Modified
- `web/src/chat/displays/types.ts` - `DisplayPayload` union, per-type interfaces, `DisplayKind`, `isDisplayPayload()`
- `web/src/chat/displays/snapshotToMessages.ts` - the named display-aware MESSAGES_SNAPSHOT → `ThreadMessageLike` replay home (delegates to the single `snapshotToThreadMessages` source of truth)
- `web/src/chat/displays/DisplayRouter.tsx` - `switch(payload.type)` shell; `default:` → raw card (never null); the 26-04/26-05 extension point
- `web/src/chat/displays/DisplayPagination.tsx` - in-card pagination (ported logic, rewritten native chrome on the tokens)
- `web/src/chat/sseAdapter.ts` - `CustomFrame` + `case 'CUSTOM'` attach-by-toolCallId; `ToolPart.display?`; snapshot `.display` forward-compat
- `web/src/chat/ExternalStoreChat.tsx` - `ToolFallback` (the `part.display` branch via `useAuiState`)
- `web/src/i18n/resources.ts` - `display.*` + `display.pagination.*` (en + it)
- `web/src/chat/__tests__/sseAdapter.test.ts` - CUSTOM/aura.display attach + no-op cases + replay error path
- `web/src/chat/__tests__/ExternalStoreChat.test.tsx` - the aura.display routing path
- `web/src/chat/displays/__tests__/*` - snapshotToMessages, DisplayRouter, DisplayPagination tests

## Decisions Made
- See `key-decisions` frontmatter. Headline: the D-06 rehydration *fetch* already existed (commit `4d248cb4`) — research/CONTEXT flagged this as the highest-value missing piece, but the committed cockpit-overhaul layer had already wired it; this plan added the missing `.display` projection + the named home + the reducer recognition, exactly as the must_haves require, rather than re-adding a fetch.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Micro-label font size violated the enforced readability gate**
- **Found during:** Task 3 (DisplayPagination)
- **Issue:** The 26-UI-SPEC specifies 11px micro-labels (`text-[0.6875rem]`) on the assumption of a 13.5px operator base. The committed tree was re-skinned for readability (commit `4d248cb4`): the operator `font-base` is now **15.5px** and `src/__tests__/readabilityTokens.test.ts` is an enforced gate requiring arbitrary-rem text utilities to resolve to ≥11px (`rem * 15.5 ≥ 11` → `rem ≥ 0.71`). `0.6875rem` resolves to 10.66px and FAILED the gate.
- **Fix:** Used `text-[0.75rem]` (12px → 11.6px effective), the shipped post-readability micro-label floor used across `ToolActivityCard`/`RuntimeFooter`.
- **Files modified:** web/src/chat/displays/DisplayPagination.tsx
- **Verification:** `readabilityTokens.test.ts` passes; full Vitest suite green.
- **Committed in:** `5df7ee15` (Task 3 commit)

**2. [Rule 3 - Blocking] useMessagePart is deprecated (lint gate)**
- **Found during:** Task 3 (ToolFallback)
- **Issue:** Reading the custom `.display` off the part via `useMessagePart()` tripped `@typescript-eslint/no-deprecated` (the lint gate is `--max-warnings=0`).
- **Fix:** Switched to `useAuiState((s) => s.part)`, the documented v0.12 replacement.
- **Files modified:** web/src/chat/ExternalStoreChat.tsx
- **Verification:** `npm run lint` clean; the aura.display routing test passes.
- **Committed in:** `5df7ee15` (Task 3 commit)

**3. [Rule 2 - Missing Critical] Added branch-coverage tests to hold the ≥85% Vitest floor**
- **Found during:** Task 3 (gate verification)
- **Issue:** The new `ToolFallback`/`sseAdapter` branches dropped global branch coverage to 84.4%, below the enforced 85% gate.
- **Fix:** Added tests for the aura.display routing path, the replay fetch error path, `isDisplayPayload` primitive rejection, and the orphan-tool-row replay edge — all exercising my new branches. Branches → 85.35%.
- **Files modified:** web/src/chat/__tests__/sseAdapter.test.ts, web/src/chat/__tests__/ExternalStoreChat.test.tsx, web/src/chat/displays/__tests__/snapshotToMessages.test.ts
- **Verification:** `npm run test` passes with no coverage-threshold error.
- **Committed in:** `5df7ee15` (Task 3 commit)

---

**Total deviations:** 3 auto-fixed (1 bug, 1 blocking, 1 missing-critical). **Impact:** all necessary to pass the enforced frontend gates (readability, lint, coverage); no scope creep — the display spine is delivered exactly as the must_haves specify.

## Issues Encountered
- A pre-commit/soft-reset interaction briefly pulled an out-of-scope, parallel-session doc (`docs/superpowers/specs/2026-06-18-...md`) into a stray commit; recovered by soft-resetting to the Task-2 commit and re-committing only the 9 owned Task-3 files. The doc is preserved on disk (untracked) for its owner. Final history is 3 clean atomic task commits.
- `src/__tests__/AppShell.test.tsx` "creates and selects a conversation before sending" is flaky under full-parallel/coverage load (async create+SSE flow timeout); it passes consistently in isolation and is unrelated to this plan's changes.

## Known Stubs
- `DisplayRouter` intentionally has NO per-type `case` branches yet — every payload falls to the raw-card default. This is the planned Wave-1 SHELL; the per-type cards (`web_result`, `document`, `code`, `table`, `chart`, `local_artifact`, `system_event`, `swarm_report`) land in 26-04/26-05. The default is the escaped raw card, so output is never lost — not a data stub.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- The `DisplayPayload` TS type (`web/src/chat/displays/types.ts`) is the frozen contract for 26-04/26-05 per-type cards.
- The `DisplayRouter` extension point is documented in-file: add `case '<kind>': return <XDisplay payload={payload}/>;` above the `default:`. The `default:` raw-card fallback must be preserved.
- `DisplayPagination` is ready to wrap any per-type item list; `snapshotToMessages` carries the re-derived `.display` on replay once 26-03 wires the server-side re-derive at `projectMessages`.
- No blockers.

## Self-Check: PASSED

All 7 created files exist on disk; all 3 task commits (`9cda1a48`, `14dc480b`, `5df7ee15`) are present in git history. Plan verification re-run: `npm run typecheck` clean, `npm run lint` clean, targeted Vitest (snapshotToMessages, sseAdapter, DisplayRouter, DisplayPagination) 54/54 pass, full suite 294/294 with coverage ≥85% on every metric.

---
*Phase: 26-typed-display-protocol-router*
*Completed: 2026-06-18*
