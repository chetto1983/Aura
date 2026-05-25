# Hybrid Ranking with Exact-Match Boost — Production Patterns Survey (2026-05-25)

Online research for **Wave G2-S3** (IDF exact-match boost in RRF). Goal: validate or
challenge the graphify three-tier scorer (EXACT=1000 / PREFIX=100 / SUBSTRING=1) ×
IDF-per-term, in the context of Aura's 45→500-doc wiki with RRF over
{exact, FTS5/BM25, Qdrant vector cosine on embeddinggemma-300m@256d}.

Time-boxed: ~45 min wall-clock. 13 high-quality findings + anti-patterns + open
questions for Davide. Scale annotation: **[OK@45]** means works at our current
size; **[OK@5k]** means production-scale only; **[OVERKILL]** means academic or
big-corpus pattern with no payoff at our size.

---

## TOP-3 IMPACT SUMMARY (for the lazy reader)

1. **Algolia-style tiebreaker sort beats RRF fusion for the literal-identifier
   case** — successive sort by criterion (exactness → proximity → text-match
   score) gives a hard, audit-able position-1 guarantee that RRF can NEVER
   provide because RRF averages signals by design. For Aura specifically: a
   pre-RRF "if EXACT slug match, pin to rank 0" gate is more reliable than any
   1000× weight inside the fuser. **Adjust G2-S3: keep tier scoring inside the
   exact channel, but ALSO add a hard pre-fusion pin when tier=EXACT.**

2. **The 1000/100/1 ratio is arbitrary; what matters is "EXACT must dominate
   the sum of all substring contributions across all other terms in any
   plausible query."** Algolia/Typesense/Meilisearch use successive sort (no
   ratio at all). Elasticsearch field-boost examples cluster around 5–10×
   (title^10 body^1), NOT 1000×. Graphify's 1000× is over-engineered but
   harmless because IDF crushes common terms. For Aura: **the safer
   reformulation is `EXACT_score = SUM_substring_max_possible + 1`** —
   guarantees dominance without an arbitrary magic number.

3. **At 45–500 docs, IDF is near-degenerate and BM25 plateaus** — most "best
   practices" from 5k+ corpus literature don't apply. The pattern that DOES
   pay off at our scale is **multi-field weighted matching (title^3
   slug^5 body^1) with per-field exact boost**, not a fancy fuser. Aura's
   `exactMatchDB` already only checks title+slug; this is correct. The wider
   "field boost tuning" body of work suggests we should expose slug-match
   separately from title-match and weight slug higher (slugs are
   user-typed identifiers, titles are LLM-generated prose).

**Net for G2-S3:** ship the graphify port (tier + IDF) as planned, BUT add
two cheap modifications: (a) hard pre-fusion pin when tier=EXACT on slug,
(b) replace the 1000/100/1 ratio with a derived "must exceed substring sum"
constant. Both changes are <20 LOC.

---

## Findings (13)

### F1 — Algolia 8-criterion successive sort (NOT score fusion)

