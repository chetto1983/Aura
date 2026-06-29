# Phase 23: Frontend Infrastructure & Industrial Foundation - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-16
**Phase:** 23-frontend-infrastructure-industrial-foundation
**Areas discussed:** Linter/formatter authority, ESLint ruleset, dist/ embed strategy, Foundation scope vs UI-phase, Default density, Test harness depth, CI cadence, Package manager, PWA depth, SW tooling, React Compiler, App-shell robustness, Routing baseline, Token pipeline, CI workflow structure

---

## Linter/formatter authority (FND-03)

| Option | Description | Selected |
|--------|-------------|----------|
| Biome — lock now | One Rust binary: lint+format+import-sort, single gate, fast; shallower React/a11y depth | |
| ESLint (flat) + Prettier — lock now | Two tools, deeper react-hooks + jsx-a11y plugin coverage, slower; ecosystem standard | ✓ |
| Set constraints, FND-01 recommends | Research picks against constraints | |

**User's choice:** ESLint (flat config) + Prettier
**Notes:** Chosen for a11y / react-hooks plugin depth on an accessibility-sensitive operator UI. REQUIREMENTS.md §137 had flagged this as the FND-01 open question — resolved here.

---

## ESLint ruleset baseline

| Option | Description | Selected |
|--------|-------------|----------|
| Curated-strict, type-aware | typescript-eslint strict-type-checked + react-hooks + jsx-a11y + import/order | |
| Airbnb-style comprehensive | eslint-config-airbnb-typescript — max coverage, heavily opinionated, stylistic overlap with Prettier | ✓ |
| Lock tools now, FND-01 picks preset | Research selects the rule preset | |

**User's choice:** Airbnb-style comprehensive
**Notes:** Integration detail recorded: layer `eslint-config-prettier` last to disable Airbnb stylistic rules that overlap Prettier; keep `eslint-plugin-react-compiler` on.

---

## dist/ embed strategy (FND-02)

| Option | Description | Selected |
|--------|-------------|----------|
| Commit dist/ + CI freshness gate | `go build` works with zero Node; CI rebuilds dist, fails on git diff; dist churns in git | ✓ |
| Build-time-only, dist gitignored | Clean repo; every Go build path needs Node first / a placeholder; risk of stale embed | |
| Let FND-01 recommend | Research weighs cleanliness vs portability | |

**User's choice:** Commit dist/ + CI freshness gate
**Notes:** Preserves single-binary invariant + zero-Node `go build` everywhere; Node-24 Docker still rebuilds dist reproducibly (no Node in runtime image).

---

## Foundation scope vs UI-phase (FND-04)

| Option | Description | Selected |
|--------|-------------|----------|
| Lock token arch + real palette here; lean scaffold | Real v1 dark-operator palette + density now; no assistant-ui yet; defer component visuals | ✓ |
| Run /gsd-ui-phase first for a visual UI-SPEC | Designed visual contract before scaffolding | |
| Structural plumbing only, neutral palette | Token plumbing only, defer all palette to ui-phase | |

**User's choice:** Lock token arch + real palette here; lean scaffold
**Notes:** Satisfies SC2 (theme+density before paint on the placeholder). assistant-ui deferred to Phase 25. A `/gsd-ui-phase` pass remains optional (noted as deferred).

---

## Default density

| Option | Description | Selected |
|--------|-------------|----------|
| Operator | Middle/primary cockpit tier — balanced density | ✓ |
| Compact | Denser default | |
| Review | Roomier default | |

**User's choice:** Operator

---

## Test harness depth (FND-06)

| Option | Description | Selected |
|--------|-------------|----------|
| Vitest + RTL component + Playwright smoke vs dist preview | Harness green now; serve-integration E2E deferred to Phase 24 | |
| Vitest unit + one render smoke only | Leanest gate; defer all E2E | |
| Full Playwright E2E vs `aura serve` now | Real E2E booting the embedded shell | ✓ |

**User's choice:** Full Playwright E2E vs `aura serve` now
**Notes:** Confirmed in-scope via SC2 (serving the placeholder shell IS Phase 23). E2E asserts placeholder shell only (brand + theme-before-paint + no marketing hero), not chat/auth/health.

---

## CI cadence for the E2E

| Option | Description | Selected |
|--------|-------------|----------|
| Every PR, blocking | Single fast smoke; full discipline; adds dist+Go+browser build per PR | ✓ |
| PR smoke + nightly full | Lighter PRs; heavier E2E nightly/on-merge | |
| Nightly / label-gated only | Fastest PRs, weakest gate | |

**User's choice:** Every PR, blocking

---

## Package manager

