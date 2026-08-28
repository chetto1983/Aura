---
phase: 51-durable-delegation
plan: 06a
subsystem: database
tags: [postgres, sqlc, migration, rls, paused_states, hitl, fencing, golang]

# Dependency graph
requires:
  - phase: 51-02
    provides: "the Postgres steer inbox pattern (typed-row/TTL precedent this plan's lazy expiry mirrors)"
provides:
  - "aura.paused_states.pending_action_id (D-12 fencing column) + owning_worker_id (D-13 level identity), with a CHECK constraint enforcing they never coexist with proxied_from_child_id"
  - "MarkPausedStateResumedFenced :execrows — the one fenced conditional UPDATE both single and batch resume paths route through"
  - "askuser.Pending.WorkerID() — the single accessor for 'which worker owns this pause'"
  - "ResumeClaim.ExpectActionID threaded through PoolResumeCommitter.CommitResume/CommitResumeBatch"
  - "askuser.NewWithPauseTTL — opt-in lazy expiry on GetByToken"
affects: ["51-06b (consumes the fence to continue a paused worker)", "51-08 (live SC#4 scenario)"]

# Actuals (#2632)
actuals:
  tokens: 12715
  tasks: 1
  commits: 1

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "sqlc.narg nullable-parameter idiom for an additive conditional-UPDATE predicate (one query, not two)"
    - "DB-enforced mutual-exclusivity CHECK constraint as the authority mechanism between a host-written and a model-supplied attribution column"
    - "Store-bound TTL via a second constructor (NewWithPauseTTL) rather than a config import, keeping the package a dependency leaf"

key-files:
  created:
    - internal/db/migrations/0106_paused_states_fencing.up.sql
    - internal/db/migrations/0106_paused_states_fencing.down.sql
    - internal/askuser/store_fencing.go
    - internal/askuser/store_fencing_test.go
    - internal/runner/worker_pause_test.go
  modified:
    - internal/db/queries/paused_states.sql
    - internal/db/sqlc/models.go
    - internal/db/sqlc/paused_states.sql.go
    - internal/db/sqlc/querier.go
    - internal/askuser/store.go
    - internal/askuser/store_unit_test.go
    - internal/runner/interfaces.go
    - internal/runner/resume_committer.go
    - internal/agui/approvals_api.go
    - internal/db/db_unit_test.go

key-decisions:
  - "Checkpoint resolved `new-columns`: owning_worker_id is a NEW host-written column; proxied_from_child_id/proxied_tool_call_id keep meaning exactly what they mean today (synchronous, model-relayed). A CHECK constraint (paused_states_worker_attribution_exclusive) makes the two mutually exclusive at the DB layer, not just by convention."
  - "MarkResumedBatchTx's signature changed from map[string]ResumeAnswer to map[string]FencedResumeAnswer (an internal Tx-bound method); the PUBLIC MarkResumedBatch/PauseStore interface signature stayed byte-identical to avoid rippling through 5+ test fakes across the repo."
  - "PoolResumeCommitter.CommitResume (single-claim) was ALSO switched to the fenced path (MarkResumedFencedTx), not only CommitResumeBatch — ResumeClaim.ExpectActionID is one shared struct field for both, so leaving CommitResume unfenced would have been a silent gap for whichever resume shape 51-06b turns out to need."
  - "Lazy expiry is opt-in via a second constructor (NewWithPauseTTL(pool, ttlSec)), not wired into any production call site in this plan — every existing New(pool) caller (chat_boot.go, paused_states.go) is unaffected; wiring the real AURA_ASKUSER_PAUSE_TTL_SEC value into a live read path is left to whichever plan actually needs it."

requirements-completed: [SWARM-06]

