---
phase: 51-durable-delegation
plan: 01
subsystem: swarm
tags: [postgres, ingestion-jobs, steer, idempotency, lease-queue, sse-e2e]

requires:
  - phase: 37-mcp-governance
    provides: idempotency Operation/Scope registry, gateway.beginOperation trusted-root pattern
provides:
  - Durable swarm_spawn delegation via aura.ingestion_jobs (job_type=swarm_delegation)
  - Daemon-resident DelegationClaimLoop calling the shipped runChild through ClaimIngestionJobs
  - Trusted operation root for background workers (delegationOperationContext)
  - Per-job-type claim scoping on IngestionJobWorker (JobType field)
affects: [51-09-inactivity-watchdog, 51-10-outbox-and-conversation-turns]

actuals:
  tokens: ~38000
  tasks: 3
  commits: 8

tech-stack:
  added: []
  patterns:
    - "Background delegation reuses the existing lease-queue (aura.ingestion_jobs) rather than a new table (D-01)"
    - "A daemon-resident claim loop is a TRUSTED ROOT for idempotency, exactly like HTTP ingress and the scheduler — it must mint its own Operation, keyed on job.ID + LeaseGeneration so a reclaim never replays a dead attempt"
    - "Every typed IngestionJobWorker must set JobType explicitly once more than one typed worker polls the same table — an empty JobType claims ANY row (job_type filter is NULL, matches everything)"

key-files:
  created:
    - internal/swarm/delegation_queue.go
    - internal/swarm/delegation_queue_test.go
    - internal/swarm/delegation_idempotency_test.go
    - cmd/aura/serve_delegation.go
    - .planning/phases/51-durable-delegation/live-check/drive-delegation.sh
  modified:
    - internal/swarm/swarm.go
    - internal/swarm/runner_adapter.go
    - internal/config/config.go
    - cmd/aura/serve.go
    - cmd/aura/serve_drain.go
    - cmd/aura/main.go
    - internal/db/queries/ingestion_jobs.sql
    - internal/documents/jobs_store.go
    - internal/documents/jobs_worker.go
    - cmd/aura/asset_processing_worker.go
    - internal/idempotency/types.go
    - internal/agent/idempotency_operation.go
    - .env.example

key-decisions:
  - "Wired RunnerAdapter (the real swarm_spawn engine) to the enqueuer via ambient identityctx instead of threading IdentityID through LlmAgentConfig/SwarmContextValue — runner.go already sets it once per real turn, so no new plumbing was needed"
  - "Minted a new idempotency.ScopeSwarmDelegation trusted root in the claim loop, mirroring cron/dispatch.go's scheduledOperationContext, rather than trying to inherit a stale HTTP-ingress operation across a claim that can outlive the original request by minutes"
  - "Scoped every typed IngestionJobWorker to its own job_type at Claim time instead of building a shared dispatch table across worker instances — keeps asset-processing and delegation as independently pollable, independently deployable loops per the plan's own prohibition against merging their event loops"

requirements-completed: [SWARM-03, SWARM-09]

coverage:
  - id: D1
    description: "swarm_spawn returns before workers finish and durably enqueues one aura.ingestion_jobs row per goal (job_type=swarm_delegation)"
    requirement: "SWARM-03"
    verification:
      - kind: integration
        ref: "internal/swarm/delegation_queue_test.go#TestBackgroundDelegationEnqueues"
        status: pass
      - kind: e2e
        ref: ".planning/phases/51-durable-delegation/live-check/drive-delegation.sh (live run 2026-08-27T13:46Z, conversation 01a0430a-accb-753b-82de-8295dc230f29): first turn 10s, second turn accepted immediately on the same thread"
        status: pass
    human_judgment: false
  - id: D2
    description: "The daemon-resident claim loop claims a row through the real ClaimIngestionJobs path and runs the worker via the shipped runChild (one worker construction, one event loop)"
    requirement: "SWARM-09"
    verification:
      - kind: integration
        ref: "internal/swarm/delegation_queue_test.go#TestDelegationClaimReclaim"
        status: pass
      - kind: e2e
        ref: "live run: worker w1's shell_exec `date` executed for real (exit 0, output Thu Aug 27 11:46:17 UTC 2026), delivered via steer worker_report — proves runChild's real tool path ran inside the claim loop, not a stub"
        status: pass
    human_judgment: false
  - id: D3
    description: "A reclaimed/dead-lettered lease never double-runs; job_type isolation between the asset-processing worker and the delegation claim loop"
    requirement: "SWARM-09"
    verification:
      - kind: integration
        ref: "internal/swarm/delegation_queue_test.go#TestDelegationClaimReclaim, #TestDelegationDeadLettersAtMaxAttempts"
        status: pass
      - kind: unit
        ref: "internal/documents/jobs_worker_test.go#TestIngestionJobWorkerScopesClaimToItsOwnJobType"
        status: pass
      - kind: e2e
        ref: "live run 2026-08-27T13:46Z: both delegation rows (51cfc38b, a490f28a) reached succeeded, zero dead_letter — the prior run's cross-worker steal (7e87c390 dead_letter/handler_missing) did not recur"
        status: pass
    human_judgment: false
  - id: D4
    description: "The consolidated worker report re-enters the conversation via the steer rail under steer.SourceWorker"
    requirement: "SWARM-09"
    verification:
      - kind: e2e
        ref: "stream2.sse CUSTOM aura.steer frame: envelope=worker_report, source=swarm, text carries goal_index/child_id/status/summary"
        status: pass
    human_judgment: false

