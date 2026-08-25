<!-- refreshed: 2026-08-25 -->
# Architecture

**Analysis Date:** 2026-08-25

## System Overview

```text
┌──────────────────────────────────────────────────────────────────────────────┐
│ Transport and operator surfaces                                              │
├──────────────────────┬──────────────────────┬────────────────────────────────┤
│ React/Vite cockpit   │ Telegram channel     │ CLI / scheduled work           │
│ `web/src/`           │ `internal/channels/` │ `cmd/aura/` · `internal/cron/` │
└──────────┬───────────┴───────────┬──────────┴───────────────┬────────────────┘
           │ AG-UI/REST + SSE       │ AG-UI event fanout       │ direct adapters
           ▼                        ▼                          ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│ Process composition and transport adapters                                   │
│ `cmd/aura/` · `internal/agui/` · `internal/agentrender/` · `internal/setup/` │
└──────────────────────────────────────┬───────────────────────────────────────┘
                                       ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│ Durable turn orchestration                                                   │
│ `internal/runner/` · `internal/conversations/` · `internal/askuser/`         │
└──────────────────────────────────────┬───────────────────────────────────────┘
                                       ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│ Agent runtime and policy-controlled capability dispatch                      │
│ `internal/agent/` · `internal/agent/tools/` · `internal/gateway/`            │
│ `internal/swarm/` · `internal/agent/mcptools/` · `internal/llm/`             │
└──────────────┬───────────────────────┬───────────────────────┬───────────────┘
               ▼                       ▼                       ▼
┌──────────────────────┐ ┌────────────────────────┐ ┌──────────────────────────┐
│ Postgres control     │ │ Per-identity data      │ │ External execution       │
│ `internal/db/`       │ │ ArcadeDB + objectstore │ │ sandbox, MCP, LLM, web   │
│ domain stores        │ │ `internal/arcadedb/`   │ │ `internal/sandbox/`      │
│                      │ │ `internal/objectstore/`│ │ `internal/mcp/`          │
└──────────────────────┘ └────────────────────────┘ └──────────────────────────┘
```

Aura is a modular Go monolith with explicit ports-and-adapters boundaries, three executable composition roots, an embedded React cockpit, and out-of-process infrastructure. The long-lived daemon is assembled in `cmd/aura/`; domain packages remain under `internal/` and expose narrow interfaces to their consumers.

## Component Responsibilities

