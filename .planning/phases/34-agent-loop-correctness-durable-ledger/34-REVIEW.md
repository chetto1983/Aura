---
phase: 34-agent-loop-correctness-durable-ledger
reviewed: 2026-07-03T12:00:00Z
depth: standard
files_reviewed: 48
files_reviewed_list:
  - cmd/aura/chat.go
  - cmd/aura/chat_boot_test.go
  - internal/agent/llm_agent_dispatch.go
  - internal/agent/llm_agent_mutating_panic_test.go
  - internal/agent/llm_agent_terminal_reject_test.go
  - internal/agent/tools/fs.go
  - internal/agent/tools/fs_edit.go
  - internal/agent/tools/fs_test.go
  - internal/agent/tools/fs_write.go
  - internal/agent/tools/send_file.go
  - internal/agent/tools/send_file_test.go
  - internal/askuser/store.go
  - internal/askuser/store_unit_test.go
  - internal/conversations/orphan_scan.go
  - internal/conversations/orphan_scan_test.go
  - internal/conversations/orphan_scan_unit_test.go
  - internal/conversations/store.go
  - internal/conversations/store_append.go
  - internal/conversations/store_append_tx_test.go
  - internal/conversations/store_branch.go
  - internal/conversations/store_fakedbtx_test.go
  - internal/conversations/store_helpers.go
  - internal/conversations/store_search_spill_integration_test.go
  - internal/conversations/store_sidecar_fence_test.go
  - internal/db/queries/conversation_turns.sql
  - internal/db/queries/paused_states.sql
  - internal/db/sqlc/conversation_turns.sql.go
  - internal/db/sqlc/paused_states.sql.go
  - internal/db/sqlc/querier.go
  - internal/runner/integration_helpers_test.go
  - internal/runner/interfaces.go
  - internal/runner/resume_committer.go
  - internal/runner/resume_committer_test.go
  - internal/runner/runner.go
  - internal/runner/runner_errorpaths_test.go
  - internal/runner/runner_more_test.go
  - internal/runner/runner_pause_exposure_integration_test.go
  - internal/runner/runner_persist.go
  - internal/runner/runner_persist_test.go
  - internal/runner/runner_resume.go
  - internal/runner/runner_resume_batch_atomic_integration_test.go
  - internal/runner/runner_resume_batch_atomic_test.go
  - internal/runner/runner_resume_single_atomic_integration_test.go
  - internal/runner/runner_stop_leak_test.go
  - internal/runner/runner_wiring.go
  - scripts/run_runner_integration.sh
findings:
  critical: 0
  warning: 2
  info: 3
  total: 5
status: issues_found
---

# Phase 34: Code Review Report

**Reviewed:** 2026-07-03T12:00:00Z
**Depth:** standard
**Files Reviewed:** 48
**Status:** issues_found

## Summary

Phase 34 ("agent-loop-correctness-durable-ledger") is a transactional/concurrency
hardening slice, and I reviewed it against the phase's own invariants rather than as a
generic lint pass. The core of the phase is solid:

- **Cross-store HITL atomicity (LOOP-02/03/04):** the `ResumeCommitter` seam threads a
  single `db.WithTx` end-to-end (`MarkResumedTx` / `AppendTurnTx` / `InsertTx` all take a
  caller-supplied `*sqlc.Queries` and open no inner tx), the batch claims in sorted-token
  order (deadlock-free) and appends in sorted order (QUAL-04a), and the `:execrows`
  conditional update (`WHERE resumed_at IS NULL`) is the idempotency key. The rollback,
  duplicate-batch, and pause-hiding paths are proven by genuine `db_integration` tests that
  probe the DB rows (not reply strings) — this is not test-baby-sitting.
- **`os.Root` sidecar fence (LOOP-05):** `readTurnSidecar` reconstructs the path from
  `(runDir, convID, seq)`, ignores the DB column value entirely, asserts an absolute
  runDir, validates the conv id, and reads through `os.OpenRoot`. The fence is exercised
  against a poisoned absolute path, `..` traversal, and a swapped symlink leaf for BOTH
  loaders. Correct and well-tested.
- **Terminal-tool exclusivity (F-031) + mutating-panic classification:** the reject in
  `dispatch` is classification-independent and total (mutating sibling / read-only sibling
  / double-terminal), fires before any sibling `Execute`, and the panic path arms
  `sideEffected`. Tests pin all combinations.
