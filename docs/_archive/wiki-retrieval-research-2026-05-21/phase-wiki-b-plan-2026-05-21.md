# Phase-WIKI-B — Wiki bedrock plan (2026-05-21)

**Strategic re-prioritization 2026-05-21 by user**: the wiki ingest + retrieval substrate is the bedrock; every staged phase (Phase-ONB, Phase-RAG, Phase-KV, Phase-MCP-UI) is conditional on it. If Aura cannot reliably ingest a real complex file and answer a targeted question about it in ≤5s with ≤3 tool calls, every higher-layer feature is built on sand. This document consolidates the cross-agent research (Agents A/B/C/D, 2026-05-21) into a single executable plan.

## Executive summary

**Problem observed live 2026-05-21** — query *"dammi il codice cliente di delta automazioni"* against a freshly-ingested xlsx (180-row customer list):

- **Pre-fix** (Bug 1+2 not yet shipped): 105s, 22 LLM calls, 24 tool calls, **EMPTY reply**, budget exhaustion.
- **Post-fix** (Bug 1+2 shipped): 30s, 6 LLM calls, 7 tool calls, **correct reply** ("615827"). −72% latency.

The fix unblocked the case but the agent still navigated through 7 tools: 2× `search_memory` (one too broad, one good), 1× wrong-file `file/read`, 1× invalid `wiki_page action=read`, 1× narrower `search_memory`, 1× `read_source` (success), then answer. **The substrate is still wrong** — the data is in `wiki/raw/<src>/extract.md` (180-row markdown table) and the auto-generated wiki page is a category-level summary that drops every customer code + email.

**Goal of Phase-WIKI-B**: the same query against the same file becomes **1 tool call, ≤5s, correct reply**, and the pattern generalizes to every complex file type (xlsx with multiple sheets, docx with nested lists + images, pdf scientific paper, pptx deck with diagrams, csv multi-encoded, json deep-nested, html DOM, zip multi-doc, audio with dialect, url, image with OCR).

## The four converging layers (cross-agent synthesis)

Four independent research streams converged on a **vertically composed solution** — each layer fixes a different failure mode observed today, and they compound:

| Layer | What it fixes | Lift source | Story |
|---|---|---|---|
| **L1 — Ingest** | xlsx rows survive as chunkable retrieval units, not buried in a 180-row table | Agent D (markitdown plugin) + Agent C (tabular ingest v2) | B03 + B08 |
| **L2 — Index** | "Delta Automazioni" search returns the right page first, not 10 noisy hits including stale test pages | Agent C (hybrid fusion wiring) | B02 |
| **L3 — Retrieval** | 1 tool call returns seed + neighbors + key facts in token budget, instead of 7-call navigation | Agent A (graphify serve.py) + Agent B (PPR seeding 2026 SOA) | B01 |
| **L4 — Quality** | "Delta Automazioni" is one node, not splattered across 3 variants from MinHash silence | Agent A (graphify dedup.py) + Agent B (Confidence-aware rerank) | B05 |

Per-layer brief:

### L1 — Ingest layer (markitdown plugin + tabular ingest v2)

Agent D's verdict was clear: write a **custom Aura xlsx converter as a markitdown plugin** (`MARKITDOWN_ENABLE_PLUGINS=true` is already plumbed in Aura's sidecar). One Python file replaces the default `XlsxConverter` (`_xlsx_converter.py:83-93`), producing:

- YAML frontmatter (workbook_title, author, sheets, row_counts)
- Per-sheet `##` heading
- **One `###` per row** with `**header**: value` bullets

Then Aura's LLM-ingest sees a structurally-decomposed extract.md and produces **per-row sub-pages** linked from the source-summary page. "Delta Automazioni 615827" is one vector hit away.

Plugin wins over post-processing because the structure is extracted at the only point with **full fidelity** (openpyxl object model — formulas, dimensions, properties survive). Post-processing the table HTML re-derives structure from a lossy projection.

