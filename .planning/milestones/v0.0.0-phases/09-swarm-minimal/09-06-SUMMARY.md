---
phase: 09-swarm-minimal
plan: 06
subsystem: testing
tags: [swarm, cot_eval, live-e2e, mcp, mail-mcp, whatsapp-mcp, judge-rubric, dual-gate, quality-snapshot]

# Dependency graph
requires:
  - phase: 09-02
    provides: "internal/swarm engine (Run/RunConfig/ChildReport/Without) — the worker fan-out the harness drives via swarm_spawn"
  - phase: 09-03
    provides: "mcptools Deferred:true + per-server allowlist + mail/whatsapp recipes — the MCP mounts the harness exercises"
  - phase: 09-05
    provides: "swarm_spawn Deferred tool + cycle-free ctx seam (swarm.NewRunnerAdapter) registered at boot — the live runner the parent agent calls"
  - phase: 03 (Slice 1)
    provides: "internal/eval cot_eval harness (build tag + OPENROUTER gate + captureTurn + judge + report writer) — the surface this plan extends"
provides:
  - "swarmScenarios() — a NATURAL autonomous swarm prompt (no 'swarm'/'parallel' word, two independent self-mail + self-WhatsApp subtasks, unique read-back tags) + a no-over-spawn control, SEPARATE from scenarios() so the CoT harness never runs them"
  - "4 D-22 judge dimensions (autonomous parallelization / sub-answer correctness / aggregation quality / no-over-spawn) + runSwarmJudge ≥90% mean gate"
  - "swarm hard-floor predicates: countSwarmWorkers (off the ChildReport goal_index keys), calledSwarmSpawn, factsPresent, timingOK"
  - "TestSwarmE2E — the live dual-gate harness: real LlmAgent + swarm_spawn (live adapter) + mail/WhatsApp MCP mounts, hard floor + judge ≥90%, scored report to docs/"
  - "docs/aura-quality-snapshot.md Phase 9 swarm row + detail section (TBD placeholders + the exact operator command)"
affects: [09 Gate-3 verification, AG-UI Phase 12 swarm progress, Phase 16 mail/whatsapp recipe hardening]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Operator-run live tier as a SEPARATE scenario generator (swarmScenarios) behind the cot_eval tag — the CoT entry point never reaches it; only TestSwarmE2E does"
    - "Live MCP read-back as ground truth: execute the SAME mounted MCP tool (mail__search_emails / whatsapp__list_messages) via reg.Get(name).Execute over a tool-call ctx, poll for a per-run tag, fall back to the spilled sidecar FullPath"
    - "Placeholder-at-commit quality-snapshot row with explicit TBD cells + the exact operator command that fills them (prior-phase convention)"

key-files:
  created:
    - internal/eval/harness_swarm_e2e_test.go
  modified:
    - internal/eval/dataset_cot_eval.go
    - internal/eval/scoring_cot_eval.go
    - internal/eval/judge_cot_eval.go
    - internal/eval/harness_cot_eval_test.go
    - docs/aura-quality-snapshot.md

key-decisions:
  - "swarmScenarios() is SEPARATE from scenarios() (not an append) — the CoT harness scores every scenario in scenarios(); a swarm scenario there would run live MCP sends in the wrong harness. TestSwarmE2E is the only consumer."
  - "Equal-weight judge mean for the ≥90% gate (D-22 fixes the four dimensions + the ≥90% average; the weighting is Claude's Discretion — an equal mean is the least-surprising default)."
  - "Relocated reportPath/dimResult/scenarioMetrics from harness_cot_eval_test.go into the non-test scoring_cot_eval.go (Rule 3) so `go build -tags cot_eval ./internal/eval/` exits 0 — the plan's literal acceptance criterion (go build was exiting 1 at HEAD because the non-test scoring file referenced test-only symbols)."
  - "Worker count is read off the ChildReport array (count of \"goal_index\" keys in the tool-result preview) not the tool-call count — robust to the report spilling to a sidecar; swarm_spawn is ONE call carrying N goals."
  - "Single-worker baseline = one of the two subtasks alone; a baseline failure makes timingOK advisory-pass (never hard-fails on a missing baseline)."

patterns-established:
  - "Live dual-gate (deterministic hard floor + LLM judge ≥90% average) over a real agent with real MCP read-back — the SC#5 shape for any future autonomous-behavior eval"
  - "MCP read-back via the same mounted server at the right JID per direction (D-19 self-chat duality) as the ground-truth assertion"

requirements-completed: [CAP-03]

# Metrics
duration: ~18min
completed: 2026-06-04
---

