---
phase: 07-web-tools
verified: 2026-06-02T00:00:00Z
status: passed
score: 4/4 must-haves verified
overrides_applied: 0
---

# Phase 7: Web Tools Verification Report

**Phase Goal:** `web_search` via SearXNG container + `web_fetch` via codeberg.org/readeck/go-readability/v2 + JohannesKaufmann/html-to-markdown/v2. SSRF defense: per-conversation DNS pin (AURA_WEB_DNS_PIN_TTL_SEC=60), IPv6 blocklist (::1/128, fe80::/10, fc00::/7, ::ffff:0:0/96), explicit cloud-metadata hostname blocks.
**Verified:** 2026-06-02
**Status:** PASSED
**Re-verification:** No — initial verification

---

## Goal Achievement

### Observable Truths (Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | `aura web tool web_search '{"query":"...","max_results":5}'` returns ranked SearXNG results {title,url,snippet} (p95 ≤ 2s advisory) | VERIFIED | `internal/web/searxng.go` — `Search()` builds `?format=json&categories=...&q=...`, `mapResults` returns `[]Result{Title,URL,Snippet}`. Integration test `TestSearch_Live` documented in `07-04-SUMMARY.md` as PASS ~1.01s. `docs/aura-quality-snapshot.md` records `TestSearch_Live PASS ~1.01s`. |
| 2 | `aura web tool web_fetch '{"url":"https://en.wikipedia.org/wiki/Knowledge_graph"}'` returns clean markdown (no nav/footer chrome) via readability + html-to-markdown | VERIFIED | `internal/web/html.go` — `ExtractMarkdown` uses `readability.FromReader` + `htmltomarkdown.ConvertNode` + `cleanMarkdown` (strips citation anchors, references tail, boilerplate). Gate-3 evidence: `content_md 36070 B → 16429 B`; no `#cite_note`/`#cite_ref`, no "From Wikipedia", confirmed in `07-04-SUMMARY.md`. `TestFetch_Readability` (fetcher_test.go line 64) asserts body present and FOOTER_FIXTURE_MARKER absent. |
| 3 | SSRF smoke (scripts/ssrf_smoke.sh) blocks http://169.254.169.254/, http://[::ffff:169.254.169.254]/, http://[fe80::1]/, http://metadata.google.internal/ with sanitized blocked_url (no IP/host leak) | VERIFIED | `scripts/ssrf_smoke.sh` tests all 4 targets, asserts `blocked_url` per target, and greps clean with `LEAK_RE='(\bip=|\bhost=|\bredirect=|Set-Cookie|X-Forwarded|127\.0\.0\.1|\b::1\b)'`. `ssrf.go` classifier: `Unmap()` fires before switch; `hostnameBlocklist` blocks `metadata.google.internal` before resolution. `07-04-SUMMARY.md` records `SC#3 ssrf_smoke: 4/4 blocked_url, grep-clean`. `TestBlocked_Classification` + `TestHostnameBlocklist` + `TestError_NonLeaky` all green per `07-VALIDATION.md`. |
| 4 | DNS-rebinding: the second web_fetch to the same host reuses the first pinned IP within AURA_WEB_DNS_PIN_TTL_SEC=60 | VERIFIED | `internal/web/dnspin.go` — `dnsPin.Pinned(conv,host)` reuses within TTL, keyed by `pinKey{conv,host}`. `internal/web/transport.go` — `dialContext` calls `validateAndPin` then dials `net.JoinHostPort(pinnedIP.String(), port)` (never the hostname). `dnspin_integration_test.go` — `TestDNSRebind` fetches the same host twice within TTL from the same conversation and asserts `first == second` pin. `07-04-SUMMARY.md` records `SC#4 TestDNSRebind: PASS`. `TestDNSPin_TTL` (dnspin_test.go) proves pin hit within TTL and miss after expiry with injectable clock. |

**Score:** 4/4 truths verified

