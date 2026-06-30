---
phase: 32-quality-cleanup-dead-code-shared-helpers
plan: 08
subsystem: frontend-dedup
tags: [qual-03, d-08, frontend, dedup, getjson, focustrap, skeleton, a11y, dist, playwright]

# Dependency graph
requires:
  - phase: 32-04
    provides: "prior web wave settled + last rebuilt internal/webui/dist baseline (D-14 sequential ordering)"
provides:
  - "getJSON has ONE source (web/src/api/json.ts); the byte-identical copies in useConversations.ts + governanceApi.ts are deleted and import the canonical helper; pimApi.ts (the lone external importer of governanceApi.getJSON) repointed to ../api/json"
  - "BoardLayout + McpLifecycleCluster RemoveDialog adopt the canonical web/src/a11y/focusTrap.ts (focusFirstDescendant/trapTabKey), fixing the inline copies' a11y defects (BoardLayout: no behavioural change since it already filtered disabled; McpLifecycleCluster: button-only query → full focusable selector)"
  - "ONE skeleton system: the rich components/skeleton/Skeleton.tsx (SkeletonBlock); the 3 shadcn consumers (ConversationSidebar, SearchPanel, governanceView) migrated; web/src/components/ui/skeleton.tsx retired"
  - "internal/webui/dist rebuilt + committed with each web commit (web-dist-freshness green)"
affects: [web, cockpit, governance, conversations, a11y, dist]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Shared HTTP read helper: all GET callers import { getJSON } from web/src/api/json.ts (single source); governanceApi keeps its own 204-aware sendJSON/postJSON/patchJSON/deleteJSON write layer (NOT folded — api/json.postJSON does not coerce 204)."
    - "Canonical focus trap: native keydown listener does Escape inline, then delegates Tab cycling to trapTabKey(event, root); initial focus via focusFirstDescendant(root) (or a deliberate safe-action ref where the dialog wants the non-destructive default, e.g. RemoveDialog's Keep server)."
    - "One skeleton system (A4): map shadcn className sizing to the rich SkeletonBlock props (h-4→height='1rem', h-12→'3rem', h-14→'3.5rem', w-3/4→width='75%', rounded-md→radius='md'); drop bg-* overrides — the .skeleton-block CSS-wave token set carries the surface."
    - "Dist rebuild discipline (Pitfall 3): `git commit --only` does NOT stage untracked new dist assets; stage the new hashed files explicitly (git add --pathspec-from-file, all under internal/webui/dist) BEFORE committing, then verify `git status --porcelain internal/webui/dist` is empty."

key-files:
  created:
    - .planning/phases/32-quality-cleanup-dead-code-shared-helpers/32-08-SUMMARY.md
  modified:
    - web/src/conversations/useConversations.ts
    - web/src/governance/governanceApi.ts
    - web/src/governance/pimApi.ts
    - web/src/governance/BoardLayout.tsx
    - web/src/governance/McpLifecycleCluster.tsx
    - web/src/conversations/ConversationSidebar.tsx
    - web/src/conversations/SearchPanel.tsx
    - web/src/governance/governanceView.tsx
    - web/src/governance/__tests__/BoardStateView.test.tsx
    - internal/webui/dist
  deleted:
    - web/src/components/ui/skeleton.tsx

