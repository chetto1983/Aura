# Observability, Audit, And Retention Reference Map

Last updated: 2026-05-14

Purpose: keep the sources behind Aura's observability, audit, and retention
decision easy to find during the deep refactor. This map supports ADR-034 and
BG-010.

## Decision

Aura needs three related but separate planes:

- Execution trace plane: durable, append-only `run_events`/trace metadata for
  runs, tools, questions, memory writes, cron fires, workflows, and swarm graph
  events.
- Operational log plane: short-retention JSON/OTel logs for process health and
  debugging, correlated by `run_id`, `trace_id`, and `span_id`, but never the
  canonical source of run truth.
- Governed artifact/audit plane: content-addressed payload artifacts and
  append-only audit events for high-risk actions, exports, purges, settings,
  grants, memory writes, source changes, skill changes, and privileged access.

OpenTelemetry is the export and correlation language, not Aura's canonical
store. The local SQLite run/event and artifact stores remain authoritative.

## Aura Evidence

- `D:/Aura/internal/agent/runtime.go`
  - Current runtime already emits useful in-process events:
    `tools_exposed`, `stats`, `final`, `llm_start`, `message_delta`,
    `tool_start`, `tool_end`, and `question_requested`.
  - Tool events store tool name, call id, argument keys, elapsed time, success,
    and a result preview. This is the right shape, but the callback is not yet a
    durable trace store.

- `D:/Aura/internal/chat/types.go`
  - `OutboundEvent` already models channel-neutral events with `RunID`, `Seq`,
    content, payload, and timestamp.
  - The file explicitly treats current behavior as Slice 0 in-memory; durable
    persistence belongs in the refactor.

- `D:/Aura/internal/logging/zap_slog.go`
  - Aura logs JSON to stdout and a daily file, currently keeping one day.
  - This is appropriate for operational logs, not enough for execution history
    or audit.

- `D:/Aura/internal/api/health_sanitize.go`
  - Redaction currently masks known secret-like attribute keys.
  - Missing: nested structures, value-pattern detection, field classification,
    and trace/artifact redaction policies before persistence.

- `D:/Aura/internal/config/config.go`
  - `DefaultTraceRetentionDays` is 30 and `AURA_TRACE_RETENTION_DAYS` is
    normalized to 1..365.
  - The setting should govern execution trace metadata retention, not arbitrary
    log files or canonical knowledge.

- `D:/Aura/internal/telegram/conversation_snapshot.go`
  - Existing snapshot pruning honors `TraceRetentionDays`.
  - Useful precedent: retention already exists, but it is too narrow and
    Telegram-shaped.

- `D:/Aura/internal/api/health.go`
  - The dashboard has health rollups for process, wiki, sources, tasks,
    scheduler, embed cache, vector memory, and reindex state.
  - Target: add run/trace/audit/exporter health without exposing payloads.

- `D:/Aura/internal/swarm/store.go`
  - Swarm persistence already has run/task snapshots and read-side
    observability.
  - Target: fold swarm events into the shared run/event trace vocabulary.

## Local Examples

- `D:/tmp/nanobot/docs/python-sdk.md`
  - Nanobot exposes lifecycle hooks for observability: before iteration,
    streaming, before tools, after iteration, and finalization.
  - Adopt: hook-style instrumentation around the loop.
  - Reject: example audit code prints raw `tc.arguments`; Aura must store
    argument keys by default and gated payload artifacts only when needed.

- `D:/tmp/nanobot/docs/memory.md`
  - Long-term memory edits are versioned with `GitStore`, making memory change
    auditable and restorable.
  - Adopt: memory writes are auditable product events, not invisible mutations.

- `D:/tmp/nanobot/docs/configuration.md`
  - Idle compaction can rewrite session files and lose structured tool-call
    history; token-driven soft consolidation preserves raw tool-call trails.
  - Adopt: context compaction must not silently destroy replay/audit trails
    unless the retention policy explicitly purges them.

- `D:/tmp/nanobot/webui/src/lib/types.ts`
  - Web UI separates user-visible messages from `trace` rows.
  - Adopt: UI timeline can expose compact trace breadcrumbs separately from
    final chat content.

- `D:/tmp/elysia/elysia/objects.py`
  - `Error` objects are saved and fed back to the decision process when the same
    tool is retried.
  - Adopt: failed tool attempts become structured observations and trace events
    that can guide recovery and later learning.

- `D:/tmp/hermes-agent/plugins/observability/langfuse/README.md`
  - Langfuse tracing is opt-in, credential-gated, configurable, and sampleable.
  - Adopt: external observability exporters are optional projections.

- `D:/tmp/hermes-agent/plugins/observability/langfuse/__init__.py`
  - The plugin tracks root traces, generations, tools, and per-turn tool calls.
  - Adopt: trace state needs run/session correlation and tool spans.
  - Reject: external SaaS tracing cannot be the canonical store for local-first
    Aura.

## External Sources

- OpenTelemetry GenAI spans:
  - <https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-spans/>
  - Adopt: LLM inference spans, retrieval spans, and `execute_tool` spans with
    `gen_ai.tool.name`, `gen_ai.tool.call.id`, status, errors, token usage, and
    retrieval document references.
  - Important constraint: tool arguments, retrieved documents, and
    input/output content are opt-in or externally stored references, not default
    log payloads.

