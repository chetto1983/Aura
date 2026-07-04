<!-- refreshed: 2026-07-04 -->
# Architecture

**Analysis Date:** 2026-07-04

## System Overview

```text
┌───────────────────────────────────────────────────────────────────────────┐
│                         Entry points (cmd/aura)                          │
│  aura serve (daemon)   aura chat/shell (REPL)   aura <sub> (CLI tools)   │
│  `cmd/aura/serve.go`   `cmd/aura/chat.go`        `cmd/aura/*.go`         │
└───────────┬───────────────────┬───────────────────────┬──────────────────┘
            │                   │                       │
            ▼                   ▼                       ▼
┌───────────────────┐ ┌───────────────────┐  ┌────────────────────────────┐
│  AG-UI HTTP layer  │ │  Telegram channel  │  │  Composition root helpers │
│  `internal/agui`   │ │ `internal/channels │  │  (buildBaseRegistry,      │
│  SSE /agent/run    │ │   /telegram`       │  │   bootChatEnv)            │
└─────────┬──────────┘ └─────────┬──────────┘  └──────────────┬─────────────┘
          │                      │                             │
          └──────────┬───────────┘                             │
                      ▼                                        │
         ┌─────────────────────────────┐                       │
         │   Runner (turn driver)      │◄──────────────────────┘
         │   `internal/runner`         │  wires Agent + Registry + Gateway
         └───────────┬─────────────────┘  + ConversationStore per turn
                      ▼
         ┌─────────────────────────────┐
         │   Agent tree (agent.Agent)  │
         │   `internal/agent`          │   LlmAgent = leaf; swarm = fan-out
         │   Budget-bounded, Event     │   parent (`internal/swarm`)
         │   stream via iter.Seq2      │
         └───────────┬─────────────────┘
                      ▼
   ┌───────────────────────────────────────────────────────────┐
   │        Tool dispatch + policy gateway                     │
   │  `internal/agent/tools` (Registry, deferred specs)        │
   │  `internal/gateway` (PEP: Decide → Allow/Deny/Approve)     │
   └───────────┬─────────────────────────────┬─────────────────┘
               ▼                             ▼
   ┌─────────────────────┐       ┌───────────────────────────────┐
   │  In-process tools    │       │  MCP-mounted tools             │
   │  fs/shell/web/skill/  │       │  `internal/mcp`, `agent/mcptools│
   │  swarm_spawn/etc.     │       │  (stdio/HTTP MCP servers)      │
   └───────────────────────┘       └───────────────────────────────┘
                      │
                      ▼
   ┌───────────────────────────────────────────────────────────┐
   │                    Persistence layer                       │
   │  Postgres `aura.*` (sqlc) `internal/db`, `internal/*store` │
   │  Neo4j graph `internal/knowledge`, `internal/neostore`     │
   │  Object storage `internal/objectstore` (S3-compatible)     │
   │  Filesystem `$AURA_RUN_DIR`, `~/.aura/agents/<id>`         │
   └───────────────────────────────────────────────────────────┘
```

## Component Responsibilities

| Component | Responsibility | File |
|-----------|----------------|------|
| CLI dispatcher | Parses `os.Args[1]`, routes to per-subcommand `run*` functions | `cmd/aura/main.go` |
| Composition root (`buildBaseRegistry*`) | Builds the shared `*tools.Registry` every boot path uses; fails closed if no non-deferred tool is registered | `cmd/aura/main.go` |
| Daemon boot (`runServe`/`serveEnv`) | Wires HTTP servers (AG-UI, setup wizard), scheduler, channels, reconciler, asset worker; owns graceful shutdown | `cmd/aura/serve.go`, `cmd/aura/serve_bootstrap.go` |
| AG-UI HTTP gateway | Translates HTTP/SSE `POST /agent/run` into a `Runner.Turn` call and streams AG-UI protocol events back | `internal/agui/server.go` |
| Runner | Single per-turn entry point: resolves conversation, drives the `Agent.Run` stream, persists turns, applies resume/HITL | `internal/runner/runner.go` |
| Agent contract | Open `Agent` interface (`Run`, `SubAgents`, `FindAgent`) + `InvocationContext` (value-copy, never mutated) + shared `*Budget` tree | `internal/agent/agent.go` |
| LlmAgent | The concrete leaf agent: builds LLM requests, dispatches tool calls, emits `Event`s, applies retries/pauses | `internal/agent/llm_agent.go` + `llm_agent_*.go` |
| Swarm | Fan-out parent agent: spawns N `LlmAgent` workers per goal in budget-bounded concurrent waves, isolates per-child failure | `internal/swarm/swarm.go` |
| Tool registry | Immutable-per-run registry of `Tool` implementations; deferred-spec pattern keeps the LLM manifest small | `internal/agent/tools/registry.go`, `spec.go` |
| Policy gateway (PEP) | Single in-process policy-enforcement point: classifies risk tier, decides Allow/Deny/Approve, records to the append-only ledger | `internal/gateway/gateway.go`, `decide.go`, `classify.go` |
| MCP client/mount | stdio/HTTP MCP transport, SSRF guard, retry-with-backoff mount at boot | `internal/mcp/client.go`, `internal/agent/mcptools` |
| Telegram channel | Bot dispatch, HITL relay, rendering (MarkdownV2/tables/TTS), onboarding | `internal/channels/telegram/*.go` |
| Scheduler | Cron-like task dispatch (reminder/agent_job/backup_*), claim/heartbeat/recover semantics | `internal/cron/scheduler.go`, `dispatch.go` |
| Documents/ingest | Upload → extract → chunk → embed → index (BM25 + vector) → GraphRAG pipeline | `internal/documents/service.go`, `worker.go`, `indexer.go` |
| Knowledge graph | Neo4j schema/migrations + read-only GraphView normalizer for the cockpit graph explorer | `internal/knowledge/client.go`, `graphview.go` |
| Config | Env-driven `*config.Config` resolution + validation + runtime profile gating | `internal/config/config.go`, `config_validate.go`, `config_runtimeprofile.go` |
| Web cockpit (SPA) | React/Vite frontend consuming the AG-UI SSE stream and REST endpoints | `web/src/*` |

## Pattern Overview

**Overall:** Layered agent-runtime monolith — a single Go binary (`cmd/aura`) hosting an LLM agent loop, a policy-enforcement gateway, a scheduler, a channel adapter (Telegram), and an HTTP/SSE gateway (AG-UI) over a shared composition root. The React SPA (`web/`) is a separate build artifact served by the same daemon.

**Key Characteristics:**
- **Open interface + composition root, not DI framework.** `agent.Agent` is an intentionally unsealed interface (`internal/agent/agent.go`); every subsystem (LlmAgent, swarm workers) implements it directly. There is no DI container — `cmd/aura/serve_bootstrap.go` / `chat_boot.go` wire concrete types by hand at boot.
- **Single Policy Enforcement Point (PEP).** All mutating tool dispatch passes through `internal/gateway` (`Gateway.Decide`) before `tool.Execute` — classification (`classify.go`), reservation/idempotency (`reserve.go`), and cross-turn approvals (`approvals.go`) are centralized, not scattered per-tool.
- **Deferred-tool manifest.** Large tool specs (long descriptions/schemas) are excluded from the default LLM-visible manifest (`Spec.Deferred = true`); the model calls the built-in `tool_search` hook to fetch them on demand — this bounds prompt-cache cost as the tool count grows (`internal/agent/tools/spec.go`).
- **Budget-bounded execution tree.** A single shared `*Budget` (atomic step counter + dedup ring) travels through the whole `InvocationContext` tree, including forked swarm children (`Budget.Child`), so runaway recursion/loops are bounded process-wide, not per-agent.
- **Event-stream-first agent contract.** `Agent.Run` returns `iter.Seq2[*Event, error]` — termination, pause (HITL `ask_user`), and budget exhaustion are all explicit `Event`s, never encoded in the error slot (D-04 in code comments).
- **Fail-soft external mounts, fail-closed core.** MCP server mounts are fail-soft (a broken server WARN-and-drops, boot continues); the tool registry itself is fail-closed (`Registry.Validate()` exits the process if zero non-deferred tools are registered).
- **sqlc-generated DB access, no ORM.** All Postgres access goes through `internal/db/sqlc` generated code over `pgx/v5`; Neo4j access goes through the driver in `internal/neostore`/`internal/knowledge`, with `mcp-neo4j-cypher` as the LLM-facing Cypher interface (not a Go driver call from the agent's perspective).

## Layers

**CLI / entry layer:**
- Purpose: parse subcommand args, delegate to the corresponding `run*` function
- Location: `cmd/aura/main.go` (dispatch table), `cmd/aura/<subcommand>.go` (per-command logic)
- Contains: argument parsing, usage strings, exit codes (`cmd/aura/exit_codes.go`)
- Depends on: every `internal/*` package it wires
- Used by: the OS process entry point only

**HTTP/channel layer:**
- Purpose: translate an external protocol (AG-UI SSE, Telegram Bot API) into a `Runner.Turn` call and back
- Location: `internal/agui/*.go`, `internal/channels/telegram/*.go`, `internal/channels/*.go` (registry)
- Contains: HTTP handlers, SSE pumps, protocol translators, per-channel rendering (MarkdownV2, tables, TTS)
- Depends on: `internal/runner` (narrow `Runner` interface, consumer-defined), `internal/askuser`, `internal/conversations`
- Used by: `cmd/aura/serve.go` (mounts both), `cmd/aura/chat.go` (drives the runner directly, no HTTP)

**Runner / orchestration layer:**
- Purpose: single per-turn entry point — resolves/creates a conversation, drives one `Agent.Run` invocation, persists the turn, applies HITL resume
- Location: `internal/runner/runner.go` + `runner_*.go`
- Contains: turn lifecycle, resume/pause handling, conversation eviction, learner hooks (reasoning-tier, tool-select)
- Depends on: `internal/agent`, `internal/conversations`, `internal/askuser`, `internal/gateway`
- Used by: `internal/agui`, `cmd/aura/chat.go`, `internal/channels/telegram`

**Agent runtime layer:**
- Purpose: the LLM reasoning loop and its budget/dedup/tool-dispatch machinery
- Location: `internal/agent/*.go` (LlmAgent), `internal/swarm` (fan-out parent), `internal/agent/tools` (registry + built-ins)
- Contains: `Agent` interface, `Budget`, `Event`/`Actions` model, LLM request construction, tool-call dispatch loop, hooks (`hooks.go`, `hooks_command.go`), completion-gate critic
- Depends on: `internal/llm` (client abstraction), `internal/gateway` (policy), `internal/agent/tools`
- Used by: `internal/runner`, `internal/cron` (agent_job handler), `internal/swarm` (as worker)

**Policy / governance layer:**
- Purpose: single enforcement point for mutating tool dispatch — risk classification, allow/deny/approve, append-only ledger, HITL approval challenges
- Location: `internal/gateway/*.go`
- Contains: `Gateway` (holds runtime profile + ledger store + approvals), `Decision`/`Verdict` vocabulary, reconciler for crash-orphaned reservations
- Depends on: `internal/scoring` (risk tiers), `internal/toolinvocations` (ledger store), `internal/config` (runtime profile)
- Used by: `internal/agent` (LlmAgent's dispatch path), `internal/swarm` (injected per worker), `internal/cron` (agent_job)

**Persistence layer:**
- Purpose: durable state — conversations, identity/capabilities, scheduler tasks, tool-invocation ledger, documents/embeddings, knowledge graph
- Location: `internal/db` (sqlc client + migrations), `internal/conversations`, `internal/identity`, `internal/cron`, `internal/toolinvocations`, `internal/documents`, `internal/knowledge`, `internal/neostore`, `internal/objectstore`
- Contains: sqlc-generated queries (`internal/db/sqlc`), raw SQL (`internal/db/queries`), golang-migrate migrations (`internal/db/migrations`), Neo4j Cypher migration (`internal/knowledge`)
- Depends on: `pgx/v5`, `neo4j-go-driver/v5` (driver present as a library dependency; LLM-facing graph access goes through the `mcp-neo4j-cypher` MCP server, not this driver, per project convention)
- Used by: nearly every other layer

**Web cockpit (separate module):**
- Purpose: React/Vite SPA consuming the AG-UI SSE stream + REST endpoints (governance, settings, onboarding, graph explorer)
- Location: `web/src/*` (own `package.json`, own build)
- Contains: `chat/` (assistant-ui-based chat surface), `governance/`, `graph/` (sigma.js graph explorer), `onboarding/`, `settings/`, `shell/` (AppShell)
- Depends on: the AG-UI HTTP API (`internal/agui`) only, via generated/typed fetch clients in `web/src/api`
- Used by: served as static assets by the daemon (`internal/webui`, `cmd/aura/serve_webui.go`)

## Data Flow

### Primary Request Path (web cockpit turn)

1. Browser POSTs `RunAgentInput` to `POST /agent/run` (`internal/agui/server.go:handleRun`)
2. Server resolves the thread, applies any protocol-native HITL resume entries via `Runner.SubmitAnswers`, and builds the per-turn user message (attachments + doc catalog context) (`internal/agui/server_context.go`)
3. `Runner.Turn` drives one `Agent.Run` invocation over the resolved `LlmAgent` (`internal/runner/runner.go`)
4. `LlmAgent.Run` builds the LLM request, dispatches tool calls through `internal/agent/tools.Registry` gated by `internal/gateway.Gateway.Decide`, and yields `*Event`s (`internal/agent/llm_agent.go`, `llm_agent_dispatch.go`)
5. `Translate` (`internal/agui/translator.go`) converts the `Event` stream into AG-UI protocol events; `streamSSE` pumps them over the HTTP response as Server-Sent Events (`internal/agui/server.go`)
6. Terminal turn state (messages, tool results) is persisted through `internal/conversations` before/while streaming

### Telegram Turn Path

1. `telebot` webhook/poll delivers an update to `internal/channels/telegram/bot_dispatch.go`
2. Auth/onboarding gates resolve the identity (`bot_dispatch_auth.go`, `onboarding.go`)
3. The same `Runner.Turn` is invoked, subscribed via `agui_subscriber.go` (in-process `agui.Translate` + fanout, not HTTP)
4. `renderer.go`/`mdv2.go`/`tables.go` convert the streamed content to Telegram MarkdownV2 + native tables/voice
5. `deliver.go` sends the rendered message(s) back through the Telegram Bot API

### Swarm Fan-Out (tool-triggered sub-agents)

1. The parent `LlmAgent` dispatches the `swarm_spawn` tool, which resolves live parent budget/registry/client/config off the tool-call context (`agent.WithSwarmContext`)
2. `swarm.Run` reserves a fixed budget slice for the parent's post-swarm synthesis turn, then runs goals in concurrency-capped waves (`internal/swarm/swarm.go:runWave`)
3. Each wave spawns child `LlmAgent`s with a forked `Budget.Child`, the parent registry minus `swarm_spawn` (no nested swarms), and the SAME gateway + originating conversation ID (Open Q1: ledger keys on the real conversation UUID, not the flat worker session)
4. A failed/timed-out child never cancels its siblings (`errgroup` result captured into its own report slot, goroutine returns nil)
5. `marshalReports` returns the ordered `[]ChildReport` as a JSON tool result string back to the parent

**State Management:**
- Per-run state (`Budget`, `InvocationContext`) is value-copied down the tree — `WithContext`/`WithSubAgent` never mutate the receiver, so concurrent branches (swarm waves) cannot race the parent's context.
- Durable state (conversations, tool-invocation ledger, scheduler tasks) lives exclusively in Postgres; nothing agent-runtime-scoped is cached in package-level globals except explicitly-scoped session maps (e.g. `ShellApprovals`, `BackgroundShells` in `internal/agent/tools`) which are per-boot singletons, not per-request.

## Key Abstractions

**Agent (interface):**
- Purpose: uniform contract for anything that can `Run` an `InvocationContext` and stream `Event`s — leaf LLM agents and workflow/swarm parents alike
- Examples: `internal/agent/llm_agent.go` (LlmAgent), swarm workers (constructed via `agent.NewLlmAgent` in `internal/swarm/swarm.go`)
- Pattern: open interface (no unexported seal, deliberate divergence from google/adk-go) so any package can implement it without importing `internal/agent`'s constructor

**Budget:**
- Purpose: shared, atomic step-count + dedup-ring gate that bounds a whole invocation tree, including forked children
- Examples: `internal/agent/budget.go`, `budget_dedup.go`; forked via `Budget.Child` in swarm
- Pattern: reference type (`*Budget` wrapping `*atomic.Int32`) passed through `InvocationContext` by pointer so all branches of a tree share the same counter

**Tool (interface) + Registry:**
- Purpose: uniform dispatchable capability surface (fs, shell, web, skill, MCP-mounted tools, swarm_spawn) with a manifest-visible `Spec`
- Examples: `internal/agent/tools/action.go` (Tool interface), `registry.go` (immutable-per-run Registry), every `*.go` file in `internal/agent/tools` implementing one tool
- Pattern: deferred-spec — big tools set `Spec.Deferred = true` and are fetched on demand via the `tool_search` built-in, keeping the default LLM manifest small (mandatory convention, see CLAUDE.md "Tool design")

**Gateway (Policy Enforcement Point):**
- Purpose: the single place a mutating tool dispatch is classified, allowed/denied/held-for-approval, and recorded to an append-only ledger
- Examples: `internal/gateway/gateway.go` (struct + Decision/Verdict), `decide.go` (PEP), `classify.go` (risk-tier classification), `reserve.go` (idempotency reservation)
- Pattern: narrow consumer-defined `reservationStore` interface over `*toolinvocations.Store`; a `nil *Gateway` is an Allow no-op (dev-parity)

**ConversationStore / Runner narrow interfaces:**
- Purpose: keep consumers (agui server, channels) decoupled from the full `runner.Runner`/`conversations.Store` surface
- Examples: `internal/agui/server.go` declares its own `Runner` interface with only `Turn`/`SubmitAnswers`/`NewConversation`/`TurnBranch`
- Pattern: interfaces defined where consumed (golang-structs-interfaces convention), never in the producing package

## Entry Points

**`aura serve` (daemon):**
- Location: `cmd/aura/serve.go` (`runServe`)
- Triggers: production/long-lived process start
- Responsibilities: boots the shared composition root (`bootChatEnv`), mounts the AG-UI HTTP server + setup wizard HTTP server, starts the scheduler tick loop, the Telegram channel registry, the crash-orphan reconciler, the asset-processing worker, and the sidecar sweeper; blocks on SIGINT/SIGTERM then drains gracefully

**`aura chat` / `aura shell` (interactive):**
- Location: `cmd/aura/chat.go`, `cmd/aura/shell.go`
- Triggers: developer/operator interactive session
- Responsibilities: same composition root as serve (`bootChatEnv`) minus the HTTP/channel/scheduler daemons; drives the `Runner` directly in a REPL

**`POST /agent/run` (AG-UI HTTP):**
- Location: `internal/agui/server.go:handleRun`
- Triggers: web cockpit SPA turn submission
- Responsibilities: validate input, resolve/lock the thread, apply resume entries, stream the translated `Agent.Run` output as SSE

**Telegram bot update:**
- Location: `internal/channels/telegram/bot_dispatch.go`
- Triggers: Telegram webhook/long-poll delivery
- Responsibilities: auth/onboarding gate, drive the shared `Runner`, render output to Telegram-native formatting

**Scheduler tick:**
- Location: `internal/cron/scheduler.go`
- Triggers: fixed-interval tick inside the `serve` daemon
- Responsibilities: claim due tasks, dispatch to per-`TaskKind` handlers (reminder/agent_job/backup_*), record run history, heartbeat/recover stuck claims

## Architectural Constraints

- **Threading:** Single Go process, no dedicated worker-thread model. Concurrency is goroutine + channel based: swarm fan-out uses `errgroup.WithContext` with a per-wave concurrency cap (`internal/swarm/swarm.go`); the AG-UI SSE pump uses a producer goroutine + buffered channel per connection (`internal/agui/server.go:streamSSE`); the scheduler, sweeper, and asset-processing worker are each a single background goroutine started/stopped by `serveEnv`.
- **Global state:** Deliberately minimal. Per-boot singletons exist for session-scoped concerns only: `tools.BackgroundShells` / `tools.ShellApprovals` (shell_exec session state), `gateway.GatewayApprovals` (cross-turn approval ledger, evicted per-conversation via `EvictSession`). No package-level mutable config or DB handle; everything is threaded through constructors from the composition root in `cmd/aura`.
- **Circular imports:** Explicitly engineered around via adapters at the composition root. Example (documented in `cmd/aura/serve.go` header comment): `internal/agent/tools` imports `internal/cron` (for the `task` tool), so `internal/cron`'s per-`TaskKind` handlers cannot import `internal/agent/tools` back — the composition root (`cmd/aura`) imports both and adapts between them. Similarly `internal/swarm` cannot be imported by `internal/cron` (D-24), so the `tools.Without` helper was promoted out of `internal/swarm` into `internal/agent/tools` so `internal/cron` can reuse it without the forbidden import.
- **Single shared Budget invariant:** Every node in one invocation tree (including swarm children) must share the same `*Budget` (same underlying `*atomic.Int32` and dedup ring) unless explicitly forked via `Budget.Child` — this is enforced by convention/tests, not the type system, so new workflow-parent code must be reviewed against `agent.BudgetOwner`.

## Anti-Patterns

### Sealed interfaces for extensibility points

**What happens:** A newer contributor might be tempted to add an unexported marker method to `Agent` (mirroring google/adk-go) to "lock down" who can implement it.
**Why it's wrong:** The project explicitly reverses adk-go's sealed-interface choice (D-01 in `internal/agent/agent.go`) so `LlmAgent` and swarm workers can implement `Agent` directly without a blessed constructor.
**Do this instead:** Keep `Agent` open; add new capabilities as optional interfaces the caller type-asserts for (see `BudgetOwner` pattern in the same file).

### Bypassing the Gateway for "simple" mutating tools

**What happens:** Adding a new mutating tool that calls `tool.Execute` directly from the agent loop without routing through `gateway.Gateway.Decide`.
**Why it's wrong:** breaks GATE-01 (single Policy Enforcement Point) — the tool escapes risk classification, the append-only ledger, and cross-turn approval semantics, silently reopening the exact gap Phase 35 closed.
**Do this instead:** Every mutating tool dispatch goes through the injected `*gateway.Gateway` at the `LlmAgent` dispatch site; a `nil` Gateway is already a safe Allow no-op for tests/standalone construction, so there is no reason to skip it in production wiring.

### Storing `InvocationContext` on a long-lived struct

**What happens:** Caching an `InvocationContext` (or its `Ctx`/`Budget`) on a service struct to "avoid re-threading it".
**Why it's wrong:** `InvocationContext` is single-Run-scoped by explicit contract (doc comment in `internal/agent/agent.go`); caching it defeats the value-copy invariant (`WithContext`/`WithSubAgent` never mutate the receiver) and can leak a cancelled context or stale budget across turns.
**Do this instead:** Pass `InvocationContext` by value down the call chain for the lifetime of one `Run`; never assign it to a struct field that outlives the call.

## Error Handling

**Strategy:** Errors are wrapped with `%w` and contextual messages at each layer boundary; the agent's `Event` stream separates "real" errors (LLM/tool failures, surfaced via the error slot of `iter.Seq2`) from control-flow signals (termination, budget exhaustion, HITL pause), which are always explicit `Event`s — never encoded as errors (documented as D-04 in `internal/agent/agent.go`).

**Patterns:**
- Domain rejections that the LLM should self-correct from (e.g. swarm preflight failures — bad depth, too many goals, insufficient budget) are returned as a model-readable `"error: ..."` string in the tool result, not a Go `error` — this lets the model retry/adjust instead of the turn hard-failing (`internal/swarm/swarm.go:preflight`)
- Gateway denials use a typed `*gateway.ErrDenied` carrying `Reason` + `Tier`, rendered as a control-plane string the model sees as a tool error
- Panics inside concurrent swarm children are recovered per-goroutine and converted into a `{status:failed}` `ChildReport` (`panicChildReport`) so one worker's panic never crashes the wave or the parent

## Cross-Cutting Concerns

**Logging:** `log/slog` throughout, structured key-value fields (e.g. `slog.Warn("mcp mount failed", "server", name, "err", err)`); no `fmt.Println`/`log.Printf` in production paths.
**Validation:** Config validation is centralized in `internal/config/config_validate.go` and runs at every boot path before the composition root wires anything; per-request validation (AG-UI) lives at the HTTP boundary (`ValidateRunInput` in `internal/agui`).
**Authentication:** Authula-based web-auth provider (`internal/webauth`) gates the cockpit HTTP routes; capability-based authorization (`identity.capability_grants`) gates individual mutating routes (`RequireCapability` in `cmd/aura/serve_webui.go`).

---

*Architecture analysis: 2026-07-04*
