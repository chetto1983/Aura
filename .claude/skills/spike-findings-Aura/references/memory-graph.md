# memory graph

Phase-15 agent-memory: mounting the `aura-agent-memory-mcp` sidecar (Neo4j Agent Memory, FastMCP) over streamable HTTP, write/read ground truth, provenance-safe dedup (fork branch `aura/provenance-safe-dedup`, commit `c1c2d65`), chaos-merge isolation, and deferred-tool loop recall through the real `LlmAgent`. All five spikes ran live against the compose sidecar on `127.0.0.1:8091`. 032/035 VALIDATED first try; 033/034 went PARTIAL/INVALIDATED then re-VALIDATED after the fork fix; 031 is a PARTIAL source-audit (prior art only, no live harness).

## Requirements

These are the Session-8 binding non-negotiables for `/gsd-plan-phase 15` (MANIFEST.md "Session-8 requirements", emerged from spikes 032-035). Each is a hard constraint.

- **Live sidecar target is fixed.** `aura-agent-memory-mcp` is mounted as a streamable-HTTP MCP server at `http://127.0.0.1:8091/mcp/` via Aura's existing client (`mcp.OpenServer` / `mcptools.MountManagedServer`). Endpoint override env: `AURA_AGENT_MEMORY_MCP_URL` (full URL) or `AURA_AGENT_MEMORY_MCP_PORT` (default `8091`). Trust class is `mcp.TrustRemoteHTTP`.
- **16 tools, kept namespaced + deferred.** The extended Neo4j Agent Memory surface exposes exactly 16 MCP tools. When mounted into the agent loop they MUST be namespaced `memory__*` and every one MUST carry `Spec().Deferred = true` (no full MCP schema in the default manifest; the model fetches via `tool_search`).
- **Policy must classify read / write / graph / trace.** MCP mount policy must be able to distinguish these four risk classes. A `DenyRisk=write` probe (`mcptools.MountManagedServerWithPolicy`) MUST keep the three read tools (`memory__memory_search`, `memory__memory_get_context`, `memory__memory_get_conversation`) and block the write/graph/trace mutation tools.
- **Facts need a sanctioned read path.** Short-term message read-back, context retrieval, and fact read-back are validated, BUT facts are not in the `memory_search` integration path — they currently round-trip only through the read-only `graph_query` MCP tool (Cypher). If facts become first-class model-facing memory, Phase 15 must add a sanctioned fact read path, not lean on `graph_query`.
- **Upstream semantic dedup is NOT provenance-safe — supply provenance keys.** Long-term entity/preference dedup over-merges distinct-but-similar records (similarity 0.95-0.997). Phase 15 must supply exact identity/source/session keys (`source_id`/`document_id`/`run_id`), metadata filters, or an Aura-side merge policy. The fork fix (`c1c2d65`) only engages provenance scope when the ingest path actually passes that metadata; a single persistent session with no per-source key still shares one global scope (intended for same-user merge — decide consciously at plan time).
- **Loop recall is validatable deterministically (no paid calls).** The agent-loop memory path (deferred-tool discovery → MCP dispatch → recall) must be proven with `agenttest.FakeClient` scripting `tool_search` → `memory__memory_search` → `text_response` over the real `LlmAgent` and live MCP bridge, before spending live model calls on autonomous tool-choice quality.

## How to Build It

### 1. Open / ping / list the live sidecar (spike 032, VALIDATED)

The exact streamable-HTTP open path that works against the compose service:

```go
server := mcp.ManagedServer{
    Type:  mcp.ServerTypeStreamableHTTP,
    URL:   "http://127.0.0.1:8091/mcp/", // or AURA_AGENT_MEMORY_MCP_URL
    Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP},
}
cli, err := mcp.OpenServer(ctx, "agent-memory", server) // initialize OK
cli.Ping(ctx)                                           // ping OK
defs, _ := cli.ListTools(ctx)                           // 16 ToolDef
defer cli.Close()                                       // clean close
```

The 16 tools returned by `tools/list` (live 2026-06-08), grouped by risk class:

- **read:** `memory_search`, `memory_get_context`, `memory_get_conversation`, `memory_get_entity`, `memory_get_observations`, `memory_list_sessions`, `memory_export_graph`, `graph_query` (read-only Cypher)
- **write:** `memory_store_message`, `memory_add_entity`, `memory_add_preference`, `memory_add_fact`, `memory_create_relationship`
- **trace:** `memory_start_trace`, `memory_record_step`, `memory_complete_trace`

