---
phase: 23
slug: frontend-infrastructure-industrial-foundation
status: planned
nyquist_compliant: true
wave_0_complete: false
created: 2026-06-16
---

# Phase 23 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Source: `23-RESEARCH.md` §Validation Architecture (SC1–SC5 → test map).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework (unit/component)** | Vitest 4.x + @testing-library/react 16 + jsdom |
| **Framework (E2E)** | @playwright/test 1.61 (Chromium-only), `webServer` boots `aura serve` on loopback |
| **Framework (Go embed)** | Go stdlib `testing` + `net/http/httptest` against `internal/webui` handler |
| **Config files** | `web/vitest.config.ts`, `web/playwright.config.ts`, `web/eslint.config.js`, `web/tsconfig.json` (all Wave 0 — none exist yet) |
| **Quick run command** | `cd web && npm run lint && npm run typecheck && npm run test` (+ `go test ./internal/webui/`) |
| **Full suite command** | `cd web && npm ci && npm run build && git diff --exit-code -- dist/ && npm run test && npm run test:e2e` (+ `make quality` for the Go embed package) |
| **Estimated runtime** | ~25–40 s quick; full suite + E2E + Docker smoke runs in CI |

---

## Sampling Rate

- **After every task commit:** `cd web && npm run lint && npm run typecheck && npm run test` (sub-30s) + `go test ./internal/webui/`
- **After every plan wave:** full suite (`npm ci && npm run build && git diff --exit-code -- dist/ && npm run test && npm run test:e2e`) + `make quality`
- **Before `/gsd-verify-work`:** all 4 web CI jobs + the Go gates green on the PR; Docker build smoke passes
- **Max feedback latency:** ~30 seconds (quick run)

---

## Per-Task Verification Map

> Plan/task IDs filled by the planner. Every task maps to an `<automated>` verify or a Wave 0 dependency. SC→test mapping is locked in `23-RESEARCH.md` §Validation Architecture (SC1–SC5 table).
> Wave numbering here is the plan `wave:` frontmatter (1/2/3). The Nyquist "Wave 0" gate configs + RED test stubs all live in plan 23-01 (wave 1) — they are created failing before 23-02/23-03 make them green.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 23-01-T1 | 01 | 1 | FND-01 | T-23-SC | supply-chain legitimacy gate (slopcheck + npmjs verify) | doc + checkpoint | reviewer sign-off on `23-RESEARCH.md` + `slopcheck scan --pkg npm eslint-plugin-import-x` | ✅ RESEARCH | ⬜ pending |
| 23-01-T2 | 01 | 1 | FND-03 | T-23-SC | committed lockfile pins resolved versions; `--max-warnings=0` | lint/type config | `cd web && npx tsc --noEmit -p tsconfig.json && grep -q -- '--max-warnings=0' web/package.json` | ❌ W0 | ⬜ pending |
| 23-01-T3 | 01 | 1 | FND-02 | T-23-02 | `http.FileServerFS` over `embed.FS` (no traversal) | go httptest + RED stubs | `go test ./internal/webui/ -count=1 && bash scripts/agui_boundary_check.sh` | ❌ W0 | ⬜ pending |
| 23-02-T1 | 02 | 2 | FND-04 | T-23-04 | localStorage→attribute (no injection; dark/operator fallback) | unit/generator | `cd web && node tokens/generate-theme.mjs && grep -q '@theme' web/src/styles/theme.css` | ❌ W0 | ⬜ pending |
| 23-02-T2 | 02 | 2 | FND-05 | T-23-DEFER-COPY | Copy-Contract no-hero-text blocklist (SC4) | component (Vitest/RTL) | `cd web && npm run lint && npm run typecheck && npm run test` | ❌ W0 | ⬜ pending |
| 23-02-T3 | 02 | 2 | FND-02 | T-23-01 | committed dist tracked + byte-stable (no sourcemaps) | build + embed | `cd web && npm run build && ! git check-ignore -q web/dist/index.html && go test ./internal/webui/ -count=1` | ❌ W0 | ⬜ pending |
| 23-03-T1 | 03 | 3 | FND-02 | T-23-02 | additive mount; AG-UI routes keep priority; no SPA-fallback | go httptest | `go test ./cmd/aura/ -run TestServeWebui -count=1 && bash scripts/agui_boundary_check.sh` | ❌ W0 | ⬜ pending |
| 23-03-T2 | 03 | 3 | FND-03, FND-06 | T-23-01, T-23-SC | freshness tamper-evidence; `npm ci` pinned; zero-warning gate | CI gate + E2E | `bash scripts/web_dist_freshness.sh` + `cd web && npm run test:e2e` (CI: `web-lint`/`web-test`/`web-dist-freshness`/`web-e2e`) | ❌ W0 | ⬜ pending |
| 23-03-T3 | 03 | 3 | FND-06 | T-23-01 | webbuild Node never in runtime; reproducible rebuild | docker build | `docker build -f docker/aura/Dockerfile .` (webbuild stage → dist; runtime smoke `curl 127.0.0.1:9080/`) | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

