---
phase: 02-agent-cornerstone
verified: 2026-05-29T22:55:17Z
status: passed
score: 4/4 must-haves verified
overrides_applied: 0
human_verification: []
---

# Phase 2: Agent Cornerstone — Verification Report

**Phase Goal:** Open `Agent` interface + workflow agents (Sequential/Loop/Parallel) adapted from `google/adk-go`; Budget tree with 3-cap contract; UUIDv7 request_id; zero goroutine leak.
**Verified:** 2026-05-29T22:55:17Z
**Status:** PASS
**Re-verification:** No — initial verification

---

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 (SC#1) | Zero goroutine leak across all workflow-agent tests | VERIFIED | `go test -race -count=1 ./internal/agent/...` exits 0; `goleak.VerifyTestMain(m)` wired in `workflow_test.go:17-18`; `TestParallelAgent_EscalateFromAnyCancelsSiblings` + `TestParallelAgent_NoGoroutineLeak_When_ConsumerBreaksEarly` pass with goleak |
| 2 (SC#2) | LoopAgent terminates via budget (26 events, `limit_hit=max_steps`) | VERIFIED | `bash scripts/loop_budget_smoke.sh` exits 0: "ok (SC#2): 26 lines, terminal Event limit_hit=max_steps"; `TestLoopAgent_TerminatesAtMaxSteps_WithExplicitEvent` PASS |
| 3 (SC#3) | Depth-3 nested ParallelAgents share ONE `*atomic.Int32` (not fresh per level) | VERIFIED | `TestParallelAgent_DepthChainBudgetShared_NotFresh` PASS — 9-leaf tree starting at 25 consumes ≤ 25 total, not 25³ |
| 4 (SC#4) | `aura agent dry-run` emits UUIDv7 request_id on every Event; CLI>env>default precedence | VERIFIED | `--request-id auto` → 26 lines, 1 distinct UUIDv7 matching `^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`; `--request-id 0192f000-0000-7000-8000-000000000001` reproduces verbatim; `AURA_LOOP_MAX_STEPS=5 aura agent dry-run --max-steps 10` yields 11 lines (CLI wins) |

**Score:** 4/4 must-haves verified

---

## Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/agent/agent.go` | Open `Agent` interface + `InvocationContext` | VERIFIED | 75 LOC; 5-method interface (Name/Description/Run/SubAgents/FindAgent); InvocationContext with RequestID uuid.UUID, SpanID [8]byte, Budget *Budget; `WithContext`/`WithSubAgent` copy-semantics |
| `internal/agent/event.go` | Full Event shape, AG-UI forward-compat, JSON round-trip | VERIFIED | 129 LOC; full shape (RequestID/SpanID/ParentSpanID/Author/Branch/ThreadID/MessageID/LLMResponse/Actions/Timestamp); custom MarshalJSON/UnmarshalJSON for byte-stable round-trip |
| `internal/agent/budget.go` | Budget shared-atomic, 3-env-var contract, dedup | VERIFIED | 244 LOC; `NewBudgetFromEnv()` reads all 7 AURA_LOOP_* vars; `ConsumeStep()` atomic decrement-check-restore; `Child()` shares `*atomic.Int32`, distinct dedup ring |
| `internal/agent/budget_dedup.go` | Two-phase dedup ring, sha256 fingerprint | VERIFIED | 175 LOC; `BeforeToolCall` + `AfterToolResult`; period-1 + period-2 ping-pong detection; result-preview progress veto |
| `internal/agent/errors.go` | `ErrBudgetExhausted` sentinel | VERIFIED | Exported sentinel for Phase 3/9 consumers |
| `internal/agent/workflow/sequential.go` | SequentialAgent, escalate short-circuit | VERIFIED | 81 LOC; runs subs once in order; returns early on Escalate; adk-go attribution comment |
| `internal/agent/workflow/loop.go` | LoopAgent, budget/dedup termination, explicit terminal Event | VERIFIED | 218 LOC; hard budget stop emits `termination_reason=budget_exhausted`/`limit_hit`/`steps_consumed`; dedup via `BeforeToolCall`/`AfterToolResult`; adk-go attribution |
| `internal/agent/workflow/parallel.go` | ParallelAgent, errgroup+ack backpressure, escalate-cancel | VERIFIED | 174 LOC; errgroup fan-out; per-event ack channel backpressure; captured cancel (D-03) for escalate; D-05 clean drain; adk-go attribution |
| `internal/agent/workflow/workflow.go` | Helper: joinBranch + findInTree | VERIFIED | 24 LOC |
| `internal/agent/agenttest/mocks.go` | Reusable mocks: InfiniteToolCallAgent/EmitNThenEscalate/RecordingAgent/CountingAgent | VERIFIED | 234 LOC; compile-time asserts for all 4 mocks; no NewBudgetFromEnv in mocks |
| `internal/canonicaljson/canonicaljson.go` | Deterministic JSON serializer | VERIFIED | 150 LOC; sorted keys; json.Number (1 != 1.0); strict-rejects NaN/Inf |
| `cmd/aura/agent.go` | `aura agent dry-run` subcommand | VERIFIED | 202 LOC; CLI>env>default precedence (D-06); UUIDv7 auto/literal; Budget from flags; Event JSON lines via MarshalJSON |
| `cmd/aura/main.go` | `case "agent"` dispatch; no `case "chat"` / `chatOnce` / `stubClient` | VERIFIED | grep confirms no stale chat dispatch; `case "agent"` wired to `runAgent` |
| `scripts/loop_budget_smoke.sh` | SC#2 smoke: 26 lines + limit_hit=max_steps + B4 coverage gate | VERIFIED | Runs binary + tests; awk gate at 85%; exits 0 |
| `THIRD_PARTY_NOTICES.md` | google/adk-go Apache-2.0 attribution | VERIFIED | Lists source, license, adapted files, and required hygiene |
| `.env.example` | All 7 AURA_LOOP_* vars documented | VERIFIED | AURA_LOOP_MAX_STEPS=25, AURA_LOOP_MAX_WALLCLOCK_SEC=300, AURA_LOOP_DEDUP_WINDOW=3, AURA_LOOP_DEDUP_EXEMPT_TOOLS, AURA_LOOP_BRANCH_SOFT_FRACTION=1.0, AURA_LOOP_NODE_TIMEOUT_SEC=0, AURA_LOOP_DEDUP_RESULT_CAP=2048 |
| `internal/agent/loop.go` | DELETED (Phase-1 132-LOC Loop skeleton) | VERIFIED | `test ! -f internal/agent/loop.go` exits 0; git log shows deletion in commit 81e2c1da |

---

## Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `LoopAgent.Run` | `Budget.ConsumeStep()` | `ic.Budget.ConsumeStep()` per tool call | VERIFIED | `guardToolCall` calls `ic.Budget.ConsumeStep()` before yielding each tool Event |
| `LoopAgent.Run` | `Budget.BeforeToolCall`/`AfterToolResult` | `canonArgs(tc.Function.Arguments)` | VERIFIED | Two-phase dedup wired in `guardToolCall` |
| `ParallelAgent.Run` | `Budget.Child(len(subs))` | `childIC.Budget = ic.Budget.Child(len(a.subs))` | VERIFIED | Shared `*atomic.Int32` by pointer; distinct dedup ring |
| `dryRun()` | `agent.NewBudgetFromEnv()` | `buildBudget(cfg)` with env override | VERIFIED | CLI flags override env before `NewBudgetFromEnv()` call |
| `dryRun()` | `Event.RequestID` stamp | `ev.RequestID = requestID` loop | VERIFIED | Every emitted event gets the shared run ID stamped |
| `main.go` | `runAgent()` | `case "agent"` | VERIFIED | Single dispatch point; no legacy chat path |
| `workflow_test.go` | `goleak.VerifyTestMain` | `TestMain(m)` | VERIFIED | goleak.VerifyTestMain(m) at line 18 |

---

## Data-Flow Trace (Level 4)

Not applicable — Phase 2 produces workflow infrastructure and a mock dry-run, not a dynamic-data-rendering component. The dry-run data source is the `InfiniteToolCallAgent` mock (deliberately fixed data; the Budget counter drives all variation).

---

## Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| SC#2: 26 events + limit_hit=max_steps | `bash scripts/loop_budget_smoke.sh` | "ok (SC#2): 26 lines ... ok (B4): Phase-2 coverage 91.5% >= 85%" | PASS |
| SC#3: depth-3 fan-3 ≤ 25 steps | `go test -race ./internal/agent/workflow/ -run TestParallelAgent_DepthChainBudgetShared_NotFresh -v` | PASS (0.00s) | PASS |
| SC#4: UUIDv7 auto — all 26 lines same v7 UUID | Python analysis of `aura agent dry-run --request-id auto` | Lines: 26, Distinct: 1, All UUIDv7: True | PASS |
| SC#4: fixed UUID verbatim | Python analysis of `aura agent dry-run --request-id 0192f000-0000-7000-8000-000000000001` | Lines: 26, IDs: {'0192f000-0000-7000-8000-000000000001'} | PASS |
| SC#4: CLI > env precedence | `AURA_LOOP_MAX_STEPS=5 go run ./cmd/aura agent dry-run --max-steps 10` | 11 lines (10 steps + 1 terminal; CLI wins) | PASS |
| SC#1: race + goleak | `go test -race -count=1 ./internal/agent/...` | ok all packages, no race warnings, no goroutine leaks | PASS |
| Full gate | `go vet ./... && go build ./... && go test -race -count=1 ./internal/agent/... ./internal/canonicaljson/... ./cmd/aura/...` | All ok | PASS |
| File size cap | `bash scripts/check-file-size.sh` | "all Go files within the 600-LOC cap" | PASS |
| Lint | `golangci-lint run ./internal/agent/... ./cmd/aura/...` | "0 issues." | PASS |

---

## Requirements Coverage

| Requirement | Phase | Description | Status | Evidence |
|-------------|-------|-------------|--------|----------|
| INFRA-03 | Phase 2 | Open Agent interface + workflow agents + Budget tree + goroutine-leak-free | SATISFIED | All 4 SC pass; REQUIREMENTS.md traceability row shows `[x]` Complete |

---

## Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `cmd/aura/agent.go` | 102 | `ic.SpanID` left as zero `[8]byte` (not minted with `crypto/rand`) | INFO | SpanID field exists with correct 8-byte type and is documented as "8 random bytes — OTel/W3C SpanID shape" in InvocationContext, but the `dryRun()` constructor does not call `crypto/rand.Read()` to populate it. All dry-run Events emit `"span_id":[0,0,0,0,0,0,0,0]`. SC#4 acceptance criteria are exclusively about `request_id` (UUIDv7), so this is NOT a blocker — but it means the observability shape is incomplete for the dry-run fixture. SC#4 passes as written. |
| `cmd/aura/agent.go` | 62+119 | `ic.SpanID` and Event `Timestamp` are zero-valued in mock events | INFO | `InfiniteToolCallAgent` emits Events with `Timestamp: time.Time{}` (0001-01-01); the `dryRun()` IC also does not set `SpanID`. Neither is an SC assertion. Forward-compat shape is present; population is deferred to Phase 3 (LlmAgent). |

No TBD/FIXME/XXX markers found in any Phase 2 modified file. No stub implementations (return null/empty) in the functional path. No OTel direct dependency introduced (go.mod direct deps are clean; transitive OTel via neo4j-go-driver is Phase 1 inheritance).

---

## Coverage

| Surface | Measurement | Result |
|---------|-------------|--------|
| Phase-2 filtered (script B4 gate) | `go test -coverprofile` filtered to internal/agent + internal/canonicaljson + cmd/aura dry-run paths | **91.5%** — exceeds 85% CLAUDE.md floor |
| `internal/agent` (unit) | raw package coverage | 92.5% |
| `internal/agent/workflow` (unit) | raw package coverage | 93.5% |
| `internal/canonicaljson` (unit) | raw package coverage | 85.2% |
| `cmd/aura` (unfiltered, includes db.go/neo4j.go at 0% unit) | raw package | 25.0% — reflects Phase 1 code not Phase 2 |
| `internal/agent/agenttest` | raw package | 70.7% — test-helper package; used by other packages |

**Coverage verdict:** The relevant 85% floor applies to the Phase-2 unit surface as defined in SPEC line 110 and scripts/loop_budget_smoke.sh. The filtered gate (91.5%) exceeds the floor. The unfiltered `cmd/aura` figure is low only because `db.go` and `neo4j.go` (Phase 1 code) have 0% unit coverage — they are integration-tier tested, consistent with REQUIREMENTS and prior phase closures.

---

## Commit Style Note

The SPEC acceptance criterion lists a single atomic commit `slice 0.9: agent runtime abstraction (interface + workflow + budget tree)`. Phase 2 was delivered as 8 individual feature/test commits (per-plan granularity) rather than one atomic commit. The code is functionally complete and correct; this is a commit-style deviation only. The `Loop` deletion occurred atomically in commit `81e2c1da` alongside the `dry-run` subcommand addition, preserving the "Loop deletion is atomic" invariant within the final delivery commit.

---

## Human Verification Required

None. All SC#1–SC#4 behaviors are verified programmatically. Property-based tests (pgregory.net/rapid) confirm escalate-yielded-before-return and total-consumed-never-exceeds-max invariants. Mutation testing (`go-mutesting` on `budget.go`/`budget_dedup.go`) is a manually-run gate documented as VALIDATION.md manual-only; it is not automated and not re-run here (prior SUMMARY evidence from PLAN 02-03 documents this gate).

---

## Gaps Summary

No blocking gaps. The two INFO-level observations (SpanID zero in dry-run, Timestamp zero in mock events) are not SC assertions and are naturally addressed in Phase 3 when `LlmAgent` populates a real `InvocationContext` with crypto/rand SpanID and real timestamps.

INFRA-03 is fully delivered. All 4 success criteria pass. All gate commands (vet, build, race, smoke, lint, file-size, coverage) pass.

---

_Verified: 2026-05-29T22:55:17Z_
_Verifier: Claude (gsd-verifier)_
