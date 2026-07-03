---
phase: 34-agent-loop-correctness-durable-ledger
plan: 05
subsystem: database
tags: [pgx, sqlc, cross-store-tx, hitl, paused-states, conversation-turns, deadlock-avoidance, int32-guard, interface-first]

# Dependency graph
requires:
  - phase: 34-agent-loop-correctness-durable-ledger
    provides: "34-01 regenerated MarkPausedStateResumed (:execrows) — the rows-affected count that drives ErrPauseNotFound inside a shared tx"
provides:
  - "askuser.Store.InsertTx / MarkResumedTx / MarkResumedBatchTx(q, ...) — tx-accepting seams that operate on a caller-supplied *sqlc.Queries and open NO transaction of their own (the pool-owning 34-06 ResumeCommitter supplies the tx)"
  - "MarkResumedBatchTx claims pauses in SORTED token order (deadlock-free concurrent batches, T-34-B); MarkResumedTx maps rows==0 -> ErrPauseNotFound via the :execrows generated fn (raw markResumedSQL const deleted)"
  - "conversations.Store.AppendTurnTx(ctx, q, p) — the no-spill tx-inner turn+aggregate insert body, EXPORTED for the 34-06 cross-store committer (requires Seq>0 -> ErrSeqRequired)"
  - "askuser.ListRecent int32(limit) guard (QUAL-04a) — math.MaxInt32 clamp with the <=0 -> 50 fallback, mirrors ListPendingAll"
affects: [runner, askuser, conversations]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Tx-accepting `…Tx(q, …)` variant + thin non-tx wrapper: single-statement public methods (Insert/MarkResumed) delegate to their *Tx variant over the pool-bound s.q (byte-identical auto-commit, fail-fast parse-before-DB preserved); the multi-statement MarkResumedBatch wraps its *Tx in db.WithTx (all-or-nothing)"
    - "Sorted-token claim order (sort.Strings before the per-row conditional UPDATE) as the Postgres 40P01 deadlock avoidance for concurrent overlapping batches under READ COMMITTED"
    - "Recording sqlc.DBTX fake (captures Exec token order + Query int32 limit) to unit-assert claim order and narrowing guards without a live pool"

key-files:
  created:
    - "internal/conversations/store_append_tx_test.go — AppendTurnTx unit tests (Seq<=0 -> ErrSeqRequired, small-turn insert via supplied queries, DB-error propagation, bad conversation id)"
  modified:
    - "internal/askuser/store.go — InsertTx/MarkResumedTx/MarkResumedBatchTx + thin wrappers; markResumedSQL const removed; ListRecent int32 guard"
    - "internal/askuser/store_unit_test.go — recordingDBTX fake + sorted-token claim-order test + int32-guard table"
    - "internal/conversations/store_append.go — AppendTurnTx no-spill tx-inner body + ErrSeqRequired sentinel"

key-decisions:
  - "Single-statement wrappers (Insert, MarkResumed) delegate to their *Tx variant through the pool-bound s.q — NOT db.WithTx. This keeps on-wire behavior byte-identical (a single INSERT / conditional UPDATE auto-commits, exactly as pre-34-05) and preserves the fail-fast parse/encode-before-any-DB-touch contract the existing nil-pool unit tests lock in; a literal single-statement db.WithTx would pool.Begin first (panicking on the nil-pool tests / opening a needless tx on malformed input). Only the genuinely multi-statement MarkResumedBatch uses db.WithTx (as before). Honors the plan intent 'DRY, no behavior change for current callers'."
  - "AppendTurn (public) is left byte-unchanged rather than rewired to opaquely call AppendTurnTx: its Seq>0 branch pre-builds `turn` OUTSIDE the tx so cleanupSidecarOnTxError removes EXACTLY the file THIS turn spilled (turn.ContentSidecarPath.String). AppendTurnTx builds the turn internally and cannot expose that path; calling it opaquely would lose the precise cleanup (an unconditional path risks removing a committed prior turn's sidecar on a duplicate-seq PK error). AppendTurnTx composes the SAME shared helpers (appendTurnWrites + insertTurnAndAggregates) + a Seq>0 guard, so there is no meaningful duplication — and the general spill path stays in AppendTurn per the plan prohibition."
  - "Sorted-token order proven by a recording sqlc.DBTX (8 tokens, map input) asserting the exact UPDATE order — a non-sorting impl surfaces a non-sorted order and fails; also the deadlock-avoidance mitigation the 34-06 concurrent-batch test relies on."

