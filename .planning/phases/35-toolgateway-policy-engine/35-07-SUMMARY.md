---
phase: 35-toolgateway-policy-engine
plan: 07
subsystem: security
tags: [gateway, policy-engine, informed-consent, confused-deputy, approval, challenge-response, go, postgres]

# Dependency graph
requires:
  - phase: 35-06
    provides: "GatewayApprovals cross-turn ledger + shell_exec-style approval-required ToolResult + newGatewayResumeHook + routeApprove Consume-on-resume (the approve pause/resume UX this plan hardens)"
  - phase: 35-04
    provides: "the single durable reservation (Store.Reserve rows-affected idempotency) the approved re-emit converges on"
provides:
  - "GatewayApprovals server-side pending-challenge map + Challenge/ApproveChallenge (byte-for-byte analog of ShellApprovals) — the informed-consent binding"
  - "routeApprove deny-before-Consume ordering + challenge-record-on-issue + single-fp threading"
  - "Gateway.ApproveChallenge host-side recorder with ProfileServerProduction refuse-guard (defense in depth)"
  - "newGatewayResumeHook challenge+question gated on the authenticated pending.ConversationID"
  - "the machine-checkable adversarial mismatched-question negative test (WR-03)"
affects: [36-multi-user-identity-isolation, gateway, approval-center, security-audit]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Challenge/response consent binding: a host-issued server-side challenge (question keyed on the authenticated (conversation_id, tool, args_sha256)) + an ApproveChallenge existence+question-match guard, ported verbatim from internal/agent/tools/shell_approval.go"
    - "Deny-before-Consume ordering: the fail-closed hard-deny (production || no-responder) precedes any cross-turn approval Consume, so a recorded/fabricated approval can never elevate a headless/production run"
    - "Single-computation threading: fingerprint+question computed once in routeApprove and passed into recorder + result builder + resume-context (no recompute)"

key-files:
  created: []
  modified:
    - "internal/gateway/approvals.go — pending map + gatewayChallenge{question} + Challenge + ApproveChallenge; Evict sweeps both maps; Peek removed"
    - "internal/gateway/approve.go — routeApprove deny-before-Consume reorder + approvals.Challenge on issue + single fp/question threaded into gatewayApprovalRequiredResult/gatewayApprovalContext"
    - "internal/gateway/gateway.go — Gateway.ApproveChallenge (production refuse-guard + delegate); RecordResolvedApproval gains the production refuse-guard"
    - "cmd/aura/serve_adapters.go — newGatewayResumeHook calls g.ApproveChallenge(pending.ConversationID, rc.Tool, rc.ArgsSHA256, pending.Question, ...)"
    - "internal/gateway/approvals_test.go — question-mismatch/not-found/challenge/evict-both tests; Peek test removed"
    - "internal/gateway/approve_test.go — challenge-on-issue + WR-01 production-deny + ApproveChallenge-refused tests"
    - "cmd/aura/gateway_resume_hook_test.go — TestGatewayResumeHookRejectsMismatchedQuestion (WR-03 adversarial)"
    - "internal/gateway/gateway_integration_test.go — WR-01 consequence: resume re-drives modeled with WithResponder (DEVIATION, 8th file)"

key-decisions:
  - "Ported shell_exec's challenge/question binding verbatim rather than inventing a new mechanism — making the 35-06 'byte-for-byte analog of shell_exec' claim finally TRUE (CR-01)"
  - "Deny-before-Consume covers BOTH production AND no-responder (not just production), so a headless hardened swarm/cron child can never consume a cross-turn approval (WR-01)"
  - "The resume hook keys on the AUTHENTICATED pending.ConversationID unconditionally; the model-relayed rc.ConversationID field is dropped entirely (WR-02)"
  - "Gateway.ApproveChallenge AND RecordResolvedApproval both refuse under ProfileServerProduction (defense in depth), so a production run records no approval by any path"

patterns-established:
  - "Informed-consent challenge binding for host-mediated approvals (gateway parity with shell_exec)"
  - "Adversarial negative test as the machine-check for an authorization boundary (WR-03)"

requirements-completed: [GATE-01]

