<!-- refreshed: 2026-05-27 -->
# Architecture

**Analysis Date:** 2026-05-27

## System Overview

```text
┌─────────────────────────────────────────────────────────────────────┐
│                        Inbound Channels                              │
├──────────────────┬──────────────────┬─────────────────┬─────────────┤
│  Telegram Bot    │  Web /api/chat   │  Cron / Sched   │  Swarm      │
│ `internal/       │ `internal/       │ `internal/      │ `internal/  │
│  channels/       │  channels/web/`  │  channels/cron/`│  channels/  │
│  telegram/`      │  `internal/api/` │ `internal/cron/`│  silent/`   │
└────────┬─────────┴────────┬─────────┴───────┬─────────┴──────┬──────┘
         │                  │                 │                │
         ▼                  ▼                 ▼                ▼
┌─────────────────────────────────────────────────────────────────────┐
│        chat.Hub  (channel-neutral dispatcher)                        │
│  `internal/chat/hub.go` + `internal/chat/types.go`                   │
│  InboundAdapter → InboundMessage → AgentLoopAdapter → emit(events)   │
└────────────────────────────────┬────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────────┐
│        Agent Runtime  (one turn = LLM ⇄ Tools, parallel dispatch)    │
│  `internal/agent/loop.go` + `internal/agent/executor.go`             │
│  + `internal/agent/runtime.go`  + `internal/agent/governance/`       │
└────────┬─────────────────────┬─────────────────────┬────────────────┘
         │                     │                     │
         ▼                     ▼                     ▼
┌────────────────────┐  ┌───────────────┐  ┌─────────────────────────┐
│   LLM Client       │  │ Tool Registry │  │   Skills / Agent Defs    │
│ `internal/llm/`    │  │ `internal/    │  │ `internal/skills/`       │
│  chat + embeddings │  │  agent/tools/ │  │ `internal/agent/         │
│  separate creds    │  │  registry/`   │  │  agentdef/`              │
└────────────────────┘  └──────┬────────┘  └─────────────────────────┘
                               │
        ┌──────────────────────┼─────────────────────────┐
        ▼                      ▼                         ▼
┌──────────────┐  ┌────────────────────────┐  ┌────────────────────┐
│  Wiki Store  │  │  Source Ingestion      │  │  Search / Memory   │
│ `internal/   │  │ `internal/storage/     │  │ `internal/storage/ │
│  wiki/`      │  │  sources/{store,ocr,   │  │  search/`          │
│  go-git +    │  │  ingest,markitdown}/`  │  │  `internal/storage/│
│  FTS5 +      │  │  Mistral OCR +         │  │  qdrant/`          │
│  graph index │  │  markitdown sidecar    │  │  `internal/storage/│
│              │  │  + LLM extractor       │  │  memoryindex/`     │
└──────┬───────┘  └──────────┬─────────────┘  └─────────┬──────────┘
       │                     │                          │
       ▼                     ▼                          ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      Persistence Layer                               │
│  SQLite (DB_PATH=/data/aura.db) — migrations `internal/db/`          │
│  Filesystem — wiki/* sources/* skills/* runtime-workspace/*          │
│  Qdrant (vector) + Garage S3 (artifacts/backups)                     │
└─────────────────────────────────────────────────────────────────────┘
```

## Component Responsibilities

