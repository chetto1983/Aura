# Phase 34: Agent-Loop Correctness + Durable Ledger — Research

**Researched:** 2026-07-01
**Domain:** Go agent-loop correctness, cross-store Postgres transactions (pgx/sqlc), `os.Root` path fencing, HITL pause/resume durability, goroutine-leak lifecycle
**Confidence:** HIGH (every decision verified against the live tree at HEAD; drift and already-done findings flagged inline)

> **Scope note (per phase framing):** Decisions **D-01..D-15** in `34-CONTEXT.md` are the binding contract and are NOT re-opened here. This document delivers only the three researcher value-adds: (1) a Nyquist-grade **Validation Architecture** (primary), (2) **implementation gotchas/landmines**, and (3) a **code-accuracy audit** of the locked decisions against the current tree. No new packages are installed (stdlib `os.Root` + existing `pgx/v5` + `sqlc`), so there is no Package Legitimacy Audit and no Standard Stack table — see *Stack* below.

## Summary

All 15 locked decisions are implementable against the current tree, and the reusable seams CONTEXT.md names (`db.WithTx`, `atomicWriteFile`, the dedup-trip path, `orphan_scan.go`, `turnSidecarPath`) all exist and behave as described. **Two findings are already or partially shipped** and materially shrink their tasks: **LOOP-08/F-031 is fully implemented** (`runToolRecovering` at `internal/agent/llm_agent_parallel.go:66-96` resolves the `Mutating` bit pre-exec and copies it into the panic-recovery result, wired end-to-end into `a.sideEffected` and `gateCompletion`) — only a regression test remains; and **LOOP-07/F-010's atomic-write half is already shipped** (`fs_write` at `internal/agent/tools/fs_write.go:63` already calls `atomicWriteFile`), so the genuine remaining work is *mode/permission preservation on overwrite*, which is **net-new with no in-repo pattern to copy** (both `fs_write` and `fs_edit` hardcode `0o644`).

The correctness-critical core (D-02/03/04/05) hinges on cross-store `db.WithTx` transactions that **cannot be unit-tested through the existing `fakeDBTX` harness** — `db.WithTx` calls `pool.Begin` on a concrete `*pgxpool.Pool` (documented at `internal/conversations/store_fakedbtx_test.go:7-10`). Atomic rollback, all-or-nothing batch resolution, and pause-exposure atomicity **must be `db_integration` tests against live Postgres**. Two non-obvious landmines dominate the risk: (a) `MarkResumedBatch` iterates a Go `map` (random order) — two concurrent overlapping batches can **deadlock (Postgres 40P01)**, so the D-04 tx must claim pauses in **sorted token order**; and (b) the D-08 `os.Root` change makes the reader **reconstruct** the sidecar path, so the existing `TestLoadHistory_FakeSidecar*` fixtures (which place the file at an arbitrary temp path) will legitimately break and must be relocated to the reconstructed path.

**Primary recommendation:** Split validation strictly into a **unit tier** (reject logic, mode preservation, `os.Root` fence via tmpfs, int32 guard, `Stop` goroutine-count, mutating-panic `sideEffected`) and a **`db_integration` tier** (every cross-store tx: single/batch atomic resume, pause-exposure atomicity, append-failure repairability, spilled-search exclusion, crash-orphan reconciliation). Treat the two "already-shipped" findings as *verify-plus-test*, not *implement*.

## User Constraints

The binding contract is `34-CONTEXT.md` (`<decisions>` D-01..D-15, `<deferred>`, `<canonical_refs>`). Do not re-derive it here. The load-bearing constraints the planner must honor:

- **D-02/D-07: one `db.WithTx`, NO ledger, NO new migration.** Changing `MarkPausedStateResumed :exec → :execrows` is a *query annotation* (sqlc regen), not a schema migration — D-07 holds. A new read-only query for D-09 (referenced-seq lookup) is likewise not a migration.
- **D-01: hard-reject the whole mixed step** (terminal `text_response` + ANY sibling), not "allow read-only siblings."
- **D-08: reconstruct sidecar path from `(runDir, convID, seq)` via `os.Root`; the DB column is a did-spill flag only.**
- **D-10: document + assert** the spilled-content search exclusion (no preview column/index).
- **D-11: deterministic unsupported error** for outside-workspace `send_file` (no approval route).
- **Deferred (Phase 35+):** tool `Mutating` classification hardening, ToolGateway/ledger reservation, `send_file` egress subsystem, tsvector search, power-loss fsync. Do NOT pull these in.

## Phase Requirements

