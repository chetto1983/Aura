# Phase01A Source Audit

Status: source-audited locally on 2026-05-15. A fresh Codex verifier pass ran
on 2026-05-15 and repaired one missing Hub durable-event path. A separate
subagent verifier was not spawned in this turn.

| Source | Decision Supported | Adopt | Reject / Avoid | Status |
| --- | --- | --- | --- | --- |
| `D:/Aura/prd.md` Phase 1A | Run/event schema and gates | Use `runs`, `run_events`, `run_outbox`, idempotency, redacted payloads, audit events | Do not make logs or cache the source of truth | read |
| `D:/Aura/AGENTS.md` Transaction Boundaries | SQLite transaction discipline | Short local DB transactions that append events, update snapshots, and enqueue outbox work | No DB transaction across network/tool work | read |
| `D:/Aura/AGENTS.md` Observability | Trace/audit separation | Metadata trace events plus governed payload handles | No raw prompts, raw tool args, or full child transcripts in run events by default | read |
| `D:/Aura/.planning/aura-deep-refactor-decisions.json` ADR-010/011/019/034 | Foundation, parent/child runs, snapshot+events, observability | `runs` snapshot plus append-only `run_events`; parent/child fields from the first schema | No private child-run loop, no full-app event sourcing | read |
| `D:/Aura/docs/chat-interface-prd.md` sections 2.4, 9.2, 11.2, 11.5 | Chat Hub run/question/event contracts | Stable `InboundMessage`, `OutboundEvent`, `run_id`, per-run `seq`, idempotent replay tolerance | Do not import web UI scope or rename `chat` again | read |
| `D:/Aura/internal/db/migrations/migrations.go` | Existing migration style | Add next registered migration and keep fresh schema plus upgrade path convergent | Do not add a separate migration runner | read |
| `D:/Aura/internal/db/migrations/migrations_test.go` | Migration verification pattern | Extend fresh/upgraded schema tests and idempotency expectations | Do not weaken schema convergence or legacy preservation tests | read |
| `D:/Aura/internal/chat/types.go` | Current run/event contracts | Reuse `Run`, `RunStatus`, `OutboundEvent`, `InboundMessage`, `ParentRunID`, `Seq` | Do not invent a parallel chat event vocabulary | read |
| `D:/Aura/internal/chat/hub.go` | First minimal consumer | Persist `run_started`, terminal `done/error/cancelled`, and per-run sequence via a store seam | Do not change adapter delivery, Telegram rendering, or `/api/chat` shape | read |
| `D:/Aura/internal/chat/agentloop.go` | Tool/usage event producer | Persist whitelisted metadata from `tool_start`, `tool_end`, `usage`, and `message_done` | Do not store raw tool argument values or raw tool result previews by default | verifier repair |
| Azure Architecture Center Event Sourcing pattern | Event-store trade-offs | Use append-only events only for run execution facts that need replay/audit | Do not event-source the entire app | identified; direct reread pending |
| AWS Prescriptive Guidance Transactional Outbox pattern | Delivery/side-effect reliability | Write retryable delivery intent in the same DB transaction as run state where applicable | Do not perform best-effort delivery as the only record of truth | identified; direct reread pending |
| OpenTelemetry GenAI semantic conventions | Trace metadata language | Keep optional exporter fields as projection/correlation metadata | Do not make OTel the local source of truth | identified; direct reread pending |

## Resolved Source Questions

- Current `internal/chat` type names are `Run`, `InboundMessage`,
  `OutboundEvent`, `AgentLoop`, `Hub.ReceiveMessage`, and `Hub.makeEmit`.
- Existing migration style uses an in-file `registered` slice with strictly
  increasing versions. The next migration must be version 6.
- First repository package target is `D:/Aura/internal/storage/runs`.
- First implementation slice should be migration + storage repository tests
  before deeper Hub wiring.

## Remaining Implementation Questions

- No local Phase01A implementation question remains after the verifier repair.
- A separate subagent/independent-agent verifier can still be run if the user
  wants an external score before Phase01B implementation.
