---
spike: 035
name: agent-memory-loop-recall
type: standard
validates: "Given Aura's real LlmAgent loop with the agent-memory MCP tools mounted as deferred tools, when a scripted model calls tool_search then memory_search then text_response, then the loop dispatches through the real MCP bridge and recalls the seeded memory"
verdict: VALIDATED
related: [032, 033]
tags: [phase-15, memory, mcp, agent-loop, deferred-tools, recall]
---

# Spike 035: Agent Memory Loop Recall

## What This Validates

Given a seeded memory and the live agent-memory MCP server, when Aura's actual `LlmAgent` runs with a deterministic fake LLM client that calls `tool_search`, `memory__memory_search`, and `text_response`, then the loop proves the deferred-tool discovery and MCP dispatch path without spending a live model call.

## Research

Aura's production loop dispatches tools through `tools.Registry`, injects `WithToolCallContext`, emits `ToolInvocation` lifecycle events, and keeps MCP tools deferred so the default manifest does not carry every full MCP schema. A deterministic fake LLM client is enough to exercise the real loop and the real memory MCP bridge while avoiding nondeterministic model-choice failures.

| Approach | Tool/Library | Pros | Cons | Status |
|---|---|---|---|---|
| Real `LlmAgent` + fake LLM script | Aura `agenttest.FakeClient` | Exercises real loop and real MCP tool dispatch deterministically | Does not prove autonomous model tool choice | Chosen |
| Live OpenRouter chat | Aura real LLM client | Proves model tool choice | Paid, flaky, harder to attribute failures | Deferred |
| Direct MCP calls only | `mcp.Transport` | Simple | Does not validate agent loop | Rejected |

## How to Run

```powershell
go run ./.planning/spikes/035-agent-memory-loop-recall
```

## What to Expect

The harness seeds a tagged memory, mounts the memory MCP tools, then runs the actual `LlmAgent`. It asserts:

- first LLM request exposes `memory__memory_search` only as a deferred stub
- the loop invokes `tool_search`
- the loop invokes `memory__memory_search`
- the memory search result contains the tag
- final `text_response` contains the tag

## Observability

The JSON summary includes the tool invocation lifecycle events, deferred-stub assertion, search-preview assertion, final text, and verdict.

## Investigation Trail

- 2026-06-08 live run used tag `AURA-SPIKE-035-1780926646` and session `aura-spike-035-1780926646-session`.
- The harness seeded the tagged message through live `memory_store_message`.
- Aura mounted the memory MCP server into the real `tools.Registry`; the run reported `mounted=13 blocked=3`.
- The first fake-LLM request saw `memory__memory_search` as a deferred stub.
- The real agent loop invoked `tool_search` and `memory__memory_search` with start/end lifecycle events.
- The `memory__memory_search` preview contained the seeded tag.
- The final `text_response` was `recalled AURA-SPIKE-035-1780926646 via memory MCP`.

## Results

Verdict: VALIDATED.

Aura's production `LlmAgent` loop can discover a deferred memory MCP tool, dispatch it through the real MCP bridge, read live Neo4j Agent Memory state, and carry the recalled content into the final response. This validates the agent-loop integration path independently from live model tool-choice quality.
