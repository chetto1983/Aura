# d1 on public Italian sets, and a centroid beside it — 2026-11-02

**Verdict: PARTIAL.** On the two usable public Italian sets, `liquid/d1.1` labels zero-shot at the
level a class centroid over EmbeddingGemma reaches only with 10 to 20 labelled examples per class, and
fusing the two adds nothing outside the noise. Asking for the most specific label and deriving its
parent beats asking for the parent: 177 against 161 of 180. PARTIAL because both sets are synthetic,
neither is mail, and one run each.

## The search

No public corpus of real Italian mail with categories was found (Hugging Face Hub search by name and
by `language:it`, two web searches). Three small Italian sets were inspected before any measurement:

| Set | Rows | Licence | Kept |
|---|---:|---|---|
| `tanaos/synthetic-spam-detection-dataset-italian`, rev `492e11c6` | 18,719 | MIT | yes, filtered: 13,648 rows left |
| `Noise144/ticket_classification_IT_EN`, rev `bcde65b6` | 1,800 (892 Italian) | none stated | yes; rows not committed, indices only |
| `MARKOPOSI/italian-intent-classification-300`, rev `806721ab` | 300 | CC-BY-4.0 | **no**: all 300 rows name their own label ("Sintetica frase esempio numero 0 riguardo l'intento 'product_inquiry'") |

The spam filter drops rows under 8 words, rows with a six-letter run of one character ("SSSS…"), and rows
announcing their class before a colon ("Malware o link dannosi: …").

## d1 alone

Sample fixed by seed 20261102: 100 spam and 100 not; 10 Italian tickets per each of 18 subcategories.

| | d1.1, OpenRouter |
|---|---:|
| spam, accuracy at 0.5 | 192/200 |
| spam, AUROC / Brier / ECE | 0.996 / 0.027 / 0.021 |
| tickets, category asked directly (5 classes) | 161/180 |
| **tickets, category derived from the subcategory** | **177/180** |
| tickets, subcategory (18 classes) | 172/180 |
| tickets, subcategory at confidence ≥ 0.9 | 129 decided, 129 right |
| latency median / max | 0.26 s / 0.91 s |

- **The parent label is the hard question.** 12 of the 19 category errors put an `Inquiry/Help` request
  (antivirus, internal application) under `Software`: the dataset's taxonomy, not the text, decides it.
  The subcategory carries it unambiguously.
- **Two runs of the same sample differed by one item** in each ticket field: the hosted route is not
  perfectly deterministic.
- The `confidence` field's ECE is 0.05–0.09. As rizzo-flow's README says, it describes the shape of the
  distribution, not accuracy; a threshold belongs on calibrated probabilities.

## A centroid, and its fusion with d1

`centroid.py`: k labelled examples per class from the rows not sampled, EmbeddingGemma vectors with
Aura's stored-document prefix, class mean, cosine (what ArcadeDB's `vector.avg` and
`vector.cosinesimilarity` compute), five draws per k. Fusion is a log-linear mix whose weight moves to
the centroid as k grows, `w = k / (k + 5)`, with τ = 0.05, both set before looking at results.

| k per class | 1 | 3 | 5 | 10 | 20 |
|---|---:|---:|---:|---:|---:|
| tickets, centroid | 74.1% | 90.2% | 91.0% | 94.2% | 94.1% |
| tickets, fused (d1 alone 95.6%) | 95.6% | 95.9% | 95.9% | 95.6% | 95.9% |
| spam, centroid | 71.6% | 78.8% | 87.1% | 92.0% | 96.2% |
| spam, fused (d1 alone 96.0%) | 96.4% | 96.6% | 97.5% | 97.0% | 96.8% |

The fusion's best gain is 1.5 points on spam, 3 items of 200: noise at this size. By BASELINE OR DROP it
has not earned a place on topical labels. Read with the mail-classification spike of the same day, where
the centroid won on per-person urgency and a decision model scored 46.1 alone: **the centroid learns what
is personal, d1 knows what is general.**

## What this does not show

- **Mail.** Neither set is mail; the tickets are one-line help-desk requests and the spam rows are short.
- **Real data.** Both sets are synthetic, and the spam set is noisy even after the filter.
- **Calibrated fusion.** τ and the weight schedule are hand-set; a fusion over temperature-scaled d1
  probabilities (rizzo-flow's `rizzo calibrate`) might do better or worse.
- **Corrections as they arrive.** Examples here are drawn at random from the pool; an operator's
  corrections are the model's mistakes, which skew the centroid (see the mail-classification spike).
- **d1-2B locally** on these sets: not run.
