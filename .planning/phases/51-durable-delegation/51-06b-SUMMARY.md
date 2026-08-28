---
phase: 51-durable-delegation
plan: 06b
subsystem: swarm
tags: [postgres, sqlc, migration, ingestion_jobs, paused_states, hitl, resume, swarm, delegation, golang]

# Dependency graph
requires:
  - phase: 51-01
    provides: "the aura.ingestion_jobs-backed delegation claim loop (DelegationClaimLoop, runChild as the ONE worker construction)"
  - phase: 51-06a
    provides: "pending_action_id (D-12 fence) + owning_worker_id (D-13 level identity) on aura.paused_states, MarkResumedFencedTx, ResumeClaim.ExpectActionID"
  - phase: 51-10
    provides: "DelegationDelivery (record to the origin conversation AND push where reachable) reused verbatim to surface a worker's question"
provides:
  - "aura.ingestion_jobs status 'awaiting_input' (migration 0107): non-terminal, non-claimable, a generalization of the generic queue (D-01)"
  - "Park / Unpark / Resolve conditional UPDATEs (RowsAffected==1 as the idempotency key) + ListAnswered / ListExpired reads joining paused_states on the pause TOKEN this park cycle minted"
  - "DelegationResumeState: reference-free continuation snapshot in the queue row's existing payload jsonb; NO derived tool list (deriveActivated re-grants from the seeded history)"
  - "openPauseAndPark: a claim-loop worker's AwaitingInput report opens its OWN attributed, fenced pause and parks its row in ONE transaction (swarm.PauseAndPark seam, cmd/aura delegationPauseCommitter)"
  - "DelegationResumeObserver: un-parks an answered pause's row exactly once; the SHIPPED ClaimIngestionJobs loop then claims it and runWithHeartbeat rebuilds through runChild seeded with RunConfig.ResumeTurns"
  - "(*Runner).ExpireWorkerPauses + PoolWorkerPauseExpirer: TTL sweep expiring a worker pause with an assistant trace in the origin conversation AND resolving its parked row to failed/awaiting_input_expired, in one transaction (D-08 extended to the queue row)"
affects: ["51-08 (live SC#4 scenario drives pause -> answer -> resume end to end)", "51-09 (child_staleness rides the same runWithHeartbeat)", "51-07 (transcript API reads the same worker history shape)"]

# Actuals (#2632)
actuals:
  tokens: 45887
  tasks: 3
  commits: 4

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Cross-store one-transaction composer at the composition root (delegationPauseCommitter / PoolWorkerPauseExpirer) mirroring PoolResumeCommitter: a consumer-declared seam (PauseAndPark, WorkerPauseExpirer) satisfied by a pool-owning adapter that composes Tx-bound store halves over one db.WithIdentityTx"
    - "History capture through agent.Hook (historyRecorder) as the ONLY way to read a worker's accumulated messages from outside internal/agent -- no new agent surface"
    - "Queue-side join on the pause token minted by THIS park cycle (payload->'resume'->>'pause_token'), never owning_worker_id alone, so repeated pause/resume cycles on one job stay disambiguated"
    - "Sweep read lives with the parked row (ingestion_jobs.sql), not with the pause (paused_states.sql): internal/askuser stays a dependency leaf"

key-files:
  created:
    - internal/db/migrations/0107_ingestion_jobs_awaiting_input.up.sql
    - internal/db/migrations/0107_ingestion_jobs_awaiting_input.down.sql
    - internal/documents/jobs_store_awaiting_input.go
    - internal/swarm/delegation_resume.go
    - internal/swarm/delegation_resume_test.go
    - internal/swarm/delegation_resume_db_test.go
    - internal/swarm/delegation_run.go
    - internal/runner/worker_pause_sweep.go
    - internal/runner/worker_pause_expirer.go
    - internal/runner/worker_pause_sweep_test.go
    - internal/runner/worker_pause_sweep_db_test.go
    - cmd/aura/delegation_pause_committer.go
  modified:
    - internal/db/queries/ingestion_jobs.sql
    - internal/db/sqlc/ingestion_jobs.sql.go
    - internal/db/sqlc/querier.go
    - internal/db/db_unit_test.go
    - internal/documents/jobs_store.go
    - internal/swarm/delegation_queue.go
    - internal/swarm/swarm.go
    - cmd/aura/serve_delegation.go

