---
phase: 03-llm-client-toolresult
plan: 02
subsystem: llm-openai-compat
tags: [llm, sse, streaming, openrouter, tool-calls, ctx-cancel]
requires:
  - "internal/llm.Config (Plan 01 — BaseURL/APIKey/Headers/timeouts/Temperature/MaxTokens/Model/Provider)"
  - "internal/llm.Client interface + Chunk/ToolCall/Request (pre-existing llm/client.go)"
  - "go.uber.org/goleak (test harness)"
  - "pgregory.net/rapid (property test)"
provides:
  - "internal/llm/openai_compat.Client implementing llm.Client (handrolled HTTP+SSE Stream)"
  - "openai_compat.HTTPError{StatusCode,RetryAfterSec,Body} — no-retry, key-safe error"
  - "openai_compat.Usage{PromptTokens,CompletionTokens,CachedTokens,Cost} (wire-half of Req#12)"
  - "5 golden testdata/*.sse fixtures (network-free deterministic parser coverage)"
affects:
  - "Plan 04 (LlmAgent drives this client; wraps total timeout on ctx; consumes Usage + length finish_reason)"
tech-stack:
  patterns:
    - "bufio.Reader.ReadString('\\n') SSE loop (NEVER bufio.Scanner — >64KiB line footgun)"
    - "private toolCallDelta{Index,...} merged into map[int]*acc; public llm.ToolCall has NO Index"
    - "no-retry HTTPError on non-2xx; request count == 1"
    - "Transport keep-alive hygiene (CloseIdleConnections / DisableKeepAlives) for order-independent goleak"
    - "Authorization: Bearer set ONLY at the wire (D-28 structural secret redaction)"
key-files:
  created:
    - "internal/llm/openai_compat/client.go"
    - "internal/llm/openai_compat/sse.go"
    - "internal/llm/openai_compat/accumulate.go"
    - "internal/llm/openai_compat/httperror.go"
    - "internal/llm/openai_compat/usage.go"
    - "internal/llm/openai_compat/main_test.go"
    - "internal/llm/openai_compat/client_test.go"
    - "internal/llm/openai_compat/sse_test.go"
    - "internal/llm/openai_compat/accumulate_test.go"
    - "internal/llm/openai_compat/httperror_test.go"
    - "internal/llm/openai_compat/testdata/text_stop.sse"
    - "internal/llm/openai_compat/testdata/toolcall_multichunk.sse"
    - "internal/llm/openai_compat/testdata/error_429.sse"
    - "internal/llm/openai_compat/testdata/premature_close.sse"
    - "internal/llm/openai_compat/testdata/length_truncation.sse"
  modified: []
decisions:
  - "Tasks 1 and 2 are mutually-referential (the parser, accumulator, error shape and the fixtures the tests replay) — landed in one coherent commit 9a9518ab per the plan's own scope-note (D-01 commit 3), not artificially split"
metrics:
  duration: "~50min (executor interrupted by API Overloaded at closeout; implementation commit had already landed)"
  completed: "2026-05-30"
  tasks: 2
  files: 15
---

# Phase 3 Plan 02: Handrolled OpenAI-Compat SSE Client Summary

Implemented the handrolled OpenAI-compatible HTTP+SSE streaming client (D-01
commit 3): a defensive `bufio.Reader` parser, index-keyed tool-call delta
accumulation, a no-retry `HTTPError`, final usage-chunk capture, OpenRouter
attribution headers + `provider.data_collection:deny`, end-to-end ctx-cancel,
and five golden SSE fixtures that exercise every parser path deterministically
without network.

## What Was Built

Single coherent commit `9a9518ab` (the plan's scope-note explicitly keeps the
SSE wire layer + the fixtures its tests replay in one commit — D-01 commit 3):

