---
phase: 08-sandbox-2b-session-bound
plan: 06
subsystem: infra
tags: [sandbox, egress-proxy, http-connect, ssrf, dns-pinning, glob-allowlist, gvisor, security]

# Dependency graph
requires:
  - phase: 08-04
    provides: internal/web export (ClassifyIP / NewDialGuard / ResolveAndPin) — the resolve-then-pin SSRF gate reused by the proxy (no copy)
  - phase: 08-02
    provides: config field SandboxNetworkAllowHosts (CSV) + PrivacyMode surface
provides:
  - host-side CONNECT forward proxy (internal/sandbox/network.go) replacing impossible in-container iptables (D-08)
  - deny-wins glob allowlist (Codex network_policy.rs model) with global-* deny rejection (footgun guard)
  - resolve-then-pin via the internal/web export (no IP-class-table copy), fail-closed on any blocked record, keyed per conversation_id
  - opaque Hijack + bidirectional io.Copy tunnel (NO MITM, D-10)
  - NewSessionProxy production constructor (CSV -> policy -> web guard) for the 08-08 wiring
affects: [08-07, 08-08, 08-09]

# Tech tracking
tech-stack:
  added: []  # stdlib net/http (Hijacker) + internal/web export only — NO new dependency (Package Legitimacy Gate)
  patterns:
    - "deny-wins glob allowlist with build-time global-* deny rejection (Codex network_policy.rs)"
    - "resolvePinner seam: signature mirrors web.DialGuard.ResolveAndPin exactly so *web.DialGuard satisfies it with no adapter; keeps the unit tier network-free without copying the IP-class table"
    - "CONNECT opaque tunnel via Hijack + io.Copy with ctx-cancel watcher (goleak-clean)"

key-files:
  created:
    - internal/sandbox/network.go
    - internal/sandbox/network_test.go
  modified: []

key-decisions:
  - "resolvePinner interface uses netip.Addr to match web.DialGuard.ResolveAndPin exactly (no net.IP adapter) — *web.DialGuard satisfies the production seam directly while tests inject a stub"
  - "Added NewSessionProxy production constructor (parse CSV -> buildPolicy -> newWebGuard) so the full wiring chain is reachable + unit-covered; 08-08 calls it with the bridge-gateway addr"
  - "Global * permitted in the ALLOW list (operator opt-in allow-all) but rejected in the DENY list (ErrGlobalDenyWildcard) — matches Codex footgun rule"
  - "Suffix-match helper for *.x globs (HasSuffix on the dotted suffix + length guard) — no globset dependency, *.x never matches the bare parent"

patterns-established:
  - "Pattern: host-side egress containment — untrusted code never owns its firewall; the proxy validates the CONNECT hostname only and tunnels opaque bytes"
  - "Pattern: SSRF reuse via thin export — sandbox reuses internal/web's classify+pin via a signature-matched seam, single source of truth (OQ2/A4, dupl-clean)"

requirements-completed: [CAP-02]

# Metrics
duration: ~35min
completed: 2026-06-03
---

# Phase 8 Plan 06: Host-Side CONNECT Forward-Proxy Egress Allowlist Summary

**A stdlib net/http CONNECT forward proxy enforcing a deny-wins glob hostname allowlist + resolve-then-pin SSRF (reusing the internal/web export, no copy) with an opaque Hijack+io.Copy tunnel and no MITM — replacing the impossible in-container iptables (D-08).**

## Performance

- **Duration:** ~35 min
- **Completed:** 2026-06-03
- **Tasks:** 1 (TDD)
- **Files modified:** 2 (both created)

## Accomplishments
- `internal/sandbox/network.go` (308 LOC): the host-side CONNECT proxy — deny-wins glob policy, resolve-then-pin via the web export, Hijack + bidirectional io.Copy opaque tunnel, per-session policy keyed by conversation_id.
- Deny-wins precedence (Codex `network_policy.rs` model): a deny glob ALWAYS beats an allow glob; baseline-deny for not-allowed (D-09). `*.pythonhosted.org` matches a subdomain but NOT the bare parent; a GLOBAL `*` in the deny list is rejected at build time (`ErrGlobalDenyWildcard`); `*` IS allowed in the allow list (operator opt-in allow-all).
- Resolve-then-pin reuses `web.NewDialGuard` / `web.ResolveAndPin` — NO copy of the IP-class table (OQ2/A4); fail-closed on ANY blocked record (private/metadata/loopback), DNS pin keyed per conversation.
- NO MITM (D-10): validates the CONNECT hostname only; no `tls.Config` cert minting (grep-verified 0); malformed CONNECT and non-CONNECT methods rejected.
- `NewSessionProxy` production constructor (CSV -> policy -> web guard) is the 08-08 wiring entrypoint; an empty allowlist yields a baseline-deny policy (2a egressless posture).