patterns-established:
  - "Interface-first store seams: fix the tx-accepting signatures a later-wave cross-store committer builds against, with the public non-tx methods as behavior-preserving wrappers"
  - "Recording DBTX fake for order/argument assertions on generated sqlc queries in the unit tier"

requirements-completed: []
requirements-advanced: [LOOP-02, LOOP-03, QUAL-04]

coverage:
  - id: D1
    description: "LOOP-02/F-004 seam: MarkResumedBatchTx claims in sorted token order (deadlock-free); operates on the caller's *sqlc.Queries; rows==0 -> ErrPauseNotFound. The atomic all-or-nothing batch behavior itself closes in 34-06's cross-store committer + db_integration."
    requirement: "LOOP-02"
    verification:
      - kind: unit
        ref: "internal/askuser/store_unit_test.go#TestMarkResumedBatchTx_ClaimsInSortedTokenOrder"
        status: pass
      - kind: integration
        ref: "internal/askuser/store_test.go#TestMarkResumedBatch_{ResolvesMany,UnknownTokenRollsBack,AlreadyResumedRollsBack} (wrapper behavior-preserving)"
        status: pass
    human_judgment: false
  - id: D2
    description: "LOOP-03/F-029 seam: MarkResumedTx (rows==0 -> ErrPauseNotFound via :execrows) + conversations.AppendTurnTx (no-spill tx-inner body, Seq>0) are the two halves the 34-06 committer spans in ONE db.WithTx. The atomic single-resume claim+append (and repairability) close in 34-06's db_integration."
    requirement: "LOOP-03"
    verification:
      - kind: unit
        ref: "internal/conversations/store_append_tx_test.go#TestAppendTurnTx_{RequiresSeq,InsertsViaSuppliedQueries,PropagatesDBError,BadConversationID}"
        status: pass
      - kind: integration
        ref: "internal/askuser/store_test.go#TestMarkResumed_InvalidTokenRejected; internal/conversations/store_test.go#TestAppendTurn_* (wrappers behavior-preserving)"
        status: pass
    human_judgment: false
  - id: D3
    description: "QUAL-04a/D-15a: ListRecent clamps int32(limit) via math.MaxInt32 with the <=0 -> 50 fallback, never wrapping to a negative LIMIT (mirrors ListPendingAll). (QUAL-04 already [x] in REQUIREMENTS.md — its 04b pool-leak/double-Validate half shipped in 34-03.)"
    requirement: "QUAL-04"
    verification:
      - kind: unit
        ref: "internal/askuser/store_unit_test.go#TestListRecent_Int32Guard (zero/negative/50/MaxInt32/overflow)"
        status: pass
      - kind: integration
        ref: "internal/askuser/store_mutation_test.go#TestListRecent_LimitClamp (clamp behavior-preserving)"
        status: pass
    human_judgment: false

# Metrics
duration: 45min
completed: 2026-07-03
status: complete
---

# Phase 34 Plan 05: HITL store tx-seams + int32 guard Summary

**Exposed the DRY tx-accepting store seams the 34-06 HITL ResumeCommitter composes into one cross-store `db.WithTx` — `askuser.{InsertTx,MarkResumedTx,MarkResumedBatchTx}` (sorted-token, deadlock-free; `:execrows` rows==0→ErrPauseNotFound; raw `markResumedSQL` deleted) and `conversations.AppendTurnTx` (no-spill tx-inner body) — plus the QUAL-04a `ListRecent` int32 guard, with every existing caller's on-wire behavior byte-unchanged (thin wrappers).**

## Performance

- **Duration:** ~45 min
- **Tasks:** 2 (both `type=auto`, `tdd=true`)
- **Files:** 4 (1 created, 3 modified)

## Accomplishments

- **Task 1 — askuser tx seams + sorted batch + int32 guard (LOOP-02/03 seams, QUAL-04a):**
  - `InsertTx(ctx, q, p)`, `MarkResumedTx(ctx, q, token, ans)`, `MarkResumedBatchTx(ctx, q, answers)` operate on a caller-supplied `*sqlc.Queries` and open **no** transaction of their own — the pool-owning 34-06 committer supplies the tx.
  - `MarkResumedTx` uses the regenerated `MarkPausedStateResumed` (`:execrows`) and maps rows-affected==0 → `ErrPauseNotFound`; the raw `markResumedSQL` `pool.Exec` const (the sole reason the store bypassed sqlc) is **deleted** — `grep -n 'markResumedSQL' internal/askuser/store.go` → 0.
  - `MarkResumedBatchTx` collects the answer tokens, `sort.Strings` them, then claims in that sorted order so two concurrent overlapping batches lock rows in the **same order** and cannot deadlock (Postgres 40P01, T-34-B); the loser blocks, rechecks `WHERE resumed_at IS NULL` under READ COMMITTED, matches 0 rows, and gets a clean `ErrPauseNotFound`.
  - `Insert`/`MarkResumed` become thin wrappers over their `*Tx` variant bound to the pool's Queries; `MarkResumedBatch` wraps `MarkResumedBatchTx` in `db.WithTx` (all-or-nothing, as before).
  - `ListRecent` clamps `int32(limit)` via `math.MaxInt32` keeping the `<=0 → 50` fallback (mirrors `ListPendingAll`).
