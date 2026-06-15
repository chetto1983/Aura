---
phase: 10-scheduler
reviewed: 2026-06-04T00:00:00Z
depth: standard
files_reviewed: 56
files_reviewed_list:
  - .github/workflows/ci.yml
  - cmd/aura/chat.go
  - cmd/aura/main.go
  - cmd/aura/serve.go
  - cmd/aura/serve_adapters.go
  - cmd/aura/task.go
  - cmd/aura/task_test.go
  - deploy/aura-scheduler.service
  - internal/agent/tools/action.go
  - internal/agent/tools/action_test.go
  - internal/agent/tools/registry.go
  - internal/agent/tools/registry_test.go
  - internal/agent/tools/task.go
  - internal/agent/tools/task_test.go
  - internal/cron/claim.go
  - internal/cron/claim_test.go
  - internal/cron/dispatch.go
  - internal/cron/dispatch_integration_test.go
  - internal/cron/dispatch_test.go
  - internal/cron/e2e_test.go
  - internal/cron/handlers/agentjob.go
  - internal/cron/handlers/agentjob_test.go
  - internal/cron/handlers/backup.go
  - internal/cron/handlers/backup_test.go
  - internal/cron/handlers/handler.go
  - internal/cron/handlers/main_test.go
  - internal/cron/handlers/reminder.go
  - internal/cron/handlers/reminder_test.go
  - internal/cron/heartbeat.go
  - internal/cron/heartbeat_test.go
  - internal/cron/main_test.go
  - internal/cron/notify.go
  - internal/cron/notify_test.go
  - internal/cron/recover.go
  - internal/cron/recover_test.go
  - internal/cron/schedule.go
  - internal/cron/scheduler.go
  - internal/cron/scheduler_integration_test.go
  - internal/cron/scheduler_test.go
  - internal/cron/store.go
  - internal/cron/store_runs.go
  - internal/cron/store_test.go
  - internal/cron/schedule_test.go
  - internal/cron/tzdata.go
  - internal/db/migrations/0009_scheduler.up.sql
  - internal/db/migrations/0009_scheduler.down.sql
  - internal/db/queries/agent_job_runs.sql
  - internal/db/queries/scheduler_tasks.sql
  - internal/db/sqlc/agent_job_runs.sql.go
  - internal/db/sqlc/models.go
  - internal/db/sqlc/querier.go
  - internal/db/sqlc/scheduler_tasks.sql.go
  - internal/swarm/brief_registry_test.go
  - internal/swarm/runner_adapter_test.go
  - internal/swarm/swarm.go
  - scripts/scheduler_chaos.sh
findings:
  critical: 2
  warning: 5
  info: 3
  total: 10
status: issues_found
---

# Phase 10: Code Review Report

**Reviewed:** 2026-06-04
**Depth:** standard
**Files Reviewed:** 56
**Status:** issues_found

## Summary

Phase 10 (scheduler) is a well-structured HA cron + agent_job queue. The held-conn
advisory-lock claim lifecycle (`claim.go`), heartbeat goroutine join (`heartbeat.go`),
idempotent completion (SC#2), parameterized SQL, fixed-argv backup (no injection
surface), and the OpenAI-wire-safe `task` schema are all correctly implemented and
well-tested. The D-24 import boundary holds: `internal/cron` does not import
`internal/swarm` or `internal/agent/tools`, and the cycle is broken via
consumer-declared interfaces + composition-root adapters.

Two BLOCKERS stand out, both **silent task-drop** defects that the test suite does not
catch:

