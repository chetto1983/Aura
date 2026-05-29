---
phase: 02-agent-cornerstone
plan: 03
subsystem: agent-runtime
tags: [go, budget, atomic, toctou, dedup, sha256, canonical-json, fail-fast-env, wallclock, property-testing, dos-prevention]

# Dependency graph
requires:
  - phase: 02-01
    provides: internal/canonicaljson.Marshal (D-08 serializer the dedup fingerprint consumes), pgregory.net/rapid v1.3.0
  - phase: 02-02
    provides: internal/agent.InvocationContext (WithSubAgent ring-sharing semantics), the TEMP Budget struct{} stub (this plan deletes it), ErrBudgetExhausted sentinel
provides:
  - "internal/agent.Budget — shared *atomic.Int32 step counter bounding the whole tree (D-10); TOCTOU-safe ConsumeStep decrement-then-check-then-restore (D-11)"
  - "internal/agent.NewBudgetFromEnv — fail-fast on malformed AURA_LOOP_* (D-06), never silently defaulting"
  - "Budget.Child(fanout) — shares the atomic counter, forks a DISTINCT dedup ring, applies a passive per-branch soft cap (D-09/D-12)"
  - "Budget.BeforeToolCall/AfterToolResult — two-tier dedup fingerprint (name+caller-canonical-args primary, result-as-veto, D-18/A2); caller-canonicalizes contract (B2)"
  - "Budget.WithDeadline — context.WithDeadline threading for end-to-end wallclock cancellation (D-13); Budget.SoftCapExceeded / NodeTimeout helpers"
affects: [02-05 LoopAgent (consumes ConsumeStep + BeforeToolCall/AfterToolResult), 02-06 ParallelAgent (consumes Child + SoftCapExceeded), 02-07 dry-run, 09 swarm, 04 conversation hash reuse of canonicaljson]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Single *atomic.Int32 shared BY POINTER across the agent tree — depth-3 fan-3 consumes ≤ max_steps total, not max_steps³ (avoids the fresh-per-child depth³ trap)"
    - "ConsumeStep decrement-then-check-then-restore: N concurrent goroutines that overshoot below zero each add 1 back; proven by an exactly-one-winner TOCTOU race test"
    - "Wallclock via an injectable now func() time.Time field (W8) — NOT testing/synctest, which spawns background goroutines that would trip goleak.VerifyTestMain (SC#1)"
    - "Fail-fast env parsing with verbatim errMalformed consts (D-06) — diverges from internal/config's silent-absorb int helper"
    - "Two-tier dedup: primary fingerprint = sha256(name+canonical_args) BEFORE re-execute; result preview tracked with a consecutive-unchanged repeat counter so a changing result resets it (fail-SAFE progress veto, never fail-open)"
    - "Caller-canonicalizes contract (B2): Before/AfterToolResult accept pre-canonical []byte; no internal canonicaljson call"

key-files:
  created:
    - internal/agent/budget.go
    - internal/agent/budget_dedup.go
    - internal/agent/budget_test.go
    - internal/agent/budget_dedup_test.go
  modified:
    - internal/agent/agent.go

key-decisions:
  - "Renamed budget defaults to defaultBudgetMaxSteps/defaultBudgetWallclockSec to avoid colliding with loop.go's defaultMaxSteps=8 (loop.go is deleted in Plan 02-07, not here)"
  - "Result-veto uses a per-fingerprint consecutive-unchanged repeat counter (resultTrack), not a single stale-marker — the single-marker design failed the changing-result test (it re-armed dedup one step after a progress reset). repeats+2>=window aligns the veto with period-1's Nth-call threshold"
  - "Soft cap is PASSIVE (D-12): ConsumeStep only ever returns hard reasons (max_steps|wallclock); the per-branch fair-share advisory is surfaced via the non-terminal SoftCapExceeded helper, leaving the hard atomic bound authoritative. No DOVA active rebalancing (deferred)"
  - "AURA_LOOP_BRANCH_SOFT_FRACTION default = 1.0, so the default softCap = ceil(remaining/fanout) — a branch's even slice of the shared pool; fail-fast rejects values outside (0,1]"
  - "dedupRing capacity = max(window,4) so a period-2 ping-pong (A-B-A-B) is observable while WINDOW=3 governs period-1 (D-20)"

