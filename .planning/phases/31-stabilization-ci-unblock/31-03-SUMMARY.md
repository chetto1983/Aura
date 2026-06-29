---
phase: 31-stabilization-ci-unblock
plan: 03
subsystem: infra
tags: [ssrf, cwe-918, codeql, mcp, net/netip, dns-rebinding, security]

# Dependency graph
requires:
  - phase: 31-01
    provides: verified QUAL-01 green baseline (file-size, dist-freshness, frontend coverage)
provides:
  - MCP-local SSRF guard (internal/mcp/ssrf.go) mirroring the CodeQL-clean internal/web classifier
  - guardEndpoint string-path barrier wired into OpenHTTP (scheme + cloud-metadata unconditional; private-range enforce-gated)
  - AURA_MCP_SSRF_ENFORCE env knob (default off → dev no-op, loopback/private sidecars reachable)
  - enforce-only hardened http.Client (DialContext pin + net.Dialer.Control DNS-rebinding defense)
affects: [phase-33-profiles, PROF-01, PROF-04, codeql-rescan, qual-03-netguard]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "MCP-local transplant of the internal/web SSRF classifier (no shared netguard extraction — QUAL-03/Phase-32 scope)"
    - "Profile-gated SSRF: unconditional scheme/metadata barrier + enforce-gated private-range block (dev-permissive zero value)"
    - "DNS-rebinding defense via DialContext IP-pin + net.Dialer.Control post-resolution re-classify"

key-files:
  created:
    - internal/mcp/ssrf.go
    - internal/mcp/ssrf_test.go
    - internal/mcp/transport_ssrf.go
    - internal/mcp/transport_ssrf_test.go
  modified:
    - internal/mcp/http_client.go
    - internal/mcp/transport.go

key-decisions:
  - "MCP-local copy of classify/guardEndpoint — no internal/web import, no shared internal/netguard (A4 RESOLVED → MCP-local)"
  - "Scheme allow-list + cloud-metadata hostname + link-local IP block are UNCONDITIONAL (the CodeQL taint barrier); only the private/loopback block is gated on AURA_MCP_SSRF_ENFORCE (default off)"
  - "classify mirrored faithfully from internal/web including the fd00:ec2::/32 branch that IsPrivate() shadows — kept for parity, blocked as 'private'"

patterns-established:
  - "Pattern: dev-permissive SSRF policy (loopback + compose-DNS private allowed) so httptest + sidecars stay reachable while metadata exfil is always blocked"
  - "Pattern: injectable resolver seam + injectable dialFunc so the guard and the hardened dialer are unit-tested with no real network (no-skip-as-green)"

requirements-completed: [SEC-08]

coverage:
  - id: D1
    description: "MCP-local SSRF guard: classify (Unmap-first, every block class) + guardEndpoint (unconditional scheme/metadata/IMDS barrier, dev-permissive vs enforce policy, allow-list, fail-closed)"
    requirement: "SEC-08"
    verification:
      - kind: unit
        ref: "internal/mcp/ssrf_test.go#TestClassify, internal/mcp/ssrf_test.go#TestGuardEndpoint"
        status: pass
    human_judgment: false
  - id: D2
    description: "OpenHTTP routes cfg.URL through guardEndpoint before c.endpoint; dev default permits loopback (existing http_client/http_client_extra httptest regression stays green — C5b)"
    requirement: "SEC-08"
    verification:
      - kind: unit
        ref: "go test ./internal/mcp/ -count=1 (full package, incl. loopback httptest)"
        status: pass
    human_judgment: false
  - id: D3
    description: "Enforce-only hardened transport: DialContext pins the classified IP + net.Dialer.Control re-classifies the post-resolution IP (DNS-rebinding defense)"
    requirement: "SEC-08"
    verification:
      - kind: unit
        ref: "internal/mcp/transport_ssrf_test.go#TestHardenedDialContextRebindFailClosed, #TestHardenedControlRejectsRebind, #TestHardenedDialContextDialsPinnedIP"
        status: pass
    human_judgment: false
  - id: D4
    description: "CodeQL go/request-forgery at internal/mcp/http_client.go resolves to Fixed (not Dismissed) on re-scan"
    requirement: "SEC-08"
    verification:
      - kind: other
        ref: "CodeQL re-scan post-merge → Security tab / gh api code-scanning/alerts"
        status: unknown
    human_judgment: true
    rationale: "C5c is a post-push GitHub-hosted CodeQL re-scan, NOT locally unit-inferable. If the conditional shape does not clear the alert, escalate per 31-RESEARCH A1 (explicit host allow-list comparison and/or unconditional DialContext classify with a dev-permissive policy), then re-scan."

# Metrics
duration: ~30min
completed: 2026-06-29
status: complete
---

