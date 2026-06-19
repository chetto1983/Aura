# Phase 27: Neo4j Graph Explorer — Research

**Researched:** 2026-06-19
**Domain:** Go graph-row normalizer + read-only REST + injection-safe parameterized Cypher; WebGL node-link graph workspace (Sigma.js v3 + graphology) inside the embedded operator cockpit
**Confidence:** HIGH (renderer + wiring + REST pattern + mcp-row serialization all verified against live code and curated reference code; one Wave-0 empirical probe required for the exact mcp-neo4j-cypher node-serialization shape)

## Summary

Phase 27 is a read-only evidence-graph workspace with a clean two-sided contract. The **backend** adds `internal/knowledge/graphview.go` — a normalizer that compiles *structured query intents* (never raw Cypher) into **parameterized** read-only Cypher, runs them through the existing `knowledge.Client.Read` MCP path, and projects the resulting row maps into a flat `{nodes, edges, paths, schema, query}` JSON contract served over two REST routes (`GET /api/graph/schema`, `POST /api/graph/query`). The **frontend** adds `web/src/graph/` — a Frame-06 three-pane workspace (guided seed/filter panel | Sigma WebGL canvas | node inspector) plus a readable, keyboard-accessible path strip, mounted as a lazy `Suspense` chunk when `surface === 'graph'`.

