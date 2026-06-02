---
phase: 07-web-tools
plan: 02
subsystem: web-security
tags: [ssrf, dns-pin, netip, transport, redirect, error-taxonomy, fail-closed, goleak, mutation-target]

# Dependency graph
requires:
  - phase: 07-web-tools (plan 01)
    provides: internal/web package skeleton + goleak main_test.go + AURA_WEB_* root config (WebDNSPinTTLSec/WebFetchMaxBodyBytes/WebUserAgent) + readability/html-to-markdown deps
  - phase: 05-sandbox-2a-stateless
    provides: docker.go DisableKeepAlives + dialer-only-timeout idiom (copied into transport.go) + fail-closed posture
  - phase: 03-llm-client
    provides: openai_compat/httperror.go two-layer non-leaky structured-error model (analog for errors.go)
provides:
  - classify(netip.Addr) Unmap-first SSRF IP classifier (the mutation-gate target, ssrf.go)
  - validateAndPin(ctx, convID, host) host security gate — hostname blocklist + pin reuse + mixed-record fail-closed
  - dnsPin per-(conversation,host) TTL pin cache (config-injected TTL, injectable clock)
  - newHardenedTransport — pinned-IP DialContext + Dialer.Control recheck + CheckRedirect ErrUseLastResponse + DisableKeepAlives http.Client
  - WebError sanitized model-visible error shape {error,reason,message,status_code?} + internalError rich layer + sanitize() chokepoint + D-38 enum
  - withConvID/convIDFrom context-key plumbing for per-conversation pin scoping
affects: [07-03 fetcher.go (consumes guard+transport+errors), 07-03 searxng.go (consumes errors), 07-04 tool adapters (sanitize -> NewResult), phase-close mutation gate on ssrf.go]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Unmap-FIRST netip classifier: ip.Unmap() before the Is* switch so IPv4-mapped IPv6 (::ffff:169.254.169.254) collapses to its v4 form and hits the v4 predicates (Pitfall 2 kill)"
    - "Mixed-record fail-closed: classify EVERY resolved IP, block the whole host if ANY is blocked — never cherry-pick a public IP from a mixed A/AAAA set (D-24)"
    - "Resolve-once-then-pin + dial-by-pinned-IP: dialContext dials net.JoinHostPort(pinnedIP, port), never the hostname, closing the rebinding TOCTOU window (Pattern 1)"
    - "Two-layer errors: rich internalError (resolvedIP/host/redirectFrom for logs+tests) -> sanitize() -> flat WebError with hand-rolled MarshalJSON that OMITS unset keys (no leaky zero status_code)"
    - "convID via unexported context-key type so DialContext (network,addr only) can still scope the pin per conversation"
    - "Dialer.Control wired only on the production net.Dialer (when no dialFunc injected); tests assert ht.control directly — defense-in-depth post-resolution recheck"

key-files:
  created:
    - internal/web/errors.go
    - internal/web/ssrf.go
    - internal/web/dnspin.go
    - internal/web/transport.go
    - internal/web/ssrf_test.go
    - internal/web/dnspin_test.go
    - internal/web/transport_test.go
  modified: []

key-decisions:
  - "224.0.0.1 classifies as link_local (it is 224.0.0.0/24 link-local MULTICAST, IsLinkLocalMulticast fires before IsMulticast); added a distinct 225.1.2.3 row to still exercise the global-multicast branch — test corrected to ground truth, not the code"
  - "Link-local check ordered BEFORE IsPrivate so fc00::1 reports private (ULA) and fe80::1/169.254.x report link_local — each blocklist class gets its own deterministic reason for the mutation gate"
  - "metadataV6Pfx fd00:ec2::/32 maps to the link_local reason (it is the cloud-metadata v6 region, same threat class as the v4 metadata IP)"
  - "dnsPin clock is injectable (p.now) so TestDNSPin_TTL is deterministic with zero real sleep; exact-expiry is a MISS (now().Before(expires)) — off-by-one kill row added"
  - "guard.newGuard takes a third 'any' param (unused) to keep the Task-1 test signature stable for the Wave-3 fetcher wiring without a churn rename"
  - "internalError sensitive fields are UNEXPORTED so they cannot be JSON-marshalled by accident; sanitize() is the single chokepoint that strips them (enforces D-27)"

patterns-established:
  - "WebError.MarshalJSON hand-rolled over a map so unset reason/status_code omit their keys entirely (no `\"status_code\":0` leak on an SSRF block)"
  - "Recording-dialer test idiom: a dialFunc that records the addr and returns a sentinel error, asserting WHERE the transport dialed without completing a connection"

requirements-completed: []

# Metrics
duration: ~25min
completed: 2026-06-02
tasks: 2
files: 7
---

# Phase 07 Plan 02: SSRF Security Engine Summary

The SSRF security boundary of Phase 7 — an Unmap-first `net/netip` IP classifier, a per-conversation DNS-pin TTL cache, an SSRF-hardened pinned-IP `http.Client` with redirect interception, and a non-leaky two-layer error taxonomy — all proven below the tool layer with deterministic, fail-closed, table-driven tests under `-race`.

## What Was Built

