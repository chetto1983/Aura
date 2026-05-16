# Phase07 Sub-Phase Summary

Status: parent summary repaired on 2026-05-16 after current git/PRD/code showed
Phase07C implementation evidence newer than the stale resume files.

| Sub-Phase | Purpose | Ready For Implementation | Benchmark |
| --- | --- | --- | --- |
| Phase07A_Compact_Archive_Hygiene | Stop raw tool output and intermediate loop noise from entering compact memory/default recall while preserving the raw conversation archive. | closed | `subphases/Phase07A_Compact_Archive_Hygiene/benchmark.md` |
| Phase07B_Typed_Collection_Registry | Define layer metadata, citation handles, score components, follow-up handles, and SourceID filtering. | closed | `subphases/Phase07B_Typed_Collection_Registry/benchmark.md` |
| Phase07C_Projection_Freshness_Registry | Make compact-memory projection freshness durable, drift-aware, and visible to the agent through retrieval annotations. | closed, self-audited | `subphases/Phase07C_Projection_Freshness_Registry/benchmark.md` |
| Phase07D_User_Operational_Memory_Typed_Tiers | Wire user and operational memory writers/recall surfaces as first-class typed tiers. | no | parent `benchmark.md` |
| Phase07E_Source_Span_Byte_Offsets | Preserve source/page/span or artifact offsets for source hit follow-up. | no | parent `benchmark.md` |
| Phase07F_Wiki_Frontmatter_Schema_Promotion | Promote wiki page metadata/schema/prompt-version trust for later GraphRAG work. | no | parent `benchmark.md` |

## Boundary Notes

- Phase07A is intentionally a no-schema, no-data-delete hygiene slice.
- Phase07C closes compact-memory projection freshness only. Wiki/source/Qdrant
  delete/rename invalidation, GraphRAG weighting, and golden RAG evals remain
  future Phase 7 work.
- Phase 9 owns promotion/write discipline after Phase 7 exposes typed recall.
- Phase 10 owns runtime configuration source-of-truth; tool schemas and
  tool-memory bootstrap are not compact memory rows.

## First Next Slice

If continuing Phase 7 planning, select Phase07D and rebuild it from current
code and `prd.md` before implementation. Do not resurrect the old
`Phase07C_Task_Level_Recall_Surface` name; task-level recall was partially
handled by later user/operational recall work and must be remapped against the
current code.
