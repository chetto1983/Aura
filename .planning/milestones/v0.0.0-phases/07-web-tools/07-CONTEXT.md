# Phase 7: Web Tools - Context

**Gathered:** 2026-06-02
**Status:** Ready for planning

<domain>
## Phase Boundary

Phase 7 delivers Aura-native web access tools:

- `web_search`: ranked public-web search through a self-hosted SearXNG service in Aura Compose.
- `web_fetch`: public HTTP(S) page fetch, readability extraction, markdown conversion, and link normalization.

The phase must preserve Aura's existing agent/tool contracts: structured tool definitions, `ToolResult` spillover, `read_tool_output` paging, prompt byte-stability, and fail-closed security behavior. External MCP SearXNG projects are reference material only; Aura does not adopt a third-party MCP server as the runtime backend in this phase.

</domain>

<decisions>
## Implementation Decisions

### Runtime and SearXNG
- **D-01:** Implement native Go `web_search` and `web_fetch` tools inside Aura. Do not fork, vendor, or run a third-party MCP SearXNG server as the Phase 7 backend.
- **D-02:** Self-host SearXNG in Aura Compose. Default Compose configuration exposes it internally to Aura as `SEARXNG_URL=http://searxng:8080/search`.
- **D-03:** The default Compose service must not publish a host port for SearXNG. Use the shared app network so SearXNG can reach the public internet while avoiding host exposure.
- **D-04:** Add a checked-in minimal SearXNG settings file mounted read-only. It must enable `search.formats` with both `html` and `json`.
- **D-05:** Outside Compose, missing `SEARXNG_URL` is an error. Do not autodetect localhost and do not fall back to public/random SearXNG instances.
- **D-06:** If SearXNG is missing or unreachable, return structured unavailable errors: `web_search_unavailable` with reason `searxng_not_configured` or `searxng_unreachable`.

### web_search Contract
- **D-07:** Use a two-tier result shape. Default result entries are stable `{title, url, snippet}`.
- **D-08:** When `include_metadata: true`, optionally add normalized metadata fields such as `engine`, `score`, `category`, `published_at`, and `thumbnail`.
- **D-09:** Expose public controls: `query`, `max_results`, `category`, `language`, `time_range`, `domains`, and `include_metadata`.
- **D-10:** Do not expose raw SearXNG pass-through parameters, raw backend responses, arbitrary `engines`, or model-controlled `safesearch`. Safesearch remains operator/config-level.
- **D-11:** Use a small Aura category enum rather than raw SearXNG categories. Start with `general` and `news`; include `images` only if planning confirms a clear shape and tests.
- **D-12:** Domain filtering uses both query rewrite and post-filtering. Validate domains as hostnames only, with no scheme or path. Rewrite the query using `site:` filters to guide SearXNG, then post-filter result URLs to enforce the contract.
- **D-13:** `example.com` matches `example.com` and subdomains. If filtering removes too many results, return fewer results rather than leaking off-domain URLs.
- **D-14:** Search has a strict 20 second wall-clock deadline and at most one retry for transient network, `408`, `429`, or `5xx` failures within that same deadline.

