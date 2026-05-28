<!-- refreshed: 2026-05-28 -->
# Architecture

**Analysis Date:** 2026-05-28

> This document carries two parallel truths the reader MUST distinguish:
>
> 1. **CURRENT STATE (commit `af4ca65c`)** — a 543 LOC Go skeleton with stubs for every concern. What you can actually `go build` today.
> 2. **TARGET STATE (per `prd.md`, 4401 lines)** — the architecture the 14+ atomic slices (0.5/0.7/0.9/1/1.5/1.7/1.8/2a/2b/3/4/5/6/7+7e/8/9a/9b/9c/10/11a-e/13a-b) will deliver.
>
> Every section below labels which truth it describes. Do not conflate them when planning a phase.

## System Overview — Current State (skeleton, today)

```text
┌─────────────────────────────────────────────────────────────┐
│                    cmd/aura/main.go                          │
│   `aura tools` (works) │ `aura chat <msg>` (stub LLM)       │
│   `aura serve` / `aura shell` → prints "TODO"                │
└────────────────────────────┬────────────────────────────────┘
                             │ (in-process)
                             ▼
┌─────────────────────────────────────────────────────────────┐
│              internal/agent (Loop, 131 LOC)                  │
│   Loop.Turn(ctx, userMsg) → assistant text                   │
│   MaxSteps=8, appends to Messages slice                      │
│   `internal/agent/loop.go`                                   │
└──────┬─────────────────────────────┬────────────────────────┘
       │                             │
       ▼                             ▼
┌──────────────────────┐   ┌──────────────────────────────────┐
│ internal/llm         │   │ internal/agent/tools             │
│ Client interface     │   │ Registry + Tool interface        │
│ (Chunk, ToolCall,    │   │ - TextResponse (non-deferred)    │
│  ToolDef, Request)   │   │ - ToolSearch (non-deferred)      │
│ `client.go` 78 LOC   │   │ `spec.go`, `manifest.go`         │
│ NO impl yet, just    │   │ Deferred flag pattern wired      │
│ contract + stubClient│   │ but no big tools registered      │
│ in main.go           │   │                                   │
└──────────────────────┘   └──────────────────────────────────┘
       │                             │
       ▼                             ▼
┌──────────────────────┐   ┌──────────────────────────────────┐
│ internal/sandbox     │   │ internal/swarm                   │
│ Runner interface     │   │ Coordinator interface            │
│ Stub returns "not    │   │ Stub returns "not implemented"   │
│ yet implemented"     │   │ MaxSpawnDepth=3 constant defined │
│ `sandbox.go` 36 LOC  │   │ `swarm.go` 42 LOC                │
└──────────────────────┘   └──────────────────────────────────┘
```

**No external dependencies wired today.** No Postgres, no Neo4j, no compose.yaml file in repo (despite CLAUDE.md and prd.md referencing `compose.yaml`). No HTTP client, no SSE parser, no docker integration. Pure Go module `github.com/chetto1983/aura`, `go 1.23`, zero `require` blocks.

## System Overview — Target State (per prd.md)