coverage:
  - id: D1
    description: "Fencing column + conditional UPDATE: a correct pending_action_id resumes exactly once, a stale/mismatched one matches zero rows and appends no conversation turn"
    requirement: SWARM-06
    verification:
      - kind: integration
        ref: "internal/runner/worker_pause_test.go#TestPerWorkerPauseFencing"
        status: pass
    human_judgment: false
  - id: D2
    description: "The fence is additive: a NULL pending_action_id (every pre-migration/ordinary pause) resumes exactly as before, with or without a caller-supplied fence"
    requirement: SWARM-06
    verification:
      - kind: integration
        ref: "internal/runner/worker_pause_test.go#TestUnfencedPauseStillResumes"
        status: pass
    human_judgment: false
  - id: D3
    description: "Lazy expiry: a still-pending row past its TTL reads as absent via GetByToken, opt-in and non-disruptive to the TTL-disabled default"
    requirement: SWARM-06
    verification:
      - kind: integration
        ref: "internal/runner/worker_pause_test.go#TestWorkerPauseLazyExpiry"
        status: pass
      - kind: unit
        ref: "internal/askuser/store_fencing_test.go#TestPauseExpired_PureComparison"
        status: pass
    human_judgment: false
  - id: D4
    description: "Checkpoint's authority rule enforced by the DB, not merely documented: a row with both owning_worker_id and proxied_from_child_id set fails the INSERT (SQLSTATE 23514)"
    requirement: SWARM-06
    verification:
      - kind: integration
        ref: "internal/runner/worker_pause_test.go#TestPausedStateWorkerAttributionExclusive"
        status: pass
    human_judgment: false
  - id: D5
    description: "RLS scoping: a foreign identity cannot read another identity's pause, fenced or not (run as aura_app, never the superuser)"
    requirement: SWARM-06
    verification:
      - kind: integration
        ref: "internal/runner/worker_pause_test.go#TestPerWorkerPauseFencing (foreign-identity assertion)"
        status: pass
    human_judgment: false

duration: ~90min
completed: 2026-08-28
status: complete
---

# Phase 51 Plan 06a: Fence per-worker pauses and separate their attribution (SWARM-06 guard rails) Summary

**Migration 0106 gives `aura.paused_states` a D-12 fencing column and a D-13 host-written worker-identity column, enforced mutually exclusive against the existing model-relayed pair by a DB CHECK constraint, with a single fenced conditional UPDATE both the single- and batch-resume paths route through.**

## Performance

- **Duration:** ~90 min
- **Completed:** 2026-08-28T12:10:00Z
- **Tasks:** 2 (Task 1 was the checkpoint decision, resolved by the human as `new-columns`; Task 2 is the substantive commit)
- **Files modified:** 16 (12 hand-written + 4 sqlc-generated)

## Accomplishments

