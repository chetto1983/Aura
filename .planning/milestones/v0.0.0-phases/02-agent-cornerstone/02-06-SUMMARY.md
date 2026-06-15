---
phase: 02-agent-cornerstone
plan: 06
subsystem: agent-runtime
tags: [go, workflow-agents, parallel, errgroup, iter-seq2, backpressure, escalate-cancel, goleak, sc1, sc3, adk-attribution]

# Dependency graph
requires:
  - phase: 02-02
    provides: internal/agent.Agent interface + InvocationContext (WithContext/WithSubAgent copy-derivation D-24, D-09) + Event/Actions shape
  - phase: 02-03
    provides: internal/agent.Budget — Child(fanout) forks a distinct dedup ring sharing the *atomic.Int32 (D-09/D-10) + ConsumeStep TOCTOU-safe (D-11) + SetMaxSteps
  - phase: 02-04
    provides: internal/agent/agenttest.CountingAgent (SC#3 shared-counter fixture) + EmitNThenEscalate (escalate-cancel fixture)
  - phase: 02-05
    provides: internal/agent/workflow.joinBranch + findInTree (reused, not duplicated) + the SC#1 goleak.VerifyTestMain package gate
provides:
  - "internal/agent/workflow.ParallelAgent + NewParallel — runs subs concurrently via errgroup, fans-in to a channel, yields SERIALLY from the iterator frame; constructor returns the agent.Agent interface (D-02)"
  - "escalate-cancel choreography: any child Escalate fires a captured context.CancelFunc (D-03), cancelled siblings drain (nil,nil) (D-05) — the swarm primitive Phase 9 reuses"
  - "SC#3 proof: a depth-3 fan-3 (9-leaf) tree sharing one *atomic.Int32 consumes ≤ max_steps total, NOT max_steps³"
affects: [02-07 CLI dry-run (may wire ParallelAgent), 03 LlmAgent, 09 swarm (reuses ParallelAgent as the fan-out primitive)]

# Tech tracking
tech-stack:
  added:
    - "golang.org/x/sync v0.18.0 — promoted indirect→direct (errgroup.WithContext); no go get, already in go.sum"
  patterns:
    - "errgroup fan-out + captured escalate-cancel (D-03): eg, egCtx := errgroup.WithContext(ic.Ctx); egCtx, cancel := context.WithCancel(egCtx); defer cancel(). The captured cancel fires on a child Escalate — NEVER a fake sentinel error, which would pollute errgroup.Wait() and the iter error slot (D-04)"
    - "Serial fan-in (D-22 footgun 3): children send to `results`; the iterator frame ranges `results` and yields each pair SERIALLY — never yields from a spawned goroutine"
    - "Synchronous ack backpressure: each result carries a per-Event ack chan the iterator closes after yielding; the child blocks on <-ack before producing its next Event → bounded in-flight work, no unbounded buffer under a slow consumer"
    - "Leak-safe cancel/drain (D-23): defer cancel() + defer close(done); every channel op in runSub is a multi-arm select with case <-done / case <-ctx.Done() returning nil (D-05, not ctx.Err()); spawn loop guarded by if egCtx.Err() != nil (Go #61611)"
    - "Two-step child IC (W6): ic.WithContext(egCtx).WithSubAgent(sub) then childIC.Budget = ic.Budget.Child(len(subs)) — Budget.Child forks a DISTINCT dedup ring while sharing the *atomic.Int32 (D-09/D-10); Branch dot-joined root.<self>.<child> (D-15)"

key-files:
  created:
    - internal/agent/workflow/parallel.go
    - internal/agent/workflow/parallel_test.go
  modified:
    - go.mod  # golang.org/x/sync promoted indirect→direct

key-decisions:
  - "Forward eg.Wait()'s error to the consumer (adk parity, non-lossy D-04): a real child failure makes runSub return that err to errgroup; the closer goroutine forwards it ONCE through `results` (2-arm select against done so it cannot leak) so the consumer actually sees the failure in the error slot. The RESEARCH skeleton's `_ = eg.Wait()` would have silently swallowed real child errors — corrected so D-04's 'error slot carries real failures' is observable (proven by _RealChildError_SurfacesThroughErrorSlot)"
  - "Two divergences from adk applied as deliberate edits (not blind copy): D-03 — escalate uses a captured context.CancelFunc, adk has no escalate-cancel; D-05 — runSub returns nil on intentional cancel where adk yields ctx.Err() (agent.go:142). Both documented inline + in the package doc + THIRD_PARTY_NOTICES already lists them"
  - "Child IC built via the real two-step API (W6), NOT a non-existent ic.Child(ctx, sub): the RESEARCH skeleton showed ic.Child(egCtx, sub) which is not a method on InvocationContext. Used ic.WithContext(egCtx).WithSubAgent(sub) + childIC.Budget = ic.Budget.Child(len(a.subs)) — the documented Budget.Child fork-ring/share-atomic path (D-09), distinct from the LoopAgent shared-ring WithSubAgent-only path"
  - "Added 3 tests beyond the 6 named in the plan (TOCTOU race-stress, real-error-surfaces, tree-introspection) to (a) hard-gate the D-11 atomic invariant under a concurrent fan, (b) make D-04's real-error path observable, (c) cover the FindAgent/SubAgents/Description tree contract — lifting parallel.go to 100% on every function except runSub's timing-dependent cancel-send select arms (72.7%, exercised under -count=10 stress but not deterministically captured per single run). Genuine contract assertions, not asilo-nido"
  - "parallel_test.go is 445 LOC (plan estimated ≤250) — well under the 600 cap. The extra LOC are the 3 added correctness tests + their fixtures (slowAgent, boundedCounter, sharedCounterTool, ackProbeAgent, erroringAgent), each driving a distinct concurrency arm. No file split needed"

patterns-established:
  - "Concurrent orchestrator shape: exported struct {name; subs}, NewParallel factory returning the interface, Run as an iter.Seq2 closure that fans-out via errgroup and yields serially from the frame; reuses joinBranch/findInTree from workflow.go (Plan 05) rather than duplicating"
  - "Concurrency test harness: budgetIC(t, branch, maxSteps) seeds the shared atomic via SetMaxSteps; per-test defer goleak.VerifyNone(t) on top of the package TestMain; slow/bounded/probe mock agents drive the cancel/backpressure/break-early arms; run with -race -count=10"

requirements-completed: [INFRA-03]  # ParallelAgent (SC#3) is the last workflow agent — this plan closes INFRA-03

# Metrics
duration: ~28min
completed: 2026-05-30
---

# Phase 2 Plan 06: ParallelAgent (Concurrent Workflow Orchestrator) Summary

`ParallelAgent` is the concurrency crown jewel and the swarm primitive Phase 9 reuses: it runs sub-agents concurrently under one `errgroup`, fans their Events into a single channel, and yields each `(Event, error)` SERIALLY from the iterator frame. It steals adk-go's `parallelagent/agent.go` channel choreography near-verbatim with exactly two documented divergences — **D-03** (escalate fires a captured `context.CancelFunc`, not a sentinel error) and **D-05** (cancelled siblings drain `(nil,nil)`, not `ctx.Err()`). It carries **SC#3** (a depth-3 fan-3 tree shares one `*atomic.Int32`, total ≤ 25) and the **SC#1/D-23** break-early goleak test (a single missing select arm would leak a goroutine on every early break). **This plan closes INFRA-03** — it is the last workflow agent.

## What Was Built

### Task 1 — ParallelAgent + NewParallel (commit 8d2e78ab)
- `parallel.go` (173 LOC): `ParallelAgent{name, subs}` exported (D-02); `NewParallel(name, subs...) agent.Agent` returns the interface (typed-nil guard implicit). `Run` follows the RESEARCH Pattern-4 skeleton:
  - `eg, egCtx := errgroup.WithContext(ic.Ctx)` → `egCtx, cancel := context.WithCancel(egCtx)` → `defer cancel()` (D-03 own cancel for escalate, never a fake error).
  - `results` + `done` channels; `defer close(done)`.
  - per child: `childIC := ic.WithContext(egCtx).WithSubAgent(sub)` (W6 two-step API), `childIC.Branch = joinBranch(joinBranch(ic.Branch, a.name), sub.Name())` (D-15, reuses Plan-05 `joinBranch`), `childIC.Budget = ic.Budget.Child(len(a.subs))` (D-09 fork-ring/share-atomic). Spawn guarded by `if egCtx.Err() != nil { return nil }` (Go #61611, D-23).
  - a closer goroutine `if err := eg.Wait(); err != nil { forward once to results }; close(results)` — real child failures reach the consumer through the error slot (D-04), 2-arm select against `done` so it cannot leak.
  - the frame ranges `results`, yields serially (footgun 3 avoided), closes each ack (backpressure), and calls `cancel()` on an Escalate Event (D-03).
  - `runSub` (per child): ranges `sub.Run(ic)`; real `err` → `return err`; every channel op is a multi-arm select with `case <-done: return nil` and `case <-ctx.Done(): return nil` — **nil, not ctx.Err()** (D-05).
  - adk Apache-2.0 attribution + both divergence rationales in the package doc and inline.
- `SubAgents`/`FindAgent` reuse `findInTree` from `workflow.go` (Plan 05).

### Task 2 — parallel_test.go (commit 874ed030)
The six named tests plus three correctness additions, each carrying `defer goleak.VerifyNone(t)`:
- `TestParallelAgent_DepthChainBudgetShared_NotFresh` (**SC#3**): Root = ParallelAgent of 3 ParallelAgents, each of 3 `CountingAgent`s (9 leaves), shared budget seeded at 25 via `SetMaxSteps` → total `ConsumeStep` successes ≤ 25 (NOT 15625); plus an accounting assertion `consumed + remaining == 25`.
- `TestParallelAgent_SubAgentExposedAsTool_SharesCounter` (Pitfall 3): a `sharedCounterTool` wraps a `CountingAgent` and threads the same `ic.Budget` → total ≤ 25.
- `TestParallelAgent_EscalateFromAnyCancelsSiblings` (D-03/D-05): child[1] `EmitNThenEscalate{N:0}`, child[0]/child[2] slow → escalate observed, siblings cut short (≤ 50 events), zero leak.
- `TestParallelAgent_NoGoroutineLeak_When_ConsumerBreaksEarly` (**SC#1/D-23**): 3 slow children, consumer breaks after the first event → all children drain via done/ctx.Done(), goleak clean.
- `TestParallelAgent_ChildrenShareParentBudget`: 5-budget, 3 children (max 3 each) → total exactly 5, remaining 0.
- `TestParallelAgent_BackpressureAckChannel`: a slow consumer + an `ackProbeAgent` → the producer never gets more than 1 event ahead (synchronous ack).
- **Added** `TestParallelAgent_TOCTOUSafe_UnderConcurrentFan`: 16 children vs a counter of 1 → exactly one wins (D-11 atomic invariant, race-stress).
- **Added** `TestParallelAgent_RealChildError_SurfacesThroughErrorSlot`: a real child error reaches the consumer's error slot (D-04 observable).
- **Added** `TestParallelAgent_TreeIntrospection`: `Description`/`SubAgents`/`FindAgent` nested recursion + absent-name nil (D-01).

## Verification Outputs

- `go test -race -count=1 ./internal/agent/...` → **exit 0** (agent + agenttest + workflow; zero leaks, zero races — SC#1 across the whole agent tree).
- `go test -race -count=1 ./internal/agent/workflow/ -run 'TestParallel'` → `ok`.
- `go test -race -count=10 ./internal/agent/workflow/ -run 'TestParallel'` → `ok` (9 tests × 10 iterations, all race + goleak clean — channel/goroutine flakiness shaken out).
- `go vet ./internal/agent/workflow/` → clean. `go build ./...` → clean (errgroup compiles; `go mod tidy` promoted golang.org/x/sync indirect→direct, no go.sum change).
- `golangci-lint run ./internal/agent/workflow/` → **0 issues**. `gofmt -l` → clean on both files.
- `bash scripts/check-file-size.sh` → all Go files within the 600-LOC cap (parallel.go 173, parallel_test.go 445).
- Acceptance greps on parallel.go: `errgroup`=6, `defer cancel()`=2, `WithContext(egCtx).WithSubAgent`=1, `ic.Budget.Child(len(a.subs))`=1, `func NewParallel(`=1, **`ic.Child(`=0** (no call to the non-existent method).
- Coverage: `go test -cover ./internal/agent/workflow/` → **90.3%** (>85% floor). parallel.go per-func: NewParallel/Name/Description/Run/SubAgents/FindAgent 100%, runSub 72.7% (remaining lines are the timing-dependent cancel-on-send select arms — exercised under -count=10 stress, not deterministically captured per single run).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical functionality] Real child errors were being swallowed**
- **Found during:** Task 1
- **Issue:** The RESEARCH Pattern-4 skeleton uses `go func() { _ = eg.Wait(); close(results) }()`, discarding `eg.Wait()`'s error. Since `runSub` returns real failures to errgroup (not to `results`), a real child error would never reach the consumer — silently violating D-04's "error slot carries real failures".
- **Fix:** Adopted adk's own pattern of forwarding the `eg.Wait()` error once through `results` via a 2-arm select against `done` (so it cannot leak). This makes D-04's real-error path observable.
- **Files modified:** internal/agent/workflow/parallel.go
- **Commit:** 8d2e78ab
- **Verified by:** `TestParallelAgent_RealChildError_SurfacesThroughErrorSlot`

**2. [Rule 3 - Blocking issue] RESEARCH skeleton used a non-existent ic.Child(ctx, sub) method**
- **Found during:** Task 1 (anticipated by the plan's W6 note)
- **Issue:** RESEARCH Pattern 4 line `childIC := ic.Child(egCtx, sub)` — `InvocationContext` has no such method.
- **Fix:** Used the real two-step API per the plan's W6 instruction: `ic.WithContext(egCtx).WithSubAgent(sub)` + `childIC.Budget = ic.Budget.Child(len(a.subs))`.
- **Files modified:** internal/agent/workflow/parallel.go
- **Commit:** 8d2e78ab

### Test additions (beyond the 6 named tests)
Three tests were added (TOCTOU race-stress, real-error-surfaces, tree-introspection) to hard-gate the D-11 atomic invariant, make D-04 observable, and cover the FindAgent/SubAgents/Description tree contract. Not deviations from intent — they strengthen the suite the plan asks for.

## License Hygiene (Apache-2.0)
`parallel.go` carries the adk-go attribution in its package doc (`Pattern derivato da google/adk-go v1.4.0 agent/workflowagents/parallelagent/agent.go (Apache 2.0)`) and documents BOTH divergences (D-03, D-05) in the package doc AND inline where the behaviour differs. THIRD_PARTY_NOTICES.md already lists `internal/agent/workflow/parallel.go` as a planned adapted file and names "captured cancel for escalation" + "clean sibling drain" as required documented divergences — no edit needed.

## Known Stubs
None — ParallelAgent is fully wired; every channel arm and divergence is exercised by a test.

## Self-Check: PASSED
- FOUND: internal/agent/workflow/parallel.go
- FOUND: internal/agent/workflow/parallel_test.go
- FOUND: .planning/phases/02-agent-cornerstone/02-06-SUMMARY.md
- FOUND commit 8d2e78ab (feat Task 1), FOUND commit 874ed030 (test Task 2)
