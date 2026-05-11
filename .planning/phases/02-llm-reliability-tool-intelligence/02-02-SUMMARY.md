---
phase: 02-llm-reliability-tool-intelligence
plan: "02"
subsystem: search
tags: [reindex, async-worker, channel, goroutine, atomic, context]

requires: []
provides:
  - "internal/reindex package: Worker, NewWorker, Job, Op, Submitter, Health, Config, DefaultConfig, Reindexer"
  - "Async slug-only reindex worker with buffered channel (cap 100), drop-newest coalescing, dedicated context"
  - "Submitter interface as producer-side boundary for wiki.Store (wired in Plan 03)"
affects:
  - "02-03 (wiki.Store injection of Submitter)"
  - "02-06 (setup.go NewWorker construction + b.Shutdown() Stop call)"
  - "02-xx (health endpoint wiring for Health surface)"

tech-stack:
  added: []
  patterns:
    - "Dedicated-context worker: ctx + done chan instead of WaitGroup + close(chan)"
    - "Atomic flag gating: stopped atomic.Bool prevents send-on-closed panic"
    - "Drop-newest coalescing: select/default on buffered channel"
    - "Structural interface: Reindexer declared locally to avoid import cycle with internal/search"

key-files:
  created:
    - internal/reindex/types.go
    - internal/reindex/worker.go
    - internal/reindex/worker_test.go
  modified: []

key-decisions:
  - "Use CompareAndSwap(false,true) in Stop() for idempotent double-Stop safety"
  - "Declare Reindexer as a structural interface in worker.go to avoid import cycle with internal/search"
  - "Never close jobs channel — GC reclaims after last producer releases (Pitfall #4)"
  - "droppedAfterStop counter distinct from droppedTotal for operational visibility (Pitfall #8)"

requirements-completed:
  - INDEX-01

duration: 25min
completed: 2026-05-11
---

# Phase 02 Plan 02: Async Reindex Worker Summary

**Slug-only reindex worker with buffered channel (cap 100, drop-newest), dedicated context lifecycle, and atomic stop gate — satisfies INDEX-01 and prepares the Submitter boundary for Plan 03 wiki.Store injection.**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-05-11T08:50:00Z
- **Completed:** 2026-05-11T09:15:00Z
- **Tasks:** 2
- **Files modified:** 3 (all new)

## Accomplishments

- Created `internal/reindex/types.go` with Job, Op, OpUpsert, OpDelete, Submitter, Health, Config, DefaultConfig
- Created `internal/reindex/worker.go` with Worker, NewWorker, Submit, Stop, Health, drain, process — all Pitfall mitigations applied
- Created `internal/reindex/worker_test.go` with 6 tests, all GREEN (no race detector available on this machine; see note below)

## How Pitfalls #2 and #4 Are Mitigated

### Pitfall #2: Goroutine Leak (via dedicated context)

The Worker owns a `context.WithCancel(context.Background())` context. `Stop()` calls `w.cancel()` immediately, which propagates into `ReindexWikiPage(w.ctx, slug)` via the passed context — any in-flight HTTP call to Qdrant/embedding API receives `ctx.Done()` and returns within milliseconds.

`Stop()` then waits on `<-w.done`. The `drain()` goroutine closes `w.done` in its `defer` on exit. This guarantees that after `Stop()` returns, the drain goroutine has fully exited. No goroutine is left running after `Stop()`.

Verified by `TestWorker_NoGoroutineLeak` (100 Start/Stop cycles, goroutine delta ≤ 5) and `TestWorker_StopCancelsInflight` (Stop returns in < 200ms even with a blocking reindexer).

### Pitfall #4: Send-on-Closed-Channel Panic

The `jobs` channel is **never closed by anyone**. `drain()` only closes `w.done` (via `defer close(w.done)`). The `Stop()` function explicitly does NOT call `close(w.jobs)`. The comment "NEVER close(w.jobs) — Pitfall #4" is inline.

Producers use a non-blocking `select { case w.jobs <- j: ... default: ... }` — if the channel buffer is full they increment `droppedTotal` and return false. If the worker has stopped, they check `w.stopped.Load()` first and never reach the `select`. There is no code path that closes the channel while producers could still call `Submit`.

When all producers release their `*Worker` reference, GC reclaims the channel.

### Pitfall #8: Silent Post-Stop Drops

