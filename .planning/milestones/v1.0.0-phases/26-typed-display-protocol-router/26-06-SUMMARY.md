---
phase: 26-typed-display-protocol-router
plan: 06
subsystem: ui
tags: [react, typescript, source-explorer, citations, replay, playwright, e2e, stryker, a11y, i18n, vitest, dist]

# Dependency graph
requires:
  - phase: 26-typed-display-protocol-router (plan 01)
    provides: "the URL-keyed source Registry + DisplaySource shape the Source Explorer renders read-only"
  - phase: 26-typed-display-protocol-router (plan 02)
    provides: "ExternalStoreChat history-rehydration + snapshotToMessages (the D-06 path the replay e2e proves); DisplayRouter shell + DisplayPagination + DisplayPayload TS types"
  - phase: 26-typed-display-protocol-router (plan 03)
    provides: "projectDisplaySnapshot re-derive (replay==live by construction) — the backend half the replay e2e exercises end-to-end; /api/image-proxy"
  - phase: 26-typed-display-protocol-router (plan 05)
    provides: "CitationBubble onOpenSource(refId) callback contract — the click-through this plan wires to the Source Explorer"
provides:
  - "web/src/chat/displays/SourceExplorerSheet.tsx: read-only fullscreen evidence dossier (Table/Metadata/Configuration, no PATCH/destructive control); focus trap + Esc + 18px Fraunces title"
  - "web/src/chat/displays/SourcesButton.tsx: answer-level 'Sources (N)' affordance (accent, hidden when N=0)"
  - "web/src/chat/displays/SourceExplorerContext.tsx + sourceExplorerControls.ts: ONE shared sheet, two entry points (the button + the citation click-through), one registry (D-13)"
  - "web/src/chat/displays/answerSources.ts + sourceExplorerData.ts: pure aggregate/dedupe + filter/sort/completeness helpers (mutation-tested)"
  - "web/e2e/replay.spec.ts: Playwright spec proving D-06 — typed display renders live, reopen re-derives + re-renders identically"
  - "stryker.config.json: the whole src/chat/displays/ dir in the mutation scope (71.74% killed >= 70%)"
  - "internal/webui/dist: rebuilt embedded bundle (shiki-*.js code-split chunk present; source.* i18n bundled)"
