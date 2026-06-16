---
phase: 23-frontend-infrastructure-industrial-foundation
plan: 02
subsystem: ui
tags: [vite, react19, tailwind4, react-compiler, vite-plugin-pwa, design-tokens, go-embed, pwa, frontend]

# Dependency graph
requires:
  - phase: 23-01 (FND-01/02/03)
    provides: pinned Vite8/React19/TS6/Tailwind4 toolchain + committed lockfile, zero-warning eslint/prettier/tsc gate, internal/webui //go:embed all:dist host (placeholder dist), Wave-0 RED Vitest/Playwright stubs
provides:
  - web/ React app: dark-operator token theme (tokens.json + tiny generator + Tailwind 4 @theme + apply-before-paint inline head script, default density operator)
  - branded placeholder AppShell (Logo header, three-zone scaffold, zero marketing copy) + D-13 ErrorBoundary + React 19 main.tsx
  - React-Compiler-enabled Vite config (babel-before-react plugin order, verified useMemoCache in the bundle) + VitePWA autoUpdate/generateSW installable PWA
  - brand/PWA icon set committed under web/public (pre-generated from public/Logo.png, no build-time sharp dep)
  - a real reproducible build committed at internal/webui/dist/ (overwrites the 23-01 placeholder) — embed test green against the real tree
affects: [23-03-serve-ci-docker, 24-web-foundation, 25-chat-approval]

# Tech tracking
tech-stack:
  added: []
  patterns: [tiny-tokens-json-generator-not-style-dictionary, apply-before-paint-inline-head-script, react-compiler-via-rolldown-plugin-babel-default-import, vite-outdir-to-internal-webui-dist, pre-generated-committed-pwa-icons, tailwind4-token-utilities]