| Component | Responsibility | File |
|-----------|----------------|------|
| `main` | Process lifecycle, tray/headless, restart loop, logger | `cmd/aura/main.go` |
| `App` (composition root) | Builds all `Deps`, owns bgCtx, mounts API + SPA, drives shutdown | `cmd/aura/app.go` |
| `App.wireBot` | Phase-C wiring: chat Hub adapters, summarizer, agent runner | `cmd/aura/app_wire.go` |
| `telegram.Bot` | Telegram polling, allowlist, command catalog, document/voice handlers | `internal/telegram/bot.go` |
| `chat.Hub` | Channel-neutral dispatcher: receives raw payload → InboundMessage → AgentLoop → fan-out OutboundEvent | `internal/chat/hub.go` |
| `chat.AgentLoopAdapter` | Translates `agent.Event` callbacks → `chat.OutboundEvent` vocabulary | `internal/chat/agentloop.go` |
| `agent.runLoop` | One turn: LLM ⇄ tool calls, budget, dedup, graceful finalize | `internal/agent/loop.go` |
| `agentExecutor` | Per-call dispatch: allowlist, timeout, untrusted wrap, TokenJuice compaction, spill | `internal/agent/executor.go` |
| `tools.Registry` | Name→Tool dispatch, allowlist gate, MCP slot, content-hash reconciler | `internal/agent/tools/registry/registry.go` |
| `llm.Client` | OpenAI-compatible chat + streaming + tool-call fragment accumulation | `internal/llm/client.go`, `internal/llm/openai.go` |
| `wiki.Store` | Atomic Markdown writes, per-slug mutex, go-git tracking, FTS5 + graph indexes | `internal/wiki/store.go` |
| `search.QdrantRepository` | Vector search over wiki pages (256d embeddinggemma) | `internal/storage/search/qdrant.go` |
| `memoryindex.Store` | Compact memory mirror (SQLite + Qdrant) for recall | `internal/storage/memoryindex/store.go` |
| `cron.Scheduler` | Polls SQLite `scheduled_tasks`, dispatches due tasks | `internal/cron/scheduler.go` |
| `mcp.Client` | Connects MCP servers (stdio + Streamable-HTTP), exposes tools dynamically | `internal/mcp/client.go` |
| `skills.Loader` | Loads Anthropic-style SKILL.md from runtime workspace + project skills | `internal/skills/loader.go` |
| `swarm.Manager` | AuraBot sub-agent dispatch (parent → child agent), depth-bounded | `internal/swarm/manager.go` |
| `api.Router` | Read/write JSON API mounted at `/api/`, embedded React SPA at `/` | `internal/api/router.go` |

## Pattern Overview

**Overall:** Modular monolith — single Go binary, layered composition root, channel-adapter pattern around a neutral chat Hub, plugin-shaped extensions (MCP + Skills + AgentDefs).

**Key Characteristics:**
- One binary embeds Telegram bot, HTTP/dashboard, agent loop, wiki, search, sandbox.
- Channel-neutral chat Hub: every conversation (Telegram, web SSE, cron, swarm) produces the same `InboundMessage` and consumes the same `OutboundEvent` stream.
- Dependency injection via a single `App` struct (composition root in `cmd/aura/app.go`) — all stores, clients, and registries are built in one place and passed via `telegram.Deps`.
- Action-enum consolidated tools — most LLM-facing tools dispatch sub-actions through an `action` parameter (`search`, `web`, `file`, `source`, `wiki_page`, `task`).
- Parallel tool dispatch within one LLM round (`agentExecutor.ExecuteToolCalls` fan-out via goroutines + WaitGroup).
- Streaming everywhere: LLM tokens stream through the agent loop and out to channel adapters via per-event callbacks; Telegram adapter throttles progressive edits at ~600 ms.
- Deterministic wiki writes (`temperature=0`), atomic file writes (temp + rename), per-slug mutex, go-git commit per change.
- Side-car friendly: markitdown (`docker/markitdown`), whisper (`docker/whisper`), pocket-tts (`docker/pocket-tts`), llama.cpp embed server, Qdrant, Garage S3 — all reachable via Docker network only, graceful degradation when missing.

## Layers

**`cmd/aura/` (Process / Composition Root):**
- Purpose: Boot, lifecycle, signal handling, restart, tray vs headless, DI wiring.
- Location: `cmd/aura/`
- Contains: `main.go`, `app.go`, `app_wire.go`, `app_adapters.go`, `mcp_runtime.go`, `web_chat*.go`.
- Depends on: Everything in `internal/*`.
- Used by: Nothing — it is the top of the dependency graph.

**`internal/chat/` (Channel-neutral Hub):**
- Purpose: Single dispatcher across all inbound surfaces; routes inbound → agent loop → outbound; owns Run lifecycle markers.
- Location: `internal/chat/`
- Contains: `hub.go`, `agentloop.go`, `types.go`, `hub_swarm.go`.
- Depends on: `internal/agent`, `internal/storage/runs`, `internal/identity`.
- Used by: All channel adapters; bot/setup wires Hub at boot.

