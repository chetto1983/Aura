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
  warning: 6
  info: 6
  total: 12
status: issues_found
---

# Phase 2: Code Review Report

**Reviewed:** 2026-05-30T00:00:00Z
**Depth:** standard
**Files Reviewed:** 25
**Status:** issues_found

## Summary

Reviewed the Phase-2 agent cornerstone: the open `Agent` interface, single-Run
`InvocationContext`, `Event`/`Actions`/`LLMResponse` wire shape, the shared-atomic
`Budget` tree with two-phase dedup, the three workflow orchestrators
(Sequential/Loop/Parallel), the `canonicaljson` serializer, the reusable mocks, and
the `aura agent dry-run` CLI + smoke gate.

The core correctness machinery is solid and well-tested. The TOCTOU-safe step
counter (decrement-then-restore), the property-based concurrency tests, the
goroutine-leak gate (goleak.VerifyTestMain), the canonical-JSON 1!=1.0 invariant, and
the parallel drain/cancel choreography are all genuinely verified. No security
vulnerability or data-loss path was found; `go vet` is clean.

The defects are concentrated in two areas: (1) the `Event` wire contract has
forward-compat tags that silently do not behave as documented (`omitempty` on UUID
arrays is a no-op; `span_id` serializes as a number array, not the base64 the test
comment claims), leaking zero-valued fields on every emitted line; and (2) several
dedup/budget behaviors are documented and tested only at `window=3`, with the
result-stability formula coupling to `window` in a way that is plausibly wrong for
`window != 3` and is entirely uncovered. Plus runtime env mutation in the CLI and a
smoke gate that grades coverage on a fragile awk field.

## Warnings

### WR-01: `MessageID` (and `ThreadID`-class UUID fields) `omitempty` is a no-op — every Event leaks a zero UUID

**File:** `internal/agent/event.go:28,70`
**Issue:** `MessageID uuid.UUID json:"message_id,omitempty"` — `uuid.UUID` is `[16]byte`, an array. `encoding/json`'s `omitempty` only fires for empty slices/maps/strings/numeric-zero/false/nil-pointer; it NEVER fires for array types. Verified empirically: a zero-valued Event marshals `"message_id":"00000000-0000-0000-0000-000000000000"`. The `eventWire` projection (line 70) carries the same broken tag. So every dry-run line and every future AG-UI fan-out Event emits a meaningless all-zero `message_id`, exactly the forward-compat noise the `omitempty` was meant to suppress (D-17 says these fields are "present now even though nothing consumes them yet" — but they were supposed to be omitted until consumed). The round-trip tests pass only because both sides serialize the zero identically, masking the defect.
**Fix:** Use a pointer so `omitempty` works, or omit manually in `MarshalJSON`:
```go
// In eventWire and Event:
MessageID *uuid.UUID `json:"message_id,omitempty"`
// or, keep the value type and gate in MarshalJSON:
if e.MessageID != uuid.Nil {
    w.MessageID = e.MessageID // only when set
}
```
Add a regression assertion to `TestEvent_NilLLMResponse_OmitsObject` that `message_id` is absent for a zero-valued Event.

### WR-02: `span_id` serializes as a JSON number array, not the base64 the test comment asserts

**File:** `internal/agent/event.go:23,66`; comment at `internal/agent/event_test.go:133`
**Issue:** `SpanID [8]byte` is a fixed-size array, so `encoding/json` emits `"span_id":[0,0,0,0,0,0,0,0]` (eight numbers), NOT base64. Only `[]byte` *slices* get base64-string treatment. The test `TestEvent_TraceID16Bytes_SpanID8Bytes` documents "SpanID serializes to a base64 string ([8]byte → 12 base64 chars w/ padding)" — that claim is factually wrong, and the test only asserts `len(b) != 0`, so it never catches the discrepancy. This is a contract issue for the D-16 "drop-in OTel mapping" premise: OTel/W3C represent span IDs as hex, and the current wire form is a verbose number array that a future gateway must special-case. Behavior is internally consistent (round-trips), so this is a quality/contract defect, not a crash.
**Fix:** Decide the intended wire encoding now (hex string is the OTel-idiomatic choice) and encode `SpanID`/`ParentSpanID` explicitly in `MarshalJSON`/`UnmarshalJSON` as hex, then correct the test comment and assert the actual format:
```go
SpanID string `json:"span_id"` // hex(e.SpanID[:]) in eventWire
```

### WR-03: Two-phase dedup result-stability formula is coupled to `window` and only verified at `window=3`

