---
phase: 07-web-tools
plan: 03
subsystem: web-engine
tags: [searxng, fetch, ssrf, redirect-revalidate, readability, markdown, link-dedup, content-gate, ttl-cache, per-host-cap, retry-once, goleak]

# Dependency graph
requires:
  - phase: 07-web-tools (plan 01)
    provides: internal/web skeleton + goleak main_test.go + AURA_WEB_* root config (SearxngURL, WebDNSPinTTLSec, WebFetchMaxBodyBytes, WebCachePersistent, WebSearchTimeoutSec, WebFetchTimeoutSec, WebUserAgent) + readability/html-to-markdown deps
  - phase: 07-web-tools (plan 02)
    provides: hardenedTransport (pinned-IP DialContext + CheckRedirect ErrUseLastResponse + Dialer.Control recheck) + guard.validateAndPin + dnsPin + withConvID/convIDFrom + WebError/internalError/sanitize + D-38 enum
  - phase: 05-sandbox-2a-stateless
    provides: docker.go ctx-deadline (not http.Client.Timeout) + bounded one-retry loop idiom
  - phase: 03-llm-client
    provides: openai_compat/httperror.go non-leaky structured HTTP error + Retry-After idiom
provides:
  - web.Client facade (NewClient(cfg)) wiring transport + DNS pin + cache + per-host throttle + config
  - web.Search(ctx, SearchParams) []Result — site: rewrite + format=json + enum category + domain post-filter + two-tier metadata + unavailable errors + one-retry
  - web.Fetch(ctx, convID, url) Page — scheme → per-hop SSRF revalidate → Content-Type allowlist → size cap → readability→markdown, one-retry, per-host concurrency cap
  - ExtractMarkdown(body, pageURL) — readability.FromReader → ConvertNode + deduped absolute links + low_content warning (NEVER self-fetches)
  - in-mem TTL cache + disk opt-in (AURA_WEB_CACHE_PERSISTENT) shared by search and fetch
affects: [07-04 web_search.go/web_fetch.go thin adapters (call Search/Fetch, sanitize → NewResult), phase-close mutation gate on fetcher.go/searxng.go]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "SearXNG client uses a PLAIN http.Client (deadline rides ctx, docker.go idiom) — the in-network backend is trusted infra and does NOT pass through the SSRF gate; only web_fetch targets do"
    - "Cache key folds the result-shaping params (IncludeMetadata, MaxResults) into the raw key so a tier-1 cached search never serves a tier-2 request"
    - "Manual redirect loop re-enters guard.validateAndPin on each resolved Location BEFORE dialing the next hop — a blocked target is rejected at the hop (redirect_to_blocked_target) and never dialed (the public hop is, the metadata target is not)"
    - "Content-Type allowlist gated BEFORE reading the body; body read through io.LimitReader(cap+1) so a body that fills the extra byte trips response_too_large (Pitfall 6)"
    - "classifyTransportErr distinguishes an SSRF *internalError (non-retryable) from a network failure (errRetryable, one retry within the deadline) — D-42"
    - "readability.FromReader on already-fetched bytes (never the self-fetching entry point) — the grep-clean invariant is enforced and the comment avoids the literal token"
    - "Per-host concurrency cap via map[host]chan struct{} semaphore acquired for the duration of one fetch (D-36, no RPM machinery)"

key-files:
  created:
    - internal/web/client.go
    - internal/web/searxng.go
    - internal/web/fetcher.go
    - internal/web/html.go
    - internal/web/cache.go
    - internal/web/throttle.go
    - internal/web/searxng_test.go
    - internal/web/fetcher_test.go
    - internal/web/html_test.go
    - internal/web/cache_test.go
  modified:
    - internal/web/errors.go
    - internal/web/doc.go

decisions:
  - "images category is OUT for Phase 7 (OQ3/D-11): auraCategoryToSearXNG accepts only general/news; an unknown category returns a structured error, not a panic. Thumbnail/img_src fields stay in the searxResult shape so a future images slice needs no re-parse."
  - "SearXNG path uses a plain http.Client, NOT the hardened SSRF transport: SEARXNG_URL is an in-network base (D-02) that would resolve to a private IP and be blocked by the public-web-only classifier. Only model-supplied web_fetch URLs cross the SSRF gate."
  - "Cache TTLs: search 2m fixed, fetch defaultCacheTTL 5m fallback. Both bounded (D-33). Disk tier is a best-effort mirror — any I/O failure degrades to memory-only, never an error to the caller (D-32)."
  - "response_too_large uses io.LimitReader(cap+1) + len>cap rather than Content-Length (which a server may omit or lie about) — the overflow is detected on the actual bytes read (D-16)."

