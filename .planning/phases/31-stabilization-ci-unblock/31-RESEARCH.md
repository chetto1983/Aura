# Phase 31: Stabilization & CI Unblock - Research

**Researched:** 2026-06-29
**Domain:** CI hygiene (GitHub Actions) + Go SSRF remediation (CWE-918) + verify-only frontend/file-size gates
**Confidence:** HIGH (criteria 1/2/4 empirically green; F-015 mechanics proven) — MEDIUM only on whether the conditional SSRF structure clears CodeQL (acceptance = re-scan)

## Summary

This is a **mechanical stabilization phase with one real security fix**. Three of the five success criteria are **already green** and reduce to verify-only tasks; I confirmed each by running the actual gate locally (local Node is **v24.16.0 / npm 11.17.0**, an exact match to CI's `AURA_CI_NODE_VERSION: 24.x`, so local measurements are authoritative):

- **C1 file-size:** `bash scripts/check-file-size.sh` exits 0 whole-tree. `cmd/aura/serve_webui.go`=525, `web/src/__tests__/LoginPage.test.tsx`=592 — both under the 600 cap (Codex already split `serve_webui_auth_config.go`=109 and `LoginPage.authula-errors.test.tsx`=128).
- **C2 dist freshness:** a fresh `npm run build` produced **zero drift** — `git diff --exit-code internal/webui/dist/` is clean and the whole tree stays clean after rebuild. The committed bundle already equals a fresh Node-24 build.
- **C4 frontend coverage:** `vitest run --coverage` → Statements 90.88%, **Branches 85.45%**, Functions 90.89%, Lines 92.85% — all four ≥85. The suite passes the floor today.

The **two substantive work items** are:

- **C3 (F-015 / SEC-07):** swap the **4 raw `./...`** invocations in `.github/workflows/ci.yml` (lines **90, 117, 128, 181**) to `$(bash scripts/go_packages.sh)`, and add a **CI lint** that rejects raw root `./...`, following the existing `quality_snapshot_gate.sh` + `quality_snapshot_gate_test.sh` (gate + negative self-test) house pattern. The Makefile already proves the helper pattern (`GO_PACKAGES := $(shell bash scripts/go_packages.sh)`).
- **C5 (SEC-08):** a **genuine CWE-918 SSRF**. A remote caller of the governance MCP-install API controls `req.URL` (`cmd/aura/serve_governance_write.go:214/223`) → managed config → `OpenHTTP` `cfg.URL` → `c.endpoint` → `c.client.Do(req)` at `internal/mcp/http_client.go:207`. The fix **mirrors the already-CodeQL-clean `internal/web` SSRF guard** (`url.Parse` + scheme allow-list + resolved-IP `classify` + optional DialContext/Control rebinding defense), profile-gated so loopback sidecars stay reachable under dev.

**Primary recommendation:** Treat C1/C2/C4 as verify-only (one wave). Do C3 (CI edits + lint + self-test) and C5 (mirror `internal/web` SSRF into `internal/mcp`) as the two implementation waves. The SSRF guard's **default/dev policy MUST permit loopback** — every existing MCP HTTP test binds `httptest.NewServer` on 127.0.0.1, so a blanket private-IP block would break ~20 tests AND every loopback sidecar (memory `127.0.0.1:8091`, PIM `:8093`). Make CodeQL re-scan the acceptance gate for C5.

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| QUAL-01 (Wave 0) | Split the two >600-LOC files + rebuild/commit `internal/webui/dist` | **Already done.** C1 (525/592, whole-tree green) and C2 (zero dist drift) verified locally. Verify-only tasks. |
| F-015 / SEC-07 | Go build/test/vuln CI jobs reuse `scripts/go_packages.sh` (no raw `./...`); a CI lint rejects raw `go test ./...` / `govulncheck ./...` | C3. Exact 4-line edit set + lint design + gate/self-test precedent documented below. |
| SEC-08 (pulled forward) | Critical CodeQL `go/request-forgery` at `internal/mcp/http_client.go` remediated; alert resolves to fixed | C5. Taint flow traced; `internal/web` mirror pattern + profile-gating + test matrix documented below. |
</phase_requirements>

## User Constraints (from ROADMAP + CLAUDE.md)

> No CONTEXT.md exists (no discuss-phase run). The locked scope is the ROADMAP Phase 31 section + CLAUDE.md invariants. These are authoritative.

### Locked Decisions (ROADMAP Phase 31 — verbatim success criteria)
1. No tracked production/test source file exceeds the 600-LOC cap (`cmd/aura/serve_webui.go`, `web/src/__tests__/LoginPage.test.tsx` split); `scripts/check-file-size.sh` green whole-tree.
2. `internal/webui/dist` rebuilt from `web/` and committed so `web-dist-freshness` is green (no source↔bundle drift).
3. Every Go build/test/vulnerability CI job sources its package list from `scripts/go_packages.sh` (no raw `./...`); a CI lint rejects raw `go test ./...` / `govulncheck ./...` (F-015).
4. Frontend coverage gate restored to ≥85% on all four vitest thresholds and the suite actually passes at that floor.
5. Critical CodeQL `go/request-forgery` (SSRF) at `internal/mcp/http_client.go` remediated (target validated against allow-list / SSRF guard) and the alert resolves to fixed (SEC-08).

### Claude's Discretion (HOW — this research recommends)
- SSRF guard placement (MCP-local vs shared extraction), env-knob naming, lint script vs inline grep, optional LoginPage split.

### CLAUDE.md hard constraints (apply to every task)
- File-size ≤600 LOC; **no-skip-as-green** (a `t.Skip` that fires under `$CI` must `t.Fatal`); owned-surface coverage floor **≥85%**; mutation ≥70% on critical files; env vars use **`AURA_<DOMAIN>_<UNIT>`**; **hardening is a no-op under `dev`/`local_trusted`**; one slice = one commit; **never run native `.exe` on the Windows host** (build/test in WSL or container — `node`/`npm` are fine); WSL is primary dev reaching the Windows Docker stack via `127.0.0.1`.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| File-size cap enforcement | CI / pre-commit hook | — | `scripts/check-file-size.sh` over `git ls-files`; build-and-lint job + lefthook hook |
| Embedded-bundle freshness | CI (web-dist-freshness) | Build (Vite→`//go:embed`) | Bundle is committed source embedded into the single binary; CI rebuilds+diffs |
| Go package-list hygiene (F-015) | CI (workflow + lint script) | Make (`GO_PACKAGES`) | A workflow-author discipline; enforced by a grep-lint over `.github/workflows/` |
| Frontend coverage floor | CI (web-test / vitest) | — | vitest v8 thresholds in `web/vitest.config.ts` |
| **Outbound MCP request validation (SSRF)** | **API/Backend (`internal/mcp`)** | CI (CodeQL re-scan) | The dial happens in the Go backend; the trust boundary is the governance install handler → managed config → outbound HTTP |

## Standard Stack

This phase adds **no external dependencies**. Everything is Go stdlib + the existing repo tooling.

### Core (all already present)
| Library / Tool | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `net/url`, `net/netip`, `net/http`, `net` | Go stdlib (go.mod toolchain) | URL parse, IP classification, hardened transport for the SSRF fix | The exact stack `internal/web/ssrf.go` + `transport.go` already use and that CodeQL already accepts |
| `scripts/go_packages.sh` | in-repo | Emits the owned Go package list (filters `web/node_modules` Go examples) | Already consumed by the Makefile + the build-and-lint job |
| `vitest` + `@vitest/coverage-v8` | 4.1.9 (`web/package.json`) | Frontend test + coverage thresholds | Existing gate; config at `web/vitest.config.ts` |
| `vite` | 8.0.16 | Builds `internal/webui/dist` (outDir `../internal/webui/dist`) | Existing embed-source build |
| CodeQL (`github/codeql-action@v4`) | `.github/workflows/codeql.yml` | `go/request-forgery` re-scan = the C5 acceptance gate | Go scan is full-repo via `autobuild` (paths-ignore only affects JS/TS) |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| MCP-local SSRF guard (copy `classify`) | Extract shared `internal/netguard` used by web + mcp | Shared extraction is the "no duplicate" ideal and keeps ONE mutation-tested classifier, but it touches `internal/web` (wider blast radius) and is arguably QUAL-03/Phase-32 scope. See Open Questions. |
| `shell: bash` on the Windows job | Rely on pwsh `$(...)` array-splat | pwsh *might* splat the helper output as args, but it is fragile (empty trailing line → `go build ""`); `shell: bash` is explicit and matches the Linux pattern. |
| Standalone lint script | Inline `grep` step in ci.yml | A script + negative self-test mirrors the existing `quality_snapshot_gate.sh`/`_test.sh` precedent and is reusable by the pre-push hook. |

**Installation:** none (no `npm install` / `go get`).

## Package Legitimacy Audit

**No external packages are installed in this phase.** The SSRF fix uses Go stdlib (`net/url`, `net/netip`, `net/http`, `net`); the CI work edits YAML + shell. slopcheck / registry verification is **N/A**. (Out-of-scope note: the pre-existing `@latest` pins on `govulncheck`/`deadcode` installs in ci.yml are SEC-05/F-051 / Phase 40 — do **not** "fix" them here.)

## Architecture Patterns

### Data-flow: the SSRF taint path (what CodeQL flags at `http_client.go:207`)

```
  governance MCP-install HTTP request  (remote principal / cockpit panel)
        │  req.URL  (agui.MCPInstallRequest, decoded from request body)
        ▼
  cmd/aura/serve_governance_write.go : buildInstallServer()
        │  L214 url := strings.TrimSpace(req.URL)   L223 URL: url
        ▼
  managed config  (ManagedServer.URL, json:"url")  ── also from servers.json file / AURA_MCP_SERVERS_JSON
        │   (custom installs default Trust=TrustBlocked — a RUNTIME gate CodeQL cannot see)
        ▼
  internal/mcp/transport.go : OpenServer()  L23  OpenHTTP(ctx, name, HTTPConfig{URL: server.URL, …})
        ▼
  internal/mcp/http_client.go : OpenHTTP → c.endpoint = cfg.URL
        │  post(): http.NewRequestWithContext(ctx, POST, c.endpoint, …)
        ▼
  ★ L207  resp, err := c.client.Do(req)        ← CodeQL go/request-forgery SINK
```

This is a **real** CWE-918, mitigated (not eliminated) by the `TrustBlocked` default + operator trust-approve. Per the `golang-security` skill: *"upstream protection does not eliminate a finding — every layer should protect itself."* So the dial boundary must validate regardless.

### The proven mirror: how `internal/web` already clears CodeQL

`internal/web` makes outbound HTTP with user-controlled URLs yet has **no open CodeQL SSRF alert**. It does **both** layers:

1. **String-path barrier (request-build time):** `fetcher.go:54` `u, err := url.Parse(strings.TrimSpace(rawURL))` + `:58` `allowedSchemes` (`{http, https}`) check; the request is built from the validated `*url.URL` (`current.String()`, `:115`).
2. **Transport barrier (dial time):** `transport.go` `hardenedTransport` — `DialContext` runs `guard.validateAndPin` (hostname blocklist → resolve → `classify` every IP → pin the IP and dial only it) and a `net.Dialer.Control` hook re-checks the post-resolution IP (defense-in-depth vs DNS-rebinding); `CheckRedirect` refuses auto-follow.

`classify(netip.Addr)` (`ssrf.go:35`) is the mutation-gated core — **`Unmap()` runs FIRST** so `::ffff:169.254.169.254` collapses to v4 before the link-local check (the documented Pitfall 2). It covers loopback, link-local uni/multicast, private, multicast, unspecified, CGNAT (`100.64/10`), this-network (`0.0.0.0/8`), and the IPv6 metadata prefix (`fd00:ec2::/32`). `hostnameBlocklist` blocks metadata hostnames *before* resolution.

### Pattern: F-015 helper swap + gate-with-self-test

The Makefile is the reference: `GO_PACKAGES := $(shell bash scripts/go_packages.sh)` feeds `vet/build/test/test-race/vuln`. CI must match. The lint follows the **existing** `quality_snapshot` precedent in ci.yml (build-and-lint job, lines 42-46): a **self-test** step runs first (proves the gate catches a violation), then the **gate** step runs.

### Anti-Patterns to Avoid
- **Blanket private-IP block in MCP.** `internal/web` blocks loopback/private unconditionally; **MCP must not** — its legitimate targets are loopback sidecars. Copying web's policy verbatim breaks every sidecar + ~20 `httptest` tests.
- **Profile-conditional-only barrier with no unconditional check.** If the *only* validation is `if hardened { … }`, CodeQL follows the dev branch and may keep the alert. Keep a real unconditional structural barrier (parse + scheme + metadata block) on the path.
- **Suppressing the alert** (`// codeql[...]` / dismiss-in-UI). The requirement is "resolves to **fixed**," not "dismissed."
- **Touching shipped web source in this phase.** Any edit to non-test `web/src` or `tokens/` re-stales `dist`. Keep web edits to test files only (which are not bundled) so C2 stays green for free.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| IP range classification | A bespoke CIDR table | Mirror `internal/web/ssrf.go` `classify` (or extract it) | It already encodes the Unmap-first pitfall, CGNAT, this-network, IPv6 metadata — and it's mutation-tested |
| DNS-rebinding defense | Manual re-resolve loops | `net.Dialer.Control` + `http.Transport.DialContext` (mirror `transport.go`) | The stdlib Control hook fires post-resolution/pre-connect — the canonical seam |
| URL scheme/host validation | Regex on the URL string | `url.Parse` + map-membership scheme allow-list | Regex misses userinfo/`[::]`/IPv4-mapped forms |
| Package enumeration | Re-deriving `go list` filtering in YAML | `scripts/go_packages.sh` | Already filters `web/node_modules` Go examples |
| A gate without proof it can fail | A grep that might silently pass | Gate + negative self-test (mirror `quality_snapshot_gate_test.sh`) | No-skip-as-green: prove the lint exits non-zero on an injected `./...` |

**Key insight:** The entire SSRF fix is a *transplant* of a known-good, CodeQL-clean, mutation-tested subsystem (`internal/web`) into `internal/mcp`, with one policy change (loopback allowed under dev). Inventing a new guard is strictly worse.

## Implementation Guidance (per criterion)

### C1 — File-size cap (VERIFY-ONLY)
- **State:** GREEN. `serve_webui.go`=525, `serve_webui_auth_config.go`=109, `LoginPage.test.tsx`=592, `LoginPage.authula-errors.test.tsx`=128. Only tracked >600 file is generated `internal/db/sqlc/assets.sql.go` (722, **exempt** by the script's `^internal/db/sqlc/` filter).
- **Task:** Run `bash scripts/check-file-size.sh` (expect exit 0). No split needed.
- **Residual risk:** `LoginPage.test.tsx` has only **8 lines of headroom** (592/600). Any task that adds tests there trips the cap. *Optional* preventive split: it has one top-level `describe('LoginPage')` with 22 `it`/`describe` blocks; a sibling `LoginPage.authula-errors.test.tsx` already exists, so a second logical split (e.g. `LoginPage.recovery.test.tsx`) is the established move if it grows. Not required this phase.

### C2 — dist freshness (VERIFY-ONLY)
- **State:** GREEN. I ran `cd web && npm run build` (Node 24.16.0) → `git diff --exit-code internal/webui/dist/` clean, whole tree clean. Build is byte-reproducible; committed 63-file bundle equals a fresh build.
- **Job:** `web-dist-freshness` (ci.yml ~L988) runs `scripts/web_dist_freshness.sh` = `(cd web && npm ci && npm run build)` then `git diff --exit-code -- internal/webui/dist/`. Path note: Vite `outDir=../internal/webui/dist` (NOT `web/dist`).
- **Deterministic local rebuild (operator/WSL):** `cd web && npm ci && npm run build && git -C .. diff --exit-code -- internal/webui/dist/`. Use **Node 24.x / npm 11** to match CI (`AURA_CI_NODE_VERSION`).
- **Landmine:** the freshness job uses `npm ci` (clean install from `package-lock.json`); if anyone rebuilds with a drifted `node_modules`, bytes can differ. Always `npm ci` before the canonical build. Keep web edits to **test files only** this phase so the bundle never changes.

### C3 — F-015 CI rewrite + lint (IMPLEMENT)

**The 4 edits in `.github/workflows/ci.yml`** (confirmed: exactly these 4 raw root `./...`, nothing else in any workflow):

| Line | Job | Current | Change to |
|------|-----|---------|-----------|
| 90 | `unit-test` | `go test -race -count=1 ./...` | `go test -race -count=1 $(bash scripts/go_packages.sh)` |
| 117 | `windows-unit` | `go build ./...` | `go build $(bash scripts/go_packages.sh)` **+ add `shell: bash`** |
| 128 | `windows-unit` | `go test -count=1 ./...` | `go test -count=1 $(bash scripts/go_packages.sh)` **+ add `shell: bash`** |
| 181 | `vulncheck` | `"$(go env GOPATH)/bin/govulncheck" ./...` | `"$(go env GOPATH)/bin/govulncheck" $(bash scripts/go_packages.sh)` |

- **Windows landmine (HIGH-impact):** GitHub `windows-latest` `run:` steps default to **pwsh**; there is **no `defaults.run.shell`** in ci.yml. `$(bash …)` is bash command substitution → add `shell: bash` to the two windows-unit steps (Git Bash ships on the runner). Verify `go_packages.sh` runs under Git Bash there (it uses `go list` + a small POSIX `awk`; Git-for-Windows `awk` handles it). **The planner MUST require the windows-unit job to actually pass post-change** — this is the only genuinely new-risk swap (Linux swaps are already proven by the build-and-lint job + Makefile).
- **Tag-matrix safety:** the swap touches only the 4 **untagged root** jobs. Scoped/integration steps already use explicit package args (`./internal/db/...`, `./internal/cron/...`, `./internal/agui/...`, `./internal/web/`, etc.) and `coverage_gate.sh` uses `./internal/...` — **none use root `./...`**, so the lint won't flag them and the tag tiers/coverage gate are unaffected. `go_packages.sh` yields the same owned set as `./...` minus `web/node_modules` examples → **no owned package is dropped**.

**The CI lint (new).** Add `scripts/check_ci_go_packages.sh` + `scripts/check_ci_go_packages_test.sh`, wired into the `build-and-lint` job as two steps **before** Set-up-Go (mirroring `quality_snapshot_gate_test.sh` → `quality_snapshot_gate.sh`):
- Scope: **`.github/workflows/` only** (so `go_packages.sh`'s own legitimate `go list ./...` is never scanned — it lives in `scripts/`).
- Match the **standalone root token** only: `(^|[[:space:]])\./\.\.\.([[:space:]]|$)`. This catches bare ` ./...` and **not** scoped `./internal/db/...` (verified against the live workflows: matches exactly the 4 target lines pre-fix, 0 after).
- Exclude comment lines (`^[[:space:]]*#`).
- Negative self-test: feed a fixture line containing `go test ./...` and assert the checker exits non-zero (no-skip-as-green parity with `cache_invariant_negative_test.sh`).
- **Validation:** after the 4 edits, `bash scripts/check_ci_go_packages.sh` exits 0; the self-test exits 0 (i.e. it confirmed the checker fails on a planted violation).

### C4 — frontend coverage (VERIFY-ONLY + don't-regress)
- **State:** GREEN today (measured local, Node 24.16.0): **Statements 90.88% / Branches 85.45% / Functions 90.89% / Lines 92.85%** — `vitest run --coverage` meets all four global thresholds (`web/vitest.config.ts` = 85 each).
- **Thin margin:** **Branches 85.45%** is the binding constraint (~0.45pp / ≈14 covered branches of headroom). Lowest-coverage files dragging branches: `governance/McpLifecycleCluster.tsx` (45% br), `settings/settingsApi.ts` (50%), `i18n.ts` (66%), `theme/ThemeSwitcher.tsx` (60%), `settings/ModelSettingsPanel.tsx` (64.7% func / 75% stmt), `governance/McpInstallPanel.tsx` (57.7% func). Thresholds are **global**, not per-file, so these don't fail today — but a new uncovered branch could tip branches < 85.
- **Task:** re-run in CI/WSL (Linux) to confirm parity — `cd web && npm ci && npm run test`. The web-test job already does this. If a future merge regressed it (the STATE.md multimodal-assets carry-forward worried about this), it has **not** regressed below 85 — confirmed.
- **Landmine:** the known *Windows→Linux npm-lock drift* (Linux WASM deps like `@emnapi/*`) affects install completeness, **not** jsdom coverage numbers. CI runs Linux `npm ci`; expect the same numbers. No action unless CI disagrees with the local 90.88/85.45/90.89/92.85.

### C5 — SSRF remediation (IMPLEMENT)

**Recommended design — transplant `internal/web`'s guard into `internal/mcp`, profile-gated.**

Add `internal/mcp/ssrf.go` (new, <600 LOC) containing:
- `classify(netip.Addr) (reason string, blocked bool)` — copy faithfully from `internal/web/ssrf.go` **including `Unmap()`-first ordering** and the CIDR constants (CGNAT, this-network, IPv6 metadata).
- `metadataHostBlocklist` — copy the cloud-metadata hostname set.
- `allowedSchemes = {http, https}`.
- `guardEndpoint(ctx, rawURL string, enforce bool) (*url.URL, error)`:
  1. `u, err := url.Parse(strings.TrimSpace(rawURL))` — reject parse error. *(unconditional)*
  2. scheme ∈ `allowedSchemes` else reject. *(unconditional — true no-op; no MCP server is non-http)*
  3. host in `metadataHostBlocklist` → reject. *(unconditional — true no-op; no MCP server lives at IMDS)*
  4. resolve host; for each IP run `classify`:
     - if `enforce` (hardened/production): block **any** classified range unless the host is in the configured allow-list. Fail-closed on any blocked record (never cherry-pick a public IP from a mixed set — D-24).
     - if `!enforce` (dev/local_trusted): block **only** the metadata class (link-local `169.254/16` + IPv6 metadata); **loopback/private allowed** so sidecars + `httptest` work.
  5. return the validated `*u`.

**Wire into `HTTPClient`:**
- `OpenHTTP`: call `guardEndpoint` once; store the validated string as `c.endpoint` (this routes the L207 `Do` through a validated value → the CodeQL string-path barrier).
- Thread the policy via a new `HTTPConfig` field (e.g. `Enforce bool` or a `Policy` struct). **Zero-value = permissive (dev)** so existing tests/sidecars are unchanged.
- Gate `enforce` on a new env knob **`AURA_MCP_SSRF_ENFORCE`** (default `false`). Phase 33 (PROF-01/PROF-04) will bind it to the runtime profile; shipping it default-off now keeps this phase self-contained and a strict no-op under dev. *(env naming per `AURA_<DOMAIN>_<UNIT>`.)*
- **Hardened-only Layer 2 (defense-in-depth):** when `enforce` AND `cfg.Client==nil`, install a hardened `http.Client` with `DialContext`+`Control` pinning (mirror `transport.go`) to defeat DNS-rebinding. Under dev, keep `http.DefaultClient` → no behavior/perf change (keep-alives intact).

**Why this clears CodeQL (and the residual risk):** the unconditional `url.Parse` + scheme allow-list + metadata block on the request-build path replicates the *string-path* barrier that `internal/web` uses (`fetcher.go:54-58`), and the optional DialContext mirrors web's *transport* barrier — the exact combination CodeQL already accepts in `internal/web`. **Confidence MEDIUM** on whether the conditional branch satisfies CodeQL's path-sensitivity: keep the parse+scheme+metadata barrier **unconditional** so there is always a real validating barrier on the path. **Acceptance is the CodeQL re-scan** (the requirement mandates "resolves to fixed") — if it stays open, escalate to an explicit host allow-list comparison (strongest barrier: the request host must equal a configured/registered server host) and/or make the DialContext classify unconditional with a dev-permissive policy.

**Tests (`internal/mcp/ssrf_test.go`, table-driven, no env → always run):**
- allowed public host passes; non-http scheme rejected; metadata hostname rejected (both policies); IMDS IP `169.254.169.254` rejected (both); `::ffff:169.254.169.254` rejected (Unmap pitfall); **loopback/private ALLOWED when `enforce=false`, BLOCKED when `enforce=true`**; allow-listed sidecar host passes under enforce.
- DNS-rebinding (enforce): injected resolver returns public-then-private → `Control` blocks (mirror `dnspin_integration_test.go`).
- **Regression:** existing `http_client_test.go` / `http_client_extra_test.go` (loopback `httptest`) must stay green → proves the default policy permits loopback.
- No-skip-as-green: if any tier is integration-tagged, its env helper `t.Fatal`s under `$CI`.

## Common Pitfalls

### Pitfall 1: Windows job silently broken by the helper swap
**What goes wrong:** `go build $(bash scripts/go_packages.sh)` under pwsh either errors or splats oddly.
**Why:** windows-latest defaults to pwsh; no `defaults.run.shell` set.
**Avoid:** add `shell: bash` to both windows-unit steps; require the windows-unit job to actually pass in the verification.
**Warning sign:** a windows-unit failure mentioning an empty/odd package argument, or a sub-second "build" that built nothing.

### Pitfall 2: Blanket private-IP block breaks MCP sidecars + tests
**What goes wrong:** copying `internal/web`'s unconditional loopback/private block.
**Why:** MCP's legitimate targets are loopback sidecars (`127.0.0.1:8091/8093/…`) and every HTTP test uses `httptest.NewServer` (loopback).
**Avoid:** default/dev policy permits loopback+private; only the metadata class is unconditional. Run `go test ./internal/mcp/` to catch regressions immediately.

### Pitfall 3: CodeQL alert dismissed, not fixed
**What goes wrong:** the alert "closes" via UI-dismiss or an inline suppression.
**Why:** misreading "resolves to fixed."
**Avoid:** keep an unconditional data-flow barrier; verify via re-scan that the state is **Fixed**, not Dismissed.
**Warning sign:** the Security tab shows "dismissed (won't fix)" rather than "fixed."

### Pitfall 4: The lint false-positives on scoped package specs or its own helper
**What goes wrong:** a naive `grep '\./\.\.\.'` flags `./internal/db/...` or `go_packages.sh`'s `go list ./...`.
**Why:** over-broad pattern / over-broad scope.
**Avoid:** anchor the standalone root token `(^|\s)\./\.\.\.(\s|$)` and scope to `.github/workflows/` only. Add the negative self-test so the lint can't silently pass.

### Pitfall 5: Re-staling `dist`
**What goes wrong:** touching `web/src` non-test files re-stales the committed bundle and reds `web-dist-freshness`.
**Avoid:** keep this phase's web edits to test files (not bundled). If shipped source must change, `cd web && npm ci && npm run build` and commit `internal/webui/dist` in the same commit.

## Code Examples

### F-015 lint (sketch — `scripts/check_ci_go_packages.sh`)
```bash
#!/usr/bin/env bash
# Reject raw root './...' in go build/test/vet/run + govulncheck inside CI workflows.
# Scope: .github/workflows ONLY (scripts/go_packages.sh's own `go list ./...` is legitimate).
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
# Standalone root token, ignoring comment lines. Scoped specs (./internal/db/...) do NOT match.
if grep -rnE '(^|[[:space:]])\./\.\.\.([[:space:]]|$)' .github/workflows/ \
   | grep -vE ':[[:space:]]*#'; then
  echo "F-015: raw root './...' found in a CI workflow — use \$(bash scripts/go_packages.sh)" >&2
  exit 1
fi
echo "F-015: no raw root './...' in .github/workflows/"
```

### SSRF guard wiring (sketch — `internal/mcp/ssrf.go`, mirrors internal/web)
```go
// Source: internal/web/ssrf.go (classify) + internal/web/fetcher.go:54-58 (string barrier)
var allowedSchemes = map[string]struct{}{"http": {}, "https": {}}

func guardEndpoint(ctx context.Context, raw string, enforce bool, res resolver) (*url.URL, error) {
    u, err := url.Parse(strings.TrimSpace(raw))
    if err != nil { return nil, fmt.Errorf("mcp: bad url: %w", err) }                 // unconditional
    if _, ok := allowedSchemes[strings.ToLower(u.Scheme)]; !ok {                       // unconditional
        return nil, fmt.Errorf("mcp: unsupported scheme %q", u.Scheme)
    }
    host := u.Hostname()
    if _, bad := metadataHostBlocklist[strings.ToLower(host)]; bad {                   // unconditional (no-op for legit)
        return nil, errBlockedHost
    }
    ips, err := res.LookupNetIP(ctx, "ip", host)
    if err != nil || len(ips) == 0 { return nil, errBlockedHost }
    for _, ip := range ips {
        reason, blocked := classify(ip)            // copy from internal/web (Unmap-first)
        if !blocked { continue }
        if enforce { return nil, errBlockedHost }  // hardened: block any private/loopback/metadata (unless allow-listed)
        if reason == "link_local" { return nil, errBlockedHost } // dev: block ONLY metadata; loopback/private OK
    }
    return u, nil
}
```

### CI edit (unit-test job, line 90)
```yaml
      - name: go test -race
        run: go test -race -count=1 $(bash scripts/go_packages.sh)   # was: ./...
```
### CI edit (windows-unit job, lines 117/128 — note shell)
```yaml
      - name: go build (windows/amd64)
        shell: bash
        run: go build $(bash scripts/go_packages.sh)                 # was: ./...
      - name: go test (unit tier — windows kill + atomic-write paths)
        shell: bash
        run: go test -count=1 $(bash scripts/go_packages.sh)         # was: ./...
```

## Validation Architecture

> nyquist_validation = true (config) → this section is the source for `31-VALIDATION.md`. No-skip-as-green honored: any `$CI` skip must `t.Fatal`.

### Test Framework
| Property | Value |
|----------|-------|
| Go framework | `go test` (toolchain from `go.mod`); no separate config file |
| Go quick run | `go test ./internal/mcp/` |
| Go full suite | `make test-race` (= `go test -race -count=1 $(bash scripts/go_packages.sh)`); `make quality-full` for the coverage floor |
| Frontend framework | vitest 4.1.9 + `@vitest/coverage-v8`; config `web/vitest.config.ts` |
| Frontend quick run | `cd web && npx vitest run` |
| Frontend full suite | `cd web && npm run test` (= `vitest run --coverage`, gates the four 85% thresholds) |
| Security re-scan | CodeQL (`.github/workflows/codeql.yml`, Go autobuild, full-repo) — runs on push/PR/weekly |

### Phase Criterion → Validation Map (1:1)
| Criterion | Behavior | Tier | Automated command / assertion | Pass looks like |
|-----------|----------|------|-------------------------------|-----------------|
| C1 file-size | No tracked source >600 LOC | CI-gate + hook | `bash scripts/check-file-size.sh` | exit 0, "all source files within the 600-LOC cap" (already true) |
| C2 dist fresh | committed bundle == fresh build | CI-gate | `cd web && npm ci && npm run build && git -C .. diff --exit-code -- internal/webui/dist/` | exit 0, empty diff (verified locally) |
| C3a F-015 swap | 4 jobs use the helper | CI-gate | swapped `unit-test` / `windows-unit` (×2, `shell: bash`) / `vulncheck` jobs all green | jobs pass with `$(bash scripts/go_packages.sh)` |
| C3b F-015 lint | no raw root `./...`; lint provably fails on one | CI-gate (+ self-test) | `bash scripts/check_ci_go_packages_test.sh` then `bash scripts/check_ci_go_packages.sh` | self-test exit 0 (caught a planted violation), gate exit 0 (0 real violations) |
| C4 coverage | four thresholds ≥85 | CI-gate (unit) | `cd web && npm run test` | exit 0; Stmts/Br/Fn/Lines ≥85 (today 90.88/85.45/90.89/92.85) |
| C5a SSRF unit | guard allows public/loopback-dev, blocks metadata/private-hardened | unit | `go test ./internal/mcp/` incl. `TestGuardEndpoint`/`TestClassify` table | exit 0; all rows incl. Unmap + rebinding |
| C5b SSRF regression | loopback `httptest` clients still work | unit | `go test ./internal/mcp/` (existing http_client tests) | exit 0 (proves dev default permits loopback) |
| C5c SSRF alert | `go/request-forgery` at http_client.go fixed | CI-gate + manual confirm | CodeQL re-scan after merge → Security tab state = **Fixed** | alert no longer open (NOT "dismissed") |

### Sampling Rate
- **Per task commit:** `go test ./internal/mcp/` (SSRF) or `bash scripts/check-file-size.sh` / `bash scripts/check_ci_go_packages.sh` (CI tasks).
- **Per wave merge:** `make test-race` + `cd web && npm run test`.
- **Phase gate:** `make quality-full` green (incl. ≥85% owned coverage) + a CodeQL re-scan confirming C5c before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] `internal/mcp/ssrf.go` — the guard (mirror of `internal/web/ssrf.go` + profile policy).
- [ ] `internal/mcp/ssrf_test.go` — table-driven guard tests incl. Unmap + rebinding + loopback-regression (covers SEC-08).
- [ ] `scripts/check_ci_go_packages.sh` + `scripts/check_ci_go_packages_test.sh` — lint + negative self-test (covers F-015 lint).
- [ ] (optional) hardened transport mirror in `internal/mcp/transport_ssrf.go` if Layer 2 is shipped now.
- *Framework install:* none — Go + vitest already present.

## Common Pitfalls already covered above (Windows shell, blanket block, dismiss-vs-fix, lint scope, dist re-stale).

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | C3 swaps, C5 build/test | ✓ | per `go.mod` | — |
| Node + npm | C2/C4 local verify | ✓ | **24.16.0 / 11.17.0** (matches CI `24.x`) | run in CI Linux job |
| Git Bash (Windows runner) | C3 windows-unit `shell: bash` | ✓ (GitHub-hosted) | bundled | — |
| CodeQL | C5 acceptance | ✓ (GitHub-hosted, post-push) | `codeql-action@v4` | local CodeQL CLI re-scan |
| WSL + Docker stack | full `make quality-full` (coverage floor) | ✓ (per CLAUDE.md) | — | CI knowledge job |

**No missing dependencies.** All five criteria are verifiable with tooling already on the host/CI. (Reminder: do not run `aura.exe`/`*.test.exe` natively on Windows — AV blocks; build/test in WSL or container. `node`/`npm`/`go test` are fine.)

## Security Domain

> security_enforcement enabled (absent = enabled). This phase **is** a security fix (SEC-08).

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V5 Input Validation | yes | `url.Parse` + scheme allow-list + host classification on the MCP endpoint (governance-install request body is the untrusted source) |
| V12.6 / SSRF Protection | yes | resolved-IP `classify` against private/loopback/link-local/metadata ranges + DNS-rebinding `Control` hook (hardened) |
| V1 Build/CI integrity | yes | F-015 package-list hygiene + the lint gate (no raw `./...`); supply-chain `govulncheck` job preserved |
| V6 Cryptography | no | — |
| V2/V3/V4 Auth/Session/Access | no (this phase) | the governance install auth is MUSR/Phase 36; here we defend the dial boundary |

### Known Threat Patterns for this stack
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| SSRF via attacker-set MCP URL (governance install → outbound `Do`) | Information Disclosure / Tampering / Elevation | `guardEndpoint` validation + IP classification; runtime `TrustBlocked` default is a second layer |
| DNS-rebinding (public A-record → private on dial) | Tampering | `net.Dialer.Control` re-check of the post-resolution IP (hardened) |
| IPv4-mapped-IPv6 metadata bypass (`::ffff:169.254.169.254`) | Information Disclosure | `Unmap()`-first in `classify` (copied pitfall) |
| Cloud-metadata exfil (`169.254.169.254`, `metadata.google.internal`) | Information Disclosure | unconditional hostname blocklist + link-local class (blocked in every profile) |
| Falsely-green CI (raw `./...` skipping owned packages / silent lint) | Repudiation / DoS-of-assurance | helper-sourced package list + lint with negative self-test (no-skip-as-green) |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The unconditional parse+scheme+metadata barrier (plus optional DialContext) clears CodeQL `go/request-forgery` for the conditional design | C5 | If CodeQL stays open, escalate to an explicit host allow-list comparison / unconditional classify. **Acceptance is the re-scan, so this self-corrects.** |
| A2 | The runtime profile machinery (PROF-01) is **not** yet wired at Phase 31, so the guard keys off a new `AURA_MCP_SSRF_ENFORCE` env knob (default off), with Phase 33 binding it to the profile | C5 | If a profile/config field already exists, key off that instead of a new env var. Discuss/planner should confirm the seam. |
| A3 | "No-op under dev" permits blocking the cloud-metadata class in **every** profile (no legitimate MCP server lives at IMDS), so the metadata block is unconditional | C5 / Security | If the operator insists dev be *byte-identical* permissive, make even the metadata block conditional — but then CodeQL is less likely to clear. Confirm in discuss-phase. |
| A4 | MCP-local guard (copy `classify`) is preferred over extracting shared `internal/netguard` for phase minimality | Stack / Open Q | If the team prefers single-source-of-truth now, extract instead (touches `internal/web`, parity-tested). |
| A5 | Local Node-24 build/coverage equals CI Linux results (criteria 2/4 green in CI) | C2/C4 | Cross-platform npm-lock drift affects install, not coverage; if CI disagrees, close the specific gap. Low risk. |

## Open Questions

1. **Shared SSRF extraction vs MCP-local copy (A4).**
   - Known: `internal/web` has the mutation-tested `classify`; duplicating security code risks drift.
   - Unclear: whether the team wants the `internal/netguard` extraction now (cleaner, but touches `internal/web` and is arguably QUAL-03/Phase-32 scope).
   - Recommendation: ship **MCP-local** for phase minimality; file the dedup as a Phase-32 QUAL-03 add-on. Let discuss/planner decide.
2. **Does the conditional guard clear CodeQL (A1)?**
   - Known: `internal/web`'s combined string+transport barrier is CodeQL-clean.
   - Unclear: CodeQL's path-sensitivity on the `if enforce` branch.
   - Recommendation: keep parse+scheme+metadata **unconditional**; gate the phase's C5 acceptance on the re-scan; have the allow-list escalation ready.
3. **Optional LoginPage split.**
   - Known: 592/600, 8 lines headroom, sibling split already exists.
   - Recommendation: leave as-is unless a later task adds tests there.

## Sources

### Primary (HIGH confidence — read/ran this session)
- `internal/mcp/http_client.go` (sink L207), `internal/mcp/transport.go` (L23 taint), `internal/mcp/managed_config.go` (`ManagedServer.URL`), `cmd/aura/serve_governance_write.go` (L214/223 remote source) — the SSRF data flow.
- `internal/web/ssrf.go`, `internal/web/transport.go`, `internal/web/client.go`, `internal/web/fetcher.go` (L54-58/115) — the proven CodeQL-clean mirror pattern.
- `.github/workflows/ci.yml` (L90/117/128/181 raw `./...`; no `defaults.run.shell`; `quality_snapshot` gate+self-test precedent L42-46), `.github/workflows/codeql.yml`, `.github/codeql/codeql-config.yml` (Go scan full-repo).
- `scripts/go_packages.sh`, `scripts/check-file-size.sh`, `scripts/web_dist_freshness.sh`, `scripts/ssrf_smoke.sh`, `scripts/coverage_gate.sh`, `Makefile` (`GO_PACKAGES`).
- `web/vitest.config.ts`, `web/package.json`, `web/stryker.config.json`.
- **Ran:** `vitest run --coverage` → 90.88/85.45/90.89/92.85; `npm run build` → zero dist drift; `check-file-size.sh` → exit 0; node `v24.16.0`/npm `11.17.0`; enumerated all workflow `./...` (exactly 4).
- `.claude/skills/golang-security/SKILL.md` — trust-boundary / defense-in-depth framing; `.planning/REQUIREMENTS.md`, `.planning/ROADMAP.md` (Phase 31), `CLAUDE.md`.

### Secondary (MEDIUM confidence)
- GitHub Actions default shell on `windows-latest` = pwsh; `shell: bash` (Git Bash) supported — training + corroborated by the absence of a `defaults.run.shell` and the existing all-pwsh windows-unit steps.
- CodeQL `go/request-forgery` barrier behavior — inferred from `internal/web` being clean under the same query; **verify by re-scan**.

## Metadata

**Confidence breakdown:**
- C1/C2/C4 state: **HIGH** — measured locally on CI-matching toolchain (exit codes + numbers above).
- C3 mechanics: **HIGH** — exact lines + proven Makefile/`build-and-lint` helper pattern + existing gate/self-test precedent; the only risk (Windows `shell: bash`) is identified with mitigation.
- C5 code pattern: **HIGH** (transplant of a known-good subsystem); C5 *CodeQL clearance of the conditional shape*: **MEDIUM** — acceptance is the re-scan with a documented escalation.

**Research date:** 2026-06-29
**Valid until:** ~2026-07-29 (stable; re-confirm dist/coverage if `web/` changes, and the 4 `ci.yml` line numbers if the workflow is edited before planning).
