---
phase: 02-agent-cornerstone
reviewed: 2026-05-30T00:00:00Z
depth: standard
files_reviewed: 25
files_reviewed_list:
  - cmd/aura/agent.go
  - cmd/aura/agent_test.go
  - internal/agent/agent.go
  - internal/agent/agent_test.go
  - internal/agent/agenttest/mocks.go
  - internal/agent/agenttest/mocks_test.go
  - internal/agent/budget.go
  - internal/agent/budget_dedup.go
  - internal/agent/budget_dedup_test.go
  - internal/agent/budget_test.go
  - internal/agent/errors.go
  - internal/agent/event.go
  - internal/agent/event_test.go
  - internal/agent/workflow/loop.go
  - internal/agent/workflow/loop_test.go
  - internal/agent/workflow/parallel.go
  - internal/agent/workflow/parallel_test.go
  - internal/agent/workflow/sequential.go
  - internal/agent/workflow/sequential_test.go
  - internal/agent/workflow/workflow.go
  - internal/agent/workflow/workflow_contract_test.go
  - internal/agent/workflow/workflow_test.go
  - internal/canonicaljson/canonicaljson.go
  - internal/canonicaljson/canonicaljson_test.go
  - scripts/loop_budget_smoke.sh
findings:
  critical: 0
  warning: 5
  info: 4
  total: 9
status: issues_found
---

# Phase 2: Code Review Report

**Reviewed:** 2026-05-30T00:00:00Z
**Depth:** standard
**Files Reviewed:** 25
**Status:** issues_found

## Summary

Reviewed the Phase-2 agent cornerstone: the `Agent` interface + `InvocationContext`,
the `Event`/`LLMResponse`/`Actions` wire shape, the shared-atomic `Budget` with the
two-tier dedup ring, the three workflow orchestrators (Sequential/Loop/Parallel),
the canonical-JSON serializer, the shared test mocks, the `aura agent dry-run`
subcommand, and the SC#2 smoke gate.

The implementation is high quality and well tested: `go vet`, `go build`, and
`golangci-lint` are all clean across the reviewed packages, and the prior cycle's
fixes (WR-01..WR-06) hold up under re-review. The concurrency design (TOCTOU-safe
`ConsumeStep`, goleak-verified ParallelAgent drain, shared `*atomic.Int32`) is sound
and backed by race + property tests.

No BLOCKER-tier defects were found. The remaining findings are correctness wrinkles
on paths the current workflow agents do not yet exercise but Phase 3's `LlmAgent`
will (multi-tool turns carrying `Escalate`, multiple identical tool calls in one
turn, and within-turn vs cross-turn dedup accounting), plus a few maintainability
items. These should be resolved before Phase 3 builds the real LLM caller on top of
this contract, because Phase 3 is precisely the consumer that produces multi-tool
turns.

## Warnings

### WR-01: Multi-tool turn with `Escalate=true` yields the escalate Event once per tool call

**File:** `internal/agent/workflow/loop.go:97-109`, `internal/agent/workflow/loop.go:173-182`
**Issue:** When an Event carries N>1 tool calls AND `Actions.Escalate=true`,
`guardToolCall` yields `scopeToToolCall(ev, tc)` for each call. `scopeToToolCall`
does `scoped := *ev`, copying `Actions` (including `Escalate=true`) onto every
per-call copy. The consumer therefore observes the escalate Event N times, then the
loop's post-call `if ev != nil && ev.Actions.Escalate { return }` returns. The D-21
contract is "the escalate Event ALWAYS precedes the iterator returning" — it is
silent on multiplicity, but a downstream consumer (Phase 12 AG-UI fan-out, or any
escalate-counting logic) will see duplicate terminal signals for a single logical
turn. Current workflow mocks never set `Escalate` on a multi-tool turn, so no test
catches this; Phase 3's `LlmAgent` is the first producer that can.
**Fix:** Strip or relocate `Escalate` so it rides exactly one step Event (e.g. clear
`Actions.Escalate` on all but the last scoped copy, or yield the escalate as a
separate terminal Event after the per-call loop):
```go
func scopeToToolCall(ev *agent.Event, tc llm.ToolCall, last bool) *agent.Event {
    if ev == nil || ev.LLMResponse == nil || len(ev.LLMResponse.ToolCalls) <= 1 {
        return ev
    }
    scoped := *ev
    resp := *ev.LLMResponse
    resp.ToolCalls = []llm.ToolCall{tc}
    scoped.LLMResponse = &resp
    if !last {
        scoped.Actions.Escalate = false // escalate rides only the final per-call Event
    }
    return &scoped
}
```

