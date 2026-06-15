---
phase: 11-skills
plan: 08
subsystem: testing
tags: [skills, cot_eval, e2e, ci, shell_exec, fs_write, sandbox, mutation-testing, coverage, xlsx, deepseek-v4]

# Dependency graph
requires:
  - phase: 11-07
    provides: sandbox-agent bearer token + ro /skills mount + baked xlsx deps (D-17/D-38) — the substrate the North-Star E2E exercises
  - phase: 11-01..06
    provides: the ONE non-deferred skill tool (ActionRouter), loader/validator/writer, audit-immutable 0010 trigger, messages[1] always-block, snippet save/by-path exec
provides:
  - The Phase-11 Gate-3 dual-gate evidence — CAP-07/CAP-08 proven TRUE end-to-end against the live substrate
  - chat-surface E2E gate (real `aura chat` binary) — the closing score per amendment #53/D-42
  - CI workflow (.github/workflows/skills.yml) gating unit + db_integration + sandbox_integration + fuzz + mutation + ≥85% coverage, no-skip-as-green
  - xlsx North-Star cot_eval scenario (structural guard) + 2 CI-runnable smokes (haiku flow + snippet reuse) + messages[1] cache-invariant extension
  - the host full-terminal skills loop (shell_exec primary + fs_write authoring) — the #52/#53 toolbelt that finally closed the autonomous loop
affects: [phase-12-agui, phase-13-channels, phase-18-snippet-reuse, eval-harness, ci-gates]

# Tech tracking
tech-stack:
  added: [chat-e2e-gate.sh real-binary harness, skills.yml CI workflow, go-mutesting spot-check on validator.go/writer.go]
  patterns:
    - "Real-binary chat-surface gate supersedes synthetic judge as the CLOSING score (#53/D-42 operator directive)"
    - "Eval registry MIRRORS the production registry — shell_exec + the 5 fs tools present, NOT a sandbox-only artificial environment (#52 rule 4, #53 fs-parity)"
    - "Host full terminal (shell_exec) is the PRIMARY skills-loop surface; sandbox_exec is deliberate per-run escalation for untrusted code (#52/D-41)"
    - "File CONTENT is authored via fs_write/fs_edit, never shell heredocs/echo blobs (#53/D-42)"
    - "Artifact-not-reply ground truth: the .xlsx is opened + content-verified (openpyxl read-back, today's date), never asserted on r.Reply alone"

key-files:
  created:
    - internal/eval/scenarios_skills.go
    - internal/eval/skills_cot_eval_test.go
    - internal/eval/skills_adapters_cot_eval.go
    - internal/eval/judge_cot_eval.go
    - internal/eval/capture_cot_eval.go
    - internal/eval/classify_cot_eval_test.go
    - internal/skills/smoke_test.go
    - .github/workflows/skills.yml
    - scripts/chat-e2e-gate.sh
  modified:
    - internal/agent/prompt.go
    - internal/agent/tools/shell_exec.go
    - internal/agent/tools/shell_exec_test.go
    - cmd/aura/cache_audit.go
    - scripts/cache_invariant_audit.sh
    - docs/aura-quality-snapshot.md
    - docs/system_prompt.txt
    - docs/aura-skills-eval-2026-06-05.md

key-decisions:
  - "The real `aura chat` binary gate is the CLOSING score; the synthetic TestSkillsE2E cot_eval judge is demoted to a structural guard (#53/D-42 operator directive)"
  - "The skills self-extension loop AND the D-35 North-Star gate ride the HOST full terminal (shell_exec), not the sandbox (#52/D-41)"
  - "install_prudence judge dimension DROPPED; the install-approval ceremony (catalog→ask_user→install) is SUPERSEDED — calling install IS the action (#51/D-40, #52)"
  - "AURA_AGENT_JOB_MAX_DURATION_SEC makes the agent_job wall-clock env-tunable (was a 120s swarm-child default starving real job artifacts) (#53/D-42)"

