---
phase: 51-durable-delegation
plan: 10
subsystem: swarm
tags: [postgres, conversations, steer, channels, delegation, go, migration, sqlc]

# Dependency graph
requires:
  - phase: 51-durable-delegation
    provides: "51-01's delegation claim loop (the terminal-success branch this plan hooks) and 51-02's durable steer_queue with drained_at/nudged_at columns"
provides:
  - "The SC#1 write: a finished background delegation's consolidated report lands in aura.conversation_turns as an out-of-band assistant turn, whether or not an operator turn is running"
  - "swarm.DelegationDelivery — the single delivery concern (record, present-operator push, absent-operator nudge) delegation_queue.go's deliverSuccess now routes through"
  - "The absent-operator channel nudge: an undrained delegation_result row gets pushed to its owning channel exactly once, after a configurable grace window (AURA_SWARM_DELEGATION_NUDGE_SEC)"
  - "Migration 0105 — aura.pending_notifications widened to accept an aura.steer_queue owner alongside its existing aura.agent_job_runs owner, closing a real FK gap the plan's own action text did not anticipate"
affects: [51-06b, 51-08, 51-09]

# Actuals
actuals:
  tokens: 23097
  tasks: 2
  commits: 3

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Claim-before-push for a periodic sweep: MarkSteerRowNudged's conditional UPDATE (WHERE nudged_at IS NULL) is executed BEFORE the channel push, not after — the same 'the drain IS the claim' idiom DrainSteerRows already established, generalized to a sweep that cannot rely on a single FOR UPDATE transaction spanning an external network call"
    - "Two separate attribution envelopes for two separate readers: the recorded copy (aura.conversation_turns, read back into future prompts) gets attributedWorkerReport's own wrapper naming the worker and goal; the pushed copy (steer_queue, read once at drain time) stays raw and earns its attribution downstream from the existing markSteer/wrapUntrustedToolOutput envelope — never one shared wrapping for two different trust contexts"
    - "A second table owner via a nullable sibling FK column plus a CHECK requiring exactly one, rather than widening the first FK's target or adding a new table — generalizes 'audit-forever, cleaned up by its owner's cascade' instead of relaxing it"

key-files:
  created:
    - internal/swarm/delegation_delivery.go
    - internal/swarm/delegation_delivery_test.go
    - internal/swarm/delegation_delivery_db_test.go
    - internal/db/migrations/0105_pending_notifications_steer_queue.up.sql
    - internal/db/migrations/0105_pending_notifications_steer_queue.down.sql
  modified:
    - internal/swarm/delegation_queue.go
    - internal/swarm/delegation_queue_unit_test.go
    - cmd/aura/serve_delegation.go
    - cmd/aura/serve.go
    - internal/steer/pg_store.go
    - internal/config/config.go
    - internal/config/config_knobs.go
    - internal/cron/store_runs.go
    - internal/cron/store_notifications_fake_test.go
    - internal/db/queries/pending_notifications.sql
    - internal/db/queries/steer_queue.sql
    - internal/db/sqlc/models.go
    - internal/db/sqlc/pending_notifications.sql.go
    - internal/db/sqlc/querier.go
    - internal/db/sqlc/steer_queue.sql.go
    - internal/db/db_unit_test.go
    - .env.example

key-decisions:
  - "Widened aura.pending_notifications (migration 0105) with a nullable steer_queue_id sibling to run_id, rather than reusing agent_job_runs' FK or building a second outbox table — the row the absent-operator leg retries is a steer_queue row, not the (by-then-already-succeeded) delegation job"
  - "Claim (MarkSteerRowNudged) runs BEFORE the channel push in NudgeUndrained, not after — a bare SELECT-then-push-then-mark would let two concurrent sweep passes both observe a row as unclaimed and both push before either commits; proven live under real concurrency (TestNudgeOnceUnderConcurrency)"
  - "The recorded conversation copy is wrapped with an explicit worker/goal attribution (attributedWorkerReport); the pushed steer copy is left exactly as 51-01 shipped it, since it already earns its own attribution downstream at drain time — one shared wrapper would have been wrong for either reader"
  - "steer.PostgresStore's two new nudge methods return a steer-local row type (UnnudgedDelegationResult), never swarm.UndrainedResult directly — internal/steer must not import internal/swarm (which already imports internal/steer for steer.SourceWorker), so the translation lives in a cmd/aura adapter, not a shared type"

