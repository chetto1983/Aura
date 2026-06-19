---
phase: 27-neo4j-graph-explorer
plan: 01
subsystem: api
tags: [neo4j, cypher, graph, mcp, normalizer, read-only, injection-safety, knowledge]

# Dependency graph
requires:
  - phase: 26-typed-display-protocol-router
    provides: the flat tagged-struct contract discipline (display.Payload) + the source/citation registry the graph inspector cross-links into
  - phase: 24-web-foundation-serve-auth-health
    provides: the /api/ carve-out + RequireAuth whole-origin gate the plan-02 graph routes inherit
provides:
  - "GraphView type (Schema + Query) wrapping a read-only GraphReader seam over knowledge.Client.Read"
  - "GraphIntent → parameterized read-Cypher compilers (compileSeed/compileExpand/compileSchema), values bound only via the param map"
  - "assertReadOnly write-verb guard (CREATE/MERGE/SET/DELETE/REMOVE/DROP/FOREACH + CALL{...write}) with string-literal stripping"
  - "row-map → {nodes,edges,paths,schema,query} normalizer with labels-via-toJson decode, edge attach by elementId, node de-dupe, and Citations derivation"
  - "the live neo4j_integration probe that pins the mcp serialization shape (A1) and the conversation footprint / schema-overview fallback (A3)"
affects: [27-02 graph REST routes, 27-03 graph frontend, 27-04 graph workspace, 29-mcp-skills]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Explicit-field Cypher projection (elementId + apoc.convert.toJson(labels)) — never RETURN n / bare list, so labels + element-ids survive the mcp boundary"
    - "Read-only GraphReader seam (Read only; no Write surfaced) — mirror reasoningstore.GraphClient minus Write"
    - "Belt-and-suspenders assertReadOnly guard with stripStringLiterals so verbs in node-property data do not false-positive"
    - "Schema-overview fallback so the default open is never blank when the seed footprint is empty"

key-files:
  created:
    - internal/knowledge/graphview.go
    - internal/knowledge/graphview_intent.go
    - internal/knowledge/graphview_guard.go
    - internal/knowledge/graphview_normalize.go
    - internal/knowledge/graphview_test.go
    - internal/knowledge/graphview_query_test.go
    - internal/knowledge/graphview_integration_test.go
  modified: []

key-decisions:
  - "assertReadOnly + stripStringLiterals live in graphview_guard.go (split off graphview.go up front, not on the 600-LOC trip) for a clean security-control file"
  - "normalizeRows derives Citations from the already-fetched edges (no extra Cypher); the :Document node itself gets an empty list — only its NEIGHBORS cite it"
  - "the live probe resolved A3: the production graph has 0 :Conversation nodes, so the schema-overview fallback is the load-bearing default open, not the seed footprint"

patterns-established:
  - "Explicit-field projection + apoc.convert.toJson for every list column (Pattern 1)"
  - "Param-map-only binding (no fmt.Sprintf into the Cypher body) verified by a no-interpolation test"
  - "Read-only milestone: the seam exposes Read only; the write-verb guard is the backstop"

requirements-completed: [GRAPH-01, GRAPH-03, GRAPH-04]

# Metrics
duration: ~18min
completed: 2026-06-19
status: complete
---

# Phase 27 Plan 01: Read-only Graph Normalizer Summary

**A Go normalizer that compiles structured GraphIntents into parameterized read-Cypher, guards them with an injection-safe write-verb backstop, and projects mcp-neo4j-cypher row maps into the flat {nodes,edges,paths,schema,query} contract — with node labels surviving via apoc.convert.toJson and Citations derived from Document/Source neighbor edges.**

## Performance

- **Duration:** ~18 min
- **Started:** 2026-06-19T08:53:00Z
- **Completed:** 2026-06-19T09:11:29Z
- **Tasks:** 3
- **Files modified:** 7 created

