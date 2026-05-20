# Provider-side Prompt / KV Cache — State of the Art, May 2026

**Audience:** Aura engineering. **Goal:** decide how to add provider-side prompt caching to `internal/llm/client.go` so that the stable prefix (system overlays + tool definitions + skill manifests, currently ~10 K tokens and headed to ~25 K post MCP-roundup) is cached across turns and across conversations.

**Scope:** online state-of-the-art only. A sibling agent maps Aura's local code; this document covers what providers offer and what 2026 has taught the field.

**TL;DR:**
- Every major provider now ships some form of prompt caching. The headline economics are similar (≈90 % discount on cached input tokens) but the **billing model**, **TTL**, and **how you tell the provider what to cache** differ enough that a naive abstraction will leave money on the table.
- For Aura specifically (stable 10–25 K prefix, ~50 turn-bursts, long-tail conversations spanning hours), Anthropic with **explicit `cache_control` breakpoints + 1 h TTL** and Gemini **explicit cached content + 1 h default TTL** are the two best fits. OpenAI is automatic and free but the discount is smaller and TTL shorter.
- The single biggest 2026 lesson: **never put dynamic content (timestamps, tool results, per-user context) before the cache breakpoint.** "Don't Break the Cache" (arXiv 2601.06007) shows full-context caching can *increase* latency for agent loops.

---

## Anthropic Claude

- **Official docs:** <https://platform.claude.com/docs/en/build-with-claude/prompt-caching>
- **2026 state:** Mature. Stable since 2024; default TTL was silently **rolled back from 1 h to 5 m on 6 Mar 2026**, causing reported 20–32 % cost inflation for users who relied on the 1 h default. Workspace-level isolation since 5 Feb 2026.
- **Eligibility:**
  - Minimum prefix: **4 096 tokens** for Opus 4.5/4.6/4.7 and Haiku 4.5; **1 024 tokens** for Sonnet 4.5/4.6 and Opus 4.1; **2 048 tokens** for Haiku 3.5. Below the threshold, requests pass through silently with no cache (no error).
  - Up to **4 explicit `cache_control` breakpoints** per request, applied on `tools`, `system`, or `messages` content blocks.
  - **20-block lookback window:** the cache engine searches up to 20 content blocks before a breakpoint to find a previous cache entry. In long tool-use loops a breakpoint at the *head* of the conversation falls out of range; you must add a "sliding anchor" breakpoint closer to the tail.
- **TTL:** `"ephemeral"` (5 min, default) or `"ephemeral" + ttl: "1h"` (requires nothing special on the request — used to need a beta header; 1 h is now standard but you must set it explicitly per breakpoint).
- **Billing:**
  | Token class | Multiplier of base input price |
  |-------------|-------------------------------|
  | Cache write (5 m TTL) | 1.25× |
  | Cache write (1 h TTL) | 2.00× |
  | Cache read | 0.10× (90 % discount) |
  | Output | unchanged |
  - Break-even: a 5 m write pays off after **1 hit**; a 1 h write after **2 hits**. For Aura's bursty turn pattern (5–15 LLM calls in a 60 s window per active conversation), 5 m is almost always correct; switch to 1 h only for the system-prompt block in a workload that bursts twice an hour or more.
- **Observability:** `response.usage.cache_creation_input_tokens`, `response.usage.cache_read_input_tokens`, `response.usage.input_tokens` (uncached). Sum = total prompt tokens.
- **API shape:**
  ```python
  client.messages.create(
      model="claude-opus-4-7",
      max_tokens=1024,
      system=[
          {"type": "text", "text": SOUL_AGENT_TOOLS,
           "cache_control": {"type": "ephemeral", "ttl": "1h"}},  # stable
          {"type": "text", "text": per_user_overlay},             # volatile
      ],
      tools=[..., {"name": "last_tool",
                   "cache_control": {"type": "ephemeral"}}],       # cache tool defs
      messages=[..., {"role": "user", "content": [
          {"type": "text", "text": last_user_msg,
           "cache_control": {"type": "ephemeral"}}                  # sliding anchor
      ]}],
  )
  ```

