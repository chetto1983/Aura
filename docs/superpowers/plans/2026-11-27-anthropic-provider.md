# Claude as a primary route: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or
> superpowers:executing-plans, task by task, on `master`. Steps use checkbox syntax.

**Goal:** a fifth primary route, `anthropic`, that runs a full Aura turn on Claude through the official
Go SDK and spends the operator's Claude Console credit through one admin-set house key.

**Architecture:** a new wire package `internal/llm/anthropic` beside `internal/llm/chatgpt`, wired where
ChatGPT is (profile, route validation, reasoning capabilities, identity resolver, model picker, vision,
media indexer, Settings page). Three provider-neutral additions carry what Claude needs; every other
client ignores them:
- `llm.Chunk.ProviderState` / `llm.Message.ProviderState`: the ordered content blocks of one assistant
  message as Claude returned them, kept in the run's memory and sent back verbatim;
- `llm.Request.StablePrefixMessages`: how many leading messages are byte-stable, so the client puts the
  conversation cache breakpoint before the per-round hint;
- finish reason `content_filter`: a refusal ends the turn instead of being retried.
On this route the per-round hint stays in the run's history, so the prefix every signed thinking block
was produced after never changes inside a run. Requests go through the SDK's Beta Messages surface with
`thinking-binding-controls-2026-08-01` and `prefix_mismatch_behavior: drop_block`, so a prefix that does
change (compaction mid-run) drops a block instead of failing the turn, and the drop is counted.

**Tech Stack:** Go 1.27.4, `github.com/anthropics/anthropic-sdk-go/v2` v2.15.0, Postgres settings store
(no migration), React + i18next + vitest.

**Spec:** `docs/superpowers/specs/2026-11-27-anthropic-provider-design.md` (approved 2026-11-27,
`7360bcec5`). Decisions 1-6 below amend it; Task 1 writes them, with its measurements, into the spec
before any other task.

## v1 → v2

Three read-only reviews (code citations YELLOW, adversarial RED, Hermes/LibreChat cross-check YELLOW).
Folded in:
1. **Thinking is bound to its prefix** on all four models (SDK `betamessage.go` `prefix_mismatch_behavior`,
   default `error`; LibreChat `bedrock.ts:136-148`). Aura's per-round hint rotated that prefix every round.
   Now the hint stays in history on this route, the Beta surface sends `drop_block`, and
   `input_transformations` is logged (decision 1).
2. **`ProviderState` holds every block in order**, not thinking only: replaying thinking ahead of
   interleaved `tool_use` blocks is a 400 (Hermes `transports/anthropic.py:71-73`). A message whose calls
   Aura shortened sends no state (decision 2).
3. **`ResolveModelProfile` gets its anthropic arm in Task 2**; without it every save of the route failed.
4. **`deadcode` passes at every commit:** the package is allowlisted in Task 5 and the entry leaves in
   Task 7; `Price.For` moves to Task 5 and `FetchModelCapabilities` to Task 8.
5. **`max_tokens` and the window are decided** (decisions 4-5); `tool_choice` only with tools; a refusal is
   terminal; a stream closed before `message_stop` is retryable; blank text never reaches the wire; tool
   ids and inputs sanitized; top-level schema combinators flattened; breakpoints never on thinking.
6. **Settings save order:** the house key is saved before the route profile, so a first setup can finish.
7. **Smaller:** command-hook guard relaxed only on the command path; `PrimaryRouteKey` untouched; refusal
   wrapper owned by `cmd/aura`; Anthropic keys redacted; `request-id` read; coverage policy and mutation
   scope files named; Task 10 pushes before it deploys; the probe measures cross-turn caching and silent
   loss, not only refusals.

## Decisions taken while writing this plan

1. **Inside a run, the prefix of every replayed thinking block stays byte-stable.** On this route
   `buildBase` (`internal/agent/prompt/builder.go:159-173`) still appends the hint, and the agent then
   keeps that hint message in `a.history` (other routes unchanged). Requests use the Beta surface with
   `block_binding: {prefix_mismatch_behavior: "drop_block"}`, and the client logs the count of
   `input_transformations` dropped blocks per request. If Task 1 shows binding is not enforced, or that
   GA reports the same, the plan still uses Beta for the count; switching to GA is a later change.
2. **`ProviderState` is the JSON of `ToParam()` of every block of the assistant response, in order.** The
   client replays it in place of rebuilding from `ToolCalls`, taking each `tool_use` input by id from
   `Message.ToolCalls`. When the message's call ids are not exactly the state's `tool_use` ids (Aura
   deduped, uniquified or hook-rewrote a call), the client ignores the state and rebuilds the message
   from `Content` + `ToolCalls`: no thinking for that message, no 400.
3. **Nothing new is persisted; the cross-turn cost is measured, then decided.** Decision 1 of v1 stands:
   the runner rebuilds turns from events (`runner_persist.go:182-249`, `runner_persist_pause.go:126-182`)
   and amendment #91 (`history_reasoning_free_test.go:12-26`) forbids persisted CoT re-entering the
   context. A resumed pause and a new user turn send earlier tool-call turns without thinking and
   without the run's hints. Task 1 measures (a) that this is accepted and (b) the cache read on the turn
   after a tool loop. If the cross-turn miss costs more than the operator accepts, persistence comes back
   as its own spec; this plan does not build it.
