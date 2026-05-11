# Phase 2: LLM Reliability & Tool Intelligence - Context

**Gathered:** 2026-05-10
**Status:** Ready for planning

<domain>
## Phase Boundary

The LLM writes wiki pages through an explicit `write_wiki_page` tool with strict JSON Schema parameters and an optimistic-concurrency `expected_updated_at` ETag; retries on LLM calls classify errors into transient/content/permanent buckets and only escalate temperature for content-quality failures; wiki writes trigger an async, slug-only reindex worker (drop-newest, single worker) that runs independently of the user's turn; every wiki mutation creates a git commit, and commit failures mark the page `unversioned: true` with auto-clear on the next successful commit; tool definitions are injected into the LLM prompt via Qdrant semantic retrieval (top-K=5) on top of an always-on core, with the `tool_search` tool removed.

**Requirements:** WIKI-01, WIKI-02, LLM-01, LLM-02, INDEX-01, GIT-01, TOOL-01, TOOL-02 (from REQUIREMENTS.md)

**Phase 1 carry-forward (do not redecide):** actor pattern (per-user serialization), shared `internal/qdrant/` Client interface, 120s blocking startup health gate, `points_count > 0` warm-cache check, composition-only changes around `agentruntime/agentloop`.
</domain>

<decisions>
## Implementation Decisions

### `write_wiki_page` Tool Surface (WIKI-01, WIKI-02)
- **D-01:** Single `write_wiki_page` tool. Required parameters: `title` (string), `body` (string, full replace — no patch mode), `expected_updated_at` (RFC3339 string, ALWAYS required). Optional: `category`, `tags`, `related`, `sources`. Slug is derived from `title` via `wiki.Slug(title)` — the LLM never supplies slug directly. Frontmatter fields the LLM must NOT control: `slug`, `unversioned`, `schema_version`, `prompt_version`, `created_at`, `updated_at`.
- **D-02:** Sentinel for create: `expected_updated_at=""` (empty string) means "create-or-fail-if-exists" (mirrors HTTP `If-Match: *` for create-only semantics). For updates, the LLM MUST pass the exact `updated_at` it observed when reading the page. Tool semantics: page does not exist + expected="" → create; page exists + expected matches → update; otherwise → conflict.
- **D-03:** Conflict response is structured JSON returned as the tool result so the LLM can recover deterministically:
  ```json
  {"error": "conflict", "slug": "<derived>", "expected_updated_at": "<llm-supplied>", "actual_updated_at": "<on-disk>"}
  ```
  The LLM is expected to re-read the page (via existing `read_source` / search tools) and retry with the fresh ETag.
- **D-04:** Tool DESCRIPTION must instruct: "Always read the page first to obtain `updated_at`; pass `expected_updated_at=''` only when creating a brand-new page; on conflict, re-read and retry." Description doubles as the contract documentation for the LLM.
- **D-05:** `wiki.Store.WritePage` signature is extended with an optional `expectedUpdatedAt string` parameter (variadic or new method `WritePageWithExpected`). The existing `WritePage(ctx, page)` calls (from `internal/ingest/pipeline.go`, `internal/tools/tool_registry.go` LLM-tool registration) are NOT migrated in this phase — they keep "trust caller" semantics. Only the new `write_wiki_page` LLM tool flows through the ETag-checked path.

### Variable-Temperature Retry & Error Classification (LLM-01, LLM-02)
- **D-06:** Three error buckets, classified BEFORE any retry decision: **TRANSIENT** (HTTP 429, 5xx, network timeout, `context.DeadlineExceeded` only when not user-cancelled, transport errors), **CONTENT** (schema validation failure, empty assistant output when tools were expected, malformed JSON tool-call arguments, refused/policy-blocked content), **PERMANENT** (HTTP 401/403/400 except where re-classifiable, model-not-found, `context.Canceled`).
- **D-07:** Retry policy per bucket:
  - **TRANSIENT:** preserve caller's `Request.Temperature`, max 5 retries, exponential backoff with jitter (base 1s → cap 30s, jitter ratio 0.5 to prevent thundering-herd).
  - **CONTENT:** override `Request.Temperature` with staged schedule `0.0 → 0.3 → 0.7`, max 3 retries, NO backoff (immediate retry — content failures are not throughput-related), validation error message appended into the request as a system-style nudge ("Previous output failed validation: <error>. Retry with corrected output.").
  - **PERMANENT:** zero retries, surface error to caller.