- **Task 2 — conversations AppendTurnTx (LOOP-03 seam):**
  - `AppendTurnTx(ctx, q, p)` is EXPORTED, takes a `*sqlc.Queries`, folds `appendTurnWrites` + `insertTurnAndAggregates` on the caller's tx, requires `Seq > 0` (`ErrSeqRequired`), and carries **no** spill/`cleanupSidecarOnTxError` logic (resume/pause turns never spill, A3).
  - `AppendTurn` (public) is byte-unchanged — its spill + rollback-cleanup semantics stay exactly as today.

## Task Commits

1. **Task 1: tx-accepting askuser store methods + sorted-token batch (LOOP-02/03, QUAL-04a)** — `2095b679` (feat)
2. **Task 2: conversations AppendTurnTx no-spill tx-inner body (LOOP-03)** — `dab73504` (feat)

## Files Created/Modified

- `internal/askuser/store.go` — `InsertTx`/`MarkResumedTx`/`MarkResumedBatchTx` (sorted) + thin wrappers; `markResumedSQL` removed; `ListRecent` int32 guard; +`sort` import
- `internal/askuser/store_unit_test.go` — `recordingDBTX`/`emptyRows` fake + `TestMarkResumedBatchTx_ClaimsInSortedTokenOrder` + `TestListRecent_Int32Guard`
- `internal/conversations/store_append.go` — `AppendTurnTx` + `ErrSeqRequired` sentinel
- `internal/conversations/store_append_tx_test.go` (new) — 4 `AppendTurnTx` unit tests via the `fakeDBTX` harness

## Decisions Made

- **Single-statement wrappers delegate to `*Tx` via the pool-bound `s.q`, not `db.WithTx`.** The plan's shorthand said "thin `db.WithTx` wrappers" for all three, but `Insert`/`MarkResumed` are single-statement ops that today run *without* an explicit transaction (`s.q.InsertPausedState`; `s.pool.Exec`). Delegating them through `s.q` keeps on-wire behavior byte-identical (one auto-commit statement) and preserves the fail-fast parse/encode-before-any-DB contract the existing nil-pool unit tests lock in; a literal single-statement `db.WithTx` would `pool.Begin` first (panicking on the nil-pool tests / opening a needless tx on malformed input). Only the multi-statement `MarkResumedBatch` uses `db.WithTx`. This honors the plan intent ("DRY, no behavior change for current callers") more faithfully than the literal mechanism.
- **`AppendTurn` left byte-unchanged rather than rewired to call `AppendTurnTx`.** Its `Seq>0` branch pre-builds `turn` outside the tx precisely so `cleanupSidecarOnTxError` removes *exactly* the file this turn spilled; `AppendTurnTx` builds the turn internally and can't expose that path, so an opaque call would lose the precise cleanup. `AppendTurnTx` composes the same shared helpers + a `Seq>0` guard (no real duplication), and the general spill path stays in `AppendTurn` per the prohibition.
- **Sorted claim order proven deterministically** with a recording `sqlc.DBTX` (8 tokens from a map input; asserts the exact ascending UPDATE order) — both the correctness assertion and the deadlock-avoidance the 34-06 concurrent-batch test relies on.

## Deviations from Plan

### Auto-fixed / behavior-preserving adjustments

**1. [Rule 1 — behavior preservation] Single-statement public wrappers use the pool-bound `s.q`, not `db.WithTx`.**
- **Found during:** Task 1
- **Issue:** The plan said "thin `db.WithTx` wrappers" for `Insert`/`MarkResumed`, but those are single-statement ops that pre-34-05 opened no transaction; a `db.WithTx` wrapper `pool.Begin`s before parse/encode, breaking the existing nil-pool fail-fast unit tests and adding a needless BEGIN/COMMIT.
- **Fix:** `Insert`/`MarkResumed` delegate to `InsertTx`/`MarkResumedTx` over `s.q` (byte-identical auto-commit; parse/encode still short-circuits before any DB touch). Only `MarkResumedBatch` (multi-statement) uses `db.WithTx`.
- **Files:** internal/askuser/store.go
- **Commit:** `2095b679`

