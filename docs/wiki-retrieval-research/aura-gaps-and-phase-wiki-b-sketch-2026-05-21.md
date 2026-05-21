# Aura wiki-retrieval gaps + Phase-WIKI-B prd sketch

**Date:** 2026-05-21
**Scope:** code-grounded gap analysis of Aura's current wiki/retrieval pipeline against (a) graphify-style retrieval patterns and (b) 2026 best practice. Companion docs: sibling agent A — graphify deep dive; sibling agent B — online state-of-art 2026 (this doc references them as placeholders, fill in once landed).
**Trigger:** live test 2026-05-21 — `search_memory("Delta Automazioni")` returned 10 noisy hits; the LLM then tried `wiki_page action=read` (invalid), then `search_memory` narrow ("Delta Automazioni cliente codice"), then `read_source` extract.md. 7 tool calls, 30s. Pre-fix it was 105s / 22 calls / empty reply.

---

## Current state — Aura wiki retrieval pipeline (code-grounded mini-map)

### `search_memory` tool (the LLM's only read-side wiki window)

`internal/agent/tools/registry/memory_search.go:154` `Execute`
1. Parses `query`, `scope` (all|wiki|sources|archive|proposals), `limit` (default 6, max 12), optional `chat_id`/`source_id`.
2. If `scope=wiki|all` → `t.wiki.Search(ctx, query, limit)` (`memory_search.go:277`).
3. If `scope=sources|archive|proposals|all` → `t.compact.Search(ctx, query, filter)` against the compact-memory index.
4. Merges all hits into one `[]memoryResult`, applies `relevanceTimesRecencyWithHalfLife` (`memory_search.go:401`): `score = clamp(relevance, 0..1) * 0.5^(ageDays/halfLife)` with halfLife=180d for wiki/source/graph kinds, 30d for archive/proposal.
5. Sorts by score desc, trims to `limit`, formats as markdown via `formatMemoryResults` (`memory_search.go:453`).
6. Output shape: one line per hit, e.g. `- [wiki] [[delta-automazioni]] — Title (3d ago) score=0.42 [exact=… fts=… vector=…] file=wiki/delta-automazioni.md category=… tags=… related=[…] sources=[…]` + indented snippet (~260 chars around the query offset, `snippetAround` `memory_search.go:650`).

### `wiki.Search` — what actually runs under the hood

The `search.Searcher` interface is implemented by `qdrantRepository` (`internal/storage/search/qdrant.go:96`), which simply forwards to `qdrantSearcher.Search` (`qdrant.go:342`):
1. Embed query → call Qdrant HTTP `/collections/<coll>/points/search` with `topK`.
2. Map Qdrant payload (`slug`, `title`, `content`, `kind`, `filepath`, `category`, `tags`, `related`, `sources`, `updated_at`, …) into `search.Result`.
3. **Score is raw cosine** (`point.Score` at `qdrant.go:377`). `ScoreExact/ScoreFTS/ScoreVector` are NEVER populated by this path — they default to 0.

**Finding — the wiki retrieval path is VECTOR-ONLY today.** `mergeHybridResults` (`search.go:426`) implements RRF fusion of exact/fts/vector groups with weights 1.0/0.6/0.8 and `k=60`, but it is only exercised by `compact_qdrant.go` (compact memory) and unit tests. **The wiki path never fuses FTS5+vector despite the SQLite mirror existing** (`sqliteSearcher` in `internal/storage/search/sqlite.go`). So the "hybrid" label in the package comment (`search.go:3-5`) is aspirational for the wiki collection.

### `wiki_page` tool (write-side)

`internal/agent/tools/registry/wiki.go:71` `Description` — four actions: `create`, `replace`, `edit`, `append`. No `read` action. Description at `wiki.go:80-86` tells the LLM to read via `file action=read` or `search_memory`, but it's buried — the LLM tried `wiki_page action=read` today anyway and `UnknownActionError` triggered (`wiki.go:214`).

### Graph layer

`internal/wiki/graph_index.go:38` `GraphIndex`
- `Neighbors(slug, depth)` (`graph_index.go:181`) — BFS in both directions, depth-bounded, **no hub-avoidance / no degree cap / no token budget**.
- `OutNeighbors` / `InNeighbors` / `Degree` / `HasNode` / `Meta` / `NodeCount` / `ShortestPath` (`graph_index.go:226-377`).
- `TopByDegree(topK)` (`internal/wiki/godnodes.go:20`) — phase-WIKI-A US-WIKI-A01.

