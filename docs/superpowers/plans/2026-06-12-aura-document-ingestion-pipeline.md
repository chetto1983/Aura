# Aura Document Ingestion Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to execute this plan.

**Goal:** Build Aura's native document ingestion pipeline for large PDF, XLSX, and DOCX files using the existing Neo4j MCP knowledge substrate. Ingestion must become fast-searchable immediately after extraction and sparse Neo4j upsert, while dense embeddings run in a bounded background lane.

**Architecture:** Extend the existing `internal/knowledge` Neo4j MCP layer with document/chunk metadata, add a document extraction/indexing service, persist ingestion job state in Postgres, expose a CLI and agent search tool, then route Telegram attachments into the same pipeline. Use the current MarkItDown sidecar as the extraction boundary and keep agent-memory MCP separate from document chunk storage.

**Tech Stack:** Go, Postgres/sqlc migrations, Neo4j 5.26, `mcp-neo4j-cypher`, existing `internal/knowledge` MCP client, Neo4j fulltext/vector indexes, existing `aura-llama-embed` embeddings sidecar, existing `aura-markitdown` Python sidecar.

---

## Context From Code Survey

Aura already has two Neo4j-related paths:

1. `internal/knowledge`
   - Native Go wrapper around the `mcp-neo4j-cypher` subprocess.
   - Raw MCP tools are `read_neo4j_cypher` and `write_neo4j_cypher`.
   - Schema DDL uses `knowledge.SchemaExecutor` with the official Neo4j Go driver because the MCP write tool wraps queries in transactions and Neo4j rejects some schema commands inside explicit transactions.
   - Existing migration `internal/knowledge/migrations/0001_init.cypher` creates:
     - `chunk_id` uniqueness constraint.
     - `chunk_embedding` vector index, 384 dimensions, cosine.
     - `chunk_text` fulltext index on `Chunk.text`.

2. `cmd/aura memory`
   - Operator path into the managed agent-memory MCP server.
   - Good for long-term conversational memories, entities, and facts.
   - Not suitable as the primary store for thousands of document chunks.

Document conversion currently lives in `internal/channels/telegram/documents.go`:

- Files are converted through the MarkItDown sidecar at `/convert`.
- Current tiers are documented as `<=5MB sync`, `5-50MB async`, `>50MB refused`.
- The actual code uses `>=` thresholds, so exactly 5 MiB becomes async and exactly 50 MiB is refused.
- `postConvert` builds the complete multipart request in a `bytes.Buffer`, which amplifies memory for large documents.

Spike evidence:

- Large Siemens PDF: 28.97 MiB, 830 pages, 1,171,929 chars, 1035 chunks, fast ingest 1.626s, retrieval p95 0.0017s, industrial score 90.4/100.
- XLSX automation file: 0.82 MiB, 11,627 row chunks, 105,455 nonempty cells, fast ingest 1.575s, retrieval p95 0.0021s, score 90.0/100.
- DOCX robot course: section-based chunking, 18 sections, 45 paragraphs, fast ingest 0.012s, retrieval p95 0.0006s, score 85.0/100.

The production rule is therefore:

- Extraction plus Neo4j fulltext upsert makes a document `searchable`.
- Dense embedding is a background enhancement.
- The user should never wait for all embeddings before retrieval starts working.

---

## Phase 1: Extend Neo4j Document Schema

### Task 1.1: Add Neo4j migration for documents and chunk metadata

Create `internal/knowledge/migrations/0002_documents.cypher`.

Content:

```cypher
CREATE CONSTRAINT document_id IF NOT EXISTS
FOR (d:Document)
REQUIRE d.id IS UNIQUE;

CREATE INDEX document_source_id IF NOT EXISTS
FOR (d:Document)
ON (d.source_id);

CREATE INDEX document_content_hash IF NOT EXISTS
FOR (d:Document)
ON (d.content_hash);

CREATE INDEX document_status IF NOT EXISTS
FOR (d:Document)
ON (d.status);

CREATE INDEX chunk_document_id IF NOT EXISTS
FOR (c:Chunk)
ON (c.document_id);

CREATE INDEX chunk_content_hash IF NOT EXISTS
FOR (c:Chunk)
ON (c.content_hash);

CREATE INDEX chunk_chunk_hash IF NOT EXISTS
FOR (c:Chunk)
ON (c.chunk_hash);
```

