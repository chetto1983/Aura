<!-- refreshed: 2026-09-07 -->
# Architecture

**Analysis Date:** 2026-09-07

## System Overview

```text
┌─────────────────────────────────────────────────────────────┐
│                      Ingress / Channels                      │
├──────────────────┬──────────────────┬───────────────────────┤
│  Telegram bot    │  AG-UI HTTP+SSE  │   CLI REPL / one-shot │
│ `internal/       │ `internal/agui/  │  `cmd/aura/chat_repl. │
│  channels/       │  server.go`      │   go`, `shell`        │
│  telegram/bot.go`│                  │                       │
└────────┬─────────┴────────┬─────────┴──────────┬────────────┘
         │                  │                     │
         ▼                  ▼                     ▼
┌─────────────────────────────────────────────────────────────┐
│              Turn orchestration — Runner                     │
│  `internal/runner/runner.go` (Turn / runTurn)                │
│  history rehydrate · budget · persist · memory capture       │
└────────────────────────────┬────────────────────────────────┘
                             │ builds a fresh LlmAgent per turn
                             ▼
┌─────────────────────────────────────────────────────────────┐
│                Agent runtime — LlmAgent loop                 │
│  `internal/agent/llm_agent.go` + `llm_agent_round.go`        │
│  `llm_agent_dispatch.go` (tool calls)                        │
│  emits `agent.Event` stream (iter.Seq2[*Event, error])       │
└──────┬───────────────────────┬──────────────────────┬───────┘
       │                       │                      │
       ▼                       ▼                      ▼
┌──────────────┐   ┌────────────────────┐   ┌──────────────────┐
│ LLM client   │   │ Tool registry      │   │ Sub-agents /     │
│ `internal/   │   │ `internal/agent/   │   │ swarm            │
│  llm/        │   │  tools/registry.go`│   │ `internal/swarm/ │
│  client.go`  │   │  + deferred specs  │   │  swarm.go`       │
└──────────────┘   └─────────┬──────────┘   └──────────────────┘
                             │
      ┌──────────────┬───────┴───────┬───────────────┬──────────┐
      ▼              ▼               ▼               ▼          ▼
┌───────────┐ ┌────────────┐ ┌────────────┐ ┌───────────┐ ┌──────────┐
│ Sandbox   │ │ MCP bridge │ │ Skills     │ │ Documents │ │ Web      │
│`internal/ │ │`internal/  │ │`internal/  │ │`internal/ │ │`internal/│
│ sandbox/  │ │ agent/     │ │ skills/`   │ │documents/`│ │ web/`    │
│usersandbox│ │ mcptools/` │ │            │ │           │ │          │
└───────────┘ └─────┬──────┘ └────────────┘ └───────────┘ └──────────┘
                    │ stdio/http MCP
                    ▼