metrics:
  duration_min: 0
  tasks: 2
  files_created: 10
  files_modified: 2
  completed: 2026-06-02
---

# Phase 7 Plan 03: Web Client Engines (SearXNG Search + SSRF-Gated Fetch) Summary

The two client engines on top of the Wave 2 security boundary: a SearXNG search client with domain post-filter and two-tier metadata, an SSRF-gated fetch state machine with per-hop redirect revalidation, a readability→markdown extractor with deduped absolute links, a hybrid TTL cache, and the `web.Client` facade that wires them. All deterministic, httptest-driven, goleak-clean under `-race`.

## What was built

**Task 1 — `searxng.go` + `client.go` + `cache.go` (commit 0bfa01be):**
- `web.Client` + `NewClient(cfg *config.Config)` wiring the hardened transport (real `net.Dialer` + Control recheck), the DNS pin, and the response cache.
- `Search(ctx, SearchParams) ([]Result, error)`: builds `url.Values` with `q` (+ `site:host OR site:host` rewrite for hostname-only domains, rejecting scheme/path per D-12), `format=json`, `categories` via the `auraCategoryToSearXNG` enum map (general/news only), optional `language` + `time_range` (day|month|year), `pageno=1`; GET under the search deadline with one retry on 408/429/5xx and none on 4xx/config (D-14/D-42). Parses `searxResponse`, post-filters URLs by domain (exact + subdomain suffix match, D-13), returns `{title,url,snippet}` (+ normalized `{engine,score,category,published_at,thumbnail}` when `IncludeMetadata`, `published_at` from the nullable `publishedDate`). Empty `SearxngURL` → `web_search_unavailable{searxng_not_configured}`; unreachable/non-2xx → `searxng_unreachable`, never leaking the backend URL/body.
- `cache.go`: in-mem `map`+mutex+TTL default with a disk opt-in tier gated by `AURA_WEB_CACHE_PERSISTENT`; injectable clock; bounded default TTL fallback.

**Task 2 — `fetcher.go` + `html.go` + `throttle.go` (commit 8b234857):**
- `Fetch(ctx, convID, rawURL) (Page, error)`: `net/url` parse + scheme allowlist {http,https} (else `unsupported_scheme`, no dial); per-host concurrency token; fetch deadline; manual hop loop (cap 5) issuing GETs through the hardened client, reading `Location` on a 3xx, `ResolveReference` against the current URL, re-checking scheme, and re-entering `validateAndPin` for the next host → block at the hop with `redirect_to_blocked_target` (the blocked target is NEVER dialed). On 2xx: `Content-Type` allowlist {text/html, application/xhtml+xml} else `unsupported_content_type`; `io.LimitReader(cap+1)` overflow → `response_too_large`. One retry on transient/408/429/5xx within the deadline; no retry on SSRF/4xx/config.
- `html.go`: `ExtractMarkdown(body, pageURL)` → `readability.FromReader` (never the self-fetching entry point) → `htmltomarkdown.ConvertNode` (RenderHTML→ConvertString fallback), `art.Title()` only, link walk over the readable node tree resolved+deduped into absolute strings (D-19), `<250` readable runes → `warning="low_content"` (not an error, D-22).
- `throttle.go`: per-host semaphore (`perHostLimit=2`, D-36).

## Tests (12 named + 2 extra, all green under -race, goleak clean)

| Test | Proves |
|------|--------|
| TestSearch_ParseAndFilter | domain post-filter drops off-domain, keeps subdomains; query contains `site:wikipedia.org` + `format=json` |
| TestSearch_Unavailable | empty URL → searxng_not_configured; dead endpoint → searxng_unreachable |
| TestSearch_RetryPolicy | 1 retry on 503; 0 retries on 404 (handler call count) |
| TestSearch_CategoryEnum | general/news accepted; unknown category is an error, not a panic |
| TestSearch_Metadata | IncludeMetadata adds normalized published_at from nullable publishedDate; default omits |
| TestFetch_Readability | markdown contains article body, excludes nav/footer fixture markers; title only |
| TestFetch_RedirectRevalidate | block at the hop (redirect_to_blocked_target); public hop dialed once, metadata target ZERO hits |
| TestFetch_ContentGate | application/pdf → unsupported_content_type; over-cap body → response_too_large |
| TestFetch_LowContent | <250 runes → warning low_content, nil error |
| TestFetch_UnsupportedScheme | ftp:// → unsupported_scheme, no dial |
| TestFetch_PerHostConcurrencyCap | N+1 concurrent fetches never exceed perHostLimit in-flight (recorded max) |
| TestRetry_Policy | 1 retry on 503; 0 on 404 |
| TestCache_TTL / TestCache_DefaultTTLFallback | expiry + default-TTL fallback (injectable clock) |
| TestExtractMarkdown_BodyAndLinks / _LowContent | extractor body/links/dedup + low_content directly |

