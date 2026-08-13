---
last_mapped_commit: 5adb3d49b9b8cd7ea4f872fbdb7199b4021c9f5c
---
<!-- refreshed: 2026-08-13 -->
# Architecture

**Analysis Date:** 2026-08-13

## System Overview

```text
┌──────────────────────────────────────────────────────────────────────────┐
│ CHANNELS / ENTRY POINTS                                                   │
├──────────────────┬──────────────────┬────────────────┬───────────────────┤
│ AG-UI HTTP/SSE    │ Telegram bot     │ CLI REPL        │ cron/scheduler    │
│ `internal/agui`   │ `internal/       │ `cmd/aura/      │ `internal/cron`   │
│ (embedded cockpit │  channels/       │  chat.go`,      │ (agent_job tasks) │
│ `internal/webui`) │  telegram`       │ `shell.go`      │                   │
└─────────┬─────────┴────────┬─────────┴────────┬────────┴─────────┬────────┘
          │                  │                  │                  │
          ▼                  ▼                  ▼                  ▼
┌──────────────────────────────────────────────────────────────────────────┐
│ ORCHESTRATION — internal/runner (Runner.Turn / TurnBranch)                │
│ per-thread lock → persist user turn → context ladder → build fresh agent  │
└───────────────────────────────┬────────────────────────────────────────-─┘
                                 │ agent.InvocationContext{Ctx,Budget,Agent}
                                 ▼
┌──────────────────────────────────────────────────────────────────────────┐
│ AGENT CORE — internal/agent (Agent interface, LlmAgent, Budget, Event)    │
│ prompt build → LLM stream → tool dispatch → history append → loop/finalize│
└───────┬───────────────────────────────────┬──────────────────────────────┘
        │ prompt.PromptBuilder                │ tools.Registry.Get/Execute
        ▼                                     ▼
┌──────────────────────────┐   ┌──────────────────────────────────────────┐
│ internal/agent/prompt     │   │ internal/agent/tools (46 built-ins)       │
│ messages[0] system prompt │   │ deferred-tool manifest, shell_exec/fs_*   │
│ + tail-injected volatile  │   │ routed into internal/sandbox/usersandbox  │
│ hints (budget/workspace/  │   │ box; document_search/open; MCP-bridged    │
│ sources/deferred_tools)   │   │ tools via internal/agent/mcptools         │
└──────────────────────────┘   └───────────────┬────────────────────────--─┘
                                                │
                 ┌──────────────────────────────┼───────────────────────────┐
                 ▼                              ▼                           ▼
    ┌──────────────────────┐   ┌──────────────────────────┐   ┌─────────────────────────┐
    │ internal/documents     │   │ internal/arcadedb         │   │ internal/gateway         │
    │ catalog + card +       │   │ typed ArcadeDB HTTP client │   │ policy PEP: mutating tool│
    │ retrieval cascade      │   │ (per-identity mem_<uuid>   │   │ calls decided/approved   │
    │ (Postgres aura.*)      │   │ database): passages, facts,│   │ before Execute            │
    └──────────┬─────────────┘   │ vectors, cards             │   └─────────────────────────┘
               │                 └───────────┬───────────────┘
               ▼                             ▲
    ┌──────────────────────┐                 │ Cypher/Bolt writes
    │ internal/objectstore   │                 │
    │ Garage/S3 bucket per   │─────────┬───────┘
    │ identity (originals)   │         │
    └──────────┬─────────────┘         │
               │ reconciled by         │
               ▼                       │
    ┌──────────────────────────────────┴───┐
    │ services/ingest (Python, CocoIndex)   │
    │ extract → chunk → embed → write       │
    │ IndexedDocument + Passage into ArcadeDB│
    └────────────────────────────────────────┘
```

## Component Responsibilities

