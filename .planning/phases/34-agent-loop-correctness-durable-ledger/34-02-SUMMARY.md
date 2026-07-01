---
phase: 34-agent-loop-correctness-durable-ledger
plan: 02
subsystem: agent-loop
tags: [dispatch, text_response, terminal-exclusivity, mutating-panic, completion-gate, tool-use-semantics]

# Dependency graph
requires:
  - phase: 03-llm-agent
    provides: "dispatch() tool-call run-loop, appendSyntheticToolResults + maybeRecover/finalize dedup-trip path, terminalTool text_response, runToolRecovering (F-031)"
  - phase: 33-runtime-profiles-config-validation
    provides: "completion gate (D-43) reading a.sideEffected — the post-mutation safeguard this plan proves survives a panic"
provides:
  - "Terminal text_response exclusivity: a step mixing a terminal with any runnable sibling — or two text_response calls — is hard-rejected before any sibling Execute runs (LOOP-01 / F-003)"
  - "Fix for the latent double-text_response silent-drop (D-01b)"
  - "Regression test pinning the full F-031 chain: mutating-tool panic -> recovered Mutating bit -> a.sideEffected armed (LOOP-08)"
affects: [35-tool-gateway, agent-loop, completion-gate]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Terminal-exclusivity short-circuit reuses the existing dedup-trip control flow (appendSyntheticToolResults + maybeRecover/finalize) — one recovery mechanism, no second path"
    - "Classification-INDEPENDENT rejection: does not trust the untrustworthy Mutating flag (native tool-use semantics — any tool call present => not final)"

key-files:
  created:
    - internal/agent/llm_agent_terminal_reject_test.go
    - internal/agent/llm_agent_mutating_panic_test.go
  modified:
    - internal/agent/llm_agent_dispatch.go

key-decisions:
  - "D-01/D-01a: hard-reject the whole mixed terminal step (both mutating AND read-only siblings) rather than option B (allow read-only siblings) — the Mutating flag is untrustworthy while skill/task/swarm_spawn stay unflagged (Phase-35 gap)"
  - "D-01b: broaden the reject condition to terminalCount>1 so a second text_response is rejected/repaired, not silently continue-dropped — fixes a latent bug"
  - "LOOP-08 is a test-only closure: runToolRecovering already copies the Mutating bit (D-13); the new test drives dispatch() end-to-end to pin the full chain, runToolRecovering is unchanged"

patterns-established:
  - "Short-circuit placed AFTER the partition loop and BEFORE the hook/dedup loop so no sibling hook, dedup, or Execute runs on the reject path"

requirements-completed: [LOOP-01, LOOP-08]

coverage:
  - id: D1
    description: "A terminal text_response combined with any runnable sibling (mutating OR read-only) rejects the whole step before the sibling's Execute runs, tripping replan/finalize (LOOP-01 / F-003, D-01/D-01a)"
    requirement: "LOOP-01"
    verification:
      - kind: unit
        ref: "internal/agent/llm_agent_terminal_reject_test.go#TestDispatch_TerminalRejectExclusivity"
        status: pass
      - kind: unit
        ref: "internal/agent/llm_agent_terminal_reject_test.go#TestDispatch_TerminalRejectFinalizesAfterRecoveryExhausted"
        status: pass
    human_judgment: false
  - id: D2
    description: "Two text_response calls in one step are rejected/repaired (synthetic result for BOTH ids), not silently dropped (D-01b latent-bug fix)"
    requirement: "LOOP-01"
    verification:
      - kind: unit
        ref: "internal/agent/llm_agent_terminal_reject_test.go#TestDispatch_TerminalRejectExclusivity/double_terminal"
        status: pass
    human_judgment: false
  - id: D3
    description: "Control cases unchanged: a read-only tool with no terminal runs normally; a lone text_response terminates the loop (no regression)"
    requirement: "LOOP-01"
    verification:
      - kind: unit
        ref: "internal/agent/llm_agent_terminal_reject_test.go#TestDispatch_ReadOnlySiblingWithoutTerminalRuns"
        status: pass
      - kind: unit
        ref: "internal/agent/llm_agent_terminal_reject_test.go#TestDispatch_SingleTerminalNoSiblingRunsTerminal"
        status: pass
    human_judgment: false
  - id: D4
    description: "A mutating tool that panics AFTER its side effect still yields Mutating==true through the recovery path and arms a.sideEffected (the completion gate) — F-031 / D-13 chain pinned"
    requirement: "LOOP-08"
    verification:
      - kind: unit
        ref: "internal/agent/llm_agent_mutating_panic_test.go#TestDispatch_MutatingPanicArmsCompletionGate"
        status: pass
    human_judgment: false

# Metrics
duration: 20min
completed: 2026-07-01
status: complete
---

# Phase 34 Plan 02: Terminal text_response Exclusivity + Mutating-Panic Regression Summary