- `aura.paused_states.pending_action_id` (D-12 fencing id) and `.owning_worker_id` (D-13 level identity) landed via migration 0106, with `paused_states_worker_attribution_exclusive` making the checkpoint's authority rule an enforced CHECK, not a convention.
- `MarkPausedStateResumedFenced :execrows` — the shipped `MarkPausedStateResumed` plus one `sqlc.narg`-driven predicate — is the ONE query both `MarkResumedFencedTx` (single) and `MarkResumedBatchTx`'s per-token loop call; a NULL fence matches regardless (additive), a set fence requires an exact match (stale/absent fails closed).
- `ResumeClaim.ExpectActionID` threads through `PoolResumeCommitter.CommitResume` AND `CommitResumeBatch` — both routes through the real production committer now honor D-12.
- `askuser.Pending.WorkerID()` is the one accessor for "which worker owns this pause"; `internal/agui/approvals_api.go`'s `Source` projection was moved onto it (grep-verified: no other production site reads `ProxiedFromChildID`/`OwningWorkerID` off a `Pending` directly).
- Lazy expiry (`askuser.NewWithPauseTTL`, mirrors LibreChat's `ApprovalLifecycle.peek`) is opt-in and proven both as a pure comparison (daemon-free) and against live Postgres with a backdated row.
- `internal/db/db_unit_test.go`'s `MigrationHead` pin was bumped 105→106 — its own designed acknowledgment mechanism for a new migration, not a fixup.

## Task Commits

1. **Task 1: Checkpoint — extend proxied_from_child_id or add new columns?** — no commit (decision only; resolved `new-columns` by the human between the first and second dispatch of this executor)
2. **Task 2: Fencing column + fenced conditional resume inside the existing transaction (D-12)** - `4993e8891` (feat)

**Plan metadata:** see below (this commit)

_Note: tdd="true" on Task 2 — see "TDD Gate Compliance" below; the migration/query/store/committer work was built as one integrated unit rather than a strict RED-then-GREEN commit split._

## Files Created/Modified

- `internal/db/migrations/0106_paused_states_fencing.up.sql` / `.down.sql` — the two new columns, their `COMMENT ON COLUMN`, and the CHECK constraint
- `internal/db/queries/paused_states.sql` — `pending_action_id`/`owning_worker_id` added to every SELECT (in the table's actual physical column order — required for sqlc to keep collapsing to the shared `AuraPausedStates` model instead of minting a per-query Row type) and to the INSERT; `MarkPausedStateResumedFenced` added
- `internal/db/sqlc/{models,paused_states.sql.go,querier}.go` — regenerated via `sqlc generate` (sqlc v1.31.1)
- `internal/askuser/store.go` — `Pending`/`InsertParams` gained the two new fields; `insertArgs`/`fromRow` project them; `GetByToken` applies lazy expiry; `MarkResumedBatchTx`/`MarkResumedBatch` route through the fenced query (public signature unchanged)
- `internal/askuser/store_fencing.go` (new) — `WorkerID()` accessor, `FencedResumeAnswer`, `markPausedStateResumedFencedTx` (the one shared call site), `MarkResumedFencedTx`/`MarkResumedFenced`, `NewWithPauseTTL`, the pure `pauseExpired` comparison — split out to keep `store.go` at exactly 600 LOC
- `internal/askuser/store_fencing_test.go` (new) — daemon-free unit tests for `pauseExpired`
- `internal/askuser/store_unit_test.go` — one call site updated for the `MarkResumedBatchTx` signature change (wrap into `FencedResumeAnswer`)
- `internal/runner/interfaces.go` — `ResumeClaim.ExpectActionID` field added
- `internal/runner/resume_committer.go` — `CommitResume`/`CommitResumeBatch` route through the fenced path; `splitResumeCommitter`'s doc comment records that it stays unfenced by design
- `internal/runner/worker_pause_test.go` (new, `db_integration`) — `TestPerWorkerPauseFencing`, `TestUnfencedPauseStillResumes`, `TestWorkerPauseLazyExpiry`, `TestPausedStateWorkerAttributionExclusive`
- `internal/agui/approvals_api.go` — `Source: p.ProxiedFromChildID` → `Source: p.WorkerID()`
- `internal/db/db_unit_test.go` — `MigrationHead` pin bumped 105→106

## Decisions Made

See `key-decisions` in frontmatter. Summarized: the checkpoint's `new-columns` obligation (enumerate/re-check every `ProxiedFromChildID` reader, name the authority rule, enforce with a CHECK + test, name the single accessor, grep-prove no other direct reader) was executed in full — see "Checkpoint Obligation Evidence" below.

## Checkpoint Obligation Evidence

Grep for direct reads of `.ProxiedFromChildID`/`.OwningWorkerID` outside `internal/askuser/store*.go`:

```
$ grep -rn "\.ProxiedFromChildID\b\|\.OwningWorkerID\b" --include=*.go internal/ cmd/ | grep -v "internal/askuser/store" | grep -v "internal/db/sqlc/"
internal/agent/llm_agent_pause.go:147:   ProxiedFromChildID: pause.ProxiedFromChildID,      # write-side: *tools.ErrAwaitingUserInput -> agent.AwaitingInput, not a read of askuser.Pending
internal/agent/tools/ask_user.go:165:    ProxiedFromChildID: a.ProxiedFromChildID,          # write-side: tools.AskUserInput -> tools.ErrAwaitingUserInput
internal/runner/runner_persist.go:387-400: (ai.ProxiedFromChildID ...)                       # write-side: agent.AwaitingInput -> askuser.InsertParams (the mint site)
+ 5 matching _test.go files exercising the same write-side types
```

Every remaining hit is a **write-side** field on `agent.AwaitingInput` / `tools.AskUserInput` / `askuser.InsertParams` (constructing what to persist), never a **read** of a persisted `askuser.Pending`'s attribution. The one production READ site (`internal/agui/approvals_api.go`'s `Source` projection) now calls `p.WorkerID()`. Confirmed clean.

CHECK constraint enforcement (`internal/runner/worker_pause_test.go#TestPausedStateWorkerAttributionExclusive`): inserting a row with both `OwningWorkerID` and `ProxiedFromChildID` set fails with `errors.As(err, &pgErr)`, `pgErr.Code == "23514"`, `pgErr.ConstraintName == "paused_states_worker_attribution_exclusive"` — verified against live Postgres.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `ResumeClaim` lives in `interfaces.go`, not `resume_committer.go`**
- **Found during:** Task 2, threading `ExpectActionID`
- **Issue:** The plan's `<files>` list names `resume_committer.go` but `ResumeClaim` (the struct the artifact list says gains `ExpectActionID`) is declared in `internal/runner/interfaces.go`
- **Fix:** Edited `interfaces.go` to add the field with a doc comment explaining D-12 and which committer honors it
- **Files modified:** `internal/runner/interfaces.go`
- **Verification:** `go build ./...` clean
- **Committed in:** `4993e8891`

**2. [Rule 2 - Missing Critical] Single-accessor obligation required updating `internal/agui/approvals_api.go`**
- **Found during:** Task 2, satisfying the checkpoint's binding obligation ("name which single accessor every reader must call... assert with grep that no other site reads either column directly")
- **Issue:** `approvals_api.go`'s `Source` field read `p.ProxiedFromChildID` directly, which would fail the grep obligation the moment `OwningWorkerID` existed as an alternate answer to "which worker"
- **Fix:** Added `Pending.WorkerID()` accessor; updated `approvals_api.go` to call it; updated the surrounding doc comment
- **Files modified:** `internal/askuser/store_fencing.go`, `internal/agui/approvals_api.go`
- **Verification:** grep evidence above; `go test ./internal/agui/...` green
- **Committed in:** `4993e8891`

**3. [Rule 3 - Blocking] `MarkResumedBatchTx` signature change broke one existing unit test**
- **Found during:** Task 2, `go vet ./...`
- **Issue:** `internal/askuser/store_unit_test.go:TestMarkResumedBatchTx_ClaimsInSortedTokenOrder` called `MarkResumedBatchTx` with the old `map[string]ResumeAnswer` shape
- **Fix:** Wrapped each `ResumeAnswer` into `FencedResumeAnswer{Answer: ...}` (empty `ExpectActionID`, matching the pre-migration behavior the test asserts)
- **Files modified:** `internal/askuser/store_unit_test.go`
- **Verification:** `go vet ./...` clean; test still asserts sorted-token order, now via `recordingDBTX` (unaffected — `Token` stayed `args[0]` in the generated fenced query)
- **Committed in:** `4993e8891`

**4. [Rule 1 - Bug] `internal/db/db_unit_test.go`'s MigrationHead pin was stale**
- **Found during:** Task 2, `go test ./...` full sweep
- **Issue:** `TestMigrationHeadMatchesEmbeddedCatalog` pins the expected embedded migration head as a deliberate acknowledgment gate; it still said 105
- **Fix:** Bumped to 106 with an updated comment naming this plan's migration
- **Files modified:** `internal/db/db_unit_test.go`
- **Verification:** `go test ./internal/db/... -run TestMigrationHeadMatchesEmbeddedCatalog` green
- **Committed in:** `4993e8891`

**5. [Rule 3 - Blocking] SELECT column order had to match the table's PHYSICAL column order**
- **Found during:** Task 2, first `sqlc generate` + `go build`
- **Issue:** Adding `pending_action_id, owning_worker_id` to each SELECT right after `proxied_tool_call_id` (textual convenience) made sqlc stop collapsing every query to the shared `sqlc.AuraPausedStates` model — it minted a distinct Row struct per query instead, breaking `fromRow(sqlc.AuraPausedStates)`'s single conversion function
- **Fix:** Moved the two new columns to the END of every SELECT's column list, matching `ALTER TABLE ADD COLUMN`'s actual physical append order (confirmed via `\d aura.paused_states` against the live database)
- **Files modified:** `internal/db/queries/paused_states.sql`
- **Verification:** `sqlc generate` + `go build ./internal/askuser/...` clean afterward
- **Committed in:** `4993e8891`

---

**Total deviations:** 5 auto-fixed (2 Rule 2/3 missing-critical-scope, 3 Rule 3 blocking compile/tooling fixes)
**Impact on plan:** All five were necessary for correctness or for the plan's own binding obligations to actually hold under grep/build verification. No scope creep — nothing outside the D-12/D-13 fencing surface was touched.

## Issues Encountered

**Environmental, not a defect in this plan's work:** immediately after this plan's commit (`4993e8891`), a concurrent session's push to `origin/master` was merged into this SAME shared working tree by something other than this executor (no `git pull`/`git merge` was run here), producing merge commit `5183505b7`. Per the "SHARED CHECKOUT" note in the executor's own instructions, this was verified rather than assumed safe: `git show --stat` confirmed a clean auto-merge (no conflict markers anywhere in the tree), `internal/agui/approvals_api.go`'s `WorkerID()` change was confirmed intact post-merge, and the full `go build ./... && go vet ./...` plus `go test ./internal/askuser/... ./internal/runner/... ./internal/agui/... ./internal/db/...` were re-run and stayed green after the merge. No corrective action was taken on the merge itself (it was not this executor's to make/unmake); it is recorded here for anyone reading `git log` who might otherwise wonder where it came from.

