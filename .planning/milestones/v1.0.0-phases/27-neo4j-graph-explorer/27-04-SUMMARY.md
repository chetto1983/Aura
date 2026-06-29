---
phase: 27-neo4j-graph-explorer
plan: 04
subsystem: web
tags: [neo4j, graph, frontend, sigma, graphology, react-sigma, webgl, a11y, axe, playwright, lazy-chunk, label-family-palette, read-only, auth-error, i18n, stryker]

# Dependency graph
requires:
  - phase: 27-neo4j-graph-explorer (plan 03)
    provides: the pure renderer-free core — types.ts (GraphResult/GraphNode incl. citations + ClientNode/ClientEdge), graphApi.ts (fetchGraphSchema/postGraphQuery, non-200 incl. 401 THROWS), graphIntent.ts (intentReducer, applyFilters, labelFamilyColor over BRAND_RAMP, degreeToSize, rowsToClientGraph)
  - phase: 27-neo4j-graph-explorer (plan 02)
    provides: GET /api/graph/schema + POST /api/graph/query (RequireAuth-only, fail-closed 503, 401 on expired session)
  - phase: 26-typed-display-protocol-router
    provides: the read-only Source Explorer (useSourceExplorer().openSources) the inspector cross-links into, and the DisplaySource shape
  - phase: 23-cockpit-frontend-foundation
    provides: the locked blue brand tokens, the AppShell lazy surface-swap host, the i18n feature-bundle-split idiom, the Vitest ≥85% coverage gate + the contrast-check 15-pair baseline
provides:
  - "web/src/graph/SigmaCanvas.tsx — the ONLY module importing sigma/@react-sigma/graphology/forceatlas2: SigmaContainer + GraphLoader (ForceAtlas2 position-cache) + EventHandler + HighlightManager + key={sigmaKey} resize-remount + ErrorBoundary + role=img accessible name"
  - "web/src/graph/SigmaCanvas_reducers.ts — pure pinned-path highlight/dim attribute transforms (no renderer import)"
  - "web/src/graph/GraphExplorer.tsx — the lazy three-pane workspace shell (seed→schema-overview fallback, 401→visible auth error, cap notice, responsive mobile inspector sheet)"
  - "web/src/graph/SeedFilterPanel.tsx — seed CTA + live-schema filter toggles + read-only Cypher preview (display-only)"
  - "web/src/graph/NodeInspector.tsx — read-only action set (pin-path/open-source/show-Cypher, NO add-note) + the citations list + the Source Explorer cross-link"
  - "web/src/graph/PathStrip.tsx — the D-10 path strip + the a11y parallel role=list node/edge DOM (keyboard traversal, the non-hover access path)"
  - "web/src/graph/nodeSources.ts — pure sourcesForNode/isSourceNode projection"
  - "web/src/i18n/resources.graph.ts — graphEn/graphIt bundles (every graph.* key, en+it) spread into resources.ts"
  - "the surface==='graph' lazy AppShell swap + 'graph' added to LIVE_MODES (the operator can now open the Graph Explorer)"
  - "the BRAND_RAMP label-family ramp added to web/scripts/contrast-check.mjs (31/31 pairs pass) + src/graph in stryker.config.json mutate scope"
  - "web/e2e/graph.spec.ts + graph-a11y.spec.ts — the live WebGL + axe + keyboard + session-expiry-401 e2e (12/12 pass on chromium+mobile)"
  - "regenerated internal/webui/dist with the distinct GraphExplorer-[hash].js + SigmaCanvas-[hash].js lazy chunks"