- **LOOP-10 search exclusion** is enforced by SQL (`content % $1` never matches the NULL a
  spilled turn stores), and **QUAL-04b boot drain** correctly arms the close-on-error guard
  first so every early return after pool/MCP init releases both.

Two robustness defects and three lower-severity items are below. No blockers: I could not
construct a partial-commit, data-loss, injection, or traversal scenario against the
committed code.

## Warnings

### WR-01: `AppendTurnTx` spills a sidecar with no rollback cleanup — the "resume/pause turns are always < turnCapBytes" (A3) invariant is undocumented-but-unenforced against user-supplied answer content

**File:** `internal/runner/resume_committer.go:26-27`, `104-111`; `internal/conversations/store_append.go:91-112`; `internal/conversations/store_helpers.go:101-113`

**Issue:** `PoolResumeCommitter` deliberately uses `conversations.Store.AppendTurnTx`, which
carries **no** `cleanupSidecarOnTxError` wrapper, on the stated assumption that
"Resume/pause turns are always < turnCapBytes (A3), so AppendTurnTx never spills." That
assumption is never validated. The `accept` answer body flows unbounded from
`ResponseInput.Content` → `Runner.answerTurn` (`runner_resume.go:149-160`, `content =
resp.Content`) → `ResumeClaim.Turn` → `appendAnswerTx` → `AppendTurnTx` → `appendTurnWrites`
→ `maybeSpill`. With the production default cap of 65536 bytes
(`AURA_CONVERSATION_TURN_CAP_BYTES`, `config.go:364`), a user who accepts an `ask_user`
prompt with a >64 KiB pasted answer triggers a real sidecar spill inside the committer's
transaction. Consequences:

- On commit: works (spill is durable and rehydrates via the fenced reader) — no corruption.
- On rollback (e.g. the loser of a concurrent duplicate batch, or a later claim in the same
  `CommitResumeBatch` failing): the sidecar file written by an earlier `appendAnswerTx` in
  that tx is **orphaned** — no immediate cleanup exists on this code path, only the next
  boot's `ScanOrphans` reconciles it (and only after the 24h grace window). This contradicts
  the phase's own atomicity posture, where every other spill site (`AppendTurn`,
  `AppendAssistantTurnWithCacheMetric`, `ForkBranch`) wraps `cleanupSidecarOnTxError`.

**Fix:** Enforce the A3 invariant instead of assuming it. Either cap the resume answer
content before it reaches the committer:

```go
// runner_resume.go, in answerTurn or SubmitAnswer/SubmitAnswers
const maxResumeAnswerBytes = 32 << 10 // < any sane turnCapBytes
if len(resp.Content) > maxResumeAnswerBytes {
    return 0, fmt.Errorf("submit answer: content %d bytes exceeds the %d-byte resume cap",
        len(resp.Content), maxResumeAnswerBytes)
}
```

or have `AppendTurnTx` hard-reject a spill (return an error when `maybeSpill` would write a
sidecar) so the "never spills" claim is a checked contract, not a comment.

### WR-02: `waitWorkers` one-shot `stopDone` returns "drained" without joining title workers spawned after the first successful drain

**File:** `internal/runner/runner_resume.go:282-295` (with `stopOnce`/`stopDone` in `runner.go:170-175`)

**Issue:** The LOOP-11 fix spawns the wg-drain waiter exactly once via `sync.Once` and closes
`stopDone` when the WaitGroup first reaches zero:

```go
r.stopOnce.Do(func() {
    go func() { r.wg.Wait(); close(r.stopDone) }()
})
select {
case <-r.stopDone: return true
case <-time.After(timeout): return false
}
```

Once the first full drain completes, `stopDone` is closed **permanently** and the waiter
goroutine has exited. Any auto-title worker spawned afterward (`maybeAutoTitle` →
`wg.Add(1)`) is tracked by no waiter, so every subsequent `Stop`/`waitWorkers` reads the
already-closed channel and returns `true` immediately — reporting "workers drained" while a
title worker is still in flight. This breaks the phase invariant "no goroutine leaked on any
exit path (… double-Stop)": the regression test (`runner_stop_leak_test.go`) only covers
double-Stop on a *hung* worker (where `stopDone` never closes), so the drain-then-respawn
case is untested. There is also a latent `sync.WaitGroup`-reuse hazard: an `Add(1)` racing
the waiter's `Wait()` return (counter momentarily 0) can trip Go's "WaitGroup is reused
before previous Wait has returned" panic.

