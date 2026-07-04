---
phase: 35-toolgateway-policy-engine
plan: 06
subsystem: infra
tags: [gateway, policy-engine, hitl, approval, ledger, shell_exec-precedent, resume-hook, postgres, go]

# Dependency graph
requires:
  - phase: 35-03
    provides: Gateway Decide PEP + routeApprove responder routing + gatewayApprovalContext + WithResolvedApproval ctx carrier
  - phase: 35-04
    provides: unified single-reservation funnel (reserve.go) + rows-affected idempotency (Verdict.Replay)
provides:
  - GatewayApprovals cross-turn one-shot approval ledger (Approve/Consume/Peek/Evict) + gatewayArgsFingerprint
  - shell_exec-style approval-required ToolResult (gatewayApprovalRequiredResult) surfaced through execTool as a normal result
  - newGatewayResumeHook — the sole production writer of the ledger, chained in chat_boot.go
  - full persist->resume round-trip proof + live db_integration resume/decline proofs
affects: [phase-36-multi-user-identity, gate-01, gate-02, verify-work]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Approval-as-ToolResult (not a pre-dispatch pause): the gateway returns a normal tool RESULT on the ORIGINAL call so the real tool name + args stay in persisted history and the resume re-emits an args-matching call"
    - "Cross-turn one-shot approval ledger keyed on (conversation_id, tool, canonical-args fingerprint) — never tool_call_id (which changes on re-emit)"
    - "Host-side resume hook is the SOLE ledger writer; routeApprove only READS/Consumes (D-03c no model self-approval)"

key-files:
  created:
    - internal/gateway/approvals.go
    - internal/gateway/approvals_test.go
    - internal/agent/llm_agent_retry_gateway_test.go
    - cmd/aura/gateway_resume_hook_test.go
    - cmd/aura/gateway_approval_roundtrip_test.go
  modified:
    - internal/gateway/gateway.go
    - internal/gateway/approve.go
    - internal/gateway/decide.go
    - internal/gateway/reserve.go
    - internal/agent/llm_agent_retry.go
    - cmd/aura/serve_adapters.go
    - cmd/aura/chat_boot.go
    - internal/runner/runner_resume.go
    - internal/gateway/gateway_integration_test.go

key-decisions:
  - "Mirror internal/agent/tools/shell_approval.go EXACTLY, hoisted ABOVE tool.Execute (strictly stronger fail-closed than shell_exec, whose gate is inside Execute)"
  - "Decide/routeApprove/reserve now return (Verdict, error) — the dead *tools.ErrAwaitingUserInput middle return is removed; the gateway no longer mints a pause"
  - "The GatewayApprovals ledger is one-shot on Consume: exactly-once execution is the 35-04 RESERVATION's job; a re-emit with tampered args (different fingerprint) fails closed"
  - "OperatorID='local' (single_user_hardened has one principal); multi-identity attribution deferred to Phase 36 (D-03b)"

patterns-established:
  - "Approval-required tool RESULT + model-relayed ask_user + cross-turn session ledger + resume hook = the reusable gateway-approval shape (no new subsystem, no pre-dispatch pause-detector widening)"

requirements-completed: []  # GATE-01 approve-path closed end-to-end; the REQUIREMENTS.md [x] flip is phase-completion bookkeeping after re-verification (per plan success criteria), not done here.

