# Phase 2: LLM Reliability & Tool Intelligence - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-05-10
**Phase:** 2-llm-reliability-tool-intelligence
**Areas discussed:** wiki tool surface, retry policy, async reindex worker, tool retrieval, git tracking, package layout

---

## Pre-Discussion Research (user-requested)

User asked: "DEEP SEARCH ONLINE AND ON D:\tmp FOR BEST PRATIC" before discussing gray areas.

**On D:\tmp surfaced:**
- `picobot/internal/agent/tools/write_memory.go` — Go bot with `write_memory` tool: required+optional split with enum constraints, `append vs overwrite` boolean flag, content pre-validation with hard prohibitions in description.
- `hermes-agent/agent/error_classifier.py` — production error taxonomy with 13 `FailoverReason` classes (auth, billing, rate_limit, overloaded, server_error, timeout, context_overflow, payload_too_large, image_too_large, model_not_found, format_error, thinking_signature, unknown) and recovery hint flags (retryable, should_compress, should_rotate_credential, should_fallback). Pattern lists for billing exhaustion vs rate-limit detection.
- `hermes-agent/agent/retry_utils.py` — `jittered_backoff(base=5s, max=120s, jitter_ratio=0.5)` to prevent thundering-herd on concurrent retries.

**Online surfaced:**
- Tool RAG (Red Hat 2025-11-26) — top-K=5 operationally validated; tripled invocation accuracy; halved prompt length.
- Tool-to-Agent Retrieval (arXiv 2511.01854) — single-step query (latest user message), single-vector embedding of name+description.
- Maxim production retry guide — classify-then-retry, exponential backoff with jitter, conservative retry limits (3 content / 5–7 transient).
- LangChain structured output docs — `handle_errors` injects validation error verbatim into a `ToolMessage` for retry; same temperature, input modification.
- Ed-Fi ETag concurrency guide — `If-Match` header + `412 Precondition Failed` response. Strong validators (content hash or `updated_at`) preferred over weak.
- Qdrant ingestion guide — micro-batch 100 points / 5s window canonical for high-throughput; per-document upsert acceptable at small scale.
- dev.to drop-newest pattern — simple `select { case ch <- x: default: }`. Drop-oldest needs nested select and is racy.
- go-git v5.19.0 — minor security/dependency refresh from v5.18.0; compatible upgrade.

The research informed which option was marked "(recommended)" in each gray-area question. User selected "(recommended)" for all 4 gray areas plus all 3 follow-ups.

---

## Gray Area 1 — `write_wiki_page` Tool Surface

| Option | Description | Selected |
|--------|-------------|----------|
| Strict + always-required ETag (recommended) | title+body+expected_updated_at all required; slug derived; full-replace body; structured conflict response | ✓ |
| Optional ETag, full-replace body | expected_updated_at optional; soft warning when missing; lighter contract / weaker safety | |
| Required ETag + body 'patch' mode | body can be full OR section-patch; more complex tool surface | |

**User's choice:** Strict + always-required ETag.
**Notes:** Captured as D-01 through D-05. Slug stays derived from title (matches existing `wiki.Slug(title)` in store.go). Full-replace only — no patch mode. Frontmatter fields the LLM must NOT control: `slug`, `unversioned`, `schema_version`, `prompt_version`, `created_at`, `updated_at`. Conflict response is structured JSON returned as the tool result so the LLM can recover deterministically.

### Follow-up: Create vs Update

| Option | Description | Selected |
|--------|-------------|----------|
| Single tool, sentinel value for create (recommended) | expected_updated_at='' means create; mirrors HTTP If-Match: * idiom | ✓ |
| Two separate tools (create_wiki_page + update_wiki_page) | cleaner semantics, doubles tool surface | |
| Single tool with explicit mode field | mode='create'\|'update'; more verbose for the LLM | |

**User's choice:** Single tool with sentinel value for create.
**Notes:** Captured as D-02. Tool semantics: page does not exist + expected="" → create; page exists + expected matches → update; otherwise → conflict. Existing internal callers (`internal/ingest/pipeline.go`, `internal/tools/tool_registry.go`) keep "trust caller" semantics — only the new LLM tool flows through the ETag-checked path (D-05).