- **Source:** [Algolia ranking criteria](https://www.algolia.com/doc/guides/managing-results/relevance-overview/in-depth/ranking-criteria)
- **Year:** Updated 2025
- **Pattern shape:** Algolia computes 8 scores per record (Typo → Geo → Words →
  Filters → Proximity → Attribute → **Exact** → Custom) and applies *successive
  sort*: documents are first sorted by criterion 1; ties broken by criterion 2;
  ties broken by criterion 3; etc. There is no single fused score. Exactness is
  criterion #7 (very late tiebreaker) because words/proximity/attribute have
  already filtered down to relevant candidates.
- **Aura applicability:** SEPARATE-STORY (the full 8-criterion stack is
  overkill), but **ABSORB the principle**: when a hard guarantee is required
  ("user typed a literal slug → that slug at position 1"), successive sort or a
  pre-fusion pin is more reliable than weight tuning. RRF mathematically cannot
  guarantee position 1 for a signal — only successive sort or a hard pin can.
- **Quantitative:** Algolia's exactness "boost" has no numerical value; it's a
  binary tiebreaker. Compare to graphify's 1000× weight: the 1000 is doing the
  job of "make this win", but successive sort does the job exactly without a
  magic number.

### F2 — Typesense `prioritize_exact_match` (binary flag, undocumented magnitude)

- **Source:** [Typesense Ranking and Relevance](https://typesense.org/docs/guide/ranking-and-relevance.html)
- **Year:** 2025
- **Pattern shape:** Typesense's `_text_match` score considers frequency, edit
  distance, proximity, and field ordering in `query_by`. When
  `prioritize_exact_match=true` (default), a document matching a field value
  verbatim gets "highest relevancy" — but the *magnitude* is undocumented,
  implying it's effectively a tiebreaker pin, not a numerical boost.
- **Aura applicability:** ABSORB — confirms that production search engines treat
  exact-match as a binary precedence rule, not a calibrated weight. This is
  evidence the 1000× weight in graphify is an arbitrary stand-in for what
  *should* be a hard pin.
- **Quantitative:** Typesense uses field-ordering as the primary field weight
  signal (first field in `query_by` weighted higher than second). For Aura: if
  we ever expose multi-field matching, list slug before title before body.

### F3 — Meilisearch ranking rules (successive sort, default order includes "exactness")

- **Source:** [Meilisearch typo tolerance settings](https://www.meilisearch.com/docs/learn/relevancy/typo_tolerance_settings),
  [Meilisearch exactness criterion spec](https://specs.meilisearch.com/specifications/text/0036-exactness-criterion.html)
- **Year:** 2024–2025
- **Pattern shape:** Default ranking rules: words → typo → proximity →
  attribute → **sort** → **exactness**. Each is a sort-stage. Exactness ranks
  documents where query terms match field values without prefix-matching ahead
  of those that only prefix-match.
- **Aura applicability:** ABSORB — second production search engine after Algolia
  using successive sort over score fusion for exact-match guarantees. Strong
  signal that the industry convention diverges from RRF for this requirement.
- **Quantitative:** No numerical boost values — pure sort-stage.

### F4 — Elasticsearch `function_score` multiplicative boost (modest values)

- **Source:** [BM25 ranking with multiplicative boosts in Elasticsearch](https://www.elastic.co/search-labs/blog/bm25-ranking-multiplicative-boosting-elasticsearch)
- **Year:** 2025
- **Pattern shape:** Elasticsearch's official 2025 best-practice is
  `function_score` with `boost_mode=multiply` over BM25. Examples in the
  official labs blog use modest multipliers: Adidas 1.5×, Nike 1.25×, Reebok
  1.0×. Even brand-affinity uplifts max out at ~2×, not 100× or 1000×.
- **Aura applicability:** ABSORB — production ratios for "soft" boosts (brand
  affinity, recency, popularity) live in 1.1–2× territory. Graphify's 1000×
  reflects a *hard* "must win" boost, not a soft one. Two different concerns.
- **Quantitative:** Elastic's typical title-vs-body boost is **title^10 body^1**
  (10:1 ratio, NOT 1000:1) — this is field boost, not exact-match boost.

### F5 — OpenSearch RRF + score-norm hybrid

- **Source:** [OpenSearch hybrid search](https://opensearch.org/blog/building-effective-hybrid-search-in-opensearch-techniques-and-best-practices/),
  [OpenSearch hybrid optimization](https://docs.opensearch.org/latest/search-plugins/search-relevance/optimize-hybrid-search/)
- **Year:** 2024–2025
- **Pattern shape:** OpenSearch supports both RRF and weighted score combinations
  (arithmetic / harmonic / geometric mean) after min-max or l2 normalization.
  Common example: 0.3 lexical + 0.7 semantic (arithmetic mean over min-max
  normalized scores). Their official guidance: "RRF requires no pretraining,
  weight tuning, or knowledge of score ranges" — i.e. RRF is the default
  recommended fuser.
- **Aura applicability:** ABSORB-PARTIAL — confirms Aura's RRF + per-channel
  normalization (commit 13d004d3) is on-pattern. But OpenSearch leaves the
  "exact-match position-1 guarantee" problem unsolved by RRF — same gap we have.
- **Quantitative:** OpenSearch weights 0.3/0.7 (lex/sem) vs Aura's normalized
  RRF weights 0.6/0.8 (FTS/vector) + 1.0 (exact). Aura's exact is correctly
  the highest, but a 1.0 RRF weight at rank-1 yields only 0.0167 (since
  1/(60+1)=0.0164) — orders of magnitude below what the LLM perceives as
  "clearly winning". This is the core G2-S3 motivation.

### F6 — Vespa phased ranking (cheap-first, precise-second)

- **Source:** [Vespa Phased Ranking](https://docs.vespa.ai/en/ranking/phased-ranking.html),
  [Vespa RAG blueprint](https://docs.vespa.ai/en/learn/tutorials/rag-blueprint.html)
- **Year:** 2024–2025
- **Pattern shape:** First-phase: cheap expression (BM25 + simple modifiers,
  e.g. `bm25(title) + 3*freshness(timestamp)`) over ALL matches. Second-phase:
  re-rank top-K with expensive model (cross-encoder, learned-to-rank).
- **Aura applicability:** SKIP for G2-S3 (premature optimization at 45 docs),
  but **note for Phase-RAG**: when Aura grows past ~5k docs and we add a
  cross-encoder rerank, the phased pattern is the right shape. The two-stage
  filter-then-rerank pipeline is also confirmed by production RAG literature
  (F12).
- **Quantitative:** Vespa example uses `+3×` additive modifier, NOT
  multiplicative. Additive is cheap; multiplicative requires both factors to be
  on comparable scales.

### F7 — Reciprocal Rank Fusion is still SOTA for fusion (2024–2025)

- **Source:** [BigData Boutique RRF deep-dive](https://bigdataboutique.com/blog/reciprocal-rank-fusion-how-it-works-and-when-to-use-it),
  [Serghei's RRF post](https://blog.serghei.pl/posts/reciprocal-rank-fusion-explained/),
  [TREC RRF original 2009 (Cormack, Clarke, Büttcher)](https://plg.uwaterloo.ca/~gvcormac/cormacksigir09-rrf.pdf)
- **Year:** RRF 2009 original; 2024 confirmations from Elastic / OpenSearch / Qdrant / Weaviate / Azure adoption
- **Pattern shape:** RRF (`score = sum over rankers of 1/(k+rank)`, k=60
  default) beats CombSUM, CombMNZ, Condorcet, and even learned fusion methods
  on TREC + LETOR 3 benchmarks. It's resilient to score distribution
  differences and needs zero tuning. It's used "under the hood by
  Elasticsearch's rrf retriever, OpenSearch's hybrid search, Weaviate, Qdrant,
  and Azure AI Search" — i.e. ubiquitous.
- **Aura applicability:** ABSORB-CONFIRM — RRF is the right fusion choice. Don't
  swap it for CombSUM. Aura's k=60 matches the universal default.
- **Quantitative:** k=60 is the de-facto industry value (TREC paper used 60,
  every implementation kept it).

### F8 — RRF's structural weakness: can't guarantee position 1 from any single signal

- **Source:** [arXiv 2508.01405 "Balancing the Blend" 2025](https://arxiv.org/pdf/2508.01405),
  [Meilisearch hybrid fix](https://www.meilisearch.com/blog/fixing-hybrid-search)
- **Year:** 2025
- **Pattern shape:** RRF averages rank positions. A document at rank-1 in one
  ranker but absent in two others scores `1/61 ≈ 0.0164`. A document at rank-3
  in all three rankers scores `3/63 ≈ 0.0476` — beats the exact match. This
  "weakest link" phenomenon is named in the arXiv paper as fundamental to
  rank-fusion methods.
- **Aura applicability:** ABSORB — proves that RRF alone cannot satisfy "exact
  slug must win". The G2-S3 tier scoring (1000×) attempts to compensate by
  inflating the exact-channel raw score before fusion, but RRF strips raw scores
  and uses ranks — so the 1000× actually has zero effect on RRF's rank input
  (it's still rank-1 in the exact channel regardless of magnitude). **The
  graphify port works inside the exact channel (picks which slug is rank-1),
  but does NOTHING to help that rank-1 dominate the final fused result.**
  This is the bug Davide must understand before shipping.
- **Quantitative:** RRF math: rank-1 in 1 channel = 0.0164; rank-3 in 3 channels
  = 0.0476. The 3-way moderate match wins fusion 2.9× over the single-channel
  exact match. **This is exactly the scenario reported in the G2-S3 motivation
  ("davide-marchetto" loses to broader semantic matches).**

### F9 — Hard-precedence / pin-before-fusion pattern

- **Source:** [Hybrid Search Done Right (Medium, Feb 2026)](https://ashutoshkumars1ngh.medium.com/hybrid-search-done-right-fixing-rag-retrieval-failures-using-bm25-hnsw-reciprocal-rank-fusion-a73596652d22),
  [VE3 Hybrid Search Ranking](https://ve3.global/blog/hybrid-search-ranking-fusion-and-relevance)
- **Year:** 2026
- **Pattern shape:** When exact-match precedence is a hard requirement (legal,
  product-SKU, identifier lookup), production systems run a pre-fusion check:
  if the lexical channel returns a document with an "exact identifier match"
  flag, that document is pinned to rank 0 in the final list BEFORE RRF runs on
  the rest. This is the "Invoice INV-98217" pattern called out by VE3.
- **Aura applicability:** **ABSORB → G2-S3 design change.** The graphify
  tier scorer is the right machinery; the right place to apply the EXACT tier
  is BEFORE RRF as a hard pin, not as an input to RRF. Two-line change:
  in `mergeHybridResults`, if any result in the exact channel has `tier=EXACT`
  on its slug, pin it to position 0 and run RRF on the remaining N-1.
- **Quantitative:** No magic numbers needed — the pin is binary precedence,
  matching Algolia / Meilisearch / Typesense convention.

### F10 — Field weighting (title^N body^1) — typical production ratios

- **Source:** [Title Search: when relevancy is only skin deep (OSC, 2014)](https://opensourceconnections.com/blog/2014/12/08/title-search-when-relevancy-is-only-skin-deep/),
  [Easier Relevance Tuning in Elasticsearch 7.0](https://www.elastic.co/blog/easier-relevance-tuning-elasticsearch-7-0)
- **Year:** 2014–2020 (foundational)
- **Pattern shape:** Standard Elastic/Solr boost values cluster around
  `title^5..^10` and `body^1`, sometimes with a `tags^2..^3`. Higher than 10×
  is unusual outside of pure-identifier fields. Solr edismax `qf=title^5.0
  abstract^2.0 body^1.0` is a canonical example.
- **Aura applicability:** ABSORB — informs the eventual multi-field exact
  channel. For Aura today: `exactMatchDB` already checks slug and title
  together. Worth splitting into `slug^N` (highest), `title^M` (medium), `body
  unmatched` because slugs are user-typed identifiers and warrant the highest
  weight.
- **Quantitative:** **Typical production ratios: 5:1 to 10:1 between strongest
  and weakest field.** Graphify's 1000:1 ratio is 100× the production norm. The
  difference is graphify is encoding *tier* (exact vs prefix vs substring) into
  the same scalar, where production systems encode tier separately via
  successive sort (Algolia) or use it as a binary boost-mode (Elasticsearch
  `multi_match type=cross_fields` with `tie_breaker`).

### F11 — IDF caching: per-segment in Lucene, full-recompute on small corpora

- **Source:** [Lucene caching & optimization (DeepWiki)](https://deepwiki.com/apache/lucene/3.3-caching-and-optimization),
  [Lucene LRUQueryCache](https://lucene.apache.org/core/7_3_1/core/org/apache/lucene/search/LRUQueryCache.html)
- **Year:** 2024 (Lucene 9.x patterns)
- **Pattern shape:** Lucene caches IDF per-segment, invalidates on segment
  merge. Best-practice guidance from the docs: "caching policies that only
  cache on 'large' segments" — small/NRT segments aren't worth caching.
- **Aura applicability:** ABSORB-SIMPLIFIED — at 45–500 docs, the entire IDF
  table is ~500 entries × 8 bytes = 4 KB. **Full recompute on every write is
  trivially cheap.** The graphify-style generation-counter invalidation
  proposed in G2-S3 is correct but over-engineered for our scale. Acceptable
  simplification: drop the cache entirely and recompute IDF lazily on first
  query of each write epoch.
- **Quantitative:** Recompute cost at 500 docs × 5000 unique tokens (worst
  case) ≈ 2.5M map lookups ≈ <20 ms in Go. At 45 docs: <2 ms. **Cache
  unnecessary below ~50k docs.** Keep the cache code as planned but flag for
  removal if profiling shows zero impact.

### F12 — Two-stage filter-then-rerank (the modern RAG standard)

- **Source:** [Stage 1 / Stage 2 RAG pattern (Medium)](https://machine-mind-ml.medium.com/production-rag-that-works-hybrid-search-re-ranking-colbert-splade-e5-bge-624e9703fa2b),
  [Hybrid retrieval and reranking (Genzeon)](https://www.genzeon.com/hybrid-retrieval-deranking-in-rag-recall-precision/)
- **Year:** 2025
- **Pattern shape:** Stage 1 = hybrid (BM25 + vector + RRF) returns top-100.
  Stage 2 = cross-encoder rerank picks top-5–10. This is the de-facto
  production RAG architecture.
- **Aura applicability:** SEPARATE-STORY (Phase-RAG / Phase-RERANK). Not in
  G2-S3 scope. But worth noting: a cross-encoder rerank in Stage 2 would
  naturally solve the "exact-slug doesn't win RRF" problem because the
  cross-encoder sees the exact lexical match in its inputs and weights it
  appropriately. **Long-term: cross-encoder rerank > tier-boost tuning.**
- **Quantitative:** Typical Stage 1 returns 50–200, Stage 2 returns 5–20.
  At Aura's 45-doc corpus, Stage 1 = "everything" and Stage 2 = top-5 is the
  whole game.

### F13 — Multi-token query scoring: BM25 sums per-term contributions

- **Source:** [Vespa BM25 docs](https://docs.vespa.ai/en/ranking/bm25.html),
  [Wikipedia Okapi BM25](https://en.wikipedia.org/wiki/Okapi_BM25)
- **Year:** Foundational + 2025 confirmation
- **Pattern shape:** BM25 sums each query term's IDF-weighted TF contribution
  to produce the document score. Sum is universal; max-per-term is unusual.
  Graphify uses max-tier-per-term then sum across terms, which is a hybrid: it
  picks the strongest tier per term (no double-counting tier-match in body if
  same term hits in title) then sums.
- **Aura applicability:** ABSORB-CONFIRM — graphify's "max-per-term then sum
  across terms" is sound. It deviates from pure BM25 (which sums all hits per
  term) in a defensible way: tier semantics make multiple hits of the same term
  in different tiers conceptually overlapping, so taking the strongest tier
  avoids double-counting.
- **Quantitative:** Pure BM25 would give a higher score to "robot robot robot"
  in the title than to "robot" in the slug. Graphify's max-tier prevents
  that pathology. For Aura: **keep the max-per-term pattern as proposed.**

---

## Anti-patterns observed

1. **Tuning boost values per-query rather than per-corpus.** Production teams
   that tune `title^10` for one set of test queries see it break on another set.
   The fix is to either (a) use successive sort with binary tier flags
   (Algolia/Meilisearch), or (b) commit to learning-to-rank with held-out
   judgment data. Aura at 45 docs has neither the judgment-data budget nor the
   query diversity to make tuning sensible — **use binary tier pins, not
   tuned weights.**

2. **Calling rank-fusion when you mean score-fusion.** RRF is rank-fusion (it
   discards raw scores). If you want score-fusion (weighted sum of normalized
   scores), you need a different algorithm. Aura's current code does both:
   normalize scores to [0,1] (commit 13d004d3) AND fuse via RRF. This is
   defensible (the normalized scores feed downstream consumers like the LLM
   that needs an absolute confidence), but it means **the 1000× tier weight in
   the EXACT channel ONLY matters for the absolute score the LLM sees, NOT for
   the RRF rank position.** Davide must understand this before approving the
   ratio.

3. **Stale IDF after burst writes.** OpenSearch / Elasticsearch see this on
   high-write-rate clusters. Aura's wiki writes are LLM-driven and rare; the
   risk is low. The G2-S3 generation-counter invalidation is correct.

4. **Confusing exact-match-on-token with exact-match-on-field-value.** When the
   user types `davide-marchetto`, the query is a SINGLE token (the slug-as-id).
   When the user types `davide marchetto` (space), it's two tokens. Aura's
   `significantSearchTerms` splits on space; the slug-as-id case must be
   detected and NOT split, or the EXACT tier never fires. **Add to G2-S3
   acceptance: tokenize-preserves-slugs test.**

5. **Boost-stuffing the title field.** Multiple production blogs warn: making
   title boost too high (>20×) causes false positives where any 2-token title
   match outranks a 5-token body match that's actually more relevant. At
   Aura's scale we won't hit this directly, but the principle reinforces F10's
   "5–10× is normal."

6. **Trusting vendor blog claims.** Multiple vendor posts claim "10x faster
   than X" without methodology. The only solid sources for this research were
   peer-reviewed (RRF 2009 paper, arXiv 2508.01405), official docs (Algolia,
   Typesense, Meilisearch, Lucene, Vespa), and structural OSS code reading
   (graphify). Vendor blogs got cited for breadth but their numerical claims
   were not load-bearing.

---

## Open design questions for Davide

These are the **calibration choices** that need a yes/no before G2-S3 lands:

1. **Should the EXACT tier be a hard pre-fusion pin (rank 0 enforced) OR stay
   inside the exact channel and let RRF do its thing?** (See F8, F9.)
   - **My recommendation:** add the hard pin. It's <20 LOC, it gives a strong
     guarantee, and it matches Algolia/Meilisearch/Typesense convention.
     Without the pin, the tier weight only affects the per-channel rank, not
     the fused position — which means graphify's 1000× weight is decoratively
     correct but functionally a no-op for the user-visible position problem.

2. **Replace the 1000/100/1 ratio with a derived "must exceed sum of all
   substring contributions" constant?** (See top-3 #2.)
   - **My recommendation:** keep 1000/100/1 if we also add the pre-fusion pin
     (then the ratio is irrelevant — only the per-channel rank matters).
     Replace with `K = sum_max(substring) + 1` ONLY if we decide NOT to add
     the pin. The two approaches are alternatives, not complements.

3. **Drop the IDF cache for now?** (See F11.) Recomputing IDF on every query
   at 500 docs costs <20 ms. The cache adds invalidation complexity that
   could be the source of future cache-coherence bugs.
   - **My recommendation:** ship the cache as planned but flag it as
     `// TODO(scale): drop if perf shows <50ms recompute up to 5k docs`.
     Don't pre-optimize, don't pre-de-optimize.

4. **Split `exactMatchDB` into `slug` and `title` sub-channels with different
   weights?** (See F10.) Aura's slugs are user-typed identifiers (high
   intentionality); titles are LLM-generated prose (medium intentionality).
   - **My recommendation:** defer to a follow-up story. G2-S3 is already
     bundled (tier + IDF + cache + test); a 4th change crosses the
     "one-module-per-slice" feedback rule.

5. **Add a tokenize-preserves-slugs test to G2-S3 acceptance?** (See
   anti-pattern #4.) Without it, `davide-marchetto` could be split to
   `["davide", "marchetto"]` and the EXACT tier on the slug-as-whole would
   never fire.
   - **My recommendation:** **YES, mandatory.** Add as acceptance criterion
     7: `TestExactMatchTier_PreservesHyphenatedSlugAsSingleToken`. This is
     the most likely silent failure mode of the feature as planned.

6. **Phased ranking + cross-encoder rerank as Phase-RAG follow-on?** (See F6,
   F12.) Once corpus passes ~1k docs, the right play is Stage 1 hybrid
   retrieval + Stage 2 cross-encoder. Not for G2-S3, but worth a note in the
   MEMORY snapshot for the next Phase-RAG planning session.
   - **My recommendation:** capture as a seed in `MEMORY.md`, no story today.

---

## Source-of-truth references (cited above)

- [Algolia ranking criteria](https://www.algolia.com/doc/guides/managing-results/relevance-overview/in-depth/ranking-criteria)
- [Typesense Ranking and Relevance](https://typesense.org/docs/guide/ranking-and-relevance.html)
- [Meilisearch typo tolerance](https://www.meilisearch.com/docs/learn/relevancy/typo_tolerance_settings)
- [Meilisearch exactness criterion spec](https://specs.meilisearch.com/specifications/text/0036-exactness-criterion.html)
- [Elasticsearch multiplicative boosts (2025)](https://www.elastic.co/search-labs/blog/bm25-ranking-multiplicative-boosting-elasticsearch)
- [Elasticsearch function_score boost-by-popularity (Labs)](https://www.elastic.co/search-labs/blog/function-score-query-boosting-profit-popularity-elasticsearch)
- [OpenSearch hybrid best practices](https://opensearch.org/blog/building-effective-hybrid-search-in-opensearch-techniques-and-best-practices/)
- [OpenSearch hybrid optimization docs](https://docs.opensearch.org/latest/search-plugins/search-relevance/optimize-hybrid-search/)
- [Vespa phased ranking](https://docs.vespa.ai/en/ranking/phased-ranking.html)
- [Vespa BM25 ranking feature](https://docs.vespa.ai/en/ranking/bm25.html)
- [BigData Boutique — RRF deep-dive](https://bigdataboutique.com/blog/reciprocal-rank-fusion-how-it-works-and-when-to-use-it)
- [Lucene caching & optimization (DeepWiki)](https://deepwiki.com/apache/lucene/3.3-caching-and-optimization)
- [Title search relevancy (OSC, 2014)](https://opensourceconnections.com/blog/2014/12/08/title-search-when-relevancy-is-only-skin-deep/)
- [Field boost tuning with LTR (OSC, 2022)](https://opensourceconnections.com/blog/2022/12/16/approaches-to-field-boost-tuning-with-learning-to-rank/)
- [Balancing the Blend (arXiv 2508.01405)](https://arxiv.org/pdf/2508.01405)
- [Cormack et al., RRF original SIGIR 2009](https://plg.uwaterloo.ca/~gvcormac/cormacksigir09-rrf.pdf)
- [Production RAG: Hybrid + Rerank (Medium 2025)](https://machine-mind-ml.medium.com/production-rag-that-works-hybrid-search-re-ranking-colbert-splade-e5-bge-624e9703fa2b)
- [Hybrid Search Done Right (Medium Feb 2026)](https://ashutoshkumars1ngh.medium.com/hybrid-search-done-right-fixing-rag-retrieval-failures-using-bm25-hnsw-reciprocal-rank-fusion-a73596652d22)
- [graphify v5 on GitHub](https://github.com/safishamsi/graphify/tree/v5)

---

## Methodology note

WebSearch + WebFetch over 45 minutes. 13 findings retained from ~30 sources
scanned. Vendor blog claims cross-checked against official docs or peer-reviewed
sources where load-bearing; vendor-only claims demoted to background context.
Distinguished ACADEMIC (RRF original paper, arXiv 2508.01405) from PRODUCTION
DOCS (Algolia, Typesense, Meilisearch, Elastic Labs, OpenSearch, Vespa, Lucene)
from VENDOR MARKETING (filtered out for numeric claims).

Aura-corpus scale annotations: most "best practices" assume 10k–10M docs.
Flagged what matters at 45–500 docs vs deferred to post-1k-docs scale.
