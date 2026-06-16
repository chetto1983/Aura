# Phase 23: Frontend Infrastructure & Industrial Foundation - Research

**Researched:** 2026-06-16
**Domain:** Frontend industrial toolchain (Vite 8 + React 19 + TS) embedded in a Go single binary
**Confidence:** HIGH (core toolchain versions + APIs verified against npm registry + official docs/changelogs; palette is a designed proposal — MEDIUM)

> This RESEARCH.md **is** the FND-01 deliverable: the locked industrial-infra **decision record**. It resolves every deferred choice (D-04 package manager, exact plugin list + versions, Playwright config, dark-operator palette, `web/` layout) and **corrects three locked decisions** (D-02, D-12) where the upstream 2026 reality diverged from the language used in CONTEXT.md — see **Decision Corrections** below. The corrections honor the *intent* of each decision; they do not reopen the user's choices.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions (DETAIL these, do not reopen)
- **D-01** — ESLint (flat config) + Prettier; NOT Biome. (deeper `react-hooks`/`jsx-a11y`/`exhaustive-deps` coverage for an a11y-sensitive operator UI)
- **D-02** — Airbnb-style comprehensive ruleset baseline; layer `eslint-config-prettier` LAST so Prettier and the linter don't fight; enable React-Compiler Rules-of-React enforcement. *(package realization corrected below — see Decision Correction #1)*
- **D-03** — Zero-warning blocking gate: `lint` + `format:check` + `tsc --noEmit` all zero-warning, blocking CI, parity with `golangci-lint`.
- **D-04** — Package manager DEFERRED to this research pass (npm vs pnpm). **RESOLVED → npm** (see Decision Record).
- **D-05** — Commit `web/dist/` into the repo + a CI freshness gate (`go build` needs zero Node everywhere; a CI job rebuilds `dist` and **fails on a non-empty `git diff`**). Accepted cost: `dist/` churns in git history.
- **D-06** — Node-24 multi-stage Docker rebuilds `dist` byte-reproducibly for the runtime image; **no Node in the runtime layer**; committed `dist` and Docker-rebuilt `dist` must match (freshness gate ties them).
- **D-07** — Lock the real dark-operator theme HERE: `tokens.json` + a real v1 dark-operator palette (elysia-informed, industrial, NO abstract sphere) + density modes (`compact`|`operator`|`review`), applied **before paint**. **Default density = `operator`.**
- **D-08** — Token pipeline = hand-authored `tokens.json` + a tiny generator (NO Style Dictionary / DTCG heavy tooling) → CSS vars + `data-theme`/`data-density`; pre-hydration inline `<head>` script applies persisted prefs before React mounts (no flash).
- **D-09** — Lean scaffold. NO assistant-ui / chat deps in Phase 23 (Phase 25). Placeholder shell is the only screen.
- **D-10** — Full installable PWA + service worker via `vite-plugin-pwa`/Workbox: content-hash-revisioned precache + `autoUpdate`; SW cache MUST version against the build hash so a new single-binary release never serves stale assets.
- **D-11** — Brand from existing `public/Logo.png` (header + favicon + apple-touch-icon + `theme-color` + manifest). NO marketing hero text / decorative badges / tutorial paragraphs in the primary viewport (ux-spec §350).
- **D-12** — React Compiler ENABLED now (`babel-plugin-react-compiler`); the React plugin must host the compiler, NOT `@vitejs/plugin-react-swc`. *(hosting mechanism corrected below — see Decision Correction #2; the *intent* "compiler on, not swc" is preserved.)*
- **D-13** — Root React error boundary rendering a themed safe fallback. NO client error telemetry yet (defer; no backend sink).
- **D-14** — No router in Phase 23; lock **React Router** as the intended choice, wire it in Phase 24.
- **D-15** — Test harness = Vitest + React Testing Library + full Playwright E2E booting `aura serve`. E2E asserts the placeholder shell ONLY (brand renders, theme+density before paint, no marketing hero text) — NOT chat/auth/health.
- **D-16** — Playwright E2E on EVERY PR, blocking; CI builds dist → builds the Go binary with embedded dist → boots `aura serve` on loopback → runs Playwright.
- **D-17** — Web CI jobs in the EXISTING CI workflow (`.github/workflows/ci.yml`), path-filtered (skip web jobs when only Go changes). One unified gate.
- **D-18** — `web/` follows master-direct commit discipline; no separate versioning/changeset tooling.

### Claude's Discretion (resolved in this pass)
- D-04 package manager → **npm** (Decision Record).
- Exact Vite plugin list + versions → **pinned below** (Standard Stack).
- Playwright project/browser config → **Chromium-only headless, `webServer` boots the Go binary** (PWA specifics + Validation).
- Exact dark-operator palette hex → **concrete v1 palette below** (Theme/Token Architecture).
- `web/` layout (single package vs workspace) → **single `web/` npm package** (Scaffold Blueprint).

### Deferred Ideas (OUT OF SCOPE — do not build in Phase 23)
- assistant-ui chat stack → Phase 25.
- React Router wiring + real SPA routes + SPA-fallback route exclusion → Phase 24.
- Real SPA host, web-auth boundary (GAP-2), non-loopback boot guard, runtime health panel → Phase 24.
- Typed-display router, graph explorer, governance boards, onboarding → Phases 26–29.
- Client error telemetry sink → later phase (needs a backend endpoint).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| FND-01 | Locked foundation decision record (linter, formatter, token architecture, layout, build/release pipeline, test harness) | THIS document — Decision Record + Scaffold Blueprint + every section below |
| FND-02 | Vite 8 + React 19 + TS scaffold + `//go:embed all:dist` pipeline → binary-embeddable dist consumed by `aura serve` | Standard Stack + Go Embed Integration |
| FND-03 | ESLint (flat) + Prettier + `tsc --noEmit` zero-warning blocking CI gate (golangci-lint parity) | ESLint Flat-Config Shape + CI + Docker |
| FND-04 | `tokens.json` → Tailwind 4 `@theme` dark-operator palette + density modes, applied before paint (no flash) | Theme/Token Architecture |
| FND-05 | `public/Logo.png` in header + favicon + PWA/theme-color metadata; no marketing hero text | Brand + PWA Specifics |
| FND-06 | Vitest + component/E2E harness + Node-24 multi-stage Docker build producing the embedded asset, no Node in runtime | CI + Docker + Validation Architecture |
</phase_requirements>

## Summary

Phase 23 stands up a single `web/` npm package (Vite 8.0 + React 19.2 + TypeScript 6) whose Rolldown-bundled `dist/` is committed to the repo, embedded into the Go binary with `//go:embed all:dist`, and served by `aura serve` as a branded, dark-operator-themed placeholder shell. The quality bar mirrors the Go side exactly: a zero-warning, blocking, every-PR gate (ESLint flat config + Prettier + `tsc --noEmit` + Vitest + a Playwright E2E that boots the real binary), all path-filtered into the existing `ci.yml`. A Node-24 multi-stage Docker stage rebuilds the same `dist` for the runtime image, and a CI freshness gate ties the committed `dist` to a fresh rebuild (`git diff` must stay empty). Theme and density are applied **before paint** via a tiny generator that maps a hand-authored `tokens.json` to a Tailwind 4 `@theme` block plus a pre-hydration inline `<head>` script — no Style Dictionary, no flash.

The 2026 toolchain reality diverged from the literal package names in CONTEXT.md in three places, all verified against the npm registry and official changelogs: (1) `eslint-config-airbnb-typescript` was **archived May 2025** and has no ESLint 9/10 flat-config support, so the Airbnb *intent* is realized through the modern flat-config equivalent (`typescript-eslint` strict-type-checked + `eslint-plugin-react-hooks` + `jsx-a11y` + `import-x`), not the dead package; (2) `@vitejs/plugin-react` v6 (shipped with Vite 8) **dropped internal Babel for Oxc**, so React Compiler is now hosted via `@rolldown/plugin-babel` + `reactCompilerPreset()` rather than the old `react({ babel })` path — the swc-vs-Babel framing is obsolete; (3) the standalone `eslint-plugin-react-compiler` is **deprecated and merged into `eslint-plugin-react-hooks@7` `recommended-latest`**. Each correction preserves the user's decision intent.

**Primary recommendation:** Scaffold a single `web/` package with **npm** + **Vite 8.0.16** + **React 19.2** + **TypeScript 6** + **Tailwind 4.3 (`@tailwindcss/vite`)** + **`@vitejs/plugin-react` 6 + `@rolldown/plugin-babel` + `babel-plugin-react-compiler` 1.0** (Compiler on, `babel()` ordered before `react()`) + **`vite-plugin-pwa` 1.3 (`autoUpdate`/`generateSW`)**; lint with **ESLint 10 flat config** (`typescript-eslint` strict-type-checked + `react-hooks` recommended-latest + `jsx-a11y` + `eslint-config-prettier` LAST); test with **Vitest 4 + RTL 16** and a **Chromium-only Playwright** E2E whose `webServer` boots the embedded `aura serve` binary on loopback. Commit `dist/`, gate it with a freshness check, and rebuild it in a Node-24 Docker build stage.

## Decision Corrections (intent preserved, package realization updated)

> These three corrections are the most important output of this research pass. Each was verified against the npm registry **and** an official changelog/doc; each keeps the user's decision *intent* and only swaps a now-dead/renamed package or an obsolete mechanism.

### Correction #1 — D-02 Airbnb baseline: realize intent via modern flat config, not the dead package
- **What CONTEXT.md said:** baseline = `eslint-config-airbnb-typescript`.
- **2026 reality:** `eslint-config-airbnb-typescript` was **archived 2025-05-12** and never gained flat-config support. `[CITED: github.com/iamturns/eslint-config-airbnb-typescript#331]` The maintained fork `@kesills/eslint-config-airbnb-typescript@20` (last publish 2024-09-16) explicitly states it **does not support ESLint v9 or above** and only works via `@eslint/eslintrc` `FlatCompat` shims. `[CITED: github.com/Kenneth-Sills/eslint-config-airbnb-typescript README]` ESLint 10 is **flat-config-only** (eslintrc removed). The literal package is therefore incompatible with the D-01 chosen stack.
- **Decision:** Realize the Airbnb *intent* ("comprehensive, opinionated, a11y-aware baseline") through the modern flat-config standard: `typescript-eslint` **strict-type-checked + stylistic-type-checked** configs + `eslint-plugin-react-hooks` (recommended-latest) + `eslint-plugin-jsx-a11y` + `eslint-plugin-import-x` (the maintained, flat-native fork of `eslint-plugin-import`), with `eslint-config-prettier` layered LAST (D-02's integration constraint, preserved). This is strictly *more* comprehensive than Airbnb's TS rules on a 2026 stack and is the only path that also satisfies D-01 (flat config) and D-12 (React Compiler lint). **No `FlatCompat` shim, no archived package.**

### Correction #2 — D-12 React Compiler: hosted via `@rolldown/plugin-babel`, not `react({ babel })`
- **What CONTEXT.md said:** use the Babel-based `@vitejs/plugin-react` (NOT `@vitejs/plugin-react-swc`) so it can host `babel-plugin-react-compiler`.
- **2026 reality:** `@vitejs/plugin-react@6.0.0` (shipped *with* Vite 8) **removed Babel as a dependency and now uses Oxc** for the JSX/React-Refresh transform; the old `react({ babel: {...} })` path no longer exists. `[CITED: github.com/vitejs/vite-plugin-react releases plugin-react@6.0.0]` `[CITED: react.dev/learn/react-compiler/installation]` There is no longer a Babel-vs-swc *plugin choice* — both transforms are Rust now. To run the (Babel-based) React Compiler you add a separate `@rolldown/plugin-babel` plugin hosting `reactCompilerPreset()`, ordered **before** `react()`.
- **Decision:** Keep `@vitejs/plugin-react` (NOT `-swc`, per D-12 intent — there is no separate swc package needed). Add `@rolldown/plugin-babel` + `babel-plugin-react-compiler@1.0`. Plugin order: `babel(reactCompilerPreset())` → `react()` → `tailwindcss()` → `VitePWA()`. The D-12 *intent* (compiler on; don't pick the swc fork) is fully preserved; only the hosting mechanism is the current one.

### Correction #3 — D-02 React-Compiler lint: `eslint-plugin-react-hooks`, not `eslint-plugin-react-compiler`
- **What CONTEXT.md said:** enable `eslint-plugin-react-compiler` for Rules-of-React enforcement.
- **2026 reality:** the standalone `eslint-plugin-react-compiler` is **deprecated**; all compiler rules were merged into `eslint-plugin-react-hooks@7` under the `recommended-latest` config. `[CITED: react.dev/reference/eslint-plugin-react-hooks]` `[CITED: github.com/reactjs/react.dev#8036]`
- **Decision:** Enable React-Compiler Rules-of-React via `eslint-plugin-react-hooks@7` `recommended-latest`. D-02's *intent* (Rules-of-React enforced in lint) is preserved with the current, non-deprecated package.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Asset build (bundle/minify/hash) | Build tooling (Vite/Rolldown) | Node-24 Docker stage | Produces the embeddable `dist/`; never runs at runtime |
| Static asset serving | API/Backend (`aura serve` Go) | — | Single-binary invariant: the Go process serves the embedded SPA; no separate web server |
| Theme/density apply-before-paint | Browser (inline `<head>` script) | Build (token→CSS-var generation) | Must run before React mounts; lives in `index.html`, generated from `tokens.json` |
| React render (placeholder shell) | Browser/Client | — | All UI is client-side; the Go tier only ships bytes |
| Service worker / PWA precache | Browser (Workbox SW) | Build (manifest revision hashing) | SW lives in the browser; revisions are content-hashed at build time |
| Lint/format/type-check gate | CI (Node, build-time only) | — | Quality gate; never in the runtime image |
| E2E (boot binary + assert shell) | CI (Playwright drives a real `aura serve`) | API/Backend (binary under test) | Proves the embed→serve→render path end-to-end |
| Routing (locked, not wired) | Browser/Client (Phase 24) | — | React Router chosen; deliberately out of Phase 23 |

## Standard Stack

> All versions below are **verified against the npm registry on 2026-06-16** via `npm view <pkg> version`. Pin with caret ranges in `package.json` and a committed `package-lock.json`; the lockfile is the real reproducibility anchor.

### Core
| Library | Version (verified) | Purpose | Why Standard |
|---------|--------------------|---------|--------------|
| `vite` | 8.0.16 | Build tool + dev server (Rolldown-bundled) | Vite 8 ships Rolldown (Rust) as the single bundler; 10–30× faster builds, full plugin compat `[VERIFIED: npm registry]` `[CITED: vite.dev/blog/announcing-vite8]` |
| `react` / `react-dom` | 19.2.7 | UI runtime | React 19; Compiler needs no extra runtime lib on 19 `[VERIFIED: npm registry]` |
| `typescript` | 6.0.3 | Type system + `tsc --noEmit` gate | Current stable; flat-config typescript-eslint targets it `[VERIFIED: npm registry]` |
| `@vitejs/plugin-react` | 6.0.2 | React/JSX + Fast Refresh (Oxc, not Babel) | v6 ships with Vite 8; Oxc transform, smaller footprint `[VERIFIED: npm registry]` `[CITED: github.com/vitejs/vite-plugin-react releases plugin-react@6.0.0]` |
| `@rolldown/plugin-babel` | 0.2.3 | Hosts the React Compiler Babel plugin on Vite 8 | The official path to run Babel plugins after v6 dropped Babel `[VERIFIED: npm registry]` `[CITED: react.dev/learn/react-compiler/installation]` |
| `babel-plugin-react-compiler` | 1.0.0 | Auto-memoization (Rules of React) | Compiler 1.0 stable (Oct 2025); free perf for all future components (D-12) `[VERIFIED: npm registry]` |
| `tailwindcss` + `@tailwindcss/vite` | 4.3.1 | CSS-first `@theme` design tokens | v4 `@theme` directive + `data-*` variants is exactly the D-07/D-08 mechanism `[VERIFIED: npm registry]` `[CITED: tailwindcss.com/docs/theme]` |
| `vite-plugin-pwa` | 1.3.0 | Installable PWA + Workbox SW (`generateSW`/`autoUpdate`) | Content-hash-revisioned precache; cleans old caches on activate → no stale assets across releases (D-10) `[VERIFIED: npm registry]` `[CITED: vite-pwa-org.netlify.app/guide/auto-update]` |

### Supporting (lint / format / test)
| Library | Version (verified) | Purpose | When to Use |
|---------|--------------------|---------|-------------|
| `eslint` | 10.5.0 | Linter (flat-config only) | The D-01/D-03 gate `[VERIFIED: npm registry]` |
| `typescript-eslint` | 8.61.1 | TS-aware flat configs (strict-type-checked + stylistic) | Airbnb-intent baseline (Correction #1) `[VERIFIED: npm registry]` |
| `@eslint/js` | 10.0.1 | ESLint core recommended flat config | Base layer `[VERIFIED: npm registry]` |
| `eslint-plugin-react-hooks` | 7.1.1 | Rules of Hooks + **React Compiler rules** (`recommended-latest`) | Corrections #1/#3 `[VERIFIED: npm registry]` `[CITED: react.dev/reference/eslint-plugin-react-hooks]` |
| `eslint-plugin-react-refresh` | 0.5.3 | Fast-Refresh boundary lint (Vite dev) | Dev-correctness `[VERIFIED: npm registry]` |
| `eslint-plugin-jsx-a11y` | 6.10.2 | Accessibility lint (the D-01 reason to keep ESLint over Biome) | a11y-sensitive operator UI `[VERIFIED: npm registry]` |
| `eslint-plugin-import-x` | latest (verify at scaffold) | Import-order/resolution (flat-native fork of `eslint-plugin-import`) | Airbnb-intent import discipline `[ASSUMED — verify version at scaffold]` |
| `prettier` | 3.8.4 | Formatter (`format` + `format:check`) | D-01/D-03 `[VERIFIED: npm registry]` |
| `eslint-config-prettier` | 10.1.8 | Turns off lint rules Prettier owns — **layered LAST** | D-02 integration constraint `[VERIFIED: npm registry]` |
| `vitest` | 4.1.9 | Unit/component test runner (Vite-native) | D-15 `[VERIFIED: npm registry]` |
| `@vitest/coverage-v8` | 4.1.9 | Coverage (V8) | Coverage gate parity `[VERIFIED: npm registry]` |
| `@testing-library/react` | 16.3.2 | Component testing (RTL) | D-15 `[VERIFIED: npm registry]` |
| `jsdom` | 29.1.1 | DOM env for Vitest component tests | RTL backend `[VERIFIED: npm registry]` |
| `@playwright/test` | 1.61.0 | E2E (boots `aura serve`) | D-15/D-16 `[VERIFIED: npm registry]` |
| `globals` | 17.6.0 | Global env definitions for flat config | ESLint env `[VERIFIED: npm registry]` |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| npm (chosen, D-04) | pnpm 11.7.0 | pnpm is faster + strict-by-default, but adds a non-standard lockfile/store + a `corepack`/`pnpm/action-setup` CI step + a `pnpm` Docker install; SC wording uses `npm run …` and Aura already runs Node-via-npm in the Docker image. npm is the minimal-industrial choice — see Decision Record rationale. |
| `typescript-eslint` strict configs (chosen) | `@kesills/eslint-config-airbnb-typescript` + FlatCompat | Dead-end: caps at ESLint 8, shims eslintrc; conflicts with ESLint 10 + D-01. |
| ESLint+Prettier (D-01 locked) | Biome | Rejected by D-01 (Biome lacks the deep `react-hooks`/`jsx-a11y`/`exhaustive-deps` plugin coverage). Not reopened. |
| `vite-plugin-pwa` generateSW (chosen) | injectManifest (hand-written SW) | injectManifest gives full SW control but is more code; generateSW's revisioned precache already satisfies D-10's stale-asset constraint. Lean-scaffold (D-08/D-09). |
| Single `web/` package (chosen) | npm/pnpm workspace monorepo | A workspace buys nothing for one package and adds root-config complexity; default to single package (D-04 discretion note). |

**Installation (npm):**
```bash
# in web/
npm install react react-dom
npm install -D vite @vitejs/plugin-react @rolldown/plugin-babel babel-plugin-react-compiler \
  typescript @types/react @types/react-dom \
  tailwindcss @tailwindcss/vite vite-plugin-pwa \
  eslint @eslint/js typescript-eslint eslint-plugin-react-hooks eslint-plugin-react-refresh \
  eslint-plugin-jsx-a11y eslint-plugin-import-x eslint-config-prettier prettier globals \
  vitest @vitest/coverage-v8 @testing-library/react jsdom @playwright/test
```
**Version verification (run again at scaffold time — versions drift):**
```bash
npm view vite version && npm view @vitejs/plugin-react version && npm view react version
npm view babel-plugin-react-compiler version && npm view tailwindcss version
```

## Package Legitimacy Audit

> Ran the Package Legitimacy Gate (slopcheck 0.6.1, forced `-e npm`) on 2026-06-16. The batch run flagged 24 OK / 1 SUS; the single SUS was the youngest package (`@rolldown/plugin-babel`, created 2026-02-26) tripping the "new package" heuristic. Re-scanned individually via `slopcheck scan --pkg npm <pkg>` → **all individually `[OK]`**. All packages resolve to official orgs (vitejs, rolldown, facebook/react, tailwindlabs, vite-pwa, microsoft) with established source repos.

| Package | Registry | Age | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-------------|-----------|-------------|
| vite | npm | 5+ yrs | github.com/vitejs/vite | OK | Approved |
| @vitejs/plugin-react | npm | mature | github.com/vitejs/vite-plugin-react | OK | Approved |
| @rolldown/plugin-babel | npm | ~4 mo (created 2026-02-26) | github.com/rolldown/plugins | OK (SUS in batch on age heuristic only) | Approved — official Rolldown org; react.dev-recommended |
| babel-plugin-react-compiler | npm | 1.0 (Oct 2025) | github.com/facebook/react | OK | Approved |
| react / react-dom | npm | mature | github.com/facebook/react | OK | Approved |
| typescript | npm | mature | github.com/microsoft/TypeScript | OK | Approved |
| eslint | npm | mature | github.com/eslint/eslint | OK | Approved |
| typescript-eslint | npm | mature | github.com/typescript-eslint/typescript-eslint | OK | Approved |
| eslint-plugin-react-hooks | npm | mature | github.com/facebook/react | OK | Approved |
| eslint-plugin-jsx-a11y | npm | mature | github.com/jsx-eslint/eslint-plugin-jsx-a11y | OK | Approved |
| eslint-config-prettier / prettier | npm | mature | github.com/prettier | OK | Approved |
| tailwindcss / @tailwindcss/vite | npm | mature | github.com/tailwindlabs/tailwindcss | OK | Approved |
| vite-plugin-pwa | npm | mature | github.com/vite-pwa/vite-plugin-pwa | OK | Approved |
| vitest / @vitest/coverage-v8 | npm | mature | github.com/vitest-dev/vitest | OK | Approved |
| @testing-library/react | npm | mature | github.com/testing-library | OK | Approved |
| jsdom | npm | mature | github.com/jsdom/jsdom | OK | Approved |
| @playwright/test | npm | mature | github.com/microsoft/playwright | OK | Approved |

**Packages removed due to slopcheck [SLOP] verdict:** none.
**Packages flagged as suspicious [SUS]:** `@rolldown/plugin-babel` (batch heuristic on age only; individually `[OK]`; official Rolldown org). Planner need not gate it, but a `checkpoint:human-verify` before first install is cheap insurance given it is the youngest dependency in the tree.
**`eslint-plugin-import-x` is `[ASSUMED]`** — not yet slopcheck-verified at the pinned version; the planner should run `slopcheck scan --pkg npm eslint-plugin-import-x` + `npm view eslint-plugin-import-x version` before adding it (it is the only non-version-verified package).

## Scaffold Blueprint

### Recommended `web/` directory tree (single package)
```
web/
├── package.json                # scripts: dev/build/lint/format/format:check/typecheck/test/test:e2e
├── package-lock.json           # committed — the reproducibility anchor (D-05/D-06)
├── tsconfig.json               # strict; references node + app configs
├── tsconfig.node.json          # for vite.config.ts / scripts (Node types)
├── vite.config.ts              # babel(reactCompiler) → react → tailwind → pwa  (order matters)
├── vitest.config.ts            # (or test block in vite.config) jsdom env + setup
├── eslint.config.js            # FLAT config (Correction #1 stack)
├── .prettierrc.json            # formatter config
├── playwright.config.ts        # Chromium-only; webServer boots ../ aura binary
├── index.html                  # pre-hydration <head> theme/density script + theme-color + manifest links
├── public/
│   ├── logo.png                # copied/derived from repo-root public/Logo.png (D-11)
│   ├── favicon.svg / favicon.ico
│   ├── apple-touch-icon.png
│   └── pwa-192.png / pwa-512.png / pwa-maskable-512.png   # generated from Logo.png
├── tokens/
│   ├── tokens.json             # hand-authored design tokens (D-08)
│   └── generate-theme.mjs      # tiny generator: tokens.json → src/styles/theme.css (@theme) + head snippet
├── src/
│   ├── main.tsx                # React 19 createRoot + <ErrorBoundary><AppShell/></ErrorBoundary>
│   ├── AppShell.tsx            # branded dark-operator placeholder shell (header w/ Logo, three-zone scaffold)
│   ├── ErrorBoundary.tsx       # D-13 root boundary, themed fallback
│   ├── theme/
│   │   ├── applyTheme.ts       # reads persisted theme/density; shared with the inline head script
│   │   └── density.ts          # compact|operator|review constants; default 'operator'
│   ├── styles/
│   │   ├── index.css           # @import "tailwindcss"; @import "./theme.css";
│   │   └── theme.css           # GENERATED from tokens.json (do not hand-edit)
│   └── __tests__/
│       └── AppShell.test.tsx   # Vitest + RTL: brand renders, no marketing hero text
├── e2e/
│   └── shell.spec.ts           # Playwright: boot aura serve → assert shell + theme-before-paint + no hero text
└── dist/                       # COMMITTED build output (D-05); go:embed target
    └── (index.html, assets/*.[hash].js|css, sw.js, manifest.webmanifest, icons)
```

### `package.json` scripts (the gate surface — names must match SC#3 `npm run …`)
```jsonc
{
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "node tokens/generate-theme.mjs && tsc -b && vite build",
    "preview": "vite preview",
    "lint": "eslint . --max-warnings=0",        // zero-warning (D-03)
    "format": "prettier --write .",
    "format:check": "prettier --check .",
    "typecheck": "tsc --noEmit",
    "test": "vitest run --coverage",
    "test:e2e": "playwright test"
  }
}
```
> `lint` uses `--max-warnings=0` so a single ESLint *warning* fails CI — the literal golangci-lint-parity bar D-03 requires. `build` runs the token generator first so `theme.css` is always fresh before `vite build`.

### `tsconfig.json` strictness (the `tsc --noEmit` gate teeth)
```jsonc
{
  "compilerOptions": {
    "target": "ES2022", "lib": ["ES2023", "DOM", "DOM.Iterable"],
    "module": "ESNext", "moduleResolution": "bundler",
    "jsx": "react-jsx", "useDefineForClassFields": true,
    "strict": true,
    "noUnusedLocals": true, "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true, "noUncheckedSideEffectImports": true,
    "noUncheckedIndexedAccess": true,            // operator-UI safety
    "exactOptionalPropertyTypes": true,
    "verbatimModuleSyntax": true, "isolatedModules": true,
    "skipLibCheck": true, "noEmit": true
  },
  "include": ["src", "e2e", "tokens", "*.config.ts"]
}
```

### `vite.config.ts` shape (plugin ORDER is load-bearing — Correction #2)
```ts
import { defineConfig } from 'vite';
import react, { reactCompilerPreset } from '@vitejs/plugin-react';
import { babel } from '@rolldown/plugin-babel';
import tailwindcss from '@tailwindcss/vite';
import { VitePWA } from 'vite-plugin-pwa';

export default defineConfig({
  plugins: [
    babel({ include: /\.[jt]sx?$/, babelConfig: reactCompilerPreset() }), // MUST be before react()
    react(),
    tailwindcss(),
    VitePWA({
      registerType: 'autoUpdate',          // D-10: skipWaiting + clientsClaim auto-set
      strategies: 'generateSW',            // Workbox content-hash revisioned precache + old-cache cleanup
      includeAssets: ['favicon.svg', 'apple-touch-icon.png'],
      manifest: {
        name: 'Aura', short_name: 'Aura',
        theme_color: '#0B0E14', background_color: '#0B0E14',
        display: 'standalone',
        icons: [
          { src: 'pwa-192.png', sizes: '192x192', type: 'image/png' },
          { src: 'pwa-512.png', sizes: '512x512', type: 'image/png' },
          { src: 'pwa-maskable-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
        ],
      },
    }),
  ],
  build: { outDir: 'dist', emptyOutDir: true /* reproducible: see Pitfall on emptyOutDir+committed dist */ },
});
```
> Verbatim plugin-order rule: `babel()` (hosting `reactCompilerPreset()`) must come **before** `react()`, else only Oxc's JSX transform runs and the Compiler never fires. `[CITED: dev.to/recca0120 — React Compiler 1.0 + Vite 8]`

### ESLint flat-config shape (`eslint.config.js`) — Correction #1 stack, prettier LAST
```js
import js from '@eslint/js';
import tseslint from 'typescript-eslint';
import reactHooks from 'eslint-plugin-react-hooks';
import reactRefresh from 'eslint-plugin-react-refresh';
import jsxA11y from 'eslint-plugin-jsx-a11y';
import prettier from 'eslint-config-prettier';

export default tseslint.config(
  { ignores: ['dist/**', 'coverage/**', 'playwright-report/**', 'src/styles/theme.css'] },
  js.configs.recommended,
  ...tseslint.configs.strictTypeChecked,        // Airbnb-intent: comprehensive + type-aware
  ...tseslint.configs.stylisticTypeChecked,
  {
    files: ['**/*.{ts,tsx}'],
    languageOptions: { parserOptions: { projectService: true, tsconfigRootDir: import.meta.dirname } },
    plugins: { 'react-hooks': reactHooks, 'react-refresh': reactRefresh, 'jsx-a11y': jsxA11y },
    rules: {
      ...reactHooks.configs['recommended-latest'].rules,  // Rules of Hooks + React COMPILER rules (Correction #3)
      ...jsxA11y.flatConfigs.recommended.rules,
      'react-refresh/only-export-components': 'warn',
    },
  },
  prettier,   // MUST be LAST — turns off rules Prettier owns (D-02 integration constraint)
);
```
> Add `eslint-plugin-import-x` flat config once version-verified (it ships a flat preset). Keep `prettier` the final element so it wins the override war.

### Prettier config (`.prettierrc.json`)
```json
{ "singleQuote": true, "semi": true, "trailingComma": "all", "printWidth": 100, "tabWidth": 2 }
```

## Go Embed Integration

### Embed package pattern
The single-binary invariant requires the SPA bytes be compiled in. Add a tiny Go package (e.g. `internal/webui/embed.go`) that owns the embed + a `Handler()`:
```go
package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var distFS embed.FS

// Sub returns the dist/ subtree rooted so index.html is served from "/".
func Sub() (fs.FS, error) { return fs.Sub(distFS, "dist") }

// Handler serves the embedded placeholder shell. Phase 23 scope: serve the static
// tree only. The SPA catch-all / index.html-fallback for client routes + the
// API/agent/health route exclusion is Phase 24 (WEB-01) — DO NOT add it here.
func Handler() (http.Handler, error) {
	sub, err := Sub()
	if err != nil { return nil, err }
	return http.FileServerFS(sub), nil  // Go 1.26 http.FileServerFS; auto MIME by extension
}
```
- **`all:dist`** (vs `dist`) embeds files whose names begin with `.` or `_` too — needed because Vite emits hashed assets and Workbox/`vite-plugin-pwa` can emit dotfiles; `all:` guarantees nothing is silently dropped. `[CITED: pkg.go.dev/embed]`
- **MIME:** `http.FileServerFS` sets `Content-Type` from the file extension via the stdlib mime table — `.js`/`.css`/`.webmanifest`/`.png` resolve correctly; no manual MIME wiring needed.
- **Mount point:** `aura serve` builds the AG-UI mux in `internal/agui` (`Server.Mux()`, Go 1.22 method-pattern `http.ServeMux`) and mounts it as `httpSrv.Handler` in `cmd/aura/serve.go`. Phase 23 mounts the embedded handler at `/` on the **same** loopback `http.Server` (or a sibling mux that delegates non-API paths to `webui.Handler()`). The AG-UI routes (`/agent/run`, `/healthz`, `/readyz`, `/metrics`, `/debug/vars`, `/threads/...`) must keep priority. **The real route-exclusion/SPA-fallback logic is Phase 24 (WEB-01)** — Phase 23 only needs the static placeholder to render at `/`.
- **Boundary:** the existing `scripts/agui_boundary_check.sh` enforces `internal/agent` must not import `internal/agui`. The new `internal/webui` package imports neither `agent` nor `agui` (it's a pure static handler) — keep it leaf-level so the boundary gate stays green.

### Committed `dist/` + `go build` with zero Node (D-05)
- `.gitignore` currently has a root `/dist/` ignore (line 10). Add a negation so `web/dist/` is tracked:
  ```gitignore
  /dist/
  !web/dist/
  ```
  (Verify the negation actually un-ignores — a parent-dir ignore can shadow a child negation; if `web/` itself is covered, use `web/dist/` explicit-add. Test with `git check-ignore -v web/dist/index.html`.)
- Because `dist/` is committed, `go build ./...` / `go install` and **all Go CI jobs** need no Node — the embed reads tracked bytes. This is the whole point of D-05.

### CI freshness gate (the stale-artifact guard, D-05/D-06)
A web CI job rebuilds `dist` and fails on drift:
```bash
cd web
npm ci
npm run build
git diff --exit-code -- dist/ \
  || { echo "web/dist is stale — run 'npm run build' in web/ and commit"; exit 1; }
```
- **Reproducibility caveat (see Pitfalls):** Vite/Rolldown output must be byte-stable across the committer's machine (WSL) and the CI/Docker Node-24 environment. Pin Node to 24 everywhere, commit `package-lock.json`, use `npm ci` (not `npm install`), and pin every tool version. Hashed asset filenames are content-derived, so identical inputs → identical names. If non-determinism appears (source-map paths, timestamps), disable sourcemaps for the committed build or normalize before diffing.

## Theme / Token Architecture

### `tokens.json` schema (hand-authored, D-08)
A flat, namespaced JSON — no DTCG, no Style Dictionary. The tiny generator maps it to a Tailwind 4 `@theme` block + per-`data-theme`/`data-density` override blocks.
```jsonc
{
  "$meta": { "defaultTheme": "dark", "defaultDensity": "operator" },
  "color": {
    "dark": {                          // the v1 dark-operator theme (only theme shipped in P23)
      "bg":         "#0B0E14",         // app background (near-black, slight blue)
      "surface":    "#121620",         // panels / cards
      "surface-2":  "#1A1F2B",         // raised surface / hover
      "border":     "#2A3140",         // hairline separators
      "text":       "#E6E9EF",         // primary text
      "text-muted": "#9AA4B2",         // secondary text
      "text-faint": "#5B6675",         // tertiary / disabled
      "accent":     "#5BA8FF",         // primary action / focus (industrial blue, not neon)
      "accent-dim": "#2E5C8A",
      "success":    "#3FB37F",
      "warning":    "#E0A23C",
      "danger":     "#E5484D",
      "info":       "#5BA8FF"
    }
  },
  "density": {                          // spacing/typography scalars per tier; default operator
    "compact":  { "space-unit": "3px", "row-h": "28px", "font-base": "12px" },
    "operator": { "space-unit": "4px", "row-h": "32px", "font-base": "13px" },
    "review":   { "space-unit": "6px", "row-h": "40px", "font-base": "15px" }
  },
  "radius": { "sm": "4px", "md": "6px", "lg": "10px" },
  "font": { "sans": "Inter, ui-sans-serif, system-ui, sans-serif", "mono": "ui-monospace, SFMono-Regular, monospace" }
}
```
> The palette is a **designed proposal** consistent with ux-spec §147 (dark operator cockpit, industrial / less-decorative, trust shown via structure not an abstract sphere) and §350 (no decoration). Confidence MEDIUM — it is intentionally restrained (no neon, no gradient) and is meant to be the committed v1; the planner/operator can adjust hex during scaffold without changing the architecture. Logged as an assumption (A-PALETTE).

### Generator output → Tailwind 4 `@theme` (`src/styles/theme.css`, generated)
```css
/* GENERATED from tokens/tokens.json — do not edit by hand */
@theme {
  --color-bg: #0B0E14; --color-surface: #121620; --color-surface-2: #1A1F2B;
  --color-border: #2A3140; --color-text: #E6E9EF; --color-text-muted: #9AA4B2;
  --color-accent: #5BA8FF; --color-success: #3FB37F; --color-warning: #E0A23C; --color-danger: #E5484D;
  --radius-md: 6px; --font-sans: Inter, ui-sans-serif, system-ui, sans-serif;
}
/* density scalars applied via data-density on <html> */
:root[data-density="compact"]  { --space-unit: 3px; --row-h: 28px; --font-base: 12px; }
:root[data-density="operator"] { --space-unit: 4px; --row-h: 32px; --font-base: 13px; }
:root[data-density="review"]   { --space-unit: 6px; --row-h: 40px; --font-base: 15px; }
```
Tailwind 4's `@theme` makes these CSS vars into utilities (`bg-bg`, `text-text-muted`, `border-border`). `[CITED: tailwindcss.com/docs/theme]` A future light theme would add `:root[data-theme="light"] { --color-bg: … }` overrides — out of P23 scope (only `dark` ships).

### Apply-before-paint inline `<head>` script (no flash — D-07/D-08)
In `index.html`, a tiny synchronous script runs **before** the bundle and before first paint, reading persisted prefs and setting the root attributes:
```html
<head>
  <meta name="theme-color" content="#0B0E14" />
  <link rel="icon" href="/favicon.svg" />
  <link rel="apple-touch-icon" href="/apple-touch-icon.png" />
  <script>
    (function () {
      try {
        var t = localStorage.getItem('aura.theme') || 'dark';
        var d = localStorage.getItem('aura.density') || 'operator';   // default operator (D-07)
        var r = document.documentElement;
        r.setAttribute('data-theme', t);
        r.setAttribute('data-density', d);
      } catch (e) { document.documentElement.setAttribute('data-theme','dark'); document.documentElement.setAttribute('data-density','operator'); }
    })();
  </script>
</head>
```
- It is **inline and synchronous** (not a module, not deferred) so it executes before the browser paints the body — the only reliable no-flash mechanism. `src/theme/applyTheme.ts` shares the same key names so React reads/writes the same source of truth after mount.
- The generator can also emit this snippet so the key names stay in sync with `tokens.json`.

## PWA Specifics

- **`registerType: 'autoUpdate'` + `strategies: 'generateSW'`** (Workbox `generateSW`). On `autoUpdate`, vite-plugin-pwa auto-sets `skipWaiting` + `clientsClaim`. `[CITED: vite-pwa-org.netlify.app/guide/auto-update]`
- **Stale-asset-across-releases (D-10, the one non-trivial bit):** Workbox builds a precache **manifest** where each entry carries a `revision` = content hash of the asset. A new single-binary release rebuilds `dist` → changed assets get new hashes → new SW → on activation Workbox **cleans up caches from previous SW versions**. `[CITED: vite-pwa-org.netlify.app/guide/service-worker-precache]` So a new Aura binary never serves the previous release's stale assets — exactly D-10's requirement. No manual versioning needed; the content hash *is* the version. Verify in the E2E/release flow (Validation SC2/SC5).
- **Icons from `Logo.png` (D-11):** generate `pwa-192.png`, `pwa-512.png`, `pwa-maskable-512.png`, `apple-touch-icon.png`, and `favicon` from the repo-root `public/Logo.png` at scaffold time (a one-off `sharp`/`@vite-pwa/assets-generator` step, or pre-generated committed assets — the latter avoids adding a build-time image dep, leaner per D-08).
- **`theme-color`** meta + manifest `theme_color`/`background_color` = `#0B0E14` (matches the dark-operator bg). FND-05.

## CI + Docker

### Path-filtered web jobs in the EXISTING `ci.yml` (D-17)
`ci.yml` triggers on push/PR to `master`/`main`/`tabula-rasa` (no top-level `paths:` today). Add `web/`-scoped jobs that early-exit when no web files changed, using the **`dorny/paths-filter`** action (or a `git diff --name-only` guard step) so Go-only PRs skip them — mirroring the single golangci-lint discipline, one workflow.

Add four web steps/jobs (all `runs-on: ubuntu-latest`, `actions/setup-node@v4` with `node-version: 24`, `cache: npm`):

1. **`web-lint`** — `cd web && npm ci && npm run lint && npm run format:check && npm run typecheck` (the D-03 zero-warning gate; `--max-warnings=0` already in the `lint` script).
2. **`web-test`** — `cd web && npm ci && npm run test` (Vitest + coverage, FND-06).
3. **`web-dist-freshness`** — `cd web && npm ci && npm run build && git diff --exit-code -- dist/` (D-05 stale guard; pins the committed dist to a fresh Node-24 build).
4. **`web-e2e`** — build dist → `go build -o aura ./cmd/aura` (binary with embedded dist) → `cd web && npm ci && npx playwright install --with-deps chromium && npm run test:e2e` (Playwright boots `aura serve` on loopback; D-16, blocking, every PR).

> Path-filter pattern (guard step), since `ci.yml` has no global `paths:`:
> ```yaml
> - uses: dorny/paths-filter@v3
>   id: changes
>   with: { filters: "web:\n  - 'web/**'\n  - '.github/workflows/ci.yml'" }
> # subsequent web steps: if: steps.changes.outputs.web == 'true'
> ```
> A Go-only change leaves `web == 'false'` → web jobs no-op green. (When `dist/` changes because Go-only work touched it — it won't, dist is web-only — the filter still holds.)

### Playwright config (`playwright.config.ts`) — Chromium-only, boots the binary
```ts
import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? 'github' : 'list',
  use: { baseURL: 'http://127.0.0.1:9080', trace: 'on-first-retry' },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: '../aura serve --only=cli',         // the embedded binary; loopback bind is the default
    url: 'http://127.0.0.1:9080/healthz',         // wait for the AG-UI health route to come up
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
    env: { /* CI does NOT inherit env by default — pass what `aura serve` needs explicitly */ },
  },
});
```
- **Chromium-only** for P23: the placeholder shell smoke needs one engine; cross-browser breadth is unnecessary cost for a single static screen (lean-scaffold). Playwright's strong CI story (D-15) is in the `webServer` lifecycle + `github` reporter, not in browser count.
- **CI env caveat (verified landmine):** Playwright's `webServer` does **not** inherit the runner's env by default in GitHub Actions `[CITED: github.com/microsoft/playwright#19780]` — pass any vars `aura serve` requires (DB/Neo4j are NOT required for the static shell on loopback; confirm `aura serve --only=cli` boots the HTTP server without a full stack — see Open Questions).
- `reuseExistingServer: !process.env.CI` → fresh boot in CI, reuse a running daemon locally.

### Node-24 multi-stage Docker (D-06)
The existing `docker/aura/Dockerfile` is a 2-stage build (golang-alpine → debian-slim runtime) and **already installs Node 24** in the runtime image (lines 28–30) — but that Node is for **recipe MCP servers** (mail-mcp), NOT for building the SPA. D-06 wants the SPA built in a **build stage** with Node 24 and **no Node added for the SPA's sake** in runtime. Approach:
```dockerfile
# --- web build stage (Node 24, builds dist, never reaches runtime) ---
FROM node:24-bookworm-slim AS webbuild
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build        # produces /web/dist

# --- go build stage (existing) ---
FROM golang:1.26.4-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=webbuild /web/dist ./web/dist     # overwrite committed dist with a fresh reproducible build
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/aura ./cmd/aura

# --- runtime (existing debian-slim; Node 24 stays ONLY for recipe MCP servers) ---
# ...unchanged...
```
- The `webbuild` stage's Node never lands in the runtime image (multi-stage). The runtime's existing Node-24 (for mail-mcp) is orthogonal to D-06's "no Node for the SPA" intent — the SPA is fully baked into the Go binary before runtime. Document this distinction so a reviewer doesn't think runtime-Node violates D-06.
- `COPY --from=webbuild /web/dist ./web/dist` makes the image's embedded SPA a **fresh reproducible build** (not the committed bytes) — and the `web-dist-freshness` CI job proves the committed bytes equal a fresh build, tying D-05↔D-06.

## Validation Architecture

> Nyquist validation is ENABLED (`config.json workflow.nyquist_validation: true`). This section maps every Phase-23 ROADMAP success criterion to concrete validating tests.

### Test Framework
| Property | Value |
|----------|-------|
| Unit/component framework | Vitest 4.1.9 + @testing-library/react 16 + jsdom (env: jsdom) |
| E2E framework | @playwright/test 1.61 (Chromium-only), `webServer` boots `../aura serve` |
| Go embed test | Go stdlib `testing` + `net/http/httptest` against `webui.Handler()` |
| Config files | `web/vitest.config.ts`, `web/playwright.config.ts`, `web/eslint.config.js` (all Wave 0) |
| Quick run command | `cd web && npm run lint && npm run typecheck && npm run test` |
| Full suite command | `cd web && npm ci && npm run build && git diff --exit-code -- dist/ && npm run test && npm run test:e2e` |
| Phase gate | Go `make quality` green (embed package) + all 4 web CI jobs green |

### Phase Requirements → Test Map (per ROADMAP SC)
| SC | Behavior to prove TRUE | Test type | Automated command | File Exists? |
|----|------------------------|-----------|-------------------|-------------|
| SC1 | Decision record exists + approved (this RESEARCH.md) + locks linter/formatter/tokens/layout/pipeline/harness | doc gate | reviewer sign-off on `23-RESEARCH.md` (no automated test — it is the artifact) | ✅ this file |
| SC2 | `aura serve` serves the branded shell embedded via `//go:embed all:dist`; theme+density applied **before paint** (no flash); same dist builds reproducibly | E2E + Go embed test + freshness | `npm run test:e2e` (asserts `data-theme`/`data-density` on `<html>` are set on first response HTML, brand visible) + `go test ./internal/webui/` (handler returns embedded index.html, 200, `text/html`) + `git diff --exit-code -- web/dist/` | ❌ Wave 0: `e2e/shell.spec.ts`, `internal/webui/embed_test.go` |
| SC3 | `npm run lint` + `format:check` + `tsc --noEmit` zero-warning, blocking CI | lint/format/type gate | `cd web && npm run lint && npm run format:check && npm run typecheck` (each exits non-zero on any issue; `--max-warnings=0`) | ❌ Wave 0: `eslint.config.js`, `.prettierrc.json`, `tsconfig.json` |
| SC4 | `Logo.png` in header + matching favicon + PWA/theme-color metadata; **no marketing hero text** in primary viewport | component + E2E | `npm run test` (RTL: `getByRole('img', { name: /aura/i })` present; assert NO text matching the marketing-hero blocklist) + `npm run test:e2e` (favicon link + `<meta name=theme-color>` present; copy-contract assertion) | ❌ Wave 0: `src/__tests__/AppShell.test.tsx`, `e2e/shell.spec.ts` |
| SC5 | Node-24 multi-stage Docker builds embedded dist, **no Node-for-SPA in runtime**; Vitest + E2E harness green in CI | docker build + CI | `docker build -f docker/aura/Dockerfile .` (webbuild stage produces dist; runtime image runs `aura serve` with embedded shell — smoke `curl 127.0.0.1:9080/` returns the shell HTML) + all 4 web CI jobs green | ❌ Wave 0: Dockerfile `webbuild` stage, 4 web jobs in `ci.yml` |

### Sampling Rate
- **Per task commit:** `cd web && npm run lint && npm run typecheck && npm run test` (sub-30s) + `go test ./internal/webui/`.
- **Per wave merge:** full suite (`npm ci && npm run build && git diff --exit-code -- dist/ && npm run test && npm run test:e2e`) + `make quality`.
- **Phase gate:** all 4 web CI jobs + the Go gates green on the PR before `/gsd-verify-work`; Docker build smoke passes.

### Wave 0 Gaps (must exist before feature/scaffold tasks assert against them)
- [ ] `web/eslint.config.js`, `web/.prettierrc.json`, `web/tsconfig.json`(+`tsconfig.node.json`) — the gate configs
- [ ] `web/vitest.config.ts` + `web/src/__tests__/AppShell.test.tsx` — covers SC4 (brand + no-hero-text) and basic shell render
- [ ] `web/playwright.config.ts` + `web/e2e/shell.spec.ts` — covers SC2/SC4 (theme-before-paint, brand, copy-contract) by booting the binary
- [ ] `internal/webui/embed.go` + `internal/webui/embed_test.go` — covers SC2 embed→serve→render (httptest)
- [ ] Framework install: the `npm install -D …` block above (Wave 0 scaffolding task)
- [ ] 4 web jobs added to `.github/workflows/ci.yml` + `webbuild` stage in `docker/aura/Dockerfile`
- [ ] `.gitignore` negation for `web/dist/`

## Common Pitfalls

### Pitfall 1: Plugin order silently disables the React Compiler
**What goes wrong:** `react()` placed before `babel(reactCompilerPreset())` → only Oxc's JSX transform runs; the Compiler never fires, no error, no memoization. **Why:** Vite runs transforms in plugin order; the compiler must see the source first. **Avoid:** `babel()` strictly before `react()`; add a Vitest/E2E smoke that asserts a known-memoizable component is compiled (or check the build for the `react-compiler` runtime import). **Warning signs:** no perf change; `react/compiler` ESLint rules pass but build output lacks compiler instrumentation. `[CITED: dev.to/recca0120]`

### Pitfall 2: Reaching for the dead `eslint-config-airbnb-typescript`
**What goes wrong:** the plan installs `eslint-config-airbnb-typescript` (archived) or `@kesills/...` (ESLint ≤8) → flat-config/ESLint-10 breakage, or a `FlatCompat` eslintrc shim that's fragile and slow. **Avoid:** use the Correction #1 flat-config stack (typescript-eslint strict + react-hooks + jsx-a11y). **Warning signs:** `FlatCompat`/`@eslint/eslintrc` imports in `eslint.config.js`; peer-dep warnings pinning `eslint@^8`.

### Pitfall 3: Committed `dist/` non-determinism breaks the freshness gate
**What goes wrong:** the committer's WSL build and CI/Docker Node-24 build differ (sourcemap absolute paths, timestamps, Node minor) → `git diff` non-empty on an unchanged source. **Avoid:** pin Node 24 + `package-lock.json` everywhere, `npm ci` not `npm install`, disable sourcemaps for the committed build (or normalize), keep all tool versions pinned. **Warning signs:** freshness gate fails with only whitespace/path/hash-of-hash diffs.

### Pitfall 4: Theme flash because the apply script is deferred/module
**What goes wrong:** the theme-apply runs as a `<script type="module">` or after the bundle → first paint uses default theme, then snaps → flash (violates SC2 "before paint"). **Avoid:** inline **synchronous** `<script>` in `<head>` before any module/bundle. **Warning signs:** Playwright sees `data-theme` unset on the very first DOM, or a visible color flip in trace video.

### Pitfall 5: Stale PWA assets after a new binary release
**What goes wrong:** a returning operator gets last release's cached JS because the SW didn't update. **Avoid:** `registerType: 'autoUpdate'` + `generateSW` (content-hash revisioned precache + auto old-cache cleanup); verify in E2E that a rebuilt dist yields a new precache revision. **Warning signs:** `manifest.webmanifest`/`sw.js` unchanged across a real source change; operators report "old UI after update". `[CITED: vite-pwa-org.netlify.app]`

### Pitfall 6: Playwright `webServer` fails in CI on missing env
**What goes wrong:** `aura serve` boots fine locally but exits/early-fails in CI because `webServer` doesn't inherit env. **Avoid:** pass required vars explicitly in `webServer.env`; confirm `aura serve --only=cli` boots the HTTP layer without a DB/Neo4j stack (see Open Questions). **Warning signs:** "Process from config.webServer was not able to start" / exit 127 / health URL never ready. `[CITED: github.com/microsoft/playwright#19780]`

### Pitfall 7: `embed` drops dotfiles / SW
**What goes wrong:** `//go:embed dist` (without `all:`) silently omits files starting with `.`/`_`, potentially the SW or hashed chunks → broken shell at runtime that built fine. **Avoid:** `//go:embed all:dist`. **Warning signs:** 404s for assets present in `dist/` on disk; SW not found. `[CITED: pkg.go.dev/embed]`

### Pitfall 8: `.gitignore /dist/` shadows `web/dist/`
**What goes wrong:** root `/dist/` ignore (or a broad `dist/`) prevents `web/dist/` from being tracked even with a negation, so the embed has stale/no bytes and CI builds a binary with an old shell. **Avoid:** explicit `!web/dist/` negation and verify with `git check-ignore -v web/dist/index.html`; force-add once if needed. **Warning signs:** `git status` doesn't show `web/dist/` after a build; the freshness gate passes locally but the binary serves an old shell.

### Pitfall 9 (host): Windows/WSL build-host line endings + path drift
**What goes wrong:** the repo's primary dev env is WSL on a Windows host; CRLF normalization or path separators leak into committed `dist`/configs → cross-host diff noise. **Avoid:** `.gitattributes` enforcing LF for `web/**`; build dist in WSL or Docker (not Windows-native Node) to match CI. **Warning signs:** freshness gate fails with pure EOL diffs; `prettier --check` disagrees across hosts.

### Anti-Patterns to Avoid
- **Adding Style Dictionary / DTCG tooling** — D-08 explicitly rejects it; the tiny generator is the minimal-industrial shape.
- **Adding a router, assistant-ui, or any feature dep** — D-09/D-14; lean placeholder shell only.
- **Adding marketing hero text / decorative badges** — ux-spec §350 Copy Contract is law (SC4 asserts its absence).
- **A separate `web.yml` workflow** — D-17 mandates the existing `ci.yml`.
- **SPA index.html-fallback / API route exclusion in Phase 23** — that's Phase 24 (WEB-01); P23 serves the static placeholder only.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Asset bundling/hashing/minify | Custom esbuild/rollup script | Vite 8 (Rolldown) | Content-hash naming, code-split, plugin ecosystem |
| Auto-memoization | Manual `useMemo`/`useCallback` everywhere | React Compiler 1.0 | Compiler does it correctly + provably (D-12) |
| Design-token → CSS pipeline (heavy) | Style Dictionary | Tiny `tokens.json` generator + Tailwind 4 `@theme` | D-08 minimal-industrial; Style Dictionary is over-tooling here |
| Service worker / precache | Hand-written SW | vite-plugin-pwa (Workbox `generateSW`) | Revisioned precache + cleanup solves D-10's stale-asset problem for free |
| Static file serving in Go | Custom byte-map handler | `embed.FS` + `http.FileServerFS` | Stdlib MIME + range + caching; `all:` embeds everything |
| ESLint Airbnb rules on flat config | Port Airbnb's eslintrc by hand | typescript-eslint strict + react-hooks + jsx-a11y | The maintained 2026 equivalent; the original is archived |
| E2E server lifecycle | Background `&` + sleep + curl | Playwright `webServer` | Health-gated boot, auto-teardown, CI-safe |

**Key insight:** Every "build it ourselves" temptation in this domain has a battle-tested, content-hashing, edge-case-handling library; the only bespoke code Phase 23 should write is the ~50-line `tokens.json` generator (D-08) and the ~30-line Go embed handler.

## State of the Art

| Old Approach | Current Approach (2026) | When Changed | Impact |
|--------------|-------------------------|--------------|--------|
| Vite + esbuild/Rollup split | Vite 8 + Rolldown (one Rust bundler) | Vite 8, Mar 2026 | 10–30× faster builds; one bundler |
| `react({ babel: {...} })` for compiler | `@rolldown/plugin-babel` + `reactCompilerPreset()` | @vitejs/plugin-react v6 (w/ Vite 8) | Babel dropped for Oxc; explicit opt-in for Babel plugins |
| `eslint-plugin-react-compiler` | `eslint-plugin-react-hooks@7` `recommended-latest` | 2025 | Standalone plugin deprecated/merged |
| `eslint-config-airbnb-typescript` (eslintrc) | typescript-eslint strict flat configs | Airbnb archived May 2025; ESLint 10 flat-only | The literal package is a dead end |
| `tailwind.config.js` (JS config) | CSS-first `@theme` directive | Tailwind v4 | Tokens live in CSS; `data-*` variants for theme/density |
| ESLint `.eslintrc` | flat `eslint.config.js` only | ESLint 10 | eslintrc removed; FlatCompat is a migration crutch |

**Deprecated/outdated:**
- `eslint-config-airbnb-typescript`, `@kesills/...` (ESLint ≤8) — do not use on this stack.
- `eslint-plugin-react-compiler` — deprecated, use `eslint-plugin-react-hooks@7`.
- `@vitejs/plugin-react-swc` as a "choice" — moot; v6's Oxc transform is already Rust (no Babel-vs-swc decision remains).
- `react({ babel })` config path — removed in plugin-react v6.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A-PALETTE | The proposed v1 dark-operator hex palette (`#0B0E14` bg, `#5BA8FF` accent, etc.) is a designed proposal, not verified against a Figma export | Theme/Token Architecture | LOW — architecture is unaffected; hex values are trivially adjustable during scaffold; operator can refine to match the elysia-informed board |
| A-IMPORTX | `eslint-plugin-import-x` is the right flat-native import plugin (version not pinned/slopchecked here) | ESLint Flat-Config Shape | LOW — verify version + slopcheck at scaffold; import-order lint is non-critical to the gate; can drop if it causes friction |
| A-SERVE-NOSTACK | `aura serve --only=cli` boots the HTTP/AG-UI server on loopback **without** a live DB/Neo4j stack (needed for Playwright `webServer` in CI) | CI + Validation | MEDIUM — if `serve` hard-requires a stack, the E2E job must provision one or use a dedicated minimal serve flag; planner must confirm against `cmd/aura/serve.go` boot path |
| A-DIST-REPRO | Vite/Rolldown output is byte-reproducible across WSL + CI/Docker Node-24 with a pinned lockfile | Go Embed Integration / Pitfall 3 | MEDIUM — if not, the freshness gate flakes; mitigations listed (no sourcemaps, pin Node, normalize) |
| A-EMBED-MOUNT | The embedded static handler can mount at `/` on the existing `aura serve` http.Server without disturbing AG-UI routes (full route-exclusion is Phase 24) | Go Embed Integration | LOW — placeholder serving at `/` is additive; Phase 24 owns the real precedence/fallback logic |

## Open Questions

1. **Does `aura serve` boot the HTTP server without a DB/Neo4j stack? (A-SERVE-NOSTACK)**
   - What we know: `serve.go` builds on `bootChatEnv` (the shared composition root) and mounts the AG-UI mux on loopback; `--only=cli` and `--no-telegram` flags exist; `/healthz` is a cheap liveness check, `/readyz` reflects required backends.
   - What's unclear: whether the boot path hard-fails without Postgres/Neo4j reachable (the Playwright E2E + the `web-e2e` CI job need a stack-free boot).
   - Recommendation: planner reads the `bootChatEnv`/`runServe` path; if a stack is required, either (a) add/confirm a minimal serve mode that mounts only the static handler + `/healthz`, or (b) provision a lightweight stack in the `web-e2e` job. Prefer (a) for a true single-binary smoke.

2. **Exact mount wiring of `webui.Handler()` into `serve.go`.**
   - What we know: the AG-UI `Server.Mux()` owns specific routes; the embed should serve everything else (`/`, `/assets/*`, `/manifest.webmanifest`, `/sw.js`).
   - What's unclear: whether to wrap the AG-UI mux + static handler in a parent mux now, or extend `Server.Mux()`. Phase 24 will refactor this anyway (WEB-01 route exclusion).
   - Recommendation: do the minimal additive mount in Phase 23 (parent mux delegating non-AG-UI paths to the embed); leave the real SPA-fallback to Phase 24.

3. **Pre-generate PWA/brand icons vs. add a build-time generator?**
   - Recommendation: pre-generate the icon set from `Logo.png` once and commit them (avoids a build-time `sharp` dependency; leaner per D-08). Document the generation command in `web/README.md` for reproducibility.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Node.js 24 | Build dist (CI + Docker webbuild stage) | ✗ (not on this Git Bash PATH) | — | Install in CI (`setup-node@v4`) + Docker (`node:24-bookworm-slim`); operator has Node in WSL (existing recipe builds use it) |
| npm | dist build + lint/test | ✗ (not on this Git Bash PATH) | — | bundled with Node 24 |
| Go 1.26.4 | embed + `aura serve` build | ✓ (per go.mod, WSL primary) | 1.26.4 | — |
| Docker | multi-stage image (D-06) + integration CI | ✓ (Windows Docker Desktop, reached from WSL via 127.0.0.1) | per host | — |
| Playwright Chromium | E2E | ✗ (installed on demand) | 1.61 | `npx playwright install --with-deps chromium` in CI |

**Missing dependencies with no fallback:** none — Node 24 is the only build-time need and it is provisioned in CI/Docker (and already present in WSL for recipe builds). Go contributors never need Node (D-05 committed dist).
**Missing dependencies with fallback:** Node/npm/Playwright are installed by the CI/Docker steps; no runtime image gains Node-for-SPA (D-06).

## Project Constraints (from CLAUDE.md)

| Directive | How Phase 23 honors it |
|-----------|------------------------|
| Single Go binary (frontend embedded, never separately deployed) | `//go:embed all:dist`; `aura serve` serves the shell; no separate web deploy |
| Minimal-industrial-shape (reject over-tooling) | Tiny `tokens.json` generator not Style Dictionary; single `web/` package not a monorepo; Chromium-only E2E; generateSW not hand-written SW |
| Quality gate must feel like `golangci-lint` (zero warnings, blocking, every PR) | `eslint --max-warnings=0` + `format:check` + `tsc --noEmit` + Vitest + Playwright, all blocking in `ci.yml` (D-03/D-16/D-17) |
| NEVER MODIFY TESTS TO PASS | Wave-0 tests assert SCs; fix code/scaffold, not the assertions |
| NO GOD CLASS (>600 LOC) | `scripts/check-file-size.sh` is Go-only today; the `internal/webui` embed package is ~30 LOC; keep TS files small by convention (no enforced TS cap in P23) |
| Master-direct commit discipline | D-18; commit `web/` directly on master, no feature branches/changesets |
| `commit_docs: true` | This RESEARCH.md will be committed |
| Coverage ≥85% (Go floor) | Applies to `internal/webui` (small surface — embed handler test covers it); TS coverage gate via Vitest `--coverage` (threshold set in vitest config; not bound by the Go 85% floor but should be meaningful) |
| Env naming `AURA_<DOMAIN>_<UNIT>` | Any new serve/web env (e.g. a future web bind) follows the convention; P23 adds none (loopback default) |

## Sources

### Primary (HIGH confidence)
- npm registry (`npm view <pkg> version`, 2026-06-16) — all Standard Stack versions verified
- slopcheck 0.6.1 (`scan --pkg npm`, `install -e npm`) — package legitimacy audit
- `vite.dev/blog/announcing-vite8` — Vite 8 stable (Mar 2026), Rolldown, Node 20.19+/22.12+ requirement
- `github.com/vitejs/vite-plugin-react` releases `plugin-react@6.0.0` + CHANGELOG — Babel→Oxc; `@rolldown/plugin-babel` migration path
- `react.dev/learn/react-compiler/installation` + `react.dev/reference/eslint-plugin-react-hooks` — Compiler install on Vite; react-compiler ESLint merged into react-hooks
- `tailwindcss.com/docs/theme` — `@theme` directive + CSS-var theming + `data-*` variants
- `vite-pwa-org.netlify.app/guide/auto-update` + `/guide/service-worker-precache` — autoUpdate, content-hash revisioned precache, old-cache cleanup
- `pkg.go.dev/embed` — `//go:embed all:` semantics
- Local codebase: `cmd/aura/serve.go`, `internal/agui/server.go`, `docker/aura/Dockerfile`, `.github/workflows/ci.yml`, `scripts/check-file-size.sh`, `scripts/agui_boundary_check.sh`, `docs/design/aura-deep-search-figma/ux-spec.md`

### Secondary (MEDIUM confidence — verified against an official source)
- `dev.to/recca0120` + `recca0120.github.io` (React Compiler 1.0 + Vite 8) — cross-checked against react.dev + the plugin-react v6 changelog
- `github.com/iamturns/eslint-config-airbnb-typescript#331` + `github.com/Kenneth-Sills/eslint-config-airbnb-typescript` README — airbnb archival + ESLint-9 incompatibility
- `github.com/microsoft/playwright#19780` — `webServer` env-inheritance caveat in GitHub Actions

### Tertiary (LOW confidence — flagged for scaffold-time verification)
- `eslint-plugin-import-x` recommendation (A-IMPORTX) — verify version + slopcheck at scaffold
- The v1 palette hex values (A-PALETTE) — designed proposal, refine against the Figma board

## Metadata

**Confidence breakdown:**
- Standard stack (versions/APIs): HIGH — every version verified against npm registry; the three corrections verified against official changelogs/docs
- Architecture (embed, theme, PWA, CI/Docker): HIGH — patterns confirmed against official docs + the existing codebase
- Palette: MEDIUM — designed proposal consistent with ux-spec §147/§350, not a Figma export
- `aura serve` stack-free boot: MEDIUM — needs confirmation in `serve.go` boot path (Open Question 1)

**Research date:** 2026-06-16
**Valid until:** ~2026-07-16 for the fast-moving JS toolchain (Vite/React-Compiler/ESLint cadence is weeks) — re-verify `npm view` versions at scaffold time.
