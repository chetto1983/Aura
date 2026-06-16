---
phase: 23-frontend-infrastructure-industrial-foundation
plan: 01
subsystem: infra
tags: [vite, react19, typescript, tailwind4, eslint, prettier, vitest, playwright, go-embed, npm, frontend]

# Dependency graph
requires:
  - phase: 23-RESEARCH (FND-01)
    provides: locked decision record (linter/formatter/tokens/layout/pipeline/harness) + package-legitimacy audit
provides:
  - web/ single npm package with a committed, cross-platform-complete package-lock.json (566 pkgs)
  - zero-warning gate configs (eslint flat config prettier-LAST, strict tsconfig, prettier, vitest jsdom, playwright Chromium)
  - internal/webui leaf package — //go:embed all:dist + Sub() + Handler() static host (green httptest, 200/404)
  - Wave-0 RED test stubs (Vitest/RTL AppShell + Playwright shell spec) pinned to the SC2/SC4 contract
  - .gitattributes LF pin + .gitignore tracking of the committed embed dist
affects: [23-02-scaffold, 23-03-serve-ci-docker, 24-web-foundation]

# Tech tracking
tech-stack:
  added: [react@19.2.7, vite@8.0.16, typescript@6.0.3, tailwindcss@4.3.1, eslint@10.5.0, typescript-eslint@8.61.1, vitest@4.1.9, "@playwright/test@1.61.0", "@rolldown/plugin-babel@0.2.3", babel-plugin-react-compiler@1.0.0, eslint-plugin-import-x@4.16.2]
  patterns: [npm-single-package, eslint-flat-config-prettier-last, tsc-project-references, go-embed-all-dist-leaf-package, stdlib-httptest-no-testify, wave-0-red-stubs]

key-files:
  created:
    - web/package.json
    - web/package-lock.json
    - web/tsconfig.json
    - web/tsconfig.node.json
    - web/eslint.config.js
    - web/.prettierrc.json
    - web/vitest.config.ts
    - web/playwright.config.ts
    - web/.gitignore
    - web/src/test/setup.ts
    - web/src/__tests__/AppShell.test.tsx
    - web/e2e/shell.spec.ts
    - internal/webui/embed.go
    - internal/webui/doc.go
    - internal/webui/embed_test.go
    - internal/webui/dist/index.html
  modified:
    - .gitattributes
    - .gitignore

key-decisions:
  - "Committed //go:embed source co-located at internal/webui/dist/ (Go embed is package-relative; ../web/dist is impossible)"
  - "npm override relaxes eslint-plugin-jsx-a11y's stale eslint<=9 peer to accept eslint 10 (runtime-compatible)"
  - "import-x resolution rules disabled (tsc owns module resolution); import-order linting kept"
  - "Wave-0 RED stubs excluded from the strict lint/typecheck gate so the gate stays green while tests stay RED at the runner"

patterns-established:
  - "eslint flat config: js.recommended -> tseslint strict+stylistic typeChecked -> import-x recommended -> react-hooks/jsx-a11y/react-refresh -> prettier LAST"
  - "internal/webui leaf package: stdlib embed/io.fs/net.http only, never agent/agui, so the D-17 boundary gate stays green"
  - "stdlib testing + httptest (no testify) for the webui/agui HTTP surface"

requirements-completed: [FND-01, FND-02, FND-03]

# Metrics
duration: 21min
completed: 2026-06-16
---

# Phase 23 Plan 01: Frontend Infrastructure Wave-0 Foundation Summary

**FND-01-vetted React 19 / Vite 8 / TS 6 / Tailwind 4 toolchain installed from a committed cross-platform lockfile, with zero-warning eslint/prettier/tsc gate configs, an `internal/webui` `//go:embed all:dist` static host (green httptest), and the Vitest + Playwright stubs landed RED before the shell they guard.**

## Performance

- **Duration:** 21 min
- **Started:** 2026-06-16T10:03:02Z
- **Completed:** 2026-06-16T10:24:20Z
- **Tasks:** 3 (1 operator-approved checkpoint + 2 auto)
- **Files modified:** 18 (16 created, 2 modified)

## Accomplishments

