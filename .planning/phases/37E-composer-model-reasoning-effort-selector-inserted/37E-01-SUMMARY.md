---
phase: 37E-composer-model-reasoning-effort-selector-inserted
plan: 01
subsystem: docs
tags: [prd-amendment, reasoning-effort, webmodel, llama.cpp, openrouter, capability-detection, effort-only]

# Dependency graph
requires:
  - phase: 37D-composer-skill-picker
    provides: the `aura` run-request envelope + composer send-payload pattern that 37E's `aura.effort` field mirrors
  - phase: 37E-CONTEXT/RESEARCH
    provides: locked decisions D-01..D-13 + the spike-095/096 wire contract this amendment records
provides:
  - "Amended REQUIREMENTS.md WEBMODEL-01/02/03 (effort-only, 7-level capability-gated, two-stage validation, llama.cpp coverage, honest fidelity)"
  - "Reconciled ROADMAP.md 37E entry (effort-only Goal + Success Criteria + resolved design forks; model-selector framing deleted)"
  - "prd.md Amendment #82 (the 37E PRD-first gate: aura.effort field, dual-backend wire map, conversations.metadata persistence, OpenRouter /models capability source, GET /api/composer/reasoning-capabilities, honest fidelity caveat)"
  - "New AURA_LLM_PROVIDER (openrouter|llamacpp) env catalogued in prd.md"
affects: [37E-02, 37E-03, 37E-04, 37E-05, 37E-06, 37E-07]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "PRD-first amendment gate (git-log ordering is the gate) — same pattern as amendments #44/#62/#63/#64/#78/#79/#81"
    - "Scope-reduction reconciliation across REQUIREMENTS.md + ROADMAP.md + prd.md in one atomic gate before any code"

key-files:
  created:
    - .planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-01-SUMMARY.md
  modified:
    - .planning/REQUIREMENTS.md
    - .planning/ROADMAP.md
    - prd.md

key-decisions:
  - "37E scope reduced to effort-only — the Composer model dropdown is dropped; model selection stays operator-scoped in Settings (AURA_LLM_MODEL, D-01)"
  - "The 7-level capability-gated scale auto·off·low·mid·high·extra·max supersedes the VOID D-09a 'no Max' resolution across all three docs (D-02/D-09a-VOID)"
  - "Persistence is aura.conversations.metadata jsonb — NO new migration; column exists in 0005; migration numbering unchanged (D-06)"
  - "New AURA_LLM_PROVIDER env (openrouter|llamacpp) catalogued — needed to positively identify a llama.cpp backend for the wire branch + capability source (D-13/OQ-1)"
  - "WEBMODEL-01/02/03 are NOT marked complete by this plan — it is the docs-only PRD-amendment gate; the requirements close when the Wave-2..5 code plans ship + verify (unit/e2e/coverage ≥85%)"

patterns-established:
  - "PRD-first gate: a docs-only amendment commit lands before any implementation plan in the phase can execute"

requirements-completed: []  # WEBMODEL-01/02/03 are DOCUMENTED/reconciled here but NOT satisfied — this is the docs gate; they close in Wave-2..5 code plans (unit + e2e + coverage ≥85%).

# Metrics
duration: ~18min
completed: 2026-07-10
---

# Phase 37E Plan 01: PRD-Amendment Gate (Composer Reasoning-Effort Selector) Summary

**Reconciled REQUIREMENTS.md + ROADMAP.md + prd.md to the resolved effort-only 37E scope — WEBMODEL-01/02/03 rewritten to the 7-level capability-gated `auto·off·low·mid·high·extra·max` scale, the stale D-09a "no Max"/model-selector framing deleted everywhere, and prd Amendment #82 records the dual-backend wire map, two-stage validation, `conversations.metadata` persistence, the OpenRouter `/models` capability source, and the honest backend-dependent fidelity caveat.**

## Performance

- **Duration:** ~18 min
- **Started:** 2026-07-10T21:02+02:00
- **Completed:** 2026-07-10T21:20+02:00
- **Tasks:** 3
- **Files modified:** 3 (+ 1 created: this SUMMARY)

## Accomplishments

- **REQUIREMENTS.md WEBMODEL-01/02/03 rewritten effort-only:** dropped the model-selector clause (D-01); WEBMODEL-01 → reasoning-effort selector, capability-auto-detected 7-level set, per-conversation `conversations.metadata` jsonb (no migration); WEBMODEL-02 → symbolic `effort` + two-stage (enum + capability) server validation → 400, dual-backend (OpenRouter + llama.cpp, D-08); WEBMODEL-03 → symbol-only no-bypass + real-knob-only (D-12) + honest fidelity caveat (D-09). Section title + intro + traceability + coverage note also de-model-ified.
- **ROADMAP.md 37E entry reconciled:** the whole entry (Goal, Success Criteria, Depends-on, Design-forks, PRD-first) rewritten to effort-only; every `selettore modello` / model-list-source phrase deleted; design forks marked RESOLVED (model selection OUT; persistence = metadata jsonb no-migration; effort = 7-level capability-gated). Requirements line + planner plan-list left intact.
- **prd.md Amendment #82 added:** the 37E PRD-first gate — 7 numbered subsections covering the symbolic scale + level→wire map for both backends, the `aura.effort` field + two-stage validation, the llama.cpp `--jinja`/`--reasoning-budget` ops contract, `conversations.metadata` persistence (NO new migration, numbering unchanged), the capability-auto-detection subsystem + `GET /api/composer/reasoning-capabilities`, and the honest backend-dependent fidelity caveat. Plus the net-new `AURA_LLM_PROVIDER` env catalogued.

