---
id: 093-turingdb-llm-graph-access-path
title: TuringDB LLM graph access path for Aura
date: 2026-07-09
status: PARTIAL_CUSTOM_BRIDGE
type: standard
tags: [turingdb, rest, mcp, llm-tools, auth, neo4j-alternative]
related: .planning/spikes/032-agent-memory-mcp-live-mount, .planning/spikes/067-apache-age-pipeline, .planning/spikes/071-arcadedb-adopt-strategy
---

# Spike 093 - TuringDB LLM graph access path

## Question

Can Aura expose TuringDB to an LLM as a graph tool path with query, schema, write, and auth semantics comparable to the current Neo4j MCP lane?

## Harness

Run from PowerShell on the Docker Desktop host:

```powershell
powershell -ExecutionPolicy Bypass -File .planning\spikes\093-turingdb-llm-graph-access-path\run.ps1
```

The harness starts the wheel-backed TuringDB daemon inside Docker, proves REST write/read, sketches an MCP-shaped REST bridge, and separately probes bearer-token auth with `-auth-on`.

## Results

Verdict: PARTIAL_CUSTOM_BRIDGE.

| Check | Result |
|---|---|
| Start REST daemon | PASS, 592 ms |
| REST write/read round-trip | PASS |
| MCP-shaped query/schema bridge sketch | PASS |
| Start auth daemon | PASS, 1014 ms |
| Missing token rejected | PASS, HTTP 401 |
| Bearer token accepted | PASS |

TuringDB exposes enough REST/SDK surface to build an Aura MCP bridge with at least `turingdb_query` and `turingdb_schema` tools. But no native MCP server or Bolt endpoint was found in the pinned package/image path. Unlike ArcadeDB's built-in MCP or Neo4j's Bolt ecosystem, Aura would own and maintain this bridge. That makes the access path feasible, not drop-in.
