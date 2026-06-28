---
spike: 074
name: fc270m-finetuned-vs-baseline
type: standard
validates: "Given the spike-072 Colab finetune produces an Aura-tuned GGUF, when the spike-071 harness scores it vs the base model AND vs the shipped embedding ranker, then a head-to-head scorecard shows whether finetuning moves base→usable, at what latency, and whether it earns the Slice-13 slot"
verdict: PENDING
related: [071-fc270m-baseline-and-slot, 072-fc270m-finetune-toolchain-fit, 073-fc270m-dataset-from-registry, 058-unified-embedding-index]
tags: [functiongemma, eval, finetune, head-to-head, slice-13, latency]
---

# Spike 074: finetuned FunctionGemma vs baseline (the payoff)

## What This Validates

The whole chain's verdict: does finetuning on Aura's tools (073 data, 072 toolchain)
move the base model from **unusable** (spike 071: top-1 ≈ 1/12, ~80% refusal) to
**usable** for local function-call generation — and does it earn a production slot
that the already-shipped free embedding ranker (spikes 054-058) doesn't already own?

The **eval harness is spike 071, unchanged** — point it at the finetuned GGUF. Same
12 grounded IT/EN queries, same valid-call / top-1 / arg-correct / latency metrics, so
the numbers are directly comparable to the baseline.

## How to Run

After the operator runs `072/FunctionGemma_270M_Aura.ipynb` on Colab and downloads
`functiongemma-270m-aura-q8_0.gguf` into this dir:

```bash
# Serve the finetuned GGUF (GPU; CPU also works via the :server image, -ngl 0)
docker run -d --name fc270m-aura --gpus all -p 127.0.0.1:8097:8097 \
  -v "$PWD:/models" ghcr.io/ggml-org/llama.cpp:server-cuda \
  -m /models/functiongemma-270m-aura-q8_0.gguf \
  --jinja --host 0.0.0.0 --port 8097 --ctx-size 8192 -ngl 99

# Score it with the 071 harness (IT + EN)
FC_BASE_URL=http://127.0.0.1:8097 FC_TAG=aura-ft-IT  go run ./.planning/spikes/071-fc270m-baseline-and-slot
FC_BASE_URL=http://127.0.0.1:8097 FC_TAG=aura-ft-EN FC_LANG=en go run ./.planning/spikes/071-fc270m-baseline-and-slot
```

(Launch docker from PowerShell, not Git-Bash — MSYS path mangling, CONVENTIONS.)

## What to Expect / Scorecard

Fill from the harness `[SCORE]` + `[LATENCY]` lines (base numbers are spike 071):

| Variant | emitted-call | top1-tool | arg-correct | p50 / p95 latency |
|---|---|---|---|---|
| base IT (071) | 2/12 | 1/12 | 1/12 | GPU 300ms / 1.1s |
| base EN (071) | 3/12 | 1/12 | 1/12 | — |
| **finetuned IT** | _?_ | _?_ | _?_ | _?_ |
| **finetuned EN** | _?_ | _?_ | _?_ | _?_ |

Plus the slot question (vs the shipped embedding ranker, spikes 054-058):

| Capability | embedding ranker (shipped) | finetuned fc270m |
|---|---|---|
| selection ("which tool?") | ~85µs, free, top1 ~8/15 | _?_ |
| argument generation | ✗ cannot | _?_ (its reason to exist) |

## Verdict Criteria (decide when the numbers land)

- **VALIDATED** — finetuned IT valid-call ≫ 2/12 and top-1/arg-correct reach a usable
  band (target ≥ ~8/12 top-1, args mostly right), at GPU latency ≤ ~1.6s/call. → build
  the Slice-13 local/offline function-call tier on it.
- **PARTIAL** — fires far more reliably but args or namespace disambiguation lag, or
  CPU latency is the only option (no GPU headroom beside the primary model). → keep as
  offline-only fallback; revisit dataset scale (073 slot lists).
- **INVALIDATED** — still refuses / wrong tool at this data scale. → the slot isn't
  worth it; embeddings own selection and the cloud LLM owns arg-generation on the hot
  path; shelve until a much larger dataset or a different base.

## Status

**PENDING the operator's Colab finetune.** Everything upstream is ready: base is
unusable-but-sound (071), dataset is generated and notebook-ready (073), the Colab
notebook is authored (072), and the eval harness is the 071 probe re-pointed. This
README is the run-book + scorecard to fill once the finetuned GGUF exists.