### web_fetch Contract
- **D-15:** `web_fetch` accepts only `http` and `https` URLs in Phase 7.
- **D-16:** `web_fetch` supports readable HTML/XHTML only. PDFs, binaries, downloads, unsupported MIME types, and giant responses return structured errors and are deferred.
- **D-17:** Fetch output defaults to a markdown article package: `{title, url, content_md, links, warning?}`.
- **D-18:** Default fetch output does not include byline, published time, excerpt, site name, or fetched time metadata in Phase 7.
- **D-19:** `links` is a deduped list of normalized absolute URLs found in the readable article content. Do not return `{text, url}` link objects in Phase 7.
- **D-20:** Use `codeberg.org/readeck/go-readability/v2` for readability extraction and `JohannesKaufmann/html-to-markdown/v2` for markdown conversion, per the roadmap amendment.
- **D-21:** The LLM-facing `content_md` preview/spillover is governed by the agent tool-result preview cap (`tools.NewResult`, `AURA_CONTEXT_PREVIEW_CAP_BYTES`): when the extracted markdown exceeds it, return a short preview plus `tool_result_id` and page the full content through `read_tool_output`. This is a SEPARATE knob from `AURA_WEB_FETCH_MAX_BODY_BYTES` (formerly `AURA_WEB_RESPONSE_CAP_BYTES`), which is only the raw HTTP response body download ceiling (DoS guard, default 5 MB) applied in `gateAndRead` BEFORE readability extraction — it does NOT govern the model-facing payload.
- **D-22:** Low-quality extraction returns extracted markdown with `warning` such as `low_content` or `extraction_maybe_incomplete`; it is not automatically an error.
- **D-23:** Fetch has a strict 30 second wall-clock deadline and at most one retry for transient network, `408`, `429`, or `5xx` failures within that same deadline.

### SSRF and Network Safety
- **D-24:** SSRF defense is fail-closed. Web tools are for public web only; local/internal fetching remains blocked.
- **D-25:** Implement the roadmap blocklist requirements: per-conversation DNS pinning with `AURA_WEB_DNS_PIN_TTL_SEC=60`, IPv6/private target blocking, IPv4-mapped IPv6 blocking, and explicit metadata/internal hostname blocks.
- **D-26:** Model-visible SSRF errors must be structured but non-leaky, for example `{error:"blocked_url", reason:"private_or_metadata_target"}` or `{error:"blocked_url", reason:"redirect_to_blocked_target"}`.
- **D-27:** Do not expose resolved IPs, CIDRs, internal hostnames, response headers, body snippets, or redirect-chain details to the model.
- **D-28:** Production/model-visible output has no debug escape hatch that reveals SSRF internals. Tests and local debug logs may contain enough detail to assert the right block reason.
- **D-29:** Disable automatic redirects. For every redirect, parse, resolve, DNS/IP-validate, and pin the next URL before following. Cap redirect hops.
- **D-30:** No allowlist is added in Phase 7. A future explicitly scoped `internal_fetch` can handle controlled internal/local resources.

### Cache, Identity, and Politeness
- **D-31:** Cache both `web_search` and `web_fetch` responses.
- **D-32:** Persistent global disk cache is opt-in only through `AURA_WEB_CACHE_PERSISTENT=true`. Default behavior falls back to an in-memory cache.
- **D-33:** Use hybrid TTL behavior: search gets a short fixed TTL; fetched pages respect `Cache-Control` where practical, never store `no-store`, and otherwise use a bounded default TTL. Full RFC revalidation is not required in Phase 7 unless planning finds it trivial.
- **D-34:** Use an Aura-specific User-Agent, for example `Aura/0.x web_fetch`. Do not pretend to be a browser by default.
- **D-35:** Use the same Aura-specific User-Agent consistently for SearXNG requests and direct fetches.
- **D-36:** Add a small per-host concurrency cap for `web_fetch`. Do not add full requests-per-minute cooldown machinery unless planning finds it trivial.
- **D-37:** Do not enforce `robots.txt` in Phase 7. `web_fetch` fetches specific requested URLs, not crawler traversal; crawler-grade robots behavior is deferred.

### Error Taxonomy
- **D-38:** Use a small stable enum: `web_search_unavailable`, `blocked_url`, `unsupported_scheme`, `unsupported_content_type`, `response_too_large`, `timeout`, `http_error`, and `extraction_failed`.
- **D-39:** Errors include `error`, a short non-sensitive `message`, and optional non-sensitive `reason`.
- **D-40:** Public HTTP errors may include `status_code` and a safe message. Do not include headers or body snippets.
- **D-41:** Errors stay inline as small structured objects. Only successful large content spills to `ToolResult`.
- **D-42:** Do not retry config errors, SSRF blocks, unsupported content types, or most `4xx` responses.