```text
┌──────────────────────────────────────────────────────────────────────────┐
│                          CLIENT LAYER                                     │
│  ┌─────────────────┐ ┌──────────────────┐ ┌────────────────────────────┐ │
│  │ CLI channel     │ │ Telegram channel │ │ AG-UI client (HTTP/SSE)    │ │
│  │ (debug-only)    │ │ (main user-      │ │ external UI (Dojo / web    │ │
│  │                 │ │  facing)         │ │ frontend / future)         │ │
│  │ internal/       │ │ internal/        │ │ POST /agent/run            │ │
│  │ channels/cli/   │ │ channels/        │ │ GET /threads/<id>/messages │ │
│  │                 │ │ telegram/        │ │                            │ │
│  └────────┬────────┘ └─────────┬────────┘ └─────────────┬──────────────┘ │
│           │                    │ in-process fanout       │ HTTP/SSE       │
└───────────┼────────────────────┼─────────────────────────┼────────────────┘
            │                    │                         │
            ▼                    ▼                         ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                       AG-UI GATEWAY (Slice 8)                             │
│   internal/agui/{server,translator,fanout,client,types}.go                │
│   Consumes iter.Seq2[*agent.Event, error] → emits ~25 AG-UI event types   │
│   (RUN_STARTED, TEXT_MESSAGE_*, TOOL_CALL_*, STATE_DELTA, REASONING_*,    │
│    RUN_FINISHED with outcome.interrupts[] from PausedState[])             │
└────────────────────────────────────┬─────────────────────────────────────┘
                                     │
                                     ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                  RUNTIME LAYER — Agent interface (Slice 0.9)              │
│   internal/agent/agent.go:                                                │
│     type Agent interface {                                                │
│       Name() string                                                       │
│       Description() string                                                │
│       Run(InvocationContext) iter.Seq2[*Event, error]   // Go 1.23+       │
│       SubAgents() []Agent                                                 │
│       FindAgent(name string) Agent                                        │
│     }                                                                     │
│                                                                           │
│   InvocationContext carries: Ctx, Agent (self), SessionID (=conv_id),     │
│     IdentityID, Branch, SessionStore (PG), GraphStore (Neo4j MCP),        │
│     LLMClient (or LLMRouter Slice 13)                                     │
│                                                                           │
│   Built-in implementations:                                               │
│   ┌──────────────────┬───────────────────┬─────────────────────────────┐  │
│   │ LlmAgent         │ Workflow agents   │ Domain agents               │  │
│   │ (Slice 1)        │ (Slice 0.9)       │ (cross-slice)               │  │
│   │                  │                   │                             │  │
│   │ Streaming LLM +  │ Sequential        │ ReminderAgent  (Slice 6)    │  │
│   │ tool dispatch +  │ Loop (maxIter)    │ AgentJobAgent  (Slice 6)    │  │
│   │ ToolResult       │ Parallel (errgrp  │ InterviewStepAgent (Sl 10)  │  │
│   │ preview+sidecar  │  +backpressure)   │ SummaryConfirmAgent (Sl 10) │  │
│   │ MaxSteps=8       │                   │ Skill virtual agent (Sl 7)  │  │
│   │ runTool dispatch │                   │ Swarm worker = LlmAgent (3) │  │
│   └──────────────────┴───────────────────┴─────────────────────────────┘  │
│                                                                           │
│   Termination model: Event{Actions.Escalate=true} bubbles up via          │
│   `return` after every `yield` check in workflow agents.                  │
└─────┬────────────────────────────────────────────────────────────────────┘
      │
      ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                      CAPABILITIES LAYER                                   │
│   Tool registry (deferred-tool pattern), one Agent per domain capability │
│   ┌────────────────┐ ┌──────────────┐ ┌──────────────┐ ┌───────────────┐ │
│   │ Sandbox        │ │ Swarm        │ │ Web          │ │ Scheduler     │ │
│   │ (Slice 2a/2b)  │ │ (Slice 3)    │ │ (Slice 5)    │ │ (Slice 6)     │ │
│   │ Docker sidecar │ │ Coordinator  │ │ SearXNG +    │ │ task tool +   │ │
│   │ exec python/sh │ │ tier model   │ │ web_fetch +  │ │ cron tick +   │ │
│   │ stateless 2a / │ │ MaxSpawnDpth │ │ readability  │ │ Risk-Based    │ │
│   │ session 2b     │ │ =3 + bus     │ │ +HTML→MD     │ │ governance    │ │
│   │ workspace mnt  │ │ +DM-by-ID    │ │ SSRF defense │ │ + ActionRoutr │ │
│   │ network allow  │ │ swarm worker │ │              │ │               │ │
│   │ internal/      │ │ = LlmAgent   │ │ internal/    │ │ internal/cron │ │
│   │ sandbox/       │ │ internal/    │ │ web/         │ │ + Handler =   │ │
│   │                │ │ swarm/       │ │              │ │ agent.Agent   │ │
│   └────────────────┘ └──────────────┘ └──────────────┘ └───────────────┘ │
│   ┌────────────────┐ ┌──────────────┐ ┌──────────────┐ ┌───────────────┐ │
│   │ Skills         │ │ Memory       │ │ KV cache     │ │ Onboarding    │ │
│   │ (Slice 7a-e)   │ │ (Slice 11)   │ │ (Slice 4)    │ │ (Slice 10)    │ │
│   │ SKILL.md tree  │ │ Letta 3-tier:│ │ PromptBuildr │ │ LoopAgent[    │ │
│   │ ~/.aura/skills │ │ Core (Agent  │ │ stable prefix│ │ InterviewStep │ │
│   │ create/install │ │ .md+Insight) │ │ provider-    │ │ Agent] +      │ │
│   │ /update/delete │ │ Recall (PG   │ │ aware cache  │ │ SummaryConfrm │ │
│   │ via ask_user   │ │ turns) +     │ │ control      │ │ Agent_md gen  │ │
│   │ governance     │ │ Archival     │ │ injection    │ │ filesystem    │ │
│   │ Voyager pattern│ │ (Neo4j)      │ │ for          │ │ ~/.aura/      │ │
│   │ +snippet 7e    │ │ GraphRAG +   │ │ DeepSeek /   │ │ agents/<id>/  │ │
│   │ +pattern anlyz │ │ mem0 2-fase  │ │ Anthropic    │ │ Agent.md      │ │
│   │ +TTL 90gg      │ │ +Memify      │ │              │ │ injected as   │ │
│   │ +archived      │ │ +Leiden      │ │              │ │ 2nd system    │ │
│   │                │ │ communities  │ │              │ │ message       │ │
│   └────────────────┘ └──────────────┘ └──────────────┘ └───────────────┘ │
│   Pause/Resume primitive (Slice 1.5): ask_user returns sentinel          │
│   ErrAwaitingUserInput → Loop emits Event{Escalate} + PausedState rows.  │
└─────┬────────────────────────────────────────────────────────────────────┘
      │
      ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                       PERSISTENCE LAYER                                   │
│   Three stores, semantic-strict (no crossover):                          │
│   ┌────────────────┐ ┌────────────────────┐ ┌──────────────────────────┐ │
│   │ PostgreSQL 17  │ │ Neo4j 5 Community  │ │ Filesystem               │ │
│   │ via sqlc+pgx   │ │ via mcp-neo4j-     │ │ ~/.aura/skills/active/   │ │
│   │ Schema `aura`  │ │ cypher (stdio MCP) │ │ ~/.aura/agents/<id>/     │ │
│   │ golang-migrate │ │ APOC + GDS +       │ │ $AURA_RUN_DIR/           │ │
│   │                │ │ HNSW vector idx    │ │   conversations/<id>/    │ │
│   │ Application    │ │ 768d native        │ │     <tool_call>.result   │ │
│   │ state ONLY:    │ │                    │ │     <seq>.content        │ │
│   │ - conversations│ │ Knowledge + vector │ │     workspace/  (2b)     │ │
│   │ - turns        │ │ ONLY:              │ │   tmp/  (24h TTL)        │ │
│   │ - paused_states│ │ - :Document        │ │                          │ │
│   │ - identities   │ │ - :Chunk(emb 768)  │ │ Operational artifacts    │ │
│   │ - capability_  │ │ - :Entity          │ │ ONLY. No knowledge.      │ │
│   │   grants       │ │ - :Community       │ │                          │ │
│   │ - scheduler_   │ │ - :AgentEpisode    │ │                          │ │
│   │   tasks        │ │ - :AgentInsight    │ │                          │ │
│   │ - skill_audit  │ │ - :UserConversation│ │                          │ │
│   │ - knowledge_   │ │ - :UserSnippet     │ │                          │ │
│   │   migrations   │ │                    │ │                          │ │
│   │ - telegram_*   │ │ Cypher migrations  │ │                          │ │
│   │ - sandbox_     │ │ in internal/       │ │                          │ │
│   │   sessions     │ │ knowledge/         │ │                          │ │
│   │ - local_llm_*  │ │ migrations/*.cypher│ │                          │ │
│   │ - profile_     │ │ audit row in PG    │ │                          │ │
│   │   audit        │ │ aura.knowledge_    │ │                          │ │
│   │ - ingest_audit │ │ migrations         │ │                          │ │
│   └────────────────┘ └────────────────────┘ └──────────────────────────┘ │
│   ┌────────────────────────────────────────────────────────────────────┐ │
│   │ Sidecars (containers, docker compose):                             │ │
│   │ - aura-postgres        (Postgres 17)                               │ │
│   │ - aura-neo4j           (Neo4j 5 community + APOC + GDS)            │ │
│   │ - aura-llama-embed     (embeddinggemma, 768d, port 8081)           │ │
│   │ - aura-sandbox         (Python+shell sidecar, port 18901)          │ │
│   │ - searxng              (Slice 5)                                   │ │
│   │ - aura-llama-multimodal (Gemma 4 E4B vision+STT, port 8082)        │ │
│   │ - markitdown           (document→markdown, Slice 9c)               │ │
│   │ - aura-vllm-chat       (Slice 13, local LLM + LMCache disk-tier)   │ │
│   └────────────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────────────┘
```