duration: ~5h (across interruption/resume)
completed: 2026-08-27
status: complete
---

# Phase 51 Plan 01: Durable Delegation Tracer Summary

**A background `swarm_spawn` call durably enqueues to `aura.ingestion_jobs`, returns the parent turn immediately, and a daemon-resident claim loop runs the worker through the real `ClaimIngestionJobs` → `runChild` path — proven live, including two real bugs the live driver (not the unit suite) caught and fixed.**

## Performance

- **Tasks:** 3 (enqueue+claim loop, daemon wiring, live tracer verification)
- **Files modified:** 19 (13 source, 2 test-only, 1 new phase artifact, 1 gitignore, 2 config)

## Accomplishments
- `swarm_spawn` at the top of the tool tree (`RunnerAdapter.Run`, `swarm.Run` at depth ≤1) now enqueues one `aura.ingestion_jobs` row per goal instead of running goals inline, and returns a model-readable "queued" result immediately.
- A `DelegationClaimLoop` runs inside the `aura` daemon (`cmd/aura/serve_delegation.go`, started/stopped alongside the other durable workers), claims due rows via the real `ClaimIngestionJobs` SQL path, and calls the SAME `runChild` the interactive swarm path uses — one worker construction, one event loop, per the plan's explicit prohibition against a second clone.
- The claim loop mints its own trusted `idempotency.Operation` root (`ScopeSwarmDelegation`, keyed on `job.ID + ":" + LeaseGeneration`) so every mutating tool call inside the worker is authorized, and a reclaimed/retried attempt never replays a dead attempt's stale result.
- The consolidated worker report is delivered back into the live conversation via the existing steer rail (`envelope=worker_report`, `source=swarm`), proven live in the SSE transcript.
- Live-verified end-to-end twice after fixing two real defects the driver — not the unit suite — surfaced (see Deviations).

## Task Commits

Each task was committed atomically (spanning this and the prior session before interruption):

1. **Task 1: Enqueue + claim/reclaim loop** - `d5b14b2b8` (feat)
2. **Task 2: Wire into daemon composition root** - `d18355c4c` (feat)
3. **Task 3 (part): Real swarm_spawn wiring closure** - `3fcaf7708` (feat) — closed the checkpoint gap (RunnerAdapter had no path to the enqueuer)
4. **Task 3 (part): Trusted operation root bug fix** - `39b8a3476` (fix) — "operation context missing" denied every worker tool call
5. **Task 3 (part): Job-type claim race bug fix** - `b6017ed3a` (fix) — asset-processing worker was stealing `swarm_delegation` rows and dead-lettering them
6. **Task 3 (part): Driver script as phase evidence** - `f08e53c59` (test)

**Plan metadata:** (this commit) `docs(51-01): complete durable delegation tracer plan`

_Note: Tasks 1 and 2 were committed in a prior session before this run was interrupted and resumed; hashes verified present in `git log` at resume time._

