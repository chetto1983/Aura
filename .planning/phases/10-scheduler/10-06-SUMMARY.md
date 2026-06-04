---
phase: 10-scheduler
plan: 06
subsystem: scheduler
tags: [scheduler, daemon, serve, graceful-shutdown, chaos, ci, systemd, north-star-e2e, gate-3, live-verification, sc1, sc2, sc3, sc4]

# Dependency graph
requires:
  - phase: 10-scheduler
    plan: 03
    provides: held-conn advisory-lock claim + tick loop + Dispatcher seam + DueTasks
  - phase: 10-scheduler
    plan: 04
    provides: non-deferred task tool (action enum) + scoring gate
  - phase: 10-scheduler
    plan: 05
    provides: cron-free handlers (reminder/agent_job/backup) + composite Notifier + Dispatch
provides:
  - cmd/aura/serve.go — aura serve long-lived daemon (D-15) on the error-returning bootChat refactor + SIGINT/SIGTERM graceful shutdown (reverse-close MCP closers, goleak-clean)
  - cmd/aura task tool registration with the live cron store injected at the composition root (deferred from 10-04)
  - scripts/scheduler_chaos.sh — 3-worker / 60s-partition SC#2 chaos test (socat-proxy topology)
  - .github/workflows/ci.yml — scheduler db_integration tier (no-skip-as-green) + non-blocking chaos-advisory step
  - deploy/aura-scheduler.service — documented systemd unit (Restart=on-failure, D-16)
  - internal/cron/e2e_test.go — env-gated live North-Star smoke (Q3 reminder + Q1 cron agent_job, natural Italian, cot_eval tag, NOT CI)
  - Gate-3 sign-off evidence (10-VALIDATION.md Manual-Only + docs/aura-quality-snapshot.md Phase 10 row + detail)
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "advance-on-claim (SC#1): the won-claim path advances next_run_at to the next fire BEFORE dispatch — the held advisory lock is a per-run singleton but does NOT remove the task from the due-set, so without the advance every tick re-selects (and, once the lock releases, re-fires) the same due task. A one-shot `at` task advances to a zero next_run_at (retired)."
    - "Gate-3 live verification discipline: every tier run live from WSL against the real Docker stack (no compile-check substitute, no skip-as-green); each SC has >=1 assertion on DB/file ground truth, never the agent's prose; the live North-Star E2E drives the REAL agent and gates on a pass-rate, not a self-graded rubric."
    - "Backup dump role = aura_migrate (owns every aura.* + public.schema_migrations); pg_dump -U aura_app fails live with `permission denied for table schema_migrations` (the app role lacks LOCK on the migration trackers)."

key-files:
  created:
    - .planning/phases/10-scheduler/10-06-SUMMARY.md
  modified:
    - cmd/aura/serve.go
    - cmd/aura/chat.go
    - cmd/aura/main.go
    - scripts/scheduler_chaos.sh
    - .github/workflows/ci.yml
    - deploy/aura-scheduler.service
    - internal/cron/e2e_test.go
    - internal/cron/scheduler.go
    - internal/agent/tools/task.go
    - internal/cron/handlers/backup.go
    - internal/cron/handlers/backup_test.go
    - .planning/phases/10-scheduler/10-VALIDATION.md
    - docs/aura-quality-snapshot.md
  deleted: []

key-decisions:
  - "Advance next_run_at on a WON claim (not only on the skip/lost-lock path). The original runOne advanced the schedule only when a worker LOST the advisory lock; on a successful claim it dispatched but left next_run_at unchanged, so a */5 cron fired 94 times in 7.5 minutes (every 5s tick) instead of twice. Reschedule-on-claim fixes SC#1 without breaking SC#2 (the chaos re-run stays green: completed=1/distinct=1)."
  - "Backup pg_dump role aura_app→aura_migrate. aura_app cannot LOCK public.schema_migrations (owned by aura_migrate), so the production fixed argv failed live. aura_migrate owns every dumped table and produces a valid custom-format archive. The unit argv test was updated WITH justification (the test encoded the bug)."
  - "Live North-Star E2E gates on a pass-rate over deterministic DB-row assertions (Q3 reminder/at + Q1 agent_job/cron), not a judge rubric — the scheduler harness has no scorer; overall = passed scenarios / total = 2/2 = 100% (> the operator's 90% gate). The agent must pick the `task` tool from a natural Italian prompt with NO scheduling literal (asserted)."
  - "The `task` tool description now teaches that an agent_job runs AT FIRE TIME with its own tools, so the model schedules a recurring mail-summary job instead of refusing because the mail tools are not mounted at schedule time (the Phase-9 swarm deferred-stub precedent applied to scheduling). The description stays a turn-stable constant (cache-safe)."

requirements-completed: [CAP-06]

# Metrics
duration: ~150min (Gate-3 live verification + two in-flight bug fixes)
completed: 2026-06-04
---

# Phase 10 Plan 06: Daemon + Validation Surface + Gate-3 Live Sign-Off Summary