## Component Responsibilities — Current State

| Component | Responsibility | File |
|-----------|----------------|------|
| `main` entry | Sub-command dispatch (`tools`, `chat`, `serve`, `shell`) | `cmd/aura/main.go` |
| `agent.Loop` | One conversation, MaxSteps=8 loop, tool dispatch, history append | `internal/agent/loop.go` |
| `llm.Client` | Provider-neutral streaming contract (no impl yet) | `internal/llm/client.go` |
| `tools.Registry` | Tool map, manifest render with deferred flag handling | `internal/agent/tools/spec.go`, `internal/agent/tools/manifest.go` |
| `tools.TextResponse` | Terminal tool, emits final reply | `internal/agent/tools/text_response.go` |
| `tools.ToolSearch` | Built-in hook to load deferred tool full specs | `internal/agent/tools/search.go` |
| `sandbox.Stub` | Placeholder returning "not yet implemented" for RunPython/RunShell | `internal/sandbox/sandbox.go` |
| `swarm.Stub` | Placeholder returning "not yet implemented" for Spawn/Talk/Join | `internal/swarm/swarm.go` |
| `stubClient` (main.go) | Canned `text_response` tool-call so `aura chat hello` runs without a real LLM | `cmd/aura/main.go:74` |

## Component Responsibilities — Target State

| Component | Responsibility | File (planned) |
|-----------|----------------|----------------|
| `agent.Agent` interface | Unified runtime for LLM/workflow/scheduler/skill/swarm agents | `internal/agent/agent.go` |
| `agent.LlmAgent` | LLM-driven Agent, replaces today's `Loop`. `Run() iter.Seq2[*Event, error]` | `internal/agent/llm_agent.go` |
| `agent.workflow.SequentialAgent` | Iterate sub-agents in order, escalate-aware | `internal/agent/workflow/sequential.go` |
| `agent.workflow.LoopAgent` | Repeat sub-agents N times or until Escalate | `internal/agent/workflow/loop.go` |
| `agent.workflow.ParallelAgent` | errgroup + ackChan backpressure concurrent sub-agents | `internal/agent/workflow/parallel.go` |
| `agent.Event` / `Actions` | Streaming unit. Actions.Escalate propagates termination | `internal/agent/event.go` |
| `llm.openai_compat.Client` | OpenAI-compatible SSE streaming client (DeepSeek/OpenRouter/Anthropic) | `internal/llm/openai_compat/client.go` |
| `llm.LLMRouter` | Routes between remote and local based on prefer_local/offline/cost | `internal/llm/router.go` |
| `tools.ActionRouter` | Dispatch shared by Slice 6/7 tools with `action` enum | `internal/agent/tools/action.go` |
| `tools.AskUser` | Returns sentinel `ErrAwaitingUserInput`, drives Loop pause state machine | `internal/agent/tools/ask_user.go` |
| `tools.Execute` | Sandbox bridge, `Deferred=true`, optional `session_id` | `internal/agent/tools/execute.go` |
| `tools.ToolResult` | Preview + persist sidecar pattern (cap `AURA_CONTEXT_PREVIEW_CAP_BYTES`) | `internal/agent/tools/result.go` |
| `sandbox.DockerRunner` | HTTP client → Docker sidecar; seccomp + ulimit + net-deny | `internal/sandbox/docker.go` |
| `sandbox.SessionManager` | Session-bound containers per conversation_id (Slice 2b) | `internal/sandbox/sessions.go` |
| `swarm.Coordinator` | Spawn/Talk/Join, tier model (chat/reasoning/worker), bus + DM-by-ID, MAX_SPAWN_DEPTH=3 | `internal/swarm/coordinator.go` |
| `conversations.Store` | sqlc adapter for conversations + turns, auto-title, microcompact | `internal/conversations/store.go` |
| `identity.Store` | identities + capability_grants, `HasCapability(id, cap)` with `*` wildcard | `internal/identity/store.go` |
| `cron.Scheduler` | Tick loop, missed-run recovery, dispatch to `agent.Agent` handlers | `internal/cron/scheduler.go` |
| `cron.handlers.*` | Each TaskKind = one `agent.Agent` impl (reminder, agent_job, backups) | `internal/cron/handlers/<kind>.go` |
| `skills.Loader` | 4-way split: filesystem scan, YAML parser, cache, name validation | `internal/skills/loader/{filesystem,parser,cache,loader}.go` |
| `skills.Validator` / `SanitizeName` | Single chokepoint regex + path-traversal guard | `internal/skills/validator.go`, `internal/skills/paths.go` |
| `web.SearXNG` / `web.Fetcher` / `web.HTML` | SearXNG client + URL fetch + readability/markdown extractor | `internal/web/{searxng,fetcher,html}.go` |
| `agui.Translator` | Map `iter.Seq2[*Event, error]` → AG-UI events (~25 types) | `internal/agui/translator.go` |
| `agui.Server` | HTTP `POST /agent/run` (SSE) + `GET /threads/<id>/messages` | `internal/agui/server.go` |
| `agui.Fanout` | Distributes Event stream to N in-process subscribers (Telegram uses this) | `internal/agui/fanout.go` |
| `channels.Channel` interface | `Name()`, `Start(ctx, sub)`, `Stop()`, `IsHealthy()` | `internal/channels/channel.go` |
| `channels.cli` | CLI as a Channel (migrated from `cmd/aura/chat.go`) | `internal/channels/cli/cli.go` |
| `channels.telegram` | telebot.v4 bot, status pane pattern B, HITL keyboards, 8 MVP commands | `internal/channels/telegram/{bot,renderer,hitl,commands,onboarding}.go` |
| `setup.Server` | HTTP wizard on `127.0.0.1:9081`, QR/deep-link onboarding | `internal/setup/server.go` |
| `onboarding.Interview` | `LoopAgent[InterviewStepAgent]`, maxIter=8, escalation on "Conferma" | `internal/onboarding/interview.go` |
| `onboarding.Store` | Filesystem read/write for `Agent.md` + `preferences.json` + `metadata.json` + `changelog.md` | `internal/onboarding/store.go` |
| `onboarding.Injector` | Inject `Agent.md` as second system message (cache-friendly) | `internal/onboarding/injector.go` |
| `memory.ingest.Pipeline` | Cognify 6-stage: classify→permissions→chunk→entities→summary→embed | `internal/memory/ingest/pipeline.go` |
| `memory.graph.Community` | Leiden detection via Neo4j GDS, hierarchical clustering | `internal/memory/graph/community.go` |
| `memory.retrieval.Search` | Hybrid BM25 + HNSW + 1-hop, mem0 fusion, LLM rerank tier=worker | `internal/memory/retrieval/search.go` |
| `memory.agent.Journal` | Post-conv `:AgentEpisode` + cross-conv `:AgentInsight` analyzer | `internal/memory/agent/journal.go` |
| `knowledge.Client` | MCP-neo4j-cypher stdio subprocess wrapper, `Cypher(ctx, query, params)` | `internal/knowledge/client.go` |
| `scoring.Compute*Tier` | Hardcoded kind→tier mapping + modifiers (Risk-Based governance) | `internal/scoring/scoring.go` |
| `db.Open` | `pgxpool.Pool` open with config + ping at boot | `internal/db/db.go` |
| `config.Config` | Composite root `{LLM, DB, RunDir, ToolPreviewCap}` — per-subsystem configs live in their own package | `internal/config/config.go` |
| `prompt.Builder` (Slice 4) | KV-cache stable-prefix prompt assembly, provider-aware cache_control | `internal/llm/prompt/builder.go` (planned) |