**`internal/channels/<chan>/` (Channel Adapters):**
- Purpose: Translate channel-specific payload (`tele.Context`, HTTP request, scheduler tick) into `chat.InboundMessage`; translate `chat.OutboundEvent` back into channel-native delivery.
- Location: `internal/channels/{telegram,web,cron,silent,askuser}/`
- Contains: `inbound.go`, `outbound.go` per channel, plus channel-specific helpers (`status_pane.go`, `streaming_outbound.go`).

**`internal/agent/` (Agent Runtime):**
- Purpose: Single agent turn — LLM call, tool dispatch (parallel), budget enforcement, governance (dedup, repeated-lookup, payload summarizer), graceful finalize.
- Location: `internal/agent/`
- Contains: `loop.go` (the loop), `executor.go` (tool fan-out), `runtime.go`, `governance/`, `tools/`.
- Depends on: `internal/llm`, `internal/conversation`, `internal/storage/runs`, `internal/agent/tools/registry`.
- Used by: `internal/chat/agentloop.go` exclusively (one channel-neutral entry).

**`internal/agent/tools/registry/` (Tool Surface):**
- Purpose: All LLM-visible tools. One file per tool family with action-enum dispatch.
- Location: `internal/agent/tools/registry/`
- Contains: 98 Go files including `registry.go`, `search.go`, `web.go`, `file.go`, `source_unified.go`, `wiki.go`, `scheduler.go` (task), `mcp.go`, `text_response.go`, `ask_user.go`, `agent_note.go`, `propose_patch.go`, `exec.go`, `skill.go`, `tool_search.go`.

**`internal/wiki/` (Markdown Knowledge Base):**
- Purpose: Atomic page writes, per-slug mutex, go-git tracking, FTS5 mirror sync, graph adjacency index, alias index, dedup, gaps, suggestions, hygiene.
- Location: `internal/wiki/`
- Contains: `store.go` (Store struct), `store_writes.go`, `store_fts5.go`, `store_graph.go`, `graph.go`, `graph_index.go`, `alias_index.go`, `dedup.go`, `parser.go`, `schema.go`, `hubs.go`, `godnodes.go`, `subgraph_render.go`, `surprise.go`, `repairs.go`, `hygiene.go`, `memory_hygiene.go`.

**`internal/storage/` (Persistence Plumbing):**
- Purpose: SQLite/Qdrant wrappers, source ingestion pipeline, run/event log, freshness projection, embed cache, FTS5 syncer.
- Sub-packages: `search/`, `qdrant/`, `memoryindex/`, `reindex/`, `runs/`, `freshness/`, `sweep/`, `sources/{store,ocr,ingest,markitdown}/`.

**`internal/api/` (HTTP API + SPA):**
- Purpose: JSON read/write endpoints for the dashboard, auth (bearer SHA-256 in SQLite), embedded React SPA via `//go:embed all:dist`, setup wizard, health endpoints.
- Location: `internal/api/` (100 Go files).
- Contains: `router.go`, `auth.go`, `setup_server.go`, `wiki.go`, `sources.go`, `tasks.go`, `chat.go`, `chat_stream.go`, `mcp*.go`, `skills*.go`, `settings.go`, `maintenance*.go`, `static.go`, `health*.go`.

**`internal/telegram/` (Telegram Bot Internals):**
- Purpose: Wraps `gopkg.in/telebot.v4` — allowlist, commands, document handler (upload → source store), voice handler (Whisper + TTS), conversation snapshots, terminal output formatting.
- Location: `internal/telegram/`
- Contains: `bot.go`, `setup.go`, `commands.go`, `handlers.go`, `documents.go`, `voice_handler.go`, `conversation_snapshot.go`, `conversation_terminal.go`, `entity_*.go`, `tool_exec_helpers.go`, `atomic_tables.go`.

**`web/` (React 19 Dashboard):**
- Purpose: TypeScript + Vite + Radix UI dashboard. Built output embedded into Go binary via `//go:embed`.
- Location: `web/src/`
- Build output: `internal/api/dist/` (embedded).
- Stack: React 19, react-router-dom v7, TipTap, react-force-graph-2d, sonner, shadcn, i18next.

