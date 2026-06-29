---
phase: 23-frontend-infrastructure-industrial-foundation
verified: 2026-06-16T12:00:00Z
status: human_needed
score: 5/5 must-haves verified (automated); 2 items require live CI/human proof
overrides_applied: 0
human_verification:
  - test: "Run web-dist-freshness CI job (or locally: cd web && npm ci && npm run build && git diff --exit-code -- internal/webui/dist/)"
    expected: "git diff produces no output — Linux Node-24 build output matches the committed Windows Node-22 dist, OR the CI job reds and the operator recommits the CI-built dist (the expected byte-canonical reconciliation documented in 23-02/23-03)"
    why_human: "The committed dist was built on Windows Node 22; Linux Node 24 bundler output may differ byte-for-byte. The freshness gate logic and script are verified correct, but the byte-identity can only be proven by running on the actual CI Linux Node-24 runner."
  - test: "Run web-e2e Playwright job in CI (provisions docker-compose Postgres + migrate + builds dist + boots aura serve + runs playwright test)"
    expected: "Both shell.spec.ts tests pass: (1) html[data-theme='dark'] and html[data-density] attributes present on first response, (2) img[alt=Aura] visible and no marketing-hero phrases in body text"
    why_human: "The Playwright E2E boots a real aura serve against a provisioned Postgres. This requires the full CI stack (docker-compose Postgres, aura binary with embedded dist, Chromium). The deterministic local proxy (serve_webui_test.go) is verified green; the end-to-end browser rendering of the embedded shell needs a live CI run."
---

# Phase 23: Frontend Infrastructure & Industrial Foundation — Verification Report

**Phase Goal:** Establish the industrial frontend foundation before any feature code: locked decision record, branded placeholder shell embedded in the single binary via `//go:embed all:dist`, zero-warning blocking CI gate, brand + PWA metadata (no marketing hero text), and Node-24 multi-stage Docker build with Vitest + Playwright harness.
**Verified:** 2026-06-16T12:00:00Z
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | FND-01 decision record exists, is approved, and locks all six dimensions (linter, formatter, tokens, layout, pipeline, harness) | VERIFIED | `23-RESEARCH.md` is the explicit FND-01 deliverable; covers all six dimensions (D-01..D-18 decisions + all three Corrections); operator-approved inline per 23-01-SUMMARY.md Task 1 gate record |
| 2 | `aura serve` serves a branded placeholder shell from `//go:embed all:dist`; dark theme + operator density applied before paint; binary serves from internal/webui/dist | VERIFIED | `go test ./internal/webui/ ./cmd/aura/ -run 'TestHandler\|TestSub\|TestServeWebui' -count=1` GREEN. `embed.go` has `//go:embed all:dist`. `internal/webui/dist/index.html` committed with inline synchronous head script (`<script>(function(){...r.setAttribute('data-theme',...)})()</script>`, not `type=module`). `newServeHandler` wired in `serve.go:250`. |
| 3 | `npm run lint` + `npm run format:check` + `tsc --noEmit` pass zero-warning and are a blocking CI gate with golangci-lint parity | VERIFIED | `package.json` `lint` script carries `--max-warnings=0`; `eslint.config.js` is flat config with `eslint-config-prettier` LAST; `web-lint` job in `ci.yml` runs `npm ci && npm run lint && npm run format:check && npm run typecheck` on Node 24, blocking, path-filtered via `dorny/paths-filter`. 23-01-SUMMARY verify evidence: lint/format:check/typecheck all GREEN. |
| 4 | `web/public/logo.png` (102 KB) renders in AppShell header; favicon + PWA/theme-color metadata present; no marketing hero text | VERIFIED | `AppShell.tsx` renders `<img src="/logo.png" alt="Aura">`. `index.html` has `<meta name="theme-color" content="#0B0E14">`, favicon, apple-touch-icon links. `manifest.webmanifest` in committed dist. `AppShell.test.tsx` RTL test asserts `getByRole('img', {name: /aura/i})` present AND no marketing-hero phrases; 23-02-SUMMARY confirms `npm run test` GREEN. |
| 5 | Node-24 multi-stage Docker build (`webbuild` stage) produces embedded dist with no Node-for-SPA in runtime; Vitest harness runs green; four blocking web CI jobs exist | VERIFIED (partial — live CI pending) | Dockerfile has `FROM node:24-bookworm-slim AS webbuild` + `COPY --from=webbuild /internal/webui/dist ./internal/webui/dist`. Runtime Node-24 is documented as orthogonal (recipe MCP servers). `web-lint`, `web-test`, `web-dist-freshness`, `web-e2e` jobs in `ci.yml` (16 total jobs confirmed, YAML parses). Vitest `AppShell.test.tsx` confirmed GREEN per 23-02-SUMMARY. Full Docker smoke + `web-e2e` + `web-dist-freshness` require live CI (see human_needed below). |

