---
spike: 033
name: agent-memory-write-read-ground-truth
type: standard
validates: "Given the mounted agent-memory MCP server and a unique session id, when Aura stores messages, entities, preferences, and facts, then read tools return the same tagged memory with session isolation"
verdict: VALIDATED
verdict_history: "PARTIAL 2026-06-08 (long-term entity/preference isolation failed) -> fixed in fork branch aura/provenance-safe-dedup (commit c1c2d65) -> re-VALIDATED 2026-06-08T19:07 live against image aura-agent-memory-mcp:spike-fixed (all 10 checks pass)"
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

### Re-validation after fix (2026-06-08T19:07, live `aura-agent-memory-mcp:spike-fixed`)

The long-term isolation failure shared its root cause with spike 034 and was fixed in the fork branch `aura/provenance-safe-dedup` (commit `c1c2d65`). Re-run live against `:spike-fixed`, tag `AURA-SPIKE-033-1780945610`, **all 10 checks pass**:

- `store_message`, `add_entity`, `add_preference`, `add_fact` all stored.
- The new entity `RoboManual AURA-SPIKE-033-1780945610` wrote with `deduplication: action=none, matched_entity_name=null, similarity_score=0.0` — no merge into a prior run's entity.
- `entity_search_readback` and `preference_search_readback` returned *this* run's records (isolation holds).
- `conversation_readback`, `message_search_readback`, `context_readback`, `fact_graph_readback` all pass.

## Results

Verdict: VALIDATED (originally PARTIAL — see verdict_history + re-validation above).

The original run validated the short-term write/read path (messages, conversation, context, fact read-back) but found long-term entity/preference dedup merged distinct tagged runs at similarity ~0.997. The `aura/provenance-safe-dedup` fork fix resolves this; the live re-run confirms long-term entity and preference isolation now hold through the MCP surface, with all ten checks green. The same standing caveat as spike 034 applies: provenance scope engages only when the ingest path supplies a per-source/per-run key.
