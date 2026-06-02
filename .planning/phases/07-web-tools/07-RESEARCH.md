# Phase 7: Web Tools - Research

**Researched:** 2026-06-02
**Domain:** Go SSRF-hardened HTTP fetching, SearXNG meta-search integration, readability→markdown extraction, ToolResult-based tooling
**Confidence:** HIGH (codebase patterns + SSRF stdlib verified live; SearXNG/readability APIs verified via official docs + live `go doc`)

## Summary

Phase 7 reintroduces two LLM-facing deferred tools — `web_search` (SearXNG-backed) and `web_fetch` (readability→markdown) — as native Go inside Aura, NOT as a third-party MCP server. The architecture is a thin-tool / shared-engine split: a new `internal/web` package owns the SearXNG client, the SSRF-hardened HTTP transport, the per-conversation DNS pin cache, redirect revalidation, MIME/size gating, and the response cache; `internal/agent/tools/web_search.go` + `web_fetch.go` are ~70-LOC deferred adapters that marshal args, call `internal/web`, and route large output through the existing `tools.NewResult` spillover path (zero new spillover code). This mirrors how `internal/agent/tools/execute.go` delegates to `internal/sandbox`.

The dominant risk is SSRF. The verified-correct pattern is **resolve-once-then-pin**: resolve the hostname to its IPs, classify every resolved IP against a blocklist, cache the chosen IP keyed by `(conversation_id, hostname)` for `AURA_WEB_DNS_PIN_TTL_SEC=60`, then dial that **pinned IP** via a custom `net.Dialer.Control` hook that re-validates the post-dial address — closing the DNS-rebinding TOCTOU window the OWASP cheat-sheet describes. Auto-redirects MUST be disabled (`CheckRedirect` returning `http.ErrUseLastResponse`) and each `Location` hop manually re-validated and re-pinned. Go's `net/netip` stdlib gives most classification for free (`IsLoopback`, `IsPrivate`, `IsLinkLocalUnicast`, `IsMulticast`, `IsUnspecified`, `Is4In6`), but CGNAT `100.64.0.0/10`, "this network" `0.0.0.0/8`, broadcast, and the explicit metadata IPs/hostnames need explicit `netip.Prefix.Contains` / string checks.

**Primary recommendation:** Build `internal/web` as the shared engine (searxng client, ssrf+dnspin transport, html extraction, cache, config); keep the two tools as thin deferred adapters reusing `tools.NewResult`. Use `net/netip` for IP classification, a custom `http.Transport.DialContext` that dials only the pinned IP, `CheckRedirect: http.ErrUseLastResponse` with a manual revalidate-and-refetch loop, `codeberg.org/readeck/go-readability/v2` (`FromReader` → `Article.Node`) fed directly into `github.com/JohannesKaufmann/html-to-markdown/v2` (`ConvertNode`), and a small in-memory TTL cache. Add a `searxng` compose service with NO host port on a shared network plus a checked-in read-only `settings.yml` enabling `formats: [html, json]`.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| SearXNG query construction + JSON parse | `internal/web` (searxng client) | — | Pure HTTP client; no tool/business logic (mirrors PRD `internal/web/searxng.go`) |
| SSRF IP classification + DNS pin + redirect revalidation | `internal/web` (ssrf transport) | — | Security boundary must live below the tool layer so both tools share one hardened transport; no tool re-implements it |
| Readability extraction + markdown conversion + link dedup | `internal/web` (html extraction) | — | CPU transform over fetched bytes; no I/O, unit-testable in isolation |
| Response/search caching | `internal/web` (cache) | — | Cross-tool concern; lives below tools |
| Tool arg schema, deferred spec, preview/spillover routing | `internal/agent/tools` (web_search.go, web_fetch.go) | `internal/web` | Thin adapter; reuses `tools.NewResult` (D-25) — same shape as `execute.go` |
| Config load (`SEARXNG_URL`, `AURA_WEB_*`) | `internal/config` or `internal/web/config.go` read by `internal/config` | — | Follows existing `config.go` AURA_* convention + fail-fast |
| SearXNG container + read-only settings.yml | `compose.yaml` + checked-in settings file | — | Infra; shared app network, NO host port (D-02/D-03/D-04) |
| `aura web doctor` / smoke CLI | `cmd/aura` (hand-rolled switch) | `internal/web` | Mirrors `cmd/aura/exec.go` hand-parse pattern (D-43/D-44) |

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CAP-05 | Web tools — `web_search` via SearXNG; `web_fetch` via `codeberg.org/readeck/go-readability/v2` + `JohannesKaufmann/html-to-markdown/v2`; SSRF defense (IPv6 blocklist + DNS rebinding pin) | SSRF resolve-then-pin pattern (Q1/Q2), SearXNG JSON API (Q3), readability/markdown libs (Q4), MIME gating (Q5), retry/deadline (Q6), cache (Q7), test strategy (Q8), package layout (Q9) — all sections below |

## User Constraints (from CONTEXT.md)

### Locked Decisions

**Runtime and SearXNG**
- D-01: Native Go `web_search`/`web_fetch` inside Aura. No third-party MCP SearXNG runtime backend.
- D-02: Self-host SearXNG in Aura Compose. Default `SEARXNG_URL=http://searxng:8080/search`.
- D-03: Default Compose service MUST NOT publish a host port; use shared app network so SearXNG reaches the public internet without host exposure.
- D-04: Checked-in minimal SearXNG settings file mounted read-only; MUST enable `search.formats` with both `html` and `json`.
- D-05: Outside Compose, missing `SEARXNG_URL` is an error. No localhost autodetect, no public/random instance fallback.
- D-06: SearXNG missing/unreachable → structured `web_search_unavailable` with reason `searxng_not_configured` or `searxng_unreachable`.

**web_search Contract**
- D-07: Two-tier result shape. Default entries are stable `{title, url, snippet}`.
- D-08: `include_metadata: true` optionally adds normalized `engine`, `score`, `category`, `published_at`, `thumbnail`.
- D-09: Public controls: `query`, `max_results`, `category`, `language`, `time_range`, `domains`, `include_metadata`.
- D-10: Do NOT expose raw SearXNG pass-through params, raw backend responses, arbitrary `engines`, or model-controlled `safesearch`. Safesearch stays operator/config-level.
- D-11: Small Aura category enum, NOT raw SearXNG categories. Start with `general` and `news`; `images` only if planning confirms shape and tests.
- D-12: Domain filtering uses both query rewrite (`site:`) and post-filtering. Validate domains as hostnames only (no scheme/path).
- D-13: `example.com` matches `example.com` and subdomains. If filtering removes too many, return fewer results rather than leaking off-domain URLs.
- D-14: Search has strict 20s wall-clock deadline + at most one retry for transient network/`408`/`429`/`5xx` within that same deadline.

**web_fetch Contract**
- D-15: `web_fetch` accepts only `http`/`https` URLs.
- D-16: Readable HTML/XHTML only. PDFs, binaries, downloads, unsupported MIME, giant responses → structured errors, deferred.
- D-17: Default output is markdown article package `{title, url, content_md, links, warning?}`.
- D-18: No byline/published time/excerpt/site name/fetched time metadata in Phase 7.
- D-19: `links` is a deduped list of normalized absolute URLs in the readable content. NOT `{text, url}` objects.
- D-20: Use `codeberg.org/readeck/go-readability/v2` + `JohannesKaufmann/html-to-markdown/v2`.
- D-21: If `content_md` > `AURA_WEB_RESPONSE_CAP_BYTES=24000`, return short preview + `tool_result_id`; full content paged via `read_tool_output`.
- D-22: Low-quality extraction returns markdown with `warning` (`low_content` / `extraction_maybe_incomplete`); not automatically an error.
- D-23: Fetch has strict 30s wall-clock deadline + at most one retry for transient network/`408`/`429`/`5xx` within that same deadline.