| Component | Responsibility | File |
|-----------|----------------|------|
| CLI dispatcher | Top-level `aura <verb>` hand-rolled switch; not cobra | `cmd/aura/main.go` |
| Runner | Per-conversation orchestration: lock, persist, context ladder, fresh-agent-per-turn | `internal/runner/runner.go` |
| Agent core | Open `Agent` interface, `InvocationContext`, `Budget`, `Event` shape | `internal/agent/agent.go`, `internal/agent/event.go` |
| LlmAgent | Budget-gated tool-dispatch loop over one LLM | `internal/agent/llm_agent.go` |
| PromptBuilder | Single chokepoint assembling the wire `llm.Request` (byte-stable messages[0] + tail hints) | `internal/agent/prompt/builder.go` |
| Tool registry | Immutable per-run tool set; deferred-tool manifest + `tool_search` | `internal/agent/tools/spec.go`, `internal/agent/tools/registry.go` |
| Context ladder | L1 tool-eviction / L2 budget gate / L2.4 LLM compaction / L2.5 hard-drop over persisted history | `internal/conversations/context.go`, `internal/conversations/compaction.go` |
| Gateway (PEP) | Policy decision on mutating tool calls before `Execute` | `internal/gateway/classify.go` |
| Sandbox box | Docker-backed per-identity execution box that `shell_exec`/`fs_*`/`send_file` route into | `internal/sandbox/usersandbox` |
| Documents | Versioned catalog + card + retrieval cascade over Postgres + ArcadeDB | `internal/documents/service.go`, `internal/documents/retrieval.go` |
| ArcadeDB client | Typed HTTP/Cypher client for the per-identity long-term-memory graph store | `internal/arcadedb/client.go`, `internal/arcadedb/memory.go` |
| arcadedb-mcp | MCP server exposing memory (facts) + document retrieval as LLM-callable tools | `cmd/arcadedb-mcp/main.go` |
| AG-UI gateway | HTTP/SSE transport translating `agent.Event` to the AG-UI wire protocol | `internal/agui/server.go`, `internal/agui/translator.go` |
| Web cockpit host | Leaf package serving the embedded Vite SPA build with API-prefix fallback | `internal/webui/embed.go` |
| Telegram channel | Polling bot; per-turn `runner.Turn` driver, HITL, voice, artifacts | `internal/channels/telegram/bot_dispatch_turn.go` |
| Ingestion (Python) | CocoIndex reconciliation of a Garage bucket into ArcadeDB passages | `services/ingest/app.py` |

## Pattern Overview

**Overall:** Layered agent-runtime with a channel-agnostic orchestration core. A fresh `LlmAgent` is constructed per turn over rehydrated, ladder-managed history (no long-lived in-process agent state); durability lives entirely in Postgres stores, never in the agent struct. Tool execution is dispatched through an open, name-keyed `Registry` with a "deferred tool" indirection (Claude-Code-parity `tool_search`) so the LLM-visible manifest stays small.

**Key Characteristics:**
- **Open interface, no seal** — `agent.Agent` has no unexported method, deliberately diverging from google/adk-go so both `LlmAgent` and the swarm coordinator implement it directly (`internal/agent/agent.go:1-23`).
- **Shared-pointer Budget tree** — one `*atomic.Int32` step counter bounds an entire agent tree (root + swarm children), never per-branch (`internal/agent/budget.go:47-62`).
- **Fresh-agent-per-turn, durable-everything-else** — `runner.buildAgent` constructs a new `LlmAgent` every `Turn`/`TurnBranch` call seeded from `LoadManagedHistory`; nothing about a conversation survives in memory between turns (`internal/runner/runner.go:506-554`).
- **Deferred-tool manifest** — big tool specs (`Deferred: true`) are hidden from the default LLM-visible manifest and loaded on demand via the built-in `tool_search` hook, protecting the KV-cache prefix (`internal/agent/tools/spec.go:1-44`).
- **Sandbox-routed execution** — every filesystem/shell/file-delivery tool takes an optional `*usersandbox.SandboxRouter`; under a strict profile the box is the ONLY place code runs, never the host (`cmd/aura/main.go:187-280`).
- **Policy PEP as a single choke point** — `internal/gateway` decides every mutating tool call before `Execute`, independent of tool implementation (`internal/agent/llm_agent_dispatch.go` calls the gateway inside `execTool`, wired via `LlmAgentConfig.Gateway`).

## Layers

