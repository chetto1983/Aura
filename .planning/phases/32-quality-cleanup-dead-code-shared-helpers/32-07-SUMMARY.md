---
phase: 32-quality-cleanup-dead-code-shared-helpers
plan: 07
subsystem: shared-leaves
tags: [qual-03, leaf-extraction, dedup, agentrender, render-primitives, eval-fix, refactor]

# Dependency graph
requires:
  - phase: 32-02
    provides: "QUAL-02 KEEP/swap baseline so the extraction touches already-clean call sites"
provides:
  - "internal/agentrender — canonical REPL/eval render primitives (FlushRemainder, IsToolResultPreview, IsTerminalToolCall, UsageFromStateDelta, AnyInt superset, AnyFloat); cmd/aura chat_render + internal/eval capture migrated, both ~80-LOC copies deleted"
  - "Documented eval token-count FIX: the eval path now parses json.Number token fields the old eval copy silently zeroed (AnyInt superset)"
affects: [32-09, 32-10, 33, cmd/aura, eval]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Leaf extraction (D-06, Shared Pattern A): agentrender imports internal/agent + internal/llm ONLY — no internal/agui back-edge, asserted by `go list -deps`"
    - "Characterization parity with documented divergence (D-09/D-10): the AnyInt table replicates BOTH old copies (evalAnyIntOld + chatAnyIntOld) and asserts new == chat_render superset while diverging from the lossy eval copy for json.Number"
    - "Refactor-on-touch dupl-folding: the migrated call sites' package-local duplicate tests fold into the new leaf's parity test rather than re-testing the moved function from the caller package"

key-files:
  created:
    - internal/agentrender/agentrender.go
    - internal/agentrender/agentrender_test.go
  modified:
    - cmd/aura/chat_render.go
    - cmd/aura/cover_test.go
    - internal/eval/capture_cot_eval.go
  deleted:
    - internal/eval/capture_cot_eval_anyfloat_test.go

key-decisions:
  - "AnyInt exports the chat_render SUPERSET (json.Number-aware). This is an intended behavior FIX for the eval path, not a regression: the old internal/eval anyInt lacked the json.Number branch and silently returned 0 for a UseNumber/jsonb-widened token count. The parity test pins both old copies and proves the new func diverges from the lossy eval copy on json.Number while matching the chat_render copy across the union."
  - "IsToolResultPreview keeps the `*agent.Event` signature (the optional map[string]any boundary-simplification was unnecessary — agent is an allowed import and the call sites already pass the event), so both call sites migrate with zero signature churn."
  - "The duplicate caller-package tests fold into agentrender_test.go: cmd/aura TestUsageFromStateDelta becomes a cross-reference comment and the eval-only capture_cot_eval_anyfloat_test.go is removed, because internal/agentrender now owns the comprehensive union (TestAnyInt/TestAnyFloat/TestUsageFromStateDelta)."

requirements-completed: []  # QUAL-03 partial — left to the orchestrator/verifier (32-07 covers the agentrender slice of ROADMAP C2).

coverage:
  - id: T1
    description: "internal/agentrender extracted test-first (parity table RED → impl GREEN); exports FlushRemainder/IsToolResultPreview/IsTerminalToolCall/UsageFromStateDelta/AnyInt(superset)/AnyFloat; AnyInt table documents the eval json.Number fix; no internal/agui back-edge."
    requirement: "QUAL-03"
    verification:
      - kind: unit
        ref: "go test -race -cover ./internal/agentrender/ → 100.0% of statements"
        status: pass
      - kind: other
        ref: "go list -deps ./internal/agentrender/ | grep -c internal/agui → 0 (boundary guard, T-32-07-BND)"
        status: pass
    human_judgment: false
  - id: T2
    description: "cmd/aura/chat_render.go + internal/eval/capture_cot_eval.go migrated to agentrender.*; both local ~80-LOC copies deleted; REPL output unchanged; eval json.Number counting now in effect."
    requirement: "QUAL-03"
    verification:
      - kind: unit
        ref: "go test -race ./cmd/aura/ ./internal/eval/ (green)"
        status: pass
      - kind: other
        ref: "rg 'func (flushRemainder|usageFromStateDelta|anyInt|anyFloat|isToolResultPreview|isTerminalToolCall)' cmd/aura/chat_render.go internal/eval/capture_cot_eval.go → NONE"
        status: pass
      - kind: other
        ref: "go vet -tags cot_eval ./internal/eval/ AND go vet -tags live_e2e ./internal/eval/ (the tagged capture file compiles against agentrender under both tags)"
        status: pass
    human_judgment: false

# Metrics
duration: ~45m (sequential, no worktree per D-14; concurrent-Codex isolation)
completed: 2026-06-30
status: complete
---

# Phase 32 Plan 07: agentrender Shared Render-Primitive Extraction Summary

