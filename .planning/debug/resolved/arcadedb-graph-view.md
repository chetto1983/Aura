---
status: resolved
trigger: "graph view is broken due to arcade db migration. arcade db have frontend weel done study and apply in aura"
created: 2026-08-03
updated: 2026-08-03
---

# ArcadeDB graph view

## Symptoms

- expected: The Aura cockpit renders the authenticated identity's ArcadeDB memory vertices and edges, with useful exploration behavior informed by ArcadeDB Studio.
- actual: `POST /api/graph/query` returns HTTP 200 with the live schema but always-empty `nodes` and `edges`, so the canvas cannot display stored memory.
- errors: No runtime exception is required to reproduce. `GraphResult.query` explicitly says the view is schema-only and traversal is not implemented.
- timeline: Introduced by the Neo4j-to-ArcadeDB removal in commit `5908b3e6f`; the prior graph traversal compilers were deleted rather than ported.
- reproduction: Open the authenticated cockpit Graph surface; the initial read returns schema metadata but no drawable memory.

## Current Focus

- hypothesis: confirmed and fixed. The renderer was healthy; the regression was the deliberate schema-only `ArcadeGraphView.Query` and a composition seam that could not execute Studio graph reads.
- test: completed focused and full Go/web verification, rebuilt the embedded cockpit, and exercised `overview` and `expand` against the live authenticated account on desktop and mobile.
- expecting: satisfied. The initial canvas contains all bounded memory for the authenticated identity, expansion cumulatively appends by RID, and neither request contains conversation or removed graph fields.
- next_action: none
- reasoning_checkpoint: The final contract contains only `overview` and `expand`; removed request shapes are rejected, not aliased. Conversation state is absent from the GraphExplorer component.
- tdd_checkpoint: true

## Evidence

- timestamp: 2026-08-03T00:00:00+02:00
  finding: `internal/agui/graph_arcadedb.go` returns empty node and edge slices for every supported operation by design.
- timestamp: 2026-08-03T00:00:01+02:00
  finding: `docs/CAPABILITIES.md` and `docs/TECHNICAL_OVERVIEW.md` classify the memory graph explorer as schema-only and traversal as an open rewrite.
- timestamp: 2026-08-03T00:00:02+02:00
  finding: ArcadeDB 26.7.3 Studio source uses `serializer: "studio"`, compact vertex/edge envelopes, bounded result limits, direct RID neighbor expansion, cumulative append, relayout, search, crop/cut, and per-type labels/styles.
- timestamp: 2026-08-03T11:20:00+02:00
  finding: The live database for the authenticated account contains 59 `Entity` vertices and 34 `FACT` edges; a Studio overview returns the drawable endpoint graph.
- timestamp: 2026-08-03T12:00:00+02:00
  finding: Focused `internal/arcadedb`, `internal/agui`, and `cmd/aura` graph tests pass; 69 focused Vitest tests, TypeScript, ESLint, and the production Vite build are green.
- timestamp: 2026-08-03T12:30:00+02:00
  finding: Full frontend verification passed 1,755 Vitest cases with 92.65% statement coverage, Knip reported no unreachable code, jscpd reported no clones, all 102 contrast pairs passed, and every frontend source file remained below 600 lines.
- timestamp: 2026-08-03T12:31:00+02:00
  finding: Real Chrome and mobile Chrome runs loaded the authenticated account's 59 nodes and 34 edges, expanded a real RID cumulatively, and recorded no browser failures.
- timestamp: 2026-08-03T13:08:00+02:00
  finding: The full owned-surface matrix passed at 23,474/27,152 statements (86.4%); `internal/arcadedb` measured 93.9% unit and 94.0% with live ArcadeDB integration, while `internal/agui` measured 85.5% on the full `db_integration` profile.
- timestamp: 2026-08-03T13:20:00+02:00
  finding: `go-mutesting internal/arcadedb/studio_graph.go` killed 18/18 mutants (100%), above the 70% critical-file gate; WSL race passed for `internal/arcadedb`, `internal/agui`, `internal/webui`, and `cmd/aura`.
- timestamp: 2026-08-03T15:28:00+02:00
  finding: The former Sigma/Graphology/ForceAtlas2 renderer was replaced, not retained, by ArcadeDB Studio's Cytoscape + fCoSE stack. The final frontend suite passed 1,747 tests at 92.76% statements / 87.26% branches / 92.65% functions / 94.59% lines; `src/graph` measured 95.88% / 87.41% / 93.45% / 97.67%, and the new projection/style module killed 153/178 mutants (85.96%). The production bundle emits no >500 kB warning: Cytoscape core is 435.41 kB and its layout chunk 122.56 kB.

## Eliminated

- hypothesis: Sigma/WebGL rendering is the primary failure.
  reason: The existing frontend unit and mocked Playwright contracts render populated `GraphResult` data; production never supplies any nodes or edges.
- hypothesis: The deleted spaCy/`LocomoEntity` branch only needed to be connected to production.
  reason: It was benchmark-only scaffolding for a second `LocomoEntity`/`MENTIONED_IN` model. Aura's deployed memory already persists the complete identity-scoped `Entity`/`FACT` model and retrieves it through native vector and full-text indexes; connecting the benchmark branch would duplicate the memory contract instead of repairing the graph view.

## Resolution

- root_cause: The ArcadeDB migration kept schema discovery but replaced graph reads with an always-empty placeholder; Compose also omitted the tenant graph configuration required by the server adapter.
- fix: Added tenant-scoped Studio serializer reads, strict `overview`/`expand` intent compilation, bounded projection, cumulative RID expansion, the identity-wide frontend flow, and explicit Compose wiring. Replaced Sigma/Graphology/ForceAtlas2 with the same Cytoscape + fCoSE renderer family as Studio, added type-derived colours and native-speed zoom, and removed the conversation seed contract and its UI.
- verification: Unit, integration, race, vet, build, full-matrix coverage, serializer mutation, full frontend, mocked Playwright, real authenticated desktop/mobile Playwright, dead-code, duplication, contrast, and embedded-bundle checks passed.
- files_changed: `internal/arcadedb`, `internal/agui`, `cmd/aura/serve_graph_schema*`, `web/src/graph`, graph E2E tests, `compose.yaml`, operator docs, and `internal/webui/dist`.
