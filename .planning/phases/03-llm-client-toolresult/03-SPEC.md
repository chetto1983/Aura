# Phase 3: LLM Client + ToolResult — Specification

**Created:** 2026-05-30
**Ambiguity score:** 0.08 (gate: ≤ 0.20)
**Requirements:** 13 locked

## Goal

Replace the LLM skeleton with a real, handrolled OpenAI-compatible SSE streaming client (OpenRouter → `deepseek/deepseek-v4-flash:exacto`), land the ToolResult preview+sidecar pattern, and drive both through a full `LlmAgent.Run` loop behind an interactive `aura chat` REPL — with budget-contract enforcement, per-call OpenTelemetry tracing (real exporter), and token+USD cost reporting.

## Background

Today `internal/llm/client.go` is an ~80-LOC pre-rewrite skeleton: it defines the wire types (`Message`, `ToolCall`, `ToolDef`, `Chunk`, `Request`) and the `Client` interface (`Stream(ctx, req) (<-chan Chunk, error)`), but there is **no implementation** — no SSE parser, no real HTTP call. `internal/agent/tools.Tool.Execute` returns `(string, error)`; there is no `ToolResult`, no preview/sidecar spillover, no `read_tool_output`. Phase 2 deleted the old `loop.go` and shipped the open `Agent` interface + Budget tree (shared `*atomic.Int32`, dedup ring, wallclock) + `Event`/`iter.Seq2` streaming + UUIDv7 `request_id`; there is **no `LlmAgent`** implementing `Agent` yet. `aura chat` was removed in Slice 0.9 and is slated to return here. `internal/config` has `AURA_RUN_DIR` and the embed URL but **no LLM config** (provider/model/key/headers/timeout). The web confirms the preview+pointer+artifact pattern is the 2025-26 industrial standard for large tool outputs (arXiv 2511.22729; truncation-only is an anti-pattern).

**Scope note — PRD deviations (amendments, locked by interview):** three choices intentionally extend the PRD's Slice 1 and must land as PRD-amendment commits before implementation:
- **A1** `aura chat` is an **interactive multi-turn REPL** (PRD smoke was single-shot `aura chat "msg"`). Bounded to **in-memory history only** — no DB persistence, auto-title, or resume (those remain Phase 4 / Slice 1.8).
- **A2** OTel emits to a **real configured exporter** (stdout/OTLP), not the PRD's emit-only no-op provider.
- **A3** USD cost uses OpenRouter's **actual reported cost with a static price-table fallback** (PRD implied simpler reporting).

## Requirements

1. **Real SSE streaming client**: An OpenAI-compatible client streams a live response from a real endpoint.
   - Current: `Client` interface exists; no implementation — `Stream` is unimplemented skeleton.
   - Target: `internal/llm/openai_compat` implements `Client.Stream`, parsing `text/event-stream` deltas into `Chunk`s (text + finish_reason), connecting to OpenRouter by default.
   - Acceptance: `go test ./internal/llm/...` passes with ≥1 golden SSE fixture for (a) plain text + `finish_reason="stop"` and (b) tool-call multi-chunk + `finish_reason="tool_calls"`.

2. **Tool-call delta accumulation**: Streamed partial tool-call fragments are merged into complete `ToolCall`s.
   - Current: No accumulator exists.
   - Target: Fragments are merged by `index` (id/name/arguments concatenated) and emitted as a complete `ToolCall` Chunk when the call finalizes.
   - Acceptance: A golden fixture splitting one tool call across ≥3 chunks yields exactly one well-formed `ToolCall` with concatenated arguments that parse as JSON.

3. **Context cancellation, zero leak**: Ctrl+C / ctx-cancel tears down the HTTP request and drains the stream with no goroutine leak.
   - Current: No HTTP layer exists to cancel.
   - Target: ctx cancellation aborts the in-flight request and closes the Chunk channel promptly; the SSE reader unblocks (no goroutine stuck on `Scan()`).
   - Acceptance: `go test -race` with `goleak.VerifyNone`(/`VerifyTestMain`) passes; a cancel-mid-stream test shows the request returns within ~100ms and `runtime.NumGoroutine()` returns to baseline.