affects: [28-gov-read-onboarding, 29-mcp-skills]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Renderer-boundary isolation: SigmaCanvas.tsx is the SOLE importer of the WebGL stack; all testable logic stays in the pure plan-03 core + the pure reducers, so jsdom Vitest + Stryker never touch WebGL (Pitfall 4)"
    - "Distinct lazy chunk per heavy surface: React.lazy(import('./graph/GraphExplorer')) + a nested lazy SigmaCanvas → Vite emits GraphExplorer-[hash].js AND SigmaCanvas-[hash].js separate from index-[hash].js (Pitfall 7); Sigma never weighs the main bundle"
    - "key={sigmaKey} resize-remount + ErrorBoundary: the parent bumps sigmaKey on mount + node-select so the GPU programs recompile cleanly; a WebGL-context-loss falls back instead of white-screening (Pitfall 1)"
    - "401-as-visible-state: graphApi throws Error('HTTP 401') on an expired session; GraphExplorer catches it and renders a visible auth-error alert, never a blank canvas (B3 / T-27-03) — asserted as a named Vitest case AND a live Playwright case"
    - "Parallel-DOM a11y: the role=list node/edge list + the path strip are the non-WebGL fallback + SR surface; Enter on a node-list item = a canvas click (hover is never the only access path, D-03)"
    - "Responsive mode-swap inspector: ONE NodeInspector instance — the right column on lg, a focus-trapped bottom sheet on mobile when a node is selected (no duplicate DOM / strict-mode collision)"
    - "axe-without-the-wrapper: the live a11y e2e injects the resolvable axe-core engine via page.addScriptTag + window.axe.run (no new @axe-core/playwright dependency)"

key-files:
  created:
    - web/src/graph/SigmaCanvas.tsx
    - web/src/graph/SigmaCanvas_reducers.ts
    - web/src/graph/GraphExplorer.tsx
    - web/src/graph/SeedFilterPanel.tsx
    - web/src/graph/NodeInspector.tsx
    - web/src/graph/PathStrip.tsx
    - web/src/graph/nodeSources.ts
    - web/src/i18n/resources.graph.ts
    - web/src/graph/__tests__/SigmaCanvas.test.tsx
    - web/src/graph/__tests__/GraphExplorer.test.tsx
    - web/src/graph/__tests__/NodeInspector.test.tsx
    - web/src/graph/__tests__/SeedFilterPanel.test.tsx
    - web/src/graph/__tests__/SigmaCanvas_reducers.test.ts
    - web/e2e/graph.spec.ts
    - web/e2e/graph-a11y.spec.ts
  modified:
    - web/src/AppShell.tsx
    - web/src/i18n/resources.ts
    - web/src/shell/modes.ts
    - web/src/shell/__tests__/shell.test.tsx
    - web/scripts/contrast-check.mjs
    - web/stryker.config.json
    - internal/webui/dist

key-decisions:
  - "'graph' added to LIVE_MODES (modes.ts): the AppShell swap is unreachable while the mode is disabled — making graph live is what lets the operator open the workspace (GRAPH-02). The two stale 'graph-is-a-disabled-future-mode' shell tests were REWRITTEN (graph now selectable; tree/displays remain the future modes), not deleted."
  - "Show Cypher lives in BOTH the SeedFilterPanel (left) and the NodeInspector (right) — both are read-only display-only affordances (D-09). The e2e scopes its assertions to the inspector region to avoid the strict-mode two-element match."
  - "The inspector is ONE instance, repositioned by responsive classes (right column on lg, fixed bottom sheet on mobile when selected) — not two breakpoint copies — so there is no duplicate DOM / strict-mode collision and the jsdom tests see a single Pin-path button."
  - "Pin path = the node + its directly-connected neighbors (client-side), driving the SigmaCanvas reducer AND the path-strip mirror — a legible evidence path, not a recomputed server query."

patterns-established:
  - "The whole src/graph surface is jsdom-testable except SigmaCanvas.tsx (mocked in Vitest, exercised live in Playwright) — the renderer boundary is a single file."
  - "Live e2e against a fresh local serve on a non-default port via AURA_E2E_ORIGIN when the production aura container occupies :9080 — the new embedded dist is served without touching the running container."

requirements-completed: [GRAPH-02, GRAPH-03, GRAPH-04]

# Metrics
duration: ~75min
completed: 2026-06-19
status: complete
---

# Phase 27 Plan 04: Graph Explorer Workspace Summary

