---
phase: 45-harness-correctness
plan: 04
subsystem: agent-harness
tags: [tool-calls, idempotency, dedup, tdd]

# Dependency graph
requires: ["45-02"]
provides:
  - "internal/agent/llm_agent_call_dedup.go: uniquifyToolCallIDs (D-13, blank-id + collision repair) and dedupeSameMessageCalls (D-12, exact same-message repeat drop), both pure/synchronous over one []llm.ToolCall slice"
  - "internal/agent/llm_agent.go: the two call sites wired at hermes' two positions -- immediately after consume() returns, and immediately before the assistant-message history append"
  - "internal/agent/llm_agent_round.go: roundBudget + recordRequestBuilt, extracted from llm_agent.go's Run loop to hold the file under the 600-LOC cap once this plan's two call sites landed"
  - "internal/agent/llm_agent_call_dedup_test.go: table-driven coverage of both functions plus the collision-transitivity, determinism, order-preservation and id-irrelevance probe edges"
affects: [45-05, 45-06, 45-07, 45-08]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Deterministic content-derived id fallback (call_<sha256(name:args:index)[:12]>) instead of a random UUID, so the prompt-cache prefix stays byte-stable across a repaired batch (D-13 HARD INVARIANT) -- confirmed against hermes' deterministic_call_id, byte-identical formula"
    - "Real taken-id membership check (map[string]struct{}) for collision repair, not a per-base-name suffix counter -- the latter misses the collision-transitivity case where a repaired id collides with a LATER call's own original id"
    - "Same-message exact-repeat drop keyed on Function.Name + NUL + canonicaljson.CanonicalArgs, independent of and non-overlapping with budget_dedup.go's cross-round loop-guard veto (D-18)"
    - "RED-via-assertion-failure (a compiling stub returning the input unchanged) instead of RED-via-compile-failure, because this repo's pre-commit hook runs `go vet ./...` over the whole tree and a genuinely undefined symbol blocks every commit, not just the one introducing it"

key-files:
  created:
    - internal/agent/llm_agent_call_dedup.go
    - internal/agent/llm_agent_call_dedup_test.go
    - internal/agent/llm_agent_round.go
  modified:
    - internal/agent/llm_agent.go
    - internal/agent/llm_agent_parallel_test.go
    - internal/agent/llm_agent_parallel_metrics_test.go

key-decisions:
  - "RED commits ship a compiling identity-passthrough stub alongside the failing tests, rather than leaving the new symbol undefined. lefthook's pre-commit `vet` hook runs `go vet ./...` (whole tree, not staged-files-only), so a genuinely undefined symbol blocks the commit outright -- this plan's execution context forbids `--no-verify`. The stub still produces genuine RED: the collision-repair and blank-id assertions fail against an identity function (the determinism probe trivially holds either way, and is re-verified against the real implementation in GREEN)."
  - "D:/tmp/hermes-agent/agent/message_sanitization.py was readable and read in full (lines 490-604, deterministic_call_id / coalesce_tool_call_id / uniquify_tool_call_ids). Confirmed byte-identical to this plan's derivation formula and confirmed the taken-set collision-repair loop shape (`while new_id in seen: n += 1`) matches this implementation's algorithm exactly, including the collision-transitivity handling. One structural divergence, and it is the plan's own explicit instruction, not a deviation: hermes splits blank-id coalescing into a separate call site (`build_assistant_message`) and has `uniquify_tool_call_ids` skip blank ids outright, while this plan's `uniquifyToolCallIDs` does both in one function (coalesce-then-repair) per its own <action> block. hermes' composite `call_x|fc_y` id-splitting was NOT ported, per the plan's explicit prohibition (Codex Responses-specific, dead code against OpenRouter/DeepSeek)."
  - "llm_agent.go measured 592/600 LOC after both call-site insertions (up from 583 before this plan), crossing the plan's 590-LOC refactor-on-touch threshold. Both measured extraction candidates were split into llm_agent_round.go (roundBudget from the prompt.Budget construction, recordRequestBuilt from the agent_request_built reasoningtrace.Record call) rather than just one, for headroom against the four remaining wave-3 plans that still touch this file. Landing point: llm_agent.go 556/600, llm_agent_round.go 70/600."
  - "Three pre-existing test fixtures (TestDispatch_ParallelRespectsFanoutCap, TestExecuteBatch_PanickingToolSurfacesModelVisibleError's parallel_batch_call case, TestExecuteBatch_PanicRecordsObservabilityMetrics) used multiple tool calls with byte-identical (name, arguments) in one assistant message as a convenience fixture for measuring concurrency/panic-recovery across N calls -- exactly the shape D-12 now legitimately deduplicates to one surviving call. Fixed by giving each call distinct arguments, restoring N genuinely different calls and preserving the fixtures' original test intent under the new, correct dedup behavior. This is a fixture repair, not a weakened assertion (CLAUDE.md: never modify a test to make it pass unless the test itself is broken -- here the fixture's identical-args shape was the bug this phase fixes)."