The pattern **generalizes** to docx (`###` per heading), pptx (`###` per slide), epub (per chapter), html (per `<section>`), csv (per row when small, per "logical group" when large). Each format gets a tiny custom converter; the ingest pipeline downstream is uniform.

### L2 — Index layer (hybrid fusion wiring)

Agent C's big find: `qdrantRepository.Search` ([internal/storage/search/qdrant.go:96](internal/storage/search/qdrant.go#L96)) is **vector-only today**. The reciprocal-rank fusion code (`mergeHybridResults`) **exists** with `ScoreExact + ScoreFTS + ScoreVector` channels but is called only by compact-memory. The `search.go` package comment lies — claims hybrid, ships vector-only on the wiki path.

**This is the root cause of the noisy "Delta Automazioni → 10 hits including a 2-day-old test page" result.** Embedding-only similarity returns semantic neighbors regardless of whether the literal token "Delta" appears. Adding FTS5 BM25 + exact lexical into the existing fusion is **~150-250 LOC of wiring**, with the algorithm already written and tested.

### L3 — Retrieval layer (wiki_subgraph + PPR + heading subnodes)

Agent A extracted graphify's `serve.py` query algorithm: 8-step pipeline (tokenize → IDF-cached lexical score → seed pick with gap-ratio cutoff → context-filter inference → hub-aware BFS → token-budgeted subgraph text rendering).

Agent B's 2026 update: **Personalized PageRank (PPR) seeded on query entities** is the most-cited "must adopt" pattern of 2026 (HippoRAG 2, LightRAG dual-level, Temporal-GraphRAG). PPR over an LLM-extracted KG gives **+9.1% accuracy at −97% tokens** vs vanilla GraphRAG. For Aura's stack: PPR adds **~30 LOC** on top of the existing `GraphIndex.Degree + Neighbors` primitives.

Combined: a new tool `wiki_subgraph(query, depth, budget)` that:

1. Hybrid-search picks initial seed candidates (uses L2)
2. PPR over the wiki graph re-ranks seeds (entity-anchored)
3. BFS expands seeds with hub-avoidance + heading-aware traversal (uses L4 subnodes from B04)
4. Token-budgeted text render: seed nodes first, then degree-sorted neighbors, edge labels with confidence (uses Phase-WIKI-A US-WIKI-A03 confidence)

Replaces today's 7-call read loop with **1 tool call** for the common case.

### L4 — Quality layer (entity dedup + confidence-aware rerank)

Agent A's dedup pipeline from graphify (`dedup.py`, ~250 LOC Go port): exact-norm → entropy gate (2.5 bits/char) → MinHash/LSH (128 perms, threshold 0.7) → Jaro-Winkler (threshold 92) → community boost → union-find. Catches "Delta Automazioni" vs "delta automazioni srl" vs "DELTA AUTOMAZIONI SRL" silently merging into one canonical node.

Agent B + Agent C: Aura's Phase-WIKI-A US-WIKI-A03 already stores `confidence: EXTRACTED|INFERRED|AMBIGUOUS` on `related:` edges. **Aura doesn't yet rerank** search results by confidence. Simple multiplier (EXTRACTED 1.0, INFERRED 0.6, AMBIGUOUS 0.0-excluded-by-default) on the fused score = ~30 LOC + huge quality lift for free.

### Layer composition example

Future test of *"dammi codice cliente delta automazioni"*:

1. L2-fused search: hybrid FTS+vec+exact → top seed = **`delta-automazioni`** sub-node (created at ingest by L1 markitdown plugin, with structured `customer_code: 615827` + `emails: [...]` frontmatter, deduplicated by L4 across DELTA/delta/Delta variants)
2. L3 wiki_subgraph(query, depth=2): PPR boost on `delta-automazioni` → BFS expands to source-ruocci-email + parent quadristi category + emails as edge labels
3. **Single text blob returned** to LLM in ~2KB: `## delta-automazioni\ncustomer_code: 615827\nemails: [3 mails]\nparent: source-ruocci-email\ncategory: quadristi\narea: bui-area-pie-est`
4. LLM answers in 1 turn

