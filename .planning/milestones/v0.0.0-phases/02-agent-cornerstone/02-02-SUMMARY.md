---
phase: 02-agent-cornerstone
plan: 02
subsystem: agent-runtime
tags: [go, agent-interface, invocation-context, event, trace-ids, otel, uuidv7, iter-seq2, sentinel-error, ag-ui]

# Dependency graph
requires:
  - phase: 02-01
    provides: github.com/google/uuid v1.6.0 (UUIDv7 source), internal/canonicaljson, pgregory.net/rapid
provides:
  - "internal/agent.Agent — OPEN interface (5 methods, no seal) every later phase implements/consumes (D-01)"
  - "internal/agent.InvocationContext — single-Run-scoped struct with WithContext/WithSubAgent copy-semantics (D-24)"
  - "internal/agent.Event / Actions / LLMResponse — full forward-compat shape, OTel-correct trace IDs, custom MarshalJSON (D-16/D-17/D-21)"
  - "internal/agent.ErrBudgetExhausted — exported sentinel for errors.Is consumers (D-04)"
  - "internal/agent.Budget — TEMP empty stub (Plan 02-03 Task 1 replaces it with the real budget.go)"
affects: [02-03 Budget tree, 02-04 agenttest mocks, 02-05/06 workflow agents, 02-07 dry-run + loop.go deletion, 12 AG-UI gateway, future OTel slice]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Open Agent interface (no internal() seal) — diverges from adk-go, enables direct implementation by LlmAgent/swarm"
    - "InvocationContext passed by value; WithContext/WithSubAgent return a copy, never mutate (ctx-as-named-field, not embedded)"
    - "Custom Event.MarshalJSON: SetEscapeHTML(false) + RFC3339Nano UTC timestamp via eventWire projection for byte-identical round-trip"
    - "OTel/W3C trace widths: 16-byte UUIDv7 RequestID + 8-byte SpanID ([8]byte) + *[8]byte ParentSpanID nil at root"
    - "Reuse llm.ToolCall in LLMResponse — no ToolCall redefinition (D-17)"

key-files:
  created:
    - internal/agent/agent.go
    - internal/agent/event.go
    - internal/agent/errors.go
    - internal/agent/event_test.go
    - internal/agent/agent_test.go
  modified:
    - internal/agent/loop.go

key-decisions:
  - "Agent interface is OPEN (no unexported seal) per D-01; proven by a non-constructor stubAgent satisfying it via compile-time assertion"
  - "SpanID/ParentSpanID are [8]byte / *[8]byte (crypto/rand width), NOT uuid.UUID — D-16/A4 supersedes SPEC Req#1; a 16B SpanID would force lossy OTel truncation"
  - "Event byte-identical round-trip achieved via custom Marshal/UnmarshalJSON through an eventWire projection (timestamp normalized to RFC3339Nano UTC), the single user-facing serialization path (W7) — canonicaljson is hashing-only"
  - "loop.go package doc demoted (not deleted) to avoid a duplicate package comment; canonical package doc moved to agent.go. loop.go deletion remains Plan 02-07's job"
  - "Budget left as TEMP empty struct stub so the package compiles FULLY clean at Plan-02 time (B1 honest gate); Plan 02-03 Task 1 deletes it"

patterns-established:
  - "eventWire projection for deterministic struct→JSON with normalized timestamp"
  - "TDD task split: contract trio (agent.go+errors.go+event.go) commits compiling-green, then test commit"

requirements-completed: [INFRA-03]

# Metrics
duration: ~5min
completed: 2026-05-29
---

# Phase 2 Plan 02: Agent Cornerstone Contract Types Summary

**Defined the substrate's public API: the OPEN `Agent` interface (5 methods, no seal — D-01), the single-Run-scoped `InvocationContext` with copy-semantics (D-24), the full forward-compat `Event`/`Actions`/`LLMResponse` shape with OTel-correct trace IDs (16B UUIDv7 RequestID + 8B SpanID — D-16/A4) and a custom byte-identical-round-trip MarshalJSON (D-21), and the exported `ErrBudgetExhausted` sentinel (D-04) — package builds FULLY clean with legacy loop.go still present via a temporary Budget stub.**

## Performance
- **Duration:** ~5 min
- **Started:** 2026-05-29T21:38Z
- **Completed:** 2026-05-29T21:43Z
- **Tasks:** 2
- **Files modified:** 6 (5 created, 1 demoted package doc on loop.go)