Keep the existing `chunk_id`, `chunk_text`, and `chunk_embedding` definitions in `0001_init.cypher`.

Expected node model:

```cypher
(:Document {
  id: string,
  source_id: string,
  source_kind: string,
  file_name: string,
  mime_type: string,
  size_bytes: integer,
  content_hash: string,
  title: string,
  status: "extracting" | "searchable" | "embedding" | "complete" | "failed",
  chunk_count: integer,
  embedded_chunk_count: integer,
  created_at: datetime,
  updated_at: datetime
})

(:Chunk {
  id: string,
  document_id: string,
  source_id: string,
  content_hash: string,
  chunk_hash: string,
  chunk_index: integer,
  chunk_count: integer,
  kind: string,
  text: string,
  locator_json: string,
  heading_path: list<string>,
  embedding: list<float>,
  embedded_at: datetime,
  created_at: datetime,
  updated_at: datetime
})

(:Document)-[:HAS_CHUNK]->(:Chunk)
```

Document id format:

```text
doc_<sha256(content_hash + ":" + source_id)>[0:32]
```

Chunk id format:

```text
chunk_<document_id>_<zero-padded chunk_index>
```

Chunk hash format:

```text
sha256(normalized chunk text + locator json)
```

### Task 1.2: Add schema migration tests

Modify or add tests under `internal/knowledge` to prove the new migration is loaded and does not remove existing indexes.

Add a unit-level migration ordering test if one does not already exist:

```go
func TestMigrationsIncludeDocumentSchemaAfterInitialChunkSchema(t *testing.T) {
    migrations := knowledge.MustLoadMigrationsForTest(t)

    names := make([]string, 0, len(migrations))
    for _, migration := range migrations {
        names = append(names, migration.Name)
    }

    require.Contains(t, names, "0001_init.cypher")
    require.Contains(t, names, "0002_documents.cypher")
    require.Less(t,
        slices.Index(names, "0001_init.cypher"),
        slices.Index(names, "0002_documents.cypher"),
    )
}
```

If `MustLoadMigrationsForTest` does not exist, expose a test-only helper in `internal/knowledge/migrate_test.go` that calls the existing migration loader.

Run:

```powershell
go test ./internal/knowledge
```

---

## Phase 2: Add Postgres Job Control Plane

### Task 2.1: Create `document_ingest_jobs` migration

Create the next numbered migration in `internal/db/migrations`.

Up migration:

```sql
CREATE TABLE IF NOT EXISTS document_ingest_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id TEXT NOT NULL,
    source_kind TEXT NOT NULL,
    document_id TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    original_path TEXT NOT NULL,
    file_name TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    status TEXT NOT NULL CHECK (
        status IN (
            'accepted',
            'extracting',
            'searchable',
            'embedding',
            'complete',
            'failed',
            'refused',
            'canceled'
        )
    ),
    sparse_chunks INTEGER NOT NULL DEFAULT 0 CHECK (sparse_chunks >= 0),
    embedded_chunks INTEGER NOT NULL DEFAULT 0 CHECK (embedded_chunks >= 0),
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    searchable_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS document_ingest_jobs_source_document_hash_idx
ON document_ingest_jobs (source_id, document_id, content_hash);

CREATE INDEX IF NOT EXISTS document_ingest_jobs_status_created_idx
ON document_ingest_jobs (status, created_at DESC);

CREATE INDEX IF NOT EXISTS document_ingest_jobs_document_id_idx
ON document_ingest_jobs (document_id);
```

Down migration:

```sql
DROP TABLE IF EXISTS document_ingest_jobs;
```

### Task 2.2: Add sqlc queries

Create `internal/db/queries/document_ingest_jobs.sql`.

Queries:

