# Aura Durable Swarm Messaging Design

Date: 2026-06-29
Status: Amended 2026-07-04 — design review + industrial survey (Temporal/DBOS/River/Hatchet/pgmq/A2A); plan updated in lockstep.
Author: Codex, brainstorming session with Davide

> **Amendment summary (2026-07-04):** the substrate is explicitly **at-least-once** with
> fenced completion (attempt-count token), short leases kept alive by **worker heartbeats**,
> reclaim of expired `running` leases, transient-vs-permanent retry contract with
> exponential backoff + full jitter and a dead-letter terminal state, task states aligned
> with the A2A lifecycle (`rejected` added; `waiting_input` never consumes attempts), an
> explicit policy-gateway tier (`Normal`) for `agent_message_send`, and a recorded slice-2
> backlog (rescuer, retention/vacuum hygiene, queue observability, step-level checkpointing
> decision). Migration slot is **0026** (0025 was taken by `document_control_plane`).

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
- Delivery semantics are **at-least-once**, never "exactly-once". Exactly-once is
  achievable only for DB-local effects by committing the effect and its record in one
  transaction (the DBOS trick); everything else relies on idempotency keys. Completion,
  failure, and retry are **fenced** on the claimed `attempt_count` plus `locked_by`, so a
  zombie worker whose lease expired and whose task was reclaimed matches zero rows
  (Kleppmann fencing-token pattern) and receives a typed `ErrLeaseLost`.
- Leases stay **short** (default 1m — the crash-recovery latency, not the max task
  duration) and are extended by a **heartbeat** goroutine while the runner works
  (Temporal heartbeat pattern). A fixed long lease is wrong in both directions: it delays
  crash recovery and still cannot bound an LLM call.
- The retry contract distinguishes **transient** (runner returns an error → reschedule
  with exponential backoff + full jitter, AWS pattern, preventing retry storms against
  the LLM provider) from **permanent** (runner returns `Status: failed` → terminal).
  Exhausted attempts dead-letter to `failed`; the row is never deleted and never retried
  silently.
- Task states align with the **A2A task lifecycle**: `rejected` (agent declines before
  working) is a first-class terminal state, and `waiting_input` is a non-terminal pause
  that never consumes `attempt_count` and is woken transactionally by the arriving reply.