**File:** `internal/agent/budget_dedup.go:129-147`
**Issue:** `resultStable := seen && track.repeats+2 >= r.window` is hand-tuned to align with the period-1 "Nth consecutive call" threshold *for window=3* (see the long comment lines 132-139). For `window=1` it makes dedup fire on the 2nd call of ANY repeated tool (`repeats+2 >= 1` is always true once seen, and `countConsecutive+1 >= 1` is always true), which silently lowers the guard to "any single repeat terminates". For period-2 ping-pong at `window>=4`, `isPingPong` only inspects the last 3 entries (a fixed period-2 shape) while `resultStable` demands `repeats >= window-2`, so the two gates disagree and ping-pong detection effectively requires extra repeats the period-2 detector does not. None of `window != 3` is covered by any test (`budget_dedup_test.go` and `loop_test.go` use only `newTestBudget(_, 3)` / the default 3). The CLI even exposes `--dedup-window` as an operator knob, so non-3 values are reachable in production yet unverified.
**Fix:** Add table-driven dedup tests across `window ∈ {1,2,3,4,5}` asserting the exact call index at which period-1 and period-2 terminate, then either prove the `+2` formula correct for all windows or replace it with a window-parameterized counter that the tests pin. At minimum, document that `isPingPong` is hard-coded to period-2 and decouple it from the period-1 window threshold.

### WR-04: `aura agent dry-run` mutates process-global environment at runtime via `os.Setenv`/`os.Unsetenv`

**File:** `cmd/aura/agent.go:150-201` (`buildBudget`, `overrideEnv`)
**Issue:** `buildBudget` injects CLI flag values into the real process environment (`os.Setenv`) before calling `NewBudgetFromEnv`, then restores via `defer`. This is a non-obvious side channel: `NewBudgetFromEnv` is reading global mutable state that the same function just wrote. It is not goroutine-safe (concurrent `os.Setenv`/`os.Getenv` race) and it collides with `t.Setenv` if any test in package `main` ever runs `t.Parallel()` (the Go runtime panics when `t.Setenv` is used in a parallel test, and `os.Setenv` from `dryRun` during a parallel `t.Setenv` test is a data race). The D-06 precedence (CLI > env > default) can be implemented without touching global state by passing the resolved values into a constructor.
**Fix:** Add an explicit override path to the budget constructor instead of round-tripping through env:
```go
// NewBudget(opts BudgetOptions) where -1/zero fields fall back to env then default,
// resolved in one place, no os.Setenv. dryRun passes cfg.maxSteps etc. directly.
```
This also removes the `AURA_LOOP_DEDUP_EXEMPT_TOOLS` save/restore dance (lines 158-170).

### WR-05: `LoopAgent` consumes one budget step per tool-call but yields one Event per turn — multi-tool-call turns silently over/under-count the SC#2 contract

**File:** `internal/agent/workflow/loop.go:77-90`
**Issue:** The loop iterates `toolCalls(ev)` and calls `ConsumeStep` once per tool call, but yields the triggering `ev` exactly once afterward. The SC#2 "25 step Events + 1 terminal = 26" contract holds ONLY because `InfiniteToolCallAgent` emits exactly one tool call per Event. A real Phase-3 `LlmAgent` that emits two tool calls in one assistant turn would consume two budget steps while producing one step Event, so the smoke's line-count invariant (`grep -c .` == 26) silently decouples from actual budget spend, and `steps_consumed` in the terminal Event will exceed the number of yielded step Events. Additionally, when a multi-tool Event trips the budget on its *second* tool call, the terminal Event REPLACES the whole turn (`return` inside the inner `toolCalls` loop), discarding the first tool call's already-consumed step from the yielded stream. This is a latent correctness gap the moment a non-mock agent is wired in.
**Fix:** Define and test the intended semantics now (one Event == one budgeted step, or N tool calls == N step Events). Either consume once per Event, or emit a per-tool-call step Event so spend and emitted Events stay 1:1. Add a fixture that emits 2 tool calls per turn and assert `steps_consumed == len(stepEvents)`.

### WR-06: Smoke coverage gate parses `$3` from `go tool cover` output — brittle and silently degradable