**Extracted the ~80-LOC render-primitive set duplicated between `cmd/aura/chat_render.go` and `internal/eval/capture_cot_eval.go` into a new `internal/agentrender` leaf (QUAL-03, ROADMAP C2 partial), test-first per D-09/D-10. The extraction adopts the `chat_render` `AnyInt` SUPERSET, which FIXES a real silent bug in the eval path: the old eval `anyInt` lacked the `json.Number` branch and zeroed UseNumber/jsonb-widened token counts. Both call sites migrated, both local copies deleted, the parity table green against both old copies first (documenting the eval fix), `internal/agentrender` at 100% coverage with no `internal/agui` back-edge.**

## Context: sequential on main tree, concurrent Codex

Per **D-14** this plan ran **sequentially on the main working tree (no git worktree)**. A parallel Codex session was committing unrelated `internal/agui`/`internal/objectstore`/document-catalog work and the `.planning/graphs/*` artifacts to `master` throughout. Every commit here used explicit-pathspec `git commit -o -F <msg> -- <paths>` (never `git add -A`/`.`/`<dir>`, never the `gsd` wrapper) so only this plan's files were committed; `git show --stat` confirmed each commit lists ONLY this plan's files (zero agui/objectstore/graphs swept in).

## Accomplishments

- **Task 1 — `internal/agentrender` (TDD):** stdlib + `internal/agent` + `internal/llm` leaf exporting `FlushRemainder` (streamed-tail flush: empty/equal/prefix/divergent), `IsToolResultPreview` (`tool_call_id` StateDelta marker), `IsTerminalToolCall` (`text_response` loop terminator), `UsageFromStateDelta` (StateDelta → `llm.Usage`), `AnyInt` (the json.Number-aware SUPERSET), and `AnyFloat`. The parity table was written FIRST (RED — undefined funcs), then the implementation (GREEN). The `AnyInt` table replicates BOTH former copies (`evalAnyIntOld` without the json.Number branch, `chatAnyIntOld` with it) and asserts `AnyInt == chatAnyIntOld` for the whole union while DIVERGING from `evalAnyIntOld` on a valid `json.Number` — the documented T-32-07-FIX. **100% coverage, race-clean, `go list -deps` has no `internal/agui`.**
- **Task 2 — call-site migration + copy deletion:** `cmd/aura/chat_render.go` and `internal/eval/capture_cot_eval.go` now call `agentrender.{FlushRemainder,IsToolResultPreview,IsTerminalToolCall,UsageFromStateDelta}`; the six local helper funcs (`flushRemainder`/`isToolResultPreview`/`isTerminalToolCall`/`usageFromStateDelta`/`anyInt`/`anyFloat`) were deleted from both files, and the now-unused `encoding/json` import dropped from each. The REPL renderer's behavior (`renderRunnerTurn`) is unchanged — proven green by the existing `cmd/aura` `TestRenderTurn_*` / `TestChat_*` cases — and the eval path now picks up the AnyInt superset (the json.Number token fix takes effect). The migrated tagged eval file compiles under both `cot_eval` and `live_e2e`.

## Task Commits

Each task committed atomically (D-11), direct `git commit -o -F <msg> -- <explicit paths>` (the `gsd` wrapper false-fails on the ~50s file-size hook):

1. **Task 1 — agentrender extraction (parity test + impl)** — `80f7c72c` (`feat(32-07)`); 2 files, +402.
2. **Task 2 — chat_render + eval migration, copies deleted** — `5ac02a55` (`refactor(32-07)`); 4 files, +31 / -286.

## Decisions Made

- **AnyInt = chat_render superset (intended eval FIX).** The old `internal/eval` `anyInt` had no `json.Number` case, so a UseNumber decoder / jsonb round-trip (M-07) silently zeroed token counts on the eval path. The extracted `AnyInt` adopts the `chat_render` branch; the parity test pins the lossy old behavior and asserts the new func differs from it for `json.Number` (T-32-07-FIX) — a documented behavior change, not a silent one.
- **`IsToolResultPreview(*agent.Event)` kept.** The optional `map[string]any` boundary-simplification in the plan was unnecessary: `internal/agent` is an allowed import and both call sites already hold the `*agent.Event`, so the original signature migrates with zero call-site churn and the boundary stays clean (agent + llm only).

## Deviations from Plan

**1. [Rule 3 — blocking + refactor-on-touch] folded the duplicate `cmd/aura` `TestUsageFromStateDelta`**
- **Found during:** Task 2 (deleting `usageFromStateDelta` from `chat_render.go` broke `cover_test.go`, which called it directly).
- **Issue:** `cover_test.go`'s `TestUsageFromStateDelta` exercised the now-deleted local helper. Its entire union (nil / int / int64 / float64 / json.Number / cost present-absent-unparseable) is now owned by `internal/agentrender/agentrender_test.go` (`TestUsageFromStateDelta` + `TestAnyInt` + `TestAnyFloat`).
- **Fix:** Replaced the duplicate test body with a cross-reference comment. `agentrender.UsageFromStateDelta` stays exercised end-to-end from `cmd/aura` via `TestChat_ToolUsingTurn` + the `TestRenderTurn_*` cases (through `renderRunnerTurn`). Per CLAUDE.md refactor-on-touch (dupl-folding, no test asilo nido).
- **Files modified:** `cmd/aura/cover_test.go`. **Commit:** `5ac02a55`.