**Score:** 5/5 automated truths verified

### Deferred Items

None — all deferred items (SPA-fallback, web-auth, non-loopback boot guard) are explicitly Phase 24 scope and accounted for in the plan threat model.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `23-RESEARCH.md` | FND-01 locked decision record covering 6 dimensions | VERIFIED | Exists; covers D-01..D-18 + 3 Decision Corrections; all 6 FND requirements mapped with "Research Support" column |
| `web/eslint.config.js` | Flat config, typescript-eslint strict, prettier LAST | VERIFIED | `tseslint.config(... prettier)` — prettier is last element; strict+stylistic typeChecked present; react-hooks recommended-latest present |
| `web/package.json` | Scripts: lint/format/format:check/typecheck/test/test:e2e; lint carries --max-warnings=0 | VERIFIED | All scripts present; `"lint": "eslint . --max-warnings=0"` confirmed |
| `internal/webui/embed.go` | `//go:embed all:dist` + `Sub()` + `Handler()` | VERIFIED | `all:dist` prefix present; `Sub()` via `fs.Sub`; `Handler()` via `http.FileServerFS`; stdlib-only imports (no agent/agui) |
| `internal/webui/embed_test.go` | httptest: GET / → 200 text/html + data-theme, GET missing → 404 | VERIFIED | `TestHandler` + `TestSub` cover both cases; stdlib testing + httptest only; confirmed GREEN |
| `internal/webui/dist/` | Real Vite build committed (index.html + hashed assets + sw.js + manifest) | VERIFIED | 13 files: `index.html`, `assets/index-*.js`, `assets/index-*.css`, `sw.js`, `workbox-*.js`, `registerSW.js`, `manifest.webmanifest`, brand icons |
| `web/index.html` | Inline synchronous head script (not type=module) setting data-theme + data-density | VERIFIED | Script is bare `<script>(function(){...})()` — no `type="module"` attribute; sets data-theme and data-density with try/catch fallback |
| `web/vite.config.ts` | `babel(reactCompilerPreset())` ordered BEFORE `react()` in plugins array; outDir=../internal/webui/dist; sourcemap:false | VERIFIED | Plugin array confirmed: `babel({...presets:[reactCompilerPreset()]})` at index 0, `react()` at index 1. `outDir: '../internal/webui/dist'`; `sourcemap: false` |
| `web/src/AppShell.tsx` | Brand logo in header; no marketing hero text | VERIFIED | `<img src="/logo.png" alt="Aura">` line 7; no marketing phrases; three-zone scaffold only |
| `web/tokens/generate-theme.mjs` | ~58 LOC generator emitting @theme + density blocks; no Style Dictionary dep | VERIFIED | 58 LOC; reads tokens.json; emits `@theme { ... }` block + `:root[data-density="X"]` blocks; no Style Dictionary import |
| `web/src/styles/theme.css` | GENERATED from tokens.json; @theme block + density overrides | VERIFIED | Contains `@theme { --color-bg: #0B0E14; ... }` + 3 density blocks + color-scheme |
| `web/src/__tests__/AppShell.test.tsx` | RTL: brand img present + no-hero-text blocklist | VERIFIED | 6-phrase blocklist; `getByRole('img', {name: /aura/i})` assertion; confirmed GREEN per 23-02-SUMMARY |
| `web/e2e/shell.spec.ts` | Playwright: data-theme/data-density on html, brand visible, no hero text | VERIFIED | 2 tests; asserts `data-theme='dark'`, `data-density` regex, `img[name=/aura/i]`, body text blocklist |
| `cmd/aura/serve_webui.go` | `newServeHandler` parent mux; AG-UI routes keep priority; webui at / | VERIFIED | `newServeHandler(aguiHandler)` registers 6 AG-UI prefixes + `/` catch-all; confirmed wired in `serve.go:250` |
| `cmd/aura/serve_webui_test.go` | httptest: `/` → 200 shell, `/healthz` → AG-UI, bogus → 404 | VERIFIED | 4 subtests including AG-UI priority via fake handler hit-count assertion; confirmed GREEN |
| `.github/workflows/ci.yml` | 4 web jobs (web-lint, web-test, web-dist-freshness, web-e2e); Node 24; blocking | VERIFIED | All 4 jobs present; `node-version: 24` confirmed; `dorny/paths-filter` guards; AURA_DB_URL in web-e2e env; YAML parses (22 total jobs) |
| `docker/aura/Dockerfile` | `node:24-bookworm-slim AS webbuild` + `COPY --from=webbuild /internal/webui/dist` | VERIFIED | Stage present; `npm ci + npm run build` in webbuild; `COPY --from=webbuild /internal/webui/dist ./internal/webui/dist` in go build stage |
| `scripts/web_dist_freshness.sh` | `git diff --exit-code -- internal/webui/dist/`; set -euo pipefail | VERIFIED | Confirmed: `git diff --exit-code -- internal/webui/dist/`; bash syntax clean; proper error message |
| `web/playwright.config.ts` | Chromium-only; webServer boots `../aura serve --only=cli`; explicit env block with DB vars | VERIFIED | `projects: [{name:'chromium'}]`; `webServer.command: '../aura serve --only=cli'`; explicit `SERVE_ENV_KEYS` allow-list forwarded from `process.env` |
| `web/public/logo.png` | Brand logo (source) committed | VERIFIED | 102,283 bytes; also committed to `internal/webui/dist/logo.png` (same size — dist serves it) |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `internal/webui/embed.go` | `internal/webui/dist/` | `//go:embed all:dist` | VERIFIED | `all:dist` prefix confirmed; `dist/index.html` + 13 files embedded |
| `web/eslint.config.js` | `eslint-config-prettier` | last element in `tseslint.config()` array | VERIFIED | `prettier` is the final argument to `tseslint.config()` |
| `web/index.html` | `document.documentElement` | inline synchronous IIFE head script | VERIFIED | `r.setAttribute('data-theme', localStorage.getItem('aura.theme') || 'dark')` — synchronous, no `type=module` |
| `web/tokens/tokens.json` | `web/src/styles/theme.css` | `generate-theme.mjs` emits `@theme` block | VERIFIED | Generator reads `tokens.json`; `writeFileSync` to `../src/styles/theme.css`; `build` script runs generator first |
| `web/vite.config.ts` | `babel-plugin-react-compiler` | `babel({ presets:[reactCompilerPreset()] })` at plugins[0], before `react()` at plugins[1] | VERIFIED | Plugin array confirmed in order: babel → react → tailwindcss → VitePWA |
| `cmd/aura/serve.go` | `internal/webui.Handler` | `newServeHandler(aguiServer.Mux())` in `bootServe` at line 250 | VERIFIED | `serve.go:250` confirmed; serve handler is the parent mux, not the raw AG-UI mux |
| `docker/aura/Dockerfile webbuild` | `go build stage internal/webui/dist` | `COPY --from=webbuild /internal/webui/dist ./internal/webui/dist` | VERIFIED | Both the webbuild stage path and the COPY target confirmed correct |
| `.github/workflows/ci.yml web-e2e` | `aura serve + docker-compose postgres` | `make db-up + go run ./cmd/aura db migrate + AURA_DB_URL env + Playwright webServer.env` | VERIFIED (structure) | All wiring confirmed in YAML; true pass requires live CI run |