## Task Commits

Each task was committed atomically (TDD):

1. **Task 1 (RED): failing CONNECT-proxy allowlist + SSRF test** — folded into the GREEN commit because the lefthook pre-commit `go vet` gate refuses to commit a non-compiling RED tree (the test references symbols `network.go` defines). The RED state was verified locally (`go vet` → `undefined: Proxy`) before the implementation was written.
2. **Task 1 (GREEN): host-side CONNECT forward-proxy egress allowlist** — `a23b555f` (feat) — `network.go` + `network_test.go`.

_Note: the project's lefthook pre-commit hook runs `go vet` and blocks any commit that fails to compile, so the canonical TDD RED commit (a deliberately non-compiling test) cannot be committed standalone without `--no-verify` (forbidden by the parallel-executor contract). The RED→GREEN discipline was followed (test written first, failure observed, then minimal implementation); the two phases land in one vet-green commit. See TDD Gate Compliance below._

## Files Created/Modified
- `internal/sandbox/network.go` (308 LOC) — host-side CONNECT forward proxy: `hostGlob`/`parseGlob`/`match` suffix-match helper, `policy.allow` deny-wins gate, `buildPolicy` (global-* deny rejection), `parseAllowHosts` CSV boundary parser, `resolvePinner` seam, `newWebGuard` (web export), `Proxy`/`NewProxy`/`NewSessionProxy`/`Serve`/`handleConnect`/`tunnel`.
- `internal/sandbox/network_test.go` (322 LOC) — `TestProxy_AllowlistGlobAndSSRF` (glob precedence, *.x-not-parent, global-* deny rejected, allowlisted CONNECT tunnels through an in-process echo, non-allowlisted 403, SSRF-resolved-private 403, malformed CONNECT refused, unreachable-upstream 502), `TestParseAllowHosts`, `TestNewSessionProxy`. Uses a `stubResolvePinner` seam — no live network (no package-level `TestMain` added; `docker_test.go` owns the goleak one).

## Decisions Made
- **resolvePinner uses `netip.Addr`** (matching `web.DialGuard.ResolveAndPin` exactly) rather than `net.IP` — so `*web.DialGuard` satisfies the production seam with zero adapter code, and the test stub matches the same signature.
- **Added `NewSessionProxy`** as the production constructor so the parse→build→guard chain is reachable and unit-covered (the bare `NewProxy`+stub-guard path alone left the web-export wiring uncalled); 08-08 calls `NewSessionProxy(addr, allowCSV, denyCSV, convID, dnsPinTTLSec)`.
- **Suffix-match over a globset library** — a stdlib `HasSuffix` on the dotted `.x` suffix + a length guard (so `*.x` never matches the bare `x`), avoiding a new dependency (Package Legitimacy Gate; threat register T-08-06-SC `accept`).

## Deviations from Plan

**1. [Rule 3 - Blocking] TDD RED commit folded into GREEN (lefthook vet gate)**
- **Found during:** Task 1 (RED commit attempt)
- **Issue:** The lefthook pre-commit hook runs `go vet` and aborts the commit when the tree does not compile. A canonical TDD RED commit (a test referencing not-yet-defined symbols) cannot pass that gate, and `--no-verify` is forbidden by the parallel-executor contract.
- **Fix:** Verified the RED failure locally (`go vet` → `undefined: Proxy`), then wrote the minimal `network.go` implementation and committed test+impl together in one vet-green commit (`a23b555f`). RED→GREEN order was preserved; only the commit granularity changed.
- **Files modified:** none beyond the planned two.
- **Verification:** `go vet ./...`, `go build ./...`, `go test -race` all green; commit `a23b555f` landed with the hook passing.