## Accomplishments
- `GraphView` (Schema + Query) over a read-only `GraphReader` seam — `*knowledge.Client` satisfies it; the full compile → assertReadOnly → Read → normalize path with a schema-overview fallback on an empty seed.
- Parameterized intent compilers (`compileSeed`/`compileExpand`/`compileSchema`): every value rides the param map (`$seed`/`$session`/`$labels`/`$rel_types`/`$node_cap`/`$edge_cap`), label/rel-type filters bound as data, caps clamped server-side (75/200 defaults, 300/800 hard maxes), explicit-field projection so labels + element-ids survive the mcp serialization boundary.
- `assertReadOnly` write-verb guard (CREATE/MERGE/SET/DELETE/REMOVE/DROP/FOREACH + `CALL{...write}`) with `stripStringLiterals` so write verbs in node-property data never false-positive; the rejection error is sanitized (never echoes the offending query).
- `normalizeRows`: labels decode from the `apoc.convert.toJson` STRING via `json.Unmarshal`, edges attach by `elementId(start/end)`, nodes de-dupe by elementId, and `GraphNode.Citations` is derived from the already-fetched `:Document`/`:Source` neighbor edges (no extra Cypher).
- Live `neo4j_integration` probe (ran against the real stack, not skipped): pinned the serialization shape (A1) and recorded the real footprint (A3) — the production graph has **0 `:Conversation` nodes**, so the schema-overview fallback (17 labels live) is the load-bearing default open.

## Task Commits

Each task was committed atomically (TDD tasks = test/feat split where applicable):

1. **Task 1: Contract structs + GraphReader seam + GraphIntent + parameterized compilers** — `04df831d` (feat)
2. **Task 2: assertReadOnly guard + row→contract normalizer + GraphView.Query/Schema** — `cba020d7` (test) + `bca59818` (test split + coverage-lift)
3. **Task 3: Live neo4j_integration probe (A1/A3)** — `4cc5e8c3` (test)

_Task 1's commit also carried the minimal `assertReadOnly`/`normalizeRows` scaffold needed for the package to compile; Task 2 added the exhaustive guard/normalize behavior tests; `bca59818` split the over-cap test file (refactor-on-touch) and lifted the decode/schema-error branches over the 85% floor._

## Files Created/Modified
- `internal/knowledge/graphview.go` (183 LOC) — GraphReader seam + GraphView (Schema/Query) + flat contract structs (GraphResult/GraphNode incl. Citations/GraphEdge/GraphPath/GraphSchema) + cap consts + dispatch/fallback.
- `internal/knowledge/graphview_intent.go` (134 LOC) — GraphIntent + compileSeed/compileExpand/compileSchema (parameterized) + clamp + nonNil.
- `internal/knowledge/graphview_guard.go` (66 LOC) — assertReadOnly + stripStringLiterals (GRAPH-04 security control).
- `internal/knowledge/graphview_normalize.go` (209 LOC) — normalizeRows + normalizeSchema + deriveCitations/deriveDegree + decodeStrings/asString/asMap.
- `internal/knowledge/graphview_test.go` (244 LOC) — clamp, compile* (no-interpolation invariant), contract round-trip/omitempty.
- `internal/knowledge/graphview_query_test.go` (388 LOC) — assertReadOnly table, normalizeRows/Citations/Schema, Query/Schema dispatch + fallback + cap-clamp, decodeStrings branches.
- `internal/knowledge/graphview_integration_test.go` (153 LOC) — `//go:build neo4j_integration && db_integration` live serialization-shape + footprint + schema probes.

## Decisions Made
- **Guard in its own file up front:** `assertReadOnly` + `stripStringLiterals` went into `graphview_guard.go` immediately (not on a 600-LOC trip) so the security control is a clean, mutation-spot-checkable unit.
- **Citations belong to neighbors, not the document:** a `:Document`/`:Source` node confers a citation on its NEIGHBORS; the document node itself gets an empty Citations list (asserted in `TestNormalizeCitations`).
- **Schema-overview fallback is load-bearing, not cosmetic:** the live A3 probe found 0 `:Conversation` nodes on the production graph — the loop writes only entity-style nodes today — so the empty-seed → schema-overview path (D-08) is what guarantees a non-blank default open, exactly as the design anticipated.

