---
phase: 51-durable-delegation
plan: 02
subsystem: database
tags: [postgres, steer, ttl, sweep, migration, go, transactions, rls]

# Dependency graph
requires:
  - phase: 51-durable-delegation
    provides: the delegation queue (51-01) whose completions this queue must be able to carry
provides:
  - Durable, identity-scoped, kind-typed steer/delegation-result queue (D-06) — the in-memory Inbox is deleted, not flagged off
  - Two TTLs derived per row kind at Push time, one sweep, two knobs (D-07)
  - Atomic expiry-plus-trace: no queue row is ever silently dropped (D-08)
  - aura.conversation_owner(text), the narrow SECURITY DEFINER identity lookup the locked Push/Drain contract needs
affects: [steer-rail, agui-run-steer, telegram-dispatch, runner, cmd-aura-serve]

# Actuals
actuals:
  tasks: 3
  commits: 2

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Drain IS the claim: a conditional UPDATE ... WHERE drained_at IS NULL RETURNING *, the same conditional-update-as-idempotency-key idiom as MarkPausedStateResumed — not a second locking story"
    - "Lazy expiry on read: Drain excludes a row past expires_at before the sweep catches up (LibreChat peek semantics, D-12)"
    - "Per-kind TTL derived at Push time from the row's own knob, never a single shared cutoff"
    - "markAndTrace as a pure, daemon-free-testable mark-then-trace sequencer inside db.WithIdentityTx, so D-08's rollback contract has a unit test that needs no Postgres"
    - "Narrow SECURITY DEFINER lookup (one column, one table, pinned search_path) as the escape hatch when a locked interface carries no principal"

key-files:
  created:
    - internal/db/migrations/0103_steer_queue.up.sql
    - internal/db/migrations/0103_steer_queue.down.sql
    - internal/db/queries/steer_queue.sql
    - internal/db/sqlc/steer_queue.sql.go
    - internal/steer/pg_store.go
    - internal/steer/pg_store_test.go
    - internal/steer/pg_store_unit_test.go
    - internal/steer/queue_sweep.go
    - internal/steer/queue_sweep_test.go
    - internal/steer/queue_sweep_unit_test.go
    - internal/steer/integration_pool_helper_test.go
    - internal/steer/steertest/fake.go
    - cmd/aura/serve_steer.go
  modified:
    - internal/steer/inbox.go
    - internal/config/config.go
    - internal/db/db_unit_test.go
    - internal/agui/server.go
    - internal/runner/runner.go
    - internal/runner/runner_deps.go
    - internal/runner/runner_steer.go
    - internal/channels/telegram/bot.go
    - cmd/aura/chat_boot.go
    - cmd/aura/serve.go
    - cmd/aura/serve_drain.go
    - cmd/aura/serve_channels.go
    - .env.example
  deleted:
    - internal/steer/inbox_test.go
---

## Accomplishments

The mid-turn steer inbox is durable. `aura.steer_queue` replaces the in-memory,
single-replica-by-construction map that silently lost a delegation completion pushed while
no turn was running — the exact failure SC#1 forbids. One table, rows typed by a NOT NULL
`kind` with no default, each deriving its own TTL from its own knob at Push time
(`AURA_STEER_QUEUE_TTL_SEC` 900, `AURA_DELEGATION_RESULT_TTL_SEC` 86400). One sweep reads
the kind per row rather than applying a single cutoff, and every expiry writes a readable
conversation trace in the same transaction that marks it.

The shipped contract did not move: `internal/agui/server_run_steer.go` and
`internal/channels/telegram/bot_dispatch_steer.go` show zero changed lines, and the
`SteerInbox` interface still has exactly one method. The new type was adapted to the
contract, never the contract to the type.

## Task Commits

| Task | Commit | What |
|---|---|---|
| 1 | — | `checkpoint:decision`, resolved by the operator as `promote-postgres` after the orchestrator read both reference implementations. No commit: checkpoint tasks produce none. |
| 2 | `12d76b45c` | Migration 0103 + sqlc queries + `PostgresStore` behind the unchanged interface; in-memory implementation deleted; both TTL knobs; all call sites re-pointed |
| 3 | `d7b2f5d90` | `Sweeper.ExpireDue` with per-kind cutoff and atomic expiry-plus-trace; wired on the existing resident-loop lifecycle |

## Decisions Made

**D-06/D-07 accepted together as `promote-postgres`** (operator, at the task-1 checkpoint).
The selection was made against the two reference implementations named by
`.planning/spikes/MANIFEST.md`, read before answering rather than after:

- hermes `async_delegations` (`hermes_state_common.py:333`, `tools/async_delegation.py`) is a
  durable table with no in-memory twin, and its retention is already differentiated by row
  state — `DELETE ... WHERE delivery_state='delivered' AND updated_at < cutoff`, and on
  overflow `ORDER BY CASE delivery_state WHEN 'delivered' THEN 0 ELSE 1 END`. Delivered dies
  first, not-yet-read survives. That is D-07's two TTLs in another syntax.