**Reachability caveat (why WARNING, not BLOCKER):** every current production caller invokes
`Runner.Stop` at most once per `Runner` (`chat_repl.go:56` REPL-session defer,
`cache_audit.go:230` once; the AG-UI gateway never calls it), so this is latent today. It
becomes live the moment a shared-`Runner` daemon calls `Stop` per conversation session — the
exact multi-session shape the `serve` path trends toward.

**Fix:** Re-arm the waiter under a mutex when it has drained, instead of a one-shot channel:

```go
func (r *Runner) waitWorkers(timeout time.Duration) bool {
    r.stopMu.Lock()
    if r.stopDone == nil { // (re)arm a fresh waiter
        done := make(chan struct{})
        r.stopDone = done
        go func() { r.wg.Wait(); r.stopMu.Lock(); r.stopDone = nil; r.stopMu.Unlock(); close(done) }()
    }
    done := r.stopDone
    r.stopMu.Unlock()
    select {
    case <-done: return true
    case <-time.After(timeout): return false
    }
}
```

At minimum, add a test that drains cleanly, spawns a new worker, and asserts the next `Stop`
actually waits for it.

## Info

### IN-01: `fs_write`/`fs_edit` atomic rename silently replaces a symlink with a regular file, detaching the symlinked target

**File:** `internal/agent/tools/fs.go:116-157` (`existingFileMode` + `atomicWriteFile`); `internal/agent/tools/fs_write.go:64`, `internal/agent/tools/fs_edit.go:88`

**Issue:** `existingFileMode` uses `os.Stat` (follows symlinks) so an overwrite of a symlink
reads the *target's* mode, but `atomicWriteFile` renames the temp file over the symlink
*path itself* — replacing the link with a fresh regular file and leaving the real target
untouched. A user editing `config.yaml` that is a symlink into `/etc/app/` would see the
edit land on a new local regular file while the real file stays stale. In the deliberate
no-fence, single-trusted-operator context this is acceptable (and arguably safer than
following the link out of the workspace), but the "preserves the existing file's mode"
comment glosses over the link→regular-file conversion. Consider documenting it, or `Lstat`ing
to detect and reject/handle a symlink target explicitly.

### IN-02: `splitResumeCommitter.CommitResumeBatch` appends answers in non-deterministic map order, unlike the atomic Pool impl

**File:** `internal/runner/resume_committer.go:162-179`

**Issue:** The atomic `PoolResumeCommitter.CommitResumeBatch` sorts `ordered` by token before
appending (QUAL-04a determinism). The pool-less fallback iterates `claims` in the order
`SubmitAnswers` built them — which comes from ranging a `map[string]ResponseInput`
(`runner_resume.go:124`), i.e. non-deterministic — so the fallback's answer-turn seq ordering
is not reproducible. This is only the non-atomic compatibility path (`cache_audit`, unit
tests), never a HITL resume in production, so impact is nil; flagging only because the
determinism guarantee the atomic path documents is silently not upheld by its sibling. Sort
`claims` by token in the fallback too, for parity.

### IN-03: A per-conversation DB error in the sidecar reconcile aborts the whole boot scan, skipping remaining conversations + tmp sweep + size warn

**File:** `internal/conversations/orphan_scan.go:106-119` (and `reconcileLiveConversationSidecars` at `134-141`)

**Issue:** `scanConversationOrphans` returns (aborting the loop and the subsequent
`sweepTmp`/`warnIfOversized` in `ScanOrphans`) on a `conversationExists` DB error *or* a
`reconcileLiveConversationSidecars` (ListSpilledSeqs) DB error. So a transient DB blip while
reconciling conversation #3 skips reconciliation of #4+, plus the tmp sweep and size audit,
for that entire boot. This is consistent with the pre-existing existence-check abort and is
recovered on the next boot (and `ScanOrphans`' error is non-fatal at the callsite), so it is
a mild deviation from the "individual rm failures are WARN-logged … only a structural failure
returns a wrapped error" contract rather than a correctness bug. Consider WARN-and-continue on
a per-conversation reconcile error (matching `removeOrphan`'s posture) so one conversation's
DB hiccup does not suppress the rest of the boot GC.

---

_Reviewed: 2026-07-03T12:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