## Deviations from Plan

None - plan executed exactly as written.

The only judgment calls were within the plan's stated discretion: (a) `assertReadOnly`/`stripStringLiterals` were placed in `graphview_guard.go` from the start (the plan allowed moving them there "if it crosses 600 LOC"; doing it up front kept the security control isolated), and (b) three extra unit tests (`TestDecodeStrings`, `TestSchemaReadError`, `TestQuerySchemaOverviewOp`) plus a test-file split were added to clear the 85% coverage floor and the 600-LOC cap (CLAUDE.md refactor-on-touch). Neither changed the plan's contract or scope.

## Issues Encountered
- **Test file crossed 600 LOC after the coverage-lift** (`graphview_test.go` hit 619 LOC). Resolved by splitting the guard/normalize/Query tests into `graphview_query_test.go` (refactor-on-touch); both files are now ≤388 LOC.
- **One self-authored test assertion was too literal** (`TestCompileSeed` expected `(:Entity)` but the compiler binds the entity as `(e:Entity)` because it is projected downstream). Fixed the assertion to match the correct bound-variable form (`:Entity)`), not the implementation.

## Live Findings (Wave-0 gate, A1/A3 resolved)
- **A1 (serialization shape):** `elementId(n)` → non-empty string, `apoc.convert.toJson(labels(n))` → a JSON STRING that unmarshals to a non-empty `[]string`, `properties(n)` → a map. `normalizeRows` reconstructs the live node. Pinned against a live `ToolSelectionExample` node.
- **A3 (footprint):** the live graph has **0 `:Conversation`** nodes (the loop writes only entity-style nodes). `GraphView.Query("seed")` for any session falls back to the schema overview (17 labels, 1 rel-type, 1 entity-type live) — the default open is never blank. Recorded in the test `t.Log`.
- The live probe ran with `-race`; goroutine-leak detection (inherited from `client_unit_test.go`'s sole `goleak.VerifyTestMain`) is clean — no second TestMain added.

## Quality Gates
- `go vet ./internal/knowledge/` clean; `go build ./...` clean.
- `go test -race ./internal/knowledge/` (unit, fake GraphReader) green.
- `go test -race -tags 'db_integration neo4j_integration' ./internal/knowledge/ -run TestGraphViewLive` green on the live stack (not skipped).
- `golangci-lint run ./internal/knowledge/` (untagged + integration tags) → 0 issues.
- graphview surface mean per-func coverage **91.5%** (full integration matrix) — above the 85% floor; the write-verb guard + cap-clamp are flagged as mutation-spot-check targets in-code.
- All `graphview*.go` files ≤600 LOC (largest: `graphview_query_test.go` 388). No `neo4j-go-driver` import; no `fmt.Sprintf` into a query body; no bare `RETURN n`.

## User Setup Required
None - no external service configuration required. The integration tier reuses the existing `NEO4J_*` / `AURA_MCP_NEO4J_CYPHER_BIN` / `AURA_NEO4J_BOLT_URL` env (no new secret).

## Next Phase Readiness
- The `{nodes,edges,paths,schema,query}` contract + `GraphView.Schema`/`Query` are the seam plan 02 (`internal/agui/graph_api.go` + the `/api/graph/*` routes) wraps; `*knowledge.Client` already satisfies `GraphReader`, and `cmd/aura/serve.go` will wire `aguiServer.SetGraphView(knowledge.NewGraphView(client))` (boot-time open per A7/Q2).
- **Carry-forward for plans 02/03:** the live graph has no `:Conversation` footprint today — the default open WILL be the schema overview, not a conversation subgraph. The frontend (plans 03/04) must render the schema-overview empty-state as a first-class default, not an edge case.

## Self-Check: PASSED

All 7 created source files + the SUMMARY exist on disk; all 4 task commits (`04df831d`, `cba020d7`, `4cc5e8c3`, `bca59818`) are in the git history.

---
*Phase: 27-neo4j-graph-explorer*
*Completed: 2026-06-19*