**Closed CAP-06 by standing up the `aura serve` daemon (D-15) on an error-returning shared boot with goleak-clean graceful shutdown, the SC#2 chaos test, CI db_integration + chaos-advisory wiring, the documented systemd unit (D-16), and the env-gated live North-Star smoke — then executed the operator-delegated Gate-3 on the live stack from WSL ("vai con tutti i test su WSL. poi E2E con Agente reale a score >90%"). Tasks 1-2 (daemon + scaffold) were already merged (592f93e9, 65b33e6d); this continuation ran the full live verification, which caught and fixed two production bugs (the SC#1 once-per-window regression and the SC#3 backup dump role) and drove the real DeepSeek-V4 agent to a 2/2 = 100% North-Star pass. All gates green: SC#1 once-per-window, SC#2 chaos survivor-pickup no-dup, SC#3 valid dump + 24h alert, SC#4 budget-10 + ask_user auto-reject, live E2E 100% (> 90%), coverage 88.5% (≥ 85%), schedule.go mutation 77.3% (≥ 70%), lint 0.**

## Operator Delegation

The Task-3 checkpoint is a `blocking-human` Gate-3. The operator did NOT auto-approve; instead the operator delegated execution to the agent with two explicit requirements: (a) ALL test tiers run from WSL; (b) the live North-Star E2E with the real agent must score > 90%. This continuation executed steps 1-7 of the checkpoint's how-to-verify on the live stack and recorded the evidence; the operator's delegation IS the sign-off authorization.

## Performance

- **Duration:** ~150 min (live verification across all tiers + two bug fixes + retries to a 100% E2E)
- **Completed:** 2026-06-04
- **Tasks:** 3/3 (Task 3 = this Gate-3 live run)
- **Files (this continuation):** 1 created, 7 modified (5 code + 2 evidence docs)

## Gate-3 Live Results (WSL, real Docker stack + real DeepSeek-V4)