**Channel layer:**
- Purpose: Turn an inbound message (HTTP POST, Telegram update, CLI stdin, cron tick) into a call to `runner.Turn`/`TurnBranch`.
- Location: `internal/agui`, `internal/channels/telegram`, `cmd/aura/chat.go`, `internal/cron`.
- Contains: Protocol translation (AG-UI SSE, Telegram Bot API, REPL rendering), per-turn identity scoping, HITL resume plumbing.
- Depends on: `internal/runner`, `internal/conversations` (thread ownership checks), `internal/identityctx`.
- Used by: External clients (browser cockpit, Telegram users, operators, scheduled jobs).

**Orchestration layer:**
- Purpose: Own conversation persistence, per-thread locking, the context ladder, and fresh-agent construction.
- Location: `internal/runner`.
- Contains: `Runner`, `Deps` (composition-root inputs), turn lifecycle, pause/resume flush, auto-title worker.
- Depends on: `internal/agent`, `internal/conversations`, `internal/gateway`, `internal/askuser`.
- Used by: Every channel layer.

**Agent core:**
- Purpose: Drive one LLM round-trip loop with budget enforcement, tool dispatch, and event streaming.
- Location: `internal/agent`, `internal/agent/prompt`, `internal/agent/tools`, `internal/agent/mcptools`, `internal/agent/display`.
- Contains: `LlmAgent.Run`, `PromptBuilder`, `Registry`/`Tool`/`Spec`, hooks, verification-evidence gate, reasoning-tier classifier.
- Depends on: `internal/llm` (provider client), `internal/gateway`, `internal/sandbox/usersandbox`.
- Used by: `internal/runner`, `internal/swarm` (sub-agent fan-out), `internal/cron` (headless `agent_job`).

**Substrate layer:**
- Purpose: Own durable state — conversations, documents, memory, identity, sandbox lifecycle.
- Location: `internal/conversations`, `internal/documents`, `internal/arcadedb`, `internal/identity`, `internal/db`, `internal/objectstore`, `internal/sandbox/usersandbox`.
- Contains: sqlc-generated Postgres access, ArcadeDB HTTP/Cypher client, Garage/S3 object store client, Docker sandbox lifecycle.
- Depends on: Postgres (`aura.*` schema), ArcadeDB (one database per identity), Garage (S3-compatible), Docker daemon.
- Used by: Agent core (via tools), documents retrieval, `cmd/arcadedb-mcp`.

**Ingestion (external process, Python):**
- Purpose: Reconcile a per-identity Garage bucket into ArcadeDB passages/vectors.
- Location: `services/ingest` (CocoIndex app, not part of the Go module).
- Contains: `app.py` (lifespan + reconcile flow), `ingest/arcade.py` (schema + writes), `ingest/chunk.py`, `ingest/extract.py`.
- Depends on: CocoIndex's `amazon_s3` connector, ArcadeDB Bolt/Cypher, the embedding sidecar (`AURA_EMBED_BASE_URL`).
- Used by: Nothing in Go directly calls it — it is an autonomous reconciler; Go's `internal/documents`/`internal/arcadedb` only ever READ what it wrote.

## Data Flow

### 1. Inbound user turn → tool call → response (AG-UI path)

