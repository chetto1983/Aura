---
spike: 056
name: hybrid-fusion-vs-pure
type: comparison
validates: "Given easy exact-keyword queries BM25 nails plus the semantic-only failures, when pure-embedding vs BM25 vs hybrid fusion (RRF, weighted, guarded) is scored on the same live corpus, then we learn whether pure embedding regresses easy cases and what the production ranking shape should be"
verdict: VALIDATED
related: [054-semantic-toolsearch-vs-bm25, 055-toolsearch-scaling-cliff]
tags: [tool-search, hybrid, rrf, bm25, embeddings, slice-tooling]
---

# Spike 056: Hybrid fusion vs pure embedding

## What This Validates

Spike 054 showed embedding ~2× BM25 overall but losing the lexical-anchor cases BM25 nails.
arXiv 2604.01733 (operator-supplied) found **Hybrid (BM25+Dense via RRF) consistently beats
either constituent** and is the "minimum viable baseline". The obvious next move is therefore
to ship hybrid. This spike tests that on Aura's real 53-tool corpus over a MIXED query set —
7 semantic-only (embedding's strength) + 6 explicit lexical-anchor (BM25's strength) — so any
regression a pure switch causes is visible, and so the paper's hybrid claim is actually checked
rather than assumed.

## How to Run

```bash
export OPENROUTER_API_KEY=dummy-for-config-load
go run ./.planning/spikes/056-hybrid-fusion-vs-pure
```

Rankers compared: production BM25 (`ToolSearch.Execute`), pure embedding (granite, bm25doc
input), naive RRF, weighted RRF (emb×3 : bm25×1), guarded RRF (BM25 contributes only when it
returned ≤5 results AND the tool is in embedding's top-15).

## Results

**VALIDATED — the production shape is EMBEDDING-PRIMARY, not hybrid. BM25 fusion provides zero
uplift on Aura's cross-lingual tool corpus and naive/weighted RRF actively regress. This
contradicts the paper's "hybrid baseline" — and the spike explains why.**

| Ranker | top-1 | sem 7 | lex 6 | recall@3 | MRR@10 |
|---|---|---|---|---|---|
| bm25-production | 4/13 | 0 | 4 | 4 | 0.319 |
| **embedding-pure** | **9/13** | 4 | 5 | 11 | **0.799** |
| hybrid-rrf-naive | 6/13 | 1 | 5 | 8 | 0.601 |
| hybrid-rrf-weighted (3:1) | 6/13 | 1 | 5 | 10 | 0.657 |
| **hybrid-rrf-guarded** | **9/13** | 4 | 5 | 11 | **0.799** |

### Why naive/weighted RRF regress (the surprise)

BM25 scores **0/7 on the semantic queries** — Italian query vs English tool description = no
lexical overlap. But BM25 doesn't return *nothing*; for some semantic queries it returns a few
weak partial matches (a tool sharing one incidental token). RRF then awards those wrong tools a
`1/(k+rank)` boost that drags the correct embedding-#1 down: "che tempo fa" 1→4, "trova un
ristorante" 1→4, "scarica articolo" 1→**off the top-10 entirely**. Weighting embedding 3:1
doesn't fix it (still 6/13) — a single BM25 wrong-tool at rank 1 keeps enough mass to outrank a
borderline embedding hit. **Fusing a retriever that has no signal on your queries adds noise,
not recall.**

### Why the paper's result doesn't transfer

arXiv 2604.01733's hybrid wins because *both* retrievers are individually strong on its corpus
(English financial docs: BM25 recall@5 0.644, dense 0.587 — they fuse two competent signals).
Aura's setting is asymmetric: BM25 is near-useless on Italian-natural-language tool queries
(sem 0/7), so there is no second signal to fuse. The paper's own caveat ("may not generalize to
other domains") is exactly this case.

### Guarded fusion is the safe shape — it degenerates to pure embedding here

Guarded RRF (BM25 only contributes when it's a confident lexical hit — small result set AND in
embedding's top-15) **exactly equals pure embedding** (9/13, MRR 0.799). The guard makes BM25 a
no-op precisely when it's noisy, and when BM25 *is* confident it already agrees with embedding
(both rank the lexical golds #1). So on this corpus guarded-hybrid ≡ pure embedding, but it is
strictly *safer* across query styles: if a user types an English/keyword query where BM25 has
real signal, the guard lets it break ties; for the dominant Italian-natural-language traffic it
falls back to pure embedding.

### Fusion does NOT fix the residual misses

Embedding's 4 misses (verbose bitcoin, send_email rank-4, document_search rank-2 inside dense
mail cluster) are unrecovered by ANY fusion variant — BM25 misses them too. Confirms spike 055:
the real remaining lever is **namespace-aware retrieval / reranking**, not BM25 fusion.

## Investigation Trail

1. Built naive RRF first (the paper's recommendation). It scored 6/13 vs pure embedding's 9/13
   — a regression. Rather than report "hybrid lost", probed *why* and tried two more fusion
   shapes (weighted, guarded) to find whether ANY fusion beats pure embedding.
2. Weighted (emb 3:1) still regressed → the problem is BM25's wrong tools having any rank at
   all, not the weight. Guarded (intersection-gated) matched pure embedding exactly → confirmed
   BM25 has no *additional* signal to contribute on this corpus; it can only break even or hurt.

## Signal for the Build

- **Ship pure embedding (bm25doc-enriched input) as the free-text `tool_search` ranker.** Do
  NOT ship naive/weighted RRF — it regresses the dominant Italian-query traffic.
- **If you want robustness across query styles, use guarded fusion** (embedding-primary; BM25
  contributes only as a confident, intersection-gated tiebreak). It is ≡ pure embedding on the
  IT corpus and degrades gracefully when a query is genuinely lexical. This is the safest
  default and costs almost nothing (BM25 already runs).
- **Keep BM25/substring for the exact-name `select:` path** — that is a different mechanism
  (load tool by name), not free-text retrieval, and stays.
- **The paper's "always hybrid" advice is corpus-dependent** — verify retriever competence
  before fusing. For Aura the dense signal is the load-bearing one.
- **The remaining quality ceiling is intra-namespace disambiguation** → namespace-aware
  retrieval / rerank (spike 055), not lexical fusion.
