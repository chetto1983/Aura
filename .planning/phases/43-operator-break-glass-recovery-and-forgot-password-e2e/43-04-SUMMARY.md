---
phase: 43-operator-break-glass-recovery-and-forgot-password-e2e
plan: 04
subsystem: web-e2e
tags: [e2e, playwright, forgot-password, password-reset, anti-enumeration, no-leak, web]

# Dependency graph
requires:
  - phase: 36-multi-user-identity-isolation-authula-cutover
    provides: "PasswordResetPanel + passwordResetApi (/api/auth/password-reset/{start,question,verify,complete}) + login.reset i18n — the already-shipped UI this spec drives"
  - phase: 43-04
    provides: "web/e2e/onboarding.spec.ts — the page.route().fulfill() + body.innerHTML() no-leak analog mirrored here"
provides:
  - "web/e2e/password-reset.spec.ts — Playwright happy + deny coverage of the online forgot-password flow, fully page.route-mocked (D-10), reached from the unauthenticated LoginPage"
  - "R5 (phase-local) forgot-password E2E: happy completes to 'Password updated'; deny renders the generic, non-enumerating denial with no recovery factor named"
affects: [web-e2e-ci-job, R5-coverage]

# Tech tracking
tech-stack:
  added: []   # Playwright already installed under web/node_modules (@playwright/test)
  patterns:
    - "Fully-mocked E2E (D-10): every /api/auth/password-reset/* leg is a page.route().fulfill() — zero dependency on the Go break-glass command or live backend DB state"
    - "Anti-enumeration parity assertion: the deny /start notice is asserted with the SAME constant as the happy path (byte-identical); the denied /question shows only the generic error"
    - "Factor-identifier no-leak: body.innerHTML() must not contain the backend join columns identity_recovery / telegram_accounts (neutral 'Telegram' brand copy is allowed)"

key-files:
  created:
    - "web/e2e/password-reset.spec.ts — happy (to 'Password updated') + deny (generic, no factor named); 176 LOC"
  modified: []

key-decisions:
  - "D-10 full-mock: both paths driven entirely by page.route(); reached from the UNAUTHENTICATED LoginPage 'Forgot password?' (no gotoAuthenticated, no real backend leg, no DB seeding)"
  - "Mocked /api/auth/config (bootstrap_available:false, provider authula) so the 'Forgot password?' entry renders deterministically — the panel is otherwise hidden behind the first-user bootstrap panel when bootstrap is available"
  - "Deny no-leak asserts body.innerHTML() excludes the backend factor identifiers identity_recovery / telegram_accounts (the LookupRecoveryByEmail join columns), not the word 'Telegram' (which legitimately appears in the neutral notice + code label)"

requirements-completed: []   # R5 is a phase-local SPEC label, not a global REQUIREMENTS.md ID — see Issues Encountered

coverage:
  - id: D1
    description: "Happy path completes the mocked 4-step flow to the 'Password updated' done panel with a 'Back to sign in' control; the typed new password + answer never survive in body.innerHTML() (R5, T-43-11)"
    requirement: "R5"
    verification:
      - kind: e2e
        ref: "web/e2e/password-reset.spec.ts#happy path completes to \"Password updated\""
        status: pass
    human_judgment: false
  - id: D2
    description: "Deny path: /start returns the byte-identical neutral notice (no account enumeration) and a 403 /question surfaces exactly the generic error, naming no recovery factor (identity_recovery / telegram_accounts absent from the DOM) (R5, T-43-04)"
    requirement: "R5"
    verification:
      - kind: e2e
        ref: "web/e2e/password-reset.spec.ts#deny path shows the generic, non-enumerating denial"
        status: pass
    human_judgment: false

# Metrics
duration: ~15min
completed: 2026-07-11
status: complete
---

# Phase 43 Plan 04: Forgot-password E2E (happy + deny) Summary

**New `web/e2e/password-reset.spec.ts`: fully `page.route`-mocked Playwright coverage (D-10) of the online forgot-password flow — a happy path that completes to "Password updated" and a deny path that renders a generic, non-enumerating denial naming no recovery factor — reached from the unauthenticated LoginPage, mirroring `onboarding.spec.ts`. 4/4 tests green (chromium + mobile-chrome).**