key-decisions:
  - "pimApi.ts repoint is REQUIRED, not optional (Rule 3 / plan action): it was the only external importer of governanceApi.getJSON; once governanceApi stops re-exporting getJSON, pimApi must import getJSON from ../api/json (its postJSON/deleteJSON/boolValue/stringValue stay on governanceApi — those are the 204-aware write helpers, distinct from api/json's)."
  - "createConversation folded to postJSON (QA-D-04) sends `{}` for the empty-title case (always a string body) rather than the old bodyless POST; the Go handler (conversations_api.go:126) ignores io.EOF and normalizeCreateTitle('')='' → untitled, so `{}`, `{title:''}`, and no-body are all equivalent — and a string body keeps the AppShell create-flow mock's `typeof init.body==='string'` guard satisfied."
  - "Adopting the canonical focusTrap is the INTENDED a11y fix, not strict byte-parity (plan prohibition): McpLifecycleCluster's inline trap queried only `button` (missed inputs/links/[tabindex]); trapTabKey uses the full focusable selector with the disabled filter."
  - "BoardStateView.test.tsx updated to assert `.skeleton-block` (the rich marker) instead of the retired shadcn `[data-slot=\"skeleton\"]` — a legitimate test rewrite tracking the migration (CLAUDE.md: rewrite-with-justification when the test asserts a retired implementation), NOT gaming; the loading contract (≥1 skeleton under role=status) is still verified."
  - "Playwright NOT deferred: ran on the host against a rebuilt aura:local container serving the fresh dist (external-serve mode, no Windows .exe) — migrated-surface specs 11/11 green; full chromium 35/37 with the 2 failures (displays document-markdown, chat history) both passing in isolation = parallel-load flake, outside the changed surface."

requirements-completed: []  # QUAL-03 partial — orchestrator/verifier owns the flip.

# Coverage metadata (#1602)
coverage:
  - id: D1
    description: "getJSON has a single source; useConversations.ts + governanceApi.ts import the canonical web/src/api/json.ts; pimApi.ts repointed; no copies remain."
    requirement: "QUAL-03"
    verification:
      - kind: unit
        ref: "rg 'function getJSON|const getJSON' web/src/conversations/useConversations.ts web/src/governance/governanceApi.ts → none; rg \"from '../api/json'\" shows both; npm test (922 tests) green; useConversations.ts 100%, governanceApi.ts 96.87%"
        status: pass
      - kind: e2e
        ref: "playwright chromium chat.spec.ts (conversation list/history over useConversations.getJSON) green vs container serving fresh dist"
        status: pass
    human_judgment: false
  - id: D2
    description: "BoardLayout + McpLifecycleCluster adopt the canonical focusTrap (inline traps removed); consumer tests stay green; the bottom-sheet trap is exercised end-to-end."
    requirement: "QUAL-03"
    verification:
      - kind: unit
        ref: "BoardLayout.test.tsx (Tab/Shift+Tab wrap, Escape+restore, mid-trap no-wrap) + McpLifecycleCluster.test.tsx (Remove dialog Escape+focus) green; BoardLayout 95.65%, McpLifecycleCluster 94.44%"
        status: pass
      - kind: e2e
        ref: "playwright governance.spec.ts:235 'selected MCP row opens a backdrop-dismissable bottom sheet on mobile' green (drives BoardLayout's refactored focusFirstDescendant/trapTabKey)"
        status: pass
    human_judgment: false
  - id: D3
    description: "One skeleton system: 3 shadcn consumers migrated to the rich SkeletonBlock; web/src/components/ui/skeleton.tsx deleted; no references remain; knip clean."
    requirement: "QUAL-03"
    verification:
      - kind: unit
        ref: "rg 'components/ui/skeleton' web/src/ → none; BoardStateView.test.tsx asserts .skeleton-block under role=status; npm run deadcode (knip) exit 0; SearchPanel 100%, ConversationSidebar 90.62%, governanceView in src/governance 94.06%"
        status: pass
      - kind: e2e
        ref: "playwright governance.spec.ts (governanceView loading→populated) + chat.spec.ts (ConversationSidebar) green vs fresh dist; no runtime error from the SkeletonBlock swap"
        status: pass
    human_judgment: false

# Metrics
duration: ~55min (sequential, no worktree; concurrent-Codex isolation held; incl. container rebuild + host Playwright)
completed: 2026-06-30
status: complete
---

# Phase 32 Plan 08: QUAL-03 Frontend Dedup (getJSON / focusTrap / Skeleton) Summary