---

## Gray Area 2 — Retry Classification + Temperature Policy

| Option | Description | Selected |
|--------|-------------|----------|
| Classify-first; staged temp on content only (recommended) | 3 buckets: TRANSIENT same-temp 5x exponential+jitter; CONTENT 0.0→0.3→0.7 max 3 with feedback; PERMANENT no retry | ✓ |
| Same-temp + input-modification (LangChain style) | 2 buckets, no temperature staging, validation error injected into prompt | |
| Hermes-grade 13-class taxonomy | port FailoverReason enum with full hint flags; overkill for v4.0 single-provider | |

**User's choice:** Classify-first; staged temp on content only.
**Notes:** Captured as D-06 through D-10. Temperature schedule 0.0→0.3→0.7 over max 3 content retries, no backoff between content retries (immediate). Transient retries preserve caller's temperature (jittered exponential, base 1s → 30s cap, jitter ratio 0.5 from Hermes pattern). Permanent errors zero retries. Existing `RetryClient` rewritten in place, same constructor signature so callers don't change.

### Follow-up: Temperature Override Semantics

| Option | Description | Selected |
|--------|-------------|----------|
| Wrapper overrides on content retry only (recommended) | first attempt uses caller's temp; wrapper rewrites only on CONTENT retry; transient preserves caller's temp | ✓ |
| Wrapper always controls temperature | wrapper ignores caller's Request.Temperature | |
| Caller opts in via flag | Request.AllowTempEscalation bool | |

**User's choice:** Wrapper overrides on content retry only.
**Notes:** Captured as D-08. Preserves explicit caller intent: wiki writes still start at 0 deterministic on the happy path; only escape to higher temp on validation fail.

---

## Gray Area 3 — Async Reindex Worker Design

| Option | Description | Selected |
|--------|-------------|----------|
| Slug-only jobs, drop-newest, single worker (recommended) | Job{Slug, Op}; worker re-reads file from disk; cap 100; select/default; new internal/reindex/ package | ✓ |
| Slug+body snapshot jobs, drop-oldest | latest-wins snapshot; nested select; lower latency but JOB memory grows | |
| Worker pool (N=2) + micro-batch | matches Qdrant canonical pattern; overkill at <50 pages | |

**User's choice:** Slug-only jobs, drop-newest, single worker.
**Notes:** Captured as D-11 through D-16. Drop-newest is safe because the worker re-reads from disk when processing, so the worker always sees the latest snapshot. Single worker keeps Qdrant upserts sequential. Buffered channel cap 100 (configurable). Worker has dedicated context for clean Stop() (Pitfall 4 prevention). Channel never closed (avoids send-on-closed races). Health surface: queue depth, dropped count, last success/error.

---

## Gray Area 4 — Tool Retrieval / `tool_search` Removal

| Option | Description | Selected |
|--------|-------------|----------|
| Always-on core + top-K=5 retrieved supplements (recommended) | 7 always-on core tools + 5 retrieved per turn; query=latest user msg; cold-start=core only; Qdrant-down=full toolset | ✓ |
| Pure retrieval, top-K=10 with no core | all tools retrieved; cleaner mental model but query 'hello' might miss essentials | |
| Last-N-turns context for retrieval, top-K=5 + core | concat last 3 turns; slower, recency-biased; arXiv 2511 prefers single-step | |

**User's choice:** Always-on core + top-K=5 retrieved supplements.
**Notes:** Captured as D-22 through D-28. Core list (7): `write_wiki_page`, `search_memory`, `list_memory`, `read_memory`, `schedule_task`, `request_dashboard_token`, `read_skill`. Retrieval query is the latest user message only (single-step per arXiv 2511.01854). Embedding strategy: single-vector `name + " " + description` per tool (no examples). Fallback when Qdrant down: inject FULL toolset and log degraded mode at WARN. Cold-start: only core. `tool_search.go` and test file deleted; `tierSearch` branch in agentloop removed; references in debug_smoke files cleaned.

