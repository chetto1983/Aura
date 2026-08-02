<!-- refreshed: 2026-08-02 -->
# Architecture

**Analysis Date:** 2026-08-02

## System Overview

```text
+----------------------------------------------------------------------------------+
| Transport and UX                                                                 |
| `web/src/` React cockpit | `cmd/aura/` CLI | `internal/channels/telegram/`       |
+--------------------------+---------------------+---------------------------------+
                           | authenticated HTTP / direct call / channel event
                           v
+----------------------------------------------------------------------------------+
| Composition and ingress                                                          |
| `cmd/aura/serve.go` + `cmd/aura/chat_boot.go` + `internal/agui/` + `webauth/`   |
+--------------------------------------+-------------------------------------------+
                                       |
                                       v
+----------------------------------------------------------------------------------+
| Turn orchestration and agent runtime                                              |
| `internal/runner/` -> `internal/agent/` -> `internal/agent/workflow/` / `swarm/` |
+--------------------------------------+-------------------------------------------+
                                       |
                                       v
+----------------------------------------------------------------------------------+
| Policy and capabilities                                                          |
| `internal/gateway/` -> `internal/agent/tools/` -> `agent/mcptools/` / `mcp/`     |
+---------------------------+--------------------------+---------------------------+
                            |                          |
                            v                          v
+------------------------------------------+  +------------------------------------+
| Durable application state                |  | Sidecars and external runtimes     |
| Postgres: `internal/db/`, domain stores  |  | `cmd/arcadedb-mcp/`, ArcadeDB,     |
| Blobs: `internal/objectstore/`           |  | embedding/media/MCP services,      |
| Memory client: `internal/arcadedb/`      |  | per-user Docker sandboxes          |
+------------------------------------------+  +------------------------------------+
```

Aura is a modular Go monolith with two executable composition roots: the main `aura` binary in `cmd/aura/` and the long-term-memory MCP sidecar in `cmd/arcadedb-mcp/`. The browser application in `web/src/` is built into `internal/webui/dist/` and embedded into the main Go binary by `internal/webui/embed.go`.

The active package graph is the output of `go list ./...`: application packages live under `internal/`, command-only adapters live under `cmd/`, and no active `internal/adaptive/` package exists in the current worktree. Historical adaptive tables and migrations remain in `internal/db/migrations/` and generated accessors remain in `internal/db/sqlc/`, but they are persistence history rather than an active runtime layer.

## Component Responsibilities

| Component | Responsibility | File |
|-----------|----------------|------|
| Aura CLI dispatcher | Loads local environment configuration, establishes CLI idempotency, and routes subcommands | `cmd/aura/main.go` |
| Daemon composition root | Boots shared chat/runtime dependencies, HTTP, scheduler, channels, workers, readiness, and graceful shutdown | `cmd/aura/serve.go` |
| Shared runtime assembly | Validates configuration/migration head and wires stores, policy, tools, MCP clients, LLM client, and Runner | `cmd/aura/chat_boot.go` |
| Authenticated HTTP parent mux | Mounts Authula, capability gates, AG-UI/REST routes, and the embedded SPA fallback | `cmd/aura/serve_webui.go` |
| Cockpit protocol server | Owns AG-UI run handling, SSE translation, cockpit REST handlers, and narrow provider setters | `internal/agui/server.go` |
| Turn orchestrator | Serializes a conversation, persists input/output, reloads managed history, creates a fresh agent, and handles HITL resume | `internal/runner/runner.go` |
| Agent runtime | Implements the budgeted streaming LLM/tool loop over the open `Agent` contract | `internal/agent/agent.go`, `internal/agent/llm_agent.go` |
| Tool policy enforcement point | Classifies calls, requests destructive approval, reserves mutations, and reconciles incomplete ledger entries | `internal/gateway/decide.go`, `internal/gateway/reserve.go`, `internal/gateway/reconcile.go` |
| Tool surface | Defines `Tool`, `Spec`, `Registry`, deferred discovery, host tools, document tools, skills, scheduling, and swarm dispatch | `internal/agent/tools/spec.go`, `internal/agent/tools/registry.go` |
| MCP extension layer | Opens stdio/HTTP MCP transports and adapts remote tools into the Aura registry | `internal/mcp/transport.go`, `internal/agent/mcptools/mount.go` |
| Scheduler | Claims due tasks, dispatches typed handlers, tracks runs, and delivers or queues notifications | `internal/cron/scheduler.go`, `internal/cron/dispatch.go` |
| PostgreSQL layer | Opens `pgxpool`, embeds/applies migrations, supplies transaction seams, and hosts sqlc-generated queries | `internal/db/db.go`, `internal/db/migrate.go`, `internal/db/tx.go` |
| Long-term memory | Exposes per-identity ArcadeDB memory through an MCP server and an HTTP client | `cmd/arcadedb-mcp/main.go`, `internal/arcadedb/client.go`, `internal/arcadedb/memory.go` |
| Blob storage | Provides S3-compatible, filesystem, and test implementations behind one `Store` interface | `internal/objectstore/types.go`, `internal/objectstore/s3.go`, `internal/objectstore/filesystem.go` |
| Cockpit SPA | Provides routes, application shell, feature workspaces, server-state queries, and AG-UI stream reduction | `web/src/main.tsx`, `web/src/AppShell.tsx`, `web/src/chat/sseAdapter.ts` |