- `web/` single npm package installs the FND-01 stack from a committed `package-lock.json` that is **cross-platform complete** — the lockfile carries the Linux native-binding optional-deps (`@rollup/rollup-linux-x64-gnu`, `@rolldown/binding-linux-x64-gnu`, `@tailwindcss/oxide-linux-x64-gnu`, `lightningcss-linux-x64-gnu`, …) so the 23-03 Linux CI `npm ci` resolves the same tree.
- Zero-warning gate configs that **run green**: `eslint.config.js` flat config (typescript-eslint strict + stylistic typeChecked → react-hooks recommended-latest → jsx-a11y → react-refresh, `eslint-config-prettier` LAST), strict `tsconfig.json` (`noUncheckedIndexedAccess` + `exactOptionalPropertyTypes` + `verbatimModuleSyntax`), `.prettierrc.json`, `vitest.config.ts` (jsdom), `playwright.config.ts` (Chromium-only, boots `../aura serve --only=cli` on loopback, health-gated `/healthz`, explicit `env` block).
- `internal/webui` leaf package: `//go:embed all:dist` + `Sub()` + `Handler()` over `http.FileServerFS`, with a stdlib `httptest` proving GET `/` → 200 `text/html` (dark-theme + brand markers) and a missing path → 404. `go vet`/`build`/`test`/`-race` + `agui_boundary_check.sh` + `check-file-size.sh` all green.
- The two frontend stubs exist **RED-as-designed**: the Vitest/RTL `AppShell.test.tsx` fails to resolve `../AppShell` (23-02 builds it), and the Playwright `shell.spec.ts` asserts theme-before-paint + brand + no-marketing-hero copy against the not-yet-served shell (23-03 wires it).
- `.gitattributes` pins LF for `web/**` and `internal/webui/dist/**`; `.gitignore` un-ignores the committed embed dist so the single-binary host has bytes to embed with no Node build.

## Task Commits

1. **Task 1: FND-01 sign-off + supply-chain legitimacy gate** — operator-approved inline by the orchestrator (operator typed "approved"); NOT re-run. See "Task 1 — Operator Approval" below.
2. **Task 2: Install pinned npm toolchain + zero-warning gate configs** — `6a11dad6` (feat)
3. **Task 3: Wave-0 RED test stubs + Go embed package & httptest** — `89e5cbb6` (feat)

_Task 1 produced no commit (gate-only)._

## Task 1 — Operator Approval (recorded, not re-run)

The FND-01 decision-record sign-off + supply-chain legitimacy gate was run inline by the orchestrator and **operator-approved** ("approved"). Pinned set verified via `npm view` on 2026-06-16, zero drift vs RESEARCH:

- `@rolldown/plugin-babel@0.2.3` — slopcheck [OK]; maintainers Evan You (yyx990803), sapphi-red, official rolldownbot → **VERIFIED legitimate** (youngest/SUS package cleared).
- `eslint-plugin-import-x@4.16.2` — slopcheck [OK] → **KEPT** (import-order lint retained).
- No `[SLOP]` packages. Versions pinned (caret in package.json, exact resolved in lockfile): react/react-dom 19.2.7, vite 8.0.16, @vitejs/plugin-react 6.0.2, @rolldown/plugin-babel 0.2.3, babel-plugin-react-compiler 1.0.0, typescript 6.0.3, tailwindcss/@tailwindcss/vite 4.3.1, vite-plugin-pwa 1.3.0, eslint 10.5.0, @eslint/js 10.0.1, typescript-eslint 8.61.1, eslint-plugin-react-hooks 7.1.1, eslint-plugin-react-refresh 0.5.3, eslint-plugin-jsx-a11y 6.10.2, eslint-plugin-import-x 4.16.2, eslint-config-prettier 10.1.8, prettier 3.8.4, globals 17.6.0, vitest/@vitest/coverage-v8 4.1.9, @testing-library/react 16.3.2, jsdom 29.1.1, @playwright/test 1.61.0.

## Verify Evidence

