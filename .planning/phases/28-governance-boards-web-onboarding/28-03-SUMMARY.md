---
phase: 28-governance-boards-web-onboarding
plan: 03
subsystem: ui
tags: [react, cockpit, governance, mcp, skills, scheduler, tanstack-query, i18n, a11y, vitest, stryker, playwright]

# Dependency graph
requires:
  - phase: 28-02
    provides: "the six authenticated GET /api/governance/* reads (mcp list + per-row probe, skills lifecycle + audit, scheduler tasks + paginated runs) with env-secret redaction by construction and per-row probe isolation"
  - phase: 27-graph-explorer
    provides: "the web/src/graph/ workspace template (lazy default-export GraphExplorer, the ViewStatus state machine, isAuthError, the same-origin throwing graphApi, the lg:grid desktop / mobile bottom-sheet idiom, NodeInspector <dl> detail pane) + the modes.ts MODES/LIVE_MODES + AppShell surface-swap precedent + the locked blue token system + contrast-check gate"
provides:
  - "A lazy 'governance' workspace mode (web/src/shell/modes.ts MODES + LIVE_MODES) the AppShell center-swaps when surface === 'governance'"
  - "GovernanceWorkspace: a role=tablist tab strip (MCP/Skills/Scheduler) over a read-only banner, rendering each board lazily"
  - "McpBoard + McpServerDetail: per-row independent live probe (Checking…/Healthy·N tools/Timed out/Error in role=status), redacted env-KEY chips (value never in DOM), a hung row never blanks the list"
  - "SkillsBoard + SkillDetail: four lifecycle sub-tabs (active/pending/archived/audit) as a role=tablist; pending rows have NO run/activate control; audit newest-first"
  - "SchedulerBoard + TaskRunHistory: task rows (cron in mono) → paginated run history with Show-more / Showing X of Y"
  - "BoardLayout (shared master/detail: lg grid, mobile backdrop-dismissable bottom sheet with focus trap/restore) + governanceView (shared loading/empty/error/error-auth contract)"
  - "governanceApi.ts: same-origin throwing fetch for the six reads (non-200 incl. 401 throws Error(HTTP <n>))"
  - "en+it governance i18n bundle (resources.governance.ts) + shell.modes.governance"
affects: [Plan 04 board UI polish, Plan 06 onboarding wizard overlay, Phase 29 governance writes]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Per-row isolated live probe in the UI: each MCP row mounts its OWN TanStack useQuery keyed by server name (McpProbeStatus), so a hung server delays only its row's status line — the static list and sibling rows render independently (T-28-03-05)"
    - "Shared board-state frame (governanceView.BoardStateView + boardStatus): the loading/empty/error/error-auth contract is written ONCE and reused by all three boards (mirrors the GraphExplorer ViewStatus, never duplicated)"
    - "Shared master/detail shell (BoardLayout): desktop lg:grid list|detail, mobile bottom sheet with a matchMedia-gated focus trap (Tab cycle + Escape) that restores focus to the originating row; defensive matchMedia lookup so jsdom/older runtimes skip the trap rather than crash"
    - "Read-only enforcement is structural: pending skill rows render NO run/activate affordance and the boards expose no action control anywhere (the secret VALUE never enters the DOM — only redacted KEY chips)"
    - "Pure-helper extraction for mutation hardening: statusLabel (probe→row status) and scheduleText (task→schedule cell) exported and unit-tested directly so their branch/string mutants are killable without the DOM"

