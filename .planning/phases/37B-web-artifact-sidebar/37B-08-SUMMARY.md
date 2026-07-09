---
phase: 37B-web-artifact-sidebar
plan: 08
subsystem: web-cockpit
tags: [e2e, playwright, coverage, mutation, embed, ship-gate]
requires: [37B-07, 37B-06, 37B-05, 37B-04, 37B-03]
provides: [webart-08-validation-surface, embedded-bundle-fresh]
affects: [internal/webui/dist, web/e2e]
tech-stack:
  added: []
  patterns: [golden-replay-page-route, sseFromFrames, id-addressed-download, go-embed-bundle]
key-files:
  created: [web/e2e/artifacts.spec.ts]
  modified: [internal/webui/dist]
decisions:
  - "e2e is golden-replay (page.route on /agent/run + /api/assets) — no live agent turn, mirroring replay.spec.ts + assets.spec.ts"
  - "WEBART-06 marked complete (download surface shipped + unit + mutation-proven, downloadAll 100% killed); WEBART-08 live-e2e proof carried forward to CI (auth-gated on this host)"
metrics:
  tasks: 2
  files_changed: 120
  duration: ~50m
  completed: 2026-07-09
---

# Phase 37B Plan 08: Artifacts e2e + Coverage/Stryker Gate + Embedded-Bundle Rebuild Summary

Terminal WEBART-08 validation surface: a golden-replay Playwright e2e proving an `aura.artifact` delivery surfaces in the Artefatti panel and downloads via the id-addressed route, plus the phase ship gates (full web coverage 91.6% ≥85%, Stryker ≥70% killed on the two pure modules) and a fresh `internal/webui/dist` that `go build ./...` embeds green.

## What Shipped

### Task 1 — `web/e2e/artifacts.spec.ts` (commit `fb7902bc`)
A golden-replay Playwright spec (chromium + mobile-chrome) with two tests:
1. **Delivery → panel → download**: `page.route('**/agent/run')` streams an `aura.artifact` CUSTOM descriptor carrying `asset_id` (+ `tool_call_id`/`filename` so `isArtifactDescriptor` fires the onArtifact pump); `GET /api/assets?thread_id=` is mocked to return two accepted agent assets. Asserts (a) the panel **auto-opens** on the first delivery of the thread (37B-07 one-time auto-open), (b) both rows render **newest-first** (`report.xlsx` before `notes.txt`), (c) the row download control href is exactly `/api/assets/{id}/download` and clicking it hits a route interceptor + yields a browser download with `suggestedFilename() === 'report.xlsx'`, and (d) only the opaque id ever reaches the wire (negative-asserted: no `bucket`/`object_key`/`garage`/host-path segment — T-37B-16).
2. **404 download (negative)**: a non-owner/404 on the download route still addresses the asset purely by id and leaves no alternative unauthenticated / raw-path link in the DOM.

The spec mirrors the existing harness verbatim (`sseFromFrames` + `sseResponse` + `installConversationRoutes` from `replay.spec.ts`; asset fixtures + `/api/assets` mocking from `assets.spec.ts`), uses the shared `gotoAuthenticated` auth helper, and guards a counted `domAssertions >= 4` so a no-op run FAILS rather than passing green.

### Task 2 — Ship gates + `internal/webui/dist` rebuild (commit `76cd27ed`)
- **Full web unit suite**: `npm test` (vitest run --coverage) — **138 files / 1142 tests passed**, coverage **Statements 91.6% / Branches 86.06% / Functions 92% / Lines 93.42%** — all ≥85%, no threshold lowered, no `src/**` carve-out.
- **Stryker mutation spot-check** on the two pure modules (`--mutate artifactMeta.ts,downloadAll.ts`): overall **79.07% killed** (≥70 break threshold, exit 0):
  - `artifactMeta.ts`: **76.56%** killed (147/192; 45 survived — mostly string-literal mutations on the extension→category label map, tolerated: ≥70).
  - `downloadAll.ts`: **100.00%** killed (23/23).
- **Embedded-bundle rebuild**: `npm run build` (generate-theme + tsc -b + vite build) rebuilt the cockpit into `internal/webui/dist` (built in 6.35s); `go build ./...` **compiles the fresh embed green** (T-37B-20 stale-bundle mitigation). Diff: **119 files, +243/-139** (content-hashed asset filenames rotate; `index.html` + `sw.js` reference the fresh chunks).

## Validation Numbers (actual, recorded)

