---
phase: 2
slug: agent-cornerstone
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-05-29
coverage_target: 0.85
artifact_quality_gate: 0.95
---

# Phase 2 - Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `02-RESEARCH.md` section "Validation Architecture" and hardened by the artifact audit.

## Test Infrastructure

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `pgregory.net/rapid` v1.3.0 (property) + `go.uber.org/goleak` v1.3.0 (leak) |
| Config file | `.golangci.yml` present; Phase 2 removes stale `internal/agent/loop.go` exclusion after deletion |
| Quick run command | `go test ./internal/agent/... ./internal/canonicaljson/... ./cmd/aura/...` |
| Race command | `go test -race -count=1 ./internal/agent/...` |
| Full suite command | `go vet ./... && go build ./... && go test -race -count=1 ./internal/agent/... ./internal/canonicaljson/... ./cmd/aura/... && bash scripts/loop_budget_smoke.sh` |
| Coverage command | `go test -cover ./internal/agent/... ./internal/canonicaljson/... ./cmd/aura/...` |
| Estimated runtime | 20-40 seconds unit/race/smoke before integration tiers |

## Sampling Rate

- After every task commit: run the task-local `<automated>` command, `go vet` for touched packages, and `go build ./...` once the module can compile.
- After every plan wave: run `go test -race -count=1 ./internal/agent/... ./internal/canonicaljson/... ./cmd/aura/...`.
- Before `/gsd:verify-work`: run the full suite, coverage command, file-size cap, lint, smoke script, and mutation spot-check evidence.
- Max feedback latency: 40 seconds for unit/race/smoke; mutation testing is documented as manual gate evidence.

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Secure / Quality Behavior | Test Type | Automated Command | Expected Test / Signal | Status |
|---------|------|------|-------------|----------------------------|-----------|-------------------|------------------------|--------|
| 2-00-01 | 00 | 0 | INFRA-03 | Truth sources converge before code; A1-A7 not deferred | artifact | `rg -n 'SpanID uuid.UUID\|already in chain via\|type sequentialAgent\|RFC 8785-like\|placeholder-plan-id' .planning/phases/02-agent-cornerstone/02-SPEC.md prd.md .planning/ROADMAP.md .planning/REQUIREMENTS.md` | no stale matches in active truth sources | pending |
| 2-00-02 | 00 | 0 | INFRA-03 | Third-party attribution for adapted adk-go Apache-2.0 patterns | artifact | `rg -n 'google/adk-go|Apache-2.0|Apache 2.0' THIRD_PARTY_NOTICES.md .planning/phases/02-agent-cornerstone/02-SPEC.md .planning/phases/02-agent-cornerstone/02-PATTERNS.md` | notice and source attribution present | pending |
| 2-01-01 | 01 | 1 | INFRA-03 / A6 | `github.com/google/uuid` direct, `pgregory.net/rapid` present | build | `go build ./... && go list -m github.com/google/uuid pgregory.net/rapid` | versions v1.6.0 and v1.3.0 | pending |
| 2-01-02 | 01 | 1 | D-08 / A3 | canonicaljson deterministic and strict-rejecting | unit/fuzz | `go test ./internal/canonicaljson/... && go test -run x -fuzz FuzzCanonical_RoundTripAndDistinctNumbers -fuzztime 10s ./internal/canonicaljson/ && go vet ./internal/canonicaljson/...` | fuzz target green; 1 != 1.0; NaN/Inf rejected | pending |
| 2-02-01 | 02 | 1 | Req#1 / D-01 | open Agent interface, no seal, InvocationContext copy semantics | build/unit | `go build ./internal/agent/ && go vet ./internal/agent/` | `Agent` has exactly five methods, `Ctx context.Context` named field, no `internal()` method | pending |
| 2-02-02 | 02 | 1 | Req#2 / D-16 / D-17 | Event full shape, 16-byte RequestID, 8-byte SpanID | unit/property | `go test ./internal/agent/ -run 'TestEvent' && go vet ./internal/agent/` | `TestEvent_FullShapeMarshalsToJSON_RoundTrips`, `TestEvent_TraceID16Bytes_SpanID8Bytes`, `TestEvent_Property_JSONRoundTripByteIdentical` | pending |
| 2-03-01 | 03 | 2 | Req#3 / D-11 | shared atomic budget never over-spends under race | unit/race | `go test -race -count=1 ./internal/agent/ -run 'TestBudget_(ConsumeStep|Child|Wallclock)'` | `TestBudget_ConsumeStep_AtomicDecrement_NoRace`, `TestBudget_ConsumeStep_ExactlyOneWinner_When_CounterIsOne` | pending |
| 2-03-02 | 03 | 2 | Req#3 / D-09 / D-10 | child shares step counter but forks dedup ring | unit | `go test -race -count=1 ./internal/agent/ -run 'TestBudget_Child'` | `TestBudget_Child_SharesStepsCounter`, `TestBudget_Child_DistinctDedupRing` | pending |
| 2-03-03 | 03 | 2 | Req#3 / D-13 | wallclock termination uses fake clock, not goleak-hostile synctest | unit | `go test ./internal/agent/ -run TestBudget_Wallclock_TerminatesAtDeadline` | returns `(false,"wallclock")` past deadline | pending |
| 2-03-04 | 03 | 2 | Req#3 / D-06 | malformed AURA_LOOP env fails fast | unit | `go test ./internal/agent/ -run TestBudget_NewBudgetFromEnv` | exact env parse error asserted | pending |
| 2-03-05 | 03 | 2 | Req#3 / D-18 / D-20 | canonical hash order-independent and pre-execution dedup blocks repeats | unit | `go test -race -count=1 ./internal/agent/ -run 'TestBudget_(BeforeToolCall|Dedup)'` | order-independent fingerprint, period-1 and period-2 repeat detection | pending |
| 2-03-06 | 03 | 2 | Req#3 / D-18 / D-19 | result preview veto and exempt allowlist prevent false runaway stops | unit | `go test ./internal/agent/ -run 'TestBudget_(DedupResultPreviewVetoesOnlyWhenProgress|DedupExemptTools)'` | changed result suppresses dedup; exempt tool never dedups | pending |
| 2-03-07 | 03 | 2 | D-21 | total consumed never exceeds initial max | property/race | `go test -race -count=1 ./internal/agent/ -run TestBudget_Property_TotalConsumedNeverExceedsMax` | rapid property green | pending |
| 2-04-01 | 04 | 2 | D-07 | reusable mocks do not create fresh budgets or import cycles | build/vet | `go build ./internal/agent/agenttest/ && go vet ./internal/agent/agenttest/` | mocks satisfy Agent, one-direction import only | pending |
| 2-05-01 | 05 | 3 | Req#4 | SequentialAgent preserves order and propagates escalate | unit/race | `go test -race -count=1 ./internal/agent/workflow/ -run 'TestSequential'` | `TestSequentialAgent_RunsAllSubsInOrder`, `TestSequentialAgent_PropagatesEscalate` | pending |
| 2-05-02 | 05 | 3 | SC#1 | workflow TestMain wires goleak globally | unit/race | `go test -race -count=1 ./internal/agent/workflow/ -run TestSequential` | `goleak.VerifyTestMain(m)` active | pending |
| 2-05-03 | 05 | 3 | Req#5 / SC#2 | LoopAgent stops at max iterations, escalate, hard budget exhausted | unit/race | `go test -race -count=1 ./internal/agent/workflow/ -run 'TestLoopAgent_(StopsAtMaxIterations|EscalatePropagation|TerminatesAtMaxSteps)'` | final budget event has author, termination_reason, limit_hit, steps_consumed | pending |
| 2-05-04 | 05 | 3 | Req#5 / D-18 | LoopAgent dedup terminates on repeated unchanged calls and vetoes changed results | unit | `go test ./internal/agent/workflow/ -run 'TestLoopAgent_Dedup'` | `limit_hit="dedup"` only when unchanged repeat is true | pending |
| 2-05-05 | 05 | 3 | D-21 | terminal escalate event is yielded before iterator return | property | `go test ./internal/agent/workflow/ -run TestLoopAgent_Property_EscalateYieldedBeforeReturn` | rapid property green | pending |
| 2-06-01 | 06 | 4 | Req#6 / SC#3 | ParallelAgent children share parent budget across depth chain | unit/race | `go test -race -count=1 ./internal/agent/workflow/ -run 'TestParallelAgent_(ChildrenShareParentBudget|DepthChainBudgetShared_NotFresh)'` | total consumed <=25, not 25^3 | pending |
| 2-06-02 | 06 | 4 | Req#6 / D-03 / D-05 | escalate cancels siblings without leaking or surfacing intentional cancel errors | unit/leak/race | `go test -race -count=1 ./internal/agent/workflow/ -run TestParallelAgent_EscalateFromAnyCancelsSiblings` | siblings receive ctx cancel, no goroutine leak | pending |
| 2-06-03 | 06 | 4 | Req#6 / D-23 | early consumer break does not leak producers | unit/leak/race | `go test -race -count=1 ./internal/agent/workflow/ -run TestParallelAgent_NoGoroutineLeak_When_ConsumerBreaksEarly` | `goleak.VerifyNone` clean | pending |
| 2-06-04 | 06 | 4 | Req#6 | ack backpressure prevents unbounded buffering | unit/race | `go test -race -count=1 ./internal/agent/workflow/ -run TestParallelAgent_BackpressureAckChannel` | slow consumer blocks producer | pending |
| 2-06-05 | 06 | 4 | SC#3 | sub-agent exposed as tool still threads shared counter | unit/race | `go test -race -count=1 ./internal/agent/workflow/ -run TestParallelAgent_SubAgentExposedAsTool_SharesCounter` | no fresh child budget reintroduced | pending |
| 2-07-01 | 07 | 5 | SC#4 | dry-run request_id auto is UUIDv7 and stable across all lines | unit/CLI | `go test ./cmd/aura/ -run 'TestDryRun_RequestIDAuto_IsValidUUIDv7_AndStable'` | every line matches v7 regex, all equal | pending |
| 2-07-02 | 07 | 5 | SC#4 | dry-run fixed request_id reproduces verbatim | unit/CLI | `go test ./cmd/aura/ -run TestDryRun_RequestIDFixed_ReproducesVerbatim` | all event lines carry fixed UUID | pending |
| 2-07-03 | 07 | 5 | SC#2 | smoke script asserts 25 step events + one budget-exhausted event | smoke | `bash scripts/loop_budget_smoke.sh` | 26 JSON lines and `"limit_hit":"max_steps"` | pending |
| 2-07-04 | 07 | 5 | Boundary | `Loop` skeleton, `chatOnce`, and `stubClient` removed | build/grep | `go build ./... && test ! -f internal/agent/loop.go && rg -n 'case "chat"|chatOnce|stubClient' cmd/aura/main.go` | build green; grep returns no stale chat stub matches | pending |
| 2-07-05 | 07 | 5 | Env / A7 | all budget env vars documented | grep | `rg -n 'AURA_LOOP_MAX_STEPS|AURA_LOOP_DEDUP_EXEMPT_TOOLS|AURA_LOOP_BRANCH_SOFT_FRACTION|AURA_LOOP_NODE_TIMEOUT_SEC|AURA_LOOP_DEDUP_RESULT_CAP' .env.example prd.md` | all vars present | pending |
| 2-GATE-01 | all | gate | Gate 2 | full build/vet/race/smoke green | suite | `go vet ./... && go build ./... && go test -race -count=1 ./internal/agent/... ./internal/canonicaljson/... ./cmd/aura/... && bash scripts/loop_budget_smoke.sh` | exit 0 | pending |
| 2-GATE-02 | all | gate | Quality | file-size cap and lint clean | suite | `bash scripts/check-file-size.sh && golangci-lint run ./internal/agent/... ./cmd/aura/...` | exit 0 | pending |
| 2-GATE-03 | all | gate | Coverage | Phase 2 unit-only surface reaches 85% coverage | coverage | `go test -cover ./internal/agent/... ./internal/canonicaljson/... ./cmd/aura/...` | combined coverage >=85% | pending |
| 2-GATE-04 | all | gate | Mutation | budget critical mutation spot-check documented | manual evidence | `go-mutesting ./internal/agent/budget.go ./internal/agent/budget_dedup.go` | >=70% killed documented in summary/commit body | pending |