## Pattern Overview

**Overall:** Modular monolith with ports-and-adapters boundaries, command-layer composition roots, streaming event orchestration, and out-of-process sidecars for isolated or heavyweight capabilities.

**Key Characteristics:**
- Keep dependency assembly in `cmd/aura/`; `cmd/aura/chat_boot.go` constructs concrete stores and injects them into consumer-owned interfaces in `internal/runner/interfaces.go` and `internal/agui/server.go`.
- Stream a single event model from the core: `agent.Agent.Run` returns `iter.Seq2[*agent.Event, error]` in `internal/agent/agent.go`, then `internal/agui/translator.go` projects it into AG-UI events without the runtime importing the transport.
- Make runtime posture explicit through `config.RuntimeProfile` in `internal/config/config_runtimeprofile.go`; strict profiles route mutations through `internal/gateway/` and host-capable tools through `internal/sandbox/usersandbox/`.
- Keep large tool schemas deferred through `tools.Spec.Deferred` in `internal/agent/tools/spec.go`; `tool_search` activates full schemas from the immutable registry built in `cmd/aura/main.go`.
- Separate durable stores by concern: control-plane and conversation data use Postgres through `internal/db/` plus domain stores, blobs use `internal/objectstore/`, and long-term semantic memory uses the `cmd/arcadedb-mcp/` boundary.
- Treat the frontend as a same-origin client: `web/src/main.tsx` mounts React Router and TanStack Query, while `cmd/aura/serve_webui.go` remains the actual authentication and authorization boundary.

## Layers

**Presentation Layer:**
- Purpose: Render browser, CLI, and Telegram experiences over the same runner/event model.
- Location: `web/src/`, `cmd/aura/chat*.go`, `cmd/aura/shell.go`, `internal/channels/`, `internal/channels/telegram/`
- Contains: React workspaces/components, CLI REPL rendering, channel lifecycle, Telegram input/output adapters.
- Depends on: `internal/agui/` over HTTP in the browser; `internal/runner/` directly for CLI/channel adapters.
- Used by: Operators and authenticated identities entering through `cmd/aura/main.go` or `cmd/aura/serve.go`.

**Ingress and Authentication Layer:**
- Purpose: Route HTTP requests, validate sessions/capabilities, enforce same-origin behavior, and translate core events to wire formats.
- Location: `cmd/aura/serve_webui.go`, `cmd/aura/serve_webui_routes.go`, `internal/agui/`, `internal/webauth/`, `internal/identityctx/`
- Contains: Parent `http.ServeMux`, Authula integration, capability middleware, REST handlers, AG-UI SSE encoder, identity context propagation.
- Depends on: Narrow Runner/store/provider interfaces declared in `internal/agui/server.go` and `internal/agui/types.go`.
- Used by: The SPA in `web/src/` and programmatic AG-UI clients.

