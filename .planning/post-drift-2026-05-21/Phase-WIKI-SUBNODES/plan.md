# Phase-WIKI-SUBNODES — Heading-level subnodes for finer retrieval

**Status:** 🟣 staged — kicks AFTER Phase-OUT (or in parallel — independent surface)
**Provenance:** Originally US-WIKI-B04 in `scripts/ralph/prd-phase-wiki-b-wave-a-staged.json` (Phase-WIKI-B Wave A). Re-scoped 2026-05-22 after user decision: Wave A B02 fusion already shipped by Phase-WIKI-FIX FIX-01; Wave A B01 (`wiki_subgraph` new tool) cancelled per "no new tools" direction. B04 remains valuable as a standalone 1-session story.
**Estimated effort:** ~1 session, 1 atomic story
**LOC delta:** ~+250

---

## Why this phase

After Phase-WIKI-FIX restored the hybrid fusion, wiki retrieval is healthy at the PAGE level: FTS hit 20/20, top-1 plausible 17-18/20, latency p95 75ms. But pages remain INDIVISIBLE — when a search matches a 3000-token page with the answer in `## Extracted Entities` section line 47, the LLM either reads the whole page (token waste) or reads nothing (token starvation).

Graphify's `extract.py:4837-4944` decomposes markdown into per-heading sub-nodes (regex-only, no tree-sitter) with `parent_slug + heading_path` metadata. For Aura: each H2/H3 section becomes a sub-node addressable as `<page-slug>#<heading-anchor>`. BFS over the subnode graph returns the SECTION that mentions the query term, not the whole page.

End-state: search results are byte-range snippets pointing at a heading-anchored sub-node, with `parent_slug` allowing the LLM to fetch the full page only when needed.

---

## Story

### US-WIKI-SUBNODES-01 — Heading-level subnodes (H2/H3 → parent_slug + heading_path)

- **Scope:** Extend `internal/wiki/parser.go` (or whichever file reads page bodies — `grep -rn "ParseHeadings\|firstBodyParagraph" internal/wiki/`) to extract heading hierarchy AFTER frontmatter parsing.
  - (a) **PARSE:** regex `^(#{2,3})\s+(.+)$` for H2 and H3 only. H1 is the page title (already in frontmatter). H4+ ignored in v0. CRITICAL: skip headings inside fenced code blocks (graphify documented gotcha).
  - (b) **NODE MODEL:** extend `internal/wiki/graph_index.go` `NodeMeta` with optional `Subnodes []SubnodeMeta` where each Subnode = `{AnchorSlug string (lowercased + dash-normalized heading), HeadingLevel int (2 or 3), HeadingText string, ByteStart int, ByteEnd int}`.
  - (c) **GRAPH UPSERT:** when `LoadFromPages / upsertLocked` runs, also upsert one node per subheading with id `<page-slug>#<anchor>`, `parent_slug` pointing at the page slug. Parent edge is inferred from id syntax (no outbound `related:` edges of its own in v0).
  - (d) **BFS NEIGHBORS:** `GraphIndex.Neighbors(slug, depth)` when called on a subnode id returns: (i) the parent page slug; (ii) any subnode siblings on the parent page; (iii) the parent page's outbound `related:` targets (so a subnode inherits its page's confidence-labeled edges). When called on a page slug it returns subnodes too (depth=1 includes them).
  - (e) **RENDERING:** new `internal/wiki/subgraph_render.go` provides `SubgraphSnippet(slug)` returning the byte range from `ByteStart..ByteEnd` of the source file — used by future searches when emitting subgraph text to the LLM.
  - (f) **WIKI_DOCUMENTS FTS:** the SQLite FTS5 mirror gains a row per subnode (not just per page). Search returns subnode ids that the rendering layer translates to byte-range snippets. The system_prompt manifest exposure stays at page level; subnodes are an internal retrieval optimization.
  - NO change to wiki frontmatter schema. NO change to on-disk markdown. Subnodes are computed at index time, derived from body content; pages stay editable as plain markdown.
- **Files:** MODIFY [internal/wiki/parser.go](internal/wiki/parser.go) (export `ParseHeadings(body []byte) []HeadingMatch` helper); MODIFY [internal/wiki/graph_index.go](internal/wiki/graph_index.go) (extend NodeMeta + upsert logic); NEW [internal/wiki/subgraph_render.go](internal/wiki/subgraph_render.go); MODIFY [internal/storage/search/qdrant.go](internal/storage/search/qdrant.go) (FTS5 mirror writes subnode rows alongside page rows).
- **LOC delta:** +250 + 100 tests.
- **Acceptance:**
  - `internal/wiki/parser_test.go`: heading regex + edge cases (no headings, mixed H1/H2/H3, headings inside ``` fenced blocks must be IGNORED — the graphify gotcha).
  - `internal/wiki/graph_index_test.go`: subnode upsert + BFS neighbors traversal — assert that `Neighbors("dante-alighieri")` includes both page-level `related:` targets AND its subnodes.
  - SQLite probe: `SELECT count(*) FROM wiki_documents WHERE id LIKE '%#%'` returns the subnode count (>page count after this story).
  - End-to-end: a query like "Dante's opera capolavoro" returns top-1 result as `dante-alighieri#divina-commedia-1265-1321` (subnode), not the whole `dante-alighieri` page.
  - Bench: re-run Phase-WIKI-FIX bench harness; verify p95 latency ≤100ms (subnode parsing adds <10ms per page indexed; query-time unaffected).
- **Single atomic commit:** `feat(wiki): heading-level subnodes (H2/H3 → parent_slug + byte-range) (US-WIKI-SUBNODES-01)`

---

## What this phase explicitly does NOT do

- **No `wiki_subgraph` tool.** US-WIKI-B01 in the old Phase-WIKI-B Wave A queue is cancelled per "no new tools" direction (2026-05-22). PPR seeding + hub-aware BFS as a NEW tool conflicts with Phase-TOOL's "kill tool RAG, all tools always-on, single search surface" mandate.
- **No new ingest changes.** Wave B (markitdown plugins per format) stays deferred.
- **No reranker sidecar.** Wave C (`bge-reranker-v2-m3`) stays deferred until bench shows top-1 quality plateau.

---

## Risks

- **R1**: subnode-level FTS multiplies row count by ~3-5× (typical page has 2-4 H2+H3 headings). SQLite FTS5 handles 1k-10k rows trivially but logged for visibility. Mitigation: cap subnodes-per-page at 20 (skip when page has more — log warning; that page is likely a god-doc that should be split anyway).
- **R2**: byte-range rendering needs page body in memory at render time. For 150-page wiki this is fine; for future 1k+ wiki, switch to mmap. Mitigation: out of scope for v0.
- **R3**: heading regex inside fenced code blocks is the documented graphify gotcha — Phase-WIKI-B Wave A plan flags this. Mitigation: parser_test.go must include a fenced-code fixture.

---

## Verification

- `go test ./internal/wiki/... ./internal/storage/search/... -count=1` green.
- Phase-WIKI-FIX bench harness (`docs/quality-bench/runs/post-wiki-fix-2026-05-22.md` style) re-run — FTS hit ratio stays 20/20; top-1 hits improve on at least 3 queries that previously matched the right page but wrong section.
- `SELECT count(*) FROM wiki_documents WHERE id LIKE '%#%'` > 100 after first index rebuild post-deploy.

---

*Created 2026-05-22 — re-scoped from Phase-WIKI-B Wave A US-WIKI-B04 after Phase-WIKI-FIX shipped + Phase-TOOL direction locked "no new tools".*