| Component | Responsibility | File |
|-----------|----------------|------|
| Aura CLI/daemon | Dispatch CLI verbs, assemble process-wide dependencies, start and drain daemon siblings | `cmd/aura/main.go`, `cmd/aura/serve.go`, `cmd/aura/chat_boot.go` |
| AG-UI gateway | Authenticate/authorize HTTP requests, owner-scope resources, translate agent events to SSE, expose cockpit REST APIs | `internal/agui/server.go`, `internal/agui/server_run.go`, `internal/agui/translator.go` |
| Embedded cockpit host | Serve the committed Vite bundle and SPA fallback without depending on other internal packages | `internal/webui/embed.go`, `cmd/aura/serve_webui.go` |
| React cockpit | Manage operator surfaces, remote server state, assistant-ui runtime state, and resilient AG-UI streams | `web/src/main.tsx`, `web/src/AppShell.tsx`, `web/src/chat/ExternalStoreChat.tsx`, `web/src/chat/sseResume.ts` |
| Runner | Serialize turns per identity/conversation, persist visible history and pauses, rebuild a fresh agent for every turn | `internal/runner/runner.go`, `internal/runner/runner_session.go`, `internal/runner/runner_persist.go` |
| Agent runtime | Enforce budgets, build cache-stable prompts, stream the LLM, dispatch tools, and emit the canonical event stream | `internal/agent/agent.go`, `internal/agent/llm_agent.go`, `internal/agent/llm_agent_dispatch.go` |
| Tool registry | Register built-ins, hide deferred schemas until needed, cap/spill results, and expose the `Tool` contract | `internal/agent/tools/spec.go`, `internal/agent/tools/registry.go`, `internal/agent/tools/result.go` |
| Policy gateway | Classify calls, withhold destructive work for approval, reserve mutations, and reconcile crash orphans | `internal/gateway/gateway.go`, `internal/gateway/decide.go`, `internal/gateway/reserve.go`, `internal/gateway/reconcile.go` |
| MCP bridge | Mount stdio or Streamable HTTP servers, namespace their tools, supervise reconnects, and propagate identity/operation metadata | `internal/agent/mcptools/mount.go`, `internal/agent/mcptools/bridge.go`, `internal/mcp/sdkclient.go` |
| Scheduler | Claim due tasks, dispatch kind-specific handlers, persist run outcomes, and deliver notifications | `internal/cron/scheduler.go`, `internal/cron/dispatch.go`, `internal/cron/handlers/` |
| Channels | Run fail-soft channel siblings and adapt each turn to channel-specific rendering/HITL | `internal/channels/channel.go`, `internal/channels/registry.go`, `internal/channels/telegram/bot.go` |
| Postgres layer | Own pool configuration, migration gates, RLS/transaction helpers, SQL sources, and generated query bindings | `internal/db/db.go`, `internal/db/tx.go`, `internal/db/rls.go`, `internal/db/queries/`, `internal/db/sqlc/` |
| Domain stores | Encapsulate Postgres access by capability instead of exposing sqlc to transports | `internal/conversations/`, `internal/identity/`, `internal/assets/`, `internal/settings/`, `internal/share/` |
| ArcadeDB layer | Provide per-identity graph memory and hybrid document retrieval through server-enforced database isolation | `internal/arcadedb/client.go`, `internal/arcadedb/tenant_clients.go`, `internal/arcadedb/memory.go`, `internal/arcadedb/document_retrieval.go` |
| ArcadeDB MCP | Expose model-facing memory/graph tools over Streamable HTTP | `cmd/arcadedb-mcp/main.go`, `cmd/arcadedb-mcp/tool_memory.go`, `cmd/arcadedb-mcp/tool_graph_schema.go` |
| Document ingestion | Reconcile identity-scoped object-store files into ArcadeDB passages/cards through CocoIndex | `services/ingest/app.py`, `services/ingest/arcade.py`, `services/ingest/extract.py` |
| Sandbox | Resolve one persistent box per identity and route shell/file operations into it with fail-closed behavior | `internal/sandbox/usersandbox/router.go`, `internal/sandbox/usersandbox/backend.go`, `internal/sandbox/usersandbox/docker_backend.go` |
| Observability | Emit structured logs, OTel spans, Prometheus/expvar metrics, and durable forensic facts | `internal/obs/`, `internal/agent/metrics.go`, `internal/toolinvocations/`, `internal/cachemetrics/` |

## Pattern Overview

**Overall:** Modular monolith with manual dependency injection, consumer-declared ports, transport adapters, and sidecar-backed infrastructure.

**Key Characteristics:**
- Keep executable packages thin and assemble cross-package dependencies in `cmd/aura/`; `cmd/aura/chat_boot.go` constructs the shared interactive runtime and `cmd/aura/serve.go` adds daemon siblings.
- Declare narrow interfaces at the consuming package, as in `internal/agui/server.go`, `internal/cron/dispatch.go`, `internal/runner/runner.go`, and `internal/sandbox/usersandbox/backend.go`.
- Break otherwise-cyclic dependencies at the composition root with adapters, as in `cmd/aura/serve_dispatch.go` for `cron` versus `cron/handlers` and `cmd/aura/serve_adapters.go` for tool/store bridges.
- Represent long-running output as `iter.Seq2` streams from `internal/agent/agent.go` through `internal/runner/runner.go` and `internal/agui/translator.go`.
- Build process-wide services once, but build a fresh `agent.LlmAgent` for each durable turn in `internal/runner/runner.go`.
- Keep heavyweight or separately secured capabilities out of process: ArcadeDB memory in `cmd/arcadedb-mcp/`, ingestion in `services/ingest/`, and identity workspaces behind `internal/sandbox/usersandbox/`.

