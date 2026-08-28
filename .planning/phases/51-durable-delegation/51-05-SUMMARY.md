---
phase: 51-durable-delegation
plan: 05
subsystem: agent-runtime
tags: [swarm, delegation, depth-cap, idempotency, go, concurrency, tdd]

# Dependency graph
requires:
  - phase: 51-durable-delegation
    provides: "51-01 (durable background delegation, worker/claim-loop paths) and 51-03 (goal/context split, live-cap schema render) this plan builds depth-bounded nesting on top of"
provides:
  - "Depth-conditional worker registry (workerRegistry): a worker keeps swarm_spawn only while its own nested spawn would still clear AURA_SWARM_MAX_DEPTH, bounded and re-derived fresh at every nesting level"
  - "Model-readable nesting-closed notice surfaced in a capped worker's own brief (hermes-style degrade-to-leaf), instead of a silent tool removal"
  - "Regression proof that a nested (worker-issued) delegation stays synchronous and never takes the plan-51-01 background branch (SWARM-04)"
  - "Extended fingerprint-collapse regression guard (SWARM-08) covering the two dispatch paths this plan opens: nested worker-issued dispatch and claim-loop-dispatched worker dispatch"
affects: [swarm-execution, agent-idempotency, swarm-tool-schema]

# Actuals (#2632)
actuals:
  tokens: 7200
  tasks: 2
  commits: 4

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Depth-bound tool rebinding: a granted swarm_spawn is bound to a FRESH RunnerAdapter{Depth: rc.Depth+1} per grant, never the shared static adapter, so depth is genuinely counted through nesting instead of pinned at a stale value (T-51-18)"
    - "Grant requires possession, not just permission: workerRegistry only ever derives from rc.ParentRegistry — a worker can never hold a tool its own parent lacked, independent of the depth cap (T-51-19)"
    - "Model-readable degrade-to-leaf notice appended to a capped worker's context section, mirroring hermes' delegate_tool.py role:leaf|orchestrator framing"

key-files:
  created:
    - internal/swarm/swarm_depth_test.go
  modified:
    - internal/swarm/swarm_depth.go
    - internal/swarm/swarm.go
    - internal/agent/swarm_context.go
    - internal/swarm/swarm_test.go
    - internal/swarm/runner_adapter_test.go
    - internal/agent/idempotency_operation_test.go

key-decisions:
  - "workerRegistry(rc RunConfig) (*tools.Registry, bool) — not the plan's loosely-specified workerRegistry(parent, depth) two-arg shape. The plan's artifact note did not account for depth actually needing to advance per nesting level (see Deviations); RunConfig carries Cfg/Enqueuer the real grant needs, and the bool return lets the caller surface the nesting-closed notice without a second depth computation."
  - "Enqueuer is left nil on every depth-bound RunnerAdapter workerRegistry constructs, as a structural (not just gate-reliant) guarantee that a nested delegation never backgrounds — Run's own rc.Depth<=1 check already makes this redundant, but nil removes the possibility outright."
  - "Task 2 built zero production code: 51-RESEARCH.md marked SWARM-08 MOSTLY DONE and the existing TestDeriveToolOperationContextDerivesForNestedToolCall guard (67d24aee4) already covers the mechanism generically; only new dispatch PATHS needed test coverage, not new logic."

patterns-established:
  - "Test-file LOC-cap refactor on touch: swarm_test.go split into swarm_test.go + swarm_depth_test.go (1:1 with production swarm_depth.go) when it crossed 600 LOC — the same refactor-on-touch discipline CLAUDE.md applies to production files"

requirements-completed: [SWARM-04, SWARM-05, SWARM-08]