# Phase 31 Plan 03: SEC-08 SSRF (CWE-918) Remediation Summary

**MCP-local SSRF guard transplanted from the CodeQL-clean internal/web classifier: an unconditional url.Parse + scheme allow-list + cloud-metadata/link-local barrier on the OpenHTTP endpoint path, an enforce-gated private-range block (AURA_MCP_SSRF_ENFORCE, default off → dev no-op), and an enforce-only DNS-rebinding-hardened http.Client.**

## Performance

- **Duration:** ~30 min
- **Started:** 2026-06-29T18:25Z (approx)
- **Completed:** 2026-06-29T18:54Z
- **Tasks:** 2 (Task 1 TDD: RED + GREEN)
- **Files modified:** 6 (4 created, 2 modified)

## Accomplishments
- `guardEndpoint` breaks the CWE-918 taint flow (governance install `req.URL` → managed config → `OpenHTTP cfg.URL` → `c.endpoint` → `c.client.Do(req)`) by validating the endpoint at the OpenHTTP seam: `url.Parse` + scheme allow-list + cloud-metadata hostname block + resolved-IP `classify` run BEFORE `c.endpoint` is assigned `validated.String()`.
- `classify` is a faithful Unmap-first mirror of `internal/web/ssrf.go` (loopback / link-local / private / multicast / unspecified / cgnat / this-network / IPv6-metadata) — `::ffff:169.254.169.254` collapses to the IMDS IP and is blocked.
- Profile-gating: the scheme + cloud-metadata + link-local barrier is UNCONDITIONAL; the loopback/private block is gated on the new `AURA_MCP_SSRF_ENFORCE` knob (default off). Under dev, loopback (127.0.0.1) and compose-DNS private sidecars stay reachable and `http.DefaultClient` is retained — the ~20 existing loopback `httptest` tests stay green (C5b regression intact).
- Enforce-only Layer-2 hardened `http.Client`: `DialContext` resolves+classifies+pins and dials only the pinned IP; `net.Dialer.Control` re-classifies the post-resolution IP (DNS-rebinding TOCTOU defense). Installed only when `Enforce && cfg.Client==nil`.

## Task Commits

Each task was committed atomically:

1. **Task 1 (RED): failing SSRF guard table tests** — `f2d5021a` (test)
2. **Task 1 (GREEN): classify + guardEndpoint implementation** — `fce979bd` (feat)
3. **Task 2: wire guardEndpoint into OpenHTTP + enforce-only hardened transport** — `6f220341` (feat)

**Plan metadata:** see final `docs(31-03)` commit.

## Files Created/Modified
- `internal/mcp/ssrf.go` (created, 138 LOC) — `classify`, `metadataHostBlocklist`, `allowedSchemes`, `resolver` seam, `guardEndpoint`.
- `internal/mcp/ssrf_test.go` (created) — `TestClassify` (every block class) + `TestGuardEndpoint` (24 rows: unconditional barriers, dev-vs-enforce loopback/private, allow-list, mixed-set fail-closed, resolve-failure). Injected fake resolver → no env, always runs.
- `internal/mcp/transport_ssrf.go` (created, 97 LOC) — `hardenedDialer` (`dialContext` IP-pin + `control` rebind re-check), `newHardenedHTTPClient`.
- `internal/mcp/transport_ssrf_test.go` (created) — rebind fail-closed, pinned-IP dial, Control reject table.
- `internal/mcp/http_client.go` (modified) — `HTTPConfig.Enforce` + `HTTPConfig.AllowHosts`; `OpenHTTP` calls `guardEndpoint` and assigns `validated.String()`; hardened client under enforce.
- `internal/mcp/transport.go` (modified) — `OpenServer` reads `AURA_MCP_SSRF_ENFORCE` via `ssrfEnforceFromEnv()` (default off).

## Test Evidence (WSL — never native .exe per CLAUDE.md)

- C5a guard: `go test ./internal/mcp/ -run 'TestGuardEndpoint|TestClassify' -count=1` → PASS (all TestClassify classes + 24 TestGuardEndpoint rows).
- C5b regression (full package, proves dev permits loopback): `go test ./internal/mcp/ -count=1` → `ok ... 10.773s`. Race: `CGO_ENABLED=1 go test -race ./internal/mcp/ -count=1` → `ok ... 15.074s`.
- Lint: `golangci-lint run ./internal/mcp/` → `0 issues`. Native `go vet ./internal/mcp/` + `go build ./...` clean.
- Dev no-op / loopback proof: the existing `http_client_test.go` / `http_client_extra_test.go` / `transport_test.go` httptest clients (all bind 127.0.0.1) and `probe_test.go` (`http://127.0.0.1:0/mcp`) stay green with `AURA_MCP_SSRF_ENFORCE` unset → `http.DefaultClient` retained, loopback permitted.

