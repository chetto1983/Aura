---
phase: 28-governance-boards-web-onboarding
plan: 06
subsystem: ui
tags: [react, onboarding, provisioning, telegram, qr, i18n, a11y, vitest, stryker, playwright]

# Dependency graph
requires:
  - phase: 28-05
    provides: "POST /api/onboarding/start, POST /api/onboarding/{token}/step, POST /api/onboarding/{token}/provision, GET /api/onboarding/{token}/telegram-status, capability-filtering, provisioning saga, Telegram deep-link + QR payloads"
  - phase: 28-03
    provides: "the cockpit lazy workspace wiring patterns, same-origin throwing fetch idiom, locked blue design tokens, TanStack Query state patterns, and embedded dist rebuild precedent"
provides:
  - "A lazy full-screen onboarding wizard overlay in AppShell, separate from shell modes/governance tabs"
  - "Credentials, capability picker, Telegram link/QR, 5-step interview, review/create, and completion surfaces over the Plan-05 endpoints"
  - "onboardingApi same-origin fetchers that throw Error(\"HTTP <n>\") for visible 401/403/409/502 handling"
  - "en+it onboarding copy bundle and onboarding-specific wizard tests, Playwright e2e, contrast, coverage, and Stryker gates"
  - "Rebuilt internal/webui/dist with the onboarding wizard chunk embedded"
affects: [Phase 29 governance write surfaces, Phase 30 absorbed Telegram onboarding UX, future identity management]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Full-screen feature overlay is lazy-loaded from AppShell and kept out of MODES/LIVE_MODES so onboarding remains a separate linear flow, not a governance tab."
    - "Wizard state is factored through onboardingWizardModel helpers and narrow step components so the flow is testable without brittle DOM-only assertions."
    - "Secrets-never-rendered posture: the password stays in a controlled write-only input until provision and is cleared on success; Telegram UI renders only the deep-link and server QR."
    - "Mutation gate is scoped with web/stryker.onb.json while the main stryker config permanently includes src/onboarding."

key-files:
  created:
    - web/src/onboarding/OnboardingWizard.tsx
    - web/src/onboarding/CredentialStep.tsx
    - web/src/onboarding/CapabilityPicker.tsx
    - web/src/onboarding/TelegramLinkStep.tsx
    - web/src/onboarding/InterviewStep.tsx
    - web/src/onboarding/ReviewStep.tsx
    - web/src/onboarding/OnboardingStepper.tsx
    - web/src/onboarding/OnboardingWizardNav.tsx
    - web/src/onboarding/onboardingApi.ts
    - web/src/onboarding/onboardingWizardModel.ts
    - web/src/onboarding/__tests__/CredentialStep.test.tsx
    - web/src/onboarding/__tests__/CapabilityPicker.test.tsx
    - web/src/onboarding/__tests__/InterviewStep.test.tsx
    - web/src/onboarding/__tests__/OnboardingStepper.test.tsx
    - web/src/onboarding/__tests__/OnboardingWizard.test.tsx
    - web/src/onboarding/__tests__/ReviewStep.test.tsx
    - web/src/onboarding/__tests__/TelegramLinkStep.test.tsx
    - web/src/onboarding/__tests__/onboardingApi.test.ts
    - web/src/onboarding/__tests__/onboardingWizardModel.test.ts
    - web/e2e/onboarding.spec.ts
    - web/stryker.onb.json
    - internal/webui/dist/assets/OnboardingWizard-BDluN6lM.js
  modified:
    - web/src/AppShell.tsx
    - web/src/i18n/resources.ts
    - web/src/i18n/resources.onboarding.ts
    - web/stryker.config.json
    - internal/webui/dist/index.html
    - internal/webui/dist/sw.js
    - internal/webui/dist/assets

key-decisions:
  - "The wizard is a full-screen lazy AppShell overlay, not a shell mode or governance tab, preserving D-04's separate linear onboarding flow."
  - "The onboarding flow uses a local reducer/model layer instead of scattering phase transitions through component event handlers, which made confirm/edit/skip and provision errors directly testable."
  - "The Telegram QR surface renders the server SVG through an image/data URL path and scans the rendered DOM for bot-token leaks in tests; only the deep-link and QR are allowed to appear."
  - "A scoped onboarding Stryker config was committed so mutation testing can run against the touched onboarding dir without measuring unrelated cockpit surfaces."

