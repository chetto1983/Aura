---
phase: 34-agent-loop-correctness-durable-ledger
review_path: 34-REVIEW.md
fix_scope: critical_warning
findings_in_scope: 2
fixed: 2
skipped: 0
iteration: 1
status: all_fixed
fixed_at: 2026-07-03T14:40:00Z
---

# Phase 34: Code Review Fix Report

**Fix scope:** critical + warning (WR-01, WR-02). The 3 Info findings (IN-01/02/03)
are out of scope for the default `--fix` and are left for a future `--all` pass or
manual follow-up (see below).

## Fixed

### WR-01 — `AppendTurnTx` spills a sidecar with no rollback cleanup → **FIXED** (`8e4793ea`)

**Finding:** `PoolResumeCommitter` uses `AppendTurnTx`, which carries no
`cleanupSidecarOnTxError` wrapper, on the *unenforced* assumption that resume/pause
turns are always `< turnCapBytes` (A3). A user-supplied `accept` answer flows unbounded
into the committer, so a `>turnCapBytes` answer spilled a sidecar inside the cross-store
tx; on rollback (concurrent duplicate-batch loser, or a later claim failing) that sidecar
was orphaned until the delayed boot scan reconciled it.

**Fix applied (option 2 from the review — enforce, don't assume):** `AppendTurnTx` now
checks the content length *before* `appendTurnWrites` (mirroring `maybeSpill` exactly:
`len(postgresTextSafe(content)) > turnCapBytes`) and rejects a would-be spill with the new
sentinel `conversations.ErrContentSpillUnsupported` **before any file is written**. The
caller's tx therefore rolls back cleanly with no orphan possible on this path. Chosen over
option 1 (a 32 KiB cap in the runner) because it is the single choke point for every
resume/pause append (CommitResume/CommitResumeBatch/CommitPause), rejects at the natural
`turnCapBytes` boundary rather than an arbitrary threshold, and matches `AppendTurnTx`'s
documented "no-spill" design intent.

**Behaviour note:** an `ask_user` answer larger than `turnCapBytes` (default 64 KiB — an
extreme, unexpected case under A3) is now rejected with a clear error instead of risking a
silent orphaned file. The happy path for all normal-size answers is unchanged.

**Files:** `internal/conversations/store_append.go` (sentinel + guard + doc),
`internal/runner/resume_committer.go` (doc: contract now enforced),
`internal/conversations/store_append_tx_test.go` (`TestAppendTurnTx_RejectsSpill`,
`TestAppendTurnTx_InlineAtCap`).

### WR-02 — `waitWorkers` one-shot `stopDone` returns "drained" without joining post-drain workers → **FIXED** (`6cabb8aa`)

**Finding:** the LOOP-11 fix spawned the wg-drain waiter once via `sync.Once` and closed
`stopDone` permanently on the first drain. Any auto-title worker spawned afterward
(`maybeAutoTitle` → `wg.Add(1)`) was tracked by no waiter, so every later `Stop` read the
already-closed channel and returned `true` immediately while a title worker was still in
flight. Latent today (each production caller Stops a `Runner` at most once) but live for a
shared-`Runner` `serve` daemon that Stops per session.

**Fix applied (re-arm under a mutex, per the review's proposed patch):** replaced the
one-shot `sync.Once` with a `stopMu`-guarded re-arm. While a worker runs, `stopDone` stays
non-nil so repeated `Stop` reuses ONE waiter (preserving the LOOP-11 no-per-call-leak
property on a hung worker); on a clean drain the waiter resets `stopDone` to nil so the
next `Stop` arms a fresh waiter that actually joins a post-drain worker.

**Files:** `internal/runner/runner.go` (`stopMu` replaces `stopOnce`; `stopDone` starts
nil), `internal/runner/runner_resume.go` (`waitWorkers` re-arm + doc),
`internal/runner/runner_stop_leak_test.go`
(`TestStop_ReArmsWaiterForWorkerSpawnedAfterCleanDrain` + refreshed comments; the existing
hung-worker leak test still passes).

## Skipped (out of scope — Info, no `--all`)

- **IN-01** — `fs_write`/`fs_edit` atomic rename replaces a symlink with a regular file.
  Acceptable in the deliberate single-trusted-operator no-fence context; documentation-only
  follow-up.
- **IN-02** — `splitResumeCommitter.CommitResumeBatch` appends in non-deterministic map
  order. Non-atomic compatibility path only (never a production HITL resume); impact nil.
- **IN-03** — a per-conversation DB error aborts the whole boot sidecar scan. Self-healing
  on next boot; a WARN-and-continue polish, not a correctness bug.

Re-run `/gsd-code-review 34 --fix --all` to address the Info items.

## Validation

All run natively in WSL (CGO race), touched packages only:

- `go build ./...` — OK
- `go vet ./internal/conversations/... ./internal/runner/...` — OK
- `go test ./internal/conversations/ ./internal/runner/` — PASS
- `go test -race ./internal/conversations/ ./internal/runner/` — PASS
- `golangci-lint run ./internal/conversations/... ./internal/runner/...` — 0 issues
- New/affected tests under `-race -v`: `TestAppendTurnTx_RejectsSpill`,
  `TestAppendTurnTx_InlineAtCap`, `TestStop_ReArmsWaiterForWorkerSpawnedAfterCleanDrain`,
  `TestStop_HungWorkerDoesNotLeakWaiterGoroutines` — all PASS.

---

_Fixed: 2026-07-03T14:40:00Z · Fixer: Claude (orchestrator, full-context inline) · Scope: critical+warning_
