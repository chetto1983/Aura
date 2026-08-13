---
phase: 45-harness-correctness
plan: 03
subsystem: gateway-idempotency
tags: [idempotency, replay, otel, boot-guard, harness-correctness, tdd]

# Dependency graph
requires: ["45-02"]
provides:
  - "internal/gateway/reserve.go: replayedMarker constant + markReplayed helper, appended to both replay layers (Layer A reservation ledger, Layer B operation registry)"
  - "internal/agent/tracing.go: stampReplayAttributes sets aura.tool.replayed / aura.tool.replay_layer on the tool.execute span"
  - "internal/agent/llm_agent_retry.go: replayLayerAttributes — the three-way (operation/reservation/fresh) derivation from state gateway.Verdict already carries, no new Verdict field"
  - "internal/gateway/guard.go: validateOperationMetadata — boot-time panic on a Mutating tool with an empty OperationScope, OperationNormalizer or ReplayPolicy"
affects: [45-04, 45-05, 45-06, 45-07, 45-08]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Marker-in-preview composition: a second harness-authored marker (replayedMarker) appends alongside an existing one (resultExpiredMarker) rather than replacing it — each answers a different question about the same result"
    - "Derive-don't-store: the replay layer (operation vs reservation) is computed from state gateway.Verdict already exposes (OperationDecision + Replay != nil), not added as a new struct field"
    - "Boot-time fail-loud wiring guard scoped by a runtime-only Spec flag (Mutating), checking field EMPTINESS only — never comparing against a specific enum value, so a single-valued type can still gate completeness without inviting a second value"

key-files:
  created:
    - internal/agent/llm_agent_replay_layer_test.go
  modified:
    - internal/gateway/reserve.go
    - internal/gateway/reserve_test.go
    - internal/agent/tracing.go
    - internal/agent/llm_agent_retry.go
    - internal/gateway/guard.go
    - internal/gateway/guard_test.go
    - internal/agent/llm_agent_retry_gateway_test.go

key-decisions:
  - "replayedMarker exact string: \"\\n\\n[replayed: this result is from a prior dispatch of this call, not a fresh execution]\" — bracketed, newline-prefixed, same shape as the existing resultExpiredMarker so it reads as harness metadata, not tool output."
  - "Span-attribute plumbing shape chosen: the smaller of the two acceptable shapes named in the plan — set attributes inside execTool on the span already carried by ctx (oteltrace.SpanFromContext(ctx).SetAttributes(...) inside stampReplayAttributes), rather than threading (bool, string) back through tools.ToolResult for runTool to stamp before endToolSpan. No signature change on the dispatch chain; the two attribute-name literals live only in tracing.go."
  - "replayLayerAttributes is a pure, unit-tested derivation over (idempotency.Decision, bool) in llm_agent_retry.go, kept separate from the one-line stamping call in tracing.go — an untested three-branch conditional inline in execTool's hot path was rejected in favor of a tested function plus a one-liner call site."
  - "Task 3's operation-metadata check runs INSIDE the same reg.All() loop as the existing multiplexed-classifier check, positioned BEFORE it (not as a second loop or a code block after the classifier check's `continue`), because the classifier check's `continue` skips non-multiplexed mutating tools — placing the new check after it would silently skip shell_exec/patch/write_file/shell_bg_owner (Mutating, non-Multiplexed). Checked via validateOperationMetadata(spec), a sibling unexported function, keeping ValidateClassifiable itself short and readable."
  - "applyMCPOperationMetadata (internal/agent/mcptools/bridge.go:230-240) confirmed BEFORE writing the guard: it fills OperationScope/OperationNormalizer/ReplayPolicy unconditionally for every Mutating bridged tool (line 232-234) and clears all three for every non-mutating one (line 237-239) — no MCP-bridged tool can trip the new guard today. All seven built-in mutating tools (patch.go:79-80, write_file.go:58-59, shell_exec.go:115-116, shell_bg_owner.go:251-252, skill_manage.go:57-58, task.go:145-146, swarm_spawn.go:89-90) were confirmed by direct grep to already declare the three fields."