---

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/web/ssrf.go` | IP classifier + validateAndPin (mutation-gate target) | VERIFIED (SUBSTANTIVE + WIRED) | 108 LOC; `classify(ip)` with `ip.Unmap()` first; `hostnameBlocklist` 5 entries; `validateAndPin` guard; `cgnatPrefix`, `thisNetPrefix`, `metadataV6Pfx` constants. Contains `Unmap`. Mutation 94.4% (17/18). |
| `internal/web/dnspin.go` | Per-(conv,host) TTL pin cache | VERIFIED (SUBSTANTIVE + WIRED) | 67 LOC; `pinKey{conv,host}`, `pinEntry{ip,expires}`, `sync.Mutex`-guarded map, injectable `now func()`, `Pinned`/`Pin` methods. References `AURA_WEB_DNS_PIN_TTL_SEC` via injected `ttlSec` arg from config. |
| `internal/web/transport.go` | Pinned-IP DialContext + Control recheck + CheckRedirect ErrUseLastResponse | VERIFIED (SUBSTANTIVE + WIRED) | 109 LOC; `hardenedTransport`; `dialContext` resolves+pins+dials `net.JoinHostPort(pinnedIP.String(), port)`; `control` hook re-parses and classifies post-resolution IP; `CheckRedirect` returns `http.ErrUseLastResponse`; `DisableKeepAlives: true`. |
| `internal/web/errors.go` | D-38 enum + sanitized WebError + sanitize() chokepoint | VERIFIED (SUBSTANTIVE + WIRED) | 141 LOC; `CodeBlockedURL`, `CodeSearchUnavailable`, etc.; `WebError` with hand-rolled `MarshalJSON` omitting zero-value fields; `internalError` with unexported sensitive fields; `sanitize()` strips sensitive fields; `AsWebError` unwraps chain. |
| `internal/web/searxng.go` | SearXNG client: query build + JSON parse + domain post-filter | VERIFIED (SUBSTANTIVE + WIRED) | 298 LOC; `buildQuery` emits `format=json`; `auraCategoryToSearXNG` enum general/news; `siteRewrite` + `domainAllowed`; `searxGet` with one retry on 408/429/5xx; missing `SEARXNG_URL` returns `web_search_unavailable{searxng_not_configured}`; `DisableKeepAlives: true` on search HTTP client (goleak fix). |
| `internal/web/fetcher.go` | Fetch state machine: scheme/SSRF/redirect/MIME/size gate + one-retry | VERIFIED (SUBSTANTIVE + WIRED) | 264 LOC; `allowedSchemes {http,https}`; `allowedContentTypes {text/html, application/xhtml+xml}`; `doHops` manual redirect loop; `resolveRedirect` re-calls `validateAndPin` per hop; `gateAndRead` with `io.LimitReader(cap+1)` overflow detection; `fetchBody` one-retry on `errRetryable`; `redirect_to_blocked_target` returned at redirect hop, target never dialed. |
| `internal/web/html.go` | readability.FromReader + ConvertNode + link dedup + low_content | VERIFIED (SUBSTANTIVE + WIRED) | 156 LOC; `readability.FromReader` (never `FromURL`); `htmltomarkdown.ConvertNode`; `cleanMarkdown` strips citation anchors + references tail + boilerplate + converter artifacts; `extractLinks` deduped absolute URLs; `< 250 runes → warning="low_content"`. |
| `internal/web/cache.go` | In-mem TTL cache + disk opt-in via AURA_WEB_CACHE_PERSISTENT | VERIFIED (SUBSTANTIVE + WIRED) | 131 LOC; `defaultCacheTTL = 5m`; `cache{mu,m,defaultTTL,persistent,dir,now}`; `getDiskLocked`/`setDiskLocked` best-effort; `cacheKey` uses `sha256` for collision resistance. |
| `internal/web/client.go` | web.Client facade wiring transport+cache+config | VERIFIED (SUBSTANTIVE + WIRED) | 40 LOC; `NewClient` wires `newDNSPin`, `newGuard`, `newHardenedTransport`, `newCache`, `newHostThrottle` from config. `*web.Client` satisfies both `searchEngine` and `fetchEngine` interfaces (compile-time assertion in `web_search_test.go`). |
| `internal/web/throttle.go` | Per-host concurrency cap (D-36) | VERIFIED (SUBSTANTIVE + WIRED) | 49 LOC; `perHostLimit = 2`; `hostThrottle` with semaphore channel per host; `acquire`/`release` pattern. Used in `fetcher.go` `Fetch()`. |
| `internal/agent/tools/web_search.go` | Deferred:true adapter → web.Search | VERIFIED (SUBSTANTIVE + WIRED) | 100 LOC; `Spec().Deferred = true`; exposes D-09 controls only (no engines/safesearch/pageno/format); maps `*web.WebError` to inline `NewResult`; on success returns `NewResult(ctx, resultsJSON)`. |
| `internal/agent/tools/web_fetch.go` | Deferred:true adapter → web.Fetch → NewResult spillover | VERIFIED (SUBSTANTIVE + WIRED) | 112 LOC; `Spec().Deferred = true`; reads convID from tool-call context via `fetchConvID`; maps `*web.WebError` to inline `NewResult`; large `content_md` spills to sidecar via `NewResult` with zero new code (D-21). `webErrorResult` uses `web.AsWebError` then `we.JSON()` via `NewResult`. |
| `cmd/aura/web.go` | aura web doctor + aura web tool web_search/web_fetch CLI | VERIFIED (SUBSTANTIVE + WIRED) | 164 LOC; hand-parsed switch; `runWebDoctor` uses `config.LoadDB()` (no OPENROUTER_API_KEY); exits 64 (unconfigured), 70 (unreachable), 0 (OK); no public fallback; `runWebTool` delegates to `web.NewClient`. |
| `scripts/ssrf_smoke.sh` | SC#3 SSRF block smoke (4 targets) | VERIFIED (SUBSTANTIVE + WIRED) | 59 lines; tests `http://169.254.169.254/latest/meta-data/`, `http://[::ffff:169.254.169.254]/`, `http://[fe80::1]/`, `http://metadata.google.internal/`; asserts `blocked_url` per target; grep-cleans `ip=|host=|redirect=|Set-Cookie|X-Forwarded|127\.0\.0\.1|::1`. |
| `internal/web/dnspin_integration_test.go` | SC#4 DNS-rebinding belt-and-suspenders (web_integration tier) | VERIFIED (SUBSTANTIVE + WIRED) | `//go:build web_integration`; `TestDNSRebind` fetches Wikipedia twice within TTL in same conversation and asserts `first == second` pin; uses `envOrSkip(t, "SEARXNG_URL")` which `t.Fatal`s under `$CI`. |
| `compose.yaml` | searxng service, NO ports, read-only settings.yml mount | VERIFIED | `searxng:` service at line 140; image `searxng/searxng:2026.5.31-7159b8aed` (concrete pin); `container_name: aura-searxng`; `./searxng/settings.yml:/etc/searxng/settings.yml:ro`; no `ports:` key for searxng service. |
| `searxng/settings.yml` | formats: [html, json] + secret_key + limiter | VERIFIED | `formats:` with both html and json at line 23; `secret_key:` at line 36; `limiter: false` at line 39. |
| `internal/config/config.go` | AURA_WEB_* + SEARXNG_URL fields, no boot-fatal | VERIFIED | `SearxngURL: os.Getenv("SEARXNG_URL")` (empty default, no `envDefault` with fallback); `WebDNSPinTTLSec: envIntDefault("AURA_WEB_DNS_PIN_TTL_SEC", 60)`; `WebFetchMaxBodyBytes: envIntDefault("AURA_WEB_FETCH_MAX_BODY_BYTES", 5_000_000)`; `WebCachePersistent: envBoolDefault("AURA_WEB_CACHE_PERSISTENT", false)`. `LoadDB()` does not require LLM key and does not fail on missing `SEARXNG_URL`. No `AURA_WEB_FETCH_ALLOW_*` anywhere. |
| `internal/web/main_test.go` | goleak.VerifyTestMain | VERIFIED | `//go:build !web_integration`; `goleak.VerifyTestMain(m)`. Integration tier has its own goleak TestMain in `searxng_integration_test.go`. |
| `.env.example` | SEARXNG_URL, AURA_WEB_DNS_PIN_TTL_SEC=60, AURA_WEB_FETCH_MAX_BODY_BYTES=5000000, AURA_WEB_CACHE_PERSISTENT=false | VERIFIED | All four entries present. No allowlist escape hatch entries. |
| `go.mod` | codeberg.org/readeck/go-readability/v2 v2.1.1 + github.com/JohannesKaufmann/html-to-markdown/v2 v2.5.1 | VERIFIED | Both at exact pinned versions. |

