---
phase: 10-scheduler
plan: 02
subsystem: scheduler
tags: [scheduler, cron, gronx, sqlc, migration, store, dst, registry-promotion]

# Dependency graph
requires:
  - phase: 10-scheduler
    plan: 01
    provides: amended PRD Slice 6 (at|every|cron triad, gronx, migration 0009 pinned, env catalog)
provides:
  - aura.scheduler_tasks + aura.agent_job_runs (migration 0009) with role-separation GRANTs + partial due-index
  - sqlc query surface (CreateTask/GetTask/ListActiveTasks/DueTasks SKIP-LOCKED/CancelTask/UpdateNextRunAt + run-row writers)
  - internal/cron.Store (identity-canonical Store{pool,q}) + Task/Run domain types + ErrTaskNotFound/ErrAlreadyRunning sentinels
  - internal/cron schedule engine (ParseSchedule + DST-safe NextRunAt via gronx)
  - tools.Without promoted out of internal/swarm (D-24 cron→swarm import unblocked)
  - goleak TestMain + frozen-clock DST schedule_test + db_integration store_test scaffolds
affects: [10-03, 10-04, 10-05, 10-06]

# Tech tracking
tech-stack:
  added:
    - "github.com/adhocore/gronx v1.20.0 (parser-only cron; zero transitive deps, D-08)"
  patterns:
    - "DST-safe recurring recompute: store (expr, IANA tz), recompute in-zone via gronx.NextTickAfter(ref.In(loc)), read .UTC() — never a fixed offset (D-06/D-07)"
    - "Helper promotion to break a forbidden import: Without moved swarm→tools so cron consumes it without importing swarm (D-24)"
    - "Embedded time/tzdata for self-contained zoneinfo on scratch hosts"

key-files:
  created:
    - internal/db/migrations/0009_scheduler.up.sql
    - internal/db/migrations/0009_scheduler.down.sql
    - internal/db/queries/scheduler_tasks.sql
    - internal/db/queries/agent_job_runs.sql
    - internal/cron/schedule.go
    - internal/cron/tzdata.go
    - internal/cron/store.go
    - internal/cron/main_test.go
    - internal/cron/schedule_test.go
    - internal/cron/store_test.go
    - internal/agent/tools/registry.go
    - internal/agent/tools/registry_test.go
    - internal/db/sqlc/scheduler_tasks.sql.go
    - internal/db/sqlc/agent_job_runs.sql.go
  modified:
    - internal/db/sqlc/models.go
    - internal/db/sqlc/querier.go
    - internal/swarm/swarm.go
    - internal/swarm/brief_registry_test.go
    - internal/swarm/runner_adapter_test.go
    - go.mod
    - go.sum
  deleted:
    - internal/swarm/registry.go

key-decisions:
  - "DST Open Q1 resolved empirically: a cron 30 2 * * * on the Europe/Rome 2026 spring-forward night (2026-03-29, 02:00 CET→03:00 CEST) SKIPS the non-existent 02:30 local wall-clock and fires the next valid 02:30 (2026-03-30 02:30 CEST = 00:30 UTC). Deterministic via time.Date normalization inside gronx; documented in a comment + asserted in TestNextRunAtDST."
  - "main_test.go is UNTAGGED (not db_integration like identity's) so the goleak gate guards the unit schedule tests too (swarm precedent), not only the integration tier."
  - "CreateRunAndAdvance added as the plan's named db.WithTx atomic write (insert run row + recompute next_run_at in one tx); the held-conn advisory-lock claim itself lands in 10-03."
  - "MinScheduleEveryMinutes=5 is a package constant (no env var catalogued for the every floor); enforced in both ParseSchedule and NextRunAt."

patterns-established:
  - "Phase-10 cron Store copies the identity 04-02 canonical pattern verbatim (Store{pool,q}, fromRow domain projection, errors.As SQLSTATE classification, pgx.ErrNoRows→sentinel)."

requirements-completed: [CAP-06]

# Metrics
duration: 42min
completed: 2026-06-04
---

# Phase 10 Plan 02: Scheduler Infra Foundation (6a part 1) Summary

