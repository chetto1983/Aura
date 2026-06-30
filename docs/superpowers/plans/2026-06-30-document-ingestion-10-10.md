# Document Ingestion 10/10 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. The user explicitly requested no worktrees, so execute inline in `D:\Aura` and stage only files touched for the current task.

**Goal:** Move Aura's document ingestion pipeline from the current partially industrial state toward the 10/10 readiness target defined in `docs/superpowers/specs/2026-06-30-document-ingestion-industrial-audit-design.md`.

**Architecture:** Keep the existing asset/document/Neo4j foundations and harden them incrementally. Start with low-risk correctness gates and compatibility fixes, then add the document/version/tag control plane, durable jobs, lifecycle-safe graph/vector deletion, cleanup tooling, and finally the React management UI.

**Tech Stack:** Go 1.26, Postgres/sqlc, Neo4j via the existing knowledge MCP seam, Garage/S3 object storage abstraction, React 19/Vite/Vitest/Playwright.

---

## Execution Rules

- Work in `D:\Aura`; do not create a git worktree.
- Before each commit, run `git status --short` and stage only files for the active task.
- Existing dirty files outside the active task are user work and must not be reverted.
- Use TDD for implementation tasks: write the failing test, run it, implement, rerun.
- The pre-commit Lefthook file-size script has hung on this machine; if it hangs again, run task-specific checks manually and commit with `--no-verify`, recording that in the final note.

## Current Dirty Worktree To Avoid

At plan creation time these files were already modified and are not part of this plan unless the user explicitly redirects:

- `.planning/STATE.md`
- `.planning/graphs/.last-build-status.json`
- `.planning/graphs/GRAPH_REPORT.md`
- `internal/agui/governance_api.go`
- `internal/agui/governance_api_test.go`

## File Map

Early hardening:

- `cmd/aura/assets_test.go` - asserts asset pipeline wiring.
- `cmd/aura/docs.go` - runtime document service construction.
- `web/src/chat/attachments/useAttachmentUploads.ts` - frontend modality inference.
- `web/src/chat/attachments/__tests__/useAttachmentUploads.test.tsx` - upload hook coverage.
- `internal/documents/search.go` - sparse search Cypher.
- `internal/documents/retrieve.go` - vector seed and graph expansion Cypher.
- `internal/documents/search_test.go` - sparse lifecycle-filter tests.
- `internal/documents/retrieve_test.go` - vector/expand lifecycle-filter tests.

Document control plane:

- `internal/db/migrations/0021_document_control_plane.up.sql`
- `internal/db/migrations/0021_document_control_plane.down.sql`
- `internal/db/queries/document_control_plane.sql`
- generated `internal/db/sqlc/*`
- `internal/documents/catalog_types.go`
- `internal/documents/catalog_store.go`
- `internal/documents/catalog_service.go`
- `internal/documents/catalog_*_test.go`
- `internal/agui/documents_api.go`

Durable work and cleanup:

- `internal/documents/jobs.go`
- `internal/documents/jobs_store.go`
- `internal/documents/jobs_worker.go`
- `internal/documents/delete.go`
- `internal/documents/orphans.go`
- Neo4j migrations under `internal/knowledge/migrations`

React management UI:

- `web/src/documents/*`
- `web/src/main.tsx`
- `web/src/chat/attachments/types.ts`
- `web/e2e/documents.spec.ts`

---

## Phase 0: Baseline and Low-Risk Correctness

### Task 0.1: Baseline status and targeted tests

**Files:**
- Read-only: current repository.

- [ ] **Step 1: Record current status**

Run:

```powershell
git status --short
```

Expected: only pre-existing user changes plus files from this task once edits begin.

- [ ] **Step 2: Run focused Go tests that cover the first slice**

Run:

```powershell
go test -count=1 ./cmd/aura ./internal/assets ./internal/documents
```

Expected: PASS before implementation or a clear pre-existing failure to record.

- [ ] **Step 3: Run focused web tests for attachments**

Run:

```powershell
cd web
npm test -- --run src/chat/attachments/__tests__/useAttachmentUploads.test.tsx
```

Expected: PASS before implementation or a clear pre-existing failure to record.

### Task 0.2: Align runtime document size limit with asset document limit

