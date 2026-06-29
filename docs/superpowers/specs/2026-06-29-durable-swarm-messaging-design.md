# Aura Durable Swarm Messaging Design

Date: 2026-06-29
Status: Draft - awaiting user review, then implementation planning.
Author: Codex, brainstorming session with Davide

## Purpose

Aura needs durable agent-to-agent messaging that can survive process restarts,
support multiple user channels, and give operators a clear audit trail. The
current `swarm_spawn` tool is a useful flat fan-out primitive, but it is
ephemeral: transcripts are best-effort files, work is not claimable from
Postgres, and external channels such as Telegram are not modeled as first-class
origins.

This design covers the first implementation slice: a Postgres-first messaging
substrate, the `agent_message_send` mutating tool, channel-agnostic thread
tracking, debug CLI commands, AG-UI projections, and tests. It keeps OpenRouter
as the practical LLM provider for now because local VRAM is not sufficient. The
substrate remains provider-neutral so local models can plug in later.

## Decisions

- Use Postgres as the source of truth for tasks, messages, idempotency, retries,
  leases, and channel-thread mapping.
- Treat in-process events and Postgres `NOTIFY` as wakeup optimizations only.
  Workers must recover from durable rows after a restart.
- Keep the substrate channel-agnostic. CLI, AG-UI, Telegram, WhatsApp, and future
  adapters normalize input into internal tasks and messages.
- Add `agent_message_send` beside `swarm_spawn`; do not replace `swarm_spawn` in
  this slice.
- Route tool integration through a narrow consumer-side interface in
  `internal/agent/tools`, matching the existing `swarm_spawn` cycle-breaking
  pattern.
- Return only IDs, status, and short previews from the tool. Large content uses
  artifact references.
- Keep real Telegram wiring optional in the first slice if the existing adapter
  path is not clean enough. The schema and tests must still prove the channel
  contract.

## Validation

The chosen pattern combines local project constraints, local reference repos,
and established messaging patterns.

Senior-dev hardening notes in
`docs/research/senior-dev-agent-hardening-2026-tool-policy-gateway.md` validate
the durable-ledger direction: every meaningful action needs idempotency,
runtime authorization context, persisted outcomes, and audit evidence. The same
document recommends Temporal-style durable primitives without requiring a
Temporal cluster on the local appliance.

Local reference repos under `D:\tmp` support the same shape:

- `D:\tmp\adk-go-study` validates narrow service boundaries for agents,
  sessions, and artifacts.
- `D:\tmp\openhuman` validates typed in-process events plus approval and policy
  gates.
- `D:\tmp\go-swarm` and `D:\tmp\nanobot` show simple actor/channel messaging,
  but their memory-first queues are not sufficient for Aura's durability,
  replay, and audit requirements.

External primary sources support the mechanics:

