---
phase: 02-agent-cornerstone
fixed_at: 2026-05-30T00:00:00Z
review_path: .planning/phases/02-agent-cornerstone/02-REVIEW.md
iteration: 2
fix_scope: all
findings_in_scope: 9
fixed: 9
skipped: 0
status: all_fixed
---

# Phase 2: Code Review Fix Report

**Fixed at:** 2026-05-30T00:00:00Z
**Source review:** .planning/phases/02-agent-cornerstone/02-REVIEW.md
**Iteration:** 2

> Note: this is the SECOND fix cycle on Phase 2. Iteration 1 (the prior
> 02-REVIEW-FIX.md content) addressed the earlier WR-01..WR-06 cycle. This report
> addresses the NEW review (warnings WR-01..WR-05 + info IN-01..IN-04) and replaces
> the prior file. `fix_scope` was "all", so every Warning and Info finding was in
> scope.

**Summary:**
- Findings in scope: 9 (5 Warnings, 4 Info)
- Fixed: 9
- Skipped: 0

**Phase-wide gates after fixes (in the isolated worktree):**
- `go vet ./...` — clean
- `go build ./...` — clean
- `golangci-lint run` (Phase-2 packages) — 0 issues
- Unit tests `internal/agent`, `internal/agent/workflow`, `internal/canonicaljson`, `cmd/aura` — all PASS
- `go test -race` on touched packages — all PASS (w64devkit toolchain via ~/.aura-toolchain.sh)
- `scripts/loop_budget_smoke.sh` — PASS: SC#2 26 lines + terminal `limit_hit=max_steps`; B4 coverage **90.7% >= 85%**

## Fixed Issues

### WR-01: Multi-tool turn with `Escalate=true` yields the escalate Event once per tool call

**Files modified:** `internal/agent/workflow/loop.go`, `internal/agent/workflow/loop_test.go`
**Commit:** 5388b745
**Applied fix:** `scopeToToolCall` now takes a `last bool` and clears
`Actions.Escalate` on every per-call scoped copy except the final one, so the
escalate signal rides exactly one step Event instead of being duplicated onto each
scoped copy of a multi-tool turn. `StateDelta`/`ArtifactDelta` still ride every copy
(additive deltas, not one-shot terminal signals); the single-tool fast path is
unchanged. Added a `multiToolEscalateAgent` fixture + `TestLoopAgent_MultiToolEscalate_EscalateRidesExactlyOneStepEvent`,
which fails against the old code (escalate seen 3/3) and passes after the fix (1/3,
on the last call).
**Requires human verification:** Yes — escalate-multiplicity is a wire-contract
semantic the reviewer flagged as load-bearing for Phase-3/Phase-12 consumers. The
choice "escalate rides the LAST per-call Event" matches the reviewer's suggested fix,
but confirm this is the intended terminal-signal placement before Phase 3 builds on it.

### WR-02: Same tool call repeated within one turn corrupts the dedup veto

**Files modified:** `internal/agent/workflow/loop.go`, `internal/agent/workflow/loop_test.go`
**Commit:** 3997ec6c
**Applied fix:** The per-call loop now tracks per-turn fingerprints
(`name + \x00 + canonArgs`) and passes a `dupInTurn` flag to `guardToolCall`. A
within-turn duplicate still consumes a budget step and yields a step Event (WR-05
preserved), but SKIPS both the dedup `BeforeToolCall` gate and the `AfterToolResult`
progress-veto record, so one turn is exactly one dedup observation per distinct
fingerprint. This stops a single `[]ToolCall{A, A}` turn from advancing the period-1
stable-repeat counter as if two turns elapsed (which fired dedup one turn early).
Added a `sameToolTwiceAgent` fixture + a baseline test that pins the window-accurate
termination turn: it fails against the old code ([A,A]/turn trips a turn early, 2
step Events) and passes after the fix (same 3rd turn, 4 step Events).
**Requires human verification:** Yes — dedup-accounting semantics are
correctness-critical for the loop guard. The "one observation per distinct
fingerprint per turn" model matches the reviewer's suggested approach; confirm it
before Phase 3's LlmAgent produces real parallel-identical tool calls.

### WR-03: `Budget` wallclock deadline anchored to real `time.Now()`, bypassing the injectable clock

**Files modified:** `internal/agent/budget.go`, `internal/agent/budget_test.go`
**Commit:** db14b7a0
**Applied fix:** Added `BudgetOptions.Now func() time.Time` (defaults to `time.Now`)
and resolved it FIRST inside `NewBudget`, then anchored
`deadlineWallclock: now().Add(wallclock)` and stored the same `now` for the
`ConsumeStep` gate. Anchor and comparison now share one time source. The production
path (`Now == nil`) is byte-for-byte unchanged. Added two constructor-driven tests:
one drives wallclock end-to-end through an injected clock (deadline anchored at
injectedNow+wallclock, step trips at the right moment), one asserts the default path
still anchors a future deadline off `time.Now`.
**Requires human verification:** Low — the change is a pure additive option with a
test pinning the default path; behavior on the production path is identical.

