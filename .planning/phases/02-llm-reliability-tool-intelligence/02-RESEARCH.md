# Phase 2: LLM Reliability & Tool Intelligence - Research

**Researched:** 2026-05-10
**Domain:** Go in-tree reliability infrastructure (LLM retry classification, optimistic-concurrency tool, async background worker, Qdrant tool retrieval, go-git audit) for an existing single-binary Telegram agent
**Confidence:** HIGH (all 32 CONTEXT.md decisions are anchored to concrete files in the codebase verified via Read; external library version verified)

## Summary

Phase 2 layers reliability and tool-intelligence infrastructure on top of stable Phase 1 surfaces. Every change is composition (wrap an existing interface, add a new package, factor a file) rather than rewrite — `internal/agentruntime` and `internal/agentloop` are deliberately untouched at the structural level. The seven concrete pieces of work are: (a) a new `write_wiki_page` tool with strict JSON Schema and an always-required `expected_updated_at` ETag; (b) extending `wiki.Store.WritePage` to compare ETag inside the existing per-slug `fileMutex` critical section and to set/clear the new `Page.Unversioned` flag on `gitCommit` failure; (c) rewriting `internal/llm/retry.go` as a classify-then-retry wrapper with three buckets (TRANSIENT same-temp 5x backoff+jitter, CONTENT staircase 0→0.3→0.7 max 3, PERMANENT zero retries); (d) a new `internal/reindex/` package — single goroutine, buffered chan cap 100, drop-newest, dedicated context for clean Stop; (e) per-turn `ToolsProvider` closure assembling 7 always-on core tools ∪ top-K=5 Qdrant retrieval, with a non-empty-toolset invariant on cold-start and Qdrant-down; (f) deletion of `tool_search.go` plus three mop-up sites that reference it; (g) one-line go-git v5.18.0 → v5.19.0 upgrade.