**The Frame-06 three-pane Graph Explorer — the Sigma WebGL canvas (position-cache + resize-remount workaround + ErrorBoundary + role=img), the seed/filter panel with the read-only Cypher preview, the read-only node inspector (citations list + pin-path/open-source/show-Cypher, no add-note), and the keyboard-accessible path strip / parallel-DOM surface — lazy-mounted when surface==='graph', with en+it copy, the label-family contrast ramp, the Stryker scope extension, and a rebuilt dist; 599 Vitest pass at 92.07%/85.83%/91.73%/94.38% global coverage, 75.88% Stryker on the pure graph logic, and 12/12 Playwright e2e (real WebGL + axe + keyboard + session-expiry-401) green live on chromium + mobile.**

## Performance

- **Duration:** ~75 min
- **Completed:** 2026-06-19
- **Tasks:** 4
- **Files:** 22 (15 created, 7 modified — incl. the regenerated dist)

## Accomplishments
- **Task 1 — SigmaCanvas (greenfield WebGL renderer):** `SigmaCanvas.tsx` is the only module importing the sigma/graphology/@react-sigma/forceatlas2 stack. Ported from the llm_wiki reference: `GraphLoader` (builds the graphology Graph from `rowsToClientGraph`, runs ForceAtlas2 with `inferSettings` + `barnesHutOptimize` >50 nodes, caches positions so re-renders never re-layout), `EventHandler` (clickNode opens the inspector), `HighlightManager` (the pinned-path node/edge reducers). The Pitfall-1 `key={sigmaKey}` resize-remount + an `ErrorBoundary` so a WebGL-context-loss never white-screens the cockpit; `role="img"` + `graph.a11y.canvasName` accessible name; `prefers-reduced-motion` shortens the settle. Reducers split into the pure `SigmaCanvas_reducers.ts`. The `resources.graph.ts` (graphEn/graphIt) bundle landed here too (the canvas accessible name needs the keys).
- **Task 2 — GraphExplorer shell + SeedFilterPanel + lazy AppShell swap:** `GraphExplorer.tsx` (lazy default export) is the three-CSS-grid-column workspace owning the intent/selection/path state + the sigmaKey counter. The default open seeds `op:'seed'` from threadId; an EMPTY result falls back to the schema overview (never a blank canvas, D-07/D-08); an `HTTP 401` rejection renders a visible auth-error alert (B3/T-27-03); a non-401 rejection the query/schema error+retry state; a cap notice on overflow. `SeedFilterPanel.tsx` is the seed CTA + live-schema filter toggles + the read-only Cypher preview (rendered as text in a `<pre>`, never an input). `AppShell.tsx` swaps `ExternalStoreChat`→`<GraphExplorer>` when `surface==='graph'`.
- **Task 3 — NodeInspector + PathStrip + a11y parallel DOM:** `NodeInspector.tsx` renders label/properties(as text)/degree/citations + the exact read-only action set (pin-path / open-source / show-Cypher — `add note` is NOT in the DOM); the citations list is a displayed field distinct from the open-source action; open-source cross-links the Phase-26 Source Explorer via `openSources(sourcesForNode(node), refId)`. `PathStrip.tsx` is the D-10 path strip + the `role="list"` node/edge list with arrow-key/Tab traversal where Enter selects = a canvas click (the non-hover access path).
- **Task 4 — i18n + contrast + Stryker + e2e + dist:** the label-family BRAND_RAMP added to `contrast-check.mjs` (31/31 pairs pass); `src/graph` added to the Stryker mutate scope (75.88% killed spot-check); `web/e2e/graph.spec.ts` + `graph-a11y.spec.ts` written and RUN live (canvas renders with real WebGL, axe 0 serious/critical WCAG-AA, tap/keyboard opens the inspector, AND the session-expiry-401 → visible auth error); `internal/webui/dist` rebuilt with the distinct `GraphExplorer-[hash].js` + `SigmaCanvas-[hash].js` lazy chunks.