### Data-Flow Trace (Level 4)

The embedded operator SPA is a static-file host (no dynamic data rendering in Phase 23). The `AppShell.tsx` renders structural chrome only — the empty `<section aria-label="Chat">` and `<aside aria-label="Display workspace">` panels are documented intentional placeholders for Phase 25/26. No data-fetching hooks exist in this phase. Level 4 trace is N/A for this phase's scope.

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Go embed compiles + Handler serves 200 text/html shell | `go test ./internal/webui/ -run TestHandler -count=1` | `ok github.com/chetto1983/aura/internal/webui 0.220s` | PASS |
| Serve mount: `/` → shell, `/healthz` → AG-UI, bogus → 404 | `go test ./cmd/aura/ -run TestServeWebui -count=1` | `ok github.com/chetto1983/aura/cmd/aura 0.463s` | PASS |
| agui boundary gate (webui is leaf) | `bash scripts/agui_boundary_check.sh` | `agui-boundary: internal/agent closure is free of internal/agui.` | PASS |
| File-size gate (≤600 LOC) | `bash scripts/check-file-size.sh` | `check-file-size: all Go files within the 600-LOC cap.` | PASS |
| `go build ./...` | (all Go packages) | exit 0, no output | PASS |
| freshness script bash syntax | `bash -n scripts/web_dist_freshness.sh` | exit 0 | PASS |
| dist/index.html has data-theme + inline sync script + theme-color | inline node check | All 4 markers present: `data-theme`, `theme-color`, inline sync `<script>`, `manifest.webmanifest` | PASS |
| vite.config.ts plugin order: babel before react in array | plugin array grep | `babel(...)` at plugins[0], `react()` at plugins[1] | PASS |
| ci.yml YAML valid + all 4 web jobs present | node parse + grep | 22 jobs parsed; web-lint/web-test/web-dist-freshness/web-e2e confirmed | PASS |
| `--max-warnings=0` in lint script | package.json grep | `"lint": "eslint . --max-warnings=0"` | PASS |
| Vitest AppShell tests GREEN | (per 23-02-SUMMARY verify evidence) | `npm run test` GREEN; brand img + no-hero-text both pass | PASS (executor-reported) |
| `npm run lint + format:check + typecheck` GREEN | (per 23-03-SUMMARY verify evidence) | All three GREEN with exit 0 | PASS (executor-reported) |