coverage:
  - id: D1
    description: "A worker's registry contains swarm_spawn when its own nested spawn would still clear the depth cap, and the depth-conditional grant is derived fresh per level (never a stale, unincrementing depth)"
    requirement: "SWARM-05"
    verification:
      - kind: unit
        ref: "internal/swarm/swarm_depth_test.go#TestWorkerRegistryDepthBoundary"
        status: pass
      - kind: unit
        ref: "internal/swarm/swarm_depth_test.go#TestWorkerRegistryNeverGrantsATheParentLacked"
        status: pass
    human_judgment: false
  - id: D2
    description: "At the depth cap, the worker is told plainly it cannot delegate further (model-readable), rather than discovering an absent tool"
    requirement: "SWARM-05"
    verification:
      - kind: unit
        ref: "internal/swarm/swarm_depth_test.go#TestSwarmDepthGuardAtCapIsModelReadable"
        status: pass
    human_judgment: false
  - id: D3
    description: "A delegation issued by a worker (nested, depth>1) runs synchronously inside its own turn and never takes the durable background-enqueue branch, including on child failure"
    requirement: "SWARM-04"
    verification:
      - kind: unit
        ref: "internal/swarm/swarm_depth_test.go#TestNestedDelegationSynchronous"
        status: pass
      - kind: unit
        ref: "internal/swarm/swarm_depth_test.go#TestNestedDelegationFailureIsolation"
        status: pass
    human_judgment: false
  - id: D4
    description: "The fingerprint-collapse regression guard (SWARM-08) covers the two NEW dispatch paths this plan opens (nested worker dispatch, claim-loop-dispatched worker dispatch), asserting the actual dispatch derivation, not registry membership"
    requirement: "SWARM-08"
    verification:
      - kind: unit
        ref: "internal/agent/idempotency_operation_test.go#TestDeriveToolOperationContextDerivesForDelegatedDispatch"
        status: pass
    human_judgment: false
  - id: D5
    description: "Live nested swarm_spawn run end-to-end on the running stack (a worker actually delegating to its own sub-workers through the real agent loop)"
    verification: []
    human_judgment: true
    rationale: "Plan's own <verification> block defers this: 'Live nested run is verified in plan 51-08's SC#2 driver.' Not in scope for this plan's automated verification."

# Metrics
duration: 40min
completed: 2026-08-28
status: complete
---

# Phase 51 Plan 05: Depth-Bounded Nesting for Worker Delegation Summary

**Workers can now orchestrate workers of their own, bounded by `AURA_SWARM_MAX_DEPTH` and re-derived at every nesting level so the cap is actually enforced (not just checked against a depth that never advances) — plus the SWARM-08 fingerprint-collapse guard extended to the two new dispatch paths this opens.**

## Performance

- **Duration:** ~40 min
- **Started:** 2026-08-28T09:35:00+02:00 (approx, first Read)
- **Completed:** 2026-08-28T10:02:17+02:00
- **Tasks:** 2
- **Files modified:** 6 (1 created, 5 modified)

## Accomplishments

