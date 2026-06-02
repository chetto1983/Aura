# Phase 7: Web Tools - Pattern Map

**Mapped:** 2026-06-02
**Files analyzed:** 16 (10 new web-engine/tool/config/CLI/infra files + 6 test files)
**Analogs found:** 16 / 16 (every new file has at least a role-match analog in-repo)

> Consumes 07-CONTEXT.md (D-01..D-45) + 07-RESEARCH.md (§Architecture Patterns, §Recommended Project Structure, §Code Examples). RESEARCH already settled the package layout (`internal/web` shared engine + thin `internal/agent/tools` adapters). This file binds each proposed file to a concrete in-repo analog with line-anchored excerpts the planner copies.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/web/config.go` | config | request-response | `internal/config/config.go` (envDefault/envIntDefault, fail-fast) | role-match |
| `internal/web/searxng.go` | service (HTTP client) | request-response | `internal/sandbox/docker.go` (remote-service HTTP client) | exact |
| `internal/web/ssrf.go` **← CRITICAL** | utility (security classifier) | transform | none (greenfield; netip stdlib + RESEARCH Pattern 2) | no-analog (logic) / test-style match |
| `internal/web/dnspin.go` | utility (TTL cache) | transform | RESEARCH Code Examples DNS-pin shape; mutex-map idiom | no-analog (logic) |
| `internal/web/transport.go` | service (HTTP transport) | request-response | `internal/sandbox/docker.go` (Dialer + DisableKeepAlives) | exact |
| `internal/web/fetcher.go` | service | request-response | `internal/sandbox/docker.go` (exec retry loop) + `httperror.go` (status gate) | role-match |
| `internal/web/html.go` | utility (transform) | transform | none (greenfield; readability/html-to-markdown libs) | no-analog (logic) |
| `internal/web/cache.go` | store (in-mem TTL) | CRUD | RESEARCH DNS-pin mutex-map shape (same idiom) | role-match |
| `internal/web/errors.go` | utility (error taxonomy) | — | `internal/llm/openai_compat/httperror.go` (non-leaky structured error) | exact |
| `internal/agent/tools/web_search.go` | tool adapter | request-response | `internal/agent/tools/execute.go` (Deferred adapter → engine → NewResult) | exact |
| `internal/agent/tools/web_fetch.go` | tool adapter | request-response | `internal/agent/tools/execute.go` (same) | exact |
| `cmd/aura/web.go` | CLI subcommand | request-response | `cmd/aura/exec.go` (hand-parsed subcommand) | exact |
| `cmd/aura/main.go` (MODIFY) | CLI wiring | — | `cmd/aura/main.go` (switch + buildRegistry) | self |
| `internal/config/config.go` (MODIFY, optional) | config wiring | — | `internal/config/config.go` (subsystem config composition) | self |
| `compose.yaml` (MODIFY) | config (infra) | — | `compose.yaml` (existing service/network style) | self |
| `searxng/settings.yml` (NEW) | config (infra) | — | none (checked-in read-only mount; new) | no-analog |

**Test files** (Wave 0, per RESEARCH §Validation): `internal/web/main_test.go`, `ssrf_test.go`, `fetcher_test.go`, `dnspin_test.go`, `searxng_test.go`, `internal/agent/tools/web_fetch_test.go` → analogs: `internal/sandbox/docker_test.go` (goleak TestMain + httptest), `internal/agent/tools/main_test.go` (goleak TestMain).

## Pattern Assignments

### `internal/agent/tools/web_search.go` + `web_fetch.go` (tool adapter, request-response)

**Analog:** `internal/agent/tools/execute.go` — the repo's reference Deferred adapter that delegates to a lower engine package (`internal/sandbox`) and routes output through `tools.NewResult`. The web tools are the SAME shape with `internal/web` swapped for `internal/sandbox`.

**Struct + injected engine** (execute.go:24-33):
```go
type Execute struct {
	Runner sandbox.Runner
}
type executeArgs struct {
	Lang       string `json:"lang"`
	Code       string `json:"code"`
	TimeoutSec int    `json:"timeout_sec"`
	SessionID  string `json:"session_id"`
}
```
→ `web_search.go`: `type WebSearch struct { Engine *web.Client }` + `webSearchArgs{ Query, MaxResults, Category, Language, TimeRange, Domains []string, IncludeMetadata bool }` (D-09).
→ `web_fetch.go`: `type WebFetch struct { Engine *web.Client }` + `webFetchArgs{ URL string }` (D-15).

**Deferred spec** (execute.go:35-58 — the `Deferred: true` pattern is MANDATORY for these long-description + enum-schema tools, CLAUDE.md deferred-tool partition):
```go
func (e *Execute) Spec() Spec {
	params := json.RawMessage(`{ "type": "object", "properties": { ... }, "required": [...] }`)
	return Spec{
		Name:        "execute",
		Summary:     "Run a Python or shell snippet in an isolated network-less sandbox.",
		Description: "...long description with Example lines...",
		Parameters:  params,
		Deferred:    true,
	}
}
```
→ Both web tools set `Deferred: true`. `web_search` Summary ~ "Search the public web via the configured SearXNG instance." `web_fetch` Summary ~ "Fetch a public web page and return it as readable markdown." (Manifest auto-sorts alphabetically — `web_fetch` < `web_search` — and KV-cache stability is preserved automatically; never hand-order.)

**Execute: unmarshal → validate enum-as-ToolResult → delegate → NewResult** (execute.go:65-99):
```go
func (e *Execute) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a executeArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("execute args: %w", err)
	}
	// ... enum/validation failures return NewResult(ctx, "error: ...") so the MODEL self-corrects ...
	res, err := e.Runner.RunPython(ctx, code, timeoutSec)
	if err != nil {
		return ToolResult{}, err // typed engine/infra error → propagates to loop
	}
	return NewResult(ctx, FormatLean(res))
}
```
**Adapter rule for web tools (D-26/27/28/41):** an SSRF block / unsupported-scheme / unavailable is a *model-visible structured object* returned via `NewResult(ctx, sanitizedJSON)` (the model self-corrects — same channel as execute's enum-error), NOT a Go `error`. Reserve the Go-`error` return for genuine infra faults the loop should see. The adapter maps the engine's internal/rich error → the sanitized `{error, reason, message}` JSON (errors.go below) before `NewResult`. **Never** put the resolved IP / internal host / redirect chain into that string (D-27).

**Spillover (D-21) is FREE — reuse, do not reimplement** (execute.go:98 + result.go:94-133):
```go
return NewResult(ctx, FormatLean(res))   // execute
return NewResult(ctx, contentMD)         // web_fetch: large markdown spills to sidecar automatically
```
`tools.NewResult` already does cap → preview → sidecar (`<run_dir>/conversations/<session>/<tool_call>.result`) → `read_tool_output` footer. The cap is `AURA_CONTEXT_PREVIEW_CAP_BYTES` injected via `WithToolCallContext`. **RESOLVED (Gate-3 2026-06-02):** `web_fetch` routes the FULL markdown through `NewResult` and lets the preview cap (`AURA_CONTEXT_PREVIEW_CAP_BYTES`) alone govern the LLM-facing preview/spillover (D-21). `AURA_WEB_FETCH_MAX_BODY_BYTES` (formerly `AURA_WEB_RESPONSE_CAP_BYTES`, default raised 24000 → 5 MB) is ONLY the raw HTTP body download ceiling applied in `gateAndRead` BEFORE readability extraction — it does NOT cap the markdown. Keep the two knobs distinct.

---

### `internal/web/searxng.go` + `transport.go` + `fetcher.go` (service, request-response)

**Analog:** `internal/sandbox/docker.go` — a thin HTTP client against a compose-managed service with explicit timeouts, a bounded retry, typed errors, and goleak-safe transport.

**Client struct + goleak-safe transport** (docker.go:42-65) — copy the `DisableKeepAlives` + dialer-only-timeout idiom EXACTLY (RESEARCH "goleak + DisableKeepAlives" pitfall):
```go
type DockerRunner struct {
	httpClient        *http.Client
	url               string
	defaultTimeoutSec int
}
func NewDockerRunner(cfg *config.Config) *DockerRunner {
	return &DockerRunner{
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext: (&net.Dialer{ Timeout: connectTimeoutSec * time.Second }).DialContext,
				DisableKeepAlives: true, // goleak order-independence
			},
		},
		url: cfg.SandboxURL,
	}
}
```
→ `transport.go`: same shape, but `DialContext` is the **pinned-IP dialer** (RESEARCH Pattern 1) + `CheckRedirect: http.ErrUseLastResponse` (Pattern 3). Keep `DisableKeepAlives: true`. For tests, accept an injectable `dialFunc` so a recording dialer can assert the pinned IP (RESEARCH "Recording dialer" pitfall).

**Deadline via ctx, NOT http.Client.Timeout** (docker.go:99-102) — D-14 (20s search) / D-23 (30s fetch) ride the ctx, exactly like the sandbox:
```go
ctx, cancel := context.WithTimeout(ctx, time.Duration(effective)*time.Second+responseGrace)
defer cancel()
```

**Bounded one-retry loop** (docker.go:109-122) — the structural template for D-14/D-23 (one retry on transient/408/429/5xx within the SAME deadline; D-42 NO retry on SSRF/4xx/config):
```go
resp, err := r.post(ctx, lang, body)
if err != nil {
	r.autoStart(ctx)           // web: replace with backoff/no-op; retry once
	resp, err = r.post(ctx, lang, body)
	if err != nil {
		return Result{}, fmt.Errorf("...: %w", ErrSandboxUnreachable)
	}
}
defer func() { _ = resp.Body.Close() }()
if resp.StatusCode/100 != 2 {
	return Result{}, fmt.Errorf("... status %d: %w", resp.StatusCode, ErrSandboxProtocol)
}
```
→ `searxng.go`: unreachable/non-2xx → wrap the sentinel that maps to `web_search_unavailable` reason `searxng_unreachable`; missing `SEARXNG_URL` → `searxng_not_configured` (D-06). Query construction + JSON parse + domain post-filter per RESEARCH §Code Examples (search query construction, `searxResult`/`searxResponse` shapes, link/domain filter D-12/D-13).
→ `fetcher.go`: scheme check (D-15) → SSRF validate+pin (ssrf.go) → dial pinned IP → manual redirect revalidate loop (Pattern 3, cap hops) → `Content-Type` allowlist + `io.LimitReader(body, AURA_WEB_FETCH_MAX_BODY_BYTES)` gate (D-16, RESEARCH Pitfall 6) → hand bytes to html.go.

---

### `internal/web/errors.go` (utility, error taxonomy)

**Analog:** `internal/llm/openai_compat/httperror.go` — the repo's reference non-leaky structured HTTP error. It is the model for the TWO-LAYER error rule (D-26/27/28): a rich internal error for tests/logs, a sanitized model-visible struct.

**Bounded-body, key-safe, headline-omits-body** (httperror.go:24-38):
```go
type HTTPError struct {
	StatusCode    int
	RetryAfterSec int
	Body          string   // bounded by maxErrorBodyBytes; never the request → never the key
}
func (e *HTTPError) Error() string {
	if e.RetryAfterSec > 0 {
		return fmt.Sprintf("llm: provider returned HTTP %d (retry after %ds)", e.StatusCode, e.RetryAfterSec)
	}
	return fmt.Sprintf("llm: provider returned HTTP %d", e.StatusCode)
}
```
→ `errors.go` defines the D-38 stable enum (`web_search_unavailable`, `blocked_url`, `unsupported_scheme`, `unsupported_content_type`, `response_too_large`, `timeout`, `http_error`, `extraction_failed`) and a sanitized model-visible struct: `type WebError struct { Code string; Reason string; Message string; StatusCode int }` serialized to `{error, reason, message, status_code?}` (D-39/D-40). The Retry-After parse (httperror.go:46-51) is reusable for 429 handling in fetcher/searxng. **The sanitized struct carries NO IP/host/header/body/redirect-chain** (D-27) — assert this with a leak-scan test (RESEARCH SC: `TestError_NonLeaky`).

---

### `internal/web/config.go` (config, fail-fast)

**Analog:** `internal/config/config.go` — `envDefault`/`envIntDefault` helpers + the subsystem-config-in-its-own-package convention (config.go:1-9 package doc: "per-subsystem configs (db, knowledge, llm) live in their owning packages; this file only wires the top-level fields").

**Helpers to mirror** (config.go:164-185):
```go
func envDefault(key, fallback string) string { if v := os.Getenv(key); v != "" { return v }; return fallback }
func envIntDefault(key string, fallback int) int { /* parse-fail → fallback, never fatal */ }
```

**Fail-fast on a required value** — model on the LLM subsystem's pattern (config.go:69-73 wires `llm.Load()` whose error propagates). For D-05 (`SEARXNG_URL` missing is an ERROR, no localhost autodetect, no public fallback): `internal/web` exposes a `web.Config` with a `Load() (*Config, error)` that returns a sentinel when `SEARXNG_URL` is empty — but that sentinel must surface as the D-06 `web_search_unavailable{searxng_not_configured}` *at search time*, not as a boot-fatal (web tools are optional; `aura db migrate` must not require SEARXNG). **Decision for planner:** add web fields to `internal/web/config.go` read lazily by the engine, OR add them to the root `config.Config` like the Sandbox fields (config.go:53-55). The Sandbox precedent (root config, `envDefault`/`envIntDefault`, no fatal) is the closer in-repo match — prefer it for `AURA_WEB_*`; keep the `SEARXNG_URL` fail-CLOSED as a structured-unavailable at call time, not a boot error.

**Env catalog (RESEARCH Runtime State Inventory):** `SEARXNG_URL` (upstream-canonical name, no `AURA_` prefix), `AURA_WEB_DNS_PIN_TTL_SEC=60`, `AURA_WEB_FETCH_MAX_BODY_BYTES=5000000` (raw-body ceiling, NOT the markdown preview cap), `AURA_WEB_CACHE_PERSISTENT=false`, search/fetch timeout seconds, User-Agent default (`Aura/0.x web_fetch`, D-34/35). Add to `.env.example`. **Do NOT** add `AURA_WEB_FETCH_ALLOW_LOOPBACK`/`ALLOW_HOSTS` (D-30 + RESEARCH Open Question 1: CONTEXT wins over older PRD — no allowlist, no escape hatch).

---

### `cmd/aura/web.go` + `cmd/aura/main.go` (CLI, hand-rolled subcommand)

**Analog:** `cmd/aura/exec.go` — hand-parsed subcommand (no cobra, repo convention), sysexits-style exit codes, `config.LoadDB()` to skip the LLM key, engine construction, shared formatter reuse.

**Hand-parse + sysexits codes** (exec.go:18-22, 43-79) — mirror for `aura web doctor` (D-44) and `aura tool web_search/web_fetch '<json>'` (D-43):
```go
const ( exitUnreachable = 70; exitInfra = 71; exitUsage = 64 )
func parseExecArgs(args []string) (execArgs, error) { /* range loop, switch, no cobra */ }
```

**LoadDB (skip LLM key) + engine + run** (exec.go:105-127):
```go
cfg := config.LoadDB()                 // web doctor must not require OPENROUTER_API_KEY
runner := sandbox.NewDockerRunner(cfg) // → web: web.NewClient(cfg)
// ... run, print human output (D-45 human CLI only), os.Exit(code)
fmt.Println(tools.FormatLean(res))     // reuse the engine's own formatter, no drift
```
→ `aura web doctor`: check `SEARXNG_URL` set → reachable → JSON round-trip → tiny search (D-44), human output, distinct exit codes; NO public fallback. Smoke verbs `aura tool web_search`/`web_fetch` marshal the JSON arg and call the engine directly (reuse the same `web.Client`).

**main.go switch + buildRegistry (MODIFY)** (main.go:35-65 switch; 72-81 buildRegistry):
```go
case "web": runWeb(os.Args[2:])   // new case; update usage() string (main.go:69) + the doc block (main.go:1-13)
// in buildRegistry(), after the Execute line (main.go:79):
reg.Register(&tools.WebSearch{Engine: webEngine})
reg.Register(&tools.WebFetch{Engine: webEngine})  // webEngine := web.NewClient(config.LoadDB())
```
The registry auto-sorts (manifest.go:39) — just register; do not hand-order (RESEARCH anti-pattern). `aura tool ...` (D-43) is a NEW verb — confirm spelling vs existing `aura tools` (manifest print, main.go:36) to avoid collision; RESEARCH/D-43 use `aura tool web_search` (singular) which does not collide with `aura tools` (plural).

---

### `compose.yaml` (MODIFY) + `searxng/settings.yml` (NEW)

**Analog:** existing services in `compose.yaml` (neo4j:30-57, aura-llama-embed:60-85). Mirror: pinned image, `restart: unless-stopped`, read-only volume mount for config, healthcheck. **CRITICAL DIVERGENCE (D-03):** every existing service publishes a loopback host port (`ports: - "127.0.0.1:PORT:PORT"`); SearXNG MUST NOT — OMIT the `ports:` key entirely and put SearXNG + the app on a shared network so it reaches the public internet without host exposure.

**Service skeleton to copy** (compose.yaml aura-llama-embed:60-85 minus `ports`):
```yaml
  searxng:                          # ↓↓ Slice 5 (Phase 7) — meta-search, NO host port (D-03) ↓↓
    image: searxng/searxng:<pin>    # pin a tag at planning (RESEARCH A3/OQ4: internal port 8080)
    container_name: aura-searxng
    restart: unless-stopped
    # NO ports: — internal-only; reached as http://searxng:8080/search from the app (D-02)
    volumes:
      - ./searxng/settings.yml:/etc/searxng/settings.yml:ro   # read-only mount (D-04)
    networks:
      - aura-web                    # shared app network so SearXNG egresses to the internet (D-03)
    healthcheck:
      test: ["CMD-SHELL", "wget -qO- http://localhost:8080/healthz || exit 1"]   # verify image's health path
      interval: 10s
      timeout: 5s
      retries: 10
      start_period: 20s
