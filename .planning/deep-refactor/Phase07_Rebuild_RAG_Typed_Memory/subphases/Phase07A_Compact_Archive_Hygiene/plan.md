# Phase07A Plan - Compact Archive Hygiene

Status: closed 2026-05-16. US-K01..US-K05 shipped and benchmark rows are met.

## Goal

Keep `conversations` as the full raw archive, but prevent compact memory and
default recall from treating raw tool outputs, tool errors, tool schema dumps,
and intermediate loop scaffolding as durable memory.

## Problem

The current compact-memory path indexes every non-empty conversation archive
turn. Recent live database evidence showed `compact_memory_documents` and FTS
returning rows such as tool errors, `tool_schemas.json`, `AGENT.md` dumps,
stdout snippets, and assistant loop noise. That makes `search_memory` a memory
soup instead of a typed retrieval surface.

## Scope

- Add archive-to-compact eligibility in `memoryindex.ArchiveDocument`.
- Ensure live archive append and startup rebuild use the same policy.
- Preserve raw `conversations` archive rows for debugging and audit.
- Exclude `role=tool` rows from compact archive indexing.
- Exclude assistant rows that are only tool-call/intermediate scaffolding when
  enough metadata exists to identify them safely.
- Keep user rows and final assistant rows eligible unless a test proves they
  are unsafe.
- Update `search_memory` description/scope tests only as needed for the
  no-schema compatibility slice.

## Non-Goals

- Do not delete existing `conversations`, `compact_memory_documents`, FTS, or
  Qdrant data in this slice.
- Do not introduce a new memory schema or migration unless tests prove the
  current fields cannot express safe eligibility.
- Do not implement full `recall_knowledge`, `recall_user`, or
  `recall_operational` in Phase07A.
- Do not rewrite wiki, source ingest, Qdrant, or GraphIndex.
- Do not promote tool failures into operational memory automatically.

## Affected Files

| File | Ownership In This Slice |
| --- | --- |
| `internal/storage/memoryindex/rebuild.go` | Central eligibility policy in `ArchiveDocument`; live append and rebuild inherit it. |
| `internal/storage/memoryindex/rebuild_test.go` | Add role/tool-output exclusion and live-vs-rebuild parity tests. |
| `internal/agent/tools/registry/memory_search.go` | Update user-facing description or default scope only if required by tests. |
| `internal/agent/tools/registry/memory_search_test.go` | Prove default/all behavior does not surface raw tool-output archive rows; preserve explicit archive scope if retained. |
| `internal/conversation/archive_turns_test.go` | Ensure raw tool rows remain archived in `conversations`; do not weaken archive durability. |

## Implementation Steps

1. Baseline current tests listed in `benchmark.md`.
2. Add a failing `memoryindex` test showing `ArchiveDocument` rejects
   `role=tool` rows while the archive store still persists them.
3. Add a rebuild parity test proving `Rebuild` and `IndexingTurnAppender`
   produce the same compact archive eligibility.
4. Add a `search_memory` regression proving tool-only archive text does not
   appear in default recall output.
5. Patch the smallest policy seam, preferably `ArchiveDocument`, before
   touching callers.
6. Run targeted tests, then broader package tests named in `benchmark.md`.
7. Record results in this folder's `progress.md`; do not mark Phase 7 complete.

## Stop Signals

- A required fix needs data deletion or migration.
- Excluding tool rows breaks archive deletion/purge invariants.
- Tests show final assistant rows cannot be distinguished from intermediate
  tool-call scaffolding without schema changes. In that case, close Phase07A
  with only `role=tool` exclusion and plan a schema-backed Phase07B/07C follow-up.
