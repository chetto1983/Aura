---
phase: 01-fondamenta-concurrency-safety
fixed_at: 2026-05-10T00:00:00Z
review_path: .planning/phases/01-fondamenta-concurrency-safety/01-REVIEW.md
iteration: 1
findings_in_scope: 7
fixed: 7
skipped: 0
status: all_fixed
---

# Phase 01: Code Review Fix Report

**Fixed at:** 2026-05-10
**Source review:** `.planning/phases/01-fondamenta-concurrency-safety/01-REVIEW.md`
**Iteration:** 1

**Summary:**
- Findings in scope: 7 (1 Critical + 6 Warning; 5 Info findings out of scope)
- Fixed: 7
- Skipped: 0

All Critical + Warning severity findings were applied cleanly. Each fix was
verified with `go vet ./...`, `go build ./...`, and the targeted package
tests; the eviction-drain regression test (CR-01) and the tightened
overflow-drop invariant (WR-06) were each run multiple times to confirm
they are not flaky.

## Fixed Issues

### CR-01: `runQueueNoticeTimer` leaks and fires spurious notices for evicted/closed users

**Files modified:** `internal/concurrency/gate.go`, `internal/concurrency/gate_test.go`
**Commit:** `00ddba61`
**Status:** fixed: requires human verification (concurrency / lifetime logic — please confirm the invariant manually)

Three coordinated changes:
1. `runActor` now defers a drain loop that pops any remaining inbox entries on actor exit and closes their `startedCh`, so any in-flight queue-notice timer goroutines can exit promptly via the `startedCh` arm.
2. `runQueueNoticeTimer` gains an `actorCtx.Done()` arm. The actor context is cancelled by both `Evict` (via `actor.cancel()`) and `Close` (via `g.mu.Lock()` + `actor.cancel()` per actor). With this arm, the timer cannot fire after eviction or shutdown even if the inbox drain has not yet observed the entry.
3. Timer goroutines are registered with `g.wg` (via `g.wg.Add(1)` at spawn and `defer g.wg.Done()` in the timer function), so `Close()` blocks until every timer has exited — eliminating the goroutine leak on graceful shutdown.

Added regression test `TestQueueNoticeDoesNotFireAfterEvict`: enqueue two queued entries behind a blocking entry, call `Evict`, sleep 10x threshold, assert `OnQueueNotice` fired zero times. Test passes deterministically.

### WR-01: `int(info.PointsCount)` truncates `uint64` on 32-bit platforms

**Files modified:** `internal/search/qdrant.go`
**Commit:** `cffa2197`
**Applied fix:** Added `saturateUint64ToInt` helper that clamps to `math.MaxInt` and uses it at the warm-cache return site in `rebuildQdrantWikiDocumentsWithClient`. The two log-only call sites in `registry_search_vector.go` and `compact_qdrant.go` were left unchanged because slog formats uint64 safely without truncation. Imported `math`.

### WR-02: `getEnvDuration` rejects negative values silently

**Files modified:** `internal/config/env.go`, `internal/config/env_test.go` (new)
**Commit:** `02c96303`
**Applied fix:** Split the err-vs-negative branches in `getEnvDuration` and emit `slog.Default().Warn` from each, recording the offending key/value/default. Created `env_test.go` with five test cases: valid value, empty/unset env, garbage string, negative duration, and explicit zero (the last verifies that `0s` is accepted as a non-negative value, preserving the "explicitly disabled" semantics for operators).

### WR-03: Warm-cache short-circuit ignores schema/vector-size drift

**Files modified:** `internal/search/qdrant.go`, `internal/search/compact_qdrant.go`, `internal/tools/registry_search_vector.go`
**Commit:** `f8f1e12a`
**Applied fix:** Per the user-supplied guidance, this is documented as accepted threat T-01-24 in `01-06-PLAN.md`. The fix is doc-only: each of the three warm-cache short-circuit sites gained a comment block referencing T-01-24, explaining why vector-size drift is not validated here (CollectionInfo does not expose vector size), what happens when an operator swaps EMBEDDING_MODEL (Search returns a clear Qdrant dimension error on first query), and where the future hardening would live (extending CollectionInfo with `vector_size`). No new validation code was added.

### WR-04: `idx.mu.Lock()` held across HTTP I/O in `toolVectorIndex.Build` blocks all `Search`

**Files modified:** `internal/tools/registry_search_vector.go`
**Commit:** `91c4d01f`
**Applied fix:** Restructured `toolVectorIndex.Build` so `idx.mu` is no longer held across HTTP calls. Added a separate `buildMu sync.Mutex` for serializing concurrent Build callers (so two Builds do not race the delete/create/upsert sequence on the same Qdrant collection). Introduced a `publish()` closure that takes `idx.mu` briefly to update `docCount`/`lastError`/`lastRebuild`. All HTTP I/O (CollectionInfo probe, embed, DeleteCollection, CreateCollection, Upsert) happens with no `idx.mu` held; `Search` callers see the previous build's snapshot during a rebuild instead of blocking. Same return semantics, same `lastError` values, same log messages preserved.

### WR-05: `loadWikiDocuments` error in warm-cache path leaves `pages` zero, contradicting log message

**Files modified:** `internal/search/qdrant.go`
**Commit:** `edb0673c`
**Applied fix:** Added a `PagesIndexedUnknown = -1` sentinel constant and assign it to the report's `PagesIndexed` field when `loadWikiDocuments` errors during a warm-cache hit. Dropped the misleading `pages_on_disk=0` from the info-level log on load failure; emit a distinct log line ("pages_on_disk unavailable") instead. Existing behavior preserved when `loadWikiDocuments` succeeds. Documented the sentinel on both the constant and the struct field. The single downstream consumer (`cmd/debug_qdrant/main.go`) prints the int directly, so a `-1` will surface visibly to operators rather than silently meaning "zero pages".

### WR-06: Test `TestQueueNoticeDoesNotFireOnOverflowDrop` is racy

**Files modified:** `internal/concurrency/gate_test.go`
**Commit:** `3bb4575c`
**Status:** fixed: requires human verification (timing-dependent test — please run a few times locally to confirm)

**Applied fix:** Restructured the test to unblock the actor IMMEDIATELY after the overflow is triggered, allowing the surviving (third) entry to begin processing well before its threshold elapses. Once it starts, its `startedCh` closes and its timer goroutine exits via the `startedCh` arm (guaranteed not to fire). The only entry whose timer could plausibly fire is the dropped one. After 5x threshold of waiting, the test asserts exactly zero notices fired — a tight invariant that targets the specific behavior under test (`dropOldestAndNotify` must cancel the dropped entry's timer). Removed the unused `overflowMu`/`overflowUserID` since only the count is asserted. Verified stable across 3 consecutive runs (`go test -count=3 -run TestQueueNoticeDoesNotFireOnOverflowDrop`).

### WR-07: `CreateCollection` on every single-doc upsert in `Index`

**Files modified:** `internal/search/qdrant.go`
**Commit:** `662f60bd`
**Applied fix:** Guard `CreateCollection` behind the existing `r.indexed` flag so it runs at most once per process lifetime. After the first successful `Index` call (or any `IndexWikiPages` call), subsequent `Index` calls skip straight to `Upsert`. On process restart the first `Index` call still hits the create path so existing "auto-create on demand" behavior is preserved. When `CreateCollection` fails with an error mentioning "dim" or "vector size", a clear `slog.Warn` hints at the EMBEDDING_MODEL-swap operator runbook (cross-references T-01-24).

## Skipped Issues

_None — all 7 in-scope findings were applied cleanly._

---

_Fixed: 2026-05-10_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
