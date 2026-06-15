---
phase: 20-scheduler-hardening-full-implementation
plan: 04
subsystem: database
tags: [scheduler, cron, migrations, sqlc, pending-notifications, telegram, routing]

requires:
  - phase: 20-03
    provides: deliverToOrigin/originGate + DispatchDeps.ChannelDeliverer/.PreferOriginChannel
  - phase: 20-02
    provides: task.IdentityID schedule-time snapshot
provides:
  - migration 0014 (pending_notifications.identity_id text, no FK)
  - identity_id threaded through InsertPendingNotification + SweepDueNotifications + store_runs projection
  - (*Dispatch).deliverSweptRow — swept rows route back to origin keyed on the row's identity snapshot
  - LIVE Step-2 sign-off: a deferred notification, swept, routes back to the origin Telegram chat
affects: [scheduler, channels, telegram]

tech-stack:
  added: []
  patterns:
    - "Stable identity snapshot column (text, no FK) mirroring scheduler_tasks.identity_id — survives a deleted origin conversation (Fork 1)"
    - "Single-sourced originGate serves both live-task (deliverToOrigin) and swept-row (deliverSweptRow) paths"

key-files:
  created:
    - internal/db/migrations/0014_pending_notifications_identity.up.sql
    - internal/db/migrations/0014_pending_notifications_identity.down.sql
  modified:
    - internal/db/queries/pending_notifications.sql
    - internal/db/sqlc/pending_notifications.sql.go
    - internal/db/sqlc/models.go
    - internal/cron/store_runs.go
    - internal/cron/dispatch.go
    - internal/cron/deliver.go
    - internal/cron/dispatch_integration_test.go

key-decisions:
  - "identity_id is text NO FK (SPEC-superseded the spike's uuid REFERENCES, drift #22) — the stable delivery key survives a deleted origin conversation"
  - "deliverSweptRow keys on the SWEPT ROW's identity_id (no live task at sweep time); shares originGate with the live path (no duplicated precedence)"
  - "owns-but-failed during a sweep returns sweepKeep (mark existing row failed for next-sweep retry) — never a new row, never a cross-channel fallback (Pitfall 3)"

patterns-established:
  - "Append-only sqlc column add ($9 on InsertPendingNotification, no placeholder shift) + sqlc generate"

requirements-completed: [R6]

duration: ~50min (incl. db_integration + live gate)
completed: 2026-06-11
---

# Phase 20 Plan 04: Deferred-sweep route-back + LIVE Step-2 Summary

**Migration 0014 carries the owning-identity snapshot onto `pending_notifications`, and `sweepNotifications` routes a swept deferred/failed row back to its origin channel via the shared `originGate` — proven live (a deferred notification routed back to the origin Telegram chat, row status pending→delivered).**

## Accomplishments
- Migration **0014** (`identity_id text`, no FK) up/down — applies + reverts clean; legacy NULL rows fall back to the route (no backfill, no regression).
- `identity_id` threaded through `InsertPendingNotification` ($9, append-only) + both `SweepDueNotifications` SELECT lists + the regenerated sqlc (`IdentityID pgtype.Text`) + `store_runs.go` projection (`text(p.IdentityID)` / `r.IdentityID.String`).
- `dispatch.insertPendingNotification` threads `task.IdentityID` (completes the 20-03 deferral).
- `deliverSweptRow` + `sweepNotifications` route swept rows to origin keyed on the row's identity, single-sourced through `originGate`.
- db_integration test (migration 0014 up + identity round-trip + down revert) runs live (not skipped).
- **LIVE Step-2 hard gate (D-04) signed off.**

## Task Commits
1. **Task 1: migration 0014 + sqlc edits + store_runs/dispatch threading** — `9cc148a9` (feat)
2. **Task 2: sweepNotifications route-back + db_integration round-trip test** — `25858641` (feat)

_(The originGate stdout-amendment from 20-03 `fcdd8ac8` is single-sourced and applies to this plan's swept-row path too.)_

## Live Step-2 sign-off evidence
- **Approach:** a faithful, controlled proof of the Phase-20 code under test (`deliverSweptRow`). A **due** `pending_notifications` row was inserted (status=pending, `notify_after` in the past, `notify_route='stdout'`, `identity_id=00000000-…-001`, valid `run_id` FK) — byte-identical to what the quiet-hours deferral path produces. (Quiet-hours deferral itself is pre-existing Phase-19 code; the Phase-20 delivery is `deliverSweptRow`.)
- **DB ground truth:** the row carried `identity_id` = the chat's identity; after the sweep tick its `status` transitioned **pending → delivered**. ✅
- **DESTINATION:** the notification text rendered in the SAME origin Telegram chat (CDP-observed), NOT stdout — no `[scheduler notify route=…]` fallback line. ✅
- Confirms `stdout` defers to origin on the swept path too (shared `originGate`), the 0014 `identity_id` is the live delivery key, and no-skip-as-green (the sweep actually fired).

## Deviations from Plan
**1. [Validation method] Step-2 proven via a controlled due-row insert rather than the full quiet-hours agent_job orchestration.** `pending_notifications.run_id` is a NOT-NULL FK to `agent_job_runs`; a faithful deferred row reuses a real completed run id and a past `notify_after` (= immediately due), which exercises the exact Phase-20 path (`sweepNotifications` → `deliverSweptRow` → `originGate` → `DeliverToIdentity`) without the TZ/window timing fragility of forcing `AURA_SCHEDULER_QUIET_HOURS`. The deferral mechanism that normally populates such rows is unchanged Phase-19 code.

**2. [Concurrent session] commit `9cc148a9` absorbed 3 Phase-14 `internal/onboarding/*` files** via a lefthook `gofmt` auto-stage during the live cross-session collision — content byte-identical to disk, no work lost; shared-branch history not rewritten.

## Issues Encountered
- sqlc `generate` ran natively (v1.31.1) — no hand-edit. Stale IDE diagnostics from the concurrent Phase-14 session were repeatedly disproven by isolated `go build`/tests.

## Next Phase Readiness
- Every scheduled-notification class (immediate reminder + deferred/failed sweep) now routes back to origin. PRD env-catalog housekeeping (`AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL`, bool default true) noted for milestone close.

---
*Phase: 20-scheduler-hardening-full-implementation*
*Completed: 2026-06-11*