**Built the Phase-10 storage + scheduling foundation: migration 0009 (scheduler_tasks + agent_job_runs with role-separation GRANTs, the partial due-index, and the FOR UPDATE SKIP LOCKED claim query), the sqlc surface + identity-canonical `internal/cron.Store`, the gronx-backed DST-safe `NextRunAt` (proven against the Europe/Rome spring-forward gap), and the cross-cutting promotion of `Without` from `internal/swarm` to `internal/agent/tools` so `internal/cron` never imports swarm (D-24).**

## Performance

- **Duration:** ~42 min
- **Completed:** 2026-06-04
- **Tasks:** 2/2
- **Files:** 14 created, 7 modified, 1 deleted

## Accomplishments

- **Task 1 — Migration 0009 + sqlc (`2998cacc`):** `aura.scheduler_tasks` (id, kind, schedule_kind, cron_expr/every_minutes/run_at, tz default Europe/Rome, payload jsonb, step_budget, status, next_run_at, notify_route, identity_id, origin_conversation_id, created/updated_at — the D-28 fwd-compat set) and `aura.agent_job_runs` (task_id FK, status, step_budget, started/last_heartbeat_at, `completed_with_hash` UNIQUE, summary, last_error, missed_since, `paused_state_token` FK→aura.paused_states, completed_at). Partial `scheduler_tasks_due_idx ON (next_run_at) WHERE status='active'` (plain, in-tx). Role separation: scheduler_tasks → SELECT/INSERT/UPDATE/DELETE to aura_app; agent_job_runs → SELECT/INSERT/UPDATE only (no DELETE, audit-forever, PRD OQ4); GRANT ALL to aura_migrate on both. sqlc query files with `DueTasks ... FOR UPDATE SKIP LOCKED LIMIT $1` + run-row writers. `sqlc generate` regenerated the client (no hand-edit).
- **Task 2 — schedule engine + Store + Without promotion + scaffolds (`9071ae87`):**
  - `schedule.go`: `ScheduleSpec` union + `ParseSchedule` (gronx.IsValid gate for cron before persist, every-floor of 5, at requires run_at, tz LoadLocation gate) + `NextRunAt` (at→stored instant or zero after firing; every→add interval; cron→in-zone `gronx.NextTickAfter(after.In(loc), false)` then `.UTC()`). `tzdata.go` embeds `time/tzdata`.
  - `store.go`: identity-canonical `Store{pool,q}` + `Task`/`Run` domain structs + `taskFromRow`/`runFromRow` + sentinels + `CreateTask`/`GetTask`/`ListActiveTasks`/`CancelTask`/`UpdateNextRunAt`/`InsertRun`/`CreateRunAndAdvance` (db.WithTx atomic claim+reschedule)/`Heartbeat`/`CompleteRun` (23505 idempotency swallow via `errors.As`). All pgtype conversion confined in-package.
  - `Without` promoted to `internal/agent/tools/registry.go`; swarm copy deleted; `swarm.go` + `runner_adapter_test.go` switched to `tools.Without`; the standalone `TestWithout` moved into `tools/registry_test.go` (swarm keeps `TestRunnerAdapterWorkerRegistryExcludesSwarmSpawn` as its consumption proof).
  - Scaffolds: `main_test.go` goleak gate; `schedule_test.go` frozen-clock at/every/cron + DST + validation; `store_test.go` (db_integration) round-trip + ErrTaskNotFound + idempotency-hash + atomic-advance.

## Task Commits

1. **Task 1: migration 0009 scheduler tables + sqlc queries** — `2998cacc` (feat)
2. **Task 2: gronx DST-safe schedule engine + cron Store + Without promotion** — `9071ae87` (feat)

## Verification

