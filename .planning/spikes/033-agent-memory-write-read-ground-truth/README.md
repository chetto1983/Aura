---
spike: 033
name: agent-memory-write-read-ground-truth
type: standard
validates: "Given the mounted agent-memory MCP server and a unique session id, when Aura stores messages, entities, preferences, and facts, then read tools return the same tagged memory with session isolation"
verdict: PARTIAL
related: [032]
tags: [phase-15, memory, mcp, neo4j, write-read, ground-truth]
---

# Spike 033: Agent Memory Write-Read Ground Truth

## What This Validates

Given the live `aura-agent-memory-mcp` service and a unique `AURA-SPIKE-033-*` session id, when Aura writes memory through MCP tools, then exact and semantic read paths return the same tagged memory.

## Research

The local Neo4j Agent Memory implementation exposes the core MCP write/read tools through `memory_store_message`, `memory_add_entity`, `memory_add_preference`, `memory_add_fact`, `memory_get_conversation`, `memory_search`, and `memory_get_context`. Facts are not included in the high-level `memory_search` integration path, so this harness reads them back through the server's read-only `graph_query` MCP tool.

| Approach | Tool/Library | Pros | Cons | Status |
|---|---|---|---|---|
| Aura bridge execution | `mcptools` + `tools.Registry` | Proves MCP tools behave as Aura tools | Needs tool-call context for results | Chosen for core memory tools |
| Direct MCP call | `mcp.Transport.CallTool` | Useful for graph_query read-back | Bypasses policy bridge | Used only for fact read-back |
| Direct Neo4j driver | Neo4j Go/Python driver | Precise DB assertions | Bypasses MCP | Rejected |

## How to Run

```powershell
go run ./.planning/spikes/033-agent-memory-write-read-ground-truth
```

## What to Expect

The harness writes a unique tagged message, entity, preference, and fact. It then asserts:

- `memory_get_conversation` returns the exact message
- `memory_search` returns the message, entity, and preference
- `memory_get_context` contains the run tag
- `graph_query` returns the fact

## Observability

The harness emits ISO-timestamped log lines and a JSON summary of every check.

## Investigation Trail

- 2026-06-08 live run used tag `AURA-SPIKE-033-1780926447` and session `aura-spike-033-1780926447-session`.
- `memory_store_message`, `memory_add_entity`, `memory_add_preference`, and `memory_add_fact` all returned stored responses.
- `memory_get_conversation` returned the exact tagged message.
- `memory_search` over messages returned the exact tagged message for the unique session.
- `memory_get_context` included the run tag.
- `graph_query` read the tagged fact back by exact subject.
- Long-term entity isolation failed: the new `RoboManual AURA-SPIKE-033-1780926447` entity was merged into a prior spike run (`RoboManual AURA-SPIKE-033-1780926389`) at similarity `0.997314453125`, so entity search returned the prior run's description rather than the new tag.
- Preference writes also reused a prior preference id in this repeated-tag-family scenario, showing the same semantic-dedup/provenance risk for preference memory.

## Results

Verdict: PARTIAL.

Short-term message storage, conversation read-back, context retrieval, and fact read-back are validated through the live MCP service. Exact run isolation for long-term semantic memories is invalidated: entity/preference dedup can merge distinct tagged harness runs that are intentionally similar. Production memory should not rely on semantic similarity alone when tenant/session/source/provenance boundaries matter.