## Layers

**Transport and Presentation:**
- Purpose: Accept operator input and render durable/run-time state for web, Telegram, and CLI clients.
- Location: `internal/agui/`, `internal/channels/`, `internal/agentrender/`, `internal/setup/`, `web/src/`.
- Contains: HTTP route handlers, auth/capability middleware, SSE translation, channel renderers, React routes and workspaces.
- Depends on: Consumer-side runner/store interfaces in `internal/agui/server.go`, `internal/channels/telegram/bot.go`, and API clients in `web/src/`.
- Used by: `cmd/aura/serve.go`, `cmd/aura/serve_webui.go`, and browser/Telegram clients.

**Composition and Lifecycle:**
- Purpose: Wire concrete stores, tools, policy, channels, schedulers, sidecars, and shutdown ordering.
- Location: `cmd/aura/`.
- Contains: CLI dispatch, `chatEnv`/`serveEnv`, adapters, route mounting, and lifecycle/drain code.
- Depends on: All internal packages required by the executable.
- Used by: `cmd/aura/main.go` only; internal packages never import `cmd/aura/`.

**Turn Orchestration:**
- Purpose: Convert a transport request into one owner-scoped durable turn, including pause/resume and conversation lifecycle.
- Location: `internal/runner/`, `internal/conversations/`, `internal/askuser/`.
- Contains: Per-thread locks, history rehydration, context management, persistence, resume committers, deletion reconciliation.
- Depends on: Narrow store interfaces, `internal/agent/`, `internal/llm/`, and `internal/gateway/`.
- Used by: `internal/agui/`, `internal/channels/telegram/`, CLI chat/shell, and scheduled agent jobs.

**Agent Runtime:**
- Purpose: Run bounded model/tool loops and emit a transport-neutral event stream.
- Location: `internal/agent/`, `internal/agent/prompt/`, `internal/agent/workflow/`, `internal/swarm/`.
- Contains: `Agent`, `InvocationContext`, `Budget`, `Event`, `LlmAgent`, hooks, workflow agents, swarm fan-out.
- Depends on: `internal/llm/`, `internal/agent/tools/`, `internal/gateway/`, and small utility packages.
- Used by: `internal/runner/` and `internal/cron/handlers/`.

**Policy and Capability Dispatch:**
- Purpose: Discover tools, classify risk, enforce approval/idempotency, execute in the correct trust boundary, and feed results back to the model.
- Location: `internal/agent/tools/`, `internal/gateway/`, `internal/agent/mcptools/`, `internal/sandbox/usersandbox/`.
- Contains: Tool specs/registry, deferred schema promotion, mutation metadata, policy decisions, MCP mounts, sandbox routing.
- Depends on: Consumer-declared execution/store ports plus `internal/idempotency/`, `internal/scoring/`, and `internal/identityctx/`.
- Used by: `internal/agent/LlmAgent` through `internal/agent/llm_agent_tool.go` and `internal/agent/llm_agent_retry.go`.

**Domain Capabilities:**
- Purpose: Implement conversations, assets, documents, identities, skills, scheduling, onboarding, settings, sharing, retention, and web access.
- Location: Domain directories directly under `internal/`, including `internal/assets/`, `internal/documents/`, `internal/skills/`, `internal/cron/`, and `internal/share/`.
- Contains: Services, stores, pure domain types, and external-adapter seams.
- Depends on: `internal/db/sqlc/` or narrow external clients where persistence is required.
- Used by: Composition adapters in `cmd/aura/`, transport handlers in `internal/agui/`, and tools in `internal/agent/tools/`.