┌─────────────────────────────────────────────────────────────┐
│  Persistence                                                 │
│  Postgres `aura.*` — `internal/db/` (sqlc + golang-migrate)  │
│  ArcadeDB memory  — `internal/arcadedb/`, reached by the     │
│                     agent through `cmd/arcadedb-mcp`         │
│  Object store / filesystem — `internal/objectstore/`         │
└─────────────────────────────────────────────────────────────┘
```

## Component Responsibilities

| Component | Responsibility | File |
|-----------|----------------|------|
| Daemon composition root | Builds pool, MCP mounts, tool registry, Runner, scheduler, channels, AG-UI server | `cmd/aura/serve.go` (`bootServe`) |
| Chat composition root | Shared sub-root reused by `serve`, `chat`, `shell` | `cmd/aura/chat_boot.go` (`bootServeChatEnv`) |
| Agent contract | Open `Agent` interface, `InvocationContext`, `Event`, budget tree | `internal/agent/agent.go`, `internal/agent/event.go`, `internal/agent/budget.go` |
| Agent loop | Streaming tool-dispatch loop, terminal `text_response`, retry, steer, pause | `internal/agent/llm_agent.go` and its `llm_agent_*.go` siblings |
| Tool dispatch | Partition terminal vs runnable calls, parallel execution, dedup, hooks | `internal/agent/llm_agent_dispatch.go` |
| Tool registry | Registration, immutability per run, deferred filtering, manifest render | `internal/agent/tools/registry.go`, `spec.go`, `manifest.go` |
| MCP → tool bridge | Adapts live MCP servers into `tools.Tool`, applies deferral/risk policy | `internal/agent/mcptools/bridge.go`, `bridge_deferral.go`, `bridge_policy.go` |
| Turn orchestration | Persistence, compaction, HITL resume, memory capture, auto-title | `internal/runner/runner.go`, `runner_deps.go` |
| LLM client | Provider-neutral streaming client (OpenAI-compatible wire), capabilities, pricing, breaker | `internal/llm/client.go`, `runtime.go`, `capabilities.go`, `breaker.go` |
| Memory | Bitemporal fact edges over ArcadeDB, vector + Lucene fusion, provenance | `internal/arcadedb/memory.go` and `memory_*.go` |
| Memory MCP server | The agent's LLM-facing interface to its own memory | `cmd/arcadedb-mcp/main.go`, `tool_memory*.go` |
| Skills | Self-extension: catalog, install, validate, materialize, write | `internal/skills/loader.go`, `installer.go`, `writer.go`, `internal/agent/tools/skill_write.go` |
| Sandbox | Per-user Docker box: spec, materialize, exec, egress policy, reap | `internal/sandbox/usersandbox/router.go`, `docker_backend.go`, `egress.go` |
| AG-UI gateway | HTTP/SSE run endpoint, event translation, fan-out, REST surface | `internal/agui/server.go`, `translator.go`, `fanout.go`, `server_sse.go` |
| Channels | Narrow daemon lifecycle contract + Telegram implementation | `internal/channels/channel.go`, `registry.go`, `internal/channels/telegram/` |
| Swarm / delegation | Sub-agent spawn, delegation queue, transcripts, depth guard | `internal/swarm/swarm.go`, `delegation_*.go`, `internal/agent/tools/swarm_spawn.go` |
| Gateway (HITL) | Tool-call classification, approval reservation, durable grants | `internal/gateway/classify.go`, `decide.go`, `grants.go` |
| Scheduler | Durable cron tasks, claim/heartbeat/recover, dispatch to handlers | `internal/cron/scheduler.go`, `dispatch.go`, `store.go` |
| Web cockpit host | Embedded Vite build, SPA fallback, deliberate leaf package | `internal/webui/embed.go`, `cmd/aura/serve_webui.go` |

## Pattern Overview

**Overall:** Layered single-binary daemon with an explicit composition root, an
open agent interface, and streaming iterators (`iter.Seq2[*Event, error]`) as the
universal runtime data path.

**Key Characteristics:**
- Composition-root wiring only. Packages take narrow consumer-side interfaces
  (`runner.Deps` in `internal/runner/runner_deps.go`); no package reaches for a
  global.
- Everything the runtime emits is one `agent.Event` (`internal/agent/event.go`)
  carrying OTel/W3C-width trace ids and AG-UI correlation fields, so the AG-UI
  gateway is a fan-out adapter, not a translation layer rewrite.
- Deferred-tool manifest discipline: big tool specs are hidden from the default
  manifest and promoted only after `tool_search` loads them.
- One fresh `LlmAgent` per turn; conversation-scoped state (`activated`,
  `everLoaded`) is re-derived from rehydrated history.
- Fail-soft daemon subsystems: a channel or scheduler failing to start is
  aggregated, never fatal (`internal/channels/registry.go`, `cmd/aura/serve.go`).
- Per-file 600-LOC ceiling, enforced by `scripts/check-file-size.sh`, which is why
  large concerns are split as `<name>_<concern>.go`.

## Layers

**Ingress / channels:**
- Purpose: accept a user turn from Telegram, HTTP/SSE, or the CLI.
- Location: `internal/channels/`, `internal/agui/`, `cmd/aura/chat_repl.go`
- Contains: transport decoding, auth, per-turn context composition, rendering.
- Depends on: `internal/runner`, `internal/agent`.
- Used by: the daemon composition root.

**Turn orchestration:**
- Purpose: own the durable lifecycle of one turn.
- Location: `internal/runner/`
- Contains: history rehydration, compaction, persistence, HITL pause/resume,
  memory capture and projection, verification.
- Depends on: `internal/conversations`, `internal/gateway`, `internal/agent`,
  `internal/llm`.
- Used by: every channel.

**Agent runtime:**
- Purpose: drive the model, dispatch tools, emit events.
- Location: `internal/agent/` (+ `tools/`, `mcptools/`, `prompt/`, `workflow/`,
  `display/`)
- Depends on: `internal/llm`, `internal/gateway`, `internal/obs`.
- Used by: `internal/runner`, `internal/swarm`.

**Capability layer (tools):**
- Purpose: everything the model can actually do.
- Location: `internal/agent/tools/` plus the domain packages it calls into
  (`internal/skills`, `internal/sandbox/usersandbox`, `internal/documents`,
  `internal/web`, `internal/mcp`).

**Persistence:**
- Purpose: durable state.
- Location: `internal/db/` (Postgres, sqlc-generated client in
  `internal/db/sqlc/`, migrations in `internal/db/migrations/`),
  `internal/arcadedb/` (memory graph), `internal/objectstore/` (artifacts).

## Data Flow

### Primary request path — a Telegram message to a delivered answer

1. Telebot poller hands the update to the dispatcher
   (`internal/channels/telegram/bot_dispatch.go`), which routes text, media, or
   callback.
2. `runTurnWithAssets` injects the channel-agnostic turn context — this turn's
   attachments plus the thread's knowledge catalog — via `composeTurnContext`
   (`internal/channels/telegram/bot_dispatch_turn.go`).
3. `startTurn` registers a cancellable per-turn context so `/cancel` aborts it, and
   redirects into the live turn's steer inbox when one is already running
   (`bot_dispatch_turn.go`, `bot_dispatch_steer.go`, `bot_dispatch_queue.go`).
4. The channel builds a fresh `agui.Fanout` per turn (`internal/agui/fanout.go`)
   and subscribes its renderer before starting the run — fanout is per turn, never
   per channel start (`internal/channels/telegram/agui_subscriber.go`).
5. `runner.Runner.Turn` (`internal/runner/runner.go`) takes the per-thread lock,
   loads and budgets history (`runner_history.go`, `runner_context.go`), and
   constructs a fresh `LlmAgent` from the injected client + registry
   (`runner_llm_runtime.go`).
6. `LlmAgent.Run` (`internal/agent/llm_agent.go`) gates the budget, builds the
   request — system prompt from `internal/agent/prompt/builder.go`, tool defs from
   `Registry.RenderToolDefs` (`internal/agent/tools/manifest.go`) with deferred
   tools excluded until activated — and streams from `llm.Client`
   (`internal/llm/client.go`).
7. Tool calls go to `dispatch` (`internal/agent/llm_agent_dispatch.go`): the first
   `text_response` is the terminal; the rest run in parallel. A terminal mixed with
   runnable siblings is rejected outright and replanned.
8. Each call passes the gateway classifier
   (`internal/gateway/classify.go` → `decide.go`); a risky call reserves an
   approval and the turn pauses (`internal/agent/llm_agent_pause.go`), resuming
   through `internal/runner/runner_resume.go`.
9. Results become `RoleTool` messages; `tool_search` results carry
   `tools.MetaActivatedTools`, and `promoteFromMeta`
   (`internal/agent/llm_agent_promote.go`) moves those names into the callable set.
10. Events stream out as `agent.Event`; `internal/agui/translator.go` maps them to
    AG-UI protocol events, `Fanout` pumps them drop-on-full to each subscriber.
11. The Telegram renderer (`internal/channels/telegram/renderer.go`,
    `status_pane.go`, `mdv2.go`) edits its status pane live and sends the final
    message; voice-in echoes voice-out via `tts.go`.
12. The Runner persists the turn (`runner_persist.go`), offers it to memory capture
    (`runner_memory_capture.go` → `internal/arcadedb/memory_capture.go`) and to the
    conversation projector, then releases the thread lock.

### AG-UI web path

1. `POST /agent/run` decoded strictly, body capped at 1 MiB
   (`internal/agui/server_run_request.go`, `strict_decode.go`).
2. Auth and capability check (`internal/agui/auth.go`, `auth_cookie.go`).
3. Run registered in `internal/agui/runregistry.go` / `runsession.go`, then the
   same `runner.Runner.Turn` as above.
4. Events translated and pushed over SSE (`internal/agui/server_sse.go`), with an
   idle heartbeat and a detachable/resumable run
   (`server_run_detach.go`, `server_run_resume.go`).

### Memory read/write path

1. The model calls a memory tool exposed through the MCP bridge
   (`internal/agent/mcptools/bridge.go`).
2. The call reaches `cmd/arcadedb-mcp` (`tool_memory.go`, `tool_memory_recall.go`,
   `tool_memory_graph.go`), authenticated per tenant (`auth.go`, `tenant.go`).
3. `internal/arcadedb/memory_recall.go` fuses a Lucene leg and an EmbeddingGemma
   vector leg inside ArcadeDB (`memory_vector.go`), expanding along fact edges
   (`memory_graph.go`, `memory_graph_temporal.go`).
4. Writes are bitemporal: a contradiction closes the prior fact's window rather
   than deleting it (`memory_supersede.go`), and provenance is multi-source
   (`memory_provenance.go`).

**State Management:**
- Durable conversation state in Postgres (`internal/conversations/store.go`).
- Long-term semantic state in ArcadeDB, one database per identity
  (`internal/arcadedb/tenant.go`).
- Per-run state lives on `agent.InvocationContext`, passed by value and copied on
  `WithContext`/`WithSubAgent` — never stored on a long-lived struct.

## Key Abstractions

**`agent.Agent`:**
- Purpose: any runnable unit — the LLM loop, a workflow node, a swarm worker.
- Examples: `internal/agent/llm_agent.go`, `internal/agent/workflow/`,
  `internal/swarm/swarm.go`
- Pattern: open interface (no unexported seal) returning `iter.Seq2[*Event, error]`.

**`agent.Event`:**
- Purpose: single signal type for the whole runtime.
- Examples: `internal/agent/event.go`
- Pattern: forward-compat superset — AG-UI fields present before consumers exist.

**`tools.Tool` / `tools.Spec`:**
- Purpose: one model-callable capability plus its LLM-visible metadata.
- Examples: `internal/agent/tools/spec.go`, `text_response.go`, `shell_exec.go`
- Pattern: `Deferred` hides the full spec; `Mutating` drives the completion gate.

**`tools.Registry`:**
- Purpose: immutable-per-run tool set; `Without` derives a narrowed copy.
- Examples: `internal/agent/tools/registry.go`, `manifest.go`

**`llm.Client` / `llm.Runtime`:**
- Purpose: provider-neutral streaming completion; hot-swappable snapshot.
- Examples: `internal/llm/client.go`, `internal/llm/runtime.go`

**`channels.Channel`:**
- Purpose: narrow daemon lifecycle (`Name`/`Start`/`Stop`/`IsHealthy`).
- Examples: `internal/channels/channel.go`, `internal/channels/telegram/bot.go`

**Bitemporal fact edge:**
- Purpose: the memory unit — the fact lives on the `FACT` edge, with
  `valid_from`/`valid_to` (world time) and `created_at`/`expired_at` (belief time).
- Examples: `internal/arcadedb/memory.go`

## Entry Points

**`cmd/aura`:**
- Location: `cmd/aura/main.go`
- Triggers: CLI dispatch — `serve`, `shell`, `chat`, `tools`, `db`, `mcp`,
  `memory`, `agent`, `web`, `identity`, `gateway`, `doctor`, `version`.
- Responsibilities: parse the sub-command, run CLI idempotency preflight, hand off
  to the matching composition root.

**`aura serve` (the production daemon):**
- Location: `cmd/aura/serve.go` (`bootServe`), spread across ~45 `serve_*.go` files.
- Responsibilities: pool + MCP mounts + registry + Runner (reused from
  `bootServeChatEnv`), then ArcadeDB tenant reconcile, object store, share service,
  channels registry, cron dispatcher/scheduler, AG-UI server, embedded web UI.

**`cmd/arcadedb-mcp`:**
- Location: `cmd/arcadedb-mcp/main.go`
- Triggers: MCP client handshake from the agent's MCP bridge.
- Responsibilities: expose memory store/recall/graph/forget as MCP tools with
  per-tenant auth.

**Auxiliary binaries:**
- `cmd/aura-media-index`, `cmd/aura-filecard`, `cmd/aura-ingest-supervisor` —
  small single-purpose helpers.

## Architectural Constraints

- **Threading:** Go goroutines. One producer goroutine per `Fanout` is the sole
  sender on every subscriber channel and closes them on exit
  (`internal/agui/fanout.go`); a slow subscriber is dropped, never back-pressured.
  `runner.Runner` serializes per-thread runs behind a lock — a second concurrent
  run returns `runner.ErrThreadBusy`.
- **Global state:** deliberately near-zero. `llm.Runtime` holds an
  `atomic.Pointer` snapshot (`internal/llm/runtime.go`); `cmd/aura` keeps
  `cliInvocationContext` for CLI idempotency. Everything else is injected.
- **Package boundary enforcement:** `internal/webui` must import only the standard
  library; `scripts/agui_boundary_check.sh` asserts the dependency closure.
  `internal/cron` must not import `internal/swarm` (D-24), which is why
  `tools.Without` was promoted out of `internal/swarm`.
- **Circular imports:** none. Layering is one-directional
  (channels → runner → agent → llm/tools).
- **File size:** hard ceiling of 600 LOC per file, checked in CI.
- **Idempotency:** CLI invocations and cron dispatch are idempotency-keyed
  (`internal/idempotency/`, `cmd/aura/main.go`).

## Anti-Patterns

### Registering a big tool without `Deferred: true`

**What happens:** a long description and complex JSON schema land in the default
manifest on every turn.
**Why it's wrong:** it invalidates the provider prompt cache and grows linearly
with tool count.
**Do this instead:** set `Deferred: true` on the `Spec`
(`internal/agent/tools/spec.go`); the model fetches the schema via `tool_search`.

### Emitting a deferred tool as callable with an empty schema

**What happens:** the model sees a callable function with no parameters and
hallucinates arguments (historically `send_file {"file":...}` instead of
`{"path":...}`).
**Why it's wrong:** a callable function without a schema is a trap.
**Do this instead:** `RenderToolDefs` excludes a deferred tool entirely until its
name is in the per-run `activated` set — see `internal/agent/tools/manifest.go`.

### Signalling termination through the error slot

**What happens:** a budget trip or a normal stop is returned as an `error` from
`Run`.
**Why it's wrong:** the error slot of `iter.Seq2` means a REAL failure (LLM or
tool error); consumers cannot distinguish "done" from "broken" (D-04).
**Do this instead:** yield a terminal `Event`
(`internal/agent/llm_agent_finalize.go`).

### Combining `text_response` with other tool calls in one step

**What happens:** the model emits a final answer alongside runnable siblings, or
two `text_response` calls.
**Why it's wrong:** native tool-use semantics say any tool call in the step means
the turn is not final; the mixed shape was an exploited attack surface (F-003).
**Do this instead:** the whole step is rejected and replanned — see the
terminal-exclusivity gate at the top of
`internal/agent/llm_agent_dispatch.go`.

### Building the AG-UI fanout once at channel start

**What happens:** a channel subscribes a single fanout in `Start`.
**Why it's wrong:** fanout is per-turn (subscribe-before-run); a start-time fanout
leaks across turns and drops events.
**Do this instead:** build a fresh `Fanout` inside the turn handler — see the
contract comment in `internal/channels/channel.go` and the implementation in
`internal/channels/telegram/bot_dispatch_turn.go`.

### Reshuffling the tool manifest

**What happens:** manifest entries are emitted in map-iteration order.
**Why it's wrong:** any reshuffle poisons the provider-side prompt cache.
**Do this instead:** `Registry.Render` sorts alphabetically by name
(`internal/agent/tools/manifest.go`).

### Storing `InvocationContext` on a long-lived struct

**What happens:** a service caches the invocation context for reuse.
**Why it's wrong:** it is single-`Run`-scoped by contract; reuse leaks budget and
trace identity across runs.
**Do this instead:** pass it by value and use `WithContext`/`WithSubAgent`, which
always return a copy (`internal/agent/agent.go`).

## Error Handling

**Strategy:** wrapped errors (`fmt.Errorf("...: %w", err)`) up to a boundary that
decides between fail-fast and fail-soft.

**Patterns:**
- Boot errors from `bootServe` are returned so the daemon exits cleanly with no
  leaked pool or MCP process; the CLI exits with `exitInfra`.
- Daemon subsystems (channels, scheduler seeds, projections) log at WARN and keep
  running — a failed channel never kills the daemon.
- The agent loop distinguishes infrastructure errors (error slot) from termination
  (terminal `Event`).
- Provider failures go through a circuit breaker and retry policy
  (`internal/llm/breaker.go`, `internal/agent/llm_agent_retry.go`,
  `llm_agent_stream_retry.go`).
- MCP tool failures are mapped to structured tool errors
  (`internal/mcp/tool_error.go`) rather than surfacing raw transport faults.

## Cross-Cutting Concerns

**Logging:** `log/slog` everywhere; redaction before emit via `internal/redact/`
and `internal/mcp/redact.go`.
**Observability:** `internal/obs/` (spans, metrics), `internal/cachemetrics/`,
`internal/tracesink/`, Prometheus exposed on the AG-UI mux
(`internal/agui/server.go`), `observability/` for the collector config.
**Validation:** strict JSON decoding at the HTTP boundary
(`internal/agui/strict_decode.go`); tool arguments validated against the spec
schema before dispatch; defense-in-depth re-validation downstream
(`internal/agent/tools/skill_write.go`).
**Authentication:** `internal/webauth/` and `internal/agui/auth.go` for the web
surface, `internal/identity/` + `internal/identityctx/` for identity propagation,
`internal/mcpoauth/` for MCP OAuth, per-tenant derived credentials for ArcadeDB
(`internal/arcadedb/tenant.go`).
**Authorization / HITL:** `internal/gateway/` classifies and gates mutating tool
calls; `internal/approvalgrants/`, `internal/breakglass/`, `internal/skillacl/`
carry the grant surfaces.
**Secrets:** `internal/secret/`, `internal/mcp/process_env.go`.

---

*Architecture analysis: 2026-09-07*