| Step / SC | Result | Ground truth |
|-----------|--------|--------------|
| Step 1 — migration 0009 + tables | PASS | `aura.scheduler_tasks` + `aura.agent_job_runs` present with CHECK constraints |
| SC#1 — cron once-per-window | PASS (after fix) | Live serve 17:42→17:52: **2 fires / 2 windows**, next_run_at advanced to 17:55 (was **94 fires** before the fix) |
| SC#2 — chaos survivor pickup, no dup (GATING) | PASS | `scheduler_chaos.sh`: completed=1, distinct=1, exit 0 (re-run green after the SC#1 fix) |
| SC#3 — backup dump + 24h alert | PASS | corrected `pg_dump -U aura_migrate` → valid 29069-byte archive (pg_restore --list shows scheduler_tasks+agent_job_runs); 24h alert `overdue=25h0m0s` fires live |
| SC#4 — budget-10 + ask_user auto-reject | PASS | `TestAgentJobBudgetInherit` + `TestAskUserAutoReject` (+2) green; audit marker `agent_job.ask_user.auto_rejected` |
| Live North-Star E2E (real agent, GATING) | **PASS — 2/2 = 100% > 90%** | Q3 natural IT → `task{at,reminder}` row; Q1 natural IT → `task{cron,agent_job}` row; no scheduling literal (asserted) |
| Coverage (owned-surface, db+neo4j tags) | PASS | **88.5%** ≥ 85% (`scripts/coverage_gate.sh`) |
| Mutation — schedule.go | PASS | **77.3%** (17/22) ≥ 70% (claim.go/heartbeat.go unreliable under go-mutesting's lock/timer state-bleed; correctness via live SC#2 + db_integration claim tests) |
| Lint / vet / file-size | PASS | golangci-lint 0; vet clean (default + db_integration + cot_eval); all touched files ≤ 600 LOC |

## Deviations from Plan

### Rule 1 (auto-fix bug) — SC#1 once-per-window regression

**1. `runOne` never advanced `next_run_at` on a won claim**
- **Found during:** SC#1 live verification — a `*/5 * * * *` cron reminder produced **94 run rows** in 7.5 minutes (one per 5s tick) instead of 2 (one per 5-min window); `next_run_at` stayed at its original value, `updated_at` never moved past schedule time.
- **Issue:** `scheduler.go runOne` advanced `next_run_at` (`reschedule`) only on the lost-lock / skip path; on a SUCCESSFUL claim it dispatched but left `next_run_at` unchanged. The held advisory lock is a per-run singleton but does NOT remove the task from `DueTasks` (next_run_at ≤ now), so every subsequent tick re-selected it and — once the lock released — re-fired it.
- **Fix:** advance `next_run_at` to the next fire on a won claim, BEFORE dispatch (so a long/failed run never re-fires mid-flight; a one-shot `at` advances to zero and retires). `internal/cron/scheduler.go`.
- **Verify:** post-fix live serve = exactly 2 fires / 2 windows, next_run_at advanced; SC#2 chaos re-run stays green (completed=1/distinct=1).
- **Commit:** `4b4030f0`

### Rule 1 (auto-fix bug) — SC#3 backup dump role

**2. `pg_dump -U aura_app` fails live (permission denied for table schema_migrations)**
- **Found during:** SC#3 live backup — the production fixed argv `pg_dump -U aura_app -Fc -f <dest> aura` failed with `permission denied for table schema_migrations`: `aura_app` lacks LOCK on `public.schema_migrations` (owned by `aura_migrate`).
- **Fix:** dump role → `aura_migrate` (owns every `aura.*` + `public.schema_migrations`). Corrected argv produces a valid custom-format archive (verified via `pg_restore --list`). The unit argv test (`TestBackupDumpArgvPostgresFixed`) encoded the buggy `aura_app` expectation — updated WITH justification (the test was asserting an argv that fails live). The live test now honors an explicit `AURA_BACKUP_DIR` (a bind-mounted path) instead of a container-invisible `t.TempDir()`. `internal/cron/handlers/backup.go`, `internal/cron/handlers/backup_test.go`.
- **Commit:** `4b4030f0`

### Rule 1/3 (auto-fix) — live E2E natural-prompt scheduling (no test/rubric weakening)

**3. Q3 prompt timing flaw + Q1 agent_job tool-availability refusal**
- **Found during:** the live North-Star E2E (3 attempts to green, within the 3-strike rule).
- **Attempt 1:** Q3 "oggi alle 17:30" was already PAST at run time (~20:00 Rome) → the model correctly declined to schedule a past reminder. Fixed the prompt to an unambiguously-future "domani alle 9:30" (a test-prompt correctness fix, not a weakening; natural-prompt discipline preserved).
- **Attempt 2:** Q3 passed; Q1 "ogni mattina alle 9:30 fammi un riassunto delle mail" failed — the model `tool_search`ed for mail tools (absent in the E2E registry), found none, and asked for clarification instead of scheduling the deferred agent_job.
- **Fix:** the `task` tool description now teaches that an agent_job runs AT FIRE TIME with its own tools (so the job need not have the tools available at schedule time — the Phase-9 swarm deferred-stub precedent). The description stays a turn-stable constant (cache-safe). `internal/agent/tools/task.go`. e2e_test.go logs the model tool choice under `-v` for diagnosability.
- **Attempt 3:** both pass — Q3 reminder/at + Q1 agent_job/cron, 2/2 = 100%.
- **Commit:** `4b4030f0`

No Rule 4 (architectural) escalation; no auth gates (OPENROUTER_API_KEY + DSNs were available).

## Known Stubs / Operator Obligations

- **Backup host-readback (CAP-02 mount-wiring):** the dump runs `docker exec ... pg_dump -f <dest>` INSIDE aura-postgres; the live test's host-side `os.ReadDir` can only read the artifact back when `AURA_BACKUP_DIR` is bind-mounted into the container at the SAME path (host==container). The dump itself is proven valid; wiring the bind mount into `compose.yaml` is the documented operator obligation (same class as the Phase-8 mount nuance). `TestBackupDockerExecLive` `t.Fatal`s under `$CI` when `AURA_BACKUP_LIVE` is unset (no skip-as-green).
- **Neo4j backup is offline-only on Community:** `neo4j-admin database dump` requires the DB stopped (Enterprise has online backup). The fixed argv is unit-proven; the live offline-dump is an ops procedure (stop→dump→start).
- **Live agent_job fire + WhatsApp/mail delivery read-back:** the E2E scaffold proves the natural-prompt → `task` tool → persisted-row leg; the live fire + delivery read-back (Q1/Q2) is a future operator op via the mounted MCP (the row + cron lands here).

## Threat Flags

None beyond the plan's `<threat_model>` register. The two bug fixes harden, not expand, the surface:
- **T-10-22** (chaos skip-as-green): the chaos run is the operator-gating record (completed=1/distinct=1, exit-non-zero on dup/miss) — exercised live, not skipped.
- **T-10-23** (live smoke wrong-recipient): the E2E asserts the persisted row's kind/schedule_kind from a natural prompt; delivery read-back at the right recipient is the future operator op (Phase-9 precedent).
- The backup role change keeps the dump LEAST-privilege within what can actually LOCK the tables (aura_migrate, not the superuser aura); the fixed argv still carries NO payload (T-10-16) and never the socket (T-10-15).

## Self-Check: PASSED

- FOUND: .planning/phases/10-scheduler/10-06-SUMMARY.md
- FOUND: internal/cron/scheduler.go (advance-on-claim fix)
- FOUND: internal/cron/handlers/backup.go (aura_migrate role)
- FOUND: internal/agent/tools/task.go (agent_job-defers-its-tools guidance)
- FOUND: internal/cron/e2e_test.go (future Q3 prompt + tool-call trace)
- FOUND: .planning/phases/10-scheduler/10-VALIDATION.md (Gate-3 evidence, signed-off)
- FOUND: docs/aura-quality-snapshot.md (Phase 10 row + detail)
- FOUND commit 4b4030f0 (code fixes)
- FOUND commit 8c281fb8 (evidence docs)
- STATE.md / ROADMAP.md NOT modified (orchestrator-owned); parallel-session eval docs (aura-cot-eval / aura-swarm-eval) left unstaged; cover_gate.out.testlog byproduct reverted; no git push
