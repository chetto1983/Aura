# Phase01A Benchmark

| Check | Command / Method | Threshold | Actual Result | Status |
| --- | --- | --- | --- | --- |
| Baseline migrations/chat | `go test ./internal/db/migrations ./internal/chat` | green before edits | PASS 2026-05-15 | done |
| Migration schema | `go test ./internal/db/migrations` | run/event/outbox/audit tables present | PASS 2026-05-15 | done |
| Run repository | `go test ./internal/storage/runs` | append/replay/idempotency tests green | PASS 2026-05-15 | done |
| Chat baseline/regression | `go test ./internal/chat` | current behavior stable | PASS 2026-05-15 | done |
| Chat durable consumer | `go test ./internal/chat` | lifecycle/tool/usage write tests green | PASS 2026-05-15 | done |
| Shared compile | `go build ./...` | green | PASS 2026-05-15 | done |
| Vet | `go vet ./...` | green | PASS 2026-05-15 | done |
| Full suite | `go test ./...` | green or documented pre-existing failures | PASS 2026-05-15 | done |

## Required Fixtures

- duplicate inbound idempotency key produces one run
- replayed events produce same snapshot
- terminal state survives reopen
- outbox failed delivery remains retryable
- parent/child correlation fields persist
- payload preview is redacted and schema-versioned

## First Patch Validation Order

1. `go test ./internal/db/migrations ./internal/chat`
2. Implement migration and repository tests.
3. `go test ./internal/db/migrations ./internal/storage/runs ./internal/chat`
4. `go build ./...`
5. `go vet ./...`
6. `go test ./...`

## Completed First Patch Coverage

- duplicate inbound idempotency key produces one run: covered by
  `internal/storage/runs`
- replayed events produce same snapshot/order: covered by
  `internal/storage/runs`
- terminal state survives store reopen: covered by `internal/storage/runs`
- outbox failed/pending delivery remains retryable by durable row: covered by
  schema and outbox idempotency test
- parent/child correlation fields persist: covered by migration/store tests
- payload preview is redacted/schema-versioned at the storage and Hub boundary:
  covered by `internal/storage/runs` and `internal/chat`

## Completed Hub Wiring Coverage

- `chat.Hub` can create durable run snapshots through an optional lifecycle
  store seam.
- duplicate inbound message IDs return the existing run and do not re-enter the
  agent loop.
- `run_started`, `error`, `cancelled`, and `done` lifecycle events are persisted
  without raw user text, channel data, or raw loop error strings in default
  event payloads.
- `tool_start`, `tool_end`, `usage`, and `message_done` events are persisted
  with whitelisted metadata only: tool names, call ids, argument keys, status,
  timing, usage counters, delivery status, and final text preview.
- Raw user prompts, raw tool argument values, raw tool result previews, raw
  usage notes, and raw delivery values are excluded from default stored event
  payloads.

## Fresh Verifier Pass

2026-05-15 Codex verifier pass found one Phase01A gap: Hub persistence covered
run lifecycle but not the PRD-required tool/usage/final-output metadata events.
The gap was repaired in `internal/chat` and revalidated with:

1. `go test ./internal/db/migrations ./internal/storage/runs ./internal/chat`
2. `go build ./...`
3. `go vet ./...`
4. `go test ./...`

No separate subagent verifier was spawned in this turn.