| Gate | Command | Result |
|------|---------|--------|
| Web unit coverage | `cd web && npm test` | 138/138 files, 1142/1142 tests; Stmts **91.6%** / Branch **86.06%** / Funcs **92%** / Lines **93.42%** (≥85%, exit 0) |
| Mutation — artifactMeta.ts | `npx stryker run --mutate …` | **76.56%** killed (147/192) |
| Mutation — downloadAll.ts | `npx stryker run --mutate …` | **100.00%** killed (23/23) |
| Mutation — aggregate | (break=70) | **79.07%** killed, exit 0 |
| Embedded bundle | `npm run build` → `go build ./...` | build 6.35s; **go build exit 0** with fresh dist (119 files, +243/-139) |
| tsc / eslint / prettier (spec) | `tsc --noEmit` / `eslint --max-warnings=0` / `prettier --check` | all clean |

## Playwright e2e Outcome — RAN to the auth gate, live run CARRIED FORWARD

The e2e was executed on this Windows host against the **live `aura serve`** already running on `127.0.0.1:9080` (`/healthz` → HTTP 200): `AURA_E2E_ORIGIN=http://127.0.0.1:9080 npx playwright test e2e/artifacts.spec.ts --project=chromium`. Chromium launched, reached the live serve, and the run failed **only at the shared credential gate** in `web/e2e/auth.ts`:

> `E2E Authula auth required, but AURA_E2E_AUTHULA_EMAIL/PASSWORD are missing from env/.env`

Root cause (verified): the repo `.env` carries `AURA_AUTHULA_OPERATOR_EMAIL` / `_PASSWORD` / `_TOTP_SECRET` as **empty** keys (provisioned at deploy-time, not committed for security), so the auth helper — by design (no-skip-as-green) — THROWS rather than silent-skipping. This is **not** a spec defect and **not** a browser-launch failure: the spec typechecks, lints, formats, and the browser launches + reaches the live serve. It is the same auth-gated tier every sibling e2e (`replay.spec.ts`, `assets.spec.ts`, `displays.spec.ts`, …) depends on, matching the Phase 36/37 precedent of deferring live browser tiers to the CI `web-e2e` job (which exports the Authula operator creds).

**Documented must-run (carried forward):** on the CI `web-e2e` job (or any host with `AURA_AUTHULA_OPERATOR_EMAIL`/`_PASSWORD`/`_TOTP_SECRET` provisioned + the stack up):
```
cd web && npx playwright test e2e/artifacts.spec.ts   # chromium + mobile-chrome
```

## Requirements Disposition

- **WEBART-05** — already `[x]` (panel render, shipped 37B-06/07; unit-proven).
- **WEBART-06** — **marked complete**: the id-addressed download action (`ArtifactRow` → `GET /api/assets/{id}/download`) + the sequential "Scarica tutto" (`downloadAll` + `ArtifactsPanel`) are shipped and **unit + mutation-proven** (downloadAll.ts **100%** mutation-killed; no-raw-path asserted in code + the e2e spec). No e2e-run clause in its text; its automated proof is genuinely satisfied.
- **WEBART-07** — already `[x]` (single derived view, shipped 37B-05/06).
- **WEBART-08** — **CARRIED FORWARD (not marked complete)**: its requirement text explicitly names *"a Playwright e2e (artifact appears in the panel + download)"* as a required coverage artifact. The React unit tests (panel render + download-all) exist, web coverage is **91.6% ≥85%**, non-regression (inline `local_artifact` chip + CLI host-path degrade) holds, and the e2e is **written + compiles**, but the **live e2e run is auth-gated on this host**. Marking it `[x]` would over-claim the e2e-run clause (CLAUDE.md no-skip-as-green / DoD "validate E2E on real scenario"). Its final proof is the CI `web-e2e` run above.

## Deviations from Plan

None — plan executed as written. The only carry-forward is the auth-gated live e2e run (WEBART-08's final proof), documented above rather than faked green.

## Known Stubs

None. No hardcoded empty values, placeholders, or unwired data sources introduced.

## Threat Flags

None — no new network endpoints, auth paths, or trust-boundary surfaces beyond the plan's `<threat_model>` (T-37B-20 stale-bundle mitigated by the dist rebuild; T-37B-21 coverage-evasion avoided — no threshold lowered; T-37B-08 XSS sandbox unchanged).

## Self-Check: PASSED

- web/e2e/artifacts.spec.ts — FOUND
- internal/webui/dist (rebuilt) — FOUND
- 37B-08-SUMMARY.md — FOUND
- commit fb7902bc (e2e spec) — FOUND
- commit 76cd27ed (dist rebuild) — FOUND
