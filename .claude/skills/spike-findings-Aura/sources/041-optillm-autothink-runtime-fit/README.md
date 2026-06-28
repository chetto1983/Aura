---
spike: 041
name: optillm-autothink-runtime-fit
type: standard
validates: "Given OptiLLM's proxy and AutoThink implementation, when audited for Aura's OpenAI-compatible runtime, then full AutoThink is classified as a heavy local-model sidecar rather than a drop-in proxy feature."
verdict: VALIDATED
related: [040-adaptive-reasoning-source-truth, 042-adaptive-budget-policy-shim]
tags: [adaptive-reasoning, autothink, optillm, sidecar, openai-compatible]
---

# Spike 041: OptiLLM AutoThink Runtime Fit

## What This Validates

Given OptiLLM can generally expose an OpenAI-compatible proxy, when the AutoThink implementation is checked specifically, then Aura knows whether `autothink-*` can be routed like a normal provider or whether it needs a resident local model process.

## Research

| Approach | Tool/Library | Pros | Cons | Status |
|---|---|---|---|---|
| OptiLLM proxy-only sidecar | `Dockerfile.proxy_only`, `requirements_proxy_only.txt` | OpenAI-compatible HTTP surface; no local model deps | Upstream marks AutoThink as "N/A for proxy"; proxy-only deps omit Transformers/Torch | Not enough for AutoThink |
| OptiLLM full sidecar | Full `requirements.txt` | Includes Torch, Transformers, datasets, adaptive-classifier | Heavy Python ML runtime; resident model required; target layer/vector tuning | Feasible only as future heavy sidecar |
| Aura-native budget policy | Go shim over `llm.Request.MaxTokens` | No new sidecar; preserves provider-neutral routing | No activation steering; no benchmark claim | Chosen immediate path |

Key source facts:

- OptiLLM's README describes the project as OpenAI-compatible, but its approach matrix lists AutoThink as not proxy-compatible.
- AutoThink's public entrypoint accepts `PreTrainedModel` and `PreTrainedTokenizer`, not a remote OpenAI client.
- The processor manually advances generation with `DynamicCache` and `self.model(input_ids=...)`.
- Steering calls `register_forward_hook` on model layers and loads model-specific HF vector datasets.
- The classifier can auto-run `pip install adaptive-classifier` at runtime; Aura must not inherit that behavior.

## How to Run

```powershell
go run ./.planning/spikes/041-optillm-autothink-runtime-fit
```

Optional override:

```powershell
$env:AURA_OPTILLM_ROOT='D:\tmp\aura-spike-040-adaptive-reasoning\optillm'
go run ./.planning/spikes/041-optillm-autothink-runtime-fit
```

## What to Expect

The harness verifies proxy compatibility claims, dependency split, AutoThink's local-model API shape, manual generation loop, runtime install behavior, and forward-hook requirement.

## Investigation Trail

1. The generic OptiLLM README looked promising because the project is an OpenAI-compatible proxy.
2. The approach matrix was the first red/green splitter: AutoThink is explicitly outside proxy-only operation.
3. `requirements_proxy_only.txt` omits `torch`, `transformers`, `adaptive-classifier`, and `datasets`; full requirements include them.
4. `autothink.py`, `processor.py`, and `steering.py` confirmed that the technique needs local model/tokenizer objects and layer access.
5. `server.py`'s `known_approaches` list does not include `autothink`; routing an Aura model name through OptiLLM's normal proxy parser will not invoke AutoThink.

## Results

Verdict: VALIDATED.

Full AutoThink is not a drop-in OpenAI-compatible proxy feature for Aura. A future implementation would need to choose one of two paths:

- heavy PyTorch sidecar with resident local model, steering-vector datasets, and target-layer tuning;
- lightweight Aura-native policy that borrows only complexity classification and token-budget allocation.

The immediate build signal is the second path.

