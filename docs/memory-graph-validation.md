# Native memory graph surface — 2026-09-07

Status: implemented and deployed locally; direct calls to the two new tools from
the operator's mounted Codex MCP session remain pending a tool-manifest reload.
Do not describe this as completed mounted-MCP acceptance.

## Contract

- `graph_diagnostics`: native graphSummary, degree(BOTH), WCC and kcore. Select
  `facts`, `mentions`, or `combined`. Full-graph totals and core histogram, bounded
  node details sorted by coreness, degree and name. Includes Entity subtypes.
- `graph_path`: native Cypher shortestPath between exact entity names, BOTH/OUT/IN,
  1..6 hops (default 3). Return ordered names, original relationship orientation,
  FACT statements/provenance/validity, and supporting facts for MENTIONS. An
  unresolved mention explicitly reports `support_missing`.
- Both tools resolve identity from the existing OAuth caller; neither accepts
  database names, arbitrary queries or identity overrides. Both are classified as
  read operations in the managed-memory bridge and deferred by its existing policy.
- `AURA_MEMORY_GRAPH_MAX_RECORDS=10000` preflights all schema record counts before
  algorithms execute. This includes technical vertices because native algorithms
  load them even with a relationship-type filter. The engine's working-memory guard
  and existing client timeout still apply. The preflight is not a transactionally
  frozen snapshot; obvious count inconsistencies fail rather than return partial
  diagnostics.
- Results describe **stored topology across all validity windows**. `as_of` is
  explicitly rejected. A topological connection is neither entailment nor causality.
  Coreness is structural and must not automatically promote, merge or erase facts.
  Multiple relationships can affect native degree/coreness; these are not counts
  of independent sources or confirmations.

## Runtime discoveries

On the installed 26.9.1-SNAPSHOT engine, using test-owned disposable databases:

1. A triangle plus a tail yields kcore 2/2/2/1. WCC and degree execute on the
   read endpoint and return node RIDs that can be joined to the entity inventory.
2. `graphSummary('FACT','Entity')` omits the Object subtype. The implementation
   supplies the exact set of types found in the polymorphic Entity inventory.
3. `graphSummary.isolatedNodes` counts zero outgoing degree. A connected sink is
   included. The API exposes this as `zero_out_degree`; its `isolated_nodes` is
   computed by counting native BOTH-degree zeros.
4. Three SQL shortestPath probes failed to parse, including quoted function name
   and HTTP limit=-1. That route was stopped. The documented Cypher shortestPath
   pattern with explicit hop bounds works, including Object endpoints.

Official references read before implementation:

- [Graph statistics](https://docs.arcadedb.com/arcadedb/reference/graph-algorithms/graph-statistics)
- [K-core](https://docs.arcadedb.com/arcadedb/reference/graph-algorithms/structural-analysis)
- [Centrality](https://docs.arcadedb.com/arcadedb/reference/graph-algorithms/centrality)
- [Components](https://docs.arcadedb.com/arcadedb/reference/graph-algorithms/community-detection)
- [SQL path contracts](https://docs.arcadedb.com/arcadedb/reference/graph-algorithms/sql-path-functions)
- [Cypher hop bounds](https://docs.arcadedb.com/arcadedb/reference/cypher/cypher-compatibility)
- [Algorithm memory bounds](https://docs.arcadedb.com/arcadedb/reference/graph-algorithms/notes)

## Verification completed

- Global go vet and go build; unit and race tests for internal/arcadedb,
  cmd/arcadedb-mcp and internal/agent/mcptools.
- Full ArcadeDB integration suite with race: pass, **86.6% statement coverage**.
  All test data was in dedicated temporary or test-owned disposable databases.
- Full TestAgentMemoryMCPLive suite with race: pass, including the new graph tool
  calls through the official MCP SDK with test-owned authenticated identities.
- Native cases cover subtype inclusion, an isolated entity, a technical vertex
  excluded from entity results, triangle/tail coreness, node-list truncation,
  forward/reverse paths, hop bounds, same/missing/disconnected endpoints,
  relationship selection and mention provenance.
- Unit cases reject arbitrary relationship types, invalid direction/depth,
  temporal requests and oversized input graphs before algorithm execution.
- Live tool-schema golden regenerated from SDK tools/list; tenant identity remains
  absent from the input schemas. Read classification and configured budget tested.
- Package lint: zero issues.
- Mutation check of the relation/temporal guard: the tool reports 5/5, but debug
  logs show four mutations do not compile (deleted required returns). Those are
  nonviable and are excluded: **1/1 viable mutation killed by an actual assertion**.
  This is a narrow guard check, not a package-wide mutation score.

Deployed image ID:
`sha256:07b5fe820f490d6289b1cd9b941a38bd76190b5a748934eb9817b0709a570496`.
The mounted graph_schema and memory_recall reads succeed after normal OAuth login;
the invented-project negative recall still abstains. The current Codex session's
callable manifest does not yet contain graph_diagnostics or graph_path.

## Mounted acceptance still required

Reload the MCP tool manifest (start a fresh Codex session if necessary), discover
the actual tool schemas, and execute these read-only requests:

1. graph_diagnostics with relations=facts, then mentions, then combined, limit=10.
   Compare component sizes, isolates, native degree and core histogram.
2. graph_path from ArcadeDB to memory_merge_entities, relations=combined,
   max_depth=4. Inspect original direction and supporting facts, not merely found.
3. Missing endpoint and over-depth requests; explicit as_of must fail.
4. Repeat memory_facts_about for ArcadeDB at depth=1 and depth=2; compare which
   supporting facts the returned path contributes at a common context budget.

No real-memory graph metrics or retrieval gain are claimed before that acceptance.
Next design question: native traversal over temporally admissible relationships,
followed by a measured comparison of bounded expansion and query-conditioned graph
ranking. Keep fact and conversation quotas independent throughout.