**SSRF and Network Safety**
- D-24: SSRF defense fail-closed. Public web only; local/internal fetching blocked.
- D-25: Per-conversation DNS pinning `AURA_WEB_DNS_PIN_TTL_SEC=60`, IPv6/private blocking, IPv4-mapped IPv6 blocking, explicit metadata/internal hostname blocks.
- D-26: Model-visible SSRF errors structured but non-leaky, e.g. `{error:"blocked_url", reason:"private_or_metadata_target"}` / `{error:"blocked_url", reason:"redirect_to_blocked_target"}`.
- D-27: Do NOT expose resolved IPs, CIDRs, internal hostnames, response headers, body snippets, or redirect-chain details to the model.
- D-28: Production/model-visible output has NO debug escape hatch revealing SSRF internals. Tests and local debug logs may contain enough detail to assert the right block reason.
- D-29: Disable automatic redirects. Per redirect: parse, resolve, DNS/IP-validate, pin the next URL before following. Cap redirect hops.
- D-30: No allowlist in Phase 7. Future scoped `internal_fetch` handles controlled internal resources.

**Cache, Identity, Politeness**
- D-31: Cache both `web_search` and `web_fetch` responses.
- D-32: Persistent global disk cache opt-in only via `AURA_WEB_CACHE_PERSISTENT=true`. Default in-memory.
- D-33: Hybrid TTL: search short fixed TTL; fetched pages respect `Cache-Control` where practical, never store `no-store`, otherwise bounded default. Full RFC revalidation not required unless trivial.
- D-34: Aura-specific User-Agent e.g. `Aura/0.x web_fetch`. Do not pretend to be a browser by default.
- D-35: Same Aura User-Agent for SearXNG requests and direct fetches.
- D-36: Small per-host concurrency cap for `web_fetch`. No full RPM cooldown machinery unless trivial.
- D-37: No `robots.txt` enforcement in Phase 7.

**Error Taxonomy**
- D-38: Stable enum: `web_search_unavailable`, `blocked_url`, `unsupported_scheme`, `unsupported_content_type`, `response_too_large`, `timeout`, `http_error`, `extraction_failed`.
- D-39: Errors include `error`, short non-sensitive `message`, optional non-sensitive `reason`.
- D-40: Public HTTP errors may include `status_code` + safe message. No headers/body snippets.
- D-41: Errors stay inline as small structured objects. Only successful large content spills to `ToolResult`.
- D-42: Do NOT retry config errors, SSRF blocks, unsupported content types, or most `4xx`.

**Operator Surface and Smoke**
- D-43: Smoke surface: `aura tool web_search '{...}'`, `aura tool web_fetch '{...}'`, SSRF fixtures for cloud metadata / IPv4-mapped IPv6 / IPv6 link-local / `metadata.google.internal` / DNS rebinding.
- D-44: Minimal `aura web doctor` (or equivalent) checking `SEARXNG_URL`, reachability, JSON output, tiny search. No public fallback.
- D-45: Smoke/doctor output is human CLI only in Phase 7. JSON operator output deferred.

### Claude's Discretion
- Exact Go package layout (`internal/web` vs `internal/agent/tools` vs other).
- Exact cache implementation, key format, location, TTL numbers, memory bounds, eviction.
- Exact per-host fetch concurrency cap and redirect hop cap.
- Exact `aura web doctor` command spelling.
- Whether `images` category belongs in Phase 7.
- Unit/integration test layout (as long as SSRF + smoke guarantees covered).

### Deferred Ideas (OUT OF SCOPE)
- Generic MCP client/runtime for third-party MCP servers.
- PDF web document extraction.
- Browser/site crawling (searxNcrawl-style).
- Public/random SearXNG instance fallback.
- Scoped `internal_fetch` for controlled internal/local resources.
- JSON-capable operator/doctor output.
- Crawler-grade `robots.txt` enforcement and site-ingest politeness.

## Project Constraints (from CLAUDE.md)

- Go 1.26.3 (go.mod) — `t.Context`, `b.Loop`, `synctest`, `iter.Seq2` available `[VERIFIED: D:\Aura\go.mod]`.
- Env var convention `AURA_<DOMAIN>_<UNIT>` → `AURA_WEB_*` for web config. Third-party-canonical names (`SEARXNG_URL`) keep upstream spelling.
- **Deferred-tool pattern mandatory**: `web_search`/`web_fetch` have long descriptions + JSON schemas → `Deferred: true`. Manifest shows only Name + Summary until `tool_search` loads the full spec (`[VERIFIED: D:\Aura\internal\agent\tools\spec.go` package doc + `execute.go` `Deferred: true]`).
- **No file >600 LOC.** PRD targets each web file ≤300 LOC. Refactor-on-touch.
- **Coverage floor 85%** across full tag matrix (unit + integration + smoke) — overrides PRD 75/60.
- **NO test asilo nido**: realistic fixtures, goleak, race detector, property-based where indicated, no skip-as-green in CI.
- **Security tests deterministic, non-leaky, fail-closed** (matches D-24..D-28 and Phase 5 posture).
- Prompt byte-stability (Phase 6): web tools return via `ToolResult`, NEVER mutate `messages[0]`. Tool registration adds entries to the alphabetically-sorted manifest (cache-stable) — `[VERIFIED: D:\Aura\internal\agent\tools\manifest.go` Render() sorts by Name].
- Mutation testing ≥70% killed on critical file(s) (the SSRF classifier is the obvious critical file).
- `golangci-lint run ./...` = 0 before close.

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `net/netip` (stdlib) | Go 1.26 | IP parsing + classification for SSRF blocklist | Value-type `Addr`, allocation-free, `Is4In6()`/`IsPrivate()`/`IsLoopback()`/`IsLinkLocalUnicast()`/`IsMulticast()`/`IsUnspecified()` built in `[VERIFIED: go doc net/netip]` |
| `net/http` + `net.Dialer.Control` (stdlib) | Go 1.26 | Hardened transport: pinned-IP dial + redirect interception | OWASP-recommended app-layer SSRF defense; no third-party dep `[CITED: OWASP SSRF cheatsheet]` |
| `codeberg.org/readeck/go-readability/v2` | v2.1.1 (2026-02-04) | Readability extraction → `Article{Node *html.Node}` | Readeck fork of go-shiori (deprecated 2025-12-05); MIT; D-20-mandated `[VERIFIED: go doc + go list -m -versions]` |
| `github.com/JohannesKaufmann/html-to-markdown/v2` | v2.5.1 (2026-05-07) | HTML node → markdown | `ConvertNode(doc *html.Node, opts...)` accepts the readability `Node` directly; MIT; D-20-mandated `[VERIFIED: go list -m -versions + pkg.go.dev]` |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `golang.org/x/net/html` (indirect, already present) | v0.55.0 | `html.Node` type shared by readability + html-to-markdown | Already an indirect dep `[VERIFIED: D:\Aura\go.mod]`; the bridge type between the two libs |
| `golang.org/x/sync/semaphore` or buffered chan | v0.20.0 (present) | Per-host fetch concurrency cap (D-36) | `golang.org/x/sync` already a direct dep; a `map[host]chan struct{}` is simpler/zero-dep |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| stdlib `net/netip` blocklist | A CIDR-list lib | netip + a handful of `Prefix.Contains` is ~40 LOC, zero deps, mutation-testable; a lib adds supply-chain surface for trivial logic |
| `ConvertNode(Article.Node)` | `Article.RenderHTML(buf)` → `ConvertReader(buf)` | RenderHTML→reparse is a wasteful round-trip; `ConvertNode` consumes the `*html.Node` directly. Use RenderHTML only if html-to-markdown chokes on the live node tree |
| In-memory cache | persistent BoltDB/sqlite | D-32 keeps disk opt-in; default in-memory `map`+mutex+TTL avoids a new store dependency |

