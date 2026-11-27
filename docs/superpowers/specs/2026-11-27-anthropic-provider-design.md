# Claude as a primary route: a native `anthropic` provider on the house key

- **Date:** 2026-11-27
- **Status:** Proposed.
- **Decided by the operator on 2026-11-27** in chat:
  - Aura gets Claude as a route of its own, so the chat spends the operator's Claude Console credit
    instead of OpenRouter credit ("usare il mio abbonamento Max invece di pagare a token"; the monthly
    credit that comes with the plan turned out to sit in the Console's balance, so an API key spends it).
  - Approach A, a native provider on Anthropic's Messages API ("sì, vai con A").
  - One house key for every identity, set by the admin ("1, chiave unica della casa").
- **Measurement this rests on:** §3, taken on 2026-11-27 with the operator's own key, revoked the same hour.

## 1. Why

The operator's Max plan carries a monthly Claude Console credit. Today Aura reaches Claude only
through OpenRouter (`anthropic/claude-*`), which bills OpenRouter credit. A route that sends the
operator's Console key spends the Console credit, with nobody in between, and puts prompt caching under
Aura's own control.

The subscription itself cannot be the credential. Anthropic's terms
(<https://code.claude.com/docs/en/legal-and-compliance>, "Authentication and credential use", read
2026-11-27) say: "Anthropic does not permit third-party developers to offer Claude.ai login into their own
applications, or to route requests through Free, Pro, Max, or Ultra plan credentials on behalf of their
users. Moreover, developers may not collect, store, or intermediate Claude.ai credentials or session
tokens". The same section allows "configuring an API key … for use by the customer's own authorized users
— provided the resulting usage is billed to the key owner … and is not resold or intermediated". A house
Console key, used by the operator's own identities, is that case.

## 2. Goals, non-goals, anti-goals

**Goals**
- `anthropic` is a fifth primary route beside `openrouter`, `llamacpp`, `ollama` and `chatgpt`, picked in
  Settings like the others and switched live without a restart.
- A full agent turn works on it: streamed text, tool calls across a multi-call tool loop, adaptive
  thinking with its summary streamed live, images and PDFs the turn projects, mid-turn steering, pauses.
- Prompt caching hits on the stable prefix (tools + system) and on the conversation tail.
- The model picker, the context window, the output cap and the reasoning levels come from the account's
  own `/v1/models`, never from a table typed into Aura.
- Every turn records its cost, cache reads and writes priced, or says the cost is unknown. Never $0.

**Non-goals**
- Signing in with a Claude subscription (§1). No OAuth, no `setup-token`, no Claude Code credential.
- A key per identity. Everyone spends the house key (operator decision).
- A per-identity spending cap. Aura records each identity's spend; the cap is the workspace spend limit
  the operator sets in the Console.
- Claude on Vertex AI or Bedrock.
- Moving embeddings, speech or OCR. They keep the services key (`OPENROUTER_API_KEY`) whatever the chat
  route is, as they already do on ChatGPT.
- Server tools (Anthropic's web search, code execution), citations, batches, the Files API.

**Anti-goals (failures even if it works)**
- The house key reaching any host other than `api.anthropic.com`, or the services key reaching Anthropic.
- A request that sends `temperature`, `top_p` or `top_k` (§3: a 400 on every current model).
- Thinking blocks dropped inside a tool loop. The docs say this "loses the reasoning in them, even without
  an error" — a silent quality loss no test of the happy path sees.
- A cost shown as $0 because a price was missing.
- An adapter written by hand over HTTP when the official Go SDK covers it
  (feedback: use the SDK, never bespoke HTTP).

## 3. Measured on 2026-11-27

Probe from the dev host with the operator's key (`sk-ant-usr-…`, workspace "Default"), `curl` against
`api.anthropic.com`, model `claude-haiku-5-5`. Cost: cents, from the Console credit.

| # | Call | Result |
|---|------|--------|
| 1 | `GET /v1/models` with `x-api-key` + `anthropic-version: 2023-06-01`, no workspace header | 200. Four models: `claude-fable-5-2`, `claude-haiku-5-5`, `claude-sonnet-5-5`, `claude-opus-5-5`. Each carries `max_input_tokens` (1,000,000), `max_tokens` (128,000) and `capabilities`: `effort.{low,medium,high,xhigh,max}.supported`, `thinking.types.{enabled,adaptive,disabled,between_tools}.supported`, `image_input`, `pdf_input`, `structured_outputs`. On Haiku 5.5 `thinking.types.enabled` is false and `adaptive` true. |
| 2a | `POST /v1/chat/completions` (OpenAI compatibility), `temperature: 0.7` (Aura's default) | 400, `` `temperature` is deprecated for this model. `` |
| 2b | Same without `temperature` | 200 |
| 3 | `POST /v1/messages`, `stream: true`, one tool, `thinking: {type: adaptive, display: summarized}`, `output_config.effort: medium` | 200. Events `message_start`, `content_block_start` (`tool_use`, `get_weather`), `content_block_delta` (`input_json_delta` ×4), `content_block_stop`, `message_delta`, `message_stop`, `ping`. `output_tokens_details.thinking_tokens: 0` — adaptive thinking chose not to think on a trivial question. |
| 4 | `POST /v1/messages` twice, 16,002-token system block with `cache_control: ephemeral`, `thinking: disabled` | First: `cache_creation_input_tokens: 16002`. Second: `cache_read_input_tokens: 16002`, `input_tokens: 16`. |

**What this does not show.** One model (Haiku 5.5), one key type, one workspace. No thinking actually
produced, so no `thinking_delta`/`signature_delta` and no replay was exercised. No image or PDF. No
long conversation, no tool loop longer than one call, no 429 or 529. Nothing from the lab VM. That the
calls drew on the Console credit is inferred from the key type, not read from the balance. The probe sent
`x-api-key` and `anthropic-version: 2023-06-01`; the SDK sends `authorization: Bearer` and `2026-10-21`.
The compatibility endpoint accepted a Bearer key (2b), but `/v1/messages` with the SDK's headers is first
measured in the plan's first task.

Read from the docs the same day (URLs in §8): thinking blocks must accompany the `tool_use` they precede
when sent back; Opus 5.5 and Fable 5.2 cannot turn thinking off and Sonnet 5.5 rejects `disabled`; on
Claude 5 `display` defaults to `omitted`; effort is `output_config.effort` (`low`…`max`), default `high`,
`medium` on Opus 5.5 and Haiku 5.5; up to 4 cache breakpoints, minimum 512 tokens on every Claude 5 model,
an automatic top-level `cache_control`, prefix order tools → system → messages, a change of thinking or
effort parameters invalidating the message cache; forced `tool_choice` returns 400 with adaptive thinking
on Opus 5.5, Sonnet 5.5 and Fable 5.x; no endpoint publishes prices.

## 4. Approaches

- **A. Native `anthropic` provider on the official Go SDK (picked).** A client package beside
  `internal/llm/chatgpt`, wired into the same places ChatGPT is. Spends the Console credit with nobody in
  between, keeps caching and thinking under Aura's control. Cost: a new adapter and its wiring.
- **B. BYOK on OpenRouter (not picked, usable as a bridge).** The Anthropic key goes into OpenRouter's
  integrations with "Never use shared capacity for any model on this provider"; `anthropic/*` then bills
  the Console. No code. OpenRouter keeps a 5% fee from OpenRouter credit (its BYOK page, 2026-11-27; the
  page names a monthly free allowance without saying it covers the fee), caching through it is
  unmeasured, and the route still depends on OpenRouter.
- **C. The OpenAI-compatibility endpoint through `openai_compat` (rejected).** Measured 400 on Aura's
  default temperature (§3, 2a). Anthropic calls it "not considered a long-term or production-ready
  solution"; no prompt caching, thinking never returned, `reasoning_effort` ignored.
- **D. Claude subscription sign-in like ChatGPT's (rejected).** Forbidden by the terms (§1). Hermes does it
  by presenting itself as Claude Code, and by its own docs it then bills only purchased extra-usage credit,
  never the Max allowance.

## 5. Design

### 5.1 The route

- Provider id `anthropic`. The base URL is fixed at `https://api.anthropic.com` and the settings validator
  refuses any other, as it does for ChatGPT (`cmd/aura/serve_settings.go`).
- `llm.ReasoningTarget` gains `ReasoningTargetAnthropic`. Every `switch` on the target (nine sites today,
  `git grep "ReasoningTarget"`) gets an explicit arm; none falls into `None` by omission.
- The id `anthropic` is the one the prompt builder's dormant cache seam already waits for
  (`internal/agent/prompt/cache_anthropic.go`, `Request.ToolsCacheControl`): choosing that id arms it
  (§5.6).
- Sampling is never sent on this route: no `temperature`, `top_p`, `top_k`, `min_p`, penalties. The
  operator's sampling settings stay stored and apply again when the route changes.

### 5.2 The house key

- A sealed setting `ANTHROPIC_API_KEY` (upstream's canonical name, per the env naming rule), admin-only,
  entered in Settings and sealed like `OPENROUTER_API_KEY` (`internal/settings`), but with no environment
  fallback (§7.7).
- `settings.Credentials` gains the Anthropic key. It is read on every request, so a key the admin replaces
  reaches a running process on the next call with no restart.
- `PrimaryRouteKey` returns the Anthropic key on the `anthropic` route and nothing else. The chat profile's
  `APIKey` never carries the services key on this route (the ChatGPT precedent,
  `2026-10-23-cloud-service-credentials-design.md`).
- The identity resolver (`internal/runner/runner_identity_llm.go`) gets an `anthropic` arm beside the
  ChatGPT one: every identity uses the route's client, no OpenRouter identity key is looked up and no
  OpenRouter credit decision runs. Usage is still attributed to the identity.
- No key stored: the route answers with the existing structured `llm_not_configured` refusal, hint pointing
  to Settings.

### 5.3 The wire client (`internal/llm/anthropic`)

On `github.com/anthropics/anthropic-sdk-go/v2` (v2.15.0, 2026-11-24; `go 1.25`; public surface read from
the source at that tag on 2026-11-27). The SDK pins `anthropic-version: 2026-10-21`.

- **Construction.** `anthropic.NewClient` with `option.WithoutEnvironmentDefaults()` (otherwise the SDK
  reads `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL`, `ANTHROPIC_CUSTOM_HEADERS`
  and a profile file from the host), exactly one credential option carrying the house key,
  `option.WithMaxRetries(0)` (default 2: the agent loop owns retries, as on ChatGPT), the shared
  stream-idle watchdog as `option.WithMiddleware`, and the connect-timeout `http.Client` ChatGPT builds.
- **Sampling cannot be sent.** `MessageNewParams` has no `temperature`, `top_p` or `top_k` in v2.15.0.
  Nothing re-adds them through `option.WithJSONSet`.
- **Tools.** GA has no schema helper (the normalizing ones are beta-only); each `llm.ToolDef`'s JSON
  schema fills `ToolInputSchemaParam{Properties, Required, ExtraFields}` directly.
- **Accumulation and replay.** `Message.Accumulate(event)` assembles text, thinking, signatures and
  tool input from the stream; `ContentBlockUnion.ToParam()` turns the finished blocks back into request
  params, which is what §5.5 replays. The client writes no accumulator of its own.

- **Request.** System messages hoisted into `system` blocks; `tool` messages become `tool_result` blocks in
  a user message; consecutive same-role messages merged; `Request.ContentProjection` and `ToolMedia`
  become image and document blocks where the model's capabilities allow them; `ToolChoice: "none"` maps to
  Anthropic's `none`; no other forced tool choice is sent (400 with adaptive thinking).
- **Stream.** `text_delta` → `Chunk.Text`; summarized `thinking_delta` → `Chunk.Reasoning`;
  `tool_use` + `input_json_delta` → one `Chunk.ToolCall` per block with the accumulated arguments;
  `message_delta.stop_reason` → `FinishReason` (`end_turn`/`stop_sequence` → `stop`, `tool_use` →
  `tool_calls`, `max_tokens` and `model_context_window_exceeded` → `length`; `refusal` and `pause_turn`
  get an explicit mapping the builder decides and tests); usage on the final chunk, from the cumulative
  `message_delta` totals.
- **Errors.** Classified by `(*anthropic.Error).Type()`, not by status: an `error` event in the middle of a
  stream arrives with the stream's own status, 200. `rate_limit_error`, `overloaded_error`, `api_error`,
  `timeout_error` and transport failures map to the retryable classes `openai_compat` already produces;
  `authentication_error`, `permission_error` and `billing_error` to a non-retryable credential error that
  names Settings (and, for billing, the Console balance).

### 5.4 Thinking and effort

- Thinking is `adaptive` on every request. `display: summarized` when `ShowReasoning` is on, `omitted`
  otherwise.
- `Request.Reasoning.Effort` maps to `output_config.effort`. `minimal` sends `low`. `none` sends
  `thinking: disabled` only when the model's capabilities say `disabled` is supported, and otherwise the
  lowest supported effort. An effort the model does not list is clamped to the nearest it does.
- The model profile reads `SupportedReasoningEfforts` from `capabilities.effort` and sets
  `ReasoningMandatory` when `thinking.types.disabled` is unsupported, so the Composer offers only what the
  model accepts.

### 5.5 Thinking replay inside a tool loop

Today `Chunk.Reasoning` is stream-only and the assistant message that carries tool calls keeps neither its
text nor its reasoning (`internal/agent/llm_agent.go`). Claude needs the signed thinking blocks back, in
order, with the `tool_use` they preceded.

- `llm.Chunk` gains one opaque, provider-owned field that the Anthropic client fills with the completed
  assistant content blocks it must see again (thinking with signature, redacted thinking). Other clients
  never set it.
- `llm.Message` gains the matching opaque field. The agent copies it onto the assistant message it appends
  for the tool calls (and for a pause), unchanged and unread.
- The Anthropic client replays it verbatim ahead of that message's `tool_use` blocks; every other client
  ignores it, so their wire requests stay byte-identical.
- Wherever history outlives the process (a pause resumed later), the field is persisted with the message.
  Outside a tool loop, thinking from completed turns may be dropped (docs) and is not stored.

### 5.6 Prompt caching

- The tools + system prefix carries a breakpoint through the existing `ToolsCacheControl` seam
  (`internal/agent/prompt/cache_anthropic.go`; today it only sets the marker, and nothing translates it).
- The conversation tail gets a second breakpoint on the **last history message before the volatile
  hint**. Not the automatic top-level `cache_control`: that marks the last block, which is the per-turn
  hint the builder appends (`Build`, `builder.go`), so every turn would write a prefix the next turn can
  never read. Default 5-minute TTL. Two of the four breakpoints, the others unused.
- This deliberately revises the seam's rule for this provider only. Its comment forbids a breakpoint on
  history because a shifting breakpoint poisons a byte-stable prefix on OpenRouter; on Anthropic a
  breakpoint that advances with the conversation is how the history is cached. The OpenRouter assertion
  (no `cache_control` on the wire) stays as it is.
- An effort change invalidates the message cache (docs), so the effort is held for the conversation
  (§7.5).
- `llm.Usage` gains `CacheWriteTokens`. `CachedTokens` keeps meaning cache reads. `PromptTokens` is
  input + cache read + cache write, the prompt that occupied the window.

### 5.7 Models, profile, prices

- The Settings model picker lists the account's `/v1/models` (paginated).
- `ResolveModelProfile` gets an `anthropic` arm: context window from `max_input_tokens`, output cap from
  `max_tokens`, efforts and mandatory reasoning from `capabilities`, image and PDF input for the content
  capability source.
- Prices: no Anthropic endpoint publishes them. The arm reads the list price from OpenRouter's public
  catalogue for the same model (no key; Aura already parses it), with a cache-write rate added to
  `llm.Price`. When no catalogue entry matches, the cost status is unknown, never $0. The id mapping
  (`claude-opus-5-5` ↔ OpenRouter's id) is measured in the plan's first task. Haiku 5.5's higher rate
  above 100k prompt tokens is applied, not averaged away.
- `Config.Spend` answers "unavailable: see the Claude Console" on this route; a standard key has no spend
  endpoint.

### 5.8 Vision

The vision route that asks the primary chat model first (`2026-10-25-media-vision-primary-design.md`)
gets an Anthropic arm, like ChatGPT's `Describe`, with the house key, for models whose capabilities list
image input.

### 5.9 Settings page

- A fifth routing button, "Claude", in the model settings panel (`web/src/settings/modelSettingsDefs.ts`).
- On that route: the admin-only key field (has value / replace / clear, never shown), the model picker,
  and the reasoning levels the model declares. Italian and English strings.

## 6. What done means

- Gate 2 green on every touched package; coverage and mutation as the repo rules require.
- Live on the lab VM with a fresh house key, recorded in `docs/superpowers/verification/`:
  1. switch the chat to Claude in Settings with no restart;
  2. a real multi-tool turn (at least three tool calls, one with an image result) on Sonnet 5.5 or
     Opus 5.5, with thinking summaries streamed live and replayed (the request log shows the signed
     blocks going back);
  3. a pause (approval card) resumed after the process restarts, same model;
  4. a cache read on turn two of a conversation, in the logged usage;
  5. the turn's cost recorded with cache reads and writes priced;
  6. the key replaced in Settings and the next turn using it, no restart;
  7. no key: the refusal card, no network call;
  8. switch back to OpenRouter: wire requests byte-identical to before the change.
- The PRD records the measurement, citing this spec, and what it does not prove.

## 7. Decisions taken after the builder check (controller's calls; the operator may overturn them)

1. **The plan opens with a measurement task, on a fresh house key, before any adapter code.** It answers,
   on `/v1/messages` with the SDK's own headers, Sonnet 5.5 and Opus 5.5, and thinking actually produced:
   - whether the API accepts a replayed assistant turn that Aura has rewritten: the text before the tool
     calls dropped, call ids rewritten (`uniquifyToolCallIDs`), same-message repeats removed
     (`dedupeSameMessageCalls`), and on a pause only the pausing calls kept (`pauseToolCalls`);
   - whether the per-round hint, merged as text into the user message that carries the `tool_result`
     blocks, ends the turn for thinking (prior blocks stripped) or not;
   - the OpenRouter catalogue id for each Anthropic model, and whether it publishes a cache-write and a
     long-prompt rate.
   The verdicts go into this spec before the adapter task starts.
2. **Replay holds the original signed blocks, never a rewrite.** The replay field carries the thinking
   and redacted-thinking blocks exactly as streamed (`ToParam()` of those blocks only). Aura's tool-call
   rewrites stay as they are on every route. If measurement 1 shows the API rejects or silently ignores
   thinking next to a rewritten turn, the Anthropic route keeps the original ids, siblings and text on
   that message, and only that route; this spec is amended with the measurement first.
3. **If the hint closes the turn, it moves on this route only.** If measurement 1 shows the merged hint
   strips prior thinking, the Anthropic client sends the hint where it does not close the turn (the
   measured alternative), leaving every other route's wire request untouched (done item 8).
4. **Cost is computed per call, in the client.** `Usage.Cost` is set on each call from the route's
   price, so the turn sums per-call costs (`agent/turn_usage.go`) and Haiku 5.5's long-prompt rate
   applies per request, never to the turn's total. A rate the catalogue does not publish is never typed
   into Aura: a call that needs it reports no cost, and the turn's cost is unknown rather than wrong.
5. **Aura's per-turn effort classifier is off on this route.** Claude's adaptive thinking already decides
   how much to think, and each effort change invalidates the message cache (§3). The effort is the one
   the Composer shows, held for the conversation; the model's default when nobody picked one. Overrun
   recovery, which today drops to effort `none` mid-loop (`llm_agent_finalize.go`), sends the lowest
   supported effort instead and never turns thinking off inside a tool loop.
6. **A cache miss after `tool_search` loads a tool is accepted and measured,** not engineered around.
   Done item 4 is measured on a turn that loads no new tool; the live run also records the cost of a
   turn that does.
7. **The house key comes from Settings only.** No environment fallback: an `ANTHROPIC_API_KEY` already
   set on a developer's host must not silently become the house key. The key gets its own read path
   (not `Credentials.read`'s environment tier); `.env.example` and `compose.yaml` get no entry. Tests
   write the sealed row.

## 8. Sources read on 2026-11-27

- <https://code.claude.com/docs/en/legal-and-compliance>, <https://code.claude.com/docs/en/authentication>
- <https://platform.claude.com/docs/en/cli-sdks-libraries/libraries/openai-sdk>
- <https://platform.claude.com/docs/en/build-with-claude/thinking>,
  `.../thinking-tool-workflows`, `.../effort`, `.../prompt-caching`, `.../about-claude/pricing`
- <https://openrouter.ai/docs/guides/overview/auth/byok>
- Hermes Agent (`website/docs/integrations/providers.md`, `plugins/model-providers/anthropic`) and
  LibreChat (`content/docs/configuration/pre_configured_ai/anthropic.mdx`).

## 9. Open for the builder

- How the tail breakpoint reaches the wire client: a request field the builder sets (the index of the last
  stable message) beside `ToolsCacheControl`, or the client finding the hint itself. The builder owns the
  hint, so it is the one that knows.
- Where a paused turn's history is persisted, so the replay field survives it.
- The mapping of `refusal` and `pause_turn` stop reasons.
