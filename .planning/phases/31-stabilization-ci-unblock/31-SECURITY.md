---
phase: 31
slug: stabilization-ci-unblock
security_audit_date: 2026-06-29
auditor: gsd-secure-phase
asvs_level: 2
threats_total: 11
threats_mitigated: 7
threats_accepted: 4
threats_open: 0
---

# Phase 31 — Security Audit Report

**Phase:** 31 — Stabilization & CI Unblock
**Requirements audited:** QUAL-01, F-015, SEC-08
**Plans audited:** 31-01-PLAN.md, 31-02-PLAN.md, 31-03-PLAN.md
**ASVS Level:** 2
**Result:** SECURED — 0 open threats

---

## Threat Verification

### 31-01 Threat Register (QUAL-01 baseline)

| Threat ID | Category | Disposition | Verification | Evidence |
|-----------|----------|-------------|--------------|---------|
| T-31-VERIFY-01 | Repudiation (assurance) | mitigate | CLOSED | `scripts/check-file-size.sh:63` exits 1 on violation, line 66 exits 0 on clean; no `$CI`-gated skip branch. `git diff --exit-code` exits non-zero on dist drift. `vitest` exits non-zero below 85% thresholds (vitest.config.ts). All three gates ran live with concrete counts (31-01-SUMMARY §Gate Evidence). |
| T-31-C1/C2/C4-SEC | — | N/A | N/A | No untrusted runtime input crosses a trust boundary in this plan. Verify-only assertions over committed artifacts. |
| T-31-SC (31-01) | Tampering | accept | ACCEPTED | `npm ci` installs strictly from the committed lockfile; no new packages introduced; no `go get`. 0 vulnerabilities reported. Package Legitimacy Audit N/A. Logged below in Accepted Risks. |

### 31-02 Threat Register (F-015 CI hygiene)

| Threat ID | Category | Disposition | Verification | Evidence |
|-----------|----------|-------------|--------------|---------|
| T-31-CI-01 | Repudiation / DoS-of-assurance | mitigate | CLOSED | `scripts/check_ci_go_packages.sh` (created, executable): greps `.github/workflows/` for anchored `(^|[[:space:]])\./\.\.\.([[:space:]]|$)`, exits 1 on match with F-015 message. `scripts/check_ci_go_packages_test.sh`: plants `go test ./...` fixture, asserts lint exits non-zero (no-skip-as-green). Both wired into `build-and-lint` job (`ci.yml:49,52`). The 4 owned Go gates now source `$(bash scripts/go_packages.sh)`: `ci.yml:96` (unit-test race), `ci.yml:124` (windows-unit build), `ci.yml:136` (windows-unit test), `ci.yml:189` (vulncheck). |
| T-31-CI-WIN | Tampering (silent no-op) | mitigate | CLOSED | `ci.yml:123` `shell: bash` on the windows-unit go-build step; `ci.yml:127` `shell: bash` on the windows-unit go-test step. Both lines present and confirmed by grep (count=2). Linux unit-test/vulncheck steps carry no superfluous `shell:` (already bash). |
| T-31-CI-SSRF | — | N/A | N/A | No network or SSRF surface in this plan. SSRF owned by T-31-SSRF-* in 31-03. |
| T-31-SC (31-02) | Tampering | accept | ACCEPTED | No package install; pre-existing `@latest` govulncheck/deadcode pins not modified (SEC-05/Phase 40 scope). Package Legitimacy Audit N/A. Logged below. |

### 31-03 Threat Register (SEC-08 SSRF / CWE-918)

| Threat ID | Category | Disposition | Verification | Evidence |
|-----------|----------|-------------|--------------|---------|
| T-31-SSRF-01 | Info Disclosure / Tampering / EoP | mitigate | CLOSED | `internal/mcp/http_client.go:70` calls `guardEndpoint(ctx, cfg.URL, cfg.Enforce, cfg.AllowHosts, net.DefaultResolver)` before `c.endpoint` is assigned. `internal/mcp/ssrf.go:45` `allowedSchemes` map; `ssrf.go:109-111` unconditional scheme check; `ssrf.go:35-41` `metadataHostBlocklist`; `ssrf.go:113-115` unconditional hostname block; `ssrf.go:128-136` per-IP classify loop with `enforce || metadataReason(reason)` gate. `http_client.go:85` assigns `validated.String()` to `c.endpoint`. |
| T-31-SSRF-02 | Tampering (DNS-rebinding) | mitigate | CLOSED | `internal/mcp/transport_ssrf.go:37` wires `Control: hd.control` into `net.Dialer`; `transport_ssrf.go:57-79` `dialContext` resolves, fail-closes on ANY blocked IP, then dials only the pinned IP literal; `transport_ssrf.go:84-96` `control` re-classifies the post-resolution ip:port and returns an error on a rebind to a blocked range. Hardened client installed at `http_client.go:77-80` only when `cfg.Enforce && cfg.Client==nil`. |
| T-31-SSRF-03 | Info Disclosure (IPv4-mapped IPv6) | mitigate | CLOSED | `internal/mcp/ssrf.go:56` `ip = ip.Unmap()` executes unconditionally before any branch test; the link-local check at `ssrf.go:60-61` therefore catches `::ffff:169.254.169.254` (collapsed to `169.254.169.254` by Unmap). |
| T-31-SSRF-04 | Info Disclosure (cloud-metadata) | mitigate | CLOSED | Pre-resolution: `ssrf.go:35-41` blocks `metadata.google.internal`, `metadata.amazonaws.com`, `metadata.azure.com`, `kubernetes.default.svc`, `host.docker.internal` unconditionally at `ssrf.go:113-115`. Post-resolution: `ssrf.go:87` `metadataReason` returns true for `"link_local"`; `ssrf.go:133` `if enforce \|\| metadataReason(reason)` ensures link_local (169.254.x.x, IMDS) is blocked on EVERY policy branch, not just under enforce. |
| T-31-SSRF-DEV | Availability (dev) | accept | ACCEPTED | Under `!enforce` (default), loopback (`127.0.0.1`) and private-range compose-DNS sidecar IPs are permitted so httptest fixtures and sidecars (agent-memory 8091, whatsapp 8092, PIM 8093) remain reachable. `AURA_MCP_SSRF_ENFORCE` defaults off (`transport.go:38-45` `ssrfEnforceFromEnv()`). Phase 33 (PROF-01/PROF-04) will bind the knob to the runtime profile. Logged below. |
| T-31-SSRF-SC | Tampering (dependencies) | accept | ACCEPTED | Go stdlib only (`net/url`, `net/netip`, `net/http`, `net`, `syscall`). No new external package; no `go get`. Verified by import block in `ssrf.go:3-9` and `transport_ssrf.go:3-11`. Logged below. |