1. **Boot catch-up fires are computed then discarded** — `Scheduler.Start` calls
   `catchUpMissed` and throws away the returned `[]MissedTask`. The catch-up advances
   `next_run_at` to a *future* instant, so the missed window is never dispatched. A
   missed reminder / backup / agent_job at boot is silently lost (breaks D-18). The
   `MissedTask.MissedSince`, `MissedBackupAlert` (SC#3), and the dispatcher's `notify`
   path for catch-up runs are all dead from the live boot path.

2. **An `at` task scheduled in the past is accepted but never fires** — both the LLM
   `task` tool and the CLI compute `NextRunAt(spec, time.Now())`, which returns a zero
   time for an already-past `at`. That zero is persisted as `next_run_at = NULL`, which
   never satisfies the `DueTasks` predicate. The tool/CLI report "scheduled" with no
   error: the user believes a one-shot was armed when it will never run.

The remaining findings are robustness and consistency issues.

## Critical Issues

### CR-01: Boot catch-up fires are computed and silently discarded (missed tasks never run)

**File:** `internal/cron/scheduler.go:115-119`, `internal/cron/recover.go:53-80`
**Issue:**
`Scheduler.Start` invokes `catchUpMissed` only for its error value and discards the
`[]MissedTask` slice it returns:

```go
func (s *Scheduler) Start(ctx context.Context) error {
	_ = s.recoverOrphans(ctx)
	if _, err := s.catchUpMissed(ctx); err != nil {   // <-- []MissedTask dropped
		slog.Warn("scheduler boot catch-up failed", "err", err)
	}
	...
}
```

`catchUpMissed` (recover.go:53) advances each overdue task's `next_run_at` to the next
**future** fire (`NextRunAt(spec, now)` is strictly after `now`) and returns the
collapsed `MissedTask`s the doc comment says "feed the dispatcher (10-05), which fires
each once." But nothing dispatches them. Because `next_run_at` is now in the future,
the very next `tick` will *not* re-select these tasks via `DueTasks` (`next_run_at <=
now()` is false). Net effect: a reminder/backup/agent_job whose window was missed while
the daemon was down is **silently dropped** at boot — the exact opposite of D-18's
"collapse N missed windows into ONE catch-up fire."

Corollaries that are also dead code as a result:
- `MissedTask.MissedSince` is never delivered to a notification.
- `handlers.MissedBackupAlert` (the SC#3 "backup still missed past 24h" alert) is never
  called from any live path — only from its unit test.
- `agent_job_runs.missed_since` is only ever written by `MarkUnknownRecovery`, never by
  a catch-up fire.

The integration test `TestCatchUpMissed_CollapsesMultipleWindowsToOne` asserts only
that `next_run_at` advanced and the slice is returned — it never asserts the missed task
is dispatched, so the gap is invisible to CI.

**Fix:** Dispatch the collapsed missed fires at boot (the held-conn claim lifecycle,
same as a tick). Sketch:

```go
func (s *Scheduler) Start(ctx context.Context) error {
	_ = s.recoverOrphans(ctx)
	missed, err := s.catchUpMissed(ctx)
	if err != nil {
		slog.Warn("scheduler boot catch-up failed", "err", err)
	}
	for _, m := range missed {
		s.runMissed(ctx, m) // claim + heartbeat + dispatch, carrying MissedSince
	}
	// ... tick loop
}
```

`runMissed` should mirror `runOne` but (a) NOT re-`reschedule` (catchUpMissed already
advanced `next_run_at`), and (b) thread `MissedSince` so the backup handler can fire
`MissedBackupAlert`. Add an integration test that asserts a dispatched run row + a
notification for an overdue task at `Start`, not just the `next_run_at` advance.

---

### CR-02: An `at` task with a past instant is persisted with NULL next_run_at and never fires (silent drop)

**File:** `internal/agent/tools/task.go:255-280` (`resolveSchedule`),
`cmd/aura/task.go:101-135` (`taskSchedule`), `internal/cron/schedule.go:100-104`
**Issue:**
`ParseSchedule` accepts any non-zero `runAt` for an `at` task — it does NOT require the
instant to be in the future (schedule.go:72-76 only checks `IsZero()`). Both schedule
paths then compute the first fire as:

```go
next, err := cron.NextRunAt(spec, time.Now())
```

For an `at` whose `RunAt` is already in the past, `NextRunAt` returns `time.Time{}`
(schedule.go:101-103, proven by `TestNextRunAt/"at already fired returns zero"`). That
zero flows into `tsOrNull`/`nullableTime`, persisting `next_run_at = NULL`. `DueTasks`
filters on `next_run_at <= now()`, which NULL never satisfies — so the task is created
`active` but is **unschedulable and will never fire**.

Worse, the user is told it succeeded: the tool returns `"scheduled task <id> ..."` and
the `!next.IsZero()` branch merely *omits* the "Next run at" line (task.go:221-223;
task.go CLI:133-135). No error, no warning — a one-shot the operator believes is armed
silently does nothing.

This is reachable directly from the LLM (`action=schedule, schedule_kind=at, at=<past
RFC3339>`) and from `aura task schedule --at <past>`.

**Fix:** Reject a past `at` at parse/resolve time with a clear error rather than
persisting an unschedulable row. Either in `ParseSchedule`:

```go
case KindAt:
	if runAt.IsZero() {
		return ScheduleSpec{}, ErrMissingRunAt
	}
	if !runAt.After(time.Now()) {
		return ScheduleSpec{}, fmt.Errorf("%w: run_at %s is in the past", ErrPastRunAt, runAt)
	}
	spec.RunAt = runAt.UTC()
```

or guard the `next.IsZero()` case in both schedulers and return an error instead of a
success message. Add a test asserting a past `at` is rejected (not silently stored).

---

## Warnings

### WR-01: `RunScheduledTaskNow` adapter and CLI `run_now` ignore NULL next_run_at, so run_now cannot rescue an unschedulable task

**File:** `cmd/aura/serve_adapters.go:193-205`, `cmd/aura/task.go:219-237`
**Issue:** `run_now` sets `next_run_at = now()` only `WHERE status = 'active'`. That is
correct, and it *would* in fact be the only escape hatch for a CR-02 task stuck with a
NULL `next_run_at`. However, neither the tool nor the CLI surfaces that a task is
unschedulable, so an operator has no signal to invoke `run_now`. This is a
maintainability/observability gap that compounds CR-02. Once CR-02 is fixed (past `at`
rejected), this becomes lower priority, but `aura task doctor` and `list` should still
flag `active` rows with a NULL `next_run_at` as unschedulable.

**Fix:** In `taskDoctor` and `renderTaskList`/`taskList`, flag `status='active' AND
next_run_at IS NULL` as `[unschedulable]` so the condition is observable.

---

### WR-02: `DueTasks` casts an unvalidated `int` limit to `int32` — a negative or huge cap silently misbehaves

**File:** `internal/cron/store_runs.go:58-59`, `internal/cron/scheduler.go:142-143`
**Issue:** `DueTasks(ctx, limit int)` does `int32(limit)` with no bounds check. The
caller passes `s.maxConcurrent`, which `NewScheduler` derives from
`AURA_SCHEDULER_MAX_CONCURRENT_RUNS` via `envInt` (which rejects `<= 0`, good) — so the
live path is safe today. But `DueTasks` is an exported store method with no contract
guarding its input; a future caller (or a test) passing `0` yields `LIMIT 0` (no tasks
ever dispatched) and a value `> 2^31` wraps to a negative `LIMIT` (a Postgres error).
The defensive posture elsewhere in this package (`envInt` clamps, `int4OrNull` clamps)
is not applied here.

**Fix:** Clamp at the store boundary:

```go
func (s *Store) DueTasks(ctx context.Context, limit int) ([]Task, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := s.q.DueTasks(ctx, int32(limit))
	...
}
```

---

### WR-03: `CreateScheduledTask` gate is a two-statement non-atomic create — a crash between INSERT and the status UPDATE leaves a destructive task `active`

**File:** `cmd/aura/serve_adapters.go:116-152`
**Issue:** The live `task` tool persists a `pending_approval` task as `CreateTask`
(which always INSERTs `status='active'`, store.go:117) followed by a separate
`UPDATE ... SET status = $2`:

```go
created, err := s.store.CreateTask(ctx, ...)   // INSERTs status='active'
...
if status != "active" {
	s.pool.Exec(ctx, `UPDATE ... SET status = $2 ...`, created.ID, status)
}
```

If the process dies (or the UPDATE fails) between the INSERT and the gate UPDATE, a
destructive task that scoring routed to `pending_approval` is left `active` with a
populated `next_run_at` — and the next tick will **claim and fire it without the
required approval** (violates T-10-12 / D-27, the destructive-gate invariant). The two
statements are not wrapped in a transaction. The comment claims "the gate is set before
the first tick can claim it," but that is only true if no fault occurs in the window.

Note: the CLI path (`cmd/aura/task.go:117-128`) does NOT have this bug — it INSERTs the
final status directly in one statement. The adapter should match.

**Fix:** Either add a `CreateTaskWithStatus` store method that INSERTs the final status
in one statement (mirroring the CLI), or wrap the INSERT+UPDATE in `db.WithTx`. The
`CreateRunAndAdvance` method (store.go:204) already establishes the `db.WithTx` pattern
in this package.

---

### WR-04: Backup `pg_dump -f <hostPath>` writes inside the container, not to AURA_BACKUP_DIR on the host

**File:** `internal/cron/handlers/backup.go:81-96, 119-124`
**Issue:** `Run` computes `dest` as a **host** path under `AURA_BACKUP_DIR`
(`~/.aura/backups/...`), `MkdirAll`s it on the host, then passes that same path as the
in-container `-f` target to `docker exec ... pg_dump -f <dest>`. `pg_dump -f` writes to
the path **inside the postgres container's filesystem**, not the host. Unless the
operator has bind-mounted `~/.aura/backups` into the container at the identical path,
the dump lands in the container's ephemeral FS and is lost on container restart, while
`sweepRetention` then scans an empty host dir (pruning nothing) and the summary reports
`backup ok → <hostPath>` for a file that does not exist on the host.

The test file itself documents this caveat (`backup_test.go:164-173`), and the live test
is Manual-Only, so CI never exercises the real path. The summary string is misleading
(claims success + a path that may be empty on the host).

**Fix:** Document the required bind-mount as an operator precondition AND verify the
artifact exists on the host after the dump (stat `dest`; if absent, return a terminal
error rather than a misleading "ok" summary). Alternatively, dump to a container-local
path and `docker cp` to the host `dest`.

---

### WR-05: `recoverOrphans` swallows every error and always returns nil — the bool contract in its doc comment does not exist

**File:** `internal/cron/recover.go:24-36`
**Issue:** The doc comment says "the bool reports whether every mark succeeded, for
callers/tests that want the signal," but the function signature is `func (s *Scheduler)
recoverOrphans(ctx) error` — there is no bool, and it unconditionally returns `nil`.
`Start` calls it as `_ = s.recoverOrphans(ctx)`. So a partial recovery failure (some
runs not marked `unknown_recovery`) is invisible to the boot path and to tests — the
documented signal is a phantom. This is a stale-comment / dead-contract defect: a future
maintainer reading the comment will look for a bool that isn't there.

**Fix:** Either return the `(allMarked bool, err error)` the comment describes (and have
`Start`/tests consult it), or update the comment to match the `error`-only signature
that always returns nil.

---

## Info

### IN-01: Heartbeat best-effort UPDATE discards its error even on a dead conn, contradicting its own comment

**File:** `internal/cron/heartbeat.go:35-38`
**Issue:** The comment says "A persistent failure (dead conn) ends the run elsewhere;
here we keep ticking" — but the goroutine `_`-discards every `conn.Exec` error and has
no path that "ends the run elsewhere." If the held conn dies, the heartbeat silently
loops doing nothing until `stop()`; the run is only reaped by the >90s orphan scan at
the *next boot*, never mid-flight. This is acceptable for v1 (the orphan scan is the
backstop) but the comment overstates the behavior.

**Fix:** Trim the comment to reflect that the orphan scan is the only reaper, or log at
debug on repeated heartbeat failures so a dead held conn is observable.

### IN-02: `assistantAskUserTurn` ignores the json.Marshal error

**File:** `internal/cron/handlers/agentjob.go:152-160`
**Issue:** `args, _ := json.Marshal(...)` drops the error. Marshaling a
`map[string]any` of strings cannot fail in practice, so this is benign, but the `_`
discard is inconsistent with the package's otherwise-strict error handling. Same pattern
in `notify.go:148,155` (`buildSend`) — also benign (string maps).

**Fix:** None required for correctness; consider a `//nolint` or a comment noting the
marshal is infallible for documentation parity.

### IN-03: `taskHash` FNV-1a collision posture is documented as benign but the loser path is untested

**File:** `internal/cron/claim.go:16-26`
**Issue:** The comment correctly argues a 64-bit FNV-1a collision only causes a benign
"singleton-skip+reschedule" rather than a correctness break. That reasoning is sound
(two distinct tasks colliding would make one skip a window, then reschedule). There is
no test that injects a forced advisory-key collision to prove the loser path is the
skip+reschedule and not a stuck task, but the risk is genuinely negligible — noting for
completeness, not as an action item.

---

_Reviewed: 2026-06-04_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