1. `POST /agent/run` decoded and validated; thread ownership checked (`internal/agui/server_run.go:22-51`).
2. Per-turn context blocks (attachments, doc catalog, pinned skill) are prepended to the model-facing copy of the user message, while the visible/persisted message stays the raw user text — `s.buildTurnUserMessage` (`internal/agui/server_run.go:66-86`, `internal/agui/server_context.go`).
3. `s.run.Turn(ctx, threadID, modelUserMsg)` → `internal/runner/runner.go:327-329` (`Runner.Turn`) → `runTurn` → per-thread lock (`lockForThread`) → `turnLocked` (`internal/runner/runner.go:365-483`).
4. `turnLocked` persists the new user turn (`appendUserTurn`), loads the memory digest (`loadMemoryContext`), builds `ContextConfig` (`contextConfig`), and calls `loadTurnHistory` → `conversations.Store.LoadManagedHistory` (context ladder, see flow 2).
5. `buildAgent` constructs a fresh `*agent.LlmAgent` + `InvocationContext` seeded from the ladder-managed history, a per-turn `Budget` from `AURA_LOOP_*` env, and the shared `Registry`/`Gateway`/`Breaker` (`internal/runner/runner.go:513-554`).
6. `LlmAgent.Run` loop (`internal/agent/llm_agent.go:198-560`): budget gate → `PromptBuilder.Build*` assembles the wire request (system prompt + history + tail-injected volatile hint) → `streamWithOpenRetry` calls the provider (`internal/llm`) → `consume` parses chunks/tool_calls.
7. On tool calls, `dispatch` (`internal/agent/llm_agent_dispatch.go:14-160`) partitions terminal (`text_response`) vs runnable calls, runs hooks (`BeforeTool`), dedup-checks via `Budget.BeforeToolCall`, then `executeBatch` runs tools concurrently. Each tool's `Execute` is gated by `internal/gateway` when mutating (policy PEP) before running.
8. Tool results are appended to `a.history` as `RoleTool` messages and streamed as `Event`s back through the iterator.
9. `Runner.turnLocked` persists each `Event` (`persistEvent`) and re-yields it to the AG-UI translator (`internal/agui/translator.go`), which turns it into AG-UI SSE frames streamed to the browser (`s.streamSSE`, `server_run.go:136`).

### 2. Context/history assembly with token budgeting (the "context ladder")

1. `Runner.contextConfig` builds `conversations.ContextConfig{ContextWindow, MaxOutputTokens, ToolEvictAfterTurns, HistoryHardCapTurns, AlwaysBlock, TransientContext, ProviderErrorReserveTokens, Summarizer}` — `internal/runner/runner_context.go:34-49`. `Summarizer` is non-nil only when `AURA_CONTEXT_COMPACTION_ENABLED` is set (`internal/runner/runner.go:86-90`, `243-248`).
2. `conversations.Store.LoadManagedHistory` fetches only the newest `HistoryHardCapTurns` rows (protected system head always retained) and calls `applyContextLadder` — `internal/conversations/context.go:150-160, 209-287`.
3. **L1 (tool micro-compact):** `applyL1` rewrites old `role='tool'` turns older than `ToolEvictAfterTurns` into a `read_tool_output(tool_call_id=...)` pointer, skipping the system turn and any `tool_search` result (which holds a still-needed schema) — `internal/conversations/context.go:325-355`.
4. **L2 (budget gate):** hard cap = `ContextWindow - max(MaxOutputTokens,20000) - 13000 - ProviderErrorReserveTokens` (floored to `ContextWindow/2` on small windows) — `internal/conversations/context.go:107-143`. Under cap → return the L1 result (zero rot). Over 75% of cap → WARN log only.
5. **L2.4 (LLM compaction, optional):** if `Summarizer` is set and still over cap, `tryCompact` condenses historical rounds into one synthetic summary turn, keeping the protected head and the active user-led round verbatim — `internal/conversations/context.go:256-262`, `internal/conversations/compaction.go`.
6. **L2.5 (deterministic hard-drop):** `dropOldestPairs` removes the oldest complete user/assistant rounds until under cap, writing exactly one `context_rot` event row for audit — `internal/conversations/context.go:264-286, 457-491`.
7. `ContextConfig.AlwaysBlock` (the messages[1] always-on skill block, rendered per turn from live skill-loader state) is injected as a PROTECTED turn immediately after the system head, counted toward budget but never evicted — `internal/conversations/context.go:220-223, 296-310`.
8. Inside the agent loop itself, a SEPARATE per-call volatile hint (`prompt.Budget`: used/remaining steps, workspace, current time, numbered web-source list, deferred-tool roster) is tail-injected to a COPY of history on every LLM call — never into the persisted/cached `messages[0]` — `internal/agent/prompt/builder.go:23-159`, `internal/agent/llm_agent.go:320-338`.
9. Token counting throughout the ladder uses a `tiktoken-go` `cl100k_base` encoder (`internal/conversations/tiktoken.go`), with `ProviderErrorReserveTokens` widening the local-llama.cpp headroom to cover Aura's tokenizer-estimate gap versus the real local tokenizer (`internal/llm/capabilities.go`).