key-decisions:
  - "A worker pause's expiry trace is a plain ASSISTANT turn in the origin conversation, not the RoleTool answer ExpirePendingApprovals writes: the worker's ask_user tool_call lives in its own persisted history, not in the origin conversation, so a RoleTool turn keyed by it there would be an orphan (wire-invalid). Same shape plan 51-10's DelegationDelivery used to surface the question."
  - "The sweep's read joins from aura.ingestion_jobs into aura.paused_states (ListExpiredAwaitingInputJobs), keeping internal/askuser and paused_states.sql untouched by this plan and the sweep per-identity/RLS-scoped like the observer; pause.created_at is the TTL clock exactly as ListExpiredPendingApprovals measures an approval's age."
  - "PoolWorkerPauseExpirer treats a queue resolution matching zero rows AFTER a successful fenced claim as a hard error (rollback), never a skip: a pause claimed exactly once whose row is no longer parked is a state Task 1's one-transaction park makes impossible."
  - "ExpireWorkerPauses takes its Lister/Expirer per call (WorkerPauseSweepDeps) instead of new runner.Deps fields: the Runner owns HITL policy, not the ingestion queue, and threading a queue store through Deps would widen every Runner constructor and fake for one sweep."
  - "Terminal state for an expired worker pause's row is failed / awaiting_input_expired -- dead_letter keeps meaning 'retried to exhaustion'; a human declining to answer is a different outcome."
  - "The sweep rides the delegation worker's runtimeTenantIngestionProcessor container ticking at approvalExpiryInterval(ttl); ttl <= 0 yields interval 0, which runtimeIngestionWorker.Start already treats as never-start -- the shipped 'TTL <= 0 disables' precedent through an existing guard, no new knob."
  - "historyRecorder records raw res.Preview, not the model-facing nonce envelope internal/agent stores for untrusted tools (its nonce never surfaces on any hook). For tool_search -- the ONE pair the promotion mechanism is load-bearing for -- that is byte-identical (trusted tool, never wrapped); for other tools a resumed worker's replayed history lacks the untrusted-envelope framing. Documented narrower scope, not a silent gap."

patterns-established:
  - "Consumer-declared seam + composition-root one-tx adapter: swarm.PauseAndPark <- cmd/aura delegationPauseCommitter; runner.WorkerPauseExpirer <- runner.PoolWorkerPauseExpirer (+ runner.WorkerPauseQueueResolver <- cmd/aura workerPauseQueueAdapter)"
  - "Refactor-on-touch split: jobs_store.go (590 LOC) -> jobs_store_awaiting_input.go carries the whole 51-06b half; delegation_queue.go -> delegation_run.go carries runWithHeartbeat/openPauseAndPark"

requirements-completed: [SWARM-06]