---

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `cmd/aura/main.go buildRegistry` | `tools.WebSearch` + `tools.WebFetch` | `reg.Register(&tools.WebSearch{Engine: webEngine})` + `reg.Register(&tools.WebFetch{Engine: webEngine})` | WIRED | Lines 85-86 of `cmd/aura/main.go`. One shared `web.NewClient(config.LoadDB())` engine. |
| `cmd/aura/main.go switch` | `cmd/aura/web.go runWeb` | `case "web": runWeb(os.Args[2:])` | WIRED | Line 44-45 of `cmd/aura/main.go`. |
| `internal/web/transport.go dialContext` | `internal/web/ssrf.go validateAndPin` | `h.guard.validateAndPin(ctx, convIDFrom(ctx), host)` before dial | WIRED | Line 85 of `transport.go`. SSRF gate fires before any dial. |
| `internal/web/ssrf.go validateAndPin` | `internal/web/dnspin.go` | `g.pin.Pinned(conv, host)` (hit) + `g.pin.Pin(conv, host, first)` (miss) | WIRED | Lines 89, 105 of `ssrf.go`. |
| `internal/web/transport.go Dialer.Control` | `internal/web/ssrf.go classify` | `classify(ip)` inside `ht.control()` | WIRED | Lines 101-106 of `transport.go`. Defense-in-depth post-resolution recheck. |
| `internal/web/fetcher.go resolveRedirect` | `internal/web/ssrf.go validateAndPin` | `c.transport.guard.validateAndPin(ctx, convID, next.Hostname())` per hop | WIRED | Line 163 of `fetcher.go`. SC#3 redirect revalidation. |
| `internal/web/html.go ExtractMarkdown` | `codeberg.org/readeck/go-readability/v2` + `html-to-markdown/v2` | `readability.FromReader` → `htmltomarkdown.ConvertNode` | WIRED | Lines 53, 109 of `html.go`. Never `FromURL`. |
| `internal/agent/tools/web_fetch.go` | `tools.NewResult` (spillover) | `NewResult(ctx, string(out))` on success | WIRED | Line 81 of `web_fetch.go`. Large markdown auto-spills via NewResult. |
| `internal/agent/tools/web_search.go` + `web_fetch.go` | `web.AsWebError` sanitizer | `webErrorResult` calls `web.AsWebError(err)` → `we.JSON()` → `NewResult` | WIRED | Lines 102-111 of `web_fetch.go`; line 93 of `web_search.go`. |
| `internal/web/searxng_integration_test.go` + `dnspin_integration_test.go` | `envOrSkip` t.Fatal-under-CI | `envOrSkip(t, "SEARXNG_URL")` → `t.Fatalf(...)` when `CI` set | WIRED | `searxng_integration_test.go` line 38; `dnspin_integration_test.go` line 24. No-skip-as-green enforced. |
| `.github/workflows/ci.yml web-integration-test` | live SearXNG container + web_integration tier | Docker container joined to `aura_aura-web` network; `SEARXNG_URL=http://searxng:8080/search` | WIRED | CI job at line 183 of `ci.yml`. SSRF smoke runs on host; live tier runs inside the compose network. |