### 3. Document ingestion path

1. A file is uploaded via a channel (Telegram attachment, AG-UI file upload) or `aura docs` CLI; it lands in `internal/objectstore` (Garage/S3-compatible bucket, one bucket per identity).
2. `documents.Service.IngestPath` (or the asset-upload equivalent) validates size/type, computes a content hash, mints a deterministic `documentID` (`SearchDocumentID`), creates a `JobStore` row, and writes a non-visible catalog `Document` row via `IngestCatalog.CreateDocument` — `internal/documents/service.go:57-145`.
3. `writeCard` reads the file once (`internal/documents/filecard`) to build a structural CARD (title/shape description) and stores it via `Catalog.SetCard` — this card is what `document_search` ranks on BEFORE any deep extraction has happened — `internal/documents/service.go:147-166`.
4. Independently, the Python **CocoIndex** app in `services/ingest` (`app.py`) reconciles each identity's Garage bucket on an interval (`AURA_INGEST_INTERVAL_SEC`, wrapped by `coco.auto_refresh` since the `amazon_s3` connector has no native live/watch mode): it extracts text (`ingest/extract.py`), chunks it (`ingest/chunk.py`), embeds it (`AURA_EMBED_BASE_URL`), and writes `IndexedDocument`/`Passage` records directly into ArcadeDB via Bolt/Cypher (`ingest/arcade.py`) — into the SAME per-identity `mem_<identity_uuid>` database the Go retriever reads (`services/ingest/app.py:1-45`).
5. The agent's `document_search` tool (`internal/agent/tools/document_search.go`) calls `documents.HostRetriever.Retrieve` (`internal/documents/retrieval.go:235-288`), which fuses three legs: the Postgres/ArcadeDB **card** (`RetrievalControlPlane.RouteDocumentCards`), the **lexical** leg, and the **dense** leg (`RetrievalProjection.FusedCandidates` over `arcadedb.PassageCandidate`) — degrading gracefully (card-only) when the embedder or ArcadeDB projection is unavailable.
6. When the model needs the full document rather than a ranked passage (e.g. "how many rows"), `document_open` (`internal/agent/tools/document_open.go`) resolves the catalog id back to the object-store key and streams the ORIGINAL file into the caller's sandbox box `/workspace/documents/`.

### 4. Memory write/recall path

1. **Recall (host-driven, pre-turn):** `Runner.loadMemoryContext` (`internal/runner/runner_context.go:55-84`) calls `MemoryContextProvider.Context` for an always-on query-less digest and, when `AURA_MEMORY_PRELOAD_ENABLED`, `.Search` for a per-message relevance recall. Both are implemented by `mountedMemoryContext` (`cmd/aura/serve_memory_context.go:18-59`), a thin adapter over the `memory` MCP host client that calls the `memory_digest`/`memory_search` MCP tools directly (not through the LLM) with a 2s bound, fail-soft on any error.
2. The digest/recall text is wrapped in `<memory_context>`/`<memory_recall>` blocks and passed as `ContextConfig.TransientContext`, inserted immediately before the current user turn by the ladder (`injectTransientContext`), never persisted to `conversation_turns`.
3. **Recall (LLM-driven, in-turn):** the agent can also directly call `memory_search`/`memory_facts_about`/`memory_digest`/`memory_entities` as ordinary MCP tools once `cmd/arcadedb-mcp` is mounted as a managed MCP server (`buildRegistryWithMCP`, `cmd/aura/main.go:298-392`, using `mcptools.MountManagedServerHostWithEgress`). The `memory-aura` skill (`internal/skills/embed/memory-aura/SKILL.md`) documents when/how to call these.
4. **Write:** the model calls `memory_upsert_fact` (an MCP tool, never a host-driven background job) with `subject/predicate/object/statement` + mandatory `source{run_id,memory_ids}` provenance; the handler resolves the caller's tenant client (`tenants.For`) and calls `arcadedb.Client.UpsertFact` (`cmd/arcadedb-mcp/tool_memory.go:48-103`, `internal/arcadedb/memory.go`).
5. `UpsertFact` stores the fact as a bitemporal `FACT` edge between `Entity` vertices in the caller's OWN per-identity ArcadeDB database (`mem_<identity_uuid>`, HMAC-derived tenant credential — server-enforced isolation, not a `WHERE` clause). A `supersedes:true` write closes the previous fact's `valid_to` window rather than deleting it (`internal/arcadedb/memory.go:12-32, 87-104`).
6. Retrieval inside the MCP server fuses a Lucene full-text leg (`EnglishAnalyzer` FULL_TEXT index on `FACT.statement`) with a vector leg (EmbeddingGemma) natively inside ArcadeDB — no Go-side reranking (`internal/arcadedb/memory.go:16, 54-59`).