coverage:
  - id: D1
    description: "GatewayApprovals records a server-side challenge and moves pending→approved ONLY on existence + operator-visible question match (CR-01 ledger half); a benign/false relayed question or a missing challenge records nothing"
    requirement: "GATE-01"
    verification:
      - kind: unit
        ref: "internal/gateway/approvals_test.go#TestGatewayApprovalsApproveChallengeQuestionMismatch"
        status: pass
      - kind: unit
        ref: "internal/gateway/approvals_test.go#TestGatewayApprovalsChallengeApproveConsume / TestGatewayApprovalsApproveChallengeNotFound / TestGatewayApprovalsEvictPrefixSweep"
        status: pass
    human_judgment: false
  - id: D2
    description: "routeApprove records the challenge on issue keyed on the authenticated triple, evaluates the production/headless hard-deny BEFORE any Consume (WR-01), and refuses to record under server_production; fingerprint computed once (IN-02)"
    requirement: "GATE-01"
    verification:
      - kind: unit
        ref: "internal/gateway/approve_test.go#TestApproveHardenedRecordsChallengeOnIssue / TestRouteApproveProductionDeniesEvenWithLedgerApproval / TestGatewayApproveChallengeRefusedUnderProduction"
        status: pass
    human_judgment: false
  - id: D3
    description: "newGatewayResumeHook records an operator accept ONLY when the gateway issued a challenge for the authenticated pending.ConversationID (WR-02) AND the operator-visible question matches (CR-01); an adversarial benign-question relay is rejected (WR-03)"
    requirement: "GATE-01"
    verification:
      - kind: unit
        ref: "cmd/aura/gateway_resume_hook_test.go#TestGatewayResumeHookRejectsMismatchedQuestion"
        status: pass
      - kind: integration
        ref: "go test -tags db_integration -race -p 1 ./internal/gateway -run TestGatewayApprovalResumeReentersAndReservesOnce"
        status: pass
    human_judgment: false
  - id: D4
    description: "ZERO REGRESSION: the live reserve/idempotency db_integration tier stays green (single reservation with operator_id in Meta, idempotent replay, decline fail-closed)"
    verification:
      - kind: integration
        ref: "go test -tags db_integration -race -p 1 ./internal/gateway -run 'GatewayApprovalResume|GatewayApprovalDecline|ApprovedCallReservedAndIdempotent'"
        status: pass
    human_judgment: false
  - id: D5
    description: "ZERO REGRESSION: the cooperative persist→resume round-trip (model copies question+resume_context verbatim) still executes the mutating tool exactly once; the ask_user pause detector is untouched"
    verification:
      - kind: e2e
        ref: "cmd/aura/gateway_approval_roundtrip_test.go#TestGatewayApprovalRoundTrip"
        status: pass
      - kind: unit
        ref: "internal/agent/...#TestAskUserOnlyPauseConstraint"
        status: pass
    human_judgment: false

# Metrics
duration: ~60min
completed: 2026-07-04
status: complete
---

# Phase 35 Plan 07: CR-01 Informed-Consent Binding Summary

**Ported shell_exec's server-side challenge/question guard into GatewayApprovals so a gateway approval moves pending→approved ONLY when the gateway issued a challenge for the authenticated (conversation_id, tool, args_sha256) AND the operator-visible question matches — closing the confused-deputy / informed-consent bypass (CR-01) that let a model relay a benign question while authorizing a destructive action.**

## Performance

- **Duration:** ~60 min
- **Completed:** 2026-07-04
- **Tasks:** 3 (all `tdd=true`, test-first; committed atomically at GREEN)
- **Files modified:** 8 (7 planned + 1 deviation)