**Problem:** `AURA_ASSET_MAX_DOCUMENT_BYTES` defaults to 100 MiB while `documents.DefaultMaxIngestBytes` is 50 MiB. Runtime asset ingestion currently constructs `documents.Service` without `MaxBytes`, so a document can pass asset validation and fail ingestion downstream.

**Files:**
- Modify: `cmd/aura/assets_test.go`
- Modify: `cmd/aura/docs.go`

- [ ] **Step 1: Write the failing wiring test**

In `cmd/aura/assets_test.go`, extend `TestBuildAssetServiceWiresDocumentProcessor` after the `runtimeDocumentIngestor` assertion:

```go
ingestor, ok := doc.Ingest.(*runtimeDocumentIngestor)
if !ok {
	t.Fatalf("document processor ingestor = %T, want *runtimeDocumentIngestor", doc.Ingest)
}
if ingestor.MaxBytes != 123 {
	t.Fatalf("runtime document ingestor MaxBytes = %d, want asset max document bytes 123", ingestor.MaxBytes)
}
```

Remove the older duplicate assertion:

```go
if _, ok := doc.Ingest.(*runtimeDocumentIngestor); !ok {
	t.Fatalf("document processor ingestor = %T, want *runtimeDocumentIngestor", doc.Ingest)
}
```

- [ ] **Step 2: Run the failing test**

Run:

```powershell
go test -count=1 ./cmd/aura -run TestBuildAssetServiceWiresDocumentProcessor
```

Expected: FAIL because `runtimeDocumentIngestor` has no `MaxBytes` field.

- [ ] **Step 3: Implement the minimal runtime wiring**

In `cmd/aura/docs.go`, change `runtimeDocumentIngestor` to:

```go
type runtimeDocumentIngestor struct {
	cfg      *config.Config
	pool     *pgxpool.Pool
	MaxBytes int64
}
```

Change `newRuntimeDocumentIngestor` to:

```go
func newRuntimeDocumentIngestor(cfg *config.Config, pool *pgxpool.Pool) *runtimeDocumentIngestor {
	var maxBytes int64
	if cfg != nil {
		maxBytes = int64(cfg.AssetMaxDocumentBytes)
	}
	return &runtimeDocumentIngestor{cfg: cfg, pool: pool, MaxBytes: maxBytes}
}
```

In `IngestPath`, set `MaxBytes` on `documents.Service`:

```go
svc := &documents.Service{
	Jobs:      documents.NewPostgresJobStore(i.pool),
	Extractor: &documents.ExtractClient{BaseURL: documentsBaseURL(i.cfg), Client: documentHTTPClient(i.cfg)},
	Indexer:   &documents.Indexer{Client: mcp},
	Searcher:  &documents.Searcher{Client: mcp},
	Embedder:  runtimeEmbeddingQueue{cfg: i.cfg, pool: i.pool},
	MaxBytes:  i.MaxBytes,
}
```

- [ ] **Step 4: Verify**

Run:

```powershell
go test -count=1 ./cmd/aura -run TestBuildAssetServiceWiresDocumentProcessor
go test -count=1 ./cmd/aura ./internal/assets ./internal/documents
```

Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```powershell
git add -- cmd/aura/assets_test.go cmd/aura/docs.go
git commit -m "fix: align asset and document ingest size limits"
```

If Lefthook hangs in `scripts/check-file-size.sh`, run:

```powershell
go test -count=1 ./cmd/aura ./internal/assets ./internal/documents
git commit --no-verify -m "fix: align asset and document ingest size limits"
```

### Task 0.3: Align browser document modality inference with backend allowlist

**Problem:** Backend accepts `.pptx`, `.html`, `.csv`, `.md`, `.txt`, `.json`, `.xml`, and `.epub`, but the frontend only hints a smaller set as `document`. Valid browser uploads can be sent as `unknown`.

**Files:**
- Modify: `web/src/chat/attachments/useAttachmentUploads.ts`
- Modify: `web/src/chat/attachments/__tests__/useAttachmentUploads.test.tsx`

- [ ] **Step 1: Write the failing test cases**

In `useAttachmentUploads.test.tsx`, replace the `files` array in `infers modality hints for common asset file types` with:

