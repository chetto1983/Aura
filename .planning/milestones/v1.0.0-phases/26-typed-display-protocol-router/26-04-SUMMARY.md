---
phase: 26-typed-display-protocol-router
plan: 04
subsystem: ui
tags: [react, typescript, display-router, table, chart, system-event, swarm-report, artifact, a11y, i18n, vitest, stryker]

# Dependency graph
requires:
  - phase: 26-typed-display-protocol-router (plan 02)
    provides: "DisplayRouter switch shell + default raw-card fallback, DisplayPayload TS wire types, DisplayPagination, the DisplayKind per-type slots (table/chart/system/swarm/artifact) this plan renders"
  - phase: 26-typed-display-protocol-router (plan 01)
    provides: "the Go display.Payload union these TS cards mirror field-for-field"
provides:
  - "web/src/chat/displays/TableDisplay.tsx: sortable/filterable/Copy-table/Export-CSV table with in-card row pagination (DISP-03/D-14)"
  - "web/src/chat/displays/ChartDisplay.tsx: zero-dep CSS bars + accessible <table> fallback, NO charting library (D-02)"
  - "web/src/chat/displays/SystemEventCard.tsx: all-8 web/errors.go codes → safe labels, role=status (DISP-04/D-07)"
  - "web/src/chat/displays/SwarmReportTable.tsx: ChildReport summary table + row expand, status-from-enum-only, no mailbox theater (SWARM-01/D-08)"
  - "web/src/chat/displays/LocalArtifactDisplay.tsx: filename + byte size + mono path chip"
  - "web/src/chat/displays/DisplayCardShell.tsx: shared neutral card chrome reused by all five cards"
  - "web/src/chat/displays/{tableData,swarmRow}.ts + useCopyAction.ts: extracted pure helpers (mutation-tested)"
  - "DisplayRouter cases: table, chart, system_event, swarm_report, local_artifact"
affects: [26-05 (web_result/document/code evidence cards + source explorer — adds the remaining router cases), 26-06 (dist rebuild)]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "DisplayCardShell: one shared neutral card chrome (label + meta + actions + semantic left rule) so each typed card stays small and visually a sibling of the raw ToolActivityCard it upgrades"
    - "Pure-logic extraction (tableData.ts / swarmRow.ts / useCopyAction.ts) off the .tsx — the toolStatus.ts idiom — so serialization/sort/status logic is directly unit- and mutation-testable and the components stay component-only (react-refresh/only-export-components)"
    - "i18n feature-bundle split: resources.display.ts holds the whole display.*/systemEvent.*/swarm.* namespace (en+it) so resources.ts stays under the 600-LOC cap as cards add copy"
    - "system_event renders ONLY the classified enum label via a code→i18n-key map; unknown code → generic safe fallback; never system.message/raw-URL (T-26-12)"
    - "swarm status + field-presence are enum/guard functions; the row renders the Status enum label only, never the free-form Error text (SWARM-01)"

key-files:
  created:
    - web/src/chat/displays/TableDisplay.tsx
    - web/src/chat/displays/ChartDisplay.tsx
    - web/src/chat/displays/SystemEventCard.tsx
    - web/src/chat/displays/SwarmReportTable.tsx
    - web/src/chat/displays/LocalArtifactDisplay.tsx
    - web/src/chat/displays/DisplayCardShell.tsx
    - web/src/chat/displays/useCopyAction.ts
    - web/src/chat/displays/tableData.ts
    - web/src/chat/displays/swarmRow.ts
    - web/src/i18n/resources.display.ts
    - web/src/chat/displays/__tests__/TableDisplay.test.tsx
    - web/src/chat/displays/__tests__/ChartDisplay.test.tsx
    - web/src/chat/displays/__tests__/SystemEventCard.test.tsx
    - web/src/chat/displays/__tests__/SwarmReportTable.test.tsx
    - web/src/chat/displays/__tests__/LocalArtifactDisplay.test.tsx
    - web/src/chat/displays/__tests__/useCopyAction.test.ts
    - web/src/chat/displays/__tests__/tableData.test.ts
    - web/src/chat/displays/__tests__/swarmRow.test.ts
  modified:
    - web/src/chat/displays/DisplayRouter.tsx
    - web/src/i18n/resources.ts
    - web/src/__tests__/AppShell.conversation.test.tsx

