---
phase: 37D
slug: composer-skill-picker
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-09
---

# Phase 37D — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Lifted from `37D-RESEARCH.md` § Validation Architecture (all rows `[VERIFIED]` against the codebase).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Frontend unit** | Vitest + @testing-library/react (`web/vitest.config.ts`) |
| **Frontend coverage gate** | v8 thresholds statements/branches/functions/lines = **85** (`web/vitest.config.ts:28-32`) |
| **Frontend e2e** | Playwright (`web/playwright.config.ts`; specs in `web/e2e/`) |
| **Backend** | Go `testing` + table-driven; `httptest` for handlers; `-race`; owned-surface ≥85% via `scripts/coverage_gate.sh` |
| **Quick run (web unit)** | `cd web && npm test` (= `vitest run --coverage`) |
| **Quick run (backend)** | `go test ./internal/agui/ ./internal/skills/` |
| **Full e2e** | `cd web && npm run test:e2e` (= `playwright test`) |
| **i18n parity** | `web/src/i18n/__tests__/resources.parity.test.ts` |
| **Estimated runtime** | web unit ~20s · backend ~15s · e2e ~1–2 min |

---

## Sampling Rate

- **After every task commit:** `cd web && npm test -- <touched file>` and/or `go test ./internal/agui/` (+ `-race` on backend packages touched).
- **After every plan wave:** full `cd web && npm test` (coverage) + `go test ./internal/agui/ ./internal/skills/`.
- **Before `/gsd-verify-work`:** full web unit + `npm run test:e2e -- composer-skills` green + owned-surface Go coverage ≥85%.
- **Max feedback latency:** ~40s (unit tiers).

---

## Per-Task Verification Map

> Task IDs are assigned by the planner (PLAN.md). Rows below map each Success Criterion / requirement
> to its automated verification (from RESEARCH.md § Success Criterion → Test Map). The executor binds
> each to a concrete `{plan}-{task}` ID during execution.

| SC / Req | Behavior | Test Type | Automated Command | File (new/existing) | Status |
|----------|----------|-----------|-------------------|---------------------|--------|
| SC1 / WEBSKILL-01 | `/` at empty-composer start opens menu; type filters; ↑/↓/Enter/Esc navigate | unit (React) | `cd web && npm test -- SkillPicker` | ❌ W0 `web/src/chat/composer/__tests__/SkillPicker.test.tsx` | ⬜ pending |
| SC1 / WEBSKILL-01 | endpoint returns global active-skills rows behind RequireAuth | backend unit | `go test ./internal/agui/ -run ComposerSkills` | ❌ W0 `internal/agui/composer_api_test.go` | ⬜ pending |
| SC1 / WEBSKILL-01 | non-admin identity gets a NON-empty list (not 403) | backend/integration | `go test ./internal/agui/ -run ComposerSkills_RequireAuthNotCapability` | ❌ W0 (auth-matrix; mirror `auth_test.go`) | ⬜ pending |
| SC2 / WEBSKILL-02 | picking a skill pins it; send carries `aura.skill`; server applies framed body first | unit (client) | `cd web && npm test -- sseAdapter` | ❌ W0 (extend `sseAdapter` tests) | ⬜ pending |
| SC2 / WEBSKILL-02 | server prepends framed skill body to model msg, persists raw visible turn | backend unit | `go test ./internal/agui/ -run Run_PinnedSkill` | ❌ W0 `internal/agui/server_skill_run_test.go` (mirror `server_assets_run_test.go`) | ⬜ pending |
| SC2 / WEBSKILL-02 | picker list ⊆ runtime loader set (no divergence) | backend unit | `go test ./internal/agui/ -run ComposerSkillsMatchesRuntime` | ❌ W0 | ⬜ pending |
| SC3 / WEBSKILL-03 | a11y: aria-expanded/controls/activedescendant; focus stays on input | unit (a11y) | `cd web && npm test -- SkillPicker.a11y` | ❌ W0 | ⬜ pending |
| SC3 / WEBSKILL-03 | paste/drop/Enter-send preserved when menu closed | unit (React) | `cd web && npm test -- Composer` | ✅ extend `web/src/chat/__tests__/Composer*.test.tsx` | ⬜ pending |
| SC3 / WEBSKILL-03 | degrade-to-no-op on empty/unreachable list | unit (React) | `cd web && npm test -- SkillPicker.degrade` | ❌ W0 | ⬜ pending |
| SC3 / WEBSKILL-03 | e2e: open→filter→select→pill→send fires invocation; new-chat/clear behavior | e2e | `cd web && npm run test:e2e -- composer-skills` | ❌ W0 `web/e2e/composer-skills.spec.ts` (mirror `artifacts.spec.ts`) | ⬜ pending |
| SC3 / WEBSKILL-03 | coverage ≥85% web + owned-surface Go | gate | `cd web && npm test` ; `bash scripts/coverage_gate.sh` | existing gates | ⬜ pending |
| D-10 | en+it parity for new `composer.skillPicker.*` keys | unit | `cd web && npm test -- resources.parity` | ✅ existing `resources.parity.test.ts` | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/agui/composer_api_test.go` — endpoint rows + RequireAuth-not-capability (WEBSKILL-01).
- [ ] `internal/agui/server_skill_run_test.go` — pinned-skill applied first; visible turn persisted raw (WEBSKILL-02) — model on `server_assets_run_test.go`.
- [ ] `web/src/chat/composer/__tests__/SkillPicker.test.tsx` — trigger, filter, keyboard, a11y, degrade (WEBSKILL-01/03).
- [ ] `web/e2e/composer-skills.spec.ts` — full flow + quick actions (WEBSKILL-03) — model on `artifacts.spec.ts`.
- [ ] extend `sseAdapter` tests for the `aura.skill` body field.
- [ ] en+it `composer.skillPicker.*` keys in `resources.ts` (parity test already exists).

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Visual parity with the Claude reference screenshot (menu-above-input, grouped rows, icon+name+subtitle) | WEBSKILL-01 / D-07 | Pixel/layout fidelity is subjective | Open composer, type `/`, compare to DISCUSSION-LOG reference screenshot |

*All functional behaviors have automated verification (unit + e2e); only visual-parity fidelity is manual.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags (CI uses `vitest run`, not `vitest --watch`)
- [ ] Feedback latency < 40s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
