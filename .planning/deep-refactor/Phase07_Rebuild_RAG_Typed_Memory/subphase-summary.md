# Phase07 Sub-Phase Summary

Status: parent summary repaired on 2026-05-16 after current git/PRD/code showed
Phase07C implementation evidence newer than the stale resume files. Phase07F
was closed on 2026-05-17.

| Sub-Phase | Purpose | Ready For Implementation | Benchmark |
| --- | --- | --- | --- |
| Phase07A_Compact_Archive_Hygiene | Stop raw tool output and intermediate loop noise from entering compact memory/default recall while preserving the raw conversation archive. | closed | `subphases/Phase07A_Compact_Archive_Hygiene/benchmark.md` |
| Phase07B_Typed_Collection_Registry | Define layer metadata, citation handles, score components, follow-up handles, and SourceID filtering. | closed | `subphases/Phase07B_Typed_Collection_Registry/benchmark.md` |
| Phase07C_Projection_Freshness_Registry | Make compact-memory projection freshness durable, drift-aware, and visible to the agent through retrieval annotations. | closed, self-audited | `subphases/Phase07C_Projection_Freshness_Registry/benchmark.md` |
| Phase07D_User_Operational_Memory_Typed_Tiers | Wire user and operational memory writers/recall surfaces as first-class typed tiers. | closed with local and live verification | `subphases/Phase07D_User_Operational_Memory_Typed_Tiers/benchmark.md` |
| Phase07E_Source_Span_Byte_Offsets | Preserve source/page/span or artifact offsets for source hit follow-up. | closed with local and live verification | `subphases/Phase07E_Source_Span_Byte_Offsets/benchmark.md` |
| Phase07F_Wiki_Frontmatter_Schema_And_Prompt_Version_Promotion | Promote wiki page metadata/schema/prompt-version trust for later GraphRAG work. | closed with local and live verification | `subphases/Phase07F_Wiki_Frontmatter_Schema_And_Prompt_Version_Promotion/benchmark.md` |

## Boundary Notes

- Phase07A is intentionally a no-schema, no-data-delete hygiene slice.
- Phase07C closes compact-memory projection freshness only. Wiki/source/Qdrant
  delete/rename invalidation, GraphRAG weighting, and golden RAG evals remain
  future Phase 7 work.
- Phase 9 owns promotion/write discipline after Phase 7 exposes typed recall.
- Phase07F closes metadata availability only. GraphRAG ranking, community
  reports, and broad delete/rename projection invalidation remain future work.
- Phase 10 owns runtime configuration source-of-truth; tool schemas and
  tool-memory bootstrap are not compact memory rows.

## First Next Slice

If continuing the roadmap, move to Phase08 or Phase09 promotion/planning.
Phase07D, Phase07E, and Phase07F are closed by deterministic local tests, live
`cmd/probe_chat` probes, and full repo build/vet/test gates.
Do not resurrect the old `Phase07C_Task_Level_Recall_Surface` name; task-level
recall is represented by `recall_user_memory` and `recall_operational`.