### Operator Surface and Smoke
- **D-43:** Guarantee the roadmap smoke surface:
  - `aura tool web_search '{"query":"site:wikipedia.org SearXNG","max_results":3}'`
  - `aura tool web_fetch '{"url":"https://en.wikipedia.org/wiki/Searx"}'`
  - SSRF fixtures/tests for cloud metadata, IPv4-mapped IPv6, IPv6 link-local, `metadata.google.internal`, and DNS rebinding.
- **D-44:** Add a minimal web health command, such as `aura web doctor` or equivalent, that checks `SEARXNG_URL`, reachability, JSON output, and a tiny search. It must not use public fallback.
- **D-45:** Smoke/doctor output is human CLI output only in Phase 7. JSON-capable operator output is deferred.

### the agent's Discretion
- Choose exact Go package layout and whether the shared web implementation lives under `internal/web`, `internal/agent/tools`, or another local pattern that fits the codebase.
- Choose exact cache implementation, key format, cache location, TTL numbers, memory bounds, and eviction policy within the locked cache posture.
- Choose the exact per-host fetch concurrency cap and redirect hop cap.
- Choose the exact command spelling for the minimal web doctor if another existing CLI pattern is clearer than `aura web doctor`.
- Decide whether `images` category belongs in Phase 7 after planning examines shape/test cost.
- Choose unit/integration test layout, as long as roadmap SSRF and smoke guarantees are covered.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Project Scope and Carry-Forward
- `.planning/ROADMAP.md` - Phase 7 goal, success criteria, SSRF requirements, and roadmap amendment selecting Readeck readability.
- `.planning/REQUIREMENTS.md` - CAP-05 web tool requirement.
- `.planning/PROJECT.md` - Aura project constraints, test discipline, Docker-on-Windows note, and Go-native substrate direction.
- `.planning/phases/06-kv-cache-builder/06-CONTEXT.md` - Preserve prompt byte-stability and use `ToolResult`/`read_tool_output` instead of prompt mutation.
- `.planning/phases/05-sandbox-2a-stateless/05-CONTEXT.md` - Fail-closed security posture and deterministic security-test expectations.
- `.planning/phases/04-hitl-identity-conversations/04-CONTEXT.md` - Store, Runner, sidecar, and conversation context patterns that Phase 7 should reuse.

### External MCP and SearXNG References
- `https://github.com/ihor-sokoliuk/mcp-searxng/blob/main/README.md` - MCP SearXNG reference behavior.
- `https://github.com/ihor-sokoliuk/mcp-searxng/blob/main/CONFIGURATION.md` - Reference configuration knobs.
- `https://github.com/ihor-sokoliuk/mcp-searxng/blob/main/src/search.ts` - Search implementation reference.
- `https://github.com/ihor-sokoliuk/mcp-searxng/blob/main/src/types.ts` - Reference search result typing.
- `https://github.com/ihor-sokoliuk/mcp-searxng/blob/main/src/url-reader.ts` - Fetch/url-reader reference.
- `https://mcpmarket.com/server/searxng-2` - Marketplace summary of the MCP SearXNG server.
- `https://github.com/tisDDM/searxng-mcp` - Alternative MCP SearXNG reference.
- `https://github.com/tisDDM/searxng-mcp/blob/main/README.md` - Alternative server capabilities and config.
- `https://github.com/tisDDM/searxng-mcp/blob/main/src/index.ts` - Alternative implementation reference.
- `https://github.com/DasDigitaleMomentum/searxNcrawl` - Broader search+crawl MCP reference; crawler behavior is deferred.
- `https://github.com/DasDigitaleMomentum/searxNcrawl/blob/main/README.md` - searxNcrawl capabilities overview.
- `https://github.com/DasDigitaleMomentum/searxNcrawl/blob/main/docs/usage/mcp-tools.md` - MCP tool shape reference.
- `https://github.com/DasDigitaleMomentum/searxNcrawl/blob/main/pyproject.toml` - Dependency/reference stack.
- `https://github.com/DasDigitaleMomentum/searxNcrawl/blob/main/docker-compose.yml` - Compose reference.
- `https://mcpservers.org/servers/OvertliDS/mcp-searxng-enhanced` - Enhanced MCP listing referenced by the user.
- `https://github.com/OvertliDS/mcp-searxng-enhanced` - Enhanced MCP source used in the spike.
- `https://github.com/OvertliDS/mcp-searxng-enhanced/blob/master/README.md` - Enhanced MCP tools and usage.
- `https://github.com/OvertliDS/mcp-searxng-enhanced/blob/master/requirements.txt` - Enhanced MCP dependency reference.
- `https://github.com/OvertliDS/mcp-searxng-enhanced/blob/master/Dockerfile` - Enhanced MCP container reference.
- `https://github.com/OvertliDS/mcp-searxng-enhanced/blob/master/mcp_server.py` - Enhanced MCP implementation reference and SSRF cautionary source.

