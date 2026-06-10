---
phase: 19-audit-bug-resolution-e2e-live-test
plan: 08
subsystem: scheduler
tags: [cron, recovery, postgres, sqlc, context, advisory-lock, shutdown]
requires:
  - phase: 19-07
    provides: durable pending_notifications + shared cron dispatch/store edits + migration 0013
provides:
  - boot catch-up consults each handler's ReschedulesOnRecovery (M-g recovery invariant)
  - terminal run-state write detached from the signal-cancelled root ctx on shutdown (M-h)
  - inert FOR UPDATE SKIP LOCKED dropped from DueTasks; advisory-lock correctness documented (L5)
affects: [scheduler, cron-dispatch, cron-recovery, serve-daemon, db-sqlc]
tech-stack:
  added: []
  patterns:
    - "Handler-meta lookup seam (consumer-declared func injected via SchedulerConfig) consulted by boot recovery"
    - "Detach-from-cancelled-root via context.WithoutCancel + short deadline for a terminal DB write on shutdown"
key-files:
  created: []
  modified:
    - internal/cron/recover.go
    - internal/cron/dispatch.go
    - internal/cron/scheduler.go
    - internal/cron/store_runs.go
    - cmd/aura/serve.go
    - internal/db/queries/scheduler_tasks.sql
    - internal/db/sqlc/scheduler_tasks.sql.go
    - internal/db/sqlc/querier.go
    - internal/cron/scheduler_test.go
    - internal/cron/recover_test.go
    - internal/cron/dispatch_integration_test.go
key-decisions:
  - "ReschedulesOnRecovery seam is a func(TaskKind) bool on SchedulerConfig, backed by a new *Dispatch.ReschedulesOnRecovery method that reads the composition root's handler map (no new exported domain type)."
  - "catchUpMissed ALWAYS advances next_run_at (cadence resumes); only the catch-up RE-FIRE is gated by the flag — a false handler's missed window is dropped, never replayed."
  - "A nil ReschedulesOnRecovery lookup fails SAFE to the historical always-fire behavior so a missing seam never silently drops every catch-up."
  - "M-h uses context.WithoutCancel(ctx) + a 5s deadline (preserves tracing values, severs cancellation) rather than WithTimeout(Background(),…); CompleteRun reads no ctx values so the choice is cosmetic."
  - "L5 is the minimal-real drop of the inert clause, NOT a select+claim tx fold — the real tx-scoped SKIP LOCKED is the 19-07 notification sweep; correctness here is the per-task pg_try_advisory_lock."
patterns-established:
  - "Boot recovery gates a re-fire on the handler's static HandlerMeta flag via an injected lookup."
  - "Terminal lifecycle DB writes detach from the shutdown-cancelled root ctx."
requirements-completed: [M-g, M-h, L5]
duration: 50min
completed: 2026-06-10
---

# Phase 19 Plan 08: Scheduler Contract Correctness (M-g / M-h / L5) Summary

**The boot catch-up now honors each handler's ReschedulesOnRecovery flag (no replay of committed side effects on recovery), a shutdown mid-run writes its terminal run state on a detached ctx instead of leaving the run stuck `running`, and the inert autocommit-pool SKIP LOCKED is gone with advisory-lock correctness documented.**

## Performance

- **Duration:** ~50 min
- **Completed:** 2026-06-10
- **Tasks:** 2
- **Files modified:** 11

## Accomplishments

- **M-g** — Added a handler-meta lookup seam (`SchedulerConfig.ReschedulesOnRecovery`, backed by the new `*Dispatch.ReschedulesOnRecovery` method over the existing handler map) and wired it through `catchUpMissed`: an overdue task whose handler does NOT reschedule on recovery is never auto-re-fired (its committed side effect is not replayed), while its cadence still advances; a handler that DOES reschedule still fires once. The PRD recovery invariant is now enforced (it was a dead control before).
- **M-h** — The terminal run-state write in `complete` now runs on a ctx detached via `context.WithoutCancel` + a 5s deadline, so a shutdown mid-run (signal-cancelled root ctx) still flips the run to `completed`/`failed` instead of leaving it `running` until the 90s orphan scan.
- **L5** — Dropped the inert `FOR UPDATE SKIP LOCKED` from the `DueTasks` query (it ran on the autocommit pool, so the lock released at SELECT return), regenerated the sqlc client (no drift), and documented in three places (the `.sql`, the generated client, and the store wrapper) that claim correctness is held by the per-task `pg_try_advisory_lock` in `claim.go`.

## Task Commits

1. **Task 1: Consult ReschedulesOnRecovery in boot catch-up (M-g)** - `cd39847e` (fix)
2. **Task 2: Detached terminal write on shutdown (M-h) + drop inert SKIP LOCKED (L5)** - `8f4163aa` (fix)

**Plan metadata:** this SUMMARY.md commit.

## Files Created/Modified