## Pattern Overview

**Current state pattern:** Minimal in-process loop with interface stubs.
- One conversation = one `Loop` = one goroutine
- Messages slice is append-only, index 0 (system) is invariant
- Tool dispatch via Registry, `text_response` is the terminal that ends a turn
- Provider-neutral `llm.Client` contract is defined but no implementation exists

**Target state pattern:** Layered agent runtime over pluggable channels.
- **Unified Agent abstraction (Slice 0.9)**: every domain that "runs a thing" implements `agent.Agent`. One runtime, N implementations. Pattern *rubato* (not imported) from `google/adk-go` v1.3.0.
- **Streaming via `iter.Seq2[*Event, error]`** (Go 1.23+ range-over-func). No custom channels, no callback emitters.
- **Termination via `Event.Actions.Escalate`**: bubbles up through Sequential/Loop/Parallel via the `return` after every `yield` check.
- **Deferred-tool pattern**: tools with long Description/complex schema set `Spec.Deferred=true`. Default manifest shows only Name + Summary. Model loads full spec on demand via `tool_search`. Protects KV cache.
- **ToolResult preview+persist**: results > `AURA_CONTEXT_PREVIEW_CAP_BYTES` (default 2048) spill to `$AURA_RUN_DIR/conversations/<conv_id>/<tool_call_id>.result`. History keeps only preview + footer pointer.
- **Pause/Resume sentinel**: `ask_user` returns `ErrAwaitingUserInput{Question, Options, Kind, Priority, ToolCallID, ResumeContext}`. Loop intercepts, persists `PausedState` to `aura.paused_states`, emits `Event{Escalate=true}`. Multi-pause FIFO ordered `(priority DESC, created_at ASC)`.
- **Risk-Based Hybrid C governance** (Slice 6 + 7): system computes RiskTier deterministically (SAFE/NORMAL/RISKY/DESTRUCTIVE), agent decides whether to gate via `ask_user`. RISKY+ mutations parked in `pending_approval` state regardless; user can approve via CLI.
- **Three stores, semantic-strict**: PG = application state, Neo4j = knowledge + vectors (via MCP only, no native Go adapter), filesystem = operational artifacts. No knowledge in PG. No tasks in Neo4j. No vector index outside Neo4j.

**Key Characteristics:**
- Single-binary Go application (`cmd/aura`)
- Container-isolated sidecars for risky/expensive subsystems (sandbox, embed, Neo4j, vLLM)
- KV-cache discipline owns the prompt prefix: `messages[0]` is byte-identical across turns
- Stable-prefix order: main system prompt → `Agent.md` (per-identity) → `:AgentInsight` (top-K, Slice 11)
- All HTTP servers default-bind `127.0.0.1` (setup wizard 9081, AG-UI 9080, sandbox 18901)
- All MCP/sidecar processes lifecycle-coupled to `aura serve`

## Layers — Target