## Performance

- **Duration:** ~15 min
- **Completed:** 2026-07-11
- **Tasks:** 1
- **Files modified:** 1 (created)

## Accomplishments
- Happy path drives the unauthenticated LoginPage → "Forgot password?" → all four mocked `/api/auth/password-reset/{start,question,verify,complete}` legs → lands on the "Password updated" done panel with a "Back to sign in" control; asserts the typed new password AND security answer never survive in `body.innerHTML()` (T-43-11 no-leak, mirroring `onboarding.spec.ts:248-251`).
- Deny path keeps `/start` ALWAYS neutral (the anti-enumeration invariant, asserted with the SAME constant as the happy path so it is character-for-character identical) and denies `/question` with a 403 → the panel's catch surfaces exactly the generic `login.reset.errors.generic` copy; asserts the DOM names no recovery factor (`identity_recovery` and `telegram_accounts` both absent) (T-43-04).
- Fully mocked (D-10): every leg is a `page.route().fulfill()` — ZERO dependency on the Go break-glass command, a live backend leg, or DB seeding (the real INNER-JOIN deny branch is covered by the R6 backend plan, not the browser). No `gotoAuthenticated`.
- Every assertion is a counted `toBeVisible()` / `not.toContain()` guarded by preceding positive assertions, so a no-op run FAILS (no-skip-as-green); the new `web/**` file auto-triggers the path-filtered web-e2e CI job (T-43-12).

## Task Commits

Committed atomically:

1. **Task 1: password-reset.spec.ts — happy + deny (page.route mocks, generic-no-factor denial) (R5, D-10)** — `2eb6ed0c` (test)

**Plan metadata:** captured in the docs commit that carries this SUMMARY + STATE.md/ROADMAP.md.

## Files Created/Modified
- `web/e2e/password-reset.spec.ts` (176 LOC) — `installAuthConfig` (mocks `/api/auth/config` → bootstrap-unavailable authula so the "Forgot password?" entry renders), `installHappyResetRoutes` / `installDenyResetRoutes`, `openResetPanel` (en-locale + unauthenticated navigation to `/login` + click "Forgot password?"), and the two `test.describe` cases (happy + deny), run on chromium + mobile-chrome.

## Verification — what WAS run (honest, no skip-as-green)
All four gates were executed live in this environment; none are compile-only claims:
- **Prettier:** `npx prettier --check e2e/password-reset.spec.ts` → "All matched files use Prettier code style!"
- **ESLint:** `npx eslint e2e/password-reset.spec.ts` → clean (`--max-warnings=0` config).
- **TypeScript:** `npx tsc --noEmit` (tsconfig `include: ["src","e2e"]`, so the spec is type-checked) → clean.
- **Playwright (REAL RUN):** `AURA_E2E_ORIGIN=http://127.0.0.1:9080 npx playwright test password-reset.spec.ts` → **4 passed (5.8s)** — happy + deny on both `chromium` and `mobile-chrome`. Runtimes 3.2–4.7s per test (multi-step, not a sub-second skip tell).
  - The spec is fully `page.route`-mocked, so it was run against the already-running `aura` container serve (reuse-external-serve via `AURA_E2E_ORIGIN`); backend DB state is irrelevant to the outcome.
  - Confirmed **no embedded-dist drift**: the served SPA bundle (`/assets/index-*.js`) embeds the current reset strings ("recovery enabled" / "Forgot password"), so the live-serve run exercises the same copy the spec asserts.

## Deviations from Plan

### Auto-added (Rule 3 — deterministic test harness)

**1. [Rule 3 - Blocking] Mocked `/api/auth/config` so the "Forgot password?" entry renders deterministically**
- **Found during:** Task 1 (reading `LoginPage.tsx:164-177,383-409`).
- **Issue:** The plan's `<action>` enumerates the four `/api/auth/password-reset/*` mocks but not `/api/auth/config`. When `loadAuthConfig()` returns `bootstrap_available: true`, `LoginPage` swaps in the first-user bootstrap panel and the "Forgot password?" button is NOT rendered — the entry point the spec depends on would vanish against a fresh-install backend.
- **Fix:** Added `installAuthConfig()` mocking `/api/auth/config` → `{ provider: 'authula', bootstrap_available: false, ... }`. This is within the D-10 full-mock spirit (the reset flow itself uses plain `postJSON` with no CSRF, so the token is inert filler).
- **Files modified:** `web/e2e/password-reset.spec.ts`
- **Committed in:** `2eb6ed0c` (Task 1 commit)