### Search API and Industrial Tool Shape
- `https://docs.searxng.org/dev/search_api.html` - SearXNG JSON search API.
- `https://docs.searxng.org/src/searx.search.html` - SearXNG search internals reference.
- `https://docs.searxng.org/admin/settings/settings_search.html` - SearXNG search settings, including formats.
- `https://developers.openai.com/api/docs/guides/tools-web-search` - Industrial web-search tool shape reference.
- `https://docs.tavily.com/documentation/api-reference/endpoint/search` - Search result/control reference.
- `https://exa.ai/docs/reference/search-api-guide-for-coding-agents` - Search controls and coding-agent reference.
- `https://exa.ai/docs/reference/contents-retrieval` - Content retrieval reference.
- `https://docs.firecrawl.dev/api-reference/endpoint/search` - Search API reference.
- `https://docs.firecrawl.dev/api-reference/v2-endpoint/search` - Newer search API reference.
- `https://developers.google.com/custom-search/v1/reference/rest/v1/SiteSearchFilter` - Site/domain filtering reference.
- `https://api-dashboard.search.brave.com/documentation/resources/search-operators` - Search-operator reference.
- `https://brave.com/search/api/` - Commercial search API reference.

### SSRF, Metadata, HTTP, Cache, and Retry
- `https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html` - SSRF defense baseline.
- `https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/configuring-instance-metadata-service.html` - AWS metadata endpoint reference.
- `https://docs.cloud.google.com/compute/docs/metadata/querying-metadata` - Google metadata endpoint reference.
- `https://learn.microsoft.com/en-us/azure/virtual-machines/instance-metadata-service` - Azure metadata endpoint reference.
- `https://developer.mozilla.org/docs/Web/HTTP/Reference/Headers/Content-Type` - Content-Type handling.
- `https://developer.mozilla.org/en-US/docs/Web/HTTP/Caching` - Practical HTTP cache behavior.
- `https://www.rfc-editor.org/rfc/rfc9111.html` - HTTP caching standard reference.
- `https://aws.amazon.com/builders-library/timeouts-retries-and-backoff-with-jitter/` - Retry and timeout reference.
- `https://docs.cloud.google.com/storage/docs/retry-strategy` - Retry strategy reference.
- `https://sre.google/sre-book/addressing-cascading-failures/` - Reliability reference for bounded retry behavior.

