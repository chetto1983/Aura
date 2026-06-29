---
phase: 22-bug-fix
plan: "03"
subsystem: agent-mcp-reasoning-budget
tags: [mcp, reconnect, reasoning, budget, loop-bounds]

requires:
  - phase: 22-bug-fix
    provides: panic/observability/secret boundaries from 22-01 and 22-02
provides:
  - off-lock single-flight MCP reconnect with backoff, breaker, and bounded reconnect timeout
  - boot-time MCP call-timeout validation and safe timeout migration semantics
  - MCP schema/property/error caps plus reconnect warnings for mutating/required-arg changes
  - static-low classifier failure fallback with bounded LLM router timeout
  - single-flight classifier cold-start anchor builds without holding c.mu across embedding/store work
  - positive active-budget validation and a default maxIter=0 loop ceiling
affects: [agent-runtime, mcp-tools, reasoning-router, reasoning-classifier, budget, workflow-loop, dry-run-cli]

tech-stack:
  added: []
  patterns:
    - reconnect ownership decided under lock, slow open/list/backoff outside lock, publish under lock
    - singleflight classifier cold-start with refresh generation
    - constructor-level budget validation before root invocation starts

key-files:
  created:
    - internal/agent/workflow/loop_bounds_test.go
  modified:
    - internal/agent/mcptools/bridge_reconnect.go
    - internal/agent/mcptools/bridge.go
    - internal/agent/mcptools/timeout.go
    - internal/agent/mcptools/bridge_reconnect_branches_test.go
    - internal/agent/mcptools/bridge_edges_test.go
    - internal/agent/mcptools/bridge_trust_test.go
    - internal/agent/llm_agent_reasoning.go
    - internal/agent/llm_agent_reasoning_test.go
    - internal/agent/prompt/reasoning_classifier.go
    - internal/agent/prompt/reasoning_classifier_test.go
    - internal/agent/budget.go
    - internal/agent/budget_test.go
    - internal/agent/workflow/loop.go
    - cmd/aura/agent_test.go

key-decisions:
  - "AURA_MCP_CALL_TIMEOUT_SEC=0 now means the default 60s timeout; -1 is the only explicit infinite/no-deadline mode."
  - "MCP reconnect uses defaults from the plan: 3 failures to breaker, 30s cooldown, 500ms->30s exponential backoff, and 10s reconnect timeout."
  - "When a classifier is wired but unavailable/abstaining, adaptive reasoning uses static ReasoningTierLow instead of issuing an LLM-router round trip."
  - "The LLM router remains available for the no-classifier path, but its timeout cap is now 2s instead of the old 8s."
  - "cmd/aura/agent.go already carried Budget.WithDeadline root wiring; this wave confirmed it and added CLI-level non-positive override tests."

patterns-established:
  - "MCP tool config is resolved once at Bridge/Mount time and stored on bridged tools."
  - "Reconnect-time spec mutation is treated as observable drift: warn on Mutating flips and required-arg changes."
  - "maxIterations==0 is not unbounded: it uses a documented 1000-iteration safety ceiling, with budget/no-progress/dedup/context still stopping earlier."

requirements-completed: [HARDEN-03, HARDEN-06, HARDEN-09, HARDEN-12]

duration: 47min
completed: 2026-06-15
---

# Phase 22-03: MCP Resilience + Reasoning Fallback + Budget Bounds Summary

**MCP dependencies now degrade instead of wedging the agent, classifier outages no longer add a router latency cliff, and active budgets/loops reject or cap unsafe configurations.**

## Accomplishments

- Closed AG-005/AG-006 by moving reconnect spawn/list work outside `s.mu`, adding single-flight reconnect ownership, `context.WithoutCancel` plus a 10s reconnect timeout, exponential backoff, and a per-server breaker.
- Closed AG-024/AG-025/AG-026/AG-027/AG-029 slices by validating MCP timeout config at bridge/mount time, changing timeout semantics, capping schema bytes/properties and inline MCP error text, and preserving deterministic collision handling.
- Landed the AG-007 single-operator slice by warning when reconnect changes a tool's `Mutating` flag or required args.
- Closed AG-008 by making a wired classifier failure fall back to static `ReasoningTierLow` with no router LLM call; the no-classifier router path remains on and bounded to 2s.
- Closed AG-032 by moving classifier anchor embedding/store work out of `c.mu` and sharing concurrent cold starts with `singleflight`.
- Closed AG-035/AG-036 by rejecting `maxSteps < 1` and `wallclock < 1` in `NewBudget`.
- Closed AG-041 by confirming root dry-run invocation already uses `Budget.WithDeadline`; added CLI-level validation tests.
- Added a default 1000-iteration safety ceiling for `NewLoop(..., maxIter=0, ...)`, with an explicit `iteration_limit/max_iterations` terminal event.