4. **`max_tokens` on this route:** `max(req.MaxTokens, cfg.MaxTokens, 16_384)` capped at
   `cfg.MaxOutputTokens`, where `cfg.MaxTokens` defaults on this route to 32,768 unless pinned. Adaptive
   thinking spends inside `max_tokens`, so a helper's 32-token cap (`title.go:42`) or 0 (compaction,
   `compaction.go:160-169`) would otherwise end in `length` or a 400. Anthropic bills tokens used, not the
   cap.
5. **Working window:** `ContextWindow` defaults to the model's long-prompt threshold when its price has one
   (Haiku 5.5: 100,000), else 200,000, never above `max_input_tokens`; `AURA_MODEL_CONTEXT_WINDOW` pins it.
   A 1M window would let compaction wait until prompts of ~500k tokens. The ChatGPT precedent uses a
   working budget too (`chatgpt_profile.go:13`). The token preflight
   (`conversations.ValidateFinalRequestBudget`, `request_budget.go:40-43`) does not see `json:"-"` state
   and drops tools on `ToolChoice: "none"` while this client keeps them; the gap between the working
   window and `max_input_tokens` absorbs both.
6. **Cost:** the client prices each call (`Usage.Cost`), cache writes at `CacheWritePer1M`. A call with
   cache writes on a model whose catalogue has no write rate gets `Cost == nil`. The turn keeps today's
   partial-sum rule (`turn_usage.go:45-49`); spec §7.4's "any unpriced call makes the turn unknown" is
   amended to this, because on this route every call of a priced model is priced. `llm.Usage` gains no
   `CacheWriteTokens`; the client logs the write count.
7. **`ProviderState` is `json:"-"`** and named for what it is ("replay" names HARN-03,
   `llm_agent_replay_probe.go`). It never reaches `conversations.Turn`.
8. **Content capabilities are read in the client**, as ChatGPT does (`chatgpt/media.go:63`);
   `NewContentCapabilitySource` gets an explicit `ReasoningTargetAnthropic → nil` arm, vision goes through
   a `PrimaryReader`, and `PrimaryRouteKey` keeps returning no key on this route (its only user is the
   OpenAI-shaped HTTP arm).
9. **The house key copies the management-key pattern** (`modelSettingsDefs.ts:83-90`), not
   `OPENROUTER_API_KEY`, which Aura mints and never shows (`settings_api_authz.go:11`).
10. **`chatGPTReasoningCaps` becomes `profileReasoningCaps`** (refactor on touch), serving both routes.

## Global Constraints

- Provider id `anthropic` (`llm.AnthropicProvider`; `prompt.providerAnthropic` is replaced by it); base
  URL exactly `https://api.anthropic.com`; every other URL refused.
- Settings key `ANTHROPIC_API_KEY`: sealed, admin-written, read on every call from the store only
  (`Store.Secret`), never from the environment, never shown, never logged.
- One SDK constructor, `anthropic.newSDK(key string, hc *http.Client, baseURL string)`, used by every call
  site: `option.WithoutEnvironmentDefaults()`, `option.WithAPIKey(key)`, `WithMaxRetries(0)`,
  `WithMiddleware(openai_compat.StreamIdleMiddleware)` (both SDKs alias `MiddlewareNext`, verified),
  `WithHTTPClient(hc)` with the connect-timeout transport, `DisableKeepAlives`, no redirects
  (`chatgpt/client.go:39-48`).
- Requests: Beta Messages, beta `thinking-binding-controls-2026-08-01`, `block_binding.prefix_mismatch_behavior
  = "drop_block"`; thinking `adaptive`, `display` `summarized` when `cfg.ShowReasoning` else `omitted`.
- Never sent: `temperature`, `top_p`, `top_k`, `min_p`, penalties, `thinking: disabled`, a forced tool
  choice, `tool_choice` without tools, a blank or whitespace-only text block, top-level `cache_control`.
- Effort `output_config.effort` ∈ {`low`,`medium`,`high`,`xhigh`,`max`}; `minimal`/`none` → the lowest
  in `cfg.SupportedReasoningEfforts`, `low` when that list is empty; empty → no `output_config`.
- Cache: breakpoints on the last tool and the last system block when `ToolsCacheControl == "ephemeral"`,
  and on the last block produced by message `StablePrefixMessages-1` (a `tool_result`, text or
  `tool_use` block, never thinking); TTL 5 minutes.
- Tool ids on the wire match `^[a-zA-Z0-9_-]{1,64}$`, mapped deterministically and identically on
  `tool_use.id` and `tool_result.tool_use_id`; tool input not a JSON object → `{}`.
- Prices per MTok from `GET https://openrouter.ai/api/v1/models` (no key), id `anthropic/` + the model id
  with its version dash turned into a dot; fields `prompt`, `completion`, `input_cache_read`,
  `input_cache_write`, `overrides[].min_prompt_tokens` and its rates. No rate typed into Aura.
- OpenRouter, llama.cpp, Ollama and ChatGPT wire requests stay byte-identical; their packages' existing
  tests do not change.
- One info log line per Claude request: model, effort, replayed state messages, dropped blocks
  (`input_transformations`), input / cache read / cache write / output tokens, cost. Never content, never
  the key.