### Probe Execution

No `probe-*.sh` scripts are declared for Phase 23 (FND-class foundation phase, not a migration). Step 7c: SKIPPED (no probes declared).

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|----------|
| FND-01 | 23-01 | Deep industrial-infra research pass; locked decision record before feature code | SATISFIED | `23-RESEARCH.md` is the FND-01 deliverable; operator-approved; all 6 dimensions covered |
| FND-02 | 23-01, 23-02, 23-03 | Vite 8 + React 19 + TS scaffold + `//go:embed all:dist` → binary-embeddable dist served by `aura serve` | SATISFIED | `embed.go` + `serve_webui.go` + real committed dist; Go tests GREEN |
| FND-03 | 23-01, 23-03 | ESLint flat + Prettier + `tsc --noEmit` zero-warning blocking CI gate | SATISFIED | `web-lint` job in ci.yml confirmed; `--max-warnings=0` confirmed; 23-03-SUMMARY GREEN |
| FND-04 | 23-02 | `tokens.json` → Tailwind 4 `@theme` dark-operator palette + density, applied before paint | SATISFIED | `generate-theme.mjs` emits `@theme` block; `index.html` inline sync script; `theme.css` confirmed |
| FND-05 | 23-02 | `public/Logo.png` in header + favicon + PWA/theme-color; no marketing hero text | SATISFIED | `AppShell.tsx` img[alt=Aura]; `index.html` theme-color + favicon; RTL test confirms no-hero-text GREEN |
| FND-06 | 23-01, 23-03 | Vitest + component/E2E harness + Node-24 Docker build; no Node in runtime | SATISFIED (conditional) | Vitest GREEN; Docker webbuild stage confirmed; `web-e2e` job wired; live CI run is the final proof |

**Orphaned requirements:** None. All 6 FND-01..FND-06 requirements declared in the plans are mapped to REQUIREMENTS.md with `[x]` status and traced to Phase 23. No requirements mapped to Phase 23 in REQUIREMENTS.md lack a covering plan.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `web/src/styles/head-snippet.generated.html` | — | Generated file created by `generate-theme.mjs` but NOT used by `web/index.html` (dual source-of-truth drift) | INFO | The generator writes a head-snippet artifact that is already hand-baked into `index.html`. The snippet is advisory/reference only; `index.html` IS the authoritative version. No actual behavior impact — the snippet serves as documentation that the localStorage key names stay in sync. Flagged in 23-REVIEW.md as WR-01 (advisory, not blocking). |
| `web/e2e/shell.spec.ts` | 1 | Top-of-file comment says "RED until 23-03 wires aura serve" — but 23-03 DID wire it; comment is now stale | INFO | Harmless stale comment. The E2E is now the live test; the "RED until" note is historical. Not a FIXME/TBD/XXX debt marker. |
| None | — | No TBD / FIXME / XXX / HACK / PLACEHOLDER markers found in Phase 23 files | — | Debt-marker gate: CLEAN |