- **D-08:** Temperature override semantics: first attempt always uses caller's `Request.Temperature` (e.g. wiki writes start at deterministic 0). Wrapper rewrites `Request.Temperature` ONLY on CONTENT retry. On TRANSIENT retry, caller's temperature is preserved. This preserves explicit caller intent (wiki writes are 0; chat is nil = API default) on the happy path.
- **D-09:** Classifier returns a structured value: `Bucket` enum + `Retryable bool` + cleaned `Message` (with secrets/URLs/tokens redacted). Classification is by HTTP status code first, error sentinel second, message-pattern match last (Hermes-style). Pattern matching covers OpenAI / OpenAI-compatible providers; provider-specific extensions deferred.
- **D-10:** Existing `RetryClient` (fixed-temp 5-retry exponential) is rewritten in place as `ClassifyRetryClient`. Same constructor signature `NewRetryClient(inner Client, cfg RetryConfig)` so callers don't change. `RetryConfig` gains `MaxContentRetries int`, `ContentTemperatures []float64`, `JitterRatio float64`.

### Async Wiki Reindex Worker (INDEX-01)
- **D-11:** New package `internal/reindex/`. Worker is slug-only: `Job{Slug string, Op Op}` where `Op` is `OpUpsert` or `OpDelete`. Body is NOT carried in the job — the worker re-reads the page from disk when processing, so drop-newest is safe (worker always sees the latest snapshot).
- **D-12:** Single worker goroutine. Buffered channel `chan Job` capacity 100 (configurable via env / `RuntimeSettings`). Submit is non-blocking via `select { case w.jobs <- job: default: log.Warn("reindex queue full, dropped"); }` — drop-newest, the simpler and idiomatic Go pattern.
- **D-13:** Lifecycle: dedicated `context.Context` for the worker, cancelled on `Stop()`. Worker exits its select loop when `ctx.Done()`. Channel is NEVER closed (let GC handle it after the worker goroutine exits) to avoid send-on-closed-channel races. `Stop()` returns after the worker goroutine exits (signaled via `done` chan).
- **D-14:** Submitter interface (consumed by `wiki.Store` and `internal/ingest`): `type Submitter interface { Submit(Job) bool }` returns `false` when dropped (caller may log). The wiki write path enqueues after the file write succeeds, regardless of git commit outcome — reindex must run even on `unversioned: true` pages.
- **D-15:** Worker calls `search.WikiPageReindexer.ReindexWikiPage(ctx, slug)` for upsert. The current implementation rebuilds the entire collection (`Engine.IndexWikiPages`) — replacing this with an incremental upsert is a follow-up but NOT scoped here; the worker's job is the buffering/coalescing layer, and the underlying reindex operation may evolve later. (The embedding cache in `EmbedCache` already keeps the rebuild cheap when content is unchanged.)
- **D-16:** Health surface: `Worker.Health() ReindexHealth { QueueDepth int; Dropped int64; LastSuccess time.Time; LastError string }`. Wired into `/api/health` for dashboard visibility.