### Docker and CLI
- `https://docs.docker.com/reference/compose-file/services/` - Compose service configuration.
- `https://docs.docker.com/reference/compose-file/networks/` - Compose network configuration.
- `https://docs.docker.com/reference/cli/docker/compose/ps/` - Compose health/operator reference.
- `https://kubernetes.io/docs/reference/kubectl/kubectl-cmds/` - CLI output/status reference.
- `https://cli.github.com/manual/gh_help_formatting` - CLI formatting reference.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/agent/tools/spec.go` - Existing tool spec and schema conventions.
- `internal/agent/tools/manifest.go` - Tool manifest/rendering patterns that new web tools must fit.
- `internal/agent/tools/result.go` - `ToolResult` spillover path and validation behavior for large fetch output.
- `internal/agent/tools/read_tool_output.go` - Paging API for sidecar content; `web_fetch` large output should reuse this path.
- `internal/agent/tools/execute.go` - Deferred tool pattern and shared result formatting style.
- `internal/config/config.go` - Environment/config loading conventions; add web config here or via a local pattern consistent with this file.
- `compose.yaml` - Existing service/network style; add SearXNG here without default host port exposure.
- `cmd/aura/main.go` and `cmd/aura/exec.go` - Hand-rolled CLI subcommand pattern to mirror for web smoke/doctor commands.
- `internal/sandbox/docker.go` - Existing remote service reachability and timeout style that may inform web health checks.

### Established Patterns
- Tool calls return structured data and errors through the tool result channel; do not mutate `messages[0]`.
- Full content that would exceed prompt budget spills to a sidecar `ToolResult`; the model gets an ID and uses `read_tool_output`.
- Config follows explicit environment variables and fail-fast/structured unavailable errors rather than hidden fallback behavior.
- Domain packages should preserve boundaries. Runner/composition owns orchestration; tools should not grow cross-cutting responsibilities.
- Security tests should be deterministic, non-leaky, and fail closed.
- Existing CLI code uses hand-rolled switches rather than introducing a new CLI framework.

### Integration Points
- Register `web_search` and `web_fetch` in the existing tool registry/manifest path.
- Add web config for `SEARXNG_URL`, `AURA_WEB_DNS_PIN_TTL_SEC`, `AURA_WEB_FETCH_MAX_BODY_BYTES` (raw-body download ceiling, default 5 MB — NOT the model-facing markdown preview cap), cache persistence, timeouts, and User-Agent defaults.
- Add SearXNG Compose service and checked-in read-only settings file.
- Add SSRF validation, DNS pinning, manual redirect validation, MIME/size limits, cache, and fetch/search clients in a scoped web package or tool package chosen during planning.
- Add CLI smoke/doctor command following existing `aura` subcommand style.
- Add tests for search contract, fetch extraction/spillover, domain filtering, unavailable SearXNG, SSRF blocks, and DNS rebinding pin behavior.

</code_context>

<specifics>
## Specific Ideas

- The user explicitly asked to study existing MCP SearXNG servers to avoid reinventing the wheel. The conclusion is reference reuse, not runtime reuse.
- The OvertliDS enhanced MCP server spike installed successfully, listed `search_web`, `get_website`, and `get_current_datetime`, and basic local SearXNG search/fetch worked.
- The same spike failed an SSRF smoke test because `get_website` could fetch a local `127.0.0.1` target. Static review also found automatic redirects, `0.0.0.0` HTTP mode, broad CORS, and no visible DNS pin/redirect revalidation/IP classification.
- Score from the spike: direct dependency `41/100`; reference value `80/100`.
- `web_search` should feel close to industrial web-search tools: stable results, bounded controls, no raw backend leakage, and clear unavailable errors.
- `web_fetch` should be an article markdown tool, not a crawler or browser automation tool.

</specifics>

<deferred>
## Deferred Ideas

- Generic MCP client/runtime integration for third-party MCP servers.
- PDF web document extraction.
- Browser or site crawling similar to searxNcrawl.
- Public/random SearXNG instance fallback.
- Explicitly scoped `internal_fetch` tool for controlled internal/local resources.
- JSON-capable operator/doctor output for CI and scripts.
- Crawler-grade `robots.txt` enforcement and site-ingest politeness.

</deferred>

---

*Phase: 7-Web Tools*
*Context gathered: 2026-06-02*
