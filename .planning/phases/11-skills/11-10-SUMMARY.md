---
phase: 11-skills
plan: 10
subsystem: testing
tags: [cot_eval, skills, xlsx, north-star, deepseek, sandbox-agent, self-extension, judge-gate]

# Dependency graph
requires:
  - phase: 11-09
    provides: "the post-deletion tree — no internal/skills catalog/installer, no tools.SkillTool Catalog/Installer fields, no SkillCatalogURL/SkillInstallTimeoutSec/SkillCatalogDisabled config; the always-on find-skills-aura builtin + RenderAlwaysBlock"
provides:
  - "A compiling, operator-runnable cot_eval xlsx North-Star eval aligned with the shipped no-ceremony architecture (#51/D-40)"
  - "Action-aware capture (classifyCall over structured tool args) that makes the D-35 self-install assertion satisfiable + ground-true"
  - "The new D-35 hard floor: self-install evidence from structured args (npx skills add anthropics/skills --skill xlsx) + a fresh .xlsx artifact (newer than run start, openpyxl read-back, today's date)"
  - "No-key pure-function tests (classifyCall arg parsing + seam-free registry construction) that catch a structural break without OPENROUTER_API_KEY"
affects: [11-skills, slice-1, slice-3, slice-13, cot_eval]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Action-aware tool-call capture: parse resp.ToolCalls[].Function.Arguments (the sandbox command line), never the model prose, for pass/fail signals (spike 012a)"
    - "Seam-free eval registry: discovery+install ride the sandbox terminal + the messages[1] always-block, no Go catalog/installer tool"
    - "No-key structural slot: pure table-driven tests over the capture + registry run WITHOUT OPENROUTER_API_KEY so the live tier being gated off cannot hide a structural regression"

key-files:
  created:
    - internal/eval/classify_cot_eval_test.go
  modified:
    - internal/eval/skills_cot_eval_test.go
    - internal/eval/scenarios_skills.go
    - internal/eval/judge_cot_eval.go
    - internal/eval/dataset_cot_eval.go
    - internal/eval/skills_xlsx_verify_cot_eval_test.go
    - internal/eval/capture_cot_eval.go
  deleted:
    - internal/eval/skills_adapters_cot_eval.go

key-decisions:
  - "Matched spike 012a buildSkillDrivenRegistry exactly: the seam-free registry registers NO skill tool — so the adapter file (evalSkillLoader/Writer/Catalog/Installer) was deleted whole, not partially kept. A kept-but-unused evalSkillLoader would fail golangci-lint's unused-symbol check; the validated 4/4-PASS shape has no skill tool at all."
  - "classifyCall is a PURE function of (name, rawArgs) mutating a *skillsResult — no I/O — so the no-key TestClassifyCall_* tests table-drive it directly. It lives in classify_cot_eval_test.go (the no-key slot) and the live harness calls it via captureSkillCalls."
  - "capture_cot_eval.go gained a toolArgs []string slice aligned with toolNames so the harness can read structured args; this is the minimal change to the shared capture that the swarm/CoT harnesses still compile and pass against."
  - "Artifact freshness enforced via an mtime>=run-start epoch check inside the openpyxl read-back (XLSX_FRESH/XLSX_STALE), so a stale .xlsx from a prior run cannot satisfy the hard floor (spike-012a -newermt discipline)."

patterns-established:
  - "Probe-must-verify-artifact-not-reply: the hard floor reads structured tool args + the live sandbox artifact, never r.Reply (T-11-10-T1)"
  - "Negative machine assertions in the verify block: deleted seams + dropped dimensions must not survive as code identifiers (the rewrite avoids the literal tokens even in comments so the ! grep gate is clean)"

requirements-completed: [CAP-07, CAP-08]

# Metrics
duration: ~40min
completed: 2026-06-06
---

# Phase 11 Plan 10: Slice 7g xlsx North-Star Eval Rewrite Summary

**Rewrote the broken cot_eval xlsx North-Star eval to the spike-012a action-aware seam-free shape: it now captures structured tool args (the sandbox command line), gates D-35 on self-install evidence + a fresh-artifact ground truth, judges on 2 dims (install-prudence dropped), and ships no-key pure-function tests that catch a structural break without OPENROUTER_API_KEY.**

## Performance

- **Duration:** ~40 min
- **Started:** 2026-06-06 (worktree agent-ab6bd8dd81f75047b)
- **Completed:** 2026-06-06
- **Tasks:** 1 completed
- **Files modified:** 8 (1 created, 6 modified, 1 deleted)

