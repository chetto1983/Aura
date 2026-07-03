---
phase: 35-toolgateway-policy-engine
plan: 03
subsystem: api
tags: [policy-engine, gateway, pep, gate-01, approve-routing, decision-fact, runtime-profile, fail-closed]

# Dependency graph
requires:
  - phase: 35-toolgateway-policy-engine (35-01)
    provides: "internal/gateway.classify(spec, rawArgs) → scoring.RiskTier monotone de-escalator + ValidateClassifiable boot-guard + the Mutating:true floor on the 3 multiplexed tools"
  - phase: 33-runtime-profiles-config-validation
    provides: "config.RuntimeProfile enum + Strict() + config.Config.Profile (AURA_PROFILE)"
  - phase: 34 (tool-invocation ledger)
    provides: "toolinvocations.Store.Insert + the append-only tool_invocations ledger (migration 0011) with ON CONFLICT DO NOTHING"
provides:
  - "internal/gateway.Gateway.Decide — the single in-process policy-enforcement point (GATE-01), interposed inside execTool above tool.Execute"
  - "profile branch: dev/local_trusted host-direct Allow no-op (SC-4); single_user_hardened/server_production fail-closed"
  - "read-only decision-fact path (D-01e) — a durable start-row verdict, never an end row"
  - "approve routing by responder-presence (D-03): hardened+responder → pause; production/headless → deny-with-guidance + a durable degraded_deny terminal end-row"
  - "Gateway injected at all 3 NewLlmAgent composition roots (runner/swarm/cron), keyed on the ORIGINATING conversation UUID"
  - "tools.WithRequestID/RequestIDFromContext + ToolCallIDFromContext (reservation-triple threading); tools.ApprovalPriority exported"
affects: [35-04-durable-reservation, 35-05-reconciler]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "single PEP interposed in the retry seam (execTool) above tool.Execute; nil-gateway is an Allow no-op (mirrors the optional *HookManager collaborator)"
    - "enforcement (Strict()/GateRecommended) kept separate from classification (classify), per D-02d"
    - "read-only/auto-allow decision-fact is a START row so the async observer's real END still lands (both race harmlessly under ON CONFLICT DO NOTHING)"
    - "headless/production denial is a durable degraded_deny TERMINAL end-row keyed on the originating conversation UUID — never logs-only"
    - "responder-presence is a host/policy-side ctx marker (WithResponder), default DENY — the model can never self-approve (D-03c)"
    - "originating-conversation-UUID ledger key relayed to headless swarm/cron so a flat session never keys the ledger (Open Q1 full enforcement)"

key-files:
  created:
    - internal/gateway/gateway.go
    - internal/gateway/decide.go
    - internal/gateway/approve.go
    - internal/gateway/decide_test.go
    - internal/gateway/approve_test.go
    - internal/gateway/main_test.go
  modified:
    - internal/agent/tools/result.go
    - internal/agent/tools/skill_write.go
    - internal/agent/llm_agent.go
    - internal/agent/llm_agent_construct.go
    - internal/agent/llm_agent_retry.go
    - internal/agent/swarm_context.go
    - internal/runner/runner.go
    - internal/swarm/swarm.go
    - internal/swarm/runner_adapter.go
    - internal/cron/dispatch.go
    - internal/cron/handlers/handler.go
    - internal/cron/handlers/agentjob.go
    - cmd/aura/chat.go
    - cmd/aura/serve.go

key-decisions:
  - "request_id reaches execTool via a companion tools.WithRequestID ctx carrier (set once per turn), NOT by extending WithToolCallContext's signature — that call has ~20 test sites and runTool has no request_id in scope (Open Q2: the smaller signature touch)"
  - "tools.ApprovalPriority exported as the single source of truth (skill_write's private skillApprovalPriority delegates to it) — reuse, not re-derive, without leaking policy into the tool descriptor"
  - "the interactive runner marks its turn ctx WithResponder so a strict-profile approval routes to an in-session pause; headless cron/swarm never do (default DENY)"
  - "GATE-01/GATE-03 left UNMARKED in REQUIREMENTS.md — the executed-call single reservation + fail-closed reserve land in 35-04; marking them complete now would be inaccurate (mirrors 35-01's stance)"

patterns-established:
  - "PEP interposition in the retry seam: Decide at the TOP of execTool, before the loop, Deny→typed error / Approve→pause sentinel / Allow→proceed"
  - "durable decision-fact write shape: start-row for a call that executes, terminal end-row for a call that is denied and never executes"

requirements-completed: []  # GATE-01/GATE-03 shared with 35-04 (reservation); not fully delivered here

