---
phase: 19-audit-bug-resolution-e2e-live-test
plan: 07
subsystem: scheduler
tags: [cron, notifications, postgres, sqlc, quiet-hours, retry]
requires: []
provides:
  - aura.pending_notifications durable queue
  - sqlc pending notification query surface
  - quiet-hours notification deferral persistence
  - bounded failed-notification retry sweep
affects: [scheduler, cron-dispatch, db-migrations, serve-daemon]
tech-stack:
  added: []
  patterns: [transactional skip-locked sweep, bounded notification retry]
key-files:
  created:
    - internal/db/migrations/0013_pending_notifications.up.sql
    - internal/db/migrations/0013_pending_notifications.down.sql
    - internal/db/queries/pending_notifications.sql
    - internal/db/sqlc/pending_notifications.sql.go
  modified:
    - internal/db/sqlc/models.go
    - internal/db/sqlc/querier.go
    - internal/cron/store_runs.go
    - internal/cron/dispatch.go
    - internal/cron/scheduler.go
    - internal/cron/dispatch_test.go
    - internal/cron/dispatch_integration_test.go
    - internal/cron/scheduler_test.go
    - cmd/aura/serve.go
key-decisions:
  - "Use a new pending_notifications table rather than overloading agent_job_runs terminal state."
  - "Run SweepDueNotifications inside db.WithTx so FOR UPDATE SKIP LOCKED has transactional effect."
  - "Auto-wire DispatchDeps.NotificationStore from DispatchDeps.Store when the store satisfies the durable queue interface."
patterns-established:
  - "Quiet-hours deferral persists notify_after as the current quiet window end."
  - "Failed self-sends persist status='failed' and are retried until attempts reaches the bound."
requirements-completed: [H6, H7]
duration: 60 min
completed: 2026-06-10
---

# Phase 19 Plan 07: Durable Scheduler Notifications Summary

**Scheduler notifications are now durable: quiet-hours deferrals are queued and flushed later, and failed MCP self-sends are retried within a hard bound.**

## Performance

- **Duration:** 60 min
- **Completed:** 2026-06-10
- **Tasks:** 2
- **Files modified:** 13

## Accomplishments

- Added migration `0013_pending_notifications` with DML-only `aura_app` grants, no DELETE grant, and a partial pending-row due index.
- Added sqlc queries and regenerated `models.go`, `querier.go`, and `pending_notifications.sql.go`.
- Added cron store wrappers for insert, sweep, delivered mark, and failed mark.
- Changed quiet-hours advisory notifications from log-and-drop to durable pending rows with `notify_after` set to the quiet window end.
- Changed undelivered notifier errors from log-only to durable failed rows.
- Added a scheduler tick sweep that flushes pending rows and retries failed rows until `attempts < 3` is exhausted.
- Added unit and db-integration coverage for H6/H7 behavior.

## Task Commits

1. **Task 1: Migration, sqlc queries, and regenerated client** - `4094d47b` (feat)
2. **Task 2: Persist deferred/failed notifications and sweep on tick** - `65574350` (feat)

**Plan metadata:** this SUMMARY.md commit.

## Files Created/Modified

- `internal/db/migrations/0013_pending_notifications.up.sql` / `.down.sql` - durable queue table and rollback.
- `internal/db/queries/pending_notifications.sql` - insert, sweep, delivered, and failed queries.
- `internal/db/sqlc/*` - generated pending notification model and query surface.
- `internal/cron/store_runs.go` - domain wrappers over the generated queue queries.
- `internal/cron/dispatch.go` - notification persistence and bounded sweep behavior.
- `internal/cron/scheduler.go` - existing tick pass invokes the optional sweep.
- `cmd/aura/serve.go` - live dispatcher receives both quiet-hours predicate and quiet-hours end function.

## Decisions Made

The sweep rides the existing scheduler tick rather than introducing another ticker. Failed rows use a fixed bound of three attempts; once attempts reaches the bound, the query no longer selects them.

## Deviations from Plan

None - plan executed as written. The quiet-hours boolean seam was kept compatible by adding a paired `QuietHoursEnd` hook instead of changing the existing predicate signature.

## Issues Encountered

None.

## Verification

- `sqlc generate` - passed.
- `go build ./internal/db/...` - passed.
- `go vet ./internal/db/...` - passed.
- `go test ./internal/cron/` - passed.
- `go build ./internal/db/... ./internal/cron/ ./cmd/aura` - passed.
- `go vet ./internal/db/... ./internal/cron/ ./cmd/aura` - passed.
- `go test -tags db_integration -run 'TestMigrat|TestPendingNotif|Test0013|TestNotify|TestSweep|TestQuietHours|TestDispatch' ./internal/db/ ./internal/cron/` - passed.
- `go test -tags db_integration -race -run 'TestPendingNotif|TestDispatch|TestMigrat|Test0013' ./internal/db/ ./internal/cron/` - passed.

## User Setup Required

Apply DB migration `0013_pending_notifications` before running the updated scheduler daemon in an existing environment.

## Next Phase Readiness

Plan 19-08 can build on the pending notification table and the cron store/dispatch edits now that H6/H7 durability is real.

---
*Phase: 19-audit-bug-resolution-e2e-live-test*
*Completed: 2026-06-10*
