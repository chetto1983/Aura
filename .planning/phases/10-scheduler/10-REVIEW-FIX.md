---
phase: 10-scheduler
fixed_at: 2026-06-04T18:45:00Z
review_path: .planning/phases/10-scheduler/10-REVIEW.md
iteration: 1
findings_in_scope: 7
fixed: 7
skipped: 0
status: all_fixed
---

# Phase 10: Code Review Fix Report

**Fixed at:** 2026-06-04
**Source review:** .planning/phases/10-scheduler/10-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 7 (CR-01, CR-02, WR-01..WR-05)
- Fixed: 7
- Skipped: 0

All Critical and Warning findings were fixed and verified. Each fix was committed
atomically and verified by re-read (Tier 1), `go vet`/`go build` (Tier 2), package
unit tests, and — for the DB-backed paths (CR-01, CR-02, WR-02, WR-03) — the live
`db_integration` tier against the running Postgres, plus a `-race` run of the full
`internal/cron` integration suite in WSL.

## Fixed Issues

### CR-01: Boot catch-up fires are computed and silently discarded

**Files modified:** `internal/cron/scheduler.go`, `internal/cron/recover_test.go`, `internal/cron/claim.go`, `internal/cron/dispatch.go`, `internal/cron/store_runs.go`, `internal/cron/handlers/handler.go`, `internal/cron/handlers/backup.go`, `cmd/aura/serve.go`
**Commit:** 4b016166
**Applied fix:** `Scheduler.Start` now captures the `[]MissedTask` from `catchUpMissed`
and dispatches each via a new `Scheduler.runMissed`, which mirrors `runOne` (held-conn
advisory-lock claim + heartbeat + dispatch) but does NOT reschedule (catchUpMissed
already advanced `next_run_at`) and threads `MissedSince` onto the `Claim` + stamps it
on the `agent_job_runs` row (new `setMissedSinceOnConn` held-conn writer). `MissedSince`
now flows `Claim → cron.Job → handlers.Job`, so the backup handler reaches
`handlers.MissedBackupAlert` (the SC#3 24h alert, previously dead from any live path).
Regression `TestStartDispatchesMissedAtBoot` asserts an overdue task is dispatched
exactly once at `Start` AND the catch-up run carries `missed_since` — not merely the
`next_run_at` advance. Verified live `db_integration` + `-race` in WSL.

### CR-02: A past `at` task is persisted with NULL next_run_at and never fires

**Files modified:** `internal/cron/schedule.go`, `internal/cron/schedule_test.go`, `internal/agent/tools/task.go`, `cmd/aura/task.go`
**Commit:** 0ce6649a
**Applied fix:** Added a shared `cron.FirstFire(spec, now)` gate in `schedule.go` (the
reusable-validation home the project rule prefers) plus an `ErrPastRunAt` sentinel. For
a one-shot `at` whose instant is already past, `NextRunAt` returns a zero time;
`FirstFire` now converts that into `ErrPastRunAt` instead of letting it persist as
`next_run_at = NULL` (which `DueTasks` could never select → silent drop). Both the LLM
`task` tool (`resolveSchedule`) and the CLI (`taskSchedule`) call `FirstFire`, so a past
`at` is rejected with a clear error at schedule time rather than reported as a successful
"scheduled" no-op. `ParseSchedule` stays pure (its unit tests exercise past-instant
`NextRunAt` semantics), so the signature is unchanged. Regression `TestFirstFire`.

### WR-01: run_now / list / doctor never surface an unschedulable (NULL next_run_at) task

**Files modified:** `internal/agent/tools/task.go`, `internal/agent/tools/task_test.go`, `cmd/aura/task.go`
**Commit:** 4dccc22b
**Applied fix:** `renderTaskList` (LLM), `taskList` (CLI), and `taskDoctor` (CLI) now flag
`status='active' AND next_run_at IS NULL` as `[unschedulable]` (and `doctor` prints an
`unschedulable` count when any exist), so an operator can `run_now`/cancel the row
instead of wondering why it never fires. Regression `TestTaskList` asserts the flag.

### WR-02: DueTasks casts an unvalidated int limit to int32

**Files modified:** `internal/cron/store_runs.go`, `internal/cron/store_test.go`
**Commit:** 9f3cde98
**Applied fix:** `DueTasks` floors any `limit <= 0 || limit > math.MaxInt32` to 1 at the
store boundary (defensive parity with `envInt`/`int4OrNull`), so a 0 (→ LIMIT 0, no
dispatch) or a >2^31 (→ negative LIMIT, Postgres error) input can no longer silently
misbehave. Regression `TestDueTasks_ClampsBadLimit`. Verified live `db_integration`.

### WR-03: Non-atomic pending_approval gate (INSERT active, then UPDATE status)

**Files modified:** `internal/cron/store.go`, `internal/cron/store_test.go`, `cmd/aura/serve_adapters.go`
**Commit:** 93f1a9b1
**Applied fix:** `cron.Store.CreateTask` now binds the initial status
(`CreateTaskParams.Status`, default `"active"`; the sqlc query already takes status as a
parameter). The live `task` tool adapter (`cronTaskStore.CreateScheduledTask`) passes the
computed status into the SINGLE INSERT and drops the follow-up `UPDATE ... SET status`,
so a destructive task scoring routed to `pending_approval` is never momentarily `active`
and claimable — a crash between two statements can no longer leave the gate open
(T-10-12 / D-27). Matches the CLI's one-statement insert. Regression
`TestCreateTask_GatedStatusIsAtomic` asserts the gated row persists in one INSERT and is
not selectable by `DueTasks`. Verified live `db_integration`.

### WR-04: Backup pg_dump -f writes inside the container, not the host

**Files modified:** `internal/cron/handlers/backup.go`
**Commit:** 1e3a980d
**Applied fix:** `BackupHandler.Run` now `os.Stat`s the dump destination on the host
after the `docker exec` and returns a terminal error naming the required bind-mount when
the artifact is absent — instead of reporting a misleading "backup ok → <hostPath>" for a
file that landed only in the container's ephemeral FS. The bind-mount precondition (host
`AURA_BACKUP_DIR` mounted into the postgres/neo4j containers at the same path) is
documented on `BackupHandler`. The fixed-argv injection posture (T-10-15/T-10-16) is
unchanged. The real dump path is exercised by the Manual-Only `TestBackupDockerExecLive`
(gated on `AURA_BACKUP_LIVE`), whose host-artifact assertion now aligns with the new stat
check; unit tier green.

