---
id: 091-turingdb-aura-cypher-compat
title: TuringDB Cypher compatibility against Aura's Neo4j surfaces
date: 2026-07-09
status: INVALIDATED_AS_DROP_IN
type: standard
tags: [turingdb, cypher, neo4j, agent-memory, graphrag, compatibility]
related: .planning/spikes/067-apache-age-pipeline, .planning/spikes/068-arcadedb-pipeline
---

# Spike 091 - TuringDB Cypher compatibility

## Question

Can TuringDB run Aura's Neo4j-facing Cypher surfaces directly, or is it only a porting target?

## Harness

Run from PowerShell on the Docker Desktop host:

```powershell
powershell -ExecutionPolicy Bypass -File .planning\spikes\091-turingdb-aura-cypher-compat\run.ps1
```

The harness runs `turingdb==1.35` inside the Docker image from `../turingdb.Dockerfile`, seeds a tiny POLE-like graph, creates a vector index, and records pass/fail for native basics, metadata procedures, Neo4j drop-in queries, agent-memory-style constructs, vector search, and graph algorithms.

## Results

Verdict: INVALIDATED_AS_DROP_IN. TuringDB is viable as a porting target for a narrower graph/vector workload, but not a drop-in Neo4j replacement for Aura's current Neo4j plus agent-memory stack.

| Importance bucket | Pass | Fail |
|---|---:|---:|
| required native basics | 3 | 0 |
| required if porting | 1 | 0 |
| useful | 5 | 1 |
| required for Neo4j drop-in | 1 | 3 |
| required for agent-memory | 0 | 7 |
| required for Neo4j GDS | 0 | 1 |

Works now: basic `MATCH`, multi-label nodes, `labels(n)`, `edgeType(e)`, Neo4j-style `type(r)`, TuringDB metadata procedures, native `VECTOR SEARCH ... MATCH`, native `shortestPath(...)`, and post-submit `MATCH ... SET ...`.

Drop-in blockers: `CALL db.relationshipTypes()`, `elementId(n)`, `properties(n)`, `datetime()`, `duration(...)`, `point(...)`, APOC, `CALL { ... }`, `MERGE ... ON CREATE SET`, `FOREACH`, Neo4j vector procedure `db.index.vector.queryNodes`, and GDS `gds.pageRank`.

Net: this clears a better native-Cypher baseline than the stale June desk report, but Aura would still need a real port of agent-memory and GraphRAG queries rather than a driver swap.