**Composition Layer:**
- Purpose: Build process-lifetime objects and connect concrete implementations without introducing package cycles.
- Location: `cmd/aura/main.go`, `cmd/aura/chat_boot.go`, `cmd/aura/serve.go`, `cmd/aura/serve_agui.go`, `cmd/aura/serve_dispatch.go`
- Contains: Configuration loading, pool lifecycle, store construction, registry/MCP mounts, scheduler handlers, auth/provider wiring, shutdown order.
- Depends on: All required `internal/` packages; no `internal/` package may import `cmd/aura/`.
- Used by: The `aura` executable at `cmd/aura/main.go`.

**Application Orchestration Layer:**
- Purpose: Coordinate durable turns, resumable HITL, scheduled work, agent workflows, and fan-out.
- Location: `internal/runner/`, `internal/cron/`, `internal/cron/handlers/`, `internal/agent/workflow/`, `internal/swarm/`
- Contains: Per-conversation locks, history hydration, turn persistence, resume commits, scheduler claims, sequential/parallel/loop agents, bounded swarm waves.
- Depends on: Consumer-side interfaces plus `internal/agent/`, `internal/conversations/`, `internal/gateway/`, and domain stores.
- Used by: HTTP, CLI, Telegram, and scheduled job composition in `cmd/aura/`.

**Agent Runtime Layer:**
- Purpose: Build prompts, select reasoning effort, stream model output, dispatch tools, and emit transport-neutral events.
- Location: `internal/agent/`, `internal/agent/prompt/`, `internal/agent/display/`, `internal/llm/`, `internal/llm/openai_compat/`
- Contains: `Agent`, `InvocationContext`, `Budget`, `LlmAgent`, hooks, completion/finalization, provider-neutral streaming client, display payloads.
- Depends on: `internal/agent/tools/`, `internal/gateway/`, `internal/llm/`, and cross-cutting utilities.
- Used by: `internal/runner/`, `internal/cron/handlers/`, `internal/swarm/`, and test agents in `internal/agent/agenttest/`.

**Policy and Capability Layer:**
- Purpose: Decide whether a model-originated operation may execute and where host-capable work runs.
- Location: `internal/gateway/`, `internal/scoring/`, `internal/agent/tools/`, `internal/sandbox/usersandbox/`, `internal/mcp/`
- Contains: Risk classification, approval routing, durable reservations, idempotent replay, tool registry, sandbox routing, MCP egress controls.
- Depends on: `internal/config/`, `internal/idempotency/`, `internal/toolinvocations/`, and concrete adapters injected from `cmd/aura/`.
- Used by: Every tool call from `internal/agent/llm_agent_tool.go` and every managed MCP mount from `cmd/aura/main.go`.

**Domain Services Layer:**
- Purpose: Implement conversations, documents/assets, skills, sharing, identity, scheduling, retention, web retrieval, and multimodal behavior.
- Location: `internal/conversations/`, `internal/documents/`, `internal/assets/`, `internal/skills/`, `internal/share/`, `internal/identity/`, `internal/retention/`, `internal/web/`, `internal/multimodal/`
- Contains: Domain types, validation, narrow stores/services, processing workers, and API-independent business rules.
- Depends on: `internal/db/sqlc/`, `internal/objectstore/`, or external-client abstractions as required by each domain.
- Used by: Tools and composition adapters in `cmd/aura/` plus REST handlers in `internal/agui/`.

**Persistence and Infrastructure Layer:**
- Purpose: Persist relational state, blobs, long-term memory, and observability data.
- Location: `internal/db/`, `internal/db/queries/`, `internal/db/sqlc/`, `internal/objectstore/`, `internal/arcadedb/`, `internal/obs/`
- Contains: pgx/sqlc/migrations, S3/filesystem stores, ArcadeDB HTTP operations, OpenTelemetry/Prometheus setup.
- Depends on: Postgres, S3-compatible storage, ArcadeDB, and configured telemetry collectors.
- Used by: Domain stores and process composition in `cmd/aura/`.