### WR-04: `terminalEvent`/dry-run emit an all-zero `SpanID`, undocumented as a deferral

**Files modified:** `internal/agent/event.go`, `internal/agent/agent.go`, `cmd/aura/agent.go`
**Commit:** 03a4fcaf
**Applied fix:** Chose the reviewer's DOCUMENTATION option over minting real spans.
The SPEC scopes OTel span minting to the future OTel-integration slice (Phase 2 ships
the OTel-compatible SHAPE without the dep), so populating `crypto/rand` SpanIDs +
`ParentSpanID` chaining would be a cross-cutting behavior change across every
orchestrator's child-IC construction — out of scope for a review fix and a risk to
the goleak / byte-stable / SC#2 contracts. Added explicit deferral notes at the three
cited sites (event.go package docs, `InvocationContext.SpanID`/`ParentSpanID` fields,
the dry-run IC build) so the constant `"span_id":"0000000000000000"` reads as
documented intent, not a defect.
**Rationale for documentation over minting:** the prompt and the reviewer both
identify this as a possible accepted Phase-2 deferral; minting now is the riskier,
out-of-scope option.

### WR-05: SC#2 smoke gate aborts before its own diagnostic on empty dry-run output

**Files modified:** `scripts/loop_budget_smoke.sh`
**Commit:** 1671e464
**Applied fix:** Guarded the `LINES` count capture with `|| true` (matching the
existing `PROFILE_ROWS` capture at line ~60) and defaulted `${LINES:-0}` in the
comparison and diagnostic. Under `set -euo pipefail`, `grep -c .` exits non-zero on
zero matches; without the guard an empty `$OUT` (the exact NO-SKIP-AS-GREEN failure
the gate exists to catch) aborted the pipeline before the hand-written
"expected exactly 26 Event lines" diagnostic could print. Verified with `bash -n`
(syntax) and an isolated `set -euo pipefail` repro showing the empty case now yields
`LINES=0` and reaches the diagnostic. The full smoke script still passes
(26 lines, coverage 90.7%).

### IN-01: `softCap` shadows the builtin `cap`

**Files modified:** `internal/agent/budget.go`
**Commit:** 6c7e9194
**Applied fix:** Renamed the local `cap` to `share` in `softCap()` (also names the
value's role — a per-branch fair share — more precisely). No behavior change.

### IN-02: `uintToString` reimplements `strconv.FormatUint`

**Files modified:** `internal/agent/workflow/loop.go`
**Commit:** 061ce804
**Applied fix:** `iterLabel` now calls `strconv.FormatUint(uint64(i), 10)`; the
hand-rolled `uintToString` formatter is removed and `strconv` is imported. The named
`iterLabel` helper is kept because it documents the `.iter-<N>` Branch-segment
semantics (D-15). No behavior change.

### IN-03: `isPingPong` carries a redundant always-true conjunct

**Files modified:** `internal/agent/budget_dedup.go`
**Commit:** 0d56213c
**Applied fix:** Simplified `return a == fp && a2 == fp && a == a2 && b != fp` to
`return a == fp && a2 == fp && b != fp` (the `a == a2` term is necessarily true when
`a == fp` and `a2 == fp`). Equivalent predicate, less noise on a correctness-critical
detector. No behavior change.

### IN-04: `Budget.Child` soft cap is a spawn-time snapshot, making sibling shares timing-dependent

**Files modified:** `internal/agent/budget.go`
**Commit:** b1657052
**Applied fix:** Chose the reviewer's DOCUMENTATION option (over capturing
`remaining` once before the fan-out loop, which would touch the ParallelAgent spawn
path and alter the documented passive-advisory timing semantics — out of narrow
scope). Documented in the `Child` doc comment that the soft cap is a spawn-time
snapshot (timing-dependent by design, consistent with the passive non-terminal D-12
framing) and noted the equal-share workaround for a caller that wants it. No behavior
change.

## Skipped Issues

None — all 9 in-scope findings were fixed.

## Notes on verification limits

- WR-01 and WR-02 are logic fixes. Both are covered by new fixtures + tests that
  fail against the pre-fix code and pass after, but the SEMANTIC choices (escalate
  rides the last per-call Event; one dedup observation per distinct fingerprint per
  turn) are wire-contract decisions Phase 3 builds on. They are flagged
  "requires human verification" above.
- All `go test -race` runs used the w64devkit toolchain (CC pre-set via
  `go env CC`, PATH-fronted by `~/.aura-toolchain.sh`) and passed on this host — no
  race tier was skipped.

---

_Fixed: 2026-05-30T00:00:00Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 2_