patterns-established:
  - "Injectable-clock (now func() time.Time) for deterministic time-dependent unit tests without goleak-hostile background timers (W8)"
  - "resultTrack consecutive-unchanged counter as the fail-safe dedup progress veto"

requirements-completed: []  # INFRA-03 stays OPEN — it closes at 02-06 when workflow agents land

# Metrics
duration: ~9min
completed: 2026-05-29
---

# Phase 2 Plan 03: Budget Tree (shared atomic + TOCTOU + two-tier dedup) Summary

**Implemented the cornerstone resource-exhaustion control: a single `*atomic.Int32` shared by pointer across the whole agent tree (D-10), a TOCTOU-safe `ConsumeStep` (decrement-then-check-then-restore, D-11), fail-fast `NewBudgetFromEnv` (D-06), `Budget.Child` that shares the counter but forks a distinct dedup ring with a passive per-branch soft cap (D-09/D-12), wallclock via an injectable fake clock + `context.WithDeadline` (W8/D-13), and the two-tier dedup fingerprint (name+caller-canonical-args primary, result-as-progress-veto, D-18/A2) — split across `budget.go` + `budget_dedup.go` so neither file nears the 600-LOC cap.**

## Performance
- **Duration:** ~9 min
- **Started:** 2026-05-29T21:47Z
- **Completed:** 2026-05-29T21:56Z
- **Tasks:** 2
- **Files:** 4 created, 1 modified (agent.go stub removal)

## Accomplishments
- **Shared-atomic budget (D-10):** `Budget.steps *atomic.Int32` shared BY POINTER; `Child(fanout)` copies the pointer, never forks it. `TestBudget_Child_SharesStepsCounter` proves a parent consuming 5 leaves the child `Remaining()==20`, and a child decrement is visible to the parent.
- **TOCTOU-safe ConsumeStep (D-11):** decrement-then-check-then-restore. `TestBudget_ConsumeStep_AtomicDecrement_NoRace` (10×100 = exactly 1000 successful decrements under `-race`) and `TestBudget_ConsumeStep_ExactlyOneWinner_When_CounterIsOne` (100 goroutines vs counter 1 → exactly one winner) both pass under the race detector.
- **Fail-fast env (D-06):** `NewBudgetFromEnv` returns a verbatim `errMalformed(key,val)` for any set-but-unparseable `AURA_LOOP_*` value; the silent-absorb pattern (`envIntDefault`) is deliberately NOT used. Tests assert the literal error string and that unset vars fall back to documented defaults.
- **Wallclock via injectable clock (W8/D-13):** `now func() time.Time` defaulting to `time.Now`; `TestBudget_Wallclock_TerminatesAtDeadline` drives the deadline with a fake clock — no `testing/synctest`, no goleak-tripping background goroutines. `WithDeadline` threads `context.WithDeadline` for end-to-end cancellation; optional `AURA_LOOP_NODE_TIMEOUT_SEC` surfaced via `NodeTimeout()`.
- **Passive soft cap (D-12):** `Child` computes `softCap = ceil(remaining*frac/fanout)`; `ConsumeStep` stays hard-only (`max_steps`/`wallclock`), and the non-terminal fairness signal is `SoftCapExceeded()`. The hard atomic bound stays authoritative.
- **Two-tier dedup (D-18/A2):** primary fingerprint `sha256(name + canonical_args)` checked in `BeforeToolCall` BEFORE re-execute; period-1 (A-A-A, window=3) and period-2 ping-pong (A-B-A-B) detection; `AfterToolResult` records a bounded result preview with a consecutive-unchanged repeat counter so a CHANGING result resets it (fail-SAFE progress veto). `AURA_LOOP_DEDUP_EXEMPT_TOOLS` allowlist honored.
- **Caller-canonicalizes contract (B2):** `BeforeToolCall`/`AfterToolResult` accept pre-canonical `[]byte`; the canonical-hash order-independence test runs `internal/canonicaljson.Marshal` on both arg orderings (exercising the caller side) and asserts equal fingerprints. No internal canonicaljson call.
- **Property test (D-21):** `TestBudget_Property_TotalConsumedNeverExceedsMax` (rapid) generates random concurrent consume sequences and asserts total consumed never exceeds the initial max and `Remaining()` never goes negative.
- Plan-02 `type Budget struct{}` TEMP stub deleted from agent.go; `go build ./...` stays green throughout.

