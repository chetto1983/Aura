---
phase: 09-swarm-minimal
plan: 05
subsystem: agent-runtime
tags: [swarm, swarm_spawn, deferred-tool, context-key, import-cycle, registry, fakeclient, cli]

# Dependency graph
requires:
  - phase: 09-02
    provides: "internal/swarm.Run(ctx, RunConfig, goals) engine + Without registry helper + ChildReport contract + 3 swarm config env vars"
  - phase: 09-03
    provides: "shared cmd/aura/main.go buildBaseRegistry/buildRegistryWithMCP fail-soft boot + mcpAllowlist"
  - phase: 03 (Slice 1)
    provides: "LlmAgent.runTool dispatch + tools.WithToolCallContext ctx-key idiom + tools.NewResult spillover + Deferred:true Spec"
provides:
  - "swarm_spawn: Deferred:true {goals}-only tool (D-01/D-03) with the D-24 anti-over-spawn literal + D-13 goals cap"
  - "Cycle-free seam: swarmRunner interface in the tools package (no swarm/agent import) + agent.WithSwarmContext private-ctx-key injector + internal/swarm.RunnerAdapter concrete impl"
  - "swarm_spawn registered parent-only at boot (buildBaseRegistry) with reg.Validate() green under the Deferred tool (Pitfall 6)"
  - "aura swarm-demo: deterministic no-LLM engine proof over agenttest.FakeClient (D-16)"
affects: [09-06 live E2E (swarm cot_eval), AG-UI Phase 12 swarm progress, future SWARM-V2 nesting]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Cycle-free engine seam: interface in the consumer (tools) package + ctx-key dep injection (agent) + concrete adapter in the engine (swarm) — the sandbox_exec sandboxRunner pattern generalized to a multi-dep engine"
    - "Private-ctx-key dep handoff (agent.WithSwarmContext) mirroring tools.WithToolCallContext: layered onto the SAME tool-call ctx in runTool, read only by the one tool that needs it"

key-files:
  created:
    - internal/agent/tools/swarm_spawn.go
    - internal/agent/tools/swarm_spawn_test.go
    - internal/agent/swarm_context.go
    - internal/swarm/runner_adapter.go
    - internal/swarm/runner_adapter_test.go
    - cmd/aura/swarm_demo.go
    - cmd/aura/swarm_demo_test.go
  modified:
    - internal/agent/llm_agent.go
    - cmd/aura/main.go
    - cmd/aura/main_test.go

key-decisions:
  - "swarmRunner interface lives in the tools package (the consumer), NOT internal/swarm — so swarm_spawn.go imports neither internal/swarm nor internal/agent, breaking the tools->swarm->agent->tools cycle by type indirection (mirrors sandbox_exec)"
  - "The adapter holds config.Config as a construction-time field (set in cmd/aura) and reads budget/registry/client/llmCfg/convID off the ctx — so the agent package never imports config and the engine's full RunConfig is assembled at the seam"
  - "Registered swarm_spawn in buildBaseRegistry (the chokepoint both buildRegistry and buildRegistryWithMCP share) — so bootChat + the runner agent inherit it WITHOUT editing chat.go or runner.go (the plan listed them but no change was needed)"
  - "convID == a.sessionID (D-26): runTool passes a.sessionID as the swarm convID, keying each worker SessionID + transcript dir"

patterns-established:
  - "Pattern: a Deferred engine tool exposes a tiny interface seam in its own package + a private-ctx-key dep injector, so the composition root wires the concrete engine adapter with zero import-cycle risk"
  - "Pattern: an operator no-LLM demo subcommand drives the real engine over agenttest.FakeClient content-stop turns, asserting ordered output under the package goleak TestMain"

requirements-completed: [CAP-03]

# Metrics
duration: ~8min
completed: 2026-06-04
---

# Phase 9 Plan 05: swarm_spawn Tool + Cycle-Free Seam Summary

**The swarm engine is now reachable: a Deferred:true `swarm_spawn {goals}` tool (D-24 anti-over-spawn literal + D-13 goals cap) wired to the 09-02 engine through a cycle-free seam (interface in tools + private-ctx-key injector in agent + adapter in swarm), registered parent-only at boot with `reg.Validate()` green, and an `aura swarm-demo` no-LLM proof.**

## Performance

- **Duration:** ~8 min
- **Started:** 2026-06-04T10:18Z
- **Completed:** 2026-06-04T10:26Z
- **Tasks:** 2 (Task 1 TDD)
- **Files modified:** 10 (7 created, 3 modified)