## Accomplishments
- The eval package compiles + vets again under `-tags cot_eval` against the post-11-09 tree (it previously failed: it referenced `evalSkillCatalog`/`evalSkillInstaller`, `tools.CatalogResult`, `tools.InstallSummary`, `skills.CatalogClient`, `skills.Installer` — all deleted in 11-09).
- The D-35 gate is now MEASURABLE: the old name-only ordered-subsequence assertion (catalog→ask_user→install→sandbox_exec) was structurally unsatisfiable (spike 012b root cause). It is replaced by action-aware capture (`classifyCall` over `resp.ToolCalls[].Function.Arguments`) that flags self-install evidence from the real `npx skills add` command line.
- The new D-35 hard floor reads only ground truth: `selfInstall && installTarget (anthropics/skills) && installSel (--skill xlsx)` from structured args, AND a FRESH `.xlsx` (newer than run start) that opens via openpyxl and contains today's date — never the model's prose.
- The eval registry is seam-free (spike-012a `buildSkillDrivenRegistry`): text_response + tool_search + read_tool_output + current_time + ask_user + web_search + web_fetch + sandbox_exec, with the always-on `find-skills-aura` body injected at messages[1] via `RenderAlwaysBlock`. No skill tool, no catalog/installer.
- The judge now averages exactly 2 dims (capability_gap_recognition + skill_output_quality); the install-prudence dimension + its rubric + its const are gone, and the capability-gap rubric describes the sandbox-terminal self-extension path (no catalog/approval flow).
- New no-key structural slot (`classify_cot_eval_test.go`): table-driven `TestClassifyCall_*` (real command lines → flags, incl. malformed-JSON-no-panic) + `TestRegistry_SeamFree` (asserts the tool set AND that no skill/catalog/install tool is registered). They run WITHOUT `OPENROUTER_API_KEY` and do not `t.Skip` on it.

## Task Commits

Each task was committed atomically:

1. **Task 1: Rewrite the xlsx North-Star eval — action-aware capture + new D-35 ground-truth signals + seam-free registry + no-key structural tests** - `547b6efc` (refactor)

## Files Created/Modified
- `internal/eval/classify_cot_eval_test.go` (NEW) — the pure `classifyCall` arg parser + `captureSkillCalls`/`makeAskCall` glue + `oneLine`/`mustJSONString` helpers, and the no-key table-driven `TestClassifyCall_SelfInstall`/`TestClassifyCall_Accumulates`/`TestRegistry_SeamFree`.
- `internal/eval/skills_cot_eval_test.go` — rewritten harness: seam-free registry builder (`buildSkillsRegistry` + the pure `buildSeamFreeSkillsRegistry`), messages[1] always-block wiring, the action-aware drive loop (`driveSkillsLoop`), the rewritten `skillsResult` (selfInstall/installTarget/installSel/xlsxFresh; seqOK/installApprove removed), 2-dim judge call, and the rewritten report + `enforceSkills`.
- `internal/eval/scenarios_skills.go` — `skillsExpect` now carries `installTargetRepo` (anthropics/skills) + `installSelector` (xlsx) instead of `catalogSkill`/`requiredSeq`; the scenario dimensions drop install-prudence.
- `internal/eval/judge_cot_eval.go` — `skillsRubrics` is now 2 entries (install-prudence rubric removed); the capability-gap rubric prose updated for the sandbox-terminal self-extension path.
- `internal/eval/dataset_cot_eval.go` — the install-prudence dimension const removed (now unused); `judgeSkillsGate` doc updated to 2 dims.
- `internal/eval/skills_xlsx_verify_cot_eval_test.go` — `verifyXlsxArtifact` gains a `runStart` param + an mtime>=run-start freshness check (XLSX_FRESH/XLSX_STALE) so a stale artifact cannot satisfy the floor.
- `internal/eval/capture_cot_eval.go` — `turnCapture` gains a `toolArgs []string` slice (aligned with `toolNames`) populated from `Function.Arguments`, enabling action-aware capture.
- `internal/eval/skills_adapters_cot_eval.go` (DELETED) — the catalog/installer bridges (`evalSkillCatalog`/`evalSkillInstaller`, and the now-unused `evalSkillLoader`/`evalSkillWriter`) over the seams 11-09 deleted.

## Verification

All run against the post-11-09 tree in this worktree:

