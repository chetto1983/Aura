# Improving Italian email-labeling accuracy — 2026-11-02

Switching to **d1-3B Q4_K_M**, with the original English instructions, improved the
original 200-email score from **130/200 (65%) to 191/200 (95.5%)**. Prompt changes did
not produce a confirmed improvement for d1-omni-600M Q8_0.

| Dataset | d1-omni-600M Q8_0 | d1-3B Q4_K_M |
|---|---:|---:|
| Original 200 Italian emails | 130/200, 65% | 191/200, 95.5% |
| Additional 80 Italian emails | 54/80, 67.5% | 80/80, 100% |
| Further 40 confirmation emails | 29/40, 72.5% | 40/40, 100% |

All cases are synthetic, authored and labeled by this benchmark's author. The same
English question, option descriptions, Italian label names and Italian subject/body
text were used for each model comparison. No examples are included in the 3B prompt;
no weights were trained. Zero generated tokens and zero API errors were observed.

## Development and confirmation boundaries

The exposed original corpus supplied 64 development emails: six standard and two
challenge cases per label. Eight other original emails, one per class, supplied the
few-shot examples. These sets are disjoint, selected with fixed seed 20261102. The
80 additional emails were frozen before the sweep; their hash is in `results/manifest.json`.

| 600M candidate | Development correct |
|---|---:|
| Original English instructions | 38/64, 59.38% |
| Short English definitions | 43/64, 67.19% |
| Short English definitions with English option names | 36/64, 56.25% |
| Short Italian definitions | 20/64, 31.25% |
| Eight examples, Italian option names | 21/64, 32.81% |
| Eight examples, English option names | 18/64, 28.12% |
| Eight independent native yes/no decisions, highest score wins | 33/64, 51.56% |

Short English definitions were selected on development data alone. On the untouched
80-email set they scored 51/80 (63.75%), below the original question's 54/80 (67.5%).
The development gain did not generalize. The yes/no diagnostic failed development
and was not promoted to the holdout. Its scores are independent probabilities, not
a normalized distribution over the eight mutually exclusive labels.

After this negative prompt result, d1-3B was chosen as a stronger model alternative
using its published model card, with the **original**, unchanged English question.
Forty more cases were authored, labels assigned and hashed before the first 3B
inference. The 3B model was run on those 40, then the earlier 80, then the original
200. No further tuning or prompt selection used these outcomes.

The original 200 and earlier 80 are exploratory comparisons for the later model
choice. The final 40 are the strongest additional in-session confirmation. They
still share an author and style with the other cases; none is an independently
annotated natural inbox or a blind external evaluation.

## Accuracy, review coverage and latency

On the original 200, macro F1 is **0.9558**, with 152/160 standard and 39/40 challenge
cases correct. Per-label correct counts are Cliente 22/25, Lavoro 25/25, Personale
23/25, Opportunità 24/25, Amministrazione 24/25, Spedizioni 24/25, Pubblicità 25/25,
Notifiche 24/25. Both email-embedded instruction probes in this corpus were classified
correctly; two probes do not establish injection resistance.

These fixed thresholds were already part of the first benchmark's summaries:

| Maximum class probability | Accepted / 200 | Correct among accepted | Accuracy on accepted | Sent to review |
|---|---:|---:|---:|---:|
| >=0.8 | 179 | 173 | 96.65% | 21 |
| >=0.9 | 148 | 146 | 98.65% | 52 |
| >=0.95 | 105 | 105 | 100% | 95 |

The threshold is on the largest option probability, not the server's separate
`confidence` field. Raising the threshold improves this observed accepted-subset
score while reducing automatic coverage. It does not correct the remaining errors
or establish calibration for real mail. The 95% Wilson interval for full 191/200
accuracy is approximately 91.67–97.61%; synthetic selection bias is outside that interval.

3B observed median HTTP latency on the original 200 was **202.40 ms**, p95 **208.55 ms**.
The earlier 600M English run's median was 38.86 ms. The new 40 and 80 runs had 3B
medians of 414.82 and 168.15 ms respectively, so latency varies with runtime state.
These are deployment-configuration observations, not a controlled model-size-only
speed comparison: quantization, batch size, thread count and fitting also differ.

## Runtime