- Enterprise Integration Patterns: Request-Reply
  (https://www.enterpriseintegrationpatterns.com/patterns/messaging/RequestReply.html),
  Correlation Identifier
  (https://www.enterpriseintegrationpatterns.com/patterns/messaging/CorrelationIdentifier.html),
  and Idempotent Receiver
  (https://www.enterpriseintegrationpatterns.com/patterns/messaging/IdempotentReceiver.html).
- PostgreSQL `SELECT ... FOR UPDATE SKIP LOCKED` for concurrent claiming
  (https://www.postgresql.org/docs/current/sql-select.html).
- PostgreSQL `NOTIFY` for non-durable wakeups
  (https://www.postgresql.org/docs/current/sql-notify.html).
- A2A's task/message/artifact vocabulary (https://github.com/a2aproject/A2A).
- Temporal workflow message passing for durable command/message semantics
  (https://docs.temporal.io/encyclopedia/workflow-message-passing).
- OpenTelemetry messaging spans for observability naming
  (https://opentelemetry.io/docs/specs/semconv/messaging/messaging-spans/).

## Current Context

Aura already has a clean agent interface in `internal/agent`, a flat swarm
engine in `internal/swarm`, a tool registry in `internal/agent/tools`, sqlc-backed
Postgres access under `internal/db`, and AG-UI translation under `internal/agui`.

The current `swarm_spawn` implementation runs bounded child workers in memory,
isolates sibling failures, partitions budget, and writes best-effort transcripts
to disk. It does not provide a durable inbox, retryable task leasing, channel
thread mapping, or replayable message history.

`cmd/aura/main.go` already composes concrete tool adapters at the edge. The new
tool should follow that pattern: the tools package owns a small interface, while
the swarm messaging package provides the concrete adapter.

## Goals

- Persist every delegated task and internal message before work begins.
- Support restart recovery through Postgres rows, not in-memory channels.
- Support channel-agnostic origins and replies for CLI, AG-UI, Telegram, and
  future adapters.
- Provide idempotent send semantics so duplicate tool calls do not duplicate
  work.
- Provide lease-based worker claiming with retry and expiry.
- Keep messages small and use artifact refs for larger payloads.
- Emit safe AG-UI custom events and useful OTel spans and metrics.
- Add debug CLI commands that let a local operator inspect task/message state.
- Preserve existing `swarm_spawn` behavior.

## Non-Goals

- Do not build a full event-sourced workflow runtime in this slice.
- Do not require a local LLM sidecar before messaging lands.
- Do not implement a full ToolGateway policy engine in this slice.
- Do not expose Telegram-specific fields to agents.
- Do not let agents deliver directly to external channels.
- Do not replace `swarm_spawn` until the durable substrate has proven itself.

## Architecture

The architecture has three layers:

1. Channel adapters receive and deliver external messages. They know about CLI,
   AG-UI, Telegram, WhatsApp, or API-specific details.
2. The durable messaging substrate stores channel threads, tasks, messages,
   artifacts, leases, retries, and idempotency state.
3. Agent workers consume internal tasks and append internal response messages.

The database is the boundary between layers. Channel adapters normalize inbound
traffic into channel threads plus internal messages. Workers read only internal
task and message context. The channel router projects completed messages back
to the source channel by reading `channel_kind` and `reply_route`.

In-process pub/sub and Postgres `NOTIFY` may wake workers quickly, but all
workers must be able to poll and claim rows from Postgres. If the process
crashes after a task is leased, the lease expiry makes the task claimable again.

## Components

### `internal/swarm/messaging`

Owns durable task/message types, validation, task status transitions, retry
classification, lease rules, and the service interface used by adapters and
workers.

### `internal/swarm/messaging/store`

Wraps sqlc queries and Postgres transactions. It owns these operations:

- create or find channel thread
- send message idempotently
- create task
- claim pending task
- renew lease
- append response message
- complete task
- fail task
- retry task
- expire stale leases

### `internal/agent/tools/agent_message_send`

Defines the mutating tool spec, JSON argument schema, validation, and
consumer-side sender interface. It must not import the concrete swarm messaging
package. It returns task/message IDs, status, and short previews only.

### `internal/swarm/messaging/adapter`

Connects the tool-facing interface to the durable service at the composition
root. This mirrors the existing `swarm.NewRunnerAdapter` pattern.

### `cmd/aura`

Adds minimal debug commands in the existing hand-rolled CLI style:

- list recent swarm tasks
- show messages for one task
- retry a failed or expired task
- optionally send a manual development message

### AG-UI Projection

Adds safe custom events such as:

- `aura.swarm.task.created`
- `aura.swarm.message.sent`
- `aura.swarm.task.running`
- `aura.swarm.task.completed`
- `aura.swarm.task.failed`

Events must expose user-visible status and IDs, not hidden reasoning or raw
secret-bearing channel metadata.

## Data Model

The first schema should be small, explicit, and indexed for queue operations.

### `aura.swarm_channel_threads`

Maps external conversations to Aura work.

- `id uuid primary key`
- `channel_kind text not null`
- `channel_instance text not null`
- `external_thread_id text not null`
- `external_actor_ref text`
- `reply_route jsonb not null default '{}'::jsonb`
- `metadata jsonb not null default '{}'::jsonb`
- `created_at timestamptz not null default now()`
- `updated_at timestamptz not null default now()`

`channel_kind` examples are `cli`, `agui`, `telegram`, `whatsapp`, and `api`.
`reply_route` stores routing data, not secrets. Bot tokens and API credentials
stay in configuration or secret storage.

### `aura.swarm_tasks`

Tracks one durable unit of delegated work.

- `id uuid primary key`
- `run_id uuid`
- `parent_task_id uuid references aura.swarm_tasks(id)`
- `channel_thread_id uuid references aura.swarm_channel_threads(id)`
- `request_id uuid`
- `from_agent text not null`
- `to_agent text not null`
- `status text not null`
- `priority integer not null default 0`
- `attempt_count integer not null default 0`
- `max_attempts integer not null default 3`
- `available_at timestamptz not null default now()`
- `locked_by text`
- `locked_until timestamptz`
- `timeout_at timestamptz`
- `budget_snapshot jsonb not null default '{}'::jsonb`
- `last_error text`
- `created_at timestamptz not null default now()`
- `updated_at timestamptz not null default now()`

Valid statuses are `pending`, `leased`, `running`, `waiting_input`, `completed`,
`failed`, `cancelled`, and `expired`.

### `aura.swarm_messages`

Stores the append-only internal message log.

- `id uuid primary key`
- `task_id uuid not null references aura.swarm_tasks(id) on delete cascade`
- `channel_thread_id uuid references aura.swarm_channel_threads(id)`
- `direction text not null`
- `from_agent text not null`
- `to_agent text not null`
- `kind text not null`
- `correlation_id uuid not null`
- `causation_id uuid`
- `idempotency_key text not null`
- `content jsonb not null`
- `artifact_refs jsonb not null default '[]'::jsonb`
- `delivery_metadata jsonb not null default '{}'::jsonb`
- `created_at timestamptz not null default now()`

The store enforces one active message per idempotency scope:
`(channel_thread_id, from_agent, to_agent, idempotency_key)`, with a separate
scope for agent-internal sends where `channel_thread_id` is absent. A repeated
send returns the existing message/task instead of creating another task.

Valid `direction` values are `request`, `response`, and `system`. Valid `kind`
values are intentionally small in the first slice: `task`, `reply`, `status`,
and `error`.

### `aura.swarm_artifacts`

Stores metadata for large payloads and outputs.

- `id uuid primary key`
- `task_id uuid references aura.swarm_tasks(id) on delete cascade`
- `message_id uuid references aura.swarm_messages(id) on delete cascade`
- `owner_agent text`
- `uri text not null`
- `sha256 text`
- `size_bytes bigint`
- `content_type text`
- `provenance jsonb not null default '{}'::jsonb`
- `created_at timestamptz not null default now()`

The schema stores references and integrity metadata. It does not force large
payloads into the message row.

## Data Flow

1. A channel adapter receives input from CLI, AG-UI, Telegram, or another source.
2. The adapter creates or finds a `swarm_channel_threads` row.
3. The adapter or an agent calls `agent_message_send`.
4. The tool validates recipient, payload size, channel scope, and idempotency
   key.
5. The store transaction creates or reuses the task and appends the message.
6. A worker claims a pending task with `SELECT ... FOR UPDATE SKIP LOCKED`.
7. The worker runs the target agent with scoped invocation context.
8. The worker appends response messages and marks the task complete, failed,
   waiting for input, or retryable.
9. AG-UI, CLI, and channel adapters project state from the same durable rows.
10. The channel router delivers allowed response content through the original
    channel's `reply_route`.

## Failure Handling

`agent_message_send` requires or derives an idempotency key. Duplicate sends in
the same scope return the existing task/message result.

Workers claim rows by lease. A claim sets `locked_by` and `locked_until`. A live
worker may renew the lease. An expired lease makes the task claimable again.

Retryable failures increment `attempt_count`, write a structured error, and set
`available_at` with backoff. Permanent failures set `status = failed` and keep
the reason. Expired tasks set `status = expired` when their timeout passes.

Agents do not deliver directly to Telegram, AG-UI, CLI, or future channels.
They append internal response messages. The channel router handles delivery and
can record channel delivery failures without corrupting the internal task result.

## Safety And Privacy

The first slice must capture enough metadata for future ToolGateway policy:
actor, source channel, target agent, requested capability, request ID, run ID,
task ID, message ID, idempotency key, and provenance.

Rows may store stable external references and routing metadata, but they must
not store raw bot tokens, API keys, passwords, reset codes, or other channel
secrets. Logs and AG-UI events must redact secrets and avoid hidden reasoning.

Tool results must stay concise. Large output belongs in artifacts. This keeps
LLM context, AG-UI streams, and CLI output predictable.

## Observability

Add OTel spans around:

- message send
- task claim
- lease renewal
- agent run
- task completion
- task failure
- retry scheduling
- channel delivery

Add metrics for:

- pending tasks
- leased and running tasks
- completed tasks
- failed tasks
- retry count
- expired leases
- channel delivery failures
- idempotency hits

Logs should include request ID, run ID, task ID, message ID, idempotency key,
channel kind, and target agent. Logs must redact secrets.

## Testing Strategy

Use test-first implementation.

Unit tests should cover validation, idempotency, status transitions, retry
classification, payload limits, redaction, artifact ref handling, and channel
routing decisions.

Store tests should run against Postgres and cover transactional send, duplicate
idempotency keys, concurrent `SKIP LOCKED` claiming, lease expiry, completion,
failure, and retry scheduling.

Tool tests should cover the `agent_message_send` schema, mutating metadata,
concise result previews, and error messages safe for model consumption.

Channel tests should use fake `cli`, `agui`, and `telegram` adapters to prove
the substrate does not depend on Telegram-specific fields.

AG-UI tests should prove only safe custom events are emitted.

Concurrency tests should prove two workers cannot complete the same task through
normal claim paths.

## Rollout

1. Add the migration and sqlc queries for channel threads, tasks, messages, and
   artifacts.
2. Add the store with tests around transactions, idempotency, claims, leases,
   retries, and completion.
3. Add the messaging service and worker-facing interfaces.
4. Add `agent_message_send` through the existing tool registration pattern.
5. Add CLI inspection commands.
6. Add fake channel adapters and AG-UI projection tests.
7. Wire real Telegram routing only if the existing adapter path is clean enough
   for this slice. Otherwise, leave Telegram wiring for the next slice and keep
   the contract covered by fake-adapter tests.
8. Run focused Go tests, Postgres integration tests, `go test ./...`, `go vet
   ./...`, and `go build ./cmd/aura`.

## Acceptance Criteria

- Sending the same logical message twice with the same idempotency key creates
  one task/message pair and returns the existing IDs on retry.
- Two concurrent workers cannot claim the same pending task.
- A leased task becomes claimable after lease expiry.
- Retryable failure reschedules the task with backoff.
- Permanent failure is durable and inspectable through the CLI.
- CLI, AG-UI, and Telegram-shaped fake adapters all use the same channel-thread
  contract.
- Agents never receive Telegram-specific schema fields.
- AG-UI emits safe custom events for task/message lifecycle changes.
- Tool results stay small and use artifact refs for large payloads.
- Existing `swarm_spawn` behavior remains unchanged.

## Follow-On Work

- Build `swarm_collaborate` or a higher-level orchestrator on top of the durable
  substrate.
- Wire the real Telegram adapter if it is not included in the first slice.
- Add ToolGateway policy decisions before `agent_message_send` executes.
- Add local LLM sidecars and strict local-only enforcement once hardware and
  model strategy are ready.
- Consider an event-sourced task log only if messages plus task state are not
  enough for replay and audit needs.