| ID | Finding | Code site (verified) | Research status |
|----|---------|----------------------|-----------------|
| LOOP-01 | F-003 terminal exclusivity | `internal/agent/llm_agent_dispatch.go` `dispatch()` | Verified; **naming drift** + **double-terminal gap** (see audit) |
| LOOP-02 | F-004 batch resume atomic | `internal/runner/runner_resume.go` `SubmitAnswers` (inject-first bug confirmed) | Verified; **map-order deadlock landmine** |
| LOOP-03 | F-029 single resume atomic | `runner_resume.go` `SubmitAnswer` (documented residual at :79-85) | Verified; needs cross-store tx |
| LOOP-04 | F-030 pause exposure atomic | `runner_persist.go` `persistPause`→`flushPause`; `runner.go:432-445` deferred `flushOnce` | Verified |
| LOOP-05 | F-005 sidecar path fence | `store.go:286` `loadTurns` **and** `store_branch.go:87` `loadBranchTurns` (both `os.ReadFile(t.ContentSidecarPath)`) | Verified BOTH vuln reads |
| LOOP-06 | F-009 send_file approval | `send_file.go:165-168` `outsideWorkspaceResult` | Verified; **no consumer of the advertised route exists** |
| LOOP-07 | F-010 fs_write atomic + mode | `fs_write.go:63` (atomic **already done**); `fs.go:115` `atomicWriteFile` | **Partially shipped** — mode-preservation is the remaining net-new work |
| LOOP-08 | F-031 mutating-panic classification | `llm_agent_parallel.go:66-96` `runToolRecovering` | **ALREADY IMPLEMENTED** — test-only |
| LOOP-09 | F-040 crash-orphan reconcile | `orphan_scan.go:61` `scanConversationOrphans` | Verified; **`.content`/`.result` co-location hazard is real** |
| LOOP-10 | F-048 spilled search exclusion | `conversation_turns.sql:30-37` (locked trigram) + `store.go:310` | Verified; docs + test only |
| LOOP-11 | F-045 Stop goroutine leak | `runner_resume.go:269-281` `waitWorkers` | Verified leak; fix = single lifecycle-owned done chan |
| QUAL-04a | int32 guard | `askuser/store.go:231` `ListRecent int32(limit)` | Verified unguarded; mirror `ListPendingAll:196-199` |
| QUAL-04b | double-Validate/pool-close | `cmd/aura/chat.go:178` + `:197` | Verified; possible overlay-branch pool leak at `:163` |

## Locked-Decision Code-Accuracy Audit

Every decision was verified against HEAD. Decisions are NOT changed; drift and already-done facts are reported so the planner scopes correctly.

### Already-shipped / partially-shipped (task scope shrinks)

