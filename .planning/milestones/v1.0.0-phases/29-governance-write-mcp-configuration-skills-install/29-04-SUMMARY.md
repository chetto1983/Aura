---
phase: 29-governance-write-mcp-configuration-skills-install
plan: 04
subsystem: ui
tags: [react, vite, typescript, tanstack-query, react-i18next, governance, cockpit, a11y]

# Dependency graph
requires:
  - phase: 28-governance-read
    provides: the McpBoard/SkillsBoard/McpServerDetail read boards extended in place (D-10), the governanceApi read client, the approvals queue + InlineApprovalCard
  - phase: 29-02
    provides: the MCP write endpoints (install/env/trust/enable/disable/remove) the panels call
  - phase: 29-03
    provides: the skills write endpoints + the operator-origin skill-install approval that lands in /api/approvals
provides:
  - "MCP write UI: McpInstallPanel (recipe/custom), McpEnvEditForm (four-state secret-preserve, ${KEY} placeholder), McpLifecycleCluster (trust/enable/disable/remove), wired into McpBoard/McpServerDetail"
  - "Skills write UI: SkillInstallPanel (RISKY supply-chain framing) wired into SkillsBoard"
  - "governanceApi write layer (TanStack useMutation + invalidateQueries) over the 29-02/29-03 endpoints"
  - "InlineApprovalCard RISKY skill-install strip (Kind=approval) — container-isolation note + resume token, NO run/activate affordance, reuses the existing ApprovalBadge"
  - "i18n: governance write + skill-approval keys in BOTH en+it; an en<->it bundle parity gate (resources.parity.test.ts)"
affects: [29-05]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Write panels follow the Phase-28 board component patterns, extended IN PLACE (D-10) — no parallel re-skin; accepted BLUE palette tokens"
    - "TanStack useMutation + invalidateQueries for every write; key-only env-chip echo (no raw secret on the wire)"
    - "aria-invalid omit-when-valid (cond || undefined); contrast-check gate kept AA"

key-files:
  created:
    - web/src/governance/McpInstallPanel.tsx
    - web/src/governance/McpEnvEditForm.tsx
    - web/src/governance/McpLifecycleCluster.tsx
    - web/src/governance/SkillInstallPanel.tsx
    - web/src/governance/__tests__/McpInstallPanel.test.tsx
    - web/src/governance/__tests__/McpEnvEditForm.test.tsx
    - web/src/governance/__tests__/SkillInstallPanel.test.tsx
    - web/src/governance/__tests__/McpServerDetail.test.tsx
    - web/src/i18n/__tests__/resources.parity.test.ts
  modified:
    - web/src/governance/McpBoard.tsx
    - web/src/governance/McpServerDetail.tsx
    - web/src/governance/SkillsBoard.tsx
    - web/src/governance/governanceApi.ts
    - web/src/governance/__tests__/SkillsBoard.test.tsx
    - web/src/approvals/InlineApprovalCard.tsx
    - web/src/approvals/__tests__/InlineApprovalCard.test.tsx
    - web/src/i18n/resources.ts
    - web/src/i18n/resources.governance.ts
    - web/scripts/contrast-check.mjs

key-decisions:
  - "Skill-install approval renders the SAME InlineApprovalCard with an added RISKY strip — no second queue/badge (the existing ApprovalBadge already counts it; D-11), no run/activate affordance (activation is the approval RESUME only)"
  - "Coverage (Vitest >=85%) + Stryker (>=70%) + Playwright e2e + axe are DEFERRED to plan 29-05 per 29-04-PLAN.md line 271 — 29-04 ships the panels + their unit tests"

patterns-established:
  - "RISKY supply-chain framing for skill installs in the approval card (container-isolated + approval-gated + Writer-validated; never 'safe')"
  - "en<->it i18n bundle parity test as a key-diff gate over the whole resources bundle"

requirements-completed: [MCPW-01, MCPW-02, MCPW-03, SKW-01, SKW-02, SKW-03]

# Metrics
duration: ~30min (executor, rate-limited) + orchestrator close-out
completed: 2026-06-21
---

# Phase 29 / Plan 04: Cockpit write controls Summary

**The cockpit governance write UI — MCP install/env-edit/lifecycle panels + the RISKY skills install panel + the skill-install inline approval card — extending the Phase-28 boards in place, wired to the 29-02/29-03 backend via TanStack mutations, with en/it i18n parity.**

## Performance