| Option | Description | Selected |
|--------|-------------|----------|
| npm | Matches SC wording, zero extra tooling | |
| pnpm | Faster, strict, disk-efficient; adds corepack step | |
| Let FND-01 recommend | Research decides | ✓ |

**User's choice:** Let FND-01 recommend
**Notes:** Deferred to the research pass; npm is the SC-implied default unless research shows pnpm is worth the setup.

---

## PWA depth (FND-05)

| Option | Description | Selected |
|--------|-------------|----------|
| Metadata-only, no service worker | manifest + theme-color + favicon; no SW | |
| Full installable PWA + service worker | Offline caching + installability; SW cache-invalidation needs care | ✓ |
| Let FND-01 recommend | Research decides | |

**User's choice:** Full installable PWA + service worker
**Notes:** Constraint recorded — the SW cache must version against the build hash so a new single-binary release doesn't serve stale assets.

---

## SW tooling

| Option | Description | Selected |
|--------|-------------|----------|
| vite-plugin-pwa / Workbox | Manifest + content-hash-revisioned precache SW + autoUpdate | ✓ |
| Hand-rolled service worker | Full control, manual cache-versioning, more footguns | |
| Let FND-01 recommend | Research picks | |

**User's choice:** vite-plugin-pwa / Workbox
**Notes:** Resolves the D-10 stale-cache constraint via Workbox's revisioned precache.

---

## React Compiler (React 19)

| Option | Description | Selected |
|--------|-------------|----------|
| Lint rule on, compiler off | eslint-plugin-react-compiler only; transform deferred | |
| Enable React Compiler now | babel-plugin-react-compiler from the start; auto-memoization | ✓ |
| Defer entirely | No rule, no compiler | |

**User's choice:** Enable React Compiler now
**Notes:** Implication recorded — Vite React plugin must be Babel-based `@vitejs/plugin-react` (not SWC) to host the compiler; lint rule stays on.

---

## App-shell robustness

| Option | Description | Selected |
|--------|-------------|----------|
| Root error boundary, no telemetry | Safe themed fallback; no client error sink yet | ✓ |
| Error boundary + client telemetry hook | Also wire a minimal error-reporting hook | |
| Neither yet | Bare shell | |

**User's choice:** Root error boundary, no telemetry

---

## Routing baseline

| Option | Description | Selected |
|--------|-------------|----------|
| No router yet, lock the choice in the record | Single placeholder screen; React Router intended, wired Phase 24 | ✓ |
| Install React Router now | react-router-dom + placeholder route now | |
| Install TanStack Router now | Type-safe routes, heavier | |

**User's choice:** No router yet, lock the choice in the record
**Notes:** React Router locked as the intended choice; wired in Phase 24 with real SPA routes.

---

## Token pipeline

| Option | Description | Selected |
|--------|-------------|----------|
| Hand-authored tokens.json + tiny generator | tokens.json → Tailwind 4 @theme + data-theme/data-density attrs; minimal | ✓ |
| DTCG / Style Dictionary pipeline | Standards-based, multi-platform, heavier tooling | |
| Let FND-01 recommend | Research decides | |

**User's choice:** Hand-authored tokens.json + tiny generator
**Notes:** Apply-before-paint via a pre-hydration inline `<head>` script setting root attributes before React mounts.

---

## CI workflow structure

| Option | Description | Selected |
|--------|-------------|----------|
| Integrate web jobs into existing CI workflow | Unified gate + path filters; one CI file | ✓ |
| Separate .github/workflows/web.yml | Isolated pipeline, two files to sync | |
| Let FND-01 recommend | Research decides | |

**User's choice:** Integrate into the existing CI workflow
**Notes:** Web jobs (lint/format/tsc/vitest/playwright) sit alongside the Go jobs with path filters that skip web when only Go changes.

---

## Claude's Discretion

- Package-manager choice (npm vs pnpm) — deferred to the FND-01 research pass.
- Exact Vite plugin list, Playwright project/browser config, exact dark-operator palette hex values — research/scaffold detail (direction locked, values not).
- `web/` package/repo layout (single package vs workspace) — FND-01 research; default single `web/` npm package.

## Deferred Ideas

- assistant-ui chat stack → Phase 25.
- React Router wiring + real SPA routes + SPA-fallback route exclusion → Phase 24.
- Real SPA host, GAP-2 web auth, non-loopback boot guard, runtime health panel → Phase 24.
- Typed-display router / graph explorer / governance boards / onboarding → Phases 26-29.
- Optional `/gsd-ui-phase 23` deeper visual contract — not required by these decisions.
- Client error telemetry sink → later phase (needs a backend endpoint).