- **Depth-bounded nesting (SWARM-05).** `swarm_depth.go`'s new `workerRegistry(rc RunConfig) (*tools.Registry, bool)` replaces `runChild`'s unconditional `tools.Without(rc.ParentRegistry, "swarm_spawn")`. A worker keeps `swarm_spawn` only when its OWN nested call (`rc.Depth+1`) would still clear `checkDepth` against the live cap — and when granted, `swarm_spawn` is rebound to a **fresh** `RunnerAdapter{Depth: rc.Depth+1}`, never the shared static adapter the composition root constructs once at boot. This matters: the shared adapter's `Depth` field is a constant set at `cmd/aura/main.go` boot time (`swarm.NewRunnerAdapter` always seeds `Depth: 1`), so reusing it unchanged across nesting levels would have pinned every nested call's depth at the SAME value forever — turning an operator-raised `AURA_SWARM_MAX_DEPTH` into unbounded recursion instead of the extra level intended (T-51-18, the plan's own DoS threat entry). The grant additionally requires the parent registry already carried `swarm_spawn` (T-51-19): a worker can never gain a tool its own parent lacked, independent of how high the cap is set.
- **Model-readable at-cap notice.** A worker denied nesting reads `nestingClosedNotice` in its own brief context (hermes' `role:leaf|orchestrator` degrade framing), rather than discovering the limit by calling an absent tool.
- **SWARM-04 synchrony, proven not just assumed.** The plan-51-01 background-enqueue branch (`rc.Enqueuer != nil && rc.Depth <= 1`) already excluded nested (depth>1) calls before this plan — `TestNestedDelegationSynchronous` and `TestNestedDelegationFailureIsolation` are explicit regression proofs of that existing behavior (they pass unmodified against the original code), satisfying the plan's `verification: explicit` requirement for the SWARM-04 edge case.
- **SWARM-08 closed with zero production changes.** `TestDeriveToolOperationContextDerivesForDelegatedDispatch` extends the existing `67d24aee4` regression guard to a nested (two-hop) worker dispatch and a claim-loop-dispatched worker dispatch — both assert the actual dispatch derivation (fingerprint), not registry membership, per RESEARCH.md's explicit warning that a membership-only test would have passed throughout the entire period the shipped defect existed.
- **Deleted the now-false "flat v1 forbids nested swarms" comment** and updated the two doc comments (`swarm.go`'s `swarmSpawnTool`, `agent/swarm_context.go`'s `SwarmContextValue`) that described the old unconditional strip.

## Task Commits

Task 1 (`swarm_depth.go`/`swarm.go`/`swarm_context.go`/`swarm_test.go`) is `tdd="true"` and produced RED→GREEN commits; Task 2 built no production code (single `test` commit). A fourth follow-on commit closes a documented-but-untested threat-model mitigation discovered while implementing Task 1.

1. **Task 1 RED — failing nested delegation tests** - `863d9c4ff` (test)
2. **Task 1 GREEN — depth-bounded nesting implementation** - `42f5a2153` (feat)
3. **Task 2 — extended SWARM-08 regression guard** - `b6b142c7f` (test)
4. **Follow-on — T-51-19 threat-model test coverage** - `a871b9577` (test)

**Plan metadata:** this commit (docs: complete plan)

## Files Created/Modified

- `internal/swarm/swarm_depth.go` - `canNest`, `nestingClosedNotice`, `appendNestingClosedNotice`, `workerRegistry` — the depth-conditional grant
- `internal/swarm/swarm.go` - `runChild` wired to `workerRegistry`; stale "flat v1" comment replaced
- `internal/agent/swarm_context.go` - `SwarmContextValue` doc comment updated to describe the depth-conditional derivation
- `internal/swarm/swarm_depth_test.go` (new) - depth-cap test suite: `TestSwarmDepthGuard`, `TestCheckDepth`, `TestNestedDelegationSynchronous`, `TestNestedDelegationFailureIsolation`, `TestSwarmDepthGuardAtCapIsModelReadable`, `TestWorkerRegistryDepthBoundary`, `TestWorkerRegistryNeverGrantsATheParentLacked`
- `internal/swarm/swarm_test.go` - split (LOC cap), `outcome.delay` field + `Stream` delay handling, `workerRegistry()` test stub renamed to `stubWorkerRegistry()`
- `internal/swarm/runner_adapter_test.go` - updated its one `stubWorkerRegistry()` call site (rename fallout)
- `internal/agent/idempotency_operation_test.go` - `TestDeriveToolOperationContextDerivesForDelegatedDispatch` (two subtests)

## Decisions Made

- **`workerRegistry(rc RunConfig)`, not `workerRegistry(parent, depth)`.** See Deviations below — the plan's artifact note under-specified the depth-propagation mechanism.
- **`RunnerAdapter.Enqueuer` left `nil` on every depth-bound adapter `workerRegistry` constructs**, as a structural guarantee (not just a gate) that a nested delegation can never background — belt-and-braces alongside `Run`'s existing `rc.Depth<=1` gate.
- **Task 2 added test coverage only.** `idempotency_operation.go` is byte-for-byte unchanged (`git diff --stat` confirms zero lines) — the mechanism 67d24aee4 shipped already generalizes to arbitrary nesting depth via the `ScopeAgentTool`-as-parent branch; only the new PATHS needed proof.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Corrected the depth-propagation design: a two-arg `workerRegistry(parent, depth)` cannot actually bound recursion**
- **Found during:** Task 1, before writing any code — reading `runner_adapter.go` and `cmd/aura/main.go`'s composition root
- **Issue:** The plan's artifact note specifies `swarm.workerRegistry(parent tools.Registry, depth int) tools.Registry`. Tracing the runtime wiring: the composition root builds exactly ONE `RunnerAdapter` (`swarm.NewRunnerAdapter(*cfg)`, `Depth: 1` fixed at construction) and binds it into the ONE `swarm_spawn` tool registered on the top-level agent's registry. If a granted worker's registry simply included THAT SAME tool object (the naive reading of a `(parent, depth)` signature — "copy the parent's swarm_spawn into the child"), every nested call would dispatch through the identical adapter with its `Depth` field frozen at 1 forever, regardless of true nesting level. `checkDepth(1, cap)` would then evaluate `true` at every recursion level whenever `cap > 2`, so raising `AURA_SWARM_MAX_DEPTH` above the default would not add "one more level" — it would open **unbounded recursion**, exactly the T-51-18 DoS threat the plan's own threat register calls out as `high` severity.
- **Fix:** `workerRegistry` takes the full `RunConfig` (not just `parent`+`depth`) and, when granting, constructs a **fresh** `*tools.SwarmSpawn{Runner: &RunnerAdapter{Cfg: rc.Cfg, Depth: rc.Depth + 1}, Caps: ...}` per grant — so depth genuinely advances by exactly one at each nesting level, and `checkDepth` correctly rejects at the configured boundary. The acceptance criterion ("contains `func workerRegistry(`") does not pin the exact parameter list, so this stays compliant with the plan's binding checks while fixing the underlying correctness/security gap.
- **Files modified:** `internal/swarm/swarm_depth.go`
- **Verification:** `TestWorkerRegistryDepthBoundary` proves the boundary sits at exactly cap-1/cap across two nesting levels (not just one); `TestSwarmDepthGuard`'s original single-level assertions pass unmodified.
- **Committed in:** `42f5a2153` (Task 1 GREEN commit)

**2. [Rule 3 - Blocking] Renamed a test-only `workerRegistry()` helper that collided with the new production function name**
- **Found during:** Task 1, writing the RED tests
- **Issue:** `swarm_test.go` already defined `func workerRegistry() *tools.Registry` (a zero-arg test fixture returning a registry with a stub `swarm_spawn` entry). Go does not support overloading, so introducing the plan-mandated production `workerRegistry` under the same name is a straight redeclaration — the package would not compile.
- **Fix:** Renamed the test helper to `stubWorkerRegistry()` and updated its two call sites (`swarm_test.go`'s `testRunConfig`, and `internal/swarm/runner_adapter_test.go`'s `TestRunnerAdapterWorkerRegistryExcludesSwarmSpawn`) — the latter file is outside this plan's declared `<files>` list but the rename is a mechanical, behavior-preserving fix required for the package to build at all.
- **Files modified:** `internal/swarm/swarm_test.go`, `internal/swarm/runner_adapter_test.go`
- **Verification:** `go vet ./internal/swarm/...` clean; `TestRunnerAdapterWorkerRegistryExcludesSwarmSpawn` still passes unchanged.
- **Committed in:** `863d9c4ff` (Task 1 RED commit)

**3. [Rule 2 - Missing Critical] `swarm_test.go` exceeded CLAUDE.md's 600-LOC cap after the RED-phase additions**
- **Found during:** Task 1 RED commit — the pre-commit `file-size` hook blocked the commit at 643 LOC
- **Issue:** CLAUDE.md's NO-GOD-CLASS rule applies to test files, not just production files, and takes precedence over the plan's file list when the two conflict.
- **Fix:** Split the depth-cap-related tests (`TestSwarmDepthGuard`, `TestCheckDepth`, and the four new tests) into a new `internal/swarm/swarm_depth_test.go`, pairing 1:1 with production `swarm_depth.go` — the same naming convention `swarm_test.go`/`swarm.go` already establishes.
- **Files modified:** `internal/swarm/swarm_test.go` (trimmed to 477 LOC), `internal/swarm/swarm_depth_test.go` (new, 208 LOC)
- **Verification:** `wc -l` on both files, plus the `file-size` pre-commit hook passing on the RED commit.
- **Committed in:** `863d9c4ff` (Task 1 RED commit)

**4. [Rule 1 - Bug] `TestSwarmDepthGuardAtCapIsModelReadable`'s first draft asserted the wrong thing**
- **Found during:** Task 1, running the RED test before committing
- **Issue:** The router client's fallback for an unmatched goal returns `{ok, ""}` (empty content, `FinishReason: stop`) rather than a failure — so an assertion checking only `reports[0].Status == StatusOK` passed regardless of whether the nesting-closed notice actually reached the worker's brief. The test was a false positive.
- **Fix:** Tightened the assertion to also require `reports[0].Summary == "ack"` — the text ONLY the matched (notice-routed) outcome produces — so the test genuinely fails when the notice is missing (confirmed: it failed correctly against the RED-phase stub) and genuinely passes only when the real implementation delivers it.
- **Files modified:** `internal/swarm/swarm_depth_test.go`
- **Verification:** Ran the test against the RED stub (failed for the right reason) and against GREEN (passed).
- **Committed in:** `863d9c4ff` (RED, discovered and fixed before commit)

**5. [Rule 2 - Missing Critical] Added explicit test coverage for the T-51-19 threat-model mitigation**
- **Found during:** Task 1, after implementing `workerRegistry`'s `hadSpawn` guard
- **Issue:** The plan's threat register lists T-51-19 ("a nested worker inheriting a wider surface than its parent") with disposition `mitigate` and names the specific mechanism (`workerRegistry derives from the PARENT's registry only`). The mechanism was implemented in the GREEN commit but had no dedicated regression test — none of the three plan-named tests exercise a parent registry that never carried `swarm_spawn` in the first place.
- **Fix:** Added `TestWorkerRegistryNeverGrantsATheParentLacked`, proving a generous `AURA_SWARM_MAX_DEPTH` alone does not grant `swarm_spawn` when the parent registry never had it.
- **Files modified:** `internal/swarm/swarm_depth_test.go`
- **Verification:** Passes against the unmodified GREEN implementation (regression-confirming, not new functionality).
- **Committed in:** `a871b9577`

---

**Total deviations:** 5 auto-fixed (2 Rule 1 bugs, 1 Rule 3 blocking fix, 2 Rule 2 missing-critical additions)
**Impact on plan:** The Rule 1 depth-propagation correction is the most consequential — a literal reading of the plan's artifact signature would have shipped an unbounded-recursion DoS the moment an operator raised `AURA_SWARM_MAX_DEPTH` above the default. All other deviations are mechanical (rename, split, test hardening) with no behavior change to production code beyond the deliberate fix. No scope creep — everything closes a requirement, a prohibition, or a threat-model entry this plan itself declared.

## Issues Encountered

- **Shared working tree (unrelated concurrent session).** Between the Task 2 commit (`b6b142c7f`) and the follow-on T-51-19 commit (`a871b9577`), a merge commit (`5b4d811bb`, authored by the operator/another session) landed `origin/master` changes into this SAME working tree — an unrelated F-03 context-paging plan (`internal/conversations/*`, `internal/db/*`, `prd.md`). Verified via `git show --stat` that it touched none of this plan's files and did not corrupt any commit; re-ran `go build ./...` and the full `-race` suite for `internal/swarm`/`internal/agent` afterward — both clean. No action needed beyond the verification.
- **`-race` requires WSL on this host.** Windows-native `go test -race` fails with `CGO_ENABLED=1` required; ran all `-race` verification in WSL per CLAUDE.md's documented environment split, mounting `D:/Repo/Aura` at `/mnt/d/Repo/Aura`.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- SWARM-04, SWARM-05, and SWARM-08 requirements are closed for this plan's mechanical scope. The plan's own `<verification>` block defers the live end-to-end nested run to **plan 51-08's SC#2 driver** — that is the one remaining proof this plan does not carry, by design.
- `go build ./... && go vet ./... && go test ./...` green (WSL, authoritative per CLAUDE.md); `go test -race ./internal/swarm/... ./internal/agent/... ./internal/gateway/...` green (WSL). Three pre-existing Windows-native-only test failures (`TestTempScriptRecordsAdHocEvidenceWithoutCanonicalSuite`, `TestNoSuiteNudgeNamesTheDirectoryTheClassifierAccepts`, `TestStageBoxArtifact_ExtractsRegularFile`) confirmed unrelated to this plan and confirmed passing in WSL.
- No blockers for downstream plans in this phase.

---
*Phase: 51-durable-delegation*
*Completed: 2026-08-28*

## Self-Check: PASSED

- All 7 modified/created files verified present on disk with `[ -f ]`.
- All 4 commit hashes (863d9c4ff, 42f5a2153, b6b142c7f, a871b9577) verified present in `git log --all`.
- Re-ran every `<acceptance_criteria>` from both tasks — all pass (Without-strip removed from swarm.go, workerRegistry defined, "flat v1" phrase gone from internal/, LOC caps held, TestSwarmDepthGuard shows only additions, idempotency_operation.go diff-stat empty, single idempotency test file, ≥2 TestDeriveToolOperationContext functions).
- Re-ran the plan-level `<verification>`: `go build ./...`, `go vet ./...`, `go test ./...` (WSL, full repo green minus 3 pre-existing Windows-native-only failures confirmed passing in WSL), and `go test -race ./internal/swarm/... ./internal/agent/... ./internal/gateway/...` (WSL, green).
