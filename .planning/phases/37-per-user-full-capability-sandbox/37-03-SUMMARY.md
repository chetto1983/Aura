---
phase: 37-per-user-full-capability-sandbox
plan: 03
subsystem: infra
tags: [scheduler, cron, taskkind, postgres, migration, golang-migrate, go, sandbox]

# Dependency graph
requires:
  - phase: 36-per-user-identity
    provides: "identity_purge scheduler-TaskKind template (handler + IdentityPurger seam) and the 0033 kind-widen migration + version-anchored reversibility-drill pattern (migrate_0033_integration_test.go)"
  - phase: 0.9-scheduler
    provides: "the migration-0009 scheduler_tasks table + dispatcher (owns the tick; handlers are stateless, no bespoke goroutine)"
provides:
  - "KindSandboxReap TaskKind (\"sandbox_reap\") + SandboxReapHandler (Meta: 5-min budget, no reschedule-on-recovery; nil-reaper disabled no-op)"
  - "SandboxReaper consumer-declared seam interface (SuspendIdle(ctx, now) (int, error)) — no reverse import into the sandbox runtime package"
  - "Migration 0034: widens aura.scheduler_tasks.kind CHECK to admit 'sandbox_reap' (reversible on a live seeded DB)"
  - "migrate_0034_integration_test.go: db_integration version-anchored round-trip drill for 0034"
affects: [37-05, sandbox-reaper-wiring, usersandbox-router]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Consumer-declared seam (IdentityPurger -> SandboxReaper): the handlers package accepts an interface the sandbox router satisfies, so no reverse-import cycle"
    - "Idle reaping as a scheduler TaskKind (D-08), never a bespoke time.Ticker/goroutine — goleak stays green"
    - "Version-anchored migration reversibility drill (Steps(n) relative to head) isolating one migration's own down"

key-files:
  created:
    - internal/cron/handlers/sandbox_reap.go
    - internal/cron/handlers/sandbox_reap_test.go
    - internal/db/migrations/0034_scheduler_sandbox_reap_kind.up.sql
    - internal/db/migrations/0034_scheduler_sandbox_reap_kind.down.sql
    - internal/db/migrate_0034_integration_test.go
  modified: []

key-decisions:
  - "Reversibility test placed in package db (internal/db/migrate_0034_integration_test.go), NOT the plan-stated internal/db/migrations/ path — that directory has no Go package and cannot reach the unexported version-anchored harness (envOrSkip/MigrateSteps/currentMigrationVersion). This is the established migrate_NNNN_integration_test.go convention the plan's read_first points to."
  - "The SandboxReaper doc comment names the seam's satisfier generically (\"per-identity sandbox router\") rather than the literal package path, satisfying the acceptance grep (no 'usersandbox' token) while the true prohibition (no import) is independently proven by go list -deps."
  - "SBX-03 NOT marked complete: it is a phase-spanning requirement claimed by 5 plans (37-01/03/04/05/06); this plan ships only the reaper scaffold, proven in isolation."

patterns-established:
  - "Scheduler-TaskKind reaper scaffold: kind const in the handler's own file + consumer-declared seam + kind-widen migration, shipped and proven before the live impl is wired (37-05)"

requirements-completed: []

coverage:
  - id: D1
    description: "sandbox_reap handler + SandboxReaper seam (KindSandboxReap, Meta 5-min/no-reschedule, nil-reaper disabled no-op, error wrapped with %w) mirroring identity_purge"
    requirement: "SBX-03"
    verification:
      - kind: unit
        ref: "internal/cron/handlers/sandbox_reap_test.go#TestSandboxReapMeta,TestSandboxReapRunSuspends,TestSandboxReapDisabled,TestSandboxReapRunError"
        status: pass
    human_judgment: false
  - id: D2
    description: "Migration 0034 widens scheduler_tasks.kind for 'sandbox_reap'; down deletes admitted rows before narrowing (reversible on a live seeded DB), proven by a version-anchored round-trip drill"
    requirement: "SBX-03"
    verification:
      - kind: integration
        ref: "internal/db/migrate_0034_integration_test.go#TestMigration0034SchedulerSandboxReapKind (db_integration; runs live at CI/WSL phase validation, skips locally without POSTGRES_PASSWORD, t.Fatals under $CI when env unset)"
        status: unknown
    human_judgment: false
    rationale: "The live (a) admit-at-head / (b) 23514-at-v33 / (c) down+re-up straddle assertions require the Postgres stack; not reachable in the worktree, so status is unknown until the verifier runs it live (compiles + skips cleanly locally, CI-guard wired)."
  - id: D3
    description: "Containment invariants: handlers never imports the sandbox runtime package (no reverse cycle) and the reaper adds no new goroutine/ticker (D-08 goleak discipline)"
    requirement: "SBX-03"
    verification:
      - kind: other
        ref: "grep -n usersandbox internal/cron/handlers/sandbox_reap.go (empty) + grep -nE 'time.Ticker|go func' (empty) + go list -deps ./internal/cron/handlers/ | grep usersandbox (empty)"
        status: pass
    human_judgment: false