### Confidence-aware schema

`internal/wiki/schema.go:31` `RelatedRef{Slug, Confidence}` accepts `EXTRACTED|INFERRED|AMBIGUOUS` (`schema.go:19-24`). `filteredRelatedSlugs` (`internal/storage/search/graph_documents.go:72`) drops AMBIGUOUS by default at index time. **AMBIGUOUS edges are excluded from the index, but INFERRED edges are included WITHOUT downweight** — the comment `graph_documents.go:71` says "semantically downweighted but real", but no rerank multiplier is applied.

### Ingest pipeline (the source-of-the-bug)

`internal/storage/sources/ingest/pipeline.go:42` `Pipeline.Compile`
- When OCR or extract completes, writes ONE compact summary page anchored on `source.Source` ID (e.g. `source-src-...`).
- Optional `Extractor` (`pipeline.go:64`) may expand into multi-page graph patches (entity/concept pages with cross-links), **but only when Extractor is non-nil**.
- For an xlsx ingested via markitdown sidecar today: the row content lives in `wiki/raw/<src>/extract.md` (180-row markdown table), and the wiki side gets a summary page like `Quadristi`, `Costruttori Macchine` — entity-level, no row content.
- **Critical:** there is no "tabular" specialization. Rows do not become per-entity pages with structured frontmatter (e.g. `customer_code: 12345`). The xlsx body is reachable only by `read_source(source_id=…, mode=ocr)` which falls back to `extract.md` (`source.go:98-100`, fixed 2026-05-21).

### Source readers

`source_read.go` (`read_source` tool) reads `ocr.md` first, falls back to `extract.md` (the 2026-05-21 Bug 1 fix), then to kind-specific originals. Returns up to 8000 chars truncated by `truncateForToolContext` (`source.go:30`).

---

## Gaps against graphify-style retrieval

| # | Graphify pattern | Aura status | File / line | Notes |
|---|---|---|---|---|
| 1 | Token-budgeted subgraph render: seed → BFS → render under token budget | **Missing.** `Neighbors` returns slug list with no rendering, no budget | `internal/wiki/graph_index.go:181` | No `wiki_subgraph` tool exists |
| 2 | IDF-cached lexical scoring | Equivalent: **SQLite FTS5 BM25**, but **not wired into the wiki search path** | `internal/storage/search/sqlite.go` + `qdrant.go:342` | The fusion helper exists (`search.go:426`) but only compact memory calls it |
| 3 | Hub-avoidance during BFS (p99-degree threshold) | **Missing.** `Neighbors` includes hub pages unconditionally | `graph_index.go:181-221` | `Degree` already computed (`graph_index.go:260`) — easy to layer |
| 4 | Seed-gap-ratio cutoff (drop the tail when score gap > threshold) | **Missing.** `search_memory` returns up to `limit` regardless of score distribution | `memory_search.go:215-223` | Today: 10 noisy hits all retained even when #1 score=0.9 and #10 score=0.05 |
| 5 | Community-aware retrieval (Leiden / modularity) | **Missing.** No clustering layer | nowhere | `gonum/graph/community` ships Leiden — Phase-WIKI-B candidate |
| 6 | Tabular → graph rows ingest (CSV/xlsx → per-row entity pages) | **Missing.** xlsx becomes one summary page; rows live only in `extract.md` | `internal/storage/sources/ingest/pipeline.go:42`, `extractor.go` | Live bug 2026-05-21 |
| 7 | Confidence-aware reranking | **Schema ready, rerank missing.** RelatedRef.Confidence exists, AMBIGUOUS is filtered at index time only | `schema.go:31-34`, `graph_documents.go:72` | INFERRED should downweight at rank time, not just at filter time |
| 8 | Path-finding cited in answers (showing why an edge connects A→B) | **Partial.** `ShortestPath` exists but no tool surfaces it | `graph_index.go:313`, `godnodes.go:68` `WikiPath` | No `wiki_path` LLM tool |

---

## Gaps against 2026 best practices

(placeholder — sibling agent B owns the online survey; this section is a stub to merge once their doc lands)