**Collapsed the cockpit's highest-visual-regression duplication into single sources, fixing the latent a11y defects the inline copies carried, with the fresh `internal/webui/dist` rebuilt+committed per concept and the migration validated end-to-end by host Playwright against a rebuilt container: (1) `getJSON` (QA-D-01) — the two byte-identical copies in `useConversations.ts` and `governanceApi.ts` are deleted and both import the canonical `web/src/api/json.ts`; `pimApi.ts` (the only external importer of `governanceApi.getJSON`) is repointed to `../api/json`, and `createConversation`'s hand-rolled POST is folded to `postJSON` (QA-D-04); (2) `focusTrap` (QA-D-02) — the inline traps in `BoardLayout.tsx` and `McpLifecycleCluster.tsx`'s `RemoveDialog` are replaced by the canonical `focusFirstDescendant`/`trapTabKey`, fixing McpLifecycleCluster's button-only focusable query (it now traps inputs/links/[tabindex] too); (3) skeleton unification (QA-D-08 / D-08) — the rich `components/skeleton/Skeleton.tsx` is kept, the 3 shadcn consumers (`ConversationSidebar`, `SearchPanel`, `governanceView`) are migrated to `SkeletonBlock`, and `components/ui/skeleton.tsx` is retired. `npm test` (110 files / 922 tests), `npm run lint`, `npm run deadcode` (knip), and `npm run build` all exit 0; every touched per-file vitest coverage is ≥85%. Two atomic code commits (each riding its dist rebuild) + this doc.**

## Accomplishments

- **Task 1 — getJSON dedup + canonical focusTrap (commit `a1e1a29b`):**
  - Deleted the local `getJSON` from `useConversations.ts` (was :61-70) and `governanceApi.ts` (was :113-122); both now `import { getJSON } from '../api/json'` (governanceApi also adds the import; its 204-aware `sendJSON`/`postJSON`/`patchJSON`/`deleteJSON` write layer is untouched).
  - Repointed `pimApi.ts` — `getJSON` now from `../api/json`; `boolValue`/`deleteJSON`/`postJSON`/`stringValue` stay on `./governanceApi`.
  - Folded `createConversation` to `postJSON<Conversation>('/api/conversations', title.length>0 ? {title} : {})` (QA-D-04) — always a string body; backend-equivalent to the old two-branch POST.
  - Replaced the inline focus traps: `BoardLayout.tsx` (mobile bottom sheet) and `McpLifecycleCluster.tsx`'s `RemoveDialog` now use `focusFirstDescendant`/`trapTabKey` (BoardLayout) and `trapTabKey` (RemoveDialog keeps its deliberate `cancelRef` safe-default focus). Inline `focusables()` helper and the button-only `querySelectorAll('button')` trap removed.
  - Rebuilt + committed `internal/webui/dist`.
- **Task 2 — skeleton unification (commit `bb3aff2a`):**
  - Migrated `ConversationSidebar` (`h-4 w-3/4` / `h-3 w-1/2`), `SearchPanel` (`h-14 rounded-md` ×2), and `governanceView` (`h-12 max-w-{xl,lg,2xl}` ×3) from `<Skeleton/>` to the rich `<SkeletonBlock/>` with dimension-preserving props; dropped the redundant `bg-surface-3` overrides (the `.skeleton-block` CSS-wave carries the surface).
  - Deleted `web/src/components/ui/skeleton.tsx`; `rg 'components/ui/skeleton' web/src/` → nothing.
  - Updated `BoardStateView.test.tsx` to assert the rich `.skeleton-block` marker (was the retired shadcn `[data-slot="skeleton"]`).
  - Rebuilt + committed `internal/webui/dist`.

## Task Commits