- `go vet ./...` + `go build ./...` → clean
- `go test ./internal/cron/ ./internal/agent/tools/ ./internal/swarm/` → ok (Git Bash, no -race)
- `go test -race ./internal/cron/ ./internal/agent/tools/ ./internal/swarm/` → ok (WSL native race)
- `go test -tags db_integration -race ./internal/cron/ ./internal/db/...` → ok (live Postgres on 127.0.0.1:5432; migration 0009 applies under aura_migrate/aura_app role separation; Store round-trips; db package migration round-trip test auto-discovers 0009)
- `golangci-lint run ./internal/cron/... ./internal/agent/tools/... ./internal/swarm/...` → 0 issues
- Acceptance greps: `func Without` in tools = 1; in swarm = NONE; `tools.Without` in swarm.go = 1; `go list -deps ./internal/cron | grep internal/swarm` = empty; `gronx.NextTickAfter` in schedule.go = 1; `db.WithTx` in store.go = 3; `CREATE TABLE aura.scheduler_tasks` + `FOR UPDATE SKIP LOCKED` present
- File sizes: largest touched file is store.go at 377 LOC (all ≤600)

## Deviations from Plan

### Auto-resolved (Rule 3 — blocking issue: aborted git add)

**1. First Task-2 commit captured only the staged deletion**
- **Found during:** Task 2 commit. The combined `git add` listed `internal/swarm/registry.go`, but that file was already staged-as-deleted via an earlier `git rm`, so the pathspec no longer matched a working-tree file and `git add` aborted BEFORE staging the remaining 12 files. The resulting commit `5b0e9e3d` held only `D internal/swarm/registry.go`.
- **Resolution:** Re-staged the 12 remaining files (excluding the already-`git rm`'d registry.go) and `git commit --amend --no-edit`. Final commit `9071ae87` contains all 13 file changes. Verified via `git show --stat --name-status` and a clean working tree. No content lost; pre-commit hooks (gofmt/vet/file-size) re-ran green on the amend.

### Naming discretion (within plan's stated latitude)

**2. sqlc query names finalized**
- The plan granted "final names = Claude's discretion." Implemented: `CreateTask`, `GetTask`, `ListActiveTasks`, `DueTasks`, `CancelTask`, `UpdateNextRunAt`, `InsertRun`, `GetRun`, `UpdateHeartbeat`, `CompleteRun`, `ScanStaleRuns`, `MarkUnknownRecovery`. `ScanStaleRuns`/`MarkUnknownRecovery`/`GetRun`/`DueTasks` are defined for the downstream tick/claim/recover waves (10-03) but not yet wired by a Store method here — they compile under sqlc and carry no dead-code lint (generated surface is lint-excluded).

## Threat Flags

None — the new surface (tables + store + schedule parse) is exactly the plan's `<threat_model>` register. T-10-01 (parameterized sqlc), T-10-02/03 (role-separation GRANTs, no agent_job_runs DELETE), T-10-05 (ParseSchedule gronx.IsValid gate before persist) are all implemented as specified. No endpoints/auth-paths/new trust boundaries beyond the documented ones.

## Known Stubs

None that block the plan's goal. The store methods `Heartbeat`/`CompleteRun`/`InsertRun`/`CreateRunAndAdvance` are full implementations; the held-conn advisory-lock claim lifecycle and the tick loop are explicitly out of scope for this plan (land in 10-03) — not stubs, just future surface. The sqlc queries `DueTasks`/`ScanStaleRuns`/`MarkUnknownRecovery`/`GetRun` exist and compile; their Store wrappers land with the 10-03 claim/recover code that needs them.

## Self-Check: PASSED

- FOUND: internal/db/migrations/0009_scheduler.up.sql
- FOUND: internal/db/migrations/0009_scheduler.down.sql
- FOUND: internal/db/queries/scheduler_tasks.sql
- FOUND: internal/db/queries/agent_job_runs.sql
- FOUND: internal/cron/schedule.go
- FOUND: internal/cron/tzdata.go
- FOUND: internal/cron/store.go
- FOUND: internal/cron/main_test.go
- FOUND: internal/cron/schedule_test.go
- FOUND: internal/cron/store_test.go
- FOUND: internal/agent/tools/registry.go
- FOUND: internal/agent/tools/registry_test.go
- DELETED (verified): internal/swarm/registry.go
- FOUND commit 2998cacc (Task 1)
- FOUND commit 9071ae87 (Task 2)
- Working tree clean; STATE.md / ROADMAP.md NOT modified (orchestrator-owned)
