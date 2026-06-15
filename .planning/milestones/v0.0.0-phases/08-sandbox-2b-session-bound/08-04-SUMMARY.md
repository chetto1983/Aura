---
phase: 08-sandbox-2b-session-bound
plan: 04
subsystem: web
tags: [ssrf, dns-pin, sandbox-egress, reuse-surface, landmine-5]
requires:
  - "08-01 (PRD-amendment gate; OQ2/A4 export-minimal decision)"
provides:
  - "web.ClassifyIP(netip.Addr)(string,bool) — exported SSRF class table (delegates to classify)"
  - "web.DialGuard + NewDialGuard(ttlSec) + ResolveAndPin(ctx,convID,host) — resolve-then-pin entry for the sandbox proxy"
affects:
  - "08-06 (internal/sandbox/network.go consumes the export — no classify copy)"
tech-stack:
  added: []
  patterns:
    - "thin exported wrapper delegating to package-private internals (single source of truth)"
    - "no-drift test asserting exported == internal classification"
key-files:
  created:
    - internal/web/export.go
    - internal/web/export_test.go
  modified: []
decisions:
  - "Exported a MINIMAL surface (ClassifyIP + DialGuard) per 08-DECISIONS-WAVE0 OQ2/A4 Option (a); did NOT extract internal/netguard (scope control)."
  - "DialGuard wraps the package-private guard over net.DefaultResolver; the proxy gets resolve-then-pin + DNS-rebinding + classify-every-fail-closed without touching web internals or the HTTP transport."
  - "ssrf.go/dnspin.go/transport.go needed NO edits: the export delegates without an export hook, so deep-refactor-on-touch did not apply to them."
metrics:
  duration: ~15m
  completed: 2026-06-03
  tasks: 1
  files: 2
---

# Phase 8 Plan 04: Export minimal SSRF reuse surface from internal/web Summary

Exported a minimal, non-duplicating SSRF reuse surface (`ClassifyIP` + a `DialGuard`
resolve-then-pin entry) from `internal/web` so the host-side egress proxy (08-06) can
reuse the Slice-5 IP-classification + DNS-rebinding pin without copy-pasting `classify`.

## What shipped

- **`internal/web/export.go`** (45 LOC)
  - `ClassifyIP(ip netip.Addr) (reason string, blocked bool)` — exported form of the
    package-private `classify`; delegates directly (`return classify(ip)`), so the full
    SSRF class table (loopback / link-local / private / multicast / unspecified / cgnat /
    this-network / IPv4-mapped Unmap) has a single source of truth.
  - `type DialGuard struct{ g *guard }` + `NewDialGuard(dnsPinTTLSec int) *DialGuard`
    (over `net.DefaultResolver`) + `ResolveAndPin(ctx, convID, host) (netip.Addr, string)`
    — a thin resolve-then-pin entry wrapping the package-private `guard.validateAndPin`
    (hostname blocklist → per-(conv,host) DNS pin reuse → resolve + classify-every-record
    + fail-closed-on-any-blocked → pin). The proxy inherits the full SSRF control without
    touching web internals or the HTTP transport.

- **`internal/web/export_test.go`** (122 LOC, `//go:build !web_integration`)
  - `TestClassifyIP_NoDrift` / `TestClassifyIP_InvalidNoDrift` — the no-drift guard:
    `ClassifyIP == classify` for a representative IP across every block class + public v4/v6
    + zero-Addr fail-closed. Fails if a copy ever creeps in (the dupl/single-source contract
    as a test, T-08-04-INFO-SSRF).
  - `TestDialGuard_ResolveAndPin` — the proxy reuse contract: all-public host pins the first
    public IP; a mixed record set (public + loopback) fails closed with no dialable IP; a
    metadata hostname is blocked by the hostname blocklist BEFORE resolution. Uses an injected
    stub resolver (no live DNS) via the package-private `newGuard`/`newDNSPin`, proving the
    exported surface delegates rather than reimplements.

## Mandate compliance

- **MANDATE 1 (re-test Slice-5 web tier, no regression):** `go test ./internal/web/` green;
  `go test -race ./internal/web/` green. The `web_integration` tier (incl. `TestDNSRebind`)
  compiles cleanly under `go vet -tags web_integration ./internal/web/` — that tier is live
  (SearXNG/network-gated, Gate-3); its deterministic SC#4 sibling `TestDNSPin_TTL` runs in the
  unit tier and stays green. No `web_fetch` SSRF behavior changed (the symbols only gained
  exported wrappers; the internals are untouched).
- **MANDATE 2 (no classify copy into sandbox):** `classify` lives in exactly one place
  (`ssrf.go`); the export delegates. `golangci-lint run ./internal/web/` reports **0 issues**
  including `dupl`. 08-06 will CALL `web.ClassifyIP` / `web.NewDialGuard`, not reimplement.

## Verification results

| Gate | Result |
|------|--------|
| `go vet ./internal/web/` | pass |
| `go vet -tags web_integration ./internal/web/` | pass (integration tier compiles, incl. TestDNSRebind) |
| `go build ./...` | pass |
| `go test ./internal/web/` | pass |
| `go test -race ./internal/web/` | pass |
| `golangci-lint run ./internal/web/` | 0 issues (incl. dupl) |
| Web package coverage | 90.8% (above 85% floor) |
| File-size cap (≤600 LOC) | export.go 45, export_test.go 122 — pass |
| `grep "func ClassifyIP" export.go` | 1 |
| Delegation grep | `classify(` and `validateAndPin` called from export.go (not reimplemented) |

## Deviations from Plan

None — plan executed exactly as written. `ssrf.go`/`dnspin.go`/`transport.go` are listed in
`files_modified` but required no edits: the export delegates without an export hook, so the
minimal change is the two new files. Deep-refactor-on-touch therefore did not apply to the
existing files (they were not touched).

## Known Stubs

None. The exported surface fully delegates to shipped, tested internals.

## Self-Check: PASSED

- `internal/web/export.go` — FOUND
- `internal/web/export_test.go` — FOUND
- Commit `dfe48ee7` — FOUND
