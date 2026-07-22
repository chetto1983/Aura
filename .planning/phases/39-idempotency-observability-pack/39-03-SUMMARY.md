---
phase: 39-idempotency-observability-pack
plan: 03
subsystem: serving-readiness
tags: [go, readiness, lifecycle, scheduler, postgres, neo4j, docker-compose]

# Dependency graph
requires:
  - phase: 39-idempotency-observability-pack
    plan: 01
    provides: "PostgreSQL migration 0043, the current embedded schema head"
  - phase: 12-agui-gateway
    provides: "AG-UI HTTP server, /healthz, /readyz, and graceful HTTP shutdown"
  - phase: 10-scheduler
    provides: "Joined scheduler tick, claim, dispatch, and heartbeat lifecycle"
provides:
  - "Race-safe listener, migration, scheduler-progress, and drain readiness snapshot"
  - "Concurrent PostgreSQL/Neo4j readiness probes under one two-second global deadline with stable sanitized reason codes"
  - "Synchronous listener binding and joined listener/scheduler runtime error propagation"
  - "Read-only PostgreSQL migration-at-head boot gate and completion-based scheduler progress"
  - "Semantic Compose healthcheck validation against the production /readyz contract"
affects: ["39-04 telemetry instrumentation", "39-07 observability appliance assets", "41 production operations"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Composition-root readiness snapshot for in-process lifecycle truth plus concurrent live dependency probes"
    - "Synchronous net.Listen followed by a joined Serve/Scheduler owner and ordered drain callback"
    - "Stable public readiness codes; detailed dependency errors remain internally redacted"

key-files:
  created:
    - internal/readiness/state.go
    - internal/readiness/state_test.go
    - internal/db/migration_head.go
    - cmd/aura/serve_lifecycle.go
    - cmd/aura/serve_readiness_test.go
  modified:
    - internal/agui/readiness.go
    - internal/agui/readiness_test.go
    - internal/agui/server.go
    - internal/cron/scheduler.go
    - internal/cron/scheduler_test.go
    - cmd/aura/serve.go
    - compose.yaml

key-decisions:
  - "The readiness handler shares one two-second context across all live probes and waits for every bounded probe goroutine before returning."
  - "The scheduler heartbeat means a completed successful scan, including an empty due-task scan; equality with max age remains fresh and only greater-than max age stalls."
  - "Listener and scheduler failures are lifecycle errors, not readiness as an error bus: the shared snapshot records truth while the joined owner cancels, drains, and returns the error."
  - "Serve boot checks public.schema_migrations through the migrate-role URL without applying migrations; missing, dirty, behind, or ahead state is fatal."

patterns-established:
  - "A disabled scheduler launches no ticker/scan and is healthy by configuration."
  - "Only http.ErrServerClosed after draining begins is a clean listener exit."
  - "Compose client timeout must strictly exceed the handler budget, and Compose's outer timeout must strictly exceed curl's timeout."

requirements-completed: [OBS-01, OBS-02]

coverage:
  - id: OBS-01
    description: "Serve binds synchronously and propagates unexpected listener/scheduler exits through one joined process lifecycle"
    verification:
      - kind: unit
        ref: "cmd/aura/serve_readiness_test.go#TestServeListener*"
        status: pass
      - kind: race
        ref: "go test -race ./internal/cron ./cmd/aura -run 'Test.*(Serve|Listener|Scheduler|Heartbeat|Drain)' under WSL"
        status: pass
    human_judgment: false
  - id: OBS-02
    description: "Readiness fails closed for live databases, schema head, listener, scheduler progress, and drain while liveness remains cheap"
    verification:
      - kind: unit
        ref: "internal/readiness/state_test.go and internal/agui/readiness_test.go"
        status: pass
      - kind: integration
        ref: "cmd/aura/serve_readiness_test.go#TestReadyzReflectsRuntimeLossAndRecoveryWithoutRestart"
        status: pass
      - kind: artifact
        ref: "cmd/aura/serve_readiness_test.go#TestComposeAuraReadinessHealthcheckContract and docker compose config --quiet"
        status: pass
    human_judgment: false

duration: 29min
completed: 2026-07-21
status: complete
---

# Phase 39 Plan 03: Truthful Readiness and Joined Serve Lifecycle Summary

**Aura now binds before boot success, treats listener and scheduler exits as joined lifecycle failures, and exposes bounded fail-closed readiness across PostgreSQL, Neo4j, migration head, listener, scheduler progress, and drain state.**

## Performance

- **Duration:** 29 min
- **Started:** 2026-07-21T10:25:03Z
- **Completed:** 2026-07-21T10:54:14Z
- **Tasks:** 3 TDD tasks
- **Files modified:** 12

## Accomplishments

- Added a mutex-backed runtime snapshot with monotonic listener/drain failure state, migration compatibility, scheduler enablement, exact heartbeat freshness boundaries, and deterministic sorted reason codes.
- Replaced sequential three-seconds-per-probe readiness with concurrent PostgreSQL/Neo4j checks sharing one two-second deadline; public JSON contains only stable codes while redacted driver detail stays in logs. `/healthz` no longer pings PostgreSQL in production.
- Bound the AG-UI listener synchronously, joined HTTP and scheduler exits under one cancellation owner, marked drain before ordered teardown, and treated only intentional `http.ErrServerClosed` as clean.
- Moved scheduler progress to the end of successful scans—including zero due work—and wired terminal scheduler failure into the shared snapshot without using queue depth or individual job failure as readiness.
- Added a read-only migration-at-head boot gate plus semantic Compose tests that reject `/healthz`, external hosts, absent curl bounds, and timeout budgets that do not strictly exceed the server deadline.

## Task Commits

Each TDD task was committed RED then GREEN:

1. **Task 1: Runtime readiness state and bounded aggregate evaluator**
   - RED - `255e6d28d` (test): failing lifecycle/scheduler snapshot, concurrency, deadline, sanitization, and liveness contracts.
   - GREEN - `a20eeddd4` (feat): race-safe snapshot and concurrent two-second aggregate readiness.
2. **Task 2: Joined listener and scheduler lifecycle**
   - RED - `23e58f31f` (test): failing bind, runtime listener propagation, cancellation, and scheduler completion-heartbeat contracts.
   - GREEN - `2727b1cf0` (feat): synchronous bind, joined runtime, migration-head boot gate, and completion-based scheduler progress.
3. **Task 3: Compose production readiness contract**
   - RED - `25e94fdb8` (test): failing semantic artifact validation for invalid endpoints/hosts/budgets.
   - GREEN - `308a987ac` (test): semantic YAML validation, explicit budget documentation, and live loss/recovery scenario.

## Files Created/Modified

- `internal/readiness/state.go` - concurrency-safe runtime listener, migration, scheduler, and drain truth with sorted codes.
- `internal/agui/readiness.go` - concurrent globally bounded probes and stable sanitized JSON response.
- `internal/agui/server.go` - injected runtime readiness snapshot.
- `internal/cron/scheduler.go` - successful-scan progress, disabled lifecycle behavior, and terminal failure recording.
- `internal/db/migration_head.go` - read-only embedded-head versus `schema_migrations` compatibility gate.
- `cmd/aura/serve.go` - synchronous `net.Listen`, snapshot composition, cheap liveness, and joined root lifecycle wiring.
- `cmd/aura/serve_lifecycle.go` - listener factory plus listener/scheduler cancellation, drain, and join owner.
- `cmd/aura/serve_readiness_test.go` - bind/runtime failure, intentional close, Compose semantics, and no-restart recovery coverage.
- `compose.yaml` - documented server/curl/Compose readiness budget ordering.

## Decisions Made

- A nil readiness snapshot remains neutral for isolated legacy AG-UI unit construction, while the production composition root always injects a fail-closed snapshot.
- Scheduler progress is stamped only after the entire scan, dispatched work join, and notification sweep succeed. Transient scan failure suppresses progress; terminal loop return is separately recorded and propagated.
- The root lifecycle marks `draining` before invoking the established channel-first drain callback, then hard-cancels work and joins both HTTP and scheduler components.
- The migration checker computes the embedded head dynamically from `.up.sql` files, so future migration additions do not require a duplicated version constant.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Added the live migration compatibility source**
- **Found during:** Task 2 boot composition.
- **Issue:** No existing startup migration-version check could supply the locked schema-at-head readiness state.
- **Fix:** Added `internal/db/migration_head.go` to compare the migrate-role tracker with the dynamically computed embedded head without applying migrations.
- **Files modified:** `internal/db/migration_head.go`, `cmd/aura/serve.go`.
- **Verification:** Whole-module tests, vet, build, and migration snapshot mismatch tests pass.
- **Committed in:** `2727b1cf0`.

**2. [Rule 3 - Blocking] Split lifecycle ownership out of the near-limit serve file**
- **Found during:** Task 2 synchronous bind and joined lifecycle implementation.
- **Issue:** `cmd/aura/serve.go` was already near the 600-LOC project ceiling; embedding the lifecycle owner there would violate the mandatory cap.
- **Fix:** Kept the required `net.Listen` call and composition in `serve.go`, and placed the reusable join/drain owner in `serve_lifecycle.go`.
- **Files modified:** `cmd/aura/serve.go`, `cmd/aura/serve_lifecycle.go`.
- **Verification:** `check-file-size` passes for all ten changed Go files; `serve.go` remains below 600 LOC.
- **Committed in:** `2727b1cf0`.

---

**Total deviations:** 2 auto-fixed (1 missing-critical source, 1 blocking file-size split)
**Impact on plan:** Both changes are internal implementation seams required to satisfy the locked readiness and project-size contracts; no optional dependency entered readiness.

## Issues Encountered

- Native Windows Go had `CGO_ENABLED=0` and a stale GCC path, so its race command could not start. The exact race suites ran against the same workspace under Ubuntu/WSL with Go 1.26.5 and `/usr/bin/gcc`; all passed.
- Compose already targeted loopback `/readyz` with a three-second curl timeout. Task 3 therefore strengthened the contract with semantic mutation tests and documented the existing correct budget relationship instead of changing a healthy endpoint.

## Threat Surface Scan

- Unauthenticated readiness output is bounded to `status`, `ready`, and sorted Aura-owned codes; raw driver, DSN, host, credential, migration, and listener errors never enter the response.
- PostgreSQL and Neo4j probes share one deadline and every launched goroutine is joined before handler return, preventing additive timeout and lingering-probe amplification.
- Schema inspection is read-only through the existing migrate-role URL. The serve process never auto-applies a missing migration.
- Listener bind/runtime failures and scheduler terminal failures reach process supervision; readiness state is observational, not an error transport that could be ignored.
- Compose probes only `127.0.0.1`, and semantic tests reject an external target or the liveness endpoint.

## Known Stubs

None introduced. A changed-file scan found no TODO, FIXME, HACK, panic, placeholder, or "not implemented" marker in the new Go surface. The existing `aura-memory-mcp-placeholder-key` Compose fallback is unrelated pre-existing sidecar configuration; all temporary RED compile shims were replaced in GREEN commits.

## User Setup Required

None. No new environment variables, ports, images, or external services were introduced. Existing deployments must continue running `aura db migrate` before `aura serve`; serve now verifies that contract explicitly.

## Next Phase Readiness

- Plan 39-04 can instrument the stable listener, readiness, scheduler-progress, and lifecycle failure seams without redefining readiness semantics.
- Plan 39-07 can consume the stable public reason codes in alerts/dashboards and rely on the semantic Compose timeout contract.
- No blockers.

---
*Phase: 39-idempotency-observability-pack*
*Completed: 2026-07-21*

## Self-Check: PASSED

**Files verified to exist:**
- FOUND: `internal/readiness/state.go`
- FOUND: `internal/readiness/state_test.go`
- FOUND: `internal/agui/readiness.go`
- FOUND: `internal/cron/scheduler.go`
- FOUND: `internal/db/migration_head.go`
- FOUND: `cmd/aura/serve_lifecycle.go`
- FOUND: `cmd/aura/serve_readiness_test.go`
- FOUND: `compose.yaml`

**Commits verified to exist:**
- FOUND: `255e6d28d` (Task 1 RED)
- FOUND: `a20eeddd4` (Task 1 GREEN)
- FOUND: `23e58f31f` (Task 2 RED)
- FOUND: `2727b1cf0` (Task 2 GREEN)
- FOUND: `25e94fdb8` (Task 3 RED)
- FOUND: `308a987ac` (Task 3 GREEN)

**Fresh plan-level verification:**
- Exact Task 1 targeted suite - pass.
- Exact Task 2 targeted suite - pass.
- Exact Task 3 targeted suite - pass.
- `go test -race ./internal/readiness ./internal/agui ./internal/cron ./cmd/aura` under WSL - pass.
- `go test ./...` - pass for the full module.
- `go vet ./internal/readiness ./internal/agui ./internal/cron ./cmd/aura` - pass.
- `go build ./...` - pass.
- `docker compose config --quiet` - pass.
- Project 600-LOC gate over all changed Go files - pass.