### 2. Mount into the registry as deferred namespaced tools (spike 032)

```go
reg := tools.NewRegistry()
closer, mounted, err := mcptools.MountManagedServer(ctx, reg, "memory", server)
defer closer()
// len(mounted) == 16; each reg.Get("memory__...").Spec().Deferred == true
```

Policy-filtered mount (read-only subset) uses `mcptools.MountManagedServerWithPolicy` with `DenyRisk=write`. Two real policy results observed:
- Spike 032 `DenyRisk=write` probe: kept the 3 read tools, blocked the 13 write/graph/trace tools.
- Spike 035 production-shape loop mount: `mounted=13 blocked=3` (blocked the highest-risk 3, mounted the rest). Pick the policy deliberately at plan time — both shapes are proven against the live surface.

### 3. Write/read ground truth through the Aura tool bridge (spike 033, VALIDATED after fix)

Write tools execute through the registry (not the raw transport) so they go through Aura's tool-call context. The seam:

```go
callID := strings.ReplaceAll(toolName, "__", "-") + fmt.Sprintf("-%d", time.Now().UnixNano())
toolCtx := tools.WithToolCallContext(ctx, sessionID, callID, os.TempDir(), 1<<20)
t, _ := reg.Get("memory__memory_store_message")
res, err := t.Execute(toolCtx, rawJSONArgs) // success body contains `"stored": true`
```

Write payload shapes that worked (use a unique per-run tag for isolation — `AURA-SPIKE-033-<unix>`):
- `memory__memory_store_message`: `{content, role:"user", session_id, metadata:{...,tag}}`
- `memory__memory_add_entity`: `{name, entity_type:"OBJECT"|"PERSON", description, aliases:[...], metadata:{...,tag}}`
- `memory__memory_add_preference`: `{category, preference, context, confidence:1.0}`
- `memory__memory_add_fact`: `{subject, predicate, object_value, confidence:1.0, metadata:{...,tag}}`

Read-back assertions (all green on `:spike-fixed`):
- `memory__memory_get_conversation` `{session_id, limit, include_metadata:true}` → returns exact stored message.
- `memory__memory_search` `{query, limit, memory_types:["messages"|"entities"|"preferences"], session_id, threshold:0.0}` → returns the tagged record (entities/preferences need `threshold:0.0` to surface).
- `memory__memory_get_context` `{session_id, query, max_items, include_short_term:true, include_long_term:true, include_reasoning:false}` → contains the run tag.
- **Facts read back only via `graph_query`** (direct transport, bypasses policy bridge — fact read is read-only Cypher):
  ```go
  cli.CallTool(ctx, "graph_query", map[string]any{
    "query": "MATCH (f:Fact {subject: $subject}) RETURN f.subject, f.predicate, f.object LIMIT 5",
    "parameters": map[string]any{"subject": factSubject},
  })
  ```
  `graph_query` returns `{success, rows:[{...}], error}`.

### 4. Provenance-safe dedup + Mario Rossi chaos (spike 034, INVALIDATED → re-VALIDATED via fork)

Chaos harness: spawn 10 concurrent goroutines calling `memory_add_entity` with name variants of one tagged identity (`Mario Rossi <tag>`, `M. Rossi <tag>`, `Rossi Mario <tag>`, lowercase, `MARlO ROSSI` l/I confusion, etc.) on the same `mcp.Transport`, then query by tag:

```cypher
MATCH (e:Entity)
WHERE toString(e.name) CONTAINS $tag OR toString(e.canonical_name) CONTAINS $tag
   OR toString(e.description) CONTAINS $tag
RETURN e.id, e.name, e.canonical_name, e.description ORDER BY name LIMIT 50
```

Pass criteria: `exact_name_count == 1`, `fragmented == false` (≤1 tagged row), `over_merge == false`. Over-merge is detected from the write response: each `memory_add_entity` returns `deduplication:{action, matched_entity_name, similarity_score}`; if `matched_entity_name` is non-empty and does NOT contain the current run tag, it merged into a foreign run → fail.

The fork fix is **branch `aura/provenance-safe-dedup`, commit `c1c2d65`** ("provenance-scoped dedup keyed off write metadata", 83/83 package unit tests green), built and run as Docker image **`aura-agent-memory-mcp:spike-fixed`**. After the fix, a new tagged run's first write returns `deduplication: action=none, matched_entity_name=null, similarity_score=0.0` — it creates its OWN canonical entity despite ~0.97 embedding similarity to a prior run, and the other 9 variants collapse into THIS run's canonical (`over_merge=false`, `tagged_count=1`, `exact_name_count=1`). This is the local checkout at `D:/tmp/agent-memory` (the operator-modifiable fork).

