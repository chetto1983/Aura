# openai-go vs `internal/llm` — BUILD-vs-REUSE inventory (2026-08-29)

**Inventoried:** `github.com/openai/openai-go/v3` **v3.54.0** (go.mod:30 pins it; module cache
`C:\Users\chett\go\pkg\mod\github.com\openai\openai-go\v3@v3.54.0`; clone `D:\tmp\openai-go` at
`4d06294` 2026-08-27 = `internal/version.go` `PackageVersion = "3.54.0"`, CHANGELOG.md:3 dated
2026-08-27). SDK requires **Go >= 1.25** (`go.mod` `go 1.25.0`; README.md:40-42: v3.45.0+ needs
1.25, GO_VERSION_POLICY.md: minor releases may raise the floor). Aura `go.mod:3` is `go 1.26.6` — OK.
All openai-go `file:line` below are relative to `D:\tmp\openai-go`; all Aura ones to `D:\Repo\Aura`.

## 0. The premise is two days stale — the replacement already happened

Commit **`f874d52e7` (2026-08-27, on `origin/master`)** "Replace Aura's bespoke OpenAI SSE/request
accumulator with the pinned official openai-go client" deleted `internal/llm/openai_compat/sse.go`
(194 LOC), `accumulate.go` (100), `usage.go` (56), `internal/llm/models.go` (53) and their tests
(sse_test 435, accumulate_test 193, models_test 85, client_reasoning_wire_test 182,
client_max_tokens_wire_test 53) and rewrote `client.go` (392 → 112 LOC). The only files that
import openai-go are `internal/llm/openai_compat/{client,request,response,stream_idle,httperror}.go`.
So this report is not "should we adopt the SDK" but "what is still bespoke, and is any of it
covered by the SDK we already ship".

**LOC today.** `internal/llm` root non-test **2,034** / test 1,845; `internal/llm/openai_compat`
non-test **712** / test 1,655 → **2,746 non-test, 3,500 test**. Per file (non-test): config.go 579,
model_reasoning_caps.go 311, client.go 205, llamacpp_caps.go 177, pricing_source.go 141,
model_content_caps.go 129, content_projection.go 113, spend.go 91, breaker.go 82, capabilities.go 78,
prices.go 75, reasoning_target.go 45, internal_context.go 8; openai_compat: request.go 277,
response.go 128, stream_idle.go 121, client.go 112, httperror.go 74.

**Blast radius.** `"github.com/chetto1983/aura/internal/llm"` is imported by **99 non-test + 200 test
files across 18 packages** (internal/agent 30, runner 13, conversations 12, agui 10, cmd/aura 9,
agent/prompt 4, swarm 3, share 3, openai_compat 3, cron/handlers 2, channels/telegram 2,
agent/agenttest 2, steer/config/agentrender/agenteval/agent/workflow/agent/tools 1 each) plus 5
spike sources under `.claude/skills/spike-findings-Aura/sources/`. `openai_compat` itself has only
**2** non-test importers: `cmd/aura/llm_client.go:11,27` and
`internal/agent/llm_agent_stream_retry.go:12,91,106,154`. Everything else targets the neutral
`llm.Client` interface (`internal/llm/client.go:103-105`) and the `llm.Chunk`/`Request`/`Config` types.

## 1. What `internal/llm` implements today (post-`f874d52e7`)