## Accomplishments
- **D-01/D-03/D-24/D-13:** `swarm_spawn` is a `Deferred:true` tool whose schema is `{goals:[...]}` ONLY (no `tier`), whose Description carries the four D-24 anti-over-spawn phrases (asserted by a test), and whose Execute rejects an over-cap goals array with a model-readable `error: too many goals … AURA_SWARM_MAX_GOALS` string before any runner call.
- **Import-cycle resolution (T-09-14):** `swarm_spawn.go` defines a `swarmRunner interface` IN the tools package and imports NEITHER `internal/swarm` NOR `internal/agent` — verified by grep + a clean `go build ./...` (no cycle). The parent's per-invocation deps travel via `agent.WithSwarmContext` (a private unexported `swarmCtxKey` mirroring `tools.WithToolCallContext`), injected in `runTool` onto the same tool-call ctx; the concrete `internal/swarm.RunnerAdapter` reads them back and calls the engine.
- **D-08/D-10 flat-v1:** the engine derives `Without(parentRegistry, "swarm_spawn")` per worker, so a worker never inherits swarm_spawn (re-asserted by a runner_adapter test + the live adapter-drives-engine path).
- **Pitfall 6 ordering:** swarm_spawn registers into the PARENT registry in `buildBaseRegistry`; `TestBuildBaseRegistryValidatesWithSwarmSpawn` proves `reg.Validate()` stays nil AFTER the Deferred tool is present (the non-deferred built-ins satisfy the ≥1-non-deferred guard, not swarm_spawn).
- **D-16:** `aura swarm-demo` drives `internal/swarm.Run` over an `agenttest.FakeClient` (content-stop mock workers) and prints an ordered `[]ChildReport` JSON array of length == goals, all `status:ok`, with no live LLM or network — goleak-clean via the cmd/aura `TestMain`.

## Task Commits

1. **Task 1 (TDD): swarm_spawn Deferred tool + cycle-free ctx seam + adapter** — `827169a7` (feat)
2. **Task 2: register swarm_spawn parent-only at boot + aura swarm-demo** — `547bed0d` (feat)

_TDD note (Task 1): the tool + seam + adapter are a new cohesive surface; the tests and impl landed in one feat commit (a separate RED would have been a no-compile, the package being new). The test tier (D-24 literal, goals cap with the fake runner NOT called, schema-no-tier, missing-runner, adapter full path + worker-registry exclusion) is committed alongside the code._

## Files Created/Modified
- `internal/agent/tools/swarm_spawn.go` (92 LOC) — `SwarmSpawn{Runner swarmRunner; MaxGoals int}`; the `swarmRunner` interface (no swarm/agent import); the D-24 literal Description const; Deferred:true `{goals}` schema; Execute = unmarshal → cap-check → delegate.
- `internal/agent/tools/swarm_spawn_test.go` — 5 tests: D-24 literal, goals cap (runner not invoked), under-cap delegation, schema (Deferred + goals-only + no tier), nil-runner Go error.
- `internal/agent/swarm_context.go` (52 LOC) — private `swarmCtxKey struct{}` + `SwarmContextValue` + `WithSwarmContext`/`SwarmContext` pair (mirrors `tools.WithToolCallContext`); carries budget/registry/client/llmCfg/convID (NOT config — the adapter holds that).
- `internal/agent/llm_agent.go` — `runTool` now takes `budget *Budget` and layers `WithSwarmContext(toolCtx, budget, a.registry, a.client, a.cfg, a.sessionID)` onto the tool-call ctx before dispatch; the single `dispatch` call site passes `ic.Budget`.
- `internal/swarm/runner_adapter.go` (59 LOC) — `RunnerAdapter{Cfg config.Config; Depth int}`; `Run` reads `agent.SwarmContext(ctx)`, builds `RunConfig`, calls `Run(...)`, wraps the report JSON in `tools.NewResult`; a missing swarm ctx is a model-readable inline error.
- `internal/swarm/runner_adapter_test.go` — missing-context inline error, full adapter path through the ctx seam (2 ok workers), worker-registry-excludes-swarm_spawn.
- `cmd/aura/main.go` — `buildBaseRegistry` registers `&tools.SwarmSpawn{Runner: swarm.NewRunnerAdapter(*cfg), MaxGoals: cfg.MaxSwarmGoals}`; `swarm-demo` dispatcher case + usage line; `internal/swarm` import.
- `cmd/aura/main_test.go` — `TestBuildBaseRegistryValidatesWithSwarmSpawn` (Pitfall 6).
- `cmd/aura/swarm_demo.go` (108 LOC) — `runSwarmDemo`/`swarmDemo`: FakeClient fixture, engine run, indented JSON report array to stdout.
- `cmd/aura/swarm_demo_test.go` — deterministic ordered report array, length == goals, all ok.