---

## Accepted Risks Log

| Risk ID | Threat | Rationale | Owner | Review Trigger |
|---------|--------|-----------|-------|----------------|
| AR-31-01 | T-31-SC (31-01): npm lockfile supply-chain | `npm ci` is restricted to the committed lockfile; 0 vulnerabilities at install time; no new package. Any future package addition will trigger a fresh legitimacy audit. | Phase 31 executor | Any `package-lock.json` change |
| AR-31-02 | T-31-SC (31-02): govulncheck/deadcode @latest pins | Pre-existing `@latest` install pins for govulncheck and deadcode are not pinned to a digest. Remediation is scoped to SEC-05 / Phase 40; the current pins have known-good behavior and are scanned by `make vuln`. | Phase 40 | SEC-05 / Phase 40 implementation |
| AR-31-03 | T-31-SSRF-DEV: loopback + private permitted under dev | `!enforce` policy intentionally allows loopback (127.x) and RFC-1918 private IPs so httptest fixtures and compose-DNS sidecar recipes remain reachable without configuration. Cloud-metadata/IMDS/link-local blocks are UNCONDITIONAL on every policy branch. Enforce mode is fully implemented and will be activated in Phase 33 (PROF-01/PROF-04). | Phase 33 | PROF-01/PROF-04 implementation |
| AR-31-04 | T-31-SSRF-SC: stdlib-only SSRF guard | No shared `internal/netguard` extraction; `internal/mcp/ssrf.go` is a local copy of `internal/web/ssrf.go`. Refactoring to a shared guard is QUAL-03 / Phase 32 scope. Until then, updates to the classifier must be ported to both copies. | Phase 32 | QUAL-03 implementation |
| AR-31-05 | SEC-08 CodeQL alert #18 dismissed as false positive | Post-push CodeQL re-scan (commit `c93e28fb`) kept alert #18 at `internal/mcp/http_client.go:229` open because CodeQL's taint-tracking model cannot represent a transport-level `DialContext`+`Control` guard as a dataflow sanitizer — the URL string still flows to `client.Do` via the `hardenedDialer`, which is opaque to the static analyser. Alert #18 was dismissed as `false_positive` with detailed justification, consistent with the already-dismissed sibling alert #12 at `internal/web/fetcher_image.go:52` (identical defense pattern). The runtime defense is complete, tested (TestGuardEndpoint/TestClassify/TestHardenedDialContextRebindFailClosed/TestHardenedControlRejectsRebind all PASS in WSL, including race detector), and lint-clean. Human operator explicitly accepted this disposition during UAT on 2026-06-29 (recorded in 31-VALIDATION.md §Manual-Only Verifications). | Phase 31 executor | Any change to `internal/mcp/http_client.go` or `internal/mcp/ssrf.go` |

---

## Unregistered Threat Flags

No unregistered threat flags. All three SUMMARY files explicitly report no new attack surface:
- 31-01-SUMMARY.md: verify-only plan, no new symbols or runtime surface.
- 31-02-SUMMARY.md: "No new threat surface introduced — no Threat Flags."
- 31-03-SUMMARY.md: "Threat Flags: None — no new security surface beyond the SEC-08 sink already in the plan's threat model (T-31-SSRF-01..04). Go stdlib only; no new dependency."

---

## Summary

| Metric | Count |
|--------|-------|
| Total threats in register | 11 |
| Disposition: mitigate | 7 |
| Disposition: accept | 4 |
| Disposition: N/A | 2 (not threat rows — informational notes in the register) |
| Mitigated (CLOSED with code evidence) | 7 / 7 |
| Open (BLOCKER) | 0 |
| Accepted risks logged | 5 |
| Unregistered flags | 0 |

**Conclusion:** All declared mitigations are present in the implemented code. The one CodeQL-visible gap (alert #18) is classified as a false positive per the verifier context and documented in AR-31-05 with operator acceptance. Phase 31 carries **0 open threats** and is clear to ship.
