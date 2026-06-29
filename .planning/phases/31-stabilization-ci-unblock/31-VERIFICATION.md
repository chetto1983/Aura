---
phase: 31-stabilization-ci-unblock
verified: 2026-06-29T22:00:00Z
status: passed
score: 5/5 must-haves verified
overrides_applied: 0
overrides:
  - must_have: "The critical CodeQL go/request-forgery (SSRF) at internal/mcp/http_client.go is remediated and the alert resolves (SEC-08)"
    reason: "Runtime SSRF guard fully implemented and unit-tested; CodeQL alert #18 at http_client.go:229 dismissed as false positive (consistent with already-dismissed sibling internal/web/fetcher_image.go:52 — CodeQL cannot model the DialContext transport-level classify as a dataflow sanitizer). Operator explicitly accepted dismissal-as-resolution during UAT 2026-06-29. Deviation from literal 'Fixed' to 'Dismissed/false positive' is documented in 31-VALIDATION.md Manual-Only table."
    accepted_by: "operator (UAT 2026-06-29)"
    accepted_at: "2026-06-29T20:01:00Z"
---

# Phase 31: Stabilization & CI Unblock — Verification Report

**Phase Goal:** Clean working tree + green CI so every commit passes the pre-commit hooks and CI gates without the `--no-verify` workaround, and remediate the one critical CodeQL SSRF pulled forward from the security backlog.
**Verified:** 2026-06-29T22:00:00Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | No tracked production/test source file exceeds 600 LOC; `scripts/check-file-size.sh` exits 0 whole-tree | ✓ VERIFIED | Script exists (67 LOC, substantive), exempts sqlc-generated; ground truth exit 0; all new phase files under cap: ssrf.go=138, transport_ssrf.go=97, http_client.go=316 |
| 2 | `internal/webui/dist` rebuilt from `web/` and committed — no drift (web-dist-freshness) | ✓ VERIFIED | `internal/webui/dist/index.html` confirmed present; ground truth: `npm ci && npm run build` then `git diff --exit-code -- internal/webui/dist/` = exit 0 (built in 7.12 s, zero diff); 31-01-SUMMARY records no source mutation |
| 3 | Every Go build/test/vuln CI job sources `scripts/go_packages.sh`; CI lint rejects raw `./...` (F-015) | ✓ VERIFIED | `check_ci_go_packages.sh` (substantive lint) + `check_ci_go_packages_test.sh` (provably-failable negative self-test) created; ci.yml shows 8 uses of `$(bash scripts/go_packages.sh)` + 2 `shell: bash` entries; lint and self-test wired into `build-and-lint` before "Set up Go" (ci.yml lines 48–52) |
| 4 | Frontend coverage ≥85% on all four vitest thresholds; suite passes at that floor | ✓ VERIFIED | `web/vitest.config.ts` thresholds: statements=85, branches=85, functions=85, lines=85; measured result: S 91.3% / B 85.62% / F 91.36% / L 93.01%; branches is binding at 0.62 pp above floor |
| 5 | Critical CodeQL `go/request-forgery` (SSRF) at `internal/mcp/http_client.go` remediated; alert resolves (SEC-08) | ✓ VERIFIED (override) | `internal/mcp/ssrf.go` (138 LOC): full `classify` + `guardEndpoint` implementation; `internal/mcp/transport_ssrf.go` (97 LOC): DNS-rebinding defense via `hardenedDialer`; `OpenHTTP` wires `guardEndpoint` on cfg.URL before assigning `c.endpoint` (data-flow broken); unit tests pass in WSL; CodeQL alert #18 dismissed as false positive — operator-accepted deviation (see overrides) |