- **LOOP-08 / D-13 — ALREADY IMPLEMENTED.** `runToolRecovering` (`llm_agent_parallel.go:66-96`) already resolves `mutating := tool.Spec().Mutating` **before** `runTool` and copies it into the panic-recovery `toolRunResult{Mutating: mutating}`. The chain is complete: `dispatch.go:101-103` sets `a.sideEffected = true` from `run.Mutating`; `gateCompletion` (`llm_agent_completion.go:58`) gates on `a.sideEffected`. There is an explicit `// (F-031)` comment. **Remaining work: the regression test only** (the audit's "mutating fake panics after side effect → assert `sideEffected` armed"). Requirement box is still `[ ]` in REQUIREMENTS.md, so it must be closed with a test, not code.
- **LOOP-07 / D-12 — atomic half shipped, mode half net-new.** `fs_write.go:63` already calls `atomicWriteFile` (comment `AG-045`); the primary F-010 concern (direct `os.WriteFile`) is gone. **CONTEXT.md's "preserve mode … like fs_edit" is misleading:** `fs_edit.go:87` **also** hardcodes `0o644` — there is *no* existing mode-preservation pattern to mirror. The genuine remaining deliverable is: stat the existing target and pass its mode to `atomicWriteFile` (new file → `0o644`), applied to `fs_write` (the requirement) and `fs_edit` (deep-refactor-on-touch consistency).

### Drift (references that don't match the tree — decisions still valid)

- **D-01 helper names are fictional.** CONTEXT.md `<canonical_refs>` cites `splitTerminalCall` and `runRunnableBatch` in `llm_agent_dispatch.go`; **neither exists** (grep-confirmed). The real entry point is `dispatch()` with an **inline** partition producing `terminalIdx` + `runnable` (`dispatch.go:18-28`). The reusable helpers that DO exist: `appendSyntheticToolResults` (`llm_agent.go:448`), `maybeRecover` (`llm_agent_finalize.go:54`), `finalize` (`llm_agent_finalize.go:113`), `terminalTool = "text_response"` (`llm_agent.go:35`). The dedup-trip at `dispatch.go:53-60` is the exact template to copy.
- **D-08 "first concrete use of `os.Root`" is accurate.** A precise grep for `os.OpenRoot`/`os.OpenInRoot`/`*os.Root` returns **zero** call sites in non-test `internal/` code. The `store.go:357-362` "os.Root no-follow cascade" comment on `PurgeConversationDir` describes *intent* (the sandbox cleaner is Lstat-based). So there is **no in-repo `os.Root` precedent** — rely on the Go 1.25 official API (`os.OpenRoot` + `Root.ReadFile`); the mirror target `read_tool_output.go` uses reconstruct-then-`os.Open`, **not** `os.Root`, so D-08 is a *superset* (adds symlink-leaf neutralization the tool reader lacks).

### Gaps in the proposed approach (planner must decide/close)

- **D-01 double-`text_response` not caught by the proposed condition.** D-01b's short-circuit `terminalIdx >= 0 && len(runnable) > 0` does **not** fire for a step containing *only* multiple `text_response` calls (the 2nd is `continue`-skipped at `dispatch.go:23`, `runnable` stays empty). To also fix the "2nd terminal silently dropped" latent bug D-01b claims to fix, the partition must count terminals and the condition must be `terminalIdx >= 0 && (len(runnable) > 0 || terminalCount > 1)`. Add a double-terminal test.
- **QUAL-04b possible real leak.** `chat.go:163` early-returns on `overlayErr != nil || !ok` **without** closing the pool `openSettingsOverlayPool` may have opened. Verify that helper's contract (does it ever return a live pool alongside `!ok`/err?); if so, add a close (or adopt the deferred-close-on-error refactor D-15b suggests). The double `cfg.Validate()` at `:178`/`:197` is partly intentional (`:178` fails fast *before* `db.Open`); collapsing to one must preserve that or justify keeping both.

### Confirmed-accurate (no drift)

- **D-03 `:exec→:execrows` is safe.** The generated `q.MarkPausedStateResumed` has **zero hand-written callers** — `MarkResumed`/`MarkResumedBatch` use a raw `markResumedSQL` const + `tag.RowsAffected()` precisely because `:exec` discards the CommandTag (`askuser/store.go:267,277-282,311`). Changing the annotation and regenerating breaks nothing; then the store calls the new `(int64, error)`-returning generated fn inside the shared tx and drops the raw const. `sqlc.yaml` (`emit_interface: true`, pgx/v5) + `make sqlc` (`sqlc generate`, CLI pinned `v1.31.1`) confirmed.
- **D-11 has no consumer to unwire.** `send_file_outside_workspace` appears only in `send_file.go:167` (the advertised string) + docs — **no resume hook consumes it** (confirms F-009). The fix is a ~5-LOC rewrite of `outsideWorkspaceResult` + updating `send_file_test.go`.
- **D-04/D-05/D-06** confirmed: `SubmitAnswers` is inject-first (`runner_resume.go:121-130`); the `WHERE resumed_at IS NULL` conditional update + `RowsAffected==0 → ErrPauseNotFound` gate is the idempotency key (`paused_states.sql:48-53`, `askuser/store.go:271`); `flushOnce` is deferred and runs on every return path including consumer-stop-on-pause (`runner.go:432-445`).
- **D-09** confirmed: `scanConversationOrphans` only removes whole *orphan* dirs, never intra-*live*-dir `.content`; `tmpTTL = 24h` (`orphan_scan.go:19`) is the grace pattern; `NewRunDirSweeper` reuses `ScanOrphans` so boot + interval both get the fix for free (`sweeper.go:55-64`).
- **D-15a** confirmed: `ListRecent` at `askuser/store.go:231` does unguarded `int32(limit)`; the guard pattern already exists in the same file at `ListPendingAll:196-199`.

## Stack (all in-tree; no new dependencies)

| Component | Version | Role in this phase |
|-----------|---------|--------------------|
| Go stdlib `os.Root` | Go 1.26.4 (`go.mod:3`; `Root.ReadFile` since 1.25, `os.OpenInRoot` since 1.24) | D-08 sidecar fence (symlink-safe reconstructed reads) |
| `github.com/jackc/pgx/v5` + `pgxpool` | in `go.mod` | D-02/03/04/05 cross-store `db.WithTx` |
| `sqlc` | CLI `v1.31.1` (`Makefile:6`), config v2 (`sqlc.yaml`) | D-03 `:exec→:execrows` regen (`make sqlc`) |
| `go.uber.org/goleak` | in `go.mod` | D-14 lifecycle; wired package-wide via `TestMain` in runner/conversations/askuser |
| existing `atomicWriteFile` (`fs.go:115`) | — | D-12 (already used; extend for mode) |

**No external packages are installed this phase.** Package Legitimacy Audit: N/A (verified — no `npm`/`pip`/`cargo`/`go get` of new modules; only stdlib + already-vendored deps).

## Implementation Gotchas / Landmines

1. **`os.Root` needs a RELATIVE path; `turnSidecarPath` returns ABSOLUTE.** `turnSidecarPath` (`store_helpers.go:117`) yields `runDir/conversations/<id>/<seq>.content` (absolute). `Root.ReadFile` takes a path relative to the opened root and rejects absolute paths + `..` + escaping symlinks. Implementation: `root, _ := os.OpenRoot(s.runDir)` (assert `filepath.IsAbs(runDir)` first, mirroring `read_tool_output.go:76-78`), then `root.ReadFile(path.Join("conversations", convID, fmt.Sprintf("%d.content", seq)))`. Extract ONE shared helper (e.g. `readTurnSidecar(convID, seq)`) and call it from **both** `loadTurns` and `loadBranchTurns` (identical vuln, DRY per deep-refactor-on-touch). Keep `validateID(convID)` before the join. Preserve the current behavior that a *missing* sidecar for a spilled turn is a hard error (`store.go:287-290`), not silent empty.

2. **`MarkResumedBatch` map-iteration → concurrent-deadlock (Postgres 40P01).** `SubmitAnswers`/`MarkResumedBatch` iterate `map[string]ResumeAnswer` (random order). Two concurrent overlapping batches locking the same rows in different orders deadlock; Postgres kills one with `40P01`, giving a *deadlock* error instead of the clean `ErrPauseNotFound`. **Fix in the D-02 tx: sort tokens before the per-row UPDATE loop** so all batches lock in the same order → the loser blocks, rechecks under READ COMMITTED, sees `resumed_at` now set, gets `RowsAffected==0 → ErrPauseNotFound`, rolls back cleanly. This is both a correctness fix and what makes the D-04 concurrency test **non-flaky**.

3. **pgx isolation is fine at READ COMMITTED — no SERIALIZABLE, no 40001 retry needed.** The `WHERE resumed_at IS NULL` conditional UPDATE serializes correctly under the default READ COMMITTED: the second writer blocks on the row lock, then re-evaluates the predicate against the newly-committed row and matches 0 rows. Do **not** reach for SERIALIZABLE (would add `40001` retry loops and flakiness). The determinism comes from row-lock blocking + predicate recheck, guaranteed once ordering (landmine #2) prevents deadlock.

4. **`.content` and `.result` sidecars co-locate — D-09 must filter to `.content` ONLY.** Tool-output sidecars are written to `conversations/<sessionID>/<spillID>.result` (`result.go:82`) and `sessionID == conversationID` (D-26, `runner.go:572`), so a live conversation dir holds BOTH `<seq>.content` (turn spills, tracked by `content_sidecar_path`) AND `<toolCallID>-<rand>.result` (tool spills, NOT tracked in `conversation_turns`). A naive sweep would delete live `.result` files (malignant history loss). The D-09 reconcile **must** match strictly on the `.content` suffix and reconcile only those against committed rows; `.result` files are out of scope. Reuse the existing `Lstat`/symlink guard (`orphan_scan.go:77-88`).

5. **D-09 needs a new read-only query for referenced seqs (not a migration).** To know which `<seq>.content` are referenced, add a `sqlc` query like `ListSpilledSeqsForConversation` (`SELECT seq FROM aura.conversation_turns WHERE conversation_id=$1 AND content_sidecar_path IS NOT NULL`). Read-only, no schema change → D-07 holds. Regenerate with the same `make sqlc` run as D-03. Enforce the age-grace cutoff (mirror `tmpTTL`) so in-flight spills (written seconds ago, commit pending) are never swept.

6. **D-08 will break existing sidecar tests — relocate fixtures, don't fudge.** `TestLoadHistory_FakeSidecarRehydrate`/`_FakeSidecarMissing` (`store_fakedbtx_test.go:395,419`) currently write the sidecar at `filepath.Join(dir, "3.content")` and pass that exact path in the fake row. After D-08 the reader *ignores* the row's path and reconstructs `runDir/conversations/<convID>/3.content`. These tests must be updated to place the file at the reconstructed path (a legitimate contract change per CLAUDE.md "never modify tests to pass *unless the contract changed*"). This is the cleanest place to add the outside-root/traversal/symlink fence assertions.

7. **D-05 token-timing is safe but verify the emission path.** `persistPause` mints the pause token internally (`runner_persist.go:311`) and the consumer learns tokens via store reads (`ListPendingAll`/`PendingFor`), not via the pause Event. Moving `Insert` into `flushPause` (round-end, deferred `flushOnce` runs before the iterator returns to the caller's post-loop store read) is therefore transparent. **Verify** the AG-UI approvals SSE does not surface a token from the pause Event *before* flush; the code shape says it can't (token is minted after the Event is observed), but confirm during planning.

8. **D-14 fix is `sync.Once` + one done channel; the test is `NumGoroutine` delta, NOT goleak.** `waitWorkers` (`runner_resume.go:269-281`) spawns a fresh `go func(){ wg.Wait(); close(done) }()` per call — a hung title worker leaks one waiter *per Stop*. Fix: a lifecycle-owned `stopOnce sync.Once` + `stopDone chan struct{}` so the waiter is spawned at most once; N `Stop` calls reuse the same channel. A hung worker then leaves exactly **one** unavoidable blocked goroutine (Go can't kill goroutines), not N. Because that one is a real leak, the hung-worker test must assert **`runtime.NumGoroutine()` does not grow across repeated Stop**, not `goleak` (which would flag the single waiter). The package-wide `goleak.VerifyTestMain` (`runner/main_test.go:15`) covers the *clean* worker case. To hang a worker deterministically: set `titleTimeout > stopTimeout` and inject a fake `llm.Client` whose stream blocks. Note: `sweeper.go:94-105` `Sweeper.Stop` has the same per-call-waiter pattern but is called once at shutdown and is **out of scope** for LOOP-11 (F-045 names `runner_resume.go` only) — flag as a Phase-35 note, don't scope-creep.

9. **sqlc regen mechanics.** `:execrows` generates `MarkPausedStateResumed(ctx, arg) (int64, error)` and updates the `sqlc.Querier` interface (`emit_interface: true`). No test implements the full `Querier` (runner/conversations tests fake the *store* interfaces, not `Querier`), so nothing else breaks. Run `make sqlc` (needs `sqlc v1.31.1`); it reads schema from `internal/db/migrations` and regenerates `internal/db/sqlc/paused_states.sql.go` + `querier.go`. Commit the regenerated files.

10. **The cross-store tx cannot be unit-faked; resume/pause turns never spill.** `db.WithTx` calls `pool.Begin` on a concrete `*pgxpool.Pool` (`db/tx.go:22`) — the `fakeDBTX` harness explicitly cannot cover it (`store_fakedbtx_test.go:7-10`). So D-02/03/04/05 atomicity is `db_integration`-only. **Simplifier:** resume answers ("user declined" / the user's reply) and the `ask_user` assistant tool_call turn are all well under the 64 KiB cap, so the ResumeCommitter's `appendTurnTx` path **never spills** — do NOT drag `cleanupSidecarOnTxError` into the ResumeCommitter tx (that complexity belongs only to the general large-content `AppendTurn`). Assert/document the no-spill assumption.

11. **D-01 reject: pass ALL `calls` to `appendSyntheticToolResults`.** It appends a `RoleTool` for every call with a non-empty ID (`llm_agent.go:448-458`), including the `text_response` call whose assistant tool_call is in history — so the wire stays valid. Place the short-circuit immediately after the partition loop (before the hook/dedup loop at `dispatch.go:36`) so no sibling hook/exec runs. Reuse the shared `recoveryAttempts` counter via `maybeRecover` (one bounded replan, then `finalize`).

## Validation Architecture

> Seeds the Nyquist VALIDATION.md. Framework: Go `testing`, table-driven, `-race`, `go.uber.org/goleak` (package-wide `TestMain` in runner/conversations/askuser). Tiers: **unit** (`go test -race ./internal/<pkg>/`), **integration** (`go test -tags db_integration -race ./internal/<pkg>/` against live PG). Coverage floor ≥85% owned-surface; mutation ≥70% on touched critical files (per CLAUDE.md).

### Requirement → Test Map

| Req | Behavior to prove | Failure mode guarded | Test(s) — method & tier | File (new/extend) |
|-----|-------------------|----------------------|--------------------------|-------------------|
| LOOP-01 | Terminal `text_response` + mutating sibling → sibling NEVER executes; step trips replan/finalize | Side effect before terminal (F-003) | Fake mutating tool with a "called" flag; assert flag stays false + `finalize` reached. **Unit, table** (agent internal `_test.go`) | extend `internal/agent/` (new `llm_agent_terminal_reject_test.go`) |
| LOOP-01 | Terminal + **read-only** sibling → also hard-rejected (D-01, not option B) | Re-introducing F-003 via unflagged tools | Same harness, read-only sibling; assert reject. **Unit** | ″ |
| LOOP-01 | Two `text_response`, no other tool → 2nd not silently dropped | Latent double-terminal drop | Assert reject/repair (requires `terminalCount>1` condition). **Unit** | ″ |
| LOOP-02 | Duplicate batch → exactly one answer/pause, no orphan `RoleTool`, dup → `ErrPauseNotFound` | Inject-first double/orphan (F-004) | Mirror `TestSubmitAnswer_DuplicateResumeInjectsExactlyOneAnswer`, batch variant. **Unit (in-mem fakes)** + **Integration (already-resumed token → whole tx rollback, no turns persisted)** | new `runner_resume_batch_atomic_test.go` (unit) + `..._integration_test.go` |
| LOOP-02 | Two concurrent identical batches → one wins, all-or-nothing, deadlock-free | Race + 40P01 deadlock | Two goroutines + sync point on live PG; assert exactly one answer/pause, loser gets `ErrPauseNotFound` (needs sorted-token claim). **Integration, live PG** | ″ |
| LOOP-03 | `AppendTurn` failure after claim → whole tx rolls back → pause stays pending → retry works | Claimed-without-answer orphan (F-029) | Wrap the conversation append to fail inside the tx; assert `resumed_at IS NULL` after + successful retry. **Integration, live PG** (fault-injection) | new `runner_resume_single_atomic_integration_test.go` |
| LOOP-03 | Existing single-resume dup test stays green | regression | `TestSubmitAnswer_DuplicateResumeInjectsExactlyOneAnswer`. **Unit** | keep `runner_resume_atomic_test.go` |
| LOOP-04 | Pause never visible without wire-valid assistant tool_call turn | Orphan tool answers on resume (F-030) | Force `flushPause` tx to fail after consumer stops on pause; assert `ListPendingAll`/`PendingFor` show NOTHING. **Integration, live PG** | new `runner_pause_exposure_integration_test.go` |
| LOOP-04 | Happy path: after flush, pause visible AND assistant tool_call turn durable | — | Assert both present + N pauses → one assistant turn. **Integration** + multipause **Unit** (mirror `runner_multipause_test.go`) | extend `runner_multipause_test.go` |
| LOOP-05 | Outside-root / traversal / symlink sidecar reads rejected; valid rehydrates | Arbitrary file read via poisoned DB path (F-005) | fakeDBTX row with malicious `content_sidecar_path` + real tmpfs runDir; assert reconstructed read never touches the poisoned path; symlink leaf → `os.Root` refuses. **Unit (tmpfs), table** — cover both `loadTurns` & `loadBranchTurns` | extend `store_fakedbtx_test.go` (relocate fixtures) + new `store_sidecar_fence_test.go` |
| LOOP-06 | Outside-workspace `send_file` → deterministic error, no `ask_user`/`resume_context` | Infinite ask loop (F-009) | Assert result message excludes `ask_user`/`send_file_outside_workspace` and instructs copy-into-workspace. **Unit** | extend `send_file_test.go` |
| LOOP-07 | Overwrite preserves mode (0o600 stays 0o600, 0o755 stays 0o755); new file → 0o644; mid-write crash never truncates | Mode clobber + truncation (F-010) | tmpfs: create file w/ mode, `fs_write`, stat result. Atomicity: inject temp-create failure, assert original intact. **Unit, table** — apply to `fs_write` **and** `fs_edit` | extend `internal/agent/tools/fs_test.go` |
| LOOP-08 | Mutating tool that panics after side effect → `a.sideEffected` armed | Post-mutation gate skipped after panic (F-031) | Fake mutating tool writes flag then panics; assert `runToolRecovering` result `Mutating==true` and `a.sideEffected==true`. **Unit** (impl already present — test closes the box) | new `llm_agent_mutating_panic_test.go` |
| LOOP-09 | Unreferenced aged `.content` removed; referenced `.content` kept; `.result` kept; young `.content` kept; symlink not followed | Unbounded crash sidecars + malignant `.result`/history deletion (F-040) | Seed a live conv dir with (referenced, unreferenced-aged, unreferenced-young, `.result`, symlink); assert selective removal. **Integration** (real `conversationExists`/referenced-seq query) | extend `orphan_scan_test.go` |
| LOOP-10 | `>cap` turn spills (content=NULL) AND is absent from `SearchConversationTurns`; inline turn IS found | Silent search miss / false expectation (F-048) | Unique-token oversized turn + control; assert 0 hits vs 1 hit. **Integration, live pg_trgm** + doc comments on `maybeSpill`/search SQL/`models.go` | new `store_search_spill_integration_test.go` |
| LOOP-11 | Repeated `Stop` on hung worker → goroutine count does not grow; clean worker → no leak | Waiter-goroutine accumulation (F-045) | Hung fake client (`titleTimeout>stopTimeout`); capture `runtime.NumGoroutine()`, `Stop` ×N, assert Δ constant. **Unit** (NumGoroutine delta, not goleak). Clean case covered by package `goleak`. | new `runner_stop_leak_test.go` |
| QUAL-04a | `ListRecent` int32 overflow guarded | Silent narrowing panic/wrap | Table over unset/0/50/MaxInt32/overflow; assert fallback/clamp. **Unit** | extend `askuser/store_unit_test.go` |
| QUAL-04b | boot error paths close the pool; no overlay-path leak | Leaked pool on misconfig | Fake `loadConfig` returning error at each stage + pool-close spy; assert closed on every path. **Unit** | new `cmd/aura/chat_boot_test.go` |

### Sampling rate

- **Per task commit:** `go test -race ./internal/<touched-pkg>/` (unit tier, sub-second).
- **Per wave merge:** full unit matrix + `go test -tags db_integration -race ./internal/runner/ ./internal/conversations/` against the live stack (composed `AURA_DB_URL`/`AURA_DB_MIGRATE_URL`).
- **Phase gate:** `make quality-full` green (owned-surface coverage ≥85%, race-clean across the tag matrix) + mutation spot-check ≥70% on the critical files: `askuser/store.go` (batch tx), `conversations/store.go` `loadTurns` (fence), `llm_agent_dispatch.go` (reject), `runner_resume.go` (ResumeCommitter). No-skip-as-green: the `db_integration` tests `t.Fatal` under `$CI` when their env is unset.

### Wave 0 gaps (test scaffolding to create before/with implementation)

- [ ] `internal/agent/llm_agent_terminal_reject_test.go` — LOOP-01 (needs a fake mutating tool with a call-flag; check `agenttest` for an existing one).
- [ ] `internal/agent/llm_agent_mutating_panic_test.go` — LOOP-08.
- [ ] `internal/runner/runner_resume_batch_atomic_test.go` (+ `_integration`) — LOOP-02/03; reuse `fakes_test.go` fakes and add a **ResumeCommitter fake** + a fault-injecting conversation-append wrapper.
- [ ] `internal/runner/runner_pause_exposure_integration_test.go` — LOOP-04.
- [ ] `internal/runner/runner_stop_leak_test.go` — LOOP-11.
- [ ] `internal/conversations/store_sidecar_fence_test.go` + relocate fixtures in `store_fakedbtx_test.go` — LOOP-05.
- [ ] `internal/conversations/store_search_spill_integration_test.go` — LOOP-10.
- [ ] extend `orphan_scan_test.go` — LOOP-09.
- [ ] extend `send_file_test.go`, `fs_test.go`, `askuser/store_unit_test.go`; new `cmd/aura/chat_boot_test.go`.
- Framework install: none — `goleak`, `sqlc`, live-PG integration harness all already present.

*(Existing infrastructure that covers a lot for free: package-wide `goleak.VerifyTestMain`; the `fakeDBTX` harness for non-tx projection branches; `agenttest.NewFakeClient` + `newTestRunner` in-memory fakes; the live `db_integration` stack.)*

## Security Domain

| ASVS | Applies | Control in this phase |
|------|---------|----------------------|
| V5 Input Validation | yes | `validateID` (traversal/separator reject) before every sidecar path join; retained under D-08 |
| V12 Files/Resources (path traversal) | **yes (core)** | D-08 `os.Root` reconstructed reads neutralize a symlink swapped at the `.content` leaf and a poisoned DB path (F-005); D-11 removes an unwireable egress prompt |
| V6 Cryptography | no | — |
| V4 Access Control | partial | pause resume idempotency (`WHERE resumed_at IS NULL`) prevents duplicate-claim; owner-scoping is Phase 36, not here |

| Threat | STRIDE | Mitigation (this phase) |
|--------|--------|------------------------|
| Poisoned `content_sidecar_path` → arbitrary local file read into history | Information Disclosure / Tampering | D-08 reconstruct-don't-trust + `os.Root` (LOOP-05) |
| Symlink swap at sidecar leaf | Tampering | `os.Root` refuses to follow escaping symlinks; `Lstat` guard in orphan scan (LOOP-05/09) |
| Duplicate/racing resume → double side effect or orphan history | Tampering / Repudiation | single-tx claim+append, sorted-token deadlock-free batch (LOOP-02/03) |
| Crash-orphaned sensitive sidecars accumulate unbounded | Information Disclosure | age-grace `.content` reconciliation (LOOP-09) |

## Assumptions Log

| # | Claim | Section | Risk if wrong |
|---|-------|---------|---------------|
| A1 | The AG-UI approvals SSE surfaces pause tokens via store reads (post-flush), not via the pre-flush pause Event | Gotcha #7 / LOOP-04 | If the Event carries a token pre-flush, moving `Insert` to `flushPause` changes token-availability timing — planner must verify the emission path |
| A2 | `openSettingsOverlayPool` never returns a live pool alongside `!ok`/err (else `chat.go:163` leaks) | Audit / QUAL-04b | A real (if narrow) pool leak on the misconfig path |
| A3 | Resume/pause turns are always < 64 KiB, so the ResumeCommitter append never spills | Gotcha #10 | If a giant answer spills, the cross-store tx needs `cleanupSidecarOnTxError` composition (added complexity) |
| A4 | F-031/LOOP-08 was fixed post-audit as refactor-on-touch (audit still lists it P2, but code implements it with an `// (F-031)` comment) | Audit / LOOP-08 | If the fix is incomplete in some path, LOOP-08 needs more than a test — but the end-to-end chain (`runToolRecovering`→`run.Mutating`→`a.sideEffected`→`gateCompletion`) is verified intact |

## Open Questions (RESOLVED)

1. **RESOLVED: broadened the reject condition to `terminalCount > 1 || len(runnable) > 0` (also rejects a pure multi-`text_response` step) + a double-terminal test — see 34-02.** D-01 scope of the double-terminal fix — does LOOP-01 require rejecting a *pure* multi-`text_response` step (not just terminal+tool)? Recommendation: yes; broaden the condition to `terminalCount > 1 || len(runnable) > 0` and test it (cheap, closes the latent bug D-01b cites).
2. **RESOLVED: the referenced-seq reconciliation runs as a `db_integration` test exercising the new read query; age-grace mirrors `tmpTTL=24h` — see 34-04.** D-09 tier — the referenced-seq reconciliation needs the real `conversation_turns` state; recommend `db_integration` (exercises the new read query) rather than faking. Confirm the age-grace constant value (mirror `tmpTTL=24h`, Claude's discretion per D-09).
3. **RESOLVED: the `ResumeCommitter` interface is declared consumer-side in `internal/runner/interfaces.go`; the pool-owning impl is injected at the `cmd/aura/chat.go` composition root — see 34-06.** ResumeCommitter seam placement — declare the interface consumer-side in `internal/runner/interfaces.go` (matching `PauseStore`/`ConversationStore`); inject the concrete impl (owns `*pgxpool.Pool` + tx-variant store methods) at each composition root (serve/cron/telegram). Unit tests use an in-memory `ResumeCommitter` fake; the real cross-store-tx behavior is `db_integration`.

## Sources

### Primary (HIGH — verified in this session against HEAD)
- Live tree: `internal/agent/{llm_agent_dispatch,llm_agent_parallel,llm_agent,llm_agent_finalize,llm_agent_completion}.go`, `internal/agent/tools/{send_file,fs_write,fs_edit,fs,result,read_tool_output}.go`, `internal/runner/{runner,runner_resume,runner_persist,interfaces}.go`, `internal/conversations/{store,store_branch,store_append,store_helpers,orphan_scan,sweeper}.go`, `internal/askuser/store.go`, `internal/db/{tx.go,queries/paused_states.sql,queries/conversation_turns.sql}`, `cmd/aura/chat.go`, `sqlc.yaml`, `Makefile`, `go.mod`.
- Test infra: `internal/runner/{main_test,runner_resume_atomic_test,fakes_test}.go`, `internal/conversations/store_fakedbtx_test.go`.
- Greps: `os.OpenRoot` (0 non-test call sites), `MarkPausedStateResumed` (0 hand-written callers), `send_file_outside_workspace` (0 consumers), `splitTerminalCall`/`runRunnableBatch` (0 matches).
- `34-CONTEXT.md`, `.planning/REQUIREMENTS.md` (LOOP-01..11, QUAL-04), `docs/audit/bug-report.md` (F-003/004/005/009/010/029/030/031/040/045/048), `.planning/STATE.md` (Phase 33 closed, "NO new migration").
- Project skills: `golang-testing` (goleak, table-driven, `db_integration` tags, `synctest`), `golang-concurrency` (single done channel, NumGoroutine, Go 1.26 leak profile), `golang-database` (READ COMMITTED, conditional update, `SELECT FOR UPDATE`), `golang-security` (`os.Root` for path traversal, Go 1.24+), `golang-error-handling` (sentinel + `%w`), `golang-structs-interfaces` (consumer-side narrow interfaces).

### Secondary (referenced from CONTEXT.md, not re-fetched)
- Go `os.Root` blog/pkg docs (`go.dev/blog/osroot`, `pkg.go.dev/os`); pg_trgm length-normalization (`postgresql.org/docs/current/pgtrgm.html`); pgx v5 (`pkg.go.dev/github.com/jackc/pgx/v5`).

## Metadata

**Confidence breakdown:**
- Code-accuracy audit: HIGH — every decision cross-checked at file:line; drift and already-done facts grep-confirmed.
- Validation Architecture: HIGH — grounded in the existing test harnesses (`fakeDBTX` limits, `goleak` TestMains, `runner_resume_atomic_test.go` template) and the real tx/isolation semantics.
- Landmines: HIGH for #1–#6, #8–#11 (code-verified); MEDIUM for #7 (A1) pending the AG-UI emission-path check.

**Research date:** 2026-07-01
**Valid until:** ~2026-08-01 (stable backend; re-verify only if the touched files change before planning)
