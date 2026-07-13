---
phase: 42-llm-conversation-compaction
plan: 05
subsystem: conversations
tags: [go, compaction, overflow, hierarchical-rebase, canary]
requires:
  - phase: 42-04
    provides: typed content parts and durable artifact projection
provides:
  - pure atomic L2.4-before-L2.5 budget decision and closed waiver gate
  - one-attempt one-reconstruction non-destructive overflow execution
  - generation-five and drift-triggered canonical hierarchical rebase
  - deterministic recovery-drill-gated canary cohorts
affects: [42-06, 42-07, compaction-rollout, conversation-runtime]
tech-stack:
  added: []
  patterns: [pure budget decision, closed waiver vocabulary, bounded overflow, immutable LKG, stable cohort hashing]
key-files:
  created: [internal/conversations/compaction_overflow_test.go, internal/conversations/compaction_rebase.go, internal/conversations/compaction_rebase_test.go, internal/config/config_compaction.go, internal/config/config_compaction_test.go]
  modified: [internal/conversations/compaction_budget.go, internal/runner/runner_compact.go, internal/runner/runner_compact_test.go]
key-decisions:
  - "L2.5 is representable only after one of five explicit L2.4 waivers; invalid capacity yields no candidate."
  - "Overflow returns an independent original projection on every timeout/failure and never activates mid-stream."
  - "Generation five or any quality threshold breach rebases complete canonical units in chunks at most 60% of summarizer capacity."
  - "Failed rebase preserves the active LKG, quarantines the outcome, and disables automatic generations."
patterns-established:
  - "Pure decision first: budget calculation cannot persist, infer, or emit rot events."
  - "Stable rollout: tenant plus conversation hashing selects only 1/5/20/50 percent cohorts after a recovery drill."
requirements-completed: [IC-03, IC-07, IC-09, IC-11, IC-13, IC-14]
coverage:
  - id: D1
    description: Atomic semantic-before-emergency ladder with hysteresis and exact waiver vocabulary
    requirement: IC-03
    verification:
      - kind: unit
        ref: "go test -race ./internal/conversations ./internal/runner -run 'BudgetDecision|L24BeforeL25|Overflow|Hysteresis|Waiver' -count=1"
        status: pass
    human_judgment: false
  - id: D2
    description: Bounded overflow with one attempt, one reconstruction, no mid-stream activation, and original-state recovery
    requirement: IC-09
    verification:
      - kind: unit
        ref: "internal/runner/runner_compact_test.go#TestOverflowOneAttemptOneReconstruction"
        status: pass
    human_judgment: false
  - id: D3
    description: Recursive generation and hierarchical canonical rebase with drift thresholds and LKG preservation
    requirement: IC-11
    verification:
      - kind: unit
        ref: "internal/conversations/compaction_rebase_test.go#TestRebaseFailurePreservesLKGAndDisablesAutoGenerations"
        status: pass
    human_judgment: false
  - id: D4
    description: Deterministic disabled/shadow/canary/enabled rollout primitives gated on recovery drills
    requirement: IC-13
    verification:
      - kind: unit
        ref: "internal/config/config_compaction_test.go#TestCompactionConfigRejectsIllegalOrIncompatibleActivation"
        status: pass
    human_judgment: false
duration: 23min
completed: 2026-07-13
status: complete
---

# Phase 42 Plan 05: Atomic Ladder, Bounded Overflow, and Canonical Rebase Summary

**Pure semantic-first budget decisions, single-shot overflow recovery, drift-bounded hierarchical rebase, and deterministic recovery-gated canary controls**

## Performance

- **Duration:** 23 min
- **Started:** 2026-07-13T14:40:00+02:00
- **Completed:** 2026-07-13T15:03:35+02:00
- **Tasks:** 2
- **Files modified:** 8

## Accomplishments

- Added an immutable `BudgetDecision` carrying projection, typed L1 edits, semantic and emergency candidates, bounded reasons, and the exact five permitted L2.4 waivers.
- Added a non-destructive overflow executor that refuses mid-stream activation and performs at most one compaction plus one reconstruction under one deadline.
- Added canonical-unit hierarchical rebase with frozen model/prompt/probe/scorer versions, 60%-capacity chunks, generation/depth four boundaries, and exact quality gates.
- Added deterministic rollout configuration for disabled, shadow, 1/5/20/50 percent canaries, and full enablement, with activation requiring a successful recovery drill.

## Task Commits

1. **Task 1: Ship pure L2.4 decision seam atomically with L2.5 and overflow gates** - `24da73116`
2. **Task 2: Add recursive generations, canonical hierarchical rebase, and deterministic canary controls** - `46c1907f1`

## Files Created/Modified

- `internal/conversations/compaction_budget.go` - Pure semantic/emergency decision and waiver gate.
- `internal/conversations/compaction_overflow_test.go` - Ladder, waiver, hysteresis, and invalid-capacity proofs.
- `internal/runner/runner_compact.go` - One-shot overflow attempt/reconstruction boundary.
- `internal/runner/runner_compact_test.go` - Overflow success, mid-stream, and recovery tests.
- `internal/conversations/compaction_rebase.go` - Frozen hierarchical rebase and LKG failure state.
- `internal/conversations/compaction_rebase_test.go` - Generations, drift, chunking, no-duplicate, and recovery proofs.
- `internal/config/config_compaction.go` - Strict rollout stages and stable cohort selection.
- `internal/config/config_compaction_test.go` - Default, deterministic sampling, and incompatibility tests.

## Decisions Made

- Kept mutation outside the pure budget seam; coordinators consume candidates and own persistence/inference.
- Used an explicit waiver enum rather than free-form provider errors so emergency degradation cannot grow an unsafe vocabulary.
- Kept semantic units indivisible during rebase even when that means rejecting an oversized unit.
- Required the recovery drill bit for canary and enabled modes; parsing stage syntax alone never authorizes activation.

## Deviations from Plan

None - plan executed within the specified files and contracts. Existing `LoadManagedHistory` callers remain compatible while the new pure decision seam is available to the coordinator.

## Issues Encountered

- Slow/async ambiguity: the first Task 1 commit tool invocation detached its git/hook process after a short tool timeout. `HEAD`, live PIDs, parent PID, index, and lock state were inspected; only the orphaned PIDs were terminated before a normal hook-enabled retry.
- Gate retry: Task 1 lint first rejected two undocumented exported constant blocks and one unused test helper, then required per-symbol exported comments. Each rejection was reported and remediated; no hook was bypassed.
- Native Windows cannot run `-race` without CGO. Both exact race suites passed through WSL/CGO.

## Known Stubs

None. Rollout remains disabled by default by design; canary/full activation additionally requires an explicit successful recovery drill.

## User Setup Required

None.

## Next Phase Readiness

- Plan 42-06 can consume the semantic-first decision and immutable rebase contracts without changing canonical turns.
- Canary primitives exist, but enabled activation remains deliberately gated pending the later evaluation and rollout plans.

## Self-Check: PASSED

- Task commits `24da73116` and `46c1907f1` exist; all eight key files exist.
- Exact WSL/CGO race suites passed for ladder/overflow and the full compaction/rebase/canary regex.
- No tracked files were deleted; unrelated graph dirt remains unstaged.

---
*Phase: 42-llm-conversation-compaction*
*Completed: 2026-07-13*