key-files:
  created:
    - web/tokens/tokens.json
    - web/tokens/generate-theme.mjs
    - web/src/styles/theme.css
    - web/src/styles/index.css
    - web/src/styles/head-snippet.generated.html
    - web/src/theme/density.ts
    - web/src/theme/applyTheme.ts
    - web/src/AppShell.tsx
    - web/src/ErrorBoundary.tsx
    - web/src/main.tsx
    - web/src/vite-env.d.ts
    - web/index.html
    - web/vite.config.ts
    - web/README.md
    - web/.prettierignore
    - web/public/logo.png
    - web/public/favicon.svg
    - web/public/apple-touch-icon.png
    - web/public/pwa-192.png
    - web/public/pwa-512.png
    - web/public/pwa-maskable-512.png
    - internal/webui/dist/* (real Vite bundle: index.html, assets/*.[hash].js|css, sw.js, workbox-*.js, registerSW.js, manifest.webmanifest, icons)
  modified:
    - web/eslint.config.js
    - web/tsconfig.json
    - internal/webui/dist/index.html (placeholder -> real bundled shell)

key-decisions:
  - "@rolldown/plugin-babel exports the plugin as DEFAULT and takes { presets } — the RESEARCH's named `babel` + `babelConfig` shape predates the shipped 0.2.3 API; corrected (the only way the React Compiler pass loads)"
  - "Vite build.outDir = ../internal/webui/dist (NOT web/dist) — Go //go:embed is package-relative, per 23-01 Deviation #1; web/dist never created"
  - "PWA/brand icons pre-generated once from public/Logo.png via a throwaway sharp install — no build-time sharp dependency added to web/ (D-08 lean)"
  - "Generated theme.css + head-snippet are .prettierignored; theme.css already eslint-ignored — the generator owns their formatting"
  - "Re-included AppShell.test.tsx in the eslint + tsconfig gate (23-01 Dev #5 reversed) now the component exists; relaxed no-unnecessary-condition for test files only (keeps the 23-01 assertions byte-identical)"

patterns-established:
  - "Token pipeline: hand-authored tokens.json -> tiny ~58-LOC generate-theme.mjs -> Tailwind 4 @theme block + :root[data-density] override blocks + synced inline head snippet (no Style Dictionary)"
  - "Apply-before-paint: inline SYNCHRONOUS head script in index.html sets data-theme/data-density before the module bundle (no flash); applyTheme.ts shares aura.theme/aura.density keys"
  - "React Compiler hosting: import babel default from @rolldown/plugin-babel; babel({ presets:[reactCompilerPreset()] }) ordered BEFORE react() — bundle proof = react/compiler-runtime useMemoCache"
  - "Committed embed bundle co-located at internal/webui/dist; sourcemap:false for byte-stability; tracked via the !internal/webui/dist/ negation"

requirements-completed: [FND-02, FND-04, FND-05]

# Metrics
duration: 14min
completed: 2026-06-16
---

# Phase 23 Plan 02: Frontend Scaffold — Dark-Operator Theme, Branded Shell, React-Compiler PWA Build Summary

**The greenfield `web/` React app: a dark-operator token theme applied before paint (tokens.json → tiny generator → Tailwind 4 @theme + inline head script, default density operator), a branded zero-marketing AppShell + error boundary, a React-Compiler-enabled Vite/PWA config, and a real reproducible build committed at `internal/webui/dist/` that turns the 23-01 Vitest component test GREEN.**

## Performance

- **Duration:** 14 min
- **Started:** 2026-06-16T10:34:38Z
- **Completed:** 2026-06-16T10:48:20Z
- **Tasks:** 3 (all auto)
- **Files modified:** 36 (33 created, 3 modified)

## Accomplishments

- **FND-04 token pipeline + apply-before-paint:** `tokens/tokens.json` (v1 dark-operator palette bg `#0B0E14` / accent `#5BA8FF`, compact|operator|review density tiers, default `operator`) → `tokens/generate-theme.mjs` (~58 LOC, NOT Style Dictionary) → `src/styles/theme.css` with a Tailwind 4 `@theme` block + per-`data-density` override blocks. `index.html` carries the **inline synchronous** head script setting `data-theme`/`data-density` before the bundle (no flash), keyed on `aura.theme`/`aura.density` — the same keys `src/theme/applyTheme.ts` reads/writes.
- **FND-05 brand + shell + PWA:** `AppShell.tsx` renders the Logo `<img alt="Aura">` in a restrained three-zone dark-operator scaffold (ux-spec §147) using Tailwind token utilities (`bg-bg`/`text-text`/`border-border`), with copy-contract labels only and **zero marketing hero text** (ux-spec §350). `ErrorBoundary.tsx` (D-13) shows a themed safe fallback with no telemetry sink. The brand/PWA icon set (`logo`, `favicon.svg`, `apple-touch-icon`, `pwa-192/512`, `pwa-maskable-512`) is pre-generated from `public/Logo.png` and committed — no build-time `sharp` dep; the command is documented in `web/README.md`.
- **React Compiler + PWA:** `vite.config.ts` orders `babel({ presets:[reactCompilerPreset()] })` **before** `react()` — the built bundle imports `react/compiler-runtime` `useMemoCache`, proving the Compiler fired. `VitePWA({ registerType:'autoUpdate', strategies:'generateSW', … })` emits `sw.js` + `workbox-*.js` + `manifest.webmanifest` (theme/background `#0B0E14`, 192/512/maskable icons).
- **FND-02 real committed dist:** `npm run build` produced `internal/webui/dist/` (index.html with the inline theme script intact, hashed `assets/index-*.js|css`, `sw.js`, `registerSW.js`, `manifest.webmanifest`, icons), **overwriting the 23-01 placeholder**. The dist is tracked (not gitignore-shadowed), and `go build`/`go vet`/`go test`/`go test -race ./internal/webui/` are all green against the real embedded tree.
- **23-01 carryover honored:** the AppShell Vitest test was re-included in the eslint + tsconfig gate (23-01 Deviation #5 reversed) now `AppShell.tsx` exists — it passes; `npm run lint` + `typecheck` + `test` + `format:check` are all GREEN with the 23-01 assertions unchanged.

## Task Commits

Each task was committed atomically:

1. **Task 1: Design-token theme — tokens.json, tiny generator, Tailwind 4 @theme, apply-before-paint head script** — `e59209c3` (feat)
2. **Task 2: Branded placeholder shell + error boundary + React-Compiler Vite config + PWA + icons** — `61f5feaa` (feat)
3. **Task 3: Real Vite build → commit reproducible internal/webui/dist (overwrites the placeholder)** — `7926e0be` (feat)

_Plan metadata commit follows this SUMMARY._

## Files Created/Modified

- `web/tokens/tokens.json` — hand-authored dark-operator palette + density/radius/font tokens (single source).
- `web/tokens/generate-theme.mjs` — ~58-LOC generator → `src/styles/theme.css` (@theme + density blocks) + `head-snippet.generated.html`.
- `web/src/styles/{theme.css,index.css,head-snippet.generated.html}` — generated theme + Tailwind import + generated head snippet.
- `web/src/theme/{density.ts,applyTheme.ts}` — density constants (default operator) + theme/density localStorage I/O sharing `aura.theme`/`aura.density`.
- `web/index.html` — inline synchronous head theme script + theme-color + favicon/apple-touch links + `#root`.
- `web/src/{AppShell.tsx,ErrorBoundary.tsx,main.tsx,vite-env.d.ts}` — branded shell, D-13 boundary, React 19 createRoot mount, Vite/PWA client type refs.
- `web/vite.config.ts` — babel(reactCompilerPreset)→react→tailwind→VitePWA; outDir `../internal/webui/dist`, emptyOutDir, sourcemap false.
- `web/public/*` — committed brand/PWA icon set from `public/Logo.png`.
- `web/README.md` — scripts, build-output note, token + icon-generation commands.
- `web/.prettierignore` — generated theme.css/head-snippet + dist/coverage/reports.
- `web/eslint.config.js` — node globals for the .mjs/config block; `import-x/named` off (tsc owns resolution); test-file `no-unnecessary-condition` off; removed the two Wave-0 RED-stub ignores.
- `web/tsconfig.json` — removed the AppShell.test.tsx exclude (test rejoins the typecheck gate).
- `internal/webui/dist/*` — the real committed Vite bundle (the //go:embed source).

## Decisions Made

- **`@rolldown/plugin-babel` API correction:** the shipped 0.2.3 package exports `babelPlugin` as **default** (named `babel`/`babelConfig` from RESEARCH do not exist) and takes `{ include, presets }`. Used `import babel from '@rolldown/plugin-babel'` + `babel({ include: /\.[jt]sx?$/, presets: [reactCompilerPreset()] })`. `reactCompilerPreset()` returns the `RolldownBabelPreset` that slot expects. Verified by `useMemoCache` in the output bundle.
- **outDir `../internal/webui/dist`** (not `web/dist`): honors 23-01 Deviation #1 (Go embed is package-relative). `web/dist/` is never created.
- **Pre-generated committed icons** (no `sharp` dep in `web/`): generated once via a throwaway `sharp` install in a temp dir; reproducible command in `web/README.md` (D-08 lean).
- **Generated artifacts excluded from prettier** via a new `web/.prettierignore` (theme.css/head-snippet) so the format gate doesn't fight the generator.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `@rolldown/plugin-babel` import/API shape in vite.config.ts**
- **Found during:** Task 3 (`npm run build` — caught by `tsc -b` which typechecks vite.config.ts via tsconfig.node.json)
- **Issue:** The PLAN/RESEARCH `import { babel } from '@rolldown/plugin-babel'` + `babel({ babelConfig: reactCompilerPreset() })` does not match the shipped 0.2.3 export (`babelPlugin as default`, options `{ include, presets }`). TS2614 "no exported member 'babel'".
- **Fix:** `import babel from '@rolldown/plugin-babel'` (default) + `babel({ include: /\.[jt]sx?$/, presets: [reactCompilerPreset()] })`. Plugin order (babel before react) preserved.
- **Files modified:** web/vite.config.ts
- **Verification:** `npm run build` green; built bundle imports `react/compiler-runtime` `useMemoCache` (Compiler fired); `go build/test/-race ./internal/webui/` green.
- **Committed in:** 7926e0be (Task 3 commit)

**2. [Rule 3 - Blocking] Generator's `process.stdout` failed the no-undef lint gate**
- **Found during:** Task 1 (`npm run lint`)
- **Issue:** `tokens/generate-theme.mjs` is linted but the `.mjs`/config flat-config block disabled type-checking without adding node globals → `'process' is not defined` (no-undef), failing `--max-warnings=0`.
- **Fix:** Added a dedicated `{ files: ['**/*.config.*','tokens/*.mjs'], languageOptions: { globals: globals.node } }` block AFTER the `disableTypeChecked` spread (which itself re-sets languageOptions).
- **Files modified:** web/eslint.config.js
- **Verification:** `npm run lint` green.
- **Committed in:** e59209c3 (Task 1 commit)

**3. [Rule 3 - Blocking] CSS side-effect import failed typecheck under `noUncheckedSideEffectImports`**
- **Found during:** Task 2 (`npm run typecheck`)
- **Issue:** `import './styles/index.css'` in main.tsx → TS2882 (no type declaration for the side-effect import).
- **Fix:** Added `web/src/vite-env.d.ts` referencing `vite/client` (+ `vite-plugin-pwa/client`), which declares the CSS side-effect import.
- **Files modified:** web/src/vite-env.d.ts
- **Verification:** `npm run typecheck` green.
- **Committed in:** 61f5feaa (Task 2 commit)

**4. [Rule 3 - Blocking] import-x/named false positive + test-file no-unnecessary-condition (gate re-inclusion)**
- **Found during:** Task 2 (`npm run lint` after re-including AppShell.test.tsx per 23-01 Dev #5)
- **Issue:** `import-x/named` couldn't resolve `babel` from `@rolldown/plugin-babel` (it owns no types map; tsc owns resolution) → false error. Separately, the re-included AppShell.test.tsx's defensive `container.textContent ?? ''` fired `no-unnecessary-condition` because the resolved DOM type is non-null — but the 23-01 test must NOT be edited.
- **Fix:** Disabled `import-x/named` (consistent with 23-01's tsc-owns-resolution decision); added a test-file-scoped override turning off `no-unnecessary-condition` (defensive runtime guards in tests are legitimate, keeps the 23-01 assertions byte-identical).
- **Files modified:** web/eslint.config.js
- **Verification:** `npm run lint` + `npm run test` green; AppShell.test.tsx passes unchanged.
- **Committed in:** 61f5feaa (Task 2 commit)

---

**Total deviations:** 4 auto-fixed (1 Rule-1 bug, 3 Rule-3 blocking)
**Impact on plan:** All four were necessary to make the React Compiler pass load, the generator lint clean, the CSS import typecheck, and the re-included Wave-0 test rejoin the gate without weakening its assertions. No scope creep — every fix stays inside the scaffold + theme + build boundary. The plugin-babel API correction is the load-bearing one (the RESEARCH shape silently predated the shipped 0.2.3 export).

## Issues Encountered

- The generated `theme.css`/`head-snippet.generated.html` initially failed `format:check`; resolved by a `.prettierignore` (generator-owned formatting) rather than hand-reformatting generated output.

## Known Stubs

The empty `<section aria-label="Chat">` and `<aside aria-label="Display workspace">` panels in `AppShell.tsx` are **intentional placeholders** — D-09 mandates a lean scaffold with the placeholder shell as the only screen in Phase 23. They carry no data source by design; the chat lane lands in Phase 25 and the display workspace in Phase 26. Not data-stubs flowing fake content to the UI.

## Dist Reproducibility — flagged for 23-03 CI

The committed `internal/webui/dist/` was built on **Windows Node 22**. The 23-03 `web-dist-freshness` CI job rebuilds it on **Linux Node 24** and `git diff --exit-code`s. Windows-Node-22 bundler output will likely NOT be byte-identical to Linux-Node-24, so that gate may red on its **first** CI run and require the operator to recommit the CI-built (Linux Node 24) dist — this is the EXPECTED "first Linux CI run is the true byte-canonical proof" per RESEARCH (Pitfall 3 / A-DIST-REPRO). Drift was minimized: `sourcemap: false`, pinned lockfile, content-hashed asset names. The dist here is a fully WORKING bundle (embed test + race green); the byte-canonical reconciliation belongs to 23-03, not faked here.

## User Setup Required

None — no external service configuration required for the scaffold.

## Next Phase Readiness

- **23-03 (serve/CI/Docker)** can now: wire `aura serve` to mount `internal/webui.Handler()` at `/` over this real bundle (turns the Playwright `e2e/shell.spec.ts` GREEN — it parses + lists 2 tests today, RED at runtime only because no server is mounted yet); add the 4 path-filtered web CI jobs (`web-lint`/`web-test`/`web-dist-freshness`/`web-e2e`); add the Node-24 `webbuild` Docker stage. **The freshness gate's first Linux run is the byte-canonical reconciliation** (flagged above).
- **24-web-foundation** inherits the embed→serve→render path proven here; it owns the real SPA-fallback route exclusion + web-auth boundary + runtime health.

## Self-Check: PASSED

All listed created files present on disk (tokens/generator/theme, AppShell/ErrorBoundary/main, vite.config, README, public/logo.png, and the real `internal/webui/dist/` bundle incl. index.html/manifest/sw.js/assets). All three task commits (`e59209c3`, `61f5feaa`, `7926e0be`) present in git history.

---
*Phase: 23-frontend-infrastructure-industrial-foundation*
*Completed: 2026-06-16*