patterns-established:
  - "Wizard step components accept explicit state + callbacks; flow orchestration lives in OnboardingWizard and pure helpers live in onboardingWizardModel."
  - "Provision error mapping is centralized: 403 -> no capability, 409 -> duplicate/empty email, everything else -> rolled-back/nothing-saved copy."
  - "Onboarding e2e can run against a real Docker-backed cockpit origin with mocked route data for the API surface."

requirements-completed: [ONBD-01, ONBD-02]

# Metrics
duration: ~4h resumed session
completed: 2026-06-20
status: complete
---

# Phase 28 Plan 06: Onboarding Wizard Frontend Summary

**A full-screen onboarding wizard now drives credentials, creator-scoped capabilities, Telegram deep-link/QR linking, the 5-step interview, review, provisioning, completion, and all visible error states over the Plan-05 API without rendering secrets.**

## Performance

- **Duration:** ~4h resumed session
- **Started:** 2026-06-20T12:39:52Z
- **Completed:** 2026-06-20T16:33:57+02:00
- **Tasks:** 2
- **Files created/modified:** 41 in the final Task-2 commit, plus Task-1 shell/API/i18n files

## Accomplishments

- Added the lazy `OnboardingWizard` overlay from `AppShell`, intentionally separate from `MODES`/`LIVE_MODES`, with credentials -> capabilities -> Telegram -> interview -> review -> completion flow.
- Added `onboardingApi.ts` same-origin start/step/provision/telegram-status fetchers using the established `Error("HTTP <n>")` state contract.
- Implemented credential and capability steps with a write-only password field, first-login 2FA hint, creator-returned checklist only, and no `*` grant option.
- Implemented Telegram link/QR, interview confirm/edit/skip, review/create, success, 401 auth-error, 403 no-capability, 409 duplicate/empty email, and rolled-back/nothing-saved states.
- Added en+it onboarding copy, component/model/API tests, desktop + mobile Playwright onboarding coverage, scoped mutation config, and rebuilt `internal/webui/dist`.

## Task Commits

1. **Task 1: Wizard shell + data layer + credentials/capability-picker steps + i18n** - `f97944f0` (feat)
2. **Task 2: Telegram-link step + interview step + review/provision + completion + e2e + contrast** - `5aacbf22` (feat)

## Quality Gates

| Gate | Command | Result |
|------|---------|--------|
| Type-check | `cd web && npx tsc --noEmit` | PASS |
| Contrast | `cd web && node scripts/contrast-check.mjs` | PASS |
| Onboarding coverage | `cd web && npx vitest run --coverage src/onboarding --coverage.include='src/onboarding/**/*.{ts,tsx}' --coverage.exclude='src/onboarding/**/__tests__/**'` | PASS, 67 tests; 95.72% statements, 89.28% branches |
| E2E | `cd web && AURA_E2E_ORIGIN=http://127.0.0.1:9080 npx playwright test e2e/onboarding.spec.ts` | PASS on chromium + mobile-chrome |
| Mutation | `cd web && npx stryker run stryker.onb.json` | PASS, 75.11% mutation score (>=70 break threshold), 81 dry-run tests |
| Commit hooks | `git commit` pre-commit hook | PASS: vet, jscpd, file-size |

## Files Created/Modified

