---
phase: 35-toolgateway-policy-engine
plan: 02
subsystem: testing
tags: [command-hook, fail-closed, fail-open, hookFault, gate-02, security, go]

# Dependency graph
requires:
  - phase: 35-01
    provides: classifier substrate + Mutating floor + gateway boot-guard (Wave 1 sibling)
provides:
  - Locked GATE-02: timeout / crashed-allow / unparseable-crash of a command hook all DENY under the default (unset) fail policy, and the gated tool never executes (no silent-allow path)
  - Explicit fail_open contained-allow proof — the per-hook opt-in is real, not a silent allow of a denied command
  - Coverage pin on BOTH hookFault branches (FailClosed abort / FailOpen contain) via one faulting fixture (F-006 regression guard)
affects: [35-03 gateway Decide PEP, 38 MCP governance hardening]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "GATE-02 test-through-the-real-manager: build a HookManager via CommandHookManagerFromEnv (operator path), drive BeforeTool through a gateTool helper that mirrors llm_agent_dispatch.go, and use a spyTool to assert Execute is never reached on a denied turn"
    - "One-fixture-two-branches: a single timing-out command hook exercises both hookFault sides (default→FailClosed deny, explicit fail_open→FailOpen contain) to pin the fail-closed default against a widened-containment regression"

key-files:
  created: []
  modified:
    - internal/agent/hooks_policy_test.go - added the fail_open contained-allow + both-branches + no-silent-allow matrix (Task 2)
    - internal/agent/hooks_command_hardening_test.go - timeout/crash/non-zero → DENY matrix through HookManager (Task 1, prior crash-salvaged commit)
    - internal/agent/hooks_command_test.go - helper modes allow_then_crash / crash_no_decision (Task 1, prior commit)
    - internal/agent/hooks_command_policy_internal_test.go - policy-resolution anti-silent-allow pins (Task 1, prior commit)

key-decisions:
  - "VERIFY-ONLY (D-04): commandHookFailPolicy already defaults empty/unset→FailClosed and honors an explicit fail_open; no production hook code changed, no new AURA_* env knob added"
  - "Reused the crash-salvaged ~71-line task-2 patch verbatim after confirming it aligned with the real task-1 helpers (newCommandHookManager / gateTool / spyTool) and helper modes"

patterns-established:
  - "Fail-closed proof pattern: assert both (err != nil) AND (spy tool Execute never ran) in one gateTool call so a deny and a no-execute are proven together"

requirements-completed: [GATE-02]

coverage:
  - id: D1
    description: "A command hook that times out DENIES the turn under the default fail-closed policy and the gated tool never executes"
    requirement: GATE-02
    verification:
      - kind: unit
        ref: "internal/agent/hooks_command_hardening_test.go#TestCommandHook_TimeoutDeniesTurnToolNeverExecutes"
        status: pass
    human_judgment: false
  - id: D2
    description: "A crashed-allow / non-zero-exit-with-no-parseable-decision DENIES (cannot silently allow a denied command)"
    requirement: GATE-02
    verification:
      - kind: unit
        ref: "internal/agent/hooks_command_hardening_test.go#TestCommandHook_CrashAllowDeniesTurnToolNeverExecutes / TestCommandHook_CrashNoDecisionDeniesTurnToolNeverExecutes"
        status: pass
      - kind: unit
        ref: "internal/agent/hooks_policy_test.go#TestCommandHookDefaultPolicyNeverSilentAllows"
        status: pass
    human_judgment: false
  - id: D3
    description: "A crashed before_model rewrite payload is rejected through the manager (AG-030)"
    requirement: GATE-02
    verification:
      - kind: unit
        ref: "internal/agent/hooks_command_hardening_test.go#TestCommandHook_CrashRewriteDeniedViaManager"
        status: pass
    human_judgment: false
  - id: D4
    description: "An explicit fail_open per-hook policy CONTAINS the fault and ALLOWS the tool (contained), while the default policy denies the same fault — both hookFault branches pinned"
    requirement: GATE-02
    verification:
      - kind: unit
        ref: "internal/agent/hooks_policy_test.go#TestCommandHookFailOpen_ContainedAllowsTool / TestCommandHookFailPolicy_BothBranchesDenyVsContain"
        status: pass
    human_judgment: false
  - id: D5
    description: "The default (empty/unset) command-hook fail policy resolves to FailClosed; only explicit fail_open opens"
    requirement: GATE-02
    verification:
      - kind: unit
        ref: "internal/agent/hooks_command_policy_internal_test.go#TestCommandHookFailPolicy / TestCommandHookFailPolicy_OnlyExplicitFailOpenOpens"
        status: pass
    human_judgment: false

# Metrics
duration: ~20min (continuation after PC crash)
completed: 2026-07-03
status: complete
---

# Phase 35 Plan 02: GATE-02 command-hook fail-closed verify + test hardening Summary