## OpenAI

- **Official docs:** <https://developers.openai.com/api/docs/guides/prompt-caching> / <https://openai.com/index/api-prompt-caching/>
- **2026 state:** Fully automatic, no opt-in, no marker. Applied transparently on all GPT-4o-and-newer models (gpt-4o, gpt-4o-mini, gpt-4.1, gpt-5.1, gpt-5.2, gpt-5.4, gpt-5.5, gpt-5.5-pro, fine-tunes of any of these, and o-series reasoners). Pre-GPT-5 models are **not** eligible on the Batch API.
- **Eligibility:**
  - Minimum prefix: **1 024 tokens**, then matches grow in **128-token increments**.
  - Cache key includes the **first ~256 tokens** for routing (model-specific); identical first 256 tokens → identical routing → highest hit rate. Use `prompt_cache_key` (string) as an extra routing hint when you have many users with different but overlapping prefixes.
- **TTL / retention:**
  - In-memory default: idle 5–10 min, max 1 h.
  - **GPT-5.5 and later:** GPU-local extended storage up to **24 h**; in-memory is no longer supported. Set `prompt_cache_retention: "24h"` to opt into the long window.
- **Billing:** cached input tokens are billed at a flat **discount versus the model's standard input price** — historically **50 %** on GPT-4o-family ($0.40 vs $4.00 per 1 M tokens for gpt-4o-2024-08-06), trending toward **90 %** on GPT-5+ tiers in published pricing. No write surcharge. No storage fee. Effectively free to enable.
- **Observability:** `usage.prompt_tokens_details.cached_tokens` (integer). Compute hit rate yourself.
- **API shape:**
  ```python
  client.chat.completions.create(
      model="gpt-5.5",
      messages=[{"role": "system", "content": SOUL_AGENT_TOOLS},
                {"role": "user",   "content": user_msg}],
      prompt_cache_key="aura:overlay-v17",       # routing hint
      prompt_cache_retention="24h",              # GPT-5.5+
  )
  # response.usage.prompt_tokens_details.cached_tokens
  ```

## Google Gemini

- **Official docs:** <https://ai.google.dev/gemini-api/docs/caching>
- **2026 state:** Two modes coexist. **Implicit caching** is on by default for all Gemini 2.5+ and 3.x models (no code change, opportunistic, no SLA). **Explicit caching** uses a separate `caches` resource you create up-front, with guaranteed discount on every reference.
- **Eligibility:**
  - Minimum prefix: **1 024 tokens** for Flash (2.5 / 3.5), **4 096 tokens** for Pro (2.5 / 3-Pro-Preview).
  - Explicit caches are first-class objects (`cache.name`) with `display_name`, `model`, `create_time`, `update_time`, `expire_time`, `usage_metadata`.
- **TTL:** configurable (any duration). Default **1 h**. No hard min or max. Storage is billed for the lifetime of the cache.
- **Billing:**
  | Component | Cost |
  |-----------|------|
  | Cache read | 0.10× base input (90 % discount on Gemini 2.5+; 75 % on 2.0) |
  | Cache storage | $4.50 / 1 M tok / hour (Pro), $1.00 / 1 M tok / hour (Flash) |
  | Output | unchanged |
  - Storage cost is what makes Gemini structurally different: caching a 25 K prefix on 3-Pro for 1 h costs about **$0.11 idle**, regardless of hits. For Aura, the break-even versus letting the prefix re-encode each turn is ~3–5 cache reads per hour. Below that, implicit caching is the safer default.
