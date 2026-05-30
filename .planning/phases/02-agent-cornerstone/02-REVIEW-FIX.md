---
phase: 02-agent-cornerstone
fixed_at: 2026-05-30T00:00:00Z
review_path: .planning/phases/02-agent-cornerstone/02-REVIEW.md
iteration: 1
fix_scope: critical_warning
findings_in_scope: 6
fixed: 6
skipped: 0
status: all_fixed
---

# Phase 2: Code Review Fix Report

**Fixed at:** 2026-05-30
**Source review:** .planning/phases/02-agent-cornerstone/02-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope (fix_scope=critical_warning): 6 (WR-01..WR-06)
- Fixed: 6
- Skipped: 0
- Info findings (IN-01..IN-06): intentionally NOT touched — out of scope for this
  `critical_warning` pass; left for a later `--all` run.

All work was done in an isolated git worktree on branch `gsd-reviewfix/02-*`,
fast-forwarded back onto `tabula-rasa`. Each fix is one atomic commit. The report
itself is NOT committed (the orchestrator owns that).

## Validation (final, full Phase-2 surface)

- `go vet ./...` — clean
- `go build ./...` — clean
- `go test -count=1` per package:
  - `internal/agent` 91.6%
  - `internal/agent/workflow` 91.5%
  - `internal/agent/agenttest` 70.7% (test-helper pkg; not in the phase floor surface)
  - `internal/canonicaljson` 85.2%
  - `cmd/aura` 19.3% raw (includes the excluded Slice-0.5/0.7 db/neo4j/main subcommands)
- `go test -race` on `internal/agent`, `internal/agent/workflow`, `internal/agent/agenttest` — all pass
- `scripts/loop_budget_smoke.sh` end-to-end — PASS: SC#2 = 26 lines, **Phase-2 filtered coverage = 90.4% >= 85%** floor
- `golangci-lint run ./internal/agent/... ./cmd/aura/` — **0 issues**