Expected pointers from 2026 SOTA, mostly to confirm direction:
- HyDE (hypothetical document embeddings) — generate a synthetic answer, embed THAT, retrieve.
- Cross-encoder rerank (top-N retrieved → reranker → top-K final).
- LLM-as-reranker for the top-20 with a yes/no relevance gate.
- "Retrieve then re-search" — first hit's `related:` becomes a query expansion seed.
- Late interaction (ColBERT-style) for long-tail recall.
- Structured row search (DuckDB / SQLite directly over tabular sources, not text retrieval).

Cross-reference once `docs/wiki-retrieval-research/state-of-art-2026-05-21.md` exists.

---

## Phase-WIKI-B prd sketch (6 stories, composable, none blocks another)

### US-WIKI-B01 — `wiki_subgraph(query, depth, budget)` tool (priority 1)

**Description.** New read-only LLM tool. Given a natural-language query, the tool (a) calls `search.Searcher.Search` for top-3 seed slugs, (b) BFS-expands each seed via `GraphIndex.Neighbors` to `depth` (default 2), (c) renders the resulting subgraph under a token budget (default 2000 tokens) by greedily including highest-score nodes first, (d) returns a markdown brief: each node = title + one-line excerpt + outbound `[[links]]` list, optionally followed by inbound count. The render uses `formatMemoryResults`'s line shape so the LLM sees a familiar structure.

The point: replace the current "search_memory → file/read → file/read → file/read" loop the LLM does today with a single tool call that gives it ALL connected context for one query. Today's 7-call loop becomes 2-3 calls.

**Files touched.** `internal/agent/tools/registry/wiki_subgraph.go` (new), registry registration in `internal/agent/tools/registry.go`, optionally a small helper in `internal/wiki/graph_index.go` for "neighbors with degree" output.

**LOC estimate.** ~300-400.
**Dependencies.** None (uses existing GraphIndex + Searcher).
**Priority.** 1 (highest impact, unblocks downstream stories that need a subgraph primitive).

---

### US-WIKI-B02 — Hybrid FTS+vector fusion on the wiki search path (priority 1)

**Description.** Wire `mergeHybridResults` into `qdrantRepository.Search` (`internal/storage/search/qdrant.go:96`). Today the call is vector-only — the SQLite FTS5 mirror is populated at index time but never queried at search time. Add a parallel call to `sqliteSearcher.search(query, topK)` (already implemented for tests), fuse the two result sets via the existing RRF helper (k=60, weights 0.6 FTS / 0.8 vector). Populate `ScoreExact/ScoreFTS/ScoreVector` so the existing `formatMemoryResults` line `[exact=… fts=… vector=…]` becomes meaningful for wiki hits, not just compact memory hits.

The point: the 2026-05-21 test's "Delta Automazioni" query should hit the exact-string row in the source's extract.md, but vector-only retrieval pulled back a noisy 2-day-old test page first. FTS5 BM25 would rank exact substring matches above semantic neighbours, fixing the false-positive problem at the root.

**Files touched.** `internal/storage/search/qdrant.go` (wire FTS), small refactor in `internal/storage/search/sqlite.go` to expose `search(query, topK)` outside the package, fixture updates in `internal/storage/search/qdrant_test.go`.

**LOC estimate.** ~150-250.
**Dependencies.** None — both sides exist.
**Priority.** 1 (cheap, immediate noise reduction).

---

### US-WIKI-B03 — Tabular ingest v2: xlsx/csv rows → per-row wiki pages (priority 2)

**Description.** Extend `internal/storage/sources/ingest/extractor.go` so that when a source's MIME is xlsx/csv/tsv, after markitdown produces `extract.md`, the pipeline (a) parses the markdown table back into rows (header row → column keys), (b) for each row emits a per-row wiki page with structured frontmatter (`type: tabular_row`, `source_id: src_xxx`, `row_index: N`, `customer_code: ...`, all column values as frontmatter fields), and (c) emits an index page that wiki-links every row page. The body of each row page is one paragraph summarizing the row plus an `^[src_xxx]` provenance marker.

Bound the work: skip if the table has >500 rows (configurable cap), emit a warning instead. For sheets with no header row, treat first column as the slug seed.

The point: the live 2026-05-21 bug. The customer code "Delta Automazioni 12345" should be retrievable in one `search_memory` call — today it requires `read_source` after a noisy search.