key-files:
  created:
    - web/src/governance/GovernanceWorkspace.tsx
    - web/src/governance/McpBoard.tsx
    - web/src/governance/McpServerDetail.tsx
    - web/src/governance/SkillsBoard.tsx
    - web/src/governance/SkillDetail.tsx
    - web/src/governance/SchedulerBoard.tsx
    - web/src/governance/TaskRunHistory.tsx
    - web/src/governance/BoardLayout.tsx
    - web/src/governance/governanceView.tsx
    - web/src/governance/governanceApi.ts
    - web/src/i18n/resources.governance.ts
    - web/src/governance/__tests__/GovernanceWorkspace.test.tsx
    - web/src/governance/__tests__/McpBoard.test.tsx
    - web/src/governance/__tests__/SkillsBoard.test.tsx
    - web/src/governance/__tests__/SchedulerBoard.test.tsx
    - web/src/governance/__tests__/McpServerDetail.test.tsx
    - web/src/governance/__tests__/SkillDetail.test.tsx
    - web/src/governance/__tests__/governanceApi.test.ts
    - web/src/governance/__tests__/helpers.test.ts
    - web/src/governance/__tests__/BoardLayout.test.tsx
    - web/e2e/governance.spec.ts
  modified:
    - web/src/shell/modes.ts
    - web/src/AppShell.tsx
    - web/src/i18n/resources.ts
    - web/src/test/setup.ts
    - web/stryker.config.json
    - internal/webui/dist (rebuilt embed — new governance chunks)

key-decisions:
  - "The three boards + three detail panes landed in the Task-1 (feat) commit, not the Task-2 (test) commit: GovernanceWorkspace.tsx lazy-imports the boards, so they are a single compile unit — splitting them would have left Task 1 tsc-broken. Task 2 adds their dedicated tests + e2e + the dist rebuild. Every commit is independently tsc-clean."
  - "Per-row probe isolation is implemented as a child component (McpProbeStatus) with its own useQuery rather than one batched query, so the React-Query lifecycle gives each row independent loading/error states — a hung row stays in Checking… while siblings resolve (the direct UI analog of the Plan-02 backend per-row 3s bound)."
  - "A shared BoardLayout + governanceView were extracted (not three copies of the GraphExplorer state/layout blocks) per CLAUDE.md 'reusable code / no god class'; the unused generic MasterList prototype was deleted (boards inline their per-row-customized lists: probe status, pending note, cron cell)."
  - "scron.Task/cron.Run/skills.AuditRow have NO json tags, so their fields serialize PascalCase (ID/Kind/CronExpr/NextRunAt/Status/StartedAt/Summary/CreatedAt/ActorID/SkillName/Action…); the TS interfaces in governanceApi.ts match that exact shape (verified against the Go structs)."
  - "The live Playwright run is UAT-deferred (not faked): the running Docker cockpit serves the pre-change BAKED embed (0 governance references confirmed via curl), a live run needs an image rebuild + container restart (out of scope / forbidden), and launching the native aura.exe webServer is blocked by the host AV directive. The spec is authored, compiles, and lists 12 tests (chromium + mobile-chrome)."

patterns-established:
  - "Governance board shape: a master list (arrow-nav + Enter, 44px rows, accent on selected) + a <dl> detail pane (NodeInspector idiom), wrapped in BoardStateView (state contract) + BoardLayout (responsive master/detail), all strings from resources.governance.ts"
  - "Mutation-hardening playbook for board UI: extract pure logic (status/schedule mappers) for direct unit tests + assert aria-state (aria-selected/tabindex/aria-pressed) + assert tone classNames on status branches"

requirements-completed: [GOV-01, GOV-02, GOV-03]

# Metrics
duration: 1h 16m
completed: 2026-06-20
status: complete
---

# Phase 28 Plan 03: Governance Boards Web Frontend Summary

**A lazy `governance` cockpit workspace mirroring the Phase-27 Graph Explorer — a MCP/Skills/Scheduler role=tablist over the six Plan-02 reads, where each board is a master-list + detail (desktop lg:grid, mobile backdrop-dismissable bottom sheet) with per-row isolated MCP live probes, no-pending-controls skills lifecycle tabs, paginated scheduler run history, the full loading/empty/error/error-auth state contract, en+it copy, and the locked blue WCAG-AA design system.**

## Performance

- **Duration:** 1h 16m
- **Started:** 2026-06-20T11:21:39Z
- **Completed:** 2026-06-20T12:37:39Z
- **Tasks:** 2 (both `type="auto"`)
- **Files created/modified:** 26 (20 created source/test + e2e, 5 modified, 1 rebuilt dist tree)

## Accomplishments