## Accomplishments
- **CR-01 CLOSED end-to-end.** The "byte-for-byte analog of shell_exec" claim is now TRUE: `GatewayApprovals` holds a `pending map[string]gatewayChallenge{question}`; `routeApprove` records the gateway-generated question keyed on the authenticated triple at issue-time; `newGatewayResumeHook` records the operator accept ONLY via `g.ApproveChallenge(...)` which requires the challenge to exist AND `pending.Question == challenge.question`.
- **WR-01 CLOSED.** `routeApprove` evaluates the `ProfileServerProduction || !responderPresent` hard-deny BEFORE any cross-turn `Consume` (deny-before-Consume), and both `Gateway.ApproveChallenge` and `RecordResolvedApproval` refuse to record under `ProfileServerProduction` (defense in depth).
- **WR-02 CLOSED.** The hook keys on the AUTHENTICATED `pending.ConversationID` (server-stored at pause creation); the model-relayed `rc.ConversationID` field is dropped — no cross-conversation approval transfer.
- **WR-03 CLOSED.** `TestGatewayResumeHookRejectsMismatchedQuestion` drives a real challenge, relays the REAL `resume_context` with a benign question, and asserts the hook errors + the re-drive stays Approve — the machine-check the cooperative round-trip never exercised.
- **IN-01 / IN-02 folded.** Dead `Peek` removed (ShellApprovals has none); `gatewayArgsFingerprint` now has a single call site (fp+question threaded from `routeApprove`).

## CR-01 → mitigation → test trace

1. **Challenge recorded on issue** — `routeApprove` (approve.go) calls `g.approvals.Challenge(key.ConversationID, spec.Name, fp, question)` the instant it issues the approval-required result. Proven by `TestApproveHardenedRecordsChallengeOnIssue` (a pending challenge exists for the key, `challenge.question == preview.question`).
2. **Hook question-match** — `newGatewayResumeHook` (serve_adapters.go) records via `g.ApproveChallenge(pending.ConversationID, rc.Tool, rc.ArgsSHA256, pending.Question, ...)`; `GatewayApprovals.ApproveChallenge` moves pending→approved ONLY on existence + `question == challenge.question`. Proven by `TestGatewayApprovalsApproveChallengeQuestionMismatch` + the cooperative `TestGatewayApprovalRoundTrip` (verbatim question still records + executes once).
3. **Adversarial mismatch rejected** — `TestGatewayResumeHookRejectsMismatchedQuestion` (WR-03): REAL `resume_context` (fp matches) + benign `pending.Question="Save your meeting notes?"` → hook returns a `question mismatch` error, records NOTHING, and the re-drive stays `Approve` (withheld). The operator can never be shown a question that differs from the action being authorized.

## Disposition table

| ID | Disposition | Proving test(s) |
|----|-------------|-----------------|
| CR-01 (HIGH, anchor) | mitigate | `TestGatewayResumeHookRejectsMismatchedQuestion` + `TestGatewayApprovalsApproveChallengeQuestionMismatch` + `TestApproveHardenedRecordsChallengeOnIssue`; cooperative `TestGatewayApprovalRoundTrip` still passes |
| WR-01 | mitigate | `TestRouteApproveProductionDeniesEvenWithLedgerApproval` (injected ledger approval NOT consumed under production, `reserves()==0`) + `TestGatewayApproveChallengeRefusedUnderProduction` |
| WR-02 | mitigate | hook source keys on `pending.ConversationID`; exercised by `TestGatewayResumeHookRecordsAcceptedApproval` / `TestGatewayResumeHookInChain` |
| WR-03 | mitigate | `TestGatewayResumeHookRejectsMismatchedQuestion` |
| IN-01 | fixed | `Peek` removed (grep 0 hits in approvals.go); `ShellApprovals` has none |
| IN-02 | fixed | `gatewayArgsFingerprint(` single call site in approve.go (grep count 1) |

## Task Commits

Each task was test-first (RED verified locally, then GREEN) and committed atomically. Pre-commit hooks (gofmt, `go vet ./...`, file-size) ran green on every commit (no `--no-verify`).

1. **Task 1: challenge/question binding into GatewayApprovals + IN-01** — `e2c9f6c1` (feat)
2. **Task 2: routeApprove challenge-on-issue + deny-before-Consume + Gateway.ApproveChallenge + IN-02** — `f257d9f4` (feat)
3. **Task 3: hook rewire to authenticated conv + question (WR-02/CR-01) + WR-03 test + WR-01 integration-test correction** — `e75f5bf6` (fix)

**Plan metadata:** this commit (docs: complete plan)