## Key Abstractions

**Agent (interface):**
- Purpose: The single open contract every runnable agent implements — leaf `LlmAgent`, workflow/swarm coordinators.
- Examples: `internal/agent/agent.go`, `internal/agent/llm_agent.go` (implements it), `internal/swarm` (fan-out coordinator).
- Pattern: `Run(InvocationContext) iter.Seq2[*Event, error]`; termination is ALWAYS a non-error `Event` (budget trips, pauses), never the error slot, which is reserved for real infra failures.

**Budget:**
- Purpose: The single resource-exhaustion control for an entire agent tree (steps, wallclock, dedup).
- Examples: `internal/agent/budget.go`, `internal/agent/budget_dedup.go`.
- Pattern: A shared `*atomic.Int32` counter by pointer across the whole tree (`Child` forks a distinct dedup ring but the SAME counter), so a fan-out swarm cannot multiply the total step budget.

**Event:**
- Purpose: The single wire/runtime type every agent- and tool-emitted signal flows through (chunks, tool start/result, pause, final answer, discard-streamed repudiation).
- Examples: `internal/agent/event.go`.
- Pattern: One struct with an `Actions` union of optional pointer fields (`AwaitingInput`, `Display`, `ToolInvocation`) so new signal types are additive and `omitempty` on the wire.

**Tool / Spec / Registry:**
- Purpose: The dispatch contract between the agent loop and every capability (built-in, MCP-bridged, or sandbox-routed).
- Examples: `internal/agent/tools/spec.go`, 46 built-ins under `internal/agent/tools/*.go`.
- Pattern: `Spec.Deferred=true` hides a tool's full schema from the default manifest until `tool_search` loads it (protects the KV-cache prefix); `Registry` is immutable per run, built once at boot via `buildBaseRegistryWithHandles` (`cmd/aura/main.go:193-296`).

**Context ladder (L1/L2/L2.4/L2.5):**
- Purpose: Deterministic, pure-function history reduction bounding the wire request to the model's context window.
- Examples: `internal/conversations/context.go`, `internal/conversations/compaction.go`.
- Pattern: Ordered, side-effect-free stages except the single audited `context_rot` event write at L2.5; LLM compaction (L2.4) is opt-in and always falls back to the zero-LLM L2.5 drop on any summarizer failure.

**InvocationContext:**
- Purpose: The single-Run-scoped value carrying ctx/budget/agent down the tree, copy-on-write only.
- Examples: `internal/agent/agent.go:64-91`.
- Pattern: `WithContext`/`WithSubAgent` always return a COPY; never stored on a long-lived struct.

## Entry Points

**`cmd/aura` (main binary):**
- Location: `cmd/aura/main.go`.
- Mechanism: **A hand-rolled top-level switch on `os.Args[1]`, explicitly NOT cobra** (`cmd/aura/identity.go:1-10` documents this as a recorded deviation from an earlier assumption — go.mod carries `spf13/cobra` only for a FEW nested subcommand trees: `identity`, `paused-states`, `recover-operator`, `skills`). Verbs: `tools|mcp|memory|agent|swarm-demo|web|doctor|db|objectstore|docs|identity|paused-states|task|retention|skills|chat|cache-stats|cache-audit|config|version|serve|shell|toolpipe`.
- Responsibilities: Every subcommand builds its own composition root (`buildRegistry`/`buildBaseRegistryWithHandles`/`buildRegistryWithMCP`) — there is no single shared "app" struct; `serve` and `chat` share `bootChatEnv`.

