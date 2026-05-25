# Wave G2 closure — 4-agent research synthesis (2026-05-25)

Synthesis of 4 parallel research agents launched 2026-05-25 to validate
and refine the Wave G2 closure plan (`docs/aura-g2-s2-s3-plan-2026-05-25.md`)
before implementation. Two D:/tmp surveys + two online searches, each ≤45
min wall-clock.

Inputs:
- `docs/research-tmp-suggest-questions-2026-05-25.md` (D:/tmp survey for G2-S2)
- `docs/research-tmp-exact-match-ranking-2026-05-25.md` (D:/tmp survey for G2-S3)
- `docs/research-online-suggest-questions-2026-05-25.md` (online for G2-S2)
- `docs/research-online-hybrid-ranking-2026-05-25.md` (online for G2-S3)

---

## 1. Validations (what we keep as-planned)

### G2-S2 architecture validated

- **Capability transparency over generic prompts** (Fuselab 2026 + MS
  Copilot Studio): Aura's current "posso aiutarti con..." IS the
  documented anti-pattern G2-S2 is designed to kill. The 4-bucket +
  named-tag design is correct.
- **Workflow-boundary timing** (Kuo et al. IUI '26): 52% engagement at
  task boundaries vs 62% dismissal mid-task. Validates G2-S2's
  tool-driven design (LLM decides when to fire), argues AGAINST a
  future server-push proactive trigger. Stays.
- **Template-driven generation correct at ≤500 pages** (InfraNodus
  + academic KGQG lit). Full LLM/GNN pipelines would be overkill at 45.
  No reshape needed.

### G2-S3 ratio safe at current scale

- At 45-page corpus, IDF range is `[0.69, 3.83]` (computed by D:/tmp
  ranker survey). Tier boundaries hold: `EXACT(1000) > PREFIX(100×3.83
  = 383)`. The 1000/100/1 ratio is mathematically safe through ~2k
  pages. So as channel-internal weighting it's fine.
- **Crucial caveat below** — the ratio is *theatrical* at the fusion
  layer. See §2 G2-S3 refinement.

---

## 2. Refinements (changes to the plan to land in the SAME commit)

### G2-S2 refinements — 4 small additions (~70 LOC, +1 prompt-overlay edit)

All four absorbable into the existing ~180 LOC budget without bloating
the story (~70 LOC net additions).

#### G2-S2.refA — Anti-double-emit (~25 LOC + 1 SQLite table)

- **Source**: openhuman GPLv3 concept (D:/tmp survey, Pattern 1).
- **Why**: spec asserts byte-determinism but has no cross-session
  anti-repeat. Without it, the same 4 questions surface every cold
  start until the graph changes — defeats the UX purpose.
- **Implementation sketch**:
  - New SQLite table `suggested_question_emissions` columns
    `(emission_hash TEXT PRIMARY KEY, first_emitted_at DATETIME, last_emitted_at DATETIME)`.
  - `emission_hash` = SHA-256 of `(type + slugs sorted)`.
  - On emit: write/update row; on next call, drop hashes seen in last
    7 days unless graph changed (compare last graph generation
    counter).
  - Migration: add via `internal/db/migrations/`.
- **Acceptance**: new test `TestSuggestQuestions_AntiDoubleEmit` — two
  back-to-back calls produce a STRICT-subset on the second.

#### G2-S2.refB — Body-token enrichment for question templates (~30 LOC)

- **Source**: elysia BSD concept (D:/tmp survey, Pattern 2).
- **Why**: at 45-page wiki size, structural-only templates produce
  weak questions ("what connects A, B, C?"). Injecting title + first
  H2 tokens raises signal density: "What is the work on [[main-motor]]
  about?" beats "what connects [[main-motor]] to the rest?".
- **Implementation sketch**:
  - For bucket 4 (isolated nodes): read page Title + first H2 heading,
    inject into the question text via templated format string.
  - Cache per-slug "anchor token" map; rebuild on `RefreshPage`.
- **Acceptance**: new test `TestSuggestQuestions_AnchorTokenEnrichment`
  asserts isolated-node questions cite the page's first H2 heading
  text verbatim when one exists.

#### G2-S2.refC — Template-hygiene sentinel test (~15 LOC)

- **Source**: docker-gordon Style B concept (D:/tmp survey, Pattern 3).
- **Why**: cheap quality floor. Bars generic stubs ("can I help?"),
  requires interrogative ending + at least one verbatim `[[slug]]`
  citation per emitted question.
- **Implementation**: a single sentinel test
  `TestSuggestQuestions_TemplateHygiene` that runs each bucket template
  against fixtures and asserts:
  - ends with `?`
  - contains ≥ 1 `[[…]]` citation
  - contains no banned phrases (`"how can I help"`, `"posso aiutarti"`,
    `"what would you like"` etc.)
- **No production-code change needed** — the test enforces template
  authors don't drift.

#### G2-S2.refD — Ovsiankina suggestive-ending framing directive (PROMPT EDIT)

- **Source**: Park et al. PMC 2024 (online research, Pattern 2). +20%
  follow-up engagement and +47% wider perspective coverage when
  suggestions are framed in prose ("È utile menzionare X o Y?") vs an
  explicit bullet list.
- **Why**: complements the structured `[]Question` data — the prose
  rendering at the chat surface should NOT be a 4-bullet dump but a
  conversational lead-in followed by the questions.
- **Implementation**: edit `runtime-workspace/TOOLS.md` to add a
  4-line directive under the `search` tool description:
  > After `search(action=suggest_questions)` returns, surface the
  > results as a brief conversational lead-in plus the questions —
  > NOT as a flat bullet list. Example: "Guardando la wiki vedo che…
  > vorresti che ti aiutassi su X o su Y?"
- **No code change**. Ships in the same commit as G2-S2.

### G2-S3 refinements — 2 critical, 1 sub-pattern absorb (~30 LOC net change)

This is the section where the research **substantially changes the
plan**. The online ranking research surfaced a structural weakness in
the original lift.

#### G2-S3.refA — **Hard pre-fusion pin on slug==query** (~20 LOC) [CRITICAL]

- **Source**: Algolia, Typesense, Meilisearch all use successive sort
  with a binary "exact" tiebreaker — NOT a numerical boost (online
  ranking research, Pattern 1).
- **The bug the original plan misses**: RRF discards raw scores and
  only uses RANKS. The 1000× weight inside the EXACT channel only
  picks WHICH slug wins INSIDE that channel — it has **zero effect**
  on the fused position. After RRF normalisation (commit `13d004d3`),
  the exact channel's rank-1 hit caps at ~0.42 of the fused score
  regardless of whether the channel-internal magnitude was 1.0 or
  1000.0. So a strong vector+FTS combination on a DIFFERENT page can
  still outrank the literal-slug match.
- **Fix**: in `mergeHybridResults`, BEFORE running RRF, check if any
  exact-channel result has `match_tier == EXACT && match_target == "slug"`.
  If so: prepend that result at rank 0 with `Score = 1.0`, then run
  RRF on the remaining channels. ~20 LOC change.
- **Why this is the right shape**: it makes the "literal slug query
  → slug at position 0" guarantee LOAD-BEARING in code, not implicit
  in a magic ratio. Composes with the 1000/100/1 tier system (which
  still does its job: picking WHICH slug wins inside the exact
  channel when multiple match).

#### G2-S3.refB — Hyphenated-slug tokenization regression test (~15 LOC) [CRITICAL]

- **Source**: online ranking research, silent-failure flag.
- **Why**: `davide-marchetto` could tokenize via
  `significantSearchTerms()` to `["davide", "marchetto"]` and never
  match the slug literally — the EXACT tier silently never fires for
  any hyphenated slug. This is a class of regression we can pin once
  and forget.
- **Test**: `TestExactMatchTier_PreservesHyphenatedSlugAsSingleToken`
  — queries `davide-marchetto`, asserts the page is at position 0 with
  score ≥ 0.9 AND the per-term tier classification logged as EXACT not
  SUBSTRING.

#### G2-S3.refC — Three graphify sub-patterns to lift in the same commit

- **Source**: D:/tmp ranking survey, Pattern 1.
- **What**:
  - `_SOURCE_MATCH_BONUS = 0.5` → port as `_CATEGORY_BONUS` for Aura
    (a slug whose `NodeMeta.Category` literally appears in query gets
    a small bonus).
  - `gap_ratio = 0.2` score-gap sanity check — emit a debug log line
    when top score < `gap_ratio × top_score` of next tier (noise
    flag, no behaviour change).
  - Object-identity cache keying (cache stored on the `*GraphIndex`
    pointer itself) — simpler than the generation-counter plan and
    auto-invalidates when `LoadFromPages` swaps the index pointer.
- **Adds**: ~25 LOC; replaces the IDF-cache invalidation strategy in
  the original plan (simpler).

#### G2-S3.refD — IDF cache scale flag (annotation only)

- **Source**: online research at scale-vs-complexity tradeoff.
- **Why**: at 45-500 docs, IDF caching is over-engineered. We keep it
  for forward compatibility but annotate the threshold.
- **Implementation**: add `// TODO(scale): drop IDF cache if recompute
  takes < 50ms at 5k docs — measured cost likely < 10ms today` next to
  the cache definition.
- **No code change** — only the comment. Ships in same commit.

---

## 3. SEPARATE-STORY candidates (catalogued, not staged)

All real wins but each its own atomic story; **do not** absorb into
G2-S2 / G2-S3.

| Story ID | Lift | LOC | Source |
|---|---|---|---|
| G2-S2b | Stale-question suppression (RepeatNet-style, beyond the 7-day anti-double-emit) | ~50 | Online research §RepeatNet 2024 |
| G2-S2c | Reply-tail follow-ups (Perplexity pattern — conversation-context-aware variant of suggest_questions) | ~150 | Online research §Perplexity |
| G2-S2d | Hotness-delta as 5th bucket (track which pages got touched in last N days) | ~80 | D:/tmp openhuman concept |
| G2-S2e | Curated hero-questions fallback file | ~50 | D:/tmp Pattern 6 |
| G2-S3-bypass | Web2BigTable "exact-name bypass → hybrid → synthesise" full cascade (extends the pre-fusion pin into a richer short-circuit) | ~120 | arXiv 2604.27221 |
| G2-S3-fields | Slug-vs-title-vs-body field weighting (`slug^5 title^2 body^1`) — pays off at >500 pages | ~80 | Online ranking research |
| G2-S3-contig | Codex contiguity scorer (penalty for scattered-match false positives) | ~60 | Apache 2.0 |
| G3-betweenness | Betweenness centrality as Bucket 2 secondary signal in suggest_questions | gated on Wave G3 clustering | InfraNodus |

Sequencing: any of these become candidates AFTER Wave G2 ships and
the live-probe gate fires. None blocks G2 ship.

---

## 4. Anti-patterns the research surfaced (Aura must avoid)

From the online research distillations:

1. **Numerical-boost theater** — relying on a 1000× boost INSIDE a
   rank-fusion channel and assuming it guarantees position-1. Fixed
   by G2-S3.refA.
2. **Generic "how can I help?"** — the documented worst-case cold
   start. G2-S2 already designed to kill this.
3. **Bullet-list dump** — suggestion fatigue. Fixed by G2-S2.refD
   (Ovsiankina framing in TOOLS.md).
4. **Repeating the same 4 questions every session** — defeats the
   purpose. Fixed by G2-S2.refA (anti-double-emit table).
5. **Mid-task unsolicited proactive surface** — 62% dismissal rate.
   G2-S2's tool-driven design (LLM decides) sidesteps this; we add
   a note in the SOUL.md framing that suggest_questions fires at
   session boundaries OR explicit `/suggest`-style intents, not
   mid-task.
6. **Boost overfitting** — tuning ratios to one corpus that break on
   another. The graphify 1000/100/1 was the canonical example but
   our pre-fusion pin makes the ratio cosmetic; it's now safe to
   inherit verbatim.

---

## 5. Open questions for Davide (research surfaced these, NOT actioned)

These need a human decision before G2-S2 ships in particular:

| # | Question | My recommendation (overridable) |
|---|---|---|
| 1 | Manual `/suggest` slash command in Telegram, or only LLM-decided? | Both. Wire `/suggest` as a no-arg command that injects `"suggerisci domande"` into the user turn, lets the agent loop decide. Trivial. |
| 2 | Reply-tail follow-ups story priority — block on G2-S2 or independent? | Independent. Ship G2-S2 first. G2-S2c when reply quality data justifies. |
| 3 | Stateless first ship, OR include G2-S2.refA anti-double-emit from day 1? | Include from day 1 (~25 LOC). Without it the headline UX fails after the first session. |
| 4 | English vs Italian templates inside `questions.go`? | English template strings + a tiny translator at the wiki_suggest_questions delegate. Matches the "all prompts in English" rule from memory `feedback_all_prompts_in_english_only`. |
| 5 | Ship the SOUL.md / TOOLS.md framing directive in same commit as G2-S2? | Yes. Stays atomic and gives the UX win immediately. ~4 lines of prose. |
| 6 | Bucket priority randomization to vary cold-start (round-robin within priority tier)? | NO. Determinism is the contract. The anti-double-emit covers stale-suppression. |

For G2-S3, the open questions are smaller:

| # | Question | My recommendation |
|---|---|---|
| 7 | Pre-fusion pin: hard `Score=1.0` pin, or "highest possible RRF score"? | Hard 1.0. Makes the position-0 guarantee load-bearing in code. |
| 8 | Keep 1000/100/1 tier ratio after the pin, or simplify? | KEEP as channel-internal. Pin makes the ratio cosmetic but it still picks the right slug WHEN multiple match. Removing it would require a different intra-channel ordering. |
| 9 | Object-identity cache key OR generation-counter? | Object-identity (G2-S3.refC). Simpler, auto-invalidates on full reload, matches graphify's actual choice. |

---

## 6. Updated story scope (delta vs original plan)

### G2-S2 final scope

- Original LOC: ~180
- After refinements: ~250 (+25 anti-double-emit, +30 anchor-token, +15 hygiene-test, +0 prompt-overlay)
- Files now: `internal/wiki/questions.go` + `questions_test.go` +
  `registry/wiki_suggest_questions.go` + `search.go` extension +
  `cmd/aura/app_wire.go` wiring + `internal/db/migrations/<new>.go`
  + `runtime-workspace/TOOLS.md` edit.
- Still 1 atomic commit per `feedback_one_module_per_slice`.
- Still SMALL story, still 1 session.

### G2-S3 final scope

- Original LOC: ~80
- After refinements: ~115 (+20 pre-fusion pin, +25 sub-pattern absorb,
  +15 hyphenated-slug test, − some IDF-cache complexity)
- Files now: `qdrant_hybrid.go` (now adds pre-fusion pin to
  `mergeHybridResults` upstream, scoredExactMatchDB downstream),
  `idf_cache.go` (simpler, object-identity keyed),
  `wiki_hybrid_test.go` extended.
- Still 1 atomic commit, still SMALL story, still 1 session.

### Joint sequencing — unchanged

1. **G2-S3 first** — pre-fusion pin is the highest-leverage change in
   either story; lands the literal-slug guarantee day-one.
2. **G2-S2 second** — better lands after ranking is correct because
   the suggest-questions output cites slugs that should rank
   correctly when the user follows up.

After both ship: Wave G2 CLOSED → ship gate → stage Wave G3.

---

## 7. Master plan delta

Update `docs/aura-g2-s2-s3-plan-2026-05-25.md`:
- §G2-S2 — replace the "Buckets" section header with a note pointing
  to this synthesis for refs A-D additions. Update LOC budget 180→250.
- §G2-S3 — REPLACE the "Algorithm" subsection paragraph 3 with the
  pre-fusion pin description. Update "Files" list to include the
  pin location. Update LOC budget 80→115. Add the hyphenated-slug
  test to acceptance criteria. Replace IDF generation-counter
  description with object-identity cache keying.
- §Risks — add: "Without the pre-fusion pin, 1000× ratio is
  theatrical (RRF rank-only). Pin is non-negotiable for the
  position-0 guarantee."
- §Cross-references — add this synthesis doc + the 4 research docs.

That's the minimum surgical edit. Master plan stays the source of
truth for execution.
