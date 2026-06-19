# Phase 27: Neo4j Graph Explorer - Context

**Gathered:** 2026-06-19
**Status:** Ready for planning

<domain>
## Phase Boundary

Deliver a **dedicated, read-only Neo4j evidence-graph workspace**. A NEW Go
normalizer (`internal/knowledge/graphview.go`) turns MCP Cypher rows into the
`{nodes, edges, paths, schema, query}` contract, served over **REST**
(`GET /api/graph/schema`, `POST /api/graph/query` — **not** the chat SSE stream),
and the cockpit renders it as an interactive **WebGL canvas** (Frame-06 three-pane:
guided query/filters left | node-link canvas center | node inspector right) with a
readable **path strip** below the canvas. Requirements: **GRAPH-01..04.**

The interaction model is **visual and point-and-click — the operator never writes
Cypher** (explicit operator directive 2026-06-19). The graph answers evidence/path
questions; it is **never a decorative hairball** (ROADMAP).

**Out of bounds (do NOT pull forward):**
- **Graph WRITES** — `add note` / any graph mutation, new persistence, PATCH routes,
  capability-gated write surfaces → **Phase 29 / follow-up**. This milestone is
  read-only (consistent with Phase 26 D-03 read-only Source Explorer + D-11 deferred
  feedback store). The `POST /api/graph/query` endpoint is read-only by construction.
- **`ui_control` operator-OS shell** (dockable tool windows, adaptive icon rail,
  command palette, AI-driven UI events; ux-spec Frame 07) → follow-up milestone
  (PROJECT.md §Deferred). The Graph Explorer is a **mode-swap workspace**, NOT a
  dockable window. Take NOTHING from odysseus's dock/tile/icon-rail machinery.
- **Raw-Cypher authoring by the end user** — rejected by operator directive. "Show
  Cypher" is a **read-only transparency affordance only**, never an input path.
- **Neo4j NVL / @neo4j-ndl** as the renderer — rejected (proprietary non-OSI license
  = liability for the commercial DGX-Spark bundle; design-system clash with the locked
  blue theme; heavier). **three.js / react-force-graph** (elysia/openhuman) also
  rejected (heavy, 3D-decorative-leaning, conflicts with "never a hairball").
- **Broader memory ingestion / new graph schema** — this phase READS the existing
  Neo4j graph (agent-memory POLE+O + documents). It does not enrich the schema.

</domain>

<decisions>
## Implementation Decisions

### Rendering engine (GRAPH-02)
- **D-01 — Renderer = Sigma.js v3 + graphology, lazy-loaded.** MIT-licensed, WebGL,
  purpose-built node-link rendering; graphology supplies the graph model + **ForceAtlas2
  layout** + **degree/community metrics** that drive label-family color, node sizing
  (degree/confidence), and evidence-path filtering. `@react-sigma` for React bindings.
  Proven in the curated `D:/tmp/llm_wiki` stack (sigma 3 + graphology + forceatlas2 +
  louvain). **Ship it as its own lazy-loaded chunk** so it never bloats the main
  single-binary bundle (the Phase-26 bundle-weight discipline that rejected recharts
  ~136KB applies here). Escalation only if a real scale problem appears; do NOT reach
  for NVL/three.js.
- **D-02 — Label-family color is SCHEMA-DRIVEN, not hard-coded.** The Frame-06 families
  (source / claim / entity / agent / topic / conflict) are a **semantic palette mapping**
  applied where live labels match; the real graph is the **agent-memory POLE+O** model +
  documents (`:Document`/`:Chunk`), whose labels are managed dynamically by the
  agent-memory MCP — NOT Aura migrations. Drive the legend + color encoding off the live
  `GET /api/graph/schema` label set; map known families to the brand palette, assign
  stable deterministic colors to unmapped labels. Never assume a label exists.
