# Research survey — D:/tmp/* patterns for "hybrid search + exact-match boost"

Target: inform Aura story **G2-S3** (IDF + three-tier 1000/100/1 exact-match
boost ported from `graphify/serve.py:67-121`).

Scope: 45-minute survey of curated reference repos under `D:/tmp/` for
ranking patterns that complement, validate, or challenge the graphify lift.

Aura baseline (today, audited):
- `internal/storage/search/qdrant_hybrid.go:129-201::exactMatchDB` — title
  substring match, uniform `Score=1.0` for any hit.
- `internal/storage/search/search.go::mergeHybridResults` — RRF fusion of
  three channels {exact-DB, FTS5 BM25, Qdrant vector} with weights
  `{1.0, 0.6, 0.8}` and `rrfK=60`.

G2-S3 plan: replace `exactMatchDB` with a three-tier scorer
`{exact=1000, prefix=100, substring=1} × IDF(term)` keyed by
`(generation, term)` cache, normalised to `[0,1]` so RRF still sees a
comparable channel.

---

## Pattern 1 — Graphify three-tier 1000/100/1 + per-term IDF + field-weighted source bonus

**Source**: `D:/tmp/graphify/graphify/serve.py:67-121` (the canonical lift).

**License**: MIT.

**Pattern shape**: Per node, the scorer iterates the query's normalised
terms and for each term adds the strongest of `{exact=1000, prefix=100,
substring=1}` × `idf(term)` — strongest tier per term, no double count.
A separate `_SOURCE_MATCH_BONUS=0.5` is added when the term hits
`source_file` (a different field than label). IDF is cached on the
`G.graph["_idf_cache"]` dict — auto-invalidates when the graph object is
replaced. `_pick_seeds(gap_ratio=0.2)` then prunes any candidate scoring
below `top_score × 0.2` so noisy common-term hits cannot steal seeds
from a dominant identifier.

**Aura applicability**: ABSORB (already the lift). Three sub-patterns to
absorb explicitly:
- **Field-weighted bonus**: the 0.5× source-file bonus is a fourth tier
  the current G2-S3 plan does not mention. Aura's analogue is "term hits
  `Page.Category` or `Page.Tags`" — a separate field with semantically
  different signal than title/slug. **Concrete change**: add a
  `_CATEGORY_BONUS=0.5` (or similar) tier so a query that hits a category
  name (e.g. `progetto`) still surfaces the canonical category page even
  if title doesn't match.
- **Score-gap seed pruning**: graphify uses this *after* scoring to
  pick BFS seeds, not for ranking itself. Aura's ranking returns a sorted
  list to RRF, so it doesn't need seed-picking — but the `gap_ratio=0.2`
  idea is useful as a **debug-time sanity log**: if `top_score / second_score
  < 1.2`, log a warning so we can spot ambiguous queries during operations.
- **Cache invalidation by object identity**: graphify keys the cache on
  the `G` networkx object itself, so any pipeline that replaces `G` invalidates
  for free. Aura's plan uses a generation counter on `qdrantRepository`,
  which works but is more code. **Concrete alternative**: key the IDF cache
  on `*GraphIndex` pointer identity (cheaper, zero counter maintenance) —
  this only works if `LoadFromPages` always swaps the index rather than
  mutating in place. Verify before adopting.

Answers to the concrete questions in the brief:
- **Is 1000/100/1 empirically validated?** Not in graphify — it's a
  hand-set ratio chosen so that EXACT always beats PREFIX even when
  IDF differs by ~10× across terms (because typical IDF ratios on a
  20k-node corpus stay within `ln(1+N/(1+df))` ≈ `[0.3, 9.5]`). For
  Aura with 45 pages (`N=45`), `idf ∈ [0.69, 3.83]` so EXACT(1000)
  always beats PREFIX(100×3.83=383). Ratio is safe at Aura's scale;
  ship as-is. Revisit only if/when wiki crosses ~2k pages.
- **Better IDF caches than generation counter?** Object-identity keying
  (graphify) is simpler if `LoadFromPages` always swaps. Otherwise
  generation counter is correct. Sorted-set TTL ("expire after 5 min")
  is also valid for write-heavy stores, but Aura writes are infrequent
  (LLM-edit per turn, not per ms) so TTL is over-engineering.
- **Field-weighted variant?** Yes — graphify's 0.5× source bonus IS
  the field-weighted pattern. Adopt for Aura: slug=1×, title=1×,
  category=0.5×, tags=0.3× (heuristic; not in graphify but obvious
  extension).