patterns-established:
  - "no-skip-as-green CI: composed DSNs + AURA_SANDBOX_AGENT_TOKEN + CI=true → t.Fatal-on-unset; sub-second integration runtime is a skip tell"
  - "Live paid eval (OPENROUTER) is operator-run, explicitly NOT CI — the cot_eval/chat-surface gates never leak the key into CI"

requirements-completed: [CAP-07, CAP-08]

# Metrics
duration: ~17h (wall, across the #51→#52→#53 rewrite churn)
completed: 2026-06-06
---

# Phase 11 Plan 08: Skills Gate-3 Dual-Gate Evidence Summary

**The Phase-11 closing gate: the real `aura chat` binary autonomously recognizes a capability gap, installs the xlsx skill, fetches today's market data through the host full terminal, and produces a fresh openpyxl-verified .xlsx — PASS 6/6 = 100% on two consecutive runs (151s/233s wall) — with CI gating all tiers (unit + db_integration + sandbox_integration + fuzz + mutation + ≥85% coverage) no-skip-as-green.**

> **Retroactive reconstruction.** This SUMMARY was written 2026-06-06 at operator direction. The 11-08 work landed across the #52/#53 PRD-rewrite churn (the plan's literal must_haves were superseded mid-flight three times), and the SUMMARY was never written at the time — the plan closed in production (28 commits, the chat-surface gate explicitly "closes Phase 11") but left the safe-resume anomaly of committed work with no SUMMARY. This document reconstructs the ground truth from git + the quality snapshot + the amendment record. No 11-08 work was re-executed.

## Performance

- **Duration:** ~17h wall (first commit 2026-06-05T19:44:34+02:00 → last 2026-06-06T12:54:58+02:00), spanning three PRD amendments
- **Started:** 2026-06-05T19:44:34+02:00
- **Completed:** 2026-06-06T12:54:58+02:00
- **Tasks:** 2 auto tasks + 1 human-verify checkpoint (Gate-3), realized across 28 commits
- **Files modified:** 9 created, 8 modified (skills-eval surface)

## How amendments #51–#53 reshaped the plan

The 11-08 PLAN.md `must_haves` were written PRE-amendment and demanded a literal `catalog→ask_user→install→sandbox_exec` tool_use sequence judged across THREE dimensions (incl. install prudence). Three amendments landed mid-execution and SUPERSEDED that spec:

- **#51 / D-40 (skill-driven self-extension, no ceremony):** the Go catalog client + Go installer + `action=catalog`/`action=install` were DELETED (11-09). Discovery = `npx skills find`, install = `npx skills add` self-service via shell, NO approval round-trip. The `install_prudence` judge dimension is dropped — calling install IS the action. The catalog→ask_user→install→sandbox_exec hard-floor sequence is retired.
- **#52 / D-41 (host full terminal primary):** the eval registry mounted ONLY `sandbox_exec` ({command,args} direct-spawn, no shell) and FAILED the live gate 0.30 — the model couldn't write a script with redirects, hit "unknown tool" reaching for the `shell_exec` the prompt teaches as primary. Verdict: **"sandbox is the issue. Aura need run full terminal like you."** The skills loop AND the D-35 gate now ride the HOST full terminal (`tools.ShellExec`). The eval registry MUST mirror the production registry.
- **#53 / D-42 (fs-tool authoring + the real-binary gate):** 9 post-#52 runs showed the model closing discovery+install but burning ~10 shell_exec calls failing to WRITE the multi-line build script (dead heredocs / Windows backslash footgun). Fixes: register the 5 native `fs_*` tools in the eval registry (parity), teach the prompt to WRITE via fs tools not shell heredocs, CRLF→LF normalize in shell_exec. **And the closing directive:** the synthetic cot_eval judge "doesn't measure anything valid — the only real test is `aura chat`, what the user uses." The **real-binary chat-surface gate supersedes the synthetic judge as the closing score** (D-43 reinforces with the completion-critic gate). The cot_eval tier remains as a structural guard.

Net: the plan's Task 1/Task 2 artifacts shipped (xlsx scenario, 2 smokes, cache-invariant, skills.yml CI), but the Gate-3 closing evidence (Task 3) is the AMENDED real-binary gate, not the original synthetic-judge ≥90% over three dimensions.

## Accomplishments

- **Closing gate (chat-surface E2E):** `scripts/chat-e2e-gate.sh` drives the REAL `aura chat new` one-shot with the natural North-Star prompt and scores ground truth only — clean exit / fresh .xlsx in workspace / openpyxl re-open / today's date / PG user+assistant turns persisted / wall-clock budget. **PASS 6/6 = 100% on two consecutive runs (151s and 233s wall).** CoT on both: skill tool (list+use of installed xlsx skill) → shell_exec data fetch → fs_write build script → verify — the #53 toolbelt working end-to-end on the product surface.
- **`aura serve` agent_job parity:** measured same day — 118s e2e, claim +4s after fire, 3/3 artifacts verified via openpyxl.
- **CI gate (skills.yml):** unit (incl. TestHaikuFlow/TestSnippetReuse) + db_integration (SC#1 audit INSERT + SC#2 immutability + migration round-trip + TTL) + FuzzSkillValidator 60s (SC#3) + sandbox_integration TestSnippetExec (SC#4) + go-mutesting ≥70% on validator.go/writer.go + ≥85% combined coverage across the FULL tag matrix; composed DSNs + AURA_SANDBOX_AGENT_TOKEN + CI=true arm no-skip-as-green.
- **Combined coverage = 86.6%** (2026-06-06, scripts/coverage_gate.sh, WSL live).
- **Structural cot_eval guard:** the xlsx North-Star scenario + judge rubric + action-aware capture remain wired (OPENROUTER-gated, operator-run NOT CI).
- **Cache-invariant extended:** the 20-turn replay now asserts messages[1] always-block (D-07) + skill manifest-in-Description (D-06) byte-stability alongside the messages[0] invariant.
- **Host toolbelt hardening:** shell_exec POSIX-bash-on-Windows + persistent cwd per session + workspace path in the tail hint + CRLF guard; fs_write authoring taught in the canonical prompt; empty-completion nudge recovery.

## Task Commits (reconstructed inventory — 28 commits)

The 11-08 series in chronological order (oldest first):

1. `f22a2754` (feat) — xlsx North-Star cot_eval scenario + 2 smokes + messages[1] cache-invariant (D-35)
2. `a4ac741c` (ci) — skills.yml gate (all tiers, no-skip-as-green) + Phase-11 quality snapshot rows
3. `8ab92b81` (fix) — route capability gaps into the skills system (amendment #49, D-39)
4. `0909349f` (fix) — catalog-first discipline at the decision point (amendment #49, iteration 2)
5. `4f7b834d` (fix) — concrete next-step routing + pip-door closed (amendment #49, iteration 3)
6. `65486b2a` (fix) — catalog query-by-format teaching (amendment #49, iteration 4)
7. `9ad21c4c` (fix) — install-leg routing: repo shorthand + multi-skill selector + catalog format-boost (amendment #49, iteration 5)
8. `a2e6fe2c` (style) — De Morgan fold in normalizeRepoShorthand (staticcheck QF1001)
9. `4359f8a9` (fix) — calling install IS the approval request (amendment #49, iteration 6)
10. `b31f6d92` (docs) — reconcile dual-gate artifacts to post-#51 shape + stamp quality numbers
11. `b98687c2` (fix) — eval rides the host full terminal: shell_exec registry + host artifact verify (#52/D-41)
12. `c6b1251c` (fix) — find-skills-aura host-terminal teaching + tool_search stale catalog tail (#52/D-41)
13. `b78862d6` (fix) — shell_exec prefers POSIX bash on Windows — Claude-Code Bash parity (#52/D-41)
14. `c3278452` (fix) — actionable hint on truncated shell_exec args (D-15 self-correction)
15. `88e4cbe0` (fix) — empty provider completion recovers with a nudge, never ends the turn silently
16. `3c31ae03` (fix) — prompt names the save folder + one transport retry in the drive loop
17. `ddd3d77d` (fix) — run-dir prefix must not leak a forbidden hint word into the natural prompt
18. `63822f35` (feat) — Bash-tool parity: persistent cwd per session + workspace path in the per-turn tail hint (#52/D-41)
19. `2b7107fc` (feat) — fs-tool file authoring + eval fs parity + shell_exec CRLF guard (#53/D-42)
20. `910a6f54` (feat) — skills-first doctrine hardening: libraries are not a method (#53/D-42)
21. `e58b25cc` (feat) — agent_job announces its workspace in the tail hint (#53/D-42)
22. `8ecb5d5c` (feat) — AURA_AGENT_JOB_MAX_DURATION_SEC: agent_job wall-clock is env-tunable (#53/D-42)
23. `30234536` (feat) — **chat-surface E2E gate — the real-binary gate closes Phase 11 (#53/D-42)**

_Note: `git log --grep="11-08"` returns 28 entries; the 23 above are the substantive content commits. The remaining are intermediate fix iterations within the #49 churn that were folded forward by later commits — the inventory above captures every distinct one-liner in the series. The Phase-11 detail-section gate-closing commit is `30234536`._

## Files Created/Modified

- `internal/eval/scenarios_skills.go` — xlsx North-Star scenario + skillsExpect ground truth (D-35)
- `internal/eval/skills_cot_eval_test.go` — live dual-gate TestSkillsE2E (host full terminal, operator-run NOT CI)
- `internal/eval/skills_adapters_cot_eval.go` — eval-package bridges onto the tools.skill* seams
- `internal/eval/judge_cot_eval.go` — D-35 skills rubrics + shared runRubricJudge driver (dedup-folds swarm + skills judges)
- `internal/eval/capture_cot_eval.go` — captures the ask_user pause (AwaitingInput) for the resume loop
- `internal/skills/smoke_test.go` — TestHaikuFlow + TestSnippetReuse (CI-runnable, LLM-free + DB-free)
- `.github/workflows/skills.yml` — the CAP-07/CAP-08 gating job (all tiers, no-skip-as-green, combined coverage)
- `scripts/chat-e2e-gate.sh` — the real `aura chat` binary gate (the #53 closing score)
- `internal/agent/prompt.go` + `docs/system_prompt.txt` — WRITE via fs tools, host terminal primary
- `internal/agent/tools/shell_exec.go` (+ `_test.go`) — POSIX bash on Windows + persistent cwd + CRLF guard
- `cmd/aura/cache_audit.go` + `scripts/cache_invariant_audit.sh` — messages[1] + manifest byte-stability
- `docs/aura-quality-snapshot.md` — Phase-11 row + detail section with the measured values

## Decisions Made

- **Real-binary gate is the closing score (#53/D-42):** the synthetic cot_eval judge measures the harness, not the product the user touches. The chat-surface gate runs the shipped `aura chat` exactly as a user would; the cot_eval tier stays as a structural regression guard.
- **Host full terminal primary (#52/D-41):** the skills loop and the gate ride `shell_exec` (POSIX `/bin/sh -c` / Windows `cmd /c`), not the sandbox. The sandbox is deliberate per-run escalation for untrusted/model-generated code.
- **fs tools author files, shell runs them (#53/D-42):** file CONTENT (scripts included) goes through fs_write/fs_edit; shell heredocs/echo blobs are fragile to quoting and proved fatal live.

## Deviations from Plan

The entire plan is a deviation-by-amendment case: the literal must_haves were superseded by #51/#52/#53 DURING execution. This is not a Rule 1–4 auto-fix — it is the PRD-amendment discipline operating as designed (the Q&A revision protocol: a slice revealing an architectural gap amends the PRD before the code lands). Documented amendments, not silent deviations:

**1. [Amendment #51/D-40] install-approval ceremony retired**
- **Supersedes:** must_have "catalog→ask_user→install→sandbox_exec sequence asserted" + the install_prudence judge dimension
- **What shipped instead:** self-service `npx skills add` via shell; the Go catalog/installer deleted (11-09)
- **Committed across:** `8ab92b81`..`4359f8a9` (the #49 iterations) + `b31f6d92`

**2. [Amendment #52/D-41] host full terminal replaces sandbox in the loop + eval**
- **Supersedes:** must_have "sandbox_exec as the eval terminal"
- **What shipped instead:** `tools.ShellExec` in the eval registry mirroring production; host workspace artifact verify
- **Committed in:** `b98687c2`, `c6b1251c`, `b78862d6`, `63822f35`

**3. [Amendment #53/D-42] fs-tool authoring + real-binary closing gate**
- **Supersedes:** must_have "the judge rubric scores ≥90%" as the CLOSING score
- **What shipped instead:** the 5 fs tools in the eval registry; chat-surface real-binary gate as the closing score; cot_eval demoted to structural guard
- **Committed in:** `2b7107fc`, `910a6f54`, `e58b25cc`, `8ecb5d5c`, `30234536`

---

**Total deviations:** 3 PRD-amendment supersessions (not auto-fixes). All ratified in prd.md (#51/#52/#53) before the code landed.
**Impact on plan:** the plan's OBJECTIVE (prove CAP-07/CAP-08 end-to-end with ground-truth artifact + gate regressions in CI + stamp the numbers) is fully met; the MECHANISM (host terminal, real-binary gate) is the amended shape. No scope creep — the amendments narrowed and grounded the gate.

## Issues Encountered

- The synthetic cot_eval gate plateaued at 0.30–0.60 under a natural prompt across 9 live runs — root-caused to (a) eval registry not mirroring production (no shell_exec, then no fs_write), (b) the prompt steering file-writes into shell-quoting hell, (c) CRLF-corrupted heredocs. All three fixed (#52/#53); the real-binary gate then passed 6/6 ×2.
- The lone 1.00 synthetic run was tainted by a hint-word leak in the run-dir path (correctly failed by the prompt-natural floor, then fixed in `ddd3d77d`).

## Mutation status (validator.go / writer.go)

The PLAN's Task 2 required a go-mutesting ≥70% spot-check on `validator.go` + `writer.go`. The quality-snapshot Phase-11 cell records this as **TBD (go-mutesting, ≥70% gate)**. The `internal/skills` surface evolved since (the Go installer was deleted #51; writer split into `writer.go` + `writer_activate.go`). Today's Phase-18 (CAP-08.1) mutation campaign measured the live successor handlers: `internal/agent/tools/skill_write.go` = **95.5% (21/22)** and `internal/skills/writer_activate.go` = **45.2% (14/31, all 17 survivors documented-equivalent FS-fault-injection error-wraps)** — see the Phase-18 detail section of the quality snapshot. Per operator direction, no NEW mutation campaign was run for this retroactive SUMMARY; the validator.go/writer.go cell is carried to the Phase-18 measurements as the live successors. Explicitly noted, not silently closed.

## Known Stubs

None introduced by 11-08. The quality-snapshot validator.go/writer.go mutation cell carries `TBD` (see above) — that is a measurement-carry note, not a code stub.

## Next Phase Readiness

- Phase 11 (Skills) is closed end-to-end: CAP-07 + CAP-08 proven on the live substrate with the real-binary gate, CI gating all tiers no-skip-as-green at 86.6% combined coverage.
- The host full-terminal skills loop + fs_write authoring is the production posture downstream phases inherit (Phase 12 transport, Phase 13 channels surface it; Phase 18 CAP-08.1 already built the snippet-reuse steady-state on top).
- No blockers.

## Self-Check: PASSED

- `internal/eval/scenarios_skills.go` — FOUND
- `internal/skills/smoke_test.go` — FOUND
- `.github/workflows/skills.yml` — FOUND
- `scripts/chat-e2e-gate.sh` — FOUND
- Commit `30234536` (chat-surface gate) — FOUND
- Commit `f22a2754` (xlsx scenario) — FOUND
- Commit `a4ac741c` (skills.yml CI) — FOUND
- Commit `2b7107fc` (fs-tool authoring) — FOUND

---
*Phase: 11-skills*
*Completed: 2026-06-06 (retroactively reconstructed at operator direction)*
