---
phase: 46-mcp-trust-and-facade
plan: 04
subsystem: mcp-bridge
tags: [deferral, tool-tiering, kv-cache, reconnect, slog, d-27, tool-14]

# Dependency graph
requires:
  - phase: 46-mcp-trust-and-facade (plan 01)
    provides: "prd.md Amendment #123 — the numeric rule this plan implements: <=3 model-facing tools earns an always-loaded slot, global cap 2 slots, overflow fails closed to deferred"
  - phase: 46-mcp-trust-and-facade (plan 02)
    provides: "The operator's views-exempt decision — WhatsApp ends at 3 model-facing tools, i.e. exactly at the ceiling, which is why the predicate is <= and not <"
provides:
  - "bridgePolicy.defaultDeferred() backed by D-27's count arithmetic instead of an unconditional true, with maxAlwaysLoadedMCPTools (3) and maxAlwaysLoadedMCPSlots (2) as Go constants and no env override"
  - "A deferral decision frozen once per mount in bridgeToolsWithPolicy and stamped identically on every bridgedTool, so refreshSpec cannot flip a tool's manifest presence mid-conversation"
  - "warnIfDeferralWouldFlip wired into refreshSpecsLocked — the reconnect path reports deferral drift and never applies it"
affects: [46-06, 46-08, 46-09]

actuals:
  tokens: 0
  tasks: 2
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Freeze-at-mount + report-drift: a decision that changes the model-visible tool manifest is taken once at mount, stored on the policy, and re-read (never recomputed) on reconnect; divergence is emitted as a structured WARN for an operator rather than silently applied. Protects the KV-cache prefix the model is mid-conversation on."
    - "Global slot budget with a test reset hook (resetLoadedSlotBudgetForTest) — package-level budget state means budget-touching subtests are deliberately NOT t.Parallel()."

key-files:
  created:
    - internal/agent/mcptools/bridge_deferral.go
    - internal/agent/mcptools/bridge_deferral_test.go
  modified:
    - internal/agent/mcptools/bridge.go
    - internal/agent/mcptools/bridge_supervisor.go
    - internal/agent/mcptools/bridge_memory.go
    - internal/agent/mcptools/bridge_test.go
    - internal/agent/mcptools/bridge_memory_policy_test.go
    - internal/agent/mcptools/mount_test.go

key-decisions:
  - "warnIfDeferralWouldFlip deliberately does NOT call grantLoadedSlot. Recomputing the real decision on every reconnect would spend (or refuse to spend) the global two-slot budget as a side effect of a health-check-shaped call, corrupting the very budget the frozen-at-mount design exists to keep stable. Eligibility is compared purely against the ceiling."
  - "The policy passed to warnIfDeferralWouldFlip is read from any tracked bridgedTool in s.bridged rather than stored a second time on MountedServer. bridgeToolsWithPolicy scores the mount once and stamps an identical policy on every tool it builds, so any one of them answers for the mount; a second copy on the server would be a second source of truth that could drift."
  - "Four pre-existing tests asserted the rule task 1 changed and were updated rather than deleted, each with justification in c2b43be2b's commit message. The most substantive: TestBridge_MemoryNamespaceToolsAreDeferredByDefault's 2-tool fixture undercounted the real memory surface and would have wrongly earned a slot under the new arithmetic; its fixture was corrected to the real cmd/arcadedb-mcp shape (10 advertised, 6 hidden by memoryHiddenFromModel, 4 model-facing), so its assertion is now provably true rather than accidentally true."
  - "Task 2's slog capture uses a TextHandler, not the JSONHandler the task's action text names. The action also says to reuse the mechanism at bridge_trust_test.go:37 'rather than inventing a second one' — and that precedent uses a TextHandler, as does task 1's already-committed TestDeferralWarnIfDeferralWouldFlip. The two instructions conflict; the specific cited precedent was followed. WARN keys are still asserted as exact key=value tokens, satisfying the acceptance criterion."

requirements-completed: []
---

# Phase 46 Plan 04: MCP deferral count rule Summary

**`bridgePolicy.defaultDeferred()` no longer returns an unconditional `true`: a mount exposing at most 3 model-facing tools now earns one of two global always-loaded slots, granted in mount order, with overflow failing closed to deferred — and that decision is frozen at mount, so a reconnect can report that it has drifted but can never act on it.**

## Performance

- **Duration:** ~2h wall across three executor attempts and an inline completion
- **Tasks:** 2
- **Files modified:** 8 (2 created, 6 modified)

## Accomplishments