- `agent_message_send` gets an explicit **policy-gateway tier**: pinned `scoring.Normal`
  via a fixed-tier table in `internal/gateway/classify.go`. Without the entry the generic
  Mutating branch saturates to `Risky` and every agent-to-agent send would pause for
  human approval, defeating the autonomous substrate. The tool writes only internal
  `aura.swarm_*` rows with a validated, secrets-blocklisted payload — comparable to the
  snippet-lifecycle skill writes already tiered Normal.

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
- A2A's task/message/artifact vocabulary (https://github.com/a2aproject/A2A) and its
  task state machine (https://a2a-protocol.org/latest/specification/).
- Temporal workflow message passing for durable command/message semantics
  (https://docs.temporal.io/encyclopedia/workflow-message-passing) and heartbeats for
  long activities (https://docs.temporal.io/encyclopedia/detecting-activity-failures).
- OpenTelemetry messaging spans for observability naming
  (https://opentelemetry.io/docs/specs/semconv/messaging/messaging-spans/).

Industrial survey added by the 2026-07-04 amendment — systems that validate (and
sharpen) this design's pattern:

- **DBOS** — Postgres-as-truth durable execution as a library, `SKIP LOCKED` queues,
  exactly-once for DB-local steps via step+checkpoint in one transaction
  (https://docs.dbos.dev/architecture). The closest production analog to this substrate.
- **River** (Go) — transactional enqueue, rescuer/cleaner maintenance services, snooze
  as a non-attempt-consuming wait (https://riverqueue.com/docs/maintenance-services);
  MVCC bloat history behind the design (https://brandur.org/postgres-queues).
- **Hatchet** — dequeue cost must be O(claimed batch), never O(queue depth); window-
  function fairness anti-scales into an unrecoverable state
  (https://hatchet.run/blog/multi-tenant-queues).
- **pgmq** — visibility-timeout semantics equivalent to this design's `locked_until`
  (https://github.com/pgmq/pgmq).
- **Recall.ai postmortem** — `NOTIFY` takes a global AccessExclusiveLock at commit and
  serializes all commits at scale; poll-as-correctness, notify-as-latency-only
  (https://www.recall.ai/blog/postgres-listen-notify-does-not-scale).
- **Kleppmann** — fencing tokens for zombie lease holders
  (https://martin.kleppmann.com/2016/02/08/how-to-do-distributed-locking.html).
- **AWS Builders' Library** — exponential backoff with full jitter
  (https://aws.amazon.com/builders-library/timeouts-retries-and-backoff-with-jitter/).
- **Anthropic multi-agent research system** — resume-from-error over restart-from-
  scratch, checkpoints, rainbow deploys, compounding-error framing
  (https://www.anthropic.com/engineering/multi-agent-research-system).
- **Diagrid critique of coarse checkpointing** — task-level-only durability re-runs
  completed side effects; step-level journaling is the fix (slice-2 decision)
  (https://www.diagrid.io/blog/checkpoints-are-not-durable-execution-why-langgraph-crewai-google-adk-and-others-fall-short-for-production-agent-workflows).

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
`failed`, `cancelled`, `expired`, and `rejected` (A2A alignment: the target agent
declined before working). `waiting_input` is the A2A `input-required` pause: non-terminal,
it never consumes `attempt_count`, and it is woken transactionally by the arriving reply
— never by a timer race. The migration ships as `0026_swarm_messaging`.

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
send returns the existing message/task instead of creating another task. Two
**concurrent** sends with the same key are also handled: the loser's transaction
rolls back on the unique index (23505) and the store refetches the winner's rows,
returning them as a reused send — never a raw constraint error.

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
the same scope return the existing task/message result (including under
concurrency, via 23505 recovery).

Workers claim rows by lease. A claim sets `locked_by` and `locked_until`. While
the runner works, a **heartbeat** goroutine extends the lease every interval
(default lease/3), so the lease itself stays short and crash recovery fast. An
expired lease makes the task claimable again **whether it is `leased` or
`running`** — a worker that dies after marking the task running must not strand
it; the claim predicate and its partial index cover both states.

Every post-claim mutation (mark-running, complete, fail, retry, lease extension)
is **fenced**: it matches `locked_by` plus the `attempt_count` the worker claimed.
A zombie worker that lost its lease gets `ErrLeaseLost` and drops the task —
the reclaimed run owns the outcome. This is the at-least-once contract: the
substrate guarantees no lost tasks and no clobbered outcomes, and callers make
side effects idempotent.

Transient failures (runner error) reschedule via `RetryTask`: increment
`attempt_count`, write a structured error, and set `available_at` with
exponential backoff plus **full jitter** so a provider outage does not produce a
synchronized retry storm. Permanent failures (runner verdict) and exhausted
attempts (`ErrRetryExhausted`) set `status = failed` — the dead-letter terminal
state, kept inspectable, never silently retried or deleted. Expired tasks set
`status = expired` when their timeout passes.

Agents do not deliver directly to Telegram, AG-UI, CLI, or future channels.
They append internal response messages. The channel router handles delivery and
can record channel delivery failures without corrupting the internal task result.

## Safety And Privacy

The policy gateway already exists (35-06): `agent_message_send` is classified via a
fixed-tier entry pinning it to `scoring.Normal` (auto-allow with a recorded decision
fact). The tier rests on three invariants that must hold for the entry to stay valid:
the tool writes only internal `aura.swarm_*` rows, the payload is channel-agnostic
(Telegram/CLI/AG-UI fields rejected at validation), and secrets are blocklisted from
content. If any invariant weakens — e.g. sends gain direct external delivery — the
tier discussion must be re-run before shipping.

The first slice must also capture enough metadata for future per-content policy:
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

- pending tasks (queue depth)
- **oldest-claimable-age** (the real queue SLO metric — depth alone hides starvation)
- leased and running tasks
- completed tasks
- failed tasks (dead-letter count — alert on growth)
- retry count and attempt histograms
- expired leases and heartbeat-extension failures
- channel delivery failures
- idempotency hits (including 23505 race recoveries)

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
  one task/message pair and returns the existing IDs on retry — including under
  concurrent duplicate sends (23505 race returns the winner as reused).
- Two concurrent workers cannot claim the same pending task.
- A leased **or running** task becomes claimable after lease expiry, and the
  original (zombie) worker's completion is fenced out with `ErrLeaseLost` instead
  of clobbering the reclaimed run.
- The worker heartbeat extends a live task's lease, so a task running longer than
  the base lease is not reclaimed while its worker is healthy.
- Transient failure reschedules the task with exponential backoff + jitter;
  exhausted attempts transition to `failed` (dead-letter), never retry-forever.
- Permanent failure is durable and inspectable through the CLI.
- `agent_message_send` classifies `scoring.Normal` at the gateway (auto-allow);
  an unknown mutating tool still saturates to `Risky`.
- CLI, AG-UI, and Telegram-shaped fake adapters all use the same channel-thread
  contract.
- Agents never receive Telegram-specific schema fields.
- AG-UI emits safe custom events for task/message lifecycle changes.
- Tool results stay small and use artifact refs for large payloads.
- Existing `swarm_spawn` behavior remains unchanged.

## Follow-On Work

Slice-2 backlog (2026-07-04 amendment — each item is industrial table stakes; recorded
here so they do not silently evaporate):

- **Rescuer/sweeper**: reclaim-or-dead-letter tasks stuck past their horizon, run
  single-flight under an advisory lock (Graphile sweeper pattern); pairs with
  worker-level liveness distinct from task heartbeats.
- **Row hygiene**: prune/archive terminal task/message rows on retention (River
  defaults ~24h) plus aggressive per-table autovacuum — Postgres queues die of MVCC
  bloat, not load. Never hold a transaction across an LLM call.
- **Queue observability wiring**: the metrics listed above surfaced via CLI and the
  metrics pipeline; alert on dead-letter growth and oldest-claimable-age.
- **Step-level checkpointing decision**: whether worker resume consults the
  append-only message log (causation chain) to skip completed steps instead of
  re-running the whole task (DBOS `operation_outputs` analog). Explicit design
  decision, not an accident of implementation.
- **`waiting_input` wiring**: the A2A input-required pause — transition set by
  `ask_user`-style needs, woken transactionally by the arriving reply, zero attempts
  consumed.
- **Wakeup path**: if LISTEN/NOTIFY is added, notify only on empty→non-empty
  transitions or from a single notifier (global commit-serialization hazard);
  poll-with-jitter stays the correctness mechanism.

Original follow-on items:

- Build `swarm_collaborate` or a higher-level orchestrator on top of the durable
  substrate.
- Wire the real Telegram adapter if it is not included in the first slice.
- Add per-content ToolGateway policy for `agent_message_send` (the fixed Normal tier
  ships in slice 1; content-sensitive escalation is future work).
- Add local LLM sidecars and strict local-only enforcement once hardware and
  model strategy are ready.
- Consider an event-sourced task log only if messages plus task state are not
  enough for replay and audit needs.