```
Add `aura-web` (or reuse the default project network) to `networks:` (compose.yaml:139-143 style). `settings.yml` MUST set `search.formats: [html, json]` (D-04, RESEARCH Pitfall 5 — JSON 403s without it) plus a `server.secret_key`. The image port (8080 vs 8888) is RESEARCH OQ4/A3 — verify against the pinned image entrypoint.

---

## Shared Patterns

### Deferred-tool registration (applies to both web tools)
**Source:** `internal/agent/tools/spec.go:18-25` (Spec.Deferred), `manifest.go:24-41` (alpha-sort), `cmd/aura/main.go:72-81` (buildRegistry).
**Apply to:** `web_search.go`, `web_fetch.go`, `cmd/aura/main.go`.
```go
// Spec(): Deferred: true   (long description + JSON schema stay out of the default manifest)
// main.go buildRegistry(): reg.Register(&tools.WebSearch{...}); reg.Register(&tools.WebFetch{...})
// manifest.go Render() sorts by Name → cache-stable; NEVER hand-order.
```

### Spillover via NewResult (D-21, web_fetch large markdown)
**Source:** `internal/agent/tools/result.go:94-133` (NewResult) + `read_tool_output.go` (paging).
**Apply to:** `web_fetch.go` (success path only — D-41: only successful large content spills; errors stay inline small).
```go
return NewResult(ctx, contentMD)   // cap → preview → sidecar → read_tool_output footer; ZERO new code
```

### Non-leaky two-layer errors (D-26/27/28)
**Source:** `internal/llm/openai_compat/httperror.go:24-54`.
**Apply to:** `errors.go`, `searxng.go`, `fetcher.go`, both adapters.
Rich internal error (logged + asserted in tests) → adapter maps to sanitized `{error, reason, message, status_code?}`. NO IP/host/header/body/redirect-chain in the model-visible string.

### goleak-safe HTTP transport + test TestMain
**Source:** `internal/sandbox/docker.go:54-60` (`DisableKeepAlives: true`), `docker_test.go:1-37` (`//go:build !*_integration` + `goleak.VerifyTestMain`), `internal/agent/tools/main_test.go:13-15`.
**Apply to:** `transport.go`, and `internal/web/main_test.go` (add `goleak.VerifyTestMain(m)`).