# Phase 9 Plan 06: Live Dual-Gate Swarm E2E Summary

**A NATURAL-prompt (no "swarm" word) live dual-gate swarm E2E (`TestSwarmE2E`, cot_eval): the REAL parent `LlmAgent` over the REAL openai_compat client with `swarm_spawn` (live adapter) + mail/WhatsApp MCP mounts must autonomously fan out ≥2 workers, land self-mail + self-WhatsApp messages read-back-verified via the same MCP at the right JID, finish < 1.5× a single-worker baseline, and pass a ≥90% 4-dimension judge — with a placeholder quality-snapshot row at commit that the operator live run fills.**

## Performance

- **Duration:** ~18 min
- **Started:** 2026-06-04T10:32Z
- **Completed:** 2026-06-04T10:50Z
- **Tasks:** 2
- **Files modified:** 5 (1 created, 4 modified)

## Accomplishments
- **Task 1 — scenarios + rubric (compile-time):** `swarmScenarios()` adds a NATURAL autonomous prompt (two independent self-contained subtasks: compute-and-self-mail, compute-and-self-WhatsApp; unique `%MAIL_TAG%`/`%WA_TAG%` markers the harness substitutes per run; asserted free of "swarm"/"parallel") + a trivial single-task control that MUST NOT spawn. Four D-22 judge dimensions + `judgeSwarmGate=0.90` + `runSwarmJudge` (equal-weight mean ≥90%). Hard-floor predicates `countSwarmWorkers`/`calledSwarmSpawn`/`factsPresent`/`timingOK`.
- **Task 2 — TestSwarmE2E harness (live, operator-run) + snapshot:** the real `LlmAgent` over the real client with a registry built like `buildBaseRegistry` + `swarm_spawn` (live `swarm.NewRunnerAdapter`) + mail/whatsapp MCP mounts (same managed-config boot path as `aura chat`, D-20 allowlists). Hard floor (≥2 workers, facts, mail+WhatsApp MCP read-back at the right JID, timing) + judge ≥90% + control no-over-spawn; scored report to `docs/aura-swarm-eval-2026-06-04.md`; the file header documents the full operator invocation (bridge bring-up @6de1dcd + REST :8080 405 health-check + `.env` + `AURA_EVAL_SELF_*` + `go test`).
- **Quality snapshot:** a Phase 9 swarm row + detail section with explicit TBD cells (worker-count / timing-ratio / mail+WA read-back / judge-average / no-over-spawn) and the exact operator command that fills them.
- **Gates:** `go vet -tags cot_eval` + `go build -tags cot_eval` clean; `golangci-lint --build-tags cot_eval` 0 issues; the test binary compiles and runs no-match (no live LLM call); cot_eval is in NO CI workflow (the one legitimate skip honored).

## Task Commits

Each task was committed atomically:

