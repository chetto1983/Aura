---
phase: 10-scheduler
verified: 2026-06-04T20:00:00Z
status: human_needed
score: 4/4
overrides_applied: 0
human_verification:
  - test: "Confirm live `aura serve` fires a cron task exactly once per window across a multi-minute observation window"
    expected: "A `*/5 * * * *` task produces exactly one run row per 5-minute window; `next_run_at` advances correctly after each fire"
    why_human: "The SC#1 fix (advance-on-claim) was verified live during Gate-3 and recorded in 10-VALIDATION.md, but the verifier cannot re-execute a live daemon run. The evidence is operator-recorded (2/2 windows, 17:45+17:50), not an automated CI assertion — static grep alone cannot confirm runtime behavior."
  - test: "Confirm `scripts/scheduler_chaos.sh` passes in the operator's environment (3 workers, 60s partition, survivor pickup, no duplicate)"
    expected: "Exit 0; stdout: `completed runs = 1, distinct completion hashes = 1`"
    why_human: "SC#2 is the gating operator-run (D-05). The script exists and is syntactically correct (`bash -n` clean); the CI advisory step runs it non-blocking. The Gate-3 VALIDATION.md records exit 0 / completed=1 / distinct=1 on 2026-06-04, but re-running it requires the live Docker stack."
  - test: "Confirm nightly backup tasks produce dump artifacts in `$AURA_BACKUP_DIR` when the bind-mount precondition is met"
    expected: "`pg_restore --list` shows TABLE DATA entries for aura.scheduler_tasks + aura.agent_job_runs; artifact file exists on the host side of the mount"
    why_human: "SC#3 requires the operator bind-mount (`AURA_BACKUP_DIR` mounted into aura-postgres at the same path). The code has the `os.Stat` guard (WR-04 fix verified). The 29069-byte valid archive was proven in Gate-3, but bind-mount wiring in compose.yaml is a documented operator obligation and cannot be verified by grep."
---

# Phase 10: Scheduler Verification Report

**Phase Goal:** Cron + persistent `agent_job` queue on Postgres. `FOR UPDATE SKIP LOCKED` initial pickup + `pg_try_advisory_lock(task_hash)` continuous ownership + heartbeat update to `aura.agent_job_runs.last_heartbeat_at` every 30s + boot-time orphan scan (stale heartbeat > 90s marked `unknown_recovery`). Backup TaskKind handlers (`backup_postgres`, `backup_neo4j`) cron-scheduled. Scheduler-spawned jobs inherit step + wall-clock budget from `agent_job_runs` row.
**Verified:** 2026-06-04T20:00:00Z
**Status:** human_needed
**Re-verification:** No — initial verification