**2. [Rule 3 — blocking + refactor-on-touch] removed the obsolete `capture_cot_eval_anyfloat_test.go`**
- **Found during:** Task 2 (deleting `anyFloat` from `capture_cot_eval.go` broke this tagged test, whose only function called it).
- **Issue:** `internal/eval/capture_cot_eval_anyfloat_test.go`'s sole test `TestAnyFloatJSONNumber` targeted the deleted local `anyFloat`. `agentrender.AnyFloat`'s json.Number valid/invalid is now covered by `agentrender_test.go` `TestAnyFloat`.
- **Fix:** Removed the file via the filesystem and staged the deletion with an explicit single-file pathspec (`git add <file>`), NOT `git rm` and NOT a bulk add — keeping the concurrent-Codex isolation intact.
- **Files deleted:** `internal/eval/capture_cot_eval_anyfloat_test.go`. **Commit:** `5ac02a55`.

**3. [Rule 3 — blocking] dropped now-unused `encoding/json` imports + updated stale comments**
- **Issue:** Both migrated files imported `encoding/json` only for the `json.Number` branch in the deleted `anyInt`/`anyFloat`; after deletion the import is unused (compile error). Several comments (the `capture_cot_eval.go` package doc's "~80 lines duplicated by design", the `cover_test.go` `TestRenderTurn_*` "flushRemainder" references) referred to the moved code.
- **Fix:** Removed the `encoding/json` import from `chat_render.go` and `capture_cot_eval.go`; updated the package doc + test comments to point at `internal/agentrender`. `go fmt` reformatted nothing (files already gofmt-clean); all touched files ≤600 LOC (max 278).
- **Impact:** None on behavior — mechanical.

---
**Total deviations:** 3 (2 duplicate-fold/blocking test repairs forced by the copy deletions, 1 import-cleanup + comment refresh). No architectural deviations; no new threat surface.

## Issues Encountered

- **Concurrent Codex on master:** unrelated agui/objectstore/document + `.planning/graphs/*` changes in the working tree throughout. Mitigated with explicit-pathspec `git commit -o`; those files were never staged or touched by this session (confirmed via `git show --stat` on both commits).
- **WSL go shim quirks:** `go` is a toolchain-downloading shim at `~/.local/bin/go`; `go env GOROOT` intermittently returns empty and the toolchain path contains an `@` that breaks WSL-interop path passing. Worked around by using `go fmt`/`go vet` (which call gofmt internally) instead of invoking the `gofmt` binary by path.
- **Untagged `go test ./internal/eval/` does not compile the migrated file:** `capture_cot_eval.go` is behind `//go:build cot_eval || live_e2e`, so the plan's untagged verify never sees it. Compile-verified the migration separately with `go vet -tags cot_eval` AND `go vet -tags live_e2e`.

## Threat Flags

None — behavior-preserving extraction with one documented eval-path fix (T-32-07-FIX), no new endpoints, auth paths, file access, or schema. The agent ⇸ agui boundary gained no back-edge (`go list -deps` asserts 0 `internal/agui`, T-32-07-BND).

## User Setup Required

None — internal refactor + documented eval token-count fix. No env, schema, or external-service changes.

## Next Phase Readiness

- **agentrender slice of QUAL-03 / ROADMAP C2 is done.** The shared REPL/eval render primitives live in `internal/agentrender` (100%), both call sites migrated, the eval json.Number bug fixed-and-documented.
- New leaf `internal/agentrender` is auto-included by `scripts/coverage_gate.sh` (exclude list is `db/sqlc|sandbox|agenttest|llm/client.go` only).
- Remaining QUAL-03 items continue in 32-08 (frontend dedup) per the phase plan.

## Self-Check: PASSED

- FOUND: internal/agentrender/agentrender.go (exports FlushRemainder/IsToolResultPreview/IsTerminalToolCall/UsageFromStateDelta/AnyInt/AnyFloat)
- FOUND: internal/agentrender/agentrender_test.go (100% cov, parity table documents the eval json.Number fix)
- FOUND: cmd/aura/chat_render.go + internal/eval/capture_cot_eval.go use agentrender.* (local copies deleted; rg returns NONE)
- FOUND: internal/eval/capture_cot_eval_anyfloat_test.go DELETED (subject moved to agentrender.AnyFloat)
- FOUND: go list -deps ./internal/agentrender/ has 0 internal/agui (boundary guard)
- FOUND commit: 80f7c72c (Task 1 — agentrender extraction)
- FOUND commit: 5ac02a55 (Task 2 — chat_render + eval migration)

---
*Phase: 32-quality-cleanup-dead-code-shared-helpers*
*Completed: 2026-06-30*