- **Observability:** `cache.usage_metadata` (total tokens cached); response surfaces `cached_content_token_count`.
- **API shape:**
  ```python
  cache = client.caches.create(
      model="models/gemini-3.5-flash",
      config=types.CreateCachedContentConfig(
          contents=[SOUL_AGENT_TOOLS_TOOL_DEFS],
          ttl="3600s"))
  resp = client.models.generate_content(
      model="models/gemini-3.5-flash",
      contents=user_msg,
      config=types.GenerateContentConfig(cached_content=cache.name))
  ```

## vLLM (self-host)

- **Official docs:** <https://docs.vllm.ai/en/stable/design/prefix_caching/>
- **2026 state:** Automatic prefix caching is on by default; production-ready; multiple eviction policies under active development.
- **Eligibility:** any common prefix at block granularity (typically 16 tokens/block). No minimum.
- **Eviction:** **LRU on reference-count = 0** with tie-break on access time, then on "block at the end of the longest prefix" (so longer prefixes are preferentially kept). **No pinning** in stable: feature request open (vllm#23083). 2026 experimental: **T-LRU** (Tail-Optimized LRU) reduces P95 TTFT by up to 27.4 % on conversation workloads (vllm#37823); **frequency + cost aware** eviction in design (vllm#23641).
- **Pitfall:** the eviction algorithm does **not** know that your system prompt is high-value. Under a noisy multi-tenant load it can be evicted and you'll see TTFT spikes. **Mitigation 2026:** prefix-aware routing on the *load balancer* (Ray Serve's `prefix-aware routing` is the canonical reference impl) so identical prefixes hit the same vLLM replica.
- **Observability:** `/metrics` endpoint exposes `vllm:prefix_cache_hits` and `vllm:prefix_cache_queries`; compute hit rate per replica.
- **API shape:** no code change needed if you call vLLM with OpenAI-compatible client. Confirm with:
  ```bash
  curl http://vllm:8000/metrics | grep prefix_cache
  ```

## llama.cpp (self-host, single-binary)