## Accomplishments
- `Agent` interface is OPEN: exactly 5 methods (`Name`/`Description`/`Run(InvocationContext) iter.Seq2[*Event, error]`/`SubAgents`/`FindAgent`), no unexported `internal()` seal. A non-constructor `stubAgent` satisfies it (compile-time `var _ Agent = stubAgent{}`), proving Phase 3 LlmAgent / Phase 9 swarm can implement it directly.
- `InvocationContext` is a plain struct with `Ctx context.Context` as a NAMED field (never embedded), the verbatim D-24 doc (`single-Run-scoped`), and `WithContext`/`WithSubAgent` that return copies without mutating the receiver (proven by tests).
- `Event`/`Actions`/`LLMResponse` full shape: `RequestID uuid.UUID` (16B), `SpanID [8]byte`, `ParentSpanID *[8]byte` (nil at root), plus `ThreadID`/`MessageID`/reused `llm.ToolCall` for AG-UI forward-compat (D-17). Custom `MarshalJSON` (SetEscapeHTML(false), RFC3339Nano UTC) + symmetric `UnmarshalJSON` give byte-identical round-trip, validated by a rapid property test.
- `ErrBudgetExhausted` exported sentinel works with `errors.Is` (D-04).
- Package compiles FULLY clean (no swallowed errors) with the legacy `loop.go` still present, via a temporary `type Budget struct{}` stub carrying the `// TEMP:` comment for Plan 02-03 to delete.
- `go build ./...`, `go vet ./...`, `go test`, `go test -race`, `golangci-lint`, gofmt, and `scripts/check-file-size.sh` all clean.

## Task Commits
1. **Task 1: open Agent interface + InvocationContext + Event contract + ErrBudgetExhausted** - `5415ad18` (feat)
2. **Task 2: Event JSON round-trip + trace-ID shape + context copy-semantics tests** - `c6a2fba5` (test)

_Note: agent.go/errors.go/event.go landed in one compiling commit because `Agent.Run` references `*Event` — the three contract files form one atomic buildable unit. The test commit follows the GREEN contract._

## Files Created/Modified
- `internal/agent/agent.go` (81 LOC) — package doc (WHY the interface is open), `Agent` interface, `InvocationContext` struct, `WithContext`/`WithSubAgent` copy-semantics, TEMP `Budget` stub.
- `internal/agent/event.go` (128 LOC) — `Event`/`Actions`/`LLMResponse`, `SetAuthorIfEmpty`, custom `MarshalJSON`/`UnmarshalJSON` via `eventWire`.
- `internal/agent/errors.go` (10 LOC) — `ErrBudgetExhausted` exported sentinel.
- `internal/agent/event_test.go` (228 LOC) — full-shape round-trip, nil-omission, trace-ID shape, rapid property round-trip, errors.Is, SetAuthorIfEmpty.
- `internal/agent/agent_test.go` (93 LOC) — InvocationContext copy-semantics, FindAgent recursion, open-interface compile assertion.
- `internal/agent/loop.go` — package doc demoted to a plain comment (canonical package doc moved to agent.go); body unchanged. Deletion stays Plan 02-07.

## Verify Command Outputs (evidence)

**Task 1:**
```
$ go build ./internal/agent/ && go vet ./internal/agent/    # BUILD+VET OK
$ go build ./...                                             # FULL BUILD OK (loop.go still present)
$ ~/go/bin/golangci-lint run ./internal/agent/              # 0 issues
$ grep -c 'func.*internal()' internal/agent/agent.go        # 0 (seal removed, D-01)
```

**Task 2:**
```
$ go test ./internal/agent/ -run 'TestEvent|TestErr' -v
  PASS TestEvent_FullShapeMarshalsToJSON_RoundTrips
  PASS TestEvent_NilLLMResponse_OmitsObject
  PASS TestEvent_TraceID16Bytes_SpanID8Bytes
  PASS TestEvent_Property_JSONRoundTripByteIdentical   [rapid] OK, passed 100 tests
  PASS TestErrBudgetExhausted_IsComparable
  PASS TestEvent_SetAuthorIfEmpty
$ go test ./internal/agent/                                 # ok  (incl. agent_test.go)
$ go test -race -count=1 ./internal/agent/                  # RACE OK  (as --version = Binutils 2.46)
$ go vet ./...                                              # FULL VET OK
$ ~/go/bin/golangci-lint run ./internal/agent/             # 0 issues
$ gofmt -l internal/agent/*.go                             # (clean)
$ bash scripts/check-file-size.sh                          # all Go files within the 600-LOC cap
$ go test ./internal/agent/ -cover                         # coverage: 41.9% of statements
```

## Decisions Made
- **SpanID is `[8]byte`, not `uuid.UUID`.** D-16/A4 explicitly supersedes SPEC Req#1's `uuid.UUID` for SpanID/ParentSpanID. The test `TestEvent_TraceID16Bytes_SpanID8Bytes` asserts `len(SpanID)==8` and `RequestID.Version()==7`. A 16-byte SpanID would force lossy truncation when a future OTel slice maps these.
- **Contract trio in one commit.** `Agent.Run` returns `iter.Seq2[*Event, error]`, so agent.go cannot compile without Event. The plan's per-task TDD split is preserved logically (Task 1 = compiling contract, Task 2 = tests) while keeping every commit buildable — no broken-build commit.
- **loop.go package doc demoted, not deleted.** Two package-level doc comments in one package would trip vet/lint; the cornerstone package doc belongs in agent.go (per plan). Demoting loop.go's comment (dropping the `Package` lead word) is the minimal compile-safe change. loop.go deletion stays Plan 02-07.
- **Added agent_test.go beyond the plan's named files** to cover the D-24 copy-semantics and D-01 open-interface contract this plan owns (raised package coverage 33.8% → 41.9%). This is in-scope verification of this plan's own deliverables, not new functionality.

