---
spike: 031
name: phase15-memory-source-audit
type: standard
validates: "Given Phase 15 memory requirements and upstream graph/memory repos in D:/tmp, when llm-graph-builder and local master examples are audited, then Aura has concrete patterns and landmines before planning the memory subsystem"
verdict: PARTIAL
related: [001, 003, 005, 014, 016]
tags: [phase-15, memory, neo4j, graphrag, graph-builder, mem0]
---

# Spike 031: Phase 15 Memory Source Audit

## What This Validates

Phase 15 is the memory subsystem: document ingest, chunk embeddings, extracted entities, Neo4j graph storage, GDS Leiden communities, and GraphRAG retrieval combining HNSW vector search, BM25 fulltext, one-hop graph expansion, and rerank. Before planning implementation, this spike audits the upstream and local example repos the operator asked to inspect:

- `neo4j-labs/llm-graph-builder`
- all relevant local master/example repos already present under `D:/tmp`

This is an evidence-gathering spike, not a live benchmark. The verdict is `PARTIAL`: it is enough to shape the implementation plan, but the Phase 15 acceptance criteria still need a live Go/Neo4j harness.

## Sources Inspected

| Source | Evidence | Phase 15 Signal |
|---|---|---|
| `D:/tmp/llm-graph-builder` | cloned from `https://github.com/neo4j-labs/llm-graph-builder`, commit `61121df4c15716f67636a4fac2c96e909d374ada` | Best source of Cypher, vector/fulltext/graph retrieval, GDS Leiden, and operational checks. Not a direct dependency. |
| `D:/tmp/aura-neo4j-spike-2026-05-27` | local prior Aura spike | Stronger performance evidence than graph-builder: structured Neo4j traversal had a reported 15-45x speedup over blob+LLM and 22-30ms retrieval probes on small corpora. |
| `D:/tmp/mem0` | local master checkout | Do not copy old OSS graph memory examples. The current changelog says OSS graph memory was removed. Reuse the phased memory add/search/history shape instead. |
| `D:/tmp/recursive-llm` | local master checkout | Useful follow-up for reducing extraction calls over long documents, but risky as the Phase 15 core ingest path. |
| `D:/tmp/graphify` | local master checkout | Useful graph-inspection and relevance-model prior art, not a backing-store implementation. |
| `D:/tmp/llm_wiki` | local master checkout | Useful source-of-truth, incremental-cache, graph expansion, and token-budget ideas. |

## Findings

1. `llm-graph-builder` should be mined for patterns, not ported. Its backend is Python/FastAPI/LangChain and too broad for Aura's Go/MCP architecture.
2. Its ingest shape matches Aura's intended broad flow: create chunks, create chunk embeddings, extract graph documents with an LLM, persist to Neo4j, then connect chunks to entities.
3. Its `Document` identity is file-name oriented, which is too weak for Aura. Phase 15 should use content-hash dedup as a first-class invariant.
4. Its duplicate entity handling is post-hoc cleanup based on similarity and APOC merges. Aura should instead add normalized keys, unique constraints, and a 10-goroutine Mario Rossi chaos test.
5. Its GraphRAG query patterns are valuable: chunk vector search, fulltext search, entity vector search, graph vector search, and graph+vector+fulltext modes.
6. Its vector-dimension guard is useful operationally, but Aura must enforce `AURA_EMBED_DIMENSIONS=768` and HNSW index configuration, including `M=32`, rather than inherit graph-builder defaults.
7. Its GDS Leiden implementation is relevant for `:Community` derivation, but communities should be derived after raw ingest stabilizes, not mixed into the primary document transaction.
8. `mem0` is useful for memory API shape: phased add/search, existing-memory retrieval, extraction, batch embeddings, hash dedup, history writes, and entity linking. It is not useful as a graph-store dependency because current OSS graph memory has been removed.
9. The prior Aura Neo4j spike is still the strongest local evidence for the architecture. It should carry more weight than llm-graph-builder for performance claims.

## Approach Comparison

| Approach | Decision | Reason |
|---|---|---|
| Port `llm-graph-builder` backend | Reject | Too much Python/LangChain/FastAPI surface area; conflicts with Aura's Go architecture and Phase 15 acceptance tests. |
| Reuse `llm-graph-builder` Cypher and operational patterns | Choose | Gives concrete retrieval, index, capability-check, duplicate-merge, and Leiden examples. |
| Copy `mem0` graph memory examples | Reject | Current OSS changelog says graph memory was removed; old examples are stale. |
| Reuse `mem0` phased memory flow | Choose | Good model for AgentEpisode/AgentInsight journal semantics and history. |
| Use `recursive-llm` as ingest engine | Defer | Promising call-reduction idea, but should be a separate speed/quality spike after the baseline ingest path exists. |

## Implementation Guidance

Phase 15 should be planned as a Go-first subsystem:

- `MemoryStore` with idempotent document ingest keyed by SHA-256 or equivalent content hash.
- Neo4j schema constraints for `Document`, `Chunk`, `Entity`, `Community`, `AgentEpisode`, and `AgentInsight`.
- Embedding dimension enforcement from configuration, defaulting to `AURA_EMBED_DIMENSIONS=768`.
- Chunk HNSW vector index configured with the explicit Phase 15 `M=32` requirement.
- Fulltext indexes for chunks and entities.
- A hybrid retrieval pipeline: vector candidates, BM25 candidates, one-hop graph expansion, then LLM rerank.
- Separate derived-community job using GDS Leiden after raw ingest.
- Concurrency test for Mario Rossi entity merging, with 10 goroutines inserting conflicting mentions.
- Recall/latency fixture that grows toward the 100K-document acceptance target.

## How To Run

```powershell
go run ./.planning/spikes/031-phase15-memory-source-audit
```

The harness checks that the required upstream evidence files and patterns are present under `D:/tmp`. Override the source root with:

```powershell
$env:AURA_SPIKE_SOURCE_ROOT="D:\tmp"
go run ./.planning/spikes/031-phase15-memory-source-audit
```

## Result

Verdict: `PARTIAL`.

The source audit is complete enough to inform the Phase 15 plan. It does not satisfy the Phase 15 live acceptance criteria by itself. The next spike or implementation slice should run against Aura's intended Go boundary and prove:

- content-hash document dedup
- idempotent chunk/entity writes
- Mario Rossi chaos merge correctness
- HNSW vector plus BM25 plus one-hop graph retrieval
- p95 retrieval latency and recall snapshots
