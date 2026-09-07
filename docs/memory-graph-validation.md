# Native memory graph surface — 2026-09-07

Status: implemented and deployed locally; mounted-MCP acceptance completed on
2026-09-07 through the operator's authenticated Codex tools. Temporal traversal
and a general retrieval-quality benchmark remain unmeasured.

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
the invented-project negative recall still abstains. At initial deployment the
session manifest lacked the new tools; the acceptance session below exposes both.

## Mounted acceptance completed — 2026-09-07

Discovered the actual mounted `graph_diagnostics`, `graph_path`, and
`memory_facts_about` schemas and executed all four requested read-only cases.
No memory fixtures or facts were written. The schemas expose no identity override.

All diagnostics used `limit=10`, returned 89 entities, 10 node details, and
`truncated=true`. Component sizes and core-histogram counts each sum to 89.

| Relations | Edges | Components | Largest component | Isolates | Zero OUT-degree | Core histogram (core: count) |
|---|---:|---:|---:|---:|---:|---|
| facts | 67 | 22 | 11 | 2 | 47 | 0: 2, 1: 87 |
| mentions | 34 | 61 | 16 | 57 | 65 | 0: 57, 1: 16, 2: 16 |
| combined | 101 | 9 | 59 | 2 | 36 | 0: 2, 1: 54, 2: 27, 3: 6 |

FACT component sizes: 11,10,10,8,8,7,4,3,3,3, then ten 2s and two 1s.
MENTIONS: 16,7,6,3, then 57 singletons. Combined: 59,10,7,4,3,2,2,1,1.
ArcadeDB (Object subtype) has native BOTH degree 3/4/7 and core 1/2/2 for
facts/mentions/combined respectively. Its IN/OUT degrees are 1/2, 4/0, 5/2.
The combined largest component grows through mention connections; this does not
measure independent confirmation or fact importance.

`graph_path(ArcadeDB, memory_merge_entities, combined, max_depth=4)` returns
`found=true` with ordered nodes `[ArcadeDB, memory_merge_entities]`. The one edge
is MENTIONS, originally **memory_merge_entities -> ArcadeDB**, so the default
BOTH traversal follows it backwards. Its resolved supporting fact is
`89982b2a895105ca2d91b2646b26fe4533e69bcd69ead33cfb3418fb6f408a85`, describing
the historical LIST OF MAP merge defect. It carries the predicate `aveva_difetto`,
the incident object, source references `merge-defect` and `validazione-2026-09-03`,
a source run ID, writer role `parent`, and `valid_from=2026-09-03 10:53:05`.
No `valid_to` or `support_missing` is returned. This historical statement is not
evidence that the deployed merge tool is currently broken.

Negative cases pass: the invented endpoint
`__aura_graph_validation_missing_20260907__` returns `found=false`, empty nodes
and edges, and `reason=entity_not_found`; `max_depth=7` returns an MCP error
`graph max_depth must be between 1 and 6`. Both graph tools reject explicit
`as_of=2026-09-07T00:00:00Z` with
`memory graph is stored topology, not an as_of projection`.

Both `memory_facts_about` calls use entity ArcadeDB and `limit=20`. Depth 1
returns 3 facts (`retrieval.path=graph`); depth 2 returns 7 (`path=mentions`),
including all three direct facts. The path's supporting fact is absent at depth 1
and present at depth 2. The other additions describe the documentation location,
the documented map restriction, and the missing live tests behind the defect.

For a common context ceiling of **2,048 Unicode characters**, concatenate complete
statements in returned order with one newline between statements, stopping before
the ceiling. Depth 1 uses 629 characters; depth 2 uses 1,610. Both fit, and only
depth 2 includes the path support. This is a statement-only character budget,
not a tokenizer measurement or the full provenance-bearing tool payload budget.
No conversation retrieval was invoked; fact and conversation quotas remain
independent. The path adds no unique supporting fact beyond depth 2 in this case.

These are single-identity, single-session observations, not a frozen snapshot,
a general retrieval gain, an E2E quality score, or new coverage/mutation evidence.
Next design question: native traversal over temporally admissible relationships,
followed by a measured comparison of bounded expansion and query-conditioned graph
ranking. Keep fact and conversation quotas independent throughout.