**dispatch() hard-rejects a terminal text_response combined with any runnable sibling or a second text_response before any side effect (LOOP-01 / F-003), and a regression test pins the mutating-panic -> a.sideEffected completion-gate chain (LOOP-08 / F-031).**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-07-01T15:40Z
- **Completed:** 2026-07-01T16:00:17Z
- **Tasks:** 2
- **Files modified:** 3 (1 source, 2 test)

## Accomplishments
- Terminal-exclusivity short-circuit in `dispatch()`: when a terminal `text_response` appears with any runnable sibling OR a second `text_response`, the whole step is rejected before any sibling hook/dedup/Execute runs, reusing the dedup-trip path (`appendSyntheticToolResults` + `maybeRecover`/`finalize`). Matches native tool-use semantics and needs zero reliance on the untrustworthy `Mutating` flag (D-01/D-01a).
- Fixed the latent double-`text_response` silent-drop: `terminalCount>1` now rejects/repairs the second terminal instead of `continue`-skipping it, and every call id gets a synthetic reject result so the wire stays valid (D-01b).
- Regression test drives `dispatch()` end-to-end for a mutating tool that panics after its side effect, proving the recovered `Mutating` bit arms `a.sideEffected` — closing the full F-031 chain the isolated `runToolRecovering` test did not exercise (LOOP-08 / D-13).

## Task Commits

Each task was committed atomically:

1. **Task 1 (RED): failing terminal-exclusivity tests** - `178b5d13` (test)
2. **Task 1 (GREEN): reject terminal + sibling / double-terminal in dispatch()** - `344b1bae` (feat)
3. **Task 2: mutating-panic classification regression test** - `445713ae` (test)

_TDD Task 1 = RED (test) then GREEN (feat)._

## Files Created/Modified
- `internal/agent/llm_agent_dispatch.go` - Partition now counts terminals; a short-circuit rejects the mixed/double-terminal step before the hook/dedup loop (+25 LOC; file well under the 600-LOC cap).
- `internal/agent/llm_agent_terminal_reject_test.go` - Table test for the three reject cases + finalize-arm test + two control cases (no-terminal and lone-terminal).
- `internal/agent/llm_agent_mutating_panic_test.go` - Drives dispatch() with a side-effect-then-panic mutating tool; asserts `a.sideEffected` is armed.

## Decisions Made
- **D-01/D-01a (honored):** rejected option B (allow read-only siblings). The reject is classification-independent — the `Mutating` flag is untrustworthy while `skill`/`task`/`swarm_spawn` stay unflagged; option B would let `skill action=create` run beside a final answer, re-introducing F-003. The dispatch comment flags this as a Phase-35 classification-hardening note (no classification change here — scope fence honored).
- **D-01b (honored):** broadened the reject condition to `terminalCount>1` so a second `text_response` is rejected, closing the latent silent-drop.
- **LOOP-08:** kept `runToolRecovering` unchanged (it already resolves + copies the `Mutating` bit); added only the end-to-end regression test as the plan mandated.
- **finalize reason string:** used `"terminal_exclusivity"` as the trip reason fed to `finalize`/`terminalBudgetEvent` StateDelta (Claude's discretion — mirrors the dedup path's reason parameter).

## Deviations from Plan

None - plan executed exactly as written. The prohibitions were honored: no change to the tool `Mutating` classification, option B not adopted, `runToolRecovering` not re-implemented.

## Issues Encountered
None. RED confirmed the three reject cases failed (siblings executed / second terminal dropped) before the fix; GREEN turned them green with vet/build/unit all clean and the full `internal/agent` package race + goleak suite passing (20.0s).

## Threat Flags
None — no new network endpoints, auth paths, file access, or schema surface introduced. The change is pure in-package agent-loop control flow. STRIDE register entries T-34-A / T-34-A2 (F-003 tampering + latent double-terminal) and T-34-H (F-031) are now mitigated + test-pinned.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- LOOP-01 and LOOP-08 closed. The `skill`/`task`/`swarm_spawn` `Mutating`-classification gap remains deliberately open for Phase 35 (ToolGateway); a scope-fence comment in `dispatch()` flags it. Only after that hardening could "allow read-only siblings" (option B) ever be reconsidered.
- No DB/composition-root touch — ran fully parallel to the Wave-1 DB/HITL work.

## Self-Check: PASSED

- Files verified present: `internal/agent/llm_agent_dispatch.go`, `internal/agent/llm_agent_terminal_reject_test.go`, `internal/agent/llm_agent_mutating_panic_test.go`, `34-02-SUMMARY.md`.
- Commit hashes verified in git log: `178b5d13` (RED test), `344b1bae` (GREEN feat), `445713ae` (regression test).
- WSL gates green: `go vet ./internal/agent/...` clean, `go build ./...` clean, `go test ./internal/agent/` pass, `go test -race ./internal/agent/` pass (20.0s, goleak-clean).

---
*Phase: 34-agent-loop-correctness-durable-ledger*
*Completed: 2026-07-01*