patterns-established:
  - "Threat-model mitigation as a named, isolated test (TestDeliveryAttributesTheRecordedCopy for T-51-38), not folded into a happy-path assertion — a regression in attribution fails with a name that points straight at the threat register row"

requirements-completed: [SWARM-03, SWARM-09]

coverage:
  - id: D1
    description: "A finished background delegation's report lands in aura.conversation_turns as an out-of-band assistant turn, independent of whether an operator turn is running (SC#1)"
    requirement: "SWARM-03"
    verification:
      - kind: unit
        ref: "internal/swarm/delegation_delivery_test.go#TestDeliveryRecordsBeforePush"
        status: pass
      - kind: unit
        ref: "internal/swarm/delegation_queue_unit_test.go#TestDeliverSuccessPushesBeforeTransitioning"
        status: pass
    human_judgment: true
    rationale: "Live proof that the row is genuinely queryable via psql on a running stack belongs to plan 51-08's E2E gate, not this plan's daemon-free/db_integration tiers"
  - id: D2
    description: "The present-operator steer push (D-04) is unchanged from 51-01: pushed under steer.SourceWorker, unwrapped, still the mid-turn rail"
    requirement: "SWARM-03"
    verification:
      - kind: unit
        ref: "internal/swarm/delegation_delivery_test.go#TestDeliveryRecordsBeforePush"
        status: pass
    human_judgment: false
  - id: D3
    description: "A record failure (WARN, not a hard error) suppresses the job's succeeded transition and routes to the shipped attempt_count/next_attempt_at retry backoff instead"
    requirement: "SWARM-03"
    verification:
      - kind: unit
        ref: "internal/swarm/delegation_queue_unit_test.go#TestRecordFailureBlocksSucceeded"
        status: pass
    human_judgment: false
  - id: D4
    description: "The recorded conversation copy is attributed to the worker and its goal, never written as Aura's own unqualified words (T-51-38)"
    requirement: "SWARM-03"
    verification:
      - kind: unit
        ref: "internal/swarm/delegation_delivery_test.go#TestDeliveryAttributesTheRecordedCopy"
        status: pass
    human_judgment: false
  - id: D5
    description: "An undrained delegation_result row is nudged to its owning channel exactly once, per the shipped tri-state (delivered / nobody-owns / owns-but-failed), after AURA_SWARM_DELEGATION_NUDGE_SEC; a drained row is never nudged; <=0 disables the leg"
    requirement: "SWARM-09"
    verification:
      - kind: unit
        ref: "internal/swarm/delegation_delivery_test.go#TestNudgeUndrainedTriState"
        status: pass
      - kind: unit
        ref: "internal/swarm/delegation_delivery_test.go#TestNudgeUndrainedDisabledWhenNudgeAfterNonPositive"
        status: pass
      - kind: integration
        ref: "internal/swarm/delegation_delivery_db_test.go#TestNudgeSkipsDrained (db_integration, live run: PASS 2.84s against a disposable Postgres)"
        status: pass
    human_judgment: false
  - id: D6
    description: "Two concurrent sweep passes over the SAME undrained row push at most once (claim-before-push, the conditional-UPDATE idempotency key)"
    requirement: "SWARM-09"
    verification:
      - kind: integration
        ref: "internal/swarm/delegation_delivery_db_test.go#TestNudgeOnceUnderConcurrency (db_integration, live run: PASS 2.62s against a disposable Postgres, real goroutine race)"
        status: pass
    human_judgment: false
  - id: D7
    description: "internal/swarm gains no import edge into internal/conversations or internal/channels (D-02's closed shape); no agent_message_send or messaging schema was built"
    requirement: "SWARM-03"
    verification:
      - kind: unit
        ref: "internal/swarm/delegation_delivery_test.go#TestSwarmPackageImportsNeitherConversationsNorChannels"
        status: pass
    human_judgment: false