affects: [27-graph-explorer (the read-only Source Explorer is the surface Phase 29 governance-write extends), gsd-verify-work]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Shared-sheet via React context (SourceExplorerProvider + useSourceExplorer): the answer-level button AND the deeply-nested citation chips (across assistant-ui's tools.Fallback render-prop boundary) open ONE sheet over ONE registry without prop-drilling — the context is the seam, the provider owns the single SourceExplorerSheet"
    - "Mount-fresh-per-open inner body (SheetBody): the sheet's view state (tab/query/sort/selected) initializes from props on each open (incl. the citation focusRefId) so there is NO reset-on-open setState effect (react-hooks/set-state-in-effect clean)"
    - "Pure logic off the .tsx (sourceExplorerData.ts / answerSources.ts): filter/sort/completeness + aggregate/dedupe live in their own .ts with exhaustive unit + mutation coverage (the toolStatus.ts/tableData.ts idiom)"
    - "Golden-replay e2e: the replay spec feeds the REAL captured aura.display CUSTOM frame (golden-events.json) through the real in-browser sseAdapter, then asserts replayText === liveText at the rendered-DOM layer (D-06 parity proven, not assumed)"

key-files:
  created:
    - web/src/chat/displays/SourceExplorerSheet.tsx
    - web/src/chat/displays/SourcesButton.tsx
    - web/src/chat/displays/SourceExplorerContext.tsx
    - web/src/chat/displays/sourceExplorerControls.ts
    - web/src/chat/displays/sourceExplorerData.ts
    - web/src/chat/displays/answerSources.ts
    - web/src/chat/ExternalStoreChat_messages.tsx
    - web/src/chat/displays/__tests__/SourceExplorerSheet.test.tsx
    - web/src/chat/displays/__tests__/SourcesButton.test.tsx
    - web/src/chat/displays/__tests__/sourceExplorerData.test.ts
    - web/e2e/replay.spec.ts
  modified:
    - web/src/chat/ExternalStoreChat.tsx
    - web/src/chat/displays/DisplayRouter.tsx
    - web/src/chat/displays/__tests__/DisplayRouter.test.tsx
    - web/src/i18n/resources.display.ts
    - web/stryker.config.json
    - internal/webui/dist

key-decisions:
  - "The shared sheet uses a React context (SourceExplorerProvider) rather than prop-drilling: the citation chips render deep inside assistant-ui's tools.Fallback render-prop, so passing a callback down the JSX tree is impossible without re-plumbing the runtime — the context is the only clean seam for 'one sheet, two openers, one registry' (D-13)"
  - "The replay e2e lives at web/e2e/replay.spec.ts (Playwright testDir=./e2e), NOT web/tests/e2e/ as the plan frontmatter stated — the plan path was wrong; placed where Playwright actually runs it (deviation, documented)"
  - "Stryker scope = src/chat/displays/**/*.{ts,tsx} minus __tests__ and the pure-type types.ts; the directory-level run is 71.74% killed (>= the 70% break threshold). The two presentation-heavy new .tsx (SourceExplorerSheet 58.96%, SourceExplorerContext 62.50%) carry Tailwind-class/i18n-key string-literal survivors; the logic helpers (SourcesButton 100%, sourceExplorerData 78.43%, answerSources 77.42%) carry the real coverage"
  - "ExternalStoreChat.tsx split (refactor-on-touch): adding the provider wrap + AnswerSources pushed it to 621 LOC (> 600 cap); extracted the presentational message renderers into ExternalStoreChat_messages.tsx (621 -> 428 LOC)"

patterns-established:
  - "Source Explorer is READ-ONLY this milestone (D-03/T-26-19): a test asserts the absence of Re-Analyze/Clear/Save/Edit/Apply controls + no form input in Configuration; the governance-write surface is Phase 29"
  - "The answer-level Sources (N) + the citation click-through both call openSources(registry, refId?) on the same context — the citation path passes a focusRefId (opens Metadata for that source), the button path does not (opens the Table)"

requirements-completed: [DISP-03, DISP-05]

# Metrics
duration: ~60min
completed: 2026-06-18
---

# Phase 26 Plan 06: Read-only Source Explorer + Replay E2E (deep-search evidence finale) Summary

**The cockpit now closes the deep-search evidence surface: a READ-ONLY fullscreen Source Explorer (Table sort/search/paginate + Metadata + Configuration, no PATCH/destructive control) reachable two ways over ONE shared registry — an answer-level "Sources (N)" button and a citation chip click-through (the 26-05 onOpenSource callback, threaded through DisplayRouter → ToolFallback → a React context) — plus a Playwright replay e2e that proves D-06 end-to-end (a typed display renders live, then re-renders identically on reopen), the Stryker mutation scope widened to the whole displays/ dir (71.74% killed), and a committed dist rebuild carrying the shiki code-split chunk + the finalized source.* i18n.**

## Performance
- **Duration:** ~60 min
- **Started:** 2026-06-18T20:10:31Z
- **Completed:** 2026-06-18T21:09:16Z
- **Tasks:** 3
- **Files:** 17 source/test/config created or modified + the rebuilt `internal/webui/dist`

## Accomplishments
- **Task 1 (DISP-05 / D-03):** `SourceExplorerSheet` — a read-only fullscreen sheet over the shared `DisplaySource[]` registry. The **Table** view is a native `<table>` with sortable `<th>` headers (aria-sort), a search box (omit-when-valid), a Cited (accent) / Consulted (neutral) status tag per row, and 9-rows/page pagination; clicking a row opens its **Metadata** (read-only ref/type/url/confidence/snippet). The **Configuration** view reports cited/consulted counts read-only. A `text-warning` banner shows when any source is incomplete. Sheet a11y: `role="dialog"` + `aria-modal`, Esc-close, focus trap + restore-on-close, 18px Fraunces section title. A NEGATIVE test asserts NO Re-Analyze/Clear/Save/Edit/Apply control and no form input in the read-only views (T-26-19). Pure logic (`sourceExplorerData.ts`) is unit + mutation tested.
- **Task 2 (DISP-05 / D-13 / D-04):** `SourcesButton` ("Sources (N)", accent, aria "View N sources", hidden when N=0). `SourceExplorerProvider` + `useSourceExplorer` own ONE shared sheet; `AnswerSources` mounts the button on each assistant turn (reads the message's tool parts via `useAuiState`, aggregates+dedupes the per-turn registries via `answerSources.ts`); `ToolFallback` threads `onOpenSource` so a citation chip click opens the SAME sheet focused to that refId. `DisplayRouter` forwards `onOpenSource` to the document + web_result evidence cards. An integration test proves both openers render ONE dialog over one registry.
- **Task 3 (D-06 / mutation / dist):** `web/e2e/replay.spec.ts` proves D-06 end-to-end — a `web_search` turn emits the typed `web_result` display LIVE, then reopen (`GET /threads/{id}/messages`) re-derives + re-renders it IDENTICALLY (asserts the rehydration fetch fired AND `replayText === liveText`). `stryker.config.json` now mutates the whole `src/chat/displays/` dir (71.74% killed >= 70%). `internal/webui/dist` rebuilt from `web/` with the `shiki-*.js` code-split chunk present and the finalized `source.*` i18n bundled.

## Task Commits
1. **Task 1: read-only Source Explorer sheet (Table/Metadata/Configuration)** — `7816551e` (feat)
2. **Task 2: 'Sources (N)' affordance + citation click-through wiring (D-13)** — `e4ae1745` (feat)
3. **Task 3: replay e2e (D-06) + Stryker displays scope + dist rebuild** — `0e183207` (test)

_(A concurrent operator commit `ca3c943e` "chore(brand): swap in new Aura logo" landed between Task 1 and Task 2 — unrelated to this plan; the Task-3 dist rebuild naturally reflects it.)_

## Final Phase Verification State (for /gsd-verify-work)

Every Phase-26 requirement → its green test command:

| Req | Surface | Green command |
|-----|---------|---------------|
| DISP-01 | normalizer + Actions.Display + aura.display CUSTOM + re-derive | `go test ./internal/agent/display/ ./internal/agent/ ./internal/agui/` |
| DISP-02 | DisplayRouter (all 8 cases + default raw card) + reducer attach | `cd web && npm run test -- DisplayRouter sseAdapter` |
| DISP-03 | evidence cards (web_result/document/code) + table + Source Explorer | `cd web && npm run test -- WebResultDisplay DocumentDisplay CodeDisplay TableDisplay SourceExplorerSheet` |
| DISP-04 | system_event safe-label card (8 codes) | `cd web && npm run test -- SystemEventCard` |
| DISP-05 | source registry + Source Explorer + "Sources (N)" | `cd web && npm run test -- SourceExplorerSheet SourcesButton sourceExplorerData` |
| SWARM-01 | swarm_report table (status-from-enum-only) | `cd web && npm run test -- SwarmReportTable` |
| DISP-01/06 (replay) | reopen → rehydrate → display re-renders identically | `cd web && AURA_E2E_ORIGIN=<serve> npx playwright test replay` |
| (mutation gate) | displays dir ≥70% killed | `cd web && npm run mutation` |
| (full frontend gate) | ≥85% Vitest + lint + typecheck + build | `cd web && npm run test && npm run lint && npm run typecheck && npm run build` |

## Quality gates (this plan)
- `npm run typecheck` clean; `npm run lint` clean (jsx-a11y on every control; focus trap; omit-when-valid search).
- Full Vitest **502/502** green, coverage **94.36% stmts / 86.36% branch / 95.73% funcs / 96.22% lines** (≥85% floor held on every metric).
- Stryker (whole `src/chat/displays/`): **71.74% killed >= 70% break** (overall PASS). New surfaces: `SourcesButton.tsx` 100%, `sourceExplorerControls.ts` 100%, `sourceExplorerData.ts` 78.43%, `answerSources.ts` 77.42%, `DisplayRouter.tsx` 78.13%; the two presentation-heavy `.tsx` (`SourceExplorerSheet` 58.96%, `SourceExplorerContext` 62.50%) carry Tailwind-class/i18n-key string-literal survivors only.
- Playwright **replay.spec.ts PASS** (run live against `aura serve` on 127.0.0.1:9091 serving the freshly-rebuilt dist; the full 8-test e2e suite green). The D-06 `replayText === liveText` parity assertion held.
- `npm run contrast` 15/15 pairs pass (new color usages reuse already-validated accent/warning/surface tokens).
- `internal/webui/dist` rebuilt; the `shiki-BM9IzjFr.js` code-split chunk (915 kB) is present and off the critical path.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Split ExternalStoreChat.tsx to satisfy the 600-LOC cap (refactor-on-touch)**
- **Found during:** Task 2 (the SourceExplorerProvider wrap + AnswerSources pushed `ExternalStoreChat.tsx` to 621 LOC; the file-size pre-commit gate blocked the commit).
- **Fix:** Extracted the presentational message renderers (`UserMessage`/`AssistantMessage`/`AnswerSources`/`ToolFallback`) into `web/src/chat/ExternalStoreChat_messages.tsx`; `ExternalStoreChat.tsx` 621→428 LOC. Per CLAUDE.md DEEP REFACTOR ON TOUCH.
- **Files modified:** web/src/chat/ExternalStoreChat.tsx, web/src/chat/ExternalStoreChat_messages.tsx
- **Verification:** file-size hook green; `ExternalStoreChat.test.tsx` 8/8 pass; typecheck + lint clean.
- **Committed in:** `e4ae1745` (Task 2 commit)

**2. [Rule 3 - Blocking] Replay e2e placed at web/e2e/ (the real Playwright testDir), not the plan's web/tests/e2e/**
- **Found during:** Task 3 (the plan frontmatter said `web/tests/e2e/replay.spec.ts`, but `web/playwright.config.ts` has `testDir: './e2e'` and the existing specs live in `web/e2e/`).
- **Fix:** Authored the spec at `web/e2e/replay.spec.ts` so Playwright actually discovers + runs it. `web/tests/` does not exist; placing it there would make `npm run test:e2e` silently never run the spec (a no-skip-as-green violation).
- **Files modified:** web/e2e/replay.spec.ts
- **Verification:** `npx playwright test replay` discovers + runs + PASSES the spec.
- **Committed in:** `0e183207` (Task 3 commit)

**3. [Rule 2 - Missing Critical] Added DisplayRouter case-routing + onOpenSource + context coverage to hold the ≥85% Vitest floor**
- **Found during:** Task 2 (gate verification — adding the onOpenSource forwarding branches + the new context dropped global branch coverage to 84.15%, below the enforced 85% gate).
- **Fix:** Extended `DisplayRouter.test.tsx` to route every per-type case + assert onOpenSource forwarding to the evidence cards; added the SourceExplorerContext close-on-Esc + the NOOP-controller fallback tests; added a dedicated `sourceExplorerData.test.ts` exercising every sort key, the search predicate, completeness, and safeHost; added Tab focus-trap + descending-sort tests to the sheet. Global branch → 86.36%.
- **Files modified:** web/src/chat/displays/__tests__/DisplayRouter.test.tsx, web/src/chat/displays/__tests__/SourcesButton.test.tsx, web/src/chat/displays/__tests__/SourceExplorerSheet.test.tsx, web/src/chat/displays/__tests__/sourceExplorerData.test.ts
- **Verification:** `npm run test` passes with no coverage-threshold error (≥85% every metric).
- **Committed in:** `e4ae1745` / `0e183207`

---

**Total deviations:** 3 auto-fixed (2 blocking — LOC split + e2e path correction, 1 missing-critical coverage). No architectural changes; no scope creep — the read-only Source Explorer, the two open paths over one registry, the replay e2e, the Stryker scope, and the committed dist landed exactly as specified.

## Threat Model Adherence
- **T-26-19 (Source Explorer elevation → mitigate):** READ-ONLY this milestone — a NEGATIVE test asserts the absence of any Re-Analyze/Clear/Save/Edit/Apply control and no form input in the Metadata/Configuration views; the governance-write surface is deferred to Phase 29.
- **T-26-20 (Source rows info disclosure → mitigate):** the sheet renders only the trusted-normalizer registry fields (ref_id/type/title/url/confidence/snippet/cited); no SSRF internals or raw backend error text — the registry is built by the 26-01 normalizer.
- **T-26-21 (citation click-through spoofing → mitigate):** `openSources(registry, refId)` resolves refId against the same registry; a refId not in the registry selects nothing (the sheet shows the "select a source" prompt) — a test asserts a ghost refId focuses no fabricated target.
- **T-26-SC (npm installs → mitigate):** NO new dependency this plan (reuses the committed Radix hovercard + native sheet + the in-tree tokens); slopcheck N/A.

## Known Stubs
None. The Source Explorer renders real registry data, the "Sources (N)" button reads the live aggregated registry, and the replay e2e exercises the real rehydration path. The Metadata/Configuration views are READ-ONLY by design (D-03) — that is the planned scope this milestone, with the governance-write surface explicitly deferred to Phase 29 (documented in 26-UI-SPEC §Out of Scope), not a data stub.

## Threat Flags
None — no new security-relevant surface beyond the plan's `<threat_model>`. The Source Explorer is a pure read-only renderer over the existing trusted registry; no new network endpoint, auth path, or persistence.

## Issues Encountered
- The aura cockpit serves the embedded dist via `//go:embed all:dist`, so proving the replay e2e against THIS plan's freshly-built bundle required rebuilding the `aura` binary (so the embed picked up the new dist) and running a fresh `aura serve` on a dedicated port (127.0.0.1:9091) against the live Postgres+Neo4j stack, then pointing Playwright at it via `AURA_E2E_ORIGIN`. The replay spec mocks all `/api/*` + `/agent/run` + `/threads/*/messages` at the page-network layer, so only the served SPA + auth flow come from the live serve. The temporary serve was stopped after the run; the pre-existing serve on 9080 was left untouched.
- A Windows case-collision (`SourceExplorerContext.tsx` vs an initial `sourceExplorerContext.ts`) forced renaming the hook/context module to `sourceExplorerControls.ts` (the two filenames differed only in casing, which the filesystem treats as identical).

## User Setup Required
None - no external service configuration required.

## Self-Check: PASSED

All 11 created source/test files + the rebuilt dist (incl. the `shiki-BM9IzjFr.js` code-split chunk) exist on disk; all 3 task commits (`7816551e`, `e4ae1745`, `0e183207`) are present in git history. Re-run: `npm run typecheck` clean, `npm run lint` clean, full Vitest 502/502 with coverage ≥85% on every metric, Stryker 71.74% killed (≥70% break) over the whole displays dir, and the Playwright `replay` spec PASSES live against the freshly-rebuilt dist.

## Next Phase Readiness
- Phase 26 is functionally complete across all 6 plans: the typed-display protocol (backend trust boundary + frontend router + all 8 type cards + citations + read-only Source Explorer + replay parity) ships. `/gsd-verify-work` can run the per-requirement commands in the table above.
- The read-only Source Explorer is the exact surface Phase 29 governance-write extends (Re-Analyze/Clear/PATCH); the D-03 read-only posture is the deliberate seam.
- No blockers.

---
*Phase: 26-typed-display-protocol-router*
*Completed: 2026-06-18*