### httptest-based table-driven security tests (deterministic, fail-closed)
**Source:** `internal/sandbox/docker_test.go:146-257` (`httptest.NewServer` per case), build-tag split for the live tier (`docker_integration_test.go` behind `//go:build sandbox_integration`).
**Apply to:** `ssrf_test.go` (table-driven IP classification SC#3 + non-leak scan), `fetcher_test.go` (httptest 302 redirect-revalidate, content-type/size gate, retry policy), `searxng_test.go` (JSON fixture parse + domain post-filter + unavailable), `dnspin_test.go` (injectable fake resolver flipping its answer on the 2nd call — RESEARCH "Injectable resolver" pitfall; preferred over a python dnslib sidecar for the unit tier). Add a new `web_integration` build tag for the live-SearXNG tier (RESEARCH §Test Framework). The recording dialer asserts the pinned IP without real network.

## No Analog Found

Files whose CORE LOGIC is greenfield (no in-repo precedent — use RESEARCH patterns + stdlib). Their STRUCT/test style still copies the analogs above; only the algorithm is new.

| File | Role | Data Flow | Reason / Source |
|------|------|-----------|-----------------|
| `internal/web/ssrf.go` **(CRITICAL — mutation ≥70%)** | utility | transform | No SSRF classifier exists in-repo. Source: RESEARCH Pattern 2 (`net/netip` `Unmap`+`Is*`+`Prefix.Contains` blocklist) + the 4 landmines (resolve-once-pin, mixed-record fail-closed, unmap-before-classify, redirect revalidate). |
| `internal/web/dnspin.go` | utility | transform | No per-conversation TTL pin cache exists. Source: RESEARCH §Code Examples DNS-pin shape (`pinKey{conv,host}` mutex map, TTL=`AURA_WEB_DNS_PIN_TTL_SEC`). |
| `internal/web/html.go` | utility | transform | No readability/markdown extraction exists. Source: RESEARCH §Code Examples (`readability.FromReader`→`Article.Node`→`htmltomarkdown.ConvertNode`; link dedup walk; 250-char low-content warning). **Anti-pattern:** NEVER `readability.FromURL` (self-fetches, bypasses SSRF). |
| `internal/web/cache.go` | store | CRUD | No in-process TTL cache exists; closest idiom is the dnspin mutex-map. Source: RESEARCH (in-mem `map`+mutex+TTL default; disk opt-in via `AURA_WEB_CACHE_PERSISTENT`, D-32). |
| `searxng/settings.yml` | config | — | New checked-in infra file. Source: D-04 + RESEARCH Pitfall 5 (`search.formats: [html, json]`). |

## God-Class Risk (CLAUDE.md NO FILE >600 LOC; PRD ≤300 target)

RESEARCH §Recommended Project Structure already pre-splits `internal/web` by concern so no file approaches the cap. The planner MUST keep this split (do NOT collapse the engine into one `web.go` — that revives the deprecated pre-rewrite 562-LOC `SearchTool` god class, RESEARCH anti-pattern + Deprecated list):

| Concern | File | Est. LOC | Split rationale |
|---------|------|----------|-----------------|
| SearXNG client (query build, JSON parse, domain filter) | `searxng.go` | ~200 | search-only |
| IP classification blocklist | `ssrf.go` | ~120 | **critical, mutation-tested in isolation** |
| DNS pin TTL cache | `dnspin.go` | ~80 | concurrency-safe, unit-testable alone |
| Pinned-IP transport + redirect revalidation | `transport.go` | ~150 | the http.Client + dialer + CheckRedirect |
| Fetch orchestration (scheme/MIME/size gate, retry) | `fetcher.go` | ~200 | the fetch state machine |
| Readability → markdown + link dedup | `html.go` | ~150 | pure CPU transform |
| In-mem/disk cache | `cache.go` | ~120 | cross-tool store |
| Error enum + sanitized shapes | `errors.go` | ~80 | taxonomy |
| Web config load | `config.go` | ~60 | env wiring |

Tool adapters: `web_search.go` ~70 LOC, `web_fetch.go` ~80 LOC (RESEARCH estimates). All under 600; all under the 300 PRD target. If any file approaches 300 during impl, apply the `<name>_<concern>.go` split (e.g. `fetcher_redirect.go`, `searxng_filter.go`) per CLAUDE.md refactor-on-touch.

## Metadata

**Analog search scope:** `internal/agent/tools/`, `internal/sandbox/`, `internal/llm/openai_compat/`, `internal/config/`, `cmd/aura/`, repo-root `compose.yaml`.
**Files read this session:** execute.go, spec.go, result.go, manifest.go, read_tool_output.go, docker.go, httperror.go, config.go, exec.go, main.go, compose.yaml, docker_test.go (head), main_test.go (tools).
**Cross-checked:** Register() call sites (grep), goleak/httptest usage (grep) — registration is via `reg.Register(...)` in `cmd/aura/main.go:buildRegistry`, manifest auto-sorts (manifest.go:39).
**Pattern extraction date:** 2026-06-02