**2. [Rule 1 — behavior preservation] `AppendTurn` Seq>0 branch kept identical (not rewired to `AppendTurnTx`).**
- **Found during:** Task 2
- **Issue:** The plan suggested rewiring the `Seq>0` branch to call `AppendTurnTx` inside its `db.WithTx`+cleanup composition, but that branch pre-builds `turn` outside the tx so cleanup can remove exactly the spilled file; `AppendTurnTx` can't expose that path, so an opaque call would degrade the cleanup precision (risking removal of a committed prior turn's sidecar on a duplicate-seq PK error).
- **Fix:** `AppendTurn` left byte-unchanged; `AppendTurnTx` extracted as the reusable no-spill body (same shared helpers + `Seq>0` guard). Spill/rollback semantics unchanged, per the plan's own "spill semantics unchanged" requirement.
- **Files:** internal/conversations/store_append.go
- **Commit:** `dab73504`

## Requirements Status

- **LOOP-02 / LOOP-03** remain `[ ]` (correctly): this is the **interface-first** plan that fixes the tx-accepting seams; the atomic single/batch resume behavior (and its concurrent/repairability db_integration proofs) close in **34-06** when the `ResumeCommitter` composes `MarkResumedTx`/`MarkResumedBatchTx` + `AppendTurnTx` under one `db.WithTx`.
- **QUAL-04** is already `[x]` — its 04b (double-`Validate`/pool-close) half shipped in 34-03; this plan lands the 04a int32-guard half. No REQUIREMENTS.md change needed.

## Verification

- **Unit (race, WSL):** `go test -race ./internal/askuser/` → ok (1.0s); `go test -race ./internal/conversations/ -run 'Append'` → ok; full `go test -race ./internal/conversations/` → ok (4.8s). `go vet ./internal/askuser/ ./internal/conversations/` clean; `go build ./...` green.
- **Regression (db_integration, live stack):** askuser tier `ok 2.918s` and conversations `-run Append` tier `ok 1.494s` — genuine per-test latency (no skip-as-green); the pre-existing mutation/wrap tests (`TestMarkResumedBatch_*`, `TestListRecent_LimitClamp`, `TestStore_DBErrorWrapMessages`, `TestAppendTurn_{AtomicRollback,SidecarSpill,AggregatesSumTurnColumns}`) stayed green, proving the wrappers are behavior-preserving.
- **Gate:** `grep -n 'markResumedSQL' internal/askuser/store.go` → 0. Every touched file ≤600 LOC (store.go 441, store_append.go 258). gofmt + vet + file-size pre-commit hooks passed on both commits.

## Known Stubs

None — the seams are fully wired to sqlc; no placeholder/empty-data paths. The tx seams ARE the T-34-B (sorted deadlock avoidance), T-34-C (tx-inner claim+append), and T-34-QA (int32 clamp) mitigations from the plan's threat register — implemented, not stubbed.

## Threat Flags

None — no new network/auth/file-access/schema surface introduced. The change is store-internal method decomposition over existing tables and the existing `db.WithTx` seam (no migration).

## Next Phase Readiness

- The three askuser `*Tx` signatures and `conversations.AppendTurnTx(ctx, q, p)` are fixed and race-clean — 34-06's `PoolResumeCommitter` builds against them directly (composes all four under one `db.WithTx`).
- No new migration (D-07 holds); consumed only 34-01's `:execrows` regen.
- No blockers. Environment note: the shared-PG `local` identity was wiped by a parallel session (FK 23503) and re-seeded idempotently before the integration tiers — re-seed again if a parallel session wipes it before the next integration run.

## Self-Check: PASSED

- **Files exist:** store_append_tx_test.go (created); store.go, store_unit_test.go, store_append.go (modified) — all present.
- **Commits exist:** `2095b679` (Task 1), `dab73504` (Task 2) — both in git history.
- **Gates:** `markResumedSQL` grep → 0; unit tiers `ok` (race); db_integration askuser + conversations Append tiers `ok` (real execution); vet/build clean; all touched files ≤600 LOC.

---
*Phase: 34-agent-loop-correctness-durable-ledger*
*Completed: 2026-07-03*
