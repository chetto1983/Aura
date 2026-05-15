# Phase01A Plan - Persist the Run/Event Foundation

Status: locally implemented, verifier-repaired, and fully Go-tested. A separate
subagent verifier was not spawned in this turn.

## Goal

Make `chat` durable before more channels, cron, and swarm work depend on it.

## Scope

- SQLite migrations for `runs`, `run_events`, `run_outbox`, idempotency keys,
  and first `audit_events`.
- `internal/storage/runs` as the run/event repository boundary.
- Minimal `internal/chat` consumer wiring for run started and terminal events.

## Non-Goals

- No Telegram rendering changes.
- No `/api/chat` response-shape changes.
- No cron missed-run policy.
- No swarm topology implementation.
- No cache-plane storage for canonical run state.

## Roadmap

1. Add migration tests for run/event/outbox/audit tables.
2. Add `internal/storage/runs` append/read APIs with per-run monotonic sequence.
3. Persist run snapshot updates in the same SQLite transaction as events.
4. Add idempotency handling for duplicate inbound messages.
5. Wire minimal `chat.Hub` run started and terminal events after the store is
   green.
6. Record redaction and payload-artifact fields without storing raw sensitive
   payloads by default.

## Codex Loop Requirement

Every remaining Phase01A implementation slice must explicitly follow the Aura
Codex Loop:

1. Inspect: reread the smallest files that define current behavior.
2. Hypothesis: state what must change and what must not change.
3. Plan: name exact files and verification commands.
4. Patch: use the smallest coherent code edit.
5. Verify: run narrow tests first, then broader Go gates when shared behavior
   changes.
6. Record: update this folder, `D:/Aura/.planning/progress.txt`, and handoff
   files only after verification or a documented blocker.

## First Bounded Code Slice

Implement only the storage foundation:

- add migration version 6 in `D:/Aura/internal/db/migrations/migrations.go`;
- extend `D:/Aura/internal/db/migrations/migrations_test.go`;
- create `D:/Aura/internal/storage/runs` with a small repository and focused
  tests for append/replay/idempotency;
- do not wire `internal/chat.Hub` until the migration and repository contract
  are green.

This keeps Phase01A useful even if Hub wiring needs a second patch.

## Next Bounded Code Slice

Completed on 2026-05-15: minimal `internal/chat.Hub` lifecycle persistence is
wired through `internal/storage/runs`.

Remaining local hardening before calling Phase01A closed:

- optional: run a separate subagent/independent-agent verifier if required by
  the current workflow before implementation of Phase01B.

## Decisions Before Implementation

| Decision | Default | Consequence |
| --- | --- | --- |
| Canonical store | SQLite run/event tables | Cache remains disposable. |
| Package boundary | `internal/storage/runs` | Channels and chat do not own SQL details. |
| Audit events | separate `audit_events` table | Audit is not mixed with debug logs or cache. |
| Payload policy | redacted previews + artifact handles | Full prompts/tool args are not default event payloads. |
| First patch shape | migrations + storage repository only | Reduces risk before Hub wiring. |

## PRD Coverage

| PRD Item | Plan Location | Benchmark Location | Source Evidence | Status |
| --- | --- | --- | --- | --- |
| Add `runs`, `run_events`, `run_outbox` | First bounded code slice | `benchmark.md` migration tests | `source.md` | locally implemented |
| Persist `run_started` and terminal events | Roadmap item 5 | `benchmark.md` chat tests | `source.md` | locally implemented |
| Same-transaction snapshot updates | First bounded code slice | `benchmark.md` storage tests | `source.md` | locally implemented |
| Monotonic per-run sequence | First bounded code slice | `benchmark.md` ordering tests | `source.md` | locally implemented |
| Idempotency keys | First bounded code slice | `benchmark.md` duplicate tests | `source.md` | locally implemented |
| Redacted schema-versioned payloads | Roadmap item 6 | `benchmark.md` payload tests | `source.md` | locally implemented |
| Persist tool names, call ids, argument keys, status, timing, usage, and error class without raw values | PRD Phase 1A durable-event verifier repair | `benchmark.md` chat tests | `source.md` | locally implemented |
| Audit events | Roadmap item 1 | `benchmark.md` migration tests | `source.md` | locally implemented |

## Implementation Gates

- Baseline tests pass before edits.
- Migration package tests pass after edits.
- New `internal/storage/runs` tests prove idempotency and replay.
- Chat tests remain green before and after the storage foundation patch.
- Fresh verifier is still required before claiming the phase as independently
  verified.

## Rollback / Deviation Rule

If migration shape or payload policy conflicts with PRD decisions, stop and
record the decision in this folder before editing code.