- **D-03 — A11y / no-WebGL path (Claude's discretion, mandated by the ux-spec rule).**
  Hover is NEVER the only access path: tap/focus opens the inspector on mobile + keyboard.
  Provide an accessible **node/edge list + tabular path strip** as the non-WebGL fallback
  and the screen-reader surface. Held to the enforced WCAG-AA contrast gate; canvas has an
  accessible name + keyboard node traversal.

### Query model + read-only guard (GRAPH-01, GRAPH-04)
- **D-04 — Interaction = guided point-and-click exploration. No Cypher typing.** The
  operator searches/picks a **starting node** (entity/source/document), toggles
  **label/edge-type filters**, and **clicks a node to expand neighbors intentionally**.
  These are **structured query intents**, not Cypher. The left panel's "Cypher preview"
  is **read-only transparency** (shows the compiled query), never an input.
- **D-05 — API contract = structured intents only; Go compiles parameterized read-Cypher.**
  `POST /api/graph/query` accepts a structured intent payload (seed id/criteria +
  label/edge filters + expand-neighbors op + caps), and `graphview.go` compiles it to
  **parameterized** `read_neo4j_cypher` calls via the existing `knowledge.Client.Read`
  (read-tx enforced at the MCP/bolt layer). **The client NEVER sends raw Cypher.**
  **Belt-and-suspenders guard:** a defensive server-side reject of write verbs
  (`CREATE/MERGE/SET/DELETE/DROP/REMOVE/CALL{...write}`) before dispatch, even though the
  intent compiler emits read-only Cypher — defense in depth on an authenticated REST
  endpoint. Parameterization (not string interpolation) is the primary injection defense.
- **D-06 — `GET /api/graph/schema`** returns the live label set, relationship types, and
  property keys (+ optional counts) — feeds the left-panel filters + the color legend +
  the schema-overview fallback (D-08). Cache shape is Claude's discretion (a short TTL or
  per-open call; the graph schema is small and slow-changing).

### Default view + dense-graph strategy (GRAPH-04)
- **D-07 — Default seed = the current conversation's memory-graph footprint, with
  fallback.** On open, seed the canvas from the **agent-memory nodes tied to the active
  conversation/session** (its episodes/observations/entities). The Phase-26 **web-citation
  registry is a SECONDARY deep-link** (a citation whose URL matches a `:Document`/`:Source`
  node opens that node), NOT the primary seed — web citations are rarely persisted as graph
  nodes. **Graceful fallback to the schema overview (D-08) when the thread has no graph
  footprint.** This keeps the deep-search "this conversation's evidence" immediacy without
  opening blank.
- **D-08 — Dense graphs default to FILTERED EVIDENCE PATHS, not hairballs.** Apply a default
  node/edge cap and a default evidence-oriented filter (e.g., paths-to-cited-sources / top-N
  by degree); the operator **expands neighbors intentionally**. The schema overview
  (label/rel-type map) is the empty-state + the no-footprint fallback. Exact caps + the
  default filter heuristic are Claude's discretion (tune for legibility).

### Node inspector + cross-links (GRAPH-03)
- **D-09 — Inspector = read-only set: pin-path + open-source + show-Cypher.** Selecting a
  node shows label / properties / degree (or confidence) / neighbors / citations. Actions:
  **pin/highlight a path** (client-side), **open the node's underlying source/document**
  (deep-link into the Phase-26 Source Explorer when it's a web source, or a document detail
  for `:Document`), and **show the read-only Cypher** that produced the view. **`add note`
  is DEFERRED** — it is a write (new persistence + capability gating), out of the read-only
  milestone; lands in Phase 29 / a follow-up.
- **D-10 — Selected path is highlighted on the canvas AND mirrored in the path strip**
  below the canvas (readable, keyboard/tap accessible — the non-hover access path).

### Surface placement
- **D-11 — Dedicated mode-swap workspace ONLY.** When `surface === 'graph'`, the cockpit
  center swaps to the Frame-06 three-pane Graph Explorer (replacing the chat lane), driven
  by the **already-declared `graph` mode** in `web/src/shell/modes.ts` (which renders
  nothing today — `AppShell` always mounts `ExternalStoreChat`). **No inline `graph_chunk`
  chat mini-preview** this phase (the Phase-26 router's extensible `default:` stays; an
  inline graph preview is a nicety, not in GRAPH-01..04, and adds a second render path). No
  dockable-window shell — that is the deferred `ui_control` milestone.

### Claude's Discretion (resolved by directive + research — no further operator input needed)
- The exact `{nodes, edges, paths, schema, query}` Go type shapes + the `graphview.go`
  normalizer layout (mirror the flat-struct discipline of `internal/agent/display` /
  `ChildReport`); the structured query-intent payload schema; the parameterized-Cypher
  templates the intent compiler emits.
- The `web/src/graph/` component layout (three-pane workspace, Sigma canvas wrapper, left
  filter/seed panel, right inspector, path strip); the lazy-chunk boundary.
