---
phase: 02-agent-cornerstone
plan: 05
subsystem: agent-runtime
tags: [go, workflow-agents, sequential, loop, iter-seq2, budget, dedup, goleak, sc1, sc2, adk-attribution]

# Dependency graph
requires:
  - phase: 02-02
    provides: internal/agent.Agent interface + InvocationContext (WithSubAgent shared-ring derivation, D-09) + Event/Actions/LLMResponse shape
  - phase: 02-03
    provides: internal/agent.Budget — ConsumeStep (D-11) + BeforeToolCall/AfterToolResult two-phase dedup (D-18) + NewBudgetFromEnv + AURA_LOOP_DEDUP_EXEMPT_TOOLS (D-19)
  - phase: 02-04
    provides: internal/agent/agenttest.InfiniteToolCallAgent (SC#2 fixture) + RecordingAgent + EmitNThenEscalate
provides:
  - "internal/agent/workflow.SequentialAgent + NewSequential — runs subs once in order, early-return on a sub Escalate (Req#4); constructor returns the agent.Agent interface (D-02)"
  - "internal/agent/workflow.LoopAgent + NewLoop — re-runs subs until maxIter / sub Escalate / Budget exhaustion; per-tool-call ConsumeStep + shared-ring dedup; emits the explicit budget-exhausted Event (SC#2, D-04)"
  - "internal/agent/workflow TestMain goleak.VerifyTestMain — the SC#1 zero-goroutine-leak gate for the whole workflow package"
  - "workflow.joinBranch + findInTree — shared Branch dot-join (D-15) + FindAgent recursion (D-01) reused by ParallelAgent (Plan 06)"
affects: [02-06 ParallelAgent (reuses joinBranch/findInTree + the goleak TestMain), 02-07 CLI dry-run + scripts/loop_budget_smoke.sh (greps the exact SC#2 Event shape), 03 LlmAgent, 09 swarm]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Factory-returns-interface (D-02): NewSequential/NewLoop return agent.Agent; structs SequentialAgent/LoopAgent stay exported for the compile-asserts. Typed-nil guard is implicit (a non-nil pointer is always boxed)"
    - "iter.Seq2 yield discipline (D-22): every mid-stream yield is `if !yield(ev,err){ return }`; the terminal budget-exhausted Event uses `_ = yield(...)` + unconditional return"
    - "Per-tool-call budget gate (D-11): the LoopAgent consumes one step per tool-call Event BEFORE yielding it, so the terminal Event REPLACES the would-be step Event (25 step + 1 terminal = 26, SC#2 smoke contract)"
    - "Two-phase dedup via WithSubAgent shared ring (D-09): caller canonicalizes args (B2 canonicaljson.Marshal) → BeforeToolCall pre-check; AfterToolResult records the Event's assistant Content as the progress veto (D-18 — changing result suppresses dedup)"
    - "Budget exhaustion is Event-only (D-04): termination_reason/limit_hit/steps_consumed in StateDelta, never the iter error slot; the error slot carries only REAL failures"
    - "Branch dot-join with .iter-<N> per loop pass (D-15): root.iter-0.worker etc."

key-files:
  created:
    - internal/agent/workflow/sequential.go
    - internal/agent/workflow/loop.go
    - internal/agent/workflow/workflow.go
    - internal/agent/workflow/workflow_test.go
    - internal/agent/workflow/sequential_test.go
    - internal/agent/workflow/loop_test.go
    - internal/agent/workflow/workflow_contract_test.go
  modified: []

key-decisions:
  - "Budget consumed PER TOOL-CALL Event (not per outer iteration): the SC#2 fixture InfiniteToolCallAgent yields forever within a single sub.Run, so per-iteration consumption would never reach the cap. Per-tool-call ConsumeStep is the only design that both bounds the infinite sub (SC#2) and re-runs finite subs N times (maxIterations)"
  - "Terminal Event REPLACES the triggering step Event: the budget/dedup gate runs BEFORE yielding the sub's tool-call Event, so on exhaustion the terminal Event is yielded INSTEAD of the 26th step Event → exactly 26 lines (25 step + 1 terminal), matching the Plan-07 smoke contract"
  - "AfterToolResult progress-veto preview = the emitting Event's LLMResponse.Content (not the canonical args): a controllable mock can then vary Content while holding tool args fixed, which is what makes TestLoopAgent_DedupVeto_When_ResultChanges meaningful. Phase 3 LlmAgent swaps in the tool's real bounded result at the same call site"
  - "SC#2 unit test sets AURA_LOOP_DEDUP_EXEMPT_TOOLS=noop (D-19): with a constant tool call AND constant result the dedup guard would otherwise fire at step 3; exempting the tool lets the HARD max_steps cap win, which is what SC#2 asserts. The Plan-07 smoke script must set the same exemption"
  - "shared workflow.go (joinBranch + findInTree) extracted so SequentialAgent, LoopAgent, and the future ParallelAgent share one Branch-label + FindAgent-recursion implementation (CLAUDE.md reusable-code; avoids three copies)"
  - "Added workflow_contract_test.go to raise coverage 74.1% → 93.8% (>85% CLAUDE.md floor): covers the Agent accessor surface, FindAgent self/child/nested/absent recursion, joinBranch empty-parent path, and canonArgs raw-bytes fallback for malformed (non-JSON) tool args — all genuine interface-contract assertions, not asilo-nido"

patterns-established:
  - "Workflow orchestrator shape: exported struct {name; subs} (+ maxIterations for Loop), NewX factory returning the interface, Name/Description/SubAgents/FindAgent via findInTree, Run as an iter.Seq2 closure with guarded yields and explicit-Event termination"
  - "Test harness: newTestIC(t, branch) builds a real Budget InvocationContext; drain(seq) collects events + first error. Reused across sequential/loop/contract tests"

requirements-completed: []  # INFRA-03 stays OPEN — it closes at 02-06 when ParallelAgent (SC#3) lands

# Metrics
duration: ~22min
completed: 2026-05-30
---

# Phase 2 Plan 05: Sequential + Loop Workflow Agents Summary

The two deterministic, non-concurrent orchestrators: `SequentialAgent` (runs subs once in order, early-returns on a sub escalate) and `LoopAgent` (the SC#2 carrier — per-tool-call budget consumption, shared-ring dedup, and an explicit Event-only budget-exhausted termination). The package wires the SC#1 `goleak.VerifyTestMain` gate and establishes the `iter.Seq2` yield discipline + Branch/Author conventions that `ParallelAgent` (Plan 06) and the CLI dry-run (Plan 07) reuse.

## What Was Built

### Task 1 — SequentialAgent + workflow scaffolding + SC#1 TestMain (commit dce9acd4)
- `sequential.go` (80 LOC): `SequentialAgent{name, subs}` exported (D-02); `NewSequential` returns `agent.Agent`. `Run` iterates subs once each under `ic.WithSubAgent(sub)` (shared budget+ring, D-09) with `Branch` dot-joined to `<branch>.<childName>` (D-15); every yield guarded (D-22); returns early when a sub emits `Actions.Escalate=true` (Req#4).
- `workflow.go` (29 LOC): shared `joinBranch` (D-15) + `findInTree` (FindAgent recursion, D-01).
- `workflow_test.go` (27 LOC): `TestMain { goleak.VerifyTestMain(m) }` (SC#1, copied verbatim from `db_test.go:26-28`) + the exported-struct compile-asserts.
- `sequential_test.go`: `TestSequentialAgent_RunsAllSubsInOrder` (A→B→C order + dot-joined branch labels) and `TestSequentialAgent_PropagatesEscalate` (B escalates → C never invoked).

### Task 2 — LoopAgent + budget/dedup termination + SC#2 Event (commit 37baac0b)
- `loop.go` (218 LOC): `LoopAgent{name, maxIterations, subs}` exported; `NewLoop(name, maxIter uint, subs...) agent.Agent`. `Run` drives the iteration loop; for each sub-event it gates every tool call through `guardToolCall` BEFORE yielding the event:
  - canonicalizes args itself (`canonicaljson.Marshal`, B2) → `ic.Budget.BeforeToolCall` dedup pre-check (terminate `limit_hit="dedup"` on a hit);
  - `ic.Budget.ConsumeStep()` (D-11) — on a hard `max_steps`/`wallclock` stop, emits the explicit budget-exhausted Event;
  - `ic.Budget.AfterToolResult(name, args, preview)` records the Event content as the progress veto (D-18).
  - Terminal Event shape (SC#2, D-04): `Author=<loop name>`, `Escalate=true`, `StateDelta{termination_reason:"budget_exhausted", limit_hit:<reason>, steps_consumed:N}`. Soft-cap is never emitted as exhaustion (D-12). `maxIter==0` means "until escalate or budget". Branch gains `.iter-<N>` per pass (D-15).
- `loop_test.go`: `StopsAtMaxIterations` (3 iters), `EscalatePropagation` (escalate on the 2nd run → stop after iter 1), `TerminatesAtMaxSteps_WithExplicitEvent` (SC#2 — 26 events, final StateDelta `max_steps`/`25`), `DedupWindow_TerminatesOn3SameToolCalls` (`limit_hit="dedup"`), `DedupVeto_When_ResultChanges` (changing content → no dedup → terminate by `max_steps`), and the rapid property `Property_EscalateYieldedBeforeReturn` (D-21).

### Coverage hardening (commit d7cf5630)
- `workflow_contract_test.go`: Agent accessor surface, `FindAgent` self/child/nested/absent recursion, `joinBranch` empty-parent path, and `canonArgs` raw-bytes fallback for malformed tool args. Raised package coverage 74.1% → 93.8%.

## Verification Outputs

- `go test -race -count=1 ./internal/agent/...` → **exit 0** (SC#1 goleak gate green across agent + agenttest + workflow; zero leaks, zero races).
- `go test -race -count=1 ./internal/agent/workflow/ -run 'TestSequential|TestMain'` → `ok` (Task 1).
- `go test -race -count=1 ./internal/agent/workflow/ -run 'TestLoop'` → `ok` (Task 2, incl. SC#2 + dedup + rapid property).
- `go vet ./...` → clean. `go build ./...` → clean (full module compiles with Budget + workflow).
- `golangci-lint run ./internal/agent/workflow/` → **0 issues**.
- `bash scripts/check-file-size.sh` → all Go files within the 600-LOC cap (loop.go 218, sequential.go 80).
- W8: `grep -rl 'testing/synctest' internal/agent/workflow/ | wc -l` → **0** (fake clock only; no synctest import or comment).
- Acceptance greps: `canonicaljson.Marshal`, `BeforeToolCall`, `AfterToolResult`, `budget_exhausted`, `func NewLoop(name string, maxIter uint, subs ...agent.Agent) agent.Agent` all present in loop.go.
- Coverage: `go test -race -cover ./internal/agent/workflow/` → **93.8%** (>85% floor).

## Deviations from Plan

### Auto-added (Rule 2 — coverage floor is a correctness requirement)
**1. [Rule 2 - Missing critical coverage] workflow_contract_test.go**
- **Found during:** post-Task-2 coverage check (74.1% < 85% CLAUDE.md hard floor).
- **Issue:** the trivial Agent accessors (Name/Description/SubAgents/FindAgent), `findInTree` recursion, `joinBranch` empty-parent path, and `canonArgs` malformed-args fallback were unexercised.
- **Fix:** added `workflow_contract_test.go` with genuine interface-contract assertions (FindAgent recursion is real behavior, not a getter rubber-stamp).
- **Commit:** d7cf5630.

### Design choices (within plan scope, documented in key-decisions)
- Budget consumed per tool-call Event (the only design that satisfies SC#2's infinite sub AND maxIterations); terminal Event replaces the triggering step Event for the exact 26-line smoke contract; SC#2 test exempts the `noop` tool from dedup (D-19) so `max_steps` wins. None of these contradict the plan — they are the concrete realization of "per-iteration consume + Event-only termination + dedup via shared ring" against the actual fixtures.

No architectural deviations (no Rule 4). No authentication gates. No stubs. No new threat surface beyond the plan's threat_model (T-02-02/T-02-11/T-02-12 all mitigated as specified: shared-ring dedup via WithSubAgent, guarded yields, Event-only termination).

## Self-Check: PASSED
- Files created — all 7 present on disk (verified below).
- Commits dce9acd4, 37baac0b, d7cf5630 — all in `git log`.