- **Tasks:** 3
- **Files:** 19 created/modified across web/src/governance, web/src/approvals, web/src/i18n, web/scripts
- **Gates:** typecheck clean; eslint zero-warning; 16 test files / 125 tests pass; contrast 36/36 WCAG AA

## Accomplishments
- MCP write panels: McpInstallPanel (recipe + custom stdio/HTTP), McpEnvEditForm (four-state secret-preserving merge with `${KEY}` placeholders, key-only echo), McpLifecycleCluster (trust/enable/disable/remove with confirm), wired into McpBoard + McpServerDetail.
- SkillInstallPanel: RISKY supply-chain framing for the npx skills install, wired into SkillsBoard.
- governanceApi write layer: TanStack `useMutation` + `invalidateQueries` over every 29-02/29-03 write endpoint.
- InlineApprovalCard: a `Kind=approval` skill-install renders the same card plus a RISKY strip (container-isolation note + resume token), no run/activate affordance, reusing the existing ApprovalBadge (no second queue).
- i18n: all new copy in BOTH en+it (resources.governance.ts + resources.ts); new `resources.parity.test.ts` asserts zero en↔it key drift + the Phase-29 required keys.

## Task Commits

1. **Task 1: governanceApi write layer + MCP install panel + four-state env-edit form** — `01a0cfa2` (feat)
2. **Task 2: skills install panel + lifecycle cluster + SkillsBoard wiring** — `5cdc6ffb` (feat) — see Anomaly below
3. **Task 3: skill-install RISKY inline approval card + en/it i18n parity gate** — `b239b777` (feat)

**Plan metadata:** this SUMMARY + STATE/ROADMAP (docs).

## Decisions Made
- Deferred the formal Vitest ≥85% / Stryker ≥70% / Playwright e2e / axe gate to plan 29-05 (explicit per 29-04-PLAN.md line 271 and the 29-05 files_modified: `governanceWrite.coverage.test.tsx` + `stryker.conf.json`). 29-04 ships the panels + their unit tests; current panel coverage ~60–77% (governanceApi.ts 60%, McpBoard 75%, SkillsBoard 77%) — **29-05 must lift the touched governance + approvals dirs to ≥85% and add Stryker ≥70%.**

## Deviations from Plan / Issues Encountered

### Anomaly 1 — Task 2 swept into a concurrent spike commit (parallel-process collision)
- **What:** While the executor was mid-flight, a concurrent process on `master` ran an add-all and committed the executor's then-uncommitted Task-2 files (`McpLifecycleCluster.tsx` [new], `SkillInstallPanel.tsx` [new], `SkillsBoard.tsx`, `McpServerDetail.tsx`, `SkillsBoard.test.tsx`) inside commit **`5cdc6ffb`**, whose message is the unrelated `docs(spike-073)` FunctionGemma text.
- **Impact:** The Task-2 code is intact and verified (typecheck/lint/tests green); only the commit attribution is wrong (violates one-slice-one-commit traceability). History was NOT rewritten — `master` has an active parallel writer, so a rebase/reset would be unsafe. The mapping is recorded here for traceability.
- **Cause:** the master-direct + parallel-Codex workflow; a sibling session's `git add -A` raced the executor's uncommitted files.

### Anomaly 2 — executor terminated by a transient provider rate-limit
- **What:** the wave-4 executor hit "Server is temporarily limiting requests (not your usage limit)" after committing Task 1 and (via the sweep) Task 2, with Task 3 left uncommitted and the new `resources.parity.test.ts` containing a TS error (the local `const it` set shadowed vitest's `it` test fn).
- **Resolution:** orchestrator close-out — fixed the parity test (renamed sets to `enKeys`/`itKeys`), re-ran typecheck (clean), the governance+approvals+i18n suites (125 pass), lint (zero-warning) and contrast (36/36 AA), then secured Task 3 as commit `b239b777` via an explicit-path commit (so the concurrent process's staged spike files were not swept in reverse).

## User Setup Required
None.

## Next Phase Readiness
- All write panels + the approval card render and call the real backend; i18n parity holds. Plan 29-05 (Gate-3 close) must: add the held-out secret-scan + no-model-approve backstops, the Playwright e2e of the full cockpit write flow + axe, lift Vitest coverage to ≥85% + Stryker ≥70% on the touched governance/approvals dirs, and rebuild `internal/webui/dist`.

---
*Phase: 29-governance-write-mcp-configuration-skills-install*
*Completed: 2026-06-21*