## Task Commits
1. **Task 1: SigmaCanvas WebGL renderer + graph i18n bundle** — `57ff5ca0` (feat)
2. **Task 2: GraphExplorer three-pane shell + SeedFilterPanel + lazy AppShell swap** — `8d21af9d` (feat)
3. **Task 3: NodeInspector (read-only set + citations) + PathStrip a11y DOM** — `ecdf37af` (feat)
4. **Task 4: graph i18n contrast + Stryker scope + Playwright e2e + dist rebuild** — `adbfc587` (feat)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] 'graph' was not a live surface — the Graph Explorer was unreachable**
- **Found during:** Task 4 (writing the e2e — the "Graph" mode button was disabled/greyed)
- **Issue:** `web/src/shell/modes.ts` had `LIVE_MODES = ['chat']`, so `isLiveSurfaceIntent('graph')` was false and the mode switcher rendered the Graph button as `aria-disabled` and ignored its click. The AppShell swap wired in Task 2 was therefore unreachable — GRAPH-02 ("the operator opens the Graph Explorer") could not be met.
- **Fix:** Added `'graph'` to `LIVE_MODES`. Rewrote the two now-stale shell tests (`useSurfaceIntent ignores stored future surfaces`, `mode controls expose future modes as disabled`) to use `tree`/`displays` as the remaining future modes and added positive assertions that the graph surface now switches + persists (the tests were stale because the feature legitimately changed, not broken — CLAUDE.md test-discipline honored with explicit justification).
- **Files modified:** `web/src/shell/modes.ts`, `web/src/shell/__tests__/shell.test.tsx`
- **Verification:** 35/35 shell Vitest pass; the live e2e confirms the operator opens the workspace.
- **Committed in:** `adbfc587` (Task 4)

**2. [Rule 1 - Bug] The inspector was hidden on mobile (lg:block hidden) — node selection had nowhere to show**
- **Found during:** Task 4 (the mobile-chrome e2e — inspector tests failed on the narrow viewport)
- **Issue:** `GraphExplorer` rendered the inspector + seed panel as `hidden ... lg:block` columns, so below `lg` the inspector never appeared and a node-list selection did nothing — violating the UI-SPEC "on mobile the inspector becomes a focus-trapped sheet" + GRAPH-03 mobile access.
- **Fix:** Made the layout responsive — the seed panel is a collapsed top strip on mobile / the left column on lg; the inspector is ONE instance repositioned by responsive classes (the right column on lg, a fixed focus-trapped bottom sheet on mobile when a node is selected). One instance avoids a duplicate-DOM / strict-mode collision.
- **Files modified:** `web/src/graph/GraphExplorer.tsx`
- **Verification:** 12/12 e2e pass on chromium AND mobile-chrome.
- **Committed in:** `adbfc587` (Task 4)

**Total deviations:** 2 auto-fixed (1 blocking reachability, 1 mobile-access bug). Both were correctness requirements directly in the plan's success criteria (GRAPH-02 reachability + GRAPH-03 mobile access). No scope creep.

## Authentication Gates
None — the e2e ran against a fresh local serve authenticated via the existing `AURA_WEB_AUTH_SECRET` passphrase helper. No new auth surface; the graph routes inherit the Phase-24 RequireAuth gate (plan 02).

## Known Stubs
None. The seed → schema-overview fallback is the carry-forward DEFAULT (the production graph has 0 `:Conversation` nodes, so the schema overview is the first-class default open, not a stub). The other future modes (`tree`/`displays`/`settings`) remain intentionally disabled placeholders — unchanged by this plan and out of scope.