coverage:
  - id: D1
    description: "A background worker's AwaitingInput report opens its OWN attributed, fenced pause and parks its queue row in ONE transaction; a parked row is claimed by neither claim loop and its attempt_count does not move"
    requirement: SWARM-06
    verification:
      - kind: integration
        ref: "internal/swarm/delegation_resume_db_test.go#TestOpenPauseAndParkAtomicity (db_integration, disposable DB as aura_app, 2.5s)"
        status: pass
      - kind: unit
        ref: "internal/swarm/delegation_resume_test.go#TestDelegationResumeObserver* + DelegationResumeState round-trip"
        status: pass
    human_judgment: false
  - id: D2
    description: "Answering the pause continues THAT worker: un-park exactly once, the shipped claim loop re-claims, runChild is re-seeded with the persisted history + the answer as the pending ask_user tool result; pre-pause tool calls run exactly once; a promoted deferred tool stays dispatchable without a second tool_search; sibling pauses are untouched; an agent_identity mismatch refuses loudly"
    requirement: SWARM-06
    verification:
      - kind: integration
        ref: "internal/swarm/delegation_resume_db_test.go#TestDelegationPauseResumeFullLifecycle (2.1s)"
        status: pass
      - kind: unit
        ref: "internal/swarm/delegation_resume_test.go (scripted-LLM continue-past-question, no-replay count, tool_search pair survives jsonb, identity mismatch refusal)"
        status: pass
    human_judgment: false
  - id: D3
    description: "An unanswered worker pause past AURA_ASKUSER_PAUSE_TTL_SEC expires with a readable assistant trace naming the worker and the question, and its parked row reaches failed/awaiting_input_expired, all in one transaction; TTL <= 0 disables; second pass expires 0; an answered pause is skipped; a failing row resolution rolls the pause claim and the trace back"
    requirement: SWARM-06
    verification:
      - kind: integration
        ref: "internal/runner/worker_pause_sweep_db_test.go#TestExpireWorkerPauses* (3 tests, db_integration)"
        status: pass
      - kind: unit
        ref: "internal/runner/worker_pause_sweep_test.go#TestExpireWorkerPauses* (3 tests)"
        status: pass
    human_judgment: false
  - id: D4
    description: "The whole SC#4 line -- a REAL background worker asks through a REAL channel, the operator answers from that channel, the worker continues and its later report reaches the operator -- driven on the running stack by the real agent"
    requirement: SWARM-06
    verification: []
    human_judgment: true
    rationale: "Plan 51-08 owns the live SC#4 drive (drive-sc.sh + 51-VALIDATION.md); this plan's proof is test-level (real Postgres, scripted LLM). CLAUDE.md DoD: a green suite alone is not the E2E verdict."

# Metrics
duration: ~4h wall-clock across two sessions (see Issues Encountered)
completed: 2026-08-28
status: complete
---

# Phase 51: Durable delegation -- Plan 06b Summary

**A background delegation worker now pauses on its own attributed, fenced ask_user, parks its queue row non-terminal and non-claimable, is rebuilt from its persisted history when the operator answers (pre-pause tool calls never re-run, promoted deferred tools come back for free), and expires with a readable trace that takes the parked row with it when nobody answers.**

## Performance

- **Duration:** ~4h wall-clock across two sessions
- **Started:** 2026-08-28T12:20Z (orphaned session, see below)
- **Completed:** 2026-08-28T19:05Z
- **Tasks:** 3
- **Files modified:** 22 (3296 insertions, 146 deletions)

## Accomplishments