- **`client.go`** (138 LOC): `Client{cfg, httpClient}` + `New(cfg llm.Config)`;
  `Stream(ctx, req) (<-chan llm.Chunk, error)` marshals the OpenAI body
  (`model, messages, tools, tool_choice:"auto", temperature, max_tokens,
  stream:true, provider:{data_collection:"deny"}` — D-20/D-16; no
  `usage`/`stream_options` keys), `http.NewRequestWithContext`, sets
  `Authorization: Bearer` only here (D-28) + `HTTP-Referer`/`X-Title` from
  cfg.Headers, dialer connect-timeout with NO `http.Client.Timeout` (total
  timeout rides ctx — D-19), keep-alive hygiene for order-independent goleak,
  non-2xx → parsed `HTTPError` with request-count 1, one parse goroutine that
  closes the channel on `[DONE]`/EOF/cancel. `var _ llm.Client = (*Client)(nil)`.
- **`sse.go`** (136 LOC): `bufio.Reader.ReadString('\n')` loop (never Scanner —
  Pitfall #1); skips blanks and `:`-prefixed keep-alive comments; `data: [DONE]`
  returns clean without unmarshal; private `wireChunk` (only consumed fields).
- **`accumulate.go`** (100 LOC): private `toolCallDelta{Index,ID,Type,Function}`
  merged into `map[int]*acc`; emits one finalized `llm.ToolCall` per index with
  concatenated arguments; public `llm.ToolCall` keeps NO `Index` field.
- **`httperror.go`** (54 LOC): `HTTPError{StatusCode,RetryAfterSec,Body}`
  implementing `error`; parses `Retry-After`; error string never carries the key.
- **`usage.go`** (42 LOC): private `usageWire` → exported `Usage{PromptTokens,
  CompletionTokens,CachedTokens,Cost}`; `cached_tokens` distinct from
  `cache_write_tokens` (RESEARCH Pitfall 4); `cost` surfaced as-is (D-18).
- **5 golden fixtures**: `text_stop` (comment + content deltas + usage chunk +
  `[DONE]`), `toolcall_multichunk` (≥3-chunk tool call, single line 70129 bytes
  >64KiB), `error_429`, `premature_close`, `length_truncation`.
- **Tests + harness**: `main_test.go` goleak `VerifyTestMain`; client/sse/
  accumulate/httperror tests replay fixtures via `httptest.NewServer`.

## Deviations from Plan

### Process deviation (not a code deviation)
- The executor agent's final turn failed with `API Error: Overloaded` **after**
  the implementation commit `9a9518ab` had already landed and the working tree
  was clean. Only the closeout (this SUMMARY + STATE/ROADMAP) was interrupted.
  The orchestrator verified ground truth — full build/vet/test/race + named
  acceptance tests + lint + fixture sanity all green (evidence below) — and
  completed the closeout. No code was added or changed by the orchestrator.

### Code deviations
None. Implementation matches the plan's action + acceptance blocks.

## Authentication Gates
None. (Live OpenRouter acceptance is Plan 05's manual `scripts/llm_smoke.sh`
gate — this plan is deterministic and network-free by design.)

## Known Stubs
None.

## Verification Evidence
- `go build ./...` green; `go vet ./internal/llm/openai_compat/` clean.
- `go test ./internal/llm/openai_compat/` ok; `BASH_ENV=~/.aura-toolchain.sh
  go test -race ./internal/llm/openai_compat/` ok.
- Named acceptance tests executed (not skipped) and PASS: `TestStream_TextStop`
  (Req#1), `TestAccumulate_*` incl. `TestAccumulate_PartitionInvariant` rapid
  property (Req#2), `TestStream_429NoRetry` (Req#4, request-count 1),
  `TestUsage` (Req#12 wire), `TestSecretRedaction` (D-28 release-gate),
  `TestStream_CancelMidStream` 0.04s real teardown + goroutine baseline + goleak
  (Req#3), `TestStream_LengthTruncation`.
- `golangci-lint run ./internal/llm/openai_compat/...` → 0 issues.
- Fixture sanity: `text_stop.sse` has `:`-comment + `[DONE]` + usage chunk;
  `toolcall_multichunk.sse` max line = 70129 bytes (>64KiB, proves bufio.Reader).
- File sizes: client 138, sse 136, accumulate 100, httperror 54, usage 42 — all ≤600 LOC.

## Self-Check: PASSED