## Files Created/Modified
- `internal/swarm/delegation_queue.go` - `DelegationEnqueuer`, `DelegationClaimLoop`, `delegationOperationContext` (trusted-root minting)
- `internal/swarm/swarm.go` - `Run()` branches to `EnqueueDelegation` when an `Enqueuer` is configured and depth ≤1
- `internal/swarm/runner_adapter.go` - reads `identityctx.IdentityID(ctx)` ambiently, no new config plumbing
- `cmd/aura/serve_delegation.go` - constructs the delegation worker, reusing `runtimeTenantIngestionProcessor`
- `cmd/aura/main.go` - wires `swarmAdapter.Enqueuer` when a task-store pool exists
- `internal/db/queries/ingestion_jobs.sql` + regenerated sqlc - optional `job_type` filter on `ClaimIngestionJobs`
- `internal/documents/jobs_worker.go` - new `IngestionJobWorker.JobType` field, threaded into the claim request
- `cmd/aura/asset_processing_worker.go` - scoped to `JobType: assetProcessJobType`
- `internal/idempotency/types.go`, `internal/agent/idempotency_operation.go` - new `ScopeSwarmDelegation` trusted-root scope
- `.planning/phases/51-durable-delegation/live-check/drive-delegation.sh` - live E2E driver, modelled on spike 098's `drive.sh`

## Decisions Made
- Ambient `identityctx` was sufficient to close the `RunnerAdapter` wiring gap — no architectural change (Rule 4) was needed, contrary to what the checkpoint initially flagged as a possible concern.
- The claim loop is a NEW kind of trusted idempotency root (alongside HTTP ingress and the scheduler), not a derived child — this is a durable pattern future daemon-resident loops should follow.
- `IngestionJobWorker.JobType` defaults to empty (claims any type) for backward compatibility; only workers sharing the table with another typed worker need to set it. Documented in-code as of this fix.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] `ClaimIngestionJobs` had no job_type filter**
- **Found during:** Task 1
- **Issue:** The shipped claim query claimed ANY due row for an identity regardless of type — a delegation claim loop and the document-ingestion worker would steal each other's leases.
- **Fix:** Added an optional `sqlc.narg('job_type')` filter to the claim CTE. Zero behavior change for callers that don't set it (until deviation #4 below).
- **Files modified:** `internal/db/queries/ingestion_jobs.sql`, regenerated sqlc, `internal/documents/jobs_store.go`
- **Verification:** `TestDelegationClaimReclaim` etc.
- **Committed in:** `d5b14b2b8` (Task 1 commit)

**2. [Rule 4→resolved without architecture change] `RunnerAdapter` never wired to the enqueuer**
- **Found during:** Task 3 checkpoint (raised to the user before proceeding)
- **Issue:** The real `swarm_spawn` tool's engine adapter had no identity or enqueuer, so a live call would still run goals inline instead of enqueuing.
- **Fix:** `RunnerAdapter.Run` reads `identityctx.IdentityID(ctx)` (already set once per real turn by `runner.go:337`) and passes an `Enqueuer` field wired from `cmd/aura/main.go` when a task-store pool exists.
- **Files modified:** `internal/swarm/runner_adapter.go`, `internal/swarm/runner_adapter_test.go`, `cmd/aura/main.go`
- **Verification:** `TestRunnerAdapterBackgroundsWhenEnqueuerConfigured`; live driver run 1 (18s/15s turns).
- **Committed in:** `3fcaf7708`

**3. [Rule 1 - Bug] Every worker tool call denied with "operation context missing"**
- **Found during:** Task 3 live verification (first driver run)
- **Issue:** `deriveToolOperationContext` only derives a CHILD operation from an existing PARENT on ctx; the claim loop's background ctx had no parent minted, unlike HTTP ingress and the scheduler which each mint their own trusted root. Every `shell_exec` inside a claimed worker was denied, 100% deterministic.
- **Fix:** Added `idempotency.ScopeSwarmDelegation` as a registered trusted scope, admitted it as a valid parent scope in `deriveToolOperationContext`, and minted the root in `delegationOperationContext` keyed on `job.ID + ":" + LeaseGeneration`.
- **Files modified:** `internal/idempotency/types.go`, `internal/agent/idempotency_operation.go`, `internal/swarm/delegation_queue.go`
- **Verification:** `TestDelegationOperationContextMintsTrustedRoot`; live driver run 2 confirmed `shell_exec` actually executed (`date` → exit 0) and the report reached the conversation via steer.
- **Committed in:** `39b8a3476`

