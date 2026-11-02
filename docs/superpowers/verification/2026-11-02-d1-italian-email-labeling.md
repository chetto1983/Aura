# d1-omni-600M Q8_0: Italian email labeling, 2026-11-02

**Measured locally in a real llama.cpp CUDA container:** 200 synthetic Italian emails,
eight labels with 25 examples each. No product code or configuration changed.

| Instruction language | Correct | Accuracy | Macro F1 | Latency p50 / p95 |
|---|---:|---:|---:|---:|
| Italian | 99/200 | 49.5% | 0.5036 | 38.27 / 43.24 ms |
| English, with the same Italian emails and label names | 130/200 | 65.0% | 0.6527 | 38.86 / 47.97 ms |
| Constant-label baseline | 25/200 | 12.5% | — | — |

The English instruction variant is a diagnostic follow-up on the same test set.
It improves accuracy by 15.5 percentage points; it is not a new held-out evaluation.
Both runs completed with zero API errors, valid probability distributions, no output
tokens and one excluded warmup. Standard/challenge counts were 83/160 and 16/40 for
Italian instructions, 109/160 and 21/40 for English instructions.

| Gold label | Correct with Italian instructions | Correct with English instructions |
|---|---:|---:|
| Cliente | 10/25 | 15/25 |
| Lavoro | 12/25 | 18/25 |
| Personale | 9/25 | 17/25 |
| Opportunità | 11/25 | 19/25 |
| Amministrazione | 20/25 | 18/25 |
| Spedizioni | 11/25 | 16/25 |
| Pubblicità | 13/25 | 10/25 |
| Notifiche | 13/25 | 17/25 |

At option probability >=0.9, Italian accepts 7/200 with 7 correct; English accepts
42/200 with 38 correct. Low coverage and the remaining English errors do not support
unattended labeling. Each variant also fails one of two instruction-in-email probes.

An eight-email CPU spot check matched all eight CUDA choices, including two errors;
the maximum absolute option-probability difference was 0.01802. This does not establish
CPU equivalence on all 200 emails.

Runtime: pinned `server-cuda-b11960` (binary `8642541b2`), RTX A2000 Laptop GPU 4 GiB,
`-ngl 99 -np 1 -c 4096 -b 4096 -ub 4096 -t 4`, native `/v1/systemone` endpoint.
Model repository revision `8eb2d2a1c5404bba8a34a88bd8e159c242197d51`; the 407,207,584-byte
Q8 file matched SHA-256 `cd94463f4cac9c700ec6750df8df005bb6846de410e159d1f5e985e1171693d3`.

The host's independent embedding container remained running. Latencies are sequential
local HTTP timings after warmup, not model-only timings or mini-PC measurements.

**Evidence:** [dataset, exact prompts, scripts and reproduction](../../../spikes/d1-email-labeling/README.md),
[Italian run](../../../spikes/d1-email-labeling/results/italian-instructions/summary.json),
[English instruction diagnostic](../../../spikes/d1-email-labeling/results/english-instructions/summary.json).
Every prediction and raw probability vector is preserved beside its summary.

**ArcadeDB reading:** the requested
[recommendation-engine page](https://arcadedb.com/recommendation-engine.html) combines
graph relationships, vector similarity and temporal signals. A possible application is
retrieving a person's previously confirmed labels and contextual sender/thread history.
That is an inference, not a measured improvement: this run did not test ArcadeDB or a
hybrid classifier. Its [vector tutorial](https://docs.arcadedb.com/arcadedb/tutorials/vector-search-tutorial)
uses `vector.neighbors(...)`, unlike the illustrative landing-page query syntax.

**Limits:** author-created balanced examples and author-assigned labels; no independent
annotation, real inbox, attachments, HTML, training, personal label history, quantization
comparison, complete CPU run or deployed Aura integration. The extra four benchmark labels
are assumptions for this experiment. This evidence records a failed candidate for automatic
labeling under these prompts, not production acceptance or a failure of all possible uses.