key-decisions:
  - "TableDisplay keeps a SINGLE native <table> with a windowed <tbody> + a footer that mirrors DisplayPagination's chrome but paginates ROWS — a generic children-paginator (DisplayPagination) can't wrap <tr> inside a <tbody> without breaking table semantics, so row-windowing is local while the prev/next/per-page/count chrome is identical"
  - "Extracted resources.display.ts (i18n feature bundle, en+it) to hold display/systemEvent/swarm/artifact copy — refactor-on-touch keeping resources.ts ≤600 LOC; Task 2/3 keys were pre-staged there in the Task-1 commit"
  - "system_event maps ALL 8 web/errors.go codes (UI-SPEC mapped only 5; RESEARCH flagged the drift) plus an unknown-code generic fallback; severity drives rule+icon+text (never color alone)"
  - "swarm_report reuses the row+expand concept (not the ToolActivityCard component directly) as a real <table>; status/dot/label come from the Status enum only, never the Error string"

patterns-established:
  - "The DisplayRouter extension point now carries 5 of 8 cases; 26-05 adds web_result/document/code above the preserved default raw-card fallback"
  - "Per-type card = DisplayCardShell(label, meta, actions, rule) wrapping a body; pure logic lives in a sibling .ts helper with its own exhaustive test"

requirements-completed: [DISP-02, DISP-04, SWARM-01]

# Metrics
duration: ~65min
completed: 2026-06-18
---

# Phase 26 Plan 04: Typed-Display Data/Status Cards Summary

**The display router now renders five self-contained typed cards — `table` (sort/filter/Copy-table/Export-CSV/paginate), `chart` (zero-dep SVG-style bars + an accessible `<table>` fallback, no charting library), `system_event` (all 8 `web/errors.go` codes → safe labels, never SSRF internals, `role="status"`), `swarm_report` (ChildReport summary table + in-place row expand, status-from-enum-only, no mailbox theater), and `local_artifact` (filename + byte size + mono path chip) — each registered above the preserved D-FALLBACK raw card, with en+it i18n, full a11y, and ≥85% Vitest / ≥70% Stryker on every new surface.**

## Performance

- **Duration:** ~65 min
- **Started:** 2026-06-18T18:08:46Z
- **Completed:** 2026-06-18T19:14:15Z
- **Tasks:** 3 (+ mutation-hardening + flake fix)
- **Files:** 21 (18 created + 3 modified)

## Accomplishments

- **TableDisplay (DISP-03/D-14):** native `<table>` with `<button>` sort headers (toggle asc→desc, `aria-sort`, an `sr-only` announced order), a filter `<input>` (`aria-invalid` omit-when-valid), `Copy table` (TSV) + `Export CSV` (RFC-4180 quote-doubling) controls with the transient `Copied` state, and an in-card row pagination footer at the default 3/page. Numeric/id cells use the mono face; values render React-escaped (T-26-13).
- **ChartDisplay (D-02):** zero-dependency CSS-width bars (decorative, `aria-hidden`) PLUS an accessible `<table>` fallback as the authoritative data path; a no-charting-library assertion reads the source and rejects recharts/uplot/chart.js/d3 imports. Tolerates a label/value length mismatch by zipping to the shorter list.
- **SystemEventCard (DISP-04/D-07):** maps all 8 `web/errors.go` codes (`web_search_unavailable`, `blocked_url`, `unsupported_scheme`, `unsupported_content_type`, `response_too_large`, `timeout`, `http_error`, `extraction_failed`) to safe friendly i18n labels; an unknown code → a generic safe fallback. NEVER renders `system.message` free text or a raw URL (T-26-12). Severity → left-rule color + status icon + status word; `role="status"`, not `alert`.
- **SwarmReportTable (SWARM-01/D-08):** a `<table>` over `payload.swarm` (`# / Worker / Status / Summary`), one row per `ChildReport`, with in-place row-expand to summary + error (+ question/options for a needs-input child). Status dot+label come from the `Status` enum only; an out-of-enum status falls to a danger dot + "Unknown". No mailbox/inter-agent-chat affordance (a negative test asserts absence).
- **LocalArtifactDisplay:** filename + human byte size (B/KB/MB/GB) + a mono path chip; render-only (no fetch/download this phase).
- **DisplayRouter:** five new `case` branches above the preserved `default:` raw card.
- **i18n:** the whole `display.*` / `systemEvent.*` / `swarm.*` / `display.artifact.*` namespace in **en + it** (extracted to `resources.display.ts`).
- **Gates:** `npm run typecheck` clean, `npm run lint` clean (jsx-a11y), full Vitest **379/379** with coverage **93.5% stmts / 85.9% branch / 95.8% funcs / 95.4% lines** (≥85% floor held); the `src/chat/displays` directory is **95.6% / 89.1% / 97.2% / 96.1%**.

