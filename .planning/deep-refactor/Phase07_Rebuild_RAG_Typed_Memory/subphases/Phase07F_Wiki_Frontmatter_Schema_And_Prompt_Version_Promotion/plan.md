# Phase07F Plan - Wiki Frontmatter Schema And Prompt-Version Promotion

Status: closed on 2026-05-17 by Codex. Source-audited locally, implemented,
live-probed, and full Go-gated. Not verified by an independent verifier.

## Goal

Wiki retrieval hits should carry the trust metadata already present in wiki
frontmatter so future GraphRAG and current `search_memory(scope=wiki)` can see
whether a page uses the expected schema, which prompt generated it, when it was
created, whether git/versioning degraded, and which sources ground the page.

## Scope

- Promote `schema_version`, `prompt_version`, `created_at`, and `unversioned`
  from `wiki.Page` into the wiki search document metadata.
- Roundtrip those fields through SQLite FTS metadata JSON and Qdrant payload
  parsing into `search.Result`.
- Surface compact wiki trust metadata in `search_memory` output for wiki and
  graph hits.
- Include `sources` in LLM-visible wiki hits; it already exists in
  `search.Result` but is not currently copied to `memoryResult`.
- Add deterministic tests for index document metadata, SQLite result metadata,
  Qdrant payload metadata, graph node metadata, and `search_memory` formatting.
- Repair single-page wiki reindexing so live wiki writes upsert changed Qdrant
  page documents instead of going through the warm-cache rebuild path.

## Non-Goals

- Do not mutate user wiki pages, live source files, or production SQLite rows.
  The live benchmark may create and delete one timestamped disposable wiki page
  and its Qdrant point through the production API/reindex path.
- Do not change `wiki.Page` validation rules or bump `CurrentSchemaVersion`.
- Do not redesign wiki write approval, prompt governance, memory promotion, or
  page repair flows.
- Do not implement graph ranking, community reports, or GraphRAG eval suites in
  this slice.
- Do not add a new dependency or a new metadata database table.

## First Bounded Implementation Slice

1. Extend `internal/storage/search.Result` and `indexedWikiPage` with:
   `SchemaVersion`, `PromptVersion`, `CreatedAt`, and `Unversioned`.
2. Populate those fields in `parseIndexedWikiPage`.
3. Add metadata keys in `loadWikiDocuments` for wiki page docs and graph node
   docs:
   `schema_version`, `prompt_version`, `created_at`, `unversioned`.
4. Parse the metadata keys in `sqliteSearcher.search` and `qdrantSearcher.Search`.
5. Extend `memoryResult` and `searchWiki` to carry wiki `Sources` plus the new
   trust metadata.
6. Format wiki trust tokens in `search_memory` only when values exist:
   `schema=<n>`, `prompt=<version>`, `created=<RFC3339>`,
   `unversioned=true`, `sources=[...]`.
7. Change `qdrantRepository.ReindexWikiPage` to upsert the changed page doc
   directly, because the full rebuild has an intentional warm-cache skip.
8. Add/extend tests before running broader gates.

## Affected Files

| File | Planned Change |
| --- | --- |
| `D:/Aura/internal/storage/search/search.go` | Extend `Result`, indexed wiki page metadata, and document metadata creation. |
| `D:/Aura/internal/storage/search/sqlite.go` | Parse frontmatter trust metadata from FTS metadata JSON. |
| `D:/Aura/internal/storage/search/qdrant.go` | Parse frontmatter trust metadata from Qdrant payload. |
| `D:/Aura/internal/storage/search/graph_documents.go` | Carry frontmatter trust metadata into graph node/index documents where useful. |
| `D:/Aura/internal/agent/tools/registry/memory_search.go` | Carry and format wiki trust metadata and sources. |
| `D:/Aura/internal/storage/search/search_test.go` | Assert index documents and SQLite search preserve metadata. |
| `D:/Aura/internal/storage/search/qdrant_test.go` | Assert Qdrant payload/result metadata preservation. |
| `D:/Aura/internal/agent/tools/registry/memory_search_test.go` | Assert LLM-visible wiki hits show trust metadata without JSON envelope. |
| `D:/Aura/internal/storage/search/qdrant.go` | Upsert one changed wiki page from `ReindexWikiPage` instead of calling the warm-cache full rebuild. |
| `D:/Aura/cmd/probe_chat/{client,cases,phase07f}.go` | Live probe setup/cleanup through the dashboard API and `/api/chat` Phase07F case. |

## PRD Coverage Matrix

| PRD Item | Plan Location | Benchmark Location | Source Evidence | Status |
| --- | --- | --- | --- | --- |
| RAG is schema-aware and returns structured cited retrieval hits | First bounded slice steps 1-6 | `benchmark.md` rows for search/index/tool output | `source.md` local sources | met |
| Wiki GraphRAG can trust page type, sources, and freshness metadata | Steps 3, 5, 6 | `benchmark.md` graph node and `search_memory` rows | `graph_documents.go`, `memory_search.go` | met for metadata availability; ranking remains future GraphRAG work |
| Frontmatter `schema_version`, `prompt_version`, `created_at`, `unversioned` no longer read-on-demand only | Steps 1-4 | `benchmark.md` SQLite/Qdrant rows | `phase07b-current-types-audit` G.2.4 | met |
| No smoke-only completion evidence | Verification section | all benchmark rows include ground truth | `aura-plan-builder` doctrine | met |

## Benchmark Plan

- Baseline package gate:
  `go test ./internal/wiki ./internal/storage/search ./internal/agent/tools/registry -count=1`.
- Targeted post-edit gate:
  `go test ./internal/storage/search ./internal/agent/tools/registry -run "Test(LoadWikiDocuments.*Frontmatter|SQLite.*Frontmatter|Qdrant.*Frontmatter|SearchMemoryTool.*Wiki.*Metadata)" -count=1`.
- Broader Phase07F package gate:
  `go test ./internal/wiki ./internal/storage/search ./internal/agent/tools/registry ./cmd/probe_chat -count=1`.
- Full shared gates after Go edits:
  `go vet ./...`, `go build ./...`, `go test ./... -count=1`.
- Live benchmark after deterministic proof:
  `cmd/probe_chat` case with a disposable wiki page and `search_memory(scope=wiki)`
  that verifies the assistant cites the unprompted page marker/source ID and
  the expected schema/prompt/source tokens after a Docker rebuild/restart.

## Rollback And Deviation Rule

If future work requires changing wiki page write governance, FTS schema, or
source/memory promotion semantics, split a new phase. The only write-path change
accepted in Phase07F is the narrow single-page Qdrant reindex fix needed for
truthful live metadata retrieval.
