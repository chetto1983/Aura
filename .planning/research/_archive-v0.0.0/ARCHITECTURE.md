# Architecture Research

**Domain:** Go-native agentic AI substrate (personal AI runtime — tool execution + persistent memory + skills + multi-channel transport)
**Researched:** 2026-05-29
**Confidence:** HIGH (Slice 0.9 / 1 / 4 / 8 / 11 patterns are converged industry consensus 2026; transport + memory tiers verified across 4+ reference systems; the deferred-tool + stable-prefix discipline is the precise pattern Anthropic shipped Nov 2025 and DeepSeek/OpenRouter inherit)

---

## TL;DR

The 2025–2026 Go agentic substrate has converged on a **5-layer architecture**:

1. **Client (channels)** — pluggable transports (CLI, Telegram, SSE/WS) consuming a single agent event stream
2. **Transport gateway** — AG-UI protocol (~17 event types) as the lingua franca between agent runtime and any UI
3. **Runtime** — single `Agent` interface (ADK-go shape) yielding `iter.Seq2[*Event, error]`, with workflow agents (Sequential/Loop/Parallel) as built-in composition primitives
4. **Capabilities** — tools (deferred-loaded), sandbox (Docker sidecar), swarm coordinator (depth-bounded), memory ingest/retrieval, skills (instruction + executable), KV cache builder, web tools, scheduler
5. **Persistence** — semantic-strict three-store model (Postgres for application state, Neo4j for knowledge + vectors, filesystem for operational artifacts)

The PRD's choices map 1:1 onto this consensus. The decisions that deserve special validation — and largely hold up — are: ADK-go pattern theft (✓ correct; Microsoft Agent Framework + OpenAI SDK + LangGraph all converge on the same primitives), AG-UI as transport (✓ correct; 8+ first-party integrations as of 2026), deferred-tool pattern (✓ correct; Anthropic shipped `advanced-tool-use-2025-11-20` beta with `defer_loading` for exactly the cache-bloat reason Aura cites), KV-cache builder living in the prompt assembly stage not the wire client (✓ correct; this is the only correct place — stable-prefix invariants must precede any provider-specific cache_control injection), and Neo4j as single store for graph+vector+fulltext (✓ correct; GraphRAG canonical implementation).

The decisions that warrant call-outs as architectural risk are documented under "Coupling concerns" below.

---

## Standard Architecture

### System Overview