### Git Commit Tracking & `unversioned` Flag (GIT-01)
- **D-17:** On `wiki.Store.WritePage` commit failure (existing `gitCommit` at `store.go:351` already returns an error but only logs): re-read the just-written page, set `page.Unversioned = true` in frontmatter, atomic re-write the file. NO commit attempted on this metadata-only re-write (would just fail again). Function returns success — the user-facing write succeeded; only versioning is degraded.
- **D-18:** Auto-clear on next successful commit for the same slug: after `gitCommit` returns nil, re-read the page; if `page.Unversioned == true`, clear it and atomic re-write. The newly-cleared file is NOT committed in the same call (avoids loop-back).
- **D-19:** New `Page.Unversioned bool` field in `internal/wiki/schema.go` with `omitempty` JSON/YAML tag. Schema version is NOT bumped (additive backward-compatible field).
- **D-20:** Dashboard UX: surface `unversioned: true` as a yellow "Git tracking pending" badge on the page detail view. Do NOT use language like "not tracked" or "not saved" — the page IS saved on disk; only git history is pending. API `/api/wiki/{slug}` includes `unversioned` field passthrough.
- **D-21:** Upgrade `github.com/go-git/go-git/v5` from v5.18.0 to v5.19.0 (compatible minor; security/dependency refresh). One-line `go.mod` change verified by `go build ./...` and `go test ./internal/wiki/`.

### Tool Retrieval / Auto-Injection (TOOL-01, TOOL-02)
- **D-22:** Hybrid injection model. Always-on core injected on every turn (6 tools: `write_wiki_page`, `search_memory`, `list_sources`, `read_source`, `schedule_task`, `request_dashboard_token`). Plus top-K=5 supplemental tools retrieved per turn via Qdrant semantic match against the latest user message. Total injected per turn ≤ 11 tool definitions. [Revised 2026-05-11: original D-22 named 7 tools including `list_memory`/`read_memory`/`read_skill` — verified at planning time that none of these three names are registered in the codebase. Substitutes: `list_sources`/`read_source` (real source-read paths, second-brain analogs of the intended memory tools); `read_skill` dropped (skills load via prompt overlay, not a tool).]
- **D-23:** Retrieval query is the LATEST USER MESSAGE only (single-step query per arXiv 2511.01854 Tool-to-Agent Retrieval). NOT the full conversation, NOT the last N turns. Empty / cold-start (system message only, no user turn yet) → core only.
- **D-24:** Embedding strategy for tools: single-vector `name + " " + description` per tool. Examples are NOT embedded (research shows name+description sufficient and keeps the index small). Reuses `internal/tools/registry_search_vector.go` `toolVectorIndex` — exported as `ToolVectorIndex`, methods promoted to the package surface.
- **D-25:** Fallback when Qdrant is down or the index has zero docs: inject the FULL toolset (current behavior preserved) and log degraded mode at WARN. Never fail the turn because tool retrieval failed — degrade to "always works, just slower / more prompt".
- **D-26:** Index lifecycle: built once at startup (after Qdrant health gate passes from Phase 1), AND rebuilt whenever a tool is registered/unregistered after startup (rare — only when MCP servers reconnect or LLM-written Python tools are added via `SaveTool`). The QDRANT-01 warm-cache short-circuit (`points_count > 0`) already handles restarts efficiently — no reindex if cache is warm.
- **D-27:** `internal/tools/tool_search.go` and its test file are DELETED. The `tier == tierSearch` case in `internal/agentloop/loop.go:462` is removed. The `ToolSearchTool` constructor reference in `internal/telegram/setup.go:746` is removed. References in `internal/telegram/debug_smoke*.go`, `cmd/debug_telegram_sandbox/main.go`, and tests are cleaned up.
- **D-28:** Agent loop integration: `agentloop.Options.ToolsProvider func() []llm.ToolDefinition` is invoked per turn (currently set once). It composes core ∪ retrieved. The provider closure captures the registry + the latest user message context.

### Package Layout
- **D-29:** New packages and files:
  - `internal/reindex/` (NEW package) — `worker.go`, `types.go`, `worker_test.go`. No upstream deps beyond `wiki.PageReader`-style interfaces and `search.WikiPageReindexer`. Pure Go stdlib concurrency (consistent with Phase 1 carry-forward).
  - `internal/llm/classify.go` (NEW file in existing pkg) — `Bucket` enum, `Classify(error) Bucket`, sentinel error types (`ErrSchemaValidation`, `ErrEmptyOutput`, etc. as needed).
  - `internal/tools/wiki.go` (NEW file in existing pkg) — `WriteWikiPageTool` implementing `Tool`, depends on `wiki.PageReader+PageWriter` and `reindex.Submitter`.