4. **No wire retry, serialized error surface**: HTTP errors bubble to the caller with retry signal preserved, but the wire does not retry.
   - Current: No error handling exists.
   - Target: Non-2xx responses return a wrapped `HTTPError{StatusCode, RetryAfterSec, Body}`; the wire performs zero retries (caller decides).
   - Acceptance: A 429 fixture yields an `HTTPError` with `StatusCode==429` and parsed `RetryAfterSec`; no retry attempt is made (request count == 1).

5. **LLM config + load order**: Typed LLM config with a deterministic precedence chain.
   - Current: No LLM config in `internal/config`.
   - Target: `LLMConfig{Provider, Model, BaseURL, APIKey, TotalTimeoutSec, Headers}`; load order built-in default < `.env` (`OPENROUTER_API_KEY`→APIKey) < `~/.aura/llm.json` < `AURA_LLM_*` env. Defaults: provider `openrouter`, model `deepseek/deepseek-v4-flash:exacto`, base `https://openrouter.ai/api/v1`, OpenRouter attribution headers; empty APIKey is a clear error.
   - Acceptance: Load-order test proves default < file < env; missing API key produces a clear non-panic error (exit ≠ 0, message to stderr); OpenRouter headers present on each request.

6. **ToolResult signature + preview/sidecar**: `Tool.Execute` returns a structured result that spills large output to disk.
   - Current: `Tool.Execute(ctx, args) (string, error)`; no spillover.
   - Target: `Execute(ctx, args) (ToolResult, error)` with `ToolResult{Preview, FullPath, Bytes, Truncated}`; `≤ AURA_CONTEXT_PREVIEW_CAP_BYTES` (default 2048) → preview only, no disk write; `>` cap → preview + footer pointer in history, full bytes persisted to the sidecar file. All existing tools (`text_response`, `tool_search`, `search`) adapt.
   - Acceptance: A fake tool returning 100 KB → the `RoleTool` history content is ≤ ~2 KiB (preview + footer), the sidecar file holds the full 100 KB; a ≤cap tool writes no file and history content equals the raw preview.

7. **`read_tool_output` tool**: A builtin retrieves ranges from a sidecar file.
   - Current: Does not exist.
   - Target: Non-deferred builtin `read_tool_output{tool_call_id, offset?=0, limit?=200 lines}` returns the requested slice; unknown `tool_call_id` hard-fails.
   - Acceptance: `read_tool_output(id, offset=50000, limit=100)` returns the correct slice of the 100 KB fixture; an unknown id returns an error (not empty/panic).

8. **Sidecar directory layout**: Sidecar files live under a session-scoped, forward-compatible path.
   - Current: No layout.
   - Target: `$AURA_RUN_DIR/conversations/<session_id>/<tool_call_id>.result`, where `session_id` is an ephemeral UUIDv7 minted per `aura chat` session (Phase 4 makes it the durable `conversation_id` — same shape, no migration). Directory created lazily on first persist.
   - Acceptance: After a chat session that spills a tool output, the file exists at `conversations/<session_id>/<tool_call_id>.result`; `session_id` matches the UUIDv7 stamped on the session's Events.

9. **`LlmAgent` implements the Agent interface**: A real streaming agent drives the loop.
   - Current: No `LlmAgent`; only mock agents (`agenttest`) implement `Agent`.
   - Target: `internal/agent.LlmAgent` implements `Agent`; `Run(ctx) iter.Seq2[*Event, error]` streams chunk/tool-call/lifecycle Events, dispatches tools via the registry, threads `ToolResult` into history, and terminates via `text_response`.
   - Acceptance: A test driving `LlmAgent.Run` against a fake `Client` yields ordered Events (chunk → tool_call → tool_result → final) and terminates; race + goleak clean.