**File:** `scripts/loop_budget_smoke.sh:49-57`
**Issue:** Two fragilities in the B4 floor gate. (1) The `grep -v -E 'cmd/aura/(db|neo4j|main)\.go|internal/agent/tools/'` filter on `cover.out` strips lines by path substring; if a future file path contains `main.go` as a substring (e.g. `cmd/aura/agentmain.go`) it is silently excluded from the floor, inflating coverage. (2) The awk gate reads `$3` of the `total:` line from `go tool cover -func`; this depends on that tool's column layout. If the format shifts or the `total:` line is missing, `$3 + 0` yields `0`, which is `< 85.0` and FAILS loudly — acceptable — but a partial/garbled line could yield a misleading number. There is no assertion that the filtered profile is non-empty, so an over-aggressive filter that removes every statement would make `go tool cover` emit no `total:` and the awk could mis-grade.
**Fix:** Anchor the path filter (`^cmd/aura/main\.go:`), assert `cover_phase2.out` has >1 line after filtering, and grep the percentage with an explicit pattern (`grep -oE '[0-9.]+%$'`) rather than positional `$3`. Fail if no percentage is captured.

## Info

### IN-01: `softCap` shadows the builtin `cap`

**File:** `internal/agent/budget.go:227-230`
**Issue:** Local variable `cap := int(math.Ceil(...))` shadows the builtin `cap`. Harmless here (no `cap()` call in scope) but a readability footgun and a common lint warning (`predeclared`).
**Fix:** Rename to `c` or `share`.

### IN-02: `done` channel typed `chan bool` but only ever closed, never sent

**File:** `internal/agent/workflow/parallel.go:83,117`; `runSub` `<-chan bool`
**Issue:** `done` is a `chan bool` used purely as a close-signal (no value is ever sent). The idiomatic type for a pure signal is `chan struct{}`, which also documents intent (zero-payload broadcast).
**Fix:** `done := make(chan struct{})` and `done <-chan struct{}` in `runSub`.

### IN-03: `uintToString` reimplements `strconv.FormatUint` to "avoid strconv"

**File:** `internal/agent/workflow/loop.go:168-181`
**Issue:** A hand-rolled uint→string with a 20-byte stack buffer, justified by "avoids strconv for a single small-uint conversion path". `strconv.FormatUint(uint64(i), 10)` is the standard, allocation-cheap, already-imported-elsewhere idiom and is less surface for an off-by-one. The micro-optimization is not warranted on a per-iteration label path.
**Fix:** Replace the helper body with `strconv.FormatUint(uint64(i), 10)`.

### IN-04: `terminalEvent` hard-codes `termination_reason: "budget_exhausted"` even for a dedup stop

**File:** `internal/agent/workflow/loop.go:138-153`
**Issue:** Both the hard `max_steps`/`wallclock` stop AND the `dedup` stop set `termination_reason: "budget_exhausted"`, distinguishing them only via `limit_hit`. A dedup loop is arguably not "budget exhausted" — it terminated on a loop-guard, not a resource cap. Consumers keying on `termination_reason` cannot tell a real exhaustion from a dedup veto without also reading `limit_hit`. Cosmetic/forward-compat, but the field naming invites a misread.
**Fix:** Either emit `termination_reason: "dedup_loop"` for the dedup path, or document explicitly that `termination_reason` is always `budget_exhausted` and `limit_hit` is the discriminator.

### IN-05: `Timestamp` is always serialized but never set by the runtime path

**File:** `internal/agent/event.go:31,92`; emitters in `loop.go`/`mocks.go`
**Issue:** Every workflow/mocks emitter builds an `Event` without setting `Timestamp`, so `MarshalJSON` emits the zero time formatted as `"0001-01-01T00:00:00Z"` on every dry-run line. Like `message_id`, this is forward-compat noise on the user-facing path (W7). Not an `omitempty` field by design, but no emitter stamps it, so it carries no signal yet.
**Fix:** Either stamp `Timestamp: time.Now().UTC()` at emit time in the workflow agents, or document that runtime Events leave it zero until Phase 3 wires it (and consider whether the dry-run should stamp it for OTel correlation alongside `request_id`).

### IN-06: `RecordingAgent` accumulates `SeenBranches`/`Emitted` across runs with no reset — reuse hazard

**File:** `internal/agent/agenttest/mocks.go:124-157`
**Issue:** `RecordingAgent.Run` appends to `SeenBranches` and `Emitted` on every invocation. A test that reuses one `RecordingAgent` instance across multiple `Run` calls (e.g. inside a `LoopAgent`) accumulates state silently, and assertions like `len(rec.SeenBranches) != 1` would break in ways that look like loop bugs. Current tests use fresh instances, so it is latent. Documented as "in order" but not as "monotonic across runs".
**Fix:** Document the accumulation contract explicitly, or expose a `Reset()` helper, so future Phase 3/9 reuse does not silently carry state.

---

_Reviewed: 2026-05-30T00:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