- ≤600 LOC a file, tests included. At or near the cap: `internal/llm/config.go` 559,
  `internal/agent/llm_agent.go` 558, `cmd/aura/chat_boot.go` 579, `cmd/aura/serve.go` 598,
  `internal/agui/server.go` 596, `internal/agui/settings_api.go` 547, `internal/agui/settings_api_test.go`
  587, `web/src/i18n/resources.settings.ts` 587, `ModelSettingsPanel.test.tsx` 576. New code goes in new
  files there.
- Gates per task, in WSL (`export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"`): `go vet ./...`,
  `go build ./...`, `go test` and `-race` on each touched package, `make quality` green at the commit,
  production `deadcode` included (`scripts/deadcode_production_allowlist.txt`, entries land and leave with
  their code). Web: `scripts/web_quality_hook.sh` and `npm test` in `web/`.
- Coverage: `internal/llm/anthropic` added to `scripts/coverage_package_policy.json` as `target`.
- Mutation ≥70% killed on `internal/llm/anthropic/request.go`, `stream.go`, `cost.go`, added to
  `.github/workflows/ci.yml`, `scripts/critical_mutation_gate.py` and `scripts/critical_mutation_gate_test.py`
  the way `6f7fe2262` did.
- Paid calls only in Task 1 and Task 10, each with the operator's OK in that session, on a key the operator
  creates and revokes after. The key never enters the repo, a log, a verification file or a commit.
- Commit by pathspec on `master`; never stage a peer session's files. The repository is public.

## Review Focus

1. **A tool loop of three or more rounds with interleaved thinking.** Every request carries each earlier
   round's blocks in their original order, after a prefix unchanged since they were produced. Pinned by
   `TestEveryRoundReplaysItsOwnBlocks` (Task 3), `TestHintStaysInHistoryOnTheAnthropicRoute` (Task 4) and
   `TestInterleavedBlocksKeepTheirOrder` (Task 5).
2. **Claude refuses.** The turn ends once, says so, and makes no nudge, finalize or retry call. Pinned by
   `TestContentFilterEndsTheTurnWithoutRecovery` (Task 3) and `TestRefusalMapsToContentFilter` (Task 5).
3. **The connection drops mid-tool-call or mid-thinking.** The round is retried; no tool runs with partial
   input. Pinned by `TestStreamClosedBeforeMessageStopIsRetryable` (Task 5).
4. **The house key replaced or cleared mid-conversation.** The next call uses the new key, or refuses with
   `llm_not_configured` and no dial. Pinned by `TestKeyIsReadOnEveryCall` (Task 5) and
   `TestNoHouseKeyRefusesWithoutDialing` (Task 7).
5. **`ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL` set in the environment.** Nothing
   reads them: no call goes to a decoy, the Settings row reads `has_value: false`. Pinned by
   `TestSDKIgnoresTheEnvironment` (Task 5) and `TestHouseKeyIgnoresTheEnvironment` (Task 6).

---

### Task 1: Measure the wire, then amend the spec

**Files:**
- Create: `spikes/anthropic-provider/probe/main.go`, `spikes/anthropic-provider/README.md`,
  `docs/superpowers/verification/<date>-anthropic-wire-probe.md`
- Modify: `go.mod`, `go.sum` (`go get github.com/anthropics/anthropic-sdk-go/v2@v2.15.0`),
  `docs/superpowers/specs/2026-11-27-anthropic-provider-design.md`

**Interfaces:** Produces the verdicts later tasks rely on and the SDK in `go.mod`.

- [ ] **Step 1: Ask the operator** for the OK (paid, a few cents on Sonnet 5.5 and Opus 5.5) and a fresh
  key, kept in a scratchpad file read through `-keyfile`, deleted after; the operator revokes it.