10. **Budget-contract enforcement (amend. #19)**: The loop honors the Phase-2 Budget tree.
    - Current: Budget exists (Phase 2) but no agent consumes it.
    - Target: `LlmAgent.Run` checks step / wallclock / dedup before each LLM call; a trip emits a terminal Event carrying the reason (`max_steps` | `max_wallclock` | `dedup`).
    - Acceptance: `TestLlmAgent_StepCap_Trips`, `TestLlmAgent_WallclockCap_Trips`, `TestLlmAgent_DedupWindow_Trips` each assert the terminal Event with the correct reason; total steps never exceed the cap.

11. **`aura chat` interactive REPL**: A multi-turn chat surface over the agent loop.
    - Current: No `chat` subcommand (removed in Slice 0.9).
    - Target: `aura chat` reads stdin turns in a loop, streams each reply token-by-token to stdout, keeps **in-memory** history across turns, reports per-turn token+USD cost, and exits cleanly on EOF/`/exit`. No DB persistence.
    - Acceptance: Scripted stdin of 2 turns produces 2 streamed replies sharing one session_id; the second turn sees the first in-memory; a missing API key fails the first turn with a clear error and no panic.

12. **Token + USD cost reporting**: Each call reports usage and spend.
    - Current: No usage/cost surface.
    - Target: Per LLM call, capture `prompt_tokens`/`completion_tokens` and USD — preferring OpenRouter's **actual** reported cost, falling back to a static config price table when the provider omits it.
    - Acceptance: A fixture with a provider cost field reports that exact USD; a fixture omitting it reports the table-computed USD; both print tokens + USD to the user.

13. **Per-call OTel span (real exporter) + history immutability**: Observability and a no-mutation guarantee.
    - Current: No tracing; no immutability assertion.
    - Target: Each call emits an `llm.request` span (attrs `llm.model`, `llm.provider`, `llm.prompt_tokens`, `llm.completion_tokens`, `llm.cache_hit_tokens`, `aura.request_id`) via a **configured** tracer provider with a stdout/OTLP exporter; span_id is stable across the call's SSE chunks. The client never mutates `req.Messages`.
    - Acceptance: With an in-memory recorder, exactly 1 span/call with all attributes populated and stable span_id; an exporter-wired run emits a trace; a test asserts `req.Messages` is byte-identical pre/post call.

## Boundaries

**In scope:**
- Real OpenAI-compat SSE client (`internal/llm/openai_compat`): parser, tool-call accumulator, ctx-cancel, no-retry `HTTPError`, OpenRouter default + attribution headers.
- `LLMConfig` + load-order chain + `~/.aura/llm.json` read/write; `aura config` read/write of that file.
- ToolResult pattern: `(ToolResult, error)` signature, preview/sidecar spillover, `read_tool_output` builtin, session-scoped sidecar layout.
- `LlmAgent` implementing `Agent` (Run/iter.Seq2), tool dispatch, budget-contract enforcement (step/wallclock/dedup terminal Events).
- `aura chat` interactive in-memory REPL with live streaming + per-turn token+USD cost.
- OTel `llm.request` span per call with a real (stdout/OTLP) exporter.

**Out of scope:**
- Conversation **persistence** — DB threads, auto-title, archive, resume, multi-thread (Phase 4 / Slice 1.8; `aura chat` history is in-memory only).
- `ask_user` pause/resume (Phase 4 / Slice 1.5) — no human-in-the-loop tool yet.
- Identity + `capability_grants` (Phase 4 / Slice 1.7) — no per-user capability checks.
- KV-cache builder / stable-prefix construction (Phase 6 / Slice 4) — the client reads `Messages` as given.
- Microcompact / history trimming (Phase 4 / Slice 1.8b) — no context-budget L2.
- Wire-level retry (deferred to the caller by design) — only the `HTTPError` signal is surfaced.
- New capability tools (web/sandbox/swarm) — only `text_response`, `tool_search`, `read_tool_output` registered.
- Conversation full-text search (Slice 1.8.5).

## Constraints

- Go (per `go.mod`, 1.26.x); LLM client **handrolled, no SDK**; CGO-free build.
- Every file ≤ 600 LOC; owned-surface coverage ≥ 85% (`make quality-full`); gofmt/dupl/vet/lint/race/vuln gates green.
- The ToolResult signature change is a single coupled commit touching `spec.go` + `text_response.go` + `search.go` + the agent runTool path (PRD rationale: avoid re-opening the same ≤600-LOC files twice).
- env naming `AURA_<DOMAIN>_<UNIT>` except canonical third-party keys (`OPENROUTER_API_KEY`); deferred-tool pattern preserved (`read_tool_output` is non-deferred).
- ctx-cancel must propagate end-to-end through the HTTP request; no idle/first-byte timeout (global timeout `AURA_LLM_*` default 120s + connect 10s).
- API keys / DSNs never appear in logs, errors, Events, or spans.

## Acceptance Criteria

- [ ] `go test ./internal/llm/...` passes with golden SSE fixtures (text+stop, tool-call multi-chunk+tool_calls, 429 no-retry, premature-close cancel).
- [ ] `aura chat` (with config) streams a real reply from `deepseek/deepseek-v4-flash:exacto` via OpenRouter and prints per-turn token + USD cost.
- [ ] `aura chat` without an API key fails the turn with a clear stderr message — no panic, no silent fallback.
- [ ] Ctrl+C / ctx-cancel during generation aborts the HTTP request within ~100ms; `go test -race` + goleak show no residual goroutine.
- [ ] `Tool.Execute` returns `(ToolResult, error)`; a 100 KB tool output leaves only preview+footer (≤ ~2 KiB) in history and a full sidecar file at `conversations/<session_id>/<tool_call_id>.result`.
- [ ] `read_tool_output{tool_call_id, offset, limit}` returns the correct slice; unknown id hard-fails.
- [ ] `LlmAgent.Run` trips on step / wallclock / dedup caps, each emitting a terminal Event with the correct reason; total steps ≤ cap.
- [ ] One `llm.request` OTel span per call with all required attributes and a stable span_id across chunks; an exporter-wired run emits a trace; in-memory recorder test passes.
- [ ] USD is taken from OpenRouter's reported cost when present, else the config price table; both paths covered by tests.
- [ ] The client does not mutate `req.Messages` (pre/post byte-identical assertion).
- [ ] PRD-amendment commit(s) for A1 (REPL), A2 (real exporter), A3 (cost actual+table) land before implementation.

## Ambiguity Report

| Dimension          | Score | Min  | Status | Notes                                                        |
|--------------------|-------|------|--------|--------------------------------------------------------------|
| Goal Clarity       | 0.95  | 0.75 | ✓      | Outcome + 4 ROADMAP SCs + machine-checkable PRD acceptance   |
| Boundary Clarity   | 0.88  | 0.70 | ✓      | Explicit defer of persistence/ask_user/identity/KV/retry     |
| Constraint Clarity | 0.92  | 0.65 | ✓      | Timeouts/no-retry/LOC/coverage/headers/OTel attrs all pinned |
| Acceptance Criteria| 0.92  | 0.70 | ✓      | 11 pass/fail criteria, all falsifiable                       |
| **Ambiguity**      | 0.08  | ≤0.20| ✓      | Gate passes                                                  |

Status: ✓ = met minimum, ⚠ = below minimum (planner treats as assumption)

## Interview Log

| Round | Perspective         | Question summary                                  | Decision locked                                                        |
|-------|---------------------|---------------------------------------------------|-----------------------------------------------------------------------|
| 0     | Researcher (scout)  | What exists vs PRD Slice 1?                        | Wire types + Client interface exist; no impl, no ToolResult, no LlmAgent |
| 1     | Boundary Keeper     | Sidecar dir scoping given conv_id is Phase 4?     | `conversations/<session_id>/…`, ephemeral UUIDv7 → durable conv_id later |
| 1     | Boundary Keeper     | CLI surface: `aura llm ping` vs `aura chat`?      | `aura chat` (full loop) — A1 amendment                                |
| 1     | Boundary Keeper     | LlmAgent scope: wire-only vs full loop?           | Full `LlmAgent.Run` + budget enforcement (amend. #19)                 |
| 1.5   | Researcher (web)    | Best industrial large-tool-output pattern?        | preview+pointer+artifact is standard (arXiv 2511.22729) — validates PRD |
| 2     | Failure Analyst     | USD cost source?                                  | OpenRouter actual cost + static table fallback — A3 amendment          |
| 2     | Failure Analyst     | `aura chat` single-shot vs REPL?                  | Interactive multi-turn REPL, in-memory only — A1 amendment            |
| 2     | Failure Analyst     | OTel emit-only vs real exporter?                  | Real stdout/OTLP exporter wired — A2 amendment                        |

---

*Phase: 03-llm-client-toolresult*
*Spec created: 2026-05-30*
*Next step: /gsd:discuss-phase 3 — implementation decisions (how to build what's specified above)*
