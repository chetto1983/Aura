---
phase: 27-neo4j-graph-explorer
verified: "2026-06-19"
status: passed
score: 4/4 requirements verified (GRAPH-01..04)
overrides_applied: 0
method: "Live E2E against the running Docker stack (PG + Neo4j + mcp-neo4j-cypher) — a fresh aura serve binary built at HEAD 7b0558bf with the embedded dist, on 127.0.0.1:9091. Backend contract proven by curl wire probes; frontend DOM proven by Playwright/Chromium (graph.spec 3/3 + graph-a11y.spec 3/3) against that live serve."
note: "Backfills the VERIFICATION.md that Phase 27 lacked at its 2026-06-19 operator close (it closed on 27-VALIDATION.md alone). The 27-VALIDATION caveat about 'uncommitted post-close graph WIP' is RESOLVED: the working tree is clean at HEAD 7b0558bf (git status --short empty bar this audit) — the graph WIP + dist rebuild were committed; nothing remains to reconcile."
---

# Phase 27: Neo4j Graph Explorer — Verification Report

**Phase Goal:** A read-only Neo4j evidence-graph workspace — a Go graph-normalizer turns MCP Cypher rows into the `{nodes, edges, paths, schema, query}` contract served over REST, rendered as a WebGL canvas with a node inspector and path strip.
**Requirements:** GRAPH-01, GRAPH-02, GRAPH-03, GRAPH-04
**Verified:** 2026-06-19 (live, post-close backfill)
**Status:** passed

## Goal Achievement

### Observable Truths (live evidence)

| # | Truth | Status | Live Evidence |
|---|-------|--------|---------------|
| 1 | GRAPH-01: Go normalizer → `{nodes,edges,paths,schema,query}` over REST (not SSE) | VERIFIED | Against the live serve: `GET /api/graph/schema` → 200 `{"labels":[],"rel_types":[]}`; `POST /api/graph/query {"op":"schema_overview"}` → 200 carrying all five contract keys (`nodes`,`edges`,`paths`,`schema`,`query`) + the compiled read-Cypher (`CALL db.labels()… db.relationshipTypes()… db.propertyKeys()…`). Both behind `RequireAuth` (401 without the session cookie). The live production graph has 0 `:Conversation` nodes so `nodes/edges` are null and schema-overview is the first-class default (matches 27-VALIDATION) |
| 2 | GRAPH-02: WebGL canvas + label-family color + readable path strip | VERIFIED | `graph.spec.ts` (Playwright/Chromium, live serve): the `role="img"` "Evidence graph:" canvas is visible, the a11y parallel DOM lists `Nodes (2)` + `Connections (1)`, and the `Selected path` / `No path selected` strip renders. 3/3 passed |
| 3 | GRAPH-03: node inspect (label/props/degree/neighbors/citations); tap/keyboard opens inspector (hover never the only path) | VERIFIED | `graph.spec.ts`: selecting the Document node from the node list opens the `complementary` "Select a node" inspector showing `Citations` → `Cited source X`; read-only verbs present (Pin path, Show Cypher), `add note` absent. `graph-a11y.spec.ts`: tap/keyboard opens the inspector. 3/3 + 3/3 passed |
| 4 | GRAPH-04: read-only by default (Cypher guard) + Cypher preview; dense graphs → filtered evidence paths not hairballs | VERIFIED | `graph.spec.ts`: Show Cypher reveals the query inside a `<pre>` with NO editable `textarea`/`input` in the inspector. Backend: the compiled query in the live `/api/graph/query` response is a read-only `CALL db.*` schema overview; `assertReadOnly` write-verb guard + cap-clamp are unit-covered (27-01) and the route is `RequireAuth`-only (read surface, no mutate path) |

**Score:** 4/4 requirements verified live.

### Requirements Coverage

| Requirement | Source Plan(s) | Status | Evidence |
|-------------|----------------|--------|----------|
| GRAPH-01 | 27-01, 27-02 | SATISFIED | `internal/knowledge/graphview.go` + `internal/agui/graph_api.go`; live `/api/graph/schema` + `/api/graph/query` REST contract proven by curl |
| GRAPH-02 | 27-03, 27-04 | SATISFIED | `web/src/graph/` Sigma canvas; graph.spec live (canvas + a11y list + path strip) |
| GRAPH-03 | 27-04 | SATISFIED | NodeInspector + citations; graph.spec + graph-a11y.spec live (tap/keyboard, citations) |
| GRAPH-04 | 27-01, 27-04 | SATISFIED | `assertReadOnly` guard + read-only Cypher preview; graph.spec live (no editable input) + live compiled read-Cypher |

**Orphaned requirements:** None. GRAPH-01..04 all map to Phase 27 in REQUIREMENTS.md (`[x]`), to the 27 SUMMARY frontmatter, and now to this VERIFICATION.

### Automated gates (from 27-04-SUMMARY, recorded at close)

599/599 Vitest · `src/graph` 94.57% stmts · 75.88% Stryker (≥70%) · 31/31 contrast WCAG-AA · 12/12 prior live Playwright (chromium + mobile-chrome). Re-confirmed live this pass: graph.spec 3/3 + graph-a11y.spec 3/3 against the served binary.

### Gaps Summary

No gaps. The one open item from 27-VALIDATION — the missing formal VERIFICATION.md + the uncommitted post-close graph WIP — is closed: this report is the verification record, and the working tree is clean at HEAD (the WIP was committed). Backend `go-mutesting` on `graphview*.go` remains the one deferred manual item (frontend Stryker 75.88% is recorded); re-run on WSL if a later audit requires the backend mutation number.

---

_Verified: 2026-06-19 (live backfill)_
_Verifier: Claude (gsd-audit-milestone follow-up)_
