---
status: resolved
phase: 37B-web-artifact-sidebar
source: [37B-VERIFICATION.md]
started: "2026-07-09T05:12:32Z"
updated: "2026-07-09T05:20:00Z"
---

## Current Test

number: 1
name: WEBART-08 — live Playwright e2e (artifact appears in the Artefatti panel + downloads)
expected: |
  With real operator auth secrets present, `cd web && npx playwright test e2e/artifacts.spec.ts`
  passes end-to-end: an agent-produced artifact appears in the right-side Artefatti panel of a
  live cockpit, and its download resolves via GET /api/assets/{id}/download.
awaiting: none — resolved

## Tests

### 1. WEBART-08 live Playwright e2e
expected: |
  The artifacts.spec.ts e2e passes against a live server with real auth — an agent
  artifact renders in the Artefatti panel and downloads by asset_id (no host path).
result: passed — 2026-07-09 live run, 4/4 green on chromium + mobile-chrome against a
  rebuilt aura container (image from HEAD with the H-01 + touch-download fixes) with real
  Authula credentials (no TOTP). Verified: panel auto-opens on delivery, lists newest-first
  (report.xlsx before notes.txt), download fetches /api/assets/{id}/download (download.url()
  asserted) with no object_key/bucket/host path leak, on desktop AND touch (mobile-chrome).
  Two defects the CI-only path never caught were fixed en route: the mobile-unreachable
  download (ArtifactRow opacity, commit 82c243f7) and the download-route-mock incompatibility
  (verify via Download.url(), same commit).

## Summary

total: 1
passed: 1
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps
