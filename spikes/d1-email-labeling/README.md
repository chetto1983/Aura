# d1-omni-600M: labeling 200 synthetic Italian emails

Measured on 2026-11-02. The emails are all Italian in both runs. The first run also
uses Italian instructions; the second translates only the instructions and option
descriptions into English. It is a diagnostic run on the same data, performed after
seeing the first result, and is not an independent validation set.

| Run | Correct | Accuracy | Macro F1 | Median HTTP latency | p95 HTTP latency |
|---|---:|---:|---:|---:|---:|
| Italian instructions | 99/200 | 49.5% | 0.5036 | 38.27 ms | 43.24 ms |
| English instructions, Italian emails and label names | 130/200 | 65.0% | 0.6527 | 38.86 ms | 47.97 ms |
| Always choose one label | 25/200 | 12.5% | — | — | — |

Both runs completed with zero API errors and zero output tokens. The main test is
zero-shot; no email examples or gold labels are sent to the model. There is one
excluded warmup per run. Requests run sequentially in a fixed shuffled order, seed
20261102. Latencies include HTTP, serialization and local server execution, exclude
startup/download/warmup, and measure this host with another embedding service running.
p95 uses the nearest-rank definition.

## Dataset and labels

`emails.tsv` contains exactly 200 individually authored synthetic subject/body pairs,
25 for each of Cliente, Lavoro, Personale, Opportunità, Amministrazione, Spedizioni,
Pubblicità and Notifiche. The first four names follow the mail-watch design; the
additional four are benchmark choices, not a change to the product taxonomy.

The benchmark author assigned the gold label before any predictions. There are 160
standard cases and 40 designated challenges: mixed topics, misleading subjects,
negation, promotional urgency, automated invoices/deliveries and two instructions
embedded in email content. The `difficulty` and gold fields are excluded from the
request. These are author-designated challenges, not an externally validated corpus.
Bodies contain at least 20 words; each email has one primary label. The fixed criteria
give accounting and parcel-transport intent precedence over sender or automation.

No real inbox data, sender history, HTML, attachments, personal corrections, training
or fine-tuning is involved. Results do not estimate accuracy on natural mail or on a
user's own labels. Wilson intervals reported in the summaries describe sampling
uncertainty under independent sampling assumptions; author-created balanced examples
do not establish that assumption or remove selection bias.

## Runtime and provenance

- Model: `LiquidAI/d1-omni-600M-GGUF`, `d1-omni-600M-Q8_0.gguf`, 407,207,584 bytes.
- Repository revision: `8eb2d2a1c5404bba8a34a88bd8e159c242197d51`.
- Verified file SHA-256: `cd94463f4cac9c700ec6750df8df005bb6846de410e159d1f5e985e1171693d3`.
- Image: `ghcr.io/ggml-org/llama.cpp:server-cuda-b11960`.
- Image digest: `sha256:4ccae195bf542f7844301ae06891baa32a7551cb16de8428af6e929403d4292c`.
- Binary: `0.8.0-dev`, build 11960, commit `8642541b2`.
- GPU: NVIDIA RTX A2000 Laptop GPU, 4096 MiB. Driver: WSL Python 3 stdlib.
- Native classification: `POST /v1/systemone`, one `choice` question with eight options.
- Server: `-ngl 99 -np 1 -c 4096 -b 4096 -ub 4096 -t 4`.
- Isolated GPU container: `aura-d1-email-benchmark`, loopback port 18086.
- CPU spot-check container: `aura-d1-email-cpu-check`, loopback port 18087, `-ngl 0`.
- Text only: the multimodal projector was not downloaded or required.

Eight CPU reference predictions (the first example of each class, fixed independently
of the predicted outcome) agreed with CUDA on all choices, including both errors.
The largest absolute option-probability difference was 0.01802. This is a spot check,
not a complete CPU benchmark or a quantization/native-weights equivalence test.

## Reproduce from WSL at the repository root

Download the pinned model; its cached copy is deliberately outside git:

```bash
mkdir -p output/d1-italian-email-200
curl --fail --location --retry 3 --output output/d1-italian-email-200/d1-omni-600M-Q8_0.gguf \
  https://huggingface.co/LiquidAI/d1-omni-600M-GGUF/resolve/8eb2d2a1c5404bba8a34a88bd8e159c242197d51/d1-omni-600M-Q8_0.gguf
sha256sum output/d1-italian-email-200/d1-omni-600M-Q8_0.gguf
docker run -d --name aura-d1-email-benchmark --gpus all -p 127.0.0.1:18086:8080 \
  --mount "type=bind,source=$(pwd)/output/d1-italian-email-200,target=/models,readonly" \
  ghcr.io/ggml-org/llama.cpp@sha256:4ccae195bf542f7844301ae06891baa32a7551cb16de8428af6e929403d4292c \
  -m /models/d1-omni-600M-Q8_0.gguf --alias d1-omni-600M-Q8_0 \
  --host 0.0.0.0 --port 8080 -ngl 99 -np 1 -c 4096 -b 4096 -ub 4096 -t 4
curl --fail http://127.0.0.1:18086/health
python3 spikes/d1-email-labeling/benchmark.py --out output/d1-italian-email-200/repeat-italian
python3 spikes/d1-email-labeling/english_prompt.py --out output/d1-italian-email-200/repeat-english
```

If the named benchmark container already exists, use `docker start
aura-d1-email-benchmark` instead of creating it again. Wait until `/health` succeeds.
The scripts refuse to overwrite existing result directories. Results include the
exact question, gold-free state construction, dataset/script hashes, server metadata,
raw option distributions, all predictions, confusion matrices and misclassifications.
The English run's manifest records the benchmark module hash; `english_prompt.py`
contains its separate literal translation, also recorded in the manifest questions.

## Reading the result

At a maximum-option-probability threshold of 0.9, the Italian run accepts 7/200 emails
and gets those 7 right. The English run accepts 42/200 and gets 38 right (90.48%).
These thresholds were summarized at fixed values, not optimized for this dataset.
Neither establishes a reliable unattended classifier. Option probabilities are not
guaranteed calibrated; they are distinct from the server's entropy-based `confidence`.

Each prompt variant mislabels one of the two email-embedded instruction probes, in
different cases. This small test cannot establish resistance to prompt injection.

**Verdict:** beats the constant-label baseline on this synthetic task, with low GPU
latency, but does not earn automatic email labeling. No model deployment or product
configuration change follows from this experiment.

## Sources and the ArcadeDB reading

- [Publisher's GGUF card](https://huggingface.co/LiquidAI/d1-omni-600M-GGUF)
  specifies `/v1/systemone` and the 4096 batch sizes.
- [Pinned llama.cpp server documentation](https://github.com/ggml-org/llama.cpp/blob/b11960/tools/server/README.md)
  documents the question/answer shapes and probability semantics.
- [ArcadeDB recommendation-engine page](https://arcadedb.com/recommendation-engine.html)
  combines relationship, content-similarity and temporal signals.
- [ArcadeDB vector tutorial](https://docs.arcadedb.com/arcadedb/tutorials/vector-search-tutorial)
  documents `vector.neighbors(...)` with `expand(...)`; this is the documented query
  form to investigate rather than copying the landing page's `ORDER BY vectorNeighbors` snippet.

**Inference for Aura:** a person's confirmed labels/corrections could supply examples
retrieved by similarity, while sender/thread relationships and recency provide additional
context. This is a candidate personalization approach alongside a decision model, not a
classification model supplied by ArcadeDB. This experiment did not query or write ArcadeDB
and does not measure whether that combined approach improves accuracy. The landing page's
performance and uplift claims were not independently validated here.