- **Governance workspace mode wired** — `'governance'` added to `MODES` + `LIVE_MODES`; `AppShell` center-swaps a lazy `GovernanceWorkspace` when `surface === 'governance'` (and drops the right runtime rail + chat approval cards for the focused workspace, matching the graph arm). The onboarding wizard remains a separate overlay (Plan 06), not a tab.
- **Three boards + three detail panes** mirroring `web/src/graph/`:
  - **McpBoard / McpServerDetail (GOV-01)** — every server row renders source/trust/runtime/startup/auth with redacted env-KEY chips (`bg-surface-3 font-mono`, value NEVER in DOM); each row fires its OWN probe query → `Checking…` → `Healthy · N tools` / `Timed out` / `Error — {state}` in a `role=status` live region; a hung row resolves only its own status and never blanks the list.
  - **SkillsBoard / SkillDetail (GOV-02)** — four lifecycle sub-tabs (`active/pending/archived/audit`) as a nested `role=tablist`; pending rows render with NO run/activate/install affordance (read-only by construction); the audit tab is a newest-first ledger.
  - **SchedulerBoard / TaskRunHistory (GOV-03)** — task rows (kind/schedule/next-run/status, cron in mono) → paginated, newest-first run history with a `Show more` control + a `Showing {{shown}} of {{total}}` status.
- **Shared primitives** — `governanceView` (the loading/populated/empty/error/error-auth contract written once + `isAuthError`/`boardStatus`) and `BoardLayout` (desktop `lg:grid` master|detail, mobile bottom sheet with a focus trap + Escape-to-close + focus-restore + backdrop-tap dismiss).
- **Data layer** — `governanceApi.ts` copies graphApi's same-origin throwing fetch verbatim (`credentials:'same-origin'`, non-200 incl. 401 → `throw Error("HTTP <n>")`) for all six reads; TanStack Query routes a 401 to the visible auth-error state.
- **i18n** — `resources.governance.ts` (`governanceEn`/`governanceIt`, 60 leaf keys each, verified parity) spread into both languages + `shell.modes.governance`.
- **Quality gates all green** (native Windows node) — see below.

## Task Commits

1. **Task 1: governance mode wiring + workspace shell + data layer + i18n (+ boards as a compile unit)** — `bcd7e9e9` (feat)
2. **Task 2: governance board tests + e2e spec + dist rebuild** — `34c9f30c` (test)

_Note: the three boards + detail panes are in the Task-1 commit (the lazy workspace imports them → single compile unit); Task 2 adds their dedicated test coverage, the e2e spec, and the rebuilt embed dist. Each commit is independently tsc-clean. A parallel Codex spike commit (`075ba1d1`) interleaved between the two; all staging was by explicit pathspec — neither commit contains any non-authored file._

## Quality Gates (recorded numbers)

| Gate | Command | Result |
|------|---------|--------|
| Type-check | `npx tsc --noEmit` | **PASS** (clean) |
| Unit/component tests | `npx vitest run src/governance` | **75 passed / 9 files** |
| Coverage (governance dir) | `vitest run --coverage` (include scoped to `src/governance/**`) | **stmts 96.2% · branch 92.7% · funcs 92.2% · lines 96.0%** (floor 85%) |
| Mutation | `npx stryker run` (`src/governance/**`) | **71.99% killed** (477 killed / 1 timeout / 166 survived; break 70%) |
| Contrast | `node scripts/contrast-check.mjs` | **31/31 WCAG-AA, 0 failures** (1 advisory) |
| Embed dist | `npm run build` | **rebuilt** → `internal/webui/dist` (new GovernanceWorkspace/governanceApi/McpBoard/SkillsBoard/SchedulerBoard chunks), committed |
| E2E (Playwright) | `e2e/governance.spec.ts` | **authored — 12 tests compile + list clean (chromium + mobile-chrome); LIVE RUN UAT-DEFERRED** (see below) |

**Coverage-scoping note:** `vitest.config.ts` declares the coverage `include` as the whole `src/**` tree with global 85% thresholds, so a bare `vitest run --coverage src/governance` reports ~6% (only governance tests ran, whole tree measured) and fails the global gate spuriously. The true per-dir figure is obtained by overriding `--coverage.include='src/governance/**/*.{ts,tsx}' --coverage.exclude='src/governance/**/__tests__/**'`, which reports the 96.2%/92.7%/92.2%/96.0% above and exits 0.