coverage:
  - id: D1
    description: "Every non-ask_user dispatch passes through gateway.Decide before tool.Execute (single PEP at execTool); nil gateway is an Allow no-op; ask_user is exempt"
    requirement: "GATE-01"
    verification:
      - kind: unit
        ref: "internal/gateway/decide_test.go#TestDecideDevNoOp"
        status: pass
      - kind: integration
        ref: "internal/agent (go test -race ./internal/agent/... — execTool interposition compiles + existing dispatch suite green)"
        status: pass
    human_judgment: false
  - id: D2
    description: "dev/local_trusted → Allow host-direct no-op with NO reservation row (SC-4); nil gateway no-op"
    requirement: "GATE-01"
    verification:
      - kind: unit
        ref: "internal/gateway/decide_test.go#TestDecideDevNoOp"
        status: pass
    human_judgment: false
  - id: D3
    description: "strict + read-only → Allow + a durable START-row decision-fact (D-01e), never an end row"
    requirement: "GATE-01"
    verification:
      - kind: unit
        ref: "internal/gateway/decide_test.go#TestDecideReadOnlyDecisionFact"
        status: pass
      - kind: unit
        ref: "internal/gateway/decide_test.go#TestDecideMutatingAutoAllowFact"
        status: pass
    human_judgment: false
  - id: D4
    description: "approve routing: hardened+responder → ErrAwaitingUserInput{approval}+gateway_approval ResumeContext (D-03); production → deny-with-guidance (D-03b); headless → deny (D-03a)"
    requirement: "GATE-01"
    verification:
      - kind: unit
        ref: "internal/gateway/approve_test.go#TestApproveHardenedInteractive"
        status: pass
      - kind: unit
        ref: "internal/gateway/approve_test.go#TestApproveProductionDenies"
        status: pass
      - kind: unit
        ref: "internal/gateway/approve_test.go#TestApproveHeadlessDenies"
        status: pass
    human_judgment: false
  - id: D5
    description: "headless/production DENY leaves a durable, queryable degraded_deny(reason=no_approver) TERMINAL end-row keyed on the originating conversation UUID (D-03 point 1)"
    requirement: "GATE-01"
    verification:
      - kind: unit
        ref: "internal/gateway/approve_test.go#assertDegradedDenyFact (via TestApproveProductionDenies + TestApproveHeadlessDenies)"
        status: pass
    human_judgment: false
  - id: D6
    description: "post-resume APPROVED branch returns Verdict{Allow, OperatorID} and emits NO competing Insert (D-03 point 2 — the executed marker rides 35-04's single reservation start Meta)"
    requirement: "GATE-01"
    verification:
      - kind: unit
        ref: "internal/gateway/approve_test.go#TestApprovePostResumeAllow"
        status: pass
    human_judgment: false
  - id: D7
    description: "no model-facing approve verdict — the responder signal is host/policy-side (WithResponder), never derivable from model-supplied args (D-03c)"
    requirement: "GATE-01"
    verification:
      - kind: unit
        ref: "internal/gateway/approve_test.go#TestApproveIsHostSideOnly"
        status: pass
    human_judgment: false
  - id: D8
    description: "Gateway injected at all 3 composition roots keyed on the ORIGINATING conversation UUID (runner convID, swarm rc.ConvID, cron OriginConversationID); ValidateClassifiable wired at serve boot"
    requirement: "GATE-01"
    verification:
      - kind: integration
        ref: "cmd/aura (go test ./cmd/aura/ — bootChat constructs gateway.New + ValidateClassifiable, no panic); go test -race ./internal/runner ./internal/swarm ./internal/cron/..."
        status: pass
    human_judgment: false

# Metrics
duration: ~90min
completed: 2026-07-03
status: complete
---

# Phase 35 Plan 03: ToolGateway Decide PEP Summary

**`gateway.Gateway.Decide` — the single in-process policy-enforcement point (GATE-01) interposed inside `execTool` above `tool.Execute` and injected at all three `NewLlmAgent` roots (runner/swarm/cron): a profile branch (dev/local_trusted host-direct no-op — SC-4; hardened/production fail-closed), a read-only start-row decision-fact (D-01e), and approve-by-responder routing that emits an interactive pause under hardened or a durable degraded_deny terminal end-row under production/headless (D-03).**

## Performance

- **Duration:** ~90 min
- **Completed:** 2026-07-03
- **Tasks:** 3
- **Files modified:** 20 (6 created, 14 modified)

## Accomplishments

