---
phase: 35-toolgateway-policy-engine
verified: 2026-07-03T20:06:34Z
status: human_needed
score: 4/4 roadmap success criteria verified in code (GATE-01..04 requirement text confirmed delivered end-to-end); 2 items escalated for human decision
overrides_applied: 0
human_verification:
  - test: "Decide whether the interactive `approve` verdict (GateRecommended mutating tool under `single_user_hardened` + a live responder) needs a working pause/resume UX before Phase 35 is considered fully closed, or whether this is intentionally deferred to a later phase."
    expected: "Either (a) accept that under single_user_hardened today, any Risky/Destructive-tier mutating tool call (shell_exec, fs_write, skill create/update/delete, task run_now/schedule[agent_job], swarm_spawn — ALL of them, since swarm_spawn is unconditionally Risky) renders as a permanent, unresolvable `error: awaiting user input` tool error with no way for the operator to actually approve it (fail-closed holds, but the feature is a dead end), and file a follow-up phase/plan to wire `gateway.WithResolvedApproval` into the Phase-25 approval center (`internal/agui/approvals_api.go` + `internal/runner/runner_resume.go`) and the non-ask_user pause path (`llm_agent_pause.go`'s name-gate), OR (b) treat this as a phase-35 gap requiring a closure plan now."
    why_human: "This is a product/scope decision, not a code-correctness question — the code behaves exactly as unit-tested (fail-closed, no self-approval, correct Verdict shape), but the CONTEXT.md's own LOCKED decision D-03 point 2 ('reuse the Phase-25 approval center ... persist-before-act. The resume MUST re-enter the gateway') is not realized in production code, and no later ROADMAP phase (36-41) mentions wiring it. None of the 4 stated roadmap Success Criteria for Phase 35 require this to work, so it does not block those SCs, but it materially limits what `single_user_hardened` can do today."
  - test: "Decide whether `cmd/aura/chat.go` (608 LOC, over the CLAUDE.md hard 600-LOC cap) needs an immediate refactor-on-touch fix now, or can be deferred to a future touch of that file."
    expected: "Either split chat.go now (the file was 597 LOC before Phase 35; 35-03's Task 3 and 35-05's Task 2 both touched it and pushed it to 607 then 608 without extracting a sub-file, despite both plans' own acceptance criteria stating '≤600 LOC' / 'no touched file > 600 LOC'), or explicitly accept the debt via an override with a tracked follow-up."
    why_human: "This is a repo-hygiene/policy call (CLAUDE.md 'NO GOD CLASS' is stated as a hard cap), not a functional break — go build/vet/test are all clean and no roadmap SC depends on file size. Neither 35-03-SUMMARY.md nor 35-05-SUMMARY.md discloses the breach (35-05 explicitly discloses fixing an analogous breach in serve.go 648→555 but is silent on chat.go crossing 600)."
---

# Phase 35: ToolGateway + Policy Engine Verification Report