**Files touched.** `internal/storage/sources/ingest/extractor.go`, new `internal/storage/sources/ingest/tabular.go`, `pipeline.go:Compile` to dispatch on MIME, schema additions in `internal/wiki/schema.go` for the `tabular_row` category.

**LOC estimate.** ~450-500.
**Dependencies.** None (independent vertical).
**Priority.** 2 (high impact but bigger surface — riskier than B01/B02; can ship after them).

---

### US-WIKI-B04 — Confidence-aware reranking in search_memory (priority 2)

**Description.** Today `RelatedRef.Confidence` (`internal/wiki/schema.go:31`) supports `EXTRACTED|INFERRED|AMBIGUOUS`, but the score path doesn't use it. AMBIGUOUS is filtered at index time (`graph_documents.go:72`). INFERRED is included unchanged. Add a per-result multiplier to `relevanceTimesRecencyWithHalfLife` (`memory_search.go:401`) — or a new `relevanceTimesRecencyTimesConfidence` — that downweights INFERRED hits by ~0.7 and EXTRACTED hits by 1.0. Source: the page's own outbound `related:` confidence average? Or the inbound edges that brought the hit into the result set? Pick the latter for cleaner semantics: a page reached via an INFERRED `related:` edge from its retrieval seed gets the multiplier.

The point: lets the writer side (wiki_page tool) and the ingest pipeline express uncertainty without manual filtering at retrieval time.

**Files touched.** `internal/agent/tools/registry/memory_search.go` (score function), `internal/storage/search/qdrant.go` (carry RelatedRef.Confidence into the Qdrant payload — today only the slug strings make it).

**LOC estimate.** ~200.
**Dependencies.** Soft — easier after B02 ships because the rerank wants the fused score, not the raw cosine.
**Priority.** 2.

---

### US-WIKI-B05 — Hub-avoidance + seed-gap-ratio cutoff (priority 2)

**Description.** Two small ranking guards. (a) `GraphIndex.Neighbors` gains an optional `MaxDegreeRatio` parameter — drop any candidate whose `Degree()` exceeds the p99 of all node degrees (call `TopByDegree(nodes)`, take the 99th percentile, cache for 60s). Prevents `[[davide]]` and other god-nodes from polluting every traversal. (b) In `search_memory.Execute` after sorting (`memory_search.go:215-223`), find the largest score-gap in the result list; if `score[i+1] / score[i] < 0.4`, truncate at i+1. Keeps the high-score head, drops the tail of marginal hits.

The point: today's "Delta Automazioni" returns 10 hits because `limit=10` and the ranker has no signal to stop earlier; (b) makes the limit upper-bound only.

**Files touched.** `internal/wiki/graph_index.go` (add `NeighborsExcludingHubs`), `internal/agent/tools/registry/memory_search.go` (gap cutoff).

**LOC estimate.** ~150.
**Dependencies.** None.
**Priority.** 2.

---

### US-WIKI-B06 — Community detection (Leiden) + cluster-aware retrieval (priority 3)

**Description.** Build a community structure over the wiki graph using `gonum.org/v1/gonum/graph/community` (Leiden algorithm). Recompute lazily — once at boot, once after every N writes (N=20), or on demand via a maintenance handler. Store cluster IDs on `NodeMeta` (`graph_index.go:30`) and surface in the Qdrant payload. In `search_memory`, when more than 60% of top-K results share a cluster ID, boost that cluster's other members by +5% relevance — surfaces "siblings" the user is implicitly asking about.

The point: graphify ships this; for big wikis (hundreds of pages) clustering naturally separates "personal/work/inbox" so a query about one cluster doesn't drag in noise from another. Aura has ~100-300 pages today — already enough to benefit.

**Files touched.** `internal/wiki/communities.go` (new), `internal/wiki/graph_index.go` (cluster field on NodeMeta), `internal/storage/search/graph_documents.go` (carry cluster ID), `internal/agent/tools/registry/memory_search.go` (cluster boost).

**LOC estimate.** ~450.
**Dependencies.** None at the code level. Depends on `gonum/graph/community` (one import).
**Priority.** 3 (nice-to-have; benefit grows with wiki size; sticker-shock LOC).

---

## Quick wins to ship NOW (pre-Phase-WIKI-B, sub-30 LOC each, mergeable today)

### QW-1 — `wiki_page action=read` alias to `file action=read` (smallest, ~10 LOC)