Estimated: **1 tool call, ≤3s, correct reply**. From 22 turns/105s/empty → 1 turn/3s/correct = **35× faster + 100% accurate**.

## Roadmap re-priorization

```text
[CRITICAL — NOW]     Phase-WIKI-B  ← BLOCKER for everything else staged
                       ├── Wave 0   quick-wins (sub-30 LOC, ship today)
                       ├── Wave A   retrieval engine (B01+B02+B04)
                       ├── Wave B   ingest robustness across formats (B03+B08 markitdown plugins)
                       └── Wave C   quality (B05 entity dedup) + advanced (B07 lazy summarization)
[DEFERRED]           Phase-ONB     — interview-driven onboarding (needs solid wiki to prove)
[DEFERRED]           Phase-RAG     — eval harness (needs reliable retrieval to measure)
[DEFERRED]           Phase-KV      — prompt cache (needs stable prefix from solid wiki)
[DEFERRED]           Phase-MCP-UI  — dashboard config framework
[DEFERRED]           MCP Roundup   — bulk MCP integration
[DEFERRED]           Phase-U       — plugin layout
```

The rule: **no staged phase ships until Phase-WIKI-B's test matrix is 100% green on real complex fixtures.**

## Test matrix — real complex fixtures

Each row: a file type × a real complex fixture × 3 assertion levels. **No 5-row smoke fixtures** — these are real-world inputs Aura must handle.

| Format | Complex fixture (real or to-create) | Ingest assertion | Wiki assertion | Retrieval assertion |
|---|---|---|---|---|
| **xlsx** | `ruocci email.xlsx` (180 rows × 9 cols, customer data, Italian, NaN values, 3 emails/customer) | extract.md preserves all 180 rows + columns | wiki page per row OR per-customer subnode; key fields (`customer_code`, `emails[]`) in frontmatter | "codice cliente delta automazioni" → 615827 in ≤5s, 1-3 tool calls |
| **xlsx-multi** | TBD — need a multi-sheet xlsx (e.g. budget with sheets per quarter or contacts split by region) | extract.md has per-sheet section + frontmatter listing sheets | wiki page per sheet OR sheet-prefixed subnodes | "totale Q3 budget" or "contatti regione X" → correct cell |
| **docx** | TBD — need a docx with nested lists, headings, tables, embedded images (e.g. project specification, contract) | extract.md preserves heading hierarchy + table rows + image alt-text/captions | wiki page with sections matching headings; images extracted as separate sources | "elenca le sezioni del documento" + "trova clausola X" both work |
| **pdf** | `docs/2602.02276v1.pdf` (existing) | OCR markdown captures all text + tables; figure captions present | wiki page with paper sections + abstract; references as separate entities | "qual è la conclusione" + "cita il riferimento 3" |
| **pptx** | TBD — need real deck with diagrams + speaker notes + multiple slides | extract.md has per-slide section + speaker notes section; alt-text for shapes | wiki page per slide OR per topic-group; relationships preserved | "cosa dice la slide su X" → exact content |
| **csv** | TBD — multi-encoding (UTF-8 BOM + UTF-16) + mixed types + 1000+ rows | encoding detection correct; all rows preserved; column types inferred | wiki page per logical group (size-adaptive: row-as-page if <500 rows, group-as-page otherwise) | "trova entry con campo X=Y" |
| **json** | TBD — deeply nested API response or config (e.g. 5-level nesting + arrays) | JSON tree preserved; arrays unrolled to lists | wiki page with sectioned structure following JSON tree | "valore di config.X.Y.Z" |
| **html** | TBD — real web page snapshot (e.g. Wikipedia article or product spec) | DOM structure → heading hierarchy; links preserved; tables intact | wiki page following section structure | "trova il paragrafo su X" |
| **zip** | TBD — multi-doc zip (e.g. project archive with docx + xlsx + pdf inside) | per-file extraction; container manifest in frontmatter | one wiki page per contained file + index page | "cosa contiene l'archivio" + per-doc queries |
| **epub** | TBD — real ebook (1 chapter + frontmatter + TOC) | per-chapter section; TOC + author metadata in frontmatter | wiki page per chapter | "riassumi capitolo 3" |
| **txt/md** | TBD — long markdown doc (e.g. specification, runbook) | content preserved verbatim; heading structure detected | wiki page with subnodes per heading (B04) | "trova sezione su X" |
| **audio** | Davide's real voice memo (any duration ≤2min) | transcript text correct; speaker turns if multi-voice | wiki page with transcript + timestamps; topics extracted | "cosa ho detto su X" |
| **image** | TBD — invoice scan or screenshot with text | OCR text correct; layout preserved (top-down reading order) | wiki page with extracted text + metadata (image dims, EXIF) | "qual è il totale dell'invoice" |
| **url** | TBD — real URL with rich content (article + comments + images) | page content extracted; comments included or filtered; canonical URL preserved | wiki page with article body + metadata + permalink | "trova X dall'articolo Y" |