```ts
const files = [
  new File(['png'], 'image.png', { type: 'image/png' }),
  new File(['ogg'], 'voice.ogg', { type: 'audio/ogg' }),
  new File(['pdf'], 'manual.pdf', { type: '' }),
  new File(['docx'], 'manual.docx', { type: '' }),
  new File(['pptx'], 'deck.pptx', { type: '' }),
  new File(['xlsx'], 'sheet.xlsx', { type: '' }),
  new File(['xlsm'], 'macro.xlsm', { type: '' }),
  new File(['html'], 'page.html', { type: '' }),
  new File(['csv'], 'data.csv', { type: '' }),
  new File(['md'], 'notes.md', { type: '' }),
  new File(['txt'], 'notes.txt', { type: '' }),
  new File(['json'], 'data.json', { type: '' }),
  new File(['xml'], 'data.xml', { type: '' }),
  new File(['epub'], 'book.epub', { type: '' }),
];
```

Replace `finalAssets` with one asset per file, all document files using `searchableAsset` and no `unknown` file.

Replace the expected modality hints with:

```ts
expect(presignBodies.map((body) => body.modality_hint)).toEqual([
  'image',
  'audio',
  'document',
  'document',
  'document',
  'document',
  'document',
  'document',
  'document',
  'document',
  'document',
  'document',
  'document',
  'document',
]);
```

- [ ] **Step 2: Run the failing web test**

Run:

```powershell
cd web
npm test -- --run src/chat/attachments/__tests__/useAttachmentUploads.test.tsx
```

Expected: FAIL because some supported extensions are still inferred as `unknown`.

- [ ] **Step 3: Implement the allowlist in the hook**

In `useAttachmentUploads.ts`, add:

```ts
const documentExtensions = new Set([
  '.pdf',
  '.docx',
  '.pptx',
  '.xlsx',
  '.xlsm',
  '.html',
  '.htm',
  '.csv',
  '.md',
  '.markdown',
  '.txt',
  '.json',
  '.xml',
  '.epub',
]);
```

Change `inferModality` to use it:

```ts
function inferModality(file: File): AssetModality {
  const type = file.type.toLowerCase();
  const name = file.name.toLowerCase();
  if (type.startsWith('image/')) return 'image';
  if (type.startsWith('audio/')) return 'audio';
  if (type === 'application/pdf' || hasDocumentExtension(name)) return 'document';
  return 'unknown';
}

function hasDocumentExtension(name: string): boolean {
  for (const ext of documentExtensions) {
    if (name.endsWith(ext)) return true;
  }
  return false;
}
```

- [ ] **Step 4: Verify**

Run:

```powershell
cd web
npm test -- --run src/chat/attachments/__tests__/useAttachmentUploads.test.tsx
npm run typecheck
```

Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```powershell
git add -- web/src/chat/attachments/useAttachmentUploads.ts web/src/chat/attachments/__tests__/useAttachmentUploads.test.tsx
git commit -m "fix: align attachment document modality hints"
```

### Task 0.4: Add lifecycle-safe query filters for current and future Neo4j nodes

**Problem:** Search and retrieval do not filter deleted/inactive graph state. Even before full document versioning lands, queries should ignore nodes marked inactive or deleted.

**Files:**
- Modify: `internal/documents/search.go`
- Modify: `internal/documents/search_test.go`
- Modify: `internal/documents/retrieve.go`
- Modify: `internal/documents/retrieve_test.go`

- [ ] **Step 1: Add sparse search query tests**

In `internal/documents/search_test.go`, add:

```go
func TestSearchQueriesExcludeInactiveOrDeletedChunks(t *testing.T) {
	fake := &fakeKnowledgeClient{}
	searcher := &Searcher{Client: fake}
	if _, err := searcher.Search(t.Context(), SearchRequest{Query: "reset"}); err != nil {
		t.Fatal(err)
	}
	query := fake.readCalls[0].query
	for _, want := range []string{"coalesce(node.active, true) = true", "node.deleted_at IS NULL"} {
		if !strings.Contains(query, want) {
			t.Fatalf("unscoped sparse query missing %q:\n%s", want, query)
		}
	}
}

func TestScopedSearchQueriesExcludeInactiveOrDeletedChunks(t *testing.T) {
	fake := &fakeKnowledgeClient{}
	searcher := &Searcher{Client: fake}
	if _, err := searcher.Search(t.Context(), SearchRequest{Query: "reset", DocumentID: "doc_123"}); err != nil {
		t.Fatal(err)
	}
	query := fake.readCalls[0].query
	for _, want := range []string{"coalesce(node.active, true) = true", "node.deleted_at IS NULL"} {
		if !strings.Contains(query, want) {
			t.Fatalf("scoped sparse query missing %q:\n%s", want, query)
		}
	}
}
```

