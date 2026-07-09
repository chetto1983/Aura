---
status: testing
phase: 37B-web-artifact-sidebar
source: [37B-VERIFICATION.md]
started: "2026-07-09T05:12:32Z"
updated: "2026-07-09T05:12:32Z"
---

## Current Test

number: 1
name: WEBART-08 — live Playwright e2e (artifact appears in the Artefatti panel + downloads)
expected: |
  With real operator auth secrets present (AURA_AUTHULA_OPERATOR_EMAIL / _PASSWORD /
  _TOTP_SECRET), `cd web && npx playwright test e2e/artifacts.spec.ts` passes end-to-end:
  an agent-produced artifact appears in the right-side Artefatti panel of a live cockpit,
  and its download resolves via GET /api/assets/{id}/download. This is the sole remaining
  proof for WEBART-08 — every automated clause (web coverage ≥85%, React unit tests, panel
  render + download-all, non-regression) already passes; the spec is code-complete and
  launched Chromium against a live `aura serve` (/healthz 200) but stopped at the shared
  credential gate because this host's .env carries the operator secrets empty by design.
awaiting: user response / CI web-e2e run

## Tests

### 1. WEBART-08 live Playwright e2e
expected: |
  The artifacts.spec.ts e2e passes against a live server with real auth — an agent
  artifact renders in the Artefatti panel and downloads by asset_id (no host path).
  Run it via the CI `web-e2e` job (secrets provisioned there) or locally with real
  operator credentials set: `cd web && npx playwright test e2e/artifacts.spec.ts`.
result: [pending]

## Summary

total: 1
passed: 0
issues: 0
pending: 1
skipped: 0
blocked: 0

## Gaps
