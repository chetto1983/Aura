---
phase: 34-agent-loop-correctness-durable-ledger
plan: 06
subsystem: database
tags: [pgx, sqlc, cross-store-tx, hitl, paused-states, conversation-turns, resume-committer, goroutine-leak, sync-once, deadlock-avoidance, goleak]

# Dependency graph
requires:
  - phase: 34-agent-loop-correctness-durable-ledger
    provides: "34-05 tx-accepting store seams — askuser.{InsertTx,MarkResumedTx,MarkResumedBatchTx}(q,…) (sorted-token, :execrows rows==0→ErrPauseNotFound) + conversations.AppendTurnTx(q,…) (no-spill, Seq>0)"
  - phase: 34-agent-loop-correctness-durable-ledger
    provides: "34-03 shared cmd/aura/chat.go composition root (bootChatEnvWithConfig deps)"
provides:
  - "runner.ResumeCommitter interface {CommitResume, CommitResumeBatch, CommitPause} — the consumer-side cross-store HITL-durability seam"
  - "runner.PoolResumeCommitter — owns the pool + concrete askuser/conversations Stores; each method runs ONE db.WithTx composing the 34-05 *Tx methods; reserves the turn seq under the conversation row-lock inside the tx"
  - "runner.splitResumeCommitter — pool-less non-atomic fallback (over the narrow Conv/Pause interfaces); runner.New defaults to it when Deps.ResumeCommitter is nil"
  - "Atomic SubmitAnswer/SubmitAnswers (claim+append one tx) + atomic flushPause (assistant tool_call turn + N paused_states rows one tx); persistPause no longer inserts (accumulates minted-token InsertParams)"
  - "Single lifecycle-owned sync.Once + stopDone waiter — repeated Stop on a hung title worker does not accumulate blocked waiter goroutines (LOOP-11)"
  - "scripts/run_runner_integration.sh + the internal/runner db_integration tier (self-seeds the local identity)"
affects: [runner, agui, telegram, cmd/aura]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Consumer-side narrow ResumeCommitter seam (matching ConversationStore/PauseStore): the pool-owning atomic impl is injected at the composition root; a pool-less split fallback keeps every existing call site (cache_audit, unit tests) compiling + running with no code change"
    - "Cross-store db.WithTx composing 34-05 tx-accepting store methods off ONE sqlc.New(tx) (D-02) — the turn seq is reserved via the shared sqlc queries (LockConversationForTurnAppend + NextConversationTurnSeq) inside the tx because AppendTurnTx requires Seq>0 and the conversations allocator is unexported"
    - "Lifecycle-owned sync.Once + single done channel for a bounded worker-drain wait (vs a fresh waiter goroutine per Stop) — proven by a runtime.NumGoroutine delta test, not goleak, since one waiter is legitimately blocked while a worker is hung"
    - "db_integration fault injection: a test committer runs the REAL claim + a forced error in one db.WithTx to prove rollback-on-append-failure; an embedded *PoolResumeCommitter override forces a CommitPause rollback to prove pause-exposure atomicity"

key-files:
  created:
    - "internal/runner/resume_committer.go — PoolResumeCommitter (atomic) + splitResumeCommitter (fallback) + allocateResumeTurnSeq"
    - "internal/runner/runner_wiring.go — extracted embed-wiring + CloseLearner (deep-refactor-on-touch: keep runner.go <=600)"
    - "internal/runner/resume_committer_test.go, runner_resume_batch_atomic_test.go, runner_stop_leak_test.go — unit tier"
    - "internal/runner/integration_helpers_test.go + runner_resume_single_atomic_integration_test.go + runner_resume_batch_atomic_integration_test.go + runner_pause_exposure_integration_test.go — db_integration tier"
    - "scripts/run_runner_integration.sh — WSL live-PG runner integration invoker"
  modified:
    - "internal/runner/interfaces.go — ResumeCommitter interface + ResumeClaim type"
    - "internal/runner/runner.go — Deps.ResumeCommitter + Runner.resumeCommitter (nil-default) + stopOnce/stopDone (init in New)"
    - "internal/runner/runner_resume.go — SubmitAnswer/SubmitAnswers rewired to the committer; answerTurn helper; waitWorkers single-Once/stopDone"
    - "internal/runner/runner_persist.go — turnTracker.pauseInserts; persistPause accumulates (no Insert); flushPause → CommitPause"
    - "cmd/aura/chat.go — inject NewPoolResumeCommitter(pool, convStore, pauseStore)"
    - "internal/runner/{runner_persist_test,runner_errorpaths_test,runner_more_test}.go — updated for the D-05 accumulate-then-flush contract"