Each concept committed atomically (D-11), direct `git commit -o` with explicit `--only` pathspecs to stay isolated from the concurrent Codex session on master. `git show --stat` confirmed each commit lists ONLY this plan's files (zero `internal/agui/**` non-dist, `internal/objectstore/**`, `.planning/graphs/**`, `docs/**`, or other-`web/src/**` swept in):

1. **Task 1 — getJSON + focusTrap** — `a1e1a29b` `refactor(32-08): dedup getJSON to api/json + adopt canonical focusTrap` (5 source: `useConversations.ts`, `governanceApi.ts`, `pimApi.ts`, `BoardLayout.tsx`, `McpLifecycleCluster.tsx` + full `internal/webui/dist`).
2. **Task 2 — skeleton unification** — `bb3aff2a` `refactor(32-08): unify skeletons on the rich SkeletonBlock; retire shadcn ui/skeleton` (4 source: `ConversationSidebar.tsx`, `SearchPanel.tsx`, `governanceView.tsx`, `BoardStateView.test.tsx` + deleted `components/ui/skeleton.tsx` + full `internal/webui/dist`).
3. **Doc** — this SUMMARY (plan-metadata commit).

_Codex's `docs: design product document library ux` (`4df9c1ee`) landed between the two; it was never staged by this executor and is untouched._

## Web Gate Results

| Gate | Result |
|------|--------|
| `npm test` (vitest --coverage) | **922 passed / 110 files**, exit 0 (both tasks) |
| `npm run lint` (eslint --max-warnings=0) | exit 0 |
| `npm run deadcode` (knip) | exit 0 — no orphaned getJSON/focusTrap/ui-skeleton |
| `npm run build` | exit 0 — `internal/webui/dist` regenerated each task |
| Per-file vitest coverage (touched, all ≥85%) | useConversations.ts **100%**, SearchPanel.tsx **100%**, ConversationSidebar.tsx **90.62%**, governanceApi.ts **96.87%**, BoardLayout.tsx **95.65%**, McpLifecycleCluster.tsx **94.44%**, src/governance dir (incl. governanceView) **94.06%** |
| Suite totals | Statements **91.02%**, Branches **85.26%**, Functions **90.92%**, Lines **92.97%** |

## Playwright — host run against the rebuilt container (user-directed)

Per the operator's request ("use container update and playwright on host"), the Playwright check was **run, not deferred**:

1. **Container update:** `docker compose build aura` rebuilt `aura:local` (the `webbuild` Dockerfile stage rebuilds the SPA from `web/` source → embeds the fresh dist via `go:embed`), then `docker compose up -d aura` recreated the container. Verified it serves the fresh bytes — `/login` references `assets/index-wqtzu6tg.js` (== committed `internal/webui/dist/index.html`), the fresh asset returns 200, container healthy. No Windows `.exe` was run (build inside Docker).
2. **Host Playwright** (browsers installed on host, `AURA_E2E_ORIGIN=http://127.0.0.1:9080` external-serve mode → no managed `aura serve`):
   - **Migrated-surface specs: 11/11 PASSED** — `governance.spec.ts` (7; governanceView loading→populated, MCP detail = McpLifecycleCluster, and `:235` mobile bottom-sheet = BoardLayout's refactored trap), `chat.spec.ts` (2; ConversationSidebar + persisted history over `useConversations.getJSON`), `shell.spec.ts` (3).
   - **Full chromium project: 35/37** — the 2 failures (`displays.spec.ts:199` document-markdown sanitization, `chat.spec.ts:227` persisted history) **both pass in isolation** (`--workers=1` → 2/2 green in 3.8s); they are parallel-load flake against the single shared live backend (8 workers), and both are **outside this plan's changed surface** (document rendering; and a test that passed in the focused run). No regression attributable to the dedup/migration.

The plan's literal `npx playwright test --grep skeleton` matches **0 tests** (no skeleton-tagged spec exists); the migrated-surface specs above are the meaningful end-to-end evidence instead.

## Decisions Made

- **getJSON is the only shared piece; the write layers stay separate.** `governanceApi`'s `postJSON`/`patchJSON`/`deleteJSON` coerce 204→`{}` (the MCP/skills remove/restore routes return 204); `api/json`'s `postJSON` does not. So only `getJSON` (byte-identical everywhere) was collapsed; `useConversations.mutate` (its own 204 helper) was likewise left alone — scope control.
- **createConversation empty-title → `{}`.** Backend-equivalent (handler ignores `io.EOF` and treats `''` title as untitled) and always a string body, so no consumer mock that asserts a JSON string body can break.
- **focusTrap adoption fixes a11y by design.** RemoveDialog's old trap only saw `<button>`; the canonical `trapTabKey` uses the full focusable selector + disabled filter. BoardLayout already filtered disabled, so its behaviour is unchanged — the win there is the dedup (one util, exercised by the existing BoardLayout/Drawer/SourceExplorerSheet suites).
- **Test rewrite over test-deletion for BoardStateView.** The loading-state contract is preserved; only the implementation marker asserted changed (`data-slot="skeleton"` → `.skeleton-block`).

## Deviations from Plan

**1. [Rule 3 — Blocking] `pimApi.ts` repoint added to Task 1 (not in the plan's `files_modified`)**
- **Found during:** Task 1 — `pimApi.ts` was the only external importer of `governanceApi.getJSON`. Once `governanceApi` stops defining/re-exporting `getJSON`, `pimApi` fails to compile.
- **Fix:** Split its import — `getJSON` from `../api/json`; the rest (`boolValue`, `deleteJSON`, `postJSON`, `stringValue`) stays on `./governanceApi`. This is exactly the plan's `<action>` instruction ("Repoint anything that imported getJSON FROM governanceApi to ../api/json"); the file was simply not enumerated in frontmatter.
- **Impact:** Required for the build; no behaviour change (canonical getJSON is byte-identical). Committed in `a1e1a29b`.

**2. [Rule 1 — Test fix] `BoardStateView.test.tsx` updated to assert the rich skeleton marker**
- **Found during:** Task 2 — `npm test` went 1-red: the test queried `[data-slot="skeleton"]` (the retired shadcn marker), which `SkeletonBlock` does not emit.
- **Fix:** Asserted `.skeleton-block` (the rich marker) instead; title trimmed `shadcn ` → `skeleton blocks`. The retired-implementation assertion is the broken part (CLAUDE.md sanctions a justified rewrite); the loading contract (≥1 skeleton under `role=status`) is unchanged. Committed in `bb3aff2a`.

**3. [Process] Dist commit method — `git commit --only <dir>` does not stage untracked new assets**
- **Found during:** Task 1 commit — the first `git commit -o -- internal/webui/dist` captured the 30 deletions + 2 mods but skipped the 36 **untracked** new hashed assets, leaving an incomplete dist (would fail web-dist-freshness).
- **Fix:** Staged the new files explicitly (all under `internal/webui/dist/`) then amended (Task 1) / pre-staged before `git commit -o --pathspec-from-file` (Task 2). Both commits verified with `git status --porcelain internal/webui/dist` → 0 leftover. Recorded as a standing pattern (tech-stack patterns) so future dist commits stage new assets first.

---
**Total deviations:** 3 (1 Rule-3 blocking repoint, 1 Rule-1 test fix, 1 process note). No Rule 4 architectural decisions arose. No production-logic behaviour change beyond the intended a11y fix.

## Threat Model Disposition

- **T-32-08-A11Y (mitigate):** canonical focusTrap adopted; BoardLayout/McpLifecycleCluster consumer tests green + governance.spec.ts:235 bottom-sheet e2e green. ✔
- **T-32-08-DIST (mitigate):** dist rebuilt+committed per web commit; 0 leftover dist changes after each; container serves the committed bytes. ✔
- **T-32-08-VIS (mitigate):** rich-system migration (A4); host Playwright on the migrated views green vs the fresh dist (the documented A4 reverse-choice keeps the richer skeleton). ✔
- **T-32-SC (accept):** no package installs this plan — cleanup-only. ✔
- **No new threat surface:** the changes remove client code and re-point an HTTP read helper to its canonical twin; no new endpoints, auth paths, or trust boundaries.

## Quality-Gate Notes (Stryker)

`npm run mutation` (`stryker run`) was **not re-run this session** — there is no local `stryker.conf` in `web/` (the mutation gate is CI-wired; a bare local `stryker run` would mutate the whole `src/` with no config). The changes are byte-equivalent dedup / refactor introducing **zero new logic branches**: the migrated consumers are exercised by the same passing suites (per-file vitest ≥85%, mostly 95–100%), and the canonical targets (`getJSON`/`postJSON` in `api/json`, `focusFirstDescendant`/`trapTabKey` in `a11y/focusTrap`, `SkeletonBlock` in `components/skeleton`) retain their own existing tests. No mutation number is fabricated; the CI Stryker leg remains the gate of record.

## Issues Encountered

- **Concurrent Codex session on master:** owns `internal/agui/**`, `internal/objectstore/**`, document-library/catalog `web/src/**` work, `docs/superpowers/**`, and `.planning/graphs/*`. The concurrent-edit STOP guard never tripped — none of this plan's 9 target files showed foreign changes. Codex committed `4df9c1ee` between my two commits; its earlier-staged doc was excluded by the `-o` scope. Every commit verified isolated.
- **`git commit --only` + untracked dist assets** (see Deviation 3) — resolved; both dist trees fully committed.
- **Inline env-var prefix breaks `docker` in w64devkit sh:** `MSYS_NO_PATHCONV=1 docker …` mis-resolved the space-containing docker path; plain `docker …` works (29.5.3). For Playwright, `export AURA_E2E_ORIGIN=…` was used instead of an inline prefix.

## User Setup Required

None — no env, schema, CI, or external-service changes. (The `aura:local` container was rebuilt+restarted to serve the fresh dist for the host Playwright run; the committed dist is the source of truth and CI rebuilds it.)

## Next Phase Readiness

- **QUAL-03 (frontend dedup) advanced:** one `getJSON`, one (canonical, bug-fixed) `focusTrap`, one skeleton system; consumer tests + host Playwright green; fresh dist committed.
- STATE.md / ROADMAP.md NOT modified here — the orchestrator owns those writes.

## Self-Check: PASSED

- FOUND: `web/src/api/json.ts` canonical getJSON; `rg 'function getJSON|const getJSON' web/src/conversations/useConversations.ts web/src/governance/governanceApi.ts` → none; both `import … from '../api/json'`; `pimApi.ts` repointed.
- FOUND: `web/src/governance/BoardLayout.tsx` + `McpLifecycleCluster.tsx` import from `../a11y/focusTrap`; inline traps removed; consumer tests green.
- CONFIRMED: `web/src/components/ui/skeleton.tsx` deleted; `rg 'components/ui/skeleton' web/src/` → none; 3 consumers import `@/components/skeleton`.
- CONFIRMED: `internal/webui/dist` rebuilt+committed in both commits; `git status --porcelain internal/webui/dist` → 0 after each.
- FOUND commit: `a1e1a29b` (Task 1 — 5 source + dist; 0 agui-nondist/objectstore/graphs/docs/other-web).
- FOUND commit: `bb3aff2a` (Task 2 — 4 source + deleted skeleton + dist; 0 foreign).
- CONFIRMED: web gates green (test 922, lint 0, knip 0, build 0); host Playwright migrated-surface 11/11, full chromium 35/37 (2 isolation-passing flakes outside surface).
- CONFIRMED: STATE.md and ROADMAP.md NOT modified.

---
*Phase: 32-quality-cleanup-dead-code-shared-helpers*
*Completed: 2026-06-30*