**Score:** 5/5 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `scripts/check-file-size.sh` | 600-LOC cap enforcement | ✓ VERIFIED | 67 LOC, substantive; exempts sqlc-generated, dist, node_modules, d.ts |
| `scripts/check_ci_go_packages.sh` | F-015 lint for raw `./...` in CI workflows | ✓ VERIFIED | 38 LOC; anchored token regex; excludes comment lines and `go list` enumeration; exits non-zero with "F-015" message |
| `scripts/check_ci_go_packages_test.sh` | Negative self-test proving lint can fail | ✓ VERIFIED | 83 LOC; plants `go test ./...` fixture via env-pointed dir; asserts lint exits non-zero with "F-015"; clean fixture asserts exit 0 |
| `scripts/go_packages.sh` | Owned-package list (excludes web/node_modules) | ✓ VERIFIED | 16 LOC; `go list ./...` filtered via awk to strip web/node_modules; yields 59 packages |
| `.github/workflows/ci.yml` | 4 raw `./...` gates swapped; `shell: bash` on windows-unit; F-015 lint wired | ✓ VERIFIED | 8 uses of `$(bash scripts/go_packages.sh)`; 2 `shell: bash`; F-015 lint + self-test as first two steps in `build-and-lint` |
| `internal/webui/dist/index.html` | Committed embedded Vite bundle | ✓ VERIFIED | File present; byte-identical to Node-24 fresh build (zero diff) |
| `web/vitest.config.ts` | Coverage thresholds 85/85/85/85 | ✓ VERIFIED | All four thresholds: statements=85, branches=85, functions=85, lines=85 |
| `internal/mcp/ssrf.go` | SSRF classify + guardEndpoint (SEC-08) | ✓ VERIFIED | 138 LOC; `classify` covers all block classes with Unmap-first; `guardEndpoint` applies unconditional scheme/metadata barrier + enforce-gated private block; injectable resolver seam for test isolation |
| `internal/mcp/ssrf_test.go` | Unit tests for classify + guardEndpoint | ✓ VERIFIED | TestClassify (every block class including mapped IMDS + IPv6 metadata); TestGuardEndpoint (24 rows: unconditional barriers, dev vs enforce, allow-list, mixed-set fail-closed, resolve-failure) |
| `internal/mcp/transport_ssrf.go` | DNS-rebinding hardened transport (enforce-only) | ✓ VERIFIED | 97 LOC; `hardenedDialer.dialContext` resolves+classifies+pins; `control` re-classifies post-resolution IP; `newHardenedHTTPClient` with DisableKeepAlives |
| `internal/mcp/transport_ssrf_test.go` | Unit tests for hardened dialer | ✓ VERIFIED | TestHardenedDialContextRebindFailClosed, TestHardenedDialContextDialsPinnedIP, TestHardenedControlRejectsRebind |
| `internal/mcp/http_client.go` | SSRF guard wired into OpenHTTP | ✓ VERIFIED | `OpenHTTP` calls `guardEndpoint(ctx, cfg.URL, cfg.Enforce, cfg.AllowHosts, net.DefaultResolver)` before assigning `validated.String()` to `c.endpoint` |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `cfg.URL` (OpenHTTP input) | `c.endpoint` (http.Client.Do target) | `guardEndpoint` in `http_client.go:70-86` | ✓ WIRED | Taint path broken: URL validated before assignment; scheme/metadata barrier unconditional |
| `ci.yml` Go gates | `scripts/go_packages.sh` | `$(bash scripts/go_packages.sh)` substitution | ✓ WIRED | 8 uses confirmed; unit-test, windows-unit build+test, govulncheck all sourced from helper |
| `build-and-lint` job | `check_ci_go_packages.sh` lint | Steps at ci.yml lines 48–52 (before "Set up Go") | ✓ WIRED | Lint runs before any Go gate; negative self-test runs first |
| `hardenedDialer.dialContext` | pinned-IP dial (no rebind) | DNS resolve → classify all records → `h.dial(ctx, network, JoinHostPort(pinned, port))` | ✓ WIRED | Rebind shape (public + private record) fails closed before any connect |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|--------------------|--------|
| `internal/mcp/http_client.go` OpenHTTP | `c.endpoint` | `guardEndpoint(cfg.URL)` → `validated.String()` | Yes — runtime endpoint URI flows through guard before use | ✓ FLOWING |
| `internal/mcp/ssrf.go` guardEndpoint | `ips` (resolved addresses) | `res.LookupNetIP(ctx, "ip", u.Hostname())` — real `net.DefaultResolver` in production | Yes — DNS resolution drives classify loop | ✓ FLOWING |
| `internal/mcp/transport_ssrf.go` dialContext | `pinned` (first clean IP) | `h.res.LookupNetIP` classify loop over all records | Yes — pinned IP from resolution, not from hostname string | ✓ FLOWING |