key-decisions:
  - "The turn seq is reserved inside the committer tx via the shared sqlc queries (LockConversationForTurnAppend + NextConversationTurnSeq), NOT via a new exported conversations seam. AppendTurnTx requires Seq>0 and never allocates; conversations.allocateTurnSeq is unexported and the plan's file scope fences out the conversations package. D-02 explicitly blesses using sqlc.New(tx)'s query surface directly for cross-store ops, so allocateResumeTurnSeq is ~8 lines of glue over the generated queries (documented), not a business-logic dup — it avoids a cross-package export minted only for the runner."
  - "The split fallback is claim-first for BOTH single and batch (fixing the pre-34-06 inject-first bug even pool-less), but is non-atomic by construction (no tx). Atomic retryability (append-fail-after-claim → resumed_at IS NULL → retry) is therefore an integration-tier property of the PoolResumeCommitter only; the unit test for SubmitAnswers append-failure was narrowed to assert error-surfacing (not retryability), with the retryability proof moved to runner_resume_single_atomic_integration_test.go. This is a genuine contract change (D-04 claim-first ordering), justified in the commit."
  - "tr.pauses (for the assistant tool_call turn) and tr.pauseInserts (the paused_states rows) are kept as parallel accumulators rather than deriving one from the other: assistantAskUserToolCalls needs []agent.PauseOption which is already-marshaled JSON in InsertParams.Options, so deriving would require an unmarshal round-trip. Minimal, matches the plan's 'accumulate InsertParams (minted token)'."
  - "The leak worker is spawned via maybeAutoTitle directly (not a full Turn): the property under test is waitWorkers/Stop's waiter lifecycle and the worker is identical either way; a full Turn would additionally require the same hung client to serve the agent's round without hanging (a client-discriminator complication). The hung client honors ctx.Done() (titleTimeout-bounded worker ctx) and is deterministically unblocked + joined in t.Cleanup, so goleak stays green with no ignore."

patterns-established:
  - "ResumeCommitter: a consumer-side cross-store commit seam with an atomic pool-owning impl injected at the composition root + a non-atomic pool-less fallback default"
  - "runtime.NumGoroutine delta (not goleak) to assert a bounded-waiter lifecycle when one waiter is legitimately blocked; deterministic t.Cleanup unblock keeps the package goleak.VerifyTestMain green"
  - "db_integration fault injection for cross-store tx atomicity (real claim + forced rollback; embedded-committer CommitPause override)"

requirements-completed: [LOOP-02, LOOP-03, LOOP-04, LOOP-11]

coverage:
  - id: D1
    description: "LOOP-02/F-004 — SubmitAnswers → CommitResumeBatch claims ALL (sorted-token, deadlock-free) then appends ALL in one tx; a duplicate/concurrent batch rolls the whole tx back → exactly one answer per pause, loser ErrPauseNotFound, no orphan RoleTool."
    requirement: "LOOP-02"
    verification:
      - kind: unit
        ref: "internal/runner/runner_resume_batch_atomic_test.go#TestResumeBatch_{DuplicateInjectsExactlyOneAnswerPerPause,PartiallyResolvedAppendsNothing}"
        status: pass
      - kind: integration
        ref: "internal/runner/runner_resume_batch_atomic_integration_test.go#TestResumeBatch_ConcurrentDuplicate_Integration (2 goroutines, live PG: 1 win + 1 ErrPauseNotFound, deadlock-free, exactly 2 answers)"
        status: pass
    human_judgment: false
  - id: D2
    description: "LOOP-03/F-029 — SubmitAnswer → CommitResume claims + appends in one tx; a duplicate token → ErrPauseNotFound (no second answer); an append failure after the claim rolls back → resumed_at IS NULL → retry succeeds."
    requirement: "LOOP-03"
    verification:
      - kind: unit
        ref: "internal/runner/resume_committer_test.go#TestSplitResumeCommitter_CommitResume_ClaimsBeforeAppend; runner_resume_atomic_test.go#TestSubmitAnswer_DuplicateResumeInjectsExactlyOneAnswer (regression)"
        status: pass
      - kind: integration
        ref: "internal/runner/runner_resume_single_atomic_integration_test.go#TestResumeSingle_{AppendFailureAfterClaimRollsBack,DuplicateIsAtomic}_Integration"
        status: pass
    human_judgment: false
  - id: D3
    description: "LOOP-04/F-030 — persistPause no longer inserts; flushPause → CommitPause writes the assistant ask_user tool_call turn + all N paused_states rows in one tx, so a pause is consumable only after durable wire-valid history; a flush failure leaves NEITHER."
    requirement: "LOOP-04"
    verification:
      - kind: unit
        ref: "internal/runner/resume_committer_test.go#TestSplitResumeCommitter_CommitPause_...; runner_errorpaths_test.go#TestFlushPause_PauseInsertError; runner_multipause_test.go#TestMultiPause_SingleAssistantTurn_CR02"
        status: pass
      - kind: integration
        ref: "internal/runner/runner_pause_exposure_integration_test.go#TestFlushPause_{HappyExposesPauseAndTurnAtomically,FailureHidesPauseAndTurn}_Integration"
        status: pass
    human_judgment: false
  - id: D4
    description: "LOOP-11/F-045 — waitWorkers uses one lifecycle-owned sync.Once + stopDone; repeated Stop on a hung title worker does not grow runtime.NumGoroutine; the leak test deterministically unblocks + joins its worker so the package goleak stays green."
    requirement: "LOOP-11"
    verification:
      - kind: unit
        ref: "internal/runner/runner_stop_leak_test.go#TestStop_HungWorkerDoesNotLeakWaiterGoroutines (NumGoroutine delta; verified to catch the per-call-waiter regression: base=4→after=24 with the bug)"
        status: pass
    human_judgment: false