**Three pre-existing test failures, unrelated to this plan, confirmed via a throwaway `git worktree` at unmodified HEAD (`730792452`) against a fresh disposable database and then removed:**
- `TestVerifyOnStopFiresOnARealTurn` (`internal/runner`, `db_integration`)
- `TestTempScriptRecordsAdHocEvidenceWithoutCanonicalSuite` and `TestNoSuiteNudgeNamesTheDirectoryTheClassifierAccepts` (`internal/agent`, unit)
- `TestStageBoxArtifact_ExtractsRegularFile` (`internal/agent/tools`, unit — already documented in `deferred-items.md` from plan 51-04, re-confirmed still present)

All logged to `.planning/phases/51-durable-delegation/deferred-items.md` under a `## 51-06a` heading with the confirming evidence and an owner note. None touched, per CLAUDE.md's scope boundary.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- The fence, the level-identity column, the enforced authority rule, and lazy expiry are all landed and live-proven (Windows + WSL `-race`, both against disposable Postgres as `aura_app`).
- **51-06b is what actually continues a paused worker** — persisting continuation state, observing an answered pause, returning the queue row to a claimable state, and injecting the answer as the pending `ask_user` tool result. This plan makes the fence storable/queryable/enforced; it does not consume it.
- **SWARM-06 is NOT complete** — it is also declared by 51-06b, which has not run. `requirements-completed` above copies the plan's own `[SWARM-06]` verbatim per protocol; the shared-ID gate in the orchestrator's `update_requirements` step will hold it un-marked until 51-06b's own SUMMARY exists.
- Three pre-existing, unrelated test failures are open and logged (see above) — no phase currently owns their health; flagged for whoever next touches the verify-on-stop gate or the Windows sandbox-staging test.