**Task 1 — `errors.go` + `ssrf.go` (commit 16f66a4c)**
- `errors.go`: the D-38 stable enum (`web_search_unavailable`, `blocked_url`, `unsupported_scheme`, `unsupported_content_type`, `response_too_large`, `timeout`, `http_error`, `extraction_failed`) + stable non-sensitive reason consts. `WebError{Code,Reason,Message,StatusCode}` is the only shape the model sees; a hand-rolled `MarshalJSON` emits `{error,reason,message,status_code?}` and omits unset keys. `internalError` is the rich, unexported-field layer (resolvedIP/host/redirectFrom) for logs+tests only; `sanitize()` is the single internal→model chokepoint.
- `ssrf.go`: `classify(netip.Addr)` runs `ip.Unmap()` FIRST then the blocklist switch (loopback / link-local uni+multicast / private+ULA / multicast / unspecified / CGNAT 100.64/10 / this-net 0.0.0.0/8 / metadata-v6 fd00:ec2::/32), invalid Addr → `invalid_target`. A case-insensitive hostname blocklist (the five metadata/internal hosts) is checked BEFORE resolution. `validateAndPin(ctx,convID,host)` = hostname-block → pin reuse → resolve-all → classify-every → fail closed on ANY block → pin+return first public IP.
- `dnspin.go` landed here too (ssrf.go depends on the `dnsPin` type); its dedicated test came with Task 2.

**Task 2 — `dnspin.go` test + `transport.go` (commit cba44087)**
- `dnspin.go`: `dnsPin` is a `sync.Mutex`-guarded `map[pinKey]pinEntry` keyed by `{conv,host}`, config-injected TTL, injectable clock. `Pinned` is a miss on absent OR expired; `Pin` stamps a fresh window.
- `transport.go`: `newHardenedTransport(guard, dialFunc, ua)` builds the `http.Client`. `dialContext` runs `validateAndPin` then dials `net.JoinHostPort(pinnedIP, port)` via the injectable dialFunc (pin-then-dial-by-IP, no second lookup). `control` is the `Dialer.Control` post-resolution recheck (wired on the production dialer; asserted directly in tests). `CheckRedirect` returns `http.ErrUseLastResponse`. `DisableKeepAlives: true`. `convID` flows via an unexported context key.

## Security Invariants Verified

All 10 invariants from the plan hold and are test-covered:
1. `classify` Unmap-first — `::ffff:169.254.169.254` → `link_local`, `::ffff:127.0.0.1` → `loopback` (explicit table rows).
2. Mixed-record fail-closed — `{1.2.3.4,127.0.0.1}` blocks the whole host (`TestValidateAndPin_MixedRecords`).
3. Hostname blocklist case-insensitive, pre-resolution (`TestHostnameBlocklist`, mixed-case inputs).
4. DNS pin keyed by (conv,host), TTL hit/miss, no cross-conversation sharing, dial-by-pinned-IP (`TestDNSPin_TTL`, `TestTransport_DialsPinnedIP`).
5. `Dialer.Control` rejects blocked post-resolution IP (`TestTransport_ControlRecheck`).
6. `CheckRedirect` → `http.ErrUseLastResponse` (`TestTransport_NoAutoRedirect`).
7. `DisableKeepAlives: true` — goleak TestMain clean under `-race`.
8. Two-layer non-leaky errors — `TestError_NonLeaky` greps the serialized WebError clean of IP/CIDR/internal-host/redirect substrings.
9. `ssrf.go` = 107 LOC (≤120 mutation-gate target), `transport.go` = 109 LOC (≤150), all files <600.
10. Injectable `resolver` + `dialFunc` + clock — every test is deterministic, no real DNS/network.

## Tests (all green under -race, x3 count)

`TestBlocked_Classification` (16 rows + invalid), `TestValidateAndPin_MixedRecords`, `TestHostnameBlocklist`, `TestError_NonLeaky`, `TestDNSPin_TTL`, `TestDNSPin_ExactExpiryIsMiss`, `TestTransport_DialsPinnedIP`, `TestTransport_ControlRecheck`, `TestTransport_NoAutoRedirect`, `TestTransport_BlockedHostDialFailsClosed`. Full `internal/web` package race-clean; `golangci-lint run ./internal/web/...` = 0 issues; `go build ./...` clean.

## Deviations from Plan

**None functionally.** One test-fixture correction (not a deviation from required behavior): the plan's behavior table listed `224.0.0.1 → multicast`, but `224.0.0.1` is in `224.0.0.0/24` (link-local multicast), so `classify` correctly returns `link_local`. The test row was corrected to ground truth and a distinct global-multicast row (`225.1.2.3 → multicast`) was added so the `IsMulticast()` branch is still exercised. This is consistent with CLAUDE.md "NEVER MODIFY TESTS TO MAKE THEM PASS unless the test itself is broken" — the expectation was wrong about RFC scope, the code is correct.

## Notes for Wave 3

- `fetcher.go` consumes `newGuard` + `newHardenedTransport` + `withConvID`; the third `any` param on `newGuard` is a stable placeholder for the fetcher wiring (rename when consumed).
- `transport.go` holds the Aura User-Agent (`ht.ua`) for the fetcher to stamp on requests (D-34/D-35).
- `internalError` already carries `redirectFrom` so the Wave-3 per-hop redirect revalidation can attach the `redirect_to_blocked_target` reason and still sanitize cleanly.
- `ssrf.go` is ready for the WSL `go-mutesting ./internal/web/ssrf.go` ≥70% phase-close gate.

## Self-Check: PASSED

- internal/web/errors.go, ssrf.go, dnspin.go, transport.go, ssrf_test.go, dnspin_test.go, transport_test.go — all present.
- Commits 16f66a4c (Task 1) + cba44087 (Task 2) present in git log.