```sql
-- name: CreateDocumentIngestJob :one
INSERT INTO document_ingest_jobs (
    source_id,
    source_kind,
    document_id,
    content_hash,
    original_path,
    file_name,
    mime_type,
    size_bytes,
    status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (source_id, document_id, content_hash)
DO UPDATE SET
    updated_at = now()
RETURNING *;

-- name: GetDocumentIngestJob :one
SELECT *
FROM document_ingest_jobs
WHERE id = $1;

-- name: GetDocumentIngestJobByDocumentID :one
SELECT *
FROM document_ingest_jobs
WHERE document_id = $1
ORDER BY created_at DESC
LIMIT 1;

-- name: UpdateDocumentIngestJobStatus :one
UPDATE document_ingest_jobs
SET
    status = $2,
    error = $3,
    updated_at = now(),
    searchable_at = CASE WHEN $2 = 'searchable' THEN now() ELSE searchable_at END,
    completed_at = CASE WHEN $2 = 'complete' THEN now() ELSE completed_at END
WHERE id = $1
RETURNING *;

-- name: UpdateDocumentIngestJobProgress :one
UPDATE document_ingest_jobs
SET
    status = $2,
    sparse_chunks = $3,
    embedded_chunks = $4,
    updated_at = now(),
    searchable_at = CASE WHEN $2 = 'searchable' AND searchable_at IS NULL THEN now() ELSE searchable_at END,
    completed_at = CASE WHEN $2 = 'complete' AND completed_at IS NULL THEN now() ELSE completed_at END
WHERE id = $1
RETURNING *;

-- name: ListRecentDocumentIngestJobs :many
SELECT *
FROM document_ingest_jobs
ORDER BY created_at DESC
LIMIT $1;
```

Regenerate DB code using the repository's existing sqlc command. If the repo uses direct sqlc:

```powershell
sqlc generate
```

If the repo wraps sqlc in `go generate`, use:

```powershell
go generate ./internal/db/...
```

Run:

```powershell
go test ./internal/db/...
```

---

## Phase 3: Add Document Domain Package

### Task 3.1: Create package skeleton

Create:

```text
internal/documents/types.go
internal/documents/ids.go
internal/documents/extractor.go
internal/documents/extract_client.go
internal/documents/indexer.go
internal/documents/search.go
internal/documents/service.go
internal/documents/embedder.go
internal/documents/worker.go
```

Core types in `internal/documents/types.go`:

```go
package documents

import "time"

type JobStatus string

const (
    JobAccepted   JobStatus = "accepted"
    JobExtracting JobStatus = "extracting"
    JobSearchable JobStatus = "searchable"
    JobEmbedding  JobStatus = "embedding"
    JobComplete   JobStatus = "complete"
    JobFailed     JobStatus = "failed"
    JobRefused    JobStatus = "refused"
    JobCanceled   JobStatus = "canceled"
)

type IngestRequest struct {
    SourceID     string
    SourceKind   string
    OriginalPath string
    FileName     string
    MIMEType     string
    SizeBytes    int64
}

type ExtractedDocument struct {
    ID          string
    SourceID    string
    SourceKind  string
    FileName    string
    MIMEType    string
    SizeBytes   int64
    ContentHash string
    Title       string
    Chunks      []Chunk
    CreatedAt   time.Time
}

type Chunk struct {
    ID          string
    DocumentID  string
    SourceID    string
    ContentHash string
    ChunkHash   string
    ChunkIndex  int
    ChunkCount  int
    Kind        string
    Text        string
    Locator     Locator
    HeadingPath []string
}

type Locator struct {
    Page      int    `json:"page,omitempty"`
    Sheet     string `json:"sheet,omitempty"`
    RowStart  int    `json:"row_start,omitempty"`
    RowEnd    int    `json:"row_end,omitempty"`
    Section   string `json:"section,omitempty"`
    Paragraph int    `json:"paragraph,omitempty"`
}

type SearchHit struct {
    DocumentID  string
    ChunkID     string
    FileName    string
    Score       float64
    Text        string
    Locator     Locator
    HeadingPath []string
}
```

### Task 3.2: Implement stable ids and hashing

In `internal/documents/ids.go`:

- Normalize chunk text by trimming whitespace and collapsing repeated whitespace.
- Compute content hash while streaming file bytes.
- Compute document id from content hash and source id.
- Compute chunk id from document id and index.
- Compute chunk hash from normalized text plus locator JSON.

Tests:

```go
func TestDocumentIDStableForSameContentAndSource(t *testing.T)
func TestDocumentIDDifferentForDifferentSource(t *testing.T)
func TestChunkHashUsesLocator(t *testing.T)
func TestChunkIDIsZeroPaddedAndOrdered(t *testing.T)
```