## Data Flow

### Primary Telegram Request Path

1. Telegram delivers an update to `tele.Bot.Poll` (`internal/telegram/bot.go`).
2. The bot's handler hands the `tele.Context` to `chat.Hub.Receive(ChannelTelegram, raw)` (`internal/chat/hub.go`).
3. `telegramadapter.Inbound.Normalize` (`internal/channels/telegram/inbound.go`) maps it to `chat.InboundMessage`. Original `tele.Context` is stashed under `ChannelData["tele_context"]`.
4. Hub mints a `Run` (id, thread, principal), then calls `AgentLoopAdapter.Run` (`internal/chat/agentloop.go:73`).
5. The adapter calls the `InvocationBuilder` (`internal/channels/telegram/invocation_builder.go`) which closes over LLM client, tool registry, prompt overlay, executor → returns `agent.Invocation`.
6. The adapter overrides `Invocation.OnEvent` with a translator emitting `chat.OutboundEvent`s.
7. `agent.Run` enters `runLoop` (`internal/agent/loop.go`) — the single-turn loop.
8. Each iteration: scrub orphan tool results → assemble messages → call `llm.Client.Send/Stream` → if `HasToolCalls`, fan-out via `streamDispatcher` (`internal/agent/stream_dispatch.go`) → executor runs each tool in parallel (`agentExecutor.ExecuteToolCalls` in `internal/agent/executor.go`).
9. LLM tokens stream back; non-empty deltas surface as `EventMessageDelta`. Tool start/end events emit only KEYS (never values).
10. `telegramadapter.Outbound` (`internal/channels/telegram/outbound.go`) consumes events, throttles progressive edits (~600 ms), and finalizes the message.
11. On terminal state Hub emits `EventDone` and persists `Run` + `Event` rows via `runstore.Store`.

### Source Upload Path (PDF → Wiki)

1. User sends a document in Telegram. `telegram.docHandler` (`internal/telegram/documents.go`) receives `tele.Document`.
2. Bytes pass `KindOf` (text/PDF/docx/xlsx/etc.) and the SSRF gate.
3. `source.Store.Put` (`internal/storage/sources/store/store.go`) writes to `<SOURCES_PATH>/src_<sha16>/source.bin` (SHA-256 dedupe).
4. OCR path: `ocr.Client.Render` (`internal/storage/sources/ocr/client.go`) POSTs to Mistral `/v1/ocr` → writes `ocr.md`. Non-PDF formats route through `markitdown.Client` (`internal/storage/sources/markitdown/client.go`) → `extract.md`.
5. `ingest.Pipeline.Run` (`internal/storage/sources/ingest/pipeline.go`) feeds extracted text to `ingest.LLMExtractor` → produces structured concept/entity pages with `[[wiki-links]]`.
6. `wiki.Store.WritePage` commits the page (atomic write + per-slug mutex + go-git commit).
7. `search.QdrantRepository.IndexWikiPages` re-embeds and updates Qdrant; `search.WikiFTS5Syncer` mirrors to SQLite FTS5.

### Scheduler Path (cron task firing)

1. `cron.Scheduler.tick` (`internal/cron/scheduler.go`) wakes every `TickInterval` (default 30 s).
2. `cron.Store.DueTasks` (`internal/cron/store.go`) returns rows from `scheduled_tasks` where `next_run_at <= now`.
3. `cron.Dispatcher` (production-wired in `internal/cron/dispatch.go`) builds an `InboundMessage` with `Channel=cron`, `DeliveryMode=silent`, and hands it to `chat.Hub.Receive`.
4. From here the flow is identical to the Telegram path, but the outbound adapter is `silent` — results land in memory/wiki only, no notification.

**State Management:**
- Per-conversation sliding window (cap: 50 messages) in `internal/conversation/`.
- Per-turn ephemeral state via `agent.State` (loop.go drives mutation through `state.AddAssistantToolCallMessage`, `state.AddToolResultMessage`).
- Durable state in SQLite (runs, events, scheduled_tasks, allowed_users, secrets, embed cache, conversation archive) and filesystem (wiki, sources, skills, prompt overlays).

## Key Abstractions