## Data Flow

### Primary Request Path

1. The cockpit calls `POST /agent/run` with `fetch` and consumes its SSE body in `streamRun` (`web/src/chat/sseAdapter.ts:407`).
2. The parent mux applies authentication and the `agent.run` capability before delegating to AG-UI (`cmd/aura/serve_webui.go:89`).
3. `handleRun` validates the AG-UI envelope, verifies thread ownership, prepares attachments/pinned-skill context, and selects request-scoped or detached execution (`internal/agui/server_run.go:22`).
4. `Runner.Turn` takes the identity-scoped per-conversation lock; `turnLocked` persists the visible user turn, loads managed history, and constructs a fresh agent (`internal/runner/runner.go:279`, `internal/runner/runner.go:317`).
5. `buildAgent` creates a per-turn `Budget`, attaches the shared classifier/breaker/registry/gateway, and returns the `InvocationContext` (`internal/runner/runner.go:465`).
6. `LlmAgent.Run` repeatedly builds a cache-stable prompt, opens the provider stream, consumes model chunks, and dispatches tool calls until a terminal event or infrastructure error (`internal/agent/llm_agent.go:182`).
7. Before an executable mutation, `Gateway.Decide` classifies it, optionally requests destructive approval, starts an idempotent operation, and reserves a ledger row (`internal/gateway/decide.go:47`, `internal/gateway/reserve.go:35`).
8. The Runner persists emitted tool/assistant/pause events through the turn tracker while `agui.Translate` converts the same core events into AG-UI frames (`internal/runner/runner_persist.go:80`, `internal/agui/translator.go:54`).
9. `streamSSE` writes translated frames; the browser folds each frame into the current assistant message with `reduceFrame` (`internal/agui/server_sse.go:35`, `web/src/chat/sseAdapter.ts:95`).

### CLI Conversation Flow

1. `main` routes `aura chat` or `aura shell` from the explicit switch in `cmd/aura/main.go:47`.
2. `bootCLIChat` resolves the operator identity after the shared runtime boot in `cmd/aura/chat_boot.go:98`; `assembleChatEnv` builds the same Runner, tool registry, gateway, and stores used by the daemon (`cmd/aura/chat_boot.go:284`).
3. The REPL invokes `Runner.Turn`, renders the transport-neutral `agent.Event` stream, and delegates durable pause/resume to the Runner (`cmd/aura/chat_repl.go`, `internal/runner/runner_resume.go`).

### Scheduled Job Flow

1. `Scheduler.Start` performs recovery and periodic due-task scans (`internal/cron/scheduler.go:177`).
2. `Scheduler.tick` claims tasks with advisory-lock-backed claims and invokes the dispatcher (`internal/cron/scheduler.go:250`, `internal/cron/claim.go:54`).
3. `Dispatch.Dispatch` runs the handler map assembled in `buildDispatch`; agent jobs reuse the agent runtime while maintenance kinds call focused handlers (`internal/cron/dispatch.go:162`, `cmd/aura/serve_dispatch.go:46`).
4. Completion and notification state are persisted through `internal/cron/store.go` and delivered through `internal/channels/registry.go` or managed MCP notification tools.

### Long-Term Memory Flow

1. `buildRegistryWithMCP` mounts configured MCP servers and adapts their tools into the parent registry (`cmd/aura/main.go:315`, `internal/agent/mcptools/mount.go`).
2. Memory tool calls cross the normal gateway/tool path, then the MCP HTTP transport in `internal/mcp/http_client.go` calls the standalone server at `cmd/arcadedb-mcp/main.go:44`.
3. The sidecar resolves a tenant-scoped client, provisions/validates the per-identity database when allowed, and executes memory operations through `internal/arcadedb/memory.go` and `internal/arcadedb/memory_vector.go`.
4. ArcadeDB results return as untrusted MCP tool output through `internal/agent/mcptools/bridge.go`, where the runtime can frame/cap them before the next model round.