- [ ] **Step 2: Write the probe** (`go run ./spikes/anthropic-provider/probe -keyfile <path> -model <id>
  -case <name>|all`), built only with the Global Constraints' SDK options, on the Beta surface with the
  binding beta. Per case it prints status or error `Type()`, `stop_reason`, usage (input, cache read,
  cache creation, thinking tokens), `input_transformations`, and the returned block types in order.
  Cases, each on both models, each with `display: summarized` and again with `omitted`:
  - `headers`: one call (Bearer key, `anthropic-version: 2026-10-21`);
  - `loop-verbatim`: three tool rounds, each round's full block list replayed in order, hint kept in
    history;
  - `hint-rotated`: the same with the hint replaced each round (Aura's v1 shape), binding `error`, then
    `drop_block`;
  - `interleaved`: a prompt that yields `thinking · tool_use · thinking · tool_use`, replayed in order;
  - `dedupe-drop`: one of two parallel calls removed from the replayed turn, state ignored per decision 2;
  - `resume-without-blocks`: a tool loop whose earlier assistant turns are sent from `Content` +
    `ToolCalls` only (the reload shape), then the next tool result; report whether it is accepted and
    whether the next round thinks;
  - `cross-turn-cache`: turn 1 a three-round tool loop; turn 2 rebuilt as `LoadManagedHistory` rebuilds
    it (no blocks, no hints, `ResultPreview` contents); report cache read vs the prefix length;
  - `foreign-history`: OpenRouter-style ids including one with `.` and `:`, mapped by the sanitizer;
  - `tool-choice-none`: `none` with tools, and a request with no tools and no `tool_choice`;
  - `pdf-in-tool-result`: a base64 PDF inside `tool_result` content;
  - `mid-stream-error`: read `packages/ssestream` at v2.15.0 and record how an `event: error` surfaces.
- [ ] **Step 3: Run it.** Read the Console balance before and after, against the summed computed cost.
- [ ] **Step 4: Write the verification file:** date, models, each case's raw outcome, what it does not
  show. No key, no key prefix beyond `sk-ant-`.
- [ ] **Step 5: Amend the spec:** header (no migration), §2 anti-goals (thinking is dropped across a
  persisted pause and across turns, measured), §5.3-§5.7, §6 items 3-5, §7.3-§7.4, from decisions 1-6 and
  the verdicts. **Stop and report to the operator** if `loop-verbatim` or `resume-without-blocks` is
  refused, if `loop-verbatim` shows dropped blocks, or if `cross-turn-cache` reads less than the tools +
  system prefix; the operator then decides persistence or the spec changes before Task 2.
- [ ] **Step 6: Commit** `spike(anthropic): measure the Messages wire Aura will send`.

### Task 2: The route in `internal/llm`

**Files:**
- Create: `internal/llm/anthropic_profile.go` (+ `_test.go`)
- Rename: `internal/llm/chatgpt_caps.go` → `profile_reasoning_caps.go` (+ test)
- Modify: `internal/llm/reasoning_target.go`, `internal/llm/pricing_source.go:44-75,173-206,321-324`,
  `internal/llm/prices.go:25-37`, `internal/llm/spend.go:97-112`, `internal/llm/llamacpp_caps.go:181-197`,
  `internal/llm/model_content_caps.go:158-180`, `internal/agent/prompt/reasoning_policy.go:122-125`,
  `internal/agent/prompt/cache_anthropic.go:8`

**Interfaces — produces:**
- `const AnthropicProvider = "anthropic"`, `const AnthropicBaseURL = "https://api.anthropic.com"`;
- `ReasoningTargetAnthropic` after `ReasoningTargetChatGPT`, `String()` `"anthropic"`, classified only for
  `EqualFold(provider, AnthropicProvider) && baseURL == AnthropicBaseURL`;
- `func (c *Config) ResolveAnthropicProfile() error`: clears `APIKey` and `Headers`, sets `CostStatus =
  CostStatusUnknown`, leaves limits and price to Task 7, validates; `ResolveModelProfile` calls it for
  `AnthropicProvider` before any network;
- `Price` gains `CacheWritePer1M float64 \`json:"cache_write_per_1m,omitempty"\`` and `LongPrompt
  *LongPromptPrice \`json:"long_prompt,omitempty"\``, `type LongPromptPrice struct { MinPromptTokens int;
  InputPer1M, OutputPer1M, CacheReadPer1M, CacheWritePer1M float64 }`, parsed from `input_cache_write`
  and `overrides` in the OpenRouter wire;
- `type profileReasoningCaps` serving `ReasoningTargetChatGPT` and `ReasoningTargetAnthropic`;
- `prompt.IsReasoningTarget` true for Anthropic; `NewContentCapabilitySource` returns nil for it explicitly.

- [ ] **Step 1: Failing tests:** `TestReasoningTargetAnthropicNeedsTheFixedEndpoint`;
  `TestResolveModelProfileOnAnthropicNeverDials` (an `httptest` server as `BaseURL` counts zero hits;
  `APIKey` and `Headers` empty after); `TestSpendOnAnthropicNeverDials` (`ErrSpendUnavailable`, zero hits);
  `TestOpenRouterWireReadsCacheWriteAndLongPrompt` (Haiku 5.5 entry of spec §5.7 → `{0.1, 0.5, 0.01, 0.125,
  &{100000, 0.5, 2.5, 0.05, 0.625}}`; Sonnet 5.5 → `CacheWritePer1M 2.5`, `LongPrompt nil`);
  `TestProfileReasoningCapsServeAnthropic`; `TestIsReasoningTargetIncludesAnthropic`. Existing OpenRouter
  fixtures carrying `input_cache_write` gain the new field in their expected `Price`: the commit body says so.
- [ ] **Step 2: Run, expect FAIL** — `go test ./internal/llm/ ./internal/agent/prompt/`.
- [ ] **Step 3: Implement.** `Spend`'s arm returns `fmt.Errorf("%w: see the Claude Console",
  ErrSpendUnavailable)` before the `switch`.
- [ ] **Step 4: Run, expect PASS**, `-race`, `make quality`.
- [ ] **Step 5: Commit** `feat(llm): recognise the anthropic route and read cache-write prices`.

### Task 3: Provider state and refusals through the agent's run

**Files:**
- Modify: `internal/llm/client.go` (`Message` :24-30, `Chunk` :90-97), `internal/agent/llm_agent_consume.go`,
  `internal/agent/llm_agent.go:384-392,446-455,519`, `internal/agent/hooks_model_guard.go:13,52-79`
- Create: `internal/agent/llm_agent_refusal.go`, `internal/agent/llm_agent_provider_state_test.go`,
  `internal/agent/llm_agent_refusal_test.go`

**Interfaces — produces:**
- `Message.ProviderState json.RawMessage \`json:"-"\``; `Chunk.ProviderState json.RawMessage`, alone in its
  chunk, at most once per stream, before the `FinishReason` chunk;