- Built `internal/gateway/{gateway,decide,approve}.go`: `Gateway{profile, store}` + `New`; `Decide(ctx, spec, rawArgs, ReservationKey) (Verdict, *tools.ErrAwaitingUserInput, error)` — the PEP that branches on the runtime profile, classifies via the 35-01 substrate, records the read-only/auto-allow decision-fact (D-01e), and routes mutating GateRecommended calls to approve.
- Interposed `Decide` at the TOP of `execTool` (before the retry loop): Deny→`*gateway.ErrDenied`, Approve→the gateway's `*tools.ErrAwaitingUserInput`, nil gateway→Allow no-op; `ask_user` exempt (defensive name short-circuit, anti-recursion).
- `routeApprove` distinguishes `single_user_hardened` (interactive pause with a `{"type":"gateway_approval",…}` ResumeContext) from `server_production`/headless (deny-with-guidance + a durable degraded_deny TERMINAL end-row keyed on the originating conversation UUID); the post-resume approved branch returns `Verdict{Allow, OperatorID}` and writes **no** competing row (D-03 point 2).
- Injected the Gateway at all three composition roots on the ORIGINATING conversation UUID (runner convID; swarm relay via `WithSwarmContext`→`RunConfig.Gateway`→`rc.ConvID`; cron `AgentDeps.Gateway`+`Job.OriginConversationID`), wired `ValidateClassifiable(reg)` at the serve boot, and threaded the reservation triple to `execTool` via `tools.WithRequestID`.

## Task Commits

1. **Task 1: Agent seam — request_id threading + Gateway field + execTool Decide interposition** — `ff95a085` (feat)
2. **Task 2: gateway.go + decide.go + approve.go — the PEP, profile branch, read-only decision-fact, approve routing** — `d4421e49` (feat)
3. **Task 3: Composition-root injection (runner/swarm/cron) + originating-conv-UUID plumbing + boot-guard + unit tests** — `05f75dc6` (feat)