- **Multi-token, mixed-tier behaviour?** Graphify sums `max-tier ×
  IDF` per term. A query like `davide marchetto` where one term is
  a common first name and the other is a rare surname: high-IDF surname
  exact-match dominates the score regardless of low-IDF first-name
  substring hit. This is the right behaviour. The G2-S3 plan
  already specifies "take the strongest tier per term"; that maps to
  graphify exactly.

---

## Pattern 2 — Web2BigTable strictly-prioritised "exact-name → hybrid → synthesis" cascade

**Source**: `D:/tmp/aura-agent-loop-papers/2604.27221-Web2BigTable.txt:414-423`.

**License**: Academic (arXiv preprint, paper-only — no code released; concept
free to lift).

**Pattern shape**: When the agent's `SkillResolver` needs a capability, it
runs a **three-stage cascade in strict order**: (1) **exact-name match** on
local skills — if hit, return immediately and skip everything else; (2) only
if (1) misses, run a **hybrid BM25 + dense (BAAI/bge-m3, ChromaDB) pipeline
fused via RRF**, optionally refined by a cross-encoder; (3) only if (2)
misses, synthesise a novel skill on demand. The crucial design choice: the
hybrid pipeline is a **fallback**, not a fusion participant. Exact-name
match is a deterministic bypass that never enters RRF.

**Aura applicability**: SEPARATE-STORY (alternative architecture to G2-S3).

**Sketch**: Today's G2-S3 plan does the opposite — it boosts the exact channel
1000× inside RRF rather than short-circuiting. The graphify-tier-in-RRF
approach has two soft failure modes Web2BigTable's bypass avoids: (a) when
FTS5 and vector both return strong matches on a different page, the 1000×
boost can still lose if RRF's denominator (`1/(rank+k)`) compresses the
exact-channel rank-1 to ~0.0166 after normalisation; (b) IDF on tiny
wikis (Aura's 45 pages) gives narrow `[0.69, 3.83]` spread, so the
"weight" lever has less authority than at graphify scale (20k nodes).
A future G2-S5 story could add a "bypass" path: **if a single page has
slug or title literally equal to the normalised query string (no other
candidate), return `[that_page]` with score 1.0 and skip RRF entirely**.
~30 LOC in `qdrant_hybrid.go` ahead of the existing hybrid call. Zero
risk for non-literal queries (the predicate fires only when the query
is exactly an identifier). Lefthook-safe.

**Note**: This does NOT invalidate the 1000/100/1 lift — the lift still
handles the PREFIX and SUBSTRING cases the bypass doesn't cover. The two
patterns compose: bypass for literal-exact (1 page wins), tier-scoring
for everything else.

---

## Pattern 3 — Codex `fuzzy-match` start-of-string prefix bonus + contiguity penalty

**Source**: `D:/tmp/codex/codex-rs/utils/fuzzy-match/src/lib.rs:12-69`.

**License**: Apache 2.0.

**Pattern shape**: Case-folded subsequence matcher returning matched-char
indices + a score where **lower is better**. Score = `(last_match_pos −
first_match_pos + 1) − needle_len` (contiguity-window penalty: 0 for
contiguous match, positive for spread-out match), **with a flat `−100`
bonus when the first match is at position 0** (start-of-string). Result:
prefix matches always rank above non-prefix matches, AND contiguous
matches always rank above scattered matches, with no IDF/term-frequency
weighting at all.

**Aura applicability**: SEPARATE-STORY (small follow-up worth banking).

**Sketch**: Aura's G2-S3 differentiates prefix vs substring via a 100×
multiplier but ignores contiguity — `file_name` and `f_i_l_e_name` are
both `prefix` for query `file`. Codex's contiguity score would discriminate.
For Aura's use case (slug ranking, where slugs are kebab-case identifiers
that humans type roughly verbatim), contiguity matters less than for
fuzzy-completion ranking. **Worth banking as G2-S6 only if** post-G2-S3
probes show false positives where a slug like `f-l-a-k-y-test-x` ranks
above `flaky-test` for query `flaky`. Until then, the 1000/100/1 tier
gives enough lift. ~40 LOC if ever needed; reuse the codex algorithm
shape, port to Go.

Side observation: the codex `-100` bonus is **structurally identical**
to graphify's `100×` prefix multiplier — both encode "prefix beats
non-prefix" as a discontinuous jump, not a smooth continuous score.
This validates the discrete-tier choice in G2-S3 over a continuous
edit-distance scorer.

---

## Pattern 4 — Elysia / Weaviate LLM-driven `search_type` routing

**Source**: `D:/tmp/elysia/elysia/tools/retrieval/query.py:606-636`,
`D:/tmp/elysia/elysia/tools/retrieval/prompt_templates.py:213-225`.