**SC → task coverage:** SC1→23-01-T1 · SC2→23-01-T3/23-02-T1/23-02-T3/23-03-T1/23-03-T2 (E2E) · SC3→23-01-T2/23-03-T2 · SC4→23-02-T2 (+E2E 23-03-T2) · SC5→23-03-T2/23-03-T3.

---

## Wave 0 Requirements

The validating tests + gate configs must exist (failing/red) before scaffold tasks assert against them. **All created in plan 23-01:**

- [ ] `web/eslint.config.js`, `web/.prettierrc.json`, `web/tsconfig.json` (+ `tsconfig.node.json`) — the zero-warning gate configs (SC3) — 23-01-T2
- [ ] `web/vitest.config.ts` + `web/src/__tests__/AppShell.test.tsx` — brand renders + NO marketing-hero text + basic shell render (SC4) — 23-01-T2/T3
- [ ] `web/playwright.config.ts` + `web/e2e/shell.spec.ts` — theme+density applied before paint, brand, Copy-Contract assertion, by booting the binary (SC2/SC4) — 23-01-T2/T3
- [ ] `internal/webui/embed.go` + `internal/webui/embed_test.go` — `//go:embed all:dist` embed → serve → render via httptest (SC2) — 23-01-T3
- [ ] Framework install (`npm install -D …` block from RESEARCH.md) — 23-01-T2
- [ ] 4 path-filtered web jobs added to `.github/workflows/ci.yml` + `webbuild` stage in `docker/aura/Dockerfile` (SC3/SC5) — 23-03-T2/T3
- [ ] `.gitignore` negation for `web/dist/` (so the committed dist is tracked — D-05) — 23-01-T2

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| FND-01 decision record is approved | FND-01 / SC1 | The decision record IS the artifact (`23-RESEARCH.md`) — no automated test asserts a doc is "approved"; operator/reviewer sign-off | Reviewer reads `23-RESEARCH.md`, confirms it locks linter/formatter/tokens/layout/pipeline/harness, signs off (23-01-T1 checkpoint) |
| Dark-operator palette matches design intent | FND-04 / SC2 | Hex values (A-PALETTE) are a designed proposal vs ux-spec §147, not a Figma export; visual judgement | Operator views the booted shell, confirms the industrial dark-operator look (no abstract sphere, no marketing copy) matches §147/§350 |
| Node-24 Docker build smoke (no Node-for-SPA in runtime) | FND-06 / SC5 | Full `docker build` is a CI/operator Gate-3 run, not a per-task sub-30s check | `docker build -f docker/aura/Dockerfile .`; run the image; `curl 127.0.0.1:9080/` returns the shell HTML; confirm the runtime stage adds no Node for the SPA |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (configs + test stubs + CI/Docker) — all in 23-01, CI/Docker in 23-03
- [x] No watch-mode flags (CI runs are one-shot, `--run`/`--max-warnings=0`)
- [x] Feedback latency < 30s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** planner-complete (operator sign-off of the 23-01-T1 decision-record checkpoint pending at execution)
