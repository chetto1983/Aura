---
phase: 45-harness-correctness
plan: 02
subsystem: agent-harness
tags: [idempotency, tracer, harness-correctness, tdd]

# Dependency graph
requires: ["45-01"]
provides:
  - "internal/agent/idempotency_operation.go: deriveToolOperationContext discriminates the child operation key by model round (RoundOrdinal), fails closed via errMissingModelRound when the round is absent"
  - "internal/agent/llm_agent.go: the round is re-pointed onto ic.Ctx immediately before a.dispatch, closing the plumbing gap that kept execTool from ever seeing it"
  - "internal/agent/idempotency_operation_test.go: unit proof that round derivation discriminates/is stable/fails closed/passes through"
  - "internal/gateway/gateway_round_ordinal_integration_test.go + gateway_adversarial_triad_integration_test.go: db_integration SQL proof of SC#1/SC#2 against aura.tool_invocations, including the D-25 adversarial triad"
  - "45-VALIDATION.md: 45-02-T1..T3 rows confirmed green against the real run"
affects: [45-03, 45-04, 45-05, 45-06, 45-07, 45-08]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Round-discriminated idempotency key: fold a per-round ordinal into a FingerprintTyped struct so cross-round re-issue and same-round retry diverge in outcome without a new counter (D-01/D-02)"
    - "Fail-closed context extraction with an explicit sentinel error, mirroring an existing sibling guard in the same function"
    - "Cross-package test mirroring under an import-cycle constraint: internal/gateway cannot import internal/agent, so its db_integration proof reproduces the production key-derivation formula rather than calling it"

key-files:
  created:
    - internal/agent/idempotency_operation_test.go
    - internal/gateway/gateway_round_ordinal_integration_test.go
    - internal/gateway/gateway_adversarial_triad_integration_test.go
  modified:
    - internal/agent/idempotency_operation.go
    - internal/agent/llm_agent.go
    - internal/agent/llm_agent_retry_gateway_test.go
    - internal/gateway/gateway_integration_test.go
    - .planning/phases/45-harness-correctness/45-VALIDATION.md

key-decisions:
  - "gateway_integration_test.go pushed to 805 LOC once the round-discrimination content was added — the CLAUDE.md 600-LOC cap forced an immediate split into two new files (gateway_round_ordinal_integration_test.go, gateway_adversarial_triad_integration_test.go), landing in the SAME RED commit rather than as a follow-up refactor."
  - "The db_integration tests in internal/gateway cannot call internal/agent.deriveToolOperationContext directly — agent already imports gateway, so the reverse import would cycle. They mirror the exact FingerprintTyped struct shape instead, which means they are GREEN both before and after the agent-side fix (self-contained, no production symbol referenced). This is recorded explicitly rather than silently presented as a RED->GREEN gate: the genuinely RED->GREEN evidence is internal/agent/idempotency_operation_test.go, which fails to COMPILE pre-fix."
  - "TestExecToolDerivesStableChildFromHTTPMutation (llm_agent_retry_gateway_test.go) was the one pre-existing test that reached deriveToolOperationContext with a mismatched parent scope and no round on ctx — repaired by seeding withModelRound on its shared base ctx, per the plan's explicit instruction not to relax the fail-closed check."
  - "Two test-design bugs were caught and fixed during Task 2 authoring: gatedArgs() classifies Destructive, which routes through routeApprove's degraded-deny path (writing its OWN end row) instead of reaching Layer A's reserve() — both affected tests were switched to the Normal-tier skillRestoreArgs shape every other reservation test in the package already uses."

patterns-established:
  - "When a package cannot reach the real implementation across an import boundary, its integration proof states this explicitly in a file-level doc comment and mirrors the exact production shape rather than approximating it."

requirements-completed: [HARN-01, HARN-02, HARN-09, ACC-02]

# Metrics
duration: ~85min
completed: 2026-08-13
---

# Phase 45 Plan 02: Round-discriminated child operation key (tracer slice) Summary