## Verification

- `go test -race ./internal/web/` — PASS, goleak clean.
- `grep -rn "FromURL" internal/web` — returns nothing (the comment was reworded to avoid the literal token; the forbidden self-fetching API is never called).
- `golangci-lint run ./internal/web/...` — 0 issues.
- File sizes: largest source `searxng.go` 294 LOC, `fetcher.go` 264 LOC — all under the 600-LOC cap and at/below the 300 PRD target.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Search cache aliased tier-1 and tier-2 results**
- **Found during:** Task 1 (TestSearch_Metadata)
- **Issue:** The cache key was the encoded query alone; `IncludeMetadata` is not part of the SearXNG query, so a prior metadata-less call served its cached tier-1 result to a later `IncludeMetadata:true` request, returning nil metadata.
- **Fix:** `searchCacheDiscriminator` folds `IncludeMetadata` + `MaxResults` into the raw cache key before hashing.
- **Files modified:** internal/web/searxng.go
- **Commit:** 0bfa01be

**2. [Rule 3 - Blocking] Removed the doc.go extraction-deps compile anchor**
- **Found during:** Task 2
- **Issue:** `doc.go` held a Wave-1 compile-time anchor (`_ = readability.FromReader`, etc.) to keep the extraction deps in go.mod ahead of html.go. With html.go now importing them directly the anchor was dead code (CLAUDE.md: dead-code removal on touch).
- **Fix:** Dropped the anchor and its imports; the package comment now states html.go consumes the deps.
- **Files modified:** internal/web/doc.go
- **Commit:** 8b234857

**3. [Rule 2 - Critical] Bounded the SearXNG response body read**
- **Found during:** Task 1
- **Issue:** Decoding the SearXNG JSON directly from `resp.Body` had no size bound; a pathological/compromised backend could exhaust memory (the backend is in-network but untrusted on the wire, D-10).
- **Fix:** `io.LimitReader(resp.Body, maxSearxBodyBytes=4MiB)` around the JSON decode.
- **Files modified:** internal/web/searxng.go
- **Commit:** 0bfa01be

## Threat Model Coverage

| Threat ID | Mitigation in this plan | Test |
|-----------|-------------------------|------|
| T-07-20 (redirect EoP) | manual per-hop ResolveReference + validateAndPin; block at hop; cap 5 hops | TestFetch_RedirectRevalidate |
| T-07-21 (body DoS) | Content-Type allowlist + io.LimitReader(cap+1) before read | TestFetch_ContentGate |
| T-07-22 (extraction EoP) | readability.FromReader on fetched bytes; self-fetching API never called (grep-clean) | TestExtractMarkdown_* + grep |
| T-07-23 (result tampering) | enum→raw category map, no pass-through engines/safesearch, normalized metadata only | TestSearch_CategoryEnum / TestSearch_Metadata |
| T-07-24 (unavailable info disclosure) | structured web_search_unavailable, no backend URL/body leaked | TestSearch_Unavailable |
| T-07-25 (retry DoS) | at most one retry within deadline on transient/408/429/5xx; none on SSRF/4xx/config | TestRetry_Policy |

## Notes for Wave 4 (tool adapters)

- Adapters hold one `*web.Client`; `WebSearch{Engine}.Execute` → `Search`, `WebFetch{Engine}.Execute` → `Fetch`.
- An engine `*WebError` is a MODEL-VISIBLE structured object: marshal via `WebError.JSON()` and feed to `tools.NewResult` so the model self-corrects (D-41) — NOT a Go error. Use `AsWebError(err)` to unwrap.
- `web_fetch` large markdown: `AURA_WEB_FETCH_MAX_BODY_BYTES` (formerly `AURA_WEB_RESPONSE_CAP_BYTES`, renamed + re-defaulted 24000 → 5 MB at Gate-3 2026-06-02) is the RAW HTTP body download ceiling enforced engine-side (the LimitReader bound) — NOT the markdown cap; `tools.NewResult`'s preview cap (`AURA_CONTEXT_PREVIEW_CAP_BYTES`) alone governs the LLM-facing markdown preview/spillover. Route the full `Page.ContentMD` through `NewResult` for spillover.

## Self-Check: PASSED

All 10 created files + 1 SUMMARY present on disk; both task commits (0bfa01be, 8b234857) present in git history.