Run:

```powershell
go test ./internal/documents
```

---

## Phase 4: Upgrade MarkItDown Sidecar Into Extractor Boundary

### Task 4.1: Add `/extract` endpoint to the sidecar

Modify the existing MarkItDown sidecar under the Docker/sidecar directory used by `aura-markitdown`.

Keep `/convert` working for compatibility.

Add:

```http
POST /extract
Content-Type: multipart/form-data
```

Response:

```json
{
  "title": "G220 operation instructions",
  "mime_type": "application/pdf",
  "stats": {
    "pages": 830,
    "sheets": 0,
    "sections": 0,
    "chunks": 1035,
    "characters": 1171929
  },
  "chunks": [
    {
      "kind": "page",
      "text": "normalized text",
      "locator": {"page": 1},
      "heading_path": []
    }
  ]
}
```

Extraction rules:

- PDF:
  - Use page-aware extraction.
  - Chunk by page first.
  - Split pages larger than 1800-2400 tokens into subchunks while preserving page locator.
  - Preserve headings when available.
- XLSX:
  - Chunk by row groups.
  - Include sheet name, row range, and visible cell values.
  - Include formulas if present, but mark them as formulas.
  - Skip fully empty rows.
- DOCX:
  - Chunk by sections/headings first.
  - Preserve heading path.
  - Fallback to paragraph batches when no headings exist.

Add dependencies in the sidecar image if missing:

```text
PyMuPDF
openpyxl
python-docx
```

### Task 4.2: Stream uploads from Go instead of buffering full multipart bodies

Create `internal/documents/extract_client.go`.

Implementation requirements:

- Accept `io.Reader` plus metadata, or a path plus metadata.
- Use `io.Pipe` and `multipart.Writer` for streaming request bodies.
- Do not build full multipart payloads in memory.
- Apply request timeout from config.
- Decode JSON into `ExtractorResponse`.
- Validate that every returned chunk has non-empty text.

Shape:

```go
type ExtractClient struct {
    BaseURL string
    Client  *http.Client
}

func (c *ExtractClient) ExtractFile(ctx context.Context, path string, req IngestRequest) (*ExtractorResponse, error)
```

Tests:

```go
func TestExtractClientStreamsMultipartWithoutBufferingFile(t *testing.T)
func TestExtractClientRejectsEmptyChunkText(t *testing.T)
func TestExtractClientPropagatesSidecarError(t *testing.T)
```

Run:

```powershell
go test ./internal/documents
```

---

## Phase 5: Implement Neo4j Sparse Indexer

### Task 5.1: Add indexer using `internal/knowledge.Client`

Implement `internal/documents/indexer.go`.

Do not open a separate Neo4j driver for normal data writes. Use the existing MCP client:

```go
type KnowledgeClient interface {
    Read(ctx context.Context, query string, params map[string]any) ([]map[string]any, error)
    Write(ctx context.Context, query string, params map[string]any) ([]map[string]any, error)
}
```

Sparse upsert query:

```cypher
MERGE (d:Document {id: $document.id})
SET
  d.source_id = $document.source_id,
  d.source_kind = $document.source_kind,
  d.file_name = $document.file_name,
  d.mime_type = $document.mime_type,
  d.size_bytes = $document.size_bytes,
  d.content_hash = $document.content_hash,
  d.title = $document.title,
  d.status = "searchable",
  d.chunk_count = $document.chunk_count,
  d.embedded_chunk_count = coalesce(d.embedded_chunk_count, 0),
  d.updated_at = datetime(),
  d.created_at = coalesce(d.created_at, datetime())
WITH d
UNWIND $chunks AS chunk
MERGE (c:Chunk {id: chunk.id})
SET
  c.document_id = chunk.document_id,
  c.source_id = chunk.source_id,
  c.content_hash = chunk.content_hash,
  c.chunk_hash = chunk.chunk_hash,
  c.chunk_index = chunk.chunk_index,
  c.chunk_count = chunk.chunk_count,
  c.kind = chunk.kind,
  c.text = chunk.text,
  c.locator_json = chunk.locator_json,
  c.heading_path = chunk.heading_path,
  c.updated_at = datetime(),
  c.created_at = coalesce(c.created_at, datetime())
MERGE (d)-[:HAS_CHUNK]->(c)
RETURN count(c) AS chunks
```