patterns-established:
  - "A same-message batch transform (nil/empty/single input is a strict identity return of the same slice value, not a fresh allocation) -- both uniquifyToolCallIDs and dedupeSameMessageCalls follow this shape and it should be the default for any future per-turn []llm.ToolCall transform in this package."

requirements-completed: [HARN-08, HARN-09]

# Metrics
duration: ~70min
completed: 2026-08-13
---

# Phase 45 Plan 04: Same-message tool-call id repair and exact-repeat dedup Summary

**`uniquifyToolCallIDs` (D-13) and `dedupeSameMessageCalls` (D-12) now run at the two points hermes runs them in the agent loop -- deterministic collision/blank-id repair the instant a response is consumed, and an exact same-message repeat dropped immediately before it reaches history or the wire -- closing the class of DeepSeek duplicate-id rejections and silent-collapsed reservations this phase exists to fix.**

## Performance

- **Duration:** ~70 min
- **Started:** 2026-08-13
- **Completed:** 2026-08-13
- **Tasks:** 3
- **Files modified:** 6 (3 new, 3 modified)

## Accomplishments

- `uniquifyToolCallIDs` (`internal/agent/llm_agent_call_dedup.go`) coalesces a blank id into `call_<sha256("name:args:index")[:12]>` first (index is the call's slice position, so two blank-id calls with identical name+args still diverge), then repairs id collisions in slice order via a real taken-id membership check -- the first occurrence keeps its id, every later occurrence gets `<id>_d<n>` incrementing n until free. The membership check (not a per-name counter) is what makes the collision-transitivity edge (`["abc","abc","abc_d2"]` -> three distinct ids) correct rather than accidentally re-colliding.
- `dedupeSameMessageCalls` drops a later call in the same assistant message whose `Function.Name + canonicaljson.CanonicalArgs(Function.Arguments)` exactly matches an earlier call, preserving order and never leaving an orphan `tool_call` or a synthesized result.
- Both call sites are wired into `internal/agent/llm_agent.go`'s `Run` loop at the two documented positions: immediately after `consume()` returns (before the terminal/runnable partition, argument validation, the reservation key, dispatch, or the history append can see an unrepaired batch), and immediately before `a.history = append(..., ToolCalls: calls)`.
- `internal/agent/llm_agent_round.go` (new) holds `roundBudget` and `recordRequestBuilt`, extracted mechanically from `llm_agent.go`'s `Run` loop once the two new call sites pushed the file to 592/600 LOC -- both are pure/side-effect-only functions with no external signature change.
- No randomness source (`uuid`, `rand.`, `time.Now`) appears anywhere in `llm_agent_call_dedup.go` -- verified structurally by grep, not by review, per D-13's HARD INVARIANT.
- Three pre-existing test fixtures that happened to construct multiple byte-identical `(name, args)` tool calls in one message were repaired to use distinct arguments per call, since that exact shape is what D-12 now correctly collapses.

## Task Commits

Each task committed atomically; RED preceded GREEN for both TDD tasks:

1. **Task 1 RED — failing test for uniquifyToolCallIDs** — `660b9bd73` (test)
2. **Task 1 GREEN — deterministic collision/blank-id repair** — `1432c7d9d` (feat)
3. **Task 2 RED — failing test for dedupeSameMessageCalls** — `fd0d6a40f` (test)
4. **Task 2 GREEN — exact same-message repeat drop** — `f5c79601b` (feat)
5. **Task 3 — wire both call sites, split llm_agent_round.go, fix 3 test fixtures** — `bf352b7bb` (feat)

## Files Created/Modified

- `internal/agent/llm_agent_call_dedup.go` (new, 131 LOC) — `uniquifyToolCallIDs`, `deterministicBlankCallID`, `dedupeSameMessageCalls`; header comment documents the HARD INVARIANT (no randomness) and the boundary with `budget_dedup.go`'s cross-round loop guard.
- `internal/agent/llm_agent_call_dedup_test.go` (new, 346 LOC) — `TestUniquifyToolCallIDs` (table: nil/empty/single identity, pair/triple/transitive collision), `TestUniquifyToolCallIDsIsDeterministic`, `TestUniquifyToolCallIDsCoalescesBlankIDs`, `TestDedupeSameMessageCalls` (table: identity, byte-identical repeat, reordered-JSON canonicalization, distinct args/names survive), `TestDedupeSameMessageCallsPreservesOrder`, `TestDedupeSameMessageCallsAcrossSeparateInvocationsBothSurvive`, `TestDedupeSameMessageCallsIgnoresIDs`.
- `internal/agent/llm_agent.go` — two reassignment statements (`calls = uniquifyToolCallIDs(calls)`, `calls = dedupeSameMessageCalls(calls)`) plus the `roundBudget`/`recordRequestBuilt` extraction; 583 -> 592 (pre-split) -> 556 LOC.
- `internal/agent/llm_agent_round.go` (new, 70 LOC) — `roundBudget`, `recordRequestBuilt`.
- `internal/agent/llm_agent_parallel_test.go` — `TestDispatch_ParallelRespectsFanoutCap`'s 5-call fixture and `TestExecuteBatch_PanickingToolSurfacesModelVisibleError`'s `parallel_batch_call` case now use distinct per-call arguments.
- `internal/agent/llm_agent_parallel_metrics_test.go` — `TestExecuteBatch_PanicRecordsObservabilityMetrics`'s two `panic_tool` calls now use distinct arguments.

## Decisions Made

- **RED via a compiling stub, not a compile failure.** This repo's `lefthook.yml` pre-commit `vet` command runs `go vet ./...` over the whole module (not staged-files-only), and the harness's execution rules forbid `--no-verify`. A genuinely undefined symbol referenced by the new test file blocks every commit attempt, not just the one that introduces it. Both RED commits therefore ship the new source file with an identity-passthrough stub body (`return calls` unchanged) alongside the failing tests: the package compiles and the pre-commit gate passes, while the collision-repair, blank-id, and (for Task 2) dedup/order/canonicalization assertions genuinely fail against the stub. Verified failing output recorded below.
- **`uniquifyToolCallIDs` RED failing output** (`go test -run 'TestUniquifyToolCallIDs' ./internal/agent/`, stub body):
  ```
  --- FAIL: TestUniquifyToolCallIDs/pair_collision (id[1] = "abc", want "abc_d2")
  --- FAIL: TestUniquifyToolCallIDs/triple_collision (id[1] = "abc", want "abc_d2")
  --- FAIL: TestUniquifyToolCallIDs/repaired_id_colliding_with_a_later_original (id[1] = "abc", want "abc_d2")
  --- FAIL: TestUniquifyToolCallIDsCoalescesBlankIDs (call 0 still has a blank id)
  ```
  (`TestUniquifyToolCallIDsIsDeterministic` trivially passes for an identity function and was re-verified as a genuine determinism proof against the real implementation in GREEN.)
- **`dedupeSameMessageCalls` RED failing output** (`go test -run 'TestDedupeSameMessageCalls' ./internal/agent/`, stub body):
  ```
  --- FAIL: TestDedupeSameMessageCalls/byte-identical_repeat (length = 2, want 1)
  --- FAIL: TestDedupeSameMessageCalls/same_name,_reordered-but-equivalent_JSON_args (length = 2, want 1)
  --- FAIL: TestDedupeSameMessageCallsPreservesOrder (got [a1 b1 a2 c1], want ids in order [a1 b1 c1])
  --- FAIL: TestDedupeSameMessageCallsIgnoresIDs/identical_name+args,_different_ids (want 1 surviving call, got 2)
  ```
- **`D:/tmp/hermes-agent` was readable** and `agent/message_sanitization.py` lines 490-604 were read in full. `deterministic_call_id` is byte-identical to this plan's formula (`f"{fn_name}:{arguments}:{index}"` -> sha256 hex[:12] -> `call_` prefix). `uniquify_tool_call_ids`'s collision loop (`seen` set, `while new_id in seen: n += 1`) confirms the taken-set membership-check design (not a per-name counter) is the correct shape, including the collision-transitivity edge. One deliberate structural divergence, directed by the plan itself: hermes splits blank-id coalescing into a separate call site and has `uniquify_tool_call_ids` skip blank ids outright; this plan's `uniquifyToolCallIDs` does both (coalesce, then repair) in one function per its own `<action>` block. hermes' composite `call_x|fc_y` id-splitting was intentionally NOT ported (D-13's explicit prohibition — Codex Responses-specific).
- **`wc -l internal/agent/llm_agent.go`: 583 before this plan (per 45-02-SUMMARY.md) -> 592 after both call-site insertions -> 556 after the `llm_agent_round.go` split.** The 590-LOC threshold fired; both measured extraction candidates (the `prompt.Budget` construction and the `agent_request_built` trace call) were extracted, not just one, for headroom against 45-05 through 45-08, which all still touch this file.
- **Three pre-existing test fixtures needed repair, not the new dedup logic.** `TestDispatch_ParallelRespectsFanoutCap` (5 identical-args `tracking` calls), `TestExecuteBatch_PanickingToolSurfacesModelVisibleError`'s `parallel_batch_call` case (2 identical-args `panic_tool` calls), and `TestExecuteBatch_PanicRecordsObservabilityMetrics` (2 identical-args `panic_tool` calls) all constructed multiple byte-identical `(name, arguments)` calls in one message as a convenience fixture for testing concurrency/panic-recovery across N calls. That is exactly the D-12 shape now correctly collapsed to one surviving call. Diagnosed by first confirming (via a temporary revert of the two Task-3-modified files to their last-committed state) that these three tests were NOT failing before the wiring landed, then confirming the fanout test's own failure message ("test did not exercise concurrency, max observed=1") matched dedup-to-one exactly rather than scheduling flakiness. Fixed by giving each call distinct arguments (`{"i":N}`), restoring N genuinely different calls per fixture and preserving each test's original intent.
- **Two other `internal/agent` test failures and one `internal/agent/tools` failure are pre-existing Windows-only environment artifacts, confirmed unrelated to this plan.** `TestTempScriptRecordsAdHocEvidenceWithoutCanonicalSuite` and `TestNoSuiteNudgeNamesTheDirectoryTheClassifierAccepts` fail identically against the last-committed baseline (before any of this plan's edits) — Windows temp-path classification, not this plan's concern. `TestStageBoxArtifact_ExtractsRegularFile` (`internal/agent/tools`) fails on `staged mode = -rw-rw-rw-, want 0600` — a POSIX file-mode-bits test that cannot be affected by this plan's changes by package dependency direction alone (`internal/agent/tools` does not import `internal/agent`), and passes cleanly under WSL/Linux (see Verification below).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Pre-commit `go vet ./...` gate is incompatible with RED-via-undefined-symbol**
- **Found during:** first attempt to commit Task 1's RED test file.
- **Issue:** `lefthook.yml`'s pre-commit `vet` command runs `go vet ./...` over the whole module. A test file referencing a genuinely undefined function fails this gate outright, and the harness's execution rules forbid `--no-verify`.
- **Fix:** both RED commits ship a compiling identity-passthrough stub for the function under test, alongside the failing assertions. The stub is fully replaced in the following GREEN commit.
- **Files modified:** `internal/agent/llm_agent_call_dedup.go` (both RED and GREEN commits).
- **Commits:** `660b9bd73`, `1432c7d9d`, `fd0d6a40f`, `f5c79601b`.

**2. [Rule 1 - Bug] Three test fixtures accidentally relied on the exact duplicate-call shape D-12 fixes**
- **Found during:** Task 3's full-suite verification after wiring both call sites.
- **Issue:** `TestDispatch_ParallelRespectsFanoutCap`, `TestExecuteBatch_PanickingToolSurfacesModelVisibleError`'s `parallel_batch_call` case, and `TestExecuteBatch_PanicRecordsObservabilityMetrics` each constructed 2-5 tool calls with byte-identical `(name, arguments)` in one assistant message to exercise concurrency/panic-recovery across N calls — a legitimate same-message duplicate under the new D-12 semantics, so `dedupeSameMessageCalls` correctly collapsed each batch to 1 surviving call and broke the N-call assumption.
- **Fix:** gave each call distinct arguments (`{"i":N}`) per fixture, restoring N genuinely different calls while preserving each test's original measurement intent.
- **Files modified:** `internal/agent/llm_agent_parallel_test.go`, `internal/agent/llm_agent_parallel_metrics_test.go`.
- **Commit:** `bf352b7bb`.

### Out of Scope (documented, not fixed)

- `TestTempScriptRecordsAdHocEvidenceWithoutCanonicalSuite`, `TestNoSuiteNudgeNamesTheDirectoryTheClassifierAccepts` (`internal/agent`) — pre-existing Windows temp-path classification failures, confirmed identical against the pre-plan baseline. Out of scope per SCOPE BOUNDARY (not caused by this plan's changes).
- `TestStageBoxArtifact_ExtractsRegularFile` (`internal/agent/tools`) — pre-existing Windows POSIX-file-mode-bits artifact (`staged mode = -rw-rw-rw-, want 0600`), confirmed green under WSL/Linux. Out of scope: this plan touches only `internal/agent` (parent package), which `internal/agent/tools` does not import.

## Issues Encountered

None beyond the auto-fixed items above; all were resolved within this plan's scope.

## User Setup Required

None — no external service configuration required.

## TDD Gate Compliance

- **Task 1 RED gate:** `test(45-04): add failing test for uniquifyToolCallIDs collision repair (D-13)` — `660b9bd73`.
- **Task 1 GREEN gate:** `feat(45-04): repair duplicate/blank tool-call ids deterministically (D-13)` — `1432c7d9d`, landing after RED.
- **Task 2 RED gate:** `test(45-04): add failing test for dedupeSameMessageCalls (D-12)` — `fd0d6a40f`.
- **Task 2 GREEN gate:** `feat(45-04): drop exact same-message tool-call repeats (D-12)` — `f5c79601b`, landing after RED.
- **Task 3:** `feat(45-04): wire same-message id repair and dedup into the agent loop` — `bf352b7bb`. Not a TDD task per the plan's own frontmatter (no `tdd="true"` on Task 3) — it wires already-tested pure functions and performs a mechanical LOC-cap split; verification is the full-suite run plus the fanout/panic fixture repairs documented above.

## Verification

- `go vet ./...` and `go build ./...`: clean (Windows and WSL).
- `go test -race ./internal/agent/... ./internal/gateway/...`: green under WSL (`/mnt/d/Repo/Aura`) — `internal/agent` 29.9s, `internal/agent/agenttest`, `internal/agent/display`, `internal/agent/mcptools`, `internal/agent/panicobs`, `internal/agent/prompt`, `internal/agent/tools`, `internal/agent/workflow`, `internal/gateway` all `ok`. `-race` cannot build natively on this Windows host (no CGO); the non-race Windows run showed the same 3 pre-existing environment-only failures documented above, and the WSL run confirms they are Windows-specific.
- `wc -l internal/agent/llm_agent.go internal/agent/llm_agent_round.go internal/agent/llm_agent_call_dedup.go`: 556 / 70 / 131 — all under the 600-LOC cap.
- `grep -c "uuid\|rand\.\|time.Now" internal/agent/llm_agent_call_dedup.go`: 0.
- `grep -c "sha256" internal/agent/llm_agent_call_dedup.go`: 3 (used only by `deterministicBlankCallID`, not re-invented for argument equality — `dedupeSameMessageCalls` uses `canonicaljson.CanonicalArgs`).
- `grep -n "uniquifyToolCallIDs(\|dedupeSameMessageCalls("  internal/agent/llm_agent.go`: exactly one line each, at the two documented positions.

## Next Phase Readiness

- Both same-message repair functions are wired and proven; the round-discrimination work from 45-02 and the replay-marker/boot-guard work from 45-03 are untouched and compatible (neither reads or writes `[]llm.ToolCall` batches before dispatch).
- `internal/agent/llm_agent.go` sits at 556/600 and `llm_agent_round.go` at 70/600 — comfortable headroom for plans 45-05 through 45-08.
- `budget_dedup.go`'s cross-round loop guard (D-18) is unmodified and remains the correct owner of cross-round duplicate detection; this plan's two functions are explicitly documented as non-overlapping with it.

---
*Phase: 45-harness-correctness*
*Completed: 2026-08-13*

## Self-Check: PASSED

- FOUND: internal/agent/llm_agent_call_dedup.go
- FOUND: internal/agent/llm_agent_call_dedup_test.go
- FOUND: internal/agent/llm_agent_round.go
- FOUND: .planning/phases/45-harness-correctness/45-04-SUMMARY.md
- FOUND: 660b9bd73 (Task 1 RED commit)
- FOUND: 1432c7d9d (Task 1 GREEN commit)
- FOUND: fd0d6a40f (Task 2 RED commit)
- FOUND: f5c79601b (Task 2 GREEN commit)
- FOUND: bf352b7bb (Task 3 wiring/split/fixture-fix commit)
- FOUND: b96a35e32 (this SUMMARY.md commit)
