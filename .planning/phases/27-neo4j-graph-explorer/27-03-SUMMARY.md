---
phase: 27-neo4j-graph-explorer
plan: 03
subsystem: web
tags: [neo4j, graph, frontend, vitest, sigma, graphology, react-sigma, label-family-palette, intent-reducer, same-origin, auth-error, jsdom]

# Dependency graph
requires:
  - phase: 27-neo4j-graph-explorer (plan 01)
    provides: the {nodes,edges,paths,schema,query} GraphResult contract + GraphIntent + the OpSeed/OpExpand/OpSchemaOverview wire-op values that types.ts mirrors field-for-field
  - phase: 27-neo4j-graph-explorer (plan 02)
    provides: the GET /api/graph/schema + POST /api/graph/query routes (behind RequireAuth) that graphApi.ts calls
  - phase: 25-cockpit-chat
    provides: the web/src/conversations/useConversations.ts getJSON credentialed-fetch convention (same-origin; non-200 throws Error(`HTTP <n>`)) that graphApi.ts mirrors
  - phase: 23-cockpit-frontend-foundation
    provides: the locked dark-operator brand tokens (theme.css) the label-family ramp is derived from + the Vitest ≥85% coverage gate
provides:
  - "web/src/graph/types.ts — TS mirror of knowledge.GraphResult/GraphNode/GraphEdge/GraphPath/GraphSchema/GraphIntent (json-tag keys) + GraphOp union ('seed'|'expand'|'schema_overview') + ClientNode/ClientEdge/ClientGraph renderer-projection types"
  - "web/src/graph/graphApi.ts — fetchGraphSchema + postGraphQuery typed wrappers (same-origin credentialed; non-200 incl. 401 THROWS Error(`HTTP <n>`); sends a structured GraphIntent only, never Cypher)"
  - "web/src/graph/graphIntent.ts — PURE core: intentReducer + initialIntentState + toClientIntent, applyFilters (empty set = show all, dim-not-remove), schema-driven labelFamilyColor over BRAND_RAMP (deterministic, Entity.type 2nd dimension), degreeToSize, rowsToClientGraph (de-dupe + drop-dangling)"
  - "the four pinned MIT graph packages (sigma 3.0.3, graphology 0.26.0, @react-sigma/core 5.0.6, graphology-layout-forceatlas2 0.10.1) in web/package.json + lockfile"
affects: [27-04 graph workspace, 28-gov-read-onboarding, 29-mcp-skills]

# Tech tracking
tech-stack:
  added:
    - "sigma@3.0.3 (MIT) — WebGL graph renderer (consumed only by SigmaCanvas in plan 04)"
    - "graphology@0.26.0 (MIT) — graph data model"
    - "@react-sigma/core@5.0.6 (MIT) — React bindings for sigma"
    - "graphology-layout-forceatlas2@0.10.1 (MIT) — ForceAtlas2 layout"
  patterns:
    - "Pure-logic-off-the-.tsx (the sourceExplorerData.ts idiom): intent reducer + filters + color mapper + contract→client projection are renderer-free so Vitest ≥85% + Stryker ≥70% is reachable in jsdom with no WebGL (Pitfall 4)"
    - "Contract mirror by json TAG: TS interface keys are the Go json-tag names so GraphResult round-trips graphview.go exactly"
    - "Credentialed-fetch convention reuse: graphApi.ts copies useConversations.ts getJSON byte-for-byte (same-origin; non-200 incl. 401 throws), so consumers drive a TanStack Query error path (B3 / T-27-03)"
    - "Schema-driven label-family palette: a single brand-derived BRAND_RAMP + label→family map + hash→ramp for unmapped labels — NO per-label color table, NO rainbow (D-02); Entity.type is a second color dimension; degree→size keeps color from being the only encoding (WCAG 1.4.1)"

key-files:
  created:
    - web/src/graph/types.ts
    - web/src/graph/graphApi.ts
    - web/src/graph/graphIntent.ts
    - web/src/graph/__tests__/graphIntent.test.ts
  modified:
    - web/package.json
    - web/package-lock.json

key-decisions:
  - "labelFamilyColor takes the live GraphSchema and uses it: an unmapped label that is IN the schema label set is anchored by its sorted position (legend reproducible across sessions for the same schema); a label outside the schema falls back to a content hash. This makes the schema parameter load-bearing (no ESLint unused-arg) AND honours the UI-SPEC 'reproducible legend' contract without a per-label table."
  - "BRAND_RAMP is the single source the mapper + the (plan-04) contrast-check share — every color is a ramp index, so there is no second hard-coded per-label palette to drift."
  - "applyFilters DIMS (returns dimmedNodeIds/dimmedEdgeIds) rather than removing nodes, and dims an edge when either endpoint is dimmed — context-preserving filtering for the canvas, mirroring the Go `$labels = [] OR …` empty-filter semantics."
  - "rowsToClientGraph drops edges referencing a missing node id (a dangling edge would crash graphology.addEdge in plan 04) and de-dupes nodes by id."

