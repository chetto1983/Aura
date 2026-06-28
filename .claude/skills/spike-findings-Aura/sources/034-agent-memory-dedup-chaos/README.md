---
spike: 034
name: agent-memory-dedup-chaos
type: standard
validates: "Given concurrent Mario Rossi variants through the agent-memory MCP surface, when writes race, then Neo4j memory dedup does not fragment tagged entities or over-merge into unrelated entities"
verdict: VALIDATED
verdict_history: "INVALIDATED 2026-06-08 (cross-run over-merge) -> fixed in fork branch aura/provenance-safe-dedup (commit c1c2d65) -> re-VALIDATED 2026-06-08T19:03 live against image aura-agent-memory-mcp:spike-fixed"
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

### Re-validation after fix (2026-06-08T19:03, live `aura-agent-memory-mcp:spike-fixed`)

The cross-run over-merge was fixed in the fork branch `aura/provenance-safe-dedup` (commit `c1c2d65`, provenance-scoped dedup; 83/83 package unit tests green). Re-run live against the rebuilt `:spike-fixed` image, **two consecutive runs**:

- Run 1 (tag `AURA-SPIKE-034-1780945405`): 10 variants collapsed into the single canonical entity of *this* run; `over_merge=false`, `fragmented=false`, `tagged_count=1`, `exact_name_count=1`.
- Run 2 (tag `AURA-SPIKE-034-1780945434`, the decisive cross-run case): the first write returned `matched_entity_name=null` — it created *its own* canonical entity instead of disappearing into Run 1's `...405` entity, despite ~0.97 embedding similarity; the remaining 9 variants merged into Run 2's canonical. `over_merge=false`, `tagged_count=1`.

Both runs reported `verdict=VALIDATED`. Provenance-safe isolation now holds across runs through the live MCP surface, not only in package unit tests.

## Results

Verdict: VALIDATED (originally INVALIDATED — see verdict_history + re-validation above).

The original run proved Neo4j Agent Memory's stock semantic dedup was too aggressive for provenance-safe isolation: a new tagged Mario Rossi run disappeared into an older one at similarity `0.95`–`0.997`. The `aura/provenance-safe-dedup` fork fix (provenance-scoped dedup keyed off write metadata) resolves this, and the live re-run against `:spike-fixed` confirms cross-run isolation holds. Standing Phase 15 caveat: the scope only engages when the ingest path supplies provenance metadata (`source_id`/`document_id`/`run_id`); a single persistent session with no per-source key still shares one global scope — intended for same-user merges, but to be decided consciously at plan time.