**Client Layer:**
- Purpose: User-facing transport (CLI, Telegram, AG-UI)
- Location: `cmd/aura/`, `internal/channels/cli/`, `internal/channels/telegram/`, external AG-UI clients
- Contains: subcommand routing, channel adapters, Telegram bot, AG-UI SSE server
- Depends on: Runtime layer (consumes `iter.Seq2[*Event, error]`)
- Used by: Human user, external UIs (Dojo, future web)

**Runtime Layer:**
- Purpose: `agent.Agent` abstraction + streaming events + termination model
- Location: `internal/agent/`, `internal/agent/workflow/`
- Contains: `Agent` interface, `InvocationContext`, `Event`/`Actions`, `LlmAgent`, workflow agents
- Depends on: Persistence (SessionStore, GraphStore), LLM (Client / LLMRouter), Tools
- Used by: All channels, AG-UI gateway, cron handlers, swarm coordinator

**Capabilities Layer:**
- Purpose: Domain capabilities exposed as tools and/or `Agent` impls
- Location: `internal/sandbox/`, `internal/swarm/`, `internal/web/`, `internal/cron/`, `internal/skills/`, `internal/memory/`, `internal/onboarding/`, `internal/llm/prompt/` (KV builder)
- Contains: Docker runner, swarm coordinator, web tools, scheduler, skill loader, memory ingest/retrieval, onboarding interview
- Depends on: Runtime layer (most expose `Agent` impls), Persistence (PG + Neo4j + filesystem)
- Used by: Runtime layer (tool dispatch, sub-agent spawning)

**Persistence Layer:**
- Purpose: Durable state in three semantic-strict stores
- Location: `internal/db/`, `internal/knowledge/`, `$AURA_RUN_DIR/`
- Contains: pgx pool + sqlc-generated Queries, MCP-neo4j stdio client, filesystem helpers
- Depends on: External services (Postgres container, Neo4j container, OS filesystem)
- Used by: Capabilities layer + Runtime layer (SessionStore for `InvocationContext`)

## Data Flow — Current State

### Primary Request Path (today)

1. User runs `aura chat "hello"` (`cmd/aura/main.go:30`)
2. `buildRegistry()` registers `TextResponse` + `ToolSearch` (`cmd/aura/main.go:48`)
3. `agent.NewLoop(stubClient{}, ...)` seeds Messages with the system prompt (`cmd/aura/main.go:62`, `internal/agent/loop.go:36`)
4. `loop.Turn(ctx, "hello")` appends user message, calls `stubClient.Stream` (`internal/agent/loop.go:50`)
5. `stubClient` returns a single chunk with a canned `text_response` tool call (`cmd/aura/main.go:76`)
6. Loop dispatches the tool call via `runTool` → `TextResponse.Execute` returns the text (`internal/agent/loop.go:107`, `internal/agent/tools/text_response.go:35`)
7. Loop sees `tc.Function.Name == "text_response"` and returns the reply string (`internal/agent/loop.go:99`)
8. `main` prints the reply to stdout (`cmd/aura/main.go:68`)

**State Management:** Entirely in-memory `Loop.Messages` slice. Nothing persists across runs.

## Data Flow — Target State

### Primary Request Path (planned, Telegram main case)

1. User sends Telegram message (text/voice/photo/document) to bot
2. `channels/telegram/bot.go` polling receives Update, dispatches to handler
3. For voice/photo: POST to `aura-llama-multimodal` (`channels/telegram/voice.go`, `channels/telegram/photo.go`) → text transcription/description
4. For document: POST to `markitdown` sidecar (`channels/telegram/documents.go`) → markdown text (≤5 MB sync, 5-50 MB async)
5. Text becomes user message; `Conversations.AppendTurn` writes `aura.conversation_turns` (`internal/conversations/store.go`)
6. `LlmAgent.Run(InvocationContext)` invoked; yields `iter.Seq2[*Event, error]` (`internal/agent/llm_agent.go`)
7. Prompt assembled by `PromptBuilder` (Slice 4): main system + `Agent.md` (Slice 10) + top-K `:AgentInsight` (Slice 11) + history (microcompact-evicted tool turns become pointer)
8. `LLMRouter.Route(ctx)` picks `remote` (OpenRouter) or `local` (vLLM) based on prefer_local/offline/cost (`internal/llm/router.go`)
9. SSE stream from provider → `Chunk` → accumulated tool-call deltas → `Event{LLMResponse{...}}` yielded
10. `agui.Translator` maps `*Event` to AG-UI events (TEXT_MESSAGE_*, TOOL_CALL_*, STATE_DELTA, etc.)
11. `agui.Fanout` distributes to in-process subscribers (Telegram `agui_subscriber.go`) + HTTP SSE clients
12. Tool calls dispatched via `Registry.Get(name).Execute(ctx, args)`; result wrapped in `ToolResult` (preview≤cap, sidecar otherwise)
13. If `text_response` invoked → final reply; `Conversations.AppendTurn` writes assistant turn + token totals
14. `Telegram renderer.go` edits status pane (Pattern B: 2 msg per turn) + sends content reply (markdown via `eekstunt/telegramify-markdown-go`)

### Pause/Resume Flow

1. Tool returns `ErrAwaitingUserInput{Question, Options, Kind, Priority, ResumeContext, ToolCallID}`
2. `LlmAgent.runTool` intercepts sentinel, builds `PausedState`, upserts to `aura.paused_states`
3. Yields `Event{Actions.Escalate=true, StateDelta:{pending: [...]}}`
4. AG-UI translator emits `RUN_FINISHED{outcome.type="interrupted", outcome.interrupts[]}`
5. Telegram renderer shows `InlineKeyboardMarkup` (kind=approval/choice) or `ForceReply` (kind=clarification)
6. User taps button → callback `resume:<token>:<idx>` → `Loop.Resume(token, answer)`
7. Loop appends `RoleTool{ToolCallID, Content: answer}` and continues
8. Multi-pause coalesce: multiple `ask_user` in same turn → multiple `PausedState` rows, Loop pauses until ALL resumed (FIFO ordered)