## E2E UAT-Deferral (no-skip-as-green, evidence-based)

`web/e2e/governance.spec.ts` is authored, type-checks, and `playwright test --list` enumerates 12 tests (6 desktop chromium + 6 mobile-chrome) covering: the MCP/Skills/Scheduler tablist roles + read-only banner, the MCP master-list arrow-nav + detail open, the healthy-probe tool count, the scheduler paginated run history, the 44px touch-target floor, the skills lifecycle sub-tabs with no run/activate control, and the mobile bottom-sheet open/backdrop-dismiss.

The **live run could not execute in this environment**, for two independently-blocking reasons (verified, not assumed):
1. The running Docker cockpit (`aura:local`, reachable on `:9080`) serves the embed **baked into the image** — `curl http://127.0.0.1:9080/<main-chunk>` returns **0 governance references**, while the freshly-built `internal/webui/dist/assets/GovernanceWorkspace-*.js` has them. The new governance mode/button does not exist in the served SPA until the image is rebuilt + the container restarted (explicitly out of scope / "do not restart the stack").
2. The Playwright `webServer` launches `../aura serve` — a freshly-compiled Go binary — which is blocked by this host's AV "never run .exe" operator directive.

**To complete the live UAT:** rebuild the `aura` image with the committed dist (or run `aura serve` inside the container/WSL with the new embed) and run `cd web && npx playwright test e2e/governance.spec.ts` (chromium + mobile-chrome). The spec is ready and expected green.

## Files Created/Modified

See `key-files` frontmatter. Highlights:
- `web/src/governance/McpBoard.tsx` (166 LOC) — the static list query + `McpProbeStatus` per-row probe child (isolation) + `statusLabel` (exported pure helper).
- `web/src/governance/BoardLayout.tsx` (139 LOC) — the shared responsive master/detail + mobile focus trap/restore.
- `web/src/governance/governanceApi.ts` (171 LOC) — the six same-origin throwing fetchers + the exact PascalCase response interfaces.
- `web/src/i18n/resources.governance.ts` — en+it bundle (60 keys each, parity-checked).
- `web/stryker.config.json` — added `src/governance/**` to the mutate set (permanent).
- `web/src/test/setup.ts` — added a jsdom `matchMedia` polyfill (desktop default) for the BoardLayout focus-trap gate.

## Decisions Made

See `key-decisions` frontmatter. Most consequential: (1) the boards ship in the Task-1 commit as a compile unit with the lazy workspace; (2) per-row probe isolation via a child-component `useQuery`; (3) the live Playwright run is honestly UAT-deferred with curl evidence rather than faked.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] BoardLayout crashed under a runtime without `window.matchMedia`**
- **Found during:** Task 2 (McpBoard detail test)
- **Issue:** `BoardLayout`'s mobile focus-trap effect called `window.matchMedia(...)` unconditionally; jsdom does not implement it, so opening a detail pane threw `TypeError: window.matchMedia is not a function` (and any embed/runtime lacking it would crash the board).
- **Fix:** Guarded the lookup (`typeof window.matchMedia === 'function' ? … : false`) so a missing implementation skips the mobile-only trap rather than crashing; added a desktop-default `matchMedia` polyfill to the shared `web/src/test/setup.ts` so tests can opt into the mobile path.
- **Files modified:** web/src/governance/BoardLayout.tsx, web/src/test/setup.ts
- **Verification:** McpBoard + BoardLayout tests pass; the BoardLayout test drives both the desktop (no-match) and mobile (match) viewports.
- **Committed in:** 34c9f30c (Task 2 commit)

**2. [Rule 1 - Bug] Redundant dead-branch ternary in SkillsBoard empty heading**
- **Found during:** Task 2 (coverage analysis)
- **Issue:** `emptyHeading={tab === 'audit' ? t('governance.skills.emptyHeading') : t('governance.skills.emptyHeading')}` — both branches returned the same value (an unreachable distinction; the audit tab differs only in the empty BODY copy).
- **Fix:** Collapsed to the single expression `emptyHeading={t('governance.skills.emptyHeading')}` (the `emptyBody` ternary that actually differs — `auditEmpty` vs `emptyBody` — is unchanged).
- **Files modified:** web/src/governance/SkillsBoard.tsx
- **Verification:** tsc + SkillsBoard tests pass; the dead branch is gone (no spurious uncovered branch).
- **Committed in:** 34c9f30c (Task 2 commit)