**`aura serve` (daemon):**
- Location: `cmd/aura/serve.go`.
- Triggers: Process start; runs until SIGINT/SIGTERM.
- Responsibilities: Boots the shared chat composition root, mounts the AG-UI HTTP/SSE gateway, the cron `Scheduler` tick loop, the Telegram channel registry (fail-soft), and the loopback setup-wizard server; graceful shutdown drains in-flight turns before reverse-closing MCP servers and the pool.

**`aura chat` (REPL):**
- Location: `cmd/aura/chat.go`.
- Triggers: Interactive CLI invocation.
- Responsibilities: Same composition root as `serve` (`bootChatEnv`), drives `runner.Runner` directly with a local REPL renderer; **also hand-rolled, not cobra**.

**`cmd/arcadedb-mcp` (MCP server binary):**
- Location: `cmd/arcadedb-mcp/main.go`.
- Triggers: Spawned as a subprocess by `aura serve`/`aura chat`'s MCP mount step (`mcptools.MountManagedServerHostWithEgress`), or run standalone for other MCP-speaking clients.
- Responsibilities: Exposes the ArcadeDB-backed memory graph (`memory_upsert_fact`, `memory_search`, `memory_facts_about`, `memory_digest`, `memory_entities`, `memory_forget`) and document-retrieval/graph-schema tools as MCP tools; owns per-identity tenant credential derivation (`cmd/arcadedb-mcp/tenant.go`).

**`POST /agent/run` (AG-UI gateway):**
- Location: `internal/agui/server_run.go`.
- Triggers: The web cockpit SSE client.
- Responsibilities: Decode+validate the AG-UI run request, resolve/own-scope the thread, drive `runner.Turn`, translate `agent.Event`s to AG-UI SSE frames.

**Telegram poller:**
- Location: `internal/channels/telegram/bot_dispatch_turn.go`.
- Triggers: An inbound Telegram update.
- Responsibilities: Resolve the linked Aura identity (fail-closed if unresolved), spawn the turn off the poller goroutine, drive `runner.Turn` via the shared `agui_subscriber.go` fanout seam, render the response back through Telegram (Markdown-v2, HITL inline keyboards, TTS).

## Architectural Constraints

- **Threading:** Go's standard goroutine-per-request model; the agent loop itself is single-goroutine per turn, but `dispatch` runs multiple RUNNABLE tool calls concurrently within one turn (`internal/agent/llm_agent_dispatch.go:91-152`) while keeping the terminal/history-append phase strictly serial and in original call order (KV-cache and audit-order invariant).
- **Global state:** Deliberately minimal. The Runner holds two per-process `sync.Map`s keyed by a composite `(identity, session)` key (`threadLocks`, `sessions` — `internal/runner/runner.go:204-219`) for per-conversation serialization and in-flight-turn cancellation; the LLM circuit breaker (`llm.Breaker`) and the reasoning classifier are process-lifetime singletons injected into every per-turn agent, never rebuilt per turn.
- **No long-lived agent state:** `LlmAgent` is constructed fresh every turn from rehydrated history — there is no persistent in-memory conversation object; a crash mid-turn loses nothing durable because `Runner` persists each `Event` as it streams (`internal/runner/runner.go:455-467`).
- **Tenant isolation is server-enforced, not query-scoped:** ArcadeDB isolation is one database per identity with an HMAC-derived credential, not a `WHERE identity_id = ?` filter that code could forget (`internal/arcadedb/tenant.go`; see CLAUDE.md §Persistence).
- **Sandbox routing:** Under a strict deployment profile, every filesystem/shell/file-delivery tool executes inside a per-identity Docker box (`internal/sandbox/usersandbox`) via a `SandboxRouter`; `web_fetch`/`web_search` are deliberately NEVER routed (they stay host-side, already SSRF-guarded) — a scope violation to route them.