## Decisions Made
- **Interface placement = the consumer (tools) package.** Putting `swarmRunner` in `internal/swarm` would have forced `swarm_spawn.go` to import the engine and re-introduce the cycle. Defining it in tools (exactly like `sandboxRunner`) keeps the tool import-free; the concrete type is injected at the composition root.
- **config.Config rides on the adapter, not the ctx.** The agent package must not import `internal/config`. So `WithSwarmContext` carries only agent/tools/llm types; the adapter (which legitimately imports config) holds `Cfg` as a construction-time field and assembles the full `RunConfig`.
- **No change to chat.go / runner.go.** Both reach the registry through `buildBaseRegistry`; registering swarm_spawn there is the single chokepoint, and `runTool`'s injection (Task 1) supplies the live deps. Editing the two files would have been dead motion — see Deviations.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] runTool needed the Budget threaded to inject WithSwarmContext**
- **Found during:** Task 1 (wiring the ctx seam)
- **Issue:** `runTool(ctx, call)` did not receive `ic.Budget`, but `WithSwarmContext` must carry the shared budget tree so the adapter can fan children off the parent's `*atomic.Int32` (D-09/SC#4). Without it the swarm could not inherit the parent budget.
- **Fix:** Changed `runTool` to `runTool(ctx, budget *Budget, call)` and updated the single `dispatch` call site to pass `ic.Budget`. No behavior change for any other tool (the extra ctx key is ignored by tools that do not read it).
- **Files modified:** internal/agent/llm_agent.go
- **Verification:** `go build ./...` + `go test ./internal/agent/` green; `go vet ./...` clean.
- **Committed in:** `827169a7` (Task 1 commit)

**2. [Rule 1 - Bug] Adapter test ctx needed the tool-call context layered too**
- **Found during:** Task 1 (first adapter test run)
- **Issue:** `tools.NewResult` (used by the adapter to wrap the report) requires a `WithToolCallContext` on the ctx to place its sidecar; the first adapter tests set only `WithSwarmContext`, so `NewResult` errored "missing tool-call context". In production `runTool` layers BOTH keys onto the same ctx, so the code was correct — the test fixture was incomplete.
- **Fix:** Added a `withToolCtx` test helper that layers `WithToolCallContext` (matching production), then layers `WithSwarmContext` on top. No production change.
- **Files modified:** internal/swarm/runner_adapter_test.go
- **Verification:** `go test ./internal/swarm/ -run TestRunnerAdapter` green.
- **Committed in:** `827169a7` (Task 1 commit)

---

**Total deviations:** 2 auto-fixed (1 blocking signature thread, 1 test-fixture bug).
**Impact on plan:** Both are mechanical and in-scope (the budget thread is load-bearing for budget inheritance; the test fixture mirrors production ctx layering). No scope creep — `chat.go`/`runner.go` were listed in the plan's files but needed no change (documented above).

## Issues Encountered
- None beyond the two auto-fixes. The 09-02 engine already exposed a clean `Run(ctx, RunConfig, goals)` signature and `Without` helper, so this plan was pure wiring + the boot registration + the demo.

## Known Stubs
None. `swarm_spawn` is wired end-to-end to the real engine; the `aura swarm-demo` FakeClient is a deterministic operator fixture (the live-LLM swarm E2E is 09-06's cot_eval tier, not a stub here).

## User Setup Required
None — no external service configuration. The three swarm env vars (`AURA_SWARM_MAX_GOALS`=8, `AURA_SWARM_CHILD_TIMEOUT_SEC`=120, `AURA_SWARM_MAX_CONCURRENT`=4) ship with safe defaults from 09-02.

## Next Phase Readiness
- The full swarm path is live in `aura chat`: the parent agent sees `swarm_spawn` in its registry (Deferred, discoverable via tool_search), and a call fans children out through the cycle-free seam. 09-06's live E2E (natural prompt, no "swarm" word, dual ground-truth + judge gate) can now exercise the tool end-to-end.
- `aura swarm-demo` gives the operator a deterministic, network-free smoke of the engine for CI/local sanity.
- Race + lint green on every touched package (swarm, tools, agent, cmd/aura) via the WSL toolchain; the post-merge `make quality-full` re-runs coverage + mutation per CLAUDE.md.

## Self-Check: PASSED

---
*Phase: 09-swarm-minimal*
*Completed: 2026-06-04*