The Phase-2 combined coverage (90.4%, via the smoke gate's filtered profile) is the
load-bearing number and remains above the CLAUDE.md 85% floor.

## Fixed Issues

### WR-01 / WR-02: Event wire contract (zero message_id leak + span_id encoding)

**Files modified:** `internal/agent/event.go`, `internal/agent/event_test.go`
**Commit:** `286cebc6`
**Status:** fixed
**Applied fix:** Both findings live in the same `eventWire` projection struct and the
shared Marshal/Unmarshal path, so they could not be split into two clean commits and
were fixed together.

- **WR-01:** `MessageID uuid.UUID` is `[16]byte`, so `json:"...,omitempty"` is a no-op
  for arrays — every Event leaked `"message_id":"000...000"`. `eventWire` now projects
  it as `*uuid.UUID` gated by `uuid.Nil` (`uuidPtrIfSet`), so omitempty actually fires;
  `UnmarshalJSON` maps the pointer back to the value field. The same array-vs-omitempty
  audit confirmed `RequestID` has NO omitempty (always present by design — correct) and
  `ThreadID` is a `string` (omitempty already works). Regression assertions added:
  `TestEvent_NilLLMResponse_OmitsObject` now asserts `message_id` absent for a zero
  Event, plus a new `TestEvent_MessageID_PresentWhenSet`.
- **WR-02:** `SpanID [8]byte` / `ParentSpanID *[8]byte` serialized as verbose JSON number
  arrays, not the base64 the old test comment wrongly claimed. They are now encoded as
  lower-hex strings in `eventWire` (`hex.EncodeToString` / `hexPtr`) and decoded back
  into the fixed arrays in `UnmarshalJSON` (`decodeSpan` / `decodeSpanPtr`), matching the
  OTel/W3C-idiomatic form (D-16). The stale comment at the former line 133 is corrected,
  and the test now asserts the actual hex form (`"span_id":"0102030405060708"`) plus a
  hex round-trip — replacing the meaningless `len != 0` check.

Round-trip byte-identity (including the `rapid` property test) stays green.

### WR-03: Two-phase dedup formula decoupled from window; window != 3 now covered

**Files modified:** `internal/agent/budget_dedup.go`, `internal/agent/budget_dedup_test.go`
**Commit:** `5e0b8493`
**Status:** fixed: requires human verification (logic/semantics change — see note)
**Applied fix:** The single `resultStable := seen && repeats+2 >= window` gate was applied
to BOTH tiers. After reasoning through the counter semantics I confirmed the `+2` formula
is provably CORRECT and window-parameterized for **period-1** (it aligns exactly with
`countConsecutive+1 >= window` for every window >= 1), so I pinned it rather than changing
it — `window=3` behaviour is byte-for-byte unchanged (SC#2 safe). The genuine bug was the
**period-2** tier: `isPingPong` is a FIXED period-2 detector (always the last three entries
A,B,A), but gating it on the period-1 window threshold wrongly SUPPRESSED ping-pong for
`window >= 4` (where `repeats(A)==1` but `repeats+2 >= window` is false). Fix: split into
`stableP1 = repeats+2 >= window` (period-1) and `stableP2 = repeats >= 1` (period-2,
window-independent). Added table-driven tests across `window in {1,2,3,4,5}` asserting the
exact termination call index: period-1 at `max(2,window)`, period-2 at call 4 for every
`window >= 2`. `window=1` is documented as a case where period-1 dominates the A,B,A warmup
(correct, not a bug). `isPingPong`'s hard-coded period-2 nature is now documented in the
new `stableP2` comment.

**Human-verification flag:** this changes dedup termination behaviour for non-default
windows. window=3 (the only value SC#2/CLI default uses) is unchanged and fully tested;
the new non-3 behaviour is pinned by tests but should be eyeballed against intended
operator semantics before phase close.

### WR-04: Budget precedence via BudgetOptions, no runtime env mutation

**Files modified:** `internal/agent/budget.go`, `internal/agent/budget_dedup.go`,
`internal/agent/budget_test.go`, `internal/agent/budget_dedup_test.go`,
`cmd/aura/agent.go`, `cmd/aura/agent_test.go`
**Commit:** `d65cc9c2`
**Status:** fixed
**Applied fix:** Replaced the `os.Setenv`/`os.Unsetenv` save/restore dance in
`cmd/aura/agent.go` (`buildBudget`, `overrideEnv`) with an explicit options path.
Added `agent.BudgetOptions` + `agent.NewBudget(opts)`: a nil pointer field falls through
to env then builtin default, a set field overrides both — D-06 precedence resolved in ONE
place with zero global mutation (`resolveInt` helper). `NewBudgetFromEnv` now delegates to
`NewBudget(BudgetOptions{})` so env-only callers are unchanged. Added
`agent.ExemptToolsFromEnv(extra...)` so the CLI composes the operator's
`AURA_LOOP_DEDUP_EXEMPT_TOOLS` allowlist with the dry-run tool without env mutation.
`overrideEnv` deleted. New tests: NewBudget override-wins-over-env-without-mutating-env,
nil-falls-through-to-default, `ExemptToolsFromEnv` merge + empty-env. The pre-existing
operator-exemption CLI test was updated (NOT silenced): it now asserts the process env is
left UNTOUCHED (the old "restored" assertion is meaningless when nothing mutates it) and
that the run still caps on max_steps; its misleading comment was corrected.

### WR-05: One step Event per budgeted tool call; mid-turn step discard fixed

**Files modified:** `internal/agent/workflow/loop.go`, `internal/agent/workflow/loop_test.go`
**Commit:** `d0804604`
**Status:** fixed: requires human verification (semantics decision — see note)
**Applied fix:** Defined the semantics explicitly: each tool call is ONE budgeted step
(D-11, preserved) AND ONE step Event (1:1). `guardToolCall` now yields a per-tool-call
step Event on each successful consume; `scopeToToolCall` narrows a multi-tool Event's
`LLMResponse` to the single call, and returns the ORIGINAL pointer unchanged for a
single-tool turn (so SC#2's 26 lines and byte output are byte-identical). A budget/dedup
trip now replaces only the REMAINING tool calls — the already-consumed first call was
already yielded, fixing the discard bug. `steps_consumed` therefore always equals the
number of yielded step Events. Non-tool Events consume no step and are yielded as-is.
Added `multiToolAgent` fixture (2 calls/turn) and a test with an odd `max_steps=5` that
trips on the 2nd call of a turn, asserting `steps_consumed == len(stepEvents) == 5` and
that each step Event carries exactly one tool call.

**Human-verification flag:** this is a deliberate Event-stream semantics choice (per-tool-call
step Events). It keeps D-11 per-tool-call budgeting and the SC#2 single-tool invariant
intact, but it does change the stream shape for a *future* multi-tool LlmAgent (one Event
per call rather than one per turn). Confirm this matches the intended Phase-3 contract
before relying on it; if the project prefers "one Event == one step" instead, that would be
a D-11 amendment (out of scope for a review fix).

### WR-06: Smoke coverage gate hardened against path-substring and column drift

**Files modified:** `scripts/loop_budget_smoke.sh`
**Commit:** `7070c65e`
**Status:** fixed
**Applied fix:** (1) Anchored each excluded file to a path-SEGMENT boundary
(`/cmd/aura/(db|neo4j|main)\.go:`, `/internal/agent/tools/`) so a future file merely
CONTAINING those substrings (e.g. `cmd/aura/agentmain.go`) is not silently dropped.
(2) Added a guard that the filtered `cover_phase2.out` still has >= 1 statement row,
failing loudly otherwise. (3) Replaced the positional awk `$3` with
`grep -oE '[0-9]+(\.[0-9]+)?%'` and a fail-loud check when no percentage is captured.
The 85.0 floor is unchanged. Verified end-to-end: the script still passes, SC#2 = 26 lines,
gate reports 90.4%.

## Skipped Issues

None in scope.

**Out of scope (intentionally not touched):** IN-01 (`softCap` shadows builtin `cap`),
IN-02 (`chan bool` -> `chan struct{}`), IN-03 (`uintToString` vs `strconv.FormatUint`),
IN-04 (`termination_reason` for dedup stop), IN-05 (unset `Timestamp`),
IN-06 (`RecordingAgent` cross-run accumulation). These are Info-tier and belong to a
later `/gsd-code-review --fix --all` pass.

---

_Fixed: 2026-05-30_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