## Fixture pool — public complex files (sourced 2026-05-21)

The xlsx fixture is already in hand (`ruocci email.xlsx`, ingested 2026-05-21). For the other formats, here's the curated fixture pool to download into `runtime-workspace/data/test-fixtures/` (or similar). All public-domain / open-license, no privacy concerns. Most have Italian content when possible (Aura is IT-primary).

| Format | Fixture | URL | Complexity rationale |
|---|---|---|---|
| **xlsx** | `ruocci email.xlsx` (already have) | local — `D:/tmp/ruocci email.xlsx` | 180 rows × 9 cols, customer data, real italian content, NaN values |
| **xlsx-multi** | ISTAT multi-sheet dataset | [istat.it open data](https://www.istat.it/en/analysis-and-products/open-data-in-istat) — pick any multi-sheet Excel (e.g. demographic + economic tabs) | Multi-sheet, Italian column headers, real economic/demographic data |
| **xlsx-tika** | Apache Tika `protectedSheets.xlsx`, `multiSheets.xlsx` | [Apache Tika test-documents](https://github.com/apache/tika/tree/master/tika-parsers/src/test/resources/test-documents) | Edge cases (protected, multi-sheet) — canonical pool |
| **docx** | Apache Tika `footnotes.docx` + complex sample | [Apache Tika `footnotes.docx`](https://github.com/apache/tika/blob/master/tika-parsers/src/test/resources/test-documents/footnotes.docx) + browse same dir for nested-list / multi-table | Footnotes + nested lists are classical conversion edge cases |
| **docx-italian** | Wikipedia article export (any Italian Wikipedia article → "Download as PDF/DOCX") | [it.wikipedia.org](https://it.wikipedia.org) — pick a featured article like "Storia di Roma" or "Sistema solare" | Real Italian prose + tables + nested headings + references |
| **pdf** | `docs/2602.02276v1.pdf` (already in tree) | local — `D:/Aura/docs/2602.02276v1.pdf` | Already curated by Davide, multi-page paper |
| **pdf-tika** | Apache Tika `testPDF*.pdf` battery | [Apache Tika test-documents](https://github.com/apache/tika/tree/master/tika-parsers/src/test/resources/test-documents) — `testPDF.pdf`, `testPDF_protected.pdf`, `testPDFTwoTextBoxes.pdf` | Encrypted, multi-column, embedded fonts — Tika's hard cases |
| **pdf-italian** | arXiv Italian-content paper [1902.03287](https://arxiv.org/abs/1902.03287) "Open data to evaluate Italian academic researchers" | [arxiv.org/pdf/1902.03287](https://arxiv.org/pdf/1902.03287) | Italian context + tables + multi-page scientific |
| **pptx** | Apache Tika `pictures.pptx` + complex sample | [Apache Tika test-documents](https://github.com/apache/tika/tree/master/tika-parsers/src/test/resources/test-documents) | Embedded images + multiple slides — exercises image extraction + slide structure |
| **pptx-public** | Public business deck (e.g. Italian gov annual report PDF→PPT) | [opencoesione.gov.it](https://opencoesione.gov.it/en/opendata/) browse for presentations | Real-world deck with diagrams + tables |
| **csv** | ISTAT demographic data | [istat.it open-data](https://www.istat.it/en/analysis-and-products/open-data-in-istat) — pick a large CSV (regional population is good) | Italian content, multi-column, encoding edge cases |
| **csv-large** | Apache Tika `complex.csv` | [Apache Tika test-documents](https://github.com/apache/tika/tree/master/tika-parsers/src/test/resources/test-documents) | Edge cases for quoting + escapes + encoding |
| **json** | dati.gov.it API response sample | [dati.gov.it](https://www.dati.gov.it/view-dataset) — pick a JSON dataset (e.g. comuni Italiani) | Deeply nested, real Italian gov data |
| **json-deep** | npm-style `package-lock.json` from a big repo | [package-lock.json from React](https://github.com/facebook/react/blob/main/package-lock.json) | 5+ level nesting, thousands of entries |
| **html** | Italian Wikipedia article (download as HTML) | [it.wikipedia.org/wiki/Roma](https://it.wikipedia.org/wiki/Roma) or any featured page | Real DOM with tables, infoboxes, references, links — heaviest realistic HTML |
| **html-rich** | Bootstrap docs page or MDN article | [developer.mozilla.org](https://developer.mozilla.org) any complex docs page | Multi-section + code blocks + nav |
| **zip** | Apache Tika `test-documents.zip` (multi-format) | [Apache Tika test-documents](https://github.com/apache/tika/tree/master/tika-parsers/src/test/resources/test-documents) | Container of docs in multiple formats |
| **epub** | Italian Project Gutenberg classic | [gutenberg.org bookshelf 127 (Italian)](https://www.gutenberg.org/ebooks/bookshelf/127) — pick a real novel (e.g. Manzoni "I Promessi Sposi") | Real Italian prose, TOC, chapters, multi-MB |
| **txt/md** | Aura's own `CLAUDE.md` or `prd.md` | local — already in tree | Real, complex, multi-section markdown |
| **audio** | Mozilla Common Voice Italian sample | [commonvoice.mozilla.org/datasets](https://commonvoice.mozilla.org/en/datasets) — Italian CV release | CC0 license, real Italian voices, varying accents |
| **audio-davide** | One real voice memo from Davide (already in Telegram archive) | local | Real-world latency baseline |
| **image** | Apache Tika `testJPEG_EXIF.jpg` + Italian invoice scan | [Apache Tika test-documents](https://github.com/apache/tika/tree/master/tika-parsers/src/test/resources/test-documents) for EXIF; Italian invoice from public PA documents (e.g. fatture esempio FE2.1) | OCR + EXIF + Italian text in image |
| **url** | A live page Aura fetches (article + comments) | choice at probe time — e.g. an Il Sole 24 Ore article | Real web page with paywall handling + real-time freshness |

**Action for Phase-WIKI-B Wave B**: write a fetch script (`scripts/fetch-test-fixtures.sh`) that pulls all of these into `runtime-workspace/data/test-fixtures/` with SHA-256-pinning per the existing aura-init-models pattern. SHA-pinned because (a) reproducibility, (b) version drift detection if upstream silently changes.

## Phase-WIKI-B story plan

### Wave 0 — Quick-wins ship today (sub-30 LOC each)

| ID | Title | LOC | Files | Insight source |
|---|---|---|---|---|
| QW-1 | `wiki_page action=read` alias → routes to `file/read` | 10 | wiki.go | Agent C |
| QW-2 | Tool-description clarification on `wiki_page` (readers go through file/source) | 5 | wiki.go description string | Agent C |
| QW-3 | Prune stale test wiki pages (`prova-wiki-*`) from runtime-workspace | shell | runtime-workspace/wiki/*.md | Agent C live observation |
| QW-4 | Fix `search.go:3-5` aspirational comment that lies about hybrid fusion | 3 | search.go | Agent C |

Total: ~20 LOC + 1 cleanup. Single chore commit. Ship in 15 min.

### Wave A — Retrieval engine (~700 LOC, single Ralph session)

| ID | Title | LOC | Depends on | Files |
|---|---|---|---|---|
| **B02** | Wire hybrid fusion (FTS+vec+exact RRF) on wiki search path | 150-250 | — | qdrant.go, search.go |
| **B04** | Heading-level subnodes (H2/H3 → sub-nodes with parent_slug + heading_path) | 150 | — | wiki/index.go, wiki/parser.go |
| **B01** | `wiki_subgraph(query, depth, budget)` tool with PPR seeding + hub-aware BFS + token-budgeted render | 350-400 | B02 + B04 | new tools/registry/wiki_subgraph.go, new wiki/ppr.go, new wiki/subgraph_render.go |

**Acceptance**: probe `wiki_subgraph("dammi codice cliente delta automazioni", 2, 2000)` returns a text blob containing `615827` + `delta automazioni` + the 3 emails, in ≤500ms, against the existing ingested fixture.

### Wave B — Ingest robustness (~900 LOC, single Ralph session)

| ID | Title | LOC | Files |
|---|---|---|---|
| **B03** | Aura xlsx markitdown plugin — Python `markitdown.plugin` entry-point + per-row `###` decomposition + YAML frontmatter (workbook_title, author, sheets[], row_counts) | 200 (Python) + 50 Go wire | docker/markitdown/plugins/aura_xlsx.py, markitdown/client.go |
| **B08** | Markitdown plugins for docx (per-heading), pptx (per-slide with speaker notes), epub (per-chapter), html (per-section) — 4 plugins same shape as B03 | 600 (Python) | docker/markitdown/plugins/aura_{docx,pptx,epub,html}.py |
| **B09** | Ingest LLM-prompt v3 — materializes per-row / per-section frontmatter into wiki sub-pages instead of summary-only | 100 (prompt + 50 LOC Go) | internal/storage/sources/ingest/prompt.go |

**Acceptance**: full test matrix above passes the 3 assertions per format on real complex fixtures.

### Wave C — Quality + advanced (defer until Wave A+B stable)

| ID | Title | LOC | Files |
|---|---|---|---|
| **B05** | Entity dedup pipeline — MinHash/LSH + Jaro-Winkler + union-find on wiki nodes | 250 | new wiki/dedup.go, wiki/index.go integration |
| **B07** | Lazy query-time summarization (LazyGraphRAG pattern) — generate community summaries ON DEMAND at query, never at ingest | 200 | new wiki/lazy_summary.go |
| **B10** | Confidence-aware rerank — multiplier on fused score (EXTRACTED 1.0, INFERRED 0.6, AMBIGUOUS excluded by default) | 30 | qdrant.go integration with mergeHybridResults |
| **B12** | Cross-encoder reranker sidecar — `gte-multilingual-reranker-base-GGUF` (306M, Apache-2.0, 70+ languages including Italian, runs on llama.cpp via `llama-server`). Acts as a 3rd stage AFTER B02 hybrid fusion: top-K=20 from RRF → rerank-then-keep top-K=5 with cross-encoder scores. Sidecar shape: `docker/aura-reranker/Dockerfile` mirrors `aura-llama-embed` (llama.cpp HTTP server, Q4_K_M=235 MB GGUF), HTTP API takes `pairs: [[query, doc], ...]` returns `scores: [...]`. Aura side: new `internal/llm/reranker/client.go` + integration in `wiki_subgraph` from B01. Italian-quality boost is significant for Aura's primary language. | 100 Go + 30 Docker | new docker/aura-reranker/, new internal/llm/reranker/, B01 integration |

**Acceptance**: same test matrix as Wave B + additional assertions on duplicate-handling + community-summary-on-demand latency.

## What we explicitly skip (rejected lifts)

| Lift | Reason |
|---|---|
| ~~Leiden community detection at ingest~~ | Agent B veto: LazyGraphRAG (Microsoft) proved 96/96 head-to-heads at 0.1% indexing cost. Leiden non-reproducible on Aura's low-degree wiki (60 nodes). Use lazy query-time clustering instead (B07). |
| ~~Tree-sitter code extraction~~ | Aura's wiki is doc-only; not a code-walker. |
| ~~Neo4j export~~ | Aura's graph fits in memory; no need for a graph DB. |
| ~~Obsidian vault sync~~ | Wiki already IS Obsidian-readable. |
| ~~Full GraphRAG community summaries~~ | See Leiden veto. |
| ~~Cohere/Voyage rerank API~~ | External API, breaks self-hosted invariant. Use confidence-aware local rerank (B10). |

## Open questions

1. **Fixture pool** — per-format complex real files. See "Fixture sourcing decision needed" above.
2. **Per-row page granularity for xlsx** — 180 rows × 1 page each = 180 wiki pages. Aura wiki has ~60 pages today; jumping to 240+ in one ingest is fine architecturally but the graph index needs to scale. Verify GraphIndex performance at 1000+ nodes (likely fine but un-measured).
3. **Plugin distribution** — Aura ships markitdown plugins where? Bundled in the sidecar image? Mounted volume? Decision affects `docker/markitdown/Dockerfile`.
4. **PPR damping factor + iteration count** — start with α=0.15, 30 iter (standard); tune via Phase-RAG eval later.
5. **Backwards-compat on already-ingested sources** — when Wave B ships, do we re-ingest the existing `ruocci email.xlsx`? Probably yes — one-shot migration script + status flag.

## Memory updates

After Phase-WIKI-B Wave A lands:

- New: `project_phase_wiki_b_wave_a_shipped.md` — what landed + measured before/after
- Update: `project_2026-05-20_roadmap_snapshot.md` — reflect Phase-WIKI-B-first priority
- New: `feedback_wiki_is_bedrock.md` — bank the "no phase ships until wiki probes green" rule

## Sources

- `docs/wiki-retrieval-research/graphify-full-pipeline-2026-05-21.md` — Agent A
- `docs/wiki-retrieval-research/state-of-art-2026-05-21.md` — Agent B
- `docs/wiki-retrieval-research/aura-gaps-and-phase-wiki-b-sketch-2026-05-21.md` — Agent C
- `docs/wiki-retrieval-research/markitdown-best-use-2026-05-21.md` — Agent D
- [HippoRAG 2 paper](https://github.com/osu-nlp-group/hipporag) — PPR-on-KG pattern
- [LazyGraphRAG (Microsoft)](https://www.microsoft.com/en-us/research/blog/lazygraphrag-setting-a-new-standard-for-quality-and-cost/) — Don't pre-summarize
- [LightRAG](https://github.com/HKUDS/LightRAG) — Dual-level retrieval
- [graphify/serve.py](D:/tmp/graphify/graphify/serve.py) — algorithm template
- [markitdown plugin docs](https://github.com/microsoft/markitdown/tree/main/packages/markitdown-sample-plugin) — extension point
- `internal/storage/search/qdrant.go:96` — vector-only Search (the bug)
- `internal/storage/search/search.go:3-5` — aspirational comment that lies
- `internal/wiki/graph_index.go` — primitives for PPR + BFS

## Estimated total Phase-WIKI-B cost

- Wave 0: 15 min (1 chore commit, ~20 LOC)
- Wave A: 1 Ralph session (~700 LOC, 3 stories)
- Wave B: 1 Ralph session (~900 LOC, 3 stories — half Python plugins, half Go)
- Wave C: 1 Ralph session when Wave A+B stable (~480 LOC, 3 stories)
- Fixture sourcing + test matrix authoring: out-of-band, parallel with Wave A/B

**Total**: ~3 Ralph sessions + 1 short chore + fixture work. Roughly 2-3 calendar days of disciplined execution.

After this lands: every staged phase (ONB, RAG, KV, MCP-UI) unblocks with confidence — wiki is solid, every file type ingests correctly, retrieval is 1-call/3s for the common case.
</thinking>
