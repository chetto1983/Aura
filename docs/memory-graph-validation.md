# Native memory graph surface — 2026-09-07

Status: implemented and deployed locally, including temporal paths, historical
mention retention and support-aware expansion. Operator-mounted MCP acceptance
and guided final-agent-answer checks completed on 2026-09-07. A broad independent
answer-quality benchmark remains open; see the final validation below.

## Final temporal and answer validation — 2026-09-07

The final implementation preserves historical support using native
`MENTIONS.fact_rid` LINK and UNIQUE (`@out`,`@in`,fact_rid). A closed FACT may lose
its active correction key without losing its record identity. Schema setup drops
the obsolete (`@out`,`@in`,fact_key) index, whose NULL-key collisions blocked
different historical supports. The actual mounted schema confirms the migration.
Complete sweeps scan retained history; incomplete inventories perform no
reconciliation writes. Native depth-two expansion checks support validity at
every mention hop and orders direct facts before expanded evidence.

| Verification | Measured result |
|---|---|
| Operator-mounted OAuth MCP, deployed containers | 11/11 cases pass |
| Six fixed-instant queries, direct evidence retained at limit=1 | 2/6 before, 6/6 after |
| Complete JSON evidence within 512 tokens | 2/6 before, 6/6 after |
| Complete JSON evidence within 1,024 or 2,048 tokens | 3/6 before, 6/6 after |
| Final answers composed by this Codex agent | 18/18 guided cases, including two abstentions |
| Full ArcadeDB integration suite with race | PASS, 86.7% statement coverage, no skips |
| Full TestAgentMemoryMCPLive SDK suite with race | PASS |
| Temporal evidence validation mutation spot-check | 26/27 unique mutations killed (96.3%) |

The token comparison uses the same six entities and direct-fact sets at
`2026-09-07T09:51:27Z`, including source metadata and complete facts. It uses Aura's
vendored cl100k vocabulary as an estimate, with no conversation tokens borrowed.
The surviving mutation removes a redundant parse-error branch: parse errors also
return the zero time rejected by the next branch. The score is confined to
`validateTemporalFact`, not the whole package.

Reproducible artifacts and question-by-question answers live in
[quick task 260907-fh3](../.planning/quick/260907-fh3-correct-temporal-memory-paths-historical/260907-fh3-SUMMARY.md),
including criteria written before the answers, aggregate MCP results and the
budget evaluator. Private captures were not versioned and were removed by a
concurrent workspace cleanup; reproducing the budget run requires paired captures.
These answers were guided and self-reviewed in a session that knows the project;
18/18 is not a blind benchmark or proof of reliable answers on every domain.

Both containers were rebuilt and are healthy. The accepted local build includes
concurrent workspace changes and is stamped `c415bcfe2-memory-dirty2`, rather than
claiming a clean-commit build:

- Aura: `sha256:2afc809ddf385ec3f3c45725aa21c2d571c74ed06c3a97db5d55559807dda7a1`
- MCP: `sha256:d48cf8e2f766e3921322eb7a2c1ff3524beb5f878517ba762cdc6d5f98a6970f`

Ordinary `codex mcp login aura-memory` restored OAuth automatically after restart.
No custom authentication implementation was needed. The final mention sweep
logged 76 links across two tenants at `2026-09-07T10:56:47Z`.

The temporal contract concerns valid-time relationships and entities still stored.
REPEATABLE_READ permits phantoms; forgotten records cannot be reconstructed.
Neither graph connectivity nor coreness proves a claim. No PPR improvement or
community-based promotion was demonstrated or introduced.

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
- Without `as_of`, paths describe **stored topology across all validity windows**.
  With `as_of`, native inline predicates select admissible relationships and their
  supporting facts in REPEATABLE_READ. Diagnostics still reject `as_of`.
  A topological connection is neither entailment nor causality.
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

## Initial verification (before temporal support)

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

## Temporal traversal follow-up — 2026-09-07

[Spike 102](../.planning/spikes/102-temporal-memory-traversal/README.md) and its
checked-in live race output now measure the next design question on disposable data.
Native `shortestPath` with an inline eligible-fact-key predicate passes 15 path
cases. An expired one-hop shortcut no longer hides a valid two-hop route; filtering
the selected shortest path afterward does hide it. Future/unknown validity,
unsupported mentions, disjoint windows, direction and depth bounds are exercised.

Two limits were reproduced: deleting support between key collection and traversal
leaves a stale keyset able to return that mention path; refreshing the keyset
excludes it. The actual `LinkMentions` sweep removes expired mentions while their
historical facts remain, so surviving mention topology cannot promise complete
historical recall.

Native PPR from an outgoing sink gives every other entity zero, and from a source
can score future or unsupported connections. At equal 32/64-character statement
budgets, bounded expansion and native PPR ordering retain 0/2 and 2/2 required
supporting facts respectively: no gain in this synthetic comparison. Conversation
quotas remain untouched. This is not a tokenizer or real-query benchmark.

The operator's structural-analysis reference prompted an additional live check:
Start/Middle/Target have core 2 in the stored FACT fixture, but core 1 after
projecting only July-admissible FACT edges in the disposable database. Middle's
triangle count falls from 1 to 0; Middle and Target become articulation points.
A core >= 2 retention rule would lose the entire valid answer path. K-core and
cut-vertex diagnostics should be interpreted together on the admissible graph,
without treating structural scores as factual confidence.

The next production slice is FACT-only temporal paths with a verified consistency
contract. Historical mentions remain unsupported until retention/reconstruction is
measured. Existing mounted tools still reject `as_of`; the spike changes no runtime
contract and supplies no new package coverage or production E2E score.

## Full graph-algorithm inventory and trials — 2026-09-07

At the operator's request, [all 71 catalog entries](../.planning/spikes/102-temporal-memory-traversal/ALGORITHM-INVENTORY.md)
were reviewed and 39 native procedures executed on a dedicated eight-node fixture.
The strongest next candidates are Leiden for community organization, articulation
points/biconnected components for preserving connectors, BFS for bounded expansion,
and Steiner for connecting several requested entities. FastRP is an experimental
structural embedding baseline; no text-embedding replacement is implied.

Two independent metric counterexamples exclude adoption: conductance reports zero
for a real one-edge community cut, and modularityScore reports 0.75 for a single
community whose mathematical modularity is zero. K-shortest/random-walk responses
also lack relationship provenance despite carrying ordered nodes. Complete native
queries, outputs, selection rationale and limits are recorded in the spike.
Successful procedure execution is distinguished from correctness and retrieval gain.
