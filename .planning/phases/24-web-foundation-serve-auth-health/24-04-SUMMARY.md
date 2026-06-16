---
phase: 24-web-foundation-serve-auth-health
plan: 04
subsystem: ui
tags: [react, react-router, react-query, react-i18next, vite, playwright, stryker, vitest, accessibility]

requires:
  - phase: 24-02
    provides: spaFallback SPA host + the embedded internal/webui/dist
  - phase: 24-03
    provides: RequireAuth boundary + POST /login + /logout + the capability gate
provides:
  - React login page (WEB-03 frontend) with passphrase show/hide toggle + i18n
  - Read-only runtime health panel (WEB-04) polling /healthz + /readyz via React Query
  - React Router + client-side 404 (WEB-01 frontend)
  - Live serve_smoke (real-binary WEB-01/02/03 proof) + Playwright health-panel E2E
  - react-i18next en/it cockpit i18n + a vitest≥85% / Stryker≥70% frontend quality gate
affects: [25-chat, 26-typed-display, 27-graph, 28-gov-read-onboarding, 29-mcp-skills]

tech-stack:
  added: [react-router-dom, "@tanstack/react-query", react-i18next, i18next, "@stryker-mutator/core", "@stryker-mutator/vitest-runner"]
  patterns:
    - "Components read copy via useTranslation()/t('feature.key'); en/it bundles in web/src/i18n/resources.ts"
    - "Embedded dist rebuilt byte-canonically on node:24 Docker (web-dist-freshness parity)"
    - "Frontend quality gate: vitest coverage threshold ≥85% + Stryker mutation ≥70% (critical-file spot-check)"

key-files:
  created:
    - web/src/routes/LoginPage.tsx
    - web/src/routes/NotFoundView.tsx
    - web/src/health/RuntimeHealthPanel.tsx
    - web/src/health/useRuntimeHealth.ts
    - web/src/i18n/{i18n.ts,resources.ts,LanguageSwitcher.tsx}
    - cmd/aura/serve_smoke_test.go
    - web/e2e/health-panel.spec.ts
    - web/stryker.config.json
  modified:
    - web/src/main.tsx
    - web/src/AppShell.tsx
    - internal/agui/auth.go
    - internal/webui/embed.go
    - cmd/aura/serve_webui.go
    - internal/webui/dist (rebuilt)
    - web/vitest.config.ts

key-decisions:
  - "react-router-dom 7 + @tanstack/react-query 5 added inside the Phase-23-locked toolchain (no Go deps)"
  - "Login page omits a client-side auth guard — the server's RequireAuth is the only boundary"
  - "Health panel reads only existing /healthz + /readyz + bind/build (D-07, no new backend endpoint)"
  - "Static assets served pre-auth (a single shared SPA bundle isn't sensitive); only the shell + API are gated"
  - "Mutation gate scoped to critical files (LoginPage, RuntimeHealthPanel, useRuntimeHealth, applyTheme, density) — backend spot-check discipline"

patterns-established:
  - "Pattern: webui.IsPublicAsset predicate wired into AuthDeps.PublicAsset — webui owns the embedded-asset truth"
  - "Pattern: aria-invalid={cond || undefined} (omit-when-valid) for conditional ARIA attributes"
  - "Pattern: hook-mocked render tests assert status->tone mapping to kill presentational mutants"

requirements-completed: [WEB-04, WEB-01]

duration: ~3h (plan exec + operator-checkpoint fixes + i18n/quality follow-ons)
completed: 2026-06-16
---

# Phase 24-04: Runtime health shell + login page + router/404 Summary

**React login page + read-only runtime health panel on the Phase-23 dark-operator tokens, React Router + client 404, a live serve_smoke proving WEB-01/02/03 against the real binary, plus react-i18next (en/it) and a vitest≥85% / Stryker≥70% frontend quality gate.**

## Performance
- **Duration:** ~3h (plan Tasks 1–4 + the human-verify checkpoint resolution + the i18n/quality follow-ons)
- **Completed:** 2026-06-16
- **Tasks:** 4 (+ 2 operator-checkpoint bug fixes + 3 quality/i18n follow-ons)
- **Files modified:** ~40 (web/ + cmd/aura + internal/agui + internal/webui)