**Phase Goal:** One in-process policy decision on every tool call; fail-closed for mutating tools; durable reservation.
**Verified:** 2026-07-03T20:06:34Z
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | No tool executes without a recorded policy decision | ✓ VERIFIED | `internal/agent/llm_agent_retry.go:49-73` (`execTool`) is the ONLY non-exempt call site of `tool.Execute` in `internal/agent` (grep confirms exactly 2 `Execute(ctx` sites: the ask_user pre-execution pre-check in `llm_agent_pause.go:100` and `execTool` in `llm_agent_retry.go:78`); every non-`ask_user` dispatch calls `a.gateway.Decide` before `tool.Execute`, and `execTool` is itself called from exactly one site (`llm_agent.go:552`). Deny → `*gateway.ErrDenied` (never executes); Approve → pause sentinel (never executes); read-only Allow under strict profiles writes a `start`-row decision-fact (`decide.go:recordDecisionFact`, proven by `TestDecideReadOnlyDecisionFact`); mutating Allow takes a synchronous reservation before Execute (`reserve.go`, proven by `TestReserveBeforeExecute` in the db_integration tier). |
| 2 | A timing-out/crashing command hook denies under hardened/production | ✓ VERIFIED | `internal/agent/hooks_command.go:167-176` `commandHookFailPolicy` defaults empty/unset → `FailClosed`; `hooks.go:119-128` `hookFault` aborts the turn on `FailClosed`. Confirmed by running `go test ./internal/agent/... -run 'CommandHook\|Hook'` — green, including `TestCommandHook_TimeoutDeniesTurnToolNeverExecutes`, `TestCommandHook_CrashAllowDeniesTurnToolNeverExecutes`, `TestCommandHook_CrashNoDecisionDeniesTurnToolNeverExecutes`, `TestCommandHookDefaultPolicyNeverSilentAllows`, `TestCommandHookFailOpen_ContainedAllowsTool` (the explicit-opt-in contrast case). The default policy applies regardless of profile (it "over-satisfies" per 35-02's own framing — denies in ALL profiles, not just hardened/production). |
| 3 | A mutating tool is blocked when ledger reservation fails in production | ✓ VERIFIED | `internal/gateway/reserve.go:37-52` `Gateway.reserve`: an `INSERT` error from `store.Reserve` returns `Verdict{Deny, "reservation failed"}` — `execTool` never calls `tool.Execute`. Live-proven by the db_integration tier's `TestReservationFailBlocks` and `TestApprovedCallReservedAndIdempotent` (forced FK-fail INSERT → spy Execute count == 0), both named in 35-04-SUMMARY.md and confirmed present via `grep -n "^func Test" internal/gateway/gateway_integration_test.go`. Compiles clean under `go vet -tags db_integration ./internal/gateway/...`. |
| 4 | The gateway is a no-op (fail-open, host-direct) under dev/local_trusted | ✓ VERIFIED | `internal/gateway/decide.go:29-32`: `if g == nil \|\| !g.profile.Strict() { return Verdict{Decision: Allow, ...}, nil, nil }` — no store write. `config_runtimeprofile.go:56-58` `Strict()` is true ONLY for `single_user_hardened`/`server_production`. Proven by `TestDecideDevNoOp` (`go test -v ./internal/gateway/... -run DevNoOp` → PASS). |

**Score:** 4/4 roadmap success criteria verified.

### Requirements Traceability (GATE-01..04)

| Requirement | REQUIREMENTS.md checkbox | Code-verified delivery | Notes |
|---|---|---|---|
| GATE-01 (single PEP, allow/deny/approve, fail-open dev / fail-closed hardened) | `[ ]` unmarked | ✓ Delivered end-to-end | `Gateway.Decide` interposed at `execTool`, injected at all 3 `NewLlmAgent` roots (runner `buildAgent`, swarm `runChild`/`RunConfig.Gateway`, cron `newAgentWorker`/`AgentDeps.Gateway`) — confirmed via grep at each root + `gateway.New`/`ValidateClassifiable` construction in `cmd/aura/chat.go:288-318` (shared by both `aura chat` and `aura serve`). See the human-verification item on the `approve` verdict's UX completeness. |
| GATE-02 (command hook fail-closed default) | `[ ]` unmarked | ✓ Delivered (pre-existing, now test-locked) | `commandHookFailPolicy` default confirmed `FailClosed` in production code; 35-02 added the DENY-matrix + `fail_open` contained-allow tests, all passing. |
| GATE-03 (durable pre-execution reservation, fail-closed on failure) | `[x]` marked | ✓ Delivered end-to-end | `InsertToolInvocation :execrows`, `Store.Reserve` (rows==1 acquire / rows==0 replay / err deny), synchronous call inside `execTool`'s mutating-Allow funnel before `Execute`. |
| GATE-04 (idempotency key, retry-safe, recoverable state machine) | `[x]` marked | ✓ Delivered end-to-end | The existing UNIQUE `(conversation_id, request_id, tool_call_id, event_kind)` index is the idempotency key; `Verdict.Replay` short-circuits a duplicate without re-invoking `Execute`; the crash-orphan `Reconciler` (`internal/gateway/reconcile.go`) closes a stale `start`∧¬`end` by appending an `end{status='error', indeterminate}` fact, never re-invoking a mutating orphan, with the `effectiveGrace > maxToolExecWindow` collision-impossibility invariant and a pre-append `GetEnd` re-check. |

**REQUIREMENTS.md marking gap (bookkeeping, not functional):** GATE-01 and GATE-02 are `[ ]` unmarked even though both are fully and correctly delivered in code (confirmed above). This matches the pattern the executors themselves documented: 35-01-SUMMARY.md and 35-03-SUMMARY.md both explicitly note "GATE-01/GATE-03 left UNMARKED... marking them complete now would be inaccurate" because at the time each plan landed, the requirement was only partially delivered (spread across 35-01/35-03/35-04). By the time 35-04 landed, GATE-03/04 were correctly flipped to `[x]`, but nobody went back to flip GATE-01 and GATE-02 to `[x]` once 35-03 (GATE-01 PEP, completed) and 35-02 (GATE-02, completed) were actually done. **Recommendation:** flip both checkboxes to `[x]` in `.planning/REQUIREMENTS.md` — this is pure bookkeeping, not a functional gap.

### Required Artifacts

| Artifact | Expected | Status | Details |
|---|---|---|---|
| `internal/gateway/classify.go` | monotone de-escalation classifier | ✓ VERIFIED | Present, 128 LOC, `scoring` package byte-unchanged (`git diff --stat internal/scoring/` empty per 35-01). |
| `internal/gateway/guard.go` | boot-time fail-loud wiring guard | ✓ VERIFIED | `ValidateClassifiable` present, wired at `cmd/aura/chat.go:318` (shared boot for both `chat` and `serve`). |
| `internal/gateway/gateway.go` / `decide.go` / `approve.go` | Gateway PEP core + profile branch + approve routing | ✓ VERIFIED | All present, all match the plan's documented shapes; `go vet`/`go build` clean. |
| `internal/gateway/reserve.go` | synchronous fatal-on-failure reservation orchestration | ✓ VERIFIED | Present; unified reserve funnel for both auto-allow and approved-resume origins confirmed by code + `TestReserveFoldsOperatorID`. |
| `internal/gateway/reconcile.go` | crash-orphan reconciler | ✓ VERIFIED | Present, mirrors `conversations.Sweeper`; wired at `cmd/aura/serve.go:480` (`gateway.NewReconciler`) + `Start` at `:215` + `Stop` at `serve_drain.go:100`. |
| `internal/toolinvocations/store.go` / `store_reserve.go` | `Store.Reserve`/`GetEnd`/`ListInFlightBefore` | ✓ VERIFIED | Present; SQL queries (`InsertToolInvocation :execrows`, `GetToolInvocationEnd :one`, `ListInFlightToolInvocationsBefore :many`) match exactly. |
| `internal/agent/tools/{skill,task,swarm_spawn}.go` | `Mutating:true` + `Multiplexed:true` | ✓ VERIFIED | Confirmed via grep on all three files. |

### Key Link Verification

| From | To | Via | Status | Details |
|---|---|---|---|---|
| `internal/agent/llm_agent_retry.go` (`execTool`) | `internal/gateway.Gateway.Decide` | interposition before `tool.Execute` | ✓ WIRED | `a.gateway.Decide(ctx, spec, args, key)` called at the top of `execTool`, before the retry loop. |
| `internal/runner/runner.go` (`buildAgent`) | `gateway.Gateway` | `Gateway: r.gateway`, `LedgerConversationID` defaults to `convID` | ✓ WIRED | `runner.go:565` + `llm_agent_construct.go:28-31` (`ledgerConvID := cfg.LedgerConversationID; if "" { = cfg.SessionID }`, and runner's `SessionID = convID`). |
| `internal/swarm/swarm.go` (`runChild`) | `gateway.Gateway` | `RunConfig.Gateway` + `LedgerConversationID: rc.ConvID` | ✓ WIRED | `swarm.go:181-182`. |
| `internal/cron/handlers/handler.go` (`newAgentWorker`) | `gateway.Gateway` | `AgentDeps.Gateway` + `LedgerConversationID: ledgerConvID` (from `OriginConversationID`) | ✓ WIRED | `handler.go:131-134`. |
| `internal/gateway/decide.go` mutating-Allow funnel | `internal/gateway/reserve.go` `Store.Reserve` | single unified call for both auto-allow and post-resume-approved origins | ✓ WIRED | `decide.go:47-55` — one `operatorID` variable threaded through, one `g.reserve(...)` call regardless of origin. |
| `internal/gateway/reconcile.go` | `internal/toolinvocations.Store.{ListInFlightBefore,GetEnd,Insert}` | list → re-check → append | ✓ WIRED | `reconcile.go:180-212`. |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|---|---|---|---|
| Full build | `go build ./...` | clean, no errors | ✓ PASS |
| Full vet | `go vet ./...` | clean, no errors | ✓ PASS |
| Full untagged test suite | `go test ./...` | all 64 packages `ok` (3 packages report "no test files", none fail) | ✓ PASS |
| Gateway unit/table/property/guard tests | `go test -v ./internal/gateway/... -run 'Classify\|Guard\|Property\|DevNoOp\|ReadOnly\|Approve\|Decide\|ValidateClassifiable'` | all PASS (25+ subtests, incl. `rapid` property tests) | ✓ PASS |
| Command-hook fail-closed tests | `go test -v ./internal/agent/... -run 'CommandHook\|Hook'` | all PASS | ✓ PASS |
| db_integration compile check (no live stack available in this session) | `go vet -tags db_integration ./internal/gateway/... ./internal/toolinvocations/...` | clean | ✓ PASS (compile-verified; live execution reported green in WSL per CLAUDE.md/SUMMARYs, not independently re-run here) |

### Probe Execution

No `scripts/*/tests/probe-*.sh` probes exist for this phase (`find scripts -path '*/tests/probe-*.sh'` — not applicable; this is a Go backend policy-engine phase, not a migration/CLI-probe phase). SKIPPED per Step 7c criteria (no runnable probe entry points declared by the PLANs or SUMMARYs).

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
|---|---|---|---|---|
| GATE-01 | 35-01, 35-03 | Single in-process policy decision (allow/deny/approve), recorded durably, fail-open dev / fail-closed hardened | ✓ SATISFIED (core PEP) / see human-verification item for the `approve` UX completeness | See Requirements Traceability table above. |
| GATE-02 | 35-02 | Command hooks fail-closed default | ✓ SATISFIED | See Requirements Traceability table above. |
| GATE-03 | 35-01, 35-03, 35-04, 35-05 | Durable pre-execution ledger reservation, fail-closed on failure | ✓ SATISFIED | See Requirements Traceability table above. |
| GATE-04 | 35-04, 35-05 | Idempotency key + recoverable durable state machine | ✓ SATISFIED | See Requirements Traceability table above. |

No orphaned requirements found: `grep -E "Phase 35" .planning/REQUIREMENTS.md` maps only GATE-01..04, and all four are claimed across the 5 plans' frontmatter `requirements:` fields.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|---|---|---|---|---|
| `cmd/aura/chat.go` | whole file, 608 LOC | Exceeds CLAUDE.md's hard 600-LOC "NO GOD CLASS" cap | ⚠️ Warning | The file was 597 LOC before Phase 35 (`git show c4496ed2:cmd/aura/chat.go \| wc -l` = 597). 35-03's Task 3 pushed it to 607, 35-05's Task 2 to 608 — both plans' own acceptance criteria state "no touched file > 600 LOC" / "every touched root ≤600 LOC," and neither SUMMARY discloses the breach (35-05-SUMMARY.md explicitly discloses fixing an analogous breach in `serve.go` 648→555 via a dedicated refactor commit, but is silent on `chat.go` crossing 600). Does not affect `go build`/`go vet`/`go test` correctness. See human-verification item. |
| `internal/gateway/approve.go` + `internal/agent/llm_agent_pause.go` | `WithResolvedApproval`/`ResolvedApproval` (approve.go:43-64); pause name-gate (llm_agent_pause.go:44-70) | Dead-code primitive: `gateway.WithResolvedApproval` has zero production callers (only test call sites); `gateway_approval` as a ResumeContext type is never read by `internal/agui/approvals_api.go` or `internal/runner/runner_resume.go` | ⚠️ Warning | Not a debt-marker (no TODO/FIXME/XXX — the code is deliberately structured and well-documented as "a later plan" concern), but a materially incomplete feature path. See human-verification item. |

No `TODO`/`FIXME`/`XXX`/"not yet implemented"/placeholder markers found in any of the phase's touched files (`internal/gateway/*.go`, `internal/toolinvocations/store*.go`, `internal/agent/llm_agent_retry.go`, `internal/agent/hooks_command.go`, `internal/agent/hooks.go`, `cmd/aura/serve*.go`, `cmd/aura/chat.go`).

### Human Verification Required

### 1. Interactive `approve` verdict — pause/resume UX completeness under `single_user_hardened`

**Test:** Under `AURA_PROFILE=single_user_hardened`, start an interactive `aura chat` session and invoke a Risky/Destructive-tier mutating tool (e.g. `swarm_spawn`, or `skill` with `action=create`).
**Expected:** Either the operator gets a real, resolvable approval prompt (via the Phase-25 approval center, per CONTEXT.md's locked D-03 point 2), or the team explicitly accepts that this path is a deliberate, tracked incompleteness (currently it renders as a permanent `error: awaiting user input` RoleTool error with no resolution path — `gateway.WithResolvedApproval` is never invoked by any production code, and the `gateway_approval` ResumeContext type is never read by the approval-center HTTP API or the resume orchestrator).
**Why human:** This is a scope/priority decision (accept as a tracked follow-up vs. block phase closure), not a code-correctness question — the fail-closed guarantee holds either way (the tool never executes without an actual, resolved approval), so no roadmap Success Criterion is violated, but a documented LOCKED design decision (D-03 point 2) is not yet realized end-to-end.

### 2. `cmd/aura/chat.go` 600-LOC breach

**Test:** Confirm whether the project wants an immediate refactor-on-touch split of `cmd/aura/chat.go` (608 LOC) before Phase 35 closes, per CLAUDE.md's hard "NO GOD CLASS. Never create a file >600 LOC" rule.
**Expected:** Either a follow-up refactor commit splitting `chat.go` (mirroring the `serve.go`→`serve_dispatch.go` extraction already done in 35-05), or an explicit acceptance/override.
**Why human:** Pure repo-hygiene/policy call; does not affect functional correctness (build/vet/test all clean).

### Gaps Summary

No functional gap was found against any of the 4 stated ROADMAP.md Success Criteria for Phase 35, and all `must_haves` truths/artifacts/key_links declared across the 5 plans' frontmatter were independently verified against the actual codebase (not just SUMMARY claims) via direct code reading, grep-based wiring verification, and live `go build`/`go vet`/`go test` runs (all green across all 64 packages). GATE-03 and GATE-04 are correctly marked `[x]` in REQUIREMENTS.md and are genuinely delivered end-to-end (durable synchronous reservation, idempotency-key replay, crash-orphan reconciliation with a proven collision-impossibility invariant). GATE-01 and GATE-02 remain `[ ]` unmarked in REQUIREMENTS.md despite being genuinely and fully delivered in code — this is a pure bookkeeping omission carried over from the executors' own (accurate, at-the-time) decision not to mark them complete in 35-01/35-02/35-03, which was never revisited once the dependent work (35-03/35-04) actually landed.

Two items are escalated for human decision rather than treated as blocking gaps, because neither violates a stated roadmap Success Criterion nor a plan's literal `must_haves` wording, but both represent real, verifiable incompleteness relative to the phase's own documented intent: (1) the interactive `approve` verdict path has no working pause/resume UX in production code today, making `single_user_hardened` effectively unable to complete any Risky/Destructive-tier mutating tool call; (2) `cmd/aura/chat.go` was pushed over the project's hard 600-LOC cap by two of this phase's plans without the mandated refactor-on-touch, and the breach was not disclosed in either SUMMARY.

---

## Orchestrator Follow-up (execute-phase)

- **Item 2 (`cmd/aura/chat.go` 608-LOC breach) — RESOLVED** in commit `246b5607`:
  refactor-on-touch split the boot/composition-root path into
  `cmd/aura/chat_boot.go`; chat.go is now 267 LOC and chat_boot.go 359 LOC,
  both under the 600-LOC cap (adopted from an in-flight quality-cleanup, then
  build/vet/test-verified). The verifier's 608-LOC finding was correct.
- **Item 1 (interactive `approve` pause/resume UX dead-end) — ROUTED TO
  GAP-CLOSURE** per user decision (2026-07-03): wire `gateway.WithResolvedApproval`
  / the `gateway_approval` ResumeContext into the approval-center API
  (`internal/agui/approvals_api.go`) + resume orchestrator
  (`internal/runner/runner_resume.go`) + the non-`ask_user` pause path
  (`internal/agent/llm_agent_pause.go`). Phase 35 stays PENDING until this
  gap plan is executed and re-verification passes.

*Verified: 2026-07-03T20:06:34Z*
*Verifier: Claude (gsd-verifier)*