coverage:
  - id: D1
    description: "GatewayApprovals cross-turn ledger (Approve/Consume one-shot, Peek non-destructive, Evict prefix sweep, nil-safe) + gatewayArgsFingerprint (cosmetic-insensitive, semantic-strict)"
    requirement: "GATE-01"
    verification:
      - kind: unit
        ref: "internal/gateway/approvals_test.go (TestGatewayApprovals*, TestGatewayArgsFingerprintCanonicalEquality) — go test -race ./internal/gateway/"
        status: pass
    human_judgment: false
  - id: D2
    description: "routeApprove ledger re-entry returns Verdict{Allow,OperatorID} with no Insert; no-hit under hardened+responder returns Approve + ApprovalRequest (gateway_approval + args_sha256 + descriptive question); production/headless deny"
    requirement: "GATE-01"
    verification:
      - kind: unit
        ref: "internal/gateway/approve_test.go (TestApproveHardenedInteractive, TestApproveHardenedLedgerReEntry, TestApproveProductionDenies, TestApproveHeadlessDenies)"
        status: pass
    human_judgment: false
  - id: D3
    description: "execTool surfaces the gateway approve as a NORMAL tool result (Execute withheld, count 0) and executes exactly once after RecordResolvedApproval; TestAskUserOnlyPauseConstraint unchanged"
    requirement: "GATE-01"
    verification:
      - kind: unit
        ref: "internal/agent/llm_agent_retry_gateway_test.go (TestExecToolGatewayApprovalWithheldThenReEnters, TestExecToolGatewayNoApprovalRequiredPaths) — go test -race ./internal/agent/"
        status: pass
    human_judgment: false
  - id: D4
    description: "newGatewayResumeHook records only on accept+gateway_approval (decline/wrong-type/missing-field no-op-or-error, nil->nil hook, chain composition)"
    requirement: "GATE-01"
    verification:
      - kind: unit
        ref: "cmd/aura/gateway_resume_hook_test.go (TestGatewayResumeHook*)"
        status: pass
    human_judgment: false
  - id: D5
    description: "Full persist->LoadManagedHistory->re-Turn round-trip: accept via newGatewayResumeHook -> resume re-emits swarm_spawn from rehydrated history -> fingerprint matches ledger key -> spy executes EXACTLY ONCE, no re-pause"
    requirement: "GATE-01"
    verification:
      - kind: integration
        ref: "cmd/aura/gateway_approval_roundtrip_test.go (TestGatewayApprovalRoundTrip) — go test -race ./cmd/aura/"
        status: pass
    human_judgment: false
  - id: D6
    description: "Live db_integration: resume-driven approved call re-enters Decide -> Allow -> one start (operator_id in Meta) + one end, no competing row, idempotent replay; decline stays fail-closed"
    requirement: "GATE-01"
    verification:
      - kind: integration
        ref: "internal/gateway/gateway_integration_test.go (TestGatewayApprovalResumeReentersAndReservesOnce, TestGatewayApprovalDeclineStaysFailClosed) — go test -tags db_integration -race -p 1 ./internal/gateway/ against live Postgres 127.0.0.1:5432"
        status: pass
    human_judgment: false

# Metrics
duration: 44min
completed: 2026-07-04
status: complete
---

# Phase 35 Plan 06: Interactive Approve Pause/Resume UX (gap-closure) Summary

**GATE-01's `approve` path closed end-to-end by mirroring the shipped shell_exec precedent: a mutating GateRecommended call under `single_user_hardened` now returns a normal approval-required tool RESULT (not a dead-end `error: awaiting user input`) that the model relays via ask_user; the operator's accept is recorded in a cross-turn `GatewayApprovals` ledger by `newGatewayResumeHook`, and the resumed turn re-emits the exact call, matches the recorded args fingerprint, takes the single 35-04 reservation, and executes exactly once — proven by a full persist→resume round-trip AND a live db_integration reservation proof.**

## Performance

- **Duration:** ~44 min (baseline 13:37 → last task commit 14:21 CEST)
- **Tasks:** 3 (all `type=auto`; Tasks 1 & 2 `tdd=true`)
- **Files:** 5 created, 12 modified
- **Environments:** Windows (edits, `go build`/`go vet`, `git commit` + lefthook gofmt/vet/file-size) + WSL (`-race` unit tier + live `db_integration` against the Windows Docker Postgres on `127.0.0.1:5432`)

## Accomplishments

