---
phase: 27-neo4j-graph-explorer
plan: 02
subsystem: api
tags: [neo4j, graph, rest, agui, requireauth, read-only, injection-safety, knowledge, sanitization]

# Dependency graph
requires:
  - phase: 27-neo4j-graph-explorer (plan 01)
    provides: the GraphView normalizer (Schema + Query) + the {nodes,edges,paths,schema,query} contract + GraphIntent + the assertReadOnly write-verb guard the REST layer wraps
  - phase: 24-web-foundation-serve-auth-health
    provides: the /api/ carve-out + the RequireAuth whole-origin gate the two graph routes inherit (and the SetImageProxy off-constructor setter pattern mirrored here)
provides:
  - "GraphView consumer interface in package agui (Schema + Query over knowledge.GraphSchema/GraphResult/GraphIntent) declared consumer-side (D-A2-02)"
  - "handleGraphSchema + handleGraphQuery thin handlers (503-when-unwired, MaxBytesReader body cap, op-enum + length-cap + live-schema filter validation, sanitized errors) + registerGraphRoutes"
  - "graph GraphView field + SetGraphView setter on *Server; the two /api/graph/* routes on Server.Mux"
  - "graphSchemaRoute + graphQueryRoute consts + their RequireAuth-only mux.Handle mounts in serve_webui.go (no RequireCapability — read-only milestone)"
  - "boot-time knowledge.Client opened once in serve.go, wired via SetGraphView, Close appended to the mcpClosers reverse-teardown; routes fail-closed 503 when the client is unavailable"
  - "exported OpSeed/OpExpand/OpSchemaOverview (one wire-op source of truth shared by the REST validator and the plan-01 dispatcher)"
  - "widened secretPattern: bolt://neo4j DSN host+credential redaction (T-27-05/V13)"
affects: [27-03 graph frontend, 27-04 graph workspace, 28-gov-read-onboarding, 29-mcp-skills]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Consumer-side narrow interface for the graph seam (GraphView declared in package agui — depends only on Schema+Query), mirroring ImageFetcher/ApprovalStore (D-A2-02)"
    - "Off-constructor setter (SetGraphView) so existing NewServer callers/tests are unchanged; 503-until-set fail-closed"
    - "Read-only /api/ sibling mounted RequireAuth-only (no RequireCapability), specific method+path pattern (never a bare /api/) — the imageProxyRoute pattern extended"
    - "Server-side V5 input validation chokepoint in the handler (op enum, length caps, live-schema filter validation) front of the param-bound normalizer"
    - "DSN/credential redaction extended to the Neo4j bolt/neo4j schemes at the wire boundary"

key-files:
  created:
    - internal/agui/graph_api.go
    - internal/agui/graph_api_test.go
    - internal/agui/server_redact.go
  modified:
    - internal/agui/server.go
    - internal/knowledge/graphview.go
    - internal/knowledge/graphview_intent.go
    - cmd/aura/serve.go
    - cmd/aura/serve_webui.go

key-decisions:
  - "Export the Op enum (OpSeed/OpExpand/OpSchemaOverview) so the REST validator and the plan-01 dispatcher switch on the SAME constants — no drift between the validator and the compiler"
  - "Widen secretPattern to redact bolt://neo4j DSNs (host AND credential) — the graph routes are the first wire surface a bolt DSN error can reach (T-27-05)"
  - "Split the wire-error redaction controls into server_redact.go (refactor-on-touch: server.go crossed the 600-LOC cap when the bolt scheme + comments landed)"
  - "Open ONE boot-time knowledge.Client for the gateway lifetime (RESEARCH A7/Q2), distinct from the ReasoningLearning-gated client in chat.go; a down Neo4j leaves the routes at 503 and never aborts serve boot"

patterns-established:
  - "Graph REST adapter: thin handler (parse → validate V5 → GraphView.Query/Schema → writeJSON), no Cypher built here, sanitizeErr on every wire error"
  - "RequireAuth-gate httptest over RequireAuth(s.Mux(), deps): unauthenticated 401 + seam-not-reached + valid-session-200 + bare-Mux-registered, mirroring TestImageProxyRequireAuthGate"

requirements-completed: [GRAPH-01, GRAPH-04]

# Metrics
duration: ~15min
completed: 2026-06-19
status: complete
---

# Phase 27 Plan 02: Read-only Graph REST Routes Summary

**Two thin authenticated REST routes — `GET /api/graph/schema` + `POST /api/graph/query` — that serve the plan-01 graph contract over plain net/http JSON behind the Phase-24 `RequireAuth` whole-origin gate, with server-side intent validation, body-cap + DSN-sanitized errors, and a fail-closed boot-time `knowledge.Client` wired into the AG-UI server.**

## Performance

- **Duration:** ~15 min
- **Started:** 2026-06-19T09:18:10Z
- **Completed:** 2026-06-19T09:33:00Z
- **Tasks:** 2
- **Files modified:** 8 (3 created, 5 modified)