## Task Commits

Each task was committed atomically:

1. **Task 1: Amend REQUIREMENTS.md WEBMODEL-01/02/03 to effort-only** — `922c8f63` (docs)
2. **Task 2: Reconcile ROADMAP.md 37E entry** — `c33bcda6` (docs)
3. **Task 3: Add prd.md Amendment #82 + AURA_LLM_PROVIDER env** — `d242632d` (docs; amended once to paraphrase the superseded D-09a wording — see Deviations)

**Plan metadata:** recorded in the final `docs(37E-01): complete PRD-amendment gate` commit (SUMMARY + STATE + ROADMAP tracking).

## Files Created/Modified

- `.planning/REQUIREMENTS.md` — WEBMODEL section header/intro + WEBMODEL-01/02/03 rows rewritten effort-only; traceability row + coverage note de-model-ified.
- `.planning/ROADMAP.md` — the entire Phase 37E entry (Goal → PRD-first) rewritten effort-only; plan list preserved.
- `prd.md` — Amendment #82 (the 37E gate, 7 subsections) + the `AURA_LLM_PROVIDER` env-catalog row.
- `.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-01-SUMMARY.md` — this file.

## Decisions Made

- **Did NOT mark WEBMODEL-01/02/03 complete.** This plan is the docs-only PRD-amendment gate; the requirements demand unit + e2e + coverage ≥85%, delivered by the Wave-2..5 code plans. Marking them complete now would falsely claim the feature shipped (CLAUDE.md DEFINITION OF DONE = validated E2E). They stay `[ ]`.
- **Kept the ROADMAP `#### Phase 37E` header and directory-matching phase name unchanged** (they are the stable identifier + the awk boundary the verify greps on); the effort-only reconciliation lives in the prose, Success Criteria, and design forks.
- **Paraphrased rather than quoted the superseded D-09a wording** in prd.md so no deliverable doc retains the literal stale phrases (see Deviations #2).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Doc-drift] De-model-ified the REQUIREMENTS.md traceability row + coverage note**
- **Found during:** Task 1 (amend WEBMODEL rows)
- **Issue:** The plan explicitly scoped Task 1 to the WEBMODEL section header/intro + the three rows, but the same file's traceability table (`| WEBMODEL | ... composer model + reasoning-effort selector ...`) and the coverage note (`WEBMODEL 37E model+effort selector`) still asserted the old model+effort scope — a self-contradiction with the amended rows.
- **Fix:** Updated both to "reasoning-effort selector" and annotated "model-selector dropped, effort-only per 37E-01/D-01".
- **Files modified:** `.planning/REQUIREMENTS.md`
- **Verification:** Task 1 verify greps still pass; no stale "model + reasoning-effort selector" scope descriptor remains in the file.
- **Committed in:** `922c8f63` (Task 1 commit)

**2. [Rule 1 - Stale-wording] Paraphrased the VOID D-09a resolution in prd.md instead of quoting it verbatim**
- **Found during:** Task 3 (Amendment #82), caught by the cross-doc consistency check
- **Issue:** The first draft of Amendment #82 subsection (2) quoted the literal strings "off/low/mid/high/auto only" and "Max is NOT added" (wrapped in a VOID marker). Objective (e) requires deleting/superseding that stale wording *everywhere* so all three docs reflect the 7-level scale — leaving the literal phrase (even quoted) is a future grep landmine.
- **Fix:** Reworded to "the earlier same-day D-09a resolution (which capped the scale at five levels and excluded an unlimited-budget top level) is VOID", preserving the supersession record without reproducing the stale phrase.
- **Files modified:** `prd.md`
- **Verification:** `grep -rl 'Max is NOT added' / 'off/low/mid/high/auto only'` across the three deliverables → 0 files; all Task 3 verify greps still pass.
- **Committed in:** `d242632d` (Task 3 commit, amended)

---

**Total deviations:** 2 auto-fixed (both Rule 1 — doc-drift / stale-wording, intra-deliverable consistency).
**Impact on plan:** Both tighten the reconciliation the plan's objective (e) mandates ("delete/supersede the stale wording everywhere"). No scope creep — no code, no requirements falsely closed.

## Issues Encountered

- `gsd-sdk` is not on PATH in this environment, so STATE.md advancement + ROADMAP plan-progress were applied as direct file edits (per the sequential-run instruction that this run owns those tracking updates). No functional impact.

## User Setup Required

None — no external service configuration required (docs-only amendment).

## Next Phase Readiness

- **Wave-2 code plans (37E-02 / 37E-03) are unblocked.** The PRD-first gate is satisfied: all three truth-source docs (REQUIREMENTS.md, ROADMAP.md, prd.md) now describe the same effort-only, 7-level, capability-gated, dual-backend scope, and the stale D-09a "no Max" + model-selector wording is gone.
- The consolidated symbol/artifact inventory the code plans consume lives in `37E-01-PLAN.md` `<artifacts_this_phase_produces>` and prd.md Amendment #82.
- **Open item for later plans (not this gate):** WEBMODEL-01/02/03 remain `[ ]` until the code + unit/e2e + coverage ≥85% land in Waves 2–5.

## Self-Check: PASSED

- Created file exists: `.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-01-SUMMARY.md` (this file).
- Task commits exist in git: `922c8f63` (FOUND), `c33bcda6` (FOUND), `d242632d` (FOUND).
- No code files touched, no file deletions across the three task commits (verified via `git diff --diff-filter=D` and `git diff --stat`).

---
*Phase: 37E-composer-model-reasoning-effort-selector-inserted*
*Completed: 2026-07-10*