## DisplayRouter case list (for 26-05 — avoid collision)

This plan added: `case 'table'`, `case 'chart'`, `case 'system_event'`, `case 'swarm_report'`, `case 'local_artifact'`. **26-05 adds the remaining three** — `web_result`, `document`, `code` — above the preserved `default:` raw-card fallback.

## Shared display-chrome helper extracted

- **`DisplayCardShell.tsx`** — the shared neutral card chrome (`label` + `meta` + `actions` + a semantic left-rule) reused by all five cards (and available to 26-05). Per-type bodies slot inside; accent stays scarce (the shell is neutral surface/border/text).
- **`useCopyAction.ts`** — best-effort clipboard + transient `Copied` (shared by Copy/CSV; reusable by the 26-05 Copy-code control).
- **`tableData.ts` / `swarmRow.ts`** — pure serialization/sort and status/field-presence helpers (the toolStatus.ts idiom).

## Task Commits

1. **Task 1: TableDisplay + ChartDisplay (+ i18n split)** — `9e3d2d3d` (feat)
2. **Task 2: SystemEventCard** — `c0a15e13` (feat)
3. **Task 3: SwarmReportTable + LocalArtifactDisplay** — `34d1bf36` (feat)
4. **useCopyAction branch coverage** — `a4044fd5` (test)
5. **tableData extraction + TableDisplay mutation ≥70%** — `3d015669` (test)
6. **swarmRow extraction + SwarmReportTable mutation ≥70%** — `8cc35b8a` (test)
7. **De-flake AppShell.conversation** — `dbdef3a8` (test)

## Mutation (Stryker ≥70% killed — operator directive)

Every new surface clears the 70% break threshold:

| File | Mutation score |
|------|----------------|
| swarmRow.ts | 100% |
| tableData.ts | 92.6% |
| useCopyAction.ts | ~94% |
| SystemEventCard.tsx | 78.9% |
| LocalArtifactDisplay.tsx | 77.8% |
| ChartDisplay.tsx | 73.7% |
| SwarmReportTable.tsx | 72.7% |
| TableDisplay.tsx | 70.3% |