**`chat.InboundAdapter` / `chat.OutboundAdapter`:**
- Purpose: One pair per channel — pure boundary translation, no agent coupling.
- Examples: `internal/channels/telegram/inbound.go`, `internal/channels/web/outbound.go`, `internal/channels/cron/inbound.go`, `internal/channels/silent/outbound.go`.
- Pattern: `Channel()` + `Normalize(raw any) (InboundMessage, error)` for inbound; `Channel()` + `Mode()` + `Deliver(ctx, event) error` for outbound.

**`agent.Invocation`:**
- Purpose: Single agent turn descriptor — wraps client, tools, executor, options, OnEvent callback.
- File: `internal/agentcore/invocation.go`.
- Pattern: `InvocationBuilder` factory closes over per-channel deps; the adapter overrides `OnEvent` for event translation.

**`tools.Tool` interface:**
- Purpose: Single contract every LLM-callable tool implements.
- File: `internal/agent/tools/registry/registry.go:20`.
- Pattern: `Name() string`, `Description() string`, `Parameters() map[string]any`, `Execute(ctx, args) (string, error)`. Optional `Definition() ToolDefinition` provides examples + visibility tier + hints.

**Action-enum tools:**
- Pattern: One LLM-visible tool name (e.g. `search`, `web`, `file`, `source`, `wiki_page`, `task`) dispatches sub-actions via the `action` parameter inside `Execute`.
- Examples: `internal/agent/tools/registry/search.go` (actions: `search`, `list`, `read`, `lessons`, `user_facts`, `god_nodes`, `subgraph`, `path`); `internal/agent/tools/registry/web.go` (`search`, `fetch`); `internal/agent/tools/registry/source_unified.go` (`list`, `read`, `store`, `reprocess`, `delete`, `lint`); `internal/agent/tools/registry/scheduler.go` (`schedule`, `list`, `cancel`, `run_now`).
- Rationale: Reduces LLM-facing tool surface; consolidates related operations under a single description block.

**`AgentDef` (sub-agent definition):**
- Purpose: Declarative spec for swarm sub-agents (orchestrator, reflector, summarizer).
- Files: `internal/agent/agentdef/definition.go`, `internal/agent/agentdef/loader.go`, `internal/agent/agentdef/registry.go`.
- Built-in defs at `internal/agent/agentdef/builtin/{orchestrator,reflector,summarizer}/`.

**`mcp.Client`:**
- Purpose: One connected MCP server (stdio or Streamable-HTTP), exposes Tools snapshot.
- File: `internal/mcp/client.go`.
- Pattern: JSON-RPC 2.0, content-hash diff via `internal/mcp/watcher.go` for hot-reload on `mcp.json` change.

## Entry Points

**Main binary:**
- Location: `cmd/aura/main.go`
- Triggers: `aura` binary launch (Docker compose, tray, direct exec).
- Responsibilities: CLI parsing (`--help`, `--version`), config load, logger setup, headless vs tray dispatch, restart cooldown.

**`startAura` (boot sequence):**
- Location: `cmd/aura/main.go:202`
- Sequence:
  1. `config.EnsureLayout` — create runtime workspace, wiki, sources, skills, mcp dirs.
  2. `auradb.Open` + `migrations.Run` — open SQLite, run migrations m01..m29.
  3. `ensureDatabaseIntegrity` — `PRAGMA integrity_check` + FTS5 rebuild on demand.
  4. `config.NewStoreWithDB` + `config.ApplyToConfig` — overlay SQLite settings on top of env config.
  5. `secrets.NewSQLiteStore` + `applySecretsToConfig` — apply secret precedence: SQLite > env > empty.
  6. Setup wizard (`api.SetupRun`) — if not bootstrapped, block until user submits Telegram token + LLM config.
  7. `api.NewHealthServer` — start health/observability HTTP server.
  8. `newApp` (`cmd/aura/app.go:152`) — build all deps (LLM client, Qdrant, wiki store, source store, OCR, markitdown, whisper, pocket-tts, ingest pipeline, sandbox, tool registry, agent defs, skills loader, scheduler store, swarm manager, MCP servers, auth store).
  9. `telegram.New(deps)` — construct Bot.
  10. `app.wireBot(bot)` — Phase-C wiring (chat Hub adapters, summarizer, agent runner).
  11. `healthServer.Mount("/api/", app.APIHandler())` + `Mount("/", static)` — mount API and embedded SPA.
  12. `app.Start(bot)` — start MCP watchers + scheduler + Telegram polling.