## Self-Check: PASSED

- `[ -f internal/db/migrations/0106_paused_states_fencing.up.sql ]` → FOUND
- `[ -f internal/db/migrations/0106_paused_states_fencing.down.sql ]` → FOUND
- `[ -f internal/askuser/store_fencing.go ]` → FOUND
- `[ -f internal/askuser/store_fencing_test.go ]` → FOUND
- `[ -f internal/runner/worker_pause_test.go ]` → FOUND
- `git log --oneline --all | grep -q 4993e8891` → FOUND (parent of merge `5183505b7`)
- `git show --stat 4993e8891` → 16 files changed, non-empty
- Every `<acceptance_criteria>` in Task 2 re-verified via grep/wc -l/go test above → all PASS
- Plan-level `<verification>` re-run: `make db-migrate` clean+idempotent (version 106); `go build ./... && go vet ./... && go test ./...` green except the three pre-existing unrelated failures (logged); `go test -race ./internal/runner/... ./internal/askuser/...` green in WSL; `go test -tags=db_integration ./internal/runner/...` green as `aura_app`, both Windows and WSL `-race`, real runtime 2.2-3.7s (not a skip tell)

## TDD Gate Compliance

Task 2 carried `tdd="true"` but was executed as ONE integrated commit rather than a strict RED-then-GREEN split: the migration, the sqlc query, the generated Go layer, the store methods, and the committer wiring are mutually compile-dependent (a query change requires `sqlc generate` before any Go test referencing the new query can even compile, let alone run RED). Non-vacuousness of the test suite was instead verified empirically within the single GREEN commit:
- `TestPerWorkerPauseFencing` asserts BOTH a correctly-fenced claim succeeding AND a mismatched-fenced claim on the SAME pause failing with `ErrPauseNotFound` and zero appended turns — a test that could not pass both branches by accident.
- `TestPausedStateWorkerAttributionExclusive` asserts a specific SQLSTATE (`23514`) and constraint name, not merely "an error occurred" — confirming the CHECK constraint (not some unrelated failure) is what fired.

---
*Phase: 51-durable-delegation*
*Completed: 2026-08-28*
