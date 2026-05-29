---
phase: 02-agent-cornerstone
plan: 04
subsystem: agent-runtime
tags: [go, test-fixtures, mocks, iter-seq2, yield-discipline, shared-atomic, budget, agent-interface, reusable-code]

# Dependency graph
requires:
  - phase: 02-02
    provides: internal/agent.Agent interface + InvocationContext + Event/Actions/LLMResponse shape (the mocks implement and emit these)
  - phase: 02-03
    provides: internal/agent.Budget (real) — NewBudgetFromEnv + SetMaxSteps + ConsumeStep + Remaining (the CountingAgent exercises the shared *atomic.Int32)
provides:
  - "internal/agent/agenttest.InfiniteToolCallAgent — SC#2 fixture: emits the SAME llm.ToolCall forever, only the LoopAgent Budget stops it"
  - "internal/agent/agenttest.EmitNThenEscalate — N normal events then one Actions.Escalate=true event (escalate-propagation fixture)"
  - "internal/agent/agenttest.RecordingAgent — records seen Branch + emitted events for order/label assertions"
  - "internal/agent/agenttest.CountingAgent — SC#3 fixture: consumes ic.Budget (shared counter) once per step, never NewBudgetFromEnv"
affects: [02-05 Sequential/Loop tests, 02-06 Parallel SC#3 depth test, 02-07 CLI dry-run, 03 LlmAgent tests, 09 swarm tests]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Shared test-helper package internal/agent/agenttest (D-07): one source of truth for Agent mocks, zero inline duplication (CLAUDE.md reusable-code)"
    - "One-direction import (RESEARCH OQ#2): agenttest imports internal/agent; agent never imports agenttest outside _test.go — no cycle"
    - "iter.Seq2 yield discipline (D-22): every yield is `if !yield(...) { return }`; the yield-after-false guard is the ONLY exit for the never-terminating InfiniteToolCallAgent"
    - "Pitfall 3 / T-02-09 guard: no mock calls NewBudgetFromEnv — a fresh budget would silently break the shared-counter guarantee (depth³ trap). CountingAgent consumes ic.Budget only"
    - "Hermetic test budget via NewBudgetFromEnv + SetMaxSteps(n) — no AURA_LOOP_* env dependence; two-var range form throughout (D-22 footgun 4)"

key-files:
  created:
    - internal/agent/agenttest/mocks.go
    - internal/agent/agenttest/mocks_test.go
  modified: []

key-decisions:
  - "Named the counting mock CountingAgent (plan said 'e.g. CountingAgent') with an exported Calls int the SC#3 test reads to assert total ConsumeStep successes ≤ max_steps"
  - "Added per-method doc comments to mocks.go to satisfy golangci-lint revive 'exported' rule (the .golangci.yml excludes internal/agent/tools but NOT agenttest, so text_response.go's comment-free style does not carry over); CLAUDE.md no-comment rule yields to the Gate-2 lint gate here — the comments are genuine API contracts"
  - "InfiniteToolCallAgent/EmitNThenEscalate/CountingAgent emit terminal events with `yield(ev, nil)` followed by an unconditional `return` (terminal-event pattern from RESEARCH Pattern 3) — only the mid-stream yields are guarded"
  - "RecordingAgent copies each canned Event template (`ev := *src`) before stamping Author/Branch so repeated Runs don't mutate the caller's templates"

patterns-established:
  - "Mock package layout: tiny struct + canned-value methods (text_response.go idiom) + compile-time `var _ agent.Agent = (*X)(nil)` assertions for all four mocks"
  - "Drain-test harness: table-driven TestMocks_RunYieldDiscipline draining each mock under SetMaxSteps(3), plus a dedicated break-after-one no-panic test for the D-22 footgun-2 runtime guard"

requirements-completed: []  # INFRA-03 stays OPEN — it closes at 02-06 when workflow agents land

# Metrics
duration: ~12min
completed: 2026-05-30
---

# Phase 2 Plan 04: Reusable Agent Mocks (agenttest, D-07) Summary

Shared `internal/agent/agenttest` package with four reusable `agent.Agent` mocks (`InfiniteToolCallAgent`, `EmitNThenEscalate`, `RecordingAgent`, `CountingAgent`) that ARE the SC#2 / SC#3 test fixtures for Plan 05/06 + the CLI dry-run + Phase 3/9 — one-direction import, no fresh-budget anti-pattern, and a runtime drain test exercising each mock's iter.Seq2 yield discipline (W5) before downstream plans depend on it.

## What Was Built

### Task 1 — `internal/agent/agenttest/mocks.go` (233 LOC)
Four mocks, each implementing the real 02-02 `agent.Agent` interface (compile-asserted via `var _ agent.Agent = (*X)(nil)`):

- **`InfiniteToolCallAgent`** (SC#2): `Run` yields an Event carrying the SAME `llm.ToolCall` (fixed name+args) on every iteration, forever. No natural termination — the LoopAgent's Budget is the only stop. The `if !yield(...) { return }` guard is the sole exit path, so a consumer `break` does not trip a "continued iteration" panic.
- **`EmitNThenEscalate`**: emits `N` normal Events then one Event with `Actions.Escalate=true`, then stops (D-04 escalate-propagation fixture).
- **`RecordingAgent`**: records the Branch of each `InvocationContext` it sees and every Event it emits, for order/label assertions; copies canned templates before stamping so repeated Runs are non-mutating.
- **`CountingAgent`** (SC#3): calls `ic.Budget.ConsumeStep()` once per step, increments an exported `Calls` counter in lockstep, and stops the instant the shared budget refuses. Consumes ONLY the injected `ic.Budget` — never `NewBudgetFromEnv` (Pitfall 3 / T-02-09 guard), so a depth-N tree consumes total ≤ max_steps, not max_steps^depth.

### Task 2 — `internal/agent/agenttest/mocks_test.go` (182 LOC)
- **`TestMocks_RunYieldDiscipline`**: table-driven drain of each mock under `SetMaxSteps(3)` — no panic, correct termination per mock.
- **`TestMocks_InfiniteToolCall_BreakAfterOne_NoPanic`**: breaks the range after the first event, asserts no panic (D-22 footgun 2 — the yield-after-false guard exercised at runtime; build/vet cannot catch this).
- **`TestMocks_CountingAgent_SharedCounter`**: drains `CountingAgent` with a generous cap and asserts `Calls == seeded` and `Budget.Remaining() == 0` — proving it consumes the injected shared `*atomic.Int32`, not a fresh 25-step default.

## Verification Results (exact)

| Check | Command | Result |
|-------|---------|--------|
| build | `go build ./internal/agent/agenttest/` | `BUILD OK` |
| build (all) | `go build ./...` | `BUILD ALL OK` |
| vet | `go vet ./internal/agent/agenttest/` | `VET OK` |
| test | `go test ./internal/agent/agenttest/ -run TestMocks` | `ok ... 0.155s` (3 tests, all PASS) |
| race | `go test -race ./internal/agent/agenttest/` | `ok ... 1.318s` |
| lint | `~/go/bin/golangci-lint run ./internal/agent/agenttest/...` | `0 issues.` |
| file-size | `bash scripts/check-file-size.sh` | `all Go files within the 600-LOC cap` (mocks.go 233, mocks_test.go 182) |
| Pitfall-3 guard | NewBudgetFromEnv references in mocks.go | only 2 doc-comment mentions documenting the invariant; ZERO call-sites |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] golangci-lint revive `exported` rule required per-method doc comments**
- **Found during:** Task 2 (lint gate before commit)
- **Issue:** `.golangci.yml` excludes `internal/agent/tools` (where `text_response.go`'s comment-free interface-impl style lives) but does NOT exclude `agenttest`. revive flagged 20 undocumented exported methods.
- **Fix:** added concise per-method contract doc comments to all four mocks' `Name`/`Description`/`Run`/`SubAgents`/`FindAgent`. This matches the repo's non-excluded house style (agent.go/budget.go/event.go document every exported method). CLAUDE.md's "no comment unless WHY non-obvious" yields to the Gate-2 lint gate — these are genuine API contracts.
- **Files modified:** internal/agent/agenttest/mocks.go
- **Commit:** 572b7b3a (folded into the Task-2 commit since the test run surfaced it)

## Acceptance Criteria

- [x] Four mocks implement `agent.Agent` (compile-asserted) — D-07
- [x] One-direction import; no `NewBudgetFromEnv` call inside any mock (Pitfall 3 guard)
- [x] `InfiniteToolCallAgent` yields the same tool call forever (SC#2 fixture)
- [x] `CountingAgent` threads the shared `*atomic.Int32` (SC#3 fixture)
- [x] Each mock's iter.Seq2 yield discipline exercised at runtime (W5: drain-3 + break-after-one, both panic-free)
- [x] Files ≤600 LOC; no duplicated mock logic
- [x] go vet / go build / go test / go test -race / golangci-lint all green

## Self-Check: PASSED

- FOUND: internal/agent/agenttest/mocks.go
- FOUND: internal/agent/agenttest/mocks_test.go
- FOUND: .planning/phases/02-agent-cornerstone/02-04-SUMMARY.md
- FOUND commit: afd019c8 (Task 1)
- FOUND commit: 572b7b3a (Task 2)