---

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | `FOR UPDATE SKIP LOCKED` initial pickup implemented | VERIFIED | `internal/cron/store_runs.go:75` DueTasks query documented as `FOR UPDATE SKIP LOCKED`; confirmed in sqlc query at `internal/db/queries/scheduler_tasks.sql` (via VALIDATION.md SC#1 live DB query) |
| 2 | `pg_try_advisory_lock(task_hash)` continuous ownership implemented | VERIFIED | `internal/cron/claim.go:62` — `conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", key)` on the held conn; session-scoped lock held for run lifetime (Pitfall 1 mitigated: unlock on same conn before Release) |
| 3 | Heartbeat update to `agent_job_runs.last_heartbeat_at` every 30s | VERIFIED | `internal/cron/heartbeat.go:38` — `UPDATE aura.agent_job_runs SET last_heartbeat_at = now()` on a 30s ticker on the held conn; `staleRecoverySeconds=90` constant in scheduler.go:34 |
| 4 | Boot-time orphan scan marks stale-heartbeat runs as `unknown_recovery` | VERIFIED | `internal/cron/recover.go:26-37` — `recoverOrphans` calls `ScanStaleRuns(ctx, 90)` + `MarkUnknownRecovery`; `store_runs.go:102-129` implements both queries |
| 5 | Boot catch-up DISPATCHES missed fires (CR-01 fix) | VERIFIED | `internal/cron/scheduler.go:115-128` — `missed, err := s.catchUpMissed(ctx)` return captured; `for _, m := range missed { s.runMissed(ctx, m) }` dispatches each once; `runMissed` method at lines 209-242 mirrors `runOne` with no reschedule + MissedSince thread |
| 6 | Past `at` schedules rejected with clear error (CR-02 fix) | VERIFIED | `internal/cron/schedule.go:134-143` — `FirstFire` sentinel returns `ErrPastRunAt` for zero-time at-past; `internal/agent/tools/task.go:269` calls `cron.FirstFire(spec, time.Now())`; `cmd/aura/task.go:101` calls `cron.FirstFire(spec, time.Now())`; test at `schedule_test.go:163` asserts rejection |
| 7 | Backup TaskKind handlers cron-scheduled (`backup_postgres`, `backup_neo4j`) | VERIFIED | `internal/cron/handlers/backup.go` — `BackupHandler` struct with `BackupPostgres`/`BackupNeo4j` variants; `cmd/aura/serve.go:106-111` wires both into `buildDispatch` handler map |
| 8 | Scheduler-spawned jobs inherit step + wall-clock budget from `agent_job_runs` | VERIFIED | `internal/cron/handlers/agentjob.go:62-63` — `Run(ctx, job Job)` calls `newJobBudget(job.StepBudget)` from the `Job.StepBudget` field derived from `agent_job_runs.step_budget`; VALIDATION.md SC#4 records `TestAgentJobBudgetInherit` green |
| 9 | D-24 import boundary: `internal/cron` does not import `internal/swarm` or `internal/agent/tools` | VERIFIED | Grep of all `internal/cron/*.go` files confirms no `internal/swarm` or `internal/agent/tools` import in the non-test cron package; `dispatch.go:13` explicitly documents the break-cycle rationale; the handlers package in `internal/cron/handlers/` imports tools, but the composition root breaks the cycle |
| 10 | `aura serve` is a real long-lived daemon replacing the main.go TODO | VERIFIED | `cmd/aura/serve.go` exists with `runServe`/`bootServe`; `cmd/aura/main.go` line 73 shows `case "serve": runServe(os.Args[2:])`; the old `case "shell", "serve":` TODO is gone |
| 11 | `bootChatEnv` is error-returning (no `os.Exit` in shared boot — Pitfall 6) | VERIFIED | `cmd/aura/chat.go:121` — `func bootChatEnv(ctx context.Context) (*chatEnv, error)` is error-returning; `bootChat` wrapper at line 102 calls it and does `os.Exit` only at the callsite |
| 12 | Migration 0009 creates `aura.scheduler_tasks` + `aura.agent_job_runs` with correct schema | VERIFIED | `internal/db/migrations/0009_scheduler.up.sql` exists with both tables, `completed_with_hash TEXT UNIQUE` idempotency column, `last_heartbeat_at`, `missed_since`, `paused_state_token FK`, partial index on `next_run_at WHERE status = 'active'`, `FOR UPDATE SKIP LOCKED` candidate, role grants |
| 13 | `task` tool: one non-deferred tool with action enum, `required=["action"]` schema | VERIFIED | `internal/agent/tools/task.go:101-117` — `taskParamsSchema` has `"required": ["action"]` only, no root `oneOf`/`anyOf`/`enum` (only property-level `enum` on action/schedule_kind/kind/notify, which is wire-safe per D-10) |
| 14 | Chaos script exists with 3-worker / 60s-partition / idempotency check | VERIFIED | `scripts/scheduler_chaos.sh` — 3 worker loops, `docker network disconnect` at line 129, asserts `completed >= 1` AND `completed == distinct_hash` |
| 15 | CI runs scheduler `db_integration` tier (no-skip-as-green) | VERIFIED | `.github/workflows/ci.yml:162` — `go test -tags db_integration -race -count=1 ./internal/db/... ./internal/cron/...` in the integration-test job; chaos-advisory at lines 178-189 with `continue-on-error: true` |

**Score:** 15/15 observable truths verified in code

---

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/cron/scheduler.go` | Tick loop + runMissed dispatch | VERIFIED | 315 LOC; `runMissed` at lines 209-242; `Start` dispatches missed at lines 117-128 |
| `internal/cron/claim.go` | Held-conn advisory lock | VERIFIED | 95 LOC; `pg_try_advisory_lock` + held conn for run lifetime |
| `internal/cron/heartbeat.go` | 30s heartbeat liveness writer | VERIFIED | 48 LOC; goleak-clean stop() join |
| `internal/cron/recover.go` | Orphan scan + catch-up | VERIFIED | 96 LOC; `recoverOrphans` + `catchUpMissed` |
| `internal/cron/schedule.go` | at|every|cron grammar + FirstFire gate | VERIFIED | 144 LOC; `ErrPastRunAt`, `FirstFire` function |
| `internal/cron/store.go` + `store_runs.go` | Store wrapping sqlc + DueTasks clamping | VERIFIED | `DueTasks` clamps at `math.MaxInt32` floor 1 (WR-02 fix) |
| `internal/cron/dispatch.go` | Handler dispatch seam | VERIFIED | Consumer-declared `Handler` interface; no tools/swarm import |
| `internal/cron/handlers/agentjob.go` | LlmAgent spawn mirroring swarm.runChild | VERIFIED | Direct `agent.NewLlmAgent` construction; auto-reject loop bounded by `maxAutoRejects=8` |
| `internal/cron/handlers/backup.go` | Fixed-argv pg_dump/neo4j-admin | VERIFIED | `os.Stat(dest)` guard after docker exec (WR-04 fix); LookPath-gated docker |
| `internal/cron/handlers/reminder.go` | Reminder delivery via Notifier | VERIFIED | File exists |
| `internal/agent/tools/task.go` | ONE non-deferred task tool | VERIFIED | `Deferred: false`; action enum `schedule|list|cancel|run_now|approve`; `FirstFire` called in `resolveSchedule` |
| `cmd/aura/task.go` | Full at|every|cron CLI triad | VERIFIED | `--at`, `--every`, `--cron` flags all present; `FirstFire` called at line 101; `[unschedulable]` flag for WR-01 |
| `cmd/aura/serve.go` | Long-lived daemon + graceful shutdown | VERIFIED | `signal.NotifyContext`; `scheduler.Start(ctx)`; `defer env.close()` reverse-closes MCP |
| `cmd/aura/serve_adapters.go` | cronTaskStore adapter + atomic CreateScheduledTask | VERIFIED | WR-03 fix: single-INSERT with status parameter (confirmed via REVIEW-FIX.md commit 93f1a9b1) |
| `internal/db/migrations/0009_scheduler.up.sql` | Schema with idempotency + heartbeat + grants | VERIFIED | Both tables, UNIQUE `completed_with_hash`, partial index, role grants |
| `scripts/scheduler_chaos.sh` | 3-worker / 60s-partition SC#2 | VERIFIED | `docker network disconnect` + dual assertion (completed + distinct_hash) |
| `deploy/aura-scheduler.service` | Systemd unit with `Restart=on-failure` | VERIFIED | Line 37: `Restart=on-failure`; `ExecStart=/usr/local/bin/aura serve` |
| `internal/cron/e2e_test.go` | Env-gated North-Star smoke | VERIFIED | File exists; build-tag `cot_eval` gates it out of CI; VALIDATION.md records 2/2=100% live pass |

---

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `cmd/aura/serve.go` | `internal/cron Scheduler.Start` | `bootServe` → `cron.NewScheduler` → `env.scheduler.Start(ctx)` | WIRED | serve.go:69 calls `env.scheduler.Start(ctx)` |
| `Scheduler.Start` | `runMissed` dispatch | `catchUpMissed` return captured + loop | WIRED | scheduler.go:117-128 — slice captured, iterated |
| `internal/agent/tools/task.go` | `cron.FirstFire` | `resolveSchedule` calls `cron.FirstFire(spec, time.Now())` | WIRED | task.go:269 confirmed |
| `cmd/aura/task.go` | `cron.FirstFire` | `taskSchedule` calls `cron.FirstFire(spec, time.Now())` | WIRED | task.go:101 confirmed |
| `internal/cron/handlers/agentjob.go` | `agent.NewLlmAgent` (NOT swarm) | Direct construction mirroring swarm.runChild | WIRED | handler.go imports `internal/agent/tools`; no `internal/swarm` import |
| `.github/workflows/ci.yml` | `go test -tags db_integration ./internal/cron/...` | integration-test job | WIRED | ci.yml:162 confirmed |
| `scripts/scheduler_chaos.sh` | `.github/workflows/ci.yml` chaos advisory | `continue-on-error: true` step | WIRED | ci.yml:178-189 runs the script as advisory |
| `BackupHandler.Run` | `os.Stat(dest)` host artifact check | After docker exec, before "ok" summary | WIRED | backup.go:111-115 confirmed |
| `cron.Store.CreateTask` | Single-INSERT with final status | `CreateTaskParams.Status` parameter | WIRED | REVIEW-FIX.md commit 93f1a9b1 — WR-03 fix |

---

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|--------------------|--------|
| `internal/cron/store_runs.go:DueTasks` | `[]Task` | sqlc `DueTasks` query with `FOR UPDATE SKIP LOCKED` | Yes — Postgres query against `aura.scheduler_tasks` | FLOWING |
| `internal/cron/heartbeat.go` | `last_heartbeat_at` | Periodic `UPDATE aura.agent_job_runs` on held conn | Yes — parameterized SQL | FLOWING |
| `internal/cron/scheduler.go:runMissed` | `[]MissedTask` | `catchUpMissed` → `ListActiveTasks` → `UpdateNextRunAt` | Yes — real DB queries; MissedSince carries original `task.NextRunAt` | FLOWING |
| `internal/cron/schedule.go:FirstFire` | `time.Time` next fire | `NextRunAt(spec, time.Now())` + ErrPastRunAt guard | Yes — deterministic time computation; no hardcoded stub | FLOWING |

---

### Behavioral Spot-Checks

These checks verify compile-time and static-analysis signals; runtime behaviors are in the human verification section.

| Behavior | Check | Result | Status |
|----------|-------|--------|--------|
| `cron.FirstFire` rejects past `at` | `ErrPastRunAt` sentinel defined + returned in `FirstFire` for `KindAt && next.IsZero()` | Confirmed in schedule.go:42,140 | PASS |
| `runMissed` called from `Scheduler.Start` | `for _, m := range missed { s.runMissed(ctx, m) }` in Start | Confirmed in scheduler.go:126-128 | PASS |
| `DueTasks` int32 clamping | `if limit <= 0 || limit > math.MaxInt32 { limit = 1 }` | Confirmed in store_runs.go:82-84 | PASS |
| `Restart=on-failure` in systemd unit | `grep "Restart=on-failure"` in deploy file | Confirmed in deploy/aura-scheduler.service:37 | PASS |
| D-24 import boundary: no swarm/tools in cron | grep of internal/cron non-test .go files | No `internal/swarm` import; `internal/agent/tools` only in `e2e_test.go` (external test package) and handlers subpackage | PASS |
| `required=["action"]` schema only at root | taskParamsSchema constant | `"required": ["action"]` only; no root oneOf/anyOf/enum | PASS |

---

### Probe Execution

No `scripts/*/tests/probe-*.sh` probes exist for this phase. The chaos script is the functional equivalent and is covered in the human verification section (it requires a live Docker stack).

---

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|----------|
| CAP-06 | All 6 plans (10-01..10-06) | Scheduler cron + `agent_job` persistent queue + advisory lock + heartbeat + backup handlers | SATISFIED | Full `internal/cron/` package ships all required mechanics; migration 0009 lands both tables; `aura serve` daemon hosts the loop; Gate-3 signed off with live SC#1-4 + E2E 100% |

---

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `internal/cron/handlers/agentjob.go` | ~152 | `args, _ := json.Marshal(...)` (IN-02) | Info | Benign — marshaling `map[string]string` cannot fail; no functional impact; documented in REVIEW.md IN-02 |
| `internal/cron/notify.go` | ~148,155 | `_, _ = json.Marshal(...)` (IN-02) | Info | Same pattern, same benign rationale |
| `internal/cron/heartbeat.go` | 38 | `_, _ = conn.Exec(...)` discards heartbeat error (IN-01) | Info | Acceptable v1 posture — orphan scan is the backstop; REVIEW.md IN-01; comment updated per WR-05 |
| `cmd/aura/main.go` | 75-76 | `case "shell": fmt.Println("TODO: ...")` | Info | Not in this phase's scope; `shell` was already a TODO before Phase 10; Phase 10 only wired `serve` |

No `TBD`, `FIXME`, or `XXX` markers were found in any `internal/cron/` or `cmd/aura/` file modified by this phase. The shell TODO in `main.go` was pre-existing and predates Phase 10.

---

### Code Review Fixes Verification (Post-Review)

The code review (10-REVIEW.md) found 2 Critical + 5 Warning findings. All 7 were fixed (10-REVIEW-FIX.md, iteration 1). The two critical fixes were verified directly against the codebase:

**CR-01 (VERIFIED):** `Scheduler.Start` now captures and dispatches `[]MissedTask`:
- `scheduler.go:117` — `missed, err := s.catchUpMissed(ctx)` (return captured, not `_, err`)
- `scheduler.go:126-128` — `for _, m := range missed { s.runMissed(ctx, m) }`
- `runMissed` method exists at lines 209-242, mirrors `runOne` without reschedule, threads `MissedSince`

**CR-02 (VERIFIED):** Past `at` schedules rejected via `cron.FirstFire`:
- `schedule.go:134-143` — `FirstFire` function defined; returns `ErrPastRunAt` when `spec.Kind == KindAt && next.IsZero()`
- `internal/agent/tools/task.go:269` — `resolveSchedule` calls `cron.FirstFire(spec, time.Now())`
- `cmd/aura/task.go:101` — `taskSchedule` calls `cron.FirstFire(spec, time.Now())`
- `ErrPastRunAt` sentinel defined at `schedule.go:42`

---

### Human Verification Required

The automated checks are all green. The following items require a live stack to confirm — they cannot be verified by static analysis alone.

#### 1. SC#1: cron fires exactly once per window

**Test:** Start `aura serve` and schedule `aura task schedule --cron "*/5 * * * *" --kind reminder --args '{"text":"test"}'`. Observe `aura task runs --task <id>` over two 5-minute windows.
**Expected:** Exactly one completed run per 5-minute window; `next_run_at` advances to the next window after each fire.
**Why human:** The advance-on-claim fix in `scheduler.go:runOne` (the `s.reschedule(ctx, task)` call before dispatch) is critical to SC#1 but its correctness can only be observed against a running tick loop. Gate-3 VALIDATION.md records a post-fix live run (2 fires / 2 windows), but this is operator-recorded evidence, not an automated assertion in CI.

#### 2. SC#2: Chaos test survivor pickup (GATING)

**Test:** Run `scripts/scheduler_chaos.sh` with the Docker stack up (3 workers, 60s partition via `docker network disconnect`).
**Expected:** Exit 0; `completed runs = 1, distinct completion hashes = 1`.
**Why human:** D-05 explicitly designates the operator run as the gating record; CI runs it non-blocking (`continue-on-error: true`). The script is syntactically valid and the code mechanics are correct, but the chaos run requires a live multi-container Docker environment.

#### 3. SC#3: Backup dump artifacts in `$AURA_BACKUP_DIR`

**Test:** Schedule nightly `backup_postgres` and `backup_neo4j` tasks; verify dump files exist in `$AURA_BACKUP_DIR` on the host after `aura serve` fires them.
**Expected:** `pg_restore --list` shows TABLE DATA entries for `aura.scheduler_tasks` and `aura.agent_job_runs`; 24h-missed alert log line appears when the backup window is skipped.
**Why human:** The `os.Stat(dest)` guard (WR-04 fix) will produce a clear error if the bind-mount is absent, but the positive case (dump succeeds and artifact is readable on the host) requires the operator to have configured the bind-mount in `compose.yaml`. Gate-3 VALIDATION.md records a valid 29069-byte archive from the live run.

---

### Gaps Summary

No gaps. All 4 ROADMAP success criteria are implemented and verified in code:
- SC#1 (cron fires once per window): implemented + the advance-on-claim fix landed; human confirmation requested.
- SC#2 (chaos survivor pickup): chaos script exists and is correct; live operator run required.
- SC#3 (backup dumps + 24h alert): handlers exist with all fixes; bind-mount operator obligation documented.
- SC#4 (budget inheritance + ask_user auto-reject): fully implemented and green in automated tests.

The CAP-06 requirement is substantively delivered. The human verification items are execution-environment dependencies (live Docker stack, operator-observed timing), not implementation gaps.

---

_Verified: 2026-06-04T20:00:00Z_
_Verifier: Claude (gsd-verifier)_