- **D-30:** Modified files:
  - `internal/llm/retry.go` — `RetryClient` rewritten as classify-aware; exported config fields added.
  - `internal/wiki/store.go` — `WritePage` extended for `expected_updated_at` semantics and `unversioned` flag handling.
  - `internal/wiki/schema.go` — `Page.Unversioned bool` added.
  - `internal/tools/registry_search_vector.go` — export `ToolVectorIndex` and methods.
  - `internal/agentloop/loop.go` — `ToolsProvider` invoked per turn; remove `tierSearch` branch.
  - `internal/telegram/setup.go` — wire `reindex.Worker`, remove `tool_search` registration, set `ToolsProvider` closure.
  - `go.mod` / `go.sum` — go-git v5.18.0 → v5.19.0.
- **D-31:** Deleted files: `internal/tools/tool_search.go`, `internal/tools/tool_search_test.go`. Other files referencing `tool_search` are cleaned in place.
- **D-32:** No "umbrella" package (no `internal/reliability/`). Component-aligned packages preserve testability and respect the CLAUDE.md god-class rule (no file > 600 LOC).

### Claude's Discretion
- Exact field names within the conflict response payload (kept structured per D-03 but JSON key shapes are Claude's call).
- Internal worker logging shape (zap fields, log levels) following existing `internal/logging` conventions.
- `ReindexConfig` env var names (consistent with existing `RUNTIME_*` pattern).
- Whether to add a `WritePageWithExpected(ctx, page, expected)` method or extend `WritePage` with variadic — implementation detail.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & Roadmap
- `.planning/REQUIREMENTS.md` — Phase 2 requirements: WIKI-01, WIKI-02, LLM-01, LLM-02, INDEX-01, GIT-01, TOOL-01, TOOL-02
- `.planning/ROADMAP.md` §"Phase 2" — phase goal, dependencies (Phase 1), 6 success criteria
- `.planning/research/SUMMARY.md` §"Phase 2: Core Hardening" — research synthesis
- `.planning/research/PITFALLS.md` §Pitfalls 4, 6, 7 — reindex goroutine leak, wiki write race, temperature retry on infra errors
- `.planning/research/FEATURES.md` §3 (tool-based wiki creation), §4 (variable-temp retry), §5 (async reindex), §6 (circuit breaker dependency notes)

### Phase 1 Carry-Forward
- `.planning/phases/01-fondamenta-concurrency-safety/01-CONTEXT.md` — actor pattern decisions D-01..D-16, shared Qdrant client D-17..D-20

### Existing Code (Phase 2 will modify)
- `internal/llm/retry.go` — fixed-temp `RetryClient` to be rewritten as classify-aware
- `internal/llm/client.go` — `Client` interface, `Request{Temperature *float64}`, `Response`, `Token` (do not change shapes)
- `internal/wiki/store.go:160` — `WritePage` (extended with ETag), `:351` `gitCommit` (already returns error; now wired to `unversioned` flag)
- `internal/wiki/schema.go` — `Page` struct (gains `Unversioned bool`)
- `internal/tools/registry_search_vector.go` — `toolVectorIndex` (already Qdrant-backed; exported in this phase)
- `internal/tools/registry_search.go` — `Registry.Search` (already merges lex+vector; consumed by ToolsProvider)
- `internal/tools/tool_search.go` — DELETED in this phase
- `internal/agentloop/loop.go` — `Options.ToolsProvider` (currently called once; called per-turn after this phase)
- `internal/telegram/setup.go:519`, `:746` — tool registration sites
- `internal/search/search.go:449 ReindexWikiPage` — synchronous full-rebuild today; the new `internal/reindex/` worker invokes this (incremental upsert is a future improvement)
- `internal/ingest/pipeline.go:155–180` — calls `WritePage` then `ReindexWikiPage` synchronously; switch reindex call to `reindex.Submitter` enqueue

### External References (for the planner / researcher)
- Tool RAG (Red Hat 2025-11-26) — top-K=5, name+description embeddings: https://next.redhat.com/2025/11/26/tool-rag-the-next-breakthrough-in-scalable-ai-agents/
- Tool-to-Agent Retrieval (arXiv 2511.01854) — single-step query, K=5 evaluation: https://arxiv.org/html/2511.01854v1
- Maxim production retry guide — classify-then-retry, jitter: https://www.getmaxim.ai/articles/retries-fallbacks-and-circuit-breakers-in-llm-apps-a-production-guide/
- LangChain structured output retry — error feedback into ToolMessage: https://docs.langchain.com/oss/python/langchain/structured-output
- Ed-Fi ETag concurrency guide — `If-Match` + `412 Precondition Failed`: https://docs.ed-fi.org/reference/data-exchange/api-guidelines/design-and-implementation-guidelines/api-implementation-guidelines/handling-optimistic-concurrency-with-etags/
- Qdrant large-scale ingestion — micro-batch 100/5s pattern: https://qdrant.tech/course/essentials/day-4/large-scale-ingestion/
- go-git v5.19.0 release: https://github.com/go-git/go-git/releases

### Analog Repo Patterns (D:\tmp)
- `D:\tmp\picobot\internal\agent\tools\write_memory.go` — write-tool JSON Schema with required+optional split, content pre-validation
- `D:\tmp\hermes-agent\agent\error_classifier.py` — `FailoverReason` enum, `ClassifiedError` dataclass, retryable/should_compress hint flags
- `D:\tmp\hermes-agent\agent\retry_utils.py` — jittered exponential backoff (`base=5s`, `max=120s`, `jitter_ratio=0.5`)

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`wiki.Store.gitCommit`** (`store.go:351`) already serializes git operations via `gitMu` and returns an error — it just isn't surfaced as a flag yet. GIT-01 wires the flag onto the existing return value.
- **`wiki.Store.fileMutex`** — per-slug mutex already prevents concurrent file corruption; the `expected_updated_at` check is layered on top inside the same critical section.
- **`toolVectorIndex`** (`registry_search_vector.go`) — already Qdrant-backed with the Phase 1 shared `internal/qdrant/` client, with WR-04's `mu` released around HTTP I/O. Exported in Phase 2 to be visible to the agent loop's tools-provider closure.
- **`Registry.Search`** (`registry_search.go`) — already merges lexical + vector results with score blending; reused for top-K=5 retrieval.
- **`EmbedCache`** (`internal/search/embed_cache.go`) — content-addressed SHA cache makes repeated reindex cheap; the worker's per-page reindex amortizes well.
- **`BufferedAppender`** (`internal/conversation/archive.go`) — same `select/default` coalescing pattern referenced by Phase 1; the reindex worker mirrors this idiom.

### Established Patterns
- **Composition over modification** — `agentruntime/agentloop` are stable. The `ClassifyRetryClient` wraps `llm.Client` like the current `RetryClient` does. The reindex `Worker` is a sidecar consumed via the `Submitter` interface, not embedded in `wiki.Store`.
- **Go stdlib-only concurrency** — channels + `context.Context` + `sync.Mutex`. No worker pool libraries (consistent with Phase 1 carry-forward).
- **Phase 1 actor model** — wiki writes happen INSIDE the per-user actor goroutine. Reindex submission is non-blocking (drop-newest), so the actor never blocks on Qdrant. This is the canonical interaction.
- **Tool argument privacy** — `internal/logging` redacts tool argument values; only argument keys are logged. `Classify`'s cleaned message MUST follow this convention.
- **No secrets in logs** — error classification's pattern matching against rate-limit / billing strings runs on already-redacted error messages.

### Integration Points
- **`telegram.setup.go` Bot creation** — wires `reindex.Worker` after Qdrant client is created, before `RegisterTool(write_wiki_page)`. Worker `Stop()` is called from `Bot.Shutdown()` (shutdown sequence: cancel context → wait worker `done` → close channels owned elsewhere).
- **`agentloop.Options.ToolsProvider`** — called per-turn (currently called at registration). Closure captures `Registry` + a function that returns the latest user message text. Returns core ∪ retrieved.
- **`wiki.Store` ↔ `reindex.Submitter`** — `WritePage` calls `submitter.Submit(reindex.Job{Slug: slug, Op: OpUpsert})` after the file write succeeds (regardless of git commit outcome). `DeletePage` submits `OpDelete`. New optional dependency injected via store config.
- **`internal/api/wiki.go` ↔ `unversioned` flag** — page detail JSON includes `unversioned bool` passthrough; web dashboard renders the badge in the page detail view.

</code_context>

<specifics>
## Specific Ideas

- ETag is the wiki page's existing `updated_at` RFC3339 string — no separate version column, no hash. Strong validator without extra storage.
- Conflict response is a TOOL RESULT JSON (not an error string) so the LLM can parse it deterministically and re-read+retry without prompt-engineering each provider's error narrative.
- Temperature override on CONTENT retry is the wrapper's responsibility, NOT the tool's responsibility. Wiki write tool keeps requesting `temperature=0`; the wrapper escalates only on schema/validation failure.
- Reindex worker is slug-only because `wiki.Store` is the source of truth on disk; the worker re-reads to coalesce naturally.
- Drop-newest, NOT drop-oldest: simpler, idiomatic, and safe given disk-backed re-read semantics.
- Always-on core list is HARD-CODED in agentloop wiring (7 tool names), not derived. Easy to audit, no surprise injection.
- Cold-start (no user turn yet) injects only core — never an empty toolset.
- Qdrant-down fallback injects FULL toolset, NEVER fails the turn.
- `Page.Unversioned` is the badge label on dashboard. Phrasing: "Git tracking pending" (not "not tracked" — the page IS saved).
- go-git upgrade is a one-line `go.mod` change; verify via `go build ./...` + `go test ./internal/wiki/`.
- All new code obeys CLAUDE.md god-class rule: every file < 600 LOC. `internal/wiki/store.go` is 755 LOC TODAY — Phase 2 must NOT push it past 800 LOC; if WriteWikiPage logic + Unversioned handling threatens that ceiling, factor a `store_writes.go` companion file (existing pkg, file split only).

</specifics>

<deferred>
## Deferred Ideas

- **Incremental Qdrant upsert (single point) replacing `Engine.IndexWikiPages` full-rebuild on per-page reindex.** Phase 2 keeps the existing rebuild as the worker's reindex implementation; replacing it with a single-point upsert is a follow-up performance improvement. Current rebuild stays cheap because `EmbedCache` skips unchanged pages.
- **Provider-specific error pattern extensions** (Anthropic thinking-signature, OpenRouter policy blocks, etc. — Hermes' 13-class taxonomy). Phase 2 covers OpenAI / OpenAI-compatible only. Extensions deferred until a non-OpenAI provider is wired.
- **Worker pool (N=2) + micro-batch coalescing** for the reindex worker. Worth revisiting if bulk imports show queue-full drops; not needed at current ≤50 pages scale.
- **Test strategy details** (Qdrant mock fixtures, dashboard concurrent-edit test, race-detector coverage for worker shutdown, cold-start tool injection assertion). Captured here so the planner picks them up; not a discussion-time decision.
- **Generic optimistic-concurrency on all `WritePage` callers** (`internal/ingest/pipeline.go`, `internal/tools/tool_registry.go` LLM-tool registration, etc.). Phase 2 wires ETag only on the new LLM-callable tool. Migrating internal callers is a follow-up that needs its own review.
- **Dashboard "manual git commit retry" action** for `unversioned` pages. Phase 2 auto-clears on next successful write; an explicit button is UX polish.

</deferred>

---

*Phase: 2-LLM Reliability & Tool Intelligence*
*Context gathered: 2026-05-10*