### 5. Deterministic loop recall through the real LlmAgent (spike 035, VALIDATED)

Seed a tagged message via the raw transport, then run the actual production `LlmAgent` with a scripted fake client — proves deferred discovery + MCP dispatch with zero paid model calls:

```go
reg := tools.NewRegistry()
reg.Register(tools.TextResponse{})
reg.Register(&tools.ToolSearch{Registry: reg})
reg.Register(&tools.ReadToolOutput{})
closer, mounted, _ := mcptools.MountManagedServer(ctx, reg, "memory", server) // mounted=13 blocked=3

fake := agenttest.NewFakeClient(
  agenttest.ToolCallTurn(agenttest.MakeToolCall("call_search_spec", "tool_search", `{"query":"select:memory__memory_search"}`)),
  agenttest.ToolCallTurn(agenttest.MakeToolCall("call_memory_search", "memory__memory_search", searchArgs)),
  agenttest.ToolCallTurn(agenttest.MakeToolCall("call_final", "text_response", `{"text":"recalled <tag> via memory MCP"}`)),
)
budget, _ := agent.NewBudget(agent.BudgetOptions{MaxSteps: intPtr(6), MaxWallclockSec: intPtr(90)})
a := agent.NewLlmAgent(agent.LlmAgentConfig{Name:"...", Client: fake, Registry: reg, RunDir: os.TempDir(), SessionID: sessionID, ...})
for ev, runErr := range a.Run(agent.InvocationContext{Ctx: ctx, Agent: a, RequestID: uuidV7, Branch: "root", Budget: budget}) { ... }
```

Assertions proven: (1) first LLM request exposes `memory__memory_search` ONLY as a deferred stub — detect via `len(def.Function.Parameters)==0 && strings.Contains(def.Function.Description,"Required args:")`; (2) loop emits `start/end:tool_search` and `start/end:memory__memory_search` lifecycle events (`ev.Actions.ToolInvocation`); (3) the `memory__memory_search` `ResultPreview` contains the seeded tag; (4) final `text_response` carries the tag.

### 6. Source-audit prior art (spike 031, PARTIAL — patterns only, do not port)

