---
phase: 37B-web-artifact-sidebar
plan: 08
type: execute
wave: 6
depends_on: ["37B-07"]
files_modified:
  - web/e2e/artifacts.spec.ts
  - internal/webui/dist
autonomous: true
requirements: [WEBART-08]
must_haves:
  truths:
    - "a Playwright e2e drives a golden-replay aura.artifact frame → the asset appears in the Artefatti panel → a download click hits GET /api/assets/{id}/download"
    - "the full web unit suite passes with coverage ≥85% across src/**"
    - "Stryker kills ≥70% on artifactMeta.ts and downloadAll.ts"
    - "internal/webui/dist is rebuilt from the current web bundle and committed (embedded-bundle freshness — 37A precedent)"
    - "non-regression: the inline local_artifact chip still renders; CLI/no-identity degrades to host-path behavior"
  artifacts:
    - path: "web/e2e/artifacts.spec.ts"
      provides: "e2e: artifact in panel + download route"
      contains: "aura.artifact"
  key_links:
    - from: "web/e2e/artifacts.spec.ts"
      to: "/api/assets"
      via: "download route assertion + golden-replay frame"
      pattern: "/api/assets"
  prohibitions:
    - "MUST NOT require a live agent turn — use golden-replay page.route (mirror replay.spec.ts sseFromFrames + assets.spec.ts fixtures)"
    - "MUST NOT lower the 85% coverage threshold or carve out src/** to pass the gate — fix the tests instead"
    - "MUST NOT ship without rebuilding internal/webui/dist (go build embeds it; a stale bundle ships the old UI)"
---

<objective>
Close the phase gate: the Playwright e2e (artifact appears in panel + download), the full-suite coverage gate (≥85% web), the Stryker mutation spot-check (≥70% on the two pure modules), and the embedded-bundle rebuild (`internal/webui/dist`) that `go build` compiles into the binary. This is the WEBART-08 validation surface + the ship-readiness step.

Purpose: Prove the phase end-to-end and ensure the built cockpit bundle is embedded fresh (37A precedent).
Output: `e2e/artifacts.spec.ts`, a green full unit suite ≥85%, Stryker ≥70% on the mutation targets, and a rebuilt/committed `internal/webui/dist`.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37B-web-artifact-sidebar/37B-RESEARCH.md
@.planning/phases/37B-web-artifact-sidebar/37B-VALIDATION.md
@web/e2e/replay.spec.ts
@web/e2e/assets.spec.ts
</context>

<artifacts_produced>
This plan produces:
- `web/e2e/artifacts.spec.ts` — golden-replay e2e (chromium + mobile-chrome projects): `aura.artifact` frame → panel row → download route hit.
- A rebuilt `internal/webui/dist` (embedded web bundle) committed for `go build` freshness.
</artifacts_produced>

<tasks>

<task type="auto">
  <name>Task 1: Playwright e2e — artifact appears in the Artefatti panel + download</name>
  <files>web/e2e/artifacts.spec.ts</files>
  <read_first>
    - web/e2e/replay.spec.ts — the `sseFromFrames` golden-replay + `page.route` SSE-mock harness to mirror (no live agent turn).
    - web/e2e/assets.spec.ts — the asset fixtures + `/api/assets` route mocking + download assertion conventions.
    - web/e2e/auth.ts — the authenticated-session e2e helper.
    - web/playwright.config.ts — the chromium + mobile-chrome project matrix.
  </read_first>
  <action>
    Create `web/e2e/artifacts.spec.ts`: using the golden-replay harness (`page.route` on `/agent/run` streaming an `aura.artifact` CUSTOM frame carrying an `asset_id`, mirroring `replay.spec.ts`) plus a mocked `GET /api/assets?thread_id=` returning that asset (mirroring `assets.spec.ts`), assert (a) the Artefatti panel auto-opens and lists the delivered asset row newest-first, (b) clicking the row's download control issues a request to `GET /api/assets/{id}/download` (assert via a route interceptor), and (c) the mobile-chrome project renders the right Drawer variant without a layout break. Add a negative case: a non-owner/404 route on the download leaves no unauthenticated surface. Run in the chromium + mobile-chrome projects.
  </action>
  <acceptance_criteria>
    - `web/e2e/artifacts.spec.ts` references `aura.artifact` and `/api/assets` and asserts a download-route hit.
    - `cd web && npx playwright test e2e/artifacts.spec.ts` passes (chromium + mobile-chrome).
    - the spec uses golden-replay `page.route` (no live agent turn / live backend required).
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura/web && npx playwright test e2e/artifacts.spec.ts && echo E2E_ARTIFACTS_OK</automated>
  </verify>
  <done>e2e proves an aura.artifact delivery surfaces in the panel and downloads via the auth route, on desktop + mobile; passes.</done>
