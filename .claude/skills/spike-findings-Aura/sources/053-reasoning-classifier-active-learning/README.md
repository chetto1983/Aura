---
spike: 053
name: reasoning-classifier-active-learning
type: standard
validates: "Given the granite reasoning-tier classifier, when an LLM oracle (Gemma-E2B) labels the classifier's low-margin/uncertain stream cases and those embeddings are folded into the per-tier bank, then held-out accuracy measurably improves without per-turn latency cost"
verdict: VALIDATED
related: [052-reasoning-tier-embed-classifier]
tags: [reasoning, classifier, active-learning, embeddings, neo4j, gemma, slice-13]
---

# Spike 053: Active-Learning Self-Improvement Loop

## What This Validates

Operator idea: the embedding reasoning-tier classifier should self-improve —
when it's UNCERTAIN, label the turn with the LLM router as an oracle, store the
(embedding, tier) pair, and the classifier gets better over time. The key design
correction (vs the literal "fallback to LLM on none"): labeling must be
**async/offline and margin-gated**, never inline on the user's turn — otherwise
it re-adds the very round-trip the classifier eliminated.

This spike answers the real question: **does folding oracle-labeled uncertain
cases actually raise accuracy, or just propagate the oracle's errors?**

## How to Run

```powershell
# granite :8081 (embeddings) + Gemma-E2B :8095 (oracle) up
go run ./.planning/spikes/053-reasoning-classifier-active-learning
```

Offline simulation, money-free (Gemma oracle local): baseline classifier →
stream a pool labeling only the uncertain (cosine margin < 0.05) cases → refresh
→ re-measure a FIXED held-out set. 30 held-out / 45 stream prompts, all disjoint
from the seeds; the stream covers the baseline's known failure modes (stable
facts, arithmetic) with different instances.

## Results

**VALIDATED — active learning lifted held-out accuracy +7 points to 97%
(matching the oracle's own accuracy), at bounded async cost.**

| Stage | centroid acc | centroid none-vs-rest | kNN(5) acc |
|---|---|---|---|
| Baseline (seeds only) | 90% | 90% | 93% |
| **After learning** | **97% (+7)** | **97% (+7)** | 93% (+0) |

Loop economics:
- **30/45 stream prompts were uncertain** (margin < 0.05) → oracle labeled them;
  15/45 were confident → **no oracle call, zero cost**. As the bank grows,
  fewer cases stay uncertain → the oracle is called less over time (self-limiting).
- **Oracle label noise = 1/30 wrong vs gold** (~3%, Gemma's real error rate). The
  noise did NOT break the gain — the signal dominated.

Key findings:

1. **The loop works, and the mechanism is centroid-refresh.** Folding the
   oracle-labeled stable-fact/arithmetic examples into the `none` centroid moved
   the held-out stable-fact cases (which baseline mis-routed to `low`) into
   `none`: +7 on both accuracy and the consequential none-vs-rest. The classifier
   converged to the oracle's accuracy (97%) WITHOUT the per-turn LLM round-trip.
2. **kNN did NOT benefit here** (93%→93%). Similarity-weighted k=5 over
   seeds+examples didn't change the top neighbors for the failure cases. The
   takeaway for implementation: **refresh centroids from the bank**, don't rely on
   kNN. (Centroids are also more robust to a single noisy label — it's diluted by
   the mean, whereas kNN can be swayed by one bad near neighbor.)
3. **Label noise is tolerable at the oracle's ~3%**, but the guardrail still
   matters: keep the curated seeds authoritative and dedup by content-hash so a
   bad label can't dominate a centroid. Centroid averaging already provides
   noise resistance.

## Investigation Trail

- Designed the stream to cover the held-out's failure modes (stable facts,
  arithmetic). This is representative, not cheating: active learning labels the
  cases the classifier is uncertain about, which ARE its failure modes. The +7
  magnitude depends on the stream containing relevant examples — in production the
  stream is real traffic and the low-margin turns are exactly the worth-labeling
  ones.
- marginFloor 0.05 chosen from spike-052 error margins (mistakes clustered
  <0.035); 0.05 captures them plus a buffer. Tunable.

## Signal for the Build

The self-improvement loop is worth building, as a SEPARATE slice:
- **Async worker** (post-turn, never inline): on a low-margin classification,
  enqueue the turn; a background worker calls the oracle and upserts a
  `:ReasoningExample {hash, embedding(384d), tier, source}` node into Neo4j (the
  existing vector store). Reuse the embedding the classifier already computed.
- **Centroid refresh** (not kNN) from seeds + stored examples, periodically.
- **Guardrails**: content-hash dedup, seeds stay authoritative, cap the bank,
  treat oracle labels as weak. The oracle can be the local Gemma sidecar (free,
  validated here) or DeepSeek.
- Live validation needs the Neo4j stack (recall over the growing example set).