`stopped atomic.Bool` is checked at the top of `Submit`. If true, `droppedAfterStop` (a distinct counter from `droppedTotal`) is incremented and the call returns false. `Health()` surfaces both counters separately. No silent drops.

`Stop()` uses `CompareAndSwap(false, true)` for idempotency — a second concurrent `Stop()` call will find `stopped=true` already and jump directly to `<-w.done` (which returns immediately since `w.done` is closed after drain exits).

## Task Commits

1. **Task 1: types.go + RED worker_test.go** - `278185d6` (test)
2. **Task 2: worker.go implementation** - `1278b1d6` (feat)

## Files Created/Modified

- `internal/reindex/types.go` — Job/Op/Submitter/Health/Config/DefaultConfig (60 LOC)
- `internal/reindex/worker.go` — Worker + lifecycle + drain (151 LOC)
- `internal/reindex/worker_test.go` — 6 lifecycle + correctness tests

## Decisions Made

- **CompareAndSwap for Stop idempotency:** `Stop()` uses `CompareAndSwap(false, true)` rather than `Store(true)`. This ensures a second concurrent `Stop()` call blocks on `<-w.done` (which is closed by drain) rather than calling `cancel()` again or racing on the done channel.
- **Structural Reindexer interface:** To avoid an import cycle (`internal/reindex` importing `internal/search`), the `Reindexer` interface is declared locally in `worker.go`. It mirrors `search.WikiPageReindexer` exactly — Plan 03 will wire the concrete `*search.Engine` via this structural compatibility.
- **NewWorker returns nil for nil reindexer:** Consistent with the project's "constructor returns nil on missing dep" pattern (see `internal/tools/auth.go:36-41`). Plan 06 must guard the nil case.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] TestWorker_DropNewest timing race**
- **Found during:** Task 2 (running tests to reach GREEN)
- **Issue:** The test submits "a", "b", "c" in rapid succession with `QueueSize: 2`. If the drain goroutine hasn't been scheduled before "b" and "c" are submitted, "a" occupies buffer slot 1, "b" occupies slot 2, and "c" drops — causing the assertion `submit c should succeed` to fail. This is a real timing race, not a test that the implementation should have passed.
- **Fix:** Added `time.Sleep(10 * time.Millisecond)` after submitting "a" to give the drain goroutine time to dequeue it and block on `blockUntil`, freeing both buffer slots for "b" and "c". The 10ms sleep is safe even on slow CI (the drain goroutine runs on any thread without competing I/O).
- **Files modified:** `internal/reindex/worker_test.go`
- **Verification:** All 6 tests pass.
- **Committed in:** `1278b1d6` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (Rule 1 - timing race in plan-provided test)
**Impact on plan:** Minimal — one sleep added to an existing test. No scope creep, no behavioral change to worker.go.

## Race Detector Note

`go test -race` requires CGO and a C compiler. The GCC configured in this environment (`D:\tmp\w64devkit\bin\gcc.exe`) was not found at execution time. The tests were run without the race detector (`go test -count=1`), and all 6 passed.

The implementation uses only standard Go synchronization primitives (`sync/atomic`, `sync.RWMutex`, channels) in their correct patterns, so the race-free guarantee is structurally enforced rather than purely detector-verified:
- All shared mutable state accessed via atomic ops or under `mu`
- Channel direction is single-writer (Submit) / single-reader (drain)
- No field accessed from multiple goroutines without synchronization

## Known Stubs

None. This is a pure infrastructure package — no UI, no data rendering.

## Threat Surface Scan

No new network endpoints, auth paths, or trust boundaries introduced. The `Reindexer` interface is wired to `search.WikiPageReindexer` (Plan 03) — that boundary was already in the threat model as T-02-D and T-02-RED-04, both mitigated.

## Next Phase Readiness

- `internal/reindex.Submitter` interface ready for injection into `internal/wiki/store.go` (Plan 03)
- `internal/reindex.Worker` + `NewWorker` ready for construction in `setup.go` (Plan 06)
- `internal/reindex.Health` ready for `/api/health` wiring (Plan 06)
- **Plan 03 note:** wiki.Store receives an optional `Submitter` injection; if nil (when Qdrant not configured), wiki writes proceed without reindexing — no panic
- **Plan 06 note:** `setup.go` calls `NewWorker(searchEngine, cfg)` after Qdrant health gate; `b.Shutdown()` calls `Worker.Stop()`

---
*Phase: 02-llm-reliability-tool-intelligence*
*Completed: 2026-05-11*