### Ingestion Flow (Slice 11)

1. `aura ingest /path/file.pdf` OR Telegram document attach
2. `markitdown` sidecar converts → markdown
3. `memory.ingest.Chunker` recursive semantic split (default 512 tok, overlap 64)
4. `memory.ingest.Embedder` batch 32/call to `aura-llama-embed` (768d native)
5. `memory.ingest.EntityExtractor` mem0 2-fase: LLM tier=reasoning extracts candidates batch 10 chunks, conflict-detect via fuzzy + embedding similarity > 0.92
6. Neo4j upsert via MCP: `:Document` + `:Chunk(embedding)` + `:Entity` + `:HAS_CHUNK` + `:MENTIONS`
7. `ingest_audit` row in Postgres (parity with `skill_audit`)
8. Background 24h: `memory.graph.Community` runs Leiden via GDS, summarizes per community via LLM tier=worker, persists `:Community` + `:CONTAINS` hierarchical

### Scheduler Tick Flow (Slice 6)

1. `cron.Scheduler` tick loop wakes (default 5s)
2. `Queries.DueTasks(ctx)` returns rows with `next_fire_at <= now() AND status='active'`
3. For each task: `handler := registry[task.Kind]` (handler is an `agent.Agent`)
4. `handler.Run(invocationCtx)` yields events; forwarded to `Notifier` + audit log
5. On completion: `Queries.MarkFired(task_id, next_fire_at)` reschedules
6. Risk-Based: pre-fire `scoring.ComputeTaskTier(args)` → RISKY+ parks in `pending_approval` instead of running

**State Management:**
- Persistent conversation state in Postgres (`aura.conversations` + `aura.conversation_turns`)
- Loop state reconstructed via `LoadHistory(conv_id)` on resume
- Sidecar files in `$AURA_RUN_DIR/conversations/<conv_id>/` for tool results > cap and content > 64 KiB
- Microcompact L1 (Slice 1.8): tool turns older than 10 seqs replaced with `read_tool_output(X)` pointer
- Budget L2: hard cap = `ContextWindow - max(MaxOutputTokens, 20K) - 13K` (Claude Code style)

## Key Abstractions

**`agent.Agent` (Slice 0.9):**
- Purpose: Unified runtime contract for any "thing that runs and emits events"
- Examples (planned): `internal/agent/llm_agent.go` (LLM-driven), `internal/agent/workflow/{sequential,loop,parallel}.go` (composition), `internal/cron/handlers/reminder.go` (timer-driven), `internal/onboarding/steps.go` (interview steps), swarm worker = `LlmAgent` instance
- Pattern: *rubato* from google/adk-go v1.3.0. ~380 LOC total, reused cross-slice with net -400 LOC savings.

**`agent.Event` + `agent.Actions`:**
- Purpose: Streaming unit + termination signal
- Examples (planned): `internal/agent/event.go` defines `Event{Author, Branch, LLMResponse, Actions, Timestamp}` and `Actions{Escalate, StateDelta, ArtifactDelta}`
- Pattern: `Actions.Escalate=true` propagates up via `return` after every `yield` check in workflow agents

**`tools.Tool` + `tools.Registry`:**
- Purpose: LLM-facing capability + manifest with deferred-tool pattern
- Examples (today): `internal/agent/tools/text_response.go`, `internal/agent/tools/search.go`
- Examples (planned): `execute` (Deferred), `task` (Deferred + ActionRouter), `skill.*` (Deferred + ActionRouter), `ask_user` (non-deferred), `read_tool_output` (non-deferred), `web_search`/`web_fetch` (Deferred), `ingest.*` (Deferred), `memory.*` (Deferred)
- Pattern: `Spec.Deferred=true` hides full Description+Parameters from default manifest. `tool_search` (built-in hook, non-deferred) loads on demand.

**`llm.Client` + `llm.Chunk`:**
- Purpose: Provider-neutral streaming wire
- Examples (today): contract only; `stubClient` in `cmd/aura/main.go`
- Examples (planned): `internal/llm/openai_compat/client.go` (DeepSeek/OpenRouter/Anthropic via SSE), `internal/llm/router.go` (LLMRouter dispatches remote vs local)
- Pattern: `Stream(ctx, req) (<-chan Chunk, error)`. KV-cache discipline (stable prefix) lives in the prompt builder, not here.

**`tools.ToolResult`:**
- Purpose: Preview + persist sidecar to protect context window
- Examples (planned): `internal/agent/tools/result.go` with `{Preview, FullPath, Bytes, Truncated}`
- Pattern: Result > `AURA_CONTEXT_PREVIEW_CAP_BYTES` → write to `$AURA_RUN_DIR/conversations/<conv_id>/<tool_call_id>.result`, history holds preview+footer pointer. `read_tool_output(tool_call_id, offset?, limit?)` fetches ranges.

**`PausedState` + `ErrAwaitingUserInput` (Slice 1.5):**
- Purpose: Pause/resume primitive for HITL
- Examples (planned): `internal/agent/tools/ask_user.go`, `internal/agent/pending.go`, table `aura.paused_states`
- Pattern: Tool returns sentinel error (NOT `ToolResult`). Loop intercepts, persists row, emits `Event{Escalate}`. Multi-pause FIFO ordered `(priority DESC, created_at ASC)`.

**`InvocationContext`:**
- Purpose: Cross-cutting state per `Run` invocation, propagated to all sub-agents
- Examples (planned): `internal/agent/agent.go` defines `{Ctx, Agent (self-ref), SessionID (=conv_id), IdentityID, Branch, SessionStore, GraphStore, LLMClient}`
- Pattern: Composed once at top-level Loop, threaded through `SubAgents()` traversal.

**`scoring.RiskTier`:**
- Purpose: Deterministic risk classification for mutation tools
- Examples (planned): `internal/scoring/scoring.go` with `ComputeTaskTier(TaskArgs)` and `ComputeSkillTier(action, body)`
- Pattern: Hard-coded kind→tier map + modifiers that bump UP only. Saturates at DESTRUCTIVE. Used by Slice 6 (cron) + Slice 7 (skills).