| Capability | Where (file:line) | Status |
|---|---|---|
| Request building — **Chat Completions only**, no Responses API | `openai_compat/request.go:18-63` builds `openai.ChatCompletionNewParams` (Model/Messages/Tools/Temperature/ToolChoice/MaxTokens/TopP/PresencePenalty); messages `:84-149`; tools `:188-204` | SDK types |
| SSE parsing | `openai_compat/client.go:89` `c.chat.NewStreaming(...)` → `ssestream.Stream[ChatCompletionChunk]`; loop `response.go:27-49` | SDK |
| Tool-call delta accumulation | `response.go:24-29` `openai.ChatCompletionAccumulator.AddChunk`; tool calls emitted **only after the stream ends** from `accumulator.Choices[0].Message.ToolCalls` `:58-71` (not incrementally via `JustFinishedToolCall`) | SDK + 14 LOC glue |
| Reasoning/thinking deltas | `response.go:92-104` `sdkReasoningDelta` reads `delta.JSON.ExtraFields["reasoning"]` (OpenRouter) and `["reasoning_content"]` (llama.cpp/vLLM/DeepSeek); emitted as `llm.Chunk{Reasoning}` `:38`; fixtures `testdata/reasoning-field.txt`, `reasoning-content-field.txt` | bespoke (13 LOC) |
| Usage accounting | `response.go:45-48` on `chunk.JSON.Usage.Valid()`; `sdkUsage :106-119` maps prompt/completion/`prompt_tokens_details.cached_tokens` + OpenRouter `usage.cost` from `ExtraFields["cost"]`; usage chunk emitted last `:78-82`; `include_usage` set **only** on the llama.cpp target `request.go:222,232` | bespoke (14 LOC) |
| Stream-end contract | `client.go:19` `errStreamMissingFinishReason` (stream w/o finish_reason is an error) `response.go:87-89`; malformed JSON → `adaptStreamError :121-128` | bespoke |
| Retries/backoff | **SDK retries disabled** `client.go:50` `option.WithMaxRetries(0)`. Agent-layer stream-OPEN retry: 2 attempts, 750 ms, Retry-After honoured but capped 5 s `internal/agent/llm_agent_stream_retry.go:16-20,22-81,153-163`; classifier `retryableStreamOpenError :83-118` (context.* never retried; 429/5xx; url.Error timeout; idle-timeout; typed net sentinels `llm_agent_retry.go:216-227`; substring table `:135-151`). No mid-stream retry (`llm_agent_consume.go:27` surfaces `Chunk.Err`). | bespoke, above the client |
| Circuit breaker | `breaker.go:17-20` 3 failures / 30 s cooldown; used at `llm_agent_stream_retry.go:33-37,63-65` | bespoke |
| Timeouts | `AURA_LLM_TOTAL_TIMEOUT_SEC` (120) applied as `context.WithTimeout` by callers `internal/agent/llm_agent.go:315`, `llm_agent_completion.go:135`, `llm_agent_finalize.go:229`, `internal/runner/runner_context.go:59`; `AURA_LLM_CONNECT_TIMEOUT_SEC` (10) → dialer `openai_compat/client.go:40-43`; `AURA_LLM_STREAM_IDLE_TIMEOUT_SEC` (60, B-08) → per-read watchdog `stream_idle.go:33-53,69-121` armed by `option.WithMiddleware(idleResponseMiddleware)` `client.go:51`; env names `config.go:82-84`, defaults `:24-32`, validation `:209-239` | bespoke |
| Error classification | `HTTPError{StatusCode,RetryAfterSec,Body}` `httperror.go:24-28`, built from non-2xx **inside the middleware** `stream_idle.go:38-41` → `newHTTPError :63-74` (body capped 64 KiB `:18`, key never in body D-28); `ErrStreamIdleTimeout` `client.go:22`; kinds `breaker_open` / `<prefix>_canceled` / `<prefix>_deadline` / `<prefix>_retryable` `llm_agent_stream_retry.go:120-133` (so `stream_open_deadline`/`stream_open_retryable` are the prefix+suffix products) | bespoke |
| OpenRouter quirks | attribution headers `HTTP-Referer`/`X-Title` `config.go:59-60,304-307` → `option.WithHeader` `client.go:56-58`; API key sent **only** on the OpenRouter target `client.go:53-55`; `provider.data_collection=deny` + `session_id` sticky routing `request.go:51-56`; `transforms:["middle-out"]` gate `:59-61` (`AURA_LLM_OPENROUTER_MIDDLE_OUT` `config.go:97`); nested `reasoning{effort,max_tokens,exclude,enabled}` object `:235-248`; `:nitro` default model `config.go:20`; `:variant` stripped for catalogue lookups `model_reasoning_caps.go:16-22`, `pricing_source.go:44-47` | bespoke |
| llama.cpp / vLLM quirks | target detection `reasoning_target.go:34-45` (explicit `provider=llamacpp`, never URL sniffing); `thinking_budget_tokens` / `chat_template_kwargs.enable_thinking` `request.go:226-234,251-277`; keyless local hosts allowlist `cmd/aura/llm_client.go:30-45`; `/props` probe `llamacpp_caps.go:135-157` (caps + modalities `model_content_caps.go:61-91`) | bespoke |
| Sampling beyond the SDK schema | `top_k`, `min_p`, `repetition_penalty` via `option.WithJSONSet` `request.go:206-217` (llama-server honours all five, `client.go:180-183`) | bespoke |
| Prompt caching | implicit byte-stable prefix, decided in `internal/agent/prompt` (`client.go:106-113`); `Request.ToolsCacheControl` is Anthropic-only wire shape, **ignored by this client** (`client.go:133-137`; only set in `agent/prompt/cache_anthropic.go:27`, compared in `agent/hooks_model_guard.go:65`) | n/a on OpenAI wire |
| Multimodal parts | image → data-URL `ImageContentPart`, audio → `InputAudioContentPart` `request.go:151-186`; attached to the last user message only `:84-122`; authorization + digest reload `content_projection.go:96-113`; capability gate `NewContentCapabilitySource` `model_content_caps.go:113-129` | SDK types + bespoke policy |
| Model catalog / capability discovery | OpenRouter `GET /models` TTL cache `model_reasoning_caps.go:158-264` (16 MiB cap `:29`, effort allowlist `:73-80`, input modalities `:82-104`); reasoning seam `ReasoningCapabilitySource :272-274` | bespoke |
| Pricing & spend | `GET /models` price for one model `pricing_source.go:56-103` (rates are JSON strings), `ResolvePricing :123-141`; `GET /key` spend `spend.go:49-91`; cost precedence `prices.go:37-59` | bespoke (OpenRouter-only endpoints) |
| Token counting | **not in `internal/llm`** — `internal/conversations/tiktoken.go`; `internal/llm` only carries the per-backend error reserve `capabilities.go:74-78` (`AURA_MODEL_LLAMACPP_ERROR_RESERVE_TOKENS` `:38`) | bespoke, elsewhere |
| DB-driven routing | `aura.settings` rows `AURA_LLM_PROVIDER/MODEL/BASE_URL`, `OPENROUTER_API_KEY` (`internal/settings/settings.go:47-49,59`) are overlaid onto the env at daemon boot (`internal/config/config_defaults.go:3-5`), then `llm.Load()` resolves the 4-tier chain default < .env < `~/.aura/llm.json` < `AURA_LLM_*` `config.go:276-342` | bespoke |
| Neutral types | `Message/ToolCall/ToolDef/Usage/Chunk/Client/Request/Sampling/ReasoningConfig` `client.go:14-205` — the contract the other 17 packages compile against | bespoke, load-bearing |