- **`GatewayApprovals` cross-turn ledger** (`internal/gateway/approvals.go`) — the byte-for-byte analog of `ShellApprovals`, storing a `ResolvedApproval` value keyed on `(conversation_id, tool, canonical-args fingerprint)`. `gatewayArgsFingerprint = hex(sha256(canonicaljson.CanonicalArgs(args)))` absorbs cosmetic JSON diffs but not semantic ones.
- **Approval-as-ToolResult** — `gateway.Decide` returns `Verdict{Approve, ApprovalRequest}` (a normal `tools.ToolResult`, no error); `execTool` returns it WITHOUT calling `tool.Execute`. The REAL `swarm_spawn(goals=[…])` call + args land in `conversation_turns`, structurally eliminating the round-trip gap the pre-dispatch pause sentinel could never cross.
- **Production resume hook** — `newGatewayResumeHook` in `serve_adapters.go`, chained into the SAME gateway instance the runner's PEP reads, is the SOLE ledger writer (D-03c). `evictSessionToolState` now evicts the gateway ledger (R-41).
- **Refactor-on-touch** — `Decide`/`routeApprove`/`reserve` all dropped the dead `*tools.ErrAwaitingUserInput` middle return; the gateway no longer mints a pause.

## Task Commits

1. **Task 1: GatewayApprovals ledger + approval-required result + routeApprove Consume re-entry** — `c038fd2b` (feat). Includes the coupled `execTool` call-site update (Task 2 production) because the `Decide (Verdict, error)` signature change forces it to compile — the module-wide `go vet ./...` pre-commit gate forbids a non-compiling intermediate commit (no `--no-verify` allowed).
2. **Task 2: execTool surfaces the gateway approval as a normal tool result** — `392e43eb` (test). The dedicated behavioral proof (`llm_agent_retry_gateway_test.go`); production landed in commit 1.
3. **Task 3: production resume hook + wiring + round-trip & live proofs** — `06204f26` (feat).

_TDD note: for Tasks 1 & 2 the RED→GREEN cycle was run in-loop (e.g. `approvals_test.go` was written first and observed failing on the undefined `GatewayApprovals`/`gatewayArgsFingerprint` symbols, then made green by `approvals.go`). It was NOT committed as a separate RED commit because the lefthook pre-commit `go vet ./...` gate rejects a non-compiling tree and `--no-verify` is prohibited; the GREEN result is the committed unit._

## Files Created/Modified

- `internal/gateway/approvals.go` (created) — `GatewayApprovals` ledger + `gatewayArgsFingerprint`.
- `internal/gateway/gateway.go` — `Verdict.ApprovalRequest`; `Gateway.approvals`; `RecordResolvedApproval` + `EvictSession`.
- `internal/gateway/approve.go` — `gatewayApprovalRequiredResult` + secret-safe `gatewayApprovalQuestion` (arg KEYS only) + `argKeySummary`; `routeApprove` Consume re-entry, takes `rawArgs`, returns `(Verdict, error)`; `args_sha256` added to `gatewayApprovalContext`.
- `internal/gateway/decide.go`, `internal/gateway/reserve.go` — `(Verdict, error)` signatures (dead pause return removed).
- `internal/agent/llm_agent_retry.go` — `case gateway.Approve:` returns `*verdict.ApprovalRequest, nil`. `llm_agent_pause.go` UNCHANGED (git diff verified).
- `cmd/aura/serve_adapters.go` — `newGatewayResumeHook`.
- `cmd/aura/chat_boot.go` — chained `newGatewayResumeHook(gw)`.
- `internal/runner/runner_resume.go` — `evictSessionToolState` → `r.gateway.EvictSession(convID)`.
- Tests: `approvals_test.go`, `approve_test.go`, `decide_test.go`, `reserve_test.go`, `gateway_integration_test.go`, `llm_agent_retry_gateway_test.go`, `gateway_resume_hook_test.go`, `gateway_approval_roundtrip_test.go`.

## Required Disclosures (per plan `<output>`)