**HTTP API:**
- Location: `internal/api/router.go`
- Triggers: HTTP requests on `HTTP_PORT` (default `0.0.0.0:8080`).
- Responsibilities: All dashboard endpoints — wiki, sources, tasks, skills, MCP, conversations, maintenance, chat (SSE), settings, backups, health.

**Setup wizard:**
- Location: `internal/api/setup_server.go`
- Triggers: First boot when `cfg.IsBootstrapped()` returns false.
- Responsibilities: Loopback-only HTTP form for Telegram token + LLM config; blocks `startAura` until submission.

## Architectural Constraints

- **Threading:** Single Go process, multi-goroutine. One goroutine per Telegram conversation (`telegram.Bot` dispatches per `tele.Context`). One goroutine per agent turn. Parallel tool dispatch via WaitGroup in `agentExecutor.ExecuteToolCalls`. `chat.Hub` uses `sync.Map` for run cancels and thread status.
- **Per-slug mutex:** Wiki writes serialized per slug via `sync.Map` in `wiki.Store.mu`. Git operations serialized globally via `wiki.Store.gitMu`.
- **bgCtx ownership:** `cmd/aura/app.go` owns `bgCtx`/`bgCancel`/`bgWg` (US-A13c). Telegram Bot does NOT own goroutine lifecycle.
- **Embedding endpoint MUST be separate:** `EMBEDDING_BASE_URL` + `EMBEDDING_API_KEY` never fall back to chat LLM credentials (see `internal/llm/cache.go`).
- **Deterministic wiki writes:** `temperature=0`, atomic temp-file+rename, go-git commit per write.
- **No secrets in logs:** Tool argument values, base64, and URLs with tokens are stripped by `internal/logging/sanitize.go` before reaching disk.
- **MCP servers are non-fatal:** Boot warnings only — Aura continues without an unreachable MCP server.
- **Sidecars degrade gracefully:** Qdrant, markitdown, whisper, pocket-tts, OCR — each absence disables a feature, never blocks boot.
- **Wiki = the graph:** Markdown frontmatter + `[[wiki-links]]` body links ARE the knowledge graph; no separate KuzuDB/Neo4j. `wiki.GraphIndex` (`internal/wiki/graph_index.go`) is the runtime adjacency layer.
- **No fast-path classifier:** Agent loop never bypassed for "easy" queries (confirmed anti-pattern). Loop tightening (MaxIterations, text_response terminal tool, parallel dispatch) is the right discipline.

## Anti-Patterns

### Splitting tools by sub-action