**Persistence and External Adapters:**
- Purpose: Isolate protocol details for Postgres, ArcadeDB, S3/Garage, LLM providers, web search, and multimodal services.
- Location: `internal/db/`, `internal/arcadedb/`, `internal/objectstore/`, `internal/llm/`, `internal/mcp/`, `internal/web/`, `internal/multimodal/`.
- Contains: pgx/sqlc adapters, HTTP/SSE clients, object-store interfaces, MCP transports, sidecar clients.
- Depends on: Standard library and pinned third-party clients from `go.mod`.
- Used by: Domain stores/services and `cmd/aura/` composition.

## Data Flow

### Primary Web Agent Request

1. React mounts router/query providers in `web/src/main.tsx:39`, and `web/src/chat/ExternalStoreChat.tsx:77` owns the assistant-ui external-store runtime.
2. A send enters the resilient AG-UI client in `web/src/chat/ExternalStoreChat.tsx:199` and `web/src/chat/sseResume.ts:348`; it posts one logical run and can reattach by run ID when detached runs are enabled.
3. The parent mux in `cmd/aura/serve_webui.go` applies whole-origin authentication and route-specific capability gates before delegating `POST /agent/run` to AG-UI.
4. `internal/agui/server_run.go:22` strictly decodes the request, validates the UUID, verifies owner access, applies reasoning governance, and builds attachment/skill context.
5. `internal/agui/server_run.go:22` acquires a non-blocking per-thread lock when supported, applies resume answers, and calls `Runner.Turn` or the visible/model-message split seam.
6. `internal/runner/runner.go:377` scopes the context to the conversation owner, persists the visible user turn, loads managed history/memory context, and creates a request UUID.
7. `internal/runner/runner.go:535` constructs a fresh `agent.LlmAgent`, a shared-budget `InvocationContext`, per-turn verification hooks, and a deadline-bounded context.
8. `internal/agent/llm_agent.go:203` builds cache-stable requests, streams the provider through `internal/llm/`, accumulates model/tool deltas, and loops until a terminal event or real failure.
9. `internal/agent/llm_agent_retry.go:89` sends each non-HITL tool call through `gateway.Decide`; allowed work executes once, destructive work may return an approval request, and replays return the recorded result.
10. Shell and file tools call `internal/sandbox/usersandbox/router.go:80`, which resolves the caller's identity box or returns an error; no host fallback exists.
11. `internal/runner/runner.go:377` persists emitted assistant/tool/pause events before yielding them to the transport.
12. `internal/agui/translator.go:63` maps Aura events to AG-UI events, and `internal/agui/server_sse.go:35` writes them as SSE for reducers in `web/src/chat/sseAdapter.ts`.

### Daemon Boot and Shutdown

1. `cmd/aura/main.go:48` loads local environment configuration and dispatches the `serve` verb.
2. `cmd/aura/serve.go:148` creates separate signal and work contexts so signal receipt stops admission before cancelling in-flight turns.
3. `cmd/aura/chat_boot.go:304` validates configuration, opens Postgres, checks migration/RLS contracts, constructs domain stores, the sandbox router, MCP mounts, tool registry, gateway, and runner.
4. `cmd/aura/serve.go:264` adds object storage, assets, channels, scheduler, AG-UI server, onboarding/provisioning, readiness, reconciliation, and background workers.
5. `cmd/aura/serve.go:148` starts HTTP, scheduler, channels, sweepers, ingestion workers, and reconcilers under joined lifecycle management.
6. `cmd/aura/serve_drain.go` stops admission, drains detached runs/SSE/background work within a configured grace, then `chatEnv.close` in `cmd/aura/chat_boot.go` closes MCP servers before Postgres.

### Telegram Turn

1. `internal/channels/registry.go` starts registered channels fail-soft; one channel failure does not abort `aura serve`.
2. `internal/channels/telegram/bot_dispatch.go` resolves the account/identity and normalizes text, media, document, or callback input.
3. `internal/channels/telegram/agui_subscriber.go:67` subscribes a per-turn renderer before calling the shared `runner.Runner`.
4. The same runner/agent/gateway path executes; `internal/channels/telegram/renderer.go` and `internal/channels/telegram/status_pane.go` render the AG-UI events for Telegram.