## Task Commits
1. **Task 1: shared-atomic Budget + fail-fast env + TOCTOU ConsumeStep + Child + wallclock + soft cap; agent.go stub removed** — `20c9c167` (feat)
2. **Task 2: two-tier dedup veto-tracking + canonical-hash + period-1/2 + exempt allowlist + property test** — `45df5796` (feat)

_Note: the dedup-ring scaffolding (`newDedupRing`, `fingerprint`, `parseExemptTools`) landed in the Task-1 commit because `budget.go`'s `Child`/struct fields depend on it for a buildable package; the two-phase API body (`BeforeToolCall`/`AfterToolResult` logic) plus all dedup tests are the Task-2 commit. Each commit builds and tests green._

## Files Created/Modified
- `internal/agent/budget.go` (243 LOC) — `Budget` struct, `NewBudgetFromEnv` (fail-fast), `envIntFailFast`/`envFloatFailFast`, `ConsumeStep`, `Remaining`, `SetMaxSteps`, `SoftCapExceeded`, `Child`, `softCap`, `WithDeadline`, `NodeTimeout`, AURA_LOOP_* env consts + defaults.
- `internal/agent/budget_dedup.go` (192 LOC) — `dedupRing` + `resultTrack`, `newDedupRing`, `ringCapacity`, `computeFingerprint`, `push`/`countConsecutive`/`isPingPong`, `BeforeToolCall`/`AfterToolResult`, `parseExemptTools`.
- `internal/agent/budget_test.go` (279 LOC) — atomic/TOCTOU race, child-shares-counter, fake-clock wallclock, fail-fast env (verbatim string), soft-cap passive, WithDeadline/NodeTimeout/Remaining-clamp/SoftCap-root edge coverage.
- `internal/agent/budget_dedup_test.go` (202 LOC) — canonical-hash order-independence, distinct-args, period-1, period-2 ping-pong, result-changed-suppresses-dedup, distinct child ring, exempt allowlist, ring-capacity, rapid property test.
- `internal/agent/agent.go` (modified) — deleted the Plan-02 TEMP `type Budget struct{}` stub (REQUIRED handoff).

## Verify Command Outputs (evidence)

**Task 1 (`go test -race ... -run 'TestBudget_(ConsumeStep|Child|Wallclock)' && go build && go vet`):**
```
ok  github.com/chetto1983/aura/internal/agent  1.302s   # RACE OK
go build ./internal/agent/   # OK
go vet ./internal/agent/     # OK
grep -c 'type Budget struct{}' internal/agent/agent.go   # 0 (stub removed)
grep 'envIntDefault' internal/agent/budget.go            # NOTHING (silent-absorb anti-pattern not copied)
grep 'now func() time.Time' internal/agent/budget.go     # present (W8 injectable clock)
```

