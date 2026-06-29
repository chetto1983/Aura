# Phase 27: Neo4j Graph Explorer - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-19
**Phase:** 27-neo4j-graph-explorer
**Areas discussed:** Rendering engine, Query model + Cypher guard, Surface placement, Default view + actions

**Operator steering directives (folded as locked constraints):**
- "End user not able to query Cypher — we need something visual, easy to use." → guided
  point-and-click model; no raw-Cypher authoring; "show Cypher" is read-only transparency.
- "Deep research online and on `D:/tmp`, best not easiest." → curated graph-viz source list
  surveyed for renderer selection + a deep-research mandate written into CONTEXT.md.

---

## Rendering engine

| Option | Description | Selected |
|--------|-------------|----------|
| Sigma.js v3 + graphology | MIT, WebGL, lightweight, lazy chunk; graphology ForceAtlas2 + degree/community drive color/size/filter; proven in D:/tmp/llm_wiki. | ✓ |
| Neo4j NVL (@neo4j-nvl) | Authoritative for Neo4j (Bloom/Explore, llm-graph-builder) but proprietary non-OSI license + @neo4j-ndl design clash + heavier. | |
| three.js / react-force-graph | What elysia/openhuman use; heavy (~600KB+), 3D-decorative-leaning, conflicts with "never a hairball". | |

**User's choice:** Sigma.js v3 + graphology (Recommended).
**Notes:** Research confirmed Sigma = MIT, NVL = "SEE LICENSE" (proprietary, non-OSI) — a
liability for the commercial DGX-Spark bundle + clashes with the locked blue operator theme.
A11y/no-WebGL fallback (accessible node/edge list + tabular path strip) is Claude's discretion.

---

## Query model + Cypher guard

| Option | Description | Selected |
|--------|-------------|----------|
| Guided point-and-click exploration | Pick a seed node, toggle label/edge filters, click to expand neighbors; Go compiles structured intents to parameterized read-Cypher; "show Cypher" read-only only. | ✓ |
| NL search box → backend generates Cypher | LLM generates Cypher from a natural-language question; adds a round-trip + generation-correctness + guard risk. | |
| Both: guided primary + NL box | Point-and-click everyday + optional NL question box; richest but more to build + NL guard/cost. | |

**User's choice:** Guided point-and-click exploration (Recommended).
**Notes:** Directly satisfies the operator's "no Cypher / visual easy" directive.

### Sub-decision — POST /api/graph/query contract + guard depth

| Option | Description | Selected |
|--------|-------------|----------|
| Structured intents only; Go compiles parameterized read-Cypher | Client sends intents (seed + filters + expand), never raw Cypher; parameterized read_neo4j_cypher + defensive write-verb reject. | ✓ |
| Server-generated Cypher strings + guard | Backend builds Cypher strings + Go string-parse guard; simpler templating, more injection surface. | |

**User's choice:** Structured intents only (Recommended).
**Notes:** Strongest security + matches "users don't write Cypher"; parameterization is the
primary injection defense, write-verb reject is belt-and-suspenders on the authenticated route.

---

## Surface placement

| Option | Description | Selected |
|--------|-------------|----------|
| Dedicated mode-swap workspace only | surface==='graph' swaps the center to the Frame-06 three-pane workspace; uses the already-declared `graph` mode; no dockable shell. | ✓ |
| Dedicated workspace + inline graph_chunk preview | Also a small in-chat graph mini-preview (Phase-26 router default) that expands into Graph mode; extra render path, not in GRAPH-01..04. | |

**User's choice:** Dedicated mode-swap workspace only (Recommended).
**Notes:** Matches the ux-spec ("dedicated Neo4j workspace rather than nesting graph data in
cards"); no `ui_control` dock machinery (deferred milestone).

---

## Default view + actions

| Option | Description | Selected |
|--------|-------------|----------|
| Seed from current conversation's cited evidence | Open onto the active conversation's evidence subgraph; expand from there. | ✓ |
| Schema overview (label + rel-type counts) | Open onto a schema-level map; always works even with no conversation. | (fallback) |
| Empty canvas + query prompt | Blank canvas; operator starts every exploration. | |

**User's choice:** Seed from current conversation's cited evidence (Recommended).
**Notes:** Refined during discussion after verifying the graph = agent-memory POLE+O +
documents (NOT the ephemeral web-citation registry). Locked precision: seed = the
conversation's **memory-graph footprint**, web-citation URL ↔ `:Document`/`:Source` node as a
**secondary deep-link**, **graceful fallback to the schema overview** when the thread has no
graph footprint.

### Sub-decision — default-view seed precision

| Option | Description | Selected |
|--------|-------------|----------|
| Memory-graph nodes for the conversation + fallback | Seed from agent-memory nodes tied to the conversation; web citations secondary; schema-overview fallback. | ✓ |
| Require web-citation→graph URL match as primary | Only seed URL-matched citation nodes; often empty → blank canvas. | |
| Always schema overview | Ignore the conversation; always open the schema map; loses immediacy. | |

**User's choice:** Memory-graph nodes for the conversation + fallback (Recommended).

### Sub-decision — node-inspector actions (read-only milestone)

| Option | Description | Selected |
|--------|-------------|----------|
| Read-only set: pin-path + open-source + show-Cypher | Client-side pin/highlight, deep-link to Source Explorer/document, read-only Cypher; defer add-note (write). | ✓ |
| Include add-note now | Also ship add-note; pulls a write surface + persistence + capability gating into a read-only milestone. | |

**User's choice:** Read-only set (Recommended). `add note` deferred (write → Phase 29/follow-up),
consistent with Phase-26 D-03 (read-only Source Explorer) + D-11 (deferred feedback store).

---

## Claude's Discretion

- Exact `{nodes,edges,paths,schema,query}` Go type shapes + `graphview.go` normalizer layout;
  the structured query-intent payload schema + the parameterized-Cypher templates.
- `web/src/graph/` component layout (three-pane workspace, Sigma wrapper, filter/seed panel,
  inspector, path strip) + the lazy-chunk boundary; a11y/no-WebGL fallback shape.
- Default node/edge caps + the evidence-path filter heuristic; schema-overview rendering;
  empty/loading/error states; schema-endpoint caching; en+it i18n keys; the conversation→seed
  query path (existing agent-memory recall vs a dedicated REST seed query).

## Deferred Ideas

- Graph writes / `add note` / annotations → Phase 29 / follow-up.
- Inline `graph_chunk` chat mini-preview → optional follow-up; not in GRAPH-01..04.
- Raw-Cypher power-user authoring → rejected by directive.
- Persisted saved graph views → write surface, deferred (view export/copy is a possible stretch).
- `ui_control` dockable Graph window / icon-rail / command palette (Frame 07) → follow-up milestone.
- NVL / three.js renderers → evaluated + rejected (license/weight/theme/hairball).