1. **Task 1: swarm + control cot_eval scenarios + 4-dim judge rubric (D-22)** — `31599320` (feat)
2. **Task 2: TestSwarmE2E live dual-gate harness + quality-snapshot placeholder (SC#5/D-22)** — `ab0b8a10` (feat)

## Files Created/Modified
- `internal/eval/harness_swarm_e2e_test.go` (494 LOC, NEW) — `TestSwarmE2E` (cot_eval, OPENROUTER + `AURA_EVAL_SELF_*` gated); `buildSwarmRegistry` (production tools + swarm_spawn live adapter + mail/whatsapp MCP mounts with D-20 allowlists); `runSwarmScenario`/`runControlScenario`/`runAgentTurn`/`swarmBaselineMS`; `mailReadBack`/`waReadBack`/`pollToolForTag`/`toolResultContains` (MCP read-back ground truth); `enforceSwarm` (dual gate); `writeSwarmReport`.
- `internal/eval/dataset_cot_eval.go` — `swarmExpect` struct + `swarmScenarios()` + 4 swarm dimensions + `judgeSwarmGate`; swarm fields on `scenario`.
- `internal/eval/judge_cot_eval.go` — `swarmRubrics` (4 dims) + `runSwarmJudge` (≥90% mean) + `runJudgeUser` (shared drain; `runJudge` delegates to it, no dupl).
- `internal/eval/scoring_cot_eval.go` — swarm hard-floor predicates + relocated `reportPath`/`dimResult`/`scenarioMetrics` (build-fix, Rule 3).
- `internal/eval/harness_cot_eval_test.go` — the three relocated symbols removed (now in the non-test file).
- `docs/aura-quality-snapshot.md` — Phase 9 swarm matrix row + detail section (TBD + operator command).

## Decisions Made
- **`swarmScenarios()` separate from `scenarios()`** — the CoT entry point scores every scenario in `scenarios()`; a swarm scenario there would fire live MCP sends inside `TestCoTEval`. Keeping them in a distinct generator means only `TestSwarmE2E` ever runs them, behind the same tag + gate.
- **Equal-weight judge mean** — D-22 fixes the four dimensions + the ≥90% average, not the weighting (Claude's Discretion); an equal mean (≥4.5/5 average) is the least-surprising default.
- **Worker count off the ChildReport array** — `swarm_spawn` is one tool call carrying N goals, so the worker count is the number of `"goal_index"` entries in the returned report (robust to a sidecar spill), not the tool-call count.
- **Build-symbol relocation (Rule 3)** — see Deviations.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Relocated report-infra symbols so `go build -tags cot_eval` exits 0**
- **Found during:** Task 1 (running the plan's `go build -tags cot_eval ./internal/eval/` acceptance gate)
- **Issue:** `go build -tags cot_eval ./internal/eval/` exited **1 at HEAD** (pre-existing, verified by stashing my edits): the NON-test file `scoring_cot_eval.go` referenced `reportPath`/`dimResult`/`scenarioMetrics`, which were defined in `harness_cot_eval_test.go` (a `_test.go` file `go build` never compiles). The plan lists `go build` exit 0 as a hard acceptance criterion twice, and Task 2's harness deepens the non-test → report-infra dependency.
- **Fix:** Moved `reportPath` + `dimResult` + `scenarioMetrics` from `harness_cot_eval_test.go` into `scoring_cot_eval.go` (a non-test, `cot_eval`-tagged file) with a forwarding comment. No behavior change; `go vet` (which already compiled them) stays green, and `go build` now exits 0.
- **Files modified:** internal/eval/scoring_cot_eval.go, internal/eval/harness_cot_eval_test.go
- **Verification:** `go build -tags cot_eval ./internal/eval/` → BUILD-OK (exit 0); `go vet` clean; test binary compiles + runs no-match.
- **Committed in:** `31599320` (Task 1 commit)

**2. [Rule 1 - Bug] Read-back used a non-existent `ToolResult.Content` field**
- **Found during:** Task 2 (first compile of the harness)
- **Issue:** the read-back scanned `res.Preview + res.Content`, but `tools.ToolResult` has no `Content` field (the body is `Preview`; spilled bytes live at `FullPath` when `Truncated`).
- **Fix:** added `toolResultContains(res, tag)` — scans `Preview`, then reads `FullPath` when `Truncated`; the read-back uses a generous 1 MiB preview cap so a 20-message list / multi-hit search is not truncated before the scan.
- **Files modified:** internal/eval/harness_swarm_e2e_test.go (pre-commit; no separate commit)
- **Verification:** `go vet` + `go build -tags cot_eval` clean; golangci-lint 0.
- **Committed in:** `ab0b8a10` (Task 2 commit)

---

**Total deviations:** 2 auto-fixed (1 blocking build-symbol relocation, 1 bug).
**Impact on plan:** Both mechanical and in-scope. The relocation makes the literal acceptance gate pass without touching behavior; the `ToolResult` fix is a straight API correction. No scope creep — no `internal/swarm/` or tool/boot code was touched (this plan only extends the eval harness + the snapshot doc, exactly as the objective states).

## Issues Encountered
- A parallel user session holds a `g2-plan-isolation` git stash; my own WIP stash (used only to measure HEAD's pre-edit build exit code) was created and popped cleanly, leaving the parallel stash intact (verified). The untracked `docs/research/mcp-sidecar-supervision.md` is not mine and was never staged.

## User Setup Required
None in code. The live tier (operator post-merge, NOT this plan's scope) requires: `OPENROUTER_API_KEY`, the user's own mail/WhatsApp self-accounts, the whatsmeow bridge up (fork @6de1dcd, REST :8080), and `AURA_EVAL_SELF_MAIL`/`AURA_EVAL_SELF_PHONE`/`AURA_EVAL_WA_CHAT_SELF` — all documented in the harness header + the quality-snapshot detail section + VALIDATION.md Manual-Only.

## Next Phase Readiness
- The compile-time deliverables are committed and green; CAP-03's Gate-3 live evidence is one operator run away (the harness + scenarios + rubric + snapshot row all land at commit, TBDs flagged).
- The dual-gate harness is the SC#5 surface the `/gsd-verify-work` step references; the operator live run replaces the TBD numbers and is the Gate-3 sign-off item.

## Self-Check: PASSED

---
*Phase: 09-swarm-minimal*
*Completed: 2026-06-04*