patterns-established:
  - "A test that pins a replayed ToolResult.Preview verbatim must be updated to tolerate the appended replay marker (documented in the plan's own reversibility table) — done here by loosening the assertion shape (HasPrefix + not-equal-to-fresh) rather than hardcoding the gateway-package-internal marker string across a package boundary."

requirements-completed: [HARN-03, HARN-02, ACC-02]

# Metrics
duration: ~2h (across three tasks; this session executed only Task 3, ~55min)
completed: 2026-08-13
---

# Phase 45 Plan 03: Replay visibility + boot-time operation-metadata guard Summary

**Both replay layers (reservation ledger and operation registry) now label a replayed result for the model through one shared helper, the same fact is stamped as `aura.tool.replayed`/`aura.tool.replay_layer` on the `tool.execute` span, and a `Mutating` tool with incomplete `OperationScope`/`OperationNormalizer`/`ReplayPolicy` now panics at boot instead of reaching the idempotency layer with an undefined replay posture.**

## Session Note

This plan was interrupted mid-flight. Tasks 1 and 2 (replayedMarker on both replay layers; the OTel replay attributes) were implemented and committed in a prior session before this one started. This session verified those two tasks against the commits and the current source (see Self-Check), then executed Task 3 end to end, and wrote this SUMMARY covering all three.

## Performance

- **Duration:** ~55 min (this session, Task 3 only — reading, TDD RED/GREEN, WSL race verification, collateral-test fix, SUMMARY)
- **Completed:** 2026-08-13
- **Tasks:** 3 (1 and 2 pre-existing; 3 executed this session)
- **Files modified this session:** 2 (`internal/gateway/guard.go`, `internal/gateway/guard_test.go`), plus 1 collateral fix (`internal/agent/llm_agent_retry_gateway_test.go`)

## Accomplishments

**Task 1 (pre-existing, verified — `internal/gateway/reserve.go`):** `replayedMarker` constant (line 36) and the shared `markReplayed(tools.ToolResult) tools.ToolResult` helper (line 43-46) append the marker string to a replayed preview. `replayResult` (Layer A) calls it on its non-nil return path (line 317); `decodeOperationReplay` (Layer B) calls it on both success branches (line 130). `replayResult(nil)` and `decodeOperationReplay` on a nil/empty replay are untouched — no marker on either. The GC'd-sidecar case composes `resultExpiredMarker` (appended first, line 313) with `replayedMarker` (appended by the helper, line 317) in one preview. `reserve.go:43`'s exact-match guard (`spec.ReplayPolicy != tools.ReplayToolResult`) is byte-unchanged.

**Task 2 (pre-existing, verified — `internal/agent/tracing.go` + `internal/agent/llm_agent_retry.go`):** `replayLayerAttributes(operationDecision idempotency.Decision, replay bool) (replayed bool, layer string)` (llm_agent_retry.go:62-70) derives the layer purely from state `gateway.Verdict` already carries — `OperationDecision == idempotency.DecisionReplay` means `"operation"`, otherwise a true `replay` means `"reservation"` — with no new field on `Verdict`. `stampReplayAttributes(ctx, replayed, layer)` (tracing.go:207-215) sets `aura.tool.replayed` (bool) and `aura.tool.replay_layer` (string) on the span `ctx` already carries, as a no-op when `replayed` is false (absent attribute on a fresh execution, never a stamped `false`). Called from `execTool`'s `verdict.Replay != nil` branch (llm_agent_retry.go:161-164), immediately before returning the recorded result. `internal/agent/llm_agent_replay_layer_test.go` unit-tests all three branches (operation / reservation / fresh) of the pure derivation.