| Gate | Command | Result |
|------|---------|--------|
| eslint (zero-warning) | `cd web && npm run lint` | GREEN (exit 0, `--max-warnings=0`) |
| prettier | `cd web && npm run format:check` | GREEN (exit 0) |
| typecheck | `cd web && npx tsc --noEmit -p tsconfig.json` + `npx tsc -b` | GREEN (exit 0) |
| lockfile present + cross-platform | `test -f package-lock.json` + grep linux-x64/linux-gnu | GREEN (36 linux-x64, 90 linux-gnu entries) |
| go embed | `go vet/build/test/-race ./internal/webui/` | GREEN (200 text/html + 404) |
| agui boundary | `bash scripts/agui_boundary_check.sh` | GREEN (webui is leaf) |
| file-size | `bash scripts/check-file-size.sh` | GREEN (all ≤600 LOC) |
| Vitest AppShell stub | `cd web && npx vitest run` | **RED as designed** (cannot resolve ../AppShell — 23-02) |
| Playwright shell stub | `web/e2e/shell.spec.ts` | **RED as designed** (no served shell yet — 23-03) |

## Lockfile Cross-Platform Status

`package-lock.json` (npm v11.6.2, Windows host) records all platform variants in the `packages` map: confirmed Linux native bindings present — `@rollup/rollup-linux-x64-gnu`, `@rollup/rollup-linux-x64-musl`, `@rolldown/binding-linux-x64-gnu`, `@tailwindcss/oxide-linux-x64-gnu`, `lightningcss-linux-x64-gnu`, plus arm64/musl/etc. variants. CI's `npm ci` on `ubuntu-latest` will resolve the Linux x64 gnu bindings. **Residual risk for 23-03:** the lockfile was generated on Windows (WSL Node was not on PATH); while npm v11 records cross-platform optional-deps, the first `npm ci` on the Linux runner is the true cross-platform proof — flagged for the 23-03 CI validation. No Vite build runs in 23-01, so no platform-specific build artifact is committed here.

## Decisions Made

- **Embed source co-located at `internal/webui/dist/`** (not `web/dist/`): Go `//go:embed` is package-relative and forbids `..`, so `//go:embed all:dist` in `internal/webui/embed.go` can only read `internal/webui/dist/`. The plan text assumed `web/dist`; reconciled to the only path the directive can read. 23-02 must set Vite `outDir` to `../internal/webui/dist` (or copy `web/dist` → `internal/webui/dist`). `.gitignore`/`.gitattributes` updated to track + LF-pin the co-located dist.
- **npm `overrides` for jsx-a11y eslint peer**: `eslint-plugin-jsx-a11y@6.10.2` (latest) still declares `eslint ^3..^9`; it is runtime-compatible with eslint 10 (react-hooks/react-refresh already declare `^10`). An `overrides` `{ "eslint-plugin-jsx-a11y": { "eslint": "$eslint" } }` resolves the ERESOLVE without `--legacy-peer-deps`, keeping the lockfile reproducible.
- **`@types/react`/`@types/react-dom` pinned to DT-real versions** `19.2.17` / `19.2.3` (the gate's `^19.2.7` for `@types/react-dom` does not exist — DT types do not track react's patch).
- **import-x resolution rules disabled** (`no-unresolved`, `namespace`, `no-named-as-default*`): tsc owns module resolution and import-x's TS-resolver interface conflicts with import-x@4; `import-x/order` (the actual value) retained.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Embed source path: web/dist is unreachable by //go:embed**
- **Found during:** Task 3 (Go embed package)
- **Issue:** `//go:embed all:dist` in `internal/webui/embed.go` resolves package-relative (`internal/webui/dist`), not `web/dist`. The plan's placeholder at `web/dist/index.html` failed to compile ("pattern all:dist: no matching files found").
- **Fix:** Co-located the committed embed source at `internal/webui/dist/index.html`; removed `web/dist`; pointed `.gitignore` negation + `.gitattributes` LF pin at `internal/webui/dist/`. Documented that 23-02 must build Vite into `internal/webui/dist`.
- **Files modified:** internal/webui/dist/index.html, .gitignore, .gitattributes
- **Verification:** `go build/test/-race ./internal/webui/` GREEN
- **Committed in:** 89e5cbb6 (Task 3 commit)

**2. [Rule 3 - Blocking] eslint-plugin-jsx-a11y stale eslint peer (ERESOLVE)**
- **Found during:** Task 2 (npm install)
- **Issue:** `npm install` failed ERESOLVE — jsx-a11y@6.10.2 peer `eslint ^3..^9` vs pinned eslint@10.
- **Fix:** Added `overrides.eslint-plugin-jsx-a11y.eslint = "$eslint"` (runtime-compatible; not a package swap).
- **Files modified:** web/package.json, web/package-lock.json
- **Verification:** `npm install` succeeded; `npm run lint` GREEN
- **Committed in:** 6a11dad6 (Task 2 commit)