## 2. What openai-go v3.54.0 provides (installed surface, not README)

| Capability | openai-go symbol (file:line) | Note |
|---|---|---|
| Client construction | `openai.NewClient` `client.go:108` reads `OPENAI_API_KEY/OPENAI_BASE_URL/...` via `DefaultClientOptions :68-79`; `openai.NewChatCompletionService` `chatcompletion.go:44-48` reads **no env** — Aura uses the latter (`openai_compat/client.go:62`), so no `OPENAI_*` leakage | covered |
| Base URL / headers / key | `option.WithBaseURL` `option/requestoption.go:30`, `WithHeader :141`, `WithAPIKey :300`, `WithHTTPClient :61`, `WithJSONSet :201` (arbitrary extra body keys), `WithQuery :167`, `WithResponseInto :255`, `WithDebugLog` | covered |
| Retries | `WithMaxRetries :114`, `WithMaxRetryDelay :129`; policy `internal/requestconfig/requestconfig.go:385-413` (`res==nil` → retry; `x-should-retry`; 408/409/429/5xx), delay `:539-560` (`Retry-After-Ms`/`Retry-After`, 0.5 s·2ⁿ, −25 % jitter, capped by `DefaultMaxServerDelay :28`); default 2 (README.md:892) | covered, **different semantics** (§4) |
| Timeouts | README.md:674 "Requests do not time out by default"; `WithRequestTimeout :285` = per-**retry** timeout; default transport `ResponseHeaderTimeout` 10 min `default_http_client.go:14-31` — **skipped when `WithHTTPClient` is passed** (Aura passes one) | partial |
| Middleware | `option.Middleware` / `WithMiddleware :93-102`; applied innermost-last `requestconfig.go:633-640`; a middleware error is returned **unwrapped** `:667-700` | covered (Aura's idle watchdog rides on it) |
| Streaming | `ChatCompletionService.NewStreaming` `chatcompletion.go:99` → `ssestream.Stream[T]` `packages/ssestream/ssestream.go:153-261`; decoder ignores SSE comment lines (`: OPENROUTER PROCESSING`) `:122-125`; `[DONE]` `:206`; a `data:` frame containing `"error"` → `*ssestream.StreamError` `:210-216`; scanner line buffer 32 MiB `:45` | covered |
| Accumulator | `ChatCompletionAccumulator` `streamaccumulator.go:20`, `AddChunk :140` (choice index ≤127 `:108`, sparse tool-call growth ≤128 `:115`, text/arguments **unbounded** `:118-125`, `-1` tool index treated as 0), `JustFinishedContent :318`, `JustFinishedRefusal :342`, `JustFinishedToolCall :369` (`FinishedChatCompletionToolCall{Index,ID,Name,Arguments}` `:37`), `.Usage` | covered |
| `reasoning_content` in accumulator | `grep -rn reasoning streamaccumulator*.go` = **0 hits** | NOT covered |
| Non-standard fields | `ChatCompletionChunkChoiceDelta.JSON.ExtraFields` `chatcompletion.go:1122-1128`, `RawJSON() :1134`; `CompletionUsage.JSON.ExtraFields` `completion.go:194`; `respjson.Field.Valid/Raw` `packages/respjson/respjson.go:66,72` | covered (this is how Aura reads `reasoning`, `reasoning_content`, `cost`) |
| Reasoning params | `shared.ReasoningEffort` `shared/shared.go:1063-1072` = `none/minimal/low/medium/high/xhigh/max` (same 7 tokens as `llm.ReasoningEffort` `client.go:165-171`) → **flat** `reasoning_effort` `chatcompletion.go:3942` (OpenAI shape). No nested OpenRouter `reasoning{}` object, no `thinking_budget_tokens`, no `chat_template_kwargs` | NOT covered (only via `WithJSONSet`) |
| Usage chunk | `ChatCompletionStreamOptionsParam.IncludeUsage` `chatcompletion.go:3358-3372`; `PromptCacheKey :3833`; `MaxTokens :3821` / `MaxCompletionTokens :3813` | covered |
| Sampling | `Temperature/TopP/PresencePenalty/FrequencyPenalty/Seed/N/Stop/Logprobs` in `ChatCompletionNewParams`; **no** `top_k`/`min_p`/`repetition_penalty` | partial |
| Errors | `*openai.Error` = `internal/apierror/apierror.go:15-33` (`StatusCode`, `Request`, `Response`, `RawJSON()`); `Error()` `:40-43` embeds method+URL+body; `DumpRequest(body)` `:45-51` dumps headers **including Authorization** — never call it in logs | covered, with a D-28 footgun |
| Responses API | `responses/response.go:59,78` `New`/`NewStreaming` → `ResponseStreamEventUnion`; **no accumulator** in `responses/` | available, unused by Aura |
| Multimodal parts | `openai.ImageContentPart`, `InputAudioContentPart`, `FileContentPart` (`chatcompletion.go`) | covered |
| llama.cpp / vLLM keyless | `WithBaseURL` + omit `WithAPIKey` → no `Authorization` header (`openai_compat/client.go:53-55` does exactly this; `TestStream_LlamaCppUsageChunk` `client_llamacpp_stream_test.go:75`) | covered |
| Stream idle timeout | none (`WithRequestTimeout` is whole-request per retry, not inter-byte) | NOT covered |
| Circuit breaker, DB-driven model selection, `/models` capability + price, `/key` spend, `/props`, token counting | none | NOT covered |

## 3. Gap table

| Aura capability | Verdict | Adapter / remainder |
|---|---|---|
| HTTP client, base URL, headers, key, extra JSON keys | **COVERED** | already `option.*` (`openai_compat/client.go:47-58`) |
| SSE decode incl. comment lines, `[DONE]`, in-band `{"error":..}` | **COVERED** | already `ssestream` via `NewStreaming` |
| Tool-call argument accumulation | **COVERED** | `ChatCompletionAccumulator` `response.go:24-29,58-71`; optional: switch to `JustFinishedToolCall` for per-call emission (behaviour change, not a deletion) |
| Text deltas, finish_reason, usage token fields, `cached_tokens` | **COVERED** | `response.go:33-48,106-111` |
| OpenRouter `usage.cost`, `reasoning` / `reasoning_content` deltas | **COVERED WITH ADAPTER** | `ExtraFields` reads `response.go:92-104,112-117` (27 LOC) — the SDK gives the raw field, not the semantics |
| OpenRouter nested `reasoning{}`; `provider`, `transforms`, `session_id`; llama.cpp `thinking_budget_tokens`/`chat_template_kwargs`; `top_k`/`min_p`/`repetition_penalty` | **COVERED WITH ADAPTER** | `option.WithJSONSet` (`request.go:51-61,206-248`) — an adapter by construction, ~70 LOC |
| Neutral `llm.Request/Message/ToolDef` → SDK params; SDK chunk → `llm.Chunk` | **COVERED WITH ADAPTER** | `request.go:84-204`, `response.go:15-90` (~190 LOC); the neutral types are what 18 packages compile against |
| Non-2xx → `HTTPError{StatusCode,RetryAfterSec,Body}` | **COVERED WITH ADAPTER** | live path is `newHTTPError` inside the middleware `stream_idle.go:38-41`; the `*openai.Error` branch `adaptSDKError` `httperror.go:30-48` is **unreachable** (see §4) |
| Stream-open retry with 5 s Retry-After cap, context-never-retried, breaker | **NOT COVERED** (must stay bespoke, or change semantics) | SDK retry is whole-request, exponential, up to `DefaultMaxServerDelay`, retries 408/409 too, and — with Aura's middleware — would retry every 4xx (§4) |
| `AURA_LLM_TOTAL_TIMEOUT_SEC` / `CONNECT` / `STREAM_IDLE` | **NOT COVERED** | total = caller ctx; connect = dialer; idle = watchdog `stream_idle.go` (121 LOC). SDK has no inter-byte timeout |
| `errStreamMissingFinishReason`, malformed-chunk wrapping | **NOT COVERED** | `client.go:19`, `response.go:87-89,121-128` |
| Target detection, `/models`, `/props`, pricing, spend, breaker, config chain, DB overlay, content projection, capabilities seams | **NOT COVERED** | `reasoning_target.go`, `model_reasoning_caps.go`, `llamacpp_caps.go`, `model_content_caps.go`, `pricing_source.go`, `spend.go`, `prices.go`, `breaker.go`, `config.go`, `content_projection.go`, `capabilities.go` = **1,935 LOC** with no SDK counterpart |
| Responses API | **N/A** | Aura does not use it; OpenRouter/llama.cpp reasoning + usage extras are Chat-Completions-shaped |

## 4. Fit verdict

**Verdict: REUSE is already done; what is left is bespoke because the SDK genuinely does not do it.**
Of 2,746 non-test LOC, the wire layer is 712 and every line of it is either SDK glue (`option.*`,
params, accumulator) or provider semantics the SDK does not model. **Deletable today: ~20 LOC** —
`adaptSDKError` `httperror.go:30-48` plus its `openai` import. Proof: `*openai.Error` is built only
at `requestconfig.go:735` from a response the SDK saw with status ≥400; Aura's middleware converts
every non-2xx into `*HTTPError` and returns `nil, err` (`stream_idle.go:38-41`), which the SDK
returns unwrapped (`requestconfig.go:667-700`), so `client.go:93` never receives an `*openai.Error`.
No test references `openai.Error` either. That is dark code under CLAUDE.md and should be removed
on next touch (fix-on-touch; not done here per the read-only brief).

**Must remain (1,935 LOC + ~690 LOC adapter):** everything in §3 marked NOT COVERED / WITH ADAPTER.
Two cosmetic swaps are possible but not recommended: aliasing `llm.ReasoningEffort` to
`shared.ReasoningEffort` (identical 7 tokens) saves ~15 LOC and couples 18 packages to the SDK; and
emitting tool calls via `JustFinishedToolCall` mid-stream changes observable ordering for every
consumer of `llm.Chunk.ToolCall` (agent, agui, telegram).

**Risks / behaviour changes already in force since `f874d52e7` that nobody has measured live:**
1. **SDK retries must stay at 0 while the idle middleware exists.** With `WithMaxRetries(>0)`,
   `shouldRetry(req, nil, err)` returns true for a nil response (`requestconfig.go:397-400`), so
   the middleware-converted 400/401/403 would be retried as "connection errors", and `Retry-After`
   would never be parsed (the SDK never sees the response). Any proposal to "let openai-go own
   retries" first has to move the non-2xx conversion out of the middleware.
2. **Mid-stream `{"error":...}` frames** (OpenRouter 429/5xx after headers, fixture
   `testdata/error_429.sse`) now surface as `*ssestream.StreamError` text
   `"received error while streaming: …"` (`ssestream.go:210-216`), not an `HTTPError`; the
   `_retryable` classifier does not match it (mid-stream is not retried anyway, but the kind label
   and the operator-visible message changed).
3. **`include_usage` is sent only to llama.cpp** (`request.go:222,232`); on OpenRouter Aura relies
   on the provider attaching `usage` (+`cost`) to the last chunk unasked. If a provider route omits
   it, `Usage` is nil and cost falls to the price table (`prices.go:41-43`).
4. `X-Stainless-Retry-Count`/`X-Stainless-Timeout` headers now go to OpenRouter and llama-server
   (`requestconfig.go:272-273`); harmless in theory, unmeasured.
5. The SDK's default 10-min `ResponseHeaderTimeout` does not apply (Aura passes its own transport);
   the only stream-open bound is the caller's 120 s total ctx — same as before, worth stating.
6. Dependency surface: go.sum +18 lines (`tidwall/gjson`/`sjson` et al.); SDK minor releases may
   raise the Go floor (GO_VERSION_POLICY.md) — pin bumps need a `go.mod` check each time.

**What a spike must MEASURE on the live stack before any PRD amendment (PRD-first rule):**
- OpenRouter, reasoning model, effort `high`, a prompt that thinks ≥90 s: confirm keep-alive
  comment bytes reset the 60 s idle watchdog (claimed at `stream_idle.go:66-68`, tested only with a
  fake server `stream_idle_test.go:20`) and that `reasoning` deltas stream token-by-token.
- OpenRouter: does the final chunk carry `usage` **and** `cost` without `include_usage`? Record the
  raw last frame; if absent, the amendment is "set `IncludeUsage` on every target".
- OpenRouter 429 with `Retry-After` at stream open → `HTTPError.RetryAfterSec` and the 5 s cap path
  (`llm_agent_stream_retry.go:153-163`); and a mid-stream error frame → what the operator sees.
- llama.cpp keyless (`provider=llamacpp`): `reasoning_content` deltas, `thinking_budget_tokens`
  honoured, `-1` tool index, and a tool call whose `arguments` exceeds 1 MiB through the accumulator.
- Multi-tool round (≥3 parallel calls) end-to-end through `internal/agent` to confirm end-of-stream
  tool emission preserves the previous ordering seen by agui/telegram.
- Coverage: the SDK-era tests are 14 functions across `client_sdk_stream_test.go`,
  `client_request_sdk_test.go`, `client_llamacpp_stream_test.go`, `stream_idle_test.go`,
  `httperror_test.go`; run the package with `-race` in WSL and record the `openai_compat` ratio
  against the 85 % floor before closing.

What this inventory does NOT show: any live measurement — every "covered" row is read from source,
and `f874d52e7` was merged with unit tests only. The SDK is a good fit for exactly the part it now
owns; the remaining 2,000 LOC are Aura's provider policy, and no library on the shelf provides
OpenRouter+llama.cpp reasoning/pricing/capability semantics behind a neutral interface.