patterns-established:
  - "Renderer-free graph core: the three modules (types/graphApi/graphIntent) import NO sigma/@react-sigma/graphology — grep-verified — so the whole module set is jsdom-testable; SigmaCanvas (plan 04) is the only WebGL consumer."
  - "Named 401-reject tests for both fetch wrappers (`postGraphQuery rejects on 401` / `fetchGraphSchema rejects on 401`) pin the visible-auth-error requirement (B3 / T-27-03)."

requirements-completed: [GRAPH-02, GRAPH-04]

# Metrics
duration: ~10min
completed: 2026-06-19
status: complete
---

# Phase 27 Plan 03: Graph Frontend Core (types + API + pure intent) Summary

**The pinned MIT sigma/graphology stack plus the PURE, jsdom-testable frontend core — `types.ts` (contract mirror), `graphApi.ts` (same-origin credentialed wrappers that throw on 401), and `graphIntent.ts` (intent reducer + dim-filters + schema-driven label-family color + contract→graphology projection) — with 25 Vitest cases at 100% line / 96.55% branch coverage and zero renderer import.**

## Performance

- **Duration:** ~10 min (execution agent; Task 1 install + legitimacy gate was done in the prior checkpoint agent)
- **Started:** 2026-06-19T09:46:00Z (Task 1 commit; Task 2 began immediately after)
- **Completed:** 2026-06-19T09:56:03Z
- **Tasks:** 2
- **Files:** 6 (4 created, 2 modified)

## Accomplishments
- **Task 1 (legitimacy-gated install):** committed the four exact-pinned MIT packages — `sigma@3.0.3`, `graphology@0.26.0`, `@react-sigma/core@5.0.6`, `graphology-layout-forceatlas2@0.10.1` (all `--save-exact`, no caret). The operator-approved legitimacy gate confirmed all four MIT, all postinstall hooks empty (T-27-SC), lockfile +7 entries / 0 removed, `npm audit` 0 vulnerabilities. The renderer stack lands here but is imported ONLY by `SigmaCanvas` in plan 04.
- **`types.ts`** mirrors the Go contract field-for-field using the json TAG names as TS keys: `GraphResult`/`GraphNode`/`GraphEdge`/`GraphPath`/`GraphSchema`/`GraphIntent`, plus the `GraphOp` union (`'seed' | 'expand' | 'schema_overview'`) read directly from the `OpSeed`/`OpExpand`/`OpSchemaOverview` string values, and the `ClientNode`/`ClientEdge`/`ClientGraph` renderer-projection types. `GraphNode.citations` is mirrored.
- **`graphApi.ts`** copies `useConversations.ts` getJSON EXACTLY: `credentials: 'same-origin'`, `Accept: application/json` (+ `Content-Type` on the POST), and a non-200 — INCLUDING 401 — THROWS `Error(\`HTTP <n>\`)` (no discriminated-union return). It sends a structured `GraphIntent` only; it builds NO Cypher.
- **`graphIntent.ts`** is the PURE core (no sigma/@react-sigma/graphology import — grep-verified): `intentReducer` (setSeed→`seed`, expand→`expand` with `seed_id`, label/rel-type toggles; default caps 75/200), `applyFilters` (empty set = show all; dims rather than removes; dims an edge when an endpoint is dimmed or its rel-type is filtered), the schema-driven `labelFamilyColor` (known Frame-06 families → brand-ramp index; `Entity.type` = a second color dimension; unmapped labels → deterministic hash/schema-position → SAME ramp), `degreeToSize` (the non-color encoding channel), and `rowsToClientGraph` (de-dupe by id, drop dangling edges).
- **`graphIntent.test.ts`** — 25 Vitest cases covering every behavior, including the NAMED 401-reject cases `postGraphQuery rejects on 401` and `fetchGraphSchema rejects on 401` (`await expect(...).rejects.toThrow('HTTP 401')`) — requirement B3 / threat T-27-03.

## Task Commits

Each task was committed atomically:

1. **Task 1: pin sigma/graphology MIT graph stack (legitimacy-gated)** — `dcff14c7` (feat)
2. **Task 2: pure jsdom-testable graph core (types/api/intent + Vitest)** — `76e7ec1f` (feat)

_Task 2 was authored as a single `feat` (the three pure modules + their Vitest suite together): the contract was fully pre-specified by plans 01/02, so the test file cannot compile/import without the production symbols — a strict RED-first split would have been an artificial empty stub. The sibling-plan precedent (27-02) used the same reasoning. All behaviors are nonetheless test-driven (every BEHAVIOR bullet has a named case), and the branch-coverage lift (in-schema unmapped label, label-less node, rel-type-less edge) was added in the same commit to clear the 85% floor._