_Note: committed in dependency order (Task 2's gateway core first so the agent seam in Task 1 compiles against it), then Task 1, then Task 3 — each commit builds and vets clean._

## Files Created/Modified

- `internal/gateway/gateway.go` — `Gateway`/`New`, the `reservationStore` seam, `Decision`/`Verdict`(+`OperatorID`)/`ReservationKey`/`ErrDenied` [Task 2]
- `internal/gateway/decide.go` — `Decide` PEP + `recordDecisionFact` (start-row) [Task 2]
- `internal/gateway/approve.go` — `routeApprove`, `responderPresent`/`WithResponder`, `ResolvedApproval`/`WithResolvedApproval`, `gatewayApprovalContext`, `recordDegradedDeny` (terminal end-row) [Task 2]
- `internal/gateway/{decide,approve,main}_test.go` — SC-4 + decision-fact + D-03/a/b/c + degraded_deny durability + goleak [Task 3]
- `internal/agent/tools/result.go` — `WithRequestID`/`RequestIDFromContext`/`ToolCallIDFromContext` [Task 1]
- `internal/agent/tools/skill_write.go` — exported `ApprovalPriority` (private delegates) [Task 2 enabler]
- `internal/agent/llm_agent.go` — `LlmAgent.gateway`/`ledgerConvID`, `LlmAgentConfig.Gateway`/`LedgerConversationID`, `WithRequestID` in Run, gateway relay in the swarm-ctx call [Task 1/3]
- `internal/agent/llm_agent_construct.go` — assign gateway + ledgerConvID (defaults to SessionID) [Task 1]
- `internal/agent/llm_agent_retry.go` — execTool Decide interposition [Task 1]
- `internal/agent/swarm_context.go` — `SwarmContextValue.Gateway` + `WithSwarmContext` param [Task 3]
- `internal/runner/runner.go` — `Deps.Gateway`/`Runner.gateway`, buildAgent injection + `WithResponder` on the interactive turn ctx [Task 3]
- `internal/swarm/{swarm,runner_adapter}.go` — `RunConfig.Gateway`, worker `LedgerConversationID: rc.ConvID` + `Gateway`, adapter relay [Task 3]
- `internal/cron/dispatch.go` + `handlers/{handler,agentjob}.go` — `Job.OriginConversationID`, `AgentDeps.Gateway`, `newAgentWorker(ledgerConvID)` [Task 3]
- `cmd/aura/{chat,serve}.go` — construct `gateway.New` once, inject into runner + agent_job, call `ValidateClassifiable` at boot [Task 3]

## Decisions Made

- **request_id threading via a companion carrier, not a WithToolCallContext signature change.** `WithToolCallContext` has ~20 test call sites and `runTool` has no request_id in scope; extending its signature would be high-churn. Instead `tools.WithRequestID` sets the id once per turn (in `Run`), and `execTool` reads the reservation triple via `RequestIDFromContext`/`ToolCallIDFromContext`. This is the explicit Open-Q2 "smaller signature touch". (Minor deviation from the plan's "extend WithToolCallContext" wording — see Deviations.)
- **`tools.ApprovalPriority` exported as the single source of truth.** The plan said "reuse skillApprovalPriority, do NOT re-derive" but it was unexported. Exporting it (and delegating the private one) is the genuine reuse without duplicating the 80/60 scheme or leaking policy into the LLM-visible descriptor.
- **The interactive runner marks its turn ctx `WithResponder`.** This is what makes a strict-profile mutating approval route to an in-session pause; headless cron/swarm never set it, so they default to DENY (D-03a).
- **GATE-01/GATE-03 left UNMARKED.** The executed-call single reservation (the marker riding one start Meta) and the fail-closed synchronous reserve land in 35-04; marking the requirements complete now would overstate delivery (mirrors 35-01's decision).

## Deviations from Plan

### Rule 3 — Blocking (referenced symbol not accessible)

**1. Exported `tools.ApprovalPriority` (skill_write.go not in the plan's file list)**
- **Found during:** Task 2 (routeApprove)
- **Issue:** the plan said reuse `skillApprovalPriority(tier)` but it is unexported in package `tools`, unreachable from `internal/gateway`.
- **Fix:** added exported `ApprovalPriority` in `internal/agent/tools/skill_write.go`; the private `skillApprovalPriority` now delegates to it (single source of truth). One extra file touched vs the plan's `files_modified`.
- **Committed in:** `d4421e49` (Task 2 commit)

### Rule 3 — Discretion (Open Q2, explicitly delegated)

**2. request_id carried by a companion `tools.WithRequestID` rather than extending `WithToolCallContext`**
- **Found during:** Task 1 (agent seam)
- **Issue:** the plan's literal wording ("extend `tools.WithToolCallContext` to ALSO carry request_id") would change a signature with ~20 test call sites, and `runTool` (where `WithToolCallContext` is built) has no request_id in scope.
- **Fix:** added `tools.WithRequestID`/`RequestIDFromContext` (+ `ToolCallIDFromContext`); `Run` sets the id once per turn; `execTool` builds the `ReservationKey` from the ctx. Functionally identical (request_id reaches the tool ctx; a from-context accessor exists) with zero churn to existing call sites. The plan explicitly grants "Per Claude's Discretion (Open Q2) — choose the smaller signature touch".
- **Committed in:** `ff95a085` (Task 1 commit)

---

**Total deviations:** 2 (both Rule 3). **Impact:** no scope creep — one is a genuine reuse export, the other is the plan-sanctioned smaller-signature choice. Behavior matches the plan's must_haves exactly.

## Issues Encountered

- **`go test -race` needs cgo, absent from the Windows PATH.** Ran the race tier natively in WSL (`CGO_ENABLED=1`, `go` at `/usr/local/go/bin`), per CLAUDE.md — `internal/gateway ./internal/agent/... ./internal/runner ./internal/swarm ./internal/cron/...` all green + goleak clean.
- **Approve pause is not yet surfaced as a real pause Event.** The dispatch loop name-gates pauses to `ask_user` (llm_agent_pause.go); a gateway Approve returned from `execTool` for a non-ask_user tool renders as a RoleTool error and the turn continues — but the mutating action is still WITHHELD (Decide returns before `tool.Execute`), so the fail-safe holds. Wiring the gateway pause into a first-class pause Event is out of this plan's task boundaries (the reservation + resume orchestration land in 35-04/later). Documented so the verifier does not mistake it for a gap.

## Known Stubs

None — the read-only decision-fact, the degraded_deny terminal fact, and the approve pause are all live-wired to real code paths. The synchronous reservation (`store.Reserve`) is deliberately deferred to 35-04 per the plan's task boundaries (the mutating-Allow funnel is in place for 35-04 to convert), which is a documented dependency, not a stub.

## Next Phase Readiness

- The single PEP + the mutating-Allow funnel (auto-allow decision-fact AND the post-resume `Verdict{Allow, OperatorID}`) are ready for 35-04 to convert to the one `store.Reserve` call; the `OperatorID` field is already carried on `Verdict`.
- The originating-conversation-UUID ledger key is plumbed at all three roots, so 35-04's reservation and 35-05's reconciler inherit a valid FK key on the headless paths.

## Self-Check: PASSED

- `internal/gateway/gateway.go` — FOUND
- `internal/gateway/decide.go` — FOUND
- `internal/gateway/approve.go` — FOUND
- `internal/gateway/decide_test.go` — FOUND
- `internal/gateway/approve_test.go` — FOUND
- `internal/gateway/main_test.go` — FOUND
- Commit `ff95a085` (Task 1) — FOUND
- Commit `d4421e49` (Task 2) — FOUND
- Commit `05f75dc6` (Task 3) — FOUND

---
*Phase: 35-toolgateway-policy-engine*
*Completed: 2026-07-03*