- `go build -tags cot_eval ./internal/eval/...` — PASS
- `go vet -tags cot_eval ./internal/eval/...` — PASS
- `go test -tags cot_eval -run 'TestClassify|TestRegistry' ./internal/eval/` (Windows, `OPENROUTER_API_KEY` unset) — PASS
- `go test -race -tags cot_eval -run 'TestClassify|TestRegistry' ./internal/eval/` (WSL native race, key unset) — PASS
- `go test -race -tags cot_eval ./internal/eval/` (WSL, full package, key unset) — PASS (live tiers correctly skip; structural tier runs)
- `! grep -rn "evalSkillCatalog\|evalSkillInstaller\|skillCatalog\|skillInstaller\|CatalogResult\|InstallSummary" internal/eval/` — PASS (no matches)
- `! grep -rn "dimInstallPrudence\|install_prudence\|installApprove\|seqOK" internal/eval/` — PASS (no matches, including comments — the rewrite avoids the literal tokens)
- `grep -rn "Function.Arguments\|skills add\|--skill" internal/eval/` — 33 matches (action-aware capture present)
- `grep -rln "newermt\|openpyxl\|anthropics/skills" internal/eval/` — matches in classify/scenarios/harness/verify
- `! grep -rn "Catalog:\|Installer:" internal/eval/` — PASS (no skill tool with a Catalog/Installer field)
- `golangci-lint run --build-tags cot_eval ./internal/eval/` (WSL) — 0 issues
- Default (no-tag) `go build ./...` + `go vet ./internal/eval/...` + `go build -tags live_e2e ./internal/eval/...` — all PASS (the shared capture change did not regress the other tiers)

The full live `TestSkillsE2E` xlsx tier stays OPERATOR-run + OPENROUTER-gated, behind the `cot_eval` build tag — never CI (T-11-10-I1; its `t.Skip` on the unset key is unchanged).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Deleted the whole adapter file instead of keeping evalSkillLoader**
- **Found during:** Task 1 (registry construction)
- **Issue:** The plan said "keep evalSkillLoader (and evalSkillWriter only if still referenced)". But the seam-free registry matches spike 012a `buildSkillDrivenRegistry`, which registers NO skill tool — so `evalSkillLoader`/`evalSkillWriter` would become unused symbols, which golangci-lint flags as a hard failure (CLAUDE.md Gate 2). The plan's own action text authorizes this: "spike 012a buildSkillDrivenRegistry had no skill tool at all — match that".
- **Fix:** `git rm internal/eval/skills_adapters_cot_eval.go` (the catalog/installer bridges AND the now-orphaned loader/writer bridges). The always-on teaching rides messages[1] instead.
- **Files modified:** `internal/eval/skills_adapters_cot_eval.go` (deleted).
- **Commit:** `547b6efc`

**2. [Rule 3 - Blocking] Extended the shared turnCapture with toolArgs**
- **Found during:** Task 1 (action-aware capture)
- **Issue:** `classifyCall` needs the structured tool-call arguments, but `turnCapture` only recorded tool NAMES (`toolNames`). The spike 012a harness reads `resp.ToolCalls[].Function.Arguments` directly in its own drive loop; the eval harness routes through the shared `captureTurn`.
- **Fix:** Added a `toolArgs []string` slice to `turnCapture` (aligned with `toolNames`), populated from `Function.Arguments` in both the tool-call branch and (empty slot) the pause branch. Verified the swarm/CoT harnesses still compile + pass.
- **Files modified:** `internal/eval/capture_cot_eval.go`.
- **Commit:** `547b6efc`

**3. [Rule 1 - Bug] Comment-token leakage broke the negative grep gate**
- **Found during:** Task 1 verify
- **Issue:** The first negative-grep run FAILED because explanatory comments still contained the literal tokens `install_prudence`, `seqOK`, `installApprove`. The acceptance treats these `! grep` checks as machine-runnable gates over the whole `internal/eval/` tree — comments count.
- **Fix:** Rephrased every comment to use "the install-prudence dimension" / "ordered-subsequence" / "install-approval" prose instead of the code-identifier tokens. The gate is now clean.
- **Files modified:** classify/dataset/judge/scenarios/skills_cot_eval test files.
- **Commit:** `547b6efc`

## Known Stubs

None. The live `TestSkillsE2E` tier is operator-gated by design (paid LLM call), not a stub — its skip on an unset `OPENROUTER_API_KEY` is the documented legitimate skip, and the no-key structural slot (`TestClassify*`/`TestRegistry*`) exercises the capture + registry surface live without the key.

## Self-Check: PASSED

- FOUND: `internal/eval/classify_cot_eval_test.go` (created)
- FOUND: `internal/eval/skills_adapters_cot_eval.go` is deleted (git diff confirms D)
- FOUND: commit `547b6efc` in git log