## Quality Gates
- `cd web && npx tsc --noEmit` — clean (incl. `exactOptionalPropertyTypes` strict mode).
- `cd web && npx eslint src/graph e2e/graph*.spec.ts` — 0 problems.
- `cd web && npx vitest run --coverage` — **599/599 pass**, global statements 92.07% / branches 85.83% / functions 91.73% / lines 94.38% — all above the 85% project floor (exit 0). `src/graph` at 94.57% statements / 90.32% branches.
- `cd web && npx stryker run --mutate src/graph/{graphIntent,SigmaCanvas_reducers,nodeSources}.ts` — **75.88% killed** (≥70% break threshold met, exit 0); `src/graph` is now in the Stryker mutate scope.
- `node web/scripts/contrast-check.mjs` — **31/31 pairs pass** WCAG AA (16 label-family ramp pairs added; every fill ≥3:1 vs bg, every legend text ≥4.5:1 vs surface).
- `cd web && npx playwright test e2e/graph.spec.ts e2e/graph-a11y.spec.ts --project=chromium --project=mobile-chrome` — **12/12 pass live** (real WebGL canvas, a11y node/edge list + path strip, read-only inspector + citations, Show Cypher display-only, axe 0 serious/critical WCAG-AA, tap/keyboard opens the inspector, AND the named session-expiry 401 → visible auth error). Run against a fresh local `aura serve` on `127.0.0.1:9099` via `AURA_E2E_ORIGIN` (the production `aura` container occupies :9080); the full Docker stack (Postgres + Neo4j) was up.
- Vite build emits DISTINCT lazy chunks: `GraphExplorer-[hash].js` (20.83 kB / 6.95 kB gzip) AND `SigmaCanvas-[hash].js` (173.74 kB / 42.62 kB gzip), both separate from `index-[hash].js` (Pitfall 7 — Sigma never in the main bundle).
- `go build ./...` + `go vet ./internal/webui/...` — clean (the rebuilt embed compiles); pre-commit hooks (vet + dup + file-size) green on all 4 commits.
- All source files ≤600 LOC (largest: `GraphExplorer.tsx` 288).
- grep-verified: the only module importing `sigma`/`@react-sigma`/`graphology` is `SigmaCanvas.tsx`; the pure core (graphIntent/graphApi/types/reducers/nodeSources) has zero renderer import.

## E2E Live-Tier Status
The Playwright graph + graph-a11y specs were **RUN live, not just written** — 12/12 green on chromium + mobile-chrome against a fresh local serve embedding this plan's dist. No `test.skip`/no-skip-as-green: every case executed and asserted real DOM/WebGL/axe outcomes. To reproduce: bring up the stack (`make neo4j-migrate` / the Docker stack), build `aura.exe` with the current dist, run it on a free port (`AURA_AGUI_BIND=127.0.0.1:9099 ./aura.exe serve --only=cli`), then `cd web && AURA_E2E_ORIGIN=http://127.0.0.1:9099 npx playwright test e2e/graph*.spec.ts` (the env auto-loads `AURA_WEB_AUTH_SECRET` from `.env`). mobile-safari requires an HTTPS origin (the `__Host-`/Secure cookie) and was not run in this session; CI runs chromium + mobile-chrome.

## Threat Surface
All four STRIDE register threats are mitigated and asserted:
- **T-27-04 (untrusted output / XSS):** node/edge label + property strings + citations render as TEXT (React-escaped); no `dangerouslySetInnerHTML`. A Vitest case asserts a `<script>`-like property value is inert; the displays.spec sanitization posture is carried forward.
- **T-27-02 (write via UI):** the inspector action set is exactly pin-path/open-source/show-Cypher; `add note` is NOT in the DOM (asserted Vitest + e2e); Show Cypher is display-only (no editable input — asserted Vitest + e2e).
- **T-27-05 (dense-graph hairball):** the default is the seed evidence subgraph with caps (75/200) + a cap notice; expand grows intentionally; the schema overview is the no-footprint fallback.
- **T-27-03 (auth):** a 401 from a graph fetch surfaces a visible auth-error state (named Vitest case + the live Playwright session-expiry case), never a silent blank canvas.

## Next Phase Readiness
- Phase 28 (gov-read + onboarding) and Phase 29 (MCP + skills) can build on a reachable, accessible, read-only Graph Explorer. The `add note` write surface + governance writes were deliberately deferred to Phase 29 (the inspector is read-only by construction).
- The seed footprint stays the schema overview until the loop writes `:Conversation` nodes (carry-forward from plans 01/02) — when that lands, the same GraphExplorer seed path renders a populated conversation subgraph with no frontend change.

## Self-Check: PASSED

All 15 created files + the SUMMARY exist on disk; all 4 task commits (`57ff5ca0`, `8d21af9d`, `ecdf37af`, `adbfc587`) are in the git history.

---
*Phase: 27-neo4j-graph-explorer*
*Completed: 2026-06-19*
