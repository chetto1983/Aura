---
status: complete
phase: 31-stabilization-ci-unblock
source: [31-01-SUMMARY.md, 31-02-SUMMARY.md, 31-03-SUMMARY.md]
started: 2026-06-29T19:58:37Z
updated: 2026-06-29T20:01:00Z
---

## Current Test

[testing complete]

## Tests

### 1. QUAL-01 stabilization baseline gates green (C1 / C2 / C4)
expected: |
  C1 file-size cap ≤600 LOC exits 0; C2 committed embedded Vite bundle byte-identical
  to a fresh Node-24 build (dist clean); C4 frontend vitest coverage ≥85% on all four
  thresholds (branches binding at 85.62%). No source mutation — verify-only baseline.
result: pass
evidence: "check-file-size.sh exit 0; dist diff clean; vitest 4/4 ≥85 (branches 85.62%, 31-01)"

### 2. F-015 CI hygiene — owned-package gates + failable lint (C3)
expected: |
  The 4 raw root `./...` Go CI gates source `$(bash scripts/go_packages.sh)`; the
  check_ci_go_packages.sh lint finds zero raw root `./...` in .github/workflows; its
  negative self-test PROVES the lint can fail (plants `go test ./...` → lint exit 1);
  both windows-unit steps carry `shell: bash`.
result: pass
evidence: "lint exit 0 (no raw ./...); self-test PASS (planted ./... -> exit 1); 8 helper-sourced; 2 shell:bash"

### 3. SEC-08 SSRF remediation — CodeQL acceptance (D4)
expected: |
  CodeQL go/request-forgery alert at internal/mcp/http_client.go resolved (dismissed as
  false-positive, consistent with sibling internal/web/fetcher_image.go:52). Runtime
  guard already unit-verified (D1/D2/D3). Operator confirms the dismissal-as-resolution
  decision (deviation from literal "Fixed" AC, documented in 31-VALIDATION.md).
result: pass
evidence: "alert #18 http_client.go:229 state=dismissed reason='false positive'; operator accepted 2026-06-29"

### 4. SSRF guard: classify + guardEndpoint (D1)
expected: MCP-local classify (Unmap-first, every block class) + guardEndpoint (unconditional scheme/metadata/IMDS barrier, dev-permissive vs enforce policy, allow-list, fail-closed)
result: pass
source: automated
coverage_id: D1

### 5. OpenHTTP routes cfg.URL through guardEndpoint (D2)
expected: OpenHTTP routes cfg.URL through guardEndpoint before c.endpoint; dev default permits loopback (existing httptest regression stays green — C5b)
result: pass
source: automated
coverage_id: D2

### 6. Enforce-only hardened transport (D3)
expected: DialContext pins the classified IP + net.Dialer.Control re-classifies the post-resolution IP (DNS-rebinding defense)
result: pass
source: automated
coverage_id: D3

## Summary

total: 6
passed: 6
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

[none yet]