### Scheduled Agent Job

1. `internal/cron/scheduler.go:177` performs crash recovery/catch-up, then scans due tasks on each tick with bounded concurrency.
2. `internal/cron/dispatch.go` resolves a task kind through a handler map and owns run completion/notification.
3. `cmd/aura/serve_dispatch.go` adapts concrete handlers from `internal/cron/handlers/` onto the cron-local interface and injects the shared client, registry, gateway, and channels.
4. Agent jobs create a headless `LlmAgent`; destructive approval requiring a responder is denied/guided instead of silently executed, through `internal/gateway/approve.go`.

### Document Ingestion and Retrieval

1. Upload/presign/finalize routes in `internal/agui/assets_api.go` persist asset/job control state in Postgres through `internal/assets/` and `internal/documents/jobs_store.go`.
2. The reconciliation process in `services/ingest/app.py:460` repeatedly scans an identity-scoped object-store source; `services/ingest/app.py:523` selects one-shot versus live blocking behavior.
3. `services/ingest/extract.py`, `services/ingest/chunk.py`, and the `aura-filecard` helper at `cmd/aura-filecard/main.go:26` extract text, chunks, embeddings, and file cards.
4. `services/ingest/arcade.py` writes the schema-compatible document/passages into that identity's ArcadeDB database.
5. `internal/arcadedb/document_retrieval.go:109` asks ArcadeDB to fuse full-text and vector candidates server-side; `internal/documents/retrieval.go` applies the product retrieval contract.
6. `internal/agent/tools/document_open.go` resolves a hit's source key and materializes the original object into the caller's sandbox workspace.

**State Management:**
- Durable control-plane and conversation state lives in Postgres through `internal/db/` plus domain stores such as `internal/conversations/`, `internal/assets/`, and `internal/identity/`.
- Long-term memory and document passage indexes live in one ArcadeDB database per identity through `internal/arcadedb/` and `cmd/arcadedb-mcp/`.
- Original file bytes and exported artifacts live behind `internal/objectstore/`; deployed Garage access is identity-scoped by `internal/objectstore/identity_store.go`.
- Live per-process state is intentionally bounded: thread locks/sessions in `internal/runner/runner.go`, tool/channel registries in `internal/agent/tools/spec.go` and `internal/channels/registry.go`, and detached SSE sessions in `internal/agui/runregistry.go`.
- Browser server state is cached through `web/src/queryClient.ts`; the current assistant thread is adapted through `useExternalStoreRuntime` in `web/src/chat/ExternalStoreChat.tsx:441`.

## Key Abstractions

**`agent.Agent`:**
- Purpose: Open, streaming execution contract for leaf and workflow agents.
- Examples: `internal/agent/agent.go`, `internal/agent/llm_agent.go`, `internal/agent/workflow/sequential.go`, `internal/agent/workflow/parallel.go`.
- Pattern: Interface plus `iter.Seq2[*Event,error]`; termination is an event, while real failures use the error slot.

**`runner.Runner`:**
- Purpose: Durable orchestration around ephemeral agents; it owns conversation locking, history, pause/resume, persistence, and turn lifecycle.
- Examples: `internal/runner/runner.go`, `internal/runner/runner_session.go`, `internal/runner/runner_resume.go`.
- Pattern: Long-lived service with narrow injected store/client ports; constructs a fresh leaf agent per turn.

**`llm.Client`:**
- Purpose: Provider-neutral streaming model boundary.
- Examples: `internal/llm/client.go`, `internal/llm/openai_compat/client.go`.
- Pattern: `Stream(context.Context, Request) (<-chan Chunk, error)`; provider wire projection stays outside the agent loop.