### WR-05: recoverOrphans doc comment describes a nonexistent bool contract

**Files modified:** `internal/cron/recover.go`
**Commit:** 697eb37c
**Applied fix:** Reworded the `recoverOrphans` doc comment to describe its actual
always-nil, non-fatal error-only contract (and why the `error` return is retained for a
future hard-fail policy), removing the phantom "the bool reports whether every mark
succeeded" claim. No behavior change.

## Verification Notes

- Unit tiers (tools, cmd/aura, cron handlers, cron) all green.
- Live `db_integration` (Postgres on 127.0.0.1:5432, DSNs derived from `.env`
  `POSTGRES_PASSWORD`): full `internal/cron` suite green, including the new
  `TestStartDispatchesMissedAtBoot`, `TestCreateTask_GatedStatusIsAtomic`,
  `TestDueTasks_ClampsBadLimit`.
- `-race` of the full `internal/cron` `db_integration` suite green in WSL (the new
  `runMissed` heartbeat-goroutine join path is race-clean and goleak-clean via the
  package `TestMain` gate).
- The two parallel-session docs (`docs/aura-cot-eval-2026-05-30.md`,
  `docs/aura-swarm-eval-2026-06-04.md`) and `cover_gate.out.testlog` were NOT touched or
  staged; every commit used an explicit `git add <file>` list.

---

_Fixed: 2026-06-04_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