**Locked GATE-02 with a test-only matrix proving a timing-out / crashing / non-zero-exit command hook DENIES through the real HookManager (tool never executes), while an explicit `fail_open` opt-in is contained → allow — pinning both `hookFault` branches with no production change and no new env knob (D-04).**

## Performance

- **Duration:** ~20 min (continuation run after a mid-execution PC crash)
- **Started:** 2026-07-03T20:30:00Z (continuation)
- **Completed:** 2026-07-03T20:42:00Z
- **Tasks:** 2 (Task 1 salvaged from a crash-interrupted prior run; Task 2 completed this run)
- **Files modified:** 1 this run (`hooks_policy_test.go`); 3 more in the prior crash-salvaged Task 1 commit

## Accomplishments
- Task 2: proved an explicit `fail_open` command hook fault is CONTAINED by `hookFault` → the gated tool ALLOWS (the deliberate contrast to Task 1's default-deny), proving the knob is real and not a silent allow of a denied command.
- Pinned BOTH sides of `HookManager.hookFault` (FailClosed abort / FailOpen contain) with a single timing-out fixture, guarding the fail-closed default against a future widened-containment regression (F-006).
- Added the dedicated no-silent-allow matrix (`TestCommandHookDefaultPolicyNeverSilentAllows`) asserting timeout / crashed-allow / unparseable-crash all deny under the default policy AND the tool never executes.
- Confirmed GATE-02 is already over-satisfied by the shipped FailClosed default — verify-only, no production hook code touched, no new `AURA_*` knob (D-04). `git diff` limited to `_test.go`.

## Task Commits

1. **Task 1: Strengthen the timeout / crash / non-zero → DENY matrix** — `1db95377` (test) — *salvaged from a crash-interrupted prior run; verified present, not redone*
2. **Task 2: Prove explicit fail_open contained-allow + pin coverage** — `0e410ffe` (test)

**Plan metadata:** committed with SUMMARY.md + STATE.md + ROADMAP.md (docs commit)

## Files Created/Modified
- `internal/agent/hooks_policy_test.go` — added `TestCommandHookFailOpen_ContainedAllowsTool`, `TestCommandHookFailPolicy_BothBranchesDenyVsContain`, `TestCommandHookDefaultPolicyNeverSilentAllows` (+ `agenttest` import); reuses the task-1 `agent_test` helpers.
- (Task 1, prior commit `1db95377`) `internal/agent/hooks_command_hardening_test.go`, `internal/agent/hooks_command_test.go`, `internal/agent/hooks_command_policy_internal_test.go`.

## Decisions Made
- **Verify-only (D-04):** `commandHookFailPolicy` already defaults empty/unset → FailClosed and only an explicit, case-insensitive, whitespace-trimmed `fail_open` opens. No production change, no new env knob. Production was inspected (`hooks_command.go` :167-176, `hooks.go` `hookFault` :119-128) and CONFIRMED to fail closed — no escalation needed.
- **Reused the crash-salvaged patch verbatim:** the ~71-line uncommitted Task-2 progress recovered from the crash aligned exactly with the real Task-1 helpers (`newCommandHookManager`, `gateTool`, `spyTool`) and helper modes (`sleep`, `allow_then_crash`, `crash_no_decision`), so it was applied as-is rather than rewritten.

## Deviations from Plan

None - plan executed exactly as written. (Task 1 was already committed by a prior crash-interrupted run and was verified, not redone; Task 2 reused the salvaged patch, which matched the plan's stated file target and intent.)

## Issues Encountered
- **PC crash mid-execution:** the original run committed Task 1 (`1db95377`) but crashed with ~71 lines of Task-2 work uncommitted, saved as a patch. This continuation verified Task 1 via `git log` + file inspection, then applied the salvaged patch and completed Task 2.
- **`-race` requires cgo (no gcc on Windows PATH):** ran the race suite in WSL per CLAUDE.md (`CGO_ENABLED=1 go test -race ./internal/agent/` → green, 22.6s). `go vet` + `go build ./...` + non-race package tests ran green on Windows.
- **lefthook not on PATH:** the pre-commit hook printed "Can't find lefthook in PATH" and the commit proceeded; validation (vet/build/tests incl. -race) was run manually and is green.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Wave 1 of Phase 35 is complete (35-01 + 35-02). Wave 2 (35-03 — Gateway Decide PEP + profile branch + 3-root injection) is unblocked.
- No blockers.

## Self-Check: PASSED
- FOUND: internal/agent/hooks_policy_test.go
- FOUND: commit 0e410ffe (Task 2)
- FOUND: commit 1db95377 (Task 1, salvaged)
- go vet ./internal/agent/ + go build ./... clean
- go test ./internal/agent/ (targeted) pass; go test -race ./internal/agent/ green in WSL (22.6s)

---
*Phase: 35-toolgateway-policy-engine*
*Completed: 2026-07-03*