- `web/src/onboarding/OnboardingWizard.tsx` - orchestrates the full-screen flow, state transitions, provisioning, completion, and auth/error states.
- `web/src/onboarding/onboardingApi.ts` - same-origin throwing fetchers for Plan-05 onboarding endpoints.
- `web/src/onboarding/onboardingWizardModel.ts` - pure helpers for phase indexing, auth/provision error mapping, terminal status, and validation.
- `web/src/onboarding/{CredentialStep,CapabilityPicker,TelegramLinkStep,InterviewStep,ReviewStep}.tsx` - focused step surfaces.
- `web/src/onboarding/{OnboardingStepper,OnboardingWizardNav}.tsx` - desktop/mobile progress and step navigation primitives.
- `web/src/onboarding/__tests__/*` - API, model, step, wizard, and secret/no-escalation tests.
- `web/e2e/onboarding.spec.ts` - desktop + mobile full-flow Playwright spec.
- `web/src/i18n/resources.onboarding.ts` and `web/src/i18n/resources.ts` - en+it copy bundle and root spread.
- `web/stryker.config.json` and `web/stryker.onb.json` - onboarding mutation coverage surface.
- `internal/webui/dist/*` - rebuilt embedded web assets including the onboarding wizard chunk.

## Decisions Made

- The wizard is a separate AppShell overlay instead of a shell mode, matching D-04 and preventing onboarding from becoming a governance tab.
- Provision error mapping is centralized in `onboardingWizardModel.ts`, keeping copy distinctions testable and consistent across the wizard.
- The flow keeps the password out of review/completion surfaces and clears it after successful provision; tests scan for password and bot-token absence.
- Mutation testing uses a committed onboarding-scoped config so the gate remains repeatable and does not depend on CLI mutate overrides.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Extracted wizard navigation/model helpers**
- **Found during:** Task 2
- **Issue:** Keeping all stepper, navigation, and phase/error mapping logic inside `OnboardingWizard.tsx` made mutation and focused tests noisy and brittle.
- **Fix:** Added `OnboardingStepper.tsx`, `OnboardingWizardNav.tsx`, and `onboardingWizardModel.ts` while preserving the planned public wizard surface.
- **Files modified:** `web/src/onboarding/*`, `web/src/onboarding/__tests__/*`
- **Verification:** onboarding Vitest coverage, Stryker 75.11%, and e2e green.
- **Committed in:** `5aacbf22`.

**2. [Rule 3 - Blocking] Added a scoped Stryker config**
- **Found during:** Task 2
- **Issue:** The project mutation config needed a repeatable onboarding-only run for the touched directory; relying on ad hoc CLI overrides would make the gate hard to reproduce.
- **Fix:** Added `web/stryker.onb.json` and also extended the main `web/stryker.config.json` mutate set to include onboarding.
- **Files modified:** `web/stryker.config.json`, `web/stryker.onb.json`
- **Verification:** `npx stryker run stryker.onb.json` exited 0 with 75.11%.
- **Committed in:** `5aacbf22`.

---

**Total deviations:** 2 auto-fixed (both blocking/tooling/maintainability). **Impact:** The delivered behavior is still exactly the planned onboarding wizard; the extra helpers make the flow more testable and mutation-hardened.

## Issues Encountered

- The first executor pass was interrupted by the orchestrator while Stryker was running. The Stryker parent/workers were stopped safely, the remaining mutation gate was rerun from a clean shell, and the final result passed at 75.11%.
- The repository already had unrelated dirty files (`.gitignore`, `.planning/spikes/*`, `.planning/phases/22-bug-fix/.gitkeep`) before finalization. They were intentionally left out of the Task 2 and summary commits.

## User Setup Required

None - no external service configuration is required for the frontend implementation. Live Telegram linking still depends on the backend/bot configuration already represented by Plan 05.

## Next Phase Readiness

- Phase 29 can build write/admin surfaces on top of the same AppShell overlay, same-origin API, i18n, and mutation-testing patterns.
- The onboarding flow is ready for phase-level verification with real backend endpoints, desktop/mobile E2E, and the embedded dist rebuilt.

## Self-Check: PASSED

- All key files from `key-files.created` exist on disk and are committed.
- Both task commits are present in git history: `f97944f0` and `5aacbf22`.
- Acceptance criteria are covered by component tests, API/model tests, Playwright desktop/mobile e2e, contrast, and mutation gates.
- Secrets-never-rendered and no-`*` capability behavior are asserted in tests.
- No `## Self-Check: FAILED` marker is present.

---
*Phase: 28-governance-boards-web-onboarding*
*Completed: 2026-06-20*