### WR-02: Same tool call repeated within one turn shares a single result preview, corrupting the dedup veto

**File:** `internal/agent/workflow/loop.go:97-106`, `internal/agent/workflow/loop.go:152-157`
**Issue:** `preview := resultPreview(ev)` is computed once per Event and passed to
`AfterToolResult` for every tool call in that Event. If one Event carries the SAME
(name, args) tool call twice (a legitimate LLM output — parallel identical calls),
the loop does `BeforeToolCall(A)` / `AfterToolResult(A, preview)` twice in a row with
an identical preview. The second `AfterToolResult` sees `prev.hash == h` and bumps
`repeats`, so a single turn advances the period-1 stable-repeat counter as if two
turns had elapsed. This makes dedup fire one turn earlier than the `dedupWindow`
contract intends for within-turn duplicates. No test covers two identical calls in
one Event (the multi-tool fixture uses distinct names), so this is latent until
Phase 3.
**Fix:** Either fingerprint-dedupe the calls within a turn before accounting, or
treat one Event as one dedup observation per distinct fingerprint. At minimum add a
loop_test fixture emitting `[]ToolCall{A, A}` in one Event and assert the
window-accurate termination index, then adjust `guardToolCall` so within-turn repeats
do not double-count the progress veto.

### WR-03: `Budget` wallclock deadline is anchored to real `time.Now()`, bypassing the injectable clock

**File:** `internal/agent/budget.go:143`
**Issue:** `NewBudget` computes `deadlineWallclock: time.Now().Add(...)` using the
real clock, while `ConsumeStep` checks the deadline through the injectable `b.now`
field (W8). The deadline anchor and the comparison clock are therefore different time
sources. For the production path (`now == time.Now`) this is correct, but a caller
cannot fully drive wallclock behavior through the injectable clock via the
constructor — the deadline is fixed at construction wall-time regardless of an
injected `now` (every wallclock test today bypasses the constructor and builds
`Budget` literally, which is why it is not yet caught). It also means a Budget
constructed long before its run starts has a deadline measured from construction, not
from run start.
**Fix:** Anchor the deadline through the same clock the gate uses. Resolve `now`
first, then `deadlineWallclock: now().Add(time.Duration(wallclockSec) * time.Second)`,
and accept an optional clock in `BudgetOptions` so construction and gating share one
time source.

### WR-04: `terminalEvent` and dry-run emit an all-zero `SpanID`, defeating the OTel correlation the design advertises

**File:** `internal/agent/workflow/loop.go:188-203`, `cmd/aura/agent.go:103-109`
**Issue:** The package docs (event.go:19-22, agent.go:50-52) make per-node `SpanID`
a load-bearing OTel/W3C concept (8 random bytes, `ParentSpanID` chaining). But the
workflow never populates `ic.SpanID`: the dry-run builds `InvocationContext` with no
`SpanID`, every emitted Event (including the terminal) carries `SpanID: [8]byte{}`,
and the wire form is the constant `"span_id":"0000000000000000"` on every line. The
forward-compat story ("Phase-12 is a fan-out adapter, not a refactor") is undermined
because no node ever gets a real span — a future OTel slice will have to retrofit
span minting into every orchestrator rather than fanning out an existing value.
**Fix:** This may be an accepted Phase-2 deferral, but it is undocumented as such.
Either mint a per-node `SpanID` (`crypto/rand` 8 bytes) when building each child
`InvocationContext` and set `ParentSpanID` to the parent's span, or add an explicit
inline note that span minting is deferred to a named later phase so the zero value is
not mistaken for a bug by the next reader.