The largest landmines are concurrency-shaped: the ETag check MUST live inside `fileMutex(slug)` (TOCTOU race otherwise), the reindex worker MUST own a dedicated context cancelled by `Stop()` (Pitfall #4 goroutine-leak otherwise), the reindex channel MUST NOT be closed by producers (`send on closed channel` panic otherwise), and the classifier `cleaned` message MUST be redacted BEFORE any zap field is set (CLAUDE.md "no secrets in logs" otherwise). The largest scope risk is `internal/wiki/store.go` (755 LOC today, ALREADY violates the 600-LOC rule) — Phase 2 must factor `store_writes.go` instead of growing `store.go` past 800.

**Primary recommendation:** Follow the file plan in CONTEXT.md D-29..D-31 verbatim (one new package + two new files in existing packages + one factored sibling file). Build the work in dependency order — `internal/llm/classify.go` and `internal/reindex/` are leaf work with no upstream dependencies and can land first, then `wiki.Page.Unversioned` + `wiki.Store.WritePage(... expected ...string)` extension, then `internal/tools/wiki.go` (which depends on both), then the wiring change in `internal/telegram/setup.go` (which depends on all of the above), then the deletion of `tool_search.go` (which the agent loop and `setup.go` no longer need by then). Every commit on every step closes with `go vet ./... && go build ./... && go test -race ./internal/{llm,reindex,tools,wiki}/` per CLAUDE.md Post-Edit Validation.

## User Constraints (from CONTEXT.md)

### Locked Decisions

#### `write_wiki_page` Tool Surface (WIKI-01, WIKI-02)
- **D-01:** Single `write_wiki_page` tool. Required parameters: `title` (string), `body` (string, full replace — no patch mode), `expected_updated_at` (RFC3339 string, ALWAYS required). Optional: `category`, `tags`, `related`, `sources`. Slug is derived from `title` via `wiki.Slug(title)` — the LLM never supplies slug directly. Frontmatter fields the LLM must NOT control: `slug`, `unversioned`, `schema_version`, `prompt_version`, `created_at`, `updated_at`.
- **D-02:** Sentinel for create: `expected_updated_at=""` (empty string) means "create-or-fail-if-exists". For updates, the LLM MUST pass the exact `updated_at` it observed when reading the page. Tool semantics: page does not exist + expected="" → create; page exists + expected matches → update; otherwise → conflict.
- **D-03:** Conflict response is structured JSON returned as the tool result so the LLM can recover deterministically: `{"error":"conflict","slug":"<derived>","expected_updated_at":"<llm-supplied>","actual_updated_at":"<on-disk>"}`. The LLM is expected to re-read the page (via existing `read_source` / search tools) and retry with the fresh ETag.
- **D-04:** Tool DESCRIPTION must instruct: "Always read the page first to obtain `updated_at`; pass `expected_updated_at=''` only when creating a brand-new page; on conflict, re-read and retry."
- **D-05:** `wiki.Store.WritePage` signature is extended with an optional `expectedUpdatedAt string` parameter (variadic or new method `WritePageWithExpected`). Existing `WritePage(ctx, page)` calls (from `internal/ingest/pipeline.go`, `internal/wiki/parser.go` Writer, `internal/wiki/store.go` RepairLink) are NOT migrated in this phase — they keep "trust caller" semantics.

#### Variable-Temperature Retry & Error Classification (LLM-01, LLM-02)
- **D-06:** Three error buckets, classified BEFORE any retry decision: **TRANSIENT** (HTTP 429, 5xx, network timeout, `context.DeadlineExceeded` only when not user-cancelled, transport errors), **CONTENT** (schema validation failure, empty assistant output when tools were expected, malformed JSON tool-call arguments, refused/policy-blocked content), **PERMANENT** (HTTP 401/403/400 except where re-classifiable, model-not-found, `context.Canceled`).
- **D-07:** Retry policy per bucket:
  - **TRANSIENT:** preserve caller's `Request.Temperature`, max 5 retries, exponential backoff with jitter (base 1s → cap 30s, jitter ratio 0.5).
  - **CONTENT:** override `Request.Temperature` with staged schedule `0.0 → 0.3 → 0.7`, max 3 retries, NO backoff (immediate retry), validation error message appended into the request as a system-style nudge.
  - **PERMANENT:** zero retries, surface error to caller.
- **D-08:** Temperature override semantics: first attempt always uses caller's `Request.Temperature`. Wrapper rewrites `Request.Temperature` ONLY on CONTENT retry. On TRANSIENT retry, caller's temperature is preserved.
- **D-09:** Classifier returns a structured value: `Bucket` enum + `Retryable bool` + cleaned `Message` (with secrets/URLs/tokens redacted). Classification is by HTTP status code first, error sentinel second, message-pattern match last (Hermes-style). Pattern matching covers OpenAI / OpenAI-compatible providers; provider-specific extensions deferred.
- **D-10:** Existing `RetryClient` (fixed-temp 5-retry exponential) is rewritten in place as `ClassifyRetryClient`. Same constructor signature `NewRetryClient(inner Client, cfg RetryConfig)` so callers don't change. `RetryConfig` gains `MaxContentRetries int`, `ContentTemperatures []float64`, `JitterRatio float64`.

#### Async Wiki Reindex Worker (INDEX-01)
- **D-11:** New package `internal/reindex/`. Worker is slug-only: `Job{Slug string, Op Op}` where `Op` is `OpUpsert` or `OpDelete`. Body is NOT carried — worker re-reads from disk.
- **D-12:** Single worker goroutine. Buffered channel `chan Job` capacity 100 (configurable). Submit non-blocking via `select { case w.jobs <- job: default: log.Warn("reindex queue full, dropped"); }` — drop-newest.
- **D-13:** Lifecycle: dedicated `context.Context` for worker, cancelled on `Stop()`. Worker exits select loop on `ctx.Done()`. Channel is NEVER closed — let GC handle it. `Stop()` returns after worker goroutine exits (signaled via `done` chan).
- **D-14:** Submitter interface: `type Submitter interface { Submit(Job) bool }` returns `false` when dropped. Wiki write enqueues after the file write succeeds, regardless of git commit outcome.
- **D-15:** Worker calls `search.WikiPageReindexer.ReindexWikiPage(ctx, slug)` for upsert. Current implementation rebuilds the entire collection (`Engine.IndexWikiPages`) — replacing this with an incremental upsert is deferred. `EmbedCache` keeps the rebuild cheap.
- **D-16:** Health surface: `Worker.Health() ReindexHealth { QueueDepth int; Dropped int64; LastSuccess time.Time; LastError string }`. Wired into `/api/health` for dashboard visibility.

#### Git Commit Tracking & `unversioned` Flag (GIT-01)
- **D-17:** On `wiki.Store.WritePage` commit failure: re-read just-written page, set `page.Unversioned = true`, atomic re-write the file. NO commit attempted on this metadata-only re-write. Function returns success — user-facing write succeeded; only versioning is degraded.
- **D-18:** Auto-clear on next successful commit for the same slug: after `gitCommit` returns nil, re-read the page; if `page.Unversioned == true`, clear it and atomic re-write. The newly-cleared file is NOT committed in the same call (avoids loop-back).
- **D-19:** New `Page.Unversioned bool` field in `internal/wiki/schema.go` with `omitempty` JSON/YAML tag. Schema version is NOT bumped (additive backward-compatible field).
- **D-20:** Dashboard UX: surface `unversioned: true` as a yellow "Git tracking pending" badge on the page detail view. API `/api/wiki/{slug}` includes `unversioned` field passthrough.
- **D-21:** Upgrade `github.com/go-git/go-git/v5` from v5.18.0 to v5.19.0 (compatible minor; security/dependency refresh).

#### Tool Retrieval / Auto-Injection (TOOL-01, TOOL-02)
- **D-22:** Hybrid injection model. Always-on core injected on every turn (6 tools — revised 2026-05-11 per plan-checker iteration 3, see CONTEXT.md D-22 note: `write_wiki_page`, `search_memory`, `list_sources`, `read_source`, `schedule_task`, `request_dashboard_token`). Plus top-K=5 supplemental tools retrieved per turn via Qdrant semantic match against the latest user message. Total injected per turn ≤ 11 tool definitions.
- **D-23:** Retrieval query is the LATEST USER MESSAGE only (single-step query per arXiv 2511.01854). NOT the full conversation. Empty / cold-start (system message only, no user turn yet) → core only.
- **D-24:** Embedding strategy for tools: single-vector `name + " " + description` per tool. Examples are NOT embedded. Reuses `internal/tools/registry_search_vector.go` `toolVectorIndex` — exported as `ToolVectorIndex`.
- **D-25:** Fallback when Qdrant is down or the index has zero docs: inject the FULL toolset and log degraded mode at WARN. Never fail the turn.
- **D-26:** Index lifecycle: built once at startup (after Qdrant health gate passes), AND rebuilt whenever a tool is registered/unregistered after startup.
- **D-27:** `internal/tools/tool_search.go` and `tool_search_test.go` are DELETED. References in `internal/telegram/setup.go:746`, `internal/telegram/conversation.go:259,293,306,331`, `internal/telegram/conversation_tool_exec.go:114`, `internal/telegram/debug_smoke.go:186`, `cmd/debug_telegram_sandbox/main.go` (lines 61, 195, 319, 816), and tests are cleaned in place.
- **D-28:** Agent loop integration: `agentloop.Options.ToolsProvider func() []llm.ToolDefinition` is invoked per turn. The provider closure captures the registry + the latest user message context.

#### Package Layout
- **D-29:** New packages and files:
  - `internal/reindex/` (NEW package) — `worker.go`, `types.go`, `worker_test.go`.
  - `internal/llm/classify.go` (NEW file) — `Bucket` enum, `Classify(error) Bucket`, sentinel error types.
  - `internal/tools/wiki.go` (NEW file) — `WriteWikiPageTool`.
- **D-30:** Modified files:
  - `internal/llm/retry.go` — `RetryClient` rewritten as classify-aware.
  - `internal/wiki/store.go` — `WritePage` extended for `expected_updated_at` semantics and `unversioned` flag handling.
  - `internal/wiki/schema.go` — `Page.Unversioned bool` added.
  - `internal/tools/registry_search_vector.go` — export `ToolVectorIndex` and methods.
  - `internal/agentloop/loop.go` — `ToolsProvider` invoked per turn.
  - `internal/telegram/setup.go` — wire `reindex.Worker`, remove `tool_search` registration, set `ToolsProvider` closure.
  - `go.mod` / `go.sum` — go-git v5.18.0 → v5.19.0.
- **D-31:** Deleted files: `internal/tools/tool_search.go`, `internal/tools/tool_search_test.go`. Other files referencing `tool_search` are cleaned in place.
- **D-32:** No "umbrella" package (no `internal/reliability/`).

### Claude's Discretion
- Exact field names within the conflict response payload (kept structured per D-03 but JSON key shapes are Claude's call).
- Internal worker logging shape (zap fields, log levels) following existing `internal/logging` conventions.
- `ReindexConfig` env var names (consistent with existing `RUNTIME_*` pattern).
- Whether to add a `WritePageWithExpected(ctx, page, expected)` method or extend `WritePage` with variadic — implementation detail.
- Exact rendering of "Git tracking pending" badge in the React dashboard.

### Deferred Ideas (OUT OF SCOPE)
- **Incremental Qdrant upsert (single point) replacing `Engine.IndexWikiPages` full-rebuild.** Phase 2 keeps the existing rebuild as the worker's reindex implementation. `EmbedCache` keeps it cheap.
- **Provider-specific error pattern extensions** (Anthropic thinking-signature, OpenRouter policy blocks, Cerebras streaming-truncation). Phase 2 covers OpenAI / OpenAI-compatible only.
- **Worker pool (N=2) + micro-batch coalescing** for reindex worker.
- **Generic optimistic-concurrency on all `WritePage` callers** (`internal/ingest/pipeline.go`, `internal/wiki/parser.go` Writer, `internal/wiki/store.go` RepairLink). Phase 2 wires ETag only on the new LLM-callable tool.
- **Dashboard "manual git commit retry" action** for `unversioned` pages.
- **Test strategy details** — captured here for the planner to pick up.

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| WIKI-01 | `write_wiki_page` tool with JSON Schema parameters — replaces `looksLikeWikiYAML` text-heuristic detection | D-01..D-05 (tool surface), §"Tool-call enforcement" investigation below confirms NO `looksLikeWikiYAML` exists today — heuristic path is the `Writer.WriteFromLLMOutput` flow in `internal/wiki/parser.go`; phase removes its agent-loop-facing role by making `write_wiki_page` the LLM's only entry into wiki writes |
| WIKI-02 | `expected_updated_at` carrying — detect concurrent manual dashboard edits | D-01..D-05; existing `Page.UpdatedAt` (RFC3339) is the strong validator; existing `fileMutex(slug)` already serializes in the right place; Pitfall #6 verified as the failure mode this prevents |
| LLM-01 | Variable-temperature retry on schema/content failure with error feedback | D-06..D-08 (CONTENT bucket: `0.0 → 0.3 → 0.7`, max 3, no backoff); D-09 (cleaned message becomes the system-style nudge) |
| LLM-02 | Error classification separates transient (429/5xx/timeout) from content failures | D-06, D-09 (priority pipeline: HTTP status → error sentinel → message-pattern match); Pitfall #7 verified — current `RetryClient` retries blindly without classification |
| INDEX-01 | Async reindex with backpressure — buffered channel + select/default coalescing; dropped signals safe | D-11..D-16; existing `internal/conversation.BufferedAppender` (archive.go:332-385) is the precedent shape; safety property holds because worker re-reads from disk on every job |
| GIT-01 | Failed commits mark `unversioned: true` for audit | D-17..D-21; existing `gitCommit` already returns `error` but only logs (`store.go:213`, `:298`, `:469`, `:490-495`, `:530`); flag wires onto the existing return value; go-git v5.18→v5.19 verified compatible |
| TOOL-01 | Qdrant-backed semantic tool retrieval — context-relevant tool injection | D-22..D-26; existing `toolVectorIndex` already Qdrant-backed with `searchableToolText` (name + description + tags + examples); Phase 2 narrows the embedding text and re-shapes the index lifecycle |
| TOOL-02 | `tool_search` removed; tool discovery automatic | D-27, D-28; 6 deletion/cleanup sites identified across `internal/telegram/`, `internal/tools/`, and `cmd/debug_telegram_sandbox/` |

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| LLM tool-call validation, retry, and temperature override | API/Backend (`internal/llm` wrapper) | — | Pure server-side concern; never touches client. The wrapper sits on top of `llm.Client` interface and is invoked synchronously inside `agentloop.Run` |
| Optimistic-concurrency check on wiki write | API/Backend (`internal/wiki.Store`) | — | The on-disk source of truth and the existing `fileMutex(slug)` both live in `wiki.Store`; the ETag check MUST happen inside that critical section |
| Async reindex worker | API/Backend (`internal/reindex` package) | — | Pure background concern; the worker is a sidecar to the Telegram bot's lifecycle. Producers (`wiki.Store`, `internal/ingest`) call `Submit` non-blocking; consumer is the worker goroutine |
| Tool retrieval & per-turn injection | API/Backend (`internal/tools` + `internal/agentloop`) | — | All retrieval happens server-side. The per-turn `ToolsProvider` closure is invoked from inside `agentloop.Run` (loop.go:155-157) |
| `unversioned` badge rendering | Frontend (web dashboard) | API/Backend (`internal/api/wiki.go` passes the flag through) | The flag is a frontmatter field rendered as a yellow badge in `WikiPageView.tsx`. Server only carries the boolean; UX language ("Git tracking pending") is a frontend label per D-20 |
| Conflict re-read + retry decision | LLM (the model itself) | API/Backend (returns the structured conflict JSON) | The conflict response is a tool RESULT — the LLM (not server code) chooses to re-read and retry. Server's job is to surface a parseable JSON shape |

## Standard Stack

### Core (Existing — no new dependencies for the LLM/reindex/tool work)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go stdlib `context` / `sync` / `sync/atomic` | 1.26.2 | Worker context cancellation, drop-newest channel send, atomic counters for `dropped`/`stopped` | `[VERIFIED: go.mod line 3 — go 1.26.2]` Phase 1 carry-forward + REQUIREMENTS.md "Out of Scope" forbids worker-pool libraries |
| Go stdlib `log/slog` | 1.26.2 | Structured logging in the new `internal/reindex` package | `[VERIFIED: existing usage in internal/wiki/store.go line 7 import; internal/conversation/archive.go]` Matches the pattern adopted across the codebase |
| `go.uber.org/zap v1.28.0` | 1.28.0 | Used by `internal/logging` redactor; classifier `cleaned` message MUST be redacted before any zap field is set | `[VERIFIED: go.mod line 22]` Existing project convention |
| `github.com/aura/aura/internal/qdrant` | in-tree (Phase 1) | Shared Qdrant client; tool vector index already uses this | `[VERIFIED: internal/tools/registry_search_vector.go line 17 import]` Phase 1 carry-forward — already wired with WR-04 mu-released-around-HTTP semantics |
| `gopkg.in/yaml.v3 v3.0.1` | 3.0.1 | Frontmatter marshaling in `wiki.MarshalMD`; the `Unversioned` field uses `omitempty` | `[VERIFIED: go.mod line 23; internal/wiki/parser.go line 12 import]` |

### Updated (one external dependency change)

| Library | Old → New | Purpose | Why Update |
|---------|-----------|---------|------------|
| `github.com/go-git/go-git/v5` | v5.18.0 → v5.19.0 | Wiki commit operations in `wiki.Store.gitCommit` (uses `git.PlainOpen`, `Worktree.Add`, `Worktree.Commit`) | `[VERIFIED: go.mod line 15 currently pins v5.18.0]` `[CITED: github.com/go-git/go-git/releases/tag/v5.19.0]` Security/dependency refresh; no breaking changes to the three call sites used by `wiki.Store` (PlainOpen, Worktree.Add, Worktree.Commit). One-line `go.mod` change verified by `go build ./... && go test ./internal/wiki/` per D-21 |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| In-tree `internal/llm/classify.go` (Hermes-shape priority pipeline) | A 3rd-party retry/classifier library | `[VERIFIED: REQUIREMENTS.md line 49 "Out of Scope" — Worker pool libraries (ants, pond)]` Same prohibition extends to retry libraries; CLAUDE.md "FOLLOW EXISTING PATTERNS" pins us to the existing `Client` wrapper shape |
| `internal/reindex.Worker` (single goroutine + buffered chan) | `github.com/panjf2000/ants` worker pool | Banned by REQUIREMENTS.md "Out of Scope"; `internal/conversation.BufferedAppender` (archive.go:332-385) is the in-tree precedent and stays 1:1 with what Phase 2 needs |
| `WriteWikiPageTool` (in-tree) | LangChain/LangGraph tool framework | `[VERIFIED: AI-SPEC §2 — every cataloged agent framework is Python/TS/Java/.NET, no Go SDK]` Phase 2 is reliability infra, not orchestration; framework swap would violate Phase 1 carry-forward "composition-only changes around `agentruntime/agentloop`" |

**Installation (verified):**

```bash
# Single dependency change.
go get github.com/go-git/go-git/v5@v5.19.0
go mod tidy

# Verify per CLAUDE.md Post-Edit Validation.
go vet ./...
go build ./...
go test -race ./internal/wiki/
```

**Version verification (current):** `go.mod` line 15 pins `github.com/go-git/go-git/v5 v5.18.0` (verified by Read). Target `v5.19.0` release notes confirmed via WebFetch — no breaking API changes affecting `Worktree.Add`, `Worktree.Commit`, or `PlainInit`. Local toolchain: `go version go1.26.2 windows/amd64` (verified). `git version 2.51.0.windows.1` (verified). `node v22.15.0` (verified — for the dashboard build).

## Architecture Patterns

### System Architecture Diagram

```
                  Telegram inbound
                        │
                        ▼
                 internal/telegram (Bot, per-user actor + UserGate from Phase 1)
                        │
                        ▼
                 agentruntime.Run ──── per-turn ──→ ToolsProvider() ──┐
                        │                                              │  (NEW per-turn closure)
                        │                                              │
                        │                                              ▼
                        │                                   internal/tools.Registry
                        │                                              │
                        │                                              ├── core (7 always-on, hard-coded list)
                        │                                              └── ToolVectorIndex (NEW exported)
                        │                                                      │
                        │                                                      ▼ Qdrant top-K=5 search
                        │                                              ToolDefinition[] ≤ 12
                        │
                        ▼ Chat() with retrieved tools
                 ClassifyRetryClient (REWRITTEN — internal/llm/retry.go)
                        │  Send → Classify(err) → bucket-specific policy
                        │    TRANSIENT: same-temp, jittered backoff, max 5
                        │    CONTENT:   staircase 0.0→0.3→0.7, max 3, nudge appended
                        │    PERMANENT: zero retries
                        ▼
                 OpenAIClient (UNCHANGED) → provider HTTP
                        │
                        ▼ Response (may have ToolCalls)
                 agentloop.Run executes tool calls → Registry.Execute("write_wiki_page", args)
                        │
                        ▼
                 WriteWikiPageTool.Execute (NEW — internal/tools/wiki.go)
                        │  Validate args (CONTENT-bucket on schema fail)
                        │  Build Page (LLM never controls slug/unversioned/schema/created_at/updated_at)
                        ▼
                 wiki.Store.WritePage(ctx, page, expectedUpdatedAt)  (EXTENDED — file split into store_writes.go)
                        │  ┌─ Acquire fileMutex(slug)
                        │  │
                        │  │   Read on-disk Page.UpdatedAt
                        │  │   Compare with expectedUpdatedAt:
                        │  │     - "" + page absent  → create
                        │  │     - "" + page exists  → ConflictError (create-only-if-absent)
                        │  │     - match             → update
                        │  │     - mismatch          → ConflictError (D-03 JSON via tool wrapper)
                        │  │
                        │  │   Atomic temp+rename write
                        │  │   gitCommit (returns error)
                        │  │     - on err: re-read page, set Unversioned=true, atomic re-write (NO commit)
                        │  │     - on ok:  if page was Unversioned, clear and atomic re-write (NO commit)
                        │  │   submitter.Submit(reindex.Job{Slug, OpUpsert})  ← non-blocking, drop-newest
                        │  └─ Release fileMutex(slug)
                        ▼
                 reindex.Worker (NEW — internal/reindex/worker.go)
                        │  single goroutine, dedicated ctx, done chan
                        │  drains jobs ← chan Job (cap 100, drop-newest on full)
                        ▼
                 search.Engine.ReindexWikiPage(ctx, slug)
                        │  full collection rebuild today (deferred: incremental upsert)
                        │  EmbedCache makes unchanged pages effectively free
                        ▼
                 chromem-go in-memory + sqlite FTS5 fallback (Phase 4 will swap to Qdrant)

  Shutdown: bot.Shutdown() → cancel agent loop ctx → wait in-flight tools → reindexWorker.Stop()
                                                                              ↑
                                            Stop() = cancel(); <-done; (NEVER close jobs)
```

### Recommended Project Structure (post-Phase-2 deltas only)

```
internal/
├── llm/
│   ├── client.go                   (UNCHANGED — Client interface, Request, Response, Token)
│   ├── retry.go                    (REWRITTEN — ClassifyRetryClient, RetryConfig fields added)
│   ├── classify.go                 (NEW — Bucket enum, Classify, sentinel errors, redactor)
│   ├── classify_test.go            (NEW — table-driven by HTTP status + message)
│   ├── retry_test.go               (UPDATED — buckets, jitter distribution, nudge mutation)
│   ├── openai.go                   (UNCHANGED)
│   └── client_test.go              (UNCHANGED)
├── reindex/                         (NEW package)
│   ├── worker.go                   (Worker, NewWorker, Start, Stop, drain)
│   ├── types.go                    (Job, Op, Submitter, Health, Config)
│   └── worker_test.go              (NoGoroutineLeak, StopCancelsInflight, DropNewest)
├── tools/
│   ├── registry.go                 (UNCHANGED)
│   ├── registry_search_vector.go   (MODIFIED — toolVectorIndex → ToolVectorIndex; methods exported)
│   ├── registry_search.go          (UNCHANGED)
│   ├── wiki.go                     (NEW — WriteWikiPageTool: Name/Description/Parameters/Execute/Definition)
│   ├── wiki_test.go                (NEW — conflict, create-only, update, privileged-field rejection)
│   ├── tool_search.go              (DELETED)
│   ├── tool_search_test.go         (DELETED)
│   └── definition.go, args.go, registry_test.go, etc. (UNCHANGED)
├── wiki/
│   ├── schema.go                   (MODIFIED — Page.Unversioned bool with omitempty yaml tag)
│   ├── store.go                    (MODIFIED — kept under 600 LOC; reads + git plumbing only)
│   ├── store_writes.go             (NEW factor-out — WritePage, DeletePage, unversioned helpers, reindex enqueue)
│   ├── store_writes_test.go        (NEW — UnversionedRoundTrip, BackwardsCompat, ConflictETag, CreateOnly)
│   ├── parser.go                   (UNCHANGED — legacy Writer.WriteFromLLMOutput stays for ingest path)
│   └── store_test.go               (UNCHANGED — covers existing surfaces)
├── agentloop/
│   └── loop.go                     (MODIFIED — ToolsProvider invoked per-turn; loop.go lines 154-157 already shaped right)
├── telegram/
│   ├── setup.go                    (MODIFIED — wire reindex.Worker; wire ToolsProvider closure; remove tool_search)
│   ├── conversation.go             (MODIFIED — remove "tool_search" mentions in lines 259, 293, 306, 331)
│   ├── conversation_tool_exec.go   (MODIFIED — remove "tool_search" branch in line 114, remove toolNamesFromToolSearchResult)
│   └── debug_smoke.go              (MODIFIED — remove "tool_search" case in line 186)
└── api/
    └── wiki.go                     (MODIFIED — pageFrontmatter passes through unversioned bool to WikiPage JSON)

cmd/
└── debug_telegram_sandbox/
    └── main.go                     (MODIFIED — remove --expect-tool-search-calls-max flag and ToolSearchCalls counter at lines 61, 195, 319, 816)

scripts/
├── test-agent-tool-search-smoke.ps1            (DELETED — no longer applicable)
└── test-runtime-answer-discipline-smokes.ps1   (REVIEWED — remove tool_search assertions)

go.mod, go.sum                       (MODIFIED — go-git v5.18.0 → v5.19.0)
```

### Pattern 1: Composition Wrapping of `llm.Client`

**What:** The `Client` interface (`internal/llm/client.go:78`) is the contract every consumer depends on. Wrappers like `ClassifyRetryClient` implement `Client` themselves and inject classification/retry behavior without their callers knowing.

**When to use:** Every reliability concern that operates on the LLM call boundary. Phase 2 stops at retry; Phase 3 will add a circuit breaker as a sibling wrapper.

**Example (verified shape from `internal/llm/retry.go` and `internal/telegram/setup.go:800`):**

```go
// internal/llm/retry.go (rewritten — sketch from AI-SPEC §4)
type ClassifyRetryClient struct {
    inner               llm.Client
    maxTransient        int
    baseDelay           time.Duration
    maxDelay            time.Duration
    jitterRatio         float64
    contentTemperatures []float64  // [0.0, 0.3, 0.7]
    logger              *slog.Logger
}

func (r *ClassifyRetryClient) Send(ctx context.Context, req llm.Request) (llm.Response, error) {
    callerTemp := req.Temperature
    contentAttempt, transientAttempt := 0, 0
    for {
        resp, err := r.inner.Send(ctx, req)
        if err == nil { return resp, nil }
        bucket, retryable, cleaned := Classify(err)
        r.logger.Warn("llm_call_failed",
            slog.String("bucket", bucket.String()),
            slog.Bool("retryable", retryable),
            slog.String("message", cleaned)) // PRE-redacted
        switch bucket {
        case BucketPermanent:
            return llm.Response{}, err
        case BucketTransient:
            if !retryable || transientAttempt >= r.maxTransient { return llm.Response{}, err }
            req.Temperature = callerTemp // D-08
            d := jitteredBackoff(transientAttempt, r.baseDelay, r.maxDelay, r.jitterRatio)
            transientAttempt++
            select {
            case <-ctx.Done(): return llm.Response{}, ctx.Err()
            case <-time.After(d):
            }
        case BucketContent:
            if contentAttempt >= len(r.contentTemperatures) { return llm.Response{}, err }
            t := r.contentTemperatures[contentAttempt]
            req.Temperature = &t // override (D-08)
            req.Messages = appendValidationNudge(req.Messages, cleaned)
            contentAttempt++
        }
    }
}
```

### Pattern 2: Per-Slug fileMutex + In-Critical-Section ETag

**What:** Existing `wiki.Store.fileMutex(slug)` (`store.go:137-140`) returns a `*sync.Mutex` from a `sync.Map` keyed by slug. The new ETag comparison happens AFTER acquiring this mutex and BEFORE the atomic temp+rename write. Outside the critical section, a TOCTOU race with the dashboard write path is possible (Pitfall #6).

**When to use:** Always for `WritePage` when an `expectedUpdatedAt` is supplied. Existing callers that pass no ETag (variadic/zero-arg) keep "trust caller" semantics.

**Example (verified shape from `internal/wiki/store.go:160-217`):**

```go
// internal/wiki/store_writes.go (NEW — factored-out)
type ConflictError struct {
    Slug     string
    Expected string
    Actual   string
}
func (e *ConflictError) Error() string {
    return fmt.Sprintf("page %s was modified since last read (expected %s, got %s)",
        e.Slug, e.Expected, e.Actual)
}

func (s *Store) WritePage(ctx context.Context, page *Page, expectedUpdatedAt ...string) error {
    if err := Validate(page); err != nil { return fmt.Errorf("validation failed: %w", err) }
    slug := Slug(page.Title)
    mu := s.fileMutex(slug)
    mu.Lock()
    defer mu.Unlock()

    // ETag check INSIDE the critical section (Pitfall #6 prevention).
    if len(expectedUpdatedAt) > 0 {
        existing, readErr := s.readPageLocked(slug) // helper that does NOT take fileMutex
        switch {
        case expectedUpdatedAt[0] == "" && readErr == nil:
            return &ConflictError{Slug: slug, Expected: "", Actual: existing.UpdatedAt}
        case expectedUpdatedAt[0] != "" && readErr != nil:
            return fmt.Errorf("page %s not found for ETag update", slug)
        case expectedUpdatedAt[0] != "" && existing.UpdatedAt != expectedUpdatedAt[0]:
            return &ConflictError{Slug: slug, Expected: expectedUpdatedAt[0], Actual: existing.UpdatedAt}
        }
    }
    // ... atomic temp+rename, gitCommit with unversioned set/clear, reindex.Submit ...
}
```

### Pattern 3: Single-Goroutine + Buffered-Channel + Dedicated-Context Worker

**What:** A worker that owns a `chan Job` (cap 100), a dedicated `ctx`, and a `done chan struct{}`. Producers call non-blocking `Submit`; the worker drains with `select { case <-ctx.Done(): … case j := <-jobs: process(ctx, j) }`. `Stop()` cancels the ctx (which cancels in-flight Qdrant/embedding HTTP calls) and waits on `done` before returning. The channel is NEVER closed by anyone.

**When to use:** Any non-blocking write path where dropping is acceptable and the work is idempotent on re-read. Mirrors `internal/conversation.BufferedAppender` (verified at `archive.go:332-385`) but adds the dedicated-ctx + done-chan lifecycle that Pitfall #4 requires (the BufferedAppender does NOT have this — it closes the channel from the producer side, which is unsafe for the reindex worker because two producers race the close).

**Example (sketch from AI-SPEC §4b, verified against PITFALLS.md #4):**

```go
// internal/reindex/worker.go
type Worker struct {
    jobs      chan Job
    reindexer search.WikiPageReindexer
    ctx       context.Context
    cancel    context.CancelFunc
    done      chan struct{}
    droppedTotal atomic.Int64
    stopped      atomic.Bool
    logger       *slog.Logger
}
func (w *Worker) Submit(j Job) bool {
    if w.stopped.Load() {
        w.logger.Warn("reindex_dropped_after_stop", slog.String("slug", j.Slug))
        return false
    }
    select {
    case w.jobs <- j: return true
    default:
        w.droppedTotal.Add(1)
        w.logger.Warn("reindex_dropped_total", slog.String("slug", j.Slug))
        return false
    }
}
func (w *Worker) Stop() {
    w.stopped.Store(true)
    w.cancel()      // cancels in-flight ReindexWikiPage HTTP calls (Pitfall #4)
    <-w.done        // wait for drain goroutine to actually exit
    // NEVER close(w.jobs) — let GC reclaim it.
}
func (w *Worker) drain() {
    defer close(w.done)
    for {
        select {
        case <-w.ctx.Done(): return
        case j := <-w.jobs:
            if err := w.reindexer.ReindexWikiPage(w.ctx, j.Slug); err != nil {
                w.logger.Warn("reindex_failed", slog.String("slug", j.Slug), slog.Any("error", err))
            }
        }
    }
}
```

### Pattern 4: Per-Turn `ToolsProvider` Closure

**What:** `agentloop.Options.ToolsProvider func() []llm.ToolDefinition` is already a hook (loop.go:83); it is invoked per-turn at lines 155-157 (verified). Today it's only set once at registration in `internal/telegram/conversation.go:333` (`currentToolDefs`). Phase 2 swaps that closure for one that composes core (7) ∪ retrieved (top-K=5) per turn.

**When to use:** Inside `internal/telegram/setup.go` after the registry is fully populated and the Qdrant tool-vector index is built.

**Example:**

```go
// internal/telegram/setup.go (NEW — replaces the line 519-528 BuildVectorIndex block + adds the closure)
var alwaysOnCore = []string{
    "write_wiki_page", "search_memory", "list_sources", "read_source",
    "schedule_task", "request_dashboard_token",
}

agentloopOpts.ToolsProvider = func() []llm.ToolDefinition {
    coreDefs := toolRegistry.DefinitionsFor(alwaysOnCore)
    latestUserMsg := convCtx.LatestUserMessageText() // returns "" on cold start
    if strings.TrimSpace(latestUserMsg) == "" {
        return coreDefs // cold-start (D-23): core only
    }
    retrieved := toolRegistry.Search(latestUserMsg, 5, alwaysOnCore...) // exclude core from supplemental
    if retrieved == nil { // Qdrant down or index empty (D-25)
        toolRegistry.Logger().Warn("tools_provider_fallback", slog.String("reason", "qdrant_down"))
        return toolRegistry.Definitions() // FULL toolset
    }
    out := append([]llm.ToolDefinition(nil), coreDefs...)
    for _, r := range retrieved {
        out = append(out, toolRegistry.DefinitionsFor([]string{r.Name})...)
    }
    return out
}
```

### Anti-Patterns to Avoid

- **Closing the reindex channel from the producer side.** Classic Go anti-pattern; produces `send on closed channel` panic if a `Submit` is mid-`select` while shutdown closes. **Do:** worker exclusively owns the channel; producers never close it; Stop() cancels ctx and waits on done.
- **ETag check outside the per-slug fileMutex.** A race between `ReadPage` (LLM-side, observes T1) and `WritePage` (LLM-side, validates against T1) where the dashboard's `WritePage` lands in between — the LLM-side check passes but the dashboard's edit is overwritten. **Do:** call a `readPageLocked` helper INSIDE the fileMutex critical section.
- **Surfacing the conflict as a tool ERROR (text string).** The LLM cannot reliably parse error narratives. **Do:** return the conflict as a tool RESULT JSON with the structure of D-03 so downstream agent loops can recover deterministically.
- **Letting the LLM control privileged frontmatter.** `slug`, `unversioned`, `schema_version`, `prompt_version`, `created_at`, `updated_at` are server-managed. **Do:** set `additionalProperties:false` on the JSON Schema and never surface those fields in `WriteWikiPageArgs`.
- **Logging the raw classifier message.** Provider error strings carry URLs with bearer tokens, base64 image payloads, basic-auth URLs, and OpenRouter `sk-or-v1-…` tokens. **Do:** redact in `Classify`'s `cleaned` output BEFORE any zap field is set.
- **Putting all of WritePage into store.go.** Already 755 LOC; CLAUDE.md god-class rule is 600. **Do:** factor `store_writes.go` (same package, file split only).
- **Bumping `schema_version` to add `Unversioned`.** It's an additive backward-compatible field. **Do:** use `omitempty` on JSON+YAML tags; do not change `CurrentSchemaVersion = 2` in `schema.go:12`.
- **Using `context.Background()` in the worker's `process`.** Makes Stop() block for ~30s waiting for the embedding API timeout (Pitfall #4). **Do:** pass `w.ctx`.
- **Empty toolset on cold-start.** A naive `messages[len(messages)-1]` returns the system message and Qdrant matches randomly. **Do:** detect "no user turn yet" and return core (7) only.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Per-page concurrency control | A new version column + content hash + custom locking | The existing `Page.UpdatedAt` RFC3339 string + existing `fileMutex(slug)` | Already in place; strong validator (RFC3339 timestamps don't repeat in a wiki write workflow); no schema migration needed |
| Optimistic concurrency on the wire | A `version: int` field + JSON Patch protocol | Variadic `expectedUpdatedAt ...string` on `WritePage` + structured conflict JSON | D-05 explicitly preserves backward compatibility; HTTP `If-Match`/`412` semantics map cleanly per Ed-Fi guide |
| Retry with backoff | A 3rd-party retry library (cenkalti/backoff, etc.) | Hermes-style jittered backoff (`base*2^n*[1-r, 1+r]`, capped) | REQUIREMENTS.md "Out of Scope" forbids worker-pool/retry libraries; D-07 nails the constants (1s base, 30s cap, 0.5 jitter ratio) |
| Background work | `github.com/panjf2000/ants`, `github.com/alitto/pond` | Single goroutine + buffered channel + dedicated ctx | REQUIREMENTS.md "Out of Scope" line 49; `internal/conversation.BufferedAppender` is the in-tree precedent |
| Tool retrieval | A new vector backend or RAG framework | The existing `toolVectorIndex` (Qdrant) — exported as `ToolVectorIndex` | Already wired with WR-04 mu-released-around-HTTP semantics; embedding strategy `name + " " + description` matches Red Hat 2025-11-26 Tool-RAG findings |
| JSON Schema validation | `github.com/santhosh-tekuri/jsonschema`, `github.com/xeipuuv/gojsonschema` | `Tool.Parameters() map[string]any` + `Validate()` method on the args struct | The existing `Tool` interface already returns a JSON-Schema fragment to the LLM; the validation step inside `Execute` is the post-extract Pydantic-equivalent |
| Git commit | Custom shell-out to `git` | `github.com/go-git/go-git/v5` (already pinned) | Already in `go.mod` line 15; the existing `Worktree.Add` + `Worktree.Commit` shape is what `wiki.Store.gitCommit` (`store.go:351-388`) already uses |
| Goroutine lifecycle | A 3rd-party "managed goroutine" library | `context.WithCancel` + `done chan struct{}` | Pattern already established in `internal/conversation.BufferedAppender` (modulo the channel-close difference D-13 corrects) |

**Key insight:** Every reliability primitive Phase 2 needs is either already in the codebase or in the Go stdlib. The temptation to add a "small library" for retries, schema validation, or background work is the wrong move — it would violate REQUIREMENTS.md "Out of Scope" and CLAUDE.md "FOLLOW EXISTING PATTERNS" simultaneously, and the Go-native shapes are at least as readable as the library equivalents.

## Runtime State Inventory

> Phase 2 is **NOT a rename/refactor/migration phase** for runtime state. The work is additive (new files, new fields with `omitempty`, file-split factoring) plus one-line dependency upgrade (go-git v5.18.0 → v5.19.0). No string is being renamed; no stored data needs migration. Skipping this section per the template guidance ("Omit entirely for greenfield phases" — this is the closest analog: net-additive infrastructure phase).

**However**, two narrow runtime-state notes apply that the planner must bake into tasks:

| Category | Items found | Action required |
|----------|-------------|-----------------|
| Stored data | None — `Page.Unversioned` is an additive field with `omitempty`. Existing `.md` files have no `unversioned:` line and parse cleanly because YAML unmarshal silently sets the field to `false` when absent. | None — no migration needed (D-19 explicitly says "Schema version is NOT bumped"). |
| Live service config | Tool-vector Qdrant collection: existing `aura_tool_search` collection (`internal/telegram/setup.go:524`). Phase 2 narrows the embedding text from "name + description + tags + examples + parameters JSON" (`registry_search_vector.go:122-141` `searchableToolText`) to "name + description" only (D-24). | **Code-only change**, but the *vector dimensions and content* of the collection change. Need to verify Phase 2 either (a) deletes and rebuilds the collection on first boot post-upgrade (existing Build does delete+recreate at lines 189-198 when warm-cache is empty), or (b) bumps a collection-version suffix. The QDRANT-01 `points_count > 0` warm-cache check (verified `registry_search_vector.go:160-170`) WILL skip the rebuild and serve stale embeddings under the new code unless the collection is invalidated. **Planner action:** add a task to either rename the collection (e.g. `aura_tool_search_v2`) or force a one-time delete on Phase 2 boot. |
| OS-registered state | None. | None. |
| Secrets/env vars | New env vars likely needed for `ReindexConfig` (queue size, etc., Claude's discretion per CONTEXT.md). Naming convention `RUNTIME_*` per existing pattern. | **Code change only** — new env vars get default values; no operator action required. |
| Build artifacts / installed packages | `go.sum` will change with the go-git upgrade. | Run `go mod tidy` after `go get`. CI rebuilds from clean. |

## Common Pitfalls

### Pitfall 1: ETag check outside fileMutex (TOCTOU)
**What goes wrong:** LLM reads page X at T1, dashboard writes at T2, LLM calls `write_wiki_page(expected=T1)`. If the ETag compare runs OUTSIDE the per-slug fileMutex, the dashboard's T2 write can sneak in between the compare and the rename — the LLM's write succeeds and silently destroys the dashboard edit.
**Why it happens:** The natural code shape is `WritePage` does an early-return guard before taking the mutex. PITFALLS.md #6 documents this exact failure.
**How to avoid:** Acquire `s.fileMutex(slug)` FIRST (line 169-171 of current `store.go` already does this), THEN read the on-disk `updated_at`, THEN compare. The compare and the rename must be in the same critical section. Use a `readPageLocked` helper that takes only the slug (the calling site is already holding the mutex).
**Warning signs:** Run a `go test -race` test that forges a stale read by mutating disk between `ReadPage` and `WritePage` — the test should reject the write with a `ConflictError`.

### Pitfall 2: Reindex worker goroutine leak on shutdown
**What goes wrong:** Worker goroutine sits in `for { select { case <-ctx.Done(): return; case j := <-jobs: process(ctx, j) } }`. If `Stop()` only closes `jobs` without cancelling `ctx`, an in-flight `ReindexWikiPage` (Qdrant call + embedding API call, each ~30s timeout) keeps the goroutine alive for up to 30s per pending job. Across N restarts, `runtime.NumGoroutine()` grows.
**Why it happens:** Channel closure does not cancel in-flight HTTP I/O. PITFALLS.md #4 documents this.
**How to avoid:** Worker owns a dedicated `ctx, cancel := context.WithCancel(parent)`. `Stop()` calls `cancel()` first (cancels in-flight HTTP), then waits on `<-done`. NEVER close the jobs channel from outside the worker — let GC reclaim it after the last submitter releases its reference.
**Warning signs:** `TestWorker_NoGoroutineLeak` snapshots `runtime.NumGoroutine()` pre-`Start`/post-`Stop` over 100 cycles; delta must be 0.

### Pitfall 3: Temperature staircase burned on infrastructure failures
**What goes wrong:** Current `RetryClient` retries blindly on any error. If retries escalate temperature (0 → 0.3 → 0.7) on HTTP 429 or context timeout, every concurrent user pays 3× the LLM cost during a provider blip and still fails. Wiki writes intended at `temperature=0` come back at `0.7` — frontmatter formatting is structurally noisier than intended.
**Why it happens:** The retry strategy was designed for "model refused to produce structured output" (a content problem) but gets applied to ALL failures. PITFALLS.md #7 documents this.
**How to avoid:** Classify FIRST (`Bucket`), then apply bucket-specific policy. CONTENT escalates temperature; TRANSIENT preserves the caller's `Request.Temperature`; PERMANENT exits immediately with zero retries (D-06..D-08).
**Warning signs:** A test with a mock that returns HTTP 429 should show ≤5 retries at the SAME temperature, not incrementing. Logs should never show `bucket=transient` paired with a `temperature=` change.

### Pitfall 4: Send on closed channel panic during shutdown
**What goes wrong:** Producer (`wiki.Store`) holds a stale `Submit` reference and is mid-`select { case w.jobs <- j: default: drop() }` while a shutdown goroutine calls `close(w.jobs)`. Result: `send on closed channel` panic that crashes the entire bot.
**Why it happens:** Classic Go anti-pattern — multiple producers + uncoordinated close.
**How to avoid:** Worker exclusively owns the channel. Producers never close. Worker exits on `ctx.Done()` and the channel is GC-reclaimed when the last submitter releases its reference. Pair this with an `atomic.Bool stopped` flag checked at the start of `Submit` so post-Stop sends can be logged distinctly without race.
**Warning signs:** `go test -race` on the producer/shutdown interleaving should be panic-free.

### Pitfall 5: Classifier `cleaned` message leaking secrets
**What goes wrong:** `Classify` inspects the raw provider error string. If the wrapper surfaces that raw string into a zap log unredacted, URLs with bearer tokens, base64 image payloads, `Authorization:` headers, OpenRouter `sk-or-v1-…` API keys, AWS pre-signed URL signatures, and JWTs end up in `data/aura.log`.
**Why it happens:** AI-SPEC §1b row 8 + failure mode #7 explicitly flag the redactor's known gaps; the natural code shape is to log the raw error and add redaction "later".
**How to avoid:** `Classify(error) (Bucket, bool, string)` returns a `cleaned` string with all secret patterns stripped BEFORE any zap field is set. Pattern match on the raw string in-memory; only the cleaned variant escapes. Test the redactor against a panel of seeded-bad inputs (URL token, Bearer, basic-auth URL, base64 ≥ threshold, Authorization header, OpenRouter `sk-or-v1-…`, JWT). Treat any panel hit as a CI-fail.
**Warning signs:** `TestRedactor_NoSecretLeak` runs each seeded-bad input through `Classify` and asserts the regex panel finds zero matches in the returned `cleaned`.

### Pitfall 6: Cold-start tool retrieval returns empty toolset
**What goes wrong:** Retrieval query is "the latest user message", but on the first turn after `/start` the conversation has only a system message. `messages[len(messages)-1]` returns the system prompt, Qdrant matches randomly or returns nothing, and the agent loop's `Tools` slice is empty for the entire first user turn. The bot replies "I can't do that right now" and the user concludes Aura is broken.
**Why it happens:** AI-SPEC §1b failure mode #5 documents this.
**How to avoid:** `ToolsProvider` closure detects "no user message yet" (latest user-role message is "" or absent) and returns the always-on core (7) only. Same fallback as Qdrant-down — never an empty toolset.
**Warning signs:** `TestToolsProvider_ColdStart` constructs a `Messages` slice with only the system entry, asserts `len(provider()) == 7` and the exact name set matches `alwaysOnCore`.

### Pitfall 7: `internal/wiki/store.go` god-class growth
**What goes wrong:** File is 755 LOC today (verified `wc -l`); CLAUDE.md hard rule is 600. Adding `expected_updated_at` checking + `Unversioned` set/clear + reindex submission glue is on track to push past 800 if the developer follows the path of least resistance. PR ships "working" but in CLAUDE.md violation.
**Why it happens:** Easiest place to add `WritePage` extensions is right next to `WritePage`.
**How to avoid:** Factor `internal/wiki/store_writes.go` (same package, no API change) holding `WritePage`, `DeletePage`, `ConflictError`, `unversioned` helpers, and the reindex enqueue. Keep `store.go` as construction + reads + git plumbing + index/log helpers. Target: `store.go ≤ 600 LOC` AND `store_writes.go ≤ 600 LOC`.
**Warning signs:** A `TestGodClass` in CI reads file sizes from disk and fails if any source file exceeds 600 LOC.

### Pitfall 8: `select/default` Submit silently drops after Stop()
**What goes wrong:** `select { case ch <- v: default: drop() }` is correct ONLY when the worker is alive and is the sole receiver. After Stop() the worker has exited; every Submit hits `default` and "drops" — but those drops are post-shutdown, operationally distinct from queue-full drops. Without a `Stopped()` atomic flag plus a separate `reindex_dropped_after_stop` counter, a shutdown-time backlog is invisible.
**Why it happens:** Phase 2 D-13 plus AI-SPEC failure mode #8.
**How to avoid:** `Worker.stopped atomic.Bool`. `Submit` checks `stopped.Load()` first; if true, log `reindex_dropped_after_stop` (distinct counter) and return false. After Stop() returns, no further sends will ever land in the channel — but the counter shape gives operators a clear signal.

### Pitfall 9: ETag mismatch type collision when caller passes empty `expected` accidentally
**What goes wrong:** Existing callers (`internal/ingest/pipeline.go:155`, `internal/wiki/parser.go:50,117`, `internal/wiki/store.go:735` RepairLink) call `WritePage(ctx, page)` with no third argument. With variadic `expectedUpdatedAt ...string`, this is `expected = []string{}` — distinguishable from `expected = []string{""}`. But if a future caller accidentally writes `WritePage(ctx, page, "")`, that's the create-only sentinel and will reject every update.
**Why it happens:** The variadic shape blurs "no opinion" vs "create-only sentinel".
**How to avoid:** In `WritePage`, treat `len(expectedUpdatedAt) == 0` as "trust caller" (existing semantics, no ETag check). Treat `len(expectedUpdatedAt) >= 1` as opt-in to ETag semantics with `expected[0]` as the value. Document this clearly in the function comment. The `WriteWikiPageTool.Execute` always passes exactly one string (possibly empty).
**Warning signs:** `TestWritePage_BackwardsCompat` asserts that `WritePage(ctx, page)` (no variadic) succeeds on an existing slug (update path, not create-only-rejection).

## Code Examples

### Common Operation 1: Classify an LLM error

```go
// internal/llm/classify.go (sketch — Hermes-style priority pipeline)
//
// Source: D:\Aura\.planning\phases\02-llm-reliability-tool-intelligence\02-AI-SPEC.md §3
// Source: D:\tmp\hermes-agent\agent\error_classifier.py (Python prior art)

type Bucket int
const (
    BucketTransient Bucket = iota
    BucketContent
    BucketPermanent
)
func (b Bucket) String() string {
    return [...]string{"transient", "content", "permanent"}[b]
}

var (
    ErrSchemaValidation = errors.New("schema validation failed")
    ErrEmptyOutput      = errors.New("empty assistant output")
    ErrMalformedToolCall = errors.New("malformed tool call arguments")
)

// Classify returns (bucket, retryable, cleaned).
// Priority: HTTP status code → error sentinel → message-pattern match.
func Classify(err error) (Bucket, bool, string) {
    if err == nil { return BucketPermanent, false, "" }

    cleaned := redact(err.Error())

    // 1. context-shape errors first.
    if errors.Is(err, context.Canceled) { return BucketPermanent, false, cleaned }
    if errors.Is(err, context.DeadlineExceeded) { return BucketTransient, true, cleaned }

    // 2. local sentinels (set by tool execute path on schema fail).
    if errors.Is(err, ErrSchemaValidation) { return BucketContent, true, cleaned }
    if errors.Is(err, ErrEmptyOutput)      { return BucketContent, true, cleaned }
    if errors.Is(err, ErrMalformedToolCall){ return BucketContent, true, cleaned }

    // 3. HTTP status (extracted by inner OpenAIClient — see openai.go for the StatusCode).
    var apiErr *APIError
    if errors.As(err, &apiErr) {
        switch {
        case apiErr.StatusCode == 429: return BucketTransient, true, cleaned
        case apiErr.StatusCode >= 500: return BucketTransient, true, cleaned
        case apiErr.StatusCode == 401, apiErr.StatusCode == 403: return BucketPermanent, false, cleaned
        case apiErr.StatusCode == 400: return BucketPermanent, false, cleaned // model_not_found et al
        }
    }

    // 4. transport / network errors.
    var netErr net.Error
    if errors.As(err, &netErr) && netErr.Timeout() { return BucketTransient, true, cleaned }

    // 5. message-pattern fallback (OpenAI / OpenAI-compat).
    lower := strings.ToLower(cleaned)
    switch {
    case strings.Contains(lower, "rate limit"):     return BucketTransient, true, cleaned
    case strings.Contains(lower, "overloaded"):     return BucketTransient, true, cleaned
    case strings.Contains(lower, "quota"):          return BucketPermanent, false, cleaned
    case strings.Contains(lower, "model not found"): return BucketPermanent, false, cleaned
    }

    // Default: treat unknowns as transient (retry once is cheaper than burning a turn).
    return BucketTransient, true, cleaned
}

// redact strips secrets BEFORE the cleaned message escapes Classify (D-09 + CLAUDE.md no-secrets-in-logs).
func redact(s string) string {
    s = redactURLToken.ReplaceAllString(s, "$1=***REDACTED***")
    s = redactBearer.ReplaceAllString(s, "Bearer ***REDACTED***")
    s = redactBasicAuthURL.ReplaceAllString(s, "$1://***REDACTED***@$3")
    s = redactBase64Long.ReplaceAllString(s, "***REDACTED-BASE64***")
    s = redactAuthHeader.ReplaceAllString(s, "Authorization: ***REDACTED***")
    s = redactOpenRouterKey.ReplaceAllString(s, "***REDACTED-API-KEY***")  // sk-or-v1-...
    s = redactJWT.ReplaceAllString(s, "***REDACTED-JWT***")
    return s
}
```

### Common Operation 2: WriteWikiPageTool.Execute returning structured conflict

```go
// internal/tools/wiki.go (sketch verified against AI-SPEC §4 Tool Use)

type WriteWikiPageTool struct {
    store     wiki.PageReadWriter // narrow interface satisfied by *wiki.Store (D-29)
    submitter reindex.Submitter   // optional; nil = skip enqueue
}

func (t *WriteWikiPageTool) Execute(ctx context.Context, args map[string]any) (string, error) {
    var a WriteWikiPageArgs
    a.Title    = stringArg(args, "title")
    a.Body     = stringArg(args, "body")
    a.ExpectedUpdatedAt = stringArg(args, "expected_updated_at")
    a.Category = stringArg(args, "category")
    a.Tags     = stringSliceArg(args, "tags")
    a.Related  = stringSliceArg(args, "related")
    a.Sources  = stringSliceArg(args, "sources")

    if err := a.Validate(); err != nil {
        // Wraps with llm.ErrSchemaValidation so the retry wrapper buckets as CONTENT.
        return "", err
    }

    now := time.Now().UTC().Format(time.RFC3339)
    page := &wiki.Page{
        Title:         a.Title,
        Body:          a.Body,
        Category:      a.Category,
        Tags:          a.Tags,
        Related:       a.Related,
        Sources:       a.Sources,
        SchemaVersion: wiki.CurrentSchemaVersion,
        PromptVersion: "v1",   // tool-write provenance
        CreatedAt:     now,    // overridden inside store.WritePage if page exists
        UpdatedAt:     now,
    }

    // Exactly one variadic argument — distinguishes opt-in ETag from "trust caller".
    if err := t.store.WritePage(ctx, page, a.ExpectedUpdatedAt); err != nil {
        var conflict *wiki.ConflictError
        if errors.As(err, &conflict) {
            // Tool RESULT (not tool ERROR) — LLM parses deterministically (D-03).
            payload, _ := json.Marshal(map[string]string{
                "error":               "conflict",
                "slug":                conflict.Slug,
                "expected_updated_at": conflict.Expected,
                "actual_updated_at":   conflict.Actual,
            })
            return string(payload), nil
        }
        return "", err // schema/IO/other → retry wrapper buckets normally
    }

    if t.submitter != nil {
        _ = t.submitter.Submit(reindex.Job{Slug: wiki.Slug(a.Title), Op: reindex.OpUpsert})
    }
    return fmt.Sprintf(`{"status":"ok","slug":%q,"updated_at":%q}`,
        wiki.Slug(a.Title), page.UpdatedAt), nil
}
```

### Common Operation 3: Submit + drain pattern with `select/default`

See "Pattern 3" above — verified against `internal/conversation/archive.go:332-385` with the lifecycle correction (dedicated ctx, never close jobs from outside).

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Heuristic text-parsing of LLM output looking for YAML frontmatter (`internal/wiki/parser.go` Writer.WriteFromLLMOutput) | Explicit `write_wiki_page` tool with strict JSON Schema | Phase 2 (this phase) | LLM can no longer accidentally trigger a wiki write through prose; tool calls are unambiguous and auditable |
| Synchronous reindex inside `WritePage` / `internal/ingest/pipeline.go:180` | Async slug-only worker with drop-newest coalescing | Phase 2 | User-facing tool latency stops including embedding-API roundtrips; Qdrant lag is observable via `Worker.Health()` |
| Fixed-temperature blind retry (`internal/llm/retry.go` v0) | Classify-first 3-bucket policy | Phase 2 | Token cost during provider blips drops from 4× to 1×; wiki writes stay deterministic at temperature 0 unless content fails |
| LLM calls `tool_search` to discover supplemental tools | Automatic top-K=5 Qdrant injection per turn | Phase 2 | Removes one tool-call round-trip per discovery; `tool_search.go` deletion is verified by D-27 cleanup site list |
| `gitCommit` errors logged but not surfaced | `Page.Unversioned` flag with auto-clear and dashboard badge | Phase 2 | Operators can see at a glance when git tracking has fallen behind disk |
| `wiki.Store.WritePage(ctx, page)` (trust caller) | `wiki.Store.WritePage(ctx, page, expectedUpdatedAt ...string)` (opt-in optimistic concurrency) | Phase 2 | Existing callers unchanged (D-05); new LLM-callable tool gets safe writes |
| Tool retrieval embedding text was `name + description + tags + examples + parameters JSON` (`registry_search_vector.go:122-141`) | Single-vector `name + " " + description` (D-24) | Phase 2 | Smaller index, faster queries, matches Red Hat Tool-RAG findings |

**Deprecated/outdated:**
- `internal/tools/tool_search.go` and `internal/tools/tool_search_test.go` — deleted in Phase 2 (D-31).
- `looksLikeWikiYAML`-style text heuristics — none exist in the current codebase (verified via Grep — no matches across `internal/`). The closest analog is `Writer.WriteFromLLMOutput` in `internal/wiki/parser.go:40-126`; Phase 2 does NOT delete this file because it remains used by the ingest pipeline path (`internal/ingest`), which is out of scope for this phase per D-05 deferred-callers exemption. The "no heuristic remains in the codebase" success criterion in ROADMAP.md should be interpreted as: no heuristic detection on the *agent-loop* path — the LLM only writes wiki pages via the explicit tool. The ingest pipeline keeps its existing trusted path.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | go-git v5.19.0 has no breaking API changes affecting `Worktree.Add`, `Worktree.Commit`, `PlainOpen`, `PlainInit` | Standard Stack: Updated | LOW — `[CITED: github.com/go-git/go-git/releases/tag/v5.19.0]` confirms via release notes that the v5.18.0→v5.19.0 changes are dependency bumps + encoding alignment; no API surface change. Verify locally with `go build ./... && go test ./internal/wiki/` after `go get`. |
| A2 | `convCtx.LatestUserMessageText()` exists or can be added to `*conversation.Context` to support cold-start detection | Pattern 4: ToolsProvider closure | LOW-MEDIUM — `[ASSUMED]` not verified by direct read. The `ToolsProvider` closure needs *some* function that returns the latest user message. Today, `internal/agentloop/loop.go:155-157` calls `opts.ToolsProvider()` without arguments — the closure must capture state. The planner should grep `internal/conversation/context.go` for an existing accessor; if missing, add a small helper. |
| A3 | Existing `Registry.Search` (registry_search.go) signature is compatible with the per-turn injection pattern (returns `[]ToolSearchResult` with `Name` field) | Pattern 4 | HIGH confidence — verified via Read of `registry_search.go:19-103`. `Registry.Search(query string, limit int, excluded ...string) []ToolSearchResult` returns a sorted, deduplicated list with score-merged lex+vector results. The `excluded` variadic is exactly what the closure needs to skip the always-on core. |
| A4 | The existing `ToolVectorIndex` (lowercase `toolVectorIndex`) can be renamed to `ToolVectorIndex` (export) without breaking dashboard /api/health which already exposes `ToolVectorHealth` (already exported) | D-24, registry_search_vector.go | HIGH confidence — verified `Registry.ToolVectorHealth() ToolVectorHealth` (registry.go:259) and `ToolVectorHealth` struct (registry_search_vector.go:33-40) are already exported. The rename is internal-only — no external consumer reads `toolVectorIndex` directly. |
| A5 | Embed-cache currently makes `Engine.IndexWikiPages` "cheap" enough for per-page reindex to be acceptable in Phase 2 | D-15 | MEDIUM — `[ASSUMED]` based on AI-SPEC §4 cost-and-latency-budget claim. The actual cost is bounded by `EmbedCache` SHA-keyed lookup, and only changed pages re-embed. For 50 pages with one changed, cost ≈ 1 embedding call + 50 cache lookups per reindex job. Dropping repeat jobs (drop-newest at queue cap 100) is the safety net. The deferred "incremental upsert" optimization is the right next step but not blocking Phase 2. |
| A6 | Tool retrieval precision@5 ≥ 0.80 is achievable on a 15-example fixture using the existing Qdrant setup with embedding text narrowed to `name + " " + description` | Validation Architecture, AI-SPEC §5 dimension 7 | MEDIUM — `[CITED: Red Hat 2025-11-26 https://next.redhat.com/2025/11/26/tool-rag-the-next-breakthrough-in-scalable-ai-agents/ and arXiv 2511.01854]` claim ≥ 0.80 precision@5 with name+description embeddings. Calibrate against the actual fixture before locking the threshold. |

If this table is empty, all claims were verified or cited — the table is non-empty here, so the planner should treat A2 (cold-start latest-user accessor) as the highest-priority assumption to verify before locking the per-turn injection task.

## Open Questions (RESOLVED)

1. **Where exactly should `LatestUserMessageText()` live?**
   - What we know: the per-turn `ToolsProvider` closure (D-22, D-23) needs a function that returns the latest user message string. `agentloop.Run` (loop.go:155-157) invokes `opts.ToolsProvider()` with no arguments — the closure must capture state.
   - What's unclear: `internal/conversation/context.go` likely already has a `Messages()` accessor (verified — `State.Messages() []llm.Message` is the agentloop-side interface at loop.go:36-37); the right helper is probably a one-liner that walks backwards over `State.Messages()` and returns the first `Role: "user"` content. May already exist.
   - RESOLVED: planner adds a "verify or add `LatestUserMessageText()` on `*conversation.Context`" task before the ToolsProvider closure task. Same task can also walk the existing setup.go to find where the State is constructed and whether the closure can capture a function reference.

2. **How does the tool-vector index handle the embedding-text shape change between Phase 1 (full text) and Phase 2 (name+desc only)?**
   - What we know: existing `searchableToolText` (registry_search_vector.go:122-141) embeds `name + description + tags + examples JSON + parameters JSON`. Phase 2 D-24 narrows to `name + " " + description` only.
   - What's unclear: vector dimension is the same (embedding model unchanged), so warm-cache short-circuit (`points_count > 0` at line 166) WILL skip rebuild and serve stale embeddings unless the collection is invalidated.
   - RESOLVED: planner adds a task to either (a) bump the collection name to `aura_tool_search_v2` (clean cutover, recoverable from Phase 1 state) or (b) skip the warm-cache check on Phase 2 first boot (e.g., a `force_rebuild` config flag). Option (a) is safer because it's idempotent across restarts.

3. **Does `NewWriter` (`internal/wiki/parser.go:29`) still need to stay around after Phase 2?**
   - What we know: the legacy `Writer.WriteFromLLMOutput` retry path (parser.go:40-126) is the closest thing to a "heuristic text-parsing" of LLM output for wiki writes. ROADMAP.md success criterion 1 says "no heuristic text-parsing remains in the codebase".
   - What's unclear: D-05 explicitly preserves "trust caller" semantics for `internal/ingest/pipeline.go:155` (`p.wiki.WritePage(ctx, page)`) — but the pipeline does NOT use `Writer.WriteFromLLMOutput`; it builds the Page directly. Grep confirms `WriteFromLLMOutput` is referenced only by tests in `internal/wiki/wiki_test.go`.
   - RESOLVED: planner verifies via Grep that `Writer.WriteFromLLMOutput` has no production callers (only tests). If true, mark `parser.go` Writer struct + WriteFromLLMOutput function as DELETED in Phase 2, satisfying success criterion 1 cleanly. If `Writer` has any production caller, the deletion is deferred and the success criterion is interpreted as "agent loop path no longer uses it".

4. **What is the right `ReindexConfig` env var name?**
   - What we know: CONTEXT.md "Claude's Discretion" leaves env var naming up to Claude; the existing pattern is `RUNTIME_*`.
   - What's unclear: whether to follow `REINDEX_QUEUE_SIZE`, `RUNTIME_REINDEX_QUEUE_SIZE`, or some other shape.
   - RESOLVED: planner's call. The simplest is `REINDEX_QUEUE_SIZE int` defaulting to 100 (matches D-12). A future setting for `REINDEX_DROP_THRESHOLD` could surface in dashboard runtime settings.

5. **How should the `unversioned` flag round-trip through the conversion to/from JSON in the dashboard API?**
   - What we know: D-20 says `/api/wiki/{slug}` includes `unversioned` field passthrough; existing `pageFrontmatter` (api/wiki.go:116-137) already builds the `fm` map and adds optional fields with `if x != "" { ... }` guards.
   - What's unclear: whether to always include `unversioned` (true or false) or only when `true`. The omitempty YAML behavior on disk is "absent when false", so the API can mirror that. But the dashboard needs a stable shape — `unversioned: false` with default omitted is friendlier to TS strict typing.
   - RESOLVED: include `unversioned bool` always in the API response (even when false). YAML stays omitempty on disk. TS type in `web/src/types/api.ts` adds `unversioned?: boolean` (optional only because old clients may not send it during a deploy window).

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | Building everything | ✓ | 1.26.2 windows/amd64 | — |
| git CLI | go-git tests sometimes shell out for fixtures; dev workflow | ✓ | 2.51.0 windows | — |
| Node.js | Web dashboard build (`npm --prefix web run build`) | ✓ | v22.15.0 | — |
| Qdrant | Tool vector index, search engine, compact memory | Phase 1 carry-forward — assumed available via docker-compose sidecar | — | Tool retrieval falls back to FULL toolset (D-25); search degrades to chromem-go primary + sqlite FTS5 |
| Embedding API endpoint | Tool index + page reindex | Configured via `EMBEDDING_BASE_URL` / `EMBEDDING_API_KEY`; no fallback | — | Reindex worker logs `reindex_failed` and proceeds (next write to same slug retries) |
| OpenAI-compatible chat LLM endpoint | Agent loop | Configured via `LLM_BASE_URL` / `LLM_API_KEY` / `LLM_MODEL`; no fallback | — | Classify-then-retry surfaces TRANSIENT to caller after 5 retries |

**Missing dependencies with no fallback:** None — all core dependencies for Phase 2 are present in the codebase (Go stdlib, existing pinned dependencies). The one updated dependency (go-git v5.19.0) is a registry pull verified compatible.

**Missing dependencies with fallback:** Qdrant and the embedding API both have explicit fallback paths (D-25, reindex retry-on-next-write). Phase 2 inherits these from Phase 1 plumbing.

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `go test -race`, ≥ Go 1.26.2 |
| Config file | none — `go test` discovers `*_test.go` files in each package |
| Quick run command | `go test -race ./internal/llm/ ./internal/reindex/ ./internal/tools/ ./internal/wiki/` |
| Full suite command | `go vet ./... && go build ./... && go test -race ./...` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|--------------|
| WIKI-01 | LLM-authored wiki writes flow exclusively through `write_wiki_page` tool with JSON Schema validation | unit | `go test -race ./internal/tools/ -run TestWriteWikiPage_HappyPath` | ❌ Wave 0 |
| WIKI-01 | Privileged frontmatter fields (`slug`, `unversioned`, `schema_version`, …) rejected by `additionalProperties:false` | unit | `go test -race ./internal/tools/ -run TestWriteWikiPage_PrivilegedFieldRejection` | ❌ Wave 0 |
| WIKI-01 | LLM args missing `expected_updated_at` rejected as schema validation failure (CONTENT bucket) | unit | `go test -race ./internal/tools/ -run TestWriteWikiPage_RequiredArg` | ❌ Wave 0 |
| WIKI-02 | ETag mismatch returns structured conflict JSON inside per-slug fileMutex | integration (race) | `go test -race ./internal/tools/ -run TestWriteWikiPage_Conflict` and `go test -race ./internal/wiki/ -run TestWritePage_ETagInsideMutex` | ❌ Wave 0 |
| WIKI-02 | Empty-string sentinel (`expected_updated_at=""`) rejects existing slug (create-only) | unit | `go test -race ./internal/tools/ -run TestWriteWikiPage_CreateOnly` | ❌ Wave 0 |
| WIKI-02 | Backwards compat: existing `WritePage(ctx, page)` callers without variadic continue to work | unit | `go test ./internal/wiki/ -run TestWritePage_BackwardsCompat` | ❌ Wave 0 |
| LLM-01 | CONTENT-bucket retries with temperatures `[0.0, 0.3, 0.7]` in order, max 3, no backoff | unit | `go test ./internal/llm/ -run TestRetryBudget_Content` | ❌ Wave 0 |
| LLM-01 | Validation-error nudge appended to `req.Messages` between attempts | unit | `go test ./internal/llm/ -run TestRetry_NudgeAppended` | ❌ Wave 0 |
| LLM-02 | Bucket classification accuracy on 8 fixture cases (429, 503, schema-fail, empty, malformed, 401, model-not-found, ctx-cancel) | unit (table-driven) | `go test ./internal/llm/ -run TestClassify` | ❌ Wave 0 |
| LLM-02 | TRANSIENT-bucket preserves caller `Request.Temperature` across retries | unit | `go test ./internal/llm/ -run TestRetryBudget_Transient` | ❌ Wave 0 |
| LLM-02 | Jittered backoff distribution (1000 samples, mean within ±15%, max ≤ cap) | unit | `go test ./internal/llm/ -run TestJitterDistribution` | ❌ Wave 0 |
| LLM-02 | Secret-leak redactor panel (URL token, Bearer, basic-auth URL, base64 ≥ threshold, Authorization, OpenRouter `sk-or-v1-…`, JWT) | unit | `go test ./internal/llm/ -run TestRedactor_NoSecretLeak` | ❌ Wave 0 |
| INDEX-01 | Worker `runtime.NumGoroutine()` baseline returns to pre-Start over 100 cycles | integration (race) | `go test -race ./internal/reindex/ -run TestWorker_NoGoroutineLeak` | ❌ Wave 0 |
| INDEX-01 | `Stop()` cancels in-flight reindex within 100ms (does not block ~30s) | integration (race) | `go test -race ./internal/reindex/ -run TestWorker_StopCancelsInflight` | ❌ Wave 0 |
| INDEX-01 | Drop-newest on queue full increments `dropped_total` counter | unit | `go test -race ./internal/reindex/ -run TestWorker_DropNewest` | ❌ Wave 0 |
| INDEX-01 | Post-Stop submit increments `dropped_after_stop` counter (distinct from queue-full) | unit | `go test -race ./internal/reindex/ -run TestWorker_DropAfterStop` | ❌ Wave 0 |
| GIT-01 | Commit failure → `Unversioned=true` set via re-read + atomic re-write (no commit on the metadata write) | integration | `go test ./internal/wiki/ -run TestUnversionedRoundTrip_SetOnFailure` | ❌ Wave 0 |
| GIT-01 | Next successful commit → `Unversioned` cleared via re-read + atomic re-write (no recursive commit) | integration | `go test ./internal/wiki/ -run TestUnversionedRoundTrip_ClearOnNextSuccess` | ❌ Wave 0 |
| GIT-01 | `Page.Unversioned` survives YAML round-trip (omitempty when false, present when true) | unit | `go test ./internal/wiki/ -run TestSchema_UnversionedRoundTrip` | ❌ Wave 0 |
| GIT-01 | Dashboard `/api/wiki/<slug>` JSON includes `unversioned` field | integration | `go test ./internal/api/ -run TestWikiPage_UnversionedJSON` | ❌ Wave 0 |
| TOOL-01 | Precision@5 ≥ 0.80 over 15-example hand-labeled fixture against in-process Qdrant test collection | integration (semantic) | `go test ./internal/tools/ -run TestToolRetrieval_Precision -tags=integration` | ❌ Wave 0 (requires `internal/tools/testdata/retrieval_fixture.jsonl`) |
| TOOL-02 | Cold-start (no user message) returns exactly the 7-tool core | unit | `go test ./internal/tools/ -run TestToolsProvider_ColdStart` (or `internal/telegram/` if closure lives there) | ❌ Wave 0 |
| TOOL-02 | Qdrant down → fallback FULL toolset + WARN log; never empty | unit | `go test ./internal/tools/ -run TestToolsProvider_QdrantDown` | ❌ Wave 0 |
| TOOL-02 | `tool_search.go` and `tool_search_test.go` deleted; codebase has zero `tool_search` references in production paths | static | `! grep -RIn 'tool_search\|ToolSearchTool\|tierSearch' internal/ cmd/ | grep -v '_test.go'` (CI gate) | ❌ Wave 0 |
| GOD-CLASS | `internal/wiki/store.go` ≤ 600 LOC; `store_writes.go` ≤ 600 LOC | static | `go test ./internal/wiki/ -run TestGodClass` reading `wc -l` from disk | ❌ Wave 0 |
| BUILD | `go build ./...` and `go vet ./...` clean after go-git v5.19.0 upgrade | smoke | `go vet ./... && go build ./...` | ✅ existing CI |

### Sampling Rate
- **Per task commit:** `go vet ./... && go build ./... && go test -race ./internal/{llm,reindex,tools,wiki}/` (per CLAUDE.md Post-Edit Validation; ~30s on this machine)
- **Per wave merge:** `go test -race -count=1 ./...` (full suite; ~3-5 min)
- **Phase gate:** Full suite green + dashboard build + manual smoke (`docker compose up -d --build`) before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `internal/llm/classify.go` — sentinel errors, Bucket, Classify, redactor
- [ ] `internal/llm/classify_test.go` — table-driven `TestClassify` over 8 fixture cases
- [ ] `internal/llm/testdata/redactor_panel.go` — table-driven `TestRedactor_NoSecretLeak` panel (7 secret patterns)
- [ ] `internal/llm/retry_test.go` updates — `TestRetryBudget_Content`, `TestRetryBudget_Transient`, `TestJitterDistribution`, `TestRetry_NudgeAppended`
- [ ] `internal/reindex/worker.go`, `types.go` — Worker, Job, Op, Submitter, Health, Config
- [ ] `internal/reindex/worker_test.go` — `TestWorker_NoGoroutineLeak`, `TestWorker_StopCancelsInflight`, `TestWorker_DropNewest`, `TestWorker_DropAfterStop`
- [ ] `internal/tools/wiki.go` — WriteWikiPageTool with Definition() examples
- [ ] `internal/tools/wiki_test.go` — `TestWriteWikiPage_HappyPath`, `_Conflict`, `_CreateOnly`, `_PrivilegedFieldRejection`, `_RequiredArg`
- [ ] `internal/tools/testdata/retrieval_fixture.jsonl` — 15-line hand-labeled `{prompt, expected_tool}`
- [ ] `internal/tools/provider_test.go` (or in setup_test.go) — `TestToolsProvider_ColdStart`, `_QdrantDown`, `_NormalRetrieval`
- [ ] `internal/wiki/store_writes.go` — factored-out `WritePage`, `DeletePage`, `ConflictError`, unversioned helpers
- [ ] `internal/wiki/store_writes_test.go` — `TestWritePage_ETagInsideMutex`, `TestWritePage_BackwardsCompat`, `TestUnversionedRoundTrip_*`
- [ ] `internal/wiki/schema.go` — add `Unversioned bool \`yaml:"unversioned,omitempty" json:"unversioned,omitempty"\``
- [ ] `internal/wiki/schema_test.go` updates — `TestSchema_UnversionedRoundTrip`
- [ ] `internal/api/wiki.go` — pass `unversioned` through `pageFrontmatter` (extending the existing fm map)
- [ ] `web/src/types/api.ts` — add `unversioned?: boolean` to the WikiPage type
- [ ] `web/src/components/WikiPageView.tsx` — render the yellow "Git tracking pending" badge when `unversioned` is true
- [ ] CI gate test or script for the god-class check (`internal/wiki/wiki_test.go` `TestGodClass` reading file sizes)

*(Existing test infrastructure: `go test` discovery is already wired; race detector is in CLAUDE.md; CI runs `go test -race` on `internal/{api,auth,mcp,skills}` per CLAUDE.md and Phase 2 extends this to `{llm,reindex,tools,wiki}`.)*

## Project Constraints (from CLAUDE.md)

The planner MUST verify every task respects these directives. They have the same authority as locked decisions in CONTEXT.md.

### Behavioral

- **NEVER SUPPOSE** — if uncertain, STOP and ASK. The Writer/parser deletion question (Open Question #3) is exactly this kind of decision; ask before deleting.
- **READ BEFORE EDIT** — re-read each file before modifying if it's been > 5 messages since the last read.
- **3-STRIKE RULE** — do not retry the same failing approach more than 3 times.
- **NEVER MODIFY TESTS TO MAKE THEM PASS** — fix the code, not the tests.
- **SCOPE CONTROL** — do exactly what was asked. Phase 2 is precisely scoped by D-01..D-32; do not add unrequested refactors (e.g., do not migrate ingest pipeline ETag in this phase — D-05 defers that).
- **FOLLOW EXISTING PATTERNS** — `internal/conversation.BufferedAppender`, `internal/llm.RetryClient` constructor signature, `internal/tools.Tool` interface shape, and `internal/wiki.PageReader/PageWriter` interfaces are the patterns to mirror.
- **INFORMATION TRUST HIERARCHY** — existing codebase > docs > web > training. The verified surfaces (`internal/llm/client.go`, `wiki/store.go:160`, `tools/registry_search_vector.go`, `agentloop/loop.go:83`) trump any contradictory pattern from training data.
- **NEVER GUESS PARAMETERS** — when wiring `ToolsProvider`, the latest-user-message accessor must be looked up, not invented.
- **NEVER CREATE FILES UNLESS NECESSARY** — Phase 2's new files are listed exhaustively in D-29 and "Recommended Project Structure" — do not add others.
- **GIT PUSH DISCIPLINE** — never `git push` unless explicitly requested.
- **GOD CLASS** — never create or grow a file > 600 LOC. `internal/wiki/store.go` is 755 today (already in violation); Phase 2 must factor `store_writes.go` as part of the WIKI-02/GIT-01 work to bring `store.go` BELOW 600.

### Post-Edit Validation (mandatory after every edit)

- `go vet ./...` (minimum)
- `go build ./...` (verify compilation)
- `go test ./internal/<package>/` (if tests exist for the changed package)
- `go test -race ./internal/{api,auth,mcp,skills,llm,reindex,tools,wiki}/` (race-detector on critical pkgs — Phase 2 extends this list)
- For TS/React edits: `npm --prefix web run build` (or `npx --prefix web tsc --noEmit`)

### Batch Operations

- Spawn independent agents in one message; fire independent reads/writes/edits/bash calls in parallel. Phase 2 plan files (one per wave) are good candidates for parallel composition.

## Sources

### Primary (HIGH confidence)
- `D:\Aura\.planning\phases\02-llm-reliability-tool-intelligence\02-CONTEXT.md` — D-01..D-32 (locked decisions)
- `D:\Aura\.planning\phases\02-llm-reliability-tool-intelligence\02-AI-SPEC.md` — Section 1 (failure modes), §1b (rubric), §3 (framework reference), §4 (implementation), §4b (Go-native idioms), §5 (eval dimensions), §6 (guardrails), §7 (monitoring)
- `D:\Aura\.planning\phases\02-llm-reliability-tool-intelligence\02-DISCUSSION-LOG.md` — Gray Areas 1-4, follow-ups, all "(recommended)" selections preserved
- `D:\Aura\.planning\REQUIREMENTS.md` — WIKI-01..TOOL-02 + "Out of Scope" (forbids worker-pool/retry libs)
- `D:\Aura\.planning\ROADMAP.md` §"Phase 2" — six success criteria
- `D:\Aura\.planning\research\PITFALLS.md` Pitfall #4 (reindex worker leak), #6 (wiki write race), #7 (temp staircase burn)
- `D:\Aura\CLAUDE.md` — Behavioral Rules (especially GOD CLASS, NEVER SUPPOSE, NEVER MODIFY TESTS), Post-Edit Validation, Build & Test Commands, Architecture sections
- `D:\Aura\go.mod` — current pinned versions (go-git v5.18.0 line 15; go.uber.org/zap v1.28.0 line 22; gopkg.in/yaml.v3 v3.0.1 line 23; modernc.org/sqlite v1.50.0 line 24; Go 1.26.2 line 3)
- `D:\Aura\internal\llm\client.go` (Client interface, Request, Response, Token, Float64Ptr)
- `D:\Aura\internal\llm\retry.go` (current fixed-temp `RetryClient` — the rewrite target)
- `D:\Aura\internal\wiki\store.go` (verified WritePage line 160-217; gitCommit line 351-388; fileMutex line 137-140)
- `D:\Aura\internal\wiki\schema.go` (Page struct line 17-32; CurrentSchemaVersion=2 line 12; Validate, Slug)
- `D:\Aura\internal\wiki\parser.go` (Writer.WriteFromLLMOutput line 40-126 — the heuristic path; MarshalMD line 236-277)
- `D:\Aura\internal\tools\registry.go` (Tool interface line 16-21; Registry, Register, BuildVectorIndex line 216-247; argKeys line 271-277)
- `D:\Aura\internal\tools\registry_search.go` (Registry.Search line 19-103 — lex+vector merge already in place)
- `D:\Aura\internal\tools\registry_search_vector.go` (toolVectorIndex, Build/Search/Health, line 124-298 — the rename target)
- `D:\Aura\internal\tools\tool_search.go` (deletion target — verified standalone, only consumer is conversation_tool_exec.go:114)
- `D:\Aura\internal\tools\definition.go` (ToolDefinition, ToolCallExample, definitionForTool — referenced by Definition())
- `D:\Aura\internal\tools\args.go` (stringArg, stringSliceArg helpers used by WriteWikiPageTool.Execute)
- `D:\Aura\internal\agentloop\loop.go` (verified ToolsProvider hook line 83 + per-turn invocation lines 154-157; State.Messages() interface line 36-37)
- `D:\Aura\internal\search\search.go` (verified WikiPageReindexer interface line 54-57; ReindexWikiPage line 444-460; IndexWikiPages full-rebuild line 178-200)
- `D:\Aura\internal\ingest\pipeline.go` (verified WritePage caller line 155; ReindexWikiPage caller line 180)
- `D:\Aura\internal\conversation\archive.go` (verified BufferedAppender precedent line 332-385)
- `D:\Aura\internal\telegram\setup.go` (verified BuildVectorIndex wiring line 519-528; createLLMClient + NewRetryClient line 793-805; ToolSearchTool registration line 746)
- `D:\Aura\internal\telegram\conversation.go` (verified tool_search references at lines 259, 293, 306, 331)
- `D:\Aura\internal\telegram\conversation_tool_exec.go` (verified tool_search branch line 114-116; toolNamesFromToolSearchResult line 121-137)
- `D:\Aura\internal\telegram\debug_smoke.go` (verified tool_search case line 186)
- `D:\Aura\cmd\debug_telegram_sandbox\main.go` (verified tool_search references lines 61, 195, 319, 816)
- `D:\Aura\internal\api\wiki.go` (verified pageFrontmatter line 116-137 — passthrough target for `unversioned`)

### Secondary (MEDIUM confidence — external sources, cross-verified with primary)
- Tool RAG (Red Hat 2025-11-26): https://next.redhat.com/2025/11/26/tool-rag-the-next-breakthrough-in-scalable-ai-agents/ — top-K=5, name+description embeddings (basis for D-22, D-24)
- Tool-to-Agent Retrieval (arXiv 2511.01854): https://arxiv.org/html/2511.01854v1 — single-step query, K=5 (basis for D-23)
- Maxim production retry guide: https://www.getmaxim.ai/articles/retries-fallbacks-and-circuit-breakers-in-llm-apps-a-production-guide/ — classify-then-retry framing
- LangChain structured-output retry: https://docs.langchain.com/oss/python/langchain/structured-output — error-feedback-into-message pattern
- Ed-Fi ETag concurrency: https://docs.ed-fi.org/reference/data-exchange/api-guidelines/design-and-implementation-guidelines/api-implementation-guidelines/handling-optimistic-concurrency-with-etags/ — `If-Match`/`412` semantics, basis for D-02 create-only sentinel
- Qdrant ingestion patterns: https://qdrant.tech/course/essentials/day-4/large-scale-ingestion/ — micro-batch + idempotency framing
- go-git v5.19.0 release: https://github.com/go-git/go-git/releases/tag/v5.19.0 (verified via WebFetch — no breaking API changes)
- Analog Go repo `D:\tmp\picobot\internal\agent\tools\write_memory.go` — write-tool JSON Schema with required+optional split (cited by AI-SPEC §3 as direct shape mirror for `WriteWikiPageTool`)
- Analog Python classifier `D:\tmp\hermes-agent\agent\error_classifier.py` — `FailoverReason` enum + `ClassifiedError` dataclass; basis for the priority-pipeline shape of `Classify`
- Analog Python retry `D:\tmp\hermes-agent\agent\retry_utils.py` — jittered exponential backoff (`base=5s, max=120s, jitter_ratio=0.5`); Phase 2 scales to `1s/30s/0.5` for chat-LLM RTTs

### Tertiary (LOW — flagged for validation in implementation)
- A2 (LatestUserMessageText accessor existence) — assumed; verify via Grep before locking ToolsProvider closure design
- A5 (EmbedCache makes per-page reindex acceptable) — assumed based on AI-SPEC claim; verify via timing test on a populated wiki
- A6 (precision@5 ≥ 0.80 achievable on 15-example fixture) — cited from external research; calibrate against actual fixture before locking the threshold

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all dependencies verified in `go.mod`; only one minor-version bump
- Architecture: HIGH — every layer is a verified existing surface (LLM Client wrapper, fileMutex, BufferedAppender precedent, ToolsProvider hook, exported toolVectorIndex)
- Pitfalls: HIGH — Pitfalls #4, #6, #7 explicitly traced to PITFALLS.md and AI-SPEC failure modes; concurrency landmines have unit/race tests in the Validation Architecture
- Code examples: HIGH — sketches verified against AI-SPEC §3-§4 and the actual file shapes (line numbers cited)
- External sources: MEDIUM — Tool-RAG / arXiv / Ed-Fi guides cited; precision@5 threshold needs calibration

**Research date:** 2026-05-10
**Valid until:** 2026-06-09 (30 days for a stable Go infrastructure phase; sooner if Phase 1 ship date moves)
