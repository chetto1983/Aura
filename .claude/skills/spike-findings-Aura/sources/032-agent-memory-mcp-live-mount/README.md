---
spike: 032
name: agent-memory-mcp-live-mount
type: standard
validates: "Given the compose aura-agent-memory-mcp service, when Aura opens it via streamable HTTP and mounts tools through managed MCP policy, then expected memory tools list, policy decisions apply, and ping/close are clean"
verdict: VALIDATED
related: [001, 002, 031]
tags: [phase-15, memory, mcp, neo4j, streamable-http, live-mount]
---

# Spike 032: Agent Memory MCP Live Mount

## What This Validates

Given the `aura-agent-memory-mcp` compose service on `127.0.0.1:8091/mcp/`, when Aura's streamable-HTTP MCP client opens it, lists tools, pings it, and mounts it through `mcptools.MountManagedServerWithPolicy`, then the Neo4j Agent Memory tools are reachable and policy filtering can block writes while retaining read tools.

## Research

Neo4j Agent Memory documents the MCP server as a profile-based memory surface: core tools cover the essential read/write cycle, while the extended profile exposes conversation history, entity details, graph export, relationships, reasoning traces, observations, and read-only Cypher. The local checkout at `D:/tmp/agent-memory` confirms the compose command uses the Python FastMCP server with the extended profile.

| Approach | Tool/Library | Pros | Cons | Status |
|---|---|---|---|---|
| Streamable HTTP MCP via Aura `mcp.HTTPClient` | Existing Aura code | Tests the exact compose endpoint Aura will mount | Requires the live service | Chosen |
| Direct Python SDK probe | `neo4j-agent-memory` | Bypasses MCP ambiguity | Does not validate Aura's bridge | Rejected for this spike |
| Raw curl JSON-RPC | PowerShell/curl | Minimal dependencies | Reimplements the client path | Rejected |

Chosen approach: use Aura's `mcp.OpenServer` and `mcptools.MountManagedServerWithPolicy` directly.

## How to Run

```powershell
go run ./.planning/spikes/032-agent-memory-mcp-live-mount
```

Optional endpoint override:

```powershell
$env:AURA_AGENT_MEMORY_MCP_URL = "http://127.0.0.1:8091/mcp/"
go run ./.planning/spikes/032-agent-memory-mcp-live-mount
```

## What to Expect

The harness prints timestamped forensic log lines plus a JSON summary. A validated run shows:

- streamable-HTTP initialize succeeds
- ping succeeds
- `tools/list` includes the expected memory tools
- a policy mount with `DenyRisk=write` blocks write tools
- mounted tools are deferred and namespaced as `memory__*`

## Observability

The harness emits ISO-timestamped `[CATEGORY]` log lines and a JSON summary with raw tool names, mounted read tools, blocked tools, missing expected tools, and final verdict.

## Investigation Trail

- 2026-06-08 live run opened `http://127.0.0.1:8091/mcp/` with Aura's streamable-HTTP MCP client.
- `initialize` and `ping` both succeeded.
- `tools/list` returned 16 tools: `graph_query`, `memory_add_entity`, `memory_add_fact`, `memory_add_preference`, `memory_complete_trace`, `memory_create_relationship`, `memory_export_graph`, `memory_get_context`, `memory_get_conversation`, `memory_get_entity`, `memory_get_observations`, `memory_list_sessions`, `memory_record_step`, `memory_search`, `memory_start_trace`, `memory_store_message`.
- A `DenyRisk=write` mount kept the expected read tools (`memory__memory_get_context`, `memory__memory_get_conversation`, `memory__memory_search`) and blocked write/graph/reasoning mutation tools.
- All mounted MCP tools were exposed as deferred Aura tools.

## Results

Verdict: VALIDATED.

The compose `aura-agent-memory-mcp` service is reachable through Aura's MCP bridge at `127.0.0.1:8091/mcp/`. The live service exposes the expected Neo4j Agent Memory extended MCP surface, Aura can ping and close it cleanly, and policy filtering can mount a read-only subset.