**4. [Rule 1 - Bug] Cross-worker job_type race — delegation rows stolen and dead-lettered**
- **Found during:** Task 3 live verification (second driver run)
- **Issue:** `IngestionJobWorker` never set `JobType` on its claim request; an empty `JobType` maps to SQL `NULL`, and the deviation-#1 filter is `job_type IS NULL OR job_type = ...` — which matches EVERY row when the filter arg itself is NULL. The pre-existing asset-processing worker (`Handlers={asset_process}` only) could therefore win the race for a `swarm_delegation` row and immediately dead-letter it with `handler_missing` since it has no handler for that type. Observed live: one of two enqueued delegation jobs reached `dead_letter`/`handler_missing` while its sibling succeeded.
- **Fix:** Added `JobType string` to `IngestionJobWorker`, threaded it into the `Claim` request, and set it to `assetProcessJobType` on the asset-processing worker constructor so each typed worker only ever competes for its own rows.
- **Files modified:** `internal/documents/jobs_worker.go`, `internal/documents/jobs_worker_test.go`, `cmd/aura/asset_processing_worker.go`
- **Verification:** `TestIngestionJobWorkerScopesClaimToItsOwnJobType`; live driver run 3 (post-rebuild): both delegation rows reached `succeeded`, zero dead letters.
- **Committed in:** `b6017ed3a`

---

**Total deviations:** 4 auto-fixed (1 missing-critical, 1 wiring-closure, 2 bugs — both found ONLY by driving the live agent, not by the unit suite)
**Impact on plan:** All four were on SC#1's critical path — without #3 and #4 specifically, the tracer's own acceptance criteria ("worker runs via the real path", "never cross-claims") would have been false despite every unit test passing. No scope creep: no fix touched files outside this plan's `files_modified` set except the two files opened to correct the newly-shipped race (`jobs_worker.go`, `asset_processing_worker.go`), which are the direct blast radius of adding a second typed claimant to a queue that previously had only one.

## Issues Encountered
- **Docker image freshness ambiguity** (twice): `docker image inspect --format '{{.Created}}'` timestamps are unreliable under BuildKit content-addressable caching. Resolved both times by content-grepping the running binary for strings unique to the new code (e.g. `swarm.delegation`, `asset_process`) rather than trusting metadata.
- **Netns-orphaned sidecars**: recreating `aura` twice (once per bug fix + rebuild) required recreating `arcadedb-mcp`, `whatsapp`, `aura-pim-mcp` each time — done via `docker compose up -d --force-recreate` on the three sidecar services.
- **Second live turn hit `RUN_ERROR`** ("context deadline exceeded" calling OpenRouter) on the third and final driver run, AFTER the steer report had already been delivered and answered in-text. This is an OpenRouter upstream latency issue, unrelated to the delegation queue — out of scope per the scope-boundary rule, not fixed, not blocking SC#1 (the delegation mechanics it was checking had already both succeeded by that point in the transcript).
- The ~240s `runChild` termination bound (D-03) is an intentionally INTERIM value per the plan's own must-haves; not touched here (plan 51-09's work).
- `aura.conversation_turns` was not checked for an out-of-band write in this run — by design, since the durable/absent-operator outbox leg is explicitly plan 51-10's scope, not this tracer's (per the plan's own must-have: "NOT claimed here").

## User Setup Required
None - no external service configuration required. `AURA_SWARM_DELEGATION_LEASE_SEC` / `AURA_SWARM_DELEGATION_POLL_SEC` ship with defaults (300s / 2s) in `.env.example`.

## Next Phase Readiness
- SC#1's present-operator leg is proven live end-to-end: enqueue → background return → real claim/run/steer round-trip, including two bugs closed that would otherwise have silently broken the exact acceptance criteria this plan claims.
- Plan 51-09 (inactivity watchdog) can build on `runChild`'s shared event loop without needing a second bound to reconcile.
- Plan 51-10 (durable outbox + `aura.conversation_turns` for the absent-operator leg) has an unmodified `aura.ingestion_jobs`/steer integration point to build against.
- `IngestionJobWorker.JobType` is now a documented, tested pattern any future typed worker sharing this queue must set.

## Self-Check: PASSED
- `internal/swarm/delegation_queue.go` — FOUND
- `internal/swarm/delegation_queue_test.go` — FOUND
- `internal/swarm/delegation_idempotency_test.go` — FOUND
- `cmd/aura/serve_delegation.go` — FOUND
- `.planning/phases/51-durable-delegation/live-check/drive-delegation.sh` — FOUND
- Commits `d5b14b2b8`, `d18355c4c`, `3fcaf7708`, `39b8a3476`, `b6017ed3a`, `f08e53c59` — all FOUND in `git log --oneline`

---
*Phase: 51-durable-delegation*
*Completed: 2026-08-27*