### WR-05: SC#2 smoke gate aborts before its own diagnostic on empty dry-run output

**File:** `scripts/loop_budget_smoke.sh:24-29`
**Issue:** Under `set -euo pipefail`, `grep -c .` exits non-zero when zero lines
match. The `LINES` capture at line 24 has no `|| true` guard (unlike `PROFILE_ROWS`
at line 60, which does). If `OUT` is empty (the exact NO-SKIP-AS-GREEN failure the
script is meant to catch loudly), the pipeline dies at line 24 and the hand-written
`"expected exactly 26 Event lines, got $LINES"` diagnostic at lines 26-28 is never
reached. The operator sees a bare pipefail abort instead of the intended message,
weakening the diagnostic the gate was written to provide.
**Fix:** Guard the count capture the same way line 60 already does:
```sh
LINES="$(printf '%s\n' "$OUT" | grep -c . || true)"
if [[ "${LINES:-0}" -ne 26 ]]; then
  echo "FAIL (SC#2): expected exactly 26 Event lines (25 step + 1 terminal), got ${LINES:-0}" >&2
  printf '%s\n' "$OUT" >&2
  exit 1
fi
```

## Info

### IN-01: `softCap` shadows the builtin `cap`

**File:** `internal/agent/budget.go:265-268`
**Issue:** The local variable `cap` shadows the builtin `cap()`. `golangci-lint`
tolerates it under the project config, but it is a readability footgun in a codebase
that also calls the builtin `cap(r.entries)` (budget_dedup.go:84).
**Fix:** Rename the local to `share` or `perBranch`.

### IN-02: `uintToString` reimplements `strconv.FormatUint` for no measured benefit

**File:** `internal/agent/workflow/loop.go:218-231`
**Issue:** The comment says "avoids strconv for a single small-uint conversion path",
but `loop.go` already imports `encoding/json`, and the conversion is off the hot path
(once per iteration label). A hand-rolled base-10 formatter is more code to audit
than the stdlib call for no demonstrated win.
**Fix:** Replace `iterLabel`/`uintToString` with
`"iter-" + strconv.FormatUint(uint64(i), 10)`.

### IN-03: `isPingPong` carries a redundant always-true conjunct

**File:** `internal/agent/budget_dedup.go:107-114`
**Issue:** `return a == fp && a2 == fp && a == a2 && b != fp` — given `a == fp` and
`a2 == fp`, the `a == a2` term is always true and adds noise to a
correctness-critical predicate.
**Fix:** Simplify to `return a == fp && a2 == fp && b != fp`.

### IN-04: `Budget.Child` soft cap is a spawn-time snapshot, making sibling shares timing-dependent

**File:** `internal/agent/budget.go:242-256`
**Issue:** `branchSoftCap: softCap(b.Remaining(), fanout, b.softFrac)` snapshots
`Remaining()` at `Child()` time. ParallelAgent spawns children in a loop, so a later
child computes its soft cap off an already-depleted pool and gets a smaller advisory
share than an earlier sibling. This is consistent with the non-terminal "passive
advisory" framing (D-12) and is informational only, but the timing dependence is
undocumented and could surprise a Phase-9 swarm author expecting equal shares.
**Fix:** Document that the soft cap is a spawn-time snapshot (timing-dependent by
design), or capture `remaining` once before the fan-out loop and pass it to all
sibling `Child` calls so every sibling gets an equal share.

---

_Reviewed: 2026-05-30T00:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
