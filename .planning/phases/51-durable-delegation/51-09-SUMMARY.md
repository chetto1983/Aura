---
phase: 51-durable-delegation
plan: 09
subsystem: swarm
tags: [swarm, delegation, postgres, rls, sandbox, docker, compose, config, go]

# Dependency graph
requires:
  - phase: 51-durable-delegation
    provides: "51-01's delegation claim loop (runChild's single event loop), 51-03's swarm_spawn cap rendering, 51-06b's runChild(ChildReport, []llm.Message) return shape, 51-10's DelegationDelivery/ConversationRecorder seam this plan's defect A fix binds identity onto"
provides:
  - "D-03's inactivity termination model: child_staleness.go reaps a worker on silence (AURA_SWARM_CHILD_IDLE_SEC), never on age; the retired AURA_SWARM_CHILD_TIMEOUT_SEC wall clock is gone from every reader"
  - "The SC#1 write's RLS identity bind (defect A): deliverSuccess and openPauseAndPark now bind job.IdentityID onto the ctx handed to DelegationDelivery.Deliver, closing the gap that made every live delegation record fail under RLS"
  - "A cancelled/timed-out shell_exec now kills its box-side process group (defect E): usersandbox.DockerBackend.Exec reuses ExecStream's own PID-file kill mechanism instead of merely detaching"
  - "compose.yaml maps AURA_LOOP_MAX_WALLCLOCK_SEC/AURA_LOOP_MAX_STEPS into the aura service (defect D), so .env's loop budget actually reaches the container -- now the background worker's real absolute ceiling under D-03"
  - "PRD Amendment #171: the D-03 live checkpoint recorded as a register of what was measured, including two findings (B: retry semantics, C: label ambiguity) left open for plan 51-08"
affects: ["51-08 (live SC#1-4 E2E gate; Finding B's retry-semantics decision; Finding C's report-text disambiguation)"]

# Actuals
actuals:
  tokens: 33000
  tasks: 3
  commits: 8

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Reused kill mechanism, not a second one: usersandbox's synchronous Exec path now shares ExecStream/Kill's PID-file process-group signal (wrapCommandWithPIDFile, killProcessGroupCommand, killBoxProcessGroup) instead of growing an independent cancellation story"
    - "Bind identity at the exact call site that needs it (identityctx.WithIdentityID(ctx, job.IdentityID) immediately before Delivery.Deliver), not upstream in processJob -- the fix stays a one-line diff at each of the two places a daemon-background ctx crosses into a per-identity RLS write"
    - "Refactor-on-touch test-file split by concern, not by size alone: delegation_queue_unit_test.go keeps config-resolver/payload-codec (pure) tests, delegation_queue_lifecycle_test.go gets the claim loop's own runtime behaviour; delegation_resume_test.go keeps pause/park, delegation_resume_observer_test.go gets DelegationResumeObserver + the runChild resume/tool-promotion proofs"

