---
spike: 042
name: adaptive-budget-policy-shim
type: standard
validates: "Given Aura's current llm.Request seam, when an AutoThink-inspired classifier maps prompts to no/small/deep reasoning tiers and adjusts MaxTokens, then messages[0] remains byte-stable and no activation steering or proxy sidecar is required."
verdict: VALIDATED
related: [037-agentmd-messages1-cache-invariant, 040-adaptive-reasoning-source-truth, 041-optillm-autothink-runtime-fit]
tags: [adaptive-reasoning, llm-routing, prompt-cache, max-tokens, policy-shim, openrouter, reasoning-tokens, reasoning-effort]
---

# Spike 042: Adaptive Budget Policy Shim

## What This Validates

Given Aura already has a provider-neutral `llm.Request{Messages, MaxTokens, ToolChoice}`, when a simple AutoThink-inspired classifier maps user queries to no/small/deep reasoning tiers, then the useful routing subset can fit today without changing the stable system prompt or requiring hidden-state access.

## Research

| Approach | Tool/Library | Pros | Cons | Status |
|---|---|---|---|---|
| Full AutoThink | OptiLLM AutoThink | Classification, token budget, steering vectors | Requires PyTorch model loop and hooks; not OpenAI proxy-compatible | Too heavy for immediate Aura |
| OpenRouter `reasoning` / `reasoning_effort` | OpenRouter chat parameters | Documented provider-native way to request reasoning effort or reasoning-token budget | Aura's current `llm.Request` has no such field; model support varies; reasoning tokens are billed output tokens | Documented next seam |
| Tiered policy shim | Pure Go over `llm.Request` plus projected OpenRouter reasoning JSON | Fits current request shape; preserves cache prefix; no sidecar; distinguishes no/small/deep reasoning | Projected reasoning is not sent until `llm.Request` gains a request-side field | Chosen |

This spike deliberately does not evaluate GPQA/MMLU accuracy. It validates that Aura can add a safe budget-routing seam now and leave performance evaluation for a live model benchmark.

## OpenRouter Parameter Follow-up (2026-06-08)

The OpenRouter parameter docs make the provider-native seam more concrete than the first pass assumed:

- `max_tokens` and `max_completion_tokens` cap generated response tokens, matching the current Aura `MaxTokens` shim shape.
- `reasoning` is the richer object for thinking-token models: `effort`, `max_tokens`, `exclude`, and `enabled`.
- `reasoning_effort` also exists as an OpenAI-style enum (`xhigh`, `high`, `medium`, `low`, `minimal`, `none`), but the guide frames `reasoning` as the unified cross-provider object.
- `include_reasoning` is a deprecated alias for `reasoning.exclude`; new Aura code should prefer `reasoning.exclude`.
- Reasoning tokens count as output tokens and are billed. If using an Anthropic-style reasoning budget, the outer response `max_tokens` must leave room for the final answer.
- If reasoning models are used across tool calls, Aura needs an explicit policy for preserving `reasoning_details`; that is separate from the current stream-only reasoning display path.

Sources:

- <https://openrouter.ai/docs/api/reference/parameters>
- <https://openrouter.ai/docs/guides/best-practices/reasoning-tokens>

## How to Run

```powershell
go run ./.planning/spikes/042-adaptive-budget-policy-shim
```

## What to Expect

The harness runs a small query corpus through a deterministic classifier and asserts:

- `ciao` uses no reasoning: `reasoning.effort=none`, `reasoning.exclude=true`, `max_tokens=512`;
- `cerca notizie di cuneo` uses small reasoning: `reasoning.effort=low`, `reasoning.exclude=false`, `max_tokens=2048`;
- `scrivi uno script di scraping di la stampa` uses deep reasoning: `reasoning.effort=high`, `reasoning.exclude=false`, `max_tokens=4096`;
- `messages[0]` is byte-identical before and after adaptation;
- `ToolChoice` is preserved;
- no think tags or hidden-state dependencies are introduced.

## Investigation Trail

1. Aura's `internal/llm.Request` already carries `MaxTokens` and `ToolChoice`, but no request-side provider-neutral reasoning config.
2. The safest immediate policy is to alter only `MaxTokens` while returning an observable decision record and a projected OpenRouter reasoning JSON shape.
3. The harness copies `Messages` and `Tools` before mutation so the original request remains immutable.
4. The first sample corpus covered low/simple and high/reasoning prompts. All expected classifications and token budgets passed.
5. OpenRouter's docs move provider-native reasoning from "speculative future field" to "known request extension": Aura should model it as a provider-neutral `Reasoning` config, then project to OpenRouter's `reasoning` object where supported.
6. The operator benchmark corpus adds a middle tier: greeting/trivial prompts -> `none`, news/search prompts -> `low`, code/proof/debug prompts -> `high`.

## Results

Verdict: VALIDATED.

Evidence from `go run`:

- `ciao` mapped to `NO_REASONING`, `reasoning={"effort":"none","exclude":true}`, `max_tokens=512`.
- `cerca notizie di cuneo` mapped to `SMALL_REASONING`, `reasoning={"effort":"low","exclude":false}`, `max_tokens=2048`.
- `scrivi uno script di scraping di la stampa` mapped to `DEEP_REASONING`, `reasoning={"effort":"high","exclude":false}`, `max_tokens=4096`.
- Proof, architecture tradeoff, and distributed debugging prompts still mapped to `DEEP_REASONING` with `max_tokens=4096`.
- `messages[0]` stayed byte-stable for every sample, protecting Aura's stable-prefix/cache invariant.

Production follow-up validation:

- `internal/llm/openai_compat.TestAdaptiveReasoningItalianCorpusE2E` exercises the real PromptBuilder -> OpenAI-compatible request-body path with 60 natural Italian queries.
- The score gate is `>=90%`; the first committed corpus run scored `100.0% (60/60)`.
- The corpus covers greeting/factual prompts (`none`/512), news/search/current lookup prompts (`low`/2048), and code/debug/design/proof prompts (`high`/4096).

Build signal: production now has a small `MaxTokens` policy layer near request assembly plus provider-neutral `Reasoning` config projected to OpenRouter's `reasoning` object. The implementation preserves `messages[0]`, `ToolChoice`, endpoint configurability, and an operator opt-out via `AURA_LLM_ADAPTIVE_REASONING=false`. Do not present the shim as AutoThink-equivalent unless/until a live benchmark validates quality gains.