## Accomplishments
- The WEB-03 login page (passphrase → POST /login, role=alert errors, session-expired notice) and the WEB-04 read-only runtime health panel (Liveness/Readiness/Postgres/Neo4j/Bind/Build over /healthz+/readyz via React Query, dot+text per row, never colour-only).
- React Router (D-14) + a client-side NotFoundView; theme/density applied before paint (D-08, the index.html pre-paint script reused).
- A live `serve_smoke` (real `aura serve` binary) proving WEB-02 fail-fast, WEB-01 `/api/*` real-404, WEB-03 gated shell + public /healthz + login-end-to-end; plus a Playwright health-panel E2E.
- The embedded `internal/webui/dist` rebuilt byte-canonically on node:24 (web-dist-freshness parity).
- **Operator-checkpoint fixes:** the aria-invalid omit-when-valid a11y fix and the D-03 static-asset-pre-auth fix (the login page was 401-ing its own CSS/JS).
- **Follow-ons:** react-i18next en/it cockpit i18n + a login show/hide-password toggle; frontend coverage ≥85% (vitest threshold) and Stryker mutation ≥70% (82.2% on the critical files).

## Task Commits
1. **Task 1: React Router + React Query** — `4e229a05` (feat)
2. **Task 2: LoginPage + NotFoundView + runtime health panel** — `131bf257` (feat)
3. **Task 3: rebuild + commit embedded dist (checkpoint)** — `384a506d` (build), `a9a8c8c0` (test: dynamic dist-asset discovery)
4. **Task 4: serve_smoke live proof + Playwright health-panel E2E** — `c0c38774` (test)

**Operator-checkpoint fixes (human-verify gate):** `1c494d24` (aria-invalid), `0fa2d865` (D-03 static assets pre-auth)
**Follow-ons:** `6f0a291d` (i18n + eye toggle), `6ccd663b` (coverage ≥85%), `6c90efe7` (Stryker harness), `a2b27f9c` (mutation ≥70%)

## Decisions Made
See key-decisions frontmatter. Notable: static assets are public (the shared SPA bundle is the same code for everyone — gating it only breaks the login render); the mutation gate is a critical-file spot-check matching the backend.

## Deviations from Plan

### Auto-fixed Issues (operator-reported at the human-verify checkpoint)

**1. [Rule 1 — Bug] aria-invalid="false" on the pristine passphrase field**
- **Found during:** Task 3 operator visual sign-off (Microsoft Edge Tools / axe aria-valid-attr-value).
- **Fix:** `aria-invalid={error !== null || undefined}` (omit-when-valid) + a regression test; dist rebuilt.
- **Committed in:** `1c494d24`.

**2. [Rule 3 — Blocking] Login page static assets 401-gated**
- **Found during:** Task 3 operator sign-off (CSS/JS/manifest/registerSW all 401, login unstyled).
- **Issue:** RequireAuth's public-path allowlist used a fictional `/login-assets/` prefix the Vite build never emits (the SPA is one shared `/assets/` bundle).
- **Fix:** a `webui.IsPublicAsset` predicate wired into `AuthDeps.PublicAsset`; static assets served pre-auth, shell + API stay gated. Unit + live verified.
- **Committed in:** `0fa2d865`.

**3. [Beyond-scope, user-directed] i18n + password toggle + frontend quality gates**
- react-i18next en/it migration + a login show/hide toggle (`6f0a291d`); frontend coverage ≥85% (`6ccd663b`) and Stryker mutation ≥70% (`6c90efe7` + `a2b27f9c`) per a session directive.

---
**Total deviations:** 2 operator-checkpoint bug fixes + 3 user-directed follow-ons.
**Impact on plan:** the two fixes were necessary for a usable/correct login surface; the follow-ons extend quality + i18n beyond the plan. No scope regression.

## Issues Encountered
- Cross-platform npm lockfile drift (Windows drops the `@emnapi/*` Linux optionals) recurred on each web `npm install`; regenerated on Linux for the committed lock. The Dockerfile rebuilds dist on node:24, so the daemon's asset hashes differ from a Windows host build but are internally consistent.

## User Setup Required
None — the daemon reads `AURA_WEB_AUTH_SECRET` (already in `.env`); compose was wired to pass it through so the containerized daemon boots behind the auth boundary.

## Next Phase Readiness
- The serve + auth + health + cockpit-shell foundation is live; Phase 25 (chat + approval loop) can build the Core-Value surface on the authenticated shell.
- Open: wire the vitest coverage + Stryker mutation gates into the web CI job; the parallel-session lock drift needs a Linux regen before the next image build.

---
*Phase: 24-web-foundation-serve-auth-health*
*Completed: 2026-06-16*