# Metrics
duration: ~2h
completed: 2026-07-03
status: complete
---

# Phase 34 Plan 06: Atomic HITL resume/pause + Stop leak fix Summary

**Made HITL resume/pause durable through ONE cross-store `db.WithTx` — a new `ResumeCommitter` seam (`PoolResumeCommitter` atomic + `splitResumeCommitter` fallback) wiring `SubmitAnswer`/`SubmitAnswers`/`flushPause` onto the 34-05 `*Tx` store methods (no ledger, no migration; the `WHERE resumed_at IS NULL` conditional update is the idempotency key) — and replaced `waitWorkers`' per-call waiter with a single `sync.Once`+`stopDone` so a hung title worker no longer leaks a goroutine per `Stop`.**

## Performance

- **Duration:** ~2h (3 task commits 11:16 → 11:50; incl. the db_integration harness build + a live-PG debug loop)
- **Tasks:** 3 (Task 1/2 `tdd=true`, Task 3 `auto`)
- **Files:** 18 (10 created, 8 modified)

## Accomplishments

- **Task 1 — ResumeCommitter seam + injection (`c4496ed2`):** `ResumeCommitter` interface + `ResumeClaim` in `interfaces.go`; `PoolResumeCommitter` (pool + concrete stores, each method one `db.WithTx` composing `MarkResumedTx`/`MarkResumedBatchTx`/`InsertTx` + `AppendTurnTx`, reserving the turn seq under the conversation row-lock) + `splitResumeCommitter` (pool-less non-atomic fallback) in `resume_committer.go`; `runner.New` nil-defaults to the split impl; `cmd/aura/chat.go` injects `NewPoolResumeCommitter(pool, convStore, pauseStore)`. Extracted the embed-wiring + `CloseLearner` to `runner_wiring.go` (runner.go 587→550, deep-refactor-on-touch).
- **Task 2 — atomic rewire + atomic flushPause (`5f7a8dd5`):** `SubmitAnswer`→`CommitResume`, `SubmitAnswers`→`CommitResumeBatch` (claim-all-then-append-all, replacing the inject-first bug), `persistPause` stops inserting (accumulates minted-token `InsertParams` in `tr.pauseInserts`), `flushPause`→`CommitPause` (assistant tool_call turn + N pause rows one tx). Full db_integration tier + `scripts/run_runner_integration.sh` (self-seeds the local identity).
- **Task 3 — Stop goroutine-leak fix (`7e402699`):** `waitWorkers` spawns the wg-drain waiter at most once via `stopOnce`+`stopDone`; the `runtime.NumGoroutine`-delta test proves repeated `Stop` on a hung worker does not accumulate waiters and deterministically joins the worker so goleak stays green.

## Task Commits

1. **Task 1: ResumeCommitter seam + composition-root injection** — `c4496ed2` (feat)
2. **Task 2: atomic single/batch resume + atomic pause flush** — `5f7a8dd5` (feat)
3. **Task 3: single lifecycle-owned Stop waiter** — `7e402699` (fix)

## Files Created/Modified