## Anti-Patterns

### Reintroducing a fresh long-lived agent object per conversation

**What happens:** A naive extension would try to cache an `*agent.LlmAgent` keyed by conversation id to "save" reconstruction cost.
**Why it's wrong:** `LlmAgent` carries per-run mutable fields (`activated`, `recoveryAttempts`, `sideEffected`, `editedPaths`) that are explicitly documented as per-run only (`internal/agent/llm_agent.go:82-142`); caching one across turns would leak recovery/verification state and silently change dedup/activation behavior across unrelated turns.
**Do this instead:** Always build a fresh `LlmAgent` per turn via `Runner.buildAgent`, seeded from `LoadManagedHistory`; durability lives in the Stores, never the struct.

### Writing directly into `messages[0]` (the system prompt) for anything volatile

**What happens:** Adding a per-turn-varying fact (current time, budget counters, web sources) directly into the system message string.
**Why it's wrong:** `messages[0]` must stay byte-stable to preserve the provider's prompt cache (`internal/agent/prompt/builder.go:103-114`); poisoning it busts the cached prefix on every turn and was measured to cost real latency (see `internal/agent/prompt/builder.go:47-51` on the deferred-tools roster placement).
**Do this instead:** Append volatile content to a COPY of history as a trailing tail-injected message (`PromptBuilder.buildBase`), never mutate `messages[0]`.

### Bypassing the context ladder by loading raw unbounded history

**What happens:** A new read path that calls a lower-level store method (e.g. a raw turn fetch) instead of `Store.LoadManagedHistory`/`LoadManagedHistoryForBranch`.
**Why it's wrong:** Skips L1 tool-eviction, the L2 hard-cap gate, and the L2.5 audited drop — a long conversation would silently overflow the model's context window with no `context_rot` audit trail.
**Do this instead:** Every history read for an LLM call goes through `conversations.Store.LoadManagedHistory*`.

## Error Handling

**Strategy:** Real infrastructure failures (LLM transport errors, dispatch panics) flow through the `iter.Seq2[*Event, error]` error slot; every OTHER termination (budget exhaustion, dedup trip, pause, empty-response, breaker-open) is an explicit non-error `Event` with `Actions.Escalate`/`AwaitingInput` set (D-04 in `internal/agent/llm_agent.go` comments) — the error slot is reserved, never overloaded for control flow.

**Patterns:**
- A recovery counter (`recoveryAttempts`, max 1) lets the FIRST budget/dedup trip inject one nudge-and-continue turn before a second trip routes straight to `finalize()` — never an infinite retry loop.
- `maybeRecoverEmptyResponse` retries once on a genuinely empty LLM completion (observed live provider hiccup) before finalizing.
- A terminal `text_response` combined with any other tool call in the same step is rejected wholesale and replanned (`internal/agent/llm_agent_dispatch.go:34-53`) rather than silently picking one.
- MCP server mount failures are fail-soft (WARN + skip that server) at boot, never fatal to the whole registry, as long as at least one non-deferred capability tool remains registered (`Registry.Validate`, `internal/agent/tools/spec.go:198-212`).

## Cross-Cutting Concerns

**Logging:** `log/slog` structured logging throughout; `internal/obs` centralizes process observability bootstrap (OTel spans, metrics). Sensitive values pass through `internal/redact` before reaching logs.

**Validation:** Config is fail-fast on malformed (but not absent) `AURA_LOOP_*`/`AURA_CONTEXT_*` env values (`errMalformed` pattern in `internal/agent/budget.go`); AG-UI request bodies are strictly decoded and size-capped (`internal/agui/strict_decode.go`).

**Authentication/Authorization:** `internal/webauth` (Authula-based) gates the AG-UI cockpit; every Telegram turn is scoped to a resolved linked identity or DROPPED (fail-closed, `internal/channels/telegram/bot_dispatch_turn.go:82-141`); `internal/gateway` is the single policy PEP for mutating tool calls; ArcadeDB tenant isolation is server-enforced per identity (`internal/arcadedb/tenant.go`).

---

*Architecture analysis: 2026-08-13*