**Installation:**
```bash
go get codeberg.org/readeck/go-readability/v2@v2.1.1
go get github.com/JohannesKaufmann/html-to-markdown/v2@v2.5.1
```

**Version verification (run before writing the install task):**
```bash
go list -m -versions github.com/JohannesKaufmann/html-to-markdown/v2   # confirms exact path + versions
go list -m -versions codeberg.org/readeck/go-readability/v2
```
Both verified present this session. **Use the EXACT module paths above** (with `github.com/` prefix and `/v2` suffix) — see the Package Legitimacy Audit for why.

## Package Legitimacy Audit

> Run before the install task. slopcheck 0.6.1 was available this session.

| Package | Registry | Age | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-------------|-----------|-------------|
| `github.com/JohannesKaufmann/html-to-markdown/v2` | Go modules | v2.0.1-alpha → v2.5.1 (14 tags) | github.com/JohannesKaufmann/html-to-markdown | `[SLOP]` — **FALSE POSITIVE** (see note) | Approved (verify exact path) |
| `codeberg.org/readeck/go-readability/v2` | Go modules | v2.0.0 → v2.1.1 | codeberg.org/readeck/go-readability | `[OK]` ("no source repo linked" — codeberg, slopcheck can't introspect) | Approved |

**slopcheck [SLOP] false-positive note (IMPORTANT for the planner):** slopcheck ran `go get JohannesKaufmann/html-to-markdown` — WITHOUT the required `github.com/` host prefix and WITHOUT the `/v2` major-version suffix — and correctly reported that bare path does not resolve. The **full canonical module path** `github.com/JohannesKaufmann/html-to-markdown/v2` **does** resolve: `go list -m -versions` returned 14 published versions ending at v2.5.1, and the upstream repo is a long-established, widely-used Go library. This is a tool-invocation artifact of slopcheck's Go support (it strips the host and major-version), **not** a hallucinated package. Both packages are explicitly mandated by D-20 and roadmap Amendment #3. **Verification path is authoritative for Go modules:** `go list -m -versions <full-path>` + `go doc <full-path> <Symbol>` (both run and confirmed this session) — Go has no central registry to slopsquat the way npm/PyPI do; the module path IS the source URL.

**Packages removed due to [SLOP] verdict:** none (the one [SLOP] is a verified false positive — full path resolves with 14 versions).
**Packages flagged [SUS]:** none.

**Recommendation:** Planner MAY treat both as approved given the live `go list`/`go doc` verification, OR (conservative) gate each `go get` behind a one-line `checkpoint:human-verify` confirming the exact module path. The risk is near-zero because the import path equals the fetch URL and both were exercised live.

## Architecture Patterns

### System Architecture Diagram

```
                    LLM tool call (deferred spec loaded via tool_search)
                                     │
              ┌──────────────────────┴───────────────────────┐
              ▼                                               ▼
   web_search.go (adapter)                          web_fetch.go (adapter)
   parse args → web.Search                          parse args → web.Fetch
   format results → NewResult                       extract+convert → NewResult
              │                                               │
              ▼                                               ▼
   ┌───────────────────────┐                    ┌──────────────────────────────┐
   │ internal/web Search    │                    │ internal/web Fetch            │
   │ build /search query    │                    │  1. scheme check (http/https) │
   │  (site: rewrite,        │                    │  2. SSRF validate URL host    │
   │   category enum→raw,    │                    │     ─ resolve host → IPs      │
   │   lang, time_range)     │                    │     ─ classify each IP        │
   │ 20s deadline, 1 retry   │                    │     ─ pin (conv_id,host)→IP   │
   └──────────┬─────────────┘                    │  3. dial PINNED IP via         │
              │ HTTP GET                          │     Dialer.Control (revalidate)│
              ▼                                    │  4. CheckRedirect=ErrUseLast   │
   ┌───────────────────────┐                      │     → manual per-hop revalidate│
   │ SearXNG container      │                      │  5. Content-Type + size gate   │
   │ (shared net, NO host    │  public internet    │  6. 30s deadline, 1 retry      │
   │  port) format=json      │ ───────────────►    └──────────────┬────────────────┘
   └───────────────────────┘                                      │ bytes (≤cap)
              │                                                    ▼
              ▼ results[]                              ┌──────────────────────────┐
   post-filter domains (D-13)                          │ html extraction           │
   → {title,url,snippet}(+meta)                         │ readability.FromReader    │
                                                        │  → Article.Node           │
   ┌─────────────────────────────────────┐             │ html-to-markdown.ConvertNode│
   │ cache (in-mem default / disk opt-in) │◄────────────│ link dedup (normalize abs)│
   │ search: short fixed TTL              │             │ low-content → warning     │
   │ fetch: Cache-Control / bounded TTL   │             └──────────────────────────┘
   └─────────────────────────────────────┘
              │
              ▼  large content_md (>24000B)
   tools.NewResult → preview + sidecar (read_tool_output)
```

Trace the primary use case: model calls `web_fetch(url)` → adapter → `web.Fetch` runs scheme/SSRF/pin checks → dials pinned IP → revalidates redirects → gates content-type/size → extracts markdown + dedup links → caches → returns; adapter routes oversized markdown through `NewResult` spillover.

### Recommended Project Structure
```
internal/web/
├── config.go        # AURA_WEB_* + SEARXNG_URL load; fail-fast on missing URL (D-05)
├── searxng.go       # Search(ctx, params) → []Result; query build + JSON parse (D-07..D-14)
├── ssrf.go          # IP classification (netip blocklist) + Validate(host)→pinnedIP (D-24..D-28) ← CRITICAL FILE
├── dnspin.go        # (conv_id,host)→IP TTL cache, concurrency-safe (D-25)
├── transport.go     # http.Client w/ pinned-IP DialContext + CheckRedirect revalidation (D-29)
├── fetcher.go       # Fetch(ctx, convID, url) → Page; scheme/MIME/size gate + retry (D-15/16/23)
├── html.go          # ExtractMarkdown(Article.Node) → (title, md, links, warning) (D-17/19/22)
├── cache.go         # in-mem TTL default + disk opt-in (D-31/32/33)
└── errors.go        # stable error enum + non-leaky structured shapes (D-38..D-42)

internal/agent/tools/
├── web_search.go    # Deferred adapter (~70 LOC) → web.Search → NewResult
└── web_fetch.go     # Deferred adapter (~80 LOC) → web.Fetch → NewResult

cmd/aura/
└── web.go           # `aura web doctor` + `aura tool web_search/web_fetch` smoke (hand-rolled)

searxng/settings.yml # checked-in, read-only mount; formats:[html,json]
compose.yaml         # +searxng service, shared net, NO host port
```
Each file stays well under the 600-LOC cap and the PRD ≤300 target. `ssrf.go` is the mutation-testing target (≥70% killed).

### Pattern 1: Resolve-once-then-pin SSRF transport
**What:** Disable Go's implicit per-dial DNS; resolve the host yourself, classify, then dial only the validated IP.
**When to use:** Every `web_fetch` (and the SearXNG client should also dial only the configured service, but SearXNG is a trusted internal hop — the SSRF gate is for `web_fetch` user URLs).
**Example:**
```go
// Source: pattern per OWASP SSRF cheatsheet + net/netip stdlib (verified go doc)
// internal/web/transport.go — shape only; planner authors final code.
func (c *Client) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
    host, port, err := net.SplitHostPort(addr)
    if err != nil { return nil, err }
    // host here is the ORIGINAL hostname for the first hop, or the redirect target.
    pinnedIP, reason := c.validateAndPin(ctx, c.convID, host) // resolve→classify→pin
    if reason != "" { return nil, &BlockedURLError{Reason: reason} } // fail closed (D-24/26)
    // Dial the PINNED IP, not the hostname → no second DNS lookup → no rebinding window.
    d := net.Dialer{Timeout: dialTimeout}
    return d.DialContext(ctx, network, net.JoinHostPort(pinnedIP.String(), port))
}
```
**Landmine (TOCTOU):** If you classify the IP but then hand the *hostname* to `net.Dialer`, the dialer re-resolves and an attacker can rebind to `127.0.0.1` between your check and the dial. **You must dial the pinned IP string.** SNI/Host header are preserved automatically because `http.Transport` sets them from the request URL, not from the dial address — but verify TLS `ServerName` matches the original host (it does by default for the request URL).

**Alternative hook — `Dialer.Control`:** `net.Dialer.Control(network, address string, c syscall.RawConn) error` runs *after* DNS resolution and *before* connect, and `address` is the **post-resolution `ip:port`**. This is the canonical place to reject a rebinding swap even if you dialed by hostname:
```go
Control: func(network, address string, _ syscall.RawConn) error {
    host, _, _ := net.SplitHostPort(address) // host is now the RESOLVED IP
    ip, _ := netip.ParseAddr(host)
    if blocked(ip) { return &BlockedURLError{Reason: "private_or_metadata_target"} }
    return nil
}
```
Best practice: **do both** — pin+dial-by-IP (primary) AND a `Control` re-check (defense-in-depth) so even a code path that dials by name is caught.

### Pattern 2: IP classification with net/netip
**What:** Classify a resolved `netip.Addr` against the full blocklist.
**Example:**
```go
// Source: go doc net/netip (verified this session) + PRD §Slice 5 enumerated blocklist
var (
    cgnat      = netip.MustParsePrefix("100.64.0.0/10")   // not covered by IsPrivate
    thisNet    = netip.MustParsePrefix("0.0.0.0/8")       // "this network"
    benchmark  = netip.MustParsePrefix("198.18.0.0/15")   // optional
    metaV4     = netip.MustParseAddr("169.254.169.254")   // also caught by IsLinkLocalUnicast
    metaV6     = netip.MustParsePrefix("fd00:ec2::/32")   // GCP/AWS v6 metadata region
)
func blocked(ip netip.Addr) (string, bool) {
    if !ip.IsValid() { return "invalid_target", true }
    ip = ip.Unmap() // collapse ::ffff:127.0.0.1 → 127.0.0.1 so IsLoopback fires
    switch {
    case ip.IsLoopback():            return "loopback", true            // 127/8, ::1
    case ip.IsPrivate():             return "private", true             // 10/8,172.16/12,192.168/16,fc00::/7
    case ip.IsLinkLocalUnicast():    return "link_local", true          // 169.254/16, fe80::/10  → metadata
    case ip.IsLinkLocalMulticast(),
         ip.IsMulticast():           return "multicast", true           // 224/4, ff00::/8
    case ip.IsUnspecified():         return "unspecified", true         // 0.0.0.0, ::
    case cgnat.Contains(ip):         return "cgnat", true
    case thisNet.Contains(ip):       return "this_network", true
    }
    return "", false
}
```
**Landmine — `Is4In6`/`Unmap`:** `::ffff:169.254.169.254` will NOT match `IsLinkLocalUnicast()` unless you `Unmap()` first (the v6-mapped form reports as a v6 address). **Always `ip = ip.Unmap()` before the switch**, AND independently reject `Is4In6()` mapped forms is unnecessary once unmapped — but the SC#3 fixture explicitly tests `[::ffff:169.254.169.254]`, so a test must prove the unmapped path. (`netip.Addr.Is4In6()` verified to report `::ffff:0:0/96` membership.)

**Landmine — multiple A/AAAA records:** Resolve returns a *set*. **If ANY resolved IP is blocked, block the whole host** (don't cherry-pick a public one) — otherwise an attacker publishes `1.2.3.4` + `127.0.0.1` and you might dial the bad one. Pin the first *public* IP only if *all* are public; else fail closed.

### Pattern 3: Manual redirect revalidation
**What:** Disable auto-follow; re-run the full SSRF gate on each `Location`.
**Example:**
```go
// Source: OWASP SSRF cheatsheet (disable redirects) + net/http CheckRedirect
client := &http.Client{
    Transport: hardenedTransport, // pinned-IP DialContext above
    CheckRedirect: func(req *http.Request, via []*http.Request) error {
        return http.ErrUseLastResponse // never auto-follow (D-29)
    },
}
// fetch loop: for hop := 0; hop < maxHops; hop++ {
//   resp := client.Do(req)
//   if 3xx { loc := resp.Header.Get("Location"); next := resolveRef(req.URL, loc)
//            if next.Scheme not in {http,https} → unsupported_scheme
//            validateAndPin(next.Host) → on block: reason "redirect_to_blocked_target" (D-26)
//            req = new GET to next; continue }
//   else break }
```
**Landmine:** Returning `ErrUseLastResponse` means the body of the 3xx is the "last response" — you must construct the next request yourself and re-enter the SSRF gate. The PRD test (line 1869) requires the block to fire **at the redirect step, NOT the first dial** — assert the first hop to a public host succeeds and the second (to `169.254.169.254`) is rejected with `redirect_to_blocked_target`.

### Anti-Patterns to Avoid
- **Using `readability.FromURL(...)`** — it does its own HTTP fetch, bypassing the entire SSRF gate. NEVER call it. Use `FromReader(body, pageURL)` on bytes you fetched through the hardened client. `[VERIFIED: go doc — FromURL exists and self-fetches]`
- **Dialing by hostname after classifying the IP** — reintroduces the rebinding window (Pattern 1 landmine).
- **Leaking the resolved IP / internal hostname / redirect chain into the model-visible error** — violates D-27/D-28. The structured error carries only `error` + `reason` enum + safe `message`.
- **Cherry-picking one public IP from a mixed A-record set** — block if ANY is private (Pattern 2 landmine).
- **Re-sorting / inserting the web tools out of alphabetical order in the manifest** — breaks KV cache stability (manifest.go sorts by Name; just register them, the sort handles ordering).
- **Mutating `messages[0]`** — web tools return via `ToolResult` only (Phase 6 invariant).
- **Reviving the pre-rewrite 562-LOC `SearchTool` god class** — Slice 5 is ONLY web_search + web_fetch (PRD line 1890).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| IP range classification | Hand-parsed CIDR string compares | `net/netip` `IsPrivate/IsLoopback/IsLinkLocalUnicast/Is4In6/Unmap` + `Prefix.Contains` | Stdlib, allocation-free, correct RFC semantics; only CGNAT/this-net/metadata need explicit prefixes `[VERIFIED: go doc]` |
| Readability extraction | DOM cleaning heuristics | `codeberg.org/readeck/go-readability/v2` | Mozilla Readability port, battle-tested, D-20-mandated |
| HTML→Markdown | Tag-walking + string builder | `html-to-markdown/v2` `ConvertNode` | CommonMark-correct, handles tables/nesting, D-20-mandated |
| Spillover of large fetch output | New sidecar writer | `tools.NewResult(ctx, content)` | Existing D-25 path: cap → preview → sidecar → `read_tool_output` footer `[VERIFIED: result.go]` |
| Deferred tool manifest plumbing | New registration path | `reg.Register(...)` + `Deferred: true` | manifest auto-sorts + hides spec until tool_search `[VERIFIED: manifest.go/spec.go]` |
| HTTP error classification | Custom status parsing | mirror `openai_compat.HTTPError` (status + bounded body, key-safe) | Existing non-leaky error pattern `[VERIFIED: httperror.go]` |
| URL parsing / `site:` ref resolution | Regex on URLs | `net/url` `Parse` + `ResolveReference` | Stdlib; manual URL regex is a documented SSRF bypass vector `[CITED: OWASP]` |

**Key insight:** The SSRF defense is the only genuinely hard part, and Go's stdlib (`net/netip` + `Dialer.Control` + `CheckRedirect`) covers ~90% of it correctly. The remaining 10% (resolve-once-pin, mixed-record fail-closed, unmap-before-classify, redirect-hop revalidation) is exactly where hand-rolling goes wrong — encode those four as explicit table-driven tests.

## Common Pitfalls

### Pitfall 1: DNS rebinding TOCTOU (the marquee risk)
**What goes wrong:** Validate `evil.com` → resolves to `1.2.3.4` (public, passes); dial `evil.com` → re-resolves to `127.0.0.1` (attacker flipped DNS) → fetch hits localhost.
**Why it happens:** Two separate DNS lookups (one to check, one to dial) with an attacker-controlled TTL=0 record between them.
**How to avoid:** Resolve once, pin the IP per `(conversation_id, host)` for `AURA_WEB_DNS_PIN_TTL_SEC=60`, dial the pinned IP string. Re-check in `Dialer.Control` as belt-and-suspenders.
**Warning signs:** Code passes a hostname to `net.Dialer.DialContext`; any second `LookupHost` after validation.

### Pitfall 2: IPv4-mapped IPv6 bypass
**What goes wrong:** `http://[::ffff:169.254.169.254]/` slips past `IsLinkLocalUnicast()` because the address reports as IPv6.
**Why it happens:** `netip.Addr` keeps the mapped form distinct; `Is*` predicates evaluate the v6 representation.
**How to avoid:** `ip = ip.Unmap()` before classification (collapses `::ffff:a.b.c.d` to `a.b.c.d`). SC#3 explicitly tests this.
**Warning signs:** No `.Unmap()` call in the classifier; `Is4In6()` true addresses passing through.

### Pitfall 3: Redirect bypass
**What goes wrong:** First URL is public; it 302s to `http://169.254.169.254/`; Go auto-follows and fetches metadata.
**Why it happens:** Default `http.Client` follows up to 10 redirects with no per-hop validation.
**How to avoid:** `CheckRedirect: http.ErrUseLastResponse` + manual loop re-validating each `Location` (Pattern 3). Cap hops (PRD suggests 5; D-29 says "cap").
**Warning signs:** No `CheckRedirect` set; assertions only on the initial URL.

### Pitfall 4: Leaky error messages
**What goes wrong:** Error returns `"blocked: resolved to 127.0.0.1 for host internal.corp"` — exposes internal topology to the model.
**Why it happens:** Debugging convenience leaking into the model-visible channel.
**How to avoid:** Two-layer errors: a rich internal error (logged, asserted in tests) and a sanitized model-visible struct `{error:"blocked_url", reason:"private_or_metadata_target"}` (D-26/27/28). The tool adapter maps internal → sanitized.
**Warning signs:** Resolved IPs/hostnames/headers in the string returned to `ToolResult`.

### Pitfall 5: SearXNG returns 403 / HTML instead of JSON
**What goes wrong:** `format=json` request gets `403 Forbidden` because the instance's `search.formats` doesn't list `json`.
**Why it happens:** SearXNG ships HTML-only by default; JSON must be explicitly enabled. `[VERIFIED: docs.searxng.org + GitHub discussion #3542]`
**How to avoid:** Checked-in `settings.yml` with `search.formats: [html, json]` mounted read-only (D-04). Doctor command asserts a JSON round-trip (D-44).
**Warning signs:** `aura web doctor` gets non-JSON body; 403 from `/search`.

### Pitfall 6: Content-type / size not gated before reading body
**What goes wrong:** `web_fetch` of a 2 GB binary or a PDF either OOMs or feeds garbage to readability.
**Why it happens:** Reading the whole body before checking `Content-Type` or `Content-Length`.
**How to avoid:** Check `Content-Type` against an HTML/XHTML allowlist (`text/html`, `application/xhtml+xml`); reject others with `unsupported_content_type` (D-16). Wrap body in `io.LimitReader(body, AURA_WEB_RESPONSE_CAP_BYTES)` and detect overflow → `response_too_large` (D-38). `[CITED: MDN Content-Type]`
**Warning signs:** `io.ReadAll(resp.Body)` with no limit; no Content-Type switch.

## Code Examples

### Search query construction (D-09..D-14)
```go
// Source: docs.searxng.org/dev/search_api.html (verified) + D-12 site: rewrite
q := args.Query
if len(args.Domains) > 0 {
    var sites []string
    for _, d := range args.Domains { sites = append(sites, "site:"+d) } // hostnames only (D-12)
    q = q + " (" + strings.Join(sites, " OR ") + ")"
}
v := url.Values{}
v.Set("q", q)
v.Set("format", "json")                       // MUST be enabled in settings.yml (Pitfall 5)
v.Set("categories", auraCategoryToSearXNG(args.Category)) // enum→raw (D-11), not pass-through (D-10)
if args.Language != ""  { v.Set("language", args.Language) }
if args.TimeRange != "" { v.Set("time_range", args.TimeRange) } // day|month|year
v.Set("pageno", "1")
// GET SEARXNG_URL + "?" + v.Encode() with Aura User-Agent (D-35); 20s deadline (D-14)
```

### SearXNG JSON response shape (D-07/D-08)
```go
// Source: docs.searxng.org + searxng result schema; minimal MCP clients type only
// {title,url,content,score} but SearXNG's own JSON emits the wider set below.
type searxResult struct {
    Title         string   `json:"title"`
    URL           string   `json:"url"`
    Content       string   `json:"content"`        // → snippet
    Engine        string   `json:"engine"`         // metadata (D-08)
    Score         float64  `json:"score"`          // metadata
    Category      string   `json:"category"`       // metadata
    PublishedDate *string  `json:"publishedDate"`  // metadata → published_at (nullable!)
    Thumbnail     string   `json:"thumbnail"`       // metadata (images)
    ImgSrc        string   `json:"img_src"`         // images category
}
type searxResponse struct {
    Query           string        `json:"query"`
    NumberOfResults float64       `json:"number_of_results"`
    Results         []searxResult `json:"results"`
}
// Default tool output: {title,url,snippet:Content}. include_metadata adds the rest,
// NORMALIZED (publishedDate string → published_at), never the raw struct (D-10).
```

### Readability → markdown (D-17/D-19/D-22)
```go
// Source: go doc codeberg.org/readeck/go-readability/v2 + html-to-markdown/v2 (verified)
import (
    readability "codeberg.org/readeck/go-readability/v2"
    htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
)
art, err := readability.FromReader(bytes.NewReader(body), pageURL) // pageURL *url.URL for link resolution
if err != nil { return Page{}, &Err{Code: "extraction_failed"} }   // D-38
if art.Node == nil { /* blank → low_content warning (D-22) */ }
md, err := htmltomarkdown.ConvertNode(art.Node)                    // *html.Node consumed directly
title := art.Title()                                               // D-17; byline/excerpt/etc NOT used (D-18)
// links: walk art.Node for <a href>, ResolveReference against pageURL, dedup, normalized absolute (D-19)
if utf8.RuneCountInString(strings.TrimSpace(string(md))) < 250 {   // PRD 250-char threshold
    warning = "low_content"                                        // D-22 — not an error
}
```

### Link extraction + dedup (D-19)
```go
// Walk the readable node tree (NOT the raw page), collect absolute hrefs, dedup.
seen := map[string]struct{}{}
var links []string
var walk func(*html.Node)
walk = func(n *html.Node) {
    if n.Type == html.ElementNode && n.Data == "a" {
        for _, a := range n.Attr {
            if a.Key == "href" {
                if ref, err := url.Parse(a.Val); err == nil {
                    abs := pageURL.ResolveReference(ref).String() // normalized absolute
                    if _, ok := seen[abs]; !ok { seen[abs] = struct{}{}; links = append(links, abs) }
                }
            }
        }
    }
    for c := n.FirstChild; c != nil; c = c.NextSibling { walk(c) }
}
walk(art.Node)
// D-19: list of strings, NOT {text,url} objects.
```

### DNS pin cache (D-25)
```go
// Source: per-conversation pin; concurrency-safe map keyed by (conv,host).
type pinKey struct{ conv, host string }
type pinEntry struct{ ip netip.Addr; expires time.Time }
type DNSPin struct {
    mu  sync.Mutex
    m   map[pinKey]pinEntry
    ttl time.Duration // AURA_WEB_DNS_PIN_TTL_SEC=60
}
func (p *DNSPin) Pinned(conv, host string) (netip.Addr, bool) {
    p.mu.Lock(); defer p.mu.Unlock()
    e, ok := p.m[pinKey{conv, host}]
    if !ok || time.Now().After(e.expires) { return netip.Addr{}, false }
    return e.ip, true // reuse → SC#4: second fetch within 60s dials the same IP
}
// On a miss: resolve, classify ALL records (fail-closed if any blocked), pin first public IP.
```

## Runtime State Inventory

> Phase 7 is greenfield (new package + new compose service + new tools). No rename/refactor of existing runtime state. The one carry-forward is the SearXNG container, which is NEW.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | None — web tools are stateless apart from an ephemeral in-process cache. | none |
| Live service config | NEW `searxng` compose service + checked-in `settings.yml` (read-only mount). Not pre-existing; created this phase. | add to compose.yaml + commit settings file |
| OS-registered state | None. | none |
| Secrets/env vars | NEW: `SEARXNG_URL`, `AURA_WEB_DNS_PIN_TTL_SEC`, `AURA_WEB_RESPONSE_CAP_BYTES`, `AURA_WEB_CACHE_PERSISTENT`, search/fetch timeouts, User-Agent. None are secrets (no API keys — self-hosted). `AURA_WEB_FETCH_ALLOW_LOOPBACK`/`ALLOW_HOSTS` exist in PRD but D-30 says NO allowlist in Phase 7 — see Open Questions. | add to `internal/config` + `.env.example` |
| Build artifacts | None — pure Go addition + one new image pull (`searxng/searxng`). | `go get` the two new deps |

## Validation Architecture

> nyquist_validation assumed enabled (no `.planning/config.json` override found stating false). Aura also enforces coverage floor 85% across the full tag matrix and mutation ≥70% on critical files.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `httptest` + `go.uber.org/goleak` v1.3.0 + `pgregory.net/rapid` v1.3.0 (property) `[VERIFIED: go.mod]` |
| Config file | none — `go test` with build tags (`db_integration`, `neo4j_integration` exist; add `web_integration` for SearXNG-container tier) |
| Quick run command | `go test ./internal/web/... ./internal/agent/tools/...` |
| Full suite command | `go test -race -tags 'web_integration' ./...` (SearXNG container up) |

### Phase Requirements → Test Map
| Req / SC | Behavior | Test Type | Automated Command | File Exists? |
|----------|----------|-----------|-------------------|-------------|
| SC#1 | `web_search` returns ranked `{title,url,snippet}` p95≤2s | integration | `go test -tags web_integration -run TestSearch_Live ./internal/web/` | ❌ Wave 0 |
| SC#1 (parse) | SearXNG JSON fixture → normalized results, domain post-filter (D-13) | unit | `go test -run TestSearch_ParseAndFilter ./internal/web/` | ❌ Wave 0 |
| SC#2 | `web_fetch` → clean markdown (no nav/footer) | unit (httptest HTML fixture) | `go test -run TestFetch_Readability ./internal/web/` | ❌ Wave 0 |
| SC#3 | block `169.254.169.254`, `[::ffff:169.254.169.254]`, `[fe80::1]`, `metadata.google.internal` w/ explicit reason | unit (table-driven) | `go test -run TestBlocked_Classification ./internal/web/` | ❌ Wave 0 |
| SC#3 | redirect to blocked target rejected at hop, not first dial → `redirect_to_blocked_target` | unit (httptest 302) | `go test -run TestFetch_RedirectRevalidate ./internal/web/` | ❌ Wave 0 |
| SC#4 | DNS-rebinding: 2nd fetch within 60s reuses pinned IP | integration (fake resolver / dnslib fixture) | `go test -tags web_integration -run TestDNSRebind_Pin ./internal/web/` | ❌ Wave 0 |
| D-06 | missing/unreachable SearXNG → `web_search_unavailable{searxng_not_configured\|searxng_unreachable}` | unit | `go test -run TestSearch_Unavailable ./internal/web/` | ❌ Wave 0 |
| D-16 | non-HTML content-type → `unsupported_content_type`; oversized → `response_too_large` | unit (httptest) | `go test -run TestFetch_ContentGate ./internal/web/` | ❌ Wave 0 |
| D-21 | content_md>24000B → preview+sidecar via NewResult, paged by read_tool_output | unit | `go test -run TestWebFetch_Spillover ./internal/agent/tools/` | ❌ Wave 0 |
| D-22 | <250 char readable → `warning:low_content`, not error | unit | `go test -run TestFetch_LowContent ./internal/web/` | ❌ Wave 0 |
| D-26/27/28 | model-visible error has NO IP/hostname/header/redirect-chain | unit (assert sanitized struct + leak-scan) | `go test -run TestError_NonLeaky ./internal/web/` | ❌ Wave 0 |
| D-14/23/42 | one retry on 408/429/5xx within deadline; NO retry on SSRF/4xx/config | unit (httptest counting handler) | `go test -run TestRetry_Policy ./internal/web/` | ❌ Wave 0 |
| Discipline | zero goroutine leak across web tests | unit | `goleak.VerifyTestMain` in `internal/web/main_test.go` | ❌ Wave 0 |
| Discipline (critical) | mutation ≥70% killed on `ssrf.go` | manual (WSL go-mutesting) | `go-mutesting ./internal/web/ssrf.go` | manual-only |

### Observable Signals (per locked decision)
- **SSRF block (D-24..D-28):** the connection NEVER reaches the blocked target (assert via a sentinel httptest server that records hits — zero hits proves block) AND the returned error is the sanitized struct (assert exact `{error,reason}` and grep the serialized error for the resolved IP / internal host → must be ABSENT). Two assertions: artifact (no connection) + non-leak (sanitized).
- **DNS pin (SC#4):** inject a resolver that returns `1.2.3.4` first then `127.0.0.1`; assert the second fetch's dial target equals the first (`1.2.3.4`) within TTL — observe via a recording dialer, not the reply text.
- **Markdown quality (SC#2):** assert extracted markdown CONTAINS expected article text and does NOT contain known nav/footer fixture strings.
- **Spillover (D-21):** assert `ToolResult.Truncated==true`, sidecar file exists at the run-dir path, and `read_tool_output` returns the tail bytes.

### Sampling Rate
- **Per task commit:** `go test ./internal/web/... ./internal/agent/tools/...` (unit, sub-second; SSRF table tests are the core).
- **Per wave merge:** `go test -race -tags web_integration ./...` with SearXNG container up + the DNS-rebinding fixture live (no skip-as-green — `t.Fatal` under `$CI` if the container/fixture env is unset).
- **Phase gate:** full suite green + mutation ≥70% on `ssrf.go` + coverage ≥85% on `internal/web` owned surface + `golangci-lint`=0 before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] `internal/web/main_test.go` — `goleak.VerifyTestMain`
- [ ] `internal/web/ssrf_test.go` — table-driven IP classification (SC#3) + non-leak assertions
- [ ] `internal/web/fetcher_test.go` — httptest redirect-revalidate, content-type/size gate, retry policy
- [ ] `internal/web/dnspin_test.go` + a fake/injectable resolver (or `web_integration` dnslib fixture) for SC#4
- [ ] `internal/web/searxng_test.go` — JSON fixture parse + domain post-filter + unavailable
- [ ] `internal/agent/tools/web_fetch_test.go` — spillover via NewResult
- [ ] `scripts/ssrf_smoke.sh` (SC#3) + a SearXNG-up `web_integration` CI job exporting `SEARXNG_URL`
- [ ] DNS-rebinding fixture: Python `dnslib` (PRD/D-43) OR pure-Go injectable `Resolver` (preferred — keeps the tier in-process, no python dep). See Open Questions.

## Common Pitfalls (test-specific)
- **Injectable resolver vs real DNS:** for SC#4 determinism, define `type resolver interface{ LookupNetIP(ctx, network, host) ([]netip.Addr, error) }` and inject a fake that flips its answer on the 2nd call. A pure-Go fake avoids the python `dnslib` sidecar entirely and runs in the unit tier (the PRD/D-43 `dnslib` fixture can remain the integration-tier belt-and-suspenders). Confirm with the planner which tier owns SC#4.
- **goleak + DisableKeepAlives:** mirror `internal/sandbox/docker.go` — `DisableKeepAlives: true` on the transport so lingering `persistConn` goroutines don't trip short test subsets `[VERIFIED: docker.go]`.
- **Recording dialer:** to assert the pinned IP without real network, the hardened transport must accept an injectable `dialFunc` so tests can record the `addr` argument.

## Security Domain

> `security_enforcement` assumed enabled. Phase 7 is a high-stakes network-egress surface — the SSRF gate is the entire security thesis.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V5 Input Validation | yes | URL scheme allowlist (http/https, D-15), hostname-only domain validation (D-12), `net/url` parse (no regex), category enum (D-11) |
| V10 Malicious / SSRF (V10.2 / "Unintended Communication") | yes | resolve-then-pin, IP classification, redirect revalidation, metadata blocks (D-24..D-30) — the core of this phase |
| V12 Files/Resources | yes | Content-Type allowlist + `io.LimitReader` size cap (D-16) |
| V7 Error Handling | yes | Non-leaky structured errors (D-26/27/28); mirror `openai_compat.HTTPError` key-safety |
| V2 Authentication | no | self-hosted SearXNG, no creds; no model-facing auth surface |
| V6 Cryptography | no | no crypto introduced (TLS handled by stdlib `http`) |

### Known Threat Patterns for Go HTTP-egress + meta-search
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| DNS rebinding (TOCTOU) | Tampering / EoP | resolve-once-pin + dial-by-IP + `Dialer.Control` re-check (Pattern 1) |
| IPv4-mapped IPv6 bypass | EoP | `netip.Addr.Unmap()` before classify (Pitfall 2) |
| Redirect to internal target | EoP | `CheckRedirect: ErrUseLastResponse` + per-hop revalidate (Pattern 3) |
| Cloud-metadata exfiltration | Info Disclosure | explicit `169.254.169.254` + metadata hostname blocks; link-local block catches the IP |
| Mixed A/AAAA poisoning | Tampering | fail-closed if ANY resolved IP is private |
| Error-channel topology leak | Info Disclosure | two-layer errors; sanitized model-visible struct (D-26/27/28) |
| Decompression / large-body DoS | DoS | `io.LimitReader` at `AURA_WEB_RESPONSE_CAP_BYTES`; Content-Type gate before read |
| SearXNG param injection | Tampering | enum→raw category map; no pass-through params; no model-controlled `engines`/`safesearch` (D-10) |

**Relevant project skills to load during planning/impl:** `golang-security` (Go SSRF/egress patterns), `golang-context` (deadline propagation D-14/D-23), `golang-error-handling` (sentinel + `%w`, sanitized errors), `golang-testing` (table-driven + goleak + httptest), `property-based-testing` (rapid — fuzz the IP classifier against the blocklist), `codeql`/`semgrep-rule-creator` (taint a rule that flags `Dialer.DialContext(host)` after validation; flag `readability.FromURL` usage), `golang-database` (only if cache goes persistent).

## State of the Art
| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `go-shiori/go-readability` | `codeberg.org/readeck/go-readability/v2` | upstream deprecated 2025-12-05; Amendment #3 | import-path swap; same Article-parsing surface; `FromReader`/`Article.Node`/`RenderHTML` verified |
| `html-to-markdown` v1 | `html-to-markdown/v2` (`ConvertNode/ConvertString/ConvertReader`) | v2 line, latest v2.5.1 (2026-05-07) | plugin-based converter; `ConvertNode` consumes `*html.Node` directly |
| `net.IP` + manual CIDR slices | `net/netip` value types | Go 1.18+ standard | allocation-free, `Is4In6/Unmap/IsPrivate` built in |
| Third-party MCP SearXNG server | native Go client (D-01) | this phase | spike scored direct-dep 41/100 (SSRF holes: auto-redirect, 0.0.0.0 bind, no DNS pin) — reference value only |

**Deprecated/outdated:**
- `go-shiori/go-readability` — deprecated; do not use.
- `readability.FromURL` — exists but bypasses SSRF; forbidden (use `FromReader`).
- Pre-rewrite `SearchTool` 12-action god class (562 LOC) — do not revive (PRD line 1890).

## Assumptions Log
| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | SearXNG JSON result fields beyond `{title,url,content,score}` (`engine,category,publishedDate,thumbnail,img_src`) are present in the instance's JSON output | Code Examples (response shape) | If absent, `include_metadata` (D-08) yields nil fields — degrade gracefully (omit absent metadata); low risk since fields are nullable/optional anyway. Verify against the actual pinned `searxng/searxng` image during Wave 0. |
| A2 | `html-to-markdown/v2 ConvertNode` accepts the readability v2 `Article.Node` (`golang.org/x/net/html.Node`) without a render round-trip | Standard Stack / Code Examples | If the node tree is incompatible, fall back to `Article.RenderHTML(buf)` → `ConvertReader(buf)`. Both packages use `golang.org/x/net/html` so compatibility is very likely; confirm with a Wave-1 spike test. |
| A3 | `searxng/searxng` image listens on `:8080` inside the container (D-02 `searxng:8080`) | compose / Architecture | SearXNG default container port — confirm against the image's docs/Dockerfile when adding the service (some setups use 8888 for the dev server; the container image uses 8080 via its uwsgi/granian entrypoint). |
| A4 | `fd00:ec2::/32` is the correct GCP/AWS IPv6 metadata prefix to block | Pattern 2 | PRD line 1866 lists `fd00:ec2::254`; the link-local + ULA (`fc00::/7`, caught by `IsPrivate`) blocks already cover `fd00::/8`. Belt-and-suspenders; low risk. |
| A5 | A pure-Go injectable resolver satisfies the SC#4 DNS-rebinding test (vs the PRD/D-43 python `dnslib` fixture) | Validation Architecture | If the planner/reviewer insists on the literal `dnslib` fixture for the integration tier, add it as a `web_integration` belt-and-suspenders; the unit-tier fake still proves pin reuse. Confirm tier ownership. |
| A6 | Per-host concurrency cap can be a `map[host]chan struct{}` (no new dep) | Standard Stack supporting | `golang.org/x/sync/semaphore` is available if preferred; either is fine (Claude's Discretion per D-36). |

## Open Questions (RESOLVED)
1. **`AURA_WEB_FETCH_ALLOW_LOOPBACK` / `AURA_WEB_FETCH_ALLOW_HOSTS` — keep or drop?**
   - What we know: PRD §Slice 5 (lines 1867, 4757-4758) defines both env overrides; CONTEXT D-30 says "No allowlist is added in Phase 7."
   - What's unclear: Whether the loopback/host override envs count as the "allowlist" D-30 defers, or are a separate dev-ergonomics escape hatch.
   - Recommendation: Treat D-30 as authoritative for Phase 7 → do NOT implement the allowlist override envs (fail-closed, no escape hatch per D-28). Flag for the planner to confirm; the PRD is older than CONTEXT, and CONTEXT decisions win.
   - **(RESOLVED — planner):** Do NOT implement `AURA_WEB_FETCH_ALLOW_LOOPBACK`/`AURA_WEB_FETCH_ALLOW_HOSTS` in Phase 7 — CONTEXT D-30 wins over the older PRD. Enforced by 07-01 Task 1 + Task 3 acceptance criteria ("no `AURA_WEB_FETCH_ALLOW_*` string appears anywhere in the repo / config.go / .env.example"). Fail-closed, no escape hatch (D-28/D-30).

2. **SC#4 test tier — pure-Go fake resolver (unit) vs python `dnslib` (integration)?**
   - What we know: D-43 names a "Python `dnslib` fixture"; an injectable Go resolver gives a deterministic in-process unit test.
   - Recommendation: Implement the injectable resolver as the primary deterministic test; add the `dnslib` fixture only if the reviewer requires the literal roadmap wording. Planner decides per "test layout is Claude's Discretion."
   - **(RESOLVED — planner):** The pure-Go injectable resolver is the PRIMARY / deterministic SC#4 proof at the unit tier (07-02 Task 2, `TestDNSPin_TTL` + the injectable-resolver pin-reuse assertion). `internal/web/dnspin_integration_test.go` (07-04 Task 2, `//go:build web_integration`) is the belt-and-suspenders live tier against the running fixture. No literal python `dnslib` fixture is required.

3. **`images` category in Phase 7 (D-11)?**
   - What we know: D-11 defers `images` unless planning confirms shape + tests; image results carry `img_src`/`thumbnail`.
   - Recommendation: Ship `general` + `news` only in Phase 7; defer `images` (extra result shape + thumbnail handling without clear consumer). Low cost to add later.
   - **(RESOLVED — planner):** `images` is OUT for Phase 7 (D-11) — ship `general` + `news` only. The category enum in 07-03 Task 1 (`auraCategoryToSearXNG`, `TestSearch_CategoryEnum`) accepts only `general`/`news`; unknown categories are an inline error. Recorded in the 07-03 SUMMARY.

4. **SearXNG container internal port (8080 vs 8888)?** — confirm against the pinned image when authoring compose (A3). The dev server default is 8888; the container image entrypoint serves 8080 (D-02 assumes 8080).
   - **(RESOLVED — planner):** Assume 8080 per D-02 (`SEARXNG_URL=http://searxng:8080/search`). 07-01 Task 2 verifies the entrypoint listens on 8080 inside the pinned image at the human-verify checkpoint before pinning the tag; if the pinned image diverges, the checkpoint corrects the port and the SEARXNG_URL default before approval.

## Environment Availability
| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | build/test | ✓ | go 1.26.3 `[VERIFIED: go.mod]` | — |
| `codeberg.org/readeck/go-readability/v2` | web_fetch | ✓ (fetchable) | v2.1.1 `[VERIFIED: go list]` | — |
| `github.com/JohannesKaufmann/html-to-markdown/v2` | web_fetch | ✓ (fetchable) | v2.5.1 `[VERIFIED: go list]` | — |
| `searxng/searxng` Docker image | web_search runtime + integration tests | NEW (must pull) | pin a tag during planning | none — web_search unavailable without it (D-06 structured error) |
| Docker / Compose | run SearXNG service | ✓ (existing stack uses it) | — | integration tier `t.Skip` locally / `t.Fatal` under `$CI` (no-skip-as-green) |
| `go-mutesting` (WSL) | mutation gate on ssrf.go | ✓ per CLAUDE.md (WSL `~/go/bin`) | — | manual-only gate |
| slopcheck | package legitimacy | ✓ | 0.6.1 `[VERIFIED: pip]` | go list/go doc (authoritative for Go) |

**Missing dependencies with no fallback:** SearXNG image is new — must be pulled/pinned. web_search returns D-06 structured-unavailable without it (by design).
**Missing dependencies with fallback:** none blocking.

## Sources

### Primary (HIGH confidence)
- `go doc net/netip` (Addr.Is4In6/IsPrivate/IsLoopback/IsLinkLocalUnicast/IsMulticast/IsUnspecified/Unmap, ParseAddr/Prefix.Contains) — verified live this session
- `go doc codeberg.org/readeck/go-readability/v2` (FromReader/FromDocument/FromURL, Article{Node}, RenderHTML/Title/Byline) — verified live (dep added then reverted)
- `go list -m -versions` for both libs (v2.1.1 / v2.5.1, full tag histories) — verified live
- D:\Aura codebase: spec.go, result.go, execute.go, read_tool_output.go, manifest.go, current_time.go, config.go, compose.yaml, cmd/aura/exec.go, cmd/aura/main.go, internal/sandbox/docker.go, internal/llm/openai_compat/httperror.go — all read this session
- D:\Aura\.planning\phases\07-web-tools\07-CONTEXT.md (D-01..D-45), ROADMAP.md (Phase 7 SC + blocklist), REQUIREMENTS.md (CAP-05), prd.md §Slice 5 (lines 1838-1916)
- `https://docs.searxng.org/dev/search_api.html` — JSON API params + format=json activation requirement
- `https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html` — resolve-then-pin, redirect disable, metadata + IPv4-mapped IPv6 blocking
- `https://pkg.go.dev/github.com/JohannesKaufmann/html-to-markdown/v2` — ConvertNode/ConvertReader/ConvertString + plugins + MIT + v2.5.1

### Secondary (MEDIUM confidence)
- `https://github.com/searxng/searxng/blob/master/searx/settings.yml` + WebSearch (settings.yml `search.formats:[html,json]`, 403 on unset format, limiter/public_instance) — corroborated by docs + GH discussion #3542
- `https://github.com/ihor-sokoliuk/mcp-searxng/blob/main/src/types.ts` — minimal result typing (title/url/content/score); wider SearXNG fields confirmed via docs
- `https://github.com/OvertliDS/mcp-searxng-enhanced/blob/master/mcp_server.py` — SSRF anti-pattern catalogue (auto-redirect, 0.0.0.0 bind, no DNS pin, no IP classification) — what NOT to do
- `https://github.com/mrkrsl/web-search-mcp` (user-supplied 2026-06-02) — TypeScript, Playwright headless-browser scraper over Bing>Brave>DuckDuckGo; tools `full-web-search`/`get-web-search-summaries`/`get-single-web-page-content`. **Reference value LOW / anti-pattern:** conflicts with locked D-01/D-02 (SearXNG + native Go, no browser — browser/crawling is in CONTEXT `<deferred>`) and has zero SSRF defense (no DNS pin, no redirect validation, no private-IP block) — same failure class as the OvertliDS spike. Only transferable idea (two-tier summaries-vs-full-content shape) is already covered by D-07 + D-21. Does not alter any locked decision.

### Tertiary (LOW confidence)
- SearXNG JSON metadata fields beyond the minimal four (A1) — inferred from SearXNG result schema; verify against the pinned image in Wave 0
- SearXNG container internal port 8080 (A3) — verify against pinned image

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — both libs verified via live `go list`/`go doc`; netip verified via `go doc`
- SSRF architecture: HIGH — pattern is OWASP-cited + stdlib-verified; the four landmines are explicit
- SearXNG API: MEDIUM-HIGH — JSON params + format activation verified via official docs; exact metadata field set is A1
- Pitfalls: HIGH — each grounded in OWASP + stdlib semantics + the OvertliDS anti-pattern spike
- Codebase reuse claims: HIGH — every cited signature read from the actual file this session

**Research date:** 2026-06-02
**Valid until:** 2026-07-02 (stable; SearXNG image tag + the two Go libs are the only fast-moving inputs — re-verify versions at install time)