**Why.** Today's live test: the LLM tried `wiki_page action=read` and got `UnknownActionError`. The fix is one of:
- Add `read` to `wikiValidActions` (`internal/agent/tools/registry/wiki.go:219`) and dispatch to a thin helper that calls `t.store.ReadPage(slug)` and returns the body + frontmatter as JSON.
- Or: in `UnknownActionError`'s hint message, explicitly say "to READ a page, use `file action=read` or `search_memory`". Today the error just lists valid actions without a redirect.

**Recommended:** the alias. It is the principle-of-least-surprise — the LLM's expectation is correct ("a wiki_page tool should let me read a page"), the tool just doesn't expose it.

**Smallest single change:** add a `read` action to the `oneOf` list (`wiki.go:188-193`) and a 5-line `doRead` handler that returns `json.Marshal(existing)`.

### QW-2 — Tool description hint update (~5 LOC)

In `wiki_page` `Description()` (`internal/agent/tools/registry/wiki.go:71`), add at the top: `"To READ a page, use file action=read (slug.md path) or search_memory(query=slug). This tool only WRITES."`. Surfaces the gap loudly in the system-prompt hint that today is buried at lines 80-86.

### QW-3 — Prune stale test wiki pages from the index (1 line + a config flag)

The 2026-05-21 noise: a 2-day-old `test-xxx` page outranked the Delta page. Two options:
- Add a `IsTestSlug(slug)` helper alongside `wiki.IsOperationalSlug` (`internal/wiki/operational_slugs.go` likely) that matches `test-*` / `qa-*` / `probe-*`, and skip those slugs in `loadWikiDocuments` (`search.go:266`) when an env-flag `WIKI_INDEX_EXCLUDE_TEST=true` is set.
- Or: hard-rm the offending pages from the wiki directory now (manual, no code).

**Smallest:** add 5-line `IsTestSlug` + add `if IsTestSlug(slug) { continue }` at `search.go:292` next to the existing `IsOperationalSlug` check.

### QW-4 — `search_memory` snippet should center on the FIRST match (~3 LOC)

`snippetAround` (`memory_search.go:650`) already does this via `findQueryOffset`, but when the query has multiple terms ("Delta Automazioni cliente codice") it only locks onto the first term. Loop through `queryTerms` and pick the offset with the most term matches in the surrounding ±limit window — gives a more informative snippet. ~3-10 LOC.

---

## Sequencing recommendation

1. **Day 0 (now, 30 min):** QW-1 + QW-2 + QW-3. These are sub-30 LOC each, no dependencies, removes the most acute observed pain points from the 2026-05-21 test.
2. **Week 1:** US-WIKI-B02 (hybrid FTS+vector fusion) — biggest noise reduction per LOC.
3. **Week 1:** US-WIKI-B01 (`wiki_subgraph` tool) — gives the LLM a graph primitive it doesn't have today.
4. **Week 2:** US-WIKI-B03 (tabular ingest) — fixes the structural xlsx bug at the source.
5. **Week 2+:** B04 (confidence rerank), B05 (hub-avoid + gap cutoff). Both ride on B02's fused score being useful.
6. **Backlog:** B06 (Leiden). Revisit once wiki >500 pages.

---

## Open questions for the human

1. The compact-memory index (`memoryindex`) already uses hybrid RRF (`memory_search.go:191`); shipping B02 means the wiki path catches up. Are there cases where vector-only is actually preferable for wiki (e.g. when the user query is conceptual, not keyword)? If yes, gate B02 behind a per-call flag — but vector-only is unlikely to lose to fused given the RRF weights already favor vector (0.8 > 0.6 FTS).
2. B03 (tabular ingest) explodes the page count — a 200-row xlsx becomes 201 pages. The graph index is in-RAM (`GraphIndex`) and the SQLite FTS5 mirror is on disk; both can absorb a 10× page count, but the embedding cost at index time scales linearly. Worth pre-confirming the operator wants that tradeoff before B03 ships.
3. The `qdrantRepository` already carries `ScoreExact/ScoreFTS/ScoreVector` fields in `Result` (`search.go:96-100`) but nobody populates them for wiki hits today. B02 lights them up. The `formatMemoryResults` line shape already prints them when non-zero (`memory_search.go:494-496`) — no formatting change needed.