**Debt marker gate:** No unreferenced TBD/FIXME/XXX markers found in any Phase 23 file. CLEAN.

### Human Verification Required

The following items are verified as correctly implemented by deterministic code checks, but their END-TO-END behavior requires a live CI run or a live binary boot to confirm:

---

#### 1. web-dist-freshness byte-canonical proof

**Test:** Push a change to `web/**` or run `bash scripts/web_dist_freshness.sh` in a Linux Node-24 environment (or wait for the CI `web-dist-freshness` job on the next PR)

**Expected:** Either (a) `git diff --exit-code -- internal/webui/dist/` produces no output and the job passes, meaning the Windows Node-22 build was byte-identical to Linux Node-24, OR (b) the job reds with "internal/webui/dist is stale" and the operator runs `npm run build` on Linux Node 24 and recommits the dist — after which the job goes green. This second outcome is the DOCUMENTED EXPECTED BEHAVIOR (23-02-SUMMARY "Dist Reproducibility" + 23-03-SUMMARY "CI Reconciliation").

**Why human:** The committed `internal/webui/dist/` was built on Windows Node 22 (23-02). The Linux Node-24 bundler (Rolldown) may produce different bytes (asset hash values, whitespace, file ordering). Byte-identity can only be verified by running the actual Linux CI runner. The gate logic (`git diff --exit-code`) and the script are verified correct; the question is which reconciliation path the first CI run takes.

---

#### 2. Playwright E2E end-to-end: aura serve + browser rendering + theme-before-paint

**Test:** Trigger the `web-e2e` CI job (by pushing a `web/**` change), or locally: bring up Postgres (`make db-up`), run `go run ./cmd/aura db migrate`, build dist (`cd web && npm run build`), build binary (`go build -o aura ./cmd/aura`), install Playwright (`cd web && npx playwright install --with-deps chromium`), run `cd web && npm run test:e2e`

**Expected:** Both `shell.spec.ts` tests pass:
- `embedded operator shell > applies dark theme + operator density before paint` — `html[data-theme='dark']` and `html[data-density]` attributes present on first navigation (no flash)
- `embedded operator shell > shows the Aura brand and no marketing-hero copy` — `img[name=/aura/i]` visible; body text contains none of the 6 marketing-hero phrases

**Why human:** The Playwright E2E boots a real `aura serve` process against a provisioned Postgres, then drives a Chromium browser against the live embedded shell. This requires the full docker-compose Postgres stack, a freshly-built binary embedding the dist, and a Chromium browser — none of which are available in this static code verification. The deterministic proxy (`serve_webui_test.go` httptest asserting `data-theme` in the embedded shell body) is VERIFIED GREEN and provides high confidence, but browser rendering + the Playwright `webServer` lifecycle add real dependencies.

---

## Gaps Summary

No BLOCKER gaps found. All five must-have truths are verified at the code/artifact level by deterministic checks.

The two human-needed items are infrastructure correctness items that require a live CI run — their code implementations are verified. The most likely outcome on the first CI run is that `web-dist-freshness` reds (Windows/Linux build output not byte-identical) and the operator recommits the CI-built dist; this is the documented, expected reconciliation path, not a gap.

**Code review note (from 23-REVIEW.md):** The review found 0 critical / 6 advisory / 5 info findings. None are blockers. The advisory items (WR-01 dual head-snippet, WR-02 localStorage isolation, WR-03 leaf-boundary CI enforcement, WR-04 gitattributes binary-corruption — verified latent-only, WR-05 webServer health URL, WR-06 E2E assertion breadth) are robustness and maintainability traps, not behavioral defects. They are appropriate cleanup candidates for Phase 24 where the serve layer is extended.

---

_Verified: 2026-06-16T12:00:00Z_
_Verifier: Claude (gsd-verifier)_