**3. [Rule 3 - Blocking] Stryker mutate set + scoped run config**
- **Found during:** Task 2 (mutation gate)
- **Issue:** `stryker.config.json` only mutated `src/chat/displays/**` + `src/graph/**`; the governance dir was outside the mutation surface, and a CLI `--mutate` override produced a "no files / no tests" dry-run failure.
- **Fix:** Added `src/governance/**/*.{ts,tsx}` (+ `!__tests__`) to the committed `stryker.config.json` mutate array (permanent). The scoped measurement run used a throwaway `stryker.gov.json` (governance-only mutate, clean array form) which was deleted after measurement; its temp dirs were cleaned and the auto-mutated `.gitignore` line was reverted.
- **Files modified:** web/stryker.config.json
- **Verification:** `npx stryker run` over `src/governance/**` reports 71.99% killed, exit 0 (≥70 break).
- **Committed in:** 34c9f30c (Task 2 commit)

---

**Total deviations:** 3 auto-fixed (2 bugs, 1 blocking tooling). **Impact:** All necessary for correctness + the mutation gate; the matchMedia fix also hardens the component against non-jsdom runtimes lacking it. No scope creep — the public surface matches the plan's artifacts.

## Issues Encountered

- **Coverage-gate scoping:** `vitest run --coverage src/governance` reports ~6% because the config's coverage `include` spans the whole `src/**` tree with global thresholds; the real per-dir figure needs an `--coverage.include` override (documented above). Not a code issue — a config interaction.
- **Stryker vitest `related` mode:** the first scoped attempts warned "Vitest failed to find test files related to mutated files" / "No tests were executed" when the `--mutate` CLI flags didn't combine; resolved by using a dedicated scoped config file with a clean `mutate` array (tests import sources directly, so per-test coverage maps correctly).
- **Parallel Codex session:** spike commits (`5947fc7e`, `075ba1d1`, dir `070-…`) landed around my work; all staging was by explicit pathspec — both commits contain only the declared files (verified: no spike file in either commit).

## User Setup Required

None — no external service configuration required. The boards consume the existing `/api/governance/*` reads behind the inherited `RequireAuth` gate; no new env vars or accounts.

## Next Phase Readiness

- **Plan 04 (board UI polish)** can build on the shipped boards + the shared `BoardLayout`/`governanceView`/`BoardStateView` primitives and the en+it bundle.
- **Plan 06 (onboarding wizard)** is unaffected — it is a separate full-screen overlay (the workspace explicitly does not host it as a tab); the `resources.governance.ts` precedent + the same-origin throwing-fetch idiom transfer directly to `resources.onboarding.ts` / `onboardingApi.ts`.
- **Open item:** the live Playwright run is UAT-deferred (see §E2E UAT-Deferral) — rebuild the cockpit image with the committed dist (or run serve in-container) and run the spec to close it. No blockers for subsequent plans.

## Self-Check: PASSED

- All 20 created source/test files + the e2e spec verified present on disk (`[ -f ]`), plus the rebuilt `internal/webui/dist/assets/GovernanceWorkspace-*.js`.
- Both task commits verified in git history: `bcd7e9e9` (Task 1), `34c9f30c` (Task 2); neither contains any non-authored (spike) file.
- All task `<acceptance_criteria>` re-run and passing: tsc clean; 75 governance tests green; coverage 96.2/92.7/92.2/96.0 (≥85); Stryker 71.99% (≥70); contrast 31/31; e2e 12 tests compile+list (live UAT-deferred with curl evidence); modes.ts + AppShell + governanceApi + resources.governance grep-confirmed; en/it key parity 60/60.
- Plan `<verification>` commands re-run: tsc, vitest coverage (scoped), stryker, contrast all pass; dist rebuilt; the one verification not satisfiable here (live `playwright test`) is documented as UAT-deferred, not skipped-as-green.

---
*Phase: 28-governance-boards-web-onboarding*
*Completed: 2026-06-20*