**State Management:**
- Durable server state belongs in Postgres domain stores backed by `internal/db/sqlc/`; multi-statement writes use `db.WithTx` or identity-scoped variants in `internal/db/tx.go`.
- Blob state belongs behind `objectstore.Store` in `internal/objectstore/types.go`; document metadata and ingestion lifecycle remain relational in `internal/documents/` and `internal/assets/`.
- Process-local coordination is intentionally bounded: conversation locks/live cancels live in `internal/runner/runner_session.go`, optional detached SSE sessions live in `internal/agui/runregistry.go`, and the tool registry is constructed at boot in `cmd/aura/main.go`.
- Browser server state uses the shared TanStack Query client in `web/src/queryClient.ts`; transient stream state is reduced from AG-UI frames in `web/src/chat/sseAdapter.ts`, while layout/theme preferences remain client-local under `web/src/shell/` and `web/src/theme/`.

## Key Abstractions

**Agent and InvocationContext:**
- Purpose: Provide the open, transport-neutral execution contract and request-scoped context for every agent/workflow.
- Examples: `internal/agent/agent.go`, `internal/agent/workflow/sequential.go`, `internal/agent/workflow/parallel.go`
- Pattern: `iter.Seq2` streaming plus composition; never store `InvocationContext` on a long-lived object.

**Budget Tree:**
- Purpose: Bound steps, wall clock, branch consumption, and repeated tool calls across an agent tree.
- Examples: `internal/agent/budget.go`, `internal/agent/budget_dedup.go`
- Pattern: Shared atomic step balance with child-local dedup rings; parallel branches call `Budget.Child`.

**Runner:**
- Purpose: Own durable turn orchestration, history hydration, identity scoping, pause/resume, and per-thread serialization.
- Examples: `internal/runner/runner.go`, `internal/runner/resume_committer.go`, `internal/runner/runner_persist.go`
- Pattern: Application service over consumer-declared store interfaces from `internal/runner/interfaces.go`.

**Tool / Spec / Registry:**
- Purpose: Expose callable capabilities without coupling the LLM loop to concrete implementations.
- Examples: `internal/agent/tools/spec.go`, `internal/agent/tools/registry.go`, `internal/agent/tools/search.go`
- Pattern: Immutable boot-time registry, duplicate-name panic, non-deferred minimum validation, deferred full-schema discovery.

**Gateway:**
- Purpose: Centralize runtime-profile policy, operator approvals, mutation reservations, replay, and crash reconciliation.
- Examples: `internal/gateway/gateway.go`, `internal/gateway/decide.go`, `internal/gateway/reserve.go`
- Pattern: Single process-wide policy enforcement point injected into each fresh `LlmAgent` by `internal/runner/runner.go`.

**Domain Store Interfaces:**
- Purpose: Keep orchestration and transports independent from pgx/sqlc implementations.
- Examples: `internal/runner/interfaces.go`, `internal/agui/types.go`, `internal/documents/catalog_service.go`, `internal/objectstore/types.go`
- Pattern: Consumers declare narrow interfaces; `cmd/aura/` injects concrete adapters.

**AG-UI Translator:**
- Purpose: Convert Aura events to protocol events without importing AG-UI into the agent runtime.
- Examples: `internal/agui/translator.go`, `internal/agui/server_sse.go`
- Pattern: Pure iterator projection followed by bounded SSE pumping.

**MCP Transport and Bridge:**
- Purpose: Add out-of-process tools through stdio or Streamable HTTP while preserving namespacing, trust, and lifecycle.
- Examples: `internal/mcp/transport.go`, `internal/mcp/http_client.go`, `internal/agent/mcptools/bridge.go`
- Pattern: Transport interface plus host-tool adapter; managed servers are configured in `internal/mcp/manager/`.

## Entry Points

**Aura CLI / Daemon:**
- Location: `cmd/aura/main.go`
- Triggers: `aura serve`, `aura chat`, `aura shell`, and explicit operational subcommands.
- Responsibilities: Load local configuration support, enforce CLI operation idempotency, and route to command-specific composition code.

**Serve Daemon:**
- Location: `cmd/aura/serve.go`
- Triggers: `aura serve` from `cmd/aura/main.go`.
- Responsibilities: Run authenticated HTTP, scheduler, Telegram/setup channel services, ingestion/sweep/reconciliation workers, metrics, readiness, and graceful drain.

**ArcadeDB MCP Sidecar:**
- Location: `cmd/arcadedb-mcp/main.go`
- Triggers: Standalone process/container start.
- Responsibilities: Serve `/mcp` and `/health`, resolve per-identity ArcadeDB clients, and expose memory and graph-schema tools.

**Cockpit Browser Application:**
- Location: `web/src/main.tsx`
- Triggers: Browser loading the embedded SPA from `internal/webui/dist/`.
- Responsibilities: Install mutation idempotency, initialize theme/i18n/query state, and route login, cockpit, conversation, and shared-link views.

## Architectural Constraints

- **Threading:** Go HTTP requests and daemon workers run concurrently; conversation turns are serialized by identity-plus-session locks in `internal/runner/runner_session.go`, workflow fan-out uses `errgroup` and serial iterator-frame yielding in `internal/agent/workflow/parallel.go`, and graceful shutdown joins workers in `cmd/aura/serve.go`.
- **Global state:** Process-wide objects include the shared LLM breaker/classifier in `internal/runner/runner.go`, global metrics/boundaries in `internal/obs/` and `internal/agent/panicobs/`, and boot-built immutable registries in `internal/agent/tools/spec.go`; request state must remain in contexts or turn-local structs.
- **Circular imports:** No compile-time circular imports are present. Preserve consumer-side seams such as `Runner` interfaces in `internal/agui/server.go` and tool-facing runner/store interfaces in `internal/agent/tools/`; concrete wiring belongs in `cmd/aura/`.
- **Transport boundary:** `internal/agent/` must not import `internal/agui/`, and `internal/webui/` stays independent of other internal packages; `scripts/agui_boundary_check.sh` checks this boundary.
- **Prompt-cache boundary:** Stable system content stays at `messages[0]`; volatile time, budget, workspace, sources, and worker framing are appended later by `internal/agent/prompt/` and `internal/agent/llm_agent.go`.
- **Identity boundary:** Web requests acquire an authenticated principal in `internal/agui/auth.go`; owner-scoped database operations use the identity context and RLS transaction seams in `internal/db/tx.go`.
- **Route boundary:** An AG-UI route must be registered inside `internal/agui/server.go` and delegated by the authenticated parent mux in `cmd/aura/serve_webui.go`; constants/prefixes live in `cmd/aura/serve_webui_routes.go`.
- **Migration boundary:** Add paired migrations only under `internal/db/migrations/`, deriving the next number from that directory; update SQL sources in `internal/db/queries/` and regenerate `internal/db/sqlc/` through `sqlc.yaml`.
- **File-size boundary:** Keep implementation files at or below 600 LOC per `CLAUDE.md`; split touched concerns using existing patterns such as `internal/agent/llm_agent_*.go` and `cmd/aura/serve_*.go`.

## Anti-Patterns

### Single-Mux Route Registration

**What happens:** A handler added only to `internal/agui/server.go` is hidden by the authenticated parent mux and can fall through to the SPA or a 404.
**Why it's wrong:** Aura deliberately uses two mux layers for capability placement and static fallback; the reachability list in `cmd/aura/serve_webui_routes.go` is part of the contract.
**Do this instead:** Register the handler in `internal/agui/server.go`, add its parent delegation/capability wrapper in `cmd/aura/serve_webui.go`, and keep route constants in `cmd/aura/serve_webui_routes.go` or the focused `serve_webui_*.go` file.

### Concrete Dependency Construction Inside Domain Packages

**What happens:** A domain or tool package imports another high-level domain to build its concrete store/runner, creating dependency inversion violations and potential cycles.
**Why it's wrong:** `internal/agent/tools/` already depends on low-level tool contracts; importing `internal/cron/`, `internal/skills/`, or `internal/swarm/` for construction would couple the runtime and defeat the consumer-interface pattern.
**Do this instead:** Declare the smallest interface beside its consumer, then adapt and inject the concrete implementation from `cmd/aura/main.go`, `cmd/aura/chat_boot.go`, or `cmd/aura/serve_dispatch.go`.

