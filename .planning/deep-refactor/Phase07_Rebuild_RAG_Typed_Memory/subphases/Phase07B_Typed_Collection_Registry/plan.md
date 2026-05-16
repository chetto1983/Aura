# Phase07B Plan - Typed Collection Registry

Status: **closed 2026-05-16**

## Goal

Deliver the four MUST items from PRD §7 identified by the 2026-05-16 audit
triple (docs/phase07b-current-types-audit-2026-05-16.md §G.1,
docs/phase07b-web-research-frameworks-2026-05-16.md §G.1-G.3,
docs/phase07b-academic-research-2026-05-16.md §G.1-G.5).

## Deliverables Shipped (US-L01..L05)

**1. Typed Collection enum + descriptor Registry (US-L01, US-L02)**

- `internal/storage/memoryindex/collections.go` — `type Collection string`,
  6 constants (wiki, source, archive, proposal, user_memory, operational),
  `CollectionDescriptor` struct, `Registry` map, `Lookup()` helper.
- `internal/storage/memoryindex/store.go` — `KindSource/Archive/Proposal`
  derived from `string(Collection*)` for back-compat with zero caller changes.
- Audit reference: §A.1-A.4, §G.1.1.

**2. Structured retrieval hits with score components (US-L03)**

- `memoryindex.Document` + `search.Result` each gained `ScoreExact`, `ScoreFTS`,
  `ScoreVector float64`.
- `mergeDocumentsRRF` + `mergeHybridResults` preserve per-channel RRF
  contributions instead of dropping them at collapse.
- Audit reference: §D, §G.1.2. Literature: arXiv:2210.11934, 2508.01405, 2504.01733.

**3. Follow-up handles per hit in memory_search formatter (US-L04)**

- `formatMemoryResults` appends `[exact=A.AA fts=B.BB vector=C.CC]` when any
  component is non-zero, and `follow_up=<tool>` using only tool names that exist
  in `internal/agent/tools/registry/`.
- Mapping: source→`read_source`; archive→`read_memory`/`search_memory`;
  wiki→`[[slug]]`; proposal→`read_memory(handle=proposal:<id>)`.
- Audit reference: §C.1-C.2, §G.1.3. Literature: RAPTOR, HiChunk (§G.3).

**4. SourceID filter for chunk-to-parent expansion (US-L05)**

- `memoryindex.Filter.SourceID string` field + `filterWhere` predicate +
  `memory_search` tool `source_id` parameter.
- Enables "give me all chunks of source X" in one query for navigation.
- Audit reference: §E.2, §G.1.4.

## Deferrals (to Phase 7C/7D per audit §G.2)

| Item | Reason deferred |
| --- | --- |
| Projection freshness registry (§G.2.1) | Requires new table + migration + reindex-job hook + dashboard surfacing — orthogonal to typed retrieval |
| User/operational memory as first-class collections (§G.2.2) | Significant feature work (proposed_updates rerouting, tool_attempts indexing); not just typing |
| Span / chunk offsets for sources (§G.2.3) | Page-level granularity sufficient for 7B; byte offsets are an improvement not required by PRD §7 MUST |
| Wiki frontmatter field promotion (§G.2.4) | schema_version, prompt_version, created_at, unversioned are read-on-demand; staleness tooling deferred |

## Research Input

- `docs/phase07b-current-types-audit-2026-05-16.md` — canonical file:line citations
- `docs/phase07b-web-research-frameworks-2026-05-16.md` — cross-framework ADOPT/AVOID
- `docs/phase07b-academic-research-2026-05-16.md` — arXiv evidence for score-components + small-to-big