### Auto-added (Rule 2 — no-leak parity)

**2. [Rule 2 - Security] Also assert the security ANSWER never survives in the DOM on the happy path**
- **Found during:** Task 1 (mirroring `onboarding.spec.ts:250`, which asserts both PASSWORD and SECURITY_ANSWER absence).
- **Issue:** The plan's explicit no-leak prohibition names `NEW_PASSWORD`; the answer is an equally sensitive typed secret.
- **Fix:** Added `expect(html).not.toContain(SECURITY_ANSWER)` alongside the `NEW_PASSWORD` assertion. Both pass (the done panel unmounts the secret fields, which are `type=password` while mounted).
- **Files modified:** `web/e2e/password-reset.spec.ts`
- **Committed in:** `2eb6ed0c` (Task 1 commit)

---

**Total deviations:** 2 auto-added (Rule 3 harness determinism + Rule 2 no-leak parity). No behavior change vs the plan's `<action>`; both additions serve the plan's own success criteria (deterministic entry point; typed-secret no-leak). No scope creep — no `web/src` change, no new dependency.

## Threat Coverage (from the plan's `<threat_model>`)
- **T-43-04** (account/factor enumeration) — mitigated: deny `/start` notice asserted byte-identical to happy; `/question` denial is the generic constant; `identity_recovery` + `telegram_accounts` absent from `body.innerHTML()`.
- **T-43-11** (typed password in DOM) — mitigated: `body.innerHTML()` excludes `NEW_PASSWORD` (and `SECURITY_ANSWER`).
- **T-43-12** (falsely-green e2e) — mitigated: counted assertions guarded by positive `toBeVisible()` checks; a no-op fails; the new `web/**` file auto-triggers the web-e2e job.
- **T-43-SC** (installs) — n/a: no new package (Playwright already installed). No new threat surface introduced (net-new test file, no product code).

## Known Stubs
None — the spec drives the already-shipped `PasswordResetPanel` end to end; no placeholder data or unwired components.

## Issues Encountered
- **State position pointer NOT advanced (deliberate):** only `43-01-SUMMARY.md` exists on disk — plans 02/03 (the Go break-glass backend) are not yet executed. This web-E2E plan (04, `wave: 1`, `depends_on: []`) was run out of order because it is fully independent of the backend. `state advance-plan` was intentionally NOT run: STATE's "Plan 2 of 4" correctly points to the genuine next plan (02); advancing would falsely imply 02/03 are done. Progress was instead recomputed from disk via `state update-progress` (idempotent, SUMMARY-count based).
- **`requirements mark-complete` NOT run (deliberate):** the SPEC's `R1..R6` are phase-local labels; `.planning/REQUIREMENTS.md` keys global IDs by domain prefix (PROF-/LOOP-/MUSR-/…) with no `R5` entry to check off. This mirrors the 43-01 precedent (its frontmatter `requirements-completed: []` for the same reason). R5's user-observable deliverable (this spec) is complete regardless.
- **`-race` / Go gates:** n/a — this plan adds only a TypeScript Playwright spec; no Go code touched.

## User Setup Required
None — no external service configuration, env var, or migration added.

## Next Phase Readiness
- The web-e2e CI job (path-filtered on `web/**`) will pick up `password-reset.spec.ts` automatically; it runs deterministically because every leg is mocked (no live backend or DB fixture).
- The backend plans (43-02 break-glass setter + reseed, 43-03 CLI glue, and their `db_integration` R6 tests) remain to be executed; they cover the REAL `LookupRecoveryByEmail` deny branch this browser spec deliberately does not touch.
- No blockers.

## Self-Check: PASSED
- Files: FOUND web/e2e/password-reset.spec.ts
- Commits: FOUND 2eb6ed0c

---
*Phase: 43-operator-break-glass-recovery-and-forgot-password-e2e*
*Completed: 2026-07-11*