- [ ] **Step 2: Add vector/expand query tests**

In `internal/documents/retrieve_test.go`, add:

```go
func TestRetrieveVectorQueriesExcludeInactiveOrDeletedChunks(t *testing.T) {
	knowledge := &fakeRetrieveKnowledge{seedRows: []map[string]any{
		seedRow("doc-1", "chunk-0", "alpha", 0.9),
	}}
	svc := &Service{
		Searcher:      &fakeSearchBackend{},
		Knowledge:     knowledge,
		QueryEmbedder: &fakeQueryEmbedder{vector: []float64{0.1}},
		Reranker:      &fakeReranker{scored: []rerank.Scored{{Index: 0, Score: 0}}},
	}
	if _, err := svc.Retrieve(t.Context(), SearchRequest{Query: "q"}); err != nil {
		t.Fatal(err)
	}
	for _, call := range knowledge.reads {
		if strings.Contains(call.query, "queryNodes") {
			for _, want := range []string{"coalesce(node.active, true) = true", "node.deleted_at IS NULL"} {
				if !strings.Contains(call.query, want) {
					t.Fatalf("vector seed query missing %q:\n%s", want, call.query)
				}
			}
		}
	}
}

func TestRetrieveExpansionExcludesInactiveOrDeletedNeighbors(t *testing.T) {
	knowledge := &fakeRetrieveKnowledge{
		seedRows: []map[string]any{
			seedRow("doc-1", "chunk-0", "alpha", 0.9),
			seedRow("doc-1", "chunk-1", "bravo", 0.8),
		},
		expandRows: []map[string]any{seedRow("doc-1", "chunk-2", "context", 0)},
	}
	svc := &Service{
		Searcher:      &fakeSearchBackend{},
		Knowledge:     knowledge,
		QueryEmbedder: &fakeQueryEmbedder{vector: []float64{0.1}},
		Reranker:      &fakeReranker{scored: []rerank.Scored{{Index: 0, Score: 0.9}, {Index: 1, Score: 0.8}}},
	}
	if _, err := svc.Retrieve(t.Context(), SearchRequest{Query: "q", Limit: 1}); err != nil {
		t.Fatal(err)
	}
	for _, call := range knowledge.reads {
		if strings.Contains(call.query, "NEXT_CHUNK") {
			for _, want := range []string{"coalesce(n.active, true) = true", "n.deleted_at IS NULL"} {
				if !strings.Contains(call.query, want) {
					t.Fatalf("neighbor query missing %q:\n%s", want, call.query)
				}
			}
		}
	}
}
```

- [ ] **Step 3: Run failing tests**

Run:

```powershell
go test -count=1 ./internal/documents -run "Test(SearchQueriesExcludeInactiveOrDeletedChunks|ScopedSearchQueriesExcludeInactiveOrDeletedChunks|RetrieveVectorQueriesExcludeInactiveOrDeletedChunks|RetrieveExpansionExcludesInactiveOrDeletedNeighbors)"
```

Expected: FAIL because queries do not include lifecycle filters.

- [ ] **Step 4: Implement lifecycle filters**

In `sparseSearchQuery`, add after the existing document filter:

```cypher
  AND coalesce(node.active, true) = true
  AND node.deleted_at IS NULL
```

In `docScopedSparseQuery`, add after `WHERE node.text IS NOT NULL`:

```cypher
  AND coalesce(node.active, true) = true
  AND node.deleted_at IS NULL
```

In `vectorSeedQuery`, add after the document filter:

```cypher
  AND coalesce(node.active, true) = true
  AND node.deleted_at IS NULL
```

In `docScopedVectorSeedQuery`, add after `WHERE node.embedding IS NOT NULL`:

```cypher
  AND coalesce(node.active, true) = true
  AND node.deleted_at IS NULL
```

In `neighborExpandQuery`, add:

```cypher
  AND coalesce(n.active, true) = true
  AND n.deleted_at IS NULL
```

- [ ] **Step 5: Verify**

Run:

```powershell
go test -count=1 ./internal/documents
```

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```powershell
git add -- internal/documents/search.go internal/documents/search_test.go internal/documents/retrieve.go internal/documents/retrieve_test.go
git commit -m "fix: filter inactive document chunks from retrieval"
```