```
┌──────────────────────────────────────────────────────────────────────────────┐
│  CLIENT LAYER  — channels (in-process + remote)                              │
│  ┌──────────┐  ┌──────────────┐  ┌─────────────────┐  ┌─────────────────┐   │
│  │ CLI      │  │ Telegram bot │  │ AG-UI SSE       │  │ future channels │   │
│  │ (debug)  │  │ (long poll)  │  │ (HTTP, port     │  │ (web/voice/MCP) │   │
│  │          │  │              │  │  9080)          │  │                 │   │
│  └────┬─────┘  └──────┬───────┘  └────────┬────────┘  └────────┬────────┘   │
│       │ in-process    │ in-process        │ HTTP/SSE           │            │
└───────┼───────────────┼───────────────────┼────────────────────┼────────────┘
        ▼               ▼                   ▼                    ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│  TRANSPORT GATEWAY  — AG-UI translator + fanout                              │
│   internal/agui/                                                             │
│   translator:  iter.Seq2[*agent.Event, error] → iter.Seq2[agui.Event, error] │
│   fanout:      one Event stream → N in-process subscribers (Telegram, CLI)   │
│   server:      HTTP POST /agent/run (SSE) + GET /threads/<id>/messages       │
│   ~17 AG-UI event types: RUN_*, TEXT_MESSAGE_*, TOOL_CALL_*, STATE_*,        │
│   REASONING_*, STEP_*, CUSTOM, INTERRUPT                                     │
└──────────────────────────────┬───────────────────────────────────────────────┘
                               ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│  RUNTIME LAYER  — Agent interface (Slice 0.9)                                │
│                                                                              │
│   type Agent interface {                                                     │
│     Name() string                                                            │
│     Description() string                                                     │
│     Run(InvocationContext) iter.Seq2[*Event, error]    // Go 1.23 range-over │
│     SubAgents() []Agent                                                      │
│     FindAgent(name string) Agent                                             │
│   }                                                                          │
│                                                                              │
│   InvocationContext: { Ctx, Agent (self), SessionID, IdentityID, Branch,     │
│     SessionStore (PG), GraphStore (Neo4j MCP), LLMClient (or LLMRouter) }    │
│                                                                              │
│   Termination: Event{Actions.Escalate=true} bubbles up via `return` after    │
│   every `yield` check. No magic — pure range-over-func.                      │
│                                                                              │
│  ┌────────────────┐  ┌──────────────────┐  ┌──────────────────────────────┐  │
│  │ LlmAgent       │  │ Workflow agents  │  │ Domain agents (cross-slice)  │  │
│  │ (Slice 1)      │  │ (Slice 0.9)      │  │                              │  │
│  │ stream LLM +   │  │ Sequential       │  │ ReminderAgent      (Slice 6) │  │
│  │ tool dispatch  │  │ Loop (maxIter)   │  │ AgentJobAgent      (Slice 6) │  │
│  │ ToolResult     │  │ Parallel (errgr  │  │ InterviewStepAgent (Sl 10)   │  │
│  │ MaxSteps=8     │  │  + ackChan)      │  │ Skill virtual agent (Sl 7)   │  │
│  │ pause/resume   │  │                  │  │ Swarm worker = LlmAgent (3)  │  │
│  └────────────────┘  └──────────────────┘  └──────────────────────────────┘  │
└─────┬────────────────────────────────────────────────────────────────────────┘
      ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│  CAPABILITIES LAYER  — domain capabilities as tools and/or Agent impls       │
│                                                                              │
│  Tools (registry, deferred-tool pattern; manifest = name + 1-line summary;   │
│  tool_search loads full spec on demand to protect prompt cache)              │
│                                                                              │
│  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌─────────────┐ │
│  │ Sandbox    │ │ Swarm      │ │ Web        │ │ Scheduler  │ │ Skills      │ │
│  │ Slice 2a/b │ │ Slice 3    │ │ Slice 5    │ │ Slice 6    │ │ Slice 7a-e  │ │
│  │ Docker     │ │ Parallel   │ │ SearXNG +  │ │ cron +     │ │ SKILL.md +  │ │
│  │ sidecar +  │ │ + bus +    │ │ web_fetch  │ │ Risk-Based │ │ snippet 7e  │ │
│  │ seccomp +  │ │ DM-by-ID + │ │ + SSRF     │ │ + Action   │ │ + TTL 90d   │ │
│  │ workspace  │ │ MaxDepth=3 │ │ defense    │ │ Router     │ │ archived    │ │
│  └────────────┘ └────────────┘ └────────────┘ └────────────┘ └─────────────┘ │
│  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐                 │
│  │ Memory     │ │ KV cache   │ │ Onboarding │ │ Identity   │                 │
│  │ Slice 11   │ │ Slice 4    │ │ Slice 10   │ │ Slice 1.7  │                 │
│  │ ingest +   │ │ Prompt-    │ │ LoopAgent[ │ │ identities │                 │
│  │ retrieval  │ │ Builder +  │ │ Interview  │ │ +          │                 │
│  │ + agent    │ │ stable-    │ │ Step]      │ │ capability_│                 │
│  │ journal    │ │ prefix +   │ │ + Agent.md │ │ grants     │                 │
│  │ (mem0 +    │ │ provider-  │ │ injector   │ │ (wildcard  │                 │
│  │ GraphRAG)  │ │ aware      │ │            │ │  '*' OK)   │                 │
│  └────────────┘ └────────────┘ └────────────┘ └────────────┘                 │
│                                                                              │
│  Pause/Resume primitive (Slice 1.5): ask_user returns sentinel               │
│  ErrAwaitingUserInput → LlmAgent emits Event{Escalate} + PausedState row.    │
│  Multi-pause FIFO (priority DESC, created_at ASC).                           │
└─────┬────────────────────────────────────────────────────────────────────────┘
      ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│  PERSISTENCE LAYER  — three stores, semantic-strict (no crossover)           │
│                                                                              │
│  ┌──────────────────┐ ┌────────────────────┐ ┌──────────────────────────┐    │
│  │ PostgreSQL 17    │ │ Neo4j 5 Community  │ │ Filesystem               │    │
│  │ (port 5432)      │ │ via mcp-neo4j-     │ │                          │    │
│  │ sqlc + pgx +     │ │ cypher MCP stdio   │ │ ~/.aura/skills/active/   │    │
│  │ golang-migrate   │ │ APOC + GDS + HNSW  │ │ ~/.aura/agents/<id>/     │    │
│  │ schema `aura.*`  │ │ vector 768d cosine │ │ $AURA_RUN_DIR/           │    │
│  │                  │ │                    │ │   conversations/<id>/    │    │
│  │ APPLICATION      │ │ KNOWLEDGE +        │ │     <tool_call>.result   │    │
│  │ STATE ONLY:      │ │ VECTORS ONLY:      │ │     <seq>.content        │    │
│  │  conversations   │ │  :Document         │ │     workspace/  (2b)     │    │
│  │  conversation_   │ │  :Chunk(emb 768)   │ │   tmp/  (24h TTL)        │    │
│  │    turns         │ │  :Entity           │ │                          │    │
│  │  paused_states   │ │  :Community        │ │ OPERATIONAL ARTIFACTS    │    │
│  │  identities      │ │  :AgentEpisode     │ │ ONLY. No knowledge.      │    │
│  │  capability_     │ │  :AgentInsight     │ │                          │    │
│  │    grants        │ │  :UserConversation │ │                          │    │
│  │  scheduler_tasks │ │  :UserSnippet      │ │                          │    │
│  │  skill_audit     │ │                    │ │                          │    │
│  │  knowledge_      │ │ Cypher migrations  │ │                          │    │
│  │    migrations    │ │ in internal/       │ │                          │    │
│  │  telegram_*      │ │ knowledge/         │ │                          │    │
│  │  sandbox_        │ │ migrations/*.cypher│ │                          │    │
│  │    sessions      │ │ audit row in PG    │ │                          │    │
│  │  ingest_audit    │ │ aura.knowledge_    │ │                          │    │
│  │  profile_audit   │ │ migrations         │ │                          │    │
│  └──────────────────┘ └────────────────────┘ └──────────────────────────┘    │
│  ┌────────────────────────────────────────────────────────────────────────┐  │
│  │ Sidecars (docker compose, lifecycle-coupled to `aura serve`):          │  │
│  │  aura-postgres         (Postgres 17, port 5432)                        │  │
│  │  aura-neo4j            (Neo4j 5 community + APOC + GDS, 7687 bolt)     │  │
│  │  aura-llama-embed      (embeddinggemma-300m, 768d, port 8081)          │  │
│  │  aura-sandbox          (python:3.12-slim + bash sidecar, port 18901)   │  │
│  │  searxng               (Slice 5 web search)                            │  │
│  │  aura-llama-multimodal (Gemma 4 E4B vision + STT, port 8082)           │  │
│  │  markitdown            (document → markdown, Slice 9c)                 │  │
│  │  mcp-neo4j-cypher      (stdio subprocess, NOT compose — child of Go)   │  │
│  └────────────────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

| Component | Responsibility | Typical Implementation |
|-----------|----------------|------------------------|
| Channel (e.g. Telegram, CLI, AG-UI HTTP) | Adapt user transport ↔ Agent event stream. One file/package per channel, never reaches into agent internals. | `Channel` interface in `internal/channels/`: `Name()`, `Start(ctx, subscriber)`, `Stop()`, `IsHealthy()`. AG-UI HTTP server is itself the canonical transport — other channels subscribe to its fanout. |
| AG-UI Translator | Pure 1:N mapping from `*agent.Event` to AG-UI events (RUN_*, TEXT_MESSAGE_*, TOOL_CALL_*, STATE_*, REASONING_*). Deterministic, no mutable state cross-event except ID generation. | `Translate(seq iter.Seq2[*agent.Event, error]) iter.Seq2[events.Event, error]` — single pure function consuming the runtime stream directly via range-over-func. |
| Fanout | Distribute one Event stream to N concurrent in-process subscribers (Telegram, web). | `Fanout` struct wrapping a source `iter.Seq2`, broadcasting to N subscriber channels with bounded buffers (cap 64) and drop-on-slow-consumer semantics. |
| `Agent` interface | The unified contract every "thing that runs and emits events" implements: LlmAgent, workflow agents, scheduler handlers, onboarding steps, skill virtual agents, swarm workers. | Go 1.23 `iter.Seq2[*Event, error]` from `Run()`. Pattern stolen from `google/adk-go` v1.3.0 — verbatim shape, zero deps imported (Aura reimplements ~380 LOC). |
| `LlmAgent` | LLM-driven agent: prompt build → stream → tool-call accumulator → ToolResult preview-or-spill → loop with MaxSteps=8. Owns conversation-state writes via `conversations.Store`. | Replaces today's `Loop` from `internal/agent/loop.go`. Lives at `internal/agent/llm_agent.go` (~480 LOC; split into `_pause.go`/`_history.go` if it crosses 600 LOC). |
| Workflow agents | `SequentialAgent`, `LoopAgent(maxIter)`, `ParallelAgent` (errgroup + ackChan synchronous backpressure). Built-in composition primitives, reused by swarm, onboarding, scheduler. | Three files under `internal/agent/workflow/`: `sequential.go` (~30 LOC), `loop.go` (~40 LOC), `parallel.go` (~70 LOC). Escalation propagates via `return` after `yield` check. |
| `Event` + `Actions` | Streaming unit + termination + state mutation signals. Fields: `Author`, `Branch`, `LLMResponse`, `Actions{Escalate, StateDelta, ArtifactDelta}`, `Timestamp`. | `internal/agent/event.go`. Shape ported verbatim from ADK-go session.Event. |
| Tool registry | Map of LLM-facing capabilities. Manifest sorted alphabetically (cache stability). Deferred tools show only `name + 1-line summary`; full spec loaded on demand via built-in `tool_search` hook tool. | `internal/agent/tools/spec.go` (Tool interface) + `manifest.go` (alphabetical sort, deferred-aware rendering). Pattern matches Anthropic `advanced-tool-use-2025-11-20` beta. |
| `ToolResult` | Preview + persist sidecar: results > `AURA_CONTEXT_PREVIEW_CAP_BYTES` (default 2048) spill to `$AURA_RUN_DIR/conversations/<conv_id>/<tool_call_id>.result`. History keeps preview + footer pointer. `read_tool_output(tool_call_id, offset?, limit?)` fetches ranges. | `internal/agent/tools/result.go` (~60 LOC). The single most important context-protection primitive in the substrate. |
| `ask_user` + `PausedState` | Pause/resume primitive. Tool returns sentinel `ErrAwaitingUserInput{Question, Options, Kind, Priority, ToolCallID, ResumeContext}`. Loop intercepts, persists `PausedState` to `aura.paused_states`, emits `Event{Escalate=true}`. Multi-pause FIFO. | `internal/agent/tools/ask_user.go` + `internal/agent/pending.go` + table `aura.paused_states`. Coexistence with `text_response` in same turn is forbidden by Loop. |
| PromptBuilder (Slice 4) | KV-cache stable-prefix assembly. `messages[0]` byte-identical turn-on-turn. Provider-aware: injects Anthropic `cache_control: ephemeral` on system + tools block; no-op for DeepSeek auto-cache; parses `usage.prompt_cache_hit_tokens` for measurement. | `internal/llm/prompt/builder.go` + `cache_anthropic.go` + `cache_deepseek.go` + `cache_metrics.go`. Lives in the prompt assembly stage, NOT in the wire client. |
| LLM Client (openai-compat) | Provider-neutral SSE streaming. Parses tool-call deltas, accumulates partial JSON args, emits chunks. Returns `HTTPError{StatusCode, RetryAfterSec, Body}` — no retry at wire level. | `internal/llm/openai_compat/client.go` (~280 LOC). DeepSeek-V4 via OpenRouter is the default; Anthropic direct + future vLLM via same interface. |
| Sandbox runner | Docker sidecar HTTP client. Slice 2a: stateless per-call (snippet untrusted, OpenAI Code Interpreter MVP pattern). Slice 2b: session-bound containers per `conversation_id` with workspace mount + network allowlist + TTL (`AURA_SANDBOX_SESSION_TTL_SEC=1800`). | `internal/sandbox/docker.go` (~220 LOC, 2a) + `sessions.go` + `workspace.go` + `network.go` (2b). Sandbox sidecar materials at `sandbox/` repo root: Dockerfile, compose, seccomp.json, sidecar.py (stdlib http.server). |
| Swarm Coordinator | Spawn/Talk/Join, tier model (chat/reasoning/worker), shared message bus + DM-by-ID. `MAX_SPAWN_DEPTH=3` hard cap. Reuses `ParallelAgent` from Slice 0.9; worker = `LlmAgent` instance. | `internal/swarm/coordinator.go` + `bus.go` + `tier.go`. Test: wall-clock parallelism < 1.5× single-worker (race detector enforced). |
| Conversations Store | sqlc adapter for multi-thread Claude.ai-style persistence. Per-row `aura.conversation_turns`. Auto-title (LLM-generated background goroutine, best-effort). Microcompact L1 (tool turn eviction). Budget L2 (Claude Code style hard cap). | `internal/conversations/store.go` (~120 LOC) + `title.go` + `microcompact.go` + `budget.go` + `sidecar.go` + `cleanup.go`. Pattern derived from LangGraph PostgresSaver. |
| Memory subsystem | Cognify 6-stage ingest (classify → permissions → chunk → entities → summary → embed). Hybrid retrieval (BM25 + HNSW + 1-hop graph + LLM re-rank). Community detection (Leiden via GDS). Agent journal (`:AgentEpisode` + `:AgentInsight`). | `internal/memory/{ingest,graph,retrieval,agent}/`. Mem0 2-fase entity dedup + GraphRAG community summarization + Letta-style 3-tier (Core/Recall/Archival). |
| Knowledge MCP client | MCP-neo4j-cypher stdio subprocess wrapper. `Cypher(ctx, query, params) → rows`. All Neo4j access (read AND write) goes through here — no native Go driver. | `internal/knowledge/client.go` (~80 LOC). The LLM owns the graph interface as a tool surface; Go code uses the same MCP shim. |
| Skills system | Loader (FS scan multi-root, TTL cache 1s, YAML frontmatter parser). Validator + SanitizeName (single chokepoint). Writer (atomic pending→active). Installer (`npx skills add --ignore-scripts`). Snippet (executable code, multi-lang via sandbox 2b). | `internal/skills/loader/{filesystem,parser,cache,loader}.go` + `validator.go` + `paths.go` + `writer.go` + `deleter.go` + `installer.go` + `snippet.go`. All mutations through Postgres `aura.skill_audit` (append-only trigger). |
| Scheduler | Tick loop (default 5s), `Queries.DueTasks` with `FOR UPDATE SKIP LOCKED`, dispatch to `agent.Agent` handlers (one file per `TaskKind`). Risk-Based pre-fire gating. | `internal/cron/scheduler.go` + `handlers/<kind>.go`. Each TaskKind = one `agent.Agent` impl. |
| Identity / capability grants | `aura.identities` + `aura.capability_grants`. `HasCapability(id, cap) bool` with `*` wildcard. Scaffolding for multi-user — single `local` user in v1. | `internal/identity/store.go` + `capability.go`. Capability check at tool dispatch level, not Agent level (see "Coupling concerns"). |

---

## Recommended Project Structure

The PRD's planned layout (per `.planning/codebase/STRUCTURE.md` target state) is the right one. Reproducing the consensus-validated form below with rationale annotations:

```
Aura/
├── cmd/aura/                          # Single binary entry point
│   ├── main.go                        # Cobra subcommand router only
│   ├── chat.go                        # `aura chat {list|resume|new|archive|delete|rename}`
│   └── paused_states.go               # `aura paused-states {list|purge}` escape hatch
├── compose.yaml                       # Docker compose at root (sidecars)
├── sandbox/                           # Sandbox SIDECAR materials (NOT internal/sandbox)
│   ├── Dockerfile                     # python:3.12-slim + bash + non-root
│   ├── compose.yaml                   # grows per slice
│   ├── seccomp.json                   # default-deny + allow list
│   └── sidecar.py                     # stdlib http.server, /exec/python /exec/shell /session/{id}
├── internal/                          # Go convention: not importable outside module
│   ├── agent/                         # RUNTIME LAYER — the Agent interface lives here
│   │   ├── agent.go                   # Agent + InvocationContext + builder helpers
│   │   ├── event.go                   # Event + Actions + LLMResponse
│   │   ├── llm_agent.go               # LlmAgent implementing Agent (Slice 1)
│   │   ├── llm_agent_pause.go         # split if llm_agent.go > 600 LOC
│   │   ├── llm_agent_history.go       # split if needed
│   │   ├── pending.go                 # PausedState type + sqlc adapter
│   │   ├── tools/                     # LLM-facing tool registry
│   │   │   ├── spec.go                # Tool interface + Registry
│   │   │   ├── manifest.go            # alphabetical sort + deferred-aware render
│   │   │   ├── action.go              # ActionRouter (Slice 6+, shared by cron + skills)
│   │   │   ├── result.go              # ToolResult preview+spill
│   │   │   ├── text_response.go       # terminal tool
│   │   │   ├── search.go              # tool_search built-in hook
│   │   │   ├── ask_user.go            # ask_user + ErrAwaitingUserInput sentinel
│   │   │   ├── read_tool_output.go    # sidecar reader (non-deferred)
│   │   │   ├── execute.go             # sandbox tool (Deferred=true)
│   │   │   ├── task.go                # scheduler tool (Deferred + ActionRouter)
│   │   │   ├── ingest.go              # memory ingestion (Deferred)
│   │   │   ├── memory.go              # memory.search/recall/forget (Deferred)
│   │   │   └── web_*.go               # web_search.go, web_fetch.go (Deferred)
│   │   └── workflow/                  # workflow agents (Sequential/Loop/Parallel)
│   │       ├── sequential.go
│   │       ├── loop.go
│   │       └── parallel.go
│   ├── agui/                          # TRANSPORT GATEWAY — AG-UI protocol
│   │   ├── client.go                  # type aliases over Go SDK community
│   │   ├── translator.go              # pure Event → AG-UI mapping
│   │   ├── fanout.go                  # multi-subscriber distribution
│   │   ├── server.go                  # HTTP POST /agent/run (SSE) + GET /threads/<id>/messages
│   │   └── types.go                   # RunAgentInput parser
│   ├── channels/                      # CLIENT LAYER — channel framework
│   │   ├── channel.go                 # Channel interface
│   │   ├── registry.go                # StartAll / StopAll
│   │   ├── cli/cli.go                 # CLI as Channel (migrated from cmd/aura/chat.go)
│   │   └── telegram/                  # Telegram bot (main user-facing)
│   │       ├── bot.go                 # tele.Bot wrapper
│   │       ├── commands.go            # 8 MVP commands
│   │       ├── renderer.go            # AG-UI events → Telegram messages
│   │       ├── status_pane.go         # Pattern B status pane
│   │       ├── hitl.go                # ask_user → InlineKeyboard / ForceReply
│   │       ├── voice.go / photo.go    # Gemma 4 multimodal (9c)
│   │       ├── documents.go           # markitdown tiered sync/async
│   │       ├── agui_subscriber.go     # subscribe to agui.Fanout
│   │       ├── onboarding.go          # /start <token> matcher
│   │       └── store.go               # sqlc adapter telegram_accounts
│   ├── setup/                         # Setup wizard (HTTP 127.0.0.1:9081)
│   ├── llm/
│   │   ├── client.go                  # provider-neutral interface
│   │   ├── router.go                  # LLMRouter (remote vs local, Slice 13)
│   │   ├── openai_compat/             # SSE + tool-call accumulator
│   │   │   ├── client.go
│   │   │   ├── models.go              # ContextWindow + MaxOutputTokens lookup
│   │   │   └── testdata/              # SSE fixtures
│   │   ├── prompt/                    # Slice 4 KV cache builder
│   │   │   ├── builder.go             # PromptBuilder (stable-prefix)
│   │   │   └── cache.go               # provider-aware cache_control injection
│   │   ├── cost_tracker.go            # sqlc adapter local_llm_cost (Slice 13)
│   │   └── offline_detector.go        # TCP dial poller (Slice 13)
│   ├── conversations/                 # Conversation persistence (Slice 1.8)
│   │   ├── store.go                   # sqlc adapter, atomic AppendTurn
│   │   ├── title.go                   # auto-title background goroutine
│   │   ├── microcompact.go            # L1 tool turn eviction
│   │   ├── budget.go                  # L2 hard cap (Claude Code style)
│   │   ├── sidecar.go                 # >64 KiB content spillover
│   │   ├── cleanup.go                 # boot orphan scan + tmp/ TTL
│   │   └── types.go
│   ├── identity/                      # Identity + capability grants (Slice 1.7)
│   │   ├── store.go
│   │   ├── capability.go              # HasCapability + wildcard
│   │   └── types.go
│   ├── sandbox/                       # Sandbox Go-side wrapper (Slice 2a/2b)
│   │   ├── docker.go                  # DockerRunner HTTP client
│   │   ├── sessions.go                # SessionManager (2b)
│   │   ├── workspace.go               # workspace mount manager
│   │   ├── network.go                 # allowlist + iptables hooks
│   │   └── config.go
│   ├── swarm/                         # Swarm coordinator (Slice 3)
│   │   ├── coordinator.go             # Spawn/Talk/Join
│   │   ├── bus.go                     # shared message bus + DM-by-ID
│   │   └── tier.go                    # chat/reasoning/worker classification
│   ├── cron/                          # Scheduler (Slice 6)
│   │   ├── scheduler.go               # tick loop + crash-recovery
│   │   ├── store.go                   # sqlc adapter
│   │   ├── types.go                   # Task, TaskKind, ScheduleKind
│   │   └── handlers/                  # one file per TaskKind, each = Agent
│   │       ├── handler.go             # Handler = type alias of agent.Agent + metadata
│   │       ├── reminder.go
│   │       ├── agent_job.go
│   │       └── backup_postgres.go / backup_neo4j.go
│   ├── skills/                        # Skills (Slice 7a-e)
│   │   ├── loader/                    # 4-way split: filesystem + parser + cache + loader
│   │   ├── validator.go               # single source of truth
│   │   ├── paths.go                   # SanitizeName chokepoint
│   │   ├── writer.go                  # atomic pending→active
│   │   ├── deleter.go
│   │   ├── installer.go               # npx skills add --ignore-scripts
│   │   ├── catalog.go                 # skills.sh fetch + parse
│   │   ├── snippet.go                 # Slice 7e executable code snippets
│   │   ├── audit.go                   # sqlc adapter skill_audit
│   │   └── types.go
│   ├── web/                           # Web tools (Slice 5)
│   │   ├── searxng.go
│   │   ├── fetcher.go                 # SSRF-defended Fetch
│   │   ├── html.go                    # readability + html→markdown
│   │   └── config.go
│   ├── memory/                        # Memory subsystem (Slice 11)
│   │   ├── ingest/                    # Cognify 6-stage pipeline
│   │   │   ├── pipeline.go
│   │   │   ├── chunker.go
│   │   │   ├── embedder.go
│   │   │   ├── entity_extractor.go    # mem0 2-fase
│   │   │   └── audit.go
│   │   ├── graph/                     # community detection + memify
│   │   │   ├── community.go           # Leiden via GDS
│   │   │   └── memify.go              # prune/strengthen/derive
│   │   ├── retrieval/                 # hybrid + rerank
│   │   │   ├── search.go              # BM25 + HNSW + 1-hop
│   │   │   ├── rerank.go              # LLM tier=worker
│   │   │   ├── recall.go              # entity-based recall
│   │   │   └── global_search.go       # GraphRAG global pattern
│   │   └── agent/                     # agent journal
│   │       ├── journal.go             # :AgentEpisode post-conv
│   │       └── insight.go             # :AgentInsight cross-conv pattern
│   ├── onboarding/                    # Onboarding (Slice 10)
│   │   ├── interview.go               # LoopAgent[InterviewStepAgent]
│   │   ├── steps.go                   # InterviewStepAgent + SummaryConfirmAgent
│   │   ├── store.go                   # filesystem ~/.aura/agents/<id>/
│   │   ├── injector.go                # Agent.md as 2nd system message
│   │   ├── extractor.go               # LLM-driven fact extraction
│   │   └── updater.go                 # hybrid auto-update (mem0 ADD-only)
│   ├── askuser/cli.go                 # CLI Responder renderer (Slice 1.5)
│   ├── knowledge/                     # Neo4j infra (Slice 0.7)
│   │   ├── client.go                  # MCP-neo4j-cypher stdio subprocess
│   │   ├── migrate.go                 # *.cypher migrations + audit in PG
│   │   ├── ping.go
│   │   ├── config.go
│   │   └── migrations/*.cypher
│   ├── db/                            # Postgres infra (Slice 0.5)
│   │   ├── db.go                      # pgxpool.Pool + ping
│   │   ├── migrate.go                 # golang-migrate + embed.FS
│   │   ├── config.go
│   │   ├── migrations/*.sql           # 0001 → 0014 numbered
│   │   ├── queries/*.sql              # sqlc source (one per domain)
│   │   └── sqlc/                      # GENERATED — committed, CI golden test
│   ├── scoring/scoring.go             # Risk-Based: ComputeTaskTier / ComputeSkillTier
│   └── config/config.go               # ROOT composite only — per-subsystem configs live in subsystems
```

### Structure Rationale

- **`cmd/aura/` thin, `internal/` fat:** Standard Go layout. `cmd/aura/main.go` is router + dependency wiring only — all business logic in `internal/`. Cumulative `cmd/aura/main.go` cap 600 LOC enforced.
- **`internal/agent/` owns the runtime contract:** This is the only package that defines `Agent`/`Event`/`InvocationContext`. Everything else (workflow agents, LlmAgent, scheduler handlers, onboarding steps, skill virtual agents) implements the interface and lives elsewhere. The interface NEVER imports its implementations.
- **`internal/agui/` is transport, not runtime:** The translator pure-function consumes `iter.Seq2[*agent.Event, error]` directly. No reverse coupling — `internal/agent/` MUST NOT import `internal/agui/`. This is the single most important boundary in the system.
- **`internal/channels/` is in-process composition:** Channels are sibling consumers of the same Event stream via `agui.Fanout`. Telegram is just one channel; AG-UI HTTP is another. Adding "web SPA" requires zero changes to the runtime layer.
- **`sandbox/` (repo root, no `internal/`) is sidecar materials, not Go code:** Dockerfile + compose + seccomp + sidecar.py. The Go HTTP client lives in `internal/sandbox/` (different concern).
- **`internal/db/` owns Postgres, `internal/knowledge/` owns Neo4j:** Strict store-per-package. No cross-imports. `internal/db/sqlc/` is generated code (committed, CI golden test).
- **`internal/config/` is composite root only:** Per-subsystem configs (`internal/sandbox/config.go`, `internal/web/config.go`, etc.) live in their subsystem packages. Explicit non-god-class rule. The root `Config{LLM, DB, RunDir, ToolPreviewCap}` references them — no subsystem fields directly.
- **`internal/scoring/` is the Risk-Based governance chokepoint:** Two functions, one file. Shared by Slice 6 (cron) + Slice 7 (skills) for `ComputeTaskTier`/`ComputeSkillTier`. Avoids governance logic scattered across capability packages.
- **No `internal/util/` or `internal/common/`:** Cross-cutting helpers live in their own thin package (e.g., `internal/scoring/`, `internal/askuser/`). The god-class package is forbidden.

---

## Architectural Patterns

### Pattern 1: Unified Agent Interface (ADK-go theft)

**What:** Every "thing that runs and emits events" implements one Go interface. LlmAgent, SequentialAgent, LoopAgent, ParallelAgent, ReminderAgent (scheduler), InterviewStepAgent (onboarding), Skill virtual agent, swarm worker — same signature, same streaming protocol, same termination model.

**When to use:** Any time you would otherwise reach for a custom runtime (a "scheduler loop", a "swarm loop", an "onboarding state machine"). One runtime, N implementations.

**Trade-offs:**
- ✓ Single mental model. Test once (mocks reuse). Documentation once.
- ✓ Composability: workflow agents nest arbitrarily. `LoopAgent[SequentialAgent[LlmAgent, LlmAgent]]` is a real pattern from ADK-go examples (critic loop).
- ✓ Streaming via `iter.Seq2` is idiomatic Go 1.23+ — range-over-func everywhere, no custom channels, no callback emitters.
- ✗ Forces Go 1.23+ minimum (worth it).
- ✗ Pattern theft (verbatim shape, zero deps) requires discipline — ADK-go evolves; Aura's copy doesn't track it. Acceptable: the interface is small (5 methods) and unlikely to churn.

**Example (Aura PRD Slice 0.9):**
```go
type Agent interface {
    Name() string
    Description() string
    Run(InvocationContext) iter.Seq2[*Event, error]
    SubAgents() []Agent
    FindAgent(name string) Agent
}

// SequentialAgent: iterate sub-agents in order, escalate-aware
func (a *sequentialAgent) Run(ctx InvocationContext) iter.Seq2[*Event, error] {
    return func(yield func(*Event, error) bool) {
        for _, sub := range ctx.Agent.SubAgents() {
            for event, err := range sub.Run(ctx) {
                if !yield(event, err) { return }
                if event != nil && event.Actions.Escalate { return }  // bubble up
            }
        }
    }
}
```

### Pattern 2: Deferred-Tool Loading (cache-protection)

**What:** Tools with non-trivial Description or complex JSON Parameters mark themselves `Deferred: true`. The default manifest exposes only `name + 1-line summary` — the model reads `tool_search` (a built-in hook tool, non-deferred) to fetch the full spec on demand.

**When to use:** Any tool whose spec exceeds a couple of fields. With N tools registered, this is the difference between linear and constant cache-invalidation cost per turn.

**Trade-offs:**
- ✓ Stable-prefix invariant preserved as N grows. The cached system + manifest block doesn't grow per added tool.
- ✓ Pattern just shipped by Anthropic (`advanced-tool-use-2025-11-20` beta) for the same reason — Aura is on the right side of the convergence curve.
- ✗ Model needs one extra round-trip to use a deferred tool first time. Acceptable cost. Caching of `tool_search` response across turns mitigates this.
- ✗ Discipline required: every tool added must be reviewed for deferred-vs-non-deferred. Small tools (`text_response`, `ask_user`, `read_tool_output`) stay non-deferred so they're always visible.

**Example:**
```go
// internal/agent/tools/execute.go
var Spec = tools.Spec{
    Name:        "execute",
    Summary:     "Run code in a sandbox (Python/shell)",
    Description: "... long description with multiple paragraphs of usage guidance ...",
    Parameters:  jsonSchemaForExecute,
    Deferred:    true,  // ← do not ship full spec in default manifest
}
```

### Pattern 3: KV Cache Stable-Prefix (the Single Most Important Performance Discipline)

**What:** `messages[0]` (system) is byte-identical turn-on-turn. Tool manifest sorted deterministically (alphabetical). Cache breakpoints injected at provider-aware locations: Anthropic `cache_control: ephemeral` on system + tools; DeepSeek auto-cache no-op (relies on prefix stability + parses `usage.prompt_cache_hit_tokens`); OpenAI automatic (no annotation needed, just prefix stability + ≥1024 tokens).

**When to use:** Always. Every prompt build site must respect this. PRD Slice 4 extracts prompt assembly out of `LlmAgent` into a dedicated `PromptBuilder` precisely so the invariants can be enforced and tested centrally.

**Trade-offs:**
- ✓ DeepSeek-V4 Flash: 80% cache savings, −63% cost on Aura's measured workload (memory `reference_openrouter_provider_capabilities_2026-05-27`).
- ✓ Anthropic: cache writes cost 1.25× input rate for 5min TTL, 2× for 1hr TTL; reads are 10% of normal — net positive after 2-3 turns of stable prefix.
- ✗ Surprisingly hard. Six "cache poisoning sites" mapped in `feedback_aura_cache_poisoning_sites_2026-05-27` — any per-turn timestamp, request ID, or context mutation in `messages[0]` invalidates the entire prefix. The PRD's "no mutating `messages[0]`" anti-pattern is non-negotiable.
- ✗ Sliding-window history compaction (if naïvely re-indexed) breaks the prefix. Aura's microcompact L1 design (replace tool turns with pointer text — keeps message indexes stable) is the right approach.

**Example:**
```go
// internal/llm/prompt/builder.go
func (b *Builder) Build(history []Message, tools []ToolSpec, provider Provider) []Message {
    // 1. messages[0] = main system prompt (byte-identical across turns)
    // 2. messages[1] = Agent.md if present (per-identity, stable for the identity)
    // 3. messages[2] = top-K :AgentInsight (stable for K minutes via cache)
    // 4. messages[3..N] = history (append-only)
    sys := b.staticSystem
    if provider == Anthropic {
        sys.CacheControl = &CacheControl{Type: "ephemeral"}
    }
    // ... tool manifest sorted alphabetical, same cache_control if Anthropic
}
```

### Pattern 4: ToolResult Preview + Persist Sidecar

**What:** Results from heavy tools (sandbox stdout, web fetches, large memory queries) get capped at `AURA_CONTEXT_PREVIEW_CAP_BYTES` (default 2048) in conversation history. Full output sidecars to `$AURA_RUN_DIR/conversations/<conv_id>/<tool_call_id>.result`. Model fetches ranges via `read_tool_output(tool_call_id, offset?, limit?)`.

**When to use:** Every tool that can produce >2 KB of output. Especially `execute`, `web_fetch`, `memory.search`, `ingest.file`.

**Trade-offs:**
- ✓ Context window protected. Without this, a 21 MB Python stdout poisons the entire conversation history.
- ✓ Microcompact L1 (Slice 1.8) reuses the sidecar pattern: old tool turns get content replaced with `[read_tool_output(X) — sidecar at ...]` pointer, indexes preserved.
- ✗ Slight cognitive load on the model — needs to know `read_tool_output` exists. Acceptable: it's non-deferred (always visible) and the footer pointer in the preview is self-documenting.

### Pattern 5: Multi-Channel Fanout via Pure Stream Transform

**What:** The agent runtime emits one `iter.Seq2[*Event, error]`. The AG-UI translator is a pure function that maps to `iter.Seq2[agui.Event, error]`. The fanout helper broadcasts this to N in-process subscribers (Telegram bot, CLI renderer, AG-UI SSE clients).

**When to use:** Any time you want one agent to fan out to multiple UIs simultaneously without duplicating runtime logic.

**Trade-offs:**
- ✓ Adding a new channel = implementing `Channel` interface + subscribing to fanout. Zero changes to runtime.
- ✓ Telegram bot in-process can subscribe to the same stream as a remote AG-UI client — user types in Telegram, sees response in web Dojo simultaneously (PRD scope).
- ✓ Backpressure via bounded subscriber channel (cap 64) + drop-on-slow-consumer prevents one slow client from blocking the runtime.
- ✗ Drop semantics need to be visible to user (UI shows `RUN_ERROR` if subscriber drops). Aura's PRD handles this explicitly.

### Pattern 6: Pause / Resume via Sentinel Error

**What:** `ask_user` tool returns Go sentinel `ErrAwaitingUserInput{Question, Options, Kind, Priority, ToolCallID, ResumeContext}` — NOT a `ToolResult`. `LlmAgent` intercepts via `errors.As`, persists `PausedState` row to `aura.paused_states`, emits `Event{Actions.Escalate=true}`, returns to caller.

**When to use:** Any tool that needs HITL (Human-In-The-Loop) — approvals, clarifications, choice between options. Notably: Risk-Based governance gating (RISKY+ task or skill mutations).

**Trade-offs:**
- ✓ First-class Go error sentinel = compile-checked. No magic strings or status enums.
- ✓ FIFO multi-pause: model can issue N parallel `ask_user` calls in same turn → N `PausedState` rows → Loop pauses until ALL resumed (ordered `priority DESC, created_at ASC`).
- ✓ Resume contract is symmetric: client POSTs to `/agent/run` with same `threadId` + new `runId` + messages including `RoleTool` answers matching `tool_call_id`.
- ✗ Exclusivity with `text_response` in same turn must be enforced by Loop (otherwise `text_response` would terminate while `ask_user` is still pending). PRD Slice 1.5 spells this out.

### Pattern 7: Semantic-Strict Three-Store Persistence

**What:** Postgres = application state (conversations, identities, audits, scheduler tasks). Neo4j = knowledge + vectors (documents, chunks, entities, communities, agent journal). Filesystem = operational artifacts (sidecar files, skill bodies, profile markdown). No crossover.

**When to use:** Always. Adding a "scratch facts" JSONB column to `aura.conversations` is forbidden. Writing a markdown wiki under `~/.aura/wiki/` for "lightweight knowledge" is forbidden.

**Trade-offs:**
- ✓ Predictable backup story: `pg_dump` for app state + `neo4j-admin database dump` for knowledge + rsync for FS.
- ✓ Clear ownership: graph algorithms (Leiden, PageRank, similarity) live in Neo4j via GDS — Go code uses them via Cypher through MCP. No "let's reimplement Leiden in Go".
- ✓ Single source of truth per concern: there is exactly one place to look for an audit row (Postgres), exactly one place for an entity (Neo4j), exactly one place for a tool result blob (filesystem).
- ✗ Three backup paths to maintain. Acceptable cost.
- ✗ Discipline: developers will be tempted to "cache facts in Postgres for speed". The PRD's persistence anti-pattern call-out exists for this exact reason.

### Pattern 8: Workflow Agents as Composition Primitives (Sequential / Loop / Parallel)

**What:** Three built-in agent implementations that compose `SubAgents()`. SequentialAgent iterates once in order. LoopAgent repeats N times or until child Escalate. ParallelAgent runs concurrently via errgroup + ackChan synchronous backpressure.

**When to use:** Any time you'd write custom iteration/branching/parallelism logic. PRD examples: Slice 3 swarm = ParallelAgent + LlmAgent workers; Slice 6 scheduler dispatch = SequentialAgent in some kinds; Slice 10 onboarding = LoopAgent[InterviewStepAgent] with escalation on "Conferma".

**Trade-offs:**
- ✓ Tested once (workflow_test.go), used everywhere. Aura's PRD estimates `−680` LOC savings across slices 1/3/6/7/8/10 from this single decision.
- ✓ Termination model is honest: escalation propagates via `return` after every `yield` check. No magic, just bubble-up. Easy to debug.
- ✗ ParallelAgent backpressure semantics (synchronous ackChan vs buffered) is a real choice. Aura takes synchronous (per ADK-go) to avoid memory bloat with large N — correct trade-off for personal-AI scale.

---

## Data Flow

### Request Flow — Telegram User Sends a Message (main user case)

```
[User: text/voice/photo/doc in Telegram]
    ↓
[channels/telegram/bot.go: polling receives Update]
    ↓
[multimodal preprocess if needed]
    ├─ voice → POST aura-llama-multimodal /transcribe → text
    ├─ photo → POST aura-llama-multimodal /describe   → text
    └─ doc   → POST markitdown /convert (tiered sync/async) → markdown
    ↓
[conversations.Store.AppendTurn: write aura.conversation_turns row]
    ↓
[LlmAgent.Run(InvocationContext) returns iter.Seq2[*Event, error]]
    ↓
[PromptBuilder.Build:
    messages[0] = main system (byte-identical, cache hit)
    messages[1] = Agent.md per identity (byte-identical for this identity)
    messages[2] = top-K :AgentInsight (Neo4j query, embedded)
    messages[3..N] = history (microcompact-applied)
    tools = manifest (alphabetical, non-deferred specs only, deferred = name+summary)]
    ↓
[LLMRouter.Route(ctx): pick remote (OpenRouter) vs local (vLLM) based on
    prefer_local / offline / cost threshold]
    ↓
[openai_compat.Client.Stream(req): SSE stream from provider
    → Chunk → tool-call delta accumulator → Event{LLMResponse{...}} yielded]
    ↓
[agui.Translator: *agent.Event → AG-UI events
    (RUN_STARTED, TEXT_MESSAGE_*, TOOL_CALL_*, STATE_DELTA, REASONING_*)]
    ↓
[agui.Fanout: distribute to in-process subscribers + HTTP SSE clients]
    ↓
[For each tool_use in stream:]
    ├─ Registry.Get(name).Execute(ctx, args)
    ├─ Wrap result in ToolResult{Preview ≤cap, FullPath if spilled, Bytes, Truncated}
    ├─ ToolResult preview → append to history (RoleTool with tool_call_id)
    └─ If preview cap exceeded → write sidecar $AURA_RUN_DIR/.../<tool_call_id>.result
    ↓
[Loop iterates MaxSteps=8 OR until text_response invoked OR ErrAwaitingUserInput]
    ↓
[On text_response: final reply. conversations.Store.AppendTurn writes assistant turn
    + token totals (input/output/cached). Optionally auto-title trigger if seq>=3.]
    ↓
[telegram/renderer.go: AG-UI events → Telegram messages
    Pattern B: 2 msgs per turn (status pane + content reply)
    Markdown via eekstunt/telegramify-markdown-go
    Throttle 1500ms status / 500ms content / 1000ms chat rate-limit]
```

### Pause / Resume Flow

```
[Tool returns ErrAwaitingUserInput{Question, Options, Kind, Priority,
    ResumeContext, ToolCallID}]
    ↓
[LlmAgent.runTool intercepts via errors.As, builds PausedState,
    upserts to aura.paused_states]
    ↓
[Yields Event{Actions.Escalate=true, StateDelta:{pending: [...]}}]
    ↓
[agui.Translator: emits RUN_FINISHED{outcome.type="interrupted",
    outcome.interrupts[]} — interrupts[] mapped from PausedState[]]
    ↓
[telegram/renderer: shows InlineKeyboardMarkup (kind=approval/choice)
    OR ForceReply (kind=clarification)]
    ↓
[User taps button → callback "resume:<token>:<idx>" → channel handler
    → LlmAgent.Resume(token, answer)]
    ↓
[Loop appends RoleTool{ToolCallID, Content: answer} and continues from
    InvocationContext snapshot]
    ↓
[Multi-pause coalesce: N ask_user in same turn → N PausedState rows →
    Loop pauses until ALL resumed, FIFO ordered (priority DESC, created_at ASC)]
```

### Ingestion Flow (Slice 11)

```
[aura ingest /path/file.pdf  OR  Telegram document attach]
    ↓
[markitdown sidecar: convert to markdown (tiered sync ≤5MB / async 5-50MB)]
    ↓
[memory.ingest.Pipeline (Cognify 6-stage):
    1. Classify document (markdown vs code vs structured)
    2. Check permissions (identity_id ownership)
    3. Chunker: recursive semantic split
       - AURA_MEMORY_CHUNK_SIZE_TOKENS=512 default
       - AURA_MEMORY_CHUNK_OVERLAP_TOKENS=64
       - Respect markdown headers; sliding-window fallback
    4. EntityExtractor: mem0 2-fase pattern
       - Phase 1: LLM tier=reasoning extracts candidates batch 10 chunks
       - Phase 2: conflict detect via fuzzy name+type + embedding sim >0.92
       - MERGE existing OR CREATE new entity
       - Type taxonomy: Person/Org/Location/Concept/Event/Topic
    5. Generate summary (LLM tier=worker)
    6. Embedder: batch 32/call to aura-llama-embed (768d native)]
    ↓
[Neo4j upsert via mcp-neo4j-cypher MCP subprocess:
    :Document + :Chunk(embedding) + :Entity + :HAS_CHUNK + :MENTIONS]
    ↓
[ingest_audit row in Postgres aura.ingest_audit (parity with skill_audit)]
    ↓
[Background goroutine 24h: memory.graph.Community
    CALL gds.leiden.stream → cluster entities into hierarchical communities
    LLM tier=worker summarizes each community
    Persist :Community + :CONTAINS hierarchical
    Embed community summaries for global-query retrieval]
```

### State Management

| Layer | State Lives Where | Lifetime |
|-------|-------------------|----------|
| In-flight conversation | `LlmAgent` instance (in-memory `Messages` slice + Loop state) | Lifetime of the agent run (one goroutine per conversation) |
| Persistent conversation | `aura.conversations` + `aura.conversation_turns` (Postgres) | Until `aura chat delete <id>` (cascade) |
| Tool result spillover | `$AURA_RUN_DIR/conversations/<id>/<tool_call>.result` (filesystem) | Cascade-deleted with conversation; boot orphan scan at restart |
| Microcompact L1 | Not a separate state — replaces `content` field in `aura.conversation_turns` for tool turns older than `AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS=10` | Indexes preserved (no re-shuffle), pointer text references the sidecar |
| Budget L2 | Computed at `LoadHistory` time: `hard_cap = ContextWindow - max(MaxOutputTokens, 20K) - 13K`. Hard cap = error. | Per-turn; no persistent state |
| Paused state | `aura.paused_states` (Postgres) — FK to `aura.conversations(id) ON DELETE CASCADE` | Until resumed OR conversation ends (Loop.Stop auto-resolves) |
| Knowledge | Neo4j `:Document` / `:Chunk` / `:Entity` / `:Community` / `:AgentEpisode` / `:AgentInsight` | Permanent unless explicit `memory.forget` or document deletion cascade |
| Agent journal | Neo4j `:AgentEpisode` (post-conv summary) + `:AgentInsight` (cross-conv pattern) — both embedded | Permanent; pruned by Memify periodic (90gg no-mention → prune) |
| Identity profile | `~/.aura/agents/<identity_id>/Agent.md` + `preferences.json` + `metadata.json` + `changelog.md` | Permanent; per-identity; injected as `messages[1]` |
| Skill body | `~/.aura/skills/active/<name>/SKILL.md` + supporting files | Until `skill.delete` OR TTL 90d archived (Slice 7e) |

---

## Build Order Implications

The PRD's 14-slice sequencing is **not arbitrary** — each slice has hard upstream dependencies. The architecturally meaningful ordering is:

| Order | Slice | Why Must Come Before Next |
|-------|-------|---------------------------|
| 1 | **0.5 Postgres infra** | Conversations (1.8), identities (1.7), paused_states (1.5), schedulers (6), audits (7/10/11), all require `pgxpool` + migrations + sqlc. Cannot persist anything otherwise. |
| 2 | **0.7 Neo4j infra** | Memory (11), skill similarity (7e), agent journal (11e), all require Neo4j + HNSW + GDS + MCP-neo4j-cypher subprocess. Cannot do knowledge work otherwise. |
| 3 | **0.9 Agent interface** | LlmAgent (1), workflow agents (used by 3/6/10), swarm (3), scheduler dispatch (6), onboarding (10), all implement `agent.Agent`. Without this, every subsystem invents its own runtime. **This is the cornerstone**. |
| 4 | **1 LLM client + ToolResult** | Every LLM call routes through openai_compat. ToolResult preview+spill pattern needed by execute (2), web (5), memory (11). |
| 5 | **1.5 ask_user pause/resume** | Risk-Based gating (6/7) needs HITL primitive. Onboarding interview (10) needs it. Skill governance (7c) needs it. |
| 6 | **1.7 Identity** | Conversations FK (1.8), capability grants (gate tools), Telegram account mapping (9a), Agent.md per-identity (10). |
| 7 | **1.8 Conversation persistence** | Multi-thread + microcompact + sidecar pattern. AG-UI gateway (8) needs `threadId = conv_id`. Memory (11) rollup `:UserConversation` needs conv_id. |
| 8 | **2 Sandbox (2a stateless first, then 2b session)** | Skills 7e executable snippets require sandbox 2b. Heavy ingestion (11) doesn't need sandbox but execute tool does. 2a unlocks `execute` tool, 2b unlocks workspace-mounted persistence. |
| 9 | **3 Swarm** | Reuses ParallelAgent (Slice 0.9). Useful for retrieval (11d re-rank batch), web tools (5 parallel fetches), but optional for v1 core. |
| 10 | **4 KV cache builder** | **Last (or near last) deliberately** — must come AFTER stable system prompt + tool manifest are real (not stubs). Extracts prompt assembly from `LlmAgent`. Empty value if built early against a churning system prompt. |
| 11 | **5 Web tools** | Independent of memory; needed by `ingest.url` (11) but `web_fetch` standalone is useful immediately after Slice 4. |
| 12 | **6 Scheduler** | Needs identity (1.7), conversations (1.8), Risk-Based scoring. Handler = `agent.Agent` impl. |
| 13 | **7 Skills (7a/b/c/d/e)** | 7a/b read-only (independent). 7c/d mutation (needs ask_user 1.5 + skill_audit table). 7e snippet (needs sandbox 2b + Neo4j HNSW 0.7). Strictly atomic 5-sub-slice. |
| 14 | **8 AG-UI gateway** | Needs LlmAgent (1) + conversations (1.8) + pause/resume (1.5). Translator consumes `iter.Seq2[*Event, error]` from Slice 0.9. |
| 15 | **9a/b/c Channels + Telegram + multimodal** | 9a needs AG-UI (8) for transport contract. 9b needs 9a + Telegram bot lib. 9c needs Gemma 4 sidecar + markitdown sidecar. |
| 16 | **10 Onboarding + Agent.md** | Needs LoopAgent (0.9) + ask_user (1.5) + identity (1.7) + filesystem layout. Agent.md injection point is `messages[1]` (Slice 4 prompt builder hook). |
| 17 | **11a/b/c/d/e Memory ingestion + retrieval + journal** | Needs 0.7 Neo4j + 1.7 identity + 1.8 conv (for `:UserConversation` rollup) + 5 web tools (for `ingest.url`) + 9c markitdown + 10 Agent.md. The most-downstream slice in v1. |
| 18 | **13 Local LLM fallback** (v2, gated on GPU) | Needs LLMRouter slot in Slice 1 + cost tracker + offline detector. Out of v1 scope per PROJECT.md. |

**Build order summary:** Postgres (0.5) → Neo4j (0.7) → **Agent interface (0.9, cornerstone)** → LLM client + ToolResult (1) → pause/resume (1.5) → identity (1.7) → conversations (1.8) → sandbox 2a → swarm (3) → KV cache (4, deliberately late) → web tools (5) → scheduler (6) → sandbox 2b → skills 7a-e → AG-UI (8) → channels 9a/b/c → onboarding (10) → memory 11a-e.

**Critical insight:** Slice 0.9 (Agent interface) is the cornerstone. The PRD's `−400 LOC` net savings claim from Slice 0.9 is structurally correct — every later slice either implements the interface or composes via workflow agents. Skipping 0.9 to "get to a real LlmAgent faster" would force re-architecture of every downstream slice.

---

## Coupling Concerns (Risks to Call Out)

These are the boundaries where Aura's architecture is most likely to leak or grow circular over time. Each warrants test enforcement (static analysis or import-cycle guards).

### Risk 1: Tools importing channels

**Symptom:** A tool decides it wants to "send a notification to Telegram" or "render a message in the CLI" directly.

**Why dangerous:** Tools live in `internal/agent/tools/`. Channels live in `internal/channels/`. If tools import channels, you've created a circular dep waiting to happen (channel → runtime → tool → channel) and you've made tools transport-aware (defeats the whole AG-UI translator design).

**Mitigation:** Tools NEVER import channels. Tools emit `Event{StateDelta: ...}` or return `ToolResult`; the channel renders whatever it observes via the AG-UI fanout. Notifier pattern (PRD Slice 6) routes through fanout, not through direct channel calls.

### Risk 2: Agent runtime importing AG-UI types

**Symptom:** `internal/agent/event.go` grows fields like `AGUIEventType string` or `MessageID string` to "help the translator".

**Why dangerous:** Couples runtime to one transport. Adding a non-AG-UI channel (e.g., a future MQTT transport) becomes impossible without runtime changes.

**Mitigation:** `internal/agent` MUST NOT import `internal/agui`. The translator is a pure function consuming the runtime stream from the outside — runtime knows nothing about transport. Static analysis check: `grep -L 'internal/agui' internal/agent/*.go` returns empty (or fails CI).

### Risk 3: Conversations.Store called from too many places

**Symptom:** Sandbox, web tools, memory ingest all want to "log an event to the conversation".

**Why dangerous:** Concurrent writers to `aura.conversation_turns` need careful sequencing. If everyone calls `AppendTurn`, the seq number contention is a race risk.

**Mitigation:** Only `LlmAgent.runTool` writes turns. Other subsystems write to their domain tables (`aura.skill_audit`, `aura.ingest_audit`, `aura.agent_job_runs`). The `aura.conversation_turns` table is owned by exactly one writer.

### Risk 4: Identity / capability check at runtime vs tool dispatch level

**Symptom:** Choice: does `LlmAgent` check `HasCapability(identityID, "skill.write")` before dispatching to `skill_create` tool? Or does `skill_create` tool check it internally?

**PRD's implicit choice:** Tool-level check (each mutation tool calls `identity.HasCapability` internally + computes Risk tier via `scoring.ComputeSkillTier`). This is correct because:
- ✓ Different tools need different capabilities (skill_write vs schedule_write vs memory_write). A central LlmAgent check would need a tool-to-capability map duplicated outside the tool.
- ✓ Risk-Based governance is per-tool, not per-Agent.

**Risk:** A new mutation tool is added without the capability check. **Mitigation:** Lint rule + audit checklist: every tool with `Spec.Risky=true` MUST call `identity.HasCapability` in its `Execute`.

### Risk 5: Microcompact L1 invalidating KV cache

**Symptom:** Microcompact rewrites tool turn content to a pointer string. If this happens to a turn at index < where Anthropic cache_control is anchored, the cache is invalidated retroactively.

**Mitigation (PRD's design):** Microcompact only affects `role='tool'` turns at `seq < (max_seq - AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS)`. The cache anchor is at messages[0..2] (system + Agent.md + AgentInsight), all of which are at much lower indexes and never microcompacted. **But** there's an edge case: if `messages[2]` (top-K AgentInsight) is recomputed per turn (different K → different content), cache breaks. PRD Slice 11 must spec: AgentInsight retrieval is **cached for N minutes** to preserve `messages[2]` stability. Flag this for Slice 11 detailed design.

### Risk 6: Sandbox session-bound containers (2b) vs identity

**Symptom:** Session-bound containers are keyed by `conversation_id`, but identity-level isolation might want per-identity workspace.

**PRD's implicit choice:** Per-conversation workspace. Acceptable for single-user v1. Multi-user (future) would key on `(identity_id, conversation_id)` or just `identity_id` with mounted shared workspace per identity. **Flag for v2.**

### Risk 7: MCP-neo4j-cypher subprocess lifecycle

**Symptom:** MCP subprocess crashes → all Neo4j access fails → memory + skills + agent journal all dark.

**Mitigation:** Health check + restart loop in `internal/knowledge/client.go`. Bounded retry with backoff. Surface failure as `RUN_ERROR` to the user, NOT silent. PRD Slice 0.7 needs explicit acceptance criterion for subprocess restart.

### Risk 8: Telegram bot + AG-UI HTTP both attempting to write conversation

**Symptom:** User sends Telegram message → bot writes turn → simultaneously a Dojo client posts to `/agent/run` for same conv → race on seq number.

**Mitigation (PRD's design):** `(conversation_id, seq) PRIMARY KEY` + `SELECT ... FOR UPDATE` in `AppendTurn` previews the race. Multi-session on same conv (rare for single-user) gets clean `unique_violation`. Test exists in PRD Slice 1.8 acceptance.

---

## Scaling Considerations

Aura's target is **single-user, mini-PC 16-core 32 GB RAM** (memory `feedback_minipc_cpu_budget`). "Scale" for Aura means handling years of conversation history + thousands of documents + hundreds of skills — not concurrent users.

| Scale | Architecture Adjustments |
|-------|--------------------------|
| **1 user, <100 conversations, <1K documents** | Default Aura PRD architecture. No changes. RAM cumulative ~5.7–6.2 GB end-of-Slice-7. |
| **1 user, 100-10K conversations, 1K-10K documents** | First bottleneck: Neo4j HNSW index size. Validated 22-30ms p95 at this scale (memory `feedback_embedding_backend_stays_mistral`). KV cache hit rate ≥80% protects LLM cost. Microcompact L1 + budget L2 protect context window. |
| **1 user, 10K+ conversations** | Second bottleneck: `aura.conversation_turns` row count. Index `conversation_active_by_identity ON (identity_id, last_active_at DESC) WHERE status='active'` keeps list queries fast. Archive old conversations + sidecar GC for `$AURA_RUN_DIR`. Consider Postgres table partitioning by `created_at` quarter. |
| **Multi-user (out of v1)** | Schema-ready: `identity_id` FKs everywhere. But missing: HTTP auth on AG-UI endpoint (currently `127.0.0.1` bound only), capability_grants enforcement at tool dispatch, per-identity sandbox session isolation, per-identity Neo4j subgraph (or just `WHERE identity_id` filter on every Cypher). Major refactor (PRD says "Multi-tenancy reale arriva in milestone futura"). |
| **GPU-backed local LLM (Slice 13, v2)** | vLLM sidecar + LMCache disk-tier (50 GB cache). Adds +5-7 GB RAM. Gated on DGX Spark bundle availability. LLMRouter routing decision (remote vs local) keyed on `prefer_local` / offline detection / cost threshold. |

### Scaling Priorities

1. **First bottleneck (most likely to bite Aura users):** Context window growth on long-running conversations. **Fix:** Microcompact L1 + budget L2 already in PRD Slice 1.8. Property test the byte-identity of `messages[0]` and the monotonic growth of history.
2. **Second bottleneck:** Neo4j HNSW recall degradation as entity count grows. **Fix:** Memify pattern (Cognee) — prune stale entities (no-mention 90d), strengthen RELATED_TO weights, derive multi-hop facts. PRD Slice 11e ships this.
3. **Third bottleneck:** LLM cost. **Fix:** KV cache discipline (Slice 4) already in PRD. DeepSeek-V4 80% cache savings + Anthropic ephemeral cache + provider-aware breakpoints. Cost tracker (Slice 13) exposes via `aura llm-router cost --today`.
4. **Fourth bottleneck (not really a bottleneck — a UX one):** Tool manifest bloat as N tools grows. **Fix:** Deferred-tool pattern (PRD universal). Already enforced.

---

## Anti-Patterns

These are specific to Go-native agentic substrates. They map onto the PRD's existing anti-pattern list but worth repeating with cross-system citations.

### Anti-Pattern 1: Agent God Class

**What people do:** Cram every concern into one `Agent` struct — LLM client, prompt builder, tool registry, memory, scheduler, channels — because "the Agent runs everything".

**Why it's wrong:** Untestable, undebuggable, and breaks the substrate-vs-overlay distinction Aura is built on. Microsoft Agent Framework, OpenAI Agents SDK, and Google ADK-go all converge on the OPPOSITE: tiny `Agent` interface (5 methods in ADK-go), big ecosystem of composable implementations.

**Do this instead:** Aura's `Agent` interface = 5 methods. `LlmAgent` is one impl. Workflow agents are three more impls. Scheduler handlers are domain-specific impls. Each impl is small (60-480 LOC). The "magic" lives in composition, not in any one struct.

### Anti-Pattern 2: Custom Streaming Protocol

**What people do:** Invent a custom event emitter interface — `Emitter.OnText(...)`, `Emitter.OnToolCall(...)`, etc. — instead of streaming via Go 1.23 `iter.Seq2`.

**Why it's wrong:** Custom emitters create N callback contracts (one per event type), couple producer to consumer, and don't compose. Ranging over `iter.Seq2[*Event, error]` gives you a single stream that workflow agents can re-yield, translators can pure-transform, and fanouts can broadcast.

**Do this instead:** PRD Slice 0.9 ships `Run() iter.Seq2[*Event, error]`. Slice 8 AG-UI translator is a pure function over this stream (saved ~100 LOC vs the original Emitter design). Telegram subscriber in Slice 9b ranges over the fanout output the same way.

### Anti-Pattern 3: Wire-Level Caching Discipline

**What people do:** Put KV cache logic in the LLM HTTP client — "inject cache_control here if Anthropic, parse hit_rate there if DeepSeek".

**Why it's wrong:** Cache discipline is about prompt assembly invariants (stable prefix, deterministic ordering). The wire client only sees the final request — it can't enforce that `messages[0]` was byte-identical to the previous turn. Putting cache logic at wire level mistakes the symptom for the cause.

**Do this instead:** PRD Slice 4 extracts `PromptBuilder` separately. Cache_control injection lives there (provider-aware). Wire client is dumb — just parses `usage.prompt_cache_hit_tokens` from the response. Invariant tests live at the PromptBuilder level (hash SHA-256 of `json.Marshal(messages[0])` constant across 5 turns).

### Anti-Pattern 4: Mixing Knowledge into App Store

**What people do:** Add a JSONB `scratch_facts` column to `aura.conversations`. Or write a markdown wiki under `~/.aura/wiki/` and `[[wiki-link]]` cross-reference it from the LLM context.

**Why it's wrong:** Postgres app store ≠ Neo4j knowledge store. Mixing destroys backup semantics, makes Leiden/PageRank impossible, and breaks the "single source of truth per concern" promise. Aura killed wiki markdown on 2026-05-27 specifically for this reason (memory `project_graph_memory_core_strategy`).

**Do this instead:** Knowledge → Neo4j `:Entity` / `:Chunk` / `:Concept` via memory ingestion pipeline (Slice 11). Per-identity preferences → `~/.aura/agents/<id>/Agent.md` (Slice 10) — that's user profile, NOT knowledge.

### Anti-Pattern 5: Synchronous Sub-Agent Spawn Without Depth Cap

**What people do:** Swarm worker (depth 3) calls `Coordinator.Spawn(...)` to spawn another worker (depth 4). No depth check. Goroutine + LLM cost explodes exponentially.

**Why it's wrong:** Microsoft research on multi-agent systems and Letta both flag this as a top-3 production failure mode. `MAX_SPAWN_DEPTH=3` exists for a reason.

**Do this instead:** PRD Slice 3 enforces: `Coordinator.Spawn` rejects `Depth >= MaxSpawnDepth`. Use `Spawn` (not raw goroutines) so the check is centralized.

### Anti-Pattern 6: Sandbox-per-Tool-Call Without Session Reuse

**What people do:** Every `execute` call spawns a fresh Docker container. 800ms cold-start per call. The user types "what does this CSV look like?" + 5 follow-ups → 5 separate containers, 4 seconds of cold-start overhead.

**Why it's wrong:** OpenHands V1 made this exact V0 → V1 transition (memory: OpenHands moved from "mandatory Docker" to "optional sandboxing with LocalWorkspace default for lower friction"). The session-bound pattern (E2B, Claude Code on the Web, Anthropic Code Execution beta) is the convergence.

**Do this instead:** PRD Slice 2 ships BOTH: 2a stateless (snippet untrusted, per-call) AND 2b session-bound (per-conversation workspace + TTL 30min). Model chooses via `session_id` argument. Default stays stateless for backwards-compat.

### Anti-Pattern 7: Pause/Resume via External State Machine

**What people do:** Build a finite-state-machine library to track "agent paused" / "awaiting user" / "resuming" states. Or worse: use a workflow engine (Temporal, etc.) for this.

**Why it's wrong:** Over-engineered. Pause/resume is one sentinel error + one Postgres row + one event escalation. State lives in `aura.paused_states`. Loop resumes by appending `RoleTool{ToolCallID, Content: answer}` and continuing.

**Do this instead:** PRD Slice 1.5's design. `ErrAwaitingUserInput` sentinel + `aura.paused_states` row + `Event{Escalate=true}`. Multi-pause FIFO ordered by priority. Total: ~250 LOC across 3 files.

---

## Integration Points

### External Services

| Service | Integration Pattern | Notes |
|---------|---------------------|-------|
| OpenRouter (LLM) | HTTPS OpenAI-compat SSE stream via `openai_compat.Client`. DeepSeek-V4 Flash :exacto default. | 80% cache savings −63% cost verified 2026-05-27. Parses `usage.prompt_cache_hit_tokens` + `usage.cost` (OpenRouter extension). |
| Anthropic direct (LLM) | Same `openai_compat.Client` (OpenAI-compat shape). `cache_control: ephemeral` injection on system + tools block via PromptBuilder. | Currently second-class — used only if/when needed. PRD keeps `cache_anthropic.go` as no-op-in-practice to avoid future churn. |
| Telegram (channel) | Long polling via telebot.v4. In-process bot subscribes to `agui.Fanout`. HITL via InlineKeyboardMarkup (approval/choice) or ForceReply (clarification). | Throttle 1500ms status / 500ms content / 1000ms chat. 429 backoff exponential up to 30s. |
| SearXNG (search) | HTTP client to local SearXNG container. SSRF-defended `safeDialContext` refuses loopback/private IPs for `web_fetch`. | Slice 5 atomic — local container, no API key. |
| skills.sh (catalog) | HTTP fetch + regex parser. Install via `npx skills add <source> --agent claude-code -y --ignore-scripts` subprocess. | `--ignore-scripts` blocks post-install supply-chain risk. Post-install `ParseSkill()` re-validation. |
| Docker daemon (sandbox) | HTTP client to docker socket-mounted sidecar (`aura-sandbox`). Seccomp + ulimit + non-root user + network-deny. 2b adds workspace mount + iptables network allowlist. | Sidecar materials at `sandbox/` repo root (NOT internal/sandbox — that's the Go client). |
| Neo4j (knowledge) | Via `mcp-neo4j-cypher` MCP stdio subprocess (Python `mcp` package). NEVER via native Go driver. | LLM and Go code use the same MCP shim for Cypher access. Migrations under `internal/knowledge/migrations/*.cypher`, audit row in Postgres `aura.knowledge_migrations`. |
| Postgres | `pgxpool.Pool` via `internal/db/db.go`. golang-migrate with embed.FS. sqlc-generated code committed under `internal/db/sqlc/`. | Schema `aura.*` (not `public`). 15 migrations 0001-0014 planned. |
| markitdown (document conversion) | HTTP client to `markitdown` sidecar. Tiered sync (≤5MB) / async (5-50MB). | Slice 9c — needed for Telegram document attachments + Slice 11 ingestion. |
| Gemma 4 multimodal (voice/photo) | HTTP client to `aura-llama-multimodal` sidecar (port 8082). STT for voice, vision for photo. | Slice 9c. Single Gemma 4 E4B model serves both. |
| vLLM + LMCache (local LLM, v2) | HTTP client OpenAI-compat to `aura-vllm-chat` sidecar. LMCache disk-tier (50 GB chunk_size 256). | Slice 13, v2-gated. LLMRouter routes between remote/local based on prefer_local/offline/cost. |

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| Channels ↔ AG-UI gateway | Channels subscribe to `agui.Fanout` (in-process) OR poll `GET /threads/<id>/messages` (HTTP) | Channels NEVER reach into `internal/agent/`. |
| AG-UI gateway ↔ Runtime | `Translate(seq iter.Seq2[*agent.Event, error])` — pure function over runtime stream | `internal/agui` imports `internal/agent` (one-way). Reverse is forbidden. |
| Runtime ↔ Tools | `tools.Registry.Get(name).Execute(ctx, args)` in `LlmAgent.runTool` | Tools live in `internal/agent/tools/` — same module as runtime, but cleanly separated by file. |
| Tools ↔ Capabilities (sandbox/web/memory/...) | Tools import capability packages (`internal/sandbox`, `internal/web`, `internal/memory`). Capability packages NEVER import `internal/agent/tools` or `internal/agent`. | Strict downstream direction. Static analysis enforceable. |
| Capabilities ↔ Persistence | Each capability package owns its store interaction. Sandbox owns `aura.sandbox_sessions`. Memory owns Neo4j + `aura.ingest_audit`. Skills own `aura.skill_audit` + `~/.aura/skills/`. | `internal/conversations/store.go` is the only one writing `aura.conversation_turns`. |
| Workflow agents ↔ Sub-agents | `ctx.Agent.SubAgents()` returns []Agent. Workflow agent ranges over `sub.Run(ctx)`. Escalation propagates via `Event{Actions.Escalate=true}`. | InvocationContext is the cross-cutting state carrier — composed once at top-level, propagated. |
| Scheduler handlers ↔ Runtime | Each `cron.handlers.X` is an `agent.Agent` impl. Scheduler tick wraps in InvocationContext + ranges over Run. | Handler doesn't know it's running on a schedule — same interface as user-initiated agent runs. |
| Skills ↔ Sandbox 2b | Slice 7e snippet skills `Execute` via `internal/sandbox.Sessions.GetOrCreate(conv_id).Run(lang, code)` | Snippet skill is itself an `agent.Agent` virtual impl whose Run() invokes the sandbox. |

---

## Module Boundary Recommendations Aligned with Agent Interface + Capability Overlays

The PRD's planned `internal/` layout is correct. Reproducing the boundary rules as concise enforceable constraints:

1. **`internal/agent/` is the runtime root.** Defines `Agent`, `Event`, `Actions`, `InvocationContext`. Imports `internal/llm`, `internal/conversations` (for write hook), `internal/identity` (for capability check at tool dispatch). NEVER imports `internal/channels`, `internal/agui`, or any capability package other than `internal/agent/tools`.

2. **`internal/agent/tools/` is the tool registry root.** Tools import their respective capability packages (e.g., `tools/execute.go` imports `internal/sandbox`). Capability packages NEVER import `internal/agent/tools`.

3. **`internal/agent/workflow/` is the composition root.** Pure logic over `agent.Agent`. No external imports beyond stdlib + `internal/agent`.

4. **`internal/agui/` is the transport gateway.** Imports `internal/agent` to consume the event stream. NEVER imported by `internal/agent`. NEVER imports any capability package.

5. **`internal/channels/` is the client layer.** Imports `internal/agui` to subscribe to fanout. NEVER imports `internal/agent` directly (always goes through AG-UI translation). NEVER imports any capability package.

6. **Each capability package (`internal/sandbox`, `internal/web`, `internal/skills`, `internal/cron`, `internal/memory`, `internal/onboarding`) is self-contained.** Owns its config (in-package), its store interaction, its types. May implement `agent.Agent` (so imports `internal/agent` for interface). NEVER imports another capability package directly (cross-capability composition goes through workflow agents at the runtime layer).

7. **`internal/db` owns Postgres exclusively.** Provides `pgxpool.Pool` + sqlc-generated queries via `internal/db/sqlc`. Capability packages call sqlc functions; they don't run raw SQL.

8. **`internal/knowledge` owns Neo4j exclusively.** Provides MCP-neo4j-cypher subprocess wrapper. Capability packages call `knowledge.Client.Cypher(...)`; they don't speak Bolt directly.

9. **`internal/config` is composite root only.** Per-subsystem configs live in their packages. Adding a field to `config.Config` for a new subsystem is forbidden — create `internal/<subsystem>/config.go` instead.

10. **`internal/scoring` is the Risk-Based chokepoint.** Two functions, one file. Shared by Slice 6 cron + Slice 7 skills. No domain logic — just the deterministic kind→tier map + modifiers.

11. **`internal/identity` is the capability check chokepoint.** `HasCapability(id, cap) bool` is the only authorization primitive. Called by tools (at dispatch) and CLI commands (at parse).

**Enforceable via:** Go's built-in import cycle detection catches the obvious ones. Static analysis lint rule for "no `internal/channels` imports under `internal/agent/`" is a simple `grep` in CI. Static analysis lint for "no `internal/agent/tools` imports outside `internal/agent/`" is similar.

---

## Sources

### Primary references (Aura PRD + skeleton)

- D:/Aura/prd.md (4401 LOC) — single source of truth, 14 slices
- D:/Aura/.planning/codebase/ARCHITECTURE.md — current skeleton + target state architecture map
- D:/Aura/.planning/codebase/STRUCTURE.md — current + target package layout
- D:/Aura/.planning/PROJECT.md — active requirements grouped by infra/core/capabilities/transport
- D:/Aura/CLAUDE.md — project guidance + GSD tooling + skills installed

### Reference systems studied

- [Google ADK-go agent package](https://pkg.go.dev/google.golang.org/adk/agent) — Agent interface, InvocationContext, iter.Seq2 Event streaming, workflow agent patterns (the explicit pattern source Aura steals from)
- [ADK Context documentation](https://google.github.io/adk-docs/context/) — InvocationContext semantics
- [ADK Custom Agents](https://google.github.io/adk-docs/agents/custom-agents/) — sub-agent invocation pattern (yield events from sub.Run)
- [Anthropic Claude Agent SDK overview](https://code.claude.com/docs/en/agent-sdk/overview) — tool dispatch + memory primitives
- [Anthropic advanced tool use](https://www.anthropic.com/engineering/advanced-tool-use) — `defer_loading` + `tool_search` pattern shipped Nov 2025
- [Anthropic Memory tool](https://platform.claude.com/docs/en/agents-and-tools/tool-use/memory-tool) — just-in-time context retrieval pattern
- [Anthropic claude-agent-sdk-python #525](https://github.com/anthropics/claude-agent-sdk-python/issues/525) — Tool Search Tool and Deferred Loading feature request (confirms pattern is current SOTA)
- [OpenAI Agents SDK](https://openai.github.io/openai-agents-python/) — Agents/Tools/Handoffs/Guardrails/Sessions primitives
- [OpenAI Agents SDK Tracing](https://openai.github.io/openai-agents-python/tracing/) — built-in tracing as cross-cutting concern
- [Letta MemGPT docs](https://docs.letta.com/concepts/letta/) — 3-tier memory architecture (Core / Recall / Archival)
- [Letta MemGPT 2025 paper](https://informationmatters.org/2025/10/memgpt-engineering-semantic-memory-through-adaptive-retention-and-context-summarization/) — virtual context management via paging
- [LangGraph Persistence](https://docs.langchain.com/oss/python/langgraph/persistence) — PostgresSaver checkpointer + multi-thread architecture
- [LangGraph PostgresSaver guide](https://ai.plainenglish.io/never-lose-ai-memory-in-production-postgressaver-for-langgraph-2f165c3688a0) — read-execute-write cycle, thread isolation
- [OpenHands Runtime Architecture](https://docs.openhands.dev/openhands/usage/architecture/runtime) — Docker Runtime + agent loop
- [OpenHands V0 → V1 transition](https://deepwiki.com/OpenHands/OpenHands) — moving from "mandatory Docker" to "optional sandboxing"
- [OpenHands V1 SDK paper](https://arxiv.org/pdf/2511.03690) — composable extensible foundation for production agents
- [Microsoft Agent Framework v1.0 announcement](https://devblogs.microsoft.com/agent-framework/microsoft-agent-framework-version-1-0/) — orchestration patterns + handoffs
- [Microsoft Agent Framework Handoff](https://learn.microsoft.com/en-us/agent-framework/workflows/orchestrations/handoff) — handoff orchestration semantics
- [AG-UI Events specification](https://docs.ag-ui.com/concepts/events) — full event type catalog
- [AG-UI 17 event types deep dive](https://www.copilotkit.ai/blog/master-the-17-ag-ui-event-types-for-building-agents-the-right-way) — categories (lifecycle, text, tool, state, special)
- [AG-UI Build Guide](https://docs.ag-ui.com/quickstart/build) — SSE transport pattern
- [Prompt caching 2026 guide](https://futureagi.com/blog/understanding-prompt-caching-for-faster-ai-responses/) — provider comparison (OpenAI/Anthropic/DeepSeek/Gemini)
- [OpenRouter Prompt Caching](https://openrouter.ai/docs/guides/best-practices/prompt-caching) — OpenRouter pass-through behavior verified
- [Prompt Caching with OpenAI/Anthropic/Google](https://www.prompthub.us/blog/prompt-caching-with-openai-anthropic-and-google-models) — cache_control vs automatic semantics
- [GraphRAG Neo4j Labs](https://neo4j.com/labs/genai-ecosystem/graphrag/) — pipeline architecture
- [Microsoft GraphRAG with Neo4j + LangChain](https://neo4j.com/blog/developer/global-graphrag-neo4j-langchain/) — global GraphRAG implementation
- [GraphRAG implementation guide 2026](https://markaicode.com/graphrag-knowledge-graph-enhanced-retrieval-guide/) — Documents/Chunks/Entities/Communities + Leiden pattern
- [LangGraph From Zero to Production: Persistence & Memory](https://medium.com/@puttt.spl/langgraph-from-zero-to-production-part-2-persistence-memory-f28b851b66f5) — multi-thread checkpointer in production

### Aura memory references (project-internal context)

- `reference_openrouter_provider_capabilities_2026-05-27` — DeepSeek V4 Flash 80% cache, OpenAI-wire shape, OpenRouter cost field
- `reference_aura_cache_poisoning_sites_2026-05-27` — 6 cache invalidation sites in pre-rewrite code (referenced by Slice 4 design)
- `project_neo4j_spike_2026-05-27` — structured graph 1.6-1.8s vs blob+LLM 27-75s (15-45× faster)
- `project_graph_memory_core_strategy` — Neo4j wins over wiki markdown for knowledge core
- `feedback_embedding_backend_stays_mistral` — Neo4j HNSW Lucene validated 22-30ms p95, recall@5 5/5
- `feedback_aura_is_platform_shaped` — substrate domain-neutral, "personal" is overlay
- `feedback_minipc_cpu_budget` — target hardware constraints

---

*Architecture research for: Go-native agentic AI substrate*
*Researched: 2026-05-29*
