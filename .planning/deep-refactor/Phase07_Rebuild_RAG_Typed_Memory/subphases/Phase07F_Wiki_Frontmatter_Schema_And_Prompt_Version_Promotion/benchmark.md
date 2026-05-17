# Phase07F Benchmark - Wiki Frontmatter Schema And Prompt-Version Promotion

Status: closed on 2026-05-17 by Codex. Deterministic package tests, full Go
gates, Docker rebuild/restart, and the live `/api/chat` metadata probe passed.

| Check | Command / Method | Fixture / Data Source | Expected Ground Truth | Pass / Fail Threshold | PRD Gate | Actual Result |
| --- | --- | --- | --- | --- | --- | --- |
| Baseline package health | `go test ./internal/wiki ./internal/storage/search ./internal/agent/tools/registry -count=1` | Current repo before Phase07F edits | Existing wiki/search/tool tests pass before implementation | PASS if command exits 0 | Readiness pre-edit baseline | passed 2026-05-17 |
| Search document metadata promotion | `go test ./internal/storage/search -run TestLoadWikiDocumentsPromotesFrontmatterMetadata -count=1` | Temp wiki dir with one page containing schema, prompt, created, updated, sources, and unversioned metadata | Indexable `Document.Metadata` contains `schema_version`, `prompt_version`, `created_at`, `updated_at`, `sources`, and `unversioned` values derived from parsed frontmatter | PASS if exact metadata values match fixture; FAIL on missing/empty/mutated values | Schema-aware RAG metadata | passed in targeted Phase07F gate |
| SQLite metadata roundtrip | `go test ./internal/storage/search -run TestSQLiteSearchReturnsFrontmatterMetadata -count=1` | Temp wiki dir plus disposable SQLite DB rebuilt through `RebuildSQLiteWikiDocuments` | `sqliteSearcher.search` returns `search.Result` with exact schema version, prompt version, created_at, unversioned, sources, and updated_at | PASS if result fields match fixture and stale docs are not present | Retrieval hit trust metadata | passed in targeted Phase07F gate |
| Qdrant payload metadata roundtrip | `go test ./internal/storage/search -run "Test(QdrantSearcherSearchQueriesPointsAndMapsPayload|RebuildQdrantWikiDocumentsCreatesCollectionAndUpsertsDocs)" -count=1` | `httptest.Server` Qdrant fake with captured upsert payload and search response payload | Qdrant payload includes frontmatter metadata and `qdrantSearcher.Search` parses it into `search.Result` | PASS if captured payload and parsed result match fixture | Vector retrieval trust metadata | passed in targeted Phase07F gate |
| Single-page Qdrant reindex after warm cache | `go test ./internal/storage/search -run TestQdrantRepositoryReindexWikiPageUpsertsChangedPageAfterWarmCache -count=1` | Warm-cache `httptest.Server` Qdrant fake plus changed wiki page fixture | `ReindexWikiPage` upserts the changed page with schema/prompt/source metadata instead of calling the warm-cache full rebuild that skips writes | PASS if exactly one point is upserted, no collection delete/recreate happens, and metadata matches fixture | Live wiki write retrieval freshness | passed 2026-05-17 |
| Graph node metadata | `go test ./internal/storage/search -run TestLoadWikiDocumentsPromotesFrontmatterMetadata -count=1` | Temp wiki dir with linked pages and source-backed frontmatter | `graph:node:<slug>` document metadata/content carries compact schema/prompt/source trust signals without dumping raw YAML | PASS if graph node includes expected trust metadata and no raw frontmatter block | Future GraphRAG trust signals | passed in targeted Phase07F gate |
| LLM-visible search_memory output | `go test ./internal/agent/tools/registry -run TestSearchMemoryTool_WikiFrontmatterMetadata -count=1` | Fake indexed wiki search result with frontmatter fields and sources | `search_memory(scope=wiki)` output includes compact tokens such as `schema=2`, `prompt=v1`, `created=...`, `sources=[...]`, `unversioned=true`, and no JSON envelope | PASS if exact tokens appear and output remains markdown lines | Cited retrieval hit surface | passed in targeted Phase07F gate |
| Phase07F package gate | `go test ./internal/wiki ./internal/storage/search ./internal/agent/tools/registry ./cmd/probe_chat -count=1` | Phase07F touched packages plus probe compile | All touched packages pass without weakening existing tests | PASS if command exits 0 | Shared package stability | passed 2026-05-17 |
| Full Go gates | `go vet ./...`; `go build ./...`; `go test ./... -count=1` | Whole repository after Phase07F implementation | Repo compiles, vets, and tests green | PASS if all commands exit 0 | Shared runtime stability | passed 2026-05-17 |
| Live wiki metadata probe | `docker compose build aura`; `docker compose up -d --no-deps aura`; seed token with `docker compose --profile test run --rm -v ${PWD}\data:/data test go run ./cmd/seed_e2e_env -db /data/aura.db -shell powershell`; `go run ./cmd/probe_chat -case phase07f-wiki-frontmatter-metadata -url http://localhost:18080/api/chat -api http://localhost:18080/api -db .\data\aura.db -token <seeded-token> -timeout 240 -json` | Disposable live wiki page written through `/api/files/wiki/file`, cleaned through the same API, then searched through `/api/chat` | `/api/chat` uses `search_memory(scope=wiki)`; assistant reply cites the unprompted marker/source ID from the disposable page and the expected schema/prompt/created/unversioned metadata; API ground truth confirms the page existed during the probe | PASS if `pass=true`, reply contains marker/source not present in prompt, metadata tokens are present, and fixture cleanup succeeds; `tool_attempts` row is diagnostic-only because current web chat can return tool calls without a recorded attempt row | Live E2E behavior | passed 2026-05-17: `pass=true`, `tool_calls=2`, `llm_calls=3`, marker `PHASE07F_MARKER_20260517_214901`, source `src_phase07f_20260517_214901` |

## Metrics

| Metric | Target |
| --- | --- |
| Targeted package test wall time | Under 45s on local dev machine |
| Full Go gate | Green without skipped production tests |
| Live probe tool path | `/api/chat` returned tool calls and replied with marker/source values absent from the prompt, backed by API-created wiki fixture and deterministic Qdrant reindex tests |
| Metadata precision | Exact fixture values, not substring-only page existence |

## Completion Rule

Phase07F is closed for wiki frontmatter metadata promotion. The live probe did
not rely on tool-call count alone: the unprompted marker and source ID came from
the disposable wiki page, and deterministic tests prove the metadata path
through SQLite, Qdrant payloads, single-page reindex, and `search_memory`
formatting.