## Entry Points

**Current State:**
- `cmd/aura/main.go`: subcommand router. Today only `tools` and `chat` are wired; `serve` and `shell` print "TODO".

**Target State:**
- `aura serve`: long-lived runtime. Boots Postgres pool + Neo4j MCP + sidecars healthcheck + `channels.Registry.StartAll` (CLI + Telegram + AG-UI HTTP server on 9080 + Setup wizard on 9081). Default in production.
- `aura shell`: interactive REPL against the agent loop (debug).
- `aura chat [new|list|resume|archive|delete|rename]`: multi-conversation CLI (Slice 1.8).
- `aura chat <msg>`: one-shot turn, prints assistant reply, exits.
- `aura exec [--session <conv_id>] <lang> <code>`: bypass agent loop, direct sandbox invocation.
- `aura ingest <path|url>`: trigger Slice 11 ingestion pipeline.
- `aura tools`: print tool manifest (active + deferred).
- `aura db {migrate|ping|status|reset|restore}`: Postgres lifecycle (Slice 0.5).
- `aura neo4j {migrate|ping|status|reset}`: Neo4j MCP migrations (Slice 0.7).
- `aura identity {list|get|grant|revoke}`: identity + capability management (Slice 1.7).
- `aura paused-states {list|purge}`: pause-state escape hatch (Slice 1.8).
- `aura task {list|cancel|approve|audit|run_now}`: scheduler control (Slice 6).
- `aura skills {list|info|catalog|install|create|update|delete|approve|audit|run}`: skill control (Slice 7).
- `aura memory {search|recall|forget|summarize}`: memory tools mirror (Slice 11).
- `aura profile {show|edit|reset}`: per-identity Agent.md control (Slice 10).
- `aura llm-router {status|cost}`: routing inspection (Slice 13).
- `aura telegram {allow|list|revoke}`: admin CLI for Telegram accounts (Slice 9).
- `aura sandbox sessions {list|terminate|prune}`: Slice 2b session control.
- `aura config`: read/write `~/.aura/llm.json`.

## Architectural Constraints

- **Threading:** Goroutine-per-conversation (Loop). Workflow `ParallelAgent` uses `errgroup` + ackChan synchronous backpressure (rubato adk-go, prevents memory bloat). MCP subprocess lifecycle accoppiato al processo `aura serve`. Slice 5 web tools use `safeDialContext` to refuse loopback/private IPs (SSRF defense).
- **Global state:** None today. Target: `tools.Registry` immutable for lifetime of an agent run; swarm children receive a filtered copy. Cache for `skills.Loader` is `sync.RWMutex` with TTL 1s + explicit `Invalidate()` on mutation.
- **Circular imports:** None today. Constraint going forward: `internal/agent` MUST NOT import `internal/channels`. Channels consume `iter.Seq2[*Event, error]` through `agui.Translator` or the in-process `agui.Fanout`. Tools may import `internal/sandbox` / `internal/swarm` / `internal/web` but those packages may not import `internal/agent`.
- **KV-cache invariants:** `messages[0]` (system prompt) is byte-identical across turns. Tool manifest sorted alphabetically (`internal/agent/tools/manifest.go:37`) for cache stability. Any reshuffle invalidates provider-side prompt cache (see `[[feedback_aura_cache_poisoning_sites_2026-05-27]]`).
- **No native Neo4j Go driver:** All Neo4j access via `mcp-neo4j-cypher` subprocess (stdio MCP). The LLM owns the graph interface. No `github.com/neo4j/neo4j-go-driver` dependency.
- **File size cap:** No file > 600 LOC. Refactor-on-touch in same commit. `llm_agent.go` post-Slice 1+1.5+1.8+4+8+10 estimated 520-580 LOC — split point planned: `llm_agent.go` (core Run) + `llm_agent_pause.go` (pause/resume) + `llm_agent_history.go` (persistence hooks).
- **No god class Config:** `internal/config` keeps ONLY `Config{LLM, DB, RunDir, ToolPreviewCap}`. Each subsystem (sandbox/web/cron/skills/...) owns its own `*Config` struct in its own package.
- **Default-bind 127.0.0.1:** All HTTP servers (setup 9081, AG-UI 9080, sandbox 18901). Remote access via explicit env override.
- **Caps (Slice 1.8 + Caps & Limits section):**
  - `AURA_CONTEXT_PREVIEW_CAP_BYTES=2048` — tool result preview cap
  - `AURA_CONVERSATION_TURN_CAP_BYTES=65536` — turn content sidecar threshold
  - `AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS=10` — microcompact L1 eviction window
  - `AURA_RUN_DIR_WARN_THRESHOLD_BYTES=1073741824` — 1 GiB warn (no auto-purge)

## Anti-Patterns

### Mutating `messages[0]`

**What happens:** A developer "refactors" the system prompt builder to splice in per-turn context (current time, last user message hint, etc.).
**Why it's wrong:** Breaks the stable-prefix invariant. Every turn invalidates the provider-side KV cache (DeepSeek auto-cache, Anthropic ephemeral). Cumulative cost can 10x on long conversations. Cache poisoning sites documented in `feedback_aura_cache_poisoning_sites_2026-05-27.md`.
**Do this instead:** Append a fresh `user`/`assistant`/`tool` message OR add a stable secondary system message at index 1 (the slot reserved for `Agent.md` in Slice 10 / `:AgentInsight` in Slice 11). See `internal/agent/loop.go:42` — the Messages slice is append-only.

### Bypassing the deferred-tool flag

**What happens:** A tool with a 50-line description + complex JSON schema is registered with `Deferred: false`, so its full spec ships on every turn.
**Why it's wrong:** Bloats the cached prefix per turn, defeats the purpose of `tool_search`. With N tools registered, cache invalidation cost scales linearly with manifest size.
**Do this instead:** Set `Spec.Deferred=true` for any tool whose Description is non-trivial or whose Parameters JSON schema is more than a couple of fields. The model finds it via `tool_search` keyword match or `select:<name>`. See `internal/agent/tools/spec.go:25`.