- **`internal/agui/approvals_api.go` verified UNCHANGED.** `handleListApprovals` lists every pending from `ListPendingAll` and renders `SanitizeString(p.Question)` with NO filter on ResumeContext type — a `gateway_approval` `Kind=approval` pending flows through `GET /api/approvals` + `POST /api/approvals/{token}/resolve` → `SubmitAnswers` → `applyResumeHook` → `newGatewayResumeHook`, identical in shape to `shell_exec_approval`. The descriptive `gatewayApprovalQuestion` the model copies into ask_user renders non-blank (fixes the checker's WARNING with no `approvals_api.go` change).
- **Live `db_integration` command + result (real rows, NOT skipped):**
  `go test -tags db_integration -race -p 1 -count=1 -run 'GatewayApprovalResume|GatewayApprovalDecline|ApprovedCallReservedAndIdempotent' ./internal/gateway/` (WSL, PATH prepended `/usr/local/go/bin:~/.local/bin:~/go/bin`, `AURA_DB_URL`/`AURA_DB_MIGRATE_URL` composed from `POSTGRES_PASSWORD` against live Postgres `127.0.0.1:5432`) →
  `--- PASS: TestApprovedCallReservedAndIdempotent (0.09s)`, `--- PASS: TestGatewayApprovalResumeReentersAndReservesOnce (0.10s)`, `--- PASS: TestGatewayApprovalDeclineStaysFailClosed (0.08s)`, `ok … 1.317s`. Real rows asserted (`tripleEvents`: exactly one `start` with `operator_id`/`approved` in Meta + one `end`, no competing row).
- **`TestGatewayApprovalRoundTrip` result:** PASS. A real `runner.Runner` over `memConvStore` drove a full persist → `LoadManagedHistory` → re-`Turn` cycle; the scripted model copied the question + resume_context VERBATIM from the observed (untrusted-envelope-wrapped, HTML-escaped) approval-required tool result, the operator accepted via the real `newGatewayResumeHook`, the resume re-emitted `swarm_spawn` from rehydrated history, the re-emit's fingerprint matched the recorded ledger key (Consume hit), and the spy tool executed **exactly once** with no re-pause.
- **≤600-LOC refactor-on-touch:** no file needed a split — the largest touched file is `internal/gateway/gateway_integration_test.go` at 559 LOC; `approve.go` grew but stays well under (the ledger + fingerprint live in `approvals.go` as the plan anticipated). lefthook `file-size` gate passed on every commit.
- **Final GATE-01 status:** the interactive `approve` path is **closed end-to-end**. Under `single_user_hardened` a Risky/Destructive mutating tool yields a resolvable approval (not a permanent `error: awaiting user input`); accept → resume re-enters `Decide` with a matching fingerprint → single reservation → executes once; decline/no-approver/production/headless stay fail-closed. Ready for the REQUIREMENTS.md `GATE-01` (and `GATE-02`) `[x]` flip pending phase re-verification (orchestrator bookkeeping, per the plan's success criteria — intentionally not flipped here).

## Decisions Made

- **Tasks 1 & 2 production committed together (commit `c038fd2b`).** The `Decide (Verdict, error)` signature change (Task 1, `internal/gateway`) breaks `llm_agent_retry.go` (Task 2, `internal/agent`) at compile time; the module-wide `go vet ./...` pre-commit gate forbids a non-compiling intermediate, and `--no-verify` is prohibited. Task 2's dedicated proof test is the separate commit `392e43eb`.
- **The ledger is one-shot on Consume.** Exactly-once execution is guaranteed by the 35-04 RESERVATION, not the ledger. This is correct for security: a consumed approval cannot be reused for a DIFFERENT dispatch (a re-emit with the same args but a new `tool_call_id` after consumption re-pauses, fail-closed). The live idempotency-replay proof re-records the approval for the retry to represent the same still-valid approval; the reservation's `rows==0 → Replay` is what prevents the second Execute.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `reserve.go` + `reserve_test.go` signature change (not in `files_modified` frontmatter)**
- **Found during:** Task 1.
- **Issue:** `Decide` calls `return g.reserve(...)` directly; changing `Decide` to `(Verdict, error)` forces `reserve` to the same signature. `reserve.go`/`reserve_test.go` were not listed in the plan's `files_modified`.
- **Fix:** Dropped the always-nil `*tools.ErrAwaitingUserInput` middle return from `reserve` (refactor-on-touch, removes dead code) and updated the 4 `reserve_test.go` call sites.
- **Verification:** `go test -race ./internal/gateway/` green (unit + db_integration).
- **Committed in:** `c038fd2b`.

**2. [Rule 1 - Bug] Corrected an over-specified fingerprint test assertion**
- **Found during:** Task 1 (TDD GREEN).
- **Issue:** `approvals_test.go` initially asserted `gatewayArgsFingerprint` keeps `2` and `2.0` distinct. `canonicaljson.CanonicalArgs` unmarshals into `any` (coercing numbers through `float64`), so `2` and `2.0` legitimately collapse — desirable cosmetic-insensitivity, not a bug. The test was wrong, not the code.
- **Fix:** Replaced the `2` vs `2.0` case with a changed-scalar-value case (`depth:2` vs `depth:3`), which correctly yields distinct fingerprints.
- **Verification:** `TestGatewayArgsFingerprintCanonicalEquality` green.
- **Committed in:** `c038fd2b`.

---

**Total deviations:** 2 auto-fixed (1 blocking signature refactor, 1 test correction).
**Impact on plan:** Both necessary; no scope creep. The plan's prohibitions (no pre-dispatch intercept, no `approvals_api.go` change, no migration/table/route/env knob, no Phase-36 identity, no model self-approval) were all honored.

## Issues Encountered

- **Round-trip fidelity vs. the untrusted-output envelope.** The agent wraps tool output in a `<tool_output … nonce=…>` envelope AND HTML-escapes the body (`"` → `&#34;`), so the scripted model's raw `json.Unmarshal` of the RoleTool content failed and the turn looped to `max_steps`. Resolved by having the round-trip client `html.UnescapeString` + extract the first balanced JSON object — exactly what a real model does when reading through the envelope. This makes the test MORE faithful (it exercises the real persisted, wrapped, escaped shape rather than a hand-fabricated resume_context).

## Known Stubs

None — every deliverable is wired and proven (unit + round-trip + live).

## Next Phase Readiness

- GATE-01 approve path is closed and re-verification-ready. The `GatewayApprovals` ledger + `newGatewayResumeHook` are the reusable seam Phase 36 (multi-user identity) will extend for per-identity operator attribution (currently `OperatorID="local"`, D-03b).
- No blockers. Working tree note: an unrelated `docs/superpowers/plans/2026-06-29-durable-swarm-messaging.md` modification appeared during the session from an external process; it was left untouched and NOT committed (out of scope).

## Self-Check: PASSED

- Files exist: `internal/gateway/approvals.go`, `internal/gateway/approvals_test.go`, `internal/agent/llm_agent_retry_gateway_test.go`, `cmd/aura/gateway_resume_hook_test.go`, `cmd/aura/gateway_approval_roundtrip_test.go` — all FOUND.
- Commits exist: `c038fd2b`, `392e43eb`, `06204f26` — all FOUND in `git log`.
- `go build ./...` + `go vet ./...` clean; `internal/agent/llm_agent_pause.go` UNCHANGED (git diff since baseline `7f14554b` lists no such file).
- `-race` unit tier (WSL): `internal/gateway` + `internal/agent` + `cmd/aura` + `internal/runner` all `ok`.
- Live `db_integration` tier: 3/3 PASS against real Postgres (not skipped).

---
*Phase: 35-toolgateway-policy-engine*
*Completed: 2026-07-04*