## Deviations from Plan

### Auto-fixed / structural adjustments (no behavior change)

**1. [Rule 3 - Blocking] loop.go package-doc collision**
- **Found during:** Task 1 (`go build` of the package with a second package doc in agent.go)
- **Issue:** Legacy `loop.go` carried the `// Package agent ...` doc; adding the required cornerstone package doc to agent.go would create a duplicate package comment (vet/stylecheck warning), and the plan mandated the package doc live in agent.go.
- **Fix:** Demoted loop.go's comment to a plain (non-`Package`) comment; canonical package doc now in agent.go. loop.go body untouched; deletion remains Plan 02-07's job per the sequential-execution constraint.
- **Files modified:** internal/agent/loop.go
- **Commit:** 5415ad18

**2. [Scope addition - verification] agent_test.go**
- **Found during:** Task 2 (coverage of this plan's own contract)
- **Issue:** event_test.go alone did not exercise InvocationContext copy-semantics (D-24) or the open-interface implementability (D-01) — both core deliverables of this plan.
- **Fix:** Added agent_test.go with WithContext/WithSubAgent non-mutation tests, FindAgent recursion, and a `var _ Agent = stubAgent{}` assertion.
- **Files modified:** internal/agent/agent_test.go (new)
- **Commit:** c6a2fba5

No functional deviation from the planned contract: every type, field width, method, and sentinel matches the plan and the D-01/D-04/D-16/D-17/D-24 decisions.

## Issues Encountered
- None blocking. `go build ./internal/agent/` initially failed with `undefined: Event` after only agent.go/errors.go existed — expected (Run references *Event), resolved by landing event.go in the same compiling commit. The package is honest-green per the B1 gate.

## Known Stubs
- `internal/agent/agent.go` — `type Budget struct{}` is an intentional TEMP stub (carries the `// TEMP:` comment). It exists only so `InvocationContext.Budget *Budget` resolves and the package compiles cleanly at Plan-02 time. **Plan 02-03 Task 1 MUST delete it** and land the real `budget.go`. This is documented in the plan (B1, option a) and is not a hidden stub.

## Coverage Note
Package coverage is 41.9%, but every uncovered statement is in the legacy `loop.go` (`NewLoop`/`Turn`/`runTool`/`toolDefs` at 0% — deleted in Plan 02-07) or in error-return branches of Marshal/Unmarshal (85-89%). The contract files this plan delivers (event.go core, errors.go, agent.go helpers) are well-covered. The 85% phase floor is a phase-close gate measured after loop.go is removed and the budget/workflow tests land (02-03+); it is not a per-plan gate.

## User Setup Required
None. No new env vars in this plan (the A7 `AURA_LOOP_*` vars land with the Budget plan 02-03).

## Threat Model Notes
- **T-02-05 (Spoofing, SpanID forgery):** mitigated by design — SpanID is `[8]byte` matching OTel `RandomIDGenerator` width (D-16); generation from `crypto/rand` is the Budget/dry-run plan's responsibility, the contract here locks the correct width. Proven by `TestEvent_TraceID16Bytes_SpanID8Bytes`.
- **T-02-06 (Tampering, cross-invocation context leakage):** mitigated — `Ctx` is a named field, `WithContext`/`WithSubAgent` return copies (tested non-mutation), `single-Run-scoped` doc enforced.
- **T-02-07 (Info disclosure, lossy SpanID truncation):** mitigated — 8-byte SpanID now means the future OTel mapping is drop-in, no truncation.
- No new security surface beyond the threat register.

## Next Phase Readiness
- The `Agent` interface, `InvocationContext`, `Event`/`Actions`/`LLMResponse`, and `ErrBudgetExhausted` are ready for Plan 02-03 (Budget tree — must delete the TEMP `Budget` stub), 02-04 (agenttest mocks implementing `Agent`), and 02-05/06 (workflow agents emitting Events).
- `iter.Seq2[*Event, error]` signature is locked; workflow agents follow the D-22 yield discipline.
- No blockers.

## Self-Check: PASSED
- FOUND: internal/agent/agent.go
- FOUND: internal/agent/event.go
- FOUND: internal/agent/errors.go
- FOUND: internal/agent/event_test.go
- FOUND: internal/agent/agent_test.go
- FOUND commit 5415ad18 (Task 1 contract)
- FOUND commit c6a2fba5 (Task 2 tests)

---
*Phase: 02-agent-cornerstone*
*Completed: 2026-05-29*