**Task 2 (`go test -race ./internal/agent/ -run TestBudget && go vet`):**
```
=== all dedup tests PASS:
  TestBudget_BeforeToolCall_CanonicalHashOrderIndependent
  TestBudget_Dedup_Period1_TerminatesOnThreeRepeats
  TestBudget_Dedup_Period2_PingPong
  TestBudget_Dedup_ResultChanged_SuppressesDedup        (progress veto, D-18)
  TestBudget_Child_DistinctDedupRing
  TestBudget_Dedup_ExemptTool_NeverDedups               (D-19)
  TestBudget_Property_TotalConsumedNeverExceedsMax       [rapid] OK, passed 100 tests
ok  github.com/chetto1983/aura/internal/agent  1.407s   # FULL PACKAGE RACE OK
```

**Final gate (whole package):**
```
go test -race -count=1 ./internal/agent/   # ok, 1.393s
go vet ./internal/agent/                   # clean
go build ./...                             # clean (stub removed, package compiles)
gofmt -l internal/agent/*.go               # (empty — clean)
golangci-lint run ./internal/agent/        # 0 issues
bash scripts/check-file-size.sh            # all Go files within the 600-LOC cap (budget.go 243, budget_dedup.go 192)
as --version                               # GNU Binutils 2.46 (race toolchain OK, no BASH_ENV prefix needed)
```

**Coverage (budget*.go surface):** `ConsumeStep` 100%, `Child` 100%, `BeforeToolCall` 100%, `AfterToolResult` 92.3%, `computeFingerprint`/`push`/`countConsecutive`/`isPingPong`/`ringCapacity`/`parseExemptTools` 100%, `NewBudgetFromEnv` 83.3%. Package total 71.5% includes legacy `loop.go` (0%, deleted in 02-07) and event/agent files — the budget surface this plan owns is near-fully covered. The 85% phase floor is a phase-close gate measured after loop.go is removed and workflow tests land (02-05/06/07).

