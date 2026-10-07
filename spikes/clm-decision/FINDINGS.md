# CLM decision spike — findings (stage 1, 2026-10-07)

**Verdict: zero-shot CLM-8B does not beat what Aura ships, on either decision. By the
pre-registered rule in README.md, stage 2 (a head for a smaller encoder) is not worth proposing.**

## Setup validity

Run on Modal, on one L4: vLLM 0.31.0 with `Qwen/Qwen3-8B --runner pooling` (resolved to
`pooling_type=LAST`, normalized), `contrastive-lm` 0.1.0, head `CLM-v0.1-8B` (HF sha `e939398d`).

- **The model card's rank example reproduces exactly.** "What causes tides on Earth?" puts the Moon
  first at 0.99334; the card states 0.993.
- **Tokenization matches the training recipe.** For the same texts, raw-string embeddings and token-id
  embeddings built with CLM's own recipe (`train/embed_utils.py`, `add_special_tokens=False`) have
  cosine 1.0.
- **The repository README's example values do not reproduce, and are not a reference.** Urgency comes
  out at 0.84 against 0.41, billing at 0.989 against 0.939, and the tides example at 0.997 in the
  README versus 0.993 on the card. The README values were captured against an earlier `clm-latest`
  head; the card's values match this checkpoint. The choices themselves agree: billing, "Very angry".

## Effort (58-case gate, `liveCorpus`)

| Variant | Accuracy | none-vs-rest | VM traffic (13) | With greeting fast path |
|---|---|---|---|---|
| Seeds, shipped (re-measured 2026-10-07) | **54/58** | 54/58 | — | — |
| E1: Italian tier definitions | 18/58 | 31/58 | 0/13 | 19/58 |
| E2: router prompt descriptions (English) | 18/58 | 31/58 | 0/13 | 19/58 |
| E3: definitions and seeds, best 3 per tier | 18/58 | 31/58 | 0/13 | 19/58 |
| E1-raw (no head) | 18/58 | 31/58 | 0/13 | 19/58 |
| E2-raw (no head) | 23/58 | 39/58 | 8/13 | 24/58 |

Every head variant predicts **`high` for all 58 prompts**. E2-raw predicts `low` for all 58. The
model does not separate reasoning tiers at all: an effort tier is a judgement about the request,
not an action, which is what CLM's state-action training covers.

## Tools (85 deferred tools; top-1 / recall@5)

| Variant | Gate (26) | Held-out (24) | Blind EN (30) | Blind IT (30) |
|---|---|---|---|---|
| BM25, shipped (2026-10-06) | **26 / 26** | 12 / 19 | 18 / 23 | **19 / 19** |
| BM25 + dense RRF (2026-10-06) | **26 / 26** | **17 / 22** | **21 / 30** | 21 / 28 |
| T-A: `searchDocument` | 11 / 19 | 5 / 9 | 15 / 18 | 10 / 17 |
| T-B: `name: summary` | 17 / 20 | 4 / 11 | 14 / 19 | 3 / 10 |
| T-A-raw (no head) | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 2 |

CLM ranks below BM25 on every set, and far below on Italian (3-10 of 30 top-1 against 19). The
model card lists English only.

## Latency

On the L4, a fresh state costs about 75-80 ms through the 8B encoder. Cached actions add under
1 ms per question. This says nothing about the appliance's CPU, where an 8B encoder is not viable
per turn; stage 2 would have had to measure a small encoder there.

## What this does not show

- **Zero-shot only.** Fine-tuning heads on Aura's own decisions was excluded by the operator's choice;
  the card itself says its verifier results need fine-tuned heads.
- **One checkpoint.** The card announces a CLM-35B for October; this measures v0.1-8B only.
- **Regression sets, already inspected.** A win here would only have justified a frozen independent set;
  a loss this wide does not depend on that.
- **The question phrasings are the router's own and were fixed before the run.** Other phrasings could
  score differently, but trying them against these sets would tune on the test.

## Cost and cleanup

Three Modal runs on one L4, including the first 16 GB model download, cached in the Modal volume
`aura-clm-cache`. The Modal token and the Hugging Face token (Modal secret `aura-clm-hf`) were provided
for this spike and are to be revoked by the operator.

Reproduce: `modal run spikes/clm-decision/modal_clm.py` (stage 1, writes `results/stage1.json`) and
`modal run spikes/clm-decision/modal_clm.py::setup_check`.