- `internal/runner/interfaces.go` — `ResumeCommitter` interface + `ResumeClaim`
- `internal/runner/resume_committer.go` (new) — `PoolResumeCommitter`/`splitResumeCommitter`/`allocateResumeTurnSeq`
- `internal/runner/runner.go` — `Deps.ResumeCommitter` + `resumeCommitter` (nil-default) + `stopOnce`/`stopDone`
- `internal/runner/runner_wiring.go` (new) — extracted `wireToolSearchEmbedder`/`CloseLearner`
- `internal/runner/runner_resume.go` — committer rewire + `answerTurn` + single-Once `waitWorkers`
- `internal/runner/runner_persist.go` — `turnTracker.pauseInserts`; `persistPause` accumulate-only; `flushPause`→`CommitPause`
- `cmd/aura/chat.go` — `NewPoolResumeCommitter` injection
- Tests: `resume_committer_test.go`, `runner_resume_batch_atomic_test.go`, `runner_stop_leak_test.go` (unit); `integration_helpers_test.go`, `runner_resume_single_atomic_integration_test.go`, `runner_resume_batch_atomic_integration_test.go`, `runner_pause_exposure_integration_test.go` (db_integration); updated `runner_persist_test.go`/`runner_errorpaths_test.go`/`runner_more_test.go`
- `scripts/run_runner_integration.sh` (new)

## A1 Manual Verification (token emission path — plan Manual-Only)

