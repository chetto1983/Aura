---
spike: 040
name: adaptive-reasoning-source-truth
type: standard
validates: "Given THU-KEG/AdaptThink and codelion/AutoThink source material, when audited against Aura's runtime seams, then adaptive reasoning is separated into training-only, heavy-runtime, and portable policy surfaces."
verdict: VALIDATED
related: [020-vllm-sidecar-4gb-fit, 037-agentmd-messages1-cache-invariant, 041-optillm-autothink-runtime-fit, 042-adaptive-budget-policy-shim]
tags: [adaptive-reasoning, autothink, adaptthink, optillm, phase-llm-routing]
---

# Spike 040: Adaptive Reasoning Source Truth

## What This Validates

Given the similarly named AdaptThink and AutoThink projects, when their current papers/repos are audited against Aura's LLM/router/prompt constraints, then Aura knows which parts are runtime-usable and which parts are training or activation-steering research only.

## Research

| Approach | Source | Pros | Cons | Status |
|---|---|---|---|---|
| AdaptThink | THU-KEG/AdaptThink + arXiv 2505.13417 | Teaches a model to choose Thinking vs NoThinking; released HF models exist | Training path is VeRL/vLLM and H800/A100 class; not a runtime policy for Aura | Prior art / model-family watch |
| AutoThink full | codelion/optillm/autothink + SSRN 5253327 | Inference-time classifier + token budget + steering vectors; reported GPQA gain | Full implementation manually drives a local PyTorch model and uses forward hooks | Heavy sidecar candidate only |
| AutoThink budget subset | AutoThink classifier/budget idea | Provider-neutral; can map complexity to `llm.Request.MaxTokens` | Does not include activation steering or paper benchmark guarantees | Chosen for spike 042 |

Sources checked:

- GitHub `THU-KEG/AdaptThink`: https://github.com/THU-KEG/AdaptThink
- arXiv AdaptThink paper page: https://arxiv.org/abs/2505.13417
- HF AdaptThink model card example: https://huggingface.co/THU-KEG/AdaptThink-1.5B-delta0.05
- Reddit AutoThink launch thread: https://www.reddit.com/r/LocalLLaMA/comments/1kwqt64/research_autothink_adaptive_reasoning_technique/
- SSRN AutoThink page: https://papers.ssrn.com/sol3/papers.cfm?abstract_id=5253327
- HF AutoThink writeup: https://huggingface.co/blog/codelion/autothink
- HF PTS writeup: https://huggingface.co/blog/codelion/pts

Scratch source audit root:

`D:\tmp\aura-spike-040-adaptive-reasoning`

Pinned local checkouts:

| Repo | Commit |
|---|---|
| AdaptThink | `9e2c0e2` |
| optillm | `df018d6` |
| pts | `f5750d6` |
| adaptive-classifier | `e2e819e` |

Chosen approach: audit both families from source, then validate the portable subset separately. This avoids conflating AdaptThink's training-time learned mode choice with AutoThink's inference-time classifier and activation steering.

## How to Run

```powershell
go run ./.planning/spikes/040-adaptive-reasoning-source-truth
```

Optional override:

```powershell
$env:AURA_ADAPTIVE_REASONING_ROOT='D:\tmp\aura-spike-040-adaptive-reasoning'
go run ./.planning/spikes/040-adaptive-reasoning-source-truth
```

## What to Expect

The harness checks concrete source signals in both repositories and exits 0 only if all expected markers are present.

## Investigation Trail

1. Initial target was `THU-KEG/AdaptThink`, but the operator supplied a Reddit AutoThink link. This shifted the spike from one repo to the broader adaptive-reasoning family.
2. AdaptThink source inspection found RL/training-specific markers: VeRL/vLLM, `nothinking_ratio`, log-prob adjustment, and documented H800/A100 scale.
3. AutoThink source inspection found runtime markers: fallback classifier, high/low token budgets, direct Transformers model stepping, PyTorch forward hooks, and HF steering-vector datasets.
4. The audit split the idea into three implementation surfaces: prior-art/model watch, heavy PyTorch sidecar, and lightweight Aura policy shim.

## Results

Verdict: VALIDATED.

Evidence from `go run`:

- AdaptThink is a training/RL source truth, useful as prior art and released model-family signal, not as Aura runtime code.
- AutoThink is inference-time source truth, but full steering/generation requires a local PyTorch model process.
- Aura should carry forward the portable classifier/budget policy and keep activation steering as a separate future sidecar decision.