**`tools.Tool` and `tools.Registry`:**
- Purpose: Uniform schema plus execution contract for built-in and bridged capabilities.
- Examples: `internal/agent/tools/spec.go`, `internal/agent/tools/registry.go`, `internal/agent/tools/search.go`.
- Pattern: Registered strategy objects, guarded mutable registry, deferred-schema promotion, runtime-only mutation metadata.

**`gateway.Gateway`:**
- Purpose: Single in-process policy enforcement and mutation reservation point.
- Examples: `internal/gateway/gateway.go`, `internal/gateway/decide.go`, `internal/gateway/reserve.go`.
- Pattern: Classify → approve/deny/allow → operation claim → durable reservation → execute/replay.

**Consumer-declared ports:**
- Purpose: Preserve package direction and testability without shared god interfaces.
- Examples: `internal/agui/server.go` (`Runner`, stores), `internal/cron/dispatch.go` (`Handler`), `internal/sandbox/usersandbox/backend.go` (`Backend`), `internal/objectstore/types.go` (`Store`).
- Pattern: Interfaces live beside the consumer; `cmd/aura/` supplies adapters and concrete implementations.

**Per-identity resolvers:**
- Purpose: Make tenant selection an explicit boundary before storage or execution access.
- Examples: `internal/identityctx/`, `internal/arcadedb/tenant_clients.go`, `internal/objectstore/identity_store.go`, `internal/sandbox/usersandbox/router.go`.
- Pattern: Identity travels on `context.Context`; adapters resolve a physically or logically isolated resource before acting.

## Entry Points

**Aura executable:**
- Location: `cmd/aura/main.go`.
- Triggers: `aura serve`, `shell`, `chat`, `mcp`, `db`, `identity`, `skills`, `task`, and other CLI verbs.
- Responsibilities: Parse top-level commands and invoke concern-specific composition/adapter functions under `cmd/aura/`.

**Long-lived daemon:**
- Location: `cmd/aura/serve.go`.
- Triggers: `aura serve`.
- Responsibilities: Boot the shared runtime, host HTTP/cockpit/scheduler/channels/workers, expose readiness/metrics, and drain cleanly.

**ArcadeDB MCP executable:**
- Location: `cmd/arcadedb-mcp/main.go`.
- Triggers: Sidecar process startup and Streamable HTTP calls under `/mcp`.
- Responsibilities: Resolve the caller identity, provision/open one tenant database, and expose memory/graph tools.

**Filecard helper:**
- Location: `cmd/aura-filecard/main.go`.
- Triggers: Ingestion sidecar subprocess invocation.
- Responsibilities: Reuse Go document inspection logic and emit rendered or JSON file cards.

**Document ingestion process:**
- Location: `services/ingest/app.py`.
- Triggers: `python -m ingest.app` in the ingest sidecar.
- Responsibilities: Reconcile object-store files into per-identity ArcadeDB document/card/passage records.

**Web cockpit:**
- Location: `web/src/main.tsx`.
- Triggers: Browser loads the embedded SPA served by `internal/webui/embed.go`.
- Responsibilities: Route login/chat/share surfaces and mount `AppShell` under query/error/runtime providers.

## Architectural Constraints

