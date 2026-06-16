---
status: partial
phase: 23-frontend-infrastructure-industrial-foundation
source: [23-VERIFICATION.md]
started: "2026-06-16T11:25:25Z"
updated: "2026-06-16T11:25:25Z"
---

## Current Test

[awaiting human testing]

## Tests

### 1. web-dist-freshness byte-canonical proof (first Linux Node-24 CI run)
expected: On the first push, the `web-dist-freshness` CI job (`bash scripts/web_dist_freshness.sh`) runs `npm ci && npm run build` on Linux Node 24 and `git diff --exit-code -- internal/webui/dist/` is empty — i.e. the committed dist equals a fresh Linux build. NOTE: the committed dist was built on Windows Node 22, so this gate may legitimately RED on its first run; the documented reconciliation is to recommit the CI-built `internal/webui/dist/` once. Gate logic + path are verified correct locally (`bash -n` clean, diffs `internal/webui/dist/`).
result: [pending]

### 2. Playwright E2E end-to-end (live aura serve over docker-compose Postgres + Chromium)
expected: The `web-e2e` CI job provisions docker-compose Postgres, runs `aura db migrate`, builds a fresh dist + the `aura` binary, installs Chromium, and `web/e2e/shell.spec.ts` passes against a live `aura serve`: `data-theme`/`data-density` set on `<html>` of the first response (theme-before-paint), the Logo brand visible, and no marketing-hero text. The Go `cmd/aura/serve_webui_test.go` httptest is the deterministic local proxy (GREEN — asserts `/`→200 text/html shell with `data-theme`, `/healthz`→AG-UI priority, bogus→404); the browser render is the remaining live proof.
result: [pending]

## Summary

total: 2
passed: 0
issues: 0
pending: 2
skipped: 0
blocked: 0

## Gaps