---

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|--------------|--------|--------------------|--------|
| `internal/web/searxng.go Search()` | `[]Result` | `searxGet` → HTTP GET `SEARXNG_URL?format=json&...` → `decodeSearx` → `mapResults` | Yes — live HTTP fetch to SearXNG, parsed JSON, normalized to `[]Result` | FLOWING |
| `internal/web/fetcher.go Fetch()` | `Page{ContentMD,...}` | `doHops` → `gateAndRead` body → `ExtractMarkdown(body, finalURL)` | Yes — live HTTP fetch, readability extraction, markdown conversion | FLOWING |
| `internal/web/html.go ExtractMarkdown()` | `title, md, links` | `readability.FromReader(bytes.NewReader(body), pageURL)` then `convertNode(art.Node)` | Yes — operates on already-fetched bytes; no self-fetch | FLOWING |
| `internal/web/dnspin.go` | `netip.Addr` pin | `g.res.LookupNetIP(ctx, "ip", host)` → first public IP after classify-all | Yes — real DNS lookup on miss, injectable in tests | FLOWING |
| `internal/agent/tools/web_fetch.go` | `Page` → `ToolResult` | `e.Engine.Fetch(ctx, fetchConvID(ctx), a.URL)` → `json.Marshal(page)` → `NewResult(ctx, string(out))` | Yes — delegates to real engine; convID from tool-call context | FLOWING |