### Behavioral Spot-Checks

Step 7b: SKIPPED — cannot run `go test` against native Windows `.exe` per CLAUDE.md hard constraint. WSL execution evidence is captured in 31-03-SUMMARY.md as ground truth: `go test ./internal/mcp/ -count=1` → `ok ... 10.773s`; `-race` → `ok ... 15.074s`. The UAT confirms the same via operator-run WSL commands (6/6 PASS).

### Probe Execution

No `scripts/*/tests/probe-*.sh` files declared for this phase. Phase 31 is a stabilization+CI-gate phase; its acceptance gates are shell scripts run as CI steps, not standalone probes. N/A.

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|----------|
| QUAL-01 | 31-01-PLAN.md | File-size cap ≤600 LOC + dist freshness + frontend coverage ≥85% | ✓ SATISFIED | C1/C2/C4 all verified per ground truth and artifact inspection |
| F-015 | 31-02-PLAN.md | CI Go gates use owned-package list; raw `./...` CI lint present and provably failable | ✓ SATISFIED | `check_ci_go_packages.sh` + self-test created; ci.yml swapped; 8 helper uses confirmed |
| SEC-08 | 31-03-PLAN.md | CWE-918 SSRF guard on MCP outbound HTTP endpoint; CodeQL alert resolved | ✓ SATISFIED | Full `classify`+`guardEndpoint`+`hardenedDialer` implementation present; data-flow broken; CodeQL alert dismissed as false positive (operator-accepted deviation) |

### Anti-Patterns Found

Anti-pattern scan on all files modified by Phase 31:

| File | Pattern | Severity | Impact |
|------|---------|----------|--------|
| (none) | — | — | No TBD/FIXME/XXX/TODO/HACK/PLACEHOLDER found in any Phase 31 file |

Stub scan: `internal/mcp/ssrf.go` and `internal/mcp/transport_ssrf.go` contain no `return null`, `return {}`, `return []` patterns. `http_client.go` contains no empty implementations. All guard functions return real computed values.

### Human Verification Required

None. UAT was completed by the operator on 2026-06-29 with 6/6 tests passing. All acceptance criteria are machine-verifiable or have been operator-accepted. No further human checks are required to close the phase.

### Gaps Summary

No gaps. All five success criteria are delivered:

1. **600-LOC cap (C1):** `check-file-size.sh` exits 0; all phase-new files are well under cap.
2. **Dist freshness (C2):** `internal/webui/dist` byte-identical to a fresh Node-24 build; zero git diff.
3. **F-015 CI hygiene (C3):** lint + negative self-test created; all 4 raw `./...` CI gates sourced from `go_packages.sh`; windows-unit steps carry `shell: bash`.
4. **Frontend coverage (C4):** vitest 4/4 thresholds ≥85 (branches binding 85.62%).
5. **SEC-08 SSRF (C5):** `guardEndpoint` + `hardenedDialer` fully implemented and wired; data-flow taint path broken at the OpenHTTP seam; CodeQL alert resolved (dismissed as false positive, operator-accepted).

---

_Verified: 2026-06-29T22:00:00Z_
_Verifier: Claude (gsd-verifier)_