## Wave 0 Requirements

- [ ] `02-00-PLAN.md` executed before code plans.
- [ ] A1-A7 truth-source changes applied before Plan 01.
- [ ] `github.com/google/uuid` v1.6.0 direct dep and `pgregory.net/rapid` v1.3.0 added by Plan 01.
- [ ] `goleak.VerifyTestMain(m)` wired in `internal/agent/workflow/workflow_test.go` by Plan 05.
- [ ] No verification command uses compiler-output filtering or `|| true` as a pass condition.
- [ ] `THIRD_PARTY_NOTICES.md` contains adk-go Apache-2.0 attribution before adapted workflow code lands.

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Mutation testing score | Gate 3 / PRD | `go-mutesting` is manual evidence, not CI | run mutation command on `budget.go`/`budget_dedup.go`, document killed percentage >=70% |

All user-facing smoke and SC#1-SC#4 behaviors have automated verification.

## Validation Sign-Off

- [ ] All tasks have an automated verify command.
- [ ] No placeholder task rows remain.
- [ ] No SC#4 manual-only gap remains.
- [ ] Sampling continuity: no 3 consecutive implementation tasks without automated verify.
- [ ] Feedback latency < 40s for unit/race/smoke.
- [ ] Coverage gate is 85%, matching CLAUDE.md override.
- [ ] The frontmatter nyquist flag is flipped to compliant only after code/tests exist and all mapped commands pass.

**Approval:** pending implementation