## Decisions Made
- **resultTrack consecutive-unchanged counter (bug fix during TDD).** The initial single-slot "stale marker" veto design FAILED `TestBudget_Dedup_ResultChanged_SuppressesDedup`: after a result change cleared the marker, the very next `AfterToolResult` re-armed it and `BeforeToolCall` deduped one step later. Replaced with a per-fingerprint `{hash, repeats}` track: a changing result resets `repeats` to 0; dedup requires `repeats+2 >= window` (aligning the result-stability requirement with period-1's Nth-consecutive-call threshold). This is a genuine fail-safe-vs-fail-open correctness fix, exactly the A2 reversal the dedup design exists to guarantee.
- **Renamed defaults to avoid the loop.go collision.** `loop.go` (deleted in 02-07, out of this plan's scope) declares `const defaultMaxSteps = 8`. To keep `go build` green without touching out-of-scope loop.go, the budget defaults are `defaultBudgetMaxSteps`/`defaultBudgetWallclockSec`.
- **`#nosec G101` on `envDedupWindow`.** gosec flags the env-var-name string const as a potential hardcoded credential — a clear false positive (it is a variable NAME, not a secret). Narrow inline `//nolint:gosec` with justification.

## Deviations from Plan

### Required handoff (expected, pre-authorized)

**1. [Rule 3 - Blocking] Deleted the Plan-02 TEMP `type Budget struct{}` stub from agent.go**
- **Found during:** Task 1 (the real Budget must replace the stub for the package to define one Budget type)
- **Issue:** Plan 02-02 left `type Budget struct{}` in agent.go (marked `// TEMP:`) so the package compiled before this plan existed. agent.go is not in this plan's `files_modified`.
- **Fix:** Deleted the stub block; the real Budget now lives in budget.go (same package). `go build ./...` stays green.
- **Files modified:** internal/agent/agent.go
- **Commit:** 20c9c167
- **Authorization:** Explicitly mandated by the plan's `<sequential_execution>` handoff note and Task 1 `<action>`.

### Auto-fixed during TDD

**2. [Rule 1 - Bug] Dedup progress-veto re-armed too eagerly**
- **Found during:** Task 2 (`TestBudget_Dedup_ResultChanged_SuppressesDedup` failed: call 4 deduped despite continuously changing results)
- **Issue:** Single-slot stale-marker veto could not distinguish "result has been stable across the window" from "result changed then was recorded once" — it re-armed dedup one step after a progress reset, which is fail-open behavior (the exact thing D-18/A2 forbids).
- **Fix:** Introduced `resultTrack{hash, repeats}`; AfterToolResult increments `repeats` only on an identical result and resets to 0 on any change; BeforeToolCall requires `repeats+2 >= window` in addition to the fingerprint period-1/period-2 condition.
- **Files modified:** internal/agent/budget_dedup.go
- **Commit:** 45df5796

**3. [Rule 1 - Bug avoided] Const collision and gosec false positive**
- Renamed budget default consts to avoid colliding with loop.go's `defaultMaxSteps` (build break); added a narrow `//nolint:gosec` for the G101 false positive on an env-var-name const. Both in commits 20c9c167 / 20c9c167.

No other functional deviation: every type, field, method, env var, default, and decision matches the plan and D-06/D-09/D-10/D-11/D-12/D-13/D-18/D-19/D-20/A2/B2/W8.

## Known Stubs
None. The Plan-02 Budget stub was removed (required handoff); the real Budget is fully implemented. `BeforeToolCall`/`AfterToolResult` are exported methods consumed by the LoopAgent caller in Plan 02-05 (not stubs — the callee side of the locked B2 contract is complete and tested).

## Threat Model Notes (mitigations implemented)
- **T-02-01 (DoS — ConsumeStep over-spend / depth³):** mitigated — decrement-then-check-then-restore (D-11) + single shared `*atomic.Int32` by pointer (D-10). Proven by `_ExactlyOneWinner_` + `_Child_SharesStepsCounter` + the rapid total-consumed property. The depth-3 tree assertion (SC#3) lands with ParallelAgent in 02-06.
- **T-02-02 (DoS — fail-open dedup):** mitigated — two-tier fingerprint fires on name+args BEFORE re-execute (D-18/A2); the result preview is a progress veto with a consecutive-unchanged counter so volatile results fail SAFE. Proven by the period-1/period-2 + result-changed-suppresses tests.
- **T-02-03 (DoS — malformed AURA_LOOP_* mid-loop):** mitigated — `NewBudgetFromEnv` fail-fast at boot with a verbatim error (D-06). Proven by the malformed-max-steps/wallclock/soft-fraction fail-fast tests.
- **T-02-08 (DoS — in-flight call overruns wallclock):** mitigated — `WithDeadline` threads `context.WithDeadline` for end-to-end cancellation (D-13) + optional `AURA_LOOP_NODE_TIMEOUT_SEC`. Wallclock check runs first in ConsumeStep.

No new threat surface introduced.

## Next Phase Readiness
- `Budget`, `ConsumeStep`, `Child`, `BeforeToolCall`/`AfterToolResult`, `SoftCapExceeded`, `WithDeadline`, `NodeTimeout` are ready for 02-05 (LoopAgent: ConsumeStep loop + dedup pre-check/result-veto + budget-exhausted Event) and 02-06 (ParallelAgent: Child + SoftCapExceeded scheduling + the SC#3 depth-3 shared-counter test).
- The caller-canonicalizes contract (B2) is locked and documented in both method doc comments for the 02-05 caller.
- `INFRA-03` intentionally left OPEN — it closes at 02-06 when the workflow agents land.
- No blockers.

## Self-Check: PASSED
- FOUND: internal/agent/budget.go
- FOUND: internal/agent/budget_dedup.go
- FOUND: internal/agent/budget_test.go
- FOUND: internal/agent/budget_dedup_test.go
- FOUND: .planning/phases/02-agent-cornerstone/02-03-SUMMARY.md
- FOUND commit 20c9c167 (Task 1)
- FOUND commit 45df5796 (Task 2)
- VERIFIED: `type Budget struct{}` absent from internal/agent/agent.go (count 0)

---
*Phase: 02-agent-cornerstone*
*Completed: 2026-05-29*
