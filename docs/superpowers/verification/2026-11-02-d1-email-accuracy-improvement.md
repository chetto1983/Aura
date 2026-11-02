# d1 email-labeling accuracy improvement — 2026-11-02

**Measured improvement:** replacing d1-omni-600M Q8_0 with d1-3B Q4_K_M raised the
original synthetic Italian email score from **130/200 (65%) to 191/200 (95.5%)**.
The English prompt, Italian label names and Italian emails were unchanged.

| Corpus | 600M Q8_0 | 3B Q4_K_M |
|---|---:|---:|
| Original 200 | 130/200, 65% | 191/200, 95.5% |
| Additional 80 | 54/80, 67.5% | 80/80, 100% |
| Further 40 | 29/40, 72.5% | 40/40, 100% |

The 600M prompt sweep did not earn a change. Shorter English definitions won on
64 exposed development cases, 43/64 versus 38/64, but lost on the untouched 80 cases,
51/80 versus 54/80. Eight labeled examples and eight native yes/no decisions also
failed to improve development accuracy. No held-out gain is claimed for prompt tuning.

The original eight-label taxonomy is unchanged. The 3B alternative was selected from
its model card after the negative prompt result, with the original English question.
Forty additional emails were authored and hashed before 3B inference; none of these
outcomes was used for subsequent tuning. The original 200 and earlier 80 are exploratory
comparisons for that model choice; the further 40 are additional in-session confirmation.

On the original 200, 3B macro F1 is 0.9558. At a maximum option probability >=0.9,
148/200 are accepted with 146 correct (**98.65% accepted-subset accuracy, 74% coverage**);
52 would go to review. At >=0.95, 105/200 are accepted and all 105 are correct. These
fixed thresholds were present in the first benchmark; they were not fitted to 3B errors.
They do not establish real-mail calibration or guaranteed precision.

3B median local HTTP latency on the original 200 is **202.40 ms**, p95 **208.55 ms**,
versus the earlier 600M English run's 38.86 ms median. Thread count, batch sizing,
quantization and fitting differ, so this is an observed configuration tradeoff.

Runtime: same pinned llama.cpp `server-cuda-b11960`, RTX A2000 Laptop GPU 4 GiB,
native `/v1/systemone`; 3B arguments `-np 1 -c 2048 -b 512 -ub 512 -t 8 --fit on
--fit-target 256`. GPU-enabled auto-fit, exact layer offload unrecorded. Independent
embedding service stayed running; benchmark containers were separate and are retained
stopped after evidence collection.

3B GGUF revision `bb1e436ea78eb96a3f1acb6da865f70c2fbeb563`, file 1,674,456,672 bytes,
verified SHA-256 `16aff27ea2eefdc32b9897f43854a5d3170c1dc8dccb9c756905af30a4e22402`.

[Reproduction, datasets and complete analysis](../../../spikes/d1-email-labeling/improvement/README.md).
[Original-200 predictions](../../../spikes/d1-email-labeling/improvement/results/d1-3b/original200.predictions.jsonl),
[summary](../../../spikes/d1-email-labeling/improvement/results/d1-3b/original200.summary.json),
and [verification ledger](../../../spikes/d1-email-labeling/improvement/results/verification.json).
The ledger independently checked all 968 new scored predictions across the experiments:
gold references, counts, probabilities, zero generated tokens and frozen-input hashes.

**Limits:** 320 unique synthetic emails, common author and author-assigned labels,
small final confirmation set, no independent annotation or natural inbox, no training,
no production deployment, no ArcadeDB-based personalization, no quantization-isolated
comparison. The 120/120 newer-email score is not a promise of perfect real-world accuracy.
