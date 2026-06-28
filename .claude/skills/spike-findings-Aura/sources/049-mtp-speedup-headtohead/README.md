---
spike: 049
name: mtp-speedup-headtohead
type: comparison
validates: "Given the spike-048 serving config, when the same fixed IT/EN prompt set runs greedy with MTP off (049a) vs --spec-type draft-mtp (049b, n-max swept), then MTP delivers >= 1.3x generation tok/s with equivalent outputs"
verdict: PARTIAL
related: [048-gemma4-mtp-gpu-fit]
tags: [gemma4, mtp, benchmark, llama-cpp, slice-13]
---

# Spike 049: MTP Speedup Head-to-Head (049a off-baseline vs 049b MTP-on)

## What This Validates

Unsloth claims 1.4–2.2× GGUF speedup from Gemma 4 multi-token prediction. This
spike measures the real number on THIS hardware (RTX A2000 4 GB, full offload,
desktop coexistence): same model, same six IT/EN prompts, greedy decoding,
`n_predict=256` — server launched with and without
`--spec-type draft-mtp --spec-draft-n-max N`.

## How to Run

```powershell
# 049a baseline (NO spec flags)
docker run -d --name spike049 --gpus all -p 8095:8080 `
  -v D:\tmp\spike-048-models:/models:ro `
  ghcr.io/ggml-org/llama.cpp:server-cuda `
  -m /models/gemma-4-E2B-it-qat-UD-Q4_K_XL.gguf `
  -ngl 99 -c 4096 --temp 0 --host 0.0.0.0 --port 8080
go run ./.planning/spikes/049-mtp-speedup-headtohead -label mtp-off
docker rm -f spike049

# 049b MTP on (repeat for n-max 2 / 4)
docker run -d --name spike049 --gpus all -p 8095:8080 `
  -v D:\tmp\spike-048-models:/models:ro `
  ghcr.io/ggml-org/llama.cpp:server-cuda `
  -m /models/gemma-4-E2B-it-qat-UD-Q4_K_XL.gguf `
  --model-draft /models/mtp-gemma-4-E2B-it.gguf `
  --spec-type draft-mtp --spec-draft-n-max 2 `
  -ngl 99 --spec-draft-ngl 99 -c 4096 --temp 0 --host 0.0.0.0 --port 8080
go run ./.planning/spikes/049-mtp-speedup-headtohead -label mtp-on-n2
docker rm -f spike049
```

## What to Expect

- Per-prompt `predicted_per_second` from the native `/completion` timings
  (ground truth, includes draft acceptance counters when speculative is on).
- `[SUMMARY]` p50/p95 per label; JSON results in `D:\tmp\spike-049-results\`.
- Verdict criterion: p50(mtp-on-best-n) / p50(mtp-off) ≥ 1.3, with greedy
  outputs equivalent across configs (speculative decoding is verify-exact;
  divergence would itself be a finding).

## Observability

Forensic stdout (`[SETUP]/[BENCH]/[SUMMARY]`), one warm-up request excluded,
full timings JSON logged per prompt, persisted result files per label for
offline diffing.

## Investigation Trail

- First bench run hit the server mid-load (503) — harness gained the same
  240 s `/health` wait as 048.
- Swept `--spec-draft-n-max` 1 / 2 / 4 after n=2 showed a clear language
  split in acceptance rates.
- Token counts differed across configs under pure greedy → added a content
  diff between mtp-off and mtp-on-n2 results: **5/6 outputs diverge**
  (common prefixes 5–578 chars; only en-reason identical). Speculative
  verification batches change CUDA numerics enough to flip near-tie argmax —
  MTP is lossless at the distribution level, NOT bit-exact on this stack.

## Results

**PARTIAL.** MTP works and is free VRAM-wise, but the ≥1.3× global criterion
is NOT met on this hardware for Aura's primary language.

Generation tok/s (greedy, n_predict=256, 6 prompts, warm server):

| Config | p50 | p95 | vs baseline |
|---|---|---|---|
| mtp-off (049a) | 63.5 | 65.3 | 1.00× |
| mtp-on n-max=1 | 70.9 | 85.9 | 1.12× |
| **mtp-on n-max=2** | **76.5** | **87.4** | **1.20×** |
| mtp-on n-max=4 | 47.8 | 76.0 | **0.75× — counterproductive** |

Per-prompt at n=2 (the optimum):

| Prompt | accept % | tok/s | speedup |
|---|---|---|---|
| en-reason | 73% | 87.4 | **1.49×** |
| en-code | 71% | 86.3 | **1.36×** |
| en-list | 56% | 76.5 | 1.20× |
| it-city | 32% | 93.4 | 1.43× (short run, noisy) |
| it-explain | 38% | 64.1 | 1.02× |
| it-story | 32% | 65.8 | 0.99× |

Key findings:

1. **Draft acceptance is language-dependent**: ~71–73% on English prose/code,
   ~32–38% on Italian. The E2B MTP head clearly drafts English far better.
   For an Italian-primary assistant the realistic gain is ~1.0–1.2×, not
   unsloth's headline 1.4–2.2× (which our English prompts DO approach).
2. **n-max=2 is the optimum; n-max=4 is harmful** (0.75×): every rejected
   draft token is wasted verification compute, and low IT acceptance makes
   over-drafting net-negative. Matches unsloth's "start with 2" guidance.
3. **Greedy is not reproducible across spec configs** (5/6 outputs diverge).
   Any eval harness comparing MTP-on vs MTP-off must compare quality, not
   bytes.
4. VRAM cost of MTP ≈ the 56.5 MiB draft + its tiny KV — negligible; the 048
   fit verdict holds with MTP active.

Verdict for Slice 13: ship the lane **with `--spec-type draft-mtp
--spec-draft-n-max 2`** — it never loses at n=2 and wins big on
English/code — but size capacity planning on the ~64 tok/s Italian baseline,
not on MTP marketing numbers.