## Accomplishments
- A `GraphView` consumer interface in package `agui` (Schema + Query over the plan-01 `knowledge.GraphSchema`/`GraphResult`/`GraphIntent`) + `handleGraphSchema`/`handleGraphQuery` thin handlers + `registerGraphRoutes` — the handlers parse, validate, dispatch to the normalizer, and project to JSON; they build NO Cypher and import NO runner/SSE adapter (Pitfall 6).
- The `POST /api/graph/query` handler is the untrusted-input chokepoint (T-27-01/T-27-05): `http.MaxBytesReader` body cap, op-enum validation, length-capped id fields + bounded filter lists, and label/rel-type filters validated against the LIVE schema set before dispatch — an unknown label is a clean 400, never a silent empty subgraph; every wire error is `sanitizeErr`-redacted.
- Both routes mount as SPECIFIC method+path siblings under the `/api/` carve-out (never a bare `/api/`, T-27-03) behind `RequireAuth` with NO `RequireCapability` (read-only milestone). A named httptest (`TestGraphRequireAuthGate`) asserts an unauthenticated GET/POST each return 401 AND never reach the `GraphView` seam, while a valid session reaches the handler.
- A boot-time `knowledge.Client` is opened ONCE in `serve.go`, wired via `SetGraphView(knowledge.NewGraphView(gclient))`, and its `Close` joins the `mcpClosers` reverse-teardown; a down Neo4j / missing binary leaves the routes at 503 and never aborts serve boot (A7 fail-closed).

## Task Commits

Each task was committed atomically:

1. **Task 1: graph_api.go — GraphView seam + handlers + registerGraphRoutes + httptest** — `9454b5f5` (feat)
2. **Task 2: SetGraphView in Mux + boot-time knowledge.Client in serve + /api/graph/* mounts + RequireAuth-gate test** — `a278c27f` (feat)
3. **Coverage-lift: validateGraphIntent + subset reject branches** — `af66a697` (test)

_Task 1 was authored as a single `feat` (test + impl together): the contract was fully pre-specified by plan 01, so the httptest cannot compile without the production symbols — a strict RED-first split would have been an artificial empty stub. The coverage-lift test commit (`af66a697`) closed the validateGraphIntent/subset branches over the 85% floor (refactor/test discipline)._

## Files Created/Modified
- `internal/agui/graph_api.go` (185 LOC) — `GraphView` consumer interface + `handleGraphSchema`/`handleGraphQuery` + `validateGraphIntent` (V5) + `subset` + `registerGraphRoutes`.
- `internal/agui/graph_api_test.go` (~315 LOC) — httptest over a fake `GraphView`: 200/400/503, malformed/unknown-op/negative-cap/over-cap/unknown-label-filter, contract shape, sanitized-error coverage, plus the `TestGraphRequireAuthGate` 401-gate test.
- `internal/agui/server_redact.go` (NEW) — the wire-error redaction controls (`secretPattern`/`urlUserinfoPattern`/`tokenPattern`/`sanitizeErr`/`SanitizeString`), split off server.go on the 600-LOC trip; `secretPattern` widened to the Neo4j bolt/neo4j schemes.
- `internal/agui/server.go` — `graph GraphView` field + `SetGraphView` setter + `registerGraphRoutes(mux)` in `Mux()`; the redaction block moved out (now 552 LOC).
- `internal/knowledge/graphview.go` + `graphview_intent.go` — exported the Op enum (`OpSeed`/`OpExpand`/`OpSchemaOverview`) and updated the dispatcher + tests to the exported names.
- `cmd/aura/serve_webui.go` — `graphSchemaRoute`/`graphQueryRoute` consts + their RequireAuth-only `mux.Handle` mounts (sibling of `imageProxyRoute`).
- `cmd/aura/serve.go` — boot-time `knowledge.Open` → `SetGraphView` wiring (best-effort, warn-and-503 on failure) + `gclient.Close` appended to `chat.mcpClosers`.

## Decisions Made
- **Export the Op enum:** the REST validator must enum-validate an inbound intent; exporting `OpSeed`/`OpExpand`/`OpSchemaOverview` (vs. duplicating string literals in `agui`) keeps ONE wire-op source of truth shared by the validator and the plan-01 dispatcher. The internal dispatcher + tests were renamed to the exported names (a mechanical same-package rename).
- **Boot-time graph client is unconditional for serve** (distinct from chat.go's ReasoningLearning-gated client): the graph routes need a client regardless of the reasoning-learner flag. Best-effort open so a graph-explorer outage is never a daemon outage.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] bolt://neo4j DSN host+credential not redacted on the wire**
- **Found during:** Task 1 (the `TestGraphSchemaErrorSanitized` assertion)
- **Issue:** `SanitizeString`'s `secretPattern` only collapsed the five DB DSN schemes (postgres/mysql/mongodb/redis/amqp). A Neo4j graph read error carrying a `bolt://user:pass@host:7687` DSN had its userinfo redacted by the generic `urlUserinfoPattern` but left the HOST (`10.0.0.1`) exposed — a V13/T-27-05 "no raw DSN/host leak" violation, and the graph routes are the FIRST wire surface a bolt DSN error can reach.
- **Fix:** Widened `secretPattern` to include `bolt`/`bolt+s`/`bolt+ssc`/`neo4j`/`neo4j+s`/`neo4j+ssc` so the whole bolt DSN collapses to a scheme marker (host + credential both dropped).
- **Files modified:** `internal/agui/server.go` (then moved to `internal/agui/server_redact.go`)
- **Verification:** `TestGraphSchemaErrorSanitized` + `TestGraphQueryErrorSanitized` + `TestGraphQueryFilterSchemaError` assert no `secret`/`10.0.0.1`/`hunter2`/`topsecret` substring survives; the existing sanitizer tests still pass.
- **Committed in:** `9454b5f5` (Task 1 commit)

**2. [Rule 3 - Blocking / refactor-on-touch] server.go crossed the 600-LOC cap**
- **Found during:** Task 1 (the pre-commit `check-file-size` hook)
- **Issue:** The bolt-scheme widening + its comment pushed `server.go` to 608 LOC, over the CLAUDE.md 600-LOC cap; the commit hook blocked.
- **Fix:** Extracted the self-contained wire-error redaction controls (`secretPattern`/`urlUserinfoPattern`/`tokenPattern`/`sanitizeErr`/`SanitizeString`) into a new `server_redact.go`; `redactEvent` stayed in server.go with the SSE pump it guards. server.go is now 552 LOC.
- **Files modified:** `internal/agui/server.go`, `internal/agui/server_redact.go`
- **Verification:** `check-file-size` hook green; `go vet`/`go test -race ./internal/agui/` green; the redaction tests still pass.
- **Committed in:** `9454b5f5` (Task 1 commit)

---

**Total deviations:** 2 auto-fixed (1 missing-critical security, 1 blocking/refactor-on-touch)
**Impact on plan:** Both were correctness/security requirements directly in the plan's threat model (T-27-05/V13) and the CLAUDE.md 600-LOC discipline. No scope creep — the contract and route surface are exactly as planned.

## Issues Encountered
- **Op enum was unexported in plan 01:** `graphview_intent.go` declared `opSeed`/`opExpand`/`opSchemaOverview` unexported, but the REST validator (a different package) needs them. Resolved by exporting them and renaming the package-internal references (dispatcher + tests) — a single-source-of-truth fix, not a contract change.
- **`validateGraphIntent` initially at 75% coverage:** the over-long-id/over-long-token/too-many-entries/filter-schema-error branches were unexercised. Added a table-driven reject test + a filter-schema-error test + a labels-only filter test to reach 100% on both `validateGraphIntent` and `subset` (commit `af66a697`).

## Quality Gates
- `go vet ./internal/agui/ ./internal/knowledge/ ./cmd/aura/...` clean; `go build ./...` clean.
- `go test ./internal/agui/ ./internal/knowledge/ ./cmd/aura/...` green; `go test -race ./internal/agui/` green.
- `golangci-lint run ./internal/agui/ ./internal/knowledge/ ./cmd/aura/...` → 0 issues.
- New `graph_api.go` surface: `registerGraphRoutes`/`handleGraphSchema`/`handleGraphQuery`/`validateGraphIntent`/`subset` all 100%; `SetGraphView` 100%; `sanitizeErr`/`SanitizeString` 100%.
- All touched files ≤600 LOC (server.go 552 after the refactor-on-touch split).

## User Setup Required
None - no external service configuration required. The graph routes reuse the existing `NEO4J_*` / `AURA_MCP_NEO4J_CYPHER_BIN` / `AURA_NEO4J_BOLT_URL` env the boot-time `knowledge.Open` reads (no new secret); the `AURA_WEB_AUTH_SECRET`-derived RequireAuth gate is the existing Phase-24 boundary.

## Next Phase Readiness
- The frontend (plans 03/04) can now `GET /api/graph/schema` for the schema-overview default open and `POST /api/graph/query` with a structured `GraphIntent` for seed/expand. Both are authenticated, body-capped, input-validated, and error-sanitized.
- **Carry-forward (inherited from plan 01's live finding):** the production graph has 0 `:Conversation` nodes today, so a `seed` intent falls back to the schema overview — the default open the frontend renders WILL be the schema-overview empty-state, a first-class valid response (200 with the schema + an empty nodes/edges set), not an error. The frontend must render that empty-state as the primary default, not an edge case.

## Self-Check: PASSED

All 3 created files (`graph_api.go`, `graph_api_test.go`, `server_redact.go`) + the 5 modified files exist on disk; all 3 commits (`9454b5f5`, `a278c27f`, `af66a697`) are in the git history.

---
*Phase: 27-neo4j-graph-explorer*
*Completed: 2026-06-19*