key-files:
  created:
    - internal/swarm/delegation_queue_lifecycle_test.go
    - internal/swarm/delegation_resume_observer_test.go
    - .planning/phases/51-durable-delegation/live-check/d03/RESULTS.md (+ evidence/, compose.d03.yaml, compose.bootgate.yaml, drive.sh, poll.sh, goal-long.txt, goal-stall.txt)
  modified:
    - internal/swarm/child_staleness.go (Task 1, prior to this continuation)
    - internal/swarm/swarm.go, swarm_depth.go, swarm_test.go (Task 1)
    - internal/config/config.go, config_validate.go, config_knobs.go (Task 1/2)
    - internal/agent/tools/swarm_spawn.go (Task 2)
    - internal/swarm/delegation_queue.go (defect A)
    - internal/swarm/delegation_run.go (defect A)
    - internal/swarm/delegation_queue_unit_test.go (defect A + split)
    - internal/swarm/delegation_resume_test.go (defect A + split)
    - internal/swarm/delegation_delivery_test.go, delegation_delivery_db_test.go (defect A)
    - internal/sandbox/usersandbox/docker_backend_exec.go, docker_backend_exec_test.go (defect E)
    - compose.yaml (defect D)
    - prd.md (Task 2 retirement row + Amendment #171)

key-decisions:
  - "Task 3's checkpoint was executed by the orchestrator on the operator's behalf; the outcome was 'verified WITH ISSUES', not 'approved' -- this SUMMARY records the measured verdict, not an approval"
  - "internal/runner's sibling 51-06b expiry-trace write (PoolWorkerPauseExpirer.ExpireWorkerPause) was audited for the same RLS gap defect A found and cleared: it threads identity explicitly through db.WithIdentityTx rather than reading it off ctx, so it was never exposed"
  - "Finding B (retry semantics) and Finding C (label ambiguity) are recorded, not fixed -- both are named decisions for plan 51-08 or the operator, out of this plan's own scope"

patterns-established:
  - "A live checkpoint result of 'verified WITH ISSUES' is fixed on-touch (Rule 1/2/3) in the SAME plan when the issues are inside the plan's own blast radius (the worker/delivery path this plan's Task 1/2 already touched), and recorded as findings (not silently fixed) when they are a policy decision for a later plan"

requirements-completed: [SWARM-03, SWARM-04, SWARM-09]

coverage:
  - id: D1
    description: "A worker is reaped for lack of progress, not for age: a worker emitting events every N<idle seconds runs indefinitely, a silent worker is reaped once at idle with a report naming stalled (D-03)"
    requirement: "SWARM-03"
    verification:
      - kind: unit
        ref: "internal/swarm/child_staleness_test.go (RED/GREEN, prior session)"
        status: pass
      - kind: e2e
        ref: "live-check/d03/RESULTS.md: 293.4s and 327.5s completions past the retired ~240s cap, a silent worker reaped once at 120.2s"
        status: pass
    human_judgment: false
  - id: D2
    description: "The retired AURA_SWARM_CHILD_TIMEOUT_SEC is gone from every reader/catalog and the model reads the live idle bound"
    requirement: "SWARM-09"
    verification:
      - kind: unit
        ref: "cmd/aura/distribution_artifacts_test.go#TestDistributionArtifacts, internal/agent/tools/swarm_spawn_schema_test.go (prior session)"
        status: pass
    human_judgment: false
  - id: D3
    description: "A background delegation's SC#1 report reaches aura.conversation_turns under real RLS as aura_app (defect A fixed)"
    requirement: "SWARM-03"
    verification:
      - kind: unit
        ref: "internal/swarm/delegation_queue_lifecycle_test.go#TestDeliverSuccessBindsTheJobIdentityForRLS, internal/swarm/delegation_resume_test.go#TestOpenPauseAndParkBindsTheJobIdentityForRLS"
        status: pass
      - kind: integration
        ref: "internal/swarm/delegation_delivery_db_test.go#TestDeliverSuccessRecordsUnderRealRLSAsAuraApp (db_integration, live run: PASS 2.54s against a disposable Postgres as aura_app)"
        status: pass
    human_judgment: false
  - id: D4
    description: "A cancelled/timed-out shell_exec kills its box-side process group instead of leaving it running (defect E fixed)"
    requirement: "SWARM-04"
    verification:
      - kind: unit
        ref: "internal/sandbox/usersandbox/docker_backend_exec_test.go#TestWrapCommandWithPIDFile_RecordsPIDBeforeRunningCmd, #TestKillProcessGroupCommand_SignalsBothGroupAndProcess"
        status: pass
    human_judgment: true
    rationale: "The docker-gated cancel/kill behaviour itself (a real docker top proof) is the orchestrator's own live follow-up after this plan returns, not re-verified by a second live run inside this continuation"
  - id: D5
    description: "compose.yaml maps the loop budget so .env's AURA_LOOP_MAX_WALLCLOCK_SEC/AURA_LOOP_MAX_STEPS actually reach the container (defect D fixed)"
    requirement: "SWARM-03"
    verification:
      - kind: other
        ref: "docker compose config --quiet (syntax-valid); grep -c 'AURA_LOOP_MAX_WALLCLOCK_SEC\\|AURA_LOOP_MAX_STEPS' compose.yaml >= 2"
        status: pass
    human_judgment: false

duration: ~3h (continuation session, after Task 1/2's prior session)
completed: 2026-08-29
status: complete
---

# Phase 51 Plan 09: D-03 Termination Model + Live-Check Defect Fixes Summary

**A worker is now reaped for silence, not for age (D-03), the retired wall-clock knob is gone from the tree, and the D-03 live checkpoint's own two write-path defects it found — the SC#1 report failing to record under RLS, and a reaped shell_exec leaving its process alive in the sandbox — are both fixed and proven RED-then-GREEN, with a third (a dark, unmapped loop budget) closed and the whole checkpoint recorded as PRD Amendment #171.**

## Performance

- **Duration:** ~3h for this continuation (Task 1/2 landed in the prior session; Task 3's checkpoint was executed by the orchestrator; this session fixed what it found)
- **Tasks:** 3 (Task 1 TDD termination model, Task 2 knob retirement, Task 3 checkpoint + this continuation's fix-on-touch)
- **Files modified:** 30 across the whole plan (9 in this continuation's defect fixes + 4 test-file splits + prd.md + compose.yaml + live-check evidence)

## Accomplishments

- **D-03 is live-proven, not just designed.** A worker making continuous progress completed real multi-step work at 293.4s and 327.5s — past the retired ~240s effective wall clock — and was never reaped; a silent worker was reaped exactly once, at 120.2s, with a report reading `stalled: no worker event for 2m0s`; `AURA_SWARM_CHILD_IDLE_SEC == AURA_SWARM_DELEGATION_LEASE_SEC` refused to boot, naming both knobs.
- **Defect A fixed: the SC#1 write was broken live under RLS.** `deliverSuccess` and `openPauseAndPark` called `DelegationDelivery.Deliver` on the claim loop's own daemon-background ctx, which carries no `identityctx` — the real `conversations.Store` reads `identityctx.IdentityID(ctx)` to scope its RLS transaction, so the write was invisible to itself and failed "conversation not found". Measured live: every delegation attempt's report failed to record, the queue retried the WHOLE worker (up to 5 minutes of real tool calls redone from scratch each time), and the job dead-lettered despite the underlying work having genuinely succeeded twice. Fixed by binding `identityctx.WithIdentityID(ctx, job.IdentityID)` at both call sites — a one-line fix, not a redesign. Proven RED→GREEN by daemon-free unit tests on both call sites and a `db_integration` test that reproduces the exact measured failure against a REAL Postgres connection as `aura_app`.
- **Defect E fixed: a reaped `shell_exec` left its process running in the sandbox.** `usersandbox.DockerBackend.Exec` on ctx cancellation only closed the hijacked exec stream; nothing signalled the box-side process. Measured live: `docker top` still showed the reaped attempt's `sleep 480` alive next to the retried attempt's own copy of the same command. Fixed by extracting `ExecStream`/`Kill`'s existing PID-file kill mechanism into shared pure helpers (`wrapCommandWithPIDFile`, `killProcessGroupCommand`, `killBoxProcessGroup`) and reusing it in the synchronous `Exec` path — one kill implementation, not two. Proven by daemon-free unit tests pinning both pure shell-string builders.
- **Defect D fixed: `.env`'s loop budget never reached the container.** `compose.yaml` mapped neither `AURA_LOOP_MAX_WALLCLOCK_SEC` nor `AURA_LOOP_MAX_STEPS`, so the container always ran on compiled defaults regardless of `.env`. D-03 removed the per-child wall clock, which makes this shared `agent.Budget` wallclock the background worker's real absolute ceiling now — the 327.5s live completion would have been cut at the shipped 300s default. Both vars now mapped with the shared block's `${VAR:-default}` shape.
- **Findings B and C recorded, not fixed** — both are named decisions for plan 51-08: Finding B (the shipped retry policy re-runs a reaped worker AND a record-only failure identically) and Finding C (`AURA_SWARM_CHILD_IDLE_SEC` equals `AURA_LLM_TOTAL_TIMEOUT_SEC` by default, so an upstream stall and a genuine reap read identically as `stalled` in the report).
- **PRD Amendment #171** records the whole checkpoint as a register of what was measured: the proven model, all three fixed defects with their evidence, both findings, and the perimeter (what the >4-minute completions do NOT show now that the loop budget is correctly wired).

## Task Commits

| Task | Commit | Type | What |
|---|---|---|---|
| 1 (RED) | `c4a086f96` | test | Failing tests for D-03 inactivity-based worker reaping |
| 1 (GREEN) | `cc9ea1250` | feat | Reap a worker for silence, not for age (`child_staleness.go`) |
| 2 | `34c5428e0` | feat | Retire `AURA_SWARM_CHILD_TIMEOUT_SEC` tree-wide |
| 3 checkpoint | — | — | Live verification, executed by the orchestrator; outcome "verified WITH ISSUES" (defects A, E, D; findings B, C) |
| Defect A (RED) | `39480b308` | test | Failing tests for the SC#1 delivery RLS identity gap, incl. refactor-on-touch test-file split |
| Defect A (GREEN) | `47ec4695a` | fix | Bind the job's identity onto Deliver's ctx |
| Defect E (RED) | `f4841d9aa` | test | Failing tests for the box-side process kill |
| Defect E (GREEN) | `09ffc74ff` | fix | Kill the box-side process group on `shell_exec` cancel/timeout |
| Defect D + PRD | `b84dc8e30` | fix | Map the loop budget into `compose.yaml`; record the D-03 live check as PRD Amendment #171 |

**Plan metadata:** (this commit) `docs(51-09): complete D-03 termination model + live-check defect fixes`

## Files Created/Modified

- `internal/swarm/child_staleness.go`, `child_staleness_test.go` — the inactivity deadline (Task 1, prior session)
- `internal/swarm/swarm.go`, `swarm_depth.go`, `swarm_test.go` — `runChild`'s staleness tick, `context.WithTimeout` removed (Task 1)
- `internal/config/config.go`, `config_validate.go`, `config_validate_test.go`, `config_knobs.go` — `SwarmChildIdleSec`, `GuardSwarmStaleness` boot gate (Task 1/2)
- `internal/agent/tools/swarm_spawn.go`, `swarm_spawn_schema_test.go` — rendered cap swap (Task 2)
- `.env.example`, `cmd/aura/distribution_artifacts_test.go`, `internal/gateway/classify.go`, `internal/cron/handlers/agentjob.go` — knob retirement (Task 2)
- `internal/swarm/delegation_queue.go` — `deliverSuccess` binds identity onto `Deliver`'s ctx (defect A)
- `internal/swarm/delegation_run.go` — `openPauseAndPark` binds identity onto `Deliver`'s ctx (defect A)
- `internal/swarm/delegation_queue_unit_test.go` — trimmed to config-resolver/payload-codec tests; `TestDeliverSuccessBindsTheJobIdentityForRLS` moved to the new lifecycle file
- `internal/swarm/delegation_queue_lifecycle_test.go` (new) — claim loop runtime behaviour split out (600-LOC refactor-on-touch)
- `internal/swarm/delegation_resume_test.go` — trimmed to pause/park tests; `TestOpenPauseAndParkBindsTheJobIdentityForRLS` added
- `internal/swarm/delegation_resume_observer_test.go` (new) — `DelegationResumeObserver` + runChild resume/tool-promotion tests split out (600-LOC refactor-on-touch)
- `internal/swarm/delegation_delivery_test.go` — `fakeConversationRecorder` now captures the ctx identity it observed
- `internal/swarm/delegation_delivery_db_test.go` — `TestDeliverSuccessRecordsUnderRealRLSAsAuraApp` (db_integration)
- `internal/sandbox/usersandbox/docker_backend_exec.go` — `Exec` reuses `ExecStream`'s PID-file kill mechanism on cancel/timeout (defect E)
- `internal/sandbox/usersandbox/docker_backend_exec_test.go` — pure shell-string builder tests
- `compose.yaml` — maps `AURA_LOOP_MAX_WALLCLOCK_SEC`/`AURA_LOOP_MAX_STEPS` (defect D)
- `prd.md` — Task 2's retirement row (prior session) + PRD Amendment #171 (this continuation)
- `.planning/phases/51-durable-delegation/live-check/d03/` — the live checkpoint's full evidence record (RESULTS.md, evidence/, driver scripts, compose overrides)

## Decisions Made

See `key-decisions` in frontmatter. The load-bearing one: Task 3's checkpoint result was "verified WITH ISSUES", not "approved" — this SUMMARY records the measured verdict rather than a rubber stamp, and the three defects it found were fixed in this same plan (Rule 1/2 fix-on-touch, all inside the blast radius of Task 1/2's own delivery/knob work) while the two policy-shaped findings were recorded for plan 51-08 rather than decided here.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug, found by the checkpoint] Defect A — SC#1 delivery broken live under RLS**
- **Found during:** Task 3's live checkpoint (executed by the orchestrator), origin plan 51-10 (`DelegationDelivery`) and 51-01 (`DelegationClaimLoop`)
- **Issue:** `deliverSuccess`/`openPauseAndPark` called `Deliver` on a ctx with no identity, so `conversations.Store`'s RLS-scoped write failed "conversation not found"
- **Fix:** Bind `identityctx.WithIdentityID(ctx, job.IdentityID)` at both call sites
- **Files modified:** `internal/swarm/delegation_queue.go`, `internal/swarm/delegation_run.go` + test files
- **Verification:** RED confirmed (temporary revert reproduced the exact measured error), GREEN confirmed; `db_integration` proof against real Postgres as `aura_app`
- **Committed in:** `39480b308` (RED), `47ec4695a` (GREEN)

**2. [Rule 1 - Bug, found by the checkpoint] Defect E — reaped `shell_exec` leaves its process running**
- **Found during:** Task 3's live checkpoint, origin plan 37-07 (`usersandbox.DockerBackend.Exec`)
- **Issue:** `Exec` on ctx cancellation only closed the stream, never signalled the box-side process
- **Fix:** Reused `ExecStream`/`Kill`'s existing PID-file kill mechanism in the synchronous path
- **Files modified:** `internal/sandbox/usersandbox/docker_backend_exec.go` + test file
- **Verification:** RED confirmed (temporary stub reproduced the failure), GREEN confirmed; docker-gated live proof deferred to the orchestrator's own follow-up
- **Committed in:** `f4841d9aa` (RED), `09ffc74ff` (GREEN)

**3. [Rule 3 - Blocking, found by the checkpoint] Defect D — dark loop-budget config**
- **Found during:** Task 3's live checkpoint, origin `compose.yaml`
- **Issue:** `.env`'s `AURA_LOOP_MAX_WALLCLOCK_SEC`/`AURA_LOOP_MAX_STEPS` were unmapped in `compose.yaml`, silently ignored
- **Fix:** Mapped both vars with the shared block's `${VAR:-default}` shape
- **Files modified:** `compose.yaml`, `prd.md` (catalog rows)
- **Verification:** `docker compose config --quiet` (syntax-valid)
- **Committed in:** `b84dc8e30`

---

**Total deviations:** 3 auto-fixed (all Rule 1/2/3, all found by the checkpoint and inside this plan's own blast radius), 2 findings recorded but not fixed (B, C — policy decisions for plan 51-08)
**Impact on plan:** All three fixes are on the plan's own critical path — SC#1 (D1/D3 of this SUMMARY's coverage), the sandbox's own safety invariant (no orphaned processes), and the termination model's real-world ceiling (defect D). No scope creep: every fix touches only the files the specific defect's own blast radius requires, plus its own test.

## Issues Encountered

- **Refactor-on-touch mid-RED:** adding the defect-A RLS tests pushed `delegation_queue_unit_test.go` and `delegation_resume_test.go` over CLAUDE.md's 600-LOC ceiling (616 and 619 LOC). Split each into two files by concern (pure config/payload tests vs. runtime/lifecycle behaviour) in the SAME RED commit, so every commit in the tree still compiles and stays under the cap.
- **`internal/swarm`'s own D-02 hygiene test** (`TestSwarmPackageImportsNeitherConversationsNorChannels`, a `go/parser` scan over every `*.go` file in the package directory, including test files) initially broke when the db_integration defect-A test imported `internal/conversations` directly for a real `AppendTurn` proof. Rewrote that test to prove the identical RLS fact via raw pgx + `db.WithIdentityTxRaw` (the same primitive `conversations.Store.AppendTurn` uses underneath), keeping the package's own import boundary intact.
- **What defect A's fix does NOT prove:** `internal/runner`'s sibling 51-06b expiry-trace write was audited for the same class of bug and found clean (explicit identity through `db.WithIdentityTx`, not ctx-derived) — this is a negative result, not a second fix.

## User Setup Required

None — no new env var requires operator action; `AURA_LOOP_MAX_WALLCLOCK_SEC`/`AURA_LOOP_MAX_STEPS` ship with the same defaults (300/25) they already had, now correctly wired.

## Next Phase Readiness

- Plan 51-08's live E2E gate has a durable delegation delivery path to drive that is now proven to actually land under RLS, not merely unit-tested against a fake recorder.
- Plan 51-08 (or the operator) owns Finding B's retry-semantics decision and Finding C's report-text disambiguation — both are named, evidenced, and unresolved by design.
- The docker-gated live proof of defect E's fix (re-running the stall probe + `docker top`) is the orchestrator's own follow-up after this plan returns, per the checkpoint's own resume instructions.
- **Unrelated, noticed but not touched:** `.planning/phases/51-durable-delegation/51-UX-ENVELOPE-RESEARCH.md` appeared as an untracked file during this session (a concurrent research artifact for a future gap plan, dated after this checkpoint's own operator decision). Not created by this plan, not staged, not committed here.

## Self-Check: PASSED

- `internal/swarm/child_staleness.go` — FOUND
- `internal/swarm/delegation_queue_lifecycle_test.go` — FOUND
- `internal/swarm/delegation_resume_observer_test.go` — FOUND
- `internal/sandbox/usersandbox/docker_backend_exec.go` — FOUND
- `.planning/phases/51-durable-delegation/live-check/d03/RESULTS.md` — FOUND
- Commits `c4a086f96`, `cc9ea1250`, `34c5428e0`, `39480b308`, `47ec4695a`, `f4841d9aa`, `09ffc74ff`, `b84dc8e30` — all FOUND in `git log --oneline`
- `go build ./...`, `go vet ./...` — exit 0 (WSL)
- `go test -race ./internal/swarm/ ./internal/conversations/ ./internal/runner/ ./internal/sandbox/usersandbox/ ./internal/agent/tools/ ./cmd/aura/ ./internal/config/...` — all `ok`
- `go test -tags=db_integration -race ./internal/swarm/...` as `aura_app` — `ok`, 26.4s (not a skip tell)
- `docker compose config --quiet` — exit 0

---
*Phase: 51-durable-delegation*
*Completed: 2026-08-29*