# Metrics
duration: ~25 min
completed: 2026-07-06
status: complete
---

# Phase 37 Plan 03: Sandbox Idle-Suspend Reaper Scaffold Summary

**`sandbox_reap` scheduler TaskKind + SandboxReapHandler driving a consumer-declared SandboxReaper{SuspendIdle} seam, plus migration 0034 widening scheduler_tasks.kind — the D-08 idle reaper landed as a scheduler kind (no goroutine), unit/db-proven in isolation and ready for 37-05 to provide the live impl.**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-07-06T21:55:00Z (approx)
- **Completed:** 2026-07-06T22:21:00Z
- **Tasks:** 2
- **Files created:** 5

## Accomplishments
- `KindSandboxReap` TaskKind + `SandboxReapHandler` (5-min budget, no reschedule-on-recovery, nil-reaper disabled no-op, terminal error wrapped with `%w`) — an exact structural mirror of `IdentityPurgeHandler`.
- `SandboxReaper` consumer-declared seam (`SuspendIdle(ctx, now) (int, error)`) so the `handlers` package does NOT import the sandbox runtime package — no reverse-import cycle (proven by `go list -deps`).
- Migration 0034 widens `aura.scheduler_tasks.kind` to admit `'sandbox_reap'`; the down deletes admitted rows (agent_job_runs FK CASCADE) BEFORE narrowing, so it is reversible on a live seeded DB.
- Version-anchored `db_integration` reversibility drill (`migrate_0034_integration_test.go`) mirroring the Phase-36 0033 approach: admit-at-head / 23514-at-v33 / down+re-up straddle; `t.Fatal`s under `$CI` without DB env (no skip-as-green).

## Task Commits

Each task was committed atomically:

1. **Task 1: sandbox_reap handler + SandboxReaper seam** - `e89bddd1` (feat) — RED proven at the CLI (undefined symbols), then GREEN test+impl together (the pre-commit `go vet` gate forbids a standalone red commit; no `--no-verify` bypass).
2. **Task 2: migration 0034 widen scheduler_tasks.kind + reversibility test** - `bb9e9641` (feat)

_TDD note: Task 1's RED gate was exercised at the command line before implementing (the four tests failed to compile with the expected `undefined: SandboxReapHandler/KindSandboxReap/...` errors). The project's pre-commit vet hook structurally rejects a commit whose package does not compile, so the standalone `test(...)` RED commit collapses into the GREEN `feat(...)` commit._

## Files Created/Modified
- `internal/cron/handlers/sandbox_reap.go` - KindSandboxReap TaskKind, SandboxReaper seam, SandboxReapHandler (Meta/Run).
- `internal/cron/handlers/sandbox_reap_test.go` - 4 unit tests + fakeReaper double (Meta contract, suspends+count, disabled no-op, terminal error).
- `internal/db/migrations/0034_scheduler_sandbox_reap_kind.up.sql` - drop + re-add scheduler_tasks_kind_check with 'sandbox_reap' appended.
- `internal/db/migrations/0034_scheduler_sandbox_reap_kind.down.sql` - delete admitted sandbox_reap rows first, then restore the 0033 CHECK list.
- `internal/db/migrate_0034_integration_test.go` - db_integration version-anchored round-trip drill for 0034.

