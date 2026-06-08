---
spike: 042
name: adaptive-budget-policy-shim
type: standard
validates: "Given Aura's current llm.Request seam, when an AutoThink-inspired complexity classifier adjusts only MaxTokens, then messages[0] remains byte-stable and no activation steering or proxy sidecar is required."
verdict: VALIDATED
related: [037-agentmd-messages1-cache-invariant, 040-adaptive-reasoning-source-truth, 041-optillm-autothink-runtime-fit]
tags: [adaptive-reasoning, llm-routing, prompt-cache, max-tokens, policy-shim]
---

# Spike 042: Adaptive Budget Policy Shim

## What This Validates

Given Aura already has a provider-neutral `llm.Request{Messages, MaxTokens, ToolChoice}`, when a simple AutoThink-inspired classifier maps user queries to low/high output budgets, then the useful budget-routing subset can fit today without changing the stable system prompt or requiring hidden-state access.

## Research

| Approach | Tool/Library | Pros | Cons | Status |
|---|---|---|---|---|
| Full AutoThink | OptiLLM AutoThink | Classification, token budget, steering vectors | Requires PyTorch model loop and hooks; not OpenAI proxy-compatible | Too heavy for immediate Aura |
| Provider `reasoning_effort` | OpenAI-style/provider-specific option | Semantically closer to "think harder" | Aura's current `llm.Request` has no such field; provider-specific | Future seam |
| MaxTokens policy shim | Pure Go over `llm.Request` | Fits current request shape; preserves cache prefix; no sidecar | Controls budget, not activation steering | Chosen |

This spike deliberately does not evaluate GPQA/MMLU accuracy. It validates that Aura can add a safe budget-routing seam now and leave performance evaluation for a live model benchmark.

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

## Results

Verdict: VALIDATED.

Evidence from `go run`:

- Simple queries such as arithmetic, one-sentence summary, and factual lookup mapped to LOW with `max_tokens=1024`.
- Proof, architecture tradeoff, and distributed debugging prompts mapped to HIGH with `max_tokens=4096`.
- `messages[0]` stayed byte-stable for every sample, protecting Aura's stable-prefix/cache invariant.

Build signal: add a small policy layer near request assembly if Aura wants adaptive reasoning now. Do not present it as AutoThink-equivalent unless/until a live benchmark validates quality gains.