Batching:

- Use batches of 250 chunks for sparse writes.
- Fail the job if any batch fails.
- Mark the document `searchable` only after all sparse chunks are written.

Tests with a fake `KnowledgeClient`:

```go
func TestIndexerBatchesChunks(t *testing.T)
func TestIndexerSetsDocumentSearchableAfterChunkWrites(t *testing.T)
func TestIndexerStopsOnWriteFailure(t *testing.T)
func TestIndexerStoresLocatorAsJSON(t *testing.T)
```

### Task 5.2: Add sparse search

Implement `internal/documents/search.go`.

Cypher:

```cypher
CALL db.index.fulltext.queryNodes('chunk_text', $query, {limit: $limit * 3})
YIELD node, score
MATCH (d:Document {id: node.document_id})
WHERE ($document_id = "" OR d.id = $document_id)
RETURN
  d.id AS document_id,
  d.file_name AS file_name,
  node.id AS chunk_id,
  node.text AS text,
  node.locator_json AS locator_json,
  node.heading_path AS heading_path,
  score AS score
ORDER BY score DESC
LIMIT $limit
```

Query sanitation:

- Trim query.
- Remove unsupported Lucene control characters unless they are explicitly escaped.
- Reject empty query.
- Default limit: 8.
- Maximum limit: 20.

Tests:

```go
func TestSearchRejectsEmptyQuery(t *testing.T)
func TestSearchCapsLimit(t *testing.T)
func TestSearchFiltersByDocumentID(t *testing.T)
func TestSearchDecodesLocatorJSON(t *testing.T)
```

Run:

```powershell
go test ./internal/documents
```

---

## Phase 6: Implement Ingestion Service

### Task 6.1: Add service orchestration

Implement `internal/documents/service.go`.

Flow:

1. Validate file exists, supported extension/MIME, and size cap.
2. Compute content hash by streaming file bytes.
3. Create or reuse Postgres job in `accepted`.
4. Mark job `extracting`.
5. Call extraction sidecar.
6. Build stable document/chunk ids.
7. Upsert sparse chunks to Neo4j through MCP.
8. Mark job `searchable`.
9. Queue background embeddings.
10. Return the job immediately after the document is searchable.

Size policy:

- `<= 50 MiB`: accepted.
- `> 50 MiB`: refused by default with actionable error.
- Keep the limit configurable with a sane default.

Service shape:

```go
type Service struct {
    Jobs      JobStore
    Extractor Extractor
    Indexer   Indexer
    Embedder  EmbedQueue
    Clock     Clock
    MaxBytes  int64
}

func (s *Service) IngestPath(ctx context.Context, req IngestRequest, path string) (*Job, error)
func (s *Service) Search(ctx context.Context, req SearchRequest) ([]SearchHit, error)
func (s *Service) GetJob(ctx context.Context, id uuid.UUID) (*Job, error)
```

Tests:

```go
func TestServiceMakesDocumentSearchableBeforeEmbedding(t *testing.T)
func TestServiceRefusesFilesOverConfiguredLimit(t *testing.T)
func TestServiceReusesExistingJobForSameSourceDocumentHash(t *testing.T)
func TestServiceMarksFailedWhenExtractionFails(t *testing.T)
func TestServiceMarksFailedWhenSparseIndexingFails(t *testing.T)
```

Run:

```powershell
go test ./internal/documents
```

---

## Phase 7: Add Background Dense Embedding Lane

### Task 7.1: Implement embedding client

Create `internal/documents/embedder.go`.

Use existing `aura-llama-embed` endpoint:

```http
POST /v1/embeddings
```

Requirements:

- Batch size: 32 chunks.
- Timeout per batch: configurable, default 60s.
- Verify embedding dimension equals `knowledge.DefaultEmbedDimensions` or configured `EmbedDimensions`.
- Do not embed chunks that already have `embedded_at`.

### Task 7.2: Implement Neo4j vector upsert

Write embeddings through the existing MCP client.

Cypher:

```cypher
UNWIND $chunks AS chunk
MATCH (c:Chunk {id: chunk.id})
SET
  c.embedding = chunk.embedding,
  c.embedded_at = datetime()
WITH c
MATCH (d:Document {id: c.document_id})
SET
  d.embedded_chunk_count = coalesce(d.embedded_chunk_count, 0) + 1,
  d.status = CASE
    WHEN coalesce(d.embedded_chunk_count, 0) + 1 >= d.chunk_count THEN "complete"
    ELSE "embedding"
  END,
  d.updated_at = datetime()
RETURN count(c) AS embedded
```

If direct vector property setting through MCP has a Neo4j type issue, use the same approach as `internal/knowledge/smoke_test.go` proves for vector data, but keep this fallback isolated behind the indexer interface.

### Task 7.3: Implement worker

Create `internal/documents/worker.go`.

Worker behavior:

- Bounded concurrency: 1 document worker by default.
- Bounded batch concurrency: 1 embedding request at a time by default.
- Retries: 3 attempts per batch with exponential backoff.
- Job status:
  - `searchable` after sparse upsert.
  - `embedding` while dense lane is running.
  - `complete` when all chunks have `embedded_at`.
  - `failed` only when extraction or sparse upsert fails. Embedding failures should keep the document searchable and record error/progress.

Tests:

```go
func TestEmbeddingWorkerDoesNotBlockSearchableStatus(t *testing.T)
func TestEmbeddingWorkerRetriesBatch(t *testing.T)
func TestEmbeddingWorkerRecordsPartialProgress(t *testing.T)
func TestEmbeddingWorkerMarksCompleteAfterAllChunks(t *testing.T)
```

Run:

```powershell
go test ./internal/documents
```

---

## Phase 8: Add `aura docs` CLI

### Task 8.1: Add command routing

Modify `cmd/aura/main.go` to route:

```text
aura docs ingest <path> [--source-id cli] [--source-kind local]
aura docs search <query> [--document-id <id>] [--limit 8]
aura docs status <job-id>
aura docs list [--limit 20]
```

Create `cmd/aura/docs.go`.

Implementation:

- Load existing config.
- Open Postgres pool for job store.
- Open `internal/knowledge` MCP client for data writes/search.
- Open extraction client using the MarkItDown sidecar URL.
- Call `documents.Service`.
- Print compact JSON by default so E2E timings can parse it.

Example output for ingest:

```json
{
  "job_id": "uuid",
  "document_id": "doc_abc123",
  "status": "searchable",
  "file_name": "G220_op_instr_0824_en-US.pdf",
  "chunks": 1035,
  "ingest_ms": 1626,
  "embedding_status": "queued"
}
```

Example output for search:

```json
{
  "query": "safety interlock reset",
  "hits": [
    {
      "document_id": "doc_abc123",
      "chunk_id": "chunk_doc_abc123_000042",
      "file_name": "G220_op_instr_0824_en-US.pdf",
      "score": 12.44,
      "locator": {"page": 57},
      "text": "..."
    }
  ],
  "retrieval_ms": 2
}
```

Tests:

```go
func TestDocsCommandRequiresSubcommand(t *testing.T)
func TestDocsIngestPrintsSearchableJob(t *testing.T)
func TestDocsSearchPrintsHits(t *testing.T)
func TestDocsStatusPrintsJob(t *testing.T)
```

Run:

```powershell
go test ./cmd/aura
```

---

## Phase 9: Add Agent Document Search Tool

### Task 9.1: Implement tool

Create `internal/agent/tools/document_search.go` or follow the existing local tool registration pattern if document search tools live elsewhere.

Tool name:

```text
document_search
```

Description:

```text
Search indexed user documents by keyword/fulltext and return cited chunks with locators.
```

Input schema:

```json
{
  "type": "object",
  "properties": {
    "query": {"type": "string"},
    "document_id": {"type": "string"},
    "limit": {"type": "integer", "minimum": 1, "maximum": 20}
  },
  "required": ["query"]
}
```

Output:

```json
{
  "hits": [
    {
      "document_id": "doc_abc123",
      "chunk_id": "chunk_doc_abc123_000042",
      "file_name": "manual.pdf",
      "locator": {"page": 57},
      "score": 12.44,
      "text": "..."
    }
  ]
}
```