## Decisions Made
- **Test lives in `package db` (`internal/db/`), not the plan-stated `internal/db/migrations/`.** The migrations directory holds only `.sql` embed sources — no Go package — and the version-anchored harness the plan mandates (`envOrSkip`, `MigrateSteps`, `currentMigrationVersion`) is unexported in `package db`. The established repo convention is `internal/db/migrate_NNNN_integration_test.go`, which is exactly the "Phase-36 36-13 version-anchored approach" the plan's `<read_first>` cites. See Deviation 1.
- **Comment wording adjusted to satisfy the acceptance grep.** A verbatim copy of the identity_purge comment named the concrete package path, tripping acceptance criterion #2's literal `grep -n "usersandbox"`. The comment now names the satisfier as "the per-identity sandbox router"; the actual no-import prohibition holds and is independently proven by `go list -deps`. See Deviation 2.
- **SBX-03 left open.** It is a lifecycle+isolation requirement shared across 5 plans; this scaffold plan advances only the reaper slice, so `requirements-completed` is `[]` and REQUIREMENTS.md is untouched (the orchestrator/verifier reconciles SBX-03 at phase completion).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Reversibility test relocated to the version-anchored harness package**
- **Found during:** Task 2 (migration 0034 reversibility test)
- **Issue:** The plan's stated path `internal/db/migrations/migrations_sandbox_reap_test.go` is infeasible: `internal/db/migrations/` contains only `.sql` files (no Go package), and the version-anchored drill the plan mandates depends on `package db`-private helpers (`envOrSkip`, `bootstrapURL`, `MigrateSteps`, `currentMigrationVersion`). A test in a separate `migrations` package cannot reach them — it would not compile.
- **Fix:** Placed the test at `internal/db/migrate_0034_integration_test.go` (package `db`, `//go:build db_integration`), an exact mirror of `migrate_0033_integration_test.go` — the "Phase-36 36-13 version-anchored approach" the plan's `<read_first>` names. Anchored the down step off `head` (`33 - head`) to isolate 0034's own down.
- **Files modified:** internal/db/migrate_0034_integration_test.go (created)
- **Verification:** `go vet -tags db_integration ./internal/db/` passes; the test compiles and skips cleanly locally (no DB env); the `envOrSkip` CI-guard (`t.Fatal` under `$CI`) is wired.
- **Committed in:** bb9e9641 (Task 2 commit)

**2. [Rule 1 - Acceptance-gate] SandboxReaper comment reworded to satisfy the no-`usersandbox` grep**
- **Found during:** Task 1 (handler implementation, acceptance-criteria loop)
- **Issue:** The verbatim identity_purge-style comment named the concrete package path in prose, so acceptance criterion #2 (`grep -n "usersandbox" sandbox_reap.go` returns nothing) failed on two comment lines even though there is no actual import.
- **Fix:** Reworded the two comment lines to describe the satisfier as "the per-identity sandbox router" (no literal `usersandbox` token) while preserving the architectural meaning (consumer-declared seam, no reverse cycle, satisfied in 37-05).
- **Files modified:** internal/cron/handlers/sandbox_reap.go
- **Verification:** `grep -n "usersandbox" sandbox_reap.go` → empty; `go list -deps ./internal/cron/handlers/ | grep usersandbox` → empty (no reverse import); 4 unit tests still green; vet clean.
- **Committed in:** e89bddd1 (Task 1 commit)

---

**Total deviations:** 2 auto-fixed (1 blocking, 1 acceptance-gate). Plus a TDD process note (RED collapsed into GREEN under the pre-commit vet gate — not a deviation).
**Impact on plan:** No scope change. Both fixes were necessary to (1) make the mandated version-anchored test compile against the real harness and (2) pass the literal acceptance grep while keeping the true no-reverse-import guarantee. The plan's `files_modified` entry `internal/db/migrations/migrations_sandbox_reap_test.go` should read `internal/db/migrate_0034_integration_test.go`.

## Issues Encountered
- The pre-commit `go vet` hook rejected the standalone RED commit (package doesn't compile while the impl is absent). Resolved by proving RED at the CLI and committing test+impl together as the GREEN feat commit — no `--no-verify` bypass (per CLAUDE.md / #2924).

## Deferred / Intentional Non-wiring (not a stub)
- `SandboxReaper` has no live implementation in this plan by design: the objective ships the handler + kind + migration "unit/db-proven in isolation," and plan **37-05** provides the live `SuspendIdle` impl (the usersandbox router) + registers `KindSandboxReap` + seeds the sweep. The nil-reaper path is a safe disabled no-op until then. No user-facing surface renders empty data.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Ready for **37-05** to provide the `SuspendIdle` impl behind the `SandboxReaper` seam, register `cron.KindSandboxReap: handlers.SandboxReapHandler{Reaper: <router>}`, and seed the reap sweep (mirror `seedIdentityPurgeSweep`).
- Migration 0034 is the next slot after 0033 (no renumber). Its live reversibility assertions run at phase validation on the WSL/CI stack (`go test -tags db_integration ./internal/db/ -run SandboxReap`).
- SBX-03 remains open (multi-plan: 37-01/04/05/06 still contribute create/resume/delete/storage/isolation).

## Self-Check: PASSED
- All 5 created files verified present on disk (Write confirmations + git-tracked in commits e89bddd1 / bb9e9641).
- Both task commits verified in `git log` (`e89bddd1`, `bb9e9641`).
- `go build ./...` clean; `go vet ./...` clean; `go vet -tags db_integration ./internal/db/` clean; 4 unit tests green; db_integration reversibility test compiles + skips cleanly locally.

---
*Phase: 37-per-user-full-capability-sandbox*
*Completed: 2026-07-06*