**Task 3 (this session — `internal/gateway/guard.go` + `internal/gateway/guard_test.go`):** `ValidateClassifiable` now runs a second assertion, `validateOperationMetadata(spec)`, inside the same `reg.All()` loop, for every `Mutating` tool — checking `OperationScope`, `OperationNormalizer` and `ReplayPolicy` for EMPTINESS only (never comparing against `tools.ReplayToolResult`, never a switch over `ReplayPolicy`, per D-07/D-09). The check runs BEFORE the existing multiplexed-classifier check's `continue`, so it also covers non-multiplexed mutating tools (shell_exec, patch, write_file, shell_bg_owner) that the classifier check itself skips. Each of the three missing-field panics names the offending tool, mirroring the existing `panic(fmt.Sprintf(...))` idiom verbatim in shape. Confirmed before writing (see key-decisions) that no path — built-in tool or MCP-bridged tool — can trip the guard today.

## Task Commits

Tasks 1 and 2 were committed in a prior session (this session verified them; see Self-Check below). Task 3's TDD RED commit precedes its GREEN implementation commit.

1. **Task 1 RED — failing tests for replayedMarker on both replay layers** — `ecd5cd09f` (test) [pre-existing]
2. **Task 1 GREEN — label replayed results on both dedup layers through one helper** — `90dfdd21d` (feat) [pre-existing]
3. **(chore) bring Task 1 onto the phase branch** — `58c282893` [pre-existing]
4. **Task 2 RED — failing test for the three-way replay-layer derivation** — `2893c1110` (test) [pre-existing]
5. **Task 2 GREEN — derive the replay layer and stamp it on the span** — `75c0fb660` (feat) [pre-existing]
6. **Task 3 RED — failing tests for the mutating-tool operation-metadata boot guard** — `fac25ba38` (test) [this session]
7. **Task 3 GREEN — panic at boot on incomplete mutating-tool operation metadata** — `6be1324b7` (feat) [this session]

## Files Created/Modified

**This session (Task 3):**
- `internal/gateway/guard.go` — new `validateOperationMetadata(spec tools.Spec)` (17 LOC) called from inside `ValidateClassifiable`'s existing `reg.All()` loop before the multiplexed-classifier check; three sequential `if field == ""` panics, no switch. File: 71 LOC.
- `internal/gateway/guard_test.go` — three new panic tests (`TestValidateClassifiablePanicsOnEmptyOperationScope/OperationNormalizer/ReplayPolicy`), two new negative-case tests (`TestValidateClassifiableAcceptsCompleteMutatingTool`, `TestValidateClassifiableIgnoresNonMutatingEmptyOperationMetadata`), and fixture updates to `TestValidateClassifiablePanicsOnUnwiredMultiplexed` (ghost_multiplex) and `TestValidateClassifiableAcceptsWiredTools` (skill_manage/task/swarm_spawn/shell_exec) so they keep exercising the classifier-wiring guard rather than tripping the new metadata guard. File: 202 LOC.
- `internal/agent/llm_agent_retry_gateway_test.go` — collateral fix: `TestExecToolRetryReusesOperationWhileAuditIDsChange` loosened from `second.Preview != "executed"` to `!strings.HasPrefix(second.Preview, "executed") || second.Preview == "executed"`, tolerating the replay marker Task 1 appends without hardcoding gateway-package-internal marker text across the package boundary.

**Pre-existing (Tasks 1 and 2, verified not re-touched):** `internal/gateway/reserve.go`, `internal/gateway/reserve_test.go`, `internal/agent/tracing.go`, `internal/agent/llm_agent_retry.go`, `internal/agent/llm_agent_replay_layer_test.go`.

## RED Output (recorded verbatim)

**Task 3 RED** — `go test -run 'Validate' ./internal/gateway/ -v` against `guard.go` before `validateOperationMetadata` existed:

```
=== RUN   TestValidateClassifiablePanicsOnEmptyOperationScope
    guard_test.go:102: ValidateClassifiable did not panic on a mutating tool with an empty OperationScope
--- FAIL: TestValidateClassifiablePanicsOnEmptyOperationScope (0.00s)
=== RUN   TestValidateClassifiablePanicsOnEmptyOperationNormalizer
    guard_test.go:127: ValidateClassifiable did not panic on a mutating tool with an empty OperationNormalizer
--- FAIL: TestValidateClassifiablePanicsOnEmptyOperationNormalizer (0.00s)
=== RUN   TestValidateClassifiablePanicsOnEmptyReplayPolicy
    guard_test.go:154: ValidateClassifiable did not panic on a mutating tool with an empty ReplayPolicy
--- FAIL: TestValidateClassifiablePanicsOnEmptyReplayPolicy (0.00s)
FAIL
FAIL	github.com/chetto1983/aura/internal/gateway	4.631s
```

The other five tests in the run (including the two new negative-case tests and the pre-existing tests with updated fixtures) already passed at RED time — they assert absence of a panic, which the unmodified `ValidateClassifiable` already provided by construction.

**Task 1 RED** (from `ecd5cd09f`'s commit message, reproduced here for completeness): `go test -run 'Replay' ./internal/gateway/` failed 4 of 7 new/extended assertions against the unmodified `reserve.go` — the two markerless functions produced previews with no "replayed" substring; the nil-end, decode-nil and decode-empty probe edges already passed (they assert absence/errors the pre-existing code already provided).

**Task 2 RED** (from `2893c1110`'s commit message): `replayLayerAttributes` shipped as a compiling stub returning `(false, "")` always; 3 of 4 subtests in `llm_agent_replay_layer_test.go` failed against the stub. A non-compiling RED commit is rejected by the strict pre-commit vet/lint gate, so the stub kept the build green while the assertions stayed red.

## Decisions Made

- **`replayedMarker` exact string** (Task 1, verified): `"\n\n[replayed: this result is from a prior dispatch of this call, not a fresh execution]"`.
- **Span-attribute plumbing shape** (Task 2, verified): attributes are set inside `execTool` on the span already carried by `ctx`, via `stampReplayAttributes(ctx, replayed, layer)` — the smaller of the two acceptable shapes the plan named, avoiding a signature change on the `tools.ToolResult` return path.
- **`applyMCPOperationMetadata` line numbers** (Task 3, this session): `internal/agent/mcptools/bridge.go:230-240`. Lines 231-235 fill all three fields unconditionally when `spec.Mutating`; lines 236-239 clear all three otherwise. No bridged tool can reach the boot guard with an empty field.
- **Task 3 guard placement**: the new emptiness check runs inside the SAME `for _, t := range reg.All()` loop as the multiplexed-classifier check, but BEFORE it (not as a second loop, not after the classifier check's `continue`) — the classifier check's `continue` statement skips non-multiplexed mutating tools, and placing the new check after that `continue` would have silently exempted shell_exec, patch, write_file and shell_bg_owner from the new guard.
- **Fixture repairs, not test-behavior weakening**: `TestValidateClassifiablePanicsOnUnwiredMultiplexed`'s `ghost_multiplex` fixture and `TestValidateClassifiableAcceptsWiredTools`'s four mutating fixtures gained the three operation-metadata fields so they continue to exercise the classifier-wiring guard they were written for, rather than tripping the newly-added metadata guard first. Neither test's assertions were relaxed; only their fixtures were completed.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Pre-existing test pinned a replayed preview verbatim, broken by Task 1's already-landed marker**
- **Found during:** running this plan's own required verification command, `go test -race ./internal/gateway/ ./internal/agent/ ./internal/agent/mcptools/`, after Task 3's GREEN commit.
- **Issue:** `TestExecToolRetryReusesOperationWhileAuditIDsChange` (`internal/agent/llm_agent_retry_gateway_test.go`) asserted `second.Preview != "executed"` on a Layer B replay result. Task 1 (already committed in a prior session) appends `replayedMarker` to every replayed preview, so the recorded `"executed"` content now arrives as `"executed" + replayedMarker`, and the exact-match assertion failed. This is not a Task 3 defect — Task 3 does not touch `reserve.go` or this test file — but it blocked this plan's own required verification and is exactly the collateral case the plan's own reversibility table anticipated: "any test pinning a replayed preview verbatim must be updated with it."
- **Fix:** loosened the assertion to `!strings.HasPrefix(second.Preview, "executed") || second.Preview == "executed"`, proving the recorded content survives the replay while tolerating the appended marker, without hardcoding the gateway-package-internal marker string across the package boundary (`replayedMarker` is unexported in `internal/gateway`).
- **Files modified:** `internal/agent/llm_agent_retry_gateway_test.go`.
- **Commit:** `6be1324b7` (folded into Task 3's GREEN commit, since it was required for that commit's own verification to pass).

## Issues Encountered

None beyond the one auto-fixed item above.

## User Setup Required

None — no external service configuration required. `go test -race` was run in WSL (Ubuntu) against `/mnt/d/Repo/Aura`, per this repo's documented primary dev environment for cgo/race support; Windows-native `go test -race` fails with `CGO_ENABLED` unset. No database or container dependency for this plan's tests.

## TDD Gate Compliance

- **Task 1 RED gate:** `ecd5cd09f` (test) — verified present.
- **Task 1 GREEN gate:** `90dfdd21d` (feat) — verified present, after RED.
- **Task 2 RED gate:** `2893c1110` (test) — verified present.
- **Task 2 GREEN gate:** `75c0fb660` (feat) — verified present, after RED.
- **Task 3 RED gate:** `fac25ba38` (test) — this session; three new assertions fail against unmodified `guard.go` (verbatim output above).
- **Task 3 GREEN gate:** `6be1324b7` (feat) — this session, after RED; all eight `guard_test.go` tests pass, plus the collateral fix required for the plan's own verification command.

All three tasks show a `test(...)` commit immediately followed by a `feat(...)` commit in `git log`, satisfying the plan-level TDD gate sequence.

## Verification Run (this session)

- `go vet ./internal/gateway/` — clean.
- `go test -run 'Validate' ./internal/gateway/ -v` (Windows, non-race) — 8/8 pass.
- `go vet ./internal/gateway/` + `go test -race -run 'Validate' ./internal/gateway/ -v` (WSL Ubuntu, `/mnt/d/Repo/Aura`) — 8/8 pass.
- `go build ./...` (Windows) — clean.
- `go vet ./...` (WSL) — clean.
- `go build ./... && go test -race ./internal/gateway/ ./internal/agent/ ./internal/agent/mcptools/` (WSL) — all green (`ok`, `ok`, `ok`).
- `grep -c "ReplayReissueExecutes\|switch.*ReplayPolicy" internal/gateway/guard.go` → `0`.
- `grep -rn "event_kind" internal/db/migrations/ | grep -c replay` → `0`; `ls internal/db/migrations/ | tail -1` unchanged (`0095_backfill_parent_seq.up.sql`) — no migration added.
- `git diff --diff-filter=D --name-only HEAD~1 HEAD` on the GREEN commit — empty (no accidental deletions).

## Next Phase Readiness

- SC#3 (replay visibility) is proved at the unit tier on both replay layers, with the OTel evidence surface (`aura.tool.replayed`/`aura.tool.replay_layer`) set alongside the model-facing marker.
- The marker string and its append rule live in exactly one place (`markReplayed` in `reserve.go`); the replay-layer derivation lives in exactly one place (`replayLayerAttributes` in `llm_agent_retry.go`) and is unit-tested independently of the stamping call.
- A `Mutating` tool with incomplete operation metadata cannot boot — closing the wiring gap the withdrawn second `ReplayPolicy` value was standing in for (D-09), scoped so it cannot become a de facto requirement on every registered tool.
- Phase 46's `bridgePolicy` overrides are flagged (per this plan's threat register, T-45-11) as the place that could make the new guard reachable from a fail-soft MCP mount — that constraint carries forward to Phase 46, not resolved here.
- `reserve.go:43`'s exact-match `ReplayPolicy` guard and the single `ReplayToolResult` value are untouched; no second `ReplayPolicy` constant exists anywhere in the tree.

---
*Phase: 45-harness-correctness*
*Completed: 2026-08-13*