**2. [Rule 2 - Missing functionality] Added `NewSessionProxy` production constructor**
- **Found during:** Task 1 (coverage/wiring review)
- **Issue:** The plan specified `NewProxy(addr, guard, ...)`; with a test-injected stub guard the production web-export wiring (`newWebGuard`) had no caller, leaving the CSV→policy→guard chain unexercised and the `web.NewDialGuard` reuse unreachable from any production path.
- **Fix:** Added `NewSessionProxy(addr, allowHosts, denyHosts, convID, dnsPinTTLSec)` that parses the allowlist CSV at the boundary, compiles the deny-wins policy, and builds the web-export guard — the entrypoint 08-08 needs. Kept `NewProxy(addr, pol, guard, convID)` for testability.
- **Files modified:** `internal/sandbox/network.go`, `internal/sandbox/network_test.go` (`TestNewSessionProxy`).
- **Verification:** `TestNewSessionProxy` covers the valid-CSV build + the global-* deny fail-fast; `NewSessionProxy`/`newWebGuard`/`NewProxy` now 100% covered; golangci 0 issues.

---

**Total deviations:** 2 ([Rule 3] TDD-commit-granularity, [Rule 2] production constructor)
**Impact on plan:** No scope creep. Deviation 1 is a commit-granularity adaptation to the project's vet-gate hook (RED→GREEN discipline preserved). Deviation 2 is a correctness/reachability requirement — without it the web-export reuse (the plan's core reuse mandate) has no production caller and the `unused` linter would flag the wiring.

## TDD Gate Compliance

The canonical separate `test(...)` RED commit could not land standalone: the repo's lefthook pre-commit hook runs `go vet`, which fails on a deliberately non-compiling RED test, and `--no-verify` is contractually forbidden for the parallel executor. The RED→GREEN cycle was still followed — the test was written first and its compile failure observed (`go vet` → `undefined: Proxy`) before the minimal implementation — but RED and GREEN are combined in one vet-green commit (`a23b555f`). This is a hook-imposed constraint, not a skipped gate.

## Verification Results
- `go vet ./...` — clean.
- `go build ./...` — clean.
- `go test -race -run TestProxy_AllowlistGlobAndSSRF ./internal/sandbox/` — PASS (WSL native race).
- `go test -race ./internal/sandbox/ ./internal/web/` — PASS (full package; goleak TestMain green; web tier unchanged, export reuse confirmed).
- `golangci-lint run ./internal/sandbox/` — 0 issues.
- grep `web\.(ClassifyIP|NewDialGuard|ResolveAndPin)` in network.go — **5 matches** (web export reused; no `classify` copy).
- grep `tls.Config|x509|tls.Certificate` in network.go — **0** (no MITM / no cert minting).
- Coverage: network.go mean func coverage **93.0%** (parseGlob/match/allow/parseAllowHosts/newWebGuard/NewProxy/NewSessionProxy/newProxyWithListener 100%; buildPolicy 91.7%; handleConnect 77.4%; tunnel 85.7%; Serve 61.1%). Remaining gaps are defensive error branches (bind failure, hijack-unsupported, ctx-cancel mid-tunnel) exercised live in 08-08/08-09.
- File size: network.go 308 LOC, network_test.go 322 LOC — both ≤600.

## Issues Encountered
None beyond the documented deviations.

## User Setup Required
None - no external service configuration required. The proxy listen address + sidecar `HTTP_PROXY`/`HTTPS_PROXY` env wiring (bridge-gateway IP:port) is 08-08; the live pip→pypi reachability assertion is 08-09.

## Next Phase Readiness
- **08-07 (sidecar/session exec):** unaffected — no shared files; the connect-allowed seccomp variant lands there.
- **08-08 (network/seccomp posture wiring + live reachability spike):** consume `NewSessionProxy(addr, allowCSV, denyCSV, convID, dnsPinTTLSec)`; bind it at the bridge-gateway IP and inject `HTTP_PROXY`/`HTTPS_PROXY`/`http_proxy`/`https_proxy` into the sidecar. The single highest-risk unverified assumption (A2) — session container → host proxy reachability at the bridge gateway — must be probed there before declaring ROADMAP criterion 4 met.
- **08-09 (live finalize + 08-SECURITY):** add the live `pip install` against pypi through the proxy; re-state the AR-05-01 deviation (host-side egress control) in the threat register; cover the live SSRF-block + DNS-rebind assertions and the proxy's defensive error branches.

## Self-Check: PASSED
- `internal/sandbox/network.go` — FOUND
- `internal/sandbox/network_test.go` — FOUND
- `.planning/phases/08-sandbox-2b-session-bound/08-06-SUMMARY.md` — FOUND
- commit `a23b555f` — FOUND

---
*Phase: 08-sandbox-2b-session-bound*
*Completed: 2026-06-03*