- Alternative: `LiquidAI/d1-3B-GGUF`, `d1-3B-Q4_K_M.gguf`, **1,674,456,672 bytes**.
- Model revision: `bb1e436ea78eb96a3f1acb6da865f70c2fbeb563`.
- Verified SHA-256: `16aff27ea2eefdc32b9897f43854a5d3170c1dc8dccb9c756905af30a4e22402`.
- Same llama.cpp CUDA image/build as the first test: `server-cuda-b11960`.
- RTX A2000 Laptop GPU, 4 GiB; independent embedding container remained running.
- Arguments: `-np 1 -c 2048 -b 512 -ub 512 -t 8 --fit on --fit-target 256`.
- GPU-enabled auto-fit was used; the actual layer-offload count was not recorded.
- Separate container `aura-d1-3b-email-benchmark`, loopback port 18088, model files
  mounted read-only. The 600M test container was stopped before loading 3B.
- Text-only: no multimodal projector, attachments, HTML or images were evaluated.

The main 3B evidence is in `results/d1-3b/`; its exact question is in `manifest.json`,
and all raw answers and probabilities are in the prediction JSONL files. The model
file is cached under `output/d1-italian-email-200/` and excluded from git.
`results/verification.json` records independent checks of 968 new scored predictions
across all experiment runs. There are 320 unique emails across the corpora.

## Reproduction

From WSL at the repository root, first start the original 600M test container:

```bash
docker start aura-d1-email-benchmark
# Wait until /health succeeds at http://127.0.0.1:18086/health.
python3 spikes/d1-email-labeling/improvement/sweep.py --phase development --out output/d1-italian-email-200/repeat-improvement
python3 spikes/d1-email-labeling/improvement/sweep.py --phase holdout --out output/d1-italian-email-200/repeat-improvement
python3 spikes/d1-email-labeling/improvement/binary_probe.py --out output/d1-italian-email-200/repeat-improvement
python3 spikes/d1-email-labeling/improvement/confirmation_baseline.py --out output/d1-italian-email-200/repeat-improvement
curl --fail --location --retry 3 -o output/d1-italian-email-200/d1-3B-Q4_K_M.gguf \
  https://huggingface.co/LiquidAI/d1-3B-GGUF/resolve/bb1e436ea78eb96a3f1acb6da865f70c2fbeb563/d1-3B-Q4_K_M.gguf
sha256sum output/d1-italian-email-200/d1-3B-Q4_K_M.gguf
docker stop aura-d1-email-benchmark
docker run -d --name aura-d1-3b-email-benchmark --gpus all -p 127.0.0.1:18088:8080 \
  --mount "type=bind,source=$(pwd)/output/d1-italian-email-200,target=/models,readonly" \
  ghcr.io/ggml-org/llama.cpp@sha256:4ccae195bf542f7844301ae06891baa32a7551cb16de8428af6e929403d4292c \
  -m /models/d1-3B-Q4_K_M.gguf --alias d1-3B-Q4_K_M --host 0.0.0.0 --port 8080 \
  -np 1 -c 2048 -b 512 -ub 512 -t 8 --fit on --fit-target 256
# If the 3B container already exists, use docker start instead of docker run.
# Wait until /health succeeds at http://127.0.0.1:18088/health.
python3 spikes/d1-email-labeling/improvement/model_comparison.py --out output/d1-italian-email-200/repeat-improvement
```

Use a new output directory; the scripts refuse to overwrite result folders. The
model binaries stay outside git. No production model selection, mail labels, provider
mailboxes or ArcadeDB data were changed.

## Decision

The best tested option is **d1-3B Q4_K_M with the original English prompt**, with
review for low-probability predictions if a higher accepted-subset precision is needed.
This is a benchmark result, not production acceptance. Validate the frozen candidate
on a person's independently labeled real mail before making an unattended-labeling claim.

Sources: [3B model card](https://huggingface.co/LiquidAI/d1-3B),
[publisher's 3B GGUF instructions](https://huggingface.co/LiquidAI/d1-3B-GGUF),
[600M model card](https://huggingface.co/LiquidAI/d1-omni-600M), and the
[original benchmark](../README.md). Published model-card scores motivated the candidate;
the email accuracies above were measured here, not copied from those scores.