---

### Behavioral Spot-Checks

All behavioral checks were verified by live Gate-3 run (documented in `07-04-SUMMARY.md`). Programmatic verification against a live stack is a human-gated checkpoint (Task 4 of Plan 04); the unit-tier spot-checks run without a server:

| Behavior | Evidence | Status |
|----------|----------|--------|
| `web_search` returns ranked {title,url,snippet} results | `TestSearch_ParseAndFilter` (07-VALIDATION.md row 07-03-T1 green); `TestWebSearch_Success` (web_search_test.go) asserts result list inline; Gate-3 `TestSearch_Live PASS ~1.01s` | PASS |
| `web_fetch` returns clean markdown | `TestFetch_Readability` asserts body present + no FOOTER_FIXTURE_MARKER; Gate-3 Wikipedia 36070→16429 bytes clean | PASS |
| SSRF blocks 4 canonical targets | `TestBlocked_Classification` (22 rows including `::ffff:169.254.169.254`); `TestHostnameBlocklist` (5 metadata hostnames); Gate-3 `ssrf_smoke: 4/4 blocked_url` | PASS |
| DNS pin reuse within TTL | `TestDNSPin_TTL` (injectable clock, TTL miss after expiry); `TestValidateAndPin_PinReuse` (counting resolver, zero second resolve); Gate-3 `TestDNSRebind: PASS` | PASS |
| Deferred:true manifest | `TestWebSearch_DeferredSpec` asserts both tools `Deferred:true` and schema has no forbidden controls | PASS |
| Large markdown spillover | `TestWebFetch_Spillover` asserts `Truncated==true`, sidecar written, `read_tool_output` returns tail bytes | PASS |
| Sanitized inline errors | `TestWeb_SanitizedInlineError` asserts only `{error,reason,message,status_code}` keys; greps clean for `169.254.169.254`, `127.0.0.1`, `::1`, `Set-Cookie`, `ip=`, `host=`, `redirect=` | PASS |

---

### Probe Execution

No `scripts/*/tests/probe-*.sh` probes declared for this phase. `scripts/ssrf_smoke.sh` is the equivalent smoke script; its live run is a human-verify checkpoint (Task 4 of Plan 04). The script's code structure and target coverage are verified above under Key Links.

---

### Requirements Coverage

| Requirement | Source Plans | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| CAP-05 | 07-01, 07-02, 07-03, 07-04 | Web tools — `web_search` via SearXNG; `web_fetch` via go-readability/v2 + html-to-markdown/v2; SSRF defense IPv6 blocklist + DNS rebinding pin | SATISFIED | All 4 ROADMAP SCs verified (PASSED above). `REQUIREMENTS.md` line 114 marks CAP-05 `Phase 7 — Web Tools` as `Complete`. All four plan files carry `requirements: [CAP-05]`. |