The remaining survivors on the presentation `.tsx` files are Tailwind class-string and i18n-key string-literal mutants — asserting exact class strings is brittle and discouraged; all logic-bearing helpers are at ≥92%.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] resources.ts exceeded the 600-LOC cap after adding i18n keys**
- **Found during:** Task 1 (pre-commit file-size hook)
- **Issue:** Adding the `display.table.*`/`display.chart.*` keys pushed `resources.ts` to 612 LOC, tripping the CLAUDE.md "no god class" cap.
- **Fix:** Extracted the entire `display.*` i18n feature bundle (en+it) into `resources.display.ts` (spread into each language's `translation`); pre-staged the Task 2/3 `systemEvent.*`/`swarm.*`/`display.artifact.*` keys there too. `resources.ts` → 526 LOC.
- **Files:** web/src/i18n/resources.ts, web/src/i18n/resources.display.ts
- **Commit:** `9e3d2d3d`

**2. [Rule 1 - Bug] react-hooks/exhaustive-deps on the table filter/sort memo**
- **Found during:** Task 1 (lint)
- **Issue:** `payload.table?.rows ?? []` created a fresh array reference each render, so the `useMemo` deps changed every render.
- **Fix:** Memoized the derived `columns`/`rows` on `payload.table`.
- **Files:** web/src/chat/displays/TableDisplay.tsx
- **Commit:** `9e3d2d3d`

**3. [Rule 2 - Missing Critical] Mutation < 70% on TableDisplay + SwarmReportTable**
- **Found during:** Gate verification (Stryker)
- **Issue:** The two logic-heavy components were at 61.8% / 58.1% killed, below the operator's ≥70% Stryker directive.
- **Fix:** Extracted pure logic (`tableData.ts` CSV/TSV/sort/filter; `swarmRow.ts` status/field-presence) and added exhaustive unit tests + targeted interaction assertions; dropped a redundant double-guard. Result: both `.tsx` ≥70%, the helpers ≥92%.
- **Files:** tableData.ts, swarmRow.ts, TableDisplay.tsx, SwarmReportTable.tsx, + tests
- **Commits:** `3d015669`, `8cc35b8a`

**4. [Rule 1 - Bug] Pre-existing flaky AppShell.conversation test (operator directive: fix, don't leave)**
- **Found during:** Full-suite coverage runs (flaked intermittently on different rows)
- **Issue:** The full-AppShell-mount tests raced the default 1000ms RTL async-wait + 5000ms vitest test timeout under parallel coverage CPU load (documented in 26-02 as a known flake). The operator instructed: "if a test is flaky, refactor — don't leave shit on the repo."
- **Fix:** Raised the file-level `asyncUtilTimeout` (restored in `afterAll`, no leak) AND gave each `it()` an explicit 10s per-test timeout via the third arg (a runtime `vi.setConfig` in `beforeAll` does NOT re-time an already-scheduled test). Verified green 6/6 consecutive full-coverage runs.
- **Files:** web/src/__tests__/AppShell.conversation.test.tsx
- **Commit:** `dbdef3a8`

---

**Total deviations:** 4 auto-fixed (2 bugs, 1 blocking, 1 missing-critical). No scope creep — all four were required to pass the enforced gates (file-size, lint, mutation, flake-free CI).

## Threat Model Adherence

- **T-26-12 (SystemEventCard info disclosure → mitigate):** the card renders ONLY the classified `web/errors.go` enum label (full 8-code map) via i18n; an unknown code → a generic safe fallback; `system.message`/raw-URL is never rendered. A test asserts a leaky message (with `169.254.169.254` + an internal host) is absent from the DOM.
- **T-26-13 (Table/Swarm tampering → mitigate):** all cells render as React-escaped text; no markdown/HTML; no `dangerouslySetInnerHTML`.
- **T-26-14 (Swarm info disclosure → accept):** status labels come from the `Status` enum only; the per-child `.jsonl` transcript stays deferred (not web-reachable).
- **T-26-SC (npm installs → mitigate):** NO new dependency added (native elements + zero-dep bars + committed tokens).

## Known Stubs

None. All five cards render real data from their payload slots; `local_artifact` is intentionally render-only this phase (no download endpoint until a later phase), which is the planned scope, not a data stub.

## Issues Encountered

- The pre-existing `AppShell.conversation.test.tsx` flake (carried from 26-02) surfaced under this plan's added coverage load and was fixed (deviation #4) per the operator's directive — the repo no longer carries a flaky test.

## User Setup Required

None.

## Self-Check: PASSED

All 18 created source/test files + the 2 modified-file edits exist on disk; all 7 task/quality commits (`9e3d2d3d`, `c0a15e13`, `34d1bf36`, `a4044fd5`, `3d015669`, `8cc35b8a`, `dbdef3a8`) are present in git history; all 5 new `case` branches are in `DisplayRouter.tsx`. Re-run: `npm run typecheck` clean, `npm run lint` clean, full Vitest 379/379 green with coverage ≥85% on every metric, Stryker ≥70% on every new surface.

---
*Phase: 26-typed-display-protocol-router*
*Completed: 2026-06-18*