- `internal/cron/recover.go` - `catchUpMissed` consults the seam; new `firesOnRecovery` helper (nil-safe fail-safe).
- `internal/cron/dispatch.go` - new `*Dispatch.ReschedulesOnRecovery` lookup method; `complete` detaches the terminal write ctx (`completeRunTimeout = 5s`).
- `internal/cron/scheduler.go` - `Scheduler.reschedulesOnRecovery` field + `SchedulerConfig.ReschedulesOnRecovery` + `NewScheduler` wiring.
- `internal/cron/store_runs.go` - `DueTasks` doc updated (advisory-lock correctness, SKIP LOCKED dropped).
- `cmd/aura/serve.go` - capture the built dispatch and pass `dispatch.ReschedulesOnRecovery` into the live scheduler.
- `internal/db/queries/scheduler_tasks.sql` - dropped `FOR UPDATE SKIP LOCKED` from `DueTasks` + clarifying comment.
- `internal/db/sqlc/scheduler_tasks.sql.go` / `internal/db/sqlc/querier.go` - regenerated via `sqlc generate` (v1.31.1) to match the `.sql` source.
- `internal/cron/scheduler_test.go` - unit-tier `TestReschedulesOnRecoverySeam` (firesOnRecovery fail-safe + flag branches) + `TestDispatchReschedulesOnRecoveryLookup`.
- `internal/cron/recover_test.go` - db_integration `TestCatchUpMissed_ConsultsReschedulesOnRecovery` (false handler not re-fired but cadence advances; true handler fires once).
- `internal/cron/dispatch_integration_test.go` - db_integration `TestDispatchCompletesRunOnCancelledRootCtx` (M-h: run flips to completed on a cancelled root ctx).

## Decisions Made

- The seam is a narrow consumer-declared `func(TaskKind) bool` injected through `SchedulerConfig`, not a new interface or exported domain type — it reuses the handler map the composition root already owns. A nil lookup fails safe to always-fire so an unwired scheduler never silently drops every catch-up.
- `catchUpMissed` advances `next_run_at` for every overdue task unconditionally; only the catch-up re-fire is gated. This preserves cadence for non-rescheduling handlers while suppressing the replay.
- M-h used `context.WithoutCancel` (per the plan's preference) over the `WithTimeout(Background(),…)` form to preserve any tracing values; `CompleteRun` reads no ctx values, so the choice is cosmetic. A 5s deadline keeps a wedged DB from hanging the shutdown drain.
- L5 is the minimal-real drop, not a tx fold (the real tx-scoped SKIP LOCKED is 19-07's notification sweep).

## Deviations from Plan

None - plan executed as written. The M-g seam landed as `SchedulerConfig.ReschedulesOnRecovery` (a func field) + `*Dispatch.ReschedulesOnRecovery` (the lookup), exactly the composition-root-owned handler-map seam the plan described.

## Issues Encountered

None.

## Verification

All gates green on the live stack (Postgres up):

- `sqlc generate` (v1.31.1) - passed; regenerated `DueTasks` no longer contains `FOR UPDATE SKIP LOCKED`; `querier.go`/`scheduler_tasks.sql.go` match the `.sql` source (no drift in other queries).
- `go build ./...` - passed.
- `go vet ./internal/cron/ ./internal/db/... ./cmd/aura` - passed.
- `go test ./internal/cron/` (unit) - passed (M-g seam tests).
- `go test -tags db_integration ./internal/cron/` (full package) - passed.
- `go test -tags db_integration -race -run 'TestDispatch|TestCatchUp|TestReschedule|TestClaim|TestScheduler|TestRecover|TestMissed' ./internal/cron/` - passed (race detector links via the w64devkit `BASH_ENV` toolchain fix).
- **M-h fails-before/passes-after proven:** with the detach reverted, `TestDispatchCompletesRunOnCancelledRootCtx` FAILS (WARN "context canceled", run stuck `running`); with the detach restored it PASSES. dispatch.go restored byte-identical.
- **M-g fails-before/passes-after:** the new seam is consulted only by this plan's code; the unit + db_integration tests assert the false branch is skipped (no re-fire) while the cadence advances and the true branch still fires once.

## sqlc Regeneration Status

Regenerated with `sqlc generate` (v1.31.1, matches the generated-file header). Only `internal/db/sqlc/scheduler_tasks.sql.go` and `internal/db/sqlc/querier.go` changed; both reflect the dropped clause and the propagated `DueTasks` doc comment. No other generated query drifted. Both the `.sql` source and the regenerated `.sql.go` were staged in the same commit.

## Next Phase Readiness

The remaining scheduler-contract findings are closed. 19-11 (Layer-2 live operator sign-off) does not depend on M-g/M-h/L5 directly (they are not in its user-observable repro list), but the scheduler subsystem is now contract-clean for any boot-recovery / shutdown E2E.

## Self-Check: PASSED

- All 11 modified files present on disk; SUMMARY.md present.
- Task commits `cd39847e` (M-g) and `8f4163aa` (M-h/L5) present in git history.

---
*Phase: 19-audit-bug-resolution-e2e-live-test*
*Completed: 2026-06-10*