**License**: MIT (Weaviate ecosystem).

**Pattern shape**: Elysia exposes Weaviate's hybrid stack to the LLM with
the search_type as an LLM-chosen argument (one of `keyword|vector|hybrid|
filter_only`). The prompt teaches the LLM: "use **keyword** when the search
terms WILL appear in the text (i.e. literal-identifier lookup); use **vector**
when only meaning matters; use **hybrid** when unsure". The LLM picks
per-query, then Weaviate runs the actual fusion (alpha-blended BM25 +
vector, server-side). Ranking is delegated; the intelligence is in route
selection.

**Aura applicability**: SEPARATE-STORY (orthogonal to G2-S3).

**Sketch**: Aura currently hardcodes hybrid for every `search(action=search)`
call. An LLM-side `search_type` argument would let Aura skip the vector
channel when the user typed a literal slug, saving ~50ms of Qdrant query
time per literal lookup. NOT competing with G2-S3 — even with `keyword`-only
routing, the tier scorer would still need to differentiate exact/prefix/
substring inside FTS5 results (FTS5 BM25 alone doesn't). **This is an
agent-loop optimisation, not a ranking fix**; bank as a Phase-OP+ follow-up
when search latency becomes the bottleneck (today it's not). ~20 LOC
behind a feature flag.

---

## Pattern 5 — Openhuman weighted-sum fusion `{graph 0.55, vector 0.30, keyword 0.15}` (NOT RRF)

**Source**: `D:/tmp/openhuman/src/openhuman/memory_store/unified/query.rs:21-23,1044-1086`.

**License**: GPLv3 (concepts only, no code copy).

**Pattern shape**: Production hybrid fusion in openhuman uses a simple
weighted **linear sum** of three normalised channels (graph, vector,
keyword), NOT RRF. Constants are static (`0.55 / 0.30 / 0.15`), with a
second variant for episodic-augmented queries (`0.45 / 0.25 / 0.10` +
episodic). Keyword channel is **term-coverage ratio** (`matched_terms /
total_terms`), not BM25. Per-channel scores are normalised by dividing
by the max score before fusion (`normalize_scores`, line 1077-1086).

**Aura applicability**: SKIP (Aura already chose RRF and shipped it).

Notes for the record: openhuman's choice validates that **fixed weighted
sum with normalised channels** is a production-viable alternative to RRF
when channels are well-calibrated. The trade-off RRF wins on: invariance
to channel score distribution (rank-only). Aura's existing `mergeHybridResults`
+ `normalisation` (commit `13d004d3`) is in the same ballpark.
Do NOT switch — but if Aura later finds RRF's `rrfK=60` constant is
hiding genuine score differences, weighted-sum-over-normalised is the
default fallback. The 0.55 graph weight in openhuman is also instructive:
graph signal is the **largest** weight, NOT vector. Aura's
`{exact 1.0, FTS5 0.6, vector 0.8}` puts vector second; this could be
re-evaluated once Wave G3 (graph clustering) ships.

---

## Pattern 6 — OpenAI ChatGPT product-lookup dual mode `search` vs `lookup`

**Source**: `D:/tmp/system_prompts_leaks/OpenAI/gpt-5-thinking.md:385-398`.

**License**: System prompt leak (concept only; reverse-engineered ChatGPT prompt).

**Pattern shape**: Production GPT-5 has two distinct product-query modes:
`search?: string[]` for fuzzy/discovery queries, `lookup?: string[]` for
**"product lookup query, expecting an exact match, with a single most
relevant product returned"**. Same backend, but the agent picks the mode
upfront based on whether it wants ranked discovery or deterministic resolution.
Reinforces the Web2BigTable cascade pattern at the API surface level —
"exact-match-expected" is a first-class semantic, not a ranking weight.

**Aura applicability**: ABSORB (lightweight, complements G2-S3 documentation).

Concrete change: when wiring G2-S3's tier scorer, also **document in the
LLM-facing `search(action=search)` description** that for a literal slug
lookup the call returns position-0 == the literal page if it exists. This
is essentially the Web2BigTable bypass exposed as a behavioural promise
rather than a separate action. Zero code; **2 lines in the tool
description**. Done in the same commit as G2-S3.

If the bypass story (Pattern 2 / G2-S5) ships, the dual-mode pattern
becomes more interesting — could expose `search(action=lookup, slug=...)`
as a separate action that skips RRF entirely. Defer until needed.

---

## Patterns checked and rejected

- **Openhuman `memory_store_raw_search`** (`memory_store/tools/raw_search.rs:1-177`,
  GPLv3): SQLite `LIKE '%q%'` over entity surface forms, ordered by
  `mention_count DESC`. SKIP — no tier differentiation, no IDF, no exact-prefix
  distinction. Strictly weaker than graphify lift. Worth noting only that openhuman
  intentionally keeps its "raw search" tool dumb and ranks by mention count
  (frequency-as-ranking), which is the opposite signal from IDF (rarity-as-ranking).
- **Openhuman `memory_tree/score/resolver.rs`** (GPLv3): "exact-match only"
  entity canonicalisation. SKIP — operates on entity ids pre-search, not
  query-time ranking. The phrase "exact match" here means lowercase normalisation
  of emails/handles, not a ranking tier.
- **Openhuman drill_down cosine-rerank** (`memory_tree/retrieval/drill_down.rs:102-156`,
  GPLv3): post-hoc cosine rerank with batched embeddings. SKIP — Aura
  already does single-query vector search; a rerank pass would add ~200ms
  per query for minimal gain at 45 pages, and the brief explicitly excludes
  cross-encoder / learned reranking infra.
- **Codex `tool_search` with external `bm25` crate** (`core/src/tools/handlers/tool_search.rs:11-46`,
  Apache 2.0): pure BM25 over tool description text, no exact-match boost
  at all. SKIP — Aura already has FTS5 BM25 as one channel; codex's choice
  to use ONLY BM25 (no hybrid) for tool search is interesting context but
  not transferable to a wiki-page retrieval scenario where slug-as-identifier
  matters.
- **Aura-agent-loop-papers `Lemon-Agent` three-tier context**
  (`2602.07092-Lemon-Agent.txt:162`): "three-tier context management" —
  intra-tool truncation + adaptive summarisation + retroactive compression.
  SKIP — tier here means agent context layers, NOT retrieval scoring tiers.
- **Aura-agent-loop-papers Kimi K2.5 "tiered caching"** (`2602.02276:1447`):
  same — caching tier hierarchy, not retrieval.
- **cli-printing-press tier-routing-golden** (`testdata/golden/expected/
  generate-tier-routing-api/...`): hits on "tier" are API codegen golden
  fixtures for a tier-routing OpenAPI scenario, not a retrieval pattern.
- **hermes-agent transcription/turnController**: hits on "tier"/"score"
  are unrelated (audio scoring, turn arbitration). No retrieval ranking.
- **nanobot filesystem and channel tests**: hits on "tier"/"boost" are
  filesystem path tier (ramdisk vs disk), not search ranking.
- **picobot**: zero substantive matches; not a retrieval system.
- **SkillX paper** (`2604.04804-SkillX.txt`): binary content, unreadable
  in 45-min window. Skipped per time-box rule.
- **Anthropic / Google / Misc system prompts**: searched all; only
  unrelated "case-insensitive exact match" hits in `browser.find` tools
  (different domain — page text search, not document ranking).

---

## Summary (top 3 by impact for G2-S3)

1. **Pattern 1 — graphify three-tier + field bonus + object-identity cache**:
   already the lift, but the brief misses three sub-patterns worth absorbing
   in the same commit: (a) `_SOURCE_MATCH_BONUS=0.5` → port as a
   `_CATEGORY_BONUS` / `_TAGS_BONUS` field-weighting; (b) gap-ratio sanity
   log for ambiguous queries (debug-only, no behaviour change); (c) consider
   object-identity cache keying over generation counter if `LoadFromPages`
   always swaps the `*GraphIndex` pointer (simpler, equally correct).
2. **Pattern 2 — Web2BigTable cascade (exact-name bypass → hybrid → synthesise)**:
   the canonical SOTA pattern for "literal identifier wins position-1". Does
   NOT invalidate the 1000/100/1 lift — composes with it. Bank as G2-S5
   "literal-slug bypass before RRF" (~30 LOC, ahead of `mergeHybridResults`).
   Eliminates the residual failure mode where 1000× still loses to a strong
   vector+FTS combination on a different page.
3. **Pattern 3 — codex prefix-position bonus + contiguity penalty**:
   structurally identical to graphify's 100× prefix multiplier (validates
   the discrete-tier design choice), with the added contiguity scorer
   as a potential G2-S6 if post-ship probes show scattered-match false
   positives. Until probes show the need, ship without.

**Nothing in the survey invalidates the 1000/100/1 graphify lift.** The
ratio is safe at Aura's 45-page scale (`idf ∈ [0.69, 3.83]` keeps tier
boundaries enforced) and the per-term-strongest-tier algorithm matches
production patterns in two independent codebases (graphify + codex
fuzzy-match). The most useful complement is the Web2BigTable cascade
pattern — bank as a follow-up story rather than blocking G2-S3.