Registration:

- Mark as deferred if Aura's current tool registry supports deferred tools.
- Keep results citeable and concise.
- Never return entire documents through the tool.

Tests:

```go
func TestDocumentSearchToolValidatesQuery(t *testing.T)
func TestDocumentSearchToolCapsLimit(t *testing.T)
func TestDocumentSearchToolReturnsCitedHits(t *testing.T)
func TestDocumentSearchToolPropagatesSearchError(t *testing.T)
```

Run:

```powershell
go test ./internal/agent/tools
```

---

## Phase 10: Route Telegram Documents Into Pipeline

### Task 10.1: Replace conversion-only flow with ingestion job flow

Modify `internal/channels/telegram/documents.go` and the Telegram dispatcher call site.

Behavior:

- For supported documents, call `documents.Service.IngestPath`.
- Send an immediate "accepted" message for async paths.
- Send a "searchable" message when sparse indexing completes.
- Do not inject the entire converted markdown into the conversation.
- For unsupported or refused files, keep current friendly refusal behavior.

User-facing messages:

```text
Ho indicizzato "G220_op_instr_0824_en-US.pdf" in 1.6s. Puoi farmi domande sul documento.
```

For background embedding:

```text
Ricerca testuale pronta. Sto completando gli embedding in background.
```

### Task 10.2: Fix exact threshold drift

Current code documents `<=5MB sync`, `5-50MB async`, `>50MB refused`, but uses `>=` for thresholds.

Define exact policy:

```go
const (
    syncMaxBytes   = 5 << 20
    ingestMaxBytes = 50 << 20
)
```

Rules:

- `size <= syncMaxBytes`: may ingest synchronously until searchable.
- `syncMaxBytes < size <= ingestMaxBytes`: accepted and processed async.
- `size > ingestMaxBytes`: refused.

Tests:

```go
func TestDocumentTierAllowsExactlyFiveMiBSync(t *testing.T)
func TestDocumentTierAllowsExactlyFiftyMiBAsync(t *testing.T)
func TestDocumentTierRefusesOverFiftyMiB(t *testing.T)
```

Run:

```powershell
go test ./internal/channels/telegram
```

---

## Phase 11: Add E2E Performance Harness

### Task 11.1: Create optional live E2E test

Create `internal/documents/document_ingest_live_test.go` with build tag:

```go
//go:build document_ingest_live
```

Environment variables:

```text
AURA_DOC_TEST_PDF
AURA_DOC_TEST_XLSX
AURA_DOC_TEST_DOCX
AURA_DOC_TEST_QUERY
```

Test flow:

1. Reset/migrate Neo4j only when `AURA_DOC_TEST_RESET=1`.
2. Ingest each provided path.
3. Measure time until `searchable`.
4. Run 5 fixed retrieval queries per file type.
5. Measure p50/p95 retrieval latency.
6. Compute industrial score.

Industrial score formula:

```text
score = 100
  - extraction_fail_penalty
  - searchable_latency_penalty
  - retrieval_latency_penalty
  - missing_locator_penalty
  - missing_expected_hit_penalty
```

Pass thresholds:

- PDF fast-searchable: <= 3s for the known Siemens 29 MiB PDF on local dev hardware.
- XLSX fast-searchable: <= 3s for the known automation XLSX.
- DOCX fast-searchable: <= 1s for the known robot course DOCX.
- Retrieval p95: <= 50ms local.
- Industrial score: >= 85.

Command for the known user files:

```powershell
$env:AURA_DOC_TEST_PDF='C:\Users\Davide\OneDrive - Sonepar\Documenti\G220_op_instr_0824_en-US.pdf'
$env:AURA_DOC_TEST_XLSX='C:\Users\Davide\Desktop\Gestito Linea Automazione 2025.xlsx'
$env:AURA_DOC_TEST_DOCX='C:\Users\Davide\OneDrive - Sonepar\Documenti\Corso Robot\Corso Base Robot.docx'
$env:AURA_DOC_TEST_RESET='1'
go test -tags document_ingest_live ./internal/documents -run TestLiveDocumentIngestE2E -count=1 -v
```

### Task 11.2: Add CLI benchmark command