- **Official docs:** <https://github.com/ggml-org/llama.cpp/discussions/13606> (KV cache reuse tutorial), <https://github.com/ggml-org/llama.cpp/discussions/20574> (host-memory caching tutorial, 2026).
- **2026 state:** `cache_prompt: true` is the default on `llama-server`. Major 2026 addition: **host-memory prompt caching** via `--cram <GB>` flag — pre-computed prompt representations spill from VRAM to system RAM, shared across slots, hugely improving TTFT for repeated prefixes.
- **Multi-user mechanics:**
  - `-np N` sets number of concurrent slots (recommended 4–8).
  - Slot assignment uses prompt-similarity matching (default threshold 0.5 — at least 50 % of new prompt must match an existing slot's KV).
  - You can pin a request to a slot with `id_slot` (e.g. for sticky per-conversation routing).
- **Known regression (open, 2026):** under `-np > 1`, **prompt-cache checkpoints are slot-local** (llama.cpp#22942). If a second request with a matching prefix is routed to a different slot than the one that holds the matching checkpoint, the request falls through to a *cold prefill* — even though a usable checkpoint exists on the same server. Most visible on agent / RAG / chat workloads with shared system prompts.
- **Mitigation for Aura's embed sidecar pattern:** if we ever serve completions from llama.cpp (we currently only embed), pin each conversation to a slot via `id_slot = hash(conv_id) % N`, or run `-np 1` and serialize.
- **Observability:** server emits `slot.cache.tokens` / `slot.processed.tokens` per request — hit rate = `slot.cache.tokens / (slot.cache.tokens + slot.processed.tokens)`.
- **API shape:**
  ```bash
  curl http://llama:8080/completion -d '{
    "prompt": "<system>...<user>...",
    "cache_prompt": true,
    "id_slot": 3,
    "n_predict": 256 }'
  ```

---

## Cross-provider abstraction

| Library | What it does in 2026 | Verdict for Aura |
|---------|---------------------|------------------|
| **LiteLLM** | Auto-injects `cache_control` checkpoints; translates Anthropic-style `cache_control` to Gemini context-caching API; full prompt-cache billing surfaced for Anthropic, OpenAI, Gemini, Vertex, Bedrock, DeepSeek. Configurable via UI or `litellm config.yaml`. | Best fit if we ever add a third LLM backend. Today (OpenAI-compatible only) it adds a proxy hop we don't need. |
| **OpenRouter** | Provider sticky routing keeps subsequent requests on the same upstream provider to preserve cache warmth. Top-level `cache_control` only works when routed to Anthropic direct; explicit per-block breakpoints work via Bedrock/Vertex too. Bills you the upstream price + small markup. | Useful if we want one-key access to many providers; cache works as long as we keep the same routing. |
| **Vercel AI SDK** | `providerOptions.anthropic.cacheControl: { type: 'ephemeral' }` for Anthropic; OpenAI/Gemini/DeepSeek cache automatically. `caching: 'auto'` on AI Gateway auto-inserts a breakpoint at the end of the static section. Returns `cache_creation_input_tokens` in `providerMetadata`. | TS-side helper. Not relevant to Aura's Go agent loop. |

### Consensus on multi-turn agent loops (arXiv 2601.06007 + Anthropic guide + OpenRouter notes)

1. **Place the breakpoint at the *end of the stable prefix*** — not at the head of messages. The cache hierarchy `tools → system → messages` means any change to tools invalidates everything after; any change to system invalidates messages.
2. **Exclude dynamic tool results from the cached prefix.** Caching tool results explicitly *increases* latency because each new tool result re-anchors the boundary. Keep the breakpoint on the last stable system block; let `tool_result` blocks live un-cached after it.
3. **Use a sliding "tail anchor" breakpoint.** Because Anthropic's lookback window is only 20 blocks, a single head-of-prompt breakpoint silently stops hitting after a 20-step agent loop. Solution: on every turn, *also* put a `cache_control` marker on the last `user` or `tool_result` block. With 4 breakpoints available, dedicate 2 to "stable" (tools, system) and 1–2 to "rolling" (last assistant turn, last user turn).
4. **Keep timestamps and per-user data *after* the breakpoint.** Date-stamping the system prompt is the #1 reported cause of 0 % hit rates.
5. **Measure hit rate every deploy.** Sample `cache_read_input_tokens / total_input_tokens` and alert if it drops below a baseline (Anthropic's reference target: ≥ 70 % on stable workloads).

Quantified payoff from the paper (across providers, agentic workloads with 10 K-token system prompt and 10–50 tool calls): **41–80 % input-cost reduction**, **13–31 % TTFT improvement**.

---

## ROI model for Aura

**Assumptions:**
- Today: ~10 K tokens of stable prefix per turn (SOUL + AGENT + USER + TOOLS overlay + tool definitions + skill manifests).
- Post-MCP-roundup target: ~25 K tokens of stable prefix.
- Typical conversation burst: 8 LLM turns within 5 min (Aura's `AURA_AGENT_LOOP_MAX_STEPS=8`), then sometimes a follow-up 30–60 min later.
- Output stays ~500 tokens average per turn.
- We pick **gpt-4o** (or compat) and **claude-sonnet-4.6** and **gemini-3.5-flash** as representative endpoints.

### Cost per 8-turn burst, 25 K prefix

| Provider / Model | Without cache | With cache | Δ |
|------------------|---------------|------------|---|
| gpt-4o ($2.50 / 1 M in) | 8 × 25 K × $2.50 = **$0.500** | 1 × 25 K × $2.50 + 7 × 25 K × $1.25 = $0.062 + $0.219 = **$0.281** | **−44 %** (50 % discount tier) |
| gpt-5.5 ($1.25 / 1 M in, 90 % disc.) | 8 × 25 K × $1.25 = **$0.250** | 1 × 25 K × $1.25 + 7 × 25 K × $0.125 = $0.031 + $0.022 = **$0.053** | **−79 %** |
| claude-sonnet-4.6 ($3 / 1 M in, 5 m TTL) | 8 × 25 K × $3 = **$0.600** | 1 × 25 K × $3.75 (1.25× write) + 7 × 25 K × $0.30 = $0.094 + $0.053 = **$0.147** | **−76 %** |
| claude-sonnet-4.6 (1 h TTL) | $0.600 | 1 × 25 K × $6 (2× write) + 7 × 25 K × $0.30 = $0.150 + $0.053 = **$0.203** | **−66 %** (worth it only if 2nd burst within the hour) |
| gemini-3.5-flash ($0.30 / 1 M in) | 8 × 25 K × $0.30 = **$0.060** | 1 × 25 K × $0.30 + 7 × 25 K × $0.03 + storage 25 K × $1 / hr / 1 M × (5 min ⇒ 1/12 h) ≈ $0.0075 + $0.0053 + $0.0002 = **$0.013** | **−78 %** |

### Latency delta (TTFT, single-turn)

- **Anthropic Claude:** cached read shaves ~85 ms per 10 K cached tokens per the paper, so ~210 ms saved on 25 K prefix; TTFT goes from ~1.1 s → ~0.9 s (rough order).
- **OpenAI GPT-4o/5:** TTFT improvement reported as 13–31 % on agentic workloads (paper).
- **Gemini explicit:** similar magnitude; implicit caching benefits are opportunistic and not guaranteed.

### Recommendation

**Best cost-impact ratio: Anthropic with explicit `cache_control` + 5 m ephemeral (or 1 h on the system block only).** Highest discount (90 %), no storage fee, no per-call overhead, mature observability. Gemini Flash is cheapest absolute but storage cost only pencils out at sustained burst-volume — Flash with implicit caching is the right "set and forget" pick if we ever route low-importance turns there.

**Worst pick for Aura today:** running our own llama.cpp with `-np > 1` for chat completion. The slot-local checkpoint bug (#22942) will silently destroy hit rate on agent loops and there's no fix shipped yet.

---

## Pitfalls to avoid

- **Timestamp in system prompt.** Date or `time.Now()` rendered into the overlay ⇒ 0 % hit rate, *more* expensive than no caching. Detection: `cache_read_input_tokens == 0` despite stable workload. Fix: put dynamic data **after** the last `cache_control` marker, or remove it entirely.
- **Anthropic 20-block lookback expiry.** Long tool-use loops (≥ 20 tool_result blocks since the last cache breakpoint) silently fall out of cache. Detection: hit rate degrades as conversation length grows. Fix: rolling tail-anchor breakpoint on the most recent stable block every turn.
- **Anthropic 5 m TTL surprise (March 2026 regression).** Code that assumed 1 h default since 2024 now thrashes. Fix: always set `ttl` explicitly per breakpoint.
- **Per-user personalization at the front of system prompt.** Destroys shared cache across users. Fix: split into a stable cross-user block (cached) + per-user block (uncached) after the breakpoint.
- **Tool definitions reordered every call.** Aura's MCP reconciler must produce **stable ordering** of tool definitions. Any reorder of `tools[]` invalidates the whole cache. Detection: hit rate drops after every MCP reload. Fix: lexicographic sort + a canonicalization step before sending to the LLM.
- **Streaming + caching + token-counting drift.** Some clients only surface `cache_*_input_tokens` on the final SSE event. If Aura's logger reads `usage` mid-stream it'll log zero and look like a cache miss. Fix: log usage from the final event only.
- **Cross-provider cache_control leaked.** `cache_control` keys in messages sent to OpenAI/Gemini get rejected (or silently ignored, worse). When we add a second provider, strip provider-specific keys at the client boundary.
- **vLLM eviction under load.** No pinning ⇒ stable prefixes can be evicted by a bursty noisy tenant. Detection: prefix_cache_hits / queries < 0.5 on `/metrics`. Mitigation: prefix-aware routing on the LB, or run a dedicated replica for the prefix-stable workload.
- **llama.cpp slot-local checkpoint bug (open as of May 2026, issue #22942).** Concurrent agents on same server miss the cache. Fix: `-np 1` or sticky `id_slot` per conversation.
- **Implicit caching is opportunistic, not guaranteed.** OpenAI's automatic and Gemini's implicit modes give **no SLA**. If you need predictable cost, use the **explicit** path (Anthropic `cache_control` or Gemini `caches.create`).
- **Cache hit rate not monitored = silently regressed.** Add `cache_read_input_tokens / total_input_tokens` to the dashboard. CI test: send the same fixture twice, assert second call has `cached > 0.7 × prompt_tokens`.

---

## Sources

- [Anthropic — Prompt caching guide](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)
- [Anthropic — API pricing](https://platform.claude.com/docs/en/about-claude/pricing)
- [Claude prompt cache TTL regression to 5 minutes — March 2026](https://github.com/anthropics/claude-code/issues/46829)
- [Claude prompt caching in 2026: 5-minute TTL change](https://dev.to/whoffagents/claude-prompt-caching-in-2026-the-5-minute-ttl-change-thats-costing-you-money-4363)
- [OpenAI — Prompt caching guide](https://developers.openai.com/api/docs/guides/prompt-caching)
- [OpenAI — Prompt caching announcement](https://openai.com/index/api-prompt-caching/)
- [OpenAI cookbook — Prompt Caching 201](https://developers.openai.com/cookbook/examples/prompt_caching_201)
- [Google — Gemini context caching docs](https://ai.google.dev/gemini-api/docs/caching)
- [Google — Vertex / Gemini Enterprise context cache overview](https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/context-cache/context-cache-overview)
- [Don't Break the Cache (arXiv 2601.06007) — agentic prompt caching evaluation](https://arxiv.org/abs/2601.06007)
- [vLLM — Automatic prefix caching design doc](https://docs.vllm.ai/en/stable/design/prefix_caching/)
- [vLLM — Persistent / pinned prefixes feature request (#23083)](https://github.com/vllm-project/vllm/issues/23083)
- [vLLM — Tail-Optimized LRU RFC (#37823)](https://github.com/vllm-project/vllm/issues/37823)
- [Ray Serve — Prefix-aware routing](https://docs.ray.io/en/latest/serve/llm/user-guides/prefix-aware-routing.html)
- [llama.cpp — KV cache reuse tutorial (#13606)](https://github.com/ggml-org/llama.cpp/discussions/13606)
- [llama.cpp — Host-memory prompt caching tutorial 2026 (#20574)](https://github.com/ggml-org/llama.cpp/discussions/20574)
- [llama.cpp — Slot-local checkpoint bug under -np > 1 (#22942)](https://github.com/ggml-org/llama.cpp/issues/22942)
- [LiteLLM — Auto-inject prompt caching checkpoints](https://docs.litellm.ai/docs/tutorials/prompt_caching)
- [LiteLLM — Prompt caching across providers](https://docs.litellm.ai/docs/completion/prompt_caching)
- [OpenRouter — Prompt caching best practices](https://openrouter.ai/docs/guides/best-practices/prompt-caching)
- [Vercel AI Gateway — Automatic caching](https://vercel.com/docs/ai-gateway/models-and-providers/automatic-caching)
- [Vercel AI SDK — Anthropic provider (cache_control via providerOptions)](https://ai-sdk.dev/providers/ai-sdk-providers/anthropic)
- [ProjectDiscovery — How we cut LLM cost by 59 % with prompt caching](https://projectdiscovery.io/blog/how-we-cut-llm-cost-with-prompt-caching)
- [PromptHub — Prompt caching with OpenAI, Anthropic, Google compared](https://www.prompthub.us/blog/prompt-caching-with-openai-anthropic-and-google-models)
