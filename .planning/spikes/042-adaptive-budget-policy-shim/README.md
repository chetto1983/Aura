---
spike: 042
name: adaptive-budget-policy-shim
type: standard
validates: "Given Aura's current llm.Request seam, when an AutoThink-inspired complexity classifier adjusts only MaxTokens, then messages[0] remains byte-stable and no activation steering or proxy sidecar is required."
verdict: VALIDATED
related: [037-agentmd-messages1-cache-invariant, 040-adaptive-reasoning-source-truth, 041-optillm-autothink-runtime-fit]
tags: [adaptive-reasoning, llm-routing, prompt-cache, max-tokens, policy-shim, openrouter, reasoning-tokens]
---

# Spike 042: Adaptive Budget Policy Shim

## What This Validates

Given Aura already has a provider-neutral `llm.Request{Messages, MaxTokens, ToolChoice}`, when a simple AutoThink-inspired classifier maps user queries to low/high output budgets, then the useful budget-routing subset can fit today without changing the stable system prompt or requiring hidden-state access.

## Research

| Approach | Tool/Library | Pros | Cons | Status |
|---|---|---|---|---|
| Full AutoThink | OptiLLM AutoThink | Classification, token budget, steering vectors | Requires PyTorch model loop and hooks; not OpenAI proxy-compatible | Too heavy for immediate Aura |
| OpenRouter `reasoning` / `reasoning_effort` | OpenRouter chat parameters | Documented provider-native way to request reasoning effort or reasoning-token budget | Aura's current `llm.Request` has no such field; model support varies; reasoning tokens are billed output tokens | Documented next seam |
| MaxTokens policy shim | Pure Go over `llm.Request` | Fits current request shape; preserves cache prefix; no sidecar | Controls budget, not activation steering | Chosen |

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

- easy factual/arithmetic prompts use `max_tokens=1024`;
- proof/debug/architecture prompts use `max_tokens=4096`;
- `messages[0]` is byte-identical before and after adaptation;
- `ToolChoice` is preserved;
- no think tags or hidden-state dependencies are introduced.

## Investigation Trail

1. Aura's `internal/llm.Request` already carries `MaxTokens` and `ToolChoice`, but no explicit provider-neutral `reasoning_effort`.
2. The safest immediate policy is to alter only `MaxTokens` while returning an observable decision record.
3. The harness copies `Messages` and `Tools` before mutation so the original request remains immutable.
4. The first sample corpus covered low/simple and high/reasoning prompts. All expected classifications and token budgets passed.
5. OpenRouter's docs move provider-native reasoning from "speculative future field" to "known request extension": Aura should model it as a provider-neutral `Reasoning` config, then project to OpenRouter's `reasoning` object where supported.

## Results

Verdict: VALIDATED.

Evidence from `go run`:

- Simple queries such as arithmetic, one-sentence summary, and factual lookup mapped to LOW with `max_tokens=1024`.
- Proof, architecture tradeoff, and distributed debugging prompts mapped to HIGH with `max_tokens=4096`.
- `messages[0]` stayed byte-stable for every sample, protecting Aura's stable-prefix/cache invariant.

Build signal: add a small `MaxTokens` policy layer near request assembly if Aura wants adaptive reasoning now. For OpenRouter-backed reasoning models, the next step is not just a raw top-level `reasoning_effort` string; it is a provider-neutral `Reasoning` config that can map LOW/HIGH decisions to OpenRouter's `reasoning` object while preserving `messages[0]`, `ToolChoice`, billing visibility, and any required `reasoning_details` round-trip behavior. Do not present the shim as AutoThink-equivalent unless/until a live benchmark validates quality gains.