- `const llm.FinishContentFilter = "content_filter"`;
- `consume` returns `roundOutput struct { text string; calls []llm.ToolCall; finish string; usage llm.Usage;
  providerState json.RawMessage; stopped bool; streamErr error }` in place of its six values (one call
  site, `llm_agent.go:384`).

- [ ] **Step 1: Failing tests** (`agenttest` fake client):
  - `TestToolCallMessageCarriesTheRoundsProviderState`; `TestEveryRoundReplaysItsOwnBlocks` (states
    `s1,s2,s3` on three rounds → the fourth request's assistant messages carry them in order);
    `TestRoundWithoutStateLeavesTheFieldNil`;
  - `TestInProcessHookCannotChangeProviderState` (the strict guard refuses it);
  - `TestCommandHookRewriteKeepsProviderState` (JSON round trip drops the field; the command guard compares
    with it cleared on its clones only; the sent request still has it);
  - `TestProviderStateNeverReachesConversationTurn` (reflect check beside #91's);
  - `TestContentFilterEndsTheTurnWithoutRecovery`: finish `content_filter` with partial text → the fake
    client sees exactly one call, the streamed partial is repudiated, the final answer is the refusal
    notice, no finalize call.
- [ ] **Step 2: Run, expect FAIL** — `go test ./internal/agent/`.
- [ ] **Step 3: Implement.** The `:519` message takes `out.providerState`. `cloneModelRequest`
  deep-copies the bytes. The refusal arm lives in `llm_agent_refusal.go`, checked before the empty-answer
  recovery at `:446`; its notice is a constant in the agent's existing copy style.
- [ ] **Step 4: Run, expect PASS**, `-race`, and `go test ./internal/conversations/ ./internal/runner/`
  unchanged.
- [ ] **Step 5: Commit** `feat(agent): carry provider state through the run and end on a refusal`.

### Task 4: Stable prefix, kept hint and held effort on the route

**Files:**
- Modify: `internal/llm/client.go` (`Request`), `internal/agent/prompt/builder.go:129-173`,
  `internal/agent/prompt/cache_anthropic.go`, `internal/agent/hooks_model_guard.go:52-70`,
  `internal/agent/llm_agent_turn_reading.go:64-65`, the agent's request path where the built request's
  hint is known (`internal/agent/llm_agent_request.go:21-37`)
- Create: `internal/agent/llm_agent_anthropic_route_test.go`
- Test: `internal/agent/prompt/builder_test.go`

**Interfaces — produces:**
- `Request.StablePrefixMessages int`, set by the builder for provider `anthropic` only: `len(history)`
  when `buildBase` appended a hint, else `len(req.Messages)`; protected by the model-hook guard;
- `func injectCacheControl(req *llm.Request, provider string, stable int)` (three call sites in
  `builder.go`).

- [ ] **Step 1: Failing tests:**
  - `TestCacheControlSeam/anthropic marks the stable prefix before the hint` (5 history + hint →
    `StablePrefixMessages 5`); `.../openrouter leaves it zero`; the existing "never marks any history
    message" subtest unchanged;
  - `TestModelHookCannotMoveTheStablePrefix`;
  - `TestHintStaysInHistoryOnTheAnthropicRoute`: three rounds → round 3's request holds hint 1 and hint 2
    at their original positions; on `openrouter` round 3 holds only hint 3 (today's behaviour);
  - `TestAnthropicRouteHoldsTheComposerEffort`: adaptive on, no override → the classifier is never called
    and no effort is sent; override `high` → `high` every round.
- [ ] **Step 2: Run, expect FAIL** — `go test ./internal/agent/prompt/ ./internal/agent/`.
- [ ] **Step 3: Implement.** `readTurn`: `adaptive := a.cfg.AdaptiveReasoning && target &&
  llm.ReasoningTarget(a.cfg.Provider, a.cfg.BaseURL) != llm.ReasoningTargetAnthropic`. After a round's
  request is sent on this route, the hint message (the request's messages beyond `StablePrefixMessages`)
  is appended to `a.history` before the assistant message.
- [ ] **Step 4: Run, expect PASS**, `-race`.
- [ ] **Step 5: Commit** `feat(agent): keep the anthropic prefix stable and hold its effort`.

### Task 5: The wire client `internal/llm/anthropic`

**Files:**
- Create: `internal/llm/anthropic/{client,sdk,request,blocks,tools,stream,errors,cost}.go` and
  `{main,client,request,stream,cost}_test.go`
- Modify: `internal/llm/openai_compat/httperror.go:165` (also read `request-id`),
  `internal/redact/string.go:57` (+ test: `sk-ant-[A-Za-z0-9_-]{16,}` and `x-api-key:` values),
  `scripts/deadcode_production_allowlist.txt` (`github.com/chetto1983/aura/internal/llm/anthropic   wired
  by cmd/aura in Task 7`), `scripts/coverage_package_policy.json`, `internal/llm/prices.go` (`Price.For`)

**Interfaces:**
- Consumes: `llm.AnthropicBaseURL` (Task 2), `ProviderState`, `FinishContentFilter` (Task 3),
  `StablePrefixMessages` (Task 4).
- Produces: `type KeySource func(context.Context) (string, error)`; `func New(cfg llm.Config, key
  KeySource) *Client`; `Stream`; `var ErrNoKey`; `func (p Price) For(promptTokens int) Price` in `llm`.

- [ ] **Step 1: Failing tests** (`httptest.NewTLSServer`, SSE inline as `chatgpt/client_test.go:25-55`,
  every body decoded):
  - `TestSDKIgnoresTheEnvironment`: `t.Setenv` of `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`,
    `ANTHROPIC_BASE_URL` (a decoy server) → the decoy sees zero hits; the fixture sees `Bearer <house key>`.
  - `TestRequestNeverCarriesSampling`; `TestBetaBindingIsAlwaysSent` (header and `block_binding`).
  - `TestSystemIsHoistedAndToolsBecomeResults`; `TestCompactionMarkerStaysAUserMessage` (a `RoleUser`
    with `ToolCallID` set, `compaction.go:52-58`).
  - `TestInterleavedBlocksKeepTheirOrder` and `TestStateIsIgnoredWhenCallsWereShortened` (decision 2).
  - `TestBlankTextNeverReachesTheWire` (empty and whitespace content on every role and inside
    `tool_result`; an empty tool result sends `(no output)`).
  - `TestToolIDsAreSanitizedOnBothSides` (`functions.x:0`, a 70-char id); `TestNonObjectInputBecomesEmpty`.
  - `TestTopLevelCombinatorSchemaIsFlattened` (`anyOf` at the top → `{"type":"object"}` plus the schema in
    the description); `TestDuplicateToolNamesKeepTheFirst`.
  - `TestCacheBreakpointsLandOnToolsSystemAndTheStablePrefix` (on the last `tool_result` of message
    `StablePrefixMessages-1`, never on the merged hint, never on thinking; at most 3 markers).
  - `TestEffortMapping` (`""`; `high`; `none`/`minimal` → lowest; empty list → `low`).
  - `TestDisplayFollowsShowReasoning`; `TestToolChoice` (`none` with tools → `{"type":"none"}`; no tools →
    no `tool_choice`); `TestMaxTokensFloor` (0, 32, 4096 → `max(..., 32768, 16384)` capped at
    `MaxOutputTokens`).
  - `TestStreamMapsTextThinkingToolsAndUsage` (ToolCall and ProviderState chunks only after
    `message_stop`; `Usage{PromptTokens: input+read+write, CachedTokens: read}`).
  - `TestStopReasonMapping` (`end_turn`/`stop_sequence` → `stop`; `tool_use` → `tool_calls`; `max_tokens`,
    `model_context_window_exceeded` → `length`; `pause_turn` → `Chunk.Err`); `TestRefusalMapsToContentFilter`.
  - `TestStreamClosedBeforeMessageStopIsRetryable` (EOF mid-`input_json_delta` and mid-thinking → retryable
    `Chunk.Err`, no ToolCall chunk).
  - `TestMidStreamOverloadIsRetryable` (`overloaded_error` in a 200 stream → `*openai_compat.HTTPError{529}`,
    `agent.IsTransientLLMError` true); `TestHTTPErrorsKeepTheirClass` (401 → not retryable, message names
    Settings; 429 with `retry-after: 2`; `request-id` captured).
  - `TestKeyIsReadOnEveryCall`; `TestNoKeyNeverDials` (`ErrNoKey`, zero hits);
    `TestStreamIdleTimeoutIsRetryable`.
  - `TestCostIsSetWheneverAPriceExists` (Sonnet: input 1000, read 16002, write 0, output 50 →
    `(1000*2 + 16002*0.10 + 50*10)/1e6`; Haiku prompt ≥ 100,000 → long rates; writes with no write rate →
    `nil`; no price → `nil`); `TestPriceForSwitchesAtTheThreshold`.
  - `TestRequestLogLineCarriesNoContentOrKey`.
  - `main_test.go`: `goleak.VerifyTestMain`.
- [ ] **Step 2: Run, expect FAIL** — `go test ./internal/llm/anthropic/ ./internal/redact/ ./internal/llm/`.
- [ ] **Step 3: Implement** on the SDK's Beta types; `Message.Accumulate` for assembly; merge
  consecutive same-role wire messages by concatenating blocks, never strings; mid-stream `*anthropic.Error`
  mapped by `Type()`: `overloaded_error` 529, `rate_limit_error` 429, `api_error`/`timeout_error` 500,
  `authentication_error` 401, `permission_error` 403, `billing_error` 402, `invalid_request_error` 400.
  The house key never appears in an error body (redacted as `chatgpt/stream.go:150`).
- [ ] **Step 4: Run, expect PASS**, `-race`, `make quality` (allowlist entry matches), each file < 600 LOC.
- [ ] **Step 5: Commit** `feat(llm): stream Aura turns on Claude through the official SDK`.

### Task 6: The house key in settings

**Files:**
- Modify: `internal/settings/settings.go:57-95`, `internal/settings/credentials.go`,
  `internal/agui/settings_api_authz.go`, `internal/agui/settings_api.go:214,238,516-529`
- Create: `internal/agui/settings_api_store_only_test.go`
- Test: `internal/settings/credentials_test.go`

**Interfaces — produces:** `settings.AnthropicKeySetting = "ANTHROPIC_API_KEY"`; `func (c Credentials)
AnthropicKey(ctx context.Context) (string, error)` (store only, `""` without a store); `storeOnlySettingKeys`
consulted by the settings list and `effectiveSettingValue`; the key in `callTimeSettingKeys`.

- [ ] **Step 1: Failing tests:** `TestHouseKeyIgnoresTheEnvironment` (env set, no row → `""`, list
  `has_value: false`, effective `""`); `TestHouseKeyIsSealedAndLive` (PUT stores `enc:v1:`, `applied:
  "live"`; DELETE clears); non-admin PUT → 403; `PrimaryRouteKey` on anthropic still returns `""`.
- [ ] **Step 2: Run, expect FAIL** — `go test ./internal/settings/ ./internal/agui/`.
- [ ] **Step 3: Implement.** Label `"Claude Console API key (house)"`; provider label
  `openrouter|llamacpp|ollama|chatgpt|anthropic`. `AnthropicKey` has no production caller until Task 7: add
  an allowlist entry with the reason and remove it in Task 7.
- [ ] **Step 4: Run, expect PASS**, `-race`, `make quality`.
- [ ] **Step 5: Commit** `feat(settings): a sealed house key for Claude, never read from the environment`.

### Task 7: Wire the route in the composition root

**Files:**
- Create: `cmd/aura/serve_anthropic.go` (+ test), `internal/llm/anthropic/models.go` (+ test)
- Modify: `cmd/aura/llm_client.go:14-67`, `cmd/aura/chat_boot.go:441-471`, `cmd/aura/serve_settings.go:37,79-105,253-258`,
  `cmd/aura/serve_webui.go:389-400`, `cmd/aura/serve_provisioning_openrouter.go:120-124`,
  `cmd/aura/identity_llm_resolver.go`, `internal/runner/runner_identity_llm.go:132-137`,
  `internal/runner/runner_identity_chatgpt.go`, `internal/agui/settings_llm_models.go:33-135`,
  `internal/llm/model_catalog.go:45-55`, `scripts/deadcode_production_allowlist.txt` (both entries leave)

**Interfaces:**
- Consumes: `anthropic.New`, `KeySource`, `ErrNoKey` (Task 5); `Credentials.AnthropicKey` (Task 6).
- Produces:
  - `ModelCatalogEntry.MaxOutputTokens int`;
  - `func FetchModels(ctx context.Context, key KeySource) ([]llm.ModelCatalogEntry, error)`:
    `Models.ListAutoPaging`, lifecycle `active`, `ContextWindow = MaxInputTokens`, `MaxOutputTokens =
    MaxTokens`, efforts from `Capabilities.Effort`, `ReasoningMandatory = !Thinking.Types.Disabled.Supported`,
    2 MiB bound;
  - `type llmClientDeps struct { chatGPT chatGPTTokenSource; anthropicKey anthropic.KeySource }`,
    `func newLLMClient(cfg llm.Config, deps llmClientDeps) llm.Client` (replaces the variadic; both callers
    and the reloader's struct field at `serve_settings.go:79-84` and its construction at `:37`);
  - `anthropicRouteClient` in `serve_anthropic.go`, wrapping the client so `ErrNoKey` becomes
    `llmNotConfiguredError` with no dial;
  - `func validateAnthropicModel(ctx context.Context, cfg *llm.Config, key anthropic.KeySource) error`:
    the model must be listed; `MaxOutputTokens` from the entry unless pinned; `MaxTokens` 32,768 unless
    pinned; `ContextWindow` per decision 5 unless pinned; efforts and mandatory; price from
    `llm.FetchModelProfile(ctx, client, "openrouter", llm.DefaultBaseURL, "", openRouterID(model))` into
    `cfg.Prices[model]` with `CostStatusRateEstimate`, else left unknown;
  - `func openRouterID(model string) string`.
- The not-configured hint, in the const and the literal at `llm_client.go:67`, becomes "connect a model in
  Settings, then retry". `identityScopedLLMClient`'s nil message becomes "route client is unavailable".
  The model picker reads the key with a store-only read in `settings_llm_models.go` (not `server.go`).

- [ ] **Step 1: Failing tests:** `TestRouteValidationAcceptsOnlyTheAnthropicEndpoint`;
  `TestAnthropicRouteSwitchesLiveWithoutTheServicesKey` (reloader `Prepare` with an `OPENROUTER_API_KEY`
  override → published `APIKey ""`, anthropic client); `TestNoHouseKeyRefusesWithoutDialing`;
  `TestIdentitiesShareTheHouseRoute` (no `identitykey.Decide`); `TestAnthropicRouteNeedsNoIdentityKeys`;
  `TestModelPickerListsTheAccountsModels` (prices joined; non-admin 403);
  `TestOpenRouterIDMapping`; `TestFetchModelsReadsCapabilities` (the four models of spec §3);
  `TestValidateAnthropicModelSetsTheWorkingWindow` (Haiku → 100,000; Sonnet → 200,000; pinned value kept).
- [ ] **Step 2: Run, expect FAIL** — `go test ./cmd/aura/ ./internal/runner/ ./internal/agui/ ./internal/llm/...`.
- [ ] **Step 3: Implement.** In `chat_boot.go` move `credentials := serviceCredentials(cfg, pool)` above
  `:446`; the anthropic boot branch lives in `serve_anthropic.go` beside the ChatGPT one so `chat_boot.go`
  stays < 600.
- [ ] **Step 4: Run, expect PASS**, `-race`, `make quality` (allowlist entries removed).
- [ ] **Step 5: Commit** `feat(aura): Claude as a live primary route on the house key`.

### Task 8: Images and documents, vision and the media indexer

**Files:**
- Create: `internal/llm/anthropic/{media,describe}.go` (+ tests), `cmd/aura/serve_primary_reader.go`,
  `cmd/aura-media-index/anthropic.go` (+ test)
- Modify: `internal/llm/anthropic/models.go` (`FetchModelCapabilities`), `cmd/aura/serve_chatgpt.go:31`,
  `cmd/aura/vision_live.go:61-73`, `cmd/aura-media-index/main.go:249-255`

**Interfaces:**
- Produces: `func FetchModelCapabilities(ctx context.Context, key KeySource, model string)
  (llm.ProviderContentCapabilities, error)` with `MIMETypes` `image/jpeg`, `image/png`, `image/gif`,
  `image/webp` when `image_input`, and `application/pdf` when `pdf_input` (no `image/*` modality, so SVG,
  BMP and TIFF stay reference-only); cached per client; `func (c *Client) Describe(ctx context.Context,
  image []byte, mimeType, prompt string) (string, error)`; `primaryImageReader(cfg, plan, anthropicKey)`
  replacing `chatGPTImageReader`.

- [ ] **Step 1: Failing tests:** `TestProjectedImageBecomesABlock`; `TestToolMediaPDFFollowsItsToolResult`
  (PDF on the `ToolMedia` path; uploads project images and video only); `TestSVGStaysReferenceOnly`;
  `TestCapabilitiesAreFetchedOnlyWithMedia`; `TestDescribeReadsWithTheHouseKey`;
  `TestDescribeFallsBackWhenTheModelReadsNoImages`; `TestLiveVisionUsesClaudeOnTheAnthropicRoute`;
  `TestMediaIndexerReadsImagesWithTheHouseKey`.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement** with `ProviderContentCapabilities.SupportsMIME`.
- [ ] **Step 4: Run, expect PASS**, `-race`, `make quality`.
- [ ] **Step 5: Commit** `feat(vision): Claude reads images and PDFs on its route`.

### Task 9: The Settings page

**Files:**
- Create: `web/src/i18n/resources.settings.providers.ts`, `web/src/settings/__tests__/ModelSettingsPanel.claude.test.tsx`
- Modify: `web/src/settings/modelSettingsDefs.ts:3-17,19-42,243-330`, `web/src/settings/ModelSettingsPanel.tsx:33,124,193-282,362-368`,
  `web/src/settings/modelSettingsState.ts:31-51,217-238`, `web/src/i18n/resources.settings.ts`

**Interfaces — produces:** `CLAUDE_PROVIDER = 'anthropic'`, `CLAUDE_BASE_URL = 'https://api.anthropic.com'`,
`ProviderChoice` `'claude'`, a `PROVIDER_OPTIONS` entry with `fallbackModel: ''`, a `SettingDef` for
`ANTHROPIC_API_KEY` (`secret: true`) shown only on the Claude route, `ANTHROPIC_API_KEY` in
`CALL_TIME_SETTING_KEYS` and in a `PRE_PROFILE_KEYS` set saved before the profile batch.

- [ ] **Step 1: Failing tests:** picking Claude forces and hides the base URL; the key field shows
  set/not set, never a value; **first setup:** no stored key, a typed key, no model → Save sends only the
  key, then the picker loads the catalogue; picking a model and saving sends the profile; a stored key →
  Save sends the profile alone; Reset deletes the key; the picker allows no custom entry; Italian and
  English labels.
- [ ] **Step 2: Run, expect FAIL** — `cd web && npx vitest run src/settings/__tests__/ModelSettingsPanel.claude.test.tsx`.
- [ ] **Step 3: Implement.** The icon is an existing lucide icon (no Anthropic logo, per the trademark
  terms); `resolveProvider` maps `anthropic` to `claude`.
- [ ] **Step 4: Run, expect PASS**, `scripts/web_quality_hook.sh`, `npm test`.
- [ ] **Step 5: Commit** `feat(web): pick Claude in Settings and give it the house key`.

### Task 10: Live acceptance on the lab VM

**Files:**
- Create: `docs/superpowers/verification/<date>-anthropic-route-live.md`
- Modify: `prd.md` (LLM routes: the route, the measurements, what they do not prove, citing the spec and
  the date), `.github/workflows/ci.yml`, `scripts/critical_mutation_gate.py`,
  `scripts/critical_mutation_gate_test.py`

- [ ] **Step 1: Add the mutation scope**, run `scripts/critical_mutation_gate_test.py`, commit.
- [ ] **Step 2: Push** with the WSL recipe of CLAUDE.md and watch every CI job green on the pushed SHA,
  mutation and coverage included.
- [ ] **Step 3: Operator OK and a fresh key** for the paid run; the VM takes master through the appliance's
  update path.
- [ ] **Step 4: Run spec §6 items 1-9** through the cockpit, recording the per-request log line of each,
  and the operator's confirmation on the UI.
- [ ] **Step 5: Write the verification file and the PRD amendment**, commit, push, CI green.