**`RoundOrdinal` now lives in the child operation key's fingerprint, threaded onto `ic.Ctx` right before dispatch, fail-closed via `errMissingModelRound` when absent — closing F-1, the headline defect of the milestone, and proven end to end against real `aura.tool_invocations` rows.**

## Performance

- **Duration:** ~85 min (research/context-reading was the majority; implementation + test authoring + fixes ~45 min)
- **Started:** 2026-08-13 (worktree spawn)
- **Completed:** 2026-08-13T16:03:45+02:00 (last commit)
- **Tasks:** 3
- **Files modified:** 8 (3 new, 5 modified)

## Accomplishments

- `deriveToolOperationContext` (`internal/agent/idempotency_operation.go`) adds `RoundOrdinal uint32` to the `FingerprintTyped` struct, bumps `Version` to `"tool-child-v2"`, and reads the round via `modelRoundFromContext` — returning the new `errMissingModelRound` sentinel (fail-closed, mirroring `errUnsupportedParentOperation`'s shape) when absent. Two derivations across different rounds now produce different keys; two derivations at the same round produce the same key.
- `internal/agent/llm_agent.go` gains one line — `ic = ic.WithContext(withModelRound(ic.Ctx, modelRound))` — immediately before `a.dispatch`, closing the plumbing gap where `executeBatch` dispatched on `ic.Ctx` (never carrying the round) while the round only ever lived on `spanCtx`. File measured at 583/600 LOC after the edit (was 579/600 before).
- `internal/agent/idempotency_operation_test.go` (new) proves all four unit behaviors: cross-round discrimination, same-round stability, fail-closed on a missing round, and passthrough when no parent operation (or a same-scope parent) is present — plus the 70-byte key-length bound.
- Two new `db_integration` files prove SC#1/SC#2 against real `aura.tool_invocations` rows: `gateway_round_ordinal_integration_test.go` (cross-round executes twice, same-round retry executes once) and `gateway_adversarial_triad_integration_test.go` (D-25's four paired assertions: same-id retry replays via Layer A, scheduler reclaim executes exactly once across turns, later-round reissue executes twice, and the `reserve.go:233-246` fabricated-success regression re-run against the new key shape).
- One pre-existing test (`TestExecToolDerivesStableChildFromHTTPMutation`) was repaired to seed a round on its shared ctx — the only call site in the affected packages that reached the child-derivation branch with a mismatched parent scope and no round.
- `45-VALIDATION.md`'s Per-Task Verification Map rows `45-02-T1..T3` moved from `pending` to `green`, and the round-ordinal Wave 0 Requirements item is ticked with the RED/GREEN commit hashes recorded.

## Task Commits

Each task was committed atomically, with the TDD RED commit preceding its GREEN implementation commit for Task 1:

1. **Task 1 RED — failing tests for round-discriminated keys** — `26c352bef` (test)
2. **Task 1 GREEN — discriminate child operation keys by model round** — `4728b77f8` (feat)
3. **Task 2 — D-25 adversarial triad (test-only, no source change)** — `c291e58c0` (test)
4. **Task 3 — confirm the phase validation record** — `e7fed026f` (docs)

**Plan metadata:** this SUMMARY.md is committed separately by this agent per worktree-mode convention; STATE.md/ROADMAP.md tracking is owned by the orchestrator.

## Files Created/Modified

- `internal/agent/idempotency_operation.go` — `errMissingModelRound` sentinel; `RoundOrdinal uint32` field (`json:"round_ordinal"`) in the child `FingerprintTyped` struct; `Version` `"tool-child-v1"` → `"tool-child-v2"`; the round is read after the parent-scope switch and before `tools.OperationFingerprint`, fail-closed on `!ok`.
- `internal/agent/llm_agent.go` — one new statement re-pointing `ic` through `withModelRound` immediately before `a.dispatch` (no new symbol, no signature change on the dispatch chain). 583/600 LOC.
- `internal/agent/idempotency_operation_test.go` (new) — `TestDeriveToolOperationContextDiscriminatesRounds`, `TestDeriveToolOperationContextIsStableWithinARound`, `TestDeriveToolOperationContextFailsClosedWithoutRound`, `TestDeriveToolOperationContextPassesThroughWithoutParent`.
- `internal/agent/llm_agent_retry_gateway_test.go` — `TestExecToolDerivesStableChildFromHTTPMutation`'s shared `base` ctx now seeds `withModelRound` before both `execTool` calls (fail-closed fallout repair).
- `internal/gateway/gateway_integration_test.go` — trimmed back to 570 LOC (the round-discrimination content that pushed it to 805 was split out) plus a one-line pointer to the new file.
- `internal/gateway/gateway_round_ordinal_integration_test.go` (new, 256 LOC) — `deriveChildKeyForRound`, `operationCtx`, `endRowCount`, `gatedExecWithOperationLifecycle` helpers; `TestRoundOrdinalCrossRoundReissueExecutesTwice`, `TestRoundOrdinalSameRoundRetryExecutesOnce`.
- `internal/gateway/gateway_adversarial_triad_integration_test.go` (new, 279 LOC) — `endRowCountForCall` helper; `TestSameToolCallIDRetryReplaysViaLayerA`, `TestSchedulerReclaimExecutesExactlyOnceAcrossTurns`, `TestLaterRoundReissueWithIdenticalArgumentsExecutesTwice`, `TestUnaccountedPriorDispatchStaysDeniedNeverFabricated`.
- `.planning/phases/45-harness-correctness/45-VALIDATION.md` — Per-Task Verification Map rows `45-02-T1..T3` set to green with commit references; the round-ordinal Wave 0 Requirements bullet ticked; the pre-flight-grep bullet's re-run confirmation recorded.

## Decisions Made

- **RED failure output, recorded verbatim.** `go test -run 'TestDeriveToolOperationContext' ./internal/agent/` against the unmodified source: `internal\agent\idempotency_operation_test.go:150:21: undefined: errMissingModelRound` / `FAIL github.com/chetto1983/aura/internal/agent [build failed]`. This is the genuine RED→GREEN gate for this plan.
- **The db_integration tests are GREEN before and after the fix, by construction — documented, not hidden.** `internal/gateway` cannot import `internal/agent` (the reverse would cycle, since `agent` already imports `gateway`), so `gateway_round_ordinal_integration_test.go` and `gateway_adversarial_triad_integration_test.go` reproduce the exact `FingerprintTyped` struct shape (`Version: "tool-child-v2"`, `RoundOrdinal`) rather than calling the real production function. Verified empirically: run against the scratch DB with the OLD (pre-Task-1) source tree, both `TestRoundOrdinalCrossRoundReissueExecutesTwice` and `TestRoundOrdinalSameRoundRetryExecutesOnce` already PASS — they prove Layer B's registry mechanics are correct GIVEN round-discriminated keys, not that production code derives them. `internal/agent/idempotency_operation_test.go`'s compile failure is what actually gates the fix.
- **Pre-flight grep re-run (RESEARCH.md open question 2), Task 1 step:** `grep -rn "tool-child-v1|FingerprintTyped" internal/ --include=*_test.go` reproduced the planning-time answer exactly — 8 `FingerprintTyped` occurrences, all building parent structs, zero `"tool-child-v1"` matches. No pinning test needed updating.
- **`wc -l internal/agent/llm_agent.go` = 583** after the edit (579 before), well inside the 600-LOC cap and inside the 21-LOC headroom the pattern map flagged at planning time. No companion split was needed.
- **`gateway_integration_test.go` file-size split landed inside the RED commit, not as a follow-up.** The lefthook `file-size` pre-commit gate rejected the first commit attempt at 805 LOC; the round-discrimination content was split into `gateway_round_ordinal_integration_test.go` (256 LOC) before the RED commit succeeded, keeping every file under the cap from the first landed commit.
- **A stale `golangci-lint` cache blocked the first RED commit attempt for an unrelated reason.** The cache carried phantom findings attributed to a sibling worktree (`agent-a360bf6cb15c021f8`, merged and removed during plan 45-01's wave) that no longer exists on disk — `golangci-lint cache clean` resolved it; `golangci-lint run` over the changed packages alone had already reported 0 issues, confirming the failure was environmental, not code-related.
- **Two test-design bugs in Task 2, caught before commit, not left as false green:** `TestSameToolCallIDRetryReplaysViaLayerA` and `TestUnaccountedPriorDispatchStaysDeniedNeverFabricated` initially used `gatedArgs()` (Destructive tier), which — without a responder on ctx — routes through `routeApprove`'s "no interactive approver" degraded-deny path and writes its OWN `end` row (`Meta.degraded_deny: true`) without ever reaching Layer A's `reserve()`. This corrupted the SQL evidence (`TestUnaccountedPriorDispatchStaysDeniedNeverFabricated` initially reported 1 unexpected `end` row where it expected 0). Both tests were switched to the Normal-tier `skillRestoreArgs` shape every other reservation test in the file already uses.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `gateway_integration_test.go` exceeded the 600-LOC file-size cap**
- **Found during:** first attempt to commit Task 1's RED phase.
- **Issue:** adding the round-discrimination proof pushed the file to 805 LOC; lefthook's `file-size` pre-commit hook rejected the commit.
- **Fix:** split the new content into a dedicated file (`gateway_round_ordinal_integration_test.go`, 256 LOC), leaving `gateway_integration_test.go` at 570 LOC and pointing to the new file with a one-line comment.
- **Files modified:** `internal/gateway/gateway_integration_test.go`, `internal/gateway/gateway_round_ordinal_integration_test.go`.
- **Commit:** `26c352bef` (landed inside the RED commit, not a follow-up).

**2. [Rule 3 - Blocking] Stale `golangci-lint` cache blocked the RED commit for an unrelated reason**
- **Found during:** second attempt to commit Task 1's RED phase.
- **Issue:** `golangci-lint run $(bash scripts/go_packages.sh)` reported 62 issues attributed to paths under a sibling worktree (`..\agent-a360bf6cb15c021f8\...`) that no longer exists on disk (the worktree was merged and removed during plan 45-01's wave).
- **Fix:** `golangci-lint cache clean`, then re-ran — 0 issues. Confirmed by first running `golangci-lint run internal/agent/... internal/gateway/...` directly (my own changed packages only), which already reported 0 issues, isolating the failure to stale cache rather than code.
- **Files modified:** none (environment-only fix).
- **Commit:** N/A (pre-commit hook fix, not a source change).

**3. [Rule 1 - Bug] `TestExecToolDerivesStableChildFromHTTPMutation` failed closed as designed, needed a seeded round**
- **Found during:** `go test ./internal/agent/...` after the GREEN implementation edit.
- **Issue:** the test builds a parent operation of a different scope (`ScopeHTTPMutation`) with no `modelRound` on its shared `base` ctx — exactly the plan's documented "known candidate" for D-04 fallout.
- **Fix:** seeded `base = withModelRound(base, modelRound{requestID: uuid.New(), ordinal: 1})` before both `execTool` calls, per the plan's explicit instruction not to relax the fail-closed check.
- **Files modified:** `internal/agent/llm_agent_retry_gateway_test.go`.
- **Commit:** `4728b77f8`.

**4. [Rule 1 - Bug] Two Task 2 test-design bugs (wrong risk tier) corrupted SQL evidence**
- **Found during:** first run of `TestSameToolCallIDRetryReplaysViaLayerA` and `TestUnaccountedPriorDispatchStaysDeniedNeverFabricated`.
- **Issue:** `gatedArgs()` classifies Destructive; without a responder, the second dispatch attempt routed through `routeApprove`'s degraded-deny path instead of Layer A's `reserve()`, writing an unexpected `end` row (`Meta.degraded_deny: true`) and producing a false "verdict = Deny" pass for the wrong reason in the first test, and a genuine SQL-count mismatch (1 vs expected 0) in the second.
- **Fix:** switched both tests to `tools.Spec{Name: "skill_manage", Mutating: true}` + `skillRestoreArgs` (Normal tier, auto-allow) — the same shape every other reservation test in the package already uses.
- **Files modified:** `internal/gateway/gateway_adversarial_triad_integration_test.go`.
- **Commit:** `c291e58c0` (caught and fixed before this commit, not left as a known-bad test).

**5. [Rule 2 - Missing functionality] Round-scoped test data collision across reruns**
- **Found during:** first WSL `-race` run of `TestRoundOrdinalCrossRoundReissueExecutesTwice`/`TestRoundOrdinalSameRoundRetryExecutesOnce`.
- **Issue:** the tests initially used a fixed literal parent operation key; the idempotency operation registry is scoped by `(identity, scope, key)`, not by conversation, so re-running the same test against the same scratch DB collided with the completed operation left behind by the prior run.
- **Fix:** made the parent key per-run-unique (`"..." + uuid.Must(uuid.NewV7()).String()`), matching how every other fixture in this file already mints a fresh `conversation_id`.
- **Files modified:** `internal/gateway/gateway_round_ordinal_integration_test.go`.
- **Commit:** `26c352bef`.

## Issues Encountered

None beyond the auto-fixed items above; all were resolved within this plan's scope.

## User Setup Required

None — no external service configuration required. A disposable scratch Postgres database (`aura_test_4502`, owned by `aura_migrate`) was created and dropped by this agent for local `db_integration` verification; the live `aura` database was never touched.

## TDD Gate Compliance

- **RED gate:** `test(45-02): add failing tests for round-discriminated child operation keys` — `26c352bef`. Verified: `go build ./internal/agent/...` fails with `undefined: errMissingModelRound` against the unmodified source.
- **GREEN gate:** `feat(45-02): discriminate child operation keys by model round (D-01/D-04)` — `4728b77f8`, landing after the RED commit.
- **Note on Task 2:** `test(45-02): D-25 adversarial triad...` — `c291e58c0` — is test-only by design (the plan's own action explicitly forbids touching `reserve.go`/`reserve_test.go`); it validates behavior Task 1's GREEN commit already shipped, so no separate feat commit exists for it. This is not a gap: the plan's Task 2 has no `<behavior>` requiring new production code.

## Next Phase Readiness

- SC#1 and SC#2 are proved at the `db_integration` tier the evidence map names (real `aura.tool_invocations` SQL, not log reading), and green before any plan in wave 3 starts.
- A missing round ordinal is a loud error (`errMissingModelRound`); no test was repaired by relaxing that guard — every fallout site was repaired by seeding a round instead (D-04's explicit requirement).
- `tool_call_id` is confirmed absent from the child key struct (`RoundOrdinal`/`ParentScope`/`ParentKey`/`ParentFingerprint`/`ToolScope`/`ToolFingerprint`/`Version` only) — the locked D-01 decision holds.
- The deploy hazard (D-06: drain the scheduler before deploying, since pre-deploy `tool-child-v1` keys become unreachable) is recorded in `45-VALIDATION.md` and carries into plan 45-08's live-run gate.
- Plans 45-03 through 45-08 can build on the round-discriminated key shape; `reserve.go` and `reserve_test.go` remain untouched and available for plan 45-03.

---
*Phase: 45-harness-correctness*
*Completed: 2026-08-13*

## Self-Check: PASSED

- FOUND: internal/agent/idempotency_operation_test.go
- FOUND: internal/gateway/gateway_round_ordinal_integration_test.go
- FOUND: internal/gateway/gateway_adversarial_triad_integration_test.go
- FOUND: .planning/phases/45-harness-correctness/45-02-SUMMARY.md
- FOUND: 26c352bef (Task 1 RED commit)
- FOUND: 4728b77f8 (Task 1 GREEN commit)
- FOUND: c291e58c0 (Task 2 commit)
- FOUND: e7fed026f (Task 3 commit)