**What happens:** Creating one Go file + one LLM-visible tool name per sub-action (e.g. `wiki_read`, `wiki_write`, `wiki_list`, `wiki_subgraph` as separate tools).
**Why it's wrong:** Bloats the LLM-visible manifest, increases prompt cost, and dilutes the model's tool-selection signal. Live history: pre-2026-05-24 had 22+ tools — the consolidation reduced this to ~12 action-enum tools while preserving the same surface.
**Do this instead:** Add a new `action` value to the existing family tool (e.g. extend `search.go`'s action enum). See `internal/agent/tools/registry/search.go` for the canonical pattern.

### Reading prompt overlays via filesystem from the agent runtime

**What happens:** Loop code that opens `SOUL.md`/`AGENT.md` directly.
**Why it's wrong:** Couples agent runtime to filesystem layout and breaks the channel-neutral abstraction (web chat and Telegram both expect the same overlay loader).
**Do this instead:** Use `conversation.EnsurePromptOverlayDefaults` + `conversation.OverlayLoader` in `internal/conversation/overlay.go`. The composition root (`cmd/aura/app.go:175`) is the only place that reads overlay paths.

### Bypassing the action-enum dispatcher for "convenience"

**What happens:** Adding a hidden code path that calls `wiki.Store.WritePage` directly from a non-tool location.
**Why it's wrong:** Skips the audit trail (agent observation logging, run events) and breaks the assumption that wiki mutations are traceable to a tool call.
**Do this instead:** Route through `wiki_page` action enum (`internal/agent/tools/registry/wiki.go`), or use the API write endpoints (`internal/api/wiki_write.go`) for dashboard operations.

### Tool argument values in logs

**What happens:** `slog.Info("tool call", "args", call.Arguments)`.
**Why it's wrong:** Leaks secrets (URLs with tokens, file contents, base64). Live incidents in early Phase-FIX.
**Do this instead:** Log argument KEYS only (`argKeysFromCall(call)` in `internal/agent/loop.go:453`). `redactToolError` + `internal/logging/sanitize.go` enforce this on the error path.

### Doubling tools "to keep the old one"

**What happens:** Adding a new tool while leaving the deprecated one registered.
**Why it's wrong:** LLM gets two ways to do the same thing, dilution + confusion. Live 2026-05-24 cleanup removed `dev_tool`, `daily_briefing`, `doc`, `subagent_dispatch`, `read_tool_result`, `request_dashboard_token`, `ask_user_clarification`, `recall_operational`, `recall_user_memory`, `recall_god_nodes`, `wiki_subgraph`, `wiki_path` as separate manifests.
**Do this instead:** Migrate callers to the action-enum tool in the same commit; delete the old tool. `AURA_TOOL_ALLOWLIST` env var lets you stage the rollout.

## Error Handling

**Strategy:** Errors are values, propagated up with `fmt.Errorf("...: %w", err)`. Boot-time errors abort `startAura` and return non-zero exit; runtime errors degrade features (warn + continue).

**Patterns:**
- Tool errors classified via `tools.ClassifyToolError` into outcome buckets (`ok`, `error`, `blocked`, `permission`, `cancelled`) and persisted to `tool_attempts` table for the briefer/recall loop.
- LLM errors: `internal/llm/usererror.go` maps provider errors to user-safe messages; `internal/llm/retry.go` enforces backoff.
- HTTP API errors: `internal/api/error_helpers.go` writes structured JSON `{error, code, detail}`.
- Wiki errors: `wiki.Store` returns typed errors; ingest pipeline logs + skips bad sources rather than abort the batch.
- Database errors: `internal/db/retry.go` retries SQLite busy/locked errors with backoff; `internal/dbrecovery/` repairs corrupted FTS5/run-event indexes.

## Cross-Cutting Concerns

**Logging:** Structured `slog` backed by `zap` via `internal/logging/zap_slog.go`. Daily-rotated files via `internal/logging/daily_writer.go`. All output sanitized through `internal/logging/sanitize.go` (URL credentials, base64 blobs, known secret keys).

**Validation:** Tool parameters validated by JSON schema in `Parameters()` map; arguments cloned per-goroutine to avoid races (`cloneToolArgs` in `internal/agent/executor.go:285`). Source uploads checked against `formats.go` Kind detection and size cap. Wiki frontmatter validated by `wiki.SchemaCheck` (`internal/wiki/schema.go`).

**Authentication:** Dashboard bearer tokens (SHA-256 hashed in SQLite `api_tokens`) — `internal/api/auth/store.go`. Tokens minted by Telegram bot (`/login` command), never returned in response bodies. Allowlist enforced at Telegram via `internal/telegram/access.go` and at API via `internal/api/auth/middleware.go`.

**Identity:** Channel-neutral principals via `internal/identity/store.go`. Capabilities (`identity.Capability`) gate sensitive tools (admin, sandbox, web_fetch unsafe) via `tools.RequiredCapability` (`internal/agent/tools/registry/registry.go`).

**Budget tracking:** `internal/budget/budget.go` — per-turn soft/hard cost caps. `internal/agent/governance/budget.go` — per-class tool-call caps. Both surface to LLM as structured errors and to `/api/health` for dashboards.

**Secrets:** `internal/secrets/store.go` — SQLite secrets table (id, key, encrypted_value). Bootstrap sidecar `aura-secrets` (`docker/secrets/init-secrets.sh`) generates per-install values on first boot.

---

*Architecture analysis: 2026-05-27*
