# Phase 7: Web Tools - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md - this log preserves the alternatives considered.

**Date:** 2026-06-02
**Phase:** 7-Web Tools
**Areas discussed:** SearXNG availability, SearXNG compose hardening, Web cache policy, HTTP identity and politeness, Error taxonomy, Search result shape, Fetch output shape, SSRF error posture, Additional industry-pattern locks, Operator smoke surface

---

## SearXNG Availability

| Option | Description | Selected |
|--------|-------------|----------|
| Aura compose default with native Go tools | Implement Aura-native tools and self-host SearXNG in Compose. | yes |
| Direct MCP sidecar backend | Use an existing MCP SearXNG server directly as the runtime backend. | |
| Defer until generic MCP client exists | Wait for a generic MCP runtime before adding web tools. | |

**User's choice:** Aura-native Go tools with self-hosted SearXNG.
**Notes:** External MCP servers were studied first. OvertliDS/mcp-searxng-enhanced installed and basic search/fetch worked, but an SSRF smoke against `127.0.0.1` succeeded. This made it unsuitable as a direct Aura dependency. Reference value remains high.

---

## Search Result Shape

| Option | Description | Selected |
|--------|-------------|----------|
| Minimal stable shape | Return only title, URL, and snippet. | |
| Research-rich shape | Return richer result objects by default. | |
| Two-tier shape | Default stable shape, optional normalized metadata. | yes |

**User's choice:** Two-tier shape.
**Notes:** Public controls are `query`, `max_results`, `category`, `language`, `time_range`, `domains`, and `include_metadata`. Raw SearXNG pass-through, `engines`, and model-controlled `safesearch` were rejected. Domain filtering uses both `site:` query rewrite and post-filtering.

---

## Fetch Output Shape

| Option | Description | Selected |
|--------|-------------|----------|
| Markdown article package | Return title, URL, markdown content, links, and optional warning. | yes |
| Markdown plus metadata | Include byline/published/excerpt/site metadata by default. | |
| Two-tier fetch shape | Add an optional richer fetch metadata tier. | |

**User's choice:** Markdown article package.
**Notes:** Links are normalized absolute URLs only. Large markdown over `AURA_WEB_RESPONSE_CAP_BYTES=24000` spills through `ToolResult`; low-quality extraction returns content with a warning rather than failing by default.

---

## SSRF Error Posture

| Option | Description | Selected |
|--------|-------------|----------|
| Structured but non-leaky | Return stable block reasons without internal IPs/CIDRs/chains. | yes |
| Detailed security diagnostics | Expose resolved IPs and redirect details. | |
| Generic denial only | Return a minimal denial without actionable reason. | |

**User's choice:** Structured but non-leaky.
**Notes:** Redirects must be manually validated before following. Tests/debug logs may assert the internal block reason, but model-visible output cannot expose IPs, CIDRs, internal hostnames, or redirect chains. No internal allowlist is added in Phase 7.

---

## Operator Smoke Surface

| Option | Description | Selected |
|--------|-------------|----------|
| Roadmap smoke only | Guarantee search/fetch/SSRF smoke tests from the roadmap. | yes |
| Add local SearXNG readiness smoke | Add a readiness command in addition to roadmap smoke. | partial |
| Full operator diagnostics suite | Build a broader diagnostics suite now. | |

**User's choice:** Roadmap smoke plus a minimal web health command.
**Notes:** Missing/unreachable SearXNG returns structured unavailable errors with no public fallback. The health command checks config, reachability, JSON output, and a tiny search. Smoke output stays human-readable only; JSON output is deferred.

---

## Additional Industry-Pattern Locks

| Option | Description | Selected |
|--------|-------------|----------|
| Lock industry recommendation set | Apply the reviewed patterns as phase constraints. | yes |
| Discuss one by one | Continue selecting each individual industrial pattern. | |

**User's choice:** Lock the industrial recommendation set.
**Notes:** `web_fetch` accepts HTTP(S) HTML/XHTML only; PDFs/binaries/downloads are deferred. Categories use a small Aura enum. Search deadline is 20s; fetch deadline is 30s. At most one retry is allowed for transient errors within the same deadline.

---

## SearXNG Compose Hardening

| Option | Description | Selected |
|--------|-------------|----------|
| Internal Compose service only | No default host port, Aura reaches SearXNG over Compose network. | yes |
| Localhost-bound debug port | Publish SearXNG for local browser/debug use. | |
| Configurable exposure | Make exposure configurable in the default phase. | |

**User's choice:** Internal Compose service only.
**Notes:** Use a checked-in minimal settings file mounted read-only and enable JSON output. Outside Compose, missing `SEARXNG_URL` is an unavailable error, not localhost autodetect.

---

## Web Cache Policy

| Option | Description | Selected |
|--------|-------------|----------|
| No response cache | Do not cache search or fetch in Phase 7. | |
| Tiny in-memory fetch cache | Cache fetch only, memory scoped. | |
| Search and fetch cache | Cache both search and fetch responses. | yes |

**User's choice:** Cache search and fetch.
**Notes:** Persistent global disk cache is explicit opt-in with `AURA_WEB_CACHE_PERSISTENT=true`; otherwise use in-memory fallback. Search gets short TTL; fetch respects `Cache-Control` where practical and never stores `no-store`.

---

## HTTP Identity and Politeness

| Option | Description | Selected |
|--------|-------------|----------|
| Aura-specific UA | Identify as Aura instead of pretending to be a browser. | yes |
| Generic browser UA | Send a browser-like user agent. | |
| Configurable UA | Make UA fully configurable now. | |

**User's choice:** Aura-specific User-Agent.
**Notes:** Use the same Aura UA for SearXNG requests and direct fetches. Add a small per-host fetch concurrency cap. Do not enforce `robots.txt` in Phase 7 because this is specific URL fetch, not crawler traversal.

---

## Error Taxonomy

| Option | Description | Selected |
|--------|-------------|----------|
| Small stable enum | Use a compact stable set of structured error codes. | yes |
| Detailed enum | Add granular failure-specific codes. | |
| Error string only | Return unstructured strings. | |

**User's choice:** Small stable enum.
**Notes:** Locked enum: `web_search_unavailable`, `blocked_url`, `unsupported_scheme`, `unsupported_content_type`, `response_too_large`, `timeout`, `http_error`, and `extraction_failed`. Errors include safe messages; HTTP errors may include status code; errors stay inline.

---

## the agent's Discretion

- Exact package layout.
- Exact cache implementation, key format, TTL numbers, and bounds.
- Exact redirect hop cap and per-host concurrency cap.
- Exact web doctor command spelling.
- Whether `images` category is included after planning evaluates shape/test cost.
- Exact test file organization.

## Deferred Ideas

- Generic MCP client/runtime integration for third-party MCP servers.
- PDF web document extraction.
- Browser or site crawling similar to searxNcrawl.
- Public/random SearXNG instance fallback.
- Explicitly scoped `internal_fetch` tool for controlled internal/local resources.
- JSON-capable operator/doctor output for CI and scripts.
- Crawler-grade `robots.txt` enforcement and site-ingest politeness.