## Files Created/Modified
- `internal/gateway/approvals.go` (180 LOC) — `pending` map + `gatewayChallenge` + `Challenge` + `ApproveChallenge`; `Evict` sweeps both maps; `Peek` removed
- `internal/gateway/approve.go` (242 LOC) — deny-before-Consume reorder + `approvals.Challenge` on issue + single fp/question threaded into `gatewayApprovalRequiredResult`/`gatewayApprovalContext`
- `internal/gateway/gateway.go` (161 LOC) — `Gateway.ApproveChallenge` + production refuse-guard on both recorders
- `cmd/aura/serve_adapters.go` (484 LOC) — `newGatewayResumeHook` → `g.ApproveChallenge(pending.ConversationID, rc.Tool, rc.ArgsSHA256, pending.Question, ...)`
- `internal/gateway/approvals_test.go` (167 LOC) — challenge/mismatch/not-found/evict-both tests; Peek test removed
- `internal/gateway/approve_test.go` (297 LOC) — challenge-on-issue + WR-01 production-deny + refuse-under-production tests
- `cmd/aura/gateway_resume_hook_test.go` (192 LOC) — WR-03 adversarial `TestGatewayResumeHookRejectsMismatchedQuestion`
- `internal/gateway/gateway_integration_test.go` — resume re-drives modeled with `WithResponder` (deviation, see below)

All 8 touched files ≤600 LOC (max: serve_adapters.go 484).

## Prohibitions honored (git diff verified)

- **6 forbidden files byte-UNCHANGED** (`git diff HEAD~3 -- <file>` empty): `internal/agui/approvals_api.go`, `internal/runner/runner_resume.go`, `internal/gateway/decide.go`, `internal/gateway/reserve.go`, `internal/agent/llm_agent_pause.go`, `internal/agent/llm_agent_retry.go`.
- **No new migration / HTTP route / env var** (grep of the diff: no `*.sql`, `AURA_*`, or route registration).
- **Ledger key unchanged** — `gatewayApprovalKey(convID, toolName, argsFingerprint)` still excludes `tool_call_id`; the new challenge map reuses the SAME key.
- **No pre-dispatch intercept, no model self-approval** — `llm_agent_pause.go`/`llm_agent_retry.go` untouched; `GatewayApprovals` is written only by the host-side hook via the challenge-gated `ApproveChallenge`.

## Validation results

| Tier | Command | Result |
|------|---------|--------|
| vet (repo-wide) | `go vet ./...` | clean |
| build (repo-wide) | `go build ./...` | clean |
| unit (native, touched pkgs) | `go test ./internal/gateway/ ./cmd/aura/` | ok |
| unit `-race` matrix (WSL) | `go test -race ./internal/gateway/ ./internal/agent/ ./cmd/aura/ ./internal/runner/ -run 'Gateway\|Approve\|Resume\|RoundTrip\|Challenge\|Mismatch\|Chain\|Evict\|AskUserOnly\|ExecTool\|Pause'` | all 4 pkgs ok |
| **live db_integration** (WSL, real Postgres stack on 127.0.0.1, `aura_app`/`aura_migrate` roles, migrate no-op at schema v25) | `go test -tags db_integration -race -p 1 ./internal/gateway/ -run 'GatewayApprovalResume\|GatewayApprovalDecline\|ApprovedCallReservedAndIdempotent' -count=1 -v` | **3/3 PASS (real rows, ~0.09s each, 1.298s total — not skipped)** |
| live db_integration (full gateway tier) | `go test -tags db_integration -race -p 1 ./internal/gateway/ -count=1` | ok (2.153s) |

**Coverage:** `internal/gateway` **89.4%** (db_integration-tagged) / 89.0% (untagged) — above the CLAUDE.md ≥85% owned-surface floor. `cmd/aura` 39.9% (untagged) — glue, EXCLUDED from the coverage gate per CLAUDE.md; its gateway-hook logic is behaviorally covered by the passing `TestGatewayResumeHook*` + `TestGatewayApprovalRoundTrip` tests. The full `make coverage` owned-surface gate additionally requires the `neo4j_integration` tier's `mcp-neo4j-cypher`, which is not host-installed in this WSL (project policy: MCP is containerized, not host-installed) — that phase-level gate defers to the verifier; this plan touches only `internal/gateway` (89.4% > 85%) and the gate-excluded `cmd/aura`, so the owned-surface floor is not regressed.

