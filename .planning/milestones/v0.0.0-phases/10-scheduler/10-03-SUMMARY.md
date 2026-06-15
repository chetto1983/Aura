---
phase: 10-scheduler
plan: 03
subsystem: scheduler
tags: [scheduler, cron, advisory-lock, held-conn, heartbeat, orphan-scan, catch-up, tick-loop, goleak, db_integration]

# Dependency graph
requires:
  - phase: 10-scheduler
    plan: 02
    provides: internal/cron.Store (Store{pool,q}) + Task/Run domain types + sentinels + sqlc surface (DueTasks SKIP-LOCKED, InsertRun, UpdateHeartbeat, CompleteRun 23505-swallow, ScanStaleRuns, MarkUnknownRecovery) + NextRunAt DST-safe engine + goleak TestMain
provides:
  - internal/cron held-conn advisory-lock claim (pool.Acquire + pg_try_advisory_lock(task_hash) singleton, D-03/D-04)
  - FNV-1a 64 taskHash → single int64 advisory key (collision-tolerant, A1)
  - goleak-clean 30s heartbeat ticker on the held conn (defer ticker.Stop + ctx-cancel + joinable stop, Pitfall 4)
  - boot orphan scan (stale heartbeat >90s → unknown_recovery, WARN-only non-blocker) + missed catch-up-once (collapse N windows → one fire with MissedSince, D-18)
  - Scheduler tick loop: injectable Now (W8) + Start(ctx) graceful shutdown + DueTasks(LIMIT=cap) + max-concurrent semaphore + D-04 skip+reschedule + Dispatcher seam for 10-05
  - DuringQuietHours Now-based predicate (D-23, fail-open) for the 10-05 Notifier
  - Store wrappers: insertRunOnConn (held-conn), GetRun, DueTasks, ScanStaleRuns, MarkUnknownRecovery
affects: [10-05, 10-06]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Held-conn session advisory lock (D-03): pool.Acquire a dedicated conn, pg_try_advisory_lock on it, hold for the run's lifetime, unlock on the SAME conn at release — the 40-year Postgres HA pattern; the conn auto-releases the lock at session end (chaos failover by construction)"
    - "FNV-1a 64 over the task UUID → one int64 advisory key (stdlib hash/fnv, zero-dep); collision is a benign singleton-skip+reschedule, never a correctness break (A1/D-A)"
    - "goleak-clean ticker: a joinable stop() that cancels then <-done blocks until the goroutine exits, so a caller's defer stop() leaves zero leaked goroutines (Pitfall 4)"
    - "Injectable Now func() time.Time clock (W8 / budget.go precedent), NOT synctest — deterministic tick tests with no background goroutines that would trip goleak"
    - "Missed catch-up collapse (D-18): NextRunAt(spec, now) skips every intervening missed window so an overdue recurring task fires ONCE then resumes cadence; original next_run_at becomes MissedSince"

key-files:
  created:
    - internal/cron/claim.go
    - internal/cron/heartbeat.go
    - internal/cron/recover.go
    - internal/cron/store_runs.go
    - internal/cron/claim_test.go
    - internal/cron/heartbeat_test.go
    - internal/cron/recover_test.go
    - internal/cron/scheduler_test.go
    - internal/cron/scheduler_integration_test.go
  modified:
    - internal/cron/scheduler.go

key-decisions:
  - "scheduler.go landed in BOTH tasks: Task 1 created the Scheduler struct + NewScheduler (claim.go depends on the type + s.store/s.pool fields); Task 2 added the tick loop / Start / DuringQuietHours methods. The struct could not live only in Task 2 without making claim.go uncompilable in its own atomic commit."
  - "store_runs.go split out of store.go (NO GOD CLASS): the held-conn run writers (insertRunOnConn) + recovery scan wrappers (GetRun, DueTasks, ScanStaleRuns, MarkUnknownRecovery) live here so store.go stays 378 LOC."
  - "catchUpMissed returns []MissedTask (the overdue set + MissedSince) and advances next_run_at forward to collapse windows — the ACTUAL one-shot dispatch is the 10-05 Dispatcher seam. This keeps the recovery half fully db_integration-testable at this tier without 10-05 handlers and without a new migration (missed_since column already shipped in 0009)."
  - "Survivor-reacquire (chaos failover) proven at the unit level via conn.Hijack() + hw.Close(ctx): hard-closing the held conn ends the Postgres session, auto-releasing the advisory lock, so a survivor's pg_try_advisory_lock succeeds. This is the SC#2 failover semantics without a full chaos harness (that lands in 10-06)."
  - "DuringQuietHours is fail-OPEN (unset/malformed window = never quiet) so a misconfigured AURA_SCHEDULER_QUIET_HOURS never silently swallows notifications; the predicate only DEFERS non-destructive notifications in 10-05, never gates firing here (D-23)."
  - "Env knobs read directly via os.Getenv in NewScheduler (envInt helper) with config>env>default precedence; no config.Load struct touched (that wiring lands in 10-05's composition root)."