**Confirmed: moving `pause.Insert` from `persistPause` to `flushPause` surfaces no token early.** `agent.AwaitingInput` (`internal/agent/event.go:130`) has **no `Token` field** — the token is minted internally in `persistPause` (`uuid.NewV7`) and never leaves the tracker before the flush insert. Consumers (AG-UI approvals SSE, REPL) learn tokens exclusively via post-flush store reads (`ListPendingAll`/`PendingFor`), which only return rows after `CommitPause` inserts them. The pause Event a consumer observes pre-flush carries no token, so the deferred insert changes no token-availability timing (the `flushOnce` defer runs before the iterator returns to the caller's post-loop read).

## Decisions Made

- **Turn-seq reserved via shared sqlc queries inside the committer tx, not a new conversations export.** `AppendTurnTx` requires `Seq>0` and never allocates; `conversations.allocateTurnSeq` is unexported and the plan fences the conversations package out of scope. Per D-02 ("`sqlc.New(tx)` already exposes every needed query"), `allocateResumeTurnSeq` calls `LockConversationForTurnAppend` + `NextConversationTurnSeq` on the tx's `*sqlc.Queries` (~8 documented lines of glue, not a logic dup), avoiding a cross-package export minted only for the runner.
- **Split fallback is claim-first (fixing inject-first) but non-atomic; atomic retryability is integration-only.** See Deviations #1.
- **Parallel `tr.pauses` + `tr.pauseInserts` accumulators** (assistant-turn build needs `[]agent.PauseOption`, `InsertParams.Options` is already-marshaled JSON — deriving would need an unmarshal round-trip).
- **Leak worker spawned via `maybeAutoTitle` directly**, with a ctx-honoring hung client + deterministic `t.Cleanup` unblock — the waiter-lifecycle property is identical to a full Turn without the client-discriminator complication.

## Deviations from Plan

### Auto-fixed / contract adjustments

**1. [Rule 1 — contract change] SubmitAnswers append-failure unit test narrowed; atomic retryability moved to the integration tier.**
- **Found during:** Task 2 (full unit suite).
- **Issue:** `TestSubmitAnswers_InjectError` (existing) asserted `unresolvedCount==1` after an append failure — a retryability property the OLD inject-first ordering had *accidentally*. The new default committer is the pool-less **split** fallback (claim-first, non-atomic), so a failed append after the claim leaves the pause claimed — the exact non-atomicity the `PoolResumeCommitter` fixes atomically.
- **Fix:** Narrowed the unit test to assert error-surfacing only; added the atomic retryability proof (append-fail-after-claim → `resumed_at IS NULL` → retry) as a live-PG `db_integration` test (`TestResumeSingle_AppendFailureAfterClaimRollsBack_Integration`).
- **Files:** internal/runner/runner_more_test.go, runner_resume_single_atomic_integration_test.go
- **Commit:** `5f7a8dd5`

**2. [Rule 1 — contract change] persistPause/flushPause tests updated for the D-05 accumulate-then-flush move.**
- **Found during:** Task 2.
- **Issue:** `TestPersistPause_{ForwardsProxiedIDs,DirectPauseLeavesProxiedNil,ForwardsResumeContext}` read `pause.lastInsert`, and `TestPersistPause_InsertError` expected `persistPause` to fail on an Insert error — but `persistPause` no longer inserts (D-05).
- **Fix:** The forwarding tests assert `tr.pauseInserts[0]`; the Insert-error test became `TestFlushPause_PauseInsertError` (the error now surfaces at `flushPause`'s `CommitPause`). `TestFlushPause_AppendError` gains a matching `pauseInserts` entry (persistPause populates `pauses`+`pauseInserts` in lock-step).
- **Files:** internal/runner/runner_persist_test.go, runner_errorpaths_test.go
- **Commit:** `5f7a8dd5`

**3. [Rule 3 — blocking infra] Created the internal/runner db_integration tier from scratch.**
- **Found during:** Task 2.
- **Issue:** The runner package had **no** db_integration harness (its comment explicitly said "there is no db_integration tier in this package"), but the plan requires live-PG atomicity proofs.
- **Fix:** New `integration_helpers_test.go` (migratedRunnerPool + self-seeding local identity + real-store Runner builder) + `scripts/run_runner_integration.sh` (mirrors run_conversations_integration.sh). No second `TestMain` (the untagged `main_test.go` goleak TestMain covers both tiers).
- **Files:** internal/runner/integration_helpers_test.go, scripts/run_runner_integration.sh
- **Commit:** `5f7a8dd5`

---

**Total deviations:** 3 (2 test-contract changes for D-04/D-05, 1 required test infra). No production-behavior scope creep.

## Issues Encountered

- **The restrictive `-run 'Resume|Pause|Multipause'` filter masked a real failure.** Two key integration tests (`...AppendFailureAfterClaimRollsBack`, `...ConcurrentDuplicateBatch`) did not contain those tokens, so the plan's stated filter skipped them and the first run showed a false "ok". Running `-run Integration` exposed a genuine FAIL; the tests were then renamed (`TestResumeSingle_*`, `TestResumeBatch_*`) so the plan's exact filter now exercises all five. (No-skip-as-green in action.)
- **Real-store `LoadHistory` synthesizes a crash-recovery placeholder RoleTool for a pending ask_user call** ("previous result unknown after crash recovery…") to keep history wire-valid — the fake store does not, so a `countToolAnswers(LoadHistory)==0` assertion failed spuriously. Fixed by probing the persisted `conversation_turns` rows directly (`countPersistedToolTurns`) for the "no orphan answer after rollback" invariant, using `LoadHistory` only for wire-validity + content.

## Known Stubs

None — the seam is fully wired (production injects the atomic `PoolResumeCommitter`; the split fallback is a real, tested impl, not a placeholder). No hardcoded-empty/placeholder data paths.

## Threat Flags

None — no new network/auth/file-access/schema surface. The change is runner-package composition over the existing `db.WithTx` seam + 34-05 store methods (no migration). It IMPLEMENTS the T-34-B (deadlock-free batch), T-34-C (single-tx claim+append repairable), T-34-D (atomic pause exposure), and T-34-K (no Stop goroutine leak) mitigations from the plan's threat register.

## Next Phase Readiness

- LOOP-02/03/04/11 close here; the phase's remaining work (34-verify / code-review / audit gates) can proceed. All 6 Phase-34 plans are executed.
- Deferred (as designed): the external resume-relay outbox (only if `applyResumeHook`'s swarm relay ever needs exactly-once external delivery); `conversations/sweeper.go`'s analogous per-call-waiter pattern (F-045 scoped runner_resume.go only) — a Phase-35 note is in `waitWorkers`.
- **Environment note:** the shared-PG `local` identity was wiped by a parallel session (FK 23503) and re-seeded before the integration tiers; the harness now self-seeds idempotently, but re-seed again if a parallel session wipes it before the next live run.

## Self-Check: PASSED

- **Files exist:** resume_committer.go, runner_wiring.go, integration_helpers_test.go + the 6 new test files, scripts/run_runner_integration.sh — all present (created); interfaces.go, runner.go, runner_resume.go, runner_persist.go, cmd/aura/chat.go (modified) — all present.
- **Commits exist:** `c4496ed2` (Task 1), `5f7a8dd5` (Task 2), `7e402699` (Task 3) — all in git history, each `git show --stat` clean-scoped (no parallel-session files).
- **Gates:** unit `go test -race ./internal/runner/` ok + goleak-clean WITH the leak test present; db_integration `-run 'Resume|Pause|Multipause'` all 5 tests PASS on the live stack (real execution, 1.7s); leak test verified to CATCH the regression; `go vet ./internal/runner/ ./cmd/aura/` clean; `go build ./...` green; every touched file ≤600 LOC; no new migration, no ledger, no SERIALIZABLE, sweeper.go untouched.

---
*Phase: 34-agent-loop-correctness-durable-ledger*
*Completed: 2026-07-03*