## Decisions Made
- Ported the shell precedent verbatim (challenge map + existence + question-match) rather than inventing a new shape — the minimal, review-mandated fix.
- Deny-before-Consume gates BOTH production AND no-responder (the plan's `ProfileServerProduction || !responderPresent` order), plus a record-time production refuse-guard on both recorders (defense in depth).
- Dropped the model-relayed `rc.ConversationID` field entirely (WR-02) rather than keeping it for a defensive equality assertion — a smaller, unambiguous hook body.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Corrected TestGatewayApprovalResumeReentersAndReservesOnce to model the interactive responder**
- **Found during:** Task 3 (full-matrix db_integration validation).
- **Issue:** The WR-01 deny-before-Consume reorder (Task 2, mandated by the plan) makes a headless (no-responder) re-drive with a recorded approval correctly DENY. `TestGatewayApprovalResumeReentersAndReservesOnce` (in `internal/gateway/gateway_integration_test.go` — NOT one of the plan's 7 `files_modified`) re-drove the resumed call with a bare `context.Background()`, so it now denied ("no interactive approver") instead of Allowing via the ledger. As written it asserted the now-forbidden headless-consume behavior.
- **Fix:** Changed the two resume re-drives to `WithResponder(context.Background())`, modeling the REAL interactive resume — the runner marks every interactive turn (including the resume) with `gateway.WithResponder` (runner.go:551). This is how production works; the cooperative end-to-end `TestGatewayApprovalRoundTrip` already passed because it drives the real runner (which sets the responder).
- **Files modified:** `internal/gateway/gateway_integration_test.go` (test-only; no production surface, SQL, route, or env changed).
- **Verification:** the 3-test tier and the full gateway db_integration tier pass live (`-race -p 1`, real Postgres).
- **Committed in:** `e75f5bf6` (Task 3 commit).

---

**Total deviations:** 1 auto-fixed (1 Rule-1 test correction, an 8th file beyond the plan's 7).
**Impact on plan:** The deviation is a direct, unavoidable consequence of the WR-01 security reorder the plan explicitly requires; the plan's "these db_integration tests stay green unmodified" assumption did not anticipate that this test's re-drive omitted the responder the real runner sets. The correction makes the test model production. No scope creep, no production behavior changed by it.

## Issues Encountered
- **Shared dev DB migration-tracking conflict.** The initial db_integration run used the `aura` superuser as the migrate role (my env-derivation error), which tried to re-apply migrations against the already-migrated shared `aura` DB and briefly dirtied `public.schema_migrations`. Root cause: the repo `.env` uses only `POSTGRES_USER=aura`/`POSTGRES_PASSWORD` (no `aura_app`/`aura_migrate` DSNs — those live in the separate D:\Aura-WSL clone). Resolved by deriving the DSNs with the correct `aura_app` (app) / `aura_migrate` (migrate) roles per the harness contract (`db.EnsureRoles` idempotently ALTERs their password to `POSTGRES_PASSWORD`; `db.Migrate` treats `ErrNoChange` at schema v25 as a clean no-op). The tracker is now `25|f` (clean, fully migrated) and all tiers pass. No production code involved.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- GATE-01's interactive-approval informed-consent property holds end-to-end (challenge recorded on issue → hook existence+question-match → adversarial mismatch rejected), machine-checked by WR-03, with zero regression to the 6/6 mechanics and the live reserve/idempotency tier.
- Ready for phase re-verification + the code-review gate re-run. On pass, the orchestrator flips GATE-01 (and GATE-02) to `[x]` in REQUIREMENTS.md and closes Phase 35.
- No blockers introduced. Phase 36 (multi-user identity isolation) inherits the consent-bound approval path unchanged.

## Self-Check: PASSED
- All 8 touched source/test files exist and are committed (`e2c9f6c1`, `f257d9f4`, `e75f5bf6` present in `git log`).
- The 3 task commits + this metadata commit account for every change; the 6 prohibition files are byte-unchanged (git diff verified).
- Unit `-race` (4 packages) + live db_integration tiers ran green (real DB rows, not skipped).

---
*Phase: 35-toolgateway-policy-engine*
*Completed: 2026-07-04*