---

## GIT-01 — Commit Tracking + `unversioned` Flag

| Option | Description | Selected |
|--------|-------------|----------|
| Set on first commit failure, cleared on next successful commit (recommended) | re-read+set flag on commit fail; clear on next success; no background retry; "Git tracking pending" badge | ✓ |
| Set on failure, background retry loop clears it | periodic ticker retries failed commits; another goroutine to manage | |
| Set on failure only, never auto-clear | manual clear via dashboard; flag may persist after issue resolved | |

**User's choice:** Set on first commit failure, cleared on next successful commit.
**Notes:** Captured as D-17 through D-21. Commit failure path: re-read page, set `Unversioned: true`, atomic re-write, NO commit retry on the metadata-only re-write. Auto-clear on next successful commit for the same slug. Schema not bumped (additive backward-compatible field with `omitempty`). Dashboard badge: "Git tracking pending" (yellow), explicitly NOT "not tracked" or "not saved" — the page IS saved on disk; only git history is pending. go-git v5.18.0 → v5.19.0 upgrade as a one-line go.mod change.

---

## Package Layout

| Option | Description | Selected |
|--------|-------------|----------|
| Component-aligned packages (recommended) | new internal/reindex/; new files internal/llm/classify.go and internal/tools/wiki.go; existing files modified in place | ✓ |
| Single internal/reliability/ umbrella | groups classify+retry+reindex; god-package risk | |
| Inline everything in existing packages | no new packages; lowest friction but blurs boundaries | |

**User's choice:** Component-aligned packages.
**Notes:** Captured as D-29 through D-32. NEW: `internal/reindex/` (worker.go, types.go, worker_test.go), `internal/llm/classify.go`, `internal/tools/wiki.go`. MODIFIED: `internal/llm/retry.go`, `internal/wiki/store.go`, `internal/wiki/schema.go`, `internal/tools/registry_search_vector.go` (export `ToolVectorIndex`), `internal/agentloop/loop.go`, `internal/telegram/setup.go`, `go.mod`/`go.sum`. DELETED: `internal/tools/tool_search.go`, `internal/tools/tool_search_test.go`. `internal/wiki/store.go` is 755 LOC today — Phase 2 must split into a `store_writes.go` companion file if changes threaten the 800 LOC ceiling (CLAUDE.md god-class rule).

---

## Claude's Discretion

- Exact JSON key shapes within the conflict response payload (structured per D-03; key naming is Claude's call).
- Internal worker logging shape (zap fields, log levels) following existing `internal/logging` conventions.
- `ReindexConfig` env var names (consistent with existing `RUNTIME_*` pattern).
- Whether to add a `WritePageWithExpected(ctx, page, expected)` method or extend `WritePage` with variadic — implementation detail.
- Exact rendering of "Git tracking pending" badge in the React dashboard.

## Deferred Ideas

- **Incremental Qdrant upsert (single point) replacing `Engine.IndexWikiPages` full-rebuild on per-page reindex.** Phase 2 keeps the existing rebuild as the worker's reindex implementation. Future performance improvement; `EmbedCache` keeps the rebuild cheap.
- **Provider-specific error pattern extensions** (Anthropic thinking-signature, OpenRouter policy blocks). Phase 2 covers OpenAI / OpenAI-compatible only.
- **Worker pool (N=2) + micro-batch coalescing** for the reindex worker. Revisit if bulk imports show queue-full drops.
- **Generic optimistic-concurrency on all `WritePage` callers** (`internal/ingest/pipeline.go`, `internal/tools/tool_registry.go`). Phase 2 wires ETag only on the new LLM-callable tool.
- **Dashboard "manual git commit retry" action** for `unversioned` pages. Phase 2 auto-clears on next successful write; an explicit button is UX polish.
- **Test strategy details** (Qdrant mock fixtures, dashboard concurrent-edit test, race-detector coverage for worker shutdown, cold-start tool injection assertion). Captured for the planner to pick up.
