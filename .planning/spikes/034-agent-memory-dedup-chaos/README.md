---
spike: 034
name: agent-memory-dedup-chaos
type: standard
validates: "Given concurrent Mario Rossi variants through the agent-memory MCP surface, when writes race, then Neo4j memory dedup does not fragment tagged entities or over-merge into unrelated entities"
verdict: INVALIDATED
related: [031, 033]
tags: [phase-15, memory, neo4j, dedup, chaos, mcp]
---

# Spike 034: Agent Memory Dedup Chaos

## What This Validates

Given a synthetic tagged Mario Rossi identity, when 10 concurrent MCP writes create exact and variant names, then the memory layer should avoid duplicate exact-name rows, avoid merging into unrelated existing people, and ideally collapse variants into one tagged entity.

## Research

The previous source audit found that Phase 15 still needs a live Mario Rossi chaos merge test. Neo4j Agent Memory advertises entity resolution/deduplication on `memory_add_entity`; this spike probes that promise through the MCP server rather than through direct database writes.

| Approach | Tool/Library | Pros | Cons | Status |
|---|---|---|---|---|
| Concurrent MCP `memory_add_entity` calls | Neo4j Agent Memory MCP | Tests the real agent-facing write surface | Dedup heuristics may be async/model-dependent | Chosen |
| Direct Cypher MERGE chaos | Neo4j | Tests Aura's eventual schema constraints | Bypasses upstream MCP behavior | Deferred |
| Single-threaded variant writes | MCP | Easier to debug | Misses race behavior | Rejected |

## How to Run

```powershell
go run ./.planning/spikes/034-agent-memory-dedup-chaos
```

## What to Expect

The harness creates 10 concurrent synthetic variants such as `Mario Rossi AURA-SPIKE-034-*`, `M. Rossi AURA-SPIKE-034-*`, and `Rossi Mario AURA-SPIKE-034-*`, then queries the graph by tag.

## Observability

The JSON summary includes every write result, the tagged entity rows returned by `graph_query`, exact-name count, fragmentation flag, over-merge flag, and verdict.

## Investigation Trail

- 2026-06-08 first live run showed the same-run chaos case can collapse 10 Mario Rossi variants into one tagged entity.
- A repeat live run with tag `AURA-SPIKE-034-1780926627` exposed the stronger failure: all 10 writes succeeded, but every write merged into the previous run's entity `Mario Rossi AURA-SPIKE-034-1780926499` (`ea97d340-ca8f-4dc6-826f-948763bc57b1`).
- Similarity scores for the cross-run merge ranged from roughly `0.9536` to `0.9973`.
- `graph_query` for the new tag returned `tagged_count=0` and `exact_name_count=0`; the harness flagged `over_merge=true`, `fragmented=false`.

## Results

Verdict: INVALIDATED.

Neo4j Agent Memory's entity dedup is strong enough to avoid same-run fragmentation, but it is too aggressive for provenance-safe exact isolation. A new synthetic Mario Rossi run can disappear into an older Mario Rossi entity even though the run tag differs. Aura's Phase 15 design needs an exact identity/provenance key, source-bound constraints, or an application-side merge policy before accepting upstream semantic dedup as the only guardrail.