### Returning a raw string from a heavy tool

**What happens:** `execute` returns the full 21 MB stdout of a Python script as the tool result string.
**Why it's wrong:** Poisons the conversation history. Next turn the full 21 MB is re-sent to the LLM. Slice 4 KV cache cannot save you — history grows past the context window.
**Do this instead:** Wrap in `ToolResult{Preview, FullPath, Bytes, Truncated}`. Preview ≤ `AURA_CONTEXT_PREVIEW_CAP_BYTES` (default 2048), full output sidecar-persisted. `read_tool_output(tool_call_id, offset?, limit?)` fetches ranges on demand. See planned `internal/agent/tools/result.go`.

### Calling `ask_user` alongside other tools in the same turn

**What happens:** The LLM batches `ask_user` + `read_tool_output` + `text_response` in one turn.
**Why it's wrong:** Mixed semantics. `text_response` would terminate the loop while `ask_user` is still pending.
**Do this instead:** The Loop enforces exclusivity: if `ask_user` appears in the batch, all sibling tool calls are dropped (and re-emitted at resume). Multiple `ask_user` in same turn coalesce as separate `PausedState` rows. See Slice 1.5 acceptance criteria.

### Putting knowledge in Postgres or filesystem

**What happens:** Developer adds a "scratch facts" JSONB column to `aura.conversations`, or writes a markdown wiki under `~/.aura/wiki/`.
**Why it's wrong:** Violates the non-negotiable persistence boundary in CLAUDE.md §Persistence. Knowledge + vectors live ONLY in Neo4j (via MCP). Postgres = application state. Filesystem = operational artifacts.
**Do this instead:** Use Neo4j `:Entity` / `:Chunk` / `:Concept` nodes via the memory ingestion pipeline (Slice 11). For per-identity preferences use `~/.aura/agents/<id>/Agent.md` (Slice 10) — that is *user profile*, not *knowledge*.

### Spawning a swarm worker without depth check

**What happens:** A swarm worker (depth 3) spawns another worker (depth 4) without checking `MAX_SPAWN_DEPTH`.
**Why it's wrong:** Exponential goroutine + LLM cost explosion. The constraint exists in `internal/swarm/swarm.go:29` (`MaxSpawnDepth = 3`) for a reason.
**Do this instead:** `Coordinator.Spawn` rejects `Depth >= MaxSpawnDepth`. Use `Spawn` (not raw goroutines) so the check is centralized. See Slice 3 acceptance criteria.

### Synchronous LLM call from a hot path

**What happens:** Auto-title (Slice 1.8) runs synchronously inside `AppendTurn`, blocking the user's reply.
**Why it's wrong:** Adds a round-trip to the user-visible latency. If the LLM call fails, the conversation save would fail too.
**Do this instead:** Background goroutine, best-effort, idempotent. See Slice 1.8 acceptance: "Atomica (transazione separata, errore non blocca chat). Best-effort: se LLM call fallisce, title resta NULL."

## Error Handling

**Strategy (target):** Errors propagate as Go values with structured wrapping. No panics in normal paths. HTTP errors from LLM wire are returned as `HTTPError{StatusCode, RetryAfterSec, Body}` (no retry at wire level — caller decides). Sentinel errors for control flow: `ErrAwaitingUserInput` (pause), `ErrEscalate` (workflow termination).

**Patterns:**
- `fmt.Errorf("context: %w", err)` everywhere with `errors.Is` / `errors.As` checks at boundaries
- Build-tagged integration tests (`//go:build db_integration`, `//go:build sandbox_integration`, `//go:build neo4j_integration`, `//go:build multimodal_integration`) skip cleanly when sidecars absent
- `goleak.VerifyNone(t)` in TestMain for the LLM client (catches blocked SSE readers post-cancel)
- `ctx.Err()` propagation: Ctrl+C closes HTTP connection mid-stream, drains channel, returns to user
- Failed migrations abort with explicit message (no panic, no silent fallthrough)
- Loop returns `(reply string, pending []*PausedState, err error)` — caller distinguishes "done" from "needs user input"

## Cross-Cutting Concerns

**Logging:** Not specified in current skeleton. Target: structured logging via standard library `log/slog` (Go 1.21+). Boot logs print tool manifest via `Registry.RenderText()`.

**Validation:**
- Tool args validated by each tool's `Execute` (e.g. `text_response: text is required`)
- Skill names validated by single chokepoint `internal/skills/paths.go:SanitizeName` (Slice 7a, audit P0)
- Capability names validated by regex `^[a-z][a-z0-9._-]{0,63}$` (Slice 1.7)
- Risk-Based pre-flight `scoring.ComputeTaskTier` / `ComputeSkillTier` (Slice 6 + 7)

**Authentication:** None in current skeleton. Target: identity scoping via `aura.identities` (single `local` user today, multi-user structurally supported via `identity_id` FK). AG-UI endpoint binds `127.0.0.1` (no auth — explicitly out of scope per `Out of scope per tutte e 4 le slice`). Telegram onboarding via QR/deep-link consumes `aura.telegram_setup_pending` token (1h TTL).

**Audit:** Per-domain audit tables in Postgres: `aura.skill_audit`, `aura.agent_job_runs`, `aura.profile_audit`, `aura.ingest_audit`, `aura.knowledge_migrations`. Each includes `computed_risk_tier`, `gate_recommended`, `gate_taken` for forensics (Risk-Based governance Area #5).

**Cost tracking:** `aura.local_llm_cost` + `aura.conversations.total_*_tokens` + `total_cost_usd` (numeric(10,4)). `aura llm-router cost --today` breakdown. Cost threshold trigger auto-switches LLMRouter to local (Slice 13).

---

*Architecture analysis: 2026-05-28*