</task>

<task type="auto">
  <name>Task 2: Full coverage gate + Stryker spot-check + rebuild internal/webui/dist</name>
  <files>internal/webui/dist</files>
  <read_first>
    - web/vitest.config.ts:22,28-33 — the `src/**` include + the 85% thresholds (the gate this must clear).
    - web/vitest.stryker.config.ts — the mutation target list (artifactMeta + downloadAll added in plan 03).
    - .planning/phases/37B-web-artifact-sidebar/37B-VALIDATION.md — the phase-gate sampling rate (full npm test + e2e + Stryker + dist rebuild).
    - the 37A SUMMARY / build docs for the `internal/webui/dist` rebuild command (the embed source `go build ./...` compiles).
  </read_first>
  <action>
    Run `cd web && npm test` (= `vitest run --coverage`) and confirm ≥85% across `src/**`; if any new `src/chat/artifacts/**` file drags the aggregate, add the missing pure/mocked tests (do NOT lower the threshold or carve out files — Pitfall 4). Run `cd web && npx stryker run --config vitest.stryker.config.ts` (or the project's Stryker invocation) and confirm ≥70% killed on `artifactMeta.ts` and `downloadAll.ts`. Then rebuild the embedded web bundle into `internal/webui/dist` using the project's build command (the same step 37A performed so `go build ./...` embeds the current UI) and confirm `go build ./...` succeeds with the fresh bundle. Commit the rebuilt `internal/webui/dist`.
  </action>
  <acceptance_criteria>
    - `cd web && npm test` exits 0 with coverage ≥85% (statements/branches/functions/lines) — no threshold lowered, no `src/**` carve-out.
    - Stryker reports ≥70% killed on `artifactMeta.ts` and `downloadAll.ts`.
    - `internal/webui/dist` is rebuilt from the current bundle; `go build ./...` succeeds.
    - existing `LocalArtifactDisplay` + displays e2e stay green (inline-chip non-regression, WEBART-08).
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura/web && npm test && cd D:/Repo/Aura && go build ./... && echo FULL_GATE_OK</automated>
  </verify>
  <done>Full web suite ≥85%, Stryker ≥70% on the pure modules, internal/webui/dist rebuilt + committed, go build green, inline chip non-regressed.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| built web bundle → embedded binary | A stale `internal/webui/dist` ships an old UI (the panel never reaches users despite green source) |
| test gate → ship decision | A lowered coverage threshold silently ships untested preview/XSS surfaces |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37B-20 | Tampering (stale-bundle ship) | internal/webui/dist embed | mitigate | Rebuild + commit the embedded bundle; `go build ./...` must succeed with the fresh dist (37A precedent) |
| T-37B-21 | Repudiation / coverage-evasion | vitest 85% gate | mitigate | Forbid threshold-lowering / file carve-outs; keep pure logic 100% + mock heavy renderers to hold the aggregate |
| T-37B-08 | Elevation (XSS regression) | HtmlPreview sandbox | mitigate | e2e + unit assert `allow-scripts` present / `allow-same-origin` absent stays true through the gate |
</threat_model>

<verification>
- `npx playwright test e2e/artifacts.spec.ts` green (chromium + mobile-chrome).
- `npm test` green with coverage ≥85%; Stryker ≥70% on artifactMeta/downloadAll.
- `go build ./...` green with a freshly rebuilt `internal/webui/dist`.
</verification>

<success_criteria>
- The phase is proven end-to-end (panel + download, desktop + mobile) with ≥85% web coverage, ≥70% mutation on the pure modules, and a fresh embedded bundle.
- The inline chip + CLI/no-identity degradation remain non-regressed (WEBART-08).
</success_criteria>

<output>
Create `.planning/phases/37B-web-artifact-sidebar/37B-08-SUMMARY.md` when done.
</output>
</content>
