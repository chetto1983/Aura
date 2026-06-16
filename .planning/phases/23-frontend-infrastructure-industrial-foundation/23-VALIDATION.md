---
phase: 23
slug: frontend-infrastructure-industrial-foundation
status: draft
nyquist_compliant: false
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

> Filled by the planner / executor once PLAN.md task IDs exist. Every task must map to an `<automated>` verify or a Wave 0 dependency below. SC→test mapping is locked in `23-RESEARCH.md` §Validation Architecture (SC1–SC5 table).

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 23-XX-XX | XX | 0 | FND-XX | — | N/A | unit/e2e | `{from RESEARCH SC map}` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

The validating tests + gate configs must exist (failing/red) before scaffold tasks assert against them:

- [ ] `web/eslint.config.js`, `web/.prettierrc.json`, `web/tsconfig.json` (+ `tsconfig.node.json`) — the zero-warning gate configs (SC3)
- [ ] `web/vitest.config.ts` + `web/src/__tests__/AppShell.test.tsx` — brand renders + NO marketing-hero text + basic shell render (SC4)
- [ ] `web/playwright.config.ts` + `web/e2e/shell.spec.ts` — theme+density applied before paint, brand, Copy-Contract assertion, by booting the binary (SC2/SC4)
- [ ] `internal/webui/embed.go` + `internal/webui/embed_test.go` — `//go:embed all:dist` embed → serve → render via httptest (SC2)
- [ ] Framework install (`npm install -D …` block from RESEARCH.md) — Wave 0 scaffold task
- [ ] 4 path-filtered web jobs added to `.github/workflows/ci.yml` + `webbuild` stage in `docker/aura/Dockerfile` (SC3/SC5)
- [ ] `.gitignore` negation for `web/dist/` (so the committed dist is tracked — D-05)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| FND-01 decision record is approved | FND-01 / SC1 | The decision record IS the artifact (`23-RESEARCH.md`) — no automated test asserts a doc is "approved"; operator/reviewer sign-off | Reviewer reads `23-RESEARCH.md`, confirms it locks linter/formatter/tokens/layout/pipeline/harness, signs off |
| Dark-operator palette matches design intent | FND-04 / SC2 | Hex values (A-PALETTE) are a designed proposal vs ux-spec §147, not a Figma export; visual judgement | Operator views the booted shell, confirms the industrial dark-operator look (no abstract sphere, no marketing copy) matches §147/§350 |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references (configs + test stubs + CI/Docker)
- [ ] No watch-mode flags (CI runs are one-shot, `--run`/`--max-warnings=0`)
- [ ] Feedback latency < 30s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