- **The count predicate (task 1).** `bridge_deferral.go` carries D-27's arithmetic: `countModelFacing`, `grantLoadedSlot`, and the two constants `maxAlwaysLoadedMCPTools` (3) and `maxAlwaysLoadedMCPSlots` (2). Both are Go constants with no env override — the budget is a design property, not an operator knob.
- **One scoring point.** `bridgeToolsWithPolicy` in `bridge.go` scores `policy.modelFacingCount` and `policy.alwaysLoaded` **once**, before building any `bridgedTool`, so every tool from a single mount carries an identical frozen decision (`bridge_memory.go`'s new `bridgePolicy` fields). There is no path by which two tools from one server disagree about whether that server earned a slot.
- **The freeze survives reconnect, in both directions (task 2).** `TestRefreshSpecsLockedFreezeSurvivesReconnectBothDirections` drives the real wiring — `bridgeTools` → `trackBridgedTools` → `refreshSpecsLocked` — not the pure function. A mount frozen `alwaysLoaded:true` stays `Deferred:false` against a 5-tool listing that would no longer qualify; a mount frozen `alwaysLoaded:false` stays `Deferred:true` against a 2-tool listing that would now qualify.
- **Drift is reported (task 2).** `refreshSpecsLocked` calls `warnIfDeferralWouldFlip` once per reconnect, before the per-tool `refreshSpec` loop — the only site holding the whole listing. The WARN carries `namespace`, `frozen_deferred`, `old_model_facing` and `new_model_facing`, mirroring the key naming of `refreshSpec`'s three existing warn-on-change blocks. An unchanged decision stays silent.
- **The rationale is now in the code.** `refreshSpec`'s `spec.Deferred` line explains why it re-reads rather than recomputes: a fork briefly advertising a different tool count during a bad deploy would otherwise add or remove a tool from the manifest mid-conversation, invalidating the KV-cache prefix the model is relying on (46-RESEARCH Pitfall 3).
- **No-op on today's surface, as the plan predicted.** memory (4 model-facing), calendar (14) and whatsapp (14) all exceed the 3-tool ceiling and stay deferred exactly as before. The arithmetic is in place ahead of the curated forks 46-05 and 46-08 build; nothing about the live manifest moves until they land.

## Task Commits

1. **Task 1: The count predicate, the two constants and the global slot budget** — `c2b43be2b` (feat)
2. **Task 2 RED: refreshSpecsLocked does not report deferral drift** — `aa78613a4` (test)
3. **Task 2 GREEN: report deferral drift on reconnect, never apply it** — `f14276bb5` (feat)

## Files Created/Modified

- `internal/agent/mcptools/bridge_deferral.go` *(created)* — the arithmetic, the constants, `grantLoadedSlot`, `warnIfDeferralWouldFlip`
- `internal/agent/mcptools/bridge_deferral_test.go` *(created)* — daemon-free unit proof of the arithmetic, the slot budget, the pure warn function, the reconnect freeze in both directions, and the reconnect wiring
- `internal/agent/mcptools/bridge.go` — `bridgeToolsWithPolicy` scores once; `refreshSpec`'s `spec.Deferred` gains its rationale comment (504 lines, inside the 600 cap)
- `internal/agent/mcptools/bridge_supervisor.go` — `refreshSpecsLocked` calls `warnIfDeferralWouldFlip` (479 lines, inside the cap)
- `internal/agent/mcptools/bridge_memory.go` — `bridgePolicy` gains frozen `alwaysLoaded` / `modelFacingCount`; `defaultDeferred()` reads them
- `internal/agent/mcptools/bridge_test.go`, `bridge_memory_policy_test.go`, `mount_test.go` — four pre-existing assertions updated to the new rule, each justified in `c2b43be2b`

## Acceptance Criteria — measured

| Criterion | Result |
|---|---|
| `go test ./internal/agent/mcptools/ -run 'TestDeferral\|TestRefresh'` | ok |
| exactly one `warnIfDeferralWouldFlip` call site in `bridge_supervisor.go` | 1 |
| `b.policy.defaultDeferred()` still appears once in `bridge.go` (no recomputation introduced) | 1 |
| WARN message and keys asserted as exact tokens | yes |
| `bridge.go` / `bridge_supervisor.go` ≤ 600 lines | 504 / 479 |
| `go test -race ./internal/agent/mcptools/...` | ok (WSL) |

## Deviations from Plan

- **`-race` was run under WSL, not Windows.** Windows `go` here has no cgo (`-race requires cgo`), and `gcc` is not on the Windows PATH even with `BASH_ENV=~/.aura-toolchain.sh`. Per CLAUDE.md, WSL is the primary environment and its native race run is the documented route. Green there.
- **Task 1 could not commit a compile-failure RED.** Its test file referenced not-yet-existing symbols, so a true RED commit cannot pass the pre-commit hook and `--no-verify` is prohibited. RED was verified locally from `go vet` output (`bridge_deferral_test.go:32:13: undefined: countModelFacing` plus seven further undefined-symbol errors) and test + implementation landed together. Task 2's RED *was* committable (`aa78613a4`) because `warnIfDeferralWouldFlip` already existed — only its call site did not — so that half followed the normal RED→GREEN sequence.
- **TextHandler over JSONHandler** for slog capture — see `key-decisions`; the task text contradicts the precedent it cites, and the precedent was followed.

## Issues Encountered

Three consecutive `gsd-executor` subagents stalled on the harness 600s stream watchdog while executing this plan — after task 1's commit, and twice more. No committed work was lost; task 2's RED tests survived in the working tree and were committed unchanged. The plan was completed inline in the orchestrator session. The stalls are a harness/API-layer symptom (the SSE idle timeout the GSD workflow documents as #2410), not a defect in this plan: the pre-commit hook was measured at 8.7s warm and 18–50s on these commits, nowhere near the watchdog threshold.

## Next Phase Readiness

46-06 and 46-08 can now assume the host-side count rule exists and is frozen. Neither needs to add a deferral mechanism; both need only to make their fork advertise a qualifying surface. 46-09's `deferred_manifest.json` fixture will see the always-loaded slots actually granted once those forks land — today nothing qualifies, so the manifest is unchanged.

## Self-Check: PASSED

- Both tasks executed and committed atomically
- `go vet ./...`, `go build ./...`, package tests, and `-race` (WSL) all green before each commit
- `golangci-lint` clean on every commit (0 issues), file-size cap respected
- No test was weakened to pass: the four updated assertions are documented rule changes, and one fixture was corrected from a wrong tool count to the real one