No orphaned requirements found — only CAP-05 is mapped to Phase 7 in REQUIREMENTS.md.

---

### Anti-Patterns Found

No blockers. Review of all Phase 7 source files (internal/web/*.go, internal/agent/tools/web_*.go, cmd/aura/web.go, scripts/ssrf_smoke.sh):

| File | Pattern | Severity | Assessment |
|------|---------|----------|------------|
| `internal/web/ssrf.go` | `metadataV6Pfx` branch is dead code (fd00:ec2::/32 is inside ULA fc00::/7, caught by `IsPrivate()` first) | INFO | Acknowledged in `07-04-SUMMARY.md` and `ssrf_test.go` comment (line 39-41). Not a security gap — the IP is still blocked; the branch is merely unreachable. Constitutes the lone survivor in mutation testing. Flagged for behavior-neutral cleanup; does not prevent phase close. |
| All files | No `TBD`, `FIXME`, `XXX` markers | CLEAN | Verified by grep. |
| All files | No `return null`, `return {}`, `return []`, unimplemented stubs | CLEAN | Verified by code review. |
| `internal/web/fetcher.go` (comment) | Word "allowlist" appears 5 times | INFO | All occurrences are comments describing the MIME/scheme allowlists, not security-bypass escape hatches. No `AURA_WEB_FETCH_ALLOW_*` env or config field exists anywhere in the codebase. |

---

### Human Verification Required

The following items cannot be verified programmatically and were gate-verified by the human operator at the Task 4 checkpoint (07-04 Plan):

1. **Live SC#1 — web_search latency and result quality**
   - Test: `aura web tool web_search '{"query":"...","max_results":5}'` against running SearXNG stack
   - Expected: ranked {title,url,snippet} results within ~2s
   - Why human: requires live SearXNG container + public internet
   - Gate-3 evidence in `07-04-SUMMARY.md`: `TestSearch_Live PASS ~1.01s`

2. **Live SC#2 — web_fetch clean markdown**
   - Test: `aura web tool web_fetch '{"url":"https://en.wikipedia.org/wiki/Knowledge_graph"}'`
   - Expected: clean markdown, no nav/footer chrome
   - Why human: requires real HTTP fetch + readability extraction from live page
   - Gate-3 evidence: `content_md 36070 B → 16429 B, no cite_note/ref, no boilerplate`

3. **aura web doctor against live stack**
   - Test: `aura web doctor` with `SEARXNG_URL` set to live container
   - Expected: `reachable: yes (JSON round-trip OK); status: OK`
   - Gate-3 evidence: `aura web doctor (live stack): reachable: yes, JSON round-trip OK, status OK`

These human verifications were completed at Gate-3 (2026-06-02) with documented pass evidence in `07-04-SUMMARY.md` and `docs/aura-quality-snapshot.md`. The gate was cleared by the human operator.

---

## Gaps Summary

No gaps. All four success criteria are verified against the codebase:

- SC#1: `Search()` returns `[]Result{Title,URL,Snippet}` from a live SearXNG container; parse+filter path fully implemented and tested. Live run documented.
- SC#2: `ExtractMarkdown()` produces clean markdown via `readability.FromReader` + `htmltomarkdown.ConvertNode` + `cleanMarkdown` post-processing. Wikipedia clean markdown confirmed live.
- SC#3: `ssrf.go` classifier blocks all required classes; `scripts/ssrf_smoke.sh` tests all 4 canonical targets; sanitized output is non-leaky. 4/4 confirmed live.
- SC#4: `dnspin.go` + `transport.go` pin-reuse path fully wired; `TestDNSPin_TTL` (unit, injectable clock) + `TestDNSRebind` (integration, live) both confirmed.

Gate-3 quality numbers: ssrf.go mutation 94.4% (≥70% floor); internal/web combined coverage 91.5% (≥85% floor); owned-surface coverage gate 87.4% (≥85% floor); golangci-lint 0 issues; goleak clean.

---

_Verified: 2026-06-02_
_Verifier: Claude (gsd-verifier)_