- OpenTelemetry GenAI agent/workflow spans:
  - <https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-agent-spans/>
  - Adopt: `invoke_agent` and `invoke_workflow` spans for parent runs, child
    runs, and swarm/workflow execution.

- OpenTelemetry log data model:
  - <https://opentelemetry.io/docs/specs/otel/logs/data-model/>
  - Adopt: `TraceId`, `SpanId`, severity, attributes, and `EventName` for log
    correlation and event semantics.
  - Reject: using free-form logs as the durable run/event store.

- OWASP Logging Cheat Sheet:
  - <https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html>
  - Adopt: record when/where/who/what, interaction identifiers, result status,
    reason, analytical confidence, high-risk actions, and configuration changes.
  - Adopt: exclude, mask, sanitize, hash, or encrypt source code, session IDs,
    tokens, PII, passwords, connection strings, keys, and data above the logging
    system's classification.
  - Adopt: protect logs/audit data from tampering, unauthorized access,
    modification, and deletion; enforce retention and disposal windows.

- Phoenix docs:
  - <https://arize.com/docs/phoenix>
  - Adopt: traces should capture model calls, retrieval, tool use, custom logic,
    evaluations, datasets, experiments, and prompt iteration.
  - Reject: sending local-first private payloads to Phoenix by default.

- Langfuse docs:
  - <https://langfuse.com/docs/observability/sdk/overview>
  - Adopt: OTel trace/span hierarchy, context propagation, generation spans,
    tool/RAG observations, scores, and optional exporter integration.
  - Reject: making Langfuse SDK concepts the internal domain model.

## Target Event Classes

Execution trace events:

- `run_started`, `prompt_built`, `llm_started`, `llm_completed`
- `tool_started`, `tool_completed`, `tool_failed`
- `retrieval_started`, `retrieval_completed`, `retrieval_degraded`
- `question_requested`, `question_answered`, `approval_requested`,
  `approval_answered`
- `child_run_requested`, `child_run_started`, `child_run_completed`,
  `child_run_failed`
- `workflow_step_started`, `workflow_step_completed`,
  `workflow_step_failed`
- `cron_fire_detected`, `cron_fire_submitted`, `cron_fire_skipped`
- `run_completed`, `run_failed`, `run_cancelled`

Audit events:

- identity and capability grant changes
- authorization denials and privilege escalation attempts
- settings changes
- memory write requests, approvals, commits, rejects, and rollbacks
- wiki/source import, delete, purge, re-OCR, and export
- skill install, update, enable, disable, delete
- cron schedule create, update, pause, resume, delete, manual fire
- artifact export, backup, restore, purge, privileged payload access
- OTel/exporter configuration changes

Operational logs:

- process start/stop and subsystem initialization
- health probe failures and exporter failures
- compact error codes and low-cardinality exception classes
- correlation IDs and counters, not raw user/tool payloads

## Redaction And Payload Policy

Default trace payload policy is `metadata_only`.

Allowed by default:

- run id, parent run id, actor id, principal id
- event type, seq, timestamps, status
- model/provider name, prompt hash/version/modules
- tool name, tool call id, argument key list
- elapsed time, retry count, token/cost counters
- result status, error class/code, redacted preview
- source ids, artifact ids, citation handles, freshness state

Gated artifact-only:

- full user messages
- full prompts and system instructions
- full tool arguments and outputs
- raw retrieved chunks
- child transcripts
- OCR/raw extraction JSON
- file contents and source paths when classified sensitive

Never in ordinary logs:

- secrets, API keys, tokens, passwords, connection strings
- private keys and encryption material
- raw PII or user-memory facts unless explicitly classified and gated
- large source code blocks
- full prompt/tool payloads

## Retention Defaults

- Operational logs: one day by default, configurable for local debugging.
- Execution trace metadata: `AURA_TRACE_RETENTION_DAYS`, default 30 days,
  range 1..365.
- Debug payload artifacts: seven days unless promoted or attached to a
  review/audit bundle.
- Reviewable payload artifacts: 30 days by default.
- Audit metadata: 365 days by default, metadata-only unless a governed payload
  artifact is required.
- Canonical knowledge and source stores are not trace retention; they follow
  their own source-of-truth and delete/forget policies.

Purges append tombstone/audit events and remove payload artifacts first. Trace
metadata may remain with redaction state and artifact tombstones when needed for
causal/audit integrity.

## Dashboard Contract

The dashboard should expose:

- run timeline and parent/child run graph;
- tool names, argument keys, status, latency, token/cost counters;
- retrieval source ids, freshness state, and cited artifact handles;
- question wait states, expiry, answer state, and blocking scope;
- cron fire and workflow links;
- swarm critical path, useful-agent utilization, protocol overhead, and error
  amplification;
- redaction indicators and "why hidden" labels;
- privileged payload unlock only through an audited access path;
- exporter health and dropped/export-failed counters.

## Verification Fixtures

Add tests for:

- tool argument values are not persisted in trace metadata by default;
- nested and value-pattern secrets are redacted before logs/export;
- `run_events` timeline correlates to logs with `run_id`/`trace_id`;
- OTel projection emits inference, retrieval, tool, agent, and workflow spans
  without raw payloads by default;
- retention purges payload artifacts but leaves audit tombstones;
- memory writes, settings changes, skill changes, and exports create audit
  events;
- export bundle includes a redaction manifest;
- privileged payload reads are themselves audited;
- UI run timeline shows hidden/redacted payload state without leaking content.