`neo4j-labs/llm-graph-builder` (commit `61121df4c15716f67636a4fac2c96e909d374ada`) is the concrete Cypher/GDS/retrieval reference, mined NOT ported (Python/FastAPI/LangChain — wrong for Aura's Go/MCP arch). Reusable patterns: ingest shape (chunks → chunk embeddings → LLM graph-doc extract → persist → connect chunks to entities); GraphRAG query modes (`VECTOR_GRAPH_SEARCH_QUERY`: chunk vector + fulltext + entity vector + graph vector + hybrid); `connection_check_and_get_vector_dimensions` operational guard; `gds.leiden.write` for `:Community` derivation. Schema constraints to add: `Document`, `Chunk`, `Entity`, `Community`, `AgentEpisode`, `AgentInsight`; content-hash document identity (SHA-256), not filename. Derive Leiden communities AFTER raw ingest stabilizes — not inside the primary document transaction. `mem0` (`D:/tmp/mem0`): reuse the phased add/search/history shape; its OSS graph memory was REMOVED (`Graph Memory Removed (OSS)` in changelog) — examples are stale. Prior Aura Neo4j spike (`D:/tmp/aura-neo4j-spike-2026-05-27`) is the stronger local performance signal (15-45x speedup vs blob+LLM, 22-30ms retrieval probes).

## What to Avoid

- **Do not trust upstream Neo4j Agent Memory semantic dedup for provenance/isolation.** Original spike 034 was INVALIDATED and 033 was PARTIAL because stock dedup over-merged a fresh tagged `Mario Rossi`/`RoboManual` run into an older one at similarity 0.95-0.997 (entity AND preference memory both affected). Looks correct in single-run tests; fails the moment two intentionally-similar runs coexist. Only the `aura/provenance-safe-dedup` fork (`c1c2d65`) fixes it, and only when ingest supplies provenance metadata.
- **Do not rely on a single persistent session with no per-source key to keep records distinct.** With no `source_id`/`document_id`/`run_id`, everything shares one global dedup scope (by design, for same-user merge). That is a deliberate plan-time decision, not a safe default.
- **Do not read facts via `memory_search`.** Facts are NOT in the `memory_search` integration path; `memory_search memory_types:["facts"]` will not surface them. They only round-trip through `graph_query` Cypher today. Don't make facts model-facing memory until a sanctioned fact read path exists.
- **Do not port `llm-graph-builder` or copy `mem0` graph-memory examples.** Graph-builder is too much Python/LangChain/FastAPI surface; mem0's OSS graph memory was removed (changelog `Graph Memory Removed (OSS)`) so its old examples are stale.
- **Do not inherit graph-builder's `Document`-by-filename identity** — too weak. Use content-hash (SHA-256) dedup as a first-class invariant.
- **Do not mix Leiden community derivation into the primary document write transaction.** Run GDS Leiden as a separate derived-community job after raw ingest stabilizes.
- **Spike 031 is a source audit only (PARTIAL).** It does NOT satisfy any live Phase-15 acceptance criterion — content-hash dedup, idempotent chunk/entity writes, chaos merge, HNSW+BM25+1-hop retrieval, p95/recall still need a live Go/Neo4j harness.

## Constraints

- **Sidecar:** compose service `aura-agent-memory-mcp`, streamable-HTTP MCP at `http://127.0.0.1:8091/mcp/`. Python FastMCP server, Neo4j Agent Memory, extended profile. Fork checkout: `D:/tmp/agent-memory`.
- **Tool surface:** exactly 16 MCP tools (extended profile). Namespaced `memory__*` and `Deferred=true` when mounted. `memory_get_facts` is in the harness's expected list but was NOT among the 16 live tools — facts read via `graph_query` instead.
- **Fork pin:** branch `aura/provenance-safe-dedup`, commit `c1c2d65`, image tag `aura-agent-memory-mcp:spike-fixed`, 83/83 package unit tests green. Provenance scope engages only when write metadata (`source_id`/`document_id`/`run_id`) is supplied.
- **Dedup similarity envelope (observed):** stock dedup over-merged at 0.9536-0.9973 (entities) and ~0.997 (preferences/RoboManual). Fixed fork creates a new canonical even at ~0.97 similarity when the run key differs.
- **Env vars:** `AURA_AGENT_MEMORY_MCP_URL` (full URL override), `AURA_AGENT_MEMORY_MCP_PORT` (default `8091`). `AURA_SPIKE_SOURCE_ROOT` (default `D:\tmp`) for the 031 audit harness.
- **Aura seams (real, in-repo):** `internal/mcp` (`ManagedServer`, `OpenServer`, `Transport.CallTool`, `Ping`, `ListTools`, `ServerTypeStreamableHTTP`, `TrustRemoteHTTP`); `internal/agent/mcptools` (`MountManagedServer`, `MountManagedServerWithPolicy`); `internal/agent/tools` (`Registry`, `WithToolCallContext`, `TextResponse`, `ToolSearch`, `ReadToolOutput`); `internal/agent` (`NewLlmAgent`, `LlmAgentConfig`, `NewBudget`, `InvocationContext`, `ToolInvocationEnd`); `internal/agent/agenttest` (`NewFakeClient`, `ToolCallTurn`, `MakeToolCall`).
- **Embedding dimension caveat:** spike 031 recommended `AURA_EMBED_DIMENSIONS=768` + HNSW `M=32`. STALE on the dimension — later graph-DB spikes (MANIFEST sessions 17/18) correct embeddings to **384d (Granite-97m)**, NOT 768d. Honor the HNSW `M=32` + dimension-guard pattern, but use 384d.
- **Module path:** spike harnesses import `github.com/chetto1983/aura/...` (the repo's module path).
- **graph_query result shape:** `{success:bool, rows:[]map, error:string}`. Write tools return success bodies containing `"stored": true`; entity writes return `deduplication:{action, matched_entity_name, similarity_score}`.

## Origin

Synthesized from spikes: 031, 032, 033, 034, 035 (MANIFEST Session-7 + Session-8). Source files in: `sources/031-phase15-memory-source-audit/`, `sources/032-agent-memory-mcp-live-mount/`, `sources/033-agent-memory-write-read-ground-truth/`, `sources/034-agent-memory-dedup-chaos/`, `sources/035-agent-memory-loop-recall/` (each README.md + main.go harness). Verdicts: 031 PARTIAL (source audit, no live harness); 032 VALIDATED; 033 VALIDATED (was PARTIAL → fixed `c1c2d65` → re-VALIDATED 2026-06-08T19:07); 034 VALIDATED (was INVALIDATED → fixed `c1c2d65` → re-VALIDATED 2026-06-08T19:03); 035 VALIDATED.