patterns-established:
  - "Held-conn lifecycle: claim (Acquire+lock+InsertRun) → heartbeat on the held conn → release (unlock+Release) on every exit path. The unlock ALWAYS runs on c.conn (never a fresh acquire) so it can never land on the wrong conn (Pitfall 1)."

requirements-completed: [CAP-06]

# Metrics
duration: 38min
completed: 2026-06-04
---

# Phase 10 Plan 03: Scheduler HA Core (6a part 2) Summary

**Built the HA core of the scheduler (D-02 full ROADMAP stack): the FOR UPDATE SKIP LOCKED claim wired to a held-conn session advisory lock (`pg_try_advisory_lock(task_hash)` on a `pool.Acquire`d conn held for the run's lifetime, D-03), the goleak-clean 30s heartbeat ticker on that same conn, the boot orphan scan (stale >90s → `unknown_recovery`) + missed-catch-up-once (collapse N windows → one fire with `MissedSince`, D-18), and the injectable-clock tick loop with a max-concurrent semaphore sized below pool MaxConns. Delivers SC#1 (singleton, no double-exec) + the SC#2 idempotency/failover half at the unit + db_integration tier, proven live against Postgres.**

## Performance

- **Duration:** ~38 min
- **Completed:** 2026-06-04
- **Tasks:** 2/2
- **Files:** 9 created, 1 modified

## Accomplishments

- **Task 1 — held-conn claim + heartbeat (`840e8624`):**
  - `claim.go`: `taskHash` (FNV-1a 64 over the task UUID → one int64 advisory key, collision posture documented per A1/D-A); `(s *Scheduler) claim` does `pool.Acquire` → `SELECT pg_try_advisory_lock($1)` on the held conn → on `!locked` Release + `ErrAlreadyRunning` (D-04), on win `insertRunOnConn` (run row opened on the SAME conn) and returns a `Claim{conn, RunID, hash}`; `(c *Claim) release` unlocks on `c.conn` then Releases (Pitfall 1 — unlock can never land on the wrong conn), idempotent-safe. `Conn()` exposes the held conn for the dispatcher (10-05).
  - `heartbeat.go`: `startHeartbeat` runs a `time.NewTicker` goroutine that `UPDATE ... last_heartbeat_at = now()` on the held conn every interval (no second pool slot, Pitfall 2); `defer ticker.Stop()` + ctx-cancel + a joinable `stop()` that cancels then `<-done` so a caller's `defer stop()` leaves zero leaked goroutines (Pitfall 4).
  - `store_runs.go`: `insertRunOnConn` (binds sqlc to the held conn), `GetRun`, `ScanStaleRuns`, `MarkUnknownRecovery` wrappers — split out of store.go (NO GOD CLASS).
  - `scheduler.go` (base): `Scheduler` struct + `NewScheduler` (injectable `Now` W8, `maxConcurrent` from `AURA_SCHEDULER_MAX_CONCURRENT_RUNS` default 4 < pool 10, `tickInterval` from `AURA_SCHEDULER_TICK_SECONDS`, `Dispatcher` seam).
  - Tests (db_integration): two-worker singleton (one lock, loser `ErrAlreadyRunning` + Released), survivor re-acquire after `conn.Hijack()`+hard-close (session-end auto-release = chaos failover), idempotent completion (23505-swallow), heartbeat interval-advance + ctx-cancel stop.
- **Task 2 — recovery + tick loop (`21854711`):**
  - `recover.go`: `recoverOrphans` (`ScanStaleRuns(90s)` → `MarkUnknownRecovery` each; WARN-only, never blocks boot — mirrors `conversations.ScanOrphans`); `catchUpMissed` (for each active overdue task, `NextRunAt(spec, now)` collapses the missed windows to one future fire, advances `next_run_at`, returns `MissedTask{Task, MissedSince=original}` for the 10-05 dispatcher); `specFromTask` rebuilds the spec for in-zone recompute.
  - `scheduler.go` (tick loop): `Start(ctx)` runs recover+catch-up at boot then a ticker loop until ctx-cancel (graceful — finishes the in-flight tick, joins runs, returns nil); `tick` selects `DueTasks(LIMIT=cap)` and claims each under a `maxConcurrent` semaphore; `runOne` heartbeats on the held conn + dispatches (Dispatcher seam) then releases; `ErrAlreadyRunning` → `slog.Info("skipped: previous run in progress")` + `reschedule` (D-04); `DuringQuietHours` Now-based wrap-around-window predicate (D-23, fail-open).
  - `store_runs.go`: `DueTasks` wrapper (FOR UPDATE SKIP LOCKED batch).
  - Tests: `recover_test.go` (db_integration: stale→unknown_recovery vs fresh-untouched, 12-window collapse-to-one with MissedSince, future-task skip), `scheduler_integration_test.go` (db_integration: tick due-dispatch, in-flight skip+reschedule SC#1, graceful Start shutdown, max-concurrent bound), `scheduler_test.go` (unit: config/env resolution, injectable clock, 8-case quiet-hours window matrix + fail-open).

## Task Commits

1. **Task 1: held-conn advisory-lock claim + goleak-clean heartbeat ticker** — `840e8624` (feat)
2. **Task 2: boot orphan scan + missed catch-up + injectable-clock tick loop** — `21854711` (feat)

## Verification

- `go vet ./internal/cron/` + `go vet -tags db_integration ./internal/cron/` → clean
- `go build ./...` + `go vet ./...` → clean (whole module)
- `go test -count=1 ./internal/cron/` → ok (unit, Git Bash)
- `go test -race -count=1 ./internal/cron/` → ok (WSL native race)
- **`go test -tags db_integration -count=1 ./internal/cron/` → ok 2.398s LIVE** against Postgres on 127.0.0.1:5432 (DSNs derived from `.env` POSTGRES_PASSWORD: aura_app / aura_migrate). Real execution — not a sub-second skip.
- **`go test -tags db_integration -race -count=1 ./internal/cron/` → ok 1.033s** (WSL native race) — the tick concurrency + heartbeat goroutines all race-clean AND goleak-clean.
- Verbose live run: all 12 new tests PASS (`TestClaimSkipLocked_Singleton`, `TestClaimSurvivorReacquiresAfterConnClose`, `TestClaimIdempotentCompletion`, `TestHeartbeatTickerUpdatesHeldConn`, `TestHeartbeatStopsOnCtxCancel`, `TestRecoverOrphans_MarksStaleLeavesFresh`, `TestCatchUpMissed_CollapsesMultipleWindowsToOne`, `TestCatchUpMissed_SkipsFutureTasks`, `TestSchedulerTickDispatchesDueTask`, `TestSchedulerTickSkipsInFlightAndReschedules`, `TestSchedulerStartGracefulShutdown`, `TestSchedulerTickBoundedByMaxConcurrent`) + the 4 unit `TestNewScheduler*`/`TestEnvInt`/`TestDuringQuietHours*`.
- `golangci-lint run ./internal/cron/...` → **0 issues**; `golangci-lint run --build-tags db_integration ./internal/cron/...` → **0 issues**.
- Acceptance greps: `pg_try_advisory_lock` in claim.go ✓; `ticker.Stop` in heartbeat.go ✓; `unknown_recovery` in recover.go ✓; `Now func() time.Time` in scheduler.go ✓; `pool.Acquire` in claim.go ✓; `last_heartbeat_at` UPDATE in heartbeat.go ✓.
- File sizes (all ≤600 LOC): scheduler.go 260, store.go 378, store_runs.go 106, recover.go 93, claim.go 88, heartbeat.go 47.

## Deviations from Plan

### Within plan latitude (scheduler.go spanning both tasks)

**1. Scheduler struct landed in Task 1, tick-loop methods in Task 2**
- The plan lists `scheduler.go` under Task 2's `<action>`, but `claim.go` (Task 1) is a method on `*Scheduler` and reads `s.store`/`s.pool`. To keep Task 1's commit atomic and compilable, the `Scheduler` struct + `NewScheduler` (the type + config) were created in Task 1; Task 2 added `Start`/`tick`/`runOne`/`reschedule`/`DuringQuietHours`. No content was duplicated; the split is additive (Task 2 only appends methods + imports to the same file).

### Naming/shape discretion (within the plan's stated latitude)

**2. catch-up modelled as a returning collector, not an inline firer**
- The plan's `catchUpMissed` "fires ONCE with MissedSince". The actual per-TaskKind dispatch is the 10-05 seam, so `catchUpMissed` returns `[]MissedTask{Task, MissedSince}` and advances `next_run_at` to collapse the windows; the single fire happens when 10-05's Dispatcher consumes the set. This keeps the recovery half fully db_integration-testable at this tier (collapse + MissedSince asserted live) without 10-05 handlers and without a new migration — the `missed_since` column already shipped in 0009.

## Threat Flags

None — the new surface is exactly the plan's `<threat_model>` register:
- **T-10-06** (double-exec): FOR UPDATE SKIP LOCKED (DueTasks) + `pg_try_advisory_lock(task_hash)` continuous ownership; loser skips (claim.go, SC#1) — proven by `TestClaimSkipLocked_Singleton` + `TestSchedulerTickSkipsInFlightAndReschedules`.
- **T-10-07** (lock leak / pool starvation): held-conn discipline (unlock on the held conn, Pitfall 1) + max-concurrent cap (4) < pool MaxConns (10) (Pitfall 2) — `TestNewScheduler_DefaultsAndClock` asserts the cap < pool invariant; `TestSchedulerTickBoundedByMaxConcurrent` asserts the batch bound.
- **T-10-08** (duplicate side-effects): `completed_with_hash` 23505-swallow (`TestClaimIdempotentCompletion`).
- **T-10-09** (silent stuck run): orphan scan → `unknown_recovery` + heartbeat liveness write (`TestRecoverOrphans_MarksStaleLeavesFresh`).
- **T-10-10** (leaked ticker): `defer ticker.Stop()` + ctx-cancel + joinable stop, goleak gate (`TestHeartbeatStopsOnCtxCancel`, whole-suite race+goleak green).
- **T-10-SC** (hash collision): FNV-1a 64, collision = benign singleton-skip (accepted, documented in claim.go).
No new endpoints / auth paths / trust boundaries beyond the documented ones.

## Known Stubs

None that block the plan's goal. The `Dispatcher` interface is an intentional seam: `runOne`/`tick` drive the full claim→heartbeat→release lifecycle, and a `nil` dispatcher (pre-10-05 wiring / tests) makes the tick a claim-and-release probe. The per-TaskKind handlers (reminder / agent_job / backup_postgres / backup_neo4j) and the Notifier that consumes `DuringQuietHours` + `MissedSince` land in 10-05 — this is documented future surface, not a stub that defeats SC#1/SC#2 (both are proven live here).

## Self-Check: PASSED

- FOUND: internal/cron/claim.go
- FOUND: internal/cron/heartbeat.go
- FOUND: internal/cron/recover.go
- FOUND: internal/cron/store_runs.go
- FOUND: internal/cron/scheduler.go (modified)
- FOUND: internal/cron/claim_test.go
- FOUND: internal/cron/heartbeat_test.go
- FOUND: internal/cron/recover_test.go
- FOUND: internal/cron/scheduler_test.go
- FOUND: internal/cron/scheduler_integration_test.go
- FOUND commit 840e8624 (Task 1)
- FOUND commit 21854711 (Task 2)
- No deletions in either commit; go.mod/go.sum/ROADMAP.md/STATE.md untouched; 10-04 files (tools/action*, task*, cmd/aura/task.go) untouched