## Files Created/Modified
- `web/src/graph/types.ts` (104 LOC) — the TS contract mirror + `GraphOp` union + `OP_*` constants + the renderer-projection types.
- `web/src/graph/graphApi.ts` (54 LOC) — `fetchGraphSchema` (GET) + `postGraphQuery` (POST) + the private `getJSON`/`postJSON` (same-origin; non-200 throws) + the `GRAPH_SCHEMA_PATH`/`GRAPH_QUERY_PATH` consts.
- `web/src/graph/graphIntent.ts` (~330 LOC) — `BRAND_RAMP` + `LABEL_FAMILY`/`FAMILY_RAMP_INDEX`/`ENTITY_TYPE_OFFSET`, `labelFamilyColor` + `unmappedRampIndex`, `applyFilters` + `GraphFilters`/`EMPTY_FILTERS`/`FilteredGraph`, `intentReducer` + `initialIntentState`/`toClientIntent`/`IntentState`/`IntentAction`, `degreeToSize`, `rowsToClientGraph`, and `DEFAULT_NODE_CAP`/`DEFAULT_EDGE_CAP`.
- `web/src/graph/__tests__/graphIntent.test.ts` (~300 LOC) — 25 cases across color/filter/intent/projection + the two named 401-reject cases.
- `web/package.json` + `web/package-lock.json` — the four pinned MIT packages + their 3 transitive deps (events@3.3.0, graphology-types@0.24.8, graphology-utils@2.5.2).

## Decisions Made
- **The live schema is load-bearing in `labelFamilyColor`.** Rather than accept the schema as an unused `_schema` arg (which the project ESLint flags — no `argsIgnorePattern` configured), the mapper anchors an unmapped-but-in-schema label by its sorted position in the schema label set, so the legend is reproducible across sessions for a given schema; a label outside the schema falls back to a content hash. This satisfies the UI-SPEC "reproducible legend" contract honestly AND clears the lint.
- **DIM, don't remove.** `applyFilters` returns `dimmedNodeIds`/`dimmedEdgeIds` so the canvas can dim-for-context; an edge dims when either endpoint dims OR its rel-type is filtered out. An EMPTY filter set means "show all" on that axis (mirrors the Go `$labels = [] OR …`).
- **`degreeToSize` floors isolated nodes and grows sub-linearly (√degree)** so a hub never dwarfs the canvas and an isolated node stays visible — the non-color channel that keeps color from being the only encoding (WCAG 1.4.1 / D-03).

## Deviations from Plan

None — plan executed as written. The only judgement call (making the `schema` arg of `labelFamilyColor` load-bearing rather than `_schema`) is documented under Decisions Made; it is a faithful reading of the UI-SPEC "reproducible legend" requirement and the project ESLint config, not a scope change.

## Known Stubs
None. The three modules are fully wired pure logic; no hardcoded empty data flows to UI (there is no UI in this plan — the SigmaCanvas consumer is plan 04). `BRAND_RAMP` is a curated, intentional palette derived from the locked brand tokens, not a placeholder.

## Quality Gates
- `cd web && npx tsc --noEmit` — clean for the new files (incl. `noUncheckedIndexedAccess` strict mode).
- `cd web && npx eslint src/graph` — 0 problems.
- `cd web && npx vitest run src/graph` — 25/25 pass; **graphIntent.ts/graphApi.ts at 100% lines / 100% statements / 100% functions / 96.55% branches** (the only two uncovered branches are the defensive `?? FALLBACK_COLOR` / `?? ''` nullish fallbacks the modulo/guards make unreachable) — well above the ≥85% line floor the task requires.
- `cd web && npx vitest run --coverage` (full suite, the CI gate) — **558/558 pass**, global statements 91.97% / branches 85.68% / functions 92.08% / lines 94.17% — all above the 85% project threshold (the new files raised, not lowered, the numbers).
- grep-verified: NO `from 'sigma'` / `@react-sigma` / `graphology` import in `graphIntent.ts`, `graphApi.ts`, or `types.ts`.
- All four files ≤600 LOC (largest: graphIntent.ts ~330).
- Pre-commit hooks (vet + dup + file-size) green on both commits.

## User Setup Required
None — no external service configuration. The wrappers call the existing Phase-24-gated `/api/graph/*` routes (plan 02); no new env, no new secret.

## Next Phase Readiness
- Plan 04 (the graph workspace) can now: import the renderer-free core (`intentReducer`/`applyFilters`/`labelFamilyColor`/`rowsToClientGraph`) for its hooks + filter panel + legend, drive `fetchGraphSchema`/`postGraphQuery` through TanStack Query (surfacing the 401 reject as a visible auth-error state, B3), and import sigma/@react-sigma/graphology ONLY inside `SigmaCanvas` (the WebGL boundary, Pitfall 4).
- **Carry-forward (from plan 02):** the production graph has 0 `:Conversation` nodes, so a `seed` intent falls back server-side to the schema overview — plan 04's default open renders the schema-overview empty-state (200 with the schema + empty nodes/edges) as the PRIMARY default, not an error. `rowsToClientGraph` on an empty result correctly yields `{nodes:[], edges:[]}`.
- **Carry-forward (Stryker):** these modules are pure + fully covered, so the plan-04 Stryker mutation-scope extension (≥70% killed) is reachable; extending the Stryker config to `src/graph` is plan 04's job.

## Self-Check: PASSED

All 4 created files (`types.ts`, `graphApi.ts`, `graphIntent.ts`, `__tests__/graphIntent.test.ts`) exist on disk; both commits (`dcff14c7`, `76e7ec1f`) are in the git history.

---
*Phase: 27-neo4j-graph-explorer*
*Completed: 2026-06-19*