## MCP Defaults and Migration

- `AURA_MCP_CALL_TIMEOUT_SEC` unset: default 60s call timeout.
- `AURA_MCP_CALL_TIMEOUT_SEC=0`: default 60s call timeout.
- `AURA_MCP_CALL_TIMEOUT_SEC=-1`: explicit infinite/no-deadline MCP call.
- `AURA_MCP_CALL_TIMEOUT_SEC<-1` or malformed: bridge/mount fails before tools are registered.
- Reconnect timeout: 10s.
- Reconnect breaker: opens after 3 failed reconnects.
- Reconnect cooldown: 30s.
- Reconnect backoff: 500ms exponential, capped at 30s.

## Task Commits

1. **Task 1: MCP reconnect, timeout, schema/error caps, and drift warnings** - `5a4f90ca`
2. **Task 2: Reasoning fallback and classifier cold-start lock narrowing** - `108bbd32`
3. **Task 3: Budget validation and default loop ceiling** - `f27e313c`

## Verification

- `go test ./internal/agent/mcptools -count=1`
- `go test ./internal/agent/prompt -run TestReasoningClassifier -count=1`
- `go test ./internal/agent -run TestLlmAgent_AdaptiveReasoning -count=1`
- `go test ./internal/agent -run 'TestNewBudget|TestBudget' -count=1`
- `go test ./internal/agent/workflow -run TestLoopAgent -count=1`
- `go test ./cmd/aura -run TestDryRun -count=1`
- `go test ./internal/agent/... -count=1`
- `go test -race ./internal/agent/mcptools ./internal/agent/prompt ./internal/agent/workflow -count=1`
- Pre-commit on all three code commits: `gofmt`, `go vet`, and Go file-size check.

## Known Unrelated Failure

- `go test ./internal/agent/... ./cmd/aura -count=1` passed the full `internal/agent/...` tree but failed in `cmd/aura` at `TestProductionContainerArtifactsMatchFatImageContract`: `compose.yaml` is missing the expected `AURA_LLM_MODEL: ${AURA_LLM_MODEL:-deepseek/deepseek-v4-flash:exacto}` line. This is outside the Wave 3 agent/MCP/reasoning/budget scope and was not changed here.

## AG Ledger Status

- **AG-005:** Fixed - reconnect no longer holds `s.mu` across spawn/list work and uses a dedicated reconnect timeout.
- **AG-006:** Fixed - MCP call timeout `0` means default and `-1` is explicit infinite.
- **AG-007 flip-warn slice:** Fixed - mutating and required-arg changes warn on reconnect; full multi-tenant capability grants remain deferred.
- **AG-008:** Fixed - classifier outage uses static low fallback; no recurring LLM-router round trip.
- **AG-022/AG-023:** Fixed/covered - bridge validates timeout config at mount and preserves deterministic collision behavior.
- **AG-024/AG-025/AG-026/AG-027/AG-029:** Fixed - schema size/property caps, error preview caps, non-replay transport behavior, and reconnect drift tests exist.
- **AG-032:** Fixed - classifier cold start is single-flight and does not hold `c.mu` during embedding/store work.
- **AG-035/AG-036:** Fixed - non-positive max steps and wallclock values fail construction.
- **AG-041:** Confirmed fixed - root dry-run context already uses `Budget.WithDeadline`.

## Deviations from Plan

- The plan listed `cmd/aura/agent.go` as a modified file for `WithDeadline` wiring, but the wiring was already present at `dryRun`; this wave added CLI validation coverage instead of touching the already-correct root context line.
- Full `go test ./cmd/aura` is not green because of an unrelated production-container artifact contract mismatch noted above.

## Next Phase Readiness

- Wave 4 can build on a bounded MCP/reasoning/budget substrate and focus on the remaining hook, provenance, tool-hardening, and workflow/tree slices.

---
*Phase: 22-bug-fix*
*Completed: 2026-06-15*