Add:

```text
aura docs bench <path> --query "<query>"
```

Output:

```json
{
  "file": "G220_op_instr_0824_en-US.pdf",
  "size_bytes": 30374364,
  "chunks": 1035,
  "time_to_searchable_ms": 1626,
  "retrieval_p95_ms": 2,
  "industrial_score": 90.4
}
```

Run:

```powershell
go test ./cmd/aura
```

---

## Phase 12: Documentation And Operator Runbook

### Task 12.1: Add documentation

Create `docs/document-ingestion.md`.

Include:

- Architecture diagram in text form.
- Difference between `internal/knowledge` Neo4j MCP and agent-memory MCP.
- Status lifecycle.
- Supported file types.
- Size limits.
- CLI examples.
- Telegram behavior.
- E2E benchmark commands.
- Troubleshooting:
  - Neo4j not reachable.
  - MCP binary missing.
  - MarkItDown sidecar down.
  - Embedding dimension mismatch.
  - Fulltext index missing.

### Task 12.2: Update audit index

Update `docs/audit/audit-index.json` only if the repository uses it to track new architecture notes.

Add an entry for `docs/document-ingestion.md` and the live E2E results after the harness passes.

---

## Verification Checklist

Run in order:

```powershell
go test ./internal/knowledge
go test ./internal/documents
go test ./internal/channels/telegram
go test ./internal/agent/tools
go test ./cmd/aura
```

Start local dependencies:

```powershell
docker compose up -d aura-neo4j aura-llama-embed aura-markitdown
```

Apply schema:

```powershell
go run ./cmd/aura neo4j migrate
go run ./cmd/aura neo4j status
```

Manual ingest smoke:

```powershell
go run ./cmd/aura docs ingest "C:\Users\Davide\OneDrive - Sonepar\Documenti\G220_op_instr_0824_en-US.pdf"
go run ./cmd/aura docs search "safety interlock reset" --limit 5
```

Live E2E:

```powershell
$env:AURA_DOC_TEST_PDF='C:\Users\Davide\OneDrive - Sonepar\Documenti\G220_op_instr_0824_en-US.pdf'
$env:AURA_DOC_TEST_XLSX='C:\Users\Davide\Desktop\Gestito Linea Automazione 2025.xlsx'
$env:AURA_DOC_TEST_DOCX='C:\Users\Davide\OneDrive - Sonepar\Documenti\Corso Robot\Corso Base Robot.docx'
$env:AURA_DOC_TEST_RESET='1'
go test -tags document_ingest_live ./internal/documents -run TestLiveDocumentIngestE2E -count=1 -v
```

Expected final local report:

```text
PDF  searchable <= 3s, retrieval p95 <= 50ms, score >= 85
XLSX searchable <= 3s, retrieval p95 <= 50ms, score >= 85
DOCX searchable <= 1s, retrieval p95 <= 50ms, score >= 85
```

---

## Execution Order

1. Phase 1: Neo4j schema.
2. Phase 2: Postgres job state.
3. Phase 3: document domain package.
4. Phase 4: sidecar `/extract` plus streaming Go client.
5. Phase 5: sparse Neo4j indexer and search.
6. Phase 6: service orchestration.
7. Phase 8: CLI, before Telegram, so local E2E is easy.
8. Phase 11: performance harness against the three known files.
9. Phase 7: dense embedding worker.
10. Phase 9: agent search tool.
11. Phase 10: Telegram integration.
12. Phase 12: docs and audit index.

This order intentionally makes the first useful production milestone: `aura docs ingest` plus `aura docs search` returning cited Neo4j fulltext results before dense embeddings exist.

---

## Definition Of Done

- `aura docs ingest <path>` makes PDF, XLSX, and DOCX files searchable through Neo4j MCP.
- Search returns cited chunks with page/sheet/row/section locators.
- Dense embeddings run after sparse searchability and do not block user retrieval.
- Telegram attachments use the same ingestion service instead of conversation-size markdown injection.
- Agent document search tool can retrieve indexed document chunks.
- Live E2E reports time to searchable, retrieval p95, and industrial score for the three known user files.
- All non-live unit tests pass.
- Live E2E reaches score >= 85 for PDF, XLSX, and DOCX on local hardware.
