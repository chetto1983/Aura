---
spike: 099
name: neo4j-outcome-policy-graph
type: standard
validates: "Given Aura's live Neo4j, when decisions, alternatives, delayed outcomes, propensities, policy snapshots, promotion, rollback, and retention are exercised, then the proposed learning ledger is durable, idempotent, queryable, and operationally bounded"
verdict: VALIDATED
related: [097-qwen-multidomain-reward-surface, 098b-graph-knn-policy, 032-agent-memory-mcp-live-mount]
tags: [neo4j, outcome-graph, policy-snapshot, observability]
---

# Spike 099: Neo4j outcome and policy graph

## What This Validates

The winning graph-kNN learner needs a durable event ledger, delayed reward
attachment, semantic neighbors, and a safe policy lifecycle. This spike writes
the 132 real Qwen surface decisions to Aura's live Neo4j, replays the same batch
to prove idempotency, attaches held-back outcomes, performs 100 vector-neighbor
queries, promotes and rolls back a policy snapshot atomically, then deletes only
the spike-owned run.

## Research

| Pattern | Source | Use |
|---|---|---|
| Managed transaction functions must be idempotent because drivers may retry them | [Neo4j Go driver transactions](https://neo4j.com/docs/go-manual/current/transactions/) | `ExecuteWrite` + `MERGE` |
| Uniqueness constraints should back identifying properties before concurrent `MERGE` | [Cypher MERGE](https://neo4j.com/docs/cypher-manual/current/clauses/merge/), [constraints](https://neo4j.com/docs/cypher-manual/current/schema/constraints/create-constraints/) | decision/context/action/outcome/snapshot identity |
| Neo4j 5.x cosine vector indexes support `db.index.vector.queryNodes` | [vector indexes](https://neo4j.com/docs/cypher-manual/current/indexes/semantic-indexes/vector-indexes/) | offline semantic outcome neighbors |

Neo4j GDS pipelines remain an offline challenger for larger graphs. They are not
placed on the turn hot path: the 098 winner chooses in process and Neo4j supplies
durability plus neighbor refreshes.

## How to Run

```powershell
# Neo4j :7687 and agent-memory :8091 must be healthy.
# Set NEO4J_PASSWORD without printing it, then:
go test ./.planning/spikes/099-neo4j-outcome-policy-graph
go run ./.planning/spikes/099-neo4j-outcome-policy-graph
```

## What to Expect

- 132 decisions, 44 contexts, 12 actions, 396 considered edges;
- 19 initially delayed outcomes, then all 132 attached;
- an identical second write changes no counts;
- candidate promotion followed by rollback to the baseline;
- agent-memory TCP health before and after;
- zero spike-owned nodes after retention cleanup.

The forensic export is `artifacts/neo4j-outcome-graph.json`.

## Investigation Trail

- Uses Aura's pinned Neo4j Go driver and the live Compose database.
- All labels, schema names, and run IDs are spike-specific. Cleanup cannot match
  production memory/document labels.
- Policy alternatives and rewards come from the real Qwen surface, not fabricated
  demo nodes.
- The first live query failed closed on Neo4j 5's clause-composition rule:
  `WITH` is required between `MERGE` and `MATCH`. Deferred cleanup removed the
  partial spike schema; adding the explicit projection fixed the query.

## Results

**VALIDATED.** The complete live contract passed:

| Check | Result |
|---|---:|
| Decisions / contexts / actions | 132 / 44 / 12 |
| Considered-action edges | 396 |
| Initially delayed outcomes | 19 |
| Outcomes after attachment | 132 |
| First 132-row transactional write | 843.2 ms |
| Idempotent identical rewrite | 81.0 ms |
| Counts changed on rewrite | **No** |
| Vector-neighbor query, 100 runs | p50 3.71 ms / p95 5.30 ms |
| Candidate promotion | graph-kNN snapshot active |
| Atomic rollback | static snapshot restored |
| Agent-memory health | healthy before and after |
| Retention cleanup | 0 spike nodes remain |

The p95 graph query is fast enough for asynchronous refresh and shadow
evaluation, but still orders of magnitude slower than an in-process policy
decision. This supports the proposed split: Neo4j is the durable source of
truth and graph-feature engine; a bounded in-memory snapshot serves turns.