**3. [Rule 3 - Blocking] @types/react-dom@^19.2.7 does not exist + missing @types/node**
- **Found during:** Task 2 (npm install / typecheck)
- **Issue:** ETARGET on `@types/react-dom@^19.2.7` (DT types don't track react patch); `tsc` errored `Cannot find name 'process'` in node-context config files (no `@types/node`).
- **Fix:** Pinned `@types/react@19.2.17` / `@types/react-dom@19.2.3`; installed `@types/node@24.13.2`; split node-context config files into `tsconfig.node.json` (composite, node types) referenced from the root.
- **Files modified:** web/package.json, web/package-lock.json, web/tsconfig.json, web/tsconfig.node.json
- **Verification:** `tsc --noEmit` + `tsc -b` GREEN (exit 0)
- **Committed in:** 6a11dad6 (Task 2 commit)

**4. [Rule 3 - Blocking] import-x resolver interface conflict + false positives**
- **Found during:** Task 2 (npm run lint)
- **Issue:** `importX.flatConfigs.typescript` wired a TS resolver with an interface invalid for import-x@4 → 20 `import-x/no-unresolved` errors; plus `no-named-as-default*` false positives on the canonical flat-config default-import-of-plugin pattern.
- **Fix:** Dropped the `typescript` flat preset; disabled `no-unresolved`/`namespace`/`no-named-as-default*` (tsc owns resolution); kept `import-x/order`.
- **Files modified:** web/eslint.config.js
- **Verification:** `npm run lint` GREEN
- **Committed in:** 6a11dad6 (Task 2 commit)

**5. [Rule 3 - Blocking] RED stubs broke the strict lint/typecheck gate**
- **Found during:** Task 3 (frontend stubs)
- **Issue:** The RED `AppShell.test.tsx` import of the not-yet-built `../AppShell` degraded type-aware lint to `any` (a `no-unnecessary-condition` error) and reded `tsc --noEmit` (TS2307).
- **Fix:** Excluded the two RED stubs from the eslint `ignores` and the root `tsconfig` `exclude`, with a TODO tying removal to 23-02/23-03. The stubs stay RED at the **runner** (vitest/playwright) — the real Wave-0 contract — while the gate configs stay green.
- **Files modified:** web/eslint.config.js, web/tsconfig.json
- **Verification:** `npm run lint`/`format:check`/`typecheck` GREEN; `npx vitest run` RED (contract)
- **Committed in:** 89e5cbb6 (Task 3 commit)

---

**Total deviations:** 5 auto-fixed (1 Rule-1 bug, 4 Rule-3 blocking)
**Impact on plan:** All necessary to make the pinned toolchain install + the gate configs run + the embed compile on this stack. No scope creep — every fix stays inside the Wave-0 install+config+RED-stub boundary. The one substantive design change (embed dist co-location) is the only Go-correct way to satisfy `//go:embed`; recorded so 23-02 wires Vite's outDir accordingly.

## Issues Encountered

- A GSD pre-commit hook auto-staged two unrelated `.planning/spikes/063-calendar-mcp-build-launch/` files (parallel-session work) into the Task-2 commit `6a11dad6`. Harmless (untracked planning artifacts), not reverted to avoid touching the parallel session.

## User Setup Required

None — no external service configuration required for Wave 0.

## Next Phase Readiness

- **23-02 (scaffold)** can now build the real `web/` app: configs + lockfile + embed host exist. **Action required:** set Vite `outDir` to `../internal/webui/dist` (the committed `//go:embed all:dist` source), then remove the two Wave-0 ignore/exclude entries (`AppShell.test.tsx`, `shell.spec.ts`) as `AppShell.tsx` and the served shell turn the stubs green.
- **23-03 (serve/CI/Docker)** must run `npm ci` on the Linux runner — the first true cross-platform lockfile proof (flagged above). Playwright's `webServer` `env` block in `playwright.config.ts` is where the real DB/Neo4j vars `aura serve` needs get wired.

## Self-Check: PASSED

All 16 created files present on disk; both task commits (`6a11dad6`, `89e5cbb6`) present in git history.

---
*Phase: 23-frontend-infrastructure-industrial-foundation*
*Completed: 2026-06-16*
