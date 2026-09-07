---
spike: 102
idea: temporal-memory-graph
name: temporal-memory-traversal
type: comparison
validates: "Given expired, future, unsupported and live relationships, native traversal finds the shortest admissible path; compare bounded expansion with native personalized ranking at equal context budgets."
verdict: PARTIAL
related: []
tags: [arcadedb, memory, temporal, shortest-path, retrieval]
---

# Spike 102: Temporal memory traversal

Measured 2026-09-07. Native edge filtering is usable. Historical mention completeness,
read consistency, and useful query-conditioned ranking are separate unresolved contracts.
This spike does not change the deployed MCP's deliberate rejection of `as_of`.

## What this validates

Given an expired one-hop shortcut and a live two-hop route, traversal must select the
longer live route at a July instant and the shortcut at a February instant. Future
facts, unknown start dates, orphan mentions and non-overlapping windows must not
create an admissible connection. Direction, hop bounds and Entity subtypes still apply.

The ranking comparison asks how Start connects to Target in July. Its two required
supporting facts are `live1` and `live2`; `gap2` is a valid distractor. This is a small
synthetic mechanism comparison, not a representative retrieval benchmark.

## Research and inventory

Runtime: `arcadedata/arcadedb:26.9.1-SNAPSHOT`, running image
`sha256:f6e9948e11a8a1e7bbcdcb5b2a2ae4aad6809d9f9ac935c005327054102e7efc`.
The installed engine JAR exposes 96 top-level procedure/support classes, enumerated
in [installed-procedures.txt](installed-procedures.txt). That is a class inventory,
not a claim of 96 callable algorithms. `javap` on the installed JAR confirms
`ShortestPathStep.computeFilteredShortestPath`, `EdgeConstraint`, hop bounds and
`AlgoPersonalizedPageRank` exist. This avoids relying solely on an upstream checkout.

Source inspected at `ArcadeData/arcadedb` commit
`ebdf527641ba444364bb9e29aa020e9e04824862`:

- [ShortestPathStep](https://github.com/ArcadeData/arcadedb/blob/ebdf527641ba444364bb9e29aa020e9e04824862/engine/src/main/java/com/arcadedb/query/opencypher/executor/steps/ShortestPathStep.java): inline relationship constraints are evaluated while expanding edges.
- [Inline WHERE regression tests](https://github.com/ArcadeData/arcadedb/blob/ebdf527641ba444364bb9e29aa020e9e04824862/engine/src/test/java/com/arcadedb/query/opencypher/CypherShortestPathInlineWhereTest.java): parameters and compound predicates, both shortest-path evaluators.
- [Personalized PageRank](https://github.com/ArcadeData/arcadedb/blob/ebdf527641ba444364bb9e29aa020e9e04824862/engine/src/main/java/com/arcadedb/query/opencypher/procedures/algo/AlgoPersonalizedPageRank.java): one seed, relationship types, damping, iterations and tolerance; outgoing propagation, no per-edge predicate argument.
- [Official path procedures](https://docs.arcadedb.com/arcadedb/reference/extended-functions/path-algorithms), [centrality](https://docs.arcadedb.com/arcadedb/reference/graph-algorithms/centrality), and [Cypher hop bounds](https://docs.arcadedb.com/arcadedb/reference/cypher/cypher-compatibility).
- [Structural analysis](https://docs.arcadedb.com/arcadedb/reference/graph-algorithms/structural-analysis), supplied by the operator during this spike: k-core, triangles and articulation points. The inspected `AlgoKCore` and `AlgoArticulationPoints` use BOTH adjacency; `AlgoBridges` uses OUT, so its endpoint pairs must not be assumed to be undirected cut edges.

Aura already has `Client.Query`, `Read`, `Command`, disposable database lifecycle,
the validity predicate in `memory.go`, and `LinkMentions`. The probe reuses these.
No custom graph algorithm, transport, dependency or live-memory projection was built.
Earlier SQL shortestPath parsing failures were not retried. The old Phase 15 spike
findings describe the retired Neo4j stack; current code and the mounted acceptance
in `docs/memory-graph-validation.md` take precedence.

| Approach | Measured advantage | Limitation | Disposition |
|---|---|---|---|
| Native shortestPath with inline key eligibility | Finds longer admissible route; honors direction and depth | Separate eligibility/read calls are not a snapshot | Reuse native traversal; resolve consistency before production |
| shortestPath then outer `all(...)` filter | Simple expression | Returns no row when the invalid shortcut hides a valid longer route | Reject this query shape |
| Native bounded expansion with inline eligibility | Retrieves both required support facts | Ordering and context cutoff can still omit them | Keep as baseline |
| Native Personalized PageRank | Available without implementing ranking | Outgoing-only propagation; scores invalid topology; no gain in this comparison | Do not promote to retrieval policy |
| K-core + triangle count + articulation points | Distinguish dense groups and structural connectors | Scores change with temporal projection; no edge-validity predicate in these procedure signatures | Use together as diagnostics; never discard a supporting path solely for low coreness |

## How to run

From the repository root in WSL, with the existing ArcadeDB stack up:

```sh
export ARCADEDB_URL=http://127.0.0.1:2480
go test -race -v -count=1 ./.planning/spikes/102-temporal-memory-traversal
```

The probe uses the existing dotenv dependency to load `ARCADEDB_PASSWORD` from the
root `.env` without printing it; existing environment values take precedence.
Each test creates a uniquely named `aura_spike102_*` database and drops it during
cleanup. No operator identity database is selected. Missing configuration fails,
rather than producing a skipped green result. No external LLM calls are needed.

## What to expect

The checked-in [results.txt](results.txt) contains a passing live race run. Fifteen
path cases pass, plus controls for disjoint windows, deleted support, mention
retention, PPR direction/invalid topology, the context-budget comparison, and
structural signals before/after projecting the admissible FACT edges.
Durations in that log describe individual tiny-fixture reads, not a latency SLO.

The query combines Aura's existing SQL eligibility contract:

```sql
SELECT fact_key FROM FACT
WHERE valid_from <= :as_of AND (valid_to IS NULL OR valid_to > :as_of)
```

with the native Cypher relationship constraint:

```cypher
MATCH (s:Entity {name:$source}), (t:Entity {name:$target}),
      p=shortestPath((s)-[r:FACT|MENTIONS*..4 WHERE r.fact_key IN $keys]->(t))
RETURN nodes(p), relationships(p)
```

For FACT, the key identifies the edge itself. For MENTIONS, it identifies the
supporting FACT. Thus future/expired/missing support is excluded before expansion.
These queries are a measured prototype, not a new MCP input schema.

## Investigation trail

1. Inspected docs, source, installed classes and existing Aura retrieval before
   writing the probe. Existing traversal and database lifecycle cover the experiment.
2. The initial fixture used `:start`/`:end` parameter names; the SQL parser rejected
   the statement. Renaming those placeholders to `:vf`/`:vt` fixed fixture setup.
3. Native inline `r.fact_key IN $keys` selected Start -> Middle -> Target in July,
   and Start -> Target in February. The boundary instant excluded `valid_to` and
   included `valid_from`. Empty eligibility, future facts, null start dates, missing
   support, direction/depth limits and subtype endpoints behaved as expected.
4. The outer `WHERE all(r IN relationships(p) ...)` control returned zero rows,
   despite the admissible two-hop route. Filtering after shortest-path selection
   cannot substitute for filtering during traversal.
5. Deliberately deleted supporting FACT `live1` between key collection and traversal.
   The stale list still allowed its MENTIONS path (one row); a fresh list excluded
   it (zero rows). This establishes a real read-consistency gap in the two-call
   prototype, not merely a hypothetical race.
6. Ran the real `LinkMentions` sweep on a disposable fixture: five mentions were
   removed, including the expired shortcut. The historical FACT remained queryable
   in February. The mention graph does not retain a complete historical projection.
7. PPR from Start assigned Future approximately 0.09034 and Orphan 0.04517. From
   sink Target it assigned Target 1 and every other entity 0, including the two
   vertices supporting the reverse query. Type filtering alone is insufficient.
8. A Cypher `id(n)=nodeId` join after PPR returned no rows. The already proven
   explicit `@rid` entity inventory joined returned `nodeId` values successfully;
   that mapping is reused by both PPR probes. No further join workaround was tried.
9. Compared candidate order at shared budgets. No temporal retrieval gain emerged;
   this is a negative result worth keeping.
10. Followed the operator's structural-analysis reference with a live comparison.
    The raw FACT fixture contains historical triangles: Start/Middle/Target have
    core 2. Copying only July-admissible FACT edges to a `VALID_FACT` type inside
    the disposable database leaves a chain: all four connected nodes have core 1,
    no triangles remain, and Middle/Target become articulation points. This fixture
    projection is an experimental control, not a proposed production materializer.

## Results

The comparison uses the same three temporally admissible, bounded candidate facts
for both methods. Baseline order is minimum expansion hop then key; PPR order is
mean endpoint score descending then key. This scoring rule is an experimental
comparator, not an Aura policy. Zero scores remain eligible, so PPR is not penalized
by an extra threshold. Each method concatenates whole statements with one newline,
stopping at the first statement that would exceed the Unicode-character budget.

| Method | Budget | Used | Ordered selection | Required support retained |
|---|---:|---:|---|---:|
| Bounded expansion | 32 | 16 | gap2 | 0/2 |
| Native PPR ordering | 32 | 16 | gap2 | 0/2 |
| Bounded expansion | 64 | 59 | gap2, live2, live1 | 2/2 |
| Native PPR ordering | 64 | 59 | gap2, live2, live1 | 2/2 |

`live1` receives mean endpoint PPR 0 despite being necessary to answer the query.
This demonstrates why a zero-score gate would be unsound here. At the tighter
budget both methods fail; graph structure alone does not establish query relevance.
No conversation retrieval runs, and no conversation quota is borrowed for facts.

### Structural analysis result

| Signal | Stored FACT topology | July-admissible FACT fixture |
|---|---|---|
| Core of Start / Middle / Target | 2 / 2 / 2 | 1 / 1 / 1 |
| Triangles at Middle | 1 | 0 |
| Articulation points | Start | Middle, Target |

The same evidence changes structural role once expired relationships are excluded.
Keeping only core >= 2 would discard the entire valid answer path in this fixture.
Articulation points can identify connectors worth inspecting even with low core,
but they too describe connectivity, not source reliability or answer relevance.
The existing `graph_diagnostics` already exposes k-core; this spike does not add
new diagnostic fields or silently change its all-validity-windows semantics.

## Full graph-algorithm survey

The operator subsequently requested a review of every graph algorithm and live
trials of the relevant ones. The complete [71-entry inventory](ALGORITHM-INVENTORY.md)
records all 12 categories, including explicit reasons for deferring entries.
Thirty-nine native procedures were executed, in addition to the Cypher shortestPath
tests above. Each call uses the documented signature and the installed engine.

### Broad algorithm comparison

`algorithmFixture` contains two directed triangles ABC and DEF, one C -> D link,
F -> Tail and an isolated vertex: eight nodes/eight edges. This provides known
components, cut vertices, shortest paths and neighborhood overlaps. Calls return
through the existing read endpoint; all fixture writes are disposable.

| Use | Measured result | Selection |
|---|---|---|
| Community organization | Leiden: ABC / DEFTail / Isolated, identical across three fixed-input runs; Louvain splits B and E out; label propagation collapses all seven connected nodes | Leiden is the first community candidate |
| Preserve connectors | Articulation points C,D,F; biconnected components repeat shared cut vertices and omit the isolated vertex | Useful alongside core/clustering, with explicit isolated-node handling |
| Bounded expansion | BFS from A at BOTH depth 2 returns B,C,D and excludes A | Reuse for limited neighborhood reads after eligibility is solved |
| Connect multiple requested entities | Steiner connects A,E,Tail with five edges, total weight 5 | Candidate for assembling a compact evidence subgraph; sources still need resolution |
| Alternative paths | Dijkstra BOTH costs 4; K-shortest OUT costs 6; allsimplepaths at depth 4 carries actual relationship records | Direction and provenance prevent treating these as interchangeable |
| Structural similarity | Jaccard A-B=1/3; A-C and A-D=1/4. Adamic-Adar/resource allocation weight shared hubs differently | Candidate generation only; not entity equivalence or a new fact |
| Diverse representatives | VoteRank picks D,A,Tail | Worth a labeled context test; peripheral evidence survives |
| Dense groups | Clique finds the two triangles; k-truss gives triangle nodes 3 and Tail 2; densestSubgraph drops Tail | Diagnostics, not an evidence retention rule |
| Embedding baseline | FastRP cosine A-B=.970, A-D=.764, A-Isolated=.138; fixed seed repeats exactly | Experimental structural feature, separate from text embeddings |
| Embedding alternative | GraphSAGE cosine A-D=.950 exceeds A-B=.884, A-Isolated=.728 | Not selected on this fixture; its native random projections are not trained |

All 39 procedure calls execute, but that is **not 39 correctness verdicts**.
The tests combine exact fixture oracles for paths/components/cores, output-shape
checks for exploratory candidates, fixed-input repeatability, and explicit
counterexamples. `results.txt` includes queries, timings and outputs for every call.
No broad retrieval-quality improvement is established by this catalog exercise.

### Engine behaviors that prevent adoption

- **Conductance fails a one-cut-edge oracle.** SQL confirms one edge crosses the
  two groups; their degree volumes are 7 and 9, so conductance should be 1/7.
  Native `boundaryEdges` and conductance both return zero. Inspected
  [AlgoConductance source](https://github.com/ArcadeData/arcadedb/blob/ebdf527641ba444364bb9e29aa020e9e04824862/engine/src/main/java/com/arcadedb/query/opencypher/procedures/algo/AlgoConductance.java)
  halves an already per-community cut count with integer division.
- **ModularityScore fails the single-community oracle.** After assigning every
  vertex to one community, native modularity remains 0.75; the expected modularity
  is zero. The inspected [source](https://github.com/ArcadeData/arcadedb/blob/ebdf527641ba444364bb9e29aa020e9e04824862/engine/src/main/java/com/arcadedb/query/opencypher/procedures/algo/AlgoModularityScore.java)
  sums OUT degrees while its denominator uses the undirected 2m formula. Do not
  use this output to select a community algorithm. The separate Louvain-returned
  modularity is not automatically covered by this specific counterexample.
- **K-shortest/random-walk payloads lack relationships.** Ordered nodes are present,
  but the serialized path has `length=0` and `relationships=[]`. Consumers must not
  treat this shape as provenance-complete or count hops using that `length` field.
- **Adamic-Adar includes adjacent nodes.** Its documentation describes non-adjacent
  candidates, but the run also returns existing neighbors B and C. This is a
  contract discrepancy to account for before exposing link suggestions.

The counterexample tests deliberately preserve these observed defects and pass
when the reproduction holds. They are not tests asserting the metrics are correct.
No upstream dependency patch, automatic graph materializer or external issue report
was created. These procedures remain unadopted until their contracts are reliable.

## Next implementation contract

The first useful production slice is **FACT-only temporal paths**, retaining the
current topology mode when `as_of` is absent. Reuse native inline filtering and
the existing validity semantics. Before implementation closes:

- Resolve the measured keyset/read race using a verified read-consistency contract;
  do not describe a two-call key list as a snapshot. Returned evidence must still
  satisfy the requested instant.
- Preserve the existing record preflight, timeout, exact endpoint identity scope,
  direction, hop bound, provenance and subtype contracts.
- Keep historical MENTIONS/combined requests explicitly unsupported until a
  retention or reconstruction strategy is measured. A filtered surviving graph
  is not complete historical recall.
- Prove the new input schema and errors through live SDK and mounted MCP calls,
  then rerun the required package coverage/race/regression gates.
- Keep PPR experimental. A larger, labeled query set with tokenized full-evidence
  budgets and answer-support scoring is needed before choosing a ranking policy.
- Evaluate structural signals on the same admissible graph as retrieval. Preserve
  query-support paths and connectors even when their core or triangle count is low.

No production implementation, mounted temporal acceptance, large-graph resource
bound, multi-identity benchmark, concurrent snapshot guarantee, aggregate coverage,
package mutation score or >9.8 agent E2E score is claimed by this spike.
