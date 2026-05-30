# Phase 3: LLM Client + ToolResult — Research

**Researched:** 2026-05-30
**Domain:** Handrolled OpenAI-compatible HTTP+SSE streaming client (Go 1.26 stdlib) + ToolResult preview/sidecar + OTel tracing
**Confidence:** HIGH

> **Scope of this document.** Phase 3 already carries an unusually complete design contract: `03-SPEC.md` (14 locked requirements, ambiguity 0.08), `03-CONTEXT.md` (32 HOW-decisions D-01..D-32), and `03-AI-SPEC.md` (70 KB framework + eval contract). Those decisions are **LOCKED** and are NOT re-litigated here. This RESEARCH adds only (a) **verification** of the externally-checkable facts those docs assert (version pins, OpenRouter wire shape, model id, ctx-cancel semantics), (b) the **deltas** I found against the wire reality, and (c) the **Validation Architecture** section Nyquist requires. Where this document and the AI-SPEC agree, treat the AI-SPEC as the primary implementation reference.

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
All 32 decisions D-01..D-32 in `03-CONTEXT.md` are locked HOW-choices. The planner MUST honor them. The load-bearing ones for implementation:

- **D-01** — 7 atomic sub-commits in dependency order, Gate 2 green between each; the ToolResult signature change stays one coupled commit. **D-02** — one combined PRD-amendment commit (A1+A2+A3+A4+A5) FIRST.
- **D-03..D-07** — full real OTel stack lands now (Phase 2 deferred it): mint 8-byte `crypto/rand` SpanIDs full-tree, real `TracerProvider`, default OTLP/gRPC → `localhost:4317` silent-drop, `AURA_OTEL_EXPORTER ∈ {stdout,otlp,none}` (default `otlp`), `AURA_OTEL_ENDPOINT` override.
- **D-08/D-09** — `current_time` is a non-deferred builtin (tool-only this phase); minimal EN tool-aware system prompt, mechanism-not-enumeration, byte-stable `messages[0]`, `Always respond in Italian` directive. No timestamp in the prefix.
- **D-10/D-11/D-12** — two-stage Ctrl+C; one-line per-turn cost footer `· {tok} ({in}/{out}) · ${usd} · {lat}s`; dim tool-activity feedback.
- **D-13/D-14/D-15/D-16** — `text_response` is the sole terminal channel (stream its `text` arg via incremental JSON-string extractor); content-stop fallback; multiple tool_calls dispatched sequentially; tool errors → error tool-result (never terminal); `tool_choice="auto"`.
- **D-17/D-18/D-19** — defensive `bufio.Reader` parser (NOT Scanner); usage chunk for tokens, OpenRouter actual cost first + price-table fallback; total timeout on request ctx (NOT `http.Client.Timeout`), connect 10s via dialer, no idle timeout.
- **D-20/D-21** — attribution headers + `provider.data_collection="deny"`; `finish_reason="length"` → partial + `[risposta troncata: max_tokens]`, no auto-continue.
- **D-22/D-23/D-24** — `LLMConfig` shape + load order (default < `.env` < `~/.aura/llm.json` < `AURA_LLM_*`); A3 price table seeded with deepseek-v4-flash; `aura config show/get/set`.
- **D-25/D-26/D-27** — shared `tools.NewResult(ctx, content)` spillover helper (ctx-injected ids); `session_id = Event.ThreadID` (UUIDv7); `read_tool_output` offset/limit in **BYTES** (Amendment A4, overrides SPEC Req#7 "lines").
- **D-28/D-29/D-30** — structural secret redaction + anti-leak test; degrade-clean (discard partial assistant msg on cancel, clean error on `context_length_exceeded`, sidecar-fail → preview + note); thin `slog` secondary to OTel, request_id-correlated, no secrets.
- **D-31/D-32** — deterministic CI tier (golden SSE fixtures + `httptest` + fake `llm.Client` + goleak + `-race`) + manual real-OpenRouter smoke (`scripts/llm_smoke.sh`, gated on `OPENROUTER_API_KEY`, NOT CI). Golden fixtures in `internal/llm/openai_compat/testdata/*.sse`.

### Claude's Discretion (defaulted, planner-overridable)
- Exact `Temperature`/`MaxTokens` defaults (0.7 / 4096); lower temp (~0.3) defensible for tool-reliability.
- Exact OTel module versions — **now pinned: v1.44.0 (verified, see below)**.
- `read_tool_output` default byte limit (~2048) and price-table seed numbers.
- `aura config` key naming (dotted `llm.model` vs nested) — cobra-idiomatic.
- Spillover helper location (`internal/agent/tools` vs `internal/agent`) — DRY is the constraint, not the path.

### Deferred Ideas (OUT OF SCOPE)
Ambient-date tail-injection (Phase 6), caching-aware provider routing (Phase 6), auto-continue on length (future), concurrent tool execution (Phase 9), conversation persistence/microcompact (Phase 4), composable snippet prompt builder (Phase 6), `tool_choice="required"` (revisit only if `auto`+fallback fails). Plus all SPEC out-of-scope items: persistence, `ask_user`, identity/capability_grants, KV stable-prefix builder, history trimming, wire retry, new capability tools, full-text search.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CORE-01 | Real handrolled OpenAI-compat SSE client (DeepSeek-V4 via OpenRouter) + ToolResult preview+sidecar + ctx-cancel end-to-end + per-call OTel span | Verified: OTel v1.44.0 pin (Go proxy), OpenRouter usage-chunk wire shape, DeepSeek-V4-Flash model id + pricing, `:exacto` variant semantics, net/http ctx-cancel body-read teardown. All 14 SPEC requirements map to deterministic Go tests (see Validation Architecture). |

CORE-01 decomposes into SPEC Req#1..#14 (the planner's task map). Every SPEC requirement has a machine-checkable acceptance criterion already; this RESEARCH confirms each is testable without network in CI (the deterministic tier) plus a manual real-smoke gate for Req#11.
</phase_requirements>

## Summary

The Phase 3 design contract is correct and current. I verified every externally-checkable fact it asserts and found it accurate, with **two small deltas** worth surfacing to the planner (neither overturns a locked decision):

1. **`cache_write_tokens` exists in the usage object** alongside `cached_tokens` and the AI-SPEC only mentions `cached_tokens`. On the *first* request that establishes a cache entry, tokens are billed as cache-writes, not hits; an honest cost/cache footer should be aware of `usage.prompt_tokens_details.cache_write_tokens` (and `usage.cost_details.upstream_inference_cost`) so the cache-hit-ratio metric isn't silently distorted on the first turn. The locked decision (prefer `usage.cost`, fall back to table) is unaffected — `usage.cost` already nets everything — but the OTel `llm.cache_hit_tokens` attr should read `cached_tokens` specifically (not "all cache tokens").

2. **`Agent.Run` takes `InvocationContext`, not a bare `ctx`.** The AI-SPEC pseudocode writes `Run(ctx) iter.Seq2[*Event,error]`; the actual interface in `internal/agent/agent.go:33` is `Run(InvocationContext) iter.Seq2[*Event, error]`. The cancellable `context.Context` rides inside `InvocationContext.Ctx` (named field, never embedded). The planner must thread `ic.Ctx` (wrapped with `context.WithTimeout`) into `http.NewRequestWithContext`, and mint the deferred `SpanID`/`ParentSpanID` (currently `[8]byte{}`/nil at `agent.go:51-52`).

Everything else — the `bufio.Reader` choice, the `context.WithTimeout`-on-request-not-`Client.Timeout` footgun, the `[DONE]` sentinel + `:`-comment skip, tool-call delta accumulation by index, the OTLP-silent-drop posture, the preview+sidecar spillover, the secret-redaction discipline — is verified-correct against current Go stdlib + OpenRouter + OTel reality.

**Primary recommendation:** Implement directly to the AI-SPEC Section 3 entry-point skeleton and Section 4 `LlmAgent.Run` loop, applying the two deltas above. Pin OTel at `v1.44.0` (all four modules, unified train — verified on the Go proxy). The whole phase is deterministically testable in CI without network; the only live dependency is the manual smoke for Req#11.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| SSE wire parse / tool-call accumulation / ctx-cancel | `internal/llm/openai_compat` (wire layer) | — | Provider-neutral wire layer; the agent never branches on provider |
| LLM config + load-order + price table | `internal/llm` (config.go, prices.go) | `internal/config` (composes `LLM llm.Config`) | Per-subsystem config in owning package (Phase 1 D-07) |
| Run-loop / budget enforcement / tool dispatch / history threading | `internal/agent` (LlmAgent) | `internal/agent/tools` (Registry) | First real `Agent` impl; consumes Phase-2 Budget tree |
| ToolResult spillover (preview/sidecar) | `internal/agent/tools` (NewResult helper) | filesystem `$AURA_RUN_DIR/conversations/<ThreadID>/` | DRY helper; tools never reimplement spillover (D-25) |
| OTel span emission + TracerProvider bootstrap | `internal/agent` (tracing.go) | OTel SDK (exporter → localhost:4317) | One `llm.request` span/call; threaded via tracer, no god-object |
| REPL / cost footer / Ctrl+C / `aura config` | `cmd/aura` (chat + config subcommands) | `internal/agent` (drives Run loop) | CLI edge owns user-facing rendering + local-tz; UTC internal |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go stdlib `net/http` + `bufio` + `encoding/json` + `context` | 1.26.3 | The entire wire client | `[CITED: 03-AI-SPEC §2]` Handrolled, no SDK — SPEC-locked. ~280 LOC, byte-level control an SDK hides |
| `go.opentelemetry.io/otel` | v1.44.0 | Tracing API | `[VERIFIED: Go proxy]` `proxy.golang.org/.../@latest` → v1.44.0, 2026-05-27 |
| `go.opentelemetry.io/otel/sdk` | v1.44.0 | TracerProvider + batcher | `[VERIFIED: Go proxy]` same unified train, `sdk/v1.44.0` tag |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` | v1.44.0 | OTLP/gRPC exporter (default) | `[VERIFIED: Go proxy]` same train |
| `go.opentelemetry.io/otel/exporters/stdout/stdouttrace` | v1.44.0 | stdout exporter (`AURA_OTEL_EXPORTER=stdout`) | `[VERIFIED: Go proxy]` same train |
| `github.com/google/uuid` | v1.6.0 | session_id (ThreadID, UUIDv7) | `[VERIFIED: codebase]` already in go.mod (Phase 2) |

### Supporting (test-only, already present)
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `go.uber.org/goleak` | v1.3.0 | goroutine-leak detection | `[VERIFIED: codebase]` `VerifyTestMain` per package; cancellation hygiene |
| `go.opentelemetry.io/otel/sdk/trace/tracetest` | (in `otel/sdk` v1.44.0) | in-memory span recorder | `[CITED: 03-AI-SPEC §5]` assert exactly 1 span/call with attrs, no live collector |
| `net/http/httptest` | stdlib | replay golden SSE fixtures | Stream `testdata/*.sse` through `httptest.Server` or custom `RoundTripper` |

### Alternatives Considered
All ruled out by the LOCKED handrolled-no-SDK decision (AI-SPEC §2). Documented for completeness only: `openai-go` SDK (hides SSE control), LangGraph/CrewAI/etc. (Python/TS only, fail the Go-only constraint). Do not revisit.

**Installation:**
```bash
go get go.opentelemetry.io/otel@v1.44.0
go get go.opentelemetry.io/otel/sdk@v1.44.0
go get go.opentelemetry.io/otel/exporters/stdout/stdouttrace@v1.44.0
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@v1.44.0
go mod tidy   # resolves transitive google.golang.org/grpc + otlptrace to the matching v1.44.0 train
```

**Version verification (this session):** All four `go.opentelemetry.io/otel*` modules return `{"Version":"v1.44.0","Time":"2026-05-27T16:42:37Z"}` from `proxy.golang.org` with identical commit hash `b62d9283` — a single unified release train, no `replace`, no pre-release. Confirm `go.sum` carries no `-rc`/`-beta`/`-alpha` suffix before committing. `otlptracegrpc` transitively pulls `google.golang.org/grpc`; let `go mod tidy` lock it.

## Package Legitimacy Audit

| Package | Registry | Age | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-------------|-----------|-------------|
| `go.opentelemetry.io/otel` (+sdk, +2 exporters) | Go proxy / GitHub | mature (v1.x since 2021) | github.com/open-telemetry/opentelemetry-go | n/a (Go, slopcheck is npm/PyPI) | Approved — verified on Go proxy, official CNCF project |
| `github.com/google/uuid` | Go proxy / GitHub | mature (v1.6.0) | github.com/google/uuid | n/a | Approved — already in go.mod (Phase 2) |
| `go.uber.org/goleak` | Go proxy / GitHub | mature (v1.3.0) | github.com/uber-go/goleak | n/a | Approved — already in go.mod |

**Packages removed due to slopcheck [SLOP]:** none. **Flagged [SUS]:** none. All dependencies are well-known CNCF/Google/Uber Go modules verified against the Go module proxy with matching VCS origins. slopcheck targets npm/PyPI hallucination vectors; these are Go modules resolved cryptographically via `go.sum`, not a hallucination surface. No new LLM-client dependency is introduced — the client itself is pure stdlib.

## Architecture Patterns

### System Architecture Diagram

```
 stdin (aura chat REPL turn)
        │
        ▼
 cmd/aura chat ──── mint session_id (ThreadID, UUIDv7, once/session) ─── two-stage Ctrl+C handler
        │                                                                   │ (signal.NotifyContext)
        ▼                                                                   ▼
 internal/agent.LlmAgent.Run(InvocationContext)  ◄──── ic.Ctx (cancellable) wrapped context.WithTimeout(120s)
        │
        │  loop:
        │   1. budget.ConsumeStep()  ── tripped? ─► yield(terminalEvent{reason}) ; return   (Event, never error)
        │   2. tracer.Start(ic.Ctx, "llm.request") → span (mint SpanID full-tree)
        │   3. build llm.Request{Model, Messages(read-only), Tools=registry.Render(), Temp, MaxTokens}
        │   4. client.Stream(callCtx, req) ──────────────────┐
        │                                                     ▼
        │                              internal/llm/openai_compat
        │                              ┌──────────────────────────────────────────────┐
        │                              │ http.NewRequestWithContext(callCtx, POST, …)  │
        │                              │  Authorization: Bearer <key>  (set here ONLY) │
        │                              │  HTTP-Referer / X-Title / Accept: text/event… │
        │                              │  body: provider{data_collection:deny}, stream │
        │                              │   ─► 1 goroutine: bufio.Reader.ReadString('\n')│
        │                              │       skip ":" comments, break "[DONE]",       │
        │                              │       json.Unmarshal each "data:" → wireChunk  │
        │                              │       accumulate tool_calls BY INDEX           │
        │                              │       capture final usage chunk                │
        │                              │   ─► push llm.Chunk into <-chan ; close on EOF/│
        │                              │       cancel/[DONE]                            │
        │                              └──────────────────────────────────────────────┘
        │   5. consume(ch): stream chunk Events; extract text_response.text incrementally
        │   6. span.SetAttributes(model, prompt_tokens, completion_tokens, cache_hit_tokens, request_id) ; span.End()
        │   7. no tool calls → final Event (text + usage) ; return
        │      else: append assistant{tool_calls} ; for each call sequentially:
        │            runTool(callCtx, call) ─► tools.NewResult(ctx, content)
        │                                        ├─ ≤2048B → Preview only (no disk)
        │                                        └─ >2048B → Preview+footer in history
        │                                                     + full bytes ─► sidecar
        │                                                       $AURA_RUN_DIR/conversations/<ThreadID>/<tool_call_id>.result
        │            append tool result (RoleTool, ToolCallID) ; loop
        ▼
 yield Events → REPL renders prose (token-by-token) + cost footer ; OTel span → OTLP localhost:4317 (silent-drop)
```

### Recommended Project Structure
Per AI-SPEC §3 (authoritative). New: `internal/llm/{config.go,prices.go}`, `internal/llm/openai_compat/{client.go,sse.go,accumulate.go,httperror.go,testdata/}`, `internal/agent/{llm_agent.go,prompt.go,tracing.go}`, `internal/agent/tools/{result.go,read_tool_output.go,current_time.go}` (+ edits to `spec.go`,`text_response.go`,`search.go`), `internal/config/config.go` edit (+`LLM`+`AURA_OTEL_*`), `cmd/aura/main.go` (+`chat`,`config`), `scripts/llm_smoke.sh`.

### Pattern 1: ctx-cancel through net/http (the ~100ms teardown)
**What:** Total timeout + Ctrl+C ride the *request context*, never `http.Client.Timeout`.
**When to use:** Always, for the SSE stream.
**Verified:** `[CITED: pkg.go.dev/net/http]` "The Client cancels requests to the underlying Transport as if the Request's Context ended … the timer remains running after Do return and will interrupt reading of the Response.Body." Confirms: a ctx canceled mid-stream makes `resp.Body.Read` return promptly (`context.Canceled`), the `bufio.Reader.ReadString` unblocks, the goroutine returns, the channel closes. `defer resp.Body.Close()` frees the connection.
```go
// Source: 03-AI-SPEC §3 (verified against pkg.go.dev/net/http)
ctx, cancel := context.WithTimeout(ic.Ctx, time.Duration(cfg.TotalTimeoutSec)*time.Second)
defer cancel()
req, _ := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/chat/completions", body)
client := &http.Client{Transport: &http.Transport{
    DialContext: (&net.Dialer{Timeout: time.Duration(cfg.ConnectTimeoutSec) * time.Second}).DialContext,
}} // NO client.Timeout — that would abort a long healthy stream (Pitfall #2)
```

### Pattern 2: tool_call delta accumulation by index
**What:** Tool-call fragments arrive split across SSE chunks; merge by `delta.tool_calls[].index`.
**Wire reality:** `[CITED: OpenAI streaming function-calling]` each streamed `tool_calls` element carries an `index` (0,1,…); `id` and `function.name` appear in the first fragment for an index, `function.arguments` accumulates across subsequent fragments. **Note:** the existing `llm.ToolCall` struct (`client.go:34`) has NO `Index` field and no `arguments`-only delta shape — the accumulator needs a private wire-delta struct that *carries* `index` + partial `function.{name,arguments}`, accumulates into a `map[int]*accumulator`, then emits a finalized `llm.ToolCall` when the call closes. Do NOT add `Index` to the public `llm.ToolCall` (it's history-shaped, not wire-delta-shaped).
```go
// private to openai_compat; mirrors the streaming delta, NOT the history ToolCall
type toolCallDelta struct {
    Index    int    `json:"index"`
    ID       string `json:"id"`
    Type     string `json:"type"`
    Function struct{ Name, Arguments string } `json:"function"`
}
```

### Pattern 3: ToolResult preview/sidecar via ctx-injected ids (D-25)
**What:** One shared `tools.NewResult(ctx, content)` does cap→preview→(maybe)sidecar uniformly; the agent injects `session_id`+`tool_call_id`+`run_dir` into ctx before each `Execute`.
**UTF-8 boundary safety (the byte-accurate 2048 cap):** truncating at byte 2048 can split a multi-byte rune. Back the cut off to the last full rune so the preview is valid UTF-8:
```go
// truncatePreview returns content truncated to at most capBytes, on a rune boundary.
func truncatePreview(content string, capBytes int) string {
    if len(content) <= capBytes { return content }
    cut := capBytes
    for cut > 0 && !utf8.RuneStart(content[cut]) { cut-- } // back off into the rune
    return content[:cut]
}
```
Footer pointer format (D-27, byte-based): `\n\n[output truncated: showing bytes 0-2000 of 51234; read more via read_tool_output(tool_call_id="…", offset=2000, limit=2048)]`. Sidecar holds the **full** bytes (not the preview). On sidecar write failure (D-29): return preview + `[full output unavailable: <reason>]`, turn continues.

### Pattern 4: incremental text_response extractor (D-13, ~30 LOC)
**What:** Stream the `"text":` value out of the accumulating tool-call `arguments` JSON, token-by-token, never showing raw JSON. Stateful scanner (`seekKey` → `inValue`), handles `\"`/`\\`/`\n` escapes, stops at the closing unescaped quote. **Structural scan, not regex over prose** (`feedback_no_regex_for_nlp`). Code in AI-SPEC §4b.

### Anti-Patterns to Avoid
- **`bufio.Scanner` for SSE** — 64KB token cap silently truncates large tool-arg deltas. Use `bufio.Reader.ReadString('\n')` (D-17). SPEC-locked, single most important wire decision.
- **`http.Client.Timeout` for total timeout** — aborts a healthy long stream. Use `context.WithTimeout` on the request (D-19).
- **Live clock / tool-name enumeration in `messages[0]`** — busts the ~80% prompt cache, silently ~5× cost. Time only via `current_time` tool (D-08/D-09).
- **`json.Unmarshal`-ing the `[DONE]` sentinel or `:` comment lines** — both are non-JSON; skip before the parse (D-17).
- **Adding `Index` to the public `llm.ToolCall`** — it's a wire-delta concern; keep it private to the accumulator.
- **Reporting `$0` for an unknown model** — report `n/a` (D-23). `$0` is a lie; `n/a` is honest.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Tracing / span batching / OTLP wire | Custom span emitter | OTel SDK v1.44.0 `WithBatcher` + exporters | Battle-tested, the project's chosen observability standard |
| UUIDv7 session ids | Custom id gen | `github.com/google/uuid` (already present) | Phase-2 decision; ThreadID shape forward-compat to Phase 4 conv_id |
| goroutine-leak assertions | Manual `runtime.NumGoroutine` polling only | `go.uber.org/goleak` `VerifyTestMain` (+ NumGoroutine for the ≤100ms timing) | goleak filters runtime/test framework goroutines correctly |
| SSE framing / `[DONE]` / comment skip | (this IS the hand-rolled part — SPEC-locked) | — | The deliberate exception: the wire parser is the phase. But it's ~280 LOC of *parsing*, not protocol invention |

**Key insight:** The handrolled scope is *exactly* the SSE parse + accumulator + cancel + spillover. Everything around it (tracing, ids, leak detection, JSON) uses stdlib or the existing dep set. Do not hand-roll an HTTP retry/backoff layer — the wire does **zero** retries by design (Req#4); retries are the loop (error tool-result → model self-corrects).

## Common Pitfalls

### Pitfall 1: bufio.Scanner 64KB truncation
**What goes wrong:** A batched tool call with large `arguments` JSON in one SSE line exceeds `bufio.MaxScanTokenSize` (64 KiB); `Scanner` returns `ErrTooLong` or drops bytes, corrupting the tool call.
**How to avoid:** `bufio.NewReader(resp.Body).ReadString('\n')` (unbounded growth). **Warning sign:** a `toolcall_multichunk.sse` fixture whose single line >64KB must be in the test set (D-32 mandates it stress past the Scanner cap).

### Pitfall 2: http.Client.Timeout kills a healthy stream
**What goes wrong:** `Client.Timeout` is wall-clock through body-read; 120s aborts a healthy 130s generation. **How to avoid:** total timeout on request ctx via `context.WithTimeout`; connect timeout on `Transport.DialContext`. No idle/first-byte timeout.

### Pitfall 3: Authorization header leaking
**What goes wrong:** Key value reaches a log/span/error/Event. **How to avoid:** set `Authorization` only at request-build; never log the request struct; never add an `api_key` span attr; `HTTPError.Body` is the *provider's* body (safe). Dedicated anti-leak test (release-blocking gate, D-28).

### Pitfall 4: cost dishonesty on first-turn cache write (DELTA — new this research)
**What goes wrong:** `usage.prompt_tokens_details` has BOTH `cached_tokens` (cache reads) AND `cache_write_tokens` (cache writes, on the first request establishing the entry). Reading only `cached_tokens` for a "cache-hit ratio" makes the first turn look like 0% cache (correct) but conflating the two would misreport. **How to avoid:** OTel `llm.cache_hit_tokens` = `cached_tokens` specifically. The USD figure is `usage.cost` (which already nets reads/writes/inference) — never recompute USD from token sub-fields when `usage.cost` is present. **Verified:** `[VERIFIED: OpenRouter usage-accounting docs]` usage object = `{completion_tokens, completion_tokens_details:{reasoning_tokens}, cost, cost_details:{upstream_inference_cost}, prompt_tokens, prompt_tokens_details:{cached_tokens, cache_write_tokens, audio_tokens}, total_tokens}`, delivered in the **last SSE message**.

### Pitfall 5: OTLP exporter must silent-drop without a collector
**What goes wrong:** Dev has no collector on `localhost:4317`; fail-fast makes tracing a hard dev dep, auto-fallback-to-stdout pollutes the REPL stream. **How to avoid:** `otlptracegrpc` retries in background, errors at debug level only — that's correct. `AURA_OTEL_EXPORTER=none` = true no-op (D-05/D-06).

### Pitfall 6: leaking the read goroutine on cancel
**What goes wrong:** Consumer stops draining the channel early while the sender blocks on send. **How to avoid:** drain-to-close OR `select` the send on `ctx.Done()`. Because the request carries the cancellable ctx, the read unblocks ~100ms; `goleak.VerifyTestMain` + cancel-mid-stream test guard it (Req#3).

### Pitfall 7: stale `-run` test names = false-green
**What goes wrong:** A renamed test left in a `-run` filter yields `[no tests to run]` reported as PASS (project memory: `reference_validate_phase_procedure`). **How to avoid:** verify execution count, not just PASS; sub-second "integration" runtime is a skip tell.

## Code Examples

### OTel span per LLM call (verified attrs)
```go
// Source: 03-AI-SPEC §4 + W3C trace-context (TraceID 16B / SpanID 8B)
callCtx, span := a.tracer.Start(ic.Ctx, "llm.request")
// ... stream ...
span.SetAttributes(
    attribute.String("llm.model", a.cfg.Model),
    attribute.String("llm.provider", a.cfg.Provider),
    attribute.Int("llm.prompt_tokens", usage.PromptTokens),
    attribute.Int("llm.completion_tokens", usage.CompletionTokens),
    attribute.Int("llm.cache_hit_tokens", usage.CachedTokens), // == prompt_tokens_details.cached_tokens
    attribute.String("aura.request_id", a.requestID),
)
span.End() // NEVER an api_key attr (D-28)
```

### OpenRouter request body (verified fields)
```jsonc
{
  "model": "deepseek/deepseek-v4-flash:exacto",
  "messages": [...],
  "tools": [...],
  "tool_choice": "auto",
  "temperature": 0.7,
  "max_tokens": 4096,
  "stream": true,
  "provider": { "data_collection": "deny" }
}
// Headers: Authorization: Bearer <key>, Content-Type: application/json,
//          Accept: text/event-stream, HTTP-Referer: <repo URL>, X-Title: Aura
// usage:{include:true} / stream_options:{include_usage:true} are DEPRECATED no-ops — do NOT send them.
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `usage:{include:true}` / `stream_options:{include_usage:true}` to opt into usage | Usage always included automatically in every response | OpenRouter (current) | `[VERIFIED: OpenRouter docs]` Do NOT send these params — they're deprecated no-ops |
| Default cost-favoring provider routing | `:exacto` curated tool-calling-quality routing | OpenRouter, Oct 2025 (`:exacto` launch) | `[VERIFIED: openrouter.ai/announcements]` +10-20% on Tau2Bench/LiveMCPBench; the locked model id uses it |
| Truncation-only for large tool output | preview + pointer + artifact (sidecar) | 2025-26 industrial standard | `[CITED: arXiv 2511.22729]` validates the ToolResult pattern |
| OTel sdk on v0.x | unified v1.44.0 train for traces | 2026-05-27 | `[VERIFIED: Go proxy]` traces stack all v1.44.0; v0.x refs are the metric/log split |

**Deprecated/outdated:**
- `usage:{include:true}` request param — gone, automatic now.
- Line-based `read_tool_output` (SPEC Req#7 text) — superseded by byte-based (CONTEXT Amendment A4 / D-27).

## Runtime State Inventory

Not a rename/refactor/migration phase — greenfield implementation against an existing skeleton. The one *signature* migration (`Tool.Execute (string,error)→(ToolResult,error)`) is a code change touching `spec.go`+`text_response.go`+`search.go`+the agent dispatch path in one coupled commit (SPEC Constraint); there is no stored data, no live service config, no OS-registered state to migrate (no LlmAgent or sidecar exists yet). **None — verified by inspecting `internal/agent/tools/{spec.go,text_response.go}` (current `Execute` returns `string`) and the absence of any `internal/llm/openai_compat` package or `conversations/` sidecar directory.**

## Common Pitfalls — see "Common Pitfalls" section above.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | DeepSeek-V4-Flash list price ≈ $0.0983/1M input, $0.1966/1M output (OpenRouter standard); cache-hit input ≈ $0.0028/1M, cache-miss input ≈ $0.14/1M, output ≈ $0.28/1M (DeepSeek native) | Cost computation / D-23 seed | The OpenRouter and native-DeepSeek numbers differ (OpenRouter is the billing surface). Seed the A3 table from the **OpenRouter model page** at commit time, not the native DeepSeek page. Wrong seed only affects the *fallback* path — `usage.cost` is always preferred. `[ASSUMED]` — verify exact numbers on openrouter.ai/deepseek/deepseek-v4-flash the day of commit |
| A2 | OpenRouter credits are 1:1 USD (so `usage.cost` ≈ USD) | Cost honesty / D-18 | If a non-1:1 promo applies, the footer USD is off. Docs call the value "credits"; the standard plan is 1 credit = 1 USD. Surface `usage.cost` as-is; label honestly. `[ASSUMED]` — re-confirm in OpenRouter billing docs if a promo ratio ever applies |
| A3 | The 80% prompt-cache discount (project memory) is *implicit* for DeepSeek-V4-Flash via OpenRouter (no explicit `cache_control` markers needed for this model) | Cache-prefix stability / D-08 | If the model needs explicit cache breakpoints, the byte-stable prefix alone won't trigger caching. DeepSeek uses automatic/implicit context caching natively. `[ASSUMED]` — the smoke test's `usage.prompt_tokens_details.cached_tokens` on turn ≥2 confirms it empirically |

**These three assumptions are all on the cost/cache *reporting* path, not the core wire/loop logic.** They affect the fallback price table and the cache-ratio metric, never correctness of streaming, cancellation, tool dispatch, or redaction. The smoke test (Req#11) and the usage-chunk fixtures empirically confirm them.

## Open Questions

1. **Does DeepSeek-V4-Flash:exacto's actual SSE delta framing match the golden fixtures byte-for-byte?**
   - What we know: OpenRouter speaks the OpenAI wire; usage object shape verified; `:exacto` only changes provider selection, not wire shape.
   - What's unclear: whether a specific provider behind `:exacto` emits tool-call argument deltas split differently (e.g., whole-arg in one chunk vs fragmented) or sends extra keep-alive comment formats.
   - Recommendation: capture one real SSE transcript during the manual smoke (`scripts/llm_smoke.sh`) and diff against `toolcall_multichunk.sse`; refresh the fixture if the framing differs (AI-SPEC §6 offline flywheel "golden-fixture drift" already plans for this).

2. **Should the cost footer expose cache-write tokens on the first turn?** (the delta from Pitfall 4)
   - What we know: `cache_write_tokens` exists; turn 1 is all writes, turns ≥2 show reads.
   - Recommendation: footer stays `· {tok} ({in}/{out}) · ${usd} · {lat}s` (D-11, locked) using `usage.cost`; do NOT add cache columns to the footer (keeps it compact). Capture `cached_tokens` in the span only. Defer any cache-write surfacing to Phase 6 (KV builder owns cache observability).

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | build/test | ✓ | 1.26.3 | — |
| OTel modules v1.44.0 | tracing | ✓ (Go proxy) | v1.44.0 | none needed |
| `go.uber.org/goleak` | cancellation tests | ✓ (go.mod) | v1.3.0 | — |
| OpenRouter API + `OPENROUTER_API_KEY` | manual smoke (Req#11) ONLY | ✗ in CI (by design) | — | Deterministic tier covers all logic; smoke is local-only |
| OTLP collector on localhost:4317 | live trace export (optional) | ✗ in dev (expected) | — | Silent-drop (D-05); `none`/`stdout` exporter modes; in-memory recorder in tests |

**Missing dependencies with no fallback:** none for CI. **Missing dependencies with fallback:** OpenRouter (smoke is local/manual, NOT a CI gate — this is the no-skip-as-green-compliant design; the deterministic tier genuinely exercises every parse/loop/redaction path); OTLP collector (silent-drop / in-memory recorder).

## Validation Architecture

> Nyquist enabled (no `workflow.nyquist_validation:false` found). This section drives `03-VALIDATION.md`.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `httptest` + `go.uber.org/goleak` v1.3.0 + OTel `tracetest` in-memory recorder (v1.44.0) |
| Config file | none (Go convention); golden fixtures in `internal/llm/openai_compat/testdata/*.sse` |
| Quick run command | `go test ./internal/llm/... ./internal/agent/...` |
| Full suite command | `go test -race ./internal/llm/... ./internal/agent/...` (+ `make quality-full` for coverage gate) |

### Phase Requirements → Test Map
| Req | Behavior | Test Type | Automated Command | File Exists? |
|-----|----------|-----------|-------------------|--------------|
| Req#1 | SSE parse: text deltas + `finish_reason=stop` | unit (golden) | `go test ./internal/llm/openai_compat/ -run TestStream_TextStop` | ❌ Wave 0 (+`text_stop.sse`) |
| Req#2 | tool_call delta accumulation by index | unit (golden, property) | `go test ./internal/llm/openai_compat/ -run TestAccumulate` | ❌ Wave 0 (+`toolcall_multichunk.sse` >64KB) |
| Req#3 | ctx-cancel ≤100ms, zero goroutine leak | unit + `-race` + goleak | `go test -race ./internal/llm/openai_compat/ -run TestStream_CancelMidStream` | ❌ Wave 0 (+`premature_close.sse`) |
| Req#4 | 429 → `HTTPError`, request count==1, no retry | unit (golden) | `go test ./internal/llm/openai_compat/ -run TestStream_429NoRetry` | ❌ Wave 0 (+`error_429.sse`) |
| Req#5 | config load-order default<file<env; empty key clean error | unit | `go test ./internal/llm/ -run TestConfigLoadOrder` | ❌ Wave 0 |
| Req#6 | ToolResult preview/sidecar (100KB→≤2KiB+sidecar; ≤cap→no file) | unit (property for UTF-8 boundary) | `go test ./internal/agent/tools/ -run TestNewResult` | ❌ Wave 0 |
| Req#7 | `read_tool_output` byte slice; unknown id hard-fails | unit | `go test ./internal/agent/tools/ -run TestReadToolOutput` | ❌ Wave 0 |
| Req#8 | sidecar at `conversations/<session_id>/<tool_call_id>.result` | unit (filesystem assert) | `go test ./internal/agent/tools/ -run TestSidecarLayout` | ❌ Wave 0 |
| Req#9 | LlmAgent ordered Events (chunk→tool_call→tool_result→final); race+goleak | unit (fake Client) | `go test -race ./internal/agent/ -run TestLlmAgent_EventOrder` | ❌ Wave 0 |
| Req#10 | budget step/wallclock/dedup → terminal Event, steps≤cap | unit (fake Client) | `go test ./internal/agent/ -run 'TestLlmAgent_(StepCap|WallclockCap|DedupWindow)_Trips'` | ❌ Wave 0 |
| Req#11 | `aura chat` 2-turn shared session_id, in-memory history; missing key clean error | unit (scripted stdin) + **manual smoke** | `go test ./cmd/aura/ -run TestChat` ; `OPENROUTER_API_KEY=… ./scripts/llm_smoke.sh` | ❌ Wave 0 |
| Req#12 | cost: `usage.cost` present→exact USD; absent→table; unknown→`n/a` | unit (3 golden usage fixtures) | `go test ./internal/llm/... -run TestCost` | ❌ Wave 0 |
| Req#13 | exactly 1 `llm.request` span/call, all attrs, stable span_id; `req.Messages` byte-identical pre/post | unit (in-memory recorder) | `go test ./internal/agent/ -run 'TestSpan|TestMessagesImmutable'` | ❌ Wave 0 |
| Req#14 | `current_time` RFC-3339 UTC + IANA tz; `messages[0]` no timestamp & byte-stable across turns; Events non-zero UTC | unit | `go test ./internal/agent/... -run 'TestCurrentTime|TestPrefixStable'` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./internal/<touched-package>/` + `go vet ./...` + `go build ./...` (Gate 2).
- **Per wave merge:** `go test -race ./internal/llm/... ./internal/agent/... ./cmd/aura/...` + goleak.
- **Phase gate:** `make quality-full` green (owned-surface coverage ≥85% across the full tag matrix — overrides PRD 75/60 per CLAUDE.md), then manual `scripts/llm_smoke.sh` for the Req#11 live-prose + non-zero cost-footer acceptance, then `/gsd-verify-work`.

### Property-Based Tests (where applicable)
- **UTF-8 preview truncation (Req#6):** `rapid`/`gopter` — for any string + any cap, `truncatePreview` output is valid UTF-8, ≤ cap bytes, and a prefix of the input. (project skill `property-based-testing`.)
- **tool_call accumulation (Req#2):** for any partition of a well-formed tool-call's bytes into ≥1 SSE deltas, accumulation yields the same single `ToolCall` whose `arguments` parse as JSON.

### Wave 0 Gaps
- [ ] `internal/llm/openai_compat/testdata/{text_stop,toolcall_multichunk,error_429,premature_close,length_truncation}.sse` — golden fixtures (authored *with* the parser, commit 3 of D-01; `text_stop.sse` includes a `: OPENROUTER PROCESSING` comment + `[DONE]` + trailing usage chunk; `toolcall_multichunk.sse` single line >64KB)
- [ ] `internal/llm/openai_compat/*_test.go` + `internal/agent/llm_agent_test.go` + `cmd/aura/chat_test.go`
- [ ] `goleak.VerifyTestMain(m)` in `internal/llm/openai_compat/main_test.go` and `internal/agent/main_test.go` (if not already present from Phase 2)
- [ ] Reuse `internal/agent/agenttest/` mocks for the fake `llm.Client` (Phase 2 D-07 — no mock duplication)
- [ ] `scripts/llm_smoke.sh` — manual, gated on `OPENROUTER_API_KEY`, NOT a CI job
- [ ] 3 usage-chunk variants as inline fixtures for Req#12 (cost-present / cost-absent / unknown-model)

**No-skip-as-green compliance:** the deterministic tier (golden fixtures + httptest + fake Client + in-memory OTel recorder) genuinely executes every parse/accumulate/cancel/budget/redaction/cost path with **no** network and **no** `t.Skip`. The only network-gated test is the manual smoke, which is explicitly NOT in CI — nothing in CI pretends to hit a live LLM (AI-SPEC §5 / D-31).

## Security Domain

> `security_enforcement` not set to false → included.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes (API key to OpenRouter) | Bearer token set only at request-build; never logged (D-28) |
| V3 Session Management | minimal (in-memory session_id only) | UUIDv7 ephemeral; no auth session |
| V4 Access Control | no (single-operator REPL; capability_grants are Phase 4) | — |
| V5 Input Validation | yes | typed struct + `json.Unmarshal` + explicit field validation (no Pydantic in Go, AI-SPEC §4b); malformed tool args → error tool-result, never panic |
| V6 Cryptography | yes (SpanID gen) | `crypto/rand` 8-byte SpanID (D-04); never hand-roll |
| V7 Error Handling / Logging | yes (the critical one) | structural secret redaction; `slog` request_id-correlated, no secrets; anti-leak test is a release-blocking gate (D-28/D-30) |

### Known Threat Patterns for a handrolled LLM client
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| API key leak in log/span/error/Event | Information Disclosure | Structural redaction + dedicated anti-leak test (Critical Failure Mode #1, D-28) — release-blocking |
| Provider prompt retention / training on user data | Information Disclosure | `provider.data_collection:"deny"` + no prompt-content logging (D-20, GDPR/SMB privacy posture) |
| Runaway tool loop burning spend | Denial of Service (cost) | Budget tree: step/wallclock/dedup → terminal Event (Req#10, Critical Failure Mode #6) |
| Malformed tool args crashing the loop | Denial of Service | parse error → error tool-result the model self-corrects from; never a panic (D-15) |
| Goroutine/connection leak compounding across turns | Resource exhaustion | ctx-cancel teardown + goleak gate (Req#3, Critical Failure Mode #2) |
| KV-cache prefix poisoning silently ~5× cost | (cost integrity) | byte-stable `messages[0]`, no live clock; `current_time` tool only (D-08/D-09) |

## Sources

### Primary (HIGH confidence)
- Go module proxy `proxy.golang.org/go.opentelemetry.io/otel*/@latest` — all four OTel modules v1.44.0, 2026-05-27, commit b62d9283 (verified this session).
- pkg.go.dev/net/http — ctx-cancel interrupts `Response.Body` read; close body to free resources (verified this session).
- OpenRouter Usage Accounting docs (openrouter.ai/docs/cookbook/administration/usage-accounting) — exact usage object shape, last-SSE-message delivery, deprecated opt-in params (verified this session).
- OpenRouter Exacto announcement (openrouter.ai/announcements/provider-variance-introducing-exacto) + `:exacto` docs — curated tool-calling-quality routing, +10-20% bench (verified this session).
- openrouter.ai/deepseek/deepseek-v4-flash — model id + standard pricing (verified this session).
- `03-AI-SPEC.md`, `03-SPEC.md`, `03-CONTEXT.md` — the locked design contract (primary implementation reference).
- `internal/llm/client.go`, `internal/agent/agent.go`, `internal/agent/tools/spec.go`, `internal/config/config.go` — existing integration points (read this session).

### Secondary (MEDIUM confidence)
- OpenRouter Prompt Caching docs + DeepSeek pricing aggregators — cache-hit pricing numbers (cross-checked, but seed the table from the live OpenRouter model page at commit time — A1).
- arXiv 2511.22729 — preview+pointer+artifact tool-output pattern (cited in SPEC, validates ToolResult).

### Tertiary (LOW confidence)
- Project memory `reference_openrouter_provider_capabilities_2026-05-27` (80% cache claim — A3, confirm empirically via smoke).

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — OTel pin verified on Go proxy; all deps already in go.mod or stdlib.
- Architecture: HIGH — entry-point skeleton + Run loop verified against net/http + existing interfaces; one delta (Run takes InvocationContext) surfaced.
- Wire shape: HIGH — OpenRouter usage object + `[DONE]`/comment framing + `:exacto` all verified against current OpenRouter docs.
- Pitfalls: HIGH — every pitfall cross-checked against stdlib docs or OpenRouter docs; one new pitfall (cache_write_tokens) discovered.
- Cost numbers: MEDIUM — pricing is a moving target; seed from the live model page at commit (A1/A2).

**Research date:** 2026-05-30
**Valid until:** 2026-06-29 (30 days; OpenRouter pricing/wire and OTel are stable, but reconfirm the price-table seed at commit time).