duration: ~1h30m
completed: 2026-08-28
status: complete
---

# Phase 51 Plan 10: Durable Delegation Delivery Summary

**A background delegation's consolidated report now appends to its origin conversation as an out-of-band assistant turn the moment the work finishes — `swarm.DelegationDelivery.Deliver` records before it pushes, gates the job's `succeeded` transition on the record having actually succeeded, and attributes the recorded copy to the worker; `NudgeUndrained` then claims-before-pushes an undrained report to its owning channel exactly once after a configurable grace window, reusing the shipped `pending_notifications` outbox rather than inventing a second one.**

## Performance

- **Duration:** ~1h30m (spanning a mid-session interruption/resume)
- **Tasks:** 2 completed (Task 1: SC#1 conversation record; Task 2: absent-operator channel nudge)
- **Files modified:** 22 (5 created, 17 modified)

## Accomplishments

- SC#1 is now literally checkable with `psql`: `DelegationDelivery.Deliver` appends the consolidated worker report to `aura.conversation_turns` via a `ConversationRecorder` seam declared in `internal/swarm` and adapted onto `*conversations.Store` at `cmd/aura/serve_delegation.go` (`Seq 0`, the same out-of-band append pattern `serve_dispatch.go`'s scheduler recorder uses) — no operator turn required.
- The recorded copy is attributed to the delegated worker and its goal (T-51-38), closing a real threat-register gap the first implementation pass missed: writing the raw worker JSON as an unqualified assistant turn would have let a later turn mistake worker output for Aura's own conclusion.
- The present-operator steer push (D-04, plan 51-01) is byte-for-byte unchanged — still raw, still `steer.SourceWorker`, still earning its own attribution downstream at drain time via the existing `markSteer` envelope.
- `deliverSuccess` (in `delegation_queue.go`) now gates the job's `succeeded` transition on the conversation record having actually succeeded — a record failure (a WARN, never a hard error) routes to the shipped `attempt_count`/`next_attempt_at` retry backoff instead of silently marking a lost report as delivered.
- The absent-operator leg (`NudgeUndrained`) sweeps `aura.steer_queue` for `delegation_result` rows the operator never drained past `AURA_SWARM_DELEGATION_NUDGE_SEC`, claims each one BEFORE pushing (the same "the drain IS the claim" idiom `DrainSteerRows` already established, generalized to a conditional UPDATE since a sweep spans a network call and cannot hold one transaction open across it), and routes the shipped `cron`-style tri-state: delivered or nobody-owns both stop (the conversation record IS the delivery when there is no per-task route to fall back to); owns-but-failed queues a `pending_notifications` retry row and lets that store's own sweep/backoff own the rest.
- The nudge rides the SAME `runtimeProcessingWorkers` container the claim loop already uses (`cmd/aura/serve_delegation.go`) — no second scheduler.
- Both DB-dependent claims (a drained row is never nudged; two concurrent passes push at most once) are proven against a REAL disposable Postgres instance, not asserted against a fake.

## Task Commits

| Task | Commit | Type | What |
|---|---|---|---|
| Blocking-issue fix (discovered mid-Task-2, Rule 3) | `c17823f08` | fix | Migration 0105 — widened `aura.pending_notifications` to accept an `aura.steer_queue` owner; the shipped table's `run_id NOT NULL REFERENCES aura.agent_job_runs` made every nudge-retry insert fail its FK |
| Task 1 + Task 2 (RED) | `46736c038` | test | Failing tests for the SC#1 delivery and absent-operator nudge, against a stubbed `Deliver`/`NudgeUndrained`; the whole repo — schema, config, wiring, adapters — was already real, only the two functions under test were no-ops |
| Task 1 + Task 2 (GREEN) | `66e79fcd6` | feat | The real `Deliver` and `NudgeUndrained` implementations, including the T-51-38 attribution wrapper |

**Plan metadata:** (this commit) `docs(51-10): complete durable delegation delivery plan`

## Files Created/Modified

- `internal/swarm/delegation_delivery.go` — `ConversationRecorder`, `ChannelDeliverer`, `SteerNudgeStore`, `PendingNotificationStore` interfaces; `DelegationDelivery` struct; `Deliver`, `NudgeUndrained`, `nudgeOne`, `attributedWorkerReport`
- `internal/swarm/delegation_delivery_test.go` — daemon-free coverage: record-before-push, empty-report, no-recorder wiring error, push-failure-is-hard-error, attribution, import-hygiene, nudge tri-state, nudge-disabled, nudge-no-collaborators
- `internal/swarm/delegation_delivery_db_test.go` — `db_integration`: `TestNudgeSkipsDrained`, `TestNudgeOnceUnderConcurrency`, both live-run against a disposable Postgres
- `internal/swarm/delegation_queue.go` — `DelegationClaimLoop.Steer` → `Delivery *DelegationDelivery`; `deliverSuccess` routes through `Deliver` and gates the transition on `recorded`
- `internal/swarm/delegation_queue_unit_test.go` — updated `deliverSuccess`/constructor tests for the new seam; added `TestRecordFailureBlocksSucceeded`
- `cmd/aura/serve_delegation.go` — `delegationConversationRecorder`, `steerNudgeAdapter`, `delegationPendingNotifier`, `delegationNudgeProcessor`, `newDelegationDelivery`; the nudge sweep joins the existing `runtimeProcessingWorkers` container
- `cmd/aura/serve.go` — wires `newDelegationDelivery(chat, store, reg)` in place of the old bare `swarm.SteerPublisher` plumbing
- `internal/steer/pg_store.go` — `ListUnnudgedDelegationResults`, `MarkSteerRowNudged` on `*PostgresStore`
- `internal/config/config.go`, `internal/config/config_knobs.go`, `.env.example` — `SwarmDelegationNudgeSec` / `AURA_SWARM_DELEGATION_NUDGE_SEC` (default 60)
- `internal/cron/store_runs.go`, `internal/cron/store_notifications_fake_test.go` — `InsertPendingNotificationParams.SteerQueueID`, exactly-one-owner Go-side validation mirroring the new DB CHECK
- `internal/db/migrations/0105_pending_notifications_steer_queue.{up,down}.sql`, `internal/db/queries/pending_notifications.sql`, `internal/db/queries/steer_queue.sql`, regenerated `internal/db/sqlc/*` — the schema fix plus the two new steer_queue nudge queries
- `internal/db/db_unit_test.go` — bumped the deliberate `MigrationHead` pin (104 → 105)

## Decisions Made

See `key-decisions` in frontmatter. The most consequential: migration 0105 exists because the plan's own action text assumed `internal/cron`'s shipped `pending_notifications` store method could be reused as-is for the absent-operator retry leg — reading the schema first (CLAUDE.md's own rule) showed the table's `run_id` FK made that impossible without a change, and the row that actually needs retrying (a `steer_queue` row) is a different kind of owner than the table was built for (an `agent_job_runs` row).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `aura.pending_notifications.run_id` FK made the plan's own reuse instruction impossible**
- **Found during:** Task 2, before writing any nudge code — read the schema per CLAUDE.md's "read the documentation first" rule ahead of designing the owns-but-failed retry path
- **Issue:** The plan's action text says "Insert a `pending_notifications` row ONLY on owns-but-failed, reusing the shipped store method rather than writing a second outbox." The shipped table has `run_id uuid NOT NULL REFERENCES aura.agent_job_runs(id)`. A delegation's row lives in `aura.ingestion_jobs`, and by the time a nudge fires the delegation job is already `succeeded` — so neither the job nor a scheduler run is the right owner for a retry keyed on the STEER row being delivered. Every insert from the nudge sweep would violate the FK, deterministically.
- **Fix:** Migration 0105 makes `run_id` nullable and adds a nullable `steer_queue_id` sibling (`ON DELETE CASCADE` from `aura.steer_queue`), with a `CHECK (run_id IS NOT NULL OR steer_queue_id IS NOT NULL)` — generalizing the table's existing "audit-forever, cleaned up by its owner's cascade" invariant to a second owner kind instead of relaxing it. Every existing `cron` caller is byte-for-byte unchanged. `cron.Store.InsertPendingNotification` mirrors the CHECK in Go (exactly-one-of `RunID`/`SteerQueueID`) so a wiring bug returns a named error before the round trip rather than an opaque constraint violation.
- **Files modified:** `internal/db/migrations/0105_pending_notifications_steer_queue.{up,down}.sql`, `internal/db/queries/pending_notifications.sql`, regenerated sqlc, `internal/cron/store_runs.go`, `internal/cron/store_notifications_fake_test.go`, `internal/db/db_unit_test.go`
- **Verification:** `go test ./internal/cron/... ./internal/db/...` green; a standalone throwaway round-trip check (migrate up → down 1 step → up 1 step → re-migrate no-op) passed against a disposable Postgres database and was then deleted (not part of the deliverable)
- **Committed in:** `c17823f08`

**2. [Rule 2 - Missing Critical] Recorded conversation copy had no attribution wrapper (T-51-38)**
- **Found during:** Task 1, threat-model review before writing the SUMMARY — the plan's own threat register (T-51-38, disposition `mitigate`) requires the recorded text to be "wrapped as an attributed worker report... never as Aura's own unqualified words." The first implementation pass wrote the raw `marshalReports` JSON directly as the assistant turn's content, with no framing beyond its own `child_id`/`status` keys.
- **Issue:** A later turn re-reading `aura.conversation_turns` as prompt history would have had no explicit signal that this content originated from a worker rather than from Aura itself — exactly the spoofing/elevation-of-privilege shape T-51-38 names.
- **Fix:** Added `attributedWorkerReport(goal, text)`, applied only to the RECORDED copy (never the pushed steer copy, which already earns its own attribution downstream at drain time from the existing `markSteer`/`wrapUntrustedToolOutput` envelope).
- **Files modified:** `internal/swarm/delegation_delivery.go`, `internal/swarm/delegation_delivery_test.go`
- **Verification:** `TestDeliveryAttributesTheRecordedCopy` (new, isolated per this deviation), `TestDeliveryRecordsBeforePush` (updated to assert the pushed copy stays raw while the recorded copy carries both the goal and the report)
- **Committed in:** `66e79fcd6` (the GREEN commit — the attribution wrapper is part of `Deliver`'s real implementation, not a follow-up patch)

---

**Total deviations:** 2 auto-fixed (1 blocking/schema, 1 missing-critical/security)
**Impact on plan:** Both were on SC#1's and T-51-38's own critical path. Without #1, Task 2's acceptance criteria (a `pending_notifications` row genuinely inserted on owns-but-failed) would have been impossible to satisfy at all — every insert would fail the FK. Without #2, the plan's own threat register would have shipped un-mitigated despite claiming `mitigate`. No scope creep: the migration touches only the one column/constraint the fix needs, and the attribution change touches only the recorded-copy code path.

## Issues Encountered

- **Session interruption mid-plan.** This execution was interrupted by a transient API `server_error` partway through Task 2 (after the schema-fix commit, before the RED/GREEN test commits) and resumed in a fresh turn. The orchestrator verified the tree state (build clean, exact break point identified) before resume; no work was lost, but the plan's own duration figure spans the gap.
- **`go test -race` could not run on this host.** This is a Windows session without a cgo toolchain (`go: -race requires cgo; enable cgo by setting CGO_ENABLED=1`), matching the project's own documented posture ("Aura runs on container/Ubuntu/DGX Spark only… run gates in WSL/CI"). The full non-race suite for every touched package (`internal/swarm`, `internal/steer`, `internal/config`, `internal/cron`, `internal/db`, `cmd/aura`) is green, and the `db_integration` tier ran live against a real disposable Postgres (`internal/swarm`: 16.9s; specifically `TestNudgeSkipsDrained` 2.84s, `TestNudgeOnceUnderConcurrency` 2.62s — the concurrency proof itself doesn't need `-race` to be meaningful, since it asserts an exact call count under real concurrent goroutines against real Postgres locking, not just absence of a data race). **Owed before phase close:** `-race` on WSL/CI for `internal/swarm/...`, `internal/config/...`, `cmd/aura/...` per the plan's own `<verification>` block.
- **Two pre-existing, unrelated test failures on this Windows host**, confirmed NOT caused by this plan (neither file nor package touched): `TestTempScriptRecordsAdHocEvidenceWithoutCanonicalSuite` / `TestNoSuiteNudgeNamesTheDirectoryTheClassifierAccepts` (`internal/agent`, a Windows-temp-path classification mismatch) and `TestStageBoxArtifact_ExtractsRegularFile` (`internal/agent/tools`, a Windows file-mode-bits mismatch: `-rw-rw-rw-` vs. the expected Unix `0600`). Out of scope per CLAUDE.md's scope boundary rule and the project's own documented "Windows-only … test failures are env artifacts, not regressions" precedent — not fixed, not touched.
- **What this plan's measurement does NOT cover (Amendment #154, stated as the plan required):** the fan-out's choice between two candidate channels (`Registry.DeliverToIdentity`'s `sort.Strings`, first-delivers-wins order) and its owns-but-failed leg have never been exercised live — Telegram is the only shipped `Deliverer`, so that choice has never actually been made between two candidates. The `pending_notifications` outbox's own retry/backoff/dead-letter behavior is read from the schema and from `internal/cron`'s existing `SweepDueNotifications`/`deliverSweptRow` machinery, not measured fresh by this plan — it is the SAME machinery a scheduled task's own owns-but-failed leg already relies on, reused rather than re-verified.

## User Setup Required

None — `AURA_SWARM_DELEGATION_NUDGE_SEC` ships with a default (60) in `.env.example`; no external service configuration required.

## Next Phase Readiness

- SC#1's assertion is now checkable with `psql`: query `aura.conversation_turns` for a delegation's origin conversation after the worker finishes, with no operator turn required.
- Plan 51-06b can reuse this exact seam for the worker's QUESTION (the plan's own stated precedent, `268580e23`) rather than writing a second one — `DelegationDelivery`, `ConversationRecorder`, and `ChannelDeliverer` are already the right shape.
- Plan 51-08's live E2E gate (delegate, close the cockpit, assert BOTH the `aura.conversation_turns` row and the channel message) has a real implementation to drive; nothing in this plan's own daemon-free or `db_integration` tiers substitutes for that live proof.
- Plan 51-09 (inactivity watchdog) is unaffected — this plan touched neither `runWithHeartbeat` nor the termination model.
- `-race` on WSL/CI remains owed (see Issues Encountered) before this phase can close.

## Self-Check: PASSED

- `internal/swarm/delegation_delivery.go` — FOUND
- `internal/swarm/delegation_delivery_test.go` — FOUND
- `internal/swarm/delegation_delivery_db_test.go` — FOUND
- `internal/db/migrations/0105_pending_notifications_steer_queue.up.sql` — FOUND
- `internal/db/migrations/0105_pending_notifications_steer_queue.down.sql` — FOUND
- Commits `c17823f08`, `46736c038`, `66e79fcd6` — all FOUND in `git log --oneline`
- `go build ./...`, `go vet ./...` — exit 0
- `go test ./internal/swarm/... ./internal/steer/... ./internal/config/... ./internal/cron/... ./internal/db/... ./cmd/aura/...` — all `ok`
- `go test -tags=db_integration ./internal/swarm/...` as `aura_app` — `ok`, 16.9s (not a skip tell)
- `grep -c 'internal/conversations\|internal/channels' internal/swarm/*.go` — 0 everywhere
- `grep -rn 'agent_message_send' internal/ cmd/` — no matches
- `grep -c 'NewDelegationClaimLoop' cmd/aura/*.go` — 1

---
*Phase: 51-durable-delegation*
*Completed: 2026-08-28*