- **Task 1 -- own pause + park (D-12/D-13).** `delegation_queue.go`'s `StatusNeedsUserInput` branch no longer fails the job; `openPauseAndPark` (`delegation_run.go`) mints a fresh `pending_action_id` + pause token, persists the full `DelegationResumeState` into the row's EXISTING payload jsonb (D-01: no new table), and writes pause AND park through the `PauseAndPark` seam in one `db.WithIdentityTx` (`cmd/aura/delegation_pause_committer.go`). Migration 0107 widens the generic `aura.ingestion_jobs` status CHECK with `awaiting_input`; `ClaimIngestionJobs` already restricts to queued/running, so a parked row is invisible to BOTH the delegation claim loop and the documents worker with zero query changes. The question surfaces through plan 51-10's `DelegationDelivery` -- no new delivery policy. No auto-deny at any depth (D-13); no `agent_job` reuse.
- **Task 2 -- resume continues the worker.** `DelegationResumeState` is reference-free (LibreChat's `SerializableJobData` discipline, D-00) and carries NO tool list: `NewLlmAgent` re-derives the promoted set from the seeded history (`llm_agent_construct.go:38-39`), so persisting the `tool_search` assistant `ToolCalls` entry paired with its `RoleTool` result IS the whole tool-permission story. `DelegationResumeObserver` un-parks an answered pause's row exactly once (`UnparkIngestionJob`'s conditional UPDATE); the SHIPPED claim loop re-claims it; `runWithHeartbeat` rebuilds through the SAME `runChild`, seeded via the ONE new `RunConfig.ResumeTurns` field. `runChild` now also returns the worker's reconstructed history via `historyRecorder`, an `agent.Hook` -- `git diff --stat internal/agent/` is empty. An `agent_identity` mismatch refuses before any worker is constructed (T-51-36).
- **Task 3 -- TTL sweep with a trace (D-08 extended to the queue row).** `(*Runner).ExpireWorkerPauses` (`worker_pause_sweep.go`, a sibling of `approval_expiry.go`, unchanged) lists parked jobs whose pause is older than `now - AURA_ASKUSER_PAUSE_TTL_SEC` (`ListExpiredAwaitingInputJobs`, joining `paused_states` on the pause token this park cycle minted) and hands each to `PoolWorkerPauseExpirer`, which commits the fenced claim, an ASSISTANT trace turn in the origin conversation, and the row's resolution to `failed/awaiting_input_expired` in ONE transaction. `ttl <= 0` disables; the second pass expires 0; an answered pause is skipped (`ErrPauseNotFound` from the fenced claim); a failing resolution rolls the claim and the trace back (proven live with an injected failure, then the retry pass completes the same outcome). Wired on the delegation worker's `runtimeTenantIngestionProcessor` container beside the claim loop and the observer -- one lifecycle, no second scheduler.

## Task Commits

1. **Task 1 (DB substrate, also Task 2/3's queries)** -- `2b44d8c17` (feat): migration 0107, Park/Unpark/Resolve + ListAnswered, jobs_store, migration-head pin 106 -> 107
2. **Task 1 + Task 2 (swarm + cmd/aura wiring)** -- `124d3bf51` (feat): openPauseAndPark, DelegationResumeState, observer, ResumeTurns, historyRecorder, delegationPauseCommitter, resume observer wiring. Committed as one unit because `NewDelegationClaimLoop`'s signature change makes swarm and cmd/aura the smallest compilable pair, and Task 1's state struct and Task 2's observer live in the same file.
3. **Task 3 RED** -- `0a724ab30` (test): three daemon-free sweep tests + seam types + a compiling "not implemented" stub (the pre-commit vet gate runs over the whole tree; the 45-04 precedent)
4. **Task 3 GREEN** -- `5b4969af0` (feat): ExpireWorkerPauses, PoolWorkerPauseExpirer, ListExpiredAwaitingInputJobs, jobs_store split, sweep wiring, three db_integration tests

**Plan metadata:** this SUMMARY's commit (docs: complete plan)

## Files Created/Modified

- `internal/db/migrations/0107_ingestion_jobs_awaiting_input.{up,down}.sql` -- widens the status CHECK (auto-name `ingestion_jobs_status_check` verified via `pg_constraint`; `DROP CONSTRAINT IF EXISTS`); down refuses while any row is parked
- `internal/db/queries/ingestion_jobs.sql` -- `ParkIngestionJobAwaitingInput`, `UnparkIngestionJob`, `ResolveIngestionJobAwaitingInput` (:execrows), `ListAnsweredAwaitingInputJobs`, `ListExpiredAwaitingInputJobs`
- `internal/documents/jobs_store_awaiting_input.go` -- the store's whole awaiting_input half (Park/Tx, Unpark, Resolve/Tx, ListAnswered, ListExpired); `jobs_store.go` back to 414 LOC
- `internal/swarm/delegation_resume.go` -- `DelegationResumeState`, `buildResumeTurns`, `PauseAndPark` seam, `DelegationResumeObserver`
- `internal/swarm/delegation_run.go` -- `openPauseAndPark`, `runWithHeartbeat` (moved), `delegationOperationContext` (moved)
- `internal/swarm/swarm.go` -- `RunConfig.ResumeTurns`, `runChild` returns history, `historyRecorder`, `pauseAskUserCall`
- `internal/swarm/delegation_queue.go` -- `PauseParker` field, `Resume` payload field, identity-mismatch refusal, the `StatusNeedsUserInput` branch
- `internal/runner/worker_pause_sweep.go` / `worker_pause_expirer.go` -- the sweep and its one-tx expirer
- `cmd/aura/delegation_pause_committer.go` / `serve_delegation.go` -- pause+park composer; resume observer and pause sweep on the delegation worker container; lister/queue adapters
- Tests: `delegation_resume_test.go` (590 LOC unit), `delegation_resume_db_test.go` (350), `worker_pause_sweep_test.go` (166), `worker_pause_sweep_db_test.go` (308), plus updated `swarm_test.go`, `runner_adapter_test.go`, `delegation_queue_unit_test.go`, `db_unit_test.go`

## Decisions Made

See `key-decisions` in the frontmatter. The load-bearing one: the expiry trace is an assistant turn in the origin conversation, not a RoleTool answer -- the plan's Task 3 text ("copy approval_expiry.go's shape exactly") was followed for the loop shape, but the trace shape had to differ because a worker's ask_user call is not in the origin conversation's history.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Task 3 wiring referenced code that did not exist**
- **Found during:** session recovery (see Issues Encountered)
- **Issue:** the orphaned session had wired `runner.ExpireWorkerPauses`/`WorkerPauseLister`/`WorkerPauseExpirer`/`newWorkerPauseExpirer` into `serve_delegation.go`, but no file defined them in the tree, any ref, worktree or stash -- `cmd/aura` did not compile
- **Fix:** stripped that wiring for the Task 1+2 commit so every commit compiles, then built Task 3 RED -> GREEN and re-wired it
- **Files modified:** `cmd/aura/serve_delegation.go`
- **Verification:** `go build ./...` / `go vet ./...` clean at every commit; pre-commit vet+lint hook green on all four
- **Committed in:** `124d3bf51` (strip), `5b4969af0` (re-wire)

**2. [Rule 1 - Bug] `paused_states.conversation_id` is `uuid` on the wire, not `text`**
- **Found during:** Task 3 GREEN (sqlc regeneration)
- **Issue:** the new `ListExpiredAwaitingInputJobsRow.ConversationID` is `pgtype.UUID` (a later migration retyped the column; 0003 declared `text`) -- assigning it to a string field did not compile
- **Fix:** mapped through the existing `uuidString` helper
- **Files modified:** `internal/documents/jobs_store_awaiting_input.go`
- **Committed in:** `5b4969af0`

**3. [Rule 2 - Refactor on touch] `jobs_store.go` at 590 LOC**
- **Issue:** adding `ResolveIngestionJobAwaitingInputTx` + `ListExpiredAwaitingInput` would cross the 600-LOC ceiling
- **Fix:** moved the whole 51-06b awaiting_input half verbatim into `jobs_store_awaiting_input.go` (253 LOC); `jobs_store.go` is 414
- **Committed in:** `5b4969af0`

---

**Total deviations:** 3 auto-fixed (1 blocking, 1 bug, 1 refactor-on-touch)
**Impact on plan:** none of the plan's truths or prohibitions changed; the trace-shape decision is a correction the plan's own "wire-valid" requirement forced.

## Issues Encountered

- **The plan was executed across two sessions, the first of which died.** A prior session (`milestone.lock` pid 11464, last touched 2026-08-28T13:02Z, process gone by 16:14Z) had written Tasks 1+2 and the Task 3 wiring but committed NOTHING and left ~2000 uncommitted lines plus a `deferred-items.md` note describing `internal/runner/worker_pause_sweep*.go` files and "12 passing TestExpireWorkerPauses tests" that existed nowhere. This session took the orphaned tree over on the operator's instruction ("commit atomici, piano piano, controlla tutto"): verified every claim in WSL before trusting it, committed the recoverable work in compilable units, and built Task 3 from scratch. The stale lock was left in place until the plan closed (its owner is dead; verified by PID).
- **One flake, recorded not hidden:** in one of three full-package `db_integration` runs of `internal/swarm`, `TestNudgeSkipsDrained` (a 51-10 test this plan does not touch) failed once; it passed 2/2 in isolation and on the full-package re-run. The same intermittent class the orphaned session reported for `internal/runner`'s steer auto-delivery tests; not reproduced here in `internal/runner` (one full run, green). Owner unchanged: whichever phase next touches the steer auto-delivery chain.
- **The plan's `<verify>` blocks name `TestWorkerOpensOwnPause|TestParkedRowNotClaimable|TestDelegationResumeContinuesWorker|TestUnparkExactlyOnce|TestResumeKeepsPromotedTools|TestSiblingPauseUnaffected|TestExpiredWorkerPauseResolvesQueueRow`** -- the orphaned session named its tests differently (`TestOpenPauseAndParkAtomicity`, `TestDelegationPauseResumeFullLifecycle`, `TestDelegationResumeObserver*`, `TestExpireWorkerPauses*`). The behaviours those names stand for are each asserted (see `coverage:`); the names are not, and were not renamed here to avoid churn on a green suite.

## Verification (all in WSL, the project's authoritative host)

- `go build ./...` / `go vet ./...`: clean at every commit
- `go test -race -count=1 ./internal/{swarm,documents,db,runner} ./cmd/aura`: green
- `golangci-lint run` over the touched packages: 0 issues
- `db_integration` tier on a DISPOSABLE Postgres (`aura_cov`, roles provisioned, 82 migrations applied, re-run no-op) as `aura_app`: `internal/swarm` 16.5s, `internal/runner` 6.5s, `internal/documents`, `internal/db` 38.7s -- all green; every named test shows real runtime (no sub-second skip tell)
- Plan acceptance greps: all 12 satisfied (awaiting_input x4 in the up migration; 0 auto-reject/autoDeny in `delegation_queue.go`; 0 `PromotedTools`; `ResumeTurns` present and `agent.NewLlmAgent(` absent in `delegation_resume.go`; the three :execrows never touch `attempt_count`; `func (*Runner) ExpireWorkerPauses(` present; `approval_expiry.go` and `internal/agent/` diff-empty vs `1df7ae44a`; `NewDelegationResumeObserver|ExpireWorkerPauses` x4 and exactly one `NewDelegationClaimLoop` in `cmd/aura`)
- Unit-only coverage (no tagged tier, so NOT the closing number): `internal/runner` 84.1%, `internal/swarm` 85.9%, `internal/documents` 62.6%. The 85% owned-surface aggregate gate (`scripts/coverage_docker.sh`) is a phase-close obligation (51-08), not re-run per plan.
- NOT done here: mutation spot-check on `delegation_resume.go` / `worker_pause_sweep.go` (phase-close, 51-08); the live SC#4 drive (51-08).

## User Setup Required

None -- no new env var. The sweep reuses `AURA_ASKUSER_PAUSE_TTL_SEC` (already catalogued).

## Next Phase Readiness

- SWARM-06 is code-complete: 51-06a fenced/attributed a pause; this plan opens, parks, resumes and expires one. `requirements-completed: [SWARM-06]` is asserted at test level; the live proof is 51-08's.
- 51-07 (transcript API) and 51-09 (child staleness) both read the `runChild`/`runWithHeartbeat` shape this plan settled: `runChild` returns `(ChildReport, []llm.Message)` now.
- Known edge for 51-08's drive: `Registry.DeliverToIdentity` picks the first started channel in `sort.Strings` order with no origin concept; that choice has never been exercised between two candidates (Telegram is the only `Deliverer` in the tree) -- do not describe today's behaviour as "the delivery policy".

---
*Phase: 51-durable-delegation*
*Completed: 2026-08-28*