- **Threading:** Go request handlers, model streams, tool batches, scheduler claims, channel pollers, and workers run concurrently; use contexts, `errgroup`, bounded semaphores, and joined shutdowns as demonstrated by `internal/agent/workflow/parallel.go`, `internal/cron/scheduler.go`, and `cmd/aura/serve.go`.
- **Global state:** Process-wide mutable state is limited and guarded: registries in `internal/agent/tools/spec.go` and `internal/channels/registry.go`, runner session maps in `internal/runner/runner.go`, and package metrics/encoders in `internal/agent/metrics.go` and `internal/conversations/cl100k/`.
- **Circular imports:** Go prevents import cycles; preserve the deliberate seams documented in `cmd/aura/serve_dispatch.go`, `internal/cron/dispatch.go`, and `internal/agent/tools/` instead of moving adapters into either side.
- **Runtime boundary:** `internal/agent/` must not import `internal/agui/`, and `internal/webui/` must remain leaf-level; `scripts/agui_boundary_check.sh` enforces these boundaries.
- **Tenancy:** Scope every request with `internal/identityctx/`; use owner-scoped stores plus Postgres RLS (`internal/db/rls.go`), one ArcadeDB database per identity (`internal/arcadedb/tenant_clients.go`), identity-scoped object storage (`internal/objectstore/identity_store.go`), and one sandbox per identity (`internal/sandbox/usersandbox/router.go`).
- **Filesystem and shell:** Route agent shell/file operations through `internal/sandbox/usersandbox/SandboxRouter`; a missing backend is a denial, never host execution.
- **Prompt cache:** Preserve the byte-stable system message in `internal/agent/prompt/`; put volatile time, budget, workspace, source, and worker framing in later messages/tail blocks.
- **Tool surface:** Put tool implementations/specs in dedicated files under `internal/agent/tools/`; long or complex specs set `Deferred: true` and are discoverable through `tool_search`.
- **Mutations:** A mutating tool spec must declare operation scope, argument normalizer, and replay policy in `internal/agent/tools/spec.go`; `internal/gateway/guard.go` validates this at boot.
- **Database migrations:** Add paired SQL files under `internal/db/migrations/`; determine the next number from the directory at implementation time, then regenerate `internal/db/sqlc/` from `internal/db/queries/` via `sqlc.yaml`.
- **File size:** Keep edited implementation files at or below 600 LOC by splitting concerns, following `cmd/aura/serve_*.go`, `internal/agent/llm_agent_*.go`, and `web/src/chat/ExternalStoreChat_*.ts`.
- **Build artifact:** Build `web/` into committed `internal/webui/dist/`; `web/vite.config.ts` owns that output path and `internal/webui/embed.go` embeds it.

## Anti-Patterns

### Transport Leakage Into the Runtime

**What happens:** A runtime package imports `internal/agui/`, Telegram, or React-specific concepts.
**Why it's wrong:** It reverses the transport-neutral event/runner direction and defeats reuse by CLI, web, Telegram, cron, and swarm.
**Do this instead:** Add a consumer-side interface or event translation at `internal/agui/translator.go`, `internal/channels/telegram/`, or `cmd/aura/`.

### Bypassing the Runner for Interactive Turns

**What happens:** A transport constructs `LlmAgent` directly and writes conversation or pause state itself.
**Why it's wrong:** It skips per-thread serialization, owner scoping, history rehydration, atomic pause durability, and event persistence.
**Do this instead:** Depend on the narrow `Runner` interface in `internal/agui/server.go` or call the shared `runner.Runner` as `internal/channels/telegram/agui_subscriber.go` does.

### Host Filesystem Fallback

**What happens:** A tool reads/writes the Aura process filesystem when sandbox resolution fails.
**Why it's wrong:** The reported path and the agent's real workspace diverge, and isolation silently disappears.
**Do this instead:** Return the routing error from `internal/sandbox/usersandbox/router.go`; all shell/file tools use the resolved `BoxHandle`.

### Direct Tool Execution Around the Gateway

**What happens:** Agent-controlled code calls `Tool.Execute` without `gateway.Decide` and operation reservation.
**Why it's wrong:** Risk policy, destructive approval, at-most-once mutation, and replay evidence disappear.
**Do this instead:** Keep dispatch through `internal/agent/llm_agent_retry.go:89`; non-agent host APIs use the shared idempotency middleware/operation registry in `internal/agui/idempotency_http.go`.

### Large Always-Visible Tool Schemas

**What happens:** A complex tool sets `Deferred: false` or grows the central manifest.
**Why it's wrong:** Every turn pays the schema token cost and prompt-cache pressure rises with the tool count.
**Do this instead:** Put the full spec beside its implementation in `internal/agent/tools/<name>.go`, set `Deferred: true`, and rely on `internal/agent/tools/search.go`.