- LibreChat has no message channel at all; `LazyMongoSaver` persists only what somebody must
  come back to and discards the clean-exit checkpoint. A `delegation_result` with no turn
  running is exactly that category; a drained steer is the clean-exit checkpoint. Hence the TTL.
- Neither reference keeps two live implementations of one contract, which is what
  `dual-keep-memory` would have been.

**Divergence recorded rather than smoothed over:** hermes uses a separate table per concern
(`async_delegations` + `delivery_obligations`), NOT one table with a `kind` discriminator.
The single-table-by-kind shape is Aura's own, justified by the in-tree precedent
(`aura.ingestion_jobs.job_type`, spike 100), not by either reference.

## Deviations from Plan

**Added `aura.conversation_owner(text)` (Rule 2 — auto-add missing critical functionality).**
The plan's action text did not anticipate it. The locked `Push(conv, source, text string)` and
`Drain(conv string)` signatures carry neither an identity nor a `context.Context`, so there is
no principal to set `app.current_identity` to; and `aura.conversations`' own fail-closed RLS
(migration 0089) means `aura_app` cannot look the identity up on its own connection either. The
function is SECURITY DEFINER with a pinned `search_path`, exposes exactly one column of one
table, and is revoked from PUBLIC. It takes `text` rather than `uuid` because Postgres resolves
one placeholder to one type per prepared statement, and the mixed `text` / `::uuid` usage failed
live with `operator does not exist: text = uuid`.

**Both task commits were landed by the orchestrator, not the executor.** The `gsd-executor`
agent wrote the whole plan but died three times to Anthropic API transport errors (connection
lost mid-response, ENOTFOUND, no response) before reaching a single commit, having deferred all
commits to the end instead of committing per task. The orchestrator verified the acceptance
criteria and committed the work in two atomic pieces aligned to tasks 2 and 3.

## TDD Gate Compliance

⚠ The `test(...)` RED commit and the `feat(...)` GREEN commit that a `tdd="true"` task must
leave in the log **do not exist**. The tests were written — `pg_store_test.go`,
`pg_store_unit_test.go`, `queue_sweep_test.go`, `queue_sweep_unit_test.go` — but the executor
never landed them separately, so the RED→GREEN sequence cannot be shown from git history. This
is recorded, not reconstructed: fabricating the pair after the fact would be a lie in the log.

## Verification

| Check | Result |
|---|---|
| `go build ./...` | exit 0 |
| `go vet` on the six touched packages | exit 0 |
| `go test` on steer, runner, agui, config, telegram | all ok |
| `git diff --stat` on the two shipped Push callers | zero lines changed |
| `grep -c 'Drain(conv string)' internal/agent/llm_agent_steer.go` | 1 |
| `wc -l` inbox.go / pg_store.go / queue_sweep.go | 88 / 202 / 189 — all ≤ 600 |
| migration `CHECK (kind IN ('steer','delegation_result'))` | present |
| `locked_by` / `lease_generation` in migration | only inside the comment explaining their absence |
| `.env.example` carries both knobs | `AURA_STEER_QUEUE_TTL_SEC=900`, `AURA_DELEGATION_RESULT_TTL_SEC=86400` |
| in-memory implementation removed | no `sync.Mutex` / `map[string][]Message` left in the package |
| lefthook pre-commit (gofmt, file-size, vet, lint) | green on both commits |

## Issues Encountered

The migration slot rule earned itself here. `ls internal/db/migrations/ | tail -1` reported
`0102_paused_state_decision_policy`, so `0103`. The planning documents also said 0102, but a
concurrent worktree took `0104` for scheduler observability while this plan was being written —
a number copied from a document instead of the directory would have collided.

## Owed Before Phase Close

- `go test -race` — Windows has no cgo on this host, so the race detector did not run. Owed
  from WSL or CI. **Not a green signal until it runs.**
- `go test -tags=db_integration ./internal/steer/` run as `aura_app` (never as `aura`, which is
  superuser + BYPASSRLS and would give a false green on the cross-identity assertion), including
  `TestPostgresSteerQueue`, `TestQueueTTLDerivedPerKind`, `TestConcurrentDrain`, `TestExpireDue`.
- `make db-migrate` clean-apply and re-run-is-a-no-op.
- Restart proof: push a `delegation_result`, restart the daemon, open a turn, confirm delivery.
- Coverage floor for `internal/steer`.

## Self-Check: PARTIAL

The plan's automated `<verify>` block is satisfied except for the two tiers this host cannot
run (`-race` needs cgo; `db_integration` needs the stack up as `aura_app`). Both are listed
above as owed. Calling this PASSED would be exactly the skip-as-green CLAUDE.md forbids.