### Transport-Specific Events in the Runtime

**What happens:** Core code emits SSE/AG-UI/Telegram-specific payloads directly from `internal/agent/`.
**Why it's wrong:** It prevents CLI/channel reuse and reverses the established `agent.Event` -> translator/renderer dependency direction.
**Do this instead:** Emit `agent.Event` or a transport-neutral `display.Payload` from `internal/agent/`; translate in `internal/agui/translator.go`, `internal/agentrender/`, or `internal/channels/telegram/renderer.go`.

### Signal-Cancelling In-Flight Work Immediately

**What happens:** A signal-derived context directly parents an active turn, terminating the stream before terminal frames and durable cleanup are emitted.
**Why it's wrong:** The shutdown contract in `cmd/aura/serve.go` separates the signal context from the work context so existing work receives a bounded drain window.
**Do this instead:** Stop admission with the signal context, drain workers/HTTP under the configured grace, and cancel the work context only as the final backstop in `cmd/aura/serve.go` and `cmd/aura/serve_lifecycle.go`.

## Error Handling

**Strategy:** Fail fast on invalid required configuration and incompatible migrations, fail closed at policy/identity boundaries, fail soft for optional sidecars/providers, and preserve graceful cleanup through error-returning boot functions.

**Patterns:**
- Reserve the `iter.Seq2` error slot for real infrastructure failures; represent budget exhaustion, pauses, and normal termination as `agent.Event` state/actions in `internal/agent/agent.go` and `internal/agent/event.go`.
- Wrap errors with context and `%w`; redact DSNs and external payloads at boundaries such as `internal/db/db.go`, `internal/agui/server_redact.go`, and `internal/mcp/redact.go`.
- Keep reusable boot helpers free of `os.Exit`; return errors from `bootChatEnv`/`assembleChatEnv` in `cmd/aura/chat_boot.go`, then translate them at the CLI boundary in `cmd/aura/main.go` or `cmd/aura/serve.go`.
- Allow optional MCP, graph, voice, and governance providers to degrade with warnings/503s rather than aborting the whole daemon; wiring and nil-provider behavior live in `cmd/aura/serve_agui.go`.
- Strictly decode bounded JSON and map domain failures to controlled HTTP statuses in `internal/agui/strict_decode.go` and focused `internal/agui/*_api.go` handlers.
- Always release resources in reverse ownership order: detached runs/workers, HTTP, MCP clients, and pool lifecycles are coordinated by `cmd/aura/serve.go`, `cmd/aura/serve_lifecycle.go`, and `cmd/aura/chat_boot.go`.

## Cross-Cutting Concerns

**Logging:** Use structured `log/slog` with bounded/redacted fields in `cmd/aura/serve.go`, `internal/agent/llm_agent.go`, and `internal/obs/`; request/thread/tool identifiers are preferred over raw payloads.

**Validation:** Configuration is centralized in `internal/config/` with profile-aware validation in `internal/config/config_validate.go`; HTTP bodies use `internal/agui/strict_decode.go`; domain types validate at service boundaries such as `internal/documents/catalog_service.go` and `internal/agent/tools/spec.go`.

**Authentication:** Authula integration in `internal/webauth/` validates sessions, `internal/agui/auth.go` establishes the request principal and capability gates, `internal/identityctx/` carries identity ownership, and `internal/db/tx.go` applies the RLS context for scoped operations.

**Idempotency:** Browser mutations receive a stable request key in `web/src/api/idempotency.ts`; HTTP mutation routes use `internal/agui/idempotency_http.go`; agent mutations reserve through `internal/gateway/reserve.go` and persist in `internal/idempotency/` plus `internal/toolinvocations/`.

**Observability:** OTel tracing/metrics and Prometheus live in `internal/obs/`; agent, database, scheduler, document, and MCP boundaries instrument their own packages, while `cmd/aura/serve_observability.go` owns daemon startup/shutdown.

---

*Architecture analysis: 2026-08-02*