### Cross-Domain SQL in Handlers

**What happens:** HTTP/channel/tool code embeds domain SQL or reaches generated queries directly for ordinary operations.
**Why it's wrong:** Owner scoping, transaction boundaries, error translation, and reuse diverge by transport.
**Do this instead:** Add an operation to the relevant store/service under `internal/<domain>/`; reserve direct `sqlc.New(tx)` composition for explicit cross-store transactions such as `cmd/aura/serve_bootstrap.go`.

### Volatile Data in the System Prompt

**What happens:** Time, budget, source lists, workspace state, or worker goals are written into `messages[0]`.
**Why it's wrong:** The byte-stable cache prefix changes every turn/worker and provider prompt-cache reads collapse.
**Do this instead:** Use the volatile tail built by `internal/agent/llm_agent_round.go` or protected later-message blocks from `internal/runner/` and `internal/swarm/`.

### Editing Generated Artifacts by Hand

**What happens:** Code is authored directly in `internal/db/sqlc/` or `internal/webui/dist/`.
**Why it's wrong:** Regeneration overwrites the change and source-of-truth review becomes impossible.
**Do this instead:** Edit `internal/db/queries/`/`sqlc.yaml` or `web/src/`/`web/vite.config.ts`, then run the appropriate generator/build.

## Error Handling

**Strategy:** Return wrapped errors through package boundaries, reserve panic for impossible boot wiring contracts, and separate terminal agent events from infrastructure failures.

**Patterns:**
- Wrap causes with `%w` at adapter boundaries, as in `cmd/aura/chat_boot.go`, `internal/arcadedb/client.go`, and `internal/documents/`.
- Translate typed domain errors to HTTP status codes inside `internal/agui/*_api.go`; sanitize unexpected errors before sending them to clients in `internal/agui/server_run.go`.
- Let optional daemon siblings fail soft with structured warnings, as in `internal/channels/registry.go` and MCP mount loops in `cmd/aura/main.go`; fail closed for required tenancy, sandbox, migration, RLS, or configuration contracts in `cmd/aura/chat_boot.go`.
- Emit agent termination/budget exhaustion as `agent.Event`; use the `iter.Seq2` error slot only for real runtime failures in `internal/agent/agent.go`.
- Recover panics at long-running boundaries and record them through `internal/agent/panicobs/`, `internal/obs/`, and `PanicSafe` lifecycle helpers.
- Complete or mark mutation claims indeterminate on failure/cancellation in `internal/agent/llm_agent_retry.go` and `internal/gateway/`.

## Cross-Cutting Concerns

**Logging:** Use structured `log/slog` and boundary-specific telemetry from `internal/obs/`; never log raw secrets, tool arguments, or external payloads without the redaction paths in `internal/secret/`, `internal/redact/`, and `internal/mcp/redact.go`.

**Validation:** Validate configuration twice around settings overlay in `cmd/aura/chat_boot.go`; strictly decode HTTP JSON in `internal/agui/strict_decode.go`; validate tool mutation metadata at boot in `internal/gateway/guard.go`; validate sandbox specs in `internal/sandbox/usersandbox/spec.go`.

**Authentication:** `internal/webauth/` wraps Authula sessions, `internal/agui/auth.go` applies authentication/capabilities, `internal/identityctx/` carries the principal, owner-scoped stores enforce application checks, and `internal/db/rls.go` supplies the Postgres backstop.

**Idempotency:** Browser/CLI mutations acquire durable operation keys through `internal/idempotency/`, `internal/agui/idempotency_http.go`, and the gateway reservation path in `internal/gateway/reserve.go`.

**Observability:** Keep trace nesting `agent.turn` → `llm.request` → `tool.execute` through `internal/agent/tracing.go`; expose readiness separately from liveness through `internal/readiness/` and `internal/agui/readiness.go`.

---

*Architecture analysis: 2026-08-25*