---

## Phase 1: Document Catalog, Tags, and Version Ledger

### Task 1.1: Add append-only document catalog schema

**Files:**
- Create: `internal/db/migrations/0021_document_control_plane.up.sql`
- Create: `internal/db/migrations/0021_document_control_plane.down.sql`
- Create: `internal/db/queries/document_control_plane.sql`
- Modify generated: `internal/db/sqlc/*`

- [ ] **Step 1: Write migration with documents, versions, storage objects, tags, jobs, and events**

Create the SQL tables exactly from the design spec, using `aura.documents`, `aura.document_versions`, `aura.storage_objects`, `aura.ingestion_jobs`, `aura.ingestion_events`, `aura.document_chunks`, `aura.document_embeddings`, `aura.delete_jobs`, and `aura.audit_logs`.

Minimum document tag schema:

```sql
CREATE TABLE aura.document_tags (
    document_id uuid NOT NULL REFERENCES aura.documents(id) ON DELETE CASCADE,
    tag text NOT NULL CHECK (tag <> ''),
    created_by uuid REFERENCES aura.identities(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (document_id, tag)
);

CREATE INDEX document_tags_tag_document_idx
    ON aura.document_tags (tag, document_id);
```

- [ ] **Step 2: Add sqlc queries**

Queries must include create/list/get/update document, replace tags, create version, create storage object, create job, claim jobs, update job status, append event, and create delete job.

- [ ] **Step 3: Generate and verify**

Run:

```powershell
sqlc generate
go test -tags db_integration -count=1 ./internal/db -run TestMigrate
```

Expected: generated code compiles and migrations apply in the db integration tier.

### Task 1.2: Add document catalog domain service

**Files:**
- Create: `internal/documents/catalog_types.go`
- Create: `internal/documents/catalog_store.go`
- Create: `internal/documents/catalog_service.go`
- Create: `internal/documents/catalog_service_test.go`

- [ ] **Step 1: Write tests for tag normalization**

Expected cases:

```go
cases := []struct {
	in   []string
	want []string
}{
	{[]string{" Servo ", "servo", "G220"}, []string{"g220", "servo"}},
	{[]string{"line automation", "LINE AUTOMATION"}, []string{"line automation"}},
	{[]string{"", "   "}, nil},
}
```

- [ ] **Step 2: Implement `NormalizeTags`**

Rules:

- trim spaces
- lowercase
- collapse internal whitespace
- drop empty tags
- dedupe
- sort for stable storage
- reject tags longer than 64 runes
- reject more than 32 tags

- [ ] **Step 3: Add service methods**

Methods:

```go
func (s *CatalogService) CreateDocument(ctx context.Context, req CreateDocumentRequest) (Document, error)
func (s *CatalogService) UpdateDocument(ctx context.Context, req UpdateDocumentRequest) (Document, error)
func (s *CatalogService) ListDocuments(ctx context.Context, req ListDocumentsRequest) ([]DocumentSummary, error)
func (s *CatalogService) GetDocument(ctx context.Context, identityID, documentID string) (DocumentDetail, error)
```

### Task 1.3: Add document management API

**Files:**
- Create: `internal/agui/documents_api.go`
- Modify: AG-UI server route registration file that owns mux setup.
- Add tests near existing AG-UI API tests, avoiding unrelated governance files unless necessary.

Endpoints:

```text
POST   /api/documents
GET    /api/documents
GET    /api/documents/{id}
PATCH  /api/documents/{id}
GET    /api/documents/{id}/versions
```

Acceptance:

- Identity-scoped access.
- Tags round-trip on create, list, detail, and patch.
- Unauthorized requests fail.

---

## Phase 2: Versioned Ingestion and Hashing

### Task 2.1: Add dual hash utility

**Files:**
- Create: `internal/documents/hash.go` or extend existing `internal/documents/ids.go`.
- Modify tests: `internal/documents/ids_test.go`, `internal/assets/service_test.go`.

Implementation contract:

```go
type ContentHashes struct {
	SHA1   string
	SHA256 string
}
```

Acceptance:

- Hashes are calculated in one stream pass.
- SHA-256 remains canonical.
- SHA-1 is stored for compatibility and duplicate reporting.

### Task 2.2: Create versions from finalized document assets

Acceptance:

- Finalizing a document asset can create or attach a document version.
- Same document and same SHA-256 is a no-op version when pipeline config is identical.
- Different SHA-256 creates a new version.

---

## Phase 3: Durable Jobs and Events

### Task 3.1: Replace fire-and-forget asset processing with durable job enqueue

Current fire-and-forget sites:

- `assets.Service.Finalize` calls `go s.process(...)`.
- `runtimeEmbeddingQueue.Enqueue` calls `go func()`.
- `documents.EmbeddingWorker.Enqueue` calls `go func()`.

Acceptance:

- Finalize creates a job row.
- Worker claims jobs with `FOR UPDATE SKIP LOCKED`.
- Attempt count, next attempt, lock owner, and errors persist.
- Failed jobs are visible and retryable.

### Task 3.2: Emit asset/document events

Acceptance:

- Every status transition appends an event.
- Events include trace/job/asset/document/version IDs.
- UI can render an ordered timeline.

---

## Phase 4: Graph and Embedding Lifecycle

### Task 4.1: Add Neo4j lifecycle properties and deletion operations

Acceptance:

- Upsert writes `identity_id`, `document_uuid`, `version_id`, `active`, `deleted_at`, `embedding_model`, `embedding_version`.
- Deactivate by `document_id`/`version_id`.
- Delete job can retry graph/vector cleanup.

### Task 4.2: Version-aware retrieval

Acceptance:

- Sparse, vector, hybrid, and graph-expanded retrieval filter active, non-deleted chunks.
- Document-scoped search can accept logical document ID and active version.
- Search/RAG does not return deleted documents in unit and live tests.

---

## Phase 5: Storage Cleanup

### Task 5.1: Add storage object ledger and immutable keys

Acceptance:

- Raw objects use versioned key prefixes.
- Derived artifacts have ledger rows.
- Existing asset keys remain readable during migration.

### Task 5.2: Add orphan dry-run and cleanup execution

Endpoints:

```text
GET  /api/storage/orphans
POST /api/storage/orphans/cleanup
```

Acceptance:

- Dry-run mutates nothing.
- Execute requires dry-run token and typed confirmation.
- Cleanup jobs are retry-safe.

---

## Phase 6: React Document Library

### Task 6.1: Add document API client and routes

Files:

- `web/src/documents/api.ts`
- `web/src/documents/types.ts`
- `web/src/documents/DocumentListPage.tsx`
- `web/src/documents/DocumentDetailPage.tsx`
- `web/src/documents/DocumentTagEditor.tsx`
- `web/src/main.tsx`

Acceptance:

- `/documents` lists documents with status, tags, file info, hash, and active version.
- Tag chips can filter the list.
- Detail page shows versions, events, errors, storage, and embeddings.

### Task 6.2: Add destructive flows

Acceptance:

- Soft delete confirmation.
- Hard delete typed confirmation.
- Reprocess/retry actions are pessimistic and visible in job timeline.

---

## Phase 7: Observability

Acceptance:

- Metrics from the spec exist for jobs, queue lag, embeddings, cleanup, and failures.
- Traces cover finalize, parse, chunk, sparse index, embed, vector index, delete, and cleanup.
- Structured logs include `identity_id`, `asset_id`, `document_id`, `version_id`, `job_id`, `stage`, `attempt`, `idempotency_key`, `trace_id`, and `error_code`.

---

## Phase 8: 10/10 Validation Gate

Run and record:

```powershell
go test -count=1 ./cmd/aura ./internal/assets ./internal/documents ./internal/agui
go test -race -count=1 ./cmd/aura ./internal/assets ./internal/documents ./internal/agui
cd web
npm run lint
npm run typecheck
npm test
npm run test:e2e
```

Live/operator gates:

```powershell
go test -tags document_ingest_live -run TestLiveDocumentIngestE2E ./internal/documents -count=1 -v
go test -tags graphrag_live -run TestGraphRAGLive ./internal/documents -count=1 -v
```

Manual acceptance:

- Upload new document.
- Upload duplicate document.
- Upload changed version.
- Edit title/tags/metadata.
- Reprocess.
- Delete with embeddings.
- Simulate Neo4j down during delete and retry.
- Dry-run orphan scan.
- Execute selected orphan cleanup.
- Prove deleted document is absent from sparse, vector, and GraphRAG retrieval.

The goal is complete only when every score in the spec's 10/10 rubric is backed by automated or recorded manual evidence.