- Default node/edge caps + the default evidence-path filter heuristic; the schema-overview
  rendering; empty/loading/error states (held to the locked design system).
- Schema-endpoint caching; i18n keys (en+it) for labels/filters/inspector/path-strip;
  whether the seed uses an existing agent-memory recall path or a dedicated REST seed query.
- **DEEP-RESEARCH MANDATE for the researcher** (operator directive: "deep research online
  + on `D:/tmp`, best not easiest"): study the curated graph-viz references before
  proposing the build — see `<specifics>` for the exact source list + online topics.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents (researcher + planner) MUST read these before planning or
implementing.**

### Phase scope & requirements
- `.planning/ROADMAP.md` §"Phase 27: Neo4j Graph Explorer" (lines ~201-214) — goal, the
  4 success criteria, GRAPH-01..04, depends-on Phase 26 (display router) + Phase 24
  (REST/auth boundary), UI hint: yes.
- `.planning/REQUIREMENTS.md` GRAPH-01..04 (lines ~70-73).
- `.planning/PROJECT.md` §"Current Milestone v1.0.0" (the Neo4j-Graph-Explorer bullet +
  single-binary `//go:embed` invariant) + §Deferred (`ui_control` shell deferral that
  bounds this phase to a mode-swap workspace, no dockable windows).

### UI/UX contract (UI hint: yes — consider /gsd-ui-phase)
- `docs/design/aura-deep-search-figma/ux-spec.md` **Frame 06 — Neo4j Graph Explorer**
  (lines ~227-247): dedicated workspace, left NL-query/Cypher-preview/label+edge filters/
  save-export-copy/saved-views, center node-link canvas (color=label family, size=degree/
  confidence, edge-label=rel type), selected-path highlight + path strip, right inspector
  (label/properties/degree/confidence/connected evidence/neighbors/citations + pin-path/
  open-source/show-Cypher/add-note), hover-never-only-access, dense→filtered-evidence-paths.
  Also §"Elysia Patterns Adopted" (~43-64) + §"Backend Capability Patterns" (~420-430) for
  the tool→display mapping. **NOTE:** ux-spec lists `add note` (a write) + dockable windows +
  `ui_control` — those are deferred, NOT Phase 27 (see `<domain>` out-of-bounds).

### Prior phase context (carried forward — do NOT re-decide)
- `.planning/phases/26-typed-display-protocol-router/26-CONTEXT.md` — the display router's
  extensible `default:` (graph_chunk deliberately deferred HERE), the **source registry +
  citations + read-only Source Explorer** (D-03/D-04/D-05) that the graph cross-links into,
  the image-proxy/`RequireAuth` `/api/` pattern, HARDEN-08 untrusted-output posture, the
  `messages[0]` KV-cache invariant.
- `.planning/phases/24-web-foundation-serve-auth-health/24-CONTEXT.md` — the SPA host +
  `/api/` exclusion carve-out + `RequireAuth` whole-origin gate every new `/api/graph/*`
  route inherits; non-loopback boot guard.
- `.planning/phases/23-frontend-infrastructure-industrial-foundation/23-CONTEXT.md` —
  React Router / React Query / the dark-operator (logo-matched **blue**) design-token theme
  + committed `web/dist` + Node-24 rebuild + CI freshness gate. The blue theme is why NVL's
  `@neo4j-ndl` design system is rejected (D-01).

### Backend graph access (LOCKED shape — reuse, do not re-invent)
- `internal/knowledge/client.go` — `Client.Read(ctx, query, params) ([]map[string]any, …)`
  over the `read_neo4j_cypher` MCP tool (read-tx enforced); `Cypher`/`Read`/`Write` split;
  D-06 fail-hard-on-crash policy; secret redaction. The `graphview.go` normalizer consumes
  `Read`'s row maps.
- `internal/knowledge/schema.go` — the Go-driver auto-commit path (schema DDL only; NOT used
  by the read-only query surface, but documents why MCP is the runtime read interface).
- `internal/knowledge/migrations/0001_init.cypher`, `0002_documents.cypher` — the only
  Aura-owned Neo4j schema (`:Document`/`:Chunk` + vector/fulltext indexes). The richer
  POLE+O memory schema is managed by the agent-memory MCP recipe, NOT here.
- `internal/mcp/manager/catalog.go` (~lines 163-175) — the `recipe:memory` agent-memory MCP
  (POLE+O + reasoning traces, streamable-HTTP, default-on). Confirms the live graph contents.

### REST handler + route pattern (model for `/api/graph/*`)
- `internal/agui/server.go` `Mux()` (~lines 127-144) — route registration; `/api/image-proxy`
  as the read-GET-behind-`RequireAuth` precedent.
- `internal/agui/conversations_api.go` — `writeJSON`/`writeJSONStatus`/`sanitizeErr` helpers +
  the thin-handler-over-store shape to mirror for the graph endpoints.
- `cmd/aura/serve_webui.go` — the `/api/` exclusion carve-out + `RequireAuth`/`RequireCapability`
  mount discipline (the new `/api/graph/*` routes register as siblings under the carve-out,
  NEVER a bare `/api/`).

### Frontend mount points
- `web/src/shell/modes.ts` — `MODES = ['chat','tree','graph','displays','settings']`; the
  `graph` SurfaceIntent already exists but renders nothing.
- `web/src/AppShell.tsx` (~lines 179, 215-246) — the center `<section>` that today always
  mounts `ExternalStoreChat`; this is where `surface==='graph'` must swap in the Graph
  Explorer (lazy `Suspense`). `useSurfaceIntent` drives the mode.
- `web/src/chat/displays/` — the Phase-26 Source Explorer (`SourceExplorerSheet.tsx`,
  `SourceExplorerContext.tsx`, `sourceExplorerData.ts`) + citation registry the inspector's
  "open source" deep-links into; `web/src/i18n/resources.ts` (en+it, rebuild `dist`).

### Architecture / stack / pitfalls (LOCKED shape)
- `.planning/research/ARCHITECTURE.md` (serve/embed + four-layer write-protection model),
  `.planning/research/STACK.md`, `.planning/research/PITFALLS.md`.
- `.planning/codebase/` maps — `STACK.md`, `INTEGRATIONS.md` (Neo4j/MCP), `CONVENTIONS.md`.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`internal/knowledge/client.go`** — `Client.Read` is the ONLY graph read path
  (`read_neo4j_cypher` over the MCP subprocess; read-tx enforced). `graphview.go` wraps it;
  no native Go Neo4j driver for reads (CLAUDE.md ban).
- **`internal/agent/display/`** — the Phase-26 normalizer package is the structural template
  for `graphview.go` (flat typed payload structs, deterministic mapping, table-tested).
- **`internal/agui/conversations_api.go` + `server.go` `Mux()`** — thin-JSON-handler +
  route-registration pattern; `image_proxy.go` = the read-GET-behind-`RequireAuth` precedent.
- **`web/src/shell/modes.ts` + `AppShell.tsx`** — the `graph` mode + the center swap seam
  (today hard-wired to `ExternalStoreChat`).
- **`web/src/chat/displays/SourceExplorerSheet.tsx` + source registry** — the inspector's
  "open source" cross-link target (Phase-26 read-only sheet).
- **`web/src/theme/` design tokens (blue)** — the label-family palette derives from here.

### Established Patterns
- **MCP-only graph reads** — every Cypher call rides `read_neo4j_cypher`; reads are read-tx
  by construction. Aura never links a native Go driver for runtime reads.
- **Thin HTTP adapters behind `RequireAuth`, under the `/api/` carve-out** — `/api/graph/schema`
  + `/api/graph/query` register as siblings (never a bare `/api/`); whole-origin gate inherited.
- **Parameterized Cypher, never interpolated** — the intent compiler emits parameter maps;
  belt-and-suspenders write-verb reject on top.
- **Minimal-industrial-shape** ([[feedback_no_atomic_bombs_minimal_industrial_shape]]) —
  Sigma+graphology lazy chunk, no NVL/three.js heavyweight.
- **Frontend quality gates** ([[feedback_frontend_quality_gates_coverage_mutation]]) —
  Vitest ≥85% + Stryker ≥70% killed + blocking CI; the WebGL canvas needs a testable seam
  (mock the renderer; unit-test the intent→Cypher compiler + normalizer + filter logic).
- **i18n** — `t('feature.key')`, en+it (`web/src/i18n/resources.ts`), rebuild `dist`.
- **Single-binary `//go:embed`** — the Sigma chunk ships inside `internal/webui/dist`.

### Integration Points
- Guided UI intent → `POST /api/graph/query` → `graphview.go` compiles parameterized
  read-Cypher → `knowledge.Client.Read` → `{nodes,edges,paths,schema,query}` JSON → Sigma
  canvas + path strip + inspector.
- `GET /api/graph/schema` → live label/rel-type set → left-panel filters + color legend +
  schema-overview fallback.
- Default seed: active conversation → its agent-memory footprint (recall/seed query) →
  filtered evidence subgraph; web-citation URL ↔ `:Document`/`:Source` node = secondary
  deep-link.
- Inspector "open source" → Phase-26 Source Explorer sheet / document detail.
- `surface==='graph'` (`useSurfaceIntent`) → `AppShell` center swaps to the lazy Graph
  Explorer workspace.

</code_context>

<specifics>
## Specific Ideas

- **Operator directive (2026-06-19):** "end user not able to query Cypher — we need
  something visual, easy to use" + "deep research online and on `D:/tmp`." This locks the
  guided point-and-click model (D-04/D-05) and mandates the research pass below.

- **DEEP-RESEARCH source list for the researcher** (curated, already surveyed for renderer
  selection — confirm + go deeper):
  - `D:/tmp/llm-graph-builder/frontend` — **Neo4j Labs' OWN** graph builder; uses `@neo4j-nvl`
    + `@neo4j-ndl`. Study its **graph-data normalization + node/edge interaction model +
    inspector**, but the renderer choice is REJECTED (license/weight/theme). Best reference
    for "how Neo4j Labs shapes Cypher rows into a graph view."
  - `D:/tmp/llm_wiki` — the **chosen stack in the wild**: `sigma` v3 + `graphology` +
    `forceatlas2` + `louvain`. Port the Sigma+graphology wiring/layout patterns.
  - `D:/tmp/logseq` — `graphology` + `pixi.js` + `d3-force` (alt WebGL-2D renderer reference;
    informs the lazy-chunk + layout approach).
  - `D:/tmp/elysia-frontend` — the Phase-26 load-bearing ref; its graph uses `three.js`
    (rejected as renderer) BUT it is the source for the **Source Explorer cross-link** + the
    typed-display router the inspector deep-links into.
  - `D:/tmp/graphify`, `D:/tmp/openhuman` — secondary graph-viz references.
  - Online: Sigma.js v3 (3.0.3, v4-beta) + graphology docs (ForceAtlas2, degree/community,
    `@react-sigma`), graph-viz **accessibility** patterns (keyboard traversal, SR fallback),
    Neo4j read-only/parameterized-Cypher best practice, "evidence-path vs hairball" filtering
    heuristics. Verified so far: Sigma = **MIT**; NVL = **proprietary non-OSI** ("SEE LICENSE").

- **Premium-but-industrial** ([[feedback_cockpit_premium_bar_over_minimal]],
  [[project_aura_dgx_spark_bundle_vision]]) — build the rich evidence-graph operators expect,
  but keep it MIT/lightweight/lazy and read-only-safe (no proprietary-license or hairball
  footguns).

</specifics>

<deferred>
## Deferred Ideas

- **Graph WRITES** — `add note` / annotations / any mutation → **Phase 29 / follow-up**
  (new persistence + capability gating; read-only milestone holds, per Phase-26 D-03/D-11).
- **Inline `graph_chunk` chat mini-preview** (small in-thread graph that expands into Graph
  mode, via the Phase-26 router `default:`) → optional follow-up; not in GRAPH-01..04.
- **Raw-Cypher power-user authoring** (editable Cypher input + run) → rejected by directive;
  revisit only if a power-operator persona is ever requested (would require an airtight guard).
- **Saved graph views / save-export-copy** (Frame 06 left-panel "saved views" + export) →
  export/copy of the current view is a possible cheap stretch; **persisted** saved views are
  a write surface → defer with the rest of the writes.
- **`ui_control` dockable Graph window / icon-rail / command palette** (ux-spec Frame 07) →
  follow-up milestone; this phase is a mode-swap workspace only.
- **NVL / three.js renderers** → evaluated + rejected (license/weight/theme/hairball);
  documented so they are not re-litigated.

### Reviewed Todos (not folded)
None — no pending todos matched Phase 27.

</deferred>

---

*Phase: 27-neo4j-graph-explorer*
*Context gathered: 2026-06-19*