The single highest-risk technical fact, now resolved: **mcp-neo4j-cypher serializes results via the Python driver's `result.data()` + a `_value_sanitize` pass that has NO special handling for Node/Relationship/Path objects** `[CITED: github.com/neo4j-contrib/mcp-neo4j .../utils.py]`. Worse, Aura's own code already documents that **list-valued columns come back NULL through the read tool** `[VERIFIED: internal/reasoningstore/store.go:31-35]`. Therefore the normalizer must NEVER `RETURN n` (label + element-id would be lost) and must NEVER return a bare `labels(n)` list. The proven Aura idiom is to project explicit scalar fields and JSON-encode any list with `apoc.convert.toJson(...)` (APOC is available in Aura's Neo4j `[VERIFIED: compose.yaml:256]`). This turns the riskiest unknown into a deterministic, testable Cypher-shape rule.

The renderer is **LOCKED** to `sigma@3.0.3` + `graphology@0.26.0` + `@react-sigma/core@5.0.6` + `graphology-layout-forceatlas2@0.10.1` (+ optional `graphology-communities-louvain@2.0.2`), all MIT, all verified current on npm, all 10+ years / 90K–1.25M weekly downloads, ~45–55 KB gzip combined as a **lazy chunk**. The curated `D:/tmp/llm_wiki/src/components/graph/graph-view.tsx` is a near-exact analog (same stack, React 19) and is the load-bearing wiring reference; `D:/tmp/llm-graph-builder/frontend` (Neo4j Labs) is the data-shaping reference (port the row→graph normalization, reject its `@neo4j-nvl` renderer).

**Primary recommendation:** Build the normalizer around explicit-field Cypher projections (`elementId(n) AS id, apoc.convert.toJson(labels(n)) AS labels_json, properties(n) AS props`), wrap `knowledge.Client` behind a narrow `GraphReader` interface (mirror `reasoningstore.GraphClient`), seed from the active conversation's agent-memory footprint via `(:Conversation {session_id:$session})-[:HAS_MESSAGE]->(:Message)-[:MENTIONS]->(:Entity)`, and render with the llm_wiki Sigma pattern split into a testable `intent→Cypher` + `rows→contract` + `applyFilters` core (Vitest/Go-table tested) and a thin renderer (mockable, no real WebGL in jsdom).

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01 — Renderer = Sigma.js v3 + graphology, lazy-loaded.** MIT, WebGL, `@react-sigma` bindings; graphology supplies the model + ForceAtlas2 layout + degree/community metrics. Ship as its OWN lazy chunk. No NVL/three.js escalation without a real scale problem.
- **D-02 — Label-family color is SCHEMA-DRIVEN, not hard-coded.** The Frame-06 families (source/claim/entity/agent/topic/conflict) are a semantic palette mapping applied where live labels match; drive the legend + color off the live `GET /api/graph/schema` set; map known families to the brand palette, assign stable deterministic colors to unmapped labels. Never assume a label exists.
- **D-03 — A11y / no-WebGL path (mandated by ux-spec).** Hover is NEVER the only access path: tap/focus opens the inspector on mobile + keyboard. Provide an accessible node/edge list + tabular path strip as the non-WebGL fallback + SR surface. Held to the WCAG-AA contrast gate; canvas has an accessible name + keyboard node traversal.
- **D-04 — Interaction = guided point-and-click. No Cypher typing.** Operator picks a starting node, toggles label/edge-type filters, clicks a node to expand neighbors. Structured query intents, not Cypher. The "Cypher preview" is read-only transparency, never an input.
- **D-05 — API contract = structured intents only; Go compiles parameterized read-Cypher.** `POST /api/graph/query` accepts a structured intent payload; `graphview.go` compiles it to **parameterized** `read_neo4j_cypher` calls via `knowledge.Client.Read`. The client NEVER sends raw Cypher. Belt-and-suspenders defensive server-side reject of write verbs (`CREATE/MERGE/SET/DELETE/DROP/REMOVE/CALL{...write}`) before dispatch. Parameterization (not interpolation) is the primary injection defense.
- **D-06 — `GET /api/graph/schema`** returns live label set, relationship types, property keys (+ optional counts) — feeds filters + color legend + schema-overview fallback. Cache shape is Claude's discretion.
- **D-07 — Default seed = the current conversation's memory-graph footprint, with fallback.** Seed from agent-memory nodes tied to the active conversation/session. The Phase-26 web-citation registry is a SECONDARY deep-link, NOT the primary seed. Graceful fallback to the schema overview (D-08) when the thread has no graph footprint.
- **D-08 — Dense graphs default to FILTERED EVIDENCE PATHS, not hairballs.** Default node/edge cap + default evidence-oriented filter; operator expands neighbors intentionally. Schema overview is the empty-state + no-footprint fallback. Exact caps + default-filter heuristic are Claude's discretion.
- **D-09 — Inspector = read-only set: pin-path + open-source + show-Cypher.** Selecting a node shows label/properties/degree(or confidence)/neighbors/citations. Actions: pin/highlight a path (client-side), open the node's underlying source/document (deep-link Phase-26 Source Explorer for a web source, or a document detail for `:Document`), show the read-only Cypher. **`add note` is DEFERRED** (a write → Phase 29).
- **D-10 — Selected path is highlighted on the canvas AND mirrored in the path strip** below the canvas (readable, keyboard/tap accessible — the non-hover access path).
- **D-11 — Dedicated mode-swap workspace ONLY.** When `surface === 'graph'`, the cockpit center swaps to the Frame-06 three-pane Graph Explorer (replacing the chat lane), driven by the already-declared `graph` mode. **No inline `graph_chunk` chat mini-preview** this phase. No dockable-window shell (`ui_control` deferred).

### Claude's Discretion (resolved by directive + research)
- The exact `{nodes, edges, paths, schema, query}` Go shapes + the `graphview.go` normalizer layout (mirror `internal/agent/display`); the structured query-intent payload schema; the parameterized-Cypher templates.
- The `web/src/graph/` component layout (three-pane workspace, Sigma canvas wrapper, left filter/seed panel, right inspector, path strip); the lazy-chunk boundary.
- Default node/edge caps + the default evidence-path filter heuristic; the schema-overview rendering; empty/loading/error states.
- Schema-endpoint caching; i18n keys (en+it) for labels/filters/inspector/path-strip; whether the seed uses an existing agent-memory recall path or a dedicated REST seed query.

### Deferred Ideas (OUT OF SCOPE)
- **Graph WRITES** — `add note`/annotations/any mutation → Phase 29.
- **Inline `graph_chunk` chat mini-preview** → optional follow-up; not in GRAPH-01..04.
- **Raw-Cypher power-user authoring** → rejected by directive.
- **Saved graph views / persisted save-export-copy** → write surface; defer. (Client-side export/copy of the *current* view is a possible cheap stretch.)
- **`ui_control` dockable Graph window / icon-rail / command palette** → follow-up milestone.
- **NVL / three.js renderers** → evaluated + rejected (license/weight/theme/hairball).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| GRAPH-01 | A Go graph-normalizer converts Neo4j MCP results to the `{nodes, edges, paths, schema, query}` contract (REST, not SSE) | §Normalizer Contract + §Standard Stack (Go) + the explicit-field-projection Cypher rule (the `result.data()`/list-NULL serialization findings) + the `display.Payload` flat-struct template |
| GRAPH-02 | Operator opens a WebGL canvas showing evidence paths with label-family color encoding + a readable path strip | §Sigma.js v3 Integration (llm_wiki wiring) + D-02 schema-driven legend + §Architecture Patterns (three-pane workspace, lazy chunk) + the path-strip mirror (D-10) |
| GRAPH-03 | Operator selects a node, inspects label/properties/degree/neighbors/citations; hover is never the only access path (tap/focus opens inspector on mobile + keyboard) | §Accessibility (parallel-DOM node/edge list, keyboard traversal, focus management) + §Inspector cross-links (Source Explorer deep-link) |
| GRAPH-04 | Read-only by default (read-only Cypher guard) with a Cypher preview; dense graphs default to filtered evidence paths, not hairballs | §Intent→Parameterized Cypher + §Write-verb guard + §Dense-graph caps/filter heuristic + §Default seed strategy |
</phase_requirements>

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Cypher execution against Neo4j | API / Backend (`knowledge.Client.Read` over mcp-neo4j-cypher) | Database (Neo4j Community, single `neo4j` DB) | CLAUDE.md bans a native Go driver for runtime reads; the MCP subprocess holds the only bolt connection and enforces read-tx |
| Intent → parameterized Cypher compilation | API / Backend (`graphview.go`) | — | Injection-safety + write-verb guard must live server-side; the browser never authors Cypher (D-05) |
| Row-map → `{nodes,edges,paths,schema,query}` normalization | API / Backend (`graphview.go`) | — | The mcp serialization quirks (list→NULL, node→property-dict) are a backend concern; the wire contract is clean typed JSON (mirror `internal/agent/display`) |
| Schema introspection (labels / rel-types / prop-keys / counts) | API / Backend (`GET /api/graph/schema`) | Database (APOC `db.*` procedures) | Live label set is unknown at build time (agent-memory manages it); must be a live query |
| Auth / capability gating | Frontend Server (SPA host) — `RequireAuth` whole-origin gate | — | Inherited from Phase-24 `/api/` carve-out; read GETs need RequireAuth only, no capability gate (read-only milestone) |
| Graph model + layout + community/degree metrics | Browser / Client (graphology + ForceAtlas2 + louvain) | — | Pure client computation on a capped subgraph; keeps the backend stateless and the payload lean |
| WebGL node-link rendering | Browser / Client (Sigma.js v3) | — | GPU-accelerated canvas; lazy chunk so it never weighs the main bundle |
| Accessible non-WebGL access path (node/edge list + path strip) | Browser / Client (parallel DOM) | — | Screen readers cannot read WebGL pixels; a DOM mirror is the only standards-compliant path (WCAG, D-03) |
| Default seed (conversation footprint) | API / Backend (dedicated seed Cypher) | — | `sessionID = Event.ThreadID`; the footprint is one parameterized Cypher over the same DB — no memory-MCP dependency |
| Inspector cross-link to source/document | Browser / Client (Phase-26 Source Explorer `openSources()`) | — | Reuses the existing read-only sheet; no new persistence |

## Standard Stack

### Core (frontend — D-01 LOCKED)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `sigma` | 3.0.3 | WebGL node-link renderer | The canonical OSS WebGL graph renderer (jacomyal/sigma.js, est. 2014); MIT; purpose-built for node-link, not 3D-decorative `[VERIFIED: npm registry — see Audit]` |
| `graphology` | 0.26.0 | Graph data model + algorithms | The standard graph model Sigma renders; 1.25M downloads/wk; supplies degree/neighbors/metrics `[VERIFIED: npm registry]` |
| `@react-sigma/core` | 5.0.6 | React 19 bindings (`SigmaContainer`, `useLoadGraph`, `useRegisterEvents`, `useSigma`) | The maintained React wrapper (sim51/react-sigma); used by the llm_wiki reference on React 19 `[VERIFIED: npm registry]` |
| `graphology-layout-forceatlas2` | 0.10.1 | Force-directed layout (`inferSettings` + `assign`) | The standard ForceAtlas2 impl; `barnesHutOptimize` for >50 nodes `[VERIFIED: npm registry]` |

### Supporting (frontend)
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `graphology-communities-louvain` | 2.0.2 | Community detection (optional color mode / clustering) | Only if a "community" color mode is wanted beyond label-family color (D-02). Optional — adds ~6 KB gzip `[VERIFIED: npm registry]` |
| `graphology-metrics` | 2.4.0 | Degree centrality + density | Node sizing by degree (Frame-06) can be done with `graph.degree(node)` directly; this pkg only if richer centrality is wanted `[VERIFIED: npm registry]` |
| `react-resizable-panels` | (already a candidate; llm_wiki uses `^4.9.0`) | Three-pane resize | OPTIONAL — Aura's AppShell uses a CSS grid; the three panes can be plain CSS-grid columns without a panel lib. Prefer the existing grid pattern unless drag-resize is required |

> Aura's frontend already has React 19.2, Vite 8, Vitest 4, Stryker 9, Playwright 1.61, i18next, react-router-dom 7, TanStack Query 5 `[VERIFIED: web/package.json]`. The four core graph packages are NOT yet installed (greenfield) `[VERIFIED: web/src/graph absent]`.

### Core (backend)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `internal/knowledge` `Client.Read` | (in-repo) | The ONLY runtime graph read path | CLAUDE.md bans a native Go driver for reads; `graphview.go` wraps this `[VERIFIED: internal/knowledge/client.go:226]` |
| Go stdlib `net/http` (1.22 method-pattern mux) | Go 1.26 | REST routes | Matches the no-router codebase posture (`agui.Server.Mux`) `[VERIFIED: internal/agui/server.go:127]` |
| `encoding/json` | Go 1.26 | Contract marshalling | `writeJSON`/`writeJSONStatus` helpers already exist `[VERIFIED: internal/agui/conversations_api.go:64]` |

No new Go dependencies are required. No new Go migration is required (the graph is read-only; labels are created by agent-memory/documents, not this phase).

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Sigma.js v3 | `@neo4j-nvl` (Neo4j Labs' own) | REJECTED (D-01): proprietary non-OSI "SEE LICENSE" → liability for the commercial DGX-Spark bundle; drags `@neo4j-ndl` design system that clashes with the locked blue theme; heavier. Used by `D:/tmp/llm-graph-builder` (port its data shaping, not its renderer). |
| Sigma.js v3 | `cytoscape.js` 3.34 | MIT fallback noted in REQUIREMENTS Open Decisions; more work, SVG/Canvas not WebGL by default. Sigma is lighter and WebGL-native. Keep as the documented fallback only. |
| Sigma.js v3 | `react-force-graph` / three.js (elysia/openhuman) | REJECTED (D-01): heavy, 3D-decorative-leaning, conflicts with "never a hairball". |
| graphology louvain (client) | GDS Leiden/PageRank (server, available) | GDS IS available `[VERIFIED: compose.yaml:256]`, but client-side louvain on a *capped* subgraph is simpler, stateless, and avoids server projection overhead. Use GDS only if cap sizes grow. |

**Installation (frontend):**
```bash
cd web && npm install sigma@3.0.3 graphology@0.26.0 @react-sigma/core@5.0.6 graphology-layout-forceatlas2@0.10.1
# optional: graphology-communities-louvain@2.0.2 graphology-metrics@2.4.0
```
Backend: no install.

**Version verification (done this session):**
```
npm view sigma version                        => 3.0.3   (modified 2026-06-09)
npm view graphology version                   => 0.26.0  (modified 2025-01-26)
npm view @react-sigma/core version            => 5.0.6   (modified 2025-12-01)
npm view graphology-layout-forceatlas2 version=> 0.10.1  (modified 2024-11-08)
npm view graphology-communities-louvain version=> 2.0.2  (modified 2024-12-17)
```

## Package Legitimacy Audit

> slopcheck 0.6.1 is installed locally but its `install` subcommand *actually installs* (no dry-run mode), so it was not run against these packages to avoid polluting `web/node_modules`. Legitimacy was verified manually via npm registry age + download counts + canonical source repos. These are the universally-used graph-viz packages, not slopsquat candidates.

| Package | Registry | Age | Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-----------|-------------|-----------|-------------|
| `sigma` | npm | since 2014-04-03 (12 yr) | ~208,536/wk | github.com/jacomyal/sigma.js | not run (manual) | Approved |
| `graphology` | npm | since 2016-08-24 (10 yr) | ~1,253,580/wk | github.com/graphology/graphology | not run (manual) | Approved |
| `@react-sigma/core` | npm | since 2022-03-06 (4 yr) | ~97,993/wk | github.com/sim51/react-sigma | not run (manual) | Approved |
| `graphology-layout-forceatlas2` | npm | (graphology org) | ~204,744/wk | github.com/graphology/graphology | not run (manual) | Approved |
| `graphology-communities-louvain` | npm | (graphology org) | ~93,564/wk | github.com/graphology/graphology | not run (manual) | Approved (optional) |

**Packages removed due to slopcheck [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

> Recommendation for the planner: gate the `npm install` behind a single `checkpoint:human-verify` task that re-runs `npm view <pkg> version` for each package (slopcheck dry-run equivalent) before the lockfile is committed — cheap defense-in-depth, consistent with the package-legitimacy protocol. No postinstall scripts of concern (these are pure JS libs; verify `npm view sigma scripts.postinstall` returns empty at install time).

## Architecture Patterns

### System Architecture Diagram

```
┌─────────────────────────── BROWSER (lazy chunk: web/src/graph/) ───────────────────────────┐
│                                                                                             │
│  ShellHeader mode=graph ──► AppShell center <section> swaps ExternalStoreChat → GraphExplorer│
│                                                                                             │
│  ┌─ LEFT: Seed + Filters ─┐   ┌─ CENTER: Sigma WebGL canvas ─┐   ┌─ RIGHT: Inspector ──────┐│
│  │ • search/pick seed node│   │ graphology Graph             │   │ label / props / degree  ││
│  │ • label/edge filters   │   │  + ForceAtlas2 layout        │   │ neighbors / citations   ││
│  │ • Cypher preview (RO)   │──►│  + nodeReducer/edgeReducer   │◄──│ pin-path | open-source  ││
│  └────────────────────────┘   │  (highlight/dim path)        │   │ | show-Cypher           ││
│        intent state            └──────────────┬───────────────┘   └─────────┬──────────────┘│
│                                               │ click/enter/leave + keyboard│ openSources()  │
│              ┌── PATH STRIP (DOM, keyboard/SR) ◄── selected path (D-10) ─────┘  └──► Phase-26 │
│              │   + parallel node/edge LIST (a11y, D-03)                          Source Explorer│
│              └───────────────────────────────────────────────────────────────────────────────┘
│                                  ▲ JSON {nodes,edges,paths,schema,query}                       │
└──────────────────────────────────┼────────────────────────────────────────────────────────────┘
            POST /api/graph/query   │   GET /api/graph/schema     (fetch; RequireAuth whole-origin)
                  (structured intent)│   (no body)
┌──────────────────────────────────┼──── GO DAEMON (single loopback http.Server) ───────────────┐
│  serve_webui mux  ── /api/graph/  ─► aguiServer.Mux ─► registerGraphRoutes                     │
│        (RequireAuth wrap)             handleGraphSchema / handleGraphQuery                      │
│                                          │                                                      │
│                                   graphview.go (NEW)                                            │
│     intent ─► validateIntent ─► compileCypher (parameterized) ─► assertReadOnly(query) ─►       │
│                                          │                                                      │
│                                   knowledge.Client.Read(ctx, cypher, params)                    │
│                                          │  []map[string]any (rows)                             │
│                                   normalizeRows ─► {nodes,edges,paths,schema,query}             │
└──────────────────────────────────┼──────────────────────────────────────────────────────────────┘
                                    │ read_neo4j_cypher (stdio JSON-RPC, read-tx enforced)
                          ┌─────────▼─────────┐
                          │ mcp-neo4j-cypher  │  result.data() + _value_sanitize → JSON
                          └─────────┬─────────┘
                          ┌─────────▼─────────┐  single DB "neo4j" (Community):
                          │      Neo4j        │  :Document/:Chunk (Aura) + :Conversation/:Message/
                          │  + APOC + GDS     │  :Entity{type:POLE+O}/:Preference/:Fact/:ReasoningTrace
                          └───────────────────┘  (agent-memory MCP writes these; Phase 27 only READS)
```

### Component Responsibilities (file → responsibility)
| File (NEW unless noted) | Responsibility |
|---|---|
| `internal/knowledge/graphview.go` | Intent validation, parameterized-Cypher compilation, write-verb guard, row→contract normalization. Keep ≤600 LOC; split into `graphview_intent.go` / `graphview_schema.go` / `graphview_normalize.go` on touch if needed (CLAUDE.md no-god-class) |
| `internal/knowledge/graphview_test.go` | Table tests for intent→Cypher, row→contract, write-verb guard (unit, no live Neo4j) |
| `internal/knowledge/graphview_integration_test.go` | `//go:build neo4j_integration` live shape assertions (the Wave-0 serialization gate) |
| `internal/agui/graph_api.go` (NEW) | Thin handlers `handleGraphSchema` / `handleGraphQuery` over a `GraphView` seam; `writeJSON`/`sanitizeErr` reuse; `registerGraphRoutes(mux)` |
| `internal/agui/server.go` (EDIT) | `Mux()` calls `s.registerGraphRoutes(mux)`; add `SetGraphView(GraphView)` setter (mirror `SetImageProxy`) |
| `cmd/aura/serve.go` (EDIT) | Construct the `GraphView` (wrapping a `knowledge.Client`) and `aguiServer.SetGraphView(...)` |
| `cmd/aura/serve_webui.go` (EDIT) | Register `/api/graph/schema` + `/api/graph/query` as siblings under the `/api/` carve-out (consts like `graphSchemaRoute`/`graphQueryRoute`); both read GETs/POSTs inherit `RequireAuth`, no capability gate (read-only) |
| `web/src/graph/GraphExplorer.tsx` (NEW, lazy) | Three-pane workspace shell + state (intent, selection, path) |
| `web/src/graph/SigmaCanvas.tsx` | `SigmaContainer` + `GraphLoader`/`EventHandler`/`HighlightManager`/reducers (port from llm_wiki) |
| `web/src/graph/SeedFilterPanel.tsx` | Seed search + label/edge filter toggles + read-only Cypher preview |
| `web/src/graph/NodeInspector.tsx` | Selected-node detail + pin-path / open-source / show-Cypher actions |
| `web/src/graph/PathStrip.tsx` | DOM path strip (D-10) + the a11y parallel node/edge list (D-03) |
| `web/src/graph/graphIntent.ts` | Pure intent state + filter logic (Vitest-testable, no WebGL) |
| `web/src/graph/graphApi.ts` | `fetch` wrappers for the two routes (typed contract) |
| `web/src/AppShell.tsx` (EDIT) | `surface==='graph'` swaps the center `<section>` to `<Suspense><GraphExplorer/></Suspense>` |
| `web/src/i18n/resources.ts` (EDIT, + optional `resources.graph.ts` split) | en+it keys for graph labels/filters/inspector/path-strip; rebuild `internal/webui/dist` |

### Pattern 1: Explicit-field Cypher projection (NEVER `RETURN n`)
**What:** Project every node/edge field as a named scalar; JSON-encode any list.
**When to use:** Every graph read in `graphview.go`.
**Why:** `result.data()` flattens nodes and `_value_sanitize` strips graph-object metadata; list columns return NULL through the read tool.
```cypher
// Source: design from internal/reasoningstore/store.go:31-35 (apoc.convert.toJson list idiom) +
// llm-graph-builder Utils.ts node shape (element_id/labels/properties) [CITED]
MATCH (c:Conversation {session_id:$session})-[:HAS_MESSAGE]->(:Message)-[m:MENTIONS]->(e:Entity)
WITH e LIMIT $node_cap
OPTIONAL MATCH (e)-[r]-(n)
WITH e, r, n LIMIT $edge_cap
RETURN
  elementId(e)                        AS id,
  apoc.convert.toJson(labels(e))      AS labels_json,   // labels() is a LIST -> NULL without toJson
  e.type                              AS entity_type,    // POLE+O sub-type lives on a property
  coalesce(e.name, e.canonical_name)  AS caption,
  properties(e)                       AS props,
  elementId(startNode(r))             AS src,
  elementId(endNode(r))               AS dst,
  type(r)                             AS rel_type,
  elementId(r)                        AS rel_id
```

### Pattern 2: Sigma + graphology React wiring (port from llm_wiki)
**What:** `SigmaContainer` with inner hook components; `useLoadGraph` builds the graphology `Graph`, runs ForceAtlas2, caches positions; `useRegisterEvents` binds `clickNode`/`enterNode`/`leaveNode`; `nodeReducer`/`edgeReducer` express highlight/dim for the pinned path (D-10).
```tsx
// Source: D:/tmp/llm_wiki/src/components/graph/graph-view.tsx (sigma 3 + graphology + react 19) [CITED]
<SigmaContainer
  key={sigmaKey}               // remount on resize — see Pitfall 1
  settings={{ renderEdgeLabels: true, labelRenderedSizeThreshold: 6,
    nodeReducer: (_n, a) => pinnedHighlightReducer(a),
    edgeReducer: (_e, a) => pinnedEdgeReducer(a) }}>
  <GraphLoader nodes={nodes} edges={edges} />        {/* useLoadGraph + forceAtlas2.assign */}
  <EventHandler onNodeClick={openInspector} />        {/* useRegisterEvents */}
  <HighlightManager pinnedPath={pinnedPath} />        {/* useSigma + setNodeAttribute/refresh */}
</SigmaContainer>
```
ForceAtlas2: `forceAtlas2.assign(graph, { iterations: 150, settings: { ...forceAtlas2.inferSettings(graph), barnesHutOptimize: nodes.length > 50 } })`, cache positions so re-renders don't re-layout.

### Pattern 3: Narrow consumer-side `GraphReader` seam (mockable backend)
```go
// Source: internal/reasoningstore/store.go:19-27 (the GraphClient idiom) [VERIFIED]
// GraphReader is the narrow Cypher seam graphview needs. *knowledge.Client satisfies it.
type GraphReader interface {
    Read(ctx context.Context, query string, params map[string]any) ([]map[string]any, error)
}
```
This lets `graphview_test.go` inject a fake returning canned `[]map[string]any` rows — unit-testing intent→Cypher + row→contract with zero live Neo4j.

### Pattern 4: Flat tagged-struct contract (mirror `display.Payload`)
The contract is a struct, not an interface, with `omitempty` so decode(encode) is identity (R1) — same discipline as `internal/agent/display/payload.go`.

### Recommended Project Structure
```
internal/knowledge/
├── graphview.go            # intent → Cypher → contract (≤600 LOC; split on touch)
├── graphview_test.go       # unit (fake GraphReader)
└── graphview_integration_test.go  # //go:build neo4j_integration — Wave-0 serialization gate
internal/agui/
├── graph_api.go            # thin handlers + registerGraphRoutes
└── graph_api_test.go       # httptest over a fake GraphView
web/src/graph/              # lazy chunk
├── GraphExplorer.tsx  SigmaCanvas.tsx  SeedFilterPanel.tsx
├── NodeInspector.tsx  PathStrip.tsx
├── graphIntent.ts (pure)  graphApi.ts (fetch)  types.ts
└── __tests__/              # vitest: graphIntent, normalization, filters (no WebGL)
```

### Anti-Patterns to Avoid
- **`RETURN n` / `RETURN p` / `RETURN collect(node)`** → loses labels + element_id through mcp serialization. Project explicit fields.
- **Returning a bare `labels(n)` or any list column** → comes back NULL through the read tool. Use `apoc.convert.toJson(...)`.
- **String-interpolating intent values into Cypher** → the whole point of D-05 is parameter maps (`$session`, `$labels`, `$node_cap`). Never `fmt.Sprintf` a value into the query body.
- **Importing/using a native Go Neo4j driver for reads** → CLAUDE.md ban; `schema.go`'s driver path is schema-DDL ONLY.
- **A bare `mux.Handle("/api/", ...)`** → shadows `/api/integrations/`. Register `/api/graph/schema` + `/api/graph/query` as specific siblings.
- **Hover as the only inspector trigger** → WCAG fail (D-03). Click/tap/focus + keyboard must open it.
- **Re-laying-out the graph on every React render** → cache ForceAtlas2 positions (llm_wiki `positionCache`).
- **Touching `messages[0]` / the SSE stream** → this is REST, not chat; the KV-cache invariant is untouched (see Pitfall 6).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Force-directed layout | A custom physics sim | `graphology-layout-forceatlas2` `inferSettings`+`assign` | Years of tuning (Barnes-Hut, gravity, scaling); the llm_wiki settings are proven |
| WebGL node-link rendering | A raw `<canvas>`/WebGL renderer | Sigma.js v3 | Camera, zoom, label collision, reducers, hit-testing all solved |
| Graph model (neighbors, degree, edge keys) | Adjacency maps in React state | graphology `Graph` | `graph.neighbors()`, `graph.degree()`, `addEdgeWithKey` dedup are first-class |
| Community/cluster detection | A clustering heuristic | `graphology-communities-louvain` (optional) | Only if a community color mode is wanted; otherwise skip |
| Cypher injection safety | Escaping/quoting helper | mcp `read_neo4j_cypher` param map + `assertReadOnly` guard | Parameterization is the real defense; escaping is a footgun |
| Schema introspection | Hard-coded label list | live `CALL db.labels()` / `apoc.meta.*` | The agent-memory label set is unknown at build time (D-02/D-06) |
| List extraction from mcp rows | Re-deriving lists from indexed columns | `apoc.convert.toJson(list)` + Go `json.Unmarshal` | The proven Aura idiom; lists return NULL otherwise |
| Source/document deep-link UI | A new source sheet | Phase-26 `SourceExplorerSheet` via `openSources()` | Already read-only, already wired, D-09 cross-link |

**Key insight:** Every "hard" part of a graph explorer (layout, rendering, graph algorithms, injection safety) has a battle-tested home in this stack. The *only* genuinely new code is the thin glue: the intent schema, the explicit-field Cypher templates, the row→contract mapping, and the three-pane wiring. Keep that glue small and testable.

## Runtime State Inventory

> Phase 27 is additive (new normalizer + new REST routes + new frontend chunk) — it does NOT rename or migrate anything. This section confirms there is no hidden runtime state to migrate.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | None — Phase 27 only READS the existing `neo4j` graph (Aura `:Document`/`:Chunk` + agent-memory `:Conversation`/`:Message`/`:Entity`/...). No new node/edge written. Verified by D-domain "read-only by construction". | None |
| Live service config | The agent-memory MCP recipe (`recipe:memory`) already exists and writes the POLE+O graph; Phase 27 adds NO MCP server and NO recipe change `[VERIFIED: internal/mcp/manager/catalog.go:163-175]`. | None |
| OS-registered state | None — no scheduler tasks, no daemons added. | None |
| Secrets/env vars | Reuses existing `NEO4J_*` / `AURA_MCP_NEO4J_CYPHER_BIN` / `AURA_NEO4J_BOLT_URL`. No new secret. (A schema-cache TTL, if added, would be an `AURA_GRAPH_*` env — Claude's discretion, optional.) | None / optional new `AURA_GRAPH_SCHEMA_TTL_SEC` |
| Build artifacts | The Sigma lazy chunk ships inside `internal/webui/dist` — after the npm install + `web` rebuild, the committed `dist` must be regenerated (Node-24 build) and re-committed; the CI freshness gate enforces this. | Rebuild + commit `internal/webui/dist` |

**Nothing found requiring data migration** — confirmed: this is an additive read-only feature.

## Common Pitfalls

### Pitfall 1: WebGL "could not find suitable program for node type circle" crash on resize
**What goes wrong:** Sigma's WebGL context corrupts when its container is resized by external layout changes (panel open/close, drag-resize, mode swap), throwing a fatal render error.
**Why it happens:** Sigma compiles GPU programs against the canvas at mount; an out-of-band resize invalidates them.
**How to avoid:** Remount Sigma via a `key={sigmaKey}` that increments after a layout change settles (debounced ~50–100 ms), and wrap the `SigmaContainer` in an `ErrorBoundary`. The llm_wiki reference does exactly this `[CITED: D:/tmp/llm_wiki/.../graph-view.tsx:485-524]` (a `MutationObserver` on `data-panel-resizing` + a layout-key effect). Aura's three-pane is a stable CSS grid, so the main trigger here is the mode swap and the inspector open/close — bump the key on those.
**Warning signs:** Blank canvas after toggling the inspector or switching modes; console error mentioning "suitable program".

### Pitfall 2: mcp-neo4j-cypher node serialization strips labels/element_id (THE crux)
**What goes wrong:** `RETURN n` yields only `n`'s property map — `labels` (which drives D-02 color) and `element_id` (which de-dupes nodes + anchors edges) are gone.
**Why it happens:** mcp serializes via `result.data()` + `_value_sanitize` which has no Node/Path/Relationship handling `[CITED: github.com/neo4j-contrib/mcp-neo4j .../server.py + utils.py]`. (The exact `data()` flattening varies by driver version — the empirical shape MUST be probed at Wave 0; the explicit-field projection is correct regardless.)
**How to avoid:** Pattern 1 — never `RETURN n`; project `elementId(n) AS id, apoc.convert.toJson(labels(n)) AS labels_json, properties(n) AS props`, and for relationships `elementId(r) AS rel_id, type(r) AS rel_type, elementId(startNode(r)) AS src, elementId(endNode(r)) AS dst`.
**Warning signs:** Nodes render with the "unmapped" fallback color (no label); edges fail to attach (missing endpoint ids).

### Pitfall 3: List-valued columns return NULL through the read tool
**What goes wrong:** A column whose value is a Cypher list (`labels(n)`, `collect(...)`, an embedding) comes back NULL via `read_neo4j_cypher`.
**Why it happens:** Documented Aura behavior `[VERIFIED: internal/reasoningstore/store.go:31-35, internal/toolselectstore/store.go:50]`.
**How to avoid:** `apoc.convert.toJson(theList)` in Cypher → `json.Unmarshal` the string in Go. Aura already does this for embeddings; reuse the idiom for `labels()`.
**Warning signs:** `labels_json` is empty/null in the row map.

### Pitfall 4: WebGL cannot be unit-tested in jsdom
**What goes wrong:** Vitest runs in jsdom which has no WebGL context; `new Sigma(...)` throws.
**Why it happens:** No GPU/canvas in the test env.
**How to avoid:** Split logic from rendering. Unit-test `graphIntent.ts` (intent state + filters), the row→contract typing, and the normalization in pure modules with NO Sigma import. Mock `@react-sigma/core` (or the whole `SigmaCanvas`) in component tests. Reserve the real WebGL render assertion for the Playwright e2e tier (a real browser). This split is also what hits the Vitest ≥85% + Stryker ≥70% gate without fighting jsdom.
**Warning signs:** Tests importing `SigmaCanvas` fail with WebGL errors.

### Pitfall 5: Live label set is unknown at build time
**What goes wrong:** Hard-coding the Frame-06 families (source/claim/entity/...) as labels mis-colors the graph — the real labels are `:Entity` (POLE+O as a `type` PROPERTY, not a label), `:Conversation`, `:Message`, `:Document`, `:Chunk`, `:Preference`, `:Fact`, `:ReasoningTrace`, etc.
**Why it happens:** The agent-memory MCP manages its schema dynamically `[VERIFIED: D:/tmp/agent-memory/.../graph/schema.py — :Entity{type:PERSON|ORGANIZATION|LOCATION|EVENT|OBJECT}]`; only `:Document`/`:Chunk` are Aura-owned `[VERIFIED: migrations/0001,0002]`.
**How to avoid:** Drive the legend + color off `GET /api/graph/schema` (D-02/D-06). Map known label *families* AND the `Entity.type` property values to the brand palette; assign deterministic stable colors (e.g. hash→palette) to anything unmapped. Treat `Entity.type` as a second color dimension, not just `labels[0]`.
**Warning signs:** Every entity is one color; POLE+O distinction is invisible.

### Pitfall 6: Confusing this REST surface with the chat SSE / KV-cache path
**What goes wrong:** A reviewer worries the graph touches `messages[0]` (the KV-cache invariant).
**Why it doesn't:** The graph endpoints are plain `net/http` JSON over `knowledge.Client.Read` — they never enter the agent loop, never emit `aura.display`/SSE, never mutate `messages[0]`. The contract is REST, not the chat stream (D-domain + ROADMAP SC1).
**How to confirm:** No import of the runner/SSE adapter in `graph_api.go`; no `Actions.Display` slot.

### Pitfall 7: Bundle-weight regression
**What goes wrong:** Importing Sigma into the main bundle bloats the single binary.
**How to avoid:** `React.lazy(() => import('./graph/GraphExplorer'))` so Vite emits a separate chunk loaded only when `surface==='graph'`. Combined gzip ~45–55 KB (sigma ~26 KB + graphology ~13 KB + react-sigma + forceatlas2) — fine for a lazy chunk; the Phase-26 recharts rejection was about the *main* bundle. Confirm a distinct chunk appears in the Vite build output.

## Code Examples

### Intent payload schema (D-05) + parameterized compile
```go
// Source: design grounded in display.Payload flat-struct discipline + reasoningstore GraphClient seam
type GraphIntent struct {
    Op        string   `json:"op"`         // "seed" | "expand" | "schema_overview"
    SeedID    string   `json:"seed_id,omitempty"`     // elementId for "expand"
    Session   string   `json:"session,omitempty"`     // active conversation ThreadID for "seed"
    Labels    []string `json:"labels,omitempty"`      // include-label filter (validated against schema set)
    RelTypes  []string `json:"rel_types,omitempty"`   // include-rel filter
    NodeCap   int      `json:"node_cap,omitempty"`    // default 75, hard max 300
    EdgeCap   int      `json:"edge_cap,omitempty"`    // default 200, hard max 800
}

// compileExpand emits a fully parameterized read-Cypher. Values ride the param map; only
// label/rel-type FILTER predicates are bound via parameters (WHERE label IN $labels), never
// interpolated as Cypher label syntax. Caps are ints validated to the hard max before binding.
func compileExpand(in GraphIntent) (cypher string, params map[string]any) {
    cypher = `MATCH (s) WHERE elementId(s) = $seed
OPTIONAL MATCH (s)-[r]-(n)
WHERE ($rel_types = [] OR type(r) IN $rel_types)
  AND ($labels = [] OR any(l IN labels(n) WHERE l IN $labels))
WITH s, r, n LIMIT $edge_cap
RETURN
  elementId(s) AS s_id, apoc.convert.toJson(labels(s)) AS s_labels, properties(s) AS s_props,
  elementId(n) AS n_id, apoc.convert.toJson(labels(n)) AS n_labels, properties(n) AS n_props,
  n.type AS n_entity_type,
  elementId(r) AS r_id, type(r) AS r_type, elementId(startNode(r)) AS r_src, elementId(endNode(r)) AS r_dst`
    return cypher, map[string]any{
        "seed":      in.SeedID,
        "labels":    nonNil(in.Labels),
        "rel_types": nonNil(in.RelTypes),
        "edge_cap":  clamp(in.EdgeCap, 200, 800),
    }
}
```

### Write-verb guard (belt-and-suspenders, D-05)
```go
// Source: design — defense-in-depth on top of the compiler-only-emits-reads invariant.
// The compiler never emits writes, but every dispatched query is re-checked.
var writeVerb = regexp.MustCompile(`(?i)\b(CREATE|MERGE|SET|DELETE|REMOVE|DROP|FOREACH)\b|CALL\s*\{[^}]*\b(CREATE|MERGE|SET|DELETE|REMOVE)\b`)

func assertReadOnly(cypher string) error {
    if writeVerb.MatchString(stripStringLiterals(cypher)) { // strip quoted strings first to avoid FP on data
        return fmt.Errorf("graphview: refusing non-read query (write verb detected)")
    }
    return nil
}
```
Test: table cases for each verb present/absent, verbs inside string literals (must NOT trip), verbs inside `CALL { ... }`, mixed case. (The read-tx is ALSO enforced at the mcp/bolt layer — this guard is the extra belt.)

### Default seed (D-07) — conversation footprint, graceful fallback
```cypher
// Source: D:/tmp/agent-memory queries.py:8,168,696 — Conversation->Message->Entity path [VERIFIED]
// sessionID = Event.ThreadID = cockpit activeThreadId [VERIFIED: internal/agent/llm_agent.go:39]
MATCH (c:Conversation {session_id:$session})-[:HAS_MESSAGE]->(:Message)-[:MENTIONS]->(e:Entity)
WITH DISTINCT e LIMIT $node_cap
OPTIONAL MATCH (e)-[r]-(n) WITH e, r, n LIMIT $edge_cap
RETURN elementId(e) AS s_id, apoc.convert.toJson(labels(e)) AS s_labels, e.type AS s_entity_type,
       coalesce(e.name, e.canonical_name) AS s_caption, properties(e) AS s_props,
       elementId(n) AS n_id, apoc.convert.toJson(labels(n)) AS n_labels, properties(n) AS n_props,
       elementId(r) AS r_id, type(r) AS r_type, elementId(startNode(r)) AS r_src, elementId(endNode(r)) AS r_dst
```
If zero rows → return the schema overview (D-08) instead of an empty canvas. The seed is a **direct Cypher over the same `neo4j` DB** — NOT a dependency on the memory MCP being mounted.

### Schema endpoint (D-06)
```cypher
// labels (list-valued → NULL through read tool, so toJson) + rel types + property keys + counts.
CALL db.labels() YIELD label RETURN collect(label) AS labels;       // run per-call OR cache (short TTL)
CALL db.relationshipTypes() YIELD relationshipType RETURN collect(relationshipType) AS rel_types;
// counts (optional, cheap on Community via APOC):
CALL apoc.meta.stats() YIELD labels, relTypesCount RETURN labels, relTypesCount;
// POLE+O sub-types (a property, not a label) for the legend's second dimension:
MATCH (e:Entity) RETURN apoc.convert.toJson(collect(DISTINCT e.type)) AS entity_types;
```
Because these return lists, prefer `apoc.convert.toJson(collect(...))` and unmarshal in Go, OR call `apoc.meta.schema()`/`apoc.meta.data()` (the same procedure mcp `get-schema` uses) for one-shot labels+rel-types+prop-keys `[CITED: neo4j-mcp-skill SKILL.md:269 — APOC powers get-schema]`. Caching: a short in-process TTL (e.g. 30–60 s) keyed on nothing (schema is global) is sufficient — the schema is small and slow-changing (D-06).

### Frontend lazy mount (D-11)
```tsx
// Source: AppShell.tsx center <section> seam [VERIFIED: web/src/AppShell.tsx:~215-246]
const GraphExplorer = React.lazy(() => import('./graph/GraphExplorer'));
// inside the center <section> (replacing ExternalStoreChat when surface==='graph'):
{surface === 'graph'
  ? <Suspense fallback={<GraphLoadingFallback/>}><GraphExplorer threadId={activeThreadId}/></Suspense>
  : <ExternalStoreChat threadId={activeThreadId} ... />}
```

### Inspector cross-link to Source Explorer (D-09)
```tsx
// Source: SourceExplorerContext.tsx openSources() [VERIFIED: web/src/chat/displays/SourceExplorerContext.tsx:30]
const { openSources } = useSourceExplorer();
// when a :Document/:Source node's url matches a Phase-26 citation, or for any document node:
onOpenSource={() => openSources(sourcesForNode(node), node.refId)}
```

## Accessibility (GRAPH-03 / D-03 — WCAG-AA gate)

WebGL is a black box to assistive tech — screen readers cannot read canvas pixels `[CITED: medium.com/@digital-anthro WebGL accessibility; vispero.com data-viz a11y]`. The standards-compliant pattern is a **parallel DOM structure** that mirrors the graph:

| Requirement | Concrete implementation |
|---|---|
| Non-hover access path | Click/tap a node opens the inspector; on keyboard, `Tab` into the canvas region then `Enter`/`Space` opens the focused node's inspector. NEVER hover-only. |
| Keyboard node traversal | A focusable, ordered DOM **node list** (the a11y mirror) with arrow-key/`Tab` navigation; `Enter` selects (same effect as clicking the canvas node). Maintain a "focused node" highlight synced to the canvas via `setNodeAttribute` + `sigma.refresh()`. |
| SR-readable structure | The node list + edge list rendered as semantic DOM (`role="list"`, each item naming label/type/degree/neighbors); the path strip (D-10) as an ordered list of `node —REL→ node` steps. `aria-label` on each canvas region. |
| Canvas accessible name | `<SigmaContainer>` wrapper gets `role="img"` (or `application`) + `aria-label` describing the current view; the real interaction lives in the parallel DOM, not the canvas. |
| Focus management | Opening the inspector moves focus into it; closing returns focus to the originating node-list item (standard dialog focus-return). |
| Contrast | Node/edge label text + legend chips meet **4.5:1** (normal) / **3:1** (large) `[CITED: .claude/skills/accessibility/SKILL.md:90-95]`. The locked blue theme tokens must be checked against the canvas background; do not rely on color alone to encode label family (also show label text / legend). |
| Reduced motion | Respect `prefers-reduced-motion`: skip/shorten the ForceAtlas2 animated settle and camera animations. |

The path strip (D-10) doubles as the primary accessible representation of a selected evidence path — it is keyboard/tap reachable and SR-readable by construction.

## Dense-graph strategy (GRAPH-04 / D-08 — "evidence paths, not hairballs")

| Lever | Default | Rationale |
|---|---|---|
| Node cap | 75 (hard max 300) | Sigma + ForceAtlas2 stay smooth into the low hundreds; 75 keeps the seed legible. `LIMIT $node_cap` in Cypher. |
| Edge cap | 200 (hard max 800) | `LIMIT $edge_cap` after the OPTIONAL MATCH expansion. |
| Default filter | Seed = conversation footprint (D-07); on overflow, prefer **top-N by degree** + **paths-to-cited-sources** (entities connected to `:Document`/`:Source` nodes that match Phase-26 citations) | Surfaces evidence over noise; matches "evidence/path questions, never a hairball" (ROADMAP). |
| Expand-on-demand | Clicking a node fetches ONLY its neighbors (`op:"expand"`, capped), appended to the current graph | The operator grows the view intentionally (D-04) instead of dumping the whole graph. |
| Empty/no-footprint | Render the **schema overview** (label/rel-type map from `GET /api/graph/schema`) | The empty-state + the no-footprint fallback (D-08). |
| Layout perf | `barnesHutOptimize: nodes.length > 50`, `iterations: 150`, position cache | The llm_wiki proven settings `[CITED]`. |

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Sigma.js v2 (vanilla, manual canvas) | Sigma.js v3 (3.0.x) + `@react-sigma/core` hooks | v3 GA; react-sigma 5.x for React 19 | Hook-based React integration; the llm_wiki reference uses exactly this on React 19 |
| `RETURN n` and trust the driver to hydrate | Explicit-field projection + `apoc.convert.toJson(list)` | n/a (mcp serialization constraint) | Deterministic, version-independent node/edge data through the MCP boundary |
| Hard-coded label→color maps | Schema-driven legend from `db.labels()`/`apoc.meta.*` | n/a (agent-memory dynamic schema) | Survives schema changes; POLE+O `type` property handled as a color dimension |
| `@neo4j-nvl` (Neo4j's bundled renderer) | MIT Sigma.js (renderer) + port NVL's data-shaping only | this phase (license decision) | Avoids proprietary-license + design-system-clash liability for the commercial bundle |

**Deprecated/outdated:**
- Sigma v2 `sigma.parsers` / vanilla instantiation → use `@react-sigma/core` `SigmaContainer` + hooks.
- Any plan to read the graph via a native Go Neo4j driver → CLAUDE.md ban; mcp `read_neo4j_cypher` only.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | mcp-neo4j-cypher `result.data()` flattens a `RETURN n` node to its property dict (labels/element_id lost). The exact shape varies by driver version and is NOT documented unambiguously online. | Pitfall 2, Normalizer | LOW — the explicit-field projection (Pattern 1) is correct regardless of `data()`'s exact node behavior; the Wave-0 integration test pins the real shape. |
| A2 | `labels(n)` (and other lists) return NULL through the read tool → must use `apoc.convert.toJson`. | Pitfall 3 | LOW — directly verified in two live Aura stores (reasoningstore, toolselectstore); Wave-0 confirms for `labels()` specifically. |
| A3 | The active conversation's agent-memory footprint is reachable via `(:Conversation {session_id:$threadID})-[:HAS_MESSAGE]->(:Message)-[:MENTIONS]->(:Entity)` and `session_id == Event.ThreadID`. | Default seed (D-07) | MEDIUM — verified from the agent-memory source queries + Aura's `sessionID = Event.ThreadID`; but whether Aura's loop actually WRITES `:Conversation`/`:Message` with that exact `session_id` (vs only entities) depends on which memory tools the loop calls. Wave-0 must probe a real conversation's footprint; if empty, the schema-overview fallback (D-08) still ships a working default. |
| A4 | POLE+O is a `type` property on `:Entity`, not separate `:Person`/`:Organization` labels. | Pitfall 5, D-02 | LOW — verified in `agent-memory/.../schema/models.py` + `entity_type_idx` on `Entity.type`. |
| A5 | Combined Sigma+graphology lazy chunk is ~45–55 KB gzip and acceptable as a lazy chunk. | Pitfall 7, Stack | LOW — sigma 26 KB + graphology 13 KB measured via bundlephobia; react-sigma + forceatlas2 add the rest. |
| A6 | `apoc.meta.schema()`/`apoc.meta.stats()` are available (APOC installed) for the schema endpoint. | Schema endpoint | LOW — APOC + GDS confirmed in compose.yaml. |
| A7 | The graph endpoints can reach a `knowledge.Client` opened for the serve lifetime (or lazily per-request). The daemon holds no long-lived graph client today. | Component table, serve.go | MEDIUM — planner must decide: open one `knowledge.Client` at boot for `SetGraphView` (simplest; one extra subprocess) vs lazy-open per request (no idle subprocess, higher per-call latency from connect+handshake). Recommend boot-time open behind the existing reverse-close teardown, mirroring how chat.go conditionally opens one. |

## Open Questions

1. **Does Aura's live loop actually populate `:Conversation`/`:Message` nodes with `session_id == ThreadID`, or only `:Entity` nodes?**
   - What we know: agent-memory's schema supports it; `sessionID = Event.ThreadID`; the recall integration test seeds + recalls a tagged memory.
   - What's unclear: whether the production loop calls `memory_store_message` (creating `:Conversation`/`:Message`) or only entity extraction — this determines whether the seed path matches on `:Conversation` or must start from `:Entity` with a session/episode property.
   - Recommendation: Wave-0 probe a real thread's footprint (`MATCH (c:Conversation) RETURN c.session_id LIMIT 5` then the full seed path). If `:Conversation` is absent, fall back to a session-scoped `:Entity`/`:Episode` seed; the schema-overview default (D-08) guarantees a non-blank open either way.

2. **Boot-time vs lazy `knowledge.Client` for the graph routes.**
   - What we know: the daemon holds no long-lived graph client; `chat.go` opens one conditionally.
   - Recommendation: open ONE at boot, register via `SetGraphView`, append `Close` to the existing `mcpClosers` reverse-close teardown. Lazy-open only if an idle-subprocess cost is a concern.

3. **Schema-cache TTL value + env var.**
   - Recommendation: 30–60 s in-process TTL, optional `AURA_GRAPH_SCHEMA_TTL_SEC` (default 60). Per-open call is also acceptable (the schema query is cheap). Claude's discretion per D-06.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Neo4j (Community) + APOC + GDS | schema endpoint, all reads | ✓ | 5.26 / single `neo4j` DB | — (APOC powers `get-schema`; GDS unused unless cap grows) |
| `mcp-neo4j-cypher` on PATH | `knowledge.Client.Read` | ✓ (WSL pipx `~/.local/bin`, v0.6.0) | 0.6.0 | none — it is the only read path |
| agent-memory MCP (`recipe:memory`) | the POLE+O graph CONTENT (not the read path) | ✓ default-on recipe | streamable-HTTP sidecar | If unmounted, the graph simply has no agent-memory nodes; reads still work on `:Document`/`:Chunk`; seed falls back to schema overview |
| Node 24 (frontend build) | rebuilding `internal/webui/dist` with the Sigma chunk | ✓ (multi-stage Docker / WSL) | 24 | none — the committed `dist` must be regenerated |
| npm registry access | installing the 4 graph packages | ✓ | — | offline install impossible; gate behind a checkpoint |

**Missing dependencies with no fallback:** none.
**Missing dependencies with fallback:** agent-memory MCP (graph content only) — the explorer degrades to documents + schema overview if it is ever unmounted.

## Validation Architecture

> nyquist_validation = true `[VERIFIED: .planning/config.json:19]`. This section is REQUIRED.

### Test Framework
| Property | Value (backend) | Value (frontend) |
|----------|-----------------|------------------|
| Framework | Go `testing` + table tests (`golang-testing` skill) | Vitest 4 (`web/package.json` `test`) + Playwright 1.61 (`test:e2e`) + Stryker 9 (`mutation`) |
| Config file | none (std `go test`) | `vite.config`/`vitest`, `playwright.config`, `stryker.conf` (existing) |
| Quick run command | `go test ./internal/knowledge/ ./internal/agui/` | `cd web && vitest run web/src/graph` |
| Full suite command | `go test -tags 'db_integration neo4j_integration' ./...` (WSL, live stack) | `cd web && vitest run --coverage && playwright test && stryker run` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File |
|--------|----------|-----------|-------------------|------|
| GRAPH-01 | intent→parameterized Cypher (no interpolation; caps clamped) | unit | `go test ./internal/knowledge/ -run TestCompile -x` | ❌ Wave 0 `graphview_test.go` |
| GRAPH-01 | row map → `{nodes,edges,paths,schema,query}` mapping (labels via toJson, edge endpoints) | unit | `go test ./internal/knowledge/ -run TestNormalize -x` | ❌ Wave 0 |
| GRAPH-01 | live mcp serialization shape pinned (the Wave-0 gate) | integration | `go test -tags 'db_integration neo4j_integration' ./internal/knowledge/ -run TestGraphViewLive` | ❌ Wave 0 `graphview_integration_test.go` |
| GRAPH-01 | `GET /api/graph/schema` + `POST /api/graph/query` return contract JSON, 400 on bad intent, 401 unauth | integration | `go test ./internal/agui/ -run TestGraph` (httptest + fake GraphView) | ❌ Wave 0 `graph_api_test.go` |
| GRAPH-02 | label-family color from schema set (known family + unmapped deterministic) | unit | `vitest run web/src/graph -t color` | ❌ Wave 0 `graphIntent.test.ts` |
| GRAPH-02 | canvas renders nodes/edges; path strip lists the selected path | e2e | `playwright test graph.spec` | ❌ Wave 0 `web/e2e/graph.spec.ts` |
| GRAPH-03 | tap/focus opens inspector (NOT hover-only); keyboard node traversal; SR node/edge list present | a11y/e2e | `playwright test graph-a11y.spec` (axe + keyboard) | ❌ Wave 0 |
| GRAPH-03 | inspector shows label/props/degree/neighbors/citations; open-source deep-links Source Explorer | unit + e2e | `vitest run web/src/graph -t inspector` + playwright | ❌ Wave 0 |
| GRAPH-04 | write-verb guard rejects CREATE/MERGE/SET/DELETE/DROP/REMOVE (incl. CALL{}); ignores verbs in string literals | unit | `go test ./internal/knowledge/ -run TestAssertReadOnly -x` | ❌ Wave 0 |
| GRAPH-04 | dense graph caps applied; schema-overview fallback on empty seed; Cypher preview matches compiled query | unit + e2e | `go test -run TestCap` + `vitest -t fallback` + playwright | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./internal/knowledge/ ./internal/agui/` + `vitest run web/src/graph`.
- **Per wave merge:** `go test -tags 'db_integration neo4j_integration' ./internal/knowledge/ ./internal/agui/` (live stack) + `vitest run --coverage` + `playwright test graph`.
- **Phase gate:** full Go matrix green + `make coverage` (owned-surface ≥85%, `internal/knowledge` + `internal/agui` included) + frontend Vitest ≥85% + Stryker ≥70% killed on `web/src/graph` + Playwright graph e2e green, before `/gsd-verify-work`.

### Quality Gates (project floors)
- **Go owned-surface ≥85%** (CLAUDE.md COVERAGE FLOOR; overrides PRD ≥75/60). `graphview.go` + `graph_api.go` are owned surface.
- **Frontend Vitest ≥85% + Stryker ≥70% killed + blocking CI** `[reference: feedback_frontend_quality_gates_coverage_mutation]`. The pure `graphIntent.ts`/normalization/filter modules carry the coverage; the WebGL `SigmaCanvas` is mocked in unit and exercised in Playwright.
- **No-skip-as-green:** the `neo4j_integration` tier `t.Fatal`s under `$CI` when its env is unset — a skipped graph integration test fails the gate, never passes it.
- **Mutation spot-check** on the write-verb guard + the cap-clamp + the intent compiler (the security/correctness-critical logic).

### Wave 0 Gaps
- [ ] `internal/knowledge/graphview_test.go` — intent→Cypher, normalize, assertReadOnly (GRAPH-01/04)
- [ ] `internal/knowledge/graphview_integration_test.go` (`//go:build neo4j_integration`) — pin the live mcp serialization shape + seed-path footprint probe (resolves A1/A3)
- [ ] `internal/agui/graph_api_test.go` — httptest over a fake `GraphView` (route registration, 400/401, contract shape)
- [ ] `web/src/graph/__tests__/graphIntent.test.ts` — filter/color/intent logic (no WebGL)
- [ ] `web/e2e/graph.spec.ts` + `web/e2e/graph-a11y.spec.ts` — render + keyboard/axe (GRAPH-02/03)
- [ ] Mock module for `@react-sigma/core` (or the `SigmaCanvas`) so component tests run in jsdom
- [ ] Stryker scope extension to include `web/src/graph`

## Security Domain

> `security_enforcement` not set to false → enabled. The graph surface is read-only and authenticated, but the parameterized-Cypher + untrusted-output posture matter.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes | Inherited Phase-24 `RequireAuth` whole-origin gate on the `/api/` carve-out; read GETs/POSTs need no capability gate (read-only milestone) |
| V3 Session Management | yes (inherited) | The in-binary signed session cookie (HttpOnly+Secure+SameSite=Strict) from Phase 24/WEB-03 |
| V4 Access Control | yes | Read-only by construction; no write/PATCH routes; `add note` deferred. Belt-and-suspenders write-verb guard. |
| V5 Input Validation | yes | Intent payload validated server-side: `op` enum, `seed_id`/`session` length-capped, `labels`/`rel_types` validated against the live schema set, caps clamped to hard max, `http.MaxBytesReader` on the POST body (mirror `maxRunBodyBytes`) |
| V6 Cryptography | no | No new crypto |
| V12 Files/Resources | n/a | No file I/O |
| V13 API / Web Service | yes | JSON-only; `sanitizeErr`/`SanitizeString` on every wire error (HARDEN-08 untrusted-output posture — graph node properties are untrusted content; never reflect raw strings into HTML; the cockpit renders text, not markup) |

### Known Threat Patterns for {Go REST over Neo4j MCP + WebGL frontend}
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Cypher injection via intent values | Tampering / Elevation | Parameter maps only (`$session`, `$labels`, `$node_cap`); NEVER `fmt.Sprintf` into the query body (D-05). Labels/rel-types bound as data in `WHERE x IN $list`, not as Cypher identifiers. |
| Write via the read endpoint | Tampering | Read-tx enforced at mcp/bolt layer + `assertReadOnly` write-verb guard before dispatch (defense in depth). |
| Prompt-injection / XSS via graph node property strings | Tampering / untrusted output | Treat all node/edge properties as untrusted (HARDEN-08); render as text, escape, never `dangerouslySetInnerHTML`; `SanitizeString` on backend error strings. |
| Resource exhaustion (huge subgraph / unbounded expand) | DoS | Hard node/edge caps clamped server-side (300/800); `LIMIT` in every Cypher; `MaxBytesReader` on the POST body. |
| Secret/internal leak in errors | Info Disclosure | `sanitizeErr`/`redactSecrets` (the knowledge client already redacts the Neo4j password); never surface raw Cypher errors with DSN/host. |
| SSRF via "open source" deep-link | Tampering | The cross-link reuses Phase-26 Source Explorer (read-only) + the existing SSRF-hardened image-proxy for any external fetch; the graph adds no new outbound fetch. |

## Sources

### Primary (HIGH confidence)
- `internal/knowledge/client.go` (`Client.Read`, `decodeRows`, mcp envelope) `[VERIFIED]`
- `internal/knowledge/migrations/0001_init.cypher`, `0002_documents.cypher` (Aura-owned `:Document`/`:Chunk` only) `[VERIFIED]`
- `internal/reasoningstore/store.go` + `internal/toolselectstore/store.go` (the `apoc.convert.toJson` list idiom + the `GraphClient` narrow seam + list-NULL/list-param mcp quirks) `[VERIFIED]`
- `internal/agent/display/payload.go` (the flat tagged-struct contract template) `[VERIFIED]`
- `internal/agui/server.go` `Mux()` + `conversations_api.go` (`writeJSON`/`sanitizeErr` thin-handler) + `image_proxy.go` (`SetImageProxy` setter, read-GET-behind-RequireAuth) `[VERIFIED]`
- `cmd/aura/serve.go` (composition root, `aguiServer.Set*`) + `cmd/aura/serve_webui.go` (the `/api/` carve-out + RequireAuth mount discipline) + `cmd/aura/chat.go:221` (conditional `knowledge.Open`) `[VERIFIED]`
- `internal/agent/llm_agent.go:39` (`sessionID = Event.ThreadID`) `[VERIFIED]`
- `internal/mcp/manager/catalog.go:163-175` (`recipe:memory` agent-memory MCP, POLE+O) `[VERIFIED]`
- `web/src/shell/modes.ts`, `web/src/shell/useSurfaceIntent.ts`, `web/src/AppShell.tsx` (the `graph` mode + center-swap seam), `web/src/chat/displays/SourceExplorerContext.tsx` (`openSources()`) `[VERIFIED]`
- `compose.yaml:256` (Neo4j + APOC + GDS) `[VERIFIED]`
- `D:/tmp/llm_wiki/src/components/graph/graph-view.tsx` + `src/lib/wiki-graph.ts` + `graph-filters.ts` (the Sigma v3 + graphology + ForceAtlas2 React-19 wiring; the resize-remount crash workaround; the pure filter logic) `[CITED]`
- `D:/tmp/agent-memory/src/neo4j_agent_memory/{graph/schema.py,graph/queries.py,schema/models.py,mcp/_tools.py}` (the live label set, the Conversation→Message→Entity seed path, POLE+O as `Entity.type`, `memory_search`/`get_context` tools) `[CITED]`
- `D:/tmp/llm-graph-builder/frontend/src/utils/Utils.ts` (`constructQuery` path-unwind RETURN shape, `processGraphData` node `{element_id, labels, properties}` mapping — port the data shaping, reject the NVL renderer) `[CITED]`

### Secondary (MEDIUM confidence)
- `github.com/neo4j-contrib/mcp-neo4j .../server.py` + `utils.py` (`result.data()` + `_value_sanitize` with no Node/Path handling) `[CITED]` — verified via raw GitHub fetch this session
- npm registry (`npm view`) + npm download API + bundlephobia for the four core packages `[VERIFIED]`
- `.claude/skills/neo4j-mcp-skill/SKILL.md` (`get-schema` returns labels/rel-types/prop-keys/indexes; APOC powers it; APOC auto-included on Aura) `[CITED]`
- `.claude/skills/neo4j-cypher-skill/SKILL.md` (`db.schema.visualization()`, `apoc.meta.schema()`, `db.schema.nodeTypeProperties()`) `[CITED]`
- `.claude/skills/accessibility/SKILL.md` (WCAG 2.2 contrast 4.5:1 / 3:1) `[CITED]`
- WebGL-a11y articles (parallel-DOM pattern for screen readers) `[CITED: medium.com, vispero.com]`

### Tertiary (LOW confidence — flagged for Wave-0 validation)
- The exact `Result.data()` node-flattening behavior across mcp-neo4j-cypher's pinned driver version (online docs ambiguous) → resolved by the explicit-field-projection design + the Wave-0 integration probe (A1).
- Whether the production loop writes `:Conversation`/`:Message` with `session_id == ThreadID` (vs entities only) → Wave-0 footprint probe (A3).

## Metadata

**Confidence breakdown:**
- Standard stack (renderer + versions): HIGH — locked by directive; all four packages verified current + legitimate on npm; llm_wiki proves the exact stack on React 19.
- Backend normalizer contract + Cypher shape: HIGH — the serialization constraint is grounded in live Aura code (two stores) + the mcp source; explicit-field projection is correct regardless of driver-version nuance; one Wave-0 probe pins the empirical shape.
- REST/route + frontend mount seams: HIGH — every file/line verified against the live tree.
- Default-seed availability: MEDIUM — the seed PATH is verified; whether the production loop populates `:Conversation`/`:Message` needs a Wave-0 probe (graceful fallback guarantees a working default either way).
- Accessibility approach: HIGH — the parallel-DOM pattern is the documented standard; WCAG thresholds from the project skill.

**Research date:** 2026-06-19
**Valid until:** 2026-07-19 (stack is stable; re-verify npm versions if the install slips past this window)
