---
id: 092-turingdb-vector-graphrag-parity
title: TuringDB vector GraphRAG parity probe
date: 2026-07-09
status: VALIDATED
type: standard
tags: [turingdb, vector-search, graphrag, embeddings, neo4j-alternative]
related: .planning/spikes/069-arcadedb-vs-neo4j-realdata, .planning/spikes/070-rerank-value-or-overengineered
---

# Spike 092 - TuringDB vector GraphRAG parity

## Question

Does current TuringDB support Aura's core vector-to-graph retrieval primitive: ANN search over 384-dimensional embeddings, followed by a graph `MATCH` in one query?

## Harness

Run from PowerShell on the Docker Desktop host:

```powershell
powershell -ExecutionPolicy Bypass -File .planning\spikes\092-turingdb-vector-graphrag-parity\run.ps1
```

The harness creates 256 synthetic 384-dimensional document embeddings, loads them into a TuringDB vector index, checks exact-neighbor recovery for `Doc 42`, composes vector search with `MATCH`, and records warm latency.

## Results

Verdict: VALIDATED.

| Check | Result |
|---|---|
| Seed 256 `Document` nodes | PASS, 446 ms |
| Create/load 384d cosine vector index | PASS, 99 ms |
| Standalone vector top-5 | PASS: `[42, 206, 58, 18, 210]` |
| Exact Python cosine top-5 | `[42, 206, 58, 18, 210]` |
| Overlap@5 | 1.00 |
| Top-1 exact self-match | PASS |
| `VECTOR SEARCH ... MATCH` composition | PASS |
| `YIELD ids, score` with composed `MATCH` | PASS |
| Warm vector-search latency | p50 45.242 ms, p95 57.243 ms, 25 runs |

Finding: TuringDB's current vector feature is real enough for Aura's basic GraphRAG primitive. It is not Neo4j API compatible, but a ported query can do vector search, retain scores, and join into graph nodes in one query.

One ordering caveat: the composed `MATCH` rows did not preserve standalone vector-search order in the returned row order (`Doc 42` remained top in standalone but appeared third in the composed row list). A production port should explicitly sort by retained `score`.