## Decisions Made
- **MCP-local copy, not a shared extraction** (A4 RESOLVED → MCP-local): `internal/mcp/ssrf.go` copies `internal/web`'s classifier faithfully; it does NOT import `internal/web` and does NOT extract `internal/netguard` (that is QUAL-03 / Phase 32 scope). `internal/web` is untouched.
- **Unconditional barrier for CodeQL clearance:** scheme allow-list + cloud-metadata hostname + link-local IP block run on every policy branch, so the request-forgery taint path breaks regardless of `Enforce`. Only the loopback/private block is profile-gated.
- **Default-off knob:** `AURA_MCP_SSRF_ENFORCE` (AURA_<DOMAIN>_<UNIT>) zero-value = dev-permissive; Phase 33 (PROF-01/PROF-04) will bind it to the runtime profile and populate `AllowHosts`.
- **Faithful classify mirror:** the `fd00:ec2::/32` (AWS IPv6 metadata) branch is shadowed by the earlier RFC-4193 `IsPrivate()` predicate and classifies as `private` — kept identical to `internal/web` for parity (still blocked). Documented in the test as `ipv6_metadata_range_as_private`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Corrected an incorrect test expectation (224.0.0.1 is link-local multicast)**
- **Found during:** Task 1 (GREEN run)
- **Issue:** the first-draft `TestClassify` row asserted `224.0.0.1 → "multicast"`, but `224.0.0.0/24` is link-local multicast, so the faithful `classify` (matching `internal/web`) returns `"link_local"` first. The test was wrong, not the code.
- **Fix:** split into two honest rows — `224.0.0.1 → "link_local"` and `239.0.0.1 → "multicast"` (an administratively-scoped multicast address that actually reaches the plain multicast branch).
- **Files modified:** internal/mcp/ssrf_test.go
- **Verification:** `go test ./internal/mcp/ -run TestClassify` PASS.
- **Committed in:** `fce979bd` (GREEN task commit).

---

**Total deviations:** 1 auto-fixed (1 bug — test-expectation correction). No production-code deviation; the guard matches the planned recipe exactly.
**Impact on plan:** None — the corrected row improves branch fidelity (now both multicast classes are covered). No scope creep.

## Known Stubs
None. The RED stub `ssrf.go` (`test(31-03)` f2d5021a) was fully replaced by the GREEN implementation (`feat(31-03)` fce979bd).

## Threat Flags
None — no new security surface beyond the SEC-08 sink already in the plan's `<threat_model>` (T-31-SSRF-01..04). Go stdlib only; no new dependency.

## TDD Gate Compliance
- RED gate: `test(31-03)` `f2d5021a` (failing table tests + compiling stub) — confirmed failing before implementation.
- GREEN gate: `feat(31-03)` `fce979bd` (implementation) lands after RED.
- REFACTOR: none required (implementation was clean at GREEN).

## Manual / Post-Merge Verification (not locally inferable)
- **C5c — CodeQL `go/request-forgery` → Fixed (SEC-08):** a post-push GitHub-hosted CodeQL re-scan must show the alert at `internal/mcp/http_client.go` state = **Fixed** (NOT Dismissed). This is the documented backstop, not a unit gate. **Escalation (31-RESEARCH A1, MEDIUM confidence on the conditional shape):** if the alert stays open, add an explicit host allow-list comparison and/or make the `DialContext` `classify` unconditional with a dev-permissive policy, then re-scan. The unconditional `url.Parse` + scheme + metadata barrier on the request-build path mirrors the exact combination CodeQL already accepts in `internal/web`.
- **Mutation ≥70% on `internal/mcp/ssrf.go` (`classify` gate target):** WSL-only (`go-mutesting`, go1.26 fork) per 31-VALIDATION Manual-Only; not a hosted-CI gate. Note the `fd00:ec2::/32` branch is shadowed by `IsPrivate()` (mirrored from `internal/web`) — classify it as a near-equivalent survivor per the mutation-autopsy convention if it surfaces.

## Next Phase Readiness
- SEC-08 remediation code is complete, tested, race-clean, and lint-clean; the CodeQL re-scan is the only remaining (post-push) acceptance gate.
- Phase 33 (PROF-01/PROF-04) is set up to bind `AURA_MCP_SSRF_ENFORCE` to the runtime profile and populate `HTTPConfig.AllowHosts` for the configured sidecars.

## Self-Check: PASSED
- Files: ssrf.go, ssrf_test.go, transport_ssrf.go, transport_ssrf_test.go, http_client.go, transport.go — all FOUND.
- Commits: f2d5021a (RED), fce979bd (GREEN), 6f220341 (wiring) — all FOUND in git log.

---
*Phase: 31-stabilization-ci-unblock*
*Completed: 2026-06-29*
