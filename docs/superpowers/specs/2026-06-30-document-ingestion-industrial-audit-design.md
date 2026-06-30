# Design - Industrial document ingestion audit

- **Date:** 2026-06-30
- **Status:** Draft (awaiting user review -> writing-plans)
- **Author:** Codex
- **Scope:** Go backend, React frontend, Postgres, Garage/S3 object storage, Neo4j document graph, embedding lifecycle, tests, CI, and operator UI.
- **Repository root:** `D:\Aura`. The earlier `D:\tmp` path is treated as an example path only.
- **Approved approach:** Approach A, evidence-first industrial audit and target design.

## Executive Summary

Aura already has a real document ingestion foundation. It can accept web and Telegram assets, store original bytes in a Garage/S3-compatible object store, track asset state in Postgres, extract documents through the MarkItDown sidecar, index `Document` and `Chunk` nodes in Neo4j, and run asynchronous embeddings. The current implementation is not a toy.

The gap is lifecycle completeness. Current workers are goroutine-driven rather than durable. Delete is a soft asset delete plus best-effort object delete, but it does not remove or deactivate Neo4j documents, chunks, or embeddings. There is no document versioning plane, no update flow, no durable outbox, no dead-letter queue, no orphan scanner, no stale vector cleanup, and no admin UI for ingestion management. The schema has `aura.asset_events`, but runtime code does not emit a real transition ledger.

The "garage bucket" phrase is a storage-bucket concept, not a garbage-bucket concept. The code and docs point to Garage as the S3-compatible object store backing `AURA_OBJECTSTORE_BUCKET`; there is no current garbage, archive, failed-ingestion, or temporary bucket implementation.

Hashing is currently SHA-256, not SHA-1. The code calculates SHA-256 for raw asset bytes and document IDs. A production plan can add SHA-1 for compatibility with the requested audit language, but SHA-256 should remain the canonical integrity and dedupe hash.

To reach 10/10 readiness, Aura needs a versioned document control plane, immutable object keys, durable idempotent workers, graph/vector lifecycle filters, retry-safe delete jobs, dry-run cleanup tooling, a document library/admin UI, and E2E tests that prove update, delete, retry, worker crash recovery, and stale-search prevention.

## Evidence Base

Local evidence reviewed:

- Asset guide: [`docs/asset-pipeline.md`](../../../docs/asset-pipeline.md)
- Document guide: [`docs/document-ingestion.md`](../../../docs/document-ingestion.md)
- Asset service: [`internal/assets/service.go`](../../../internal/assets/service.go)
- Asset store: [`internal/assets/store.go`](../../../internal/assets/store.go)
- Asset schema: [`internal/db/migrations/0020_assets.up.sql`](../../../internal/db/migrations/0020_assets.up.sql)
- Object store contract: [`internal/objectstore/types.go`](../../../internal/objectstore/types.go)
- Document service: [`internal/documents/service.go`](../../../internal/documents/service.go)
- Embedding worker: [`internal/documents/worker.go`](../../../internal/documents/worker.go)
- Neo4j indexer/search/retrieval: [`internal/documents/indexer.go`](../../../internal/documents/indexer.go), [`internal/documents/search.go`](../../../internal/documents/search.go), [`internal/documents/retrieve.go`](../../../internal/documents/retrieve.go)
- Neo4j document migration: [`internal/knowledge/migrations/0002_documents.cypher`](../../../internal/knowledge/migrations/0002_documents.cypher)
- Asset API routes: [`internal/agui/assets_api.go`](../../../internal/agui/assets_api.go)
- Runtime wiring: [`cmd/aura/docs.go`](../../../cmd/aura/docs.go), [`cmd/aura/document_processor_wiring.go`](../../../cmd/aura/document_processor_wiring.go)
- Web attachment types/upload hook: [`web/src/chat/attachments/types.ts`](../../../web/src/chat/attachments/types.ts), [`web/src/chat/attachments/useAttachmentUploads.ts`](../../../web/src/chat/attachments/useAttachmentUploads.ts)
- Browser upload E2E: [`web/e2e/assets.spec.ts`](../../../web/e2e/assets.spec.ts)
- CI workflow: [`.github/workflows/ci.yml`](../../../.github/workflows/ci.yml)

Primary external references to carry into implementation planning:

- AWS Builders' Library, retry-safe idempotent APIs: <https://aws.amazon.com/builders-library/making-retries-safe-with-idempotent-APIs/>
- AWS Prescriptive Guidance, transactional outbox: <https://docs.aws.amazon.com/prescriptive-guidance/latest/cloud-design-patterns/transactional-outbox.html>
- Amazon S3 presigned upload: <https://docs.aws.amazon.com/AmazonS3/latest/userguide/PresignedUrlUploadObject.html>
- Amazon S3 object keys: <https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-keys.html>
- Amazon S3 lifecycle expiration: <https://docs.aws.amazon.com/AmazonS3/latest/userguide/lifecycle-expire-general-considerations.html>
- OWASP File Upload Cheat Sheet: <https://cheatsheetseries.owasp.org/cheatsheets/File_Upload_Cheat_Sheet.html>
- PostgreSQL `FOR UPDATE SKIP LOCKED`: <https://www.postgresql.org/docs/current/sql-select.html>
- PostgreSQL `INSERT ... ON CONFLICT`: <https://www.postgresql.org/docs/current/sql-insert.html>
- Neo4j vector indexes: <https://neo4j.com/docs/cypher-manual/current/indexes/semantic-indexes/vector-indexes/>
- Neo4j fulltext indexes: <https://neo4j.com/docs/cypher-manual/current/indexes/semantic-indexes/full-text-indexes/>
- Neo4j `DELETE`: <https://neo4j.com/docs/cypher-manual/current/clauses/delete/>
- Neo4j GraphRAG Python repository: <https://github.com/neo4j/neo4j-graphrag-python>
- Neo4j GraphRAG `VectorRetriever`: <https://github.com/neo4j/neo4j-graphrag-python/blob/main/src/neo4j_graphrag/retrievers/vector.py>
- Neo4j GraphRAG `HybridRetriever`: <https://github.com/neo4j/neo4j-graphrag-python/blob/main/src/neo4j_graphrag/retrievers/hybrid.py>
- Neo4j GraphRAG filters: <https://github.com/neo4j/neo4j-graphrag-python/blob/main/src/neo4j_graphrag/filters.py>
- Neo4j GraphRAG pipeline events: <https://github.com/neo4j/neo4j-graphrag-python/blob/main/src/neo4j_graphrag/experimental/pipeline/pipeline.py>
- OpenTelemetry signals: <https://opentelemetry.io/docs/concepts/signals/>

## Current-State Audit

| Area | Current implementation | Status | Evidence | Risk | Recommendation |
| --- | --- | --- | --- | --- | --- |
| Ingestion flow | Web creates an asset, uploads to object storage, finalizes, hashes/sniffs, then dispatches a processor. Documents call `documents.Service.IngestPath`, become sparse-searchable, then embeddings run later. | Implemented | `Service.Presign`, `Finalize`, `processAsset`; `documents.Service.IngestPath`; `docs/asset-pipeline.md`; `docs/document-ingestion.md` | Good foundation, but the async part is volatile. | Preserve the flow, but move processing into durable jobs. |
| Backend architecture | `internal/assets`, `internal/objectstore`, and `internal/documents` are separated. AG-UI exposes asset endpoints. | Partially implemented | `internal/agui/assets_api.go`, `cmd/aura/document_processor_wiring.go` | No first-class document management API beyond assets and CLI docs. | Add `/api/documents` and `/api/ingestion/jobs` planes instead of overloading assets. |
| Frontend architecture | Chat attachments support upload, polling, remove, and ready asset IDs. | Partially implemented | `web/src/chat/attachments/*` | No document library, detail, version, cleanup, or admin workflow. | Add a real `/documents` or `/library` operational surface. |
| Database model | `aura.assets`, `aura.asset_events`, and `aura.document_ingest_jobs` exist. | Partially implemented | `0020_assets.up.sql`, `0015_document_ingest_jobs.up.sql` | Tables do not model document versions, durable worker claims, vector deletes, or cleanup jobs. | Add versioned documents, storage objects, durable jobs, events, and delete jobs. |
| Asset events | Schema and sqlc methods exist. | Risky | `aura.asset_events`; `InsertAssetEvent` in generated sqlc | Runtime store methods do not emit events, so the ledger is mostly aspirational. | Wrap status changes in transactions that append events. |
| Object storage and bucket model | One S3-compatible object bucket stores originals. Keys are `identity/{identityID}/asset/{assetID}/original`. | Partially implemented | `objectstore.AssetKey`; `AURA_OBJECTSTORE_BUCKET` docs | Keys are not content-addressed or versioned; no temp/archive/failed/garbage lifecycle. | Use versioned immutable prefixes and a `storage_objects` ledger. |
| Garage bucket terminology | Garage is the S3-compatible storage backend. | Implemented | `docs/asset-pipeline.md`, objectstore config | The word "garage" can be confused with "garbage"; code shows storage bucket only. | Document this conclusion and use "object bucket" in future specs. |
| Hash behavior | Assets and documents compute SHA-256. `DocumentID` is derived from content hash plus source ID. | Partially implemented | `hashAndSniff` uses `crypto/sha256`; `documents/ids.go` | SHA-1 is absent despite requested audit target. Dedupe is only indexed, not operationalized. | Store `sha256` as canonical, add `sha1` for compatibility if needed. |
| Create document | Web can create assets and documents become searchable. CLI can ingest local paths. | Implemented | `/api/assets/presign`, `/finalize`; `aura docs ingest` | Asset create is not a full document create contract. | Keep assets, add document/version abstraction. |
| Update metadata | No document metadata update flow found. | Missing | No `/api/documents/:id PATCH`; no React document detail | Operators cannot correct title, tags, scope, or retention metadata. | Add metadata PATCH with audit log and version-independent fields. |
| Tag-based library search | No first-class document tags found. | Missing | No document table/API/UI for tags; only generic metadata concepts are proposed today | Operators will fall back to filenames or full-text search for library organization. | Add normalized tags and indexed tag filters in the document library. |
| Update content/new version | No content update or document versioning. | Missing | `DocumentID(contentHash, sourceID)` creates another ID rather than one document with versions | Changed files can leave old graph data active and unconnected to a document history. | Add `document_versions` and activate one version at a time. |
| Reprocess/rechunk | Retry only resets asset status and reruns processor. No versioned reprocess/rechunk job. | Risky | `assets.Service.Retry`; `documents.Service.IngestPath` | Retry can duplicate/overwrite graph state without clear idempotency or history. | Add idempotent reprocess jobs keyed by document version and pipeline config hash. |
| Delete behavior | Asset row is soft-deleted and object delete is best-effort. | Risky | `assets.Service.Delete`, `Store.Delete` | Neo4j documents/chunks/embeddings remain searchable; object delete errors are ignored. | Add deletion state machine and retryable graph/object cleanup jobs. |
| Embedding creation | Embedding worker processes chunks in batches and records progress. | Partially implemented | `documents.EmbeddingWorker.Process`; `Indexer.UpsertEmbeddings` | Goroutine queue is lost on process death; failures revert job to searchable, not failed/dead-letter. | Move embeddings to durable job table with leases and retries. |
| Embedding update/delete | No explicit vector deactivation or delete by document/version. | Missing | Search/retrieve do not filter active/deleted; no delete Cypher in indexer | Stale vectors can be returned after delete or version update. | Add active/version filters and delete/deactivate Cypher with retry jobs. |
| Search/RAG consistency | Fulltext, vector, rerank, and graph expansion exist. Document-scoped prefilter exists. | Partially implemented | `search.go`, `retrieve.go`, `graphrag.go` | Queries filter by `document_id` only when requested; no `active`, `deleted_at`, version, tenant, or status filter. | Add filterable graph properties and enforce them everywhere. |
| Neo4j schema | Document and chunk constraints/indexes exist. | Partially implemented | `0002_documents.cypher` | No version, active, deleted, identity, asset, or embedding model properties in constraints/indexes. | Extend graph schema for lifecycle and filterable retrieval. |
| Idempotency/retry safety | Some `MERGE` and unique constraints exist. Retry APIs exist. | Risky | `MERGE` in `indexer.go`; `assets.Service.Retry` | No idempotency keys, no job leases, no retry counts, no transition guards, no dead-letter queue. | Add idempotency keys, outbox, leases, and retry policies. |
| Cleanup/orphan handling | No scanner or cleanup API found. | Missing | No `/api/storage/orphans`; no cleanup jobs | Bucket objects, DB rows, and graph nodes can drift silently. | Add dry-run scanners and execute modes. |
| Web UI management | Attachment flow only. | Partially implemented | `useAttachmentUploads.ts`; no document routes in `web/src/main.tsx` | Operators cannot inspect hashes, versions, failed stages, or cleanup impact. | Build document list/detail/jobs/orphans UI. |
| Test coverage | Strong unit tests exist for services and retrieval; E2E covers presigned upload. | Partially implemented | `internal/assets/*_test.go`, `internal/documents/*_test.go`, `web/e2e/assets.spec.ts` | No full upload -> finalize -> extract -> search -> delete graph cleanup E2E. | Add lifecycle E2E and failure-injection integration tests. |
| CI validation | Broad Go, web, DB, knowledge, and Playwright jobs exist. | Partially implemented | `.github/workflows/ci.yml` | Live document/GraphRAG tiers are partly compile-floor on standard runner; asset E2E does not finalize/process. | Add deterministic local fixtures and targeted lifecycle tests. |
| Observability | General observability and health surfaces exist. | Partially implemented | `internal/obs`, `internal/agui/metrics.go` | Ingestion lacks stage metrics, traces, queue gauges, worker leases, and event log UI. | Add OpenTelemetry spans, Prometheus metrics, structured logs, and event stream. |
| Security and auth | Asset APIs bind to principal identity and use presigned uploads. | Partially implemented | `principalIdentityID`, `GetForIdentity`, auth tests | File upload scanning, admin cleanup authorization, and hard-delete confirmations are missing. | Follow OWASP file-upload controls and define admin capabilities. |

## Missing Capabilities

| Capability | Current status | Why it matters | Recommended implementation | Backend impact | Frontend impact | Database changes | Required tests |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Document abstraction separate from asset | Missing | One document can have many versions and storage objects. | Add document, version, and storage-object services. | `internal/documents`, `internal/assets`, AG-UI routes | New list/detail clients and routes | `documents`, `document_versions`, `storage_objects` | Repository and API tests for create/list/detail. |
| Tags for library search | Missing | Operators need quick retrieval by customer, machine, project, supplier, or workflow without remembering filenames. | Add normalized document tags as first-class filterable fields, not only loose JSON metadata. | Document service, list/search API, Neo4j lifecycle properties | Tag editor, tag filter chips, tag column | `document_tags` or `documents.tags text[]` plus GIN/search indexes | Tag add/remove, tag filter, permission, and search tests. |
| Metadata update | Missing | Operators need safe edits without re-ingesting content. | `PATCH /api/documents/:id` updates title, tags, metadata, and emits an audit event. | New document API/service method | Detail edit form/dialog | `documents.metadata`, tags storage, `audit_logs` | API auth, validation, tag normalization, audit tests. |
| Binary/content update | Missing | Changed content must not overwrite old chunks/vectors in place. | `POST /api/documents/:id/new-version` creates a new immutable version. | Asset finalization, document service, worker | New-version upload flow | `document_versions`, `ingestion_jobs`, `storage_objects` | New version activates only after ready. |
| Reprocess | Risky | Extraction, chunking, or embedding models change. | Durable reprocess job with `pipeline_config_hash` and idempotency key. | Worker layer, document service | Reprocess action and job timeline | `ingestion_jobs`, `ingestion_events` | Reprocess same key is no-op; new key creates new job. |
| Rechunk | Missing | Chunking strategy affects retrieval quality. | Store chunking config and create a new version or derived generation. | Chunker/indexer changes | Version/job detail displays config | `document_chunks`, config hash on `document_versions` | Old chunks become inactive when new version activates. |
| Regenerate embeddings | Partially implemented | Embedding model changes require safe backfill. | Store embedding model/version and re-embed by version. | `worker.go`, `indexer.go`, embedding service | Embedding status panel | `document_embeddings` | Re-embed failure leaves old active embeddings until cutover. |
| Vector delete/deactivate | Missing | Deleted/updated docs must not appear in search/RAG. | Delete by `document_id`/`version_id` or set `active=false`, then retry until confirmed. | Neo4j migrations, `indexer.go`, `search.go`, `retrieve.go` | Delete result/timeline | `delete_jobs`, embedding rows marked inactive/deleted | Deleted doc not returned in sparse/vector/GraphRAG. |
| Orphan object cleanup | Missing | Object store costs and privacy risk grow over time. | Dry-run scanner compares `storage_objects` with bucket listing and asset/doc rows. | objectstore interface, admin API | Orphan cleanup page | `storage_objects`, cleanup job rows | Dry-run no mutation; execute deletes expected objects only. |
| Orphan graph cleanup | Missing | Stale chunks/vectors pollute answers. | Scanner compares graph nodes against active versions. | Neo4j queries, worker jobs | Stale graph cleanup results | `delete_jobs`, `ingestion_events` | Stale node cleanup with retryable delete job. |
| Idempotent retries | Risky | Duplicate requests and worker restarts must be safe. | Add idempotency table or unique `(operation, key)` fields; workers use leases. | Services, APIs, worker claim loop | Retry buttons surface existing operation | Unique idempotency keys on jobs/operations | Duplicate POST returns same operation result. |
| Durable queue/outbox | Missing | Goroutine jobs vanish on process death. | Postgres outbox/job table with `FOR UPDATE SKIP LOCKED`, leases, retry counts, dead-letter. | Replace goroutine dispatch in asset/doc embedding paths | Jobs page shows queued/running/dead-letter | `ingestion_jobs`, optional `outbox_events` | Crash/restart resumes queued work. |
| Failure recovery | Partially implemented | Operators need visible recovery path. | Persist `failed` stage, error code, retryable flag, next retry time. | Store and worker transitions | Error detail plus retry action | Error fields and event records | Failed parse, embed, vector delete can be retried. |
| UI document library | Missing | Chat chips are not an admin cockpit. | Add routes for list, detail, versions, jobs, orphans. | `/api/documents`, `/api/ingestion/jobs`, `/api/storage/orphans` | `web/src/main.tsx`, new document modules | Reads from new tables above | React and Playwright tests. |
| UI destructive safety | Partially implemented | Remove now deletes immediately. | Confirmation modal for delete, typed confirmation for hard delete/cleanup. | Delete APIs require mode/reason/confirmation | Attachment UI, document detail, cleanup page | `audit_logs`, `delete_jobs` | Confirm/cancel tests. |
| Observability | Partially implemented | Ingestion issues are multi-system. | Stage spans, metrics, structured logs, event table, health checks for objectstore/sidecars. | `internal/obs`, services, health endpoints | Health/job timeline panels | `ingestion_events`, trace fields | Metrics exposed; trace IDs in events. |

## Target Industrial Architecture

The target architecture keeps the existing asset pipeline but promotes documents and document versions to first-class entities.

```text
Web / Telegram / CLI
        |
        v
Asset upload and storage object ledger
        |
        v
Document service creates document version
        |
        v
Durable ingestion job + outbox
        |
        +--> parser/extractor sidecar
        +--> chunker
        +--> Postgres chunk ledger
        +--> Neo4j sparse graph upsert
        +--> embedding worker
        +--> Neo4j vector upsert
        +--> active-version cutover
        |
        v
Ready document, observable in UI, queryable by RAG
```

Main components:

- **API layer:** Existing `/api/assets` remains for upload. New `/api/documents`, `/api/ingestion/jobs`, and `/api/storage/orphans` expose management.
- **Go service layer:** `internal/documents` owns documents, versions, chunks, embeddings, and lifecycle. `internal/assets` owns file ingress and original object records.
- **Worker/queue layer:** A durable Postgres job/outbox worker claims work using leases and `SKIP LOCKED`, applies idempotent side effects, and emits events.
- **Database layer:** Postgres is the system of record for document state, job state, object references, chunk/embedding metadata, and audit events.
- **Object storage layer:** Garage/S3 stores immutable bytes and derived artifacts under versioned keys.
- **Embedding provider layer:** Embedding model/version are stored with every embedding generation.
- **Vector database layer:** Neo4j stores active/inactive lifecycle metadata on `Document` and `Chunk` nodes.
- **React UI layer:** Operators can inspect documents, versions, jobs, errors, hashes, storage objects, and cleanup dry-runs.
- **Observability layer:** Every stage emits structured logs, metrics, traces, and durable events.
- **Validation layer:** Tests prove cross-store consistency, stale-vector prevention, and retry safety.

## State Machine

Use a version-level state machine. An asset can remain an upload artifact; readiness belongs to `document_versions`.

Canonical version states:

```text
Uploaded
HashCalculated
Stored
Queued
Parsing
Parsed
Chunking
Chunked
Embedding
Embedded
Indexed
Ready
Failed
Deleting
Deleted
Archived
```

Valid transitions:

- `Uploaded -> HashCalculated -> Stored -> Queued`
- `Queued -> Parsing -> Parsed -> Chunking -> Chunked -> Embedding -> Embedded -> Indexed -> Ready`
- Any active work state can move to `Failed` with an error code, retry policy, and event.
- `Failed -> Queued` is allowed only through an idempotent retry operation.
- `Ready -> Queued` is allowed for reprocess or re-embed when a new processing config is used.
- `Ready -> Deleting -> Deleted`
- `Ready -> Archived` for retention-preserving removal from search.
- `Archived -> Deleting -> Deleted` for hard delete.
- `Deleting -> Failed` is allowed only for retryable cleanup failures.

Terminal states:

- `Ready`: active and queryable.
- `Deleted`: not queryable; hard-delete cleanup complete or tombstone retained per policy.
- `Archived`: not active in normal search, retained by policy.
- `Failed`: terminal until explicit retry or operator action.

Retry rules:

- Every operation has an idempotency key.
- Worker claims use `locked_by`, `locked_until`, `attempt_count`, `next_attempt_at`, and `last_error`.
- Side effects are committed in small steps with events. A retried worker can observe completed steps and skip them.
- Delete is retry-safe. If object deletion succeeds but graph deletion fails, the delete job continues from graph deletion.
- Dead-letter is a state, not log text: after max attempts, a job stays visible and retryable by an operator.

## Data Model Proposal

### `aura.documents`

Purpose: Stable logical document across versions.

Key columns:

- `id uuid primary key`
- `identity_id uuid not null`
- `scope text not null`
- `title text not null`
- `tags text[] not null default '{}'`
- `metadata jsonb not null default '{}'`
- `active_version_id uuid`
- `status text not null`
- `created_at`, `updated_at`, `deleted_at`

Indexes and constraints:

- `(identity_id, created_at desc)`
- `(identity_id, scope, created_at desc)`
- `(identity_id, title)`
- GIN index on `tags` for fast tag filters.
- Foreign key from `active_version_id` to `document_versions(id)` should be deferrable or set after version creation.

Lifecycle:

- Metadata changes do not create a new version.
- Tag changes do not create a new version. Tags are operator/library metadata, not content identity.
- Deleting a document creates delete jobs for active and historical versions according to retention policy.

If richer tag management is needed later, split `tags text[]` into `aura.document_tags`:

- `document_id uuid not null`
- `tag text not null`
- `created_by uuid`
- `created_at timestamptz not null default now()`
- Primary key `(document_id, tag)`
- Index `(tag, document_id)`

The first implementation can start with `tags text[]` and a GIN index unless tag ownership, tag descriptions, aliases, or global tag governance are required.

### `aura.document_versions`

Purpose: Immutable content version and processing state.

Key columns:

- `id uuid primary key`
- `document_id uuid not null`
- `asset_id uuid`
- `version_number integer not null`
- `status text not null`
- `sha1 text not null default ''`
- `sha256 text not null`
- `content_type text not null`
- `size_bytes bigint not null`
- `storage_object_id uuid not null`
- `chunking_config_hash text not null`
- `pipeline_config_hash text not null`
- `ready_at`, `activated_at`, `created_at`, `updated_at`, `deleted_at`
- `error_code`, `error_message`

Indexes and constraints:

- Unique `(document_id, version_number)`
- Unique `(document_id, sha256, pipeline_config_hash)`
- `(status, created_at)`
- `(sha256)`
- `(sha1)` only if SHA-1 compatibility is needed.

Lifecycle:

- A new content upload creates a new version.
- No-op update with the same hash and config returns the existing version.
- Old versions stay inactive unless explicitly restored.

### `aura.storage_objects`

Purpose: Ledger for every object in Garage/S3.

Key columns:

- `id uuid primary key`
- `identity_id uuid not null`
- `document_id uuid`
- `version_id uuid`
- `asset_id uuid`
- `bucket text not null`
- `object_key text not null`
- `kind text not null` such as `raw`, `extracted_text`, `chunks`, `preview`, `temp`, `failed_artifact`
- `sha1 text not null default ''`
- `sha256 text not null default ''`
- `etag text not null default ''`
- `size_bytes bigint not null`
- `content_type text not null`
- `retention_class text not null`
- `created_at`, `deleted_at`

Indexes and constraints:

- Unique `(bucket, object_key)`
- `(identity_id, document_id, version_id)`
- `(kind, created_at)`
- `(deleted_at)` partial indexes for cleanup.

### `aura.ingestion_jobs`

Purpose: Durable queue for parsing, chunking, embedding, indexing, reprocess, and cleanup work.

Key columns:

- `id uuid primary key`
- `job_type text not null`
- `document_id uuid`
- `version_id uuid`
- `status text not null`
- `idempotency_key text not null`
- `stage text not null`
- `attempt_count integer not null`
- `max_attempts integer not null`
- `locked_by text`
- `locked_until timestamptz`
- `next_attempt_at timestamptz not null`
- `payload jsonb not null`
- `error_code`, `error_message`
- `created_at`, `updated_at`, `completed_at`

Indexes and constraints:

- Unique `(job_type, idempotency_key)`
- `(status, next_attempt_at, created_at)`
- `(locked_until)` for lease recovery.

### `aura.ingestion_events`

Purpose: Durable progress and audit trail for documents, versions, jobs, and assets.

Key columns:

- `id bigserial primary key`
- `entity_type text not null`
- `entity_id uuid not null`
- `job_id uuid`
- `from_status text`
- `to_status text`
- `event_type text not null`
- `message text not null default ''`
- `detail jsonb not null default '{}'`
- `trace_id text not null default ''`
- `created_at timestamptz not null default now()`

Indexes:

- `(entity_type, entity_id, created_at)`
- `(job_id, created_at)`

### `aura.document_chunks`

Purpose: Postgres ledger for chunk identity and lifecycle, even if text is primarily searched in Neo4j.

Key columns:

- `id uuid primary key`
- `document_id uuid not null`
- `version_id uuid not null`
- `chunk_index integer not null`
- `chunk_hash text not null`
- `text text not null`
- `locator jsonb not null`
- `active boolean not null default false`
- `created_at`, `deleted_at`

Indexes and constraints:

- Unique `(version_id, chunk_index)`
- Unique `(version_id, chunk_hash)`
- `(document_id, active)`

### `aura.document_embeddings`

Purpose: Embedding metadata and consistency checks.

Key columns:

- `id uuid primary key`
- `document_id uuid not null`
- `version_id uuid not null`
- `chunk_id uuid not null`
- `embedding_model text not null`
- `embedding_version text not null`
- `embedding_dim integer not null`
- `vector_namespace text not null`
- `vector_id text not null`
- `active boolean not null default false`
- `created_at`, `deleted_at`

Indexes and constraints:

- Unique `(vector_namespace, vector_id)`
- Unique `(chunk_id, embedding_model, embedding_version)`
- `(document_id, version_id, active)`

### `aura.delete_jobs`

Purpose: Retry-safe cleanup of object store, Postgres metadata, and Neo4j/vector state.

Key columns:

- `id uuid primary key`
- `document_id uuid`
- `version_id uuid`
- `scope text not null` such as `soft`, `hard`, `archive`
- `status text not null`
- `steps jsonb not null`
- `attempt_count`, `locked_by`, `locked_until`, `next_attempt_at`
- `created_at`, `updated_at`, `completed_at`

### `aura.audit_logs`

Purpose: Human/operator audit trail for destructive or administrative actions.

Key columns:

- `id bigserial primary key`
- `actor_identity_id uuid not null`
- `action text not null`
- `entity_type text not null`
- `entity_id uuid not null`
- `before jsonb`
- `after jsonb`
- `reason text not null default ''`
- `created_at timestamptz not null default now()`

## Hashing and Deduplication

Current code computes SHA-256. The target design should calculate both:

- `sha256`: canonical integrity and dedupe hash.
- `sha1`: optional compatibility hash because the audit brief asks for SHA-1.

Rules:

- Calculate hashes on raw file bytes after upload/finalize and before parsing.
- Never rely on filename for document identity.
- Use `(identity_id, sha256)` for duplicate-content hints.
- Use `(document_id, sha256, pipeline_config_hash)` to detect no-op content updates.
- Use hashes in object keys for operator readability and tamper evidence, but keep UUIDs in keys to avoid cross-tenant hash disclosure.
- If SHA-1 and SHA-256 disagree due to any bug in stream handling, fail the version and do not process.

Recommended raw object key:

```text
documents/{tenant_id}/{document_id}/versions/{version_id}/raw/{sha256}-{safe_filename}
```

If SHA-1 compatibility is required:

```text
documents/{tenant_id}/{document_id}/versions/{version_id}/raw/{sha1}-{sha256}-{safe_filename}
```

## API Design

Existing `/api/assets` should stay as the browser upload mechanism. New APIs should manage documents and operations.

| Endpoint | Purpose | Request | Response | Validation and auth | Idempotency and failure behavior |
| --- | --- | --- | --- | --- | --- |
| `POST /api/documents` | Create a document from an existing finalized asset or initiate upload-backed create. | `asset_id`, `title`, `tags`, `metadata`, `idempotency_key` | Document, active/pending version, job | Identity owns asset; asset is accepted/searchable-capable; tags are normalized and bounded. | Duplicate key returns existing operation. |
| `GET /api/documents` | List documents/library. | Query: scope, status, tag, q, cursor | Page of documents with tags and active version summary | Identity-scoped; admin may filter broader scopes. | Read-only. |
| `GET /api/documents/:id` | Detail view. | None | Document, active version, versions, latest job, storage summary | Identity owns document. | Read-only. |
| `PATCH /api/documents/:id` | Update metadata. | `title`, `tags`, `metadata`, `scope`, `idempotency_key` | Updated document | Validate metadata size/schema and tag rules. | Audit log; duplicate key returns same result. |
| `POST /api/documents/:id/new-version` | Attach new binary/content version. | `asset_id`, optional `activate_when_ready`, `idempotency_key` | Version and job | Asset belongs to identity and content hash exists. | Same hash/config returns existing version. |
| `POST /api/documents/:id/reprocess` | Re-run parse/chunk/embed/index. | `version_id`, `pipeline_config`, `idempotency_key` | Job | Version exists; only one active reprocess per version/config. | Retry-safe job creation. |
| `DELETE /api/documents/:id` | Soft delete or schedule hard delete. | `mode`, `reason`, optional typed confirmation | Delete job | Hard delete requires admin capability and confirmation. | Delete job resumes until all stores agree. |
| `GET /api/documents/:id/versions` | Version history. | None | Versions with hash, status, jobs | Identity owns document. | Read-only. |
| `POST /api/documents/:id/versions/:version_id/activate` | Restore or activate ready version. | `idempotency_key` | Document with active version | Version must be `Ready` and not deleted. | Deactivates old graph/vector state transactionally where possible. |
| `GET /api/ingestion/jobs` | Operator job list. | Filters: status, type, document, stage | Jobs page | Operator/admin scope. | Read-only. |
| `GET /api/ingestion/jobs/:id` | Job detail and events. | None | Job plus event timeline | Operator/admin scope. | Read-only. |
| `POST /api/ingestion/jobs/:id/retry` | Retry failed job. | `idempotency_key`, optional reason | Job | Failed/dead-letter job only. | Resets attempt policy and emits event. |
| `GET /api/storage/orphans` | Dry-run orphan scan. | Query: kind, age, scope | Candidate objects/rows/vectors | Admin capability. | No mutation. |
| `POST /api/storage/orphans/cleanup` | Execute selected cleanup. | Scan token, selected IDs, typed confirmation, `idempotency_key` | Cleanup job | Token must match recent dry-run. | Retry-safe cleanup job. |

Response bodies should expose:

- `document_id`
- `version_id`
- `status`
- `tags`
- `sha1`
- `sha256`
- `size_bytes`
- `content_type`
- `active`
- `embedding_status`
- `last_error`
- `retryable`
- `created_at`
- `updated_at`
- relevant `job_id`

## Embedding Lifecycle Design

Rules:

- Embeddings are generated only after chunk records are durable.
- Every embedding records `document_id`, `version_id`, `chunk_id`, model, model version, dimension, namespace, and vector ID.
- A version is `Ready` only when sparse graph indexing and required embeddings are complete. If Aura intentionally keeps sparse-searchable-before-embedding for UX, then expose two readiness flags: `searchable_ready` and `embedding_ready`.
- When a new version activates, old chunks and embeddings become inactive before or atomically with activating the new graph state.
- Search/RAG must filter to `active=true`, non-deleted status, identity/scope, and active version.
- Embedding model changes create re-embedding jobs. Old vectors remain active until the replacement generation is complete.
- Failed vector deletion creates a retryable delete job. Do not hide the failure in logs only.
- Vector IDs should be stable application IDs, such as `chunk_id:{embedding_model}:{embedding_version}`, not Neo4j element IDs.

Neo4j changes:

- Add properties on `Document`: `identity_id`, `asset_id`, `document_uuid`, `version_id`, `status`, `active`, `deleted_at`, `sha256`, `pipeline_config_hash`.
- Add properties on `Chunk`: `identity_id`, `document_uuid`, `version_id`, `active`, `deleted_at`, `embedding_model`, `embedding_version`.
- Extend indexes for lifecycle filters.
- Consider Neo4j 2026 filterable vector properties where available. The Neo4j GraphRAG Python retriever checks vector index metadata and uses in-index filters when supported; Aura should adopt the same concept with a fallback path.

Retrieval modes:

- `sparse`: fulltext only.
- `vector`: vector seed only.
- `hybrid`: vector plus fulltext with explicit ranker/weight.
- `graph_expanded`: seed, rerank, then expand connected chunks.
- `document_scoped`: require `document_id` or `version_id`.

The current two-stage retrieval and GraphRAG work can be retained. The missing part is lifecycle filtering and version-aware result metadata.

## Bucket and Garbage Cleanup Design

Current conclusion:

- Garage is the object storage backend.
- `AURA_OBJECTSTORE_BUCKET` is the object bucket.
- `objectstore.AssetKey` creates `identity/{identityID}/asset/{assetID}/original`.
- No garbage, archive, failed, temp, or processed-artifact bucket was found.

Target storage layout can use one bucket with prefixes or multiple buckets. Prefer one bucket plus explicit prefixes first, because it fits the current objectstore abstraction and is easier to ship incrementally.

Recommended prefixes:

```text
documents/{tenant_id}/{document_id}/versions/{version_id}/raw/{sha256}-{filename}
documents/{tenant_id}/{document_id}/versions/{version_id}/extracted/text.json
documents/{tenant_id}/{document_id}/versions/{version_id}/chunks/chunks.json
documents/{tenant_id}/{document_id}/versions/{version_id}/failed/{job_id}/{artifact}
tmp/uploads/{tenant_id}/{asset_id}/original
archive/{tenant_id}/{document_id}/versions/{version_id}/...
```

Lifecycle policies:

- Temporary uploads expire after a configured TTL if not finalized.
- Failed artifacts are retained for a shorter debugging window.
- Raw documents are retained according to tenant/document retention policy.
- Soft delete hides document and graph state but retains raw objects.
- Hard delete removes or tombstones raw objects, derived artifacts, chunk rows, embedding rows, and graph/vector state.
- Cleanup is dry-run by default.
- Execute cleanup requires a dry-run token, selected item list, and typed confirmation.

Orphan scanners:

- DB object exists but bucket object missing.
- Bucket object exists but no `storage_objects` row.
- Active version points to missing raw object.
- Neo4j `Document`/`Chunk` exists but no active Postgres version.
- Embedding row/vector exists for inactive/deleted version.
- Asset is deleted but original object still exists beyond retention.

## React Web UI Design

Current UI:

- Chat composer uploads files and polls status.
- Attachment type definitions omit storage and hash fields.
- Browser-side modality inference covers fewer document extensions than the backend.
- No document routes or admin screens were found.

Target routes:

| Route | Purpose | Components | API calls | States and UX |
| --- | --- | --- | --- | --- |
| `/documents` | Document library/list. | `DocumentListPage`, filters, tag chips, status chips, hash/file columns | `GET /api/documents` with `tag` and `q` filters | Empty, loading, error, pagination, fast tag filtering. |
| `/documents/:id` | Document detail. | `DocumentDetailPage`, metadata/tags panel, active version, processing timeline | `GET /api/documents/:id` | Shows tags, hash, size, type, storage, embeddings, latest error. |
| `/documents/:id/versions` | Version history. | `VersionTimeline`, active badge, restore action | `GET /api/documents/:id/versions` | Restore only ready versions. |
| `/documents/:id/edit` | Metadata edit. | Form/dialog | `PATCH /api/documents/:id` | Pessimistic save with validation errors. |
| `/documents/:id/new-version` | Upload replacement. | Upload panel reusing asset upload internals | `/api/assets/*`, `POST /api/documents/:id/new-version` | Blocks activation until ready. |
| `/documents/:id/reprocess` | Reprocess/re-embed. | Config summary, confirmation | `POST /api/documents/:id/reprocess` | Shows idempotency/retry state. |
| `/ingestion/jobs` | Job operations. | Jobs table, filters, retry button | `GET /api/ingestion/jobs`, retry endpoint | Failed/dead-letter visible. |
| `/storage/orphans` | Cleanup admin. | Dry-run scanner, diff table, execute panel | `/api/storage/orphans`, cleanup endpoint | Execute disabled until dry-run and confirmation. |

Destructive UX:

- Soft delete uses a confirmation modal.
- Hard delete requires typed document title or ID.
- Cleanup execute requires a dry-run preview and selected rows.
- Show impacted objects, chunks, embeddings, and Neo4j nodes before delete.
- Use pessimistic updates for destructive actions.

Frontend tests:

- Component tests for list/detail/version/error states.
- Component tests for tag add/remove, tag normalization, and tag-filter chips.
- API client contract tests for document/job/orphan payloads.
- Playwright tests for upload, version, delete, retry, and cleanup dry-run.

Tag UX rules:

- Tags are short, lowercase, trimmed strings, displayed as chips.
- A document can have multiple tags; duplicate tags collapse after normalization.
- List filters support selecting one or more tags.
- The search box (`q`) searches title, filename, and optionally tags; full document content remains `document_search` territory.
- Tags are operator metadata and should not be treated as user instructions in the agent prompt.

## Implementation Roadmap

### Phase 0: Discovery and baseline tests

Objective: Freeze current behavior and expose gaps before refactor.

Tasks:

- Add failing tests for delete not returning stale graph hits.
- Add test for asset event emission expectation.
- Add test for asset document size limit matching document service max bytes.
- Add fixture-based upload -> finalize -> document search integration test where possible.

Files likely to change:

- `internal/assets/*_test.go`
- `internal/documents/*_test.go`
- `web/e2e/assets.spec.ts`

Acceptance:

- Tests describe current gaps and fail for the intended reasons.

### Phase 1: Data model and migrations

Objective: Add document/version/storage/job/event ledgers without changing user behavior.

Tasks:

- Add migrations and sqlc queries.
- Add repository interfaces.
- Add document tag storage and indexes.
- Backfill existing assets/document jobs into compatibility views where needed.

Acceptance:

- Migrations up/down pass.
- Repository tests cover constraints and idempotency.

### Phase 2: Hashing and version creation

Objective: Calculate SHA-1 plus SHA-256 and create document versions.

Tasks:

- Extend `hashAndSniff` or create shared streaming hash utility.
- Persist `sha1` and `sha256`.
- Create version on finalized document asset.
- Detect duplicate/no-op content.

Acceptance:

- Same bytes produce same hashes/version behavior.
- Different bytes create new version.
- Tags can be added during create and edited without creating a new version.

### Phase 3: Durable ingestion worker

Objective: Replace fire-and-forget goroutines with durable jobs.

Tasks:

- Implement worker leases with `SKIP LOCKED`.
- Add retry/backoff/dead-letter.
- Emit ingestion events.
- Keep old synchronous paths as wrappers around durable jobs where needed.

Acceptance:

- Worker crash simulation resumes job.
- Duplicate retries are idempotent.

### Phase 4: Embedding and graph lifecycle

Objective: Make embeddings version-aware and stale-proof.

Tasks:

- Add Neo4j lifecycle properties and indexes.
- Write active filters into sparse/vector/GraphRAG queries.
- Add vector deactivate/delete operations.
- Store embedding model/version metadata.

Acceptance:

- Deleted/inactive versions never appear in search/RAG.
- Model re-embedding can cut over safely.

### Phase 5: Storage cleanup and delete jobs

Objective: Make object and graph cleanup retry-safe.

Tasks:

- Implement delete jobs.
- Implement orphan scanners.
- Add dry-run and execute APIs.
- Add storage object ledger.

Acceptance:

- Object delete failure is retried.
- Dry-run cleanup mutates nothing.

### Phase 6: React Web UI

Objective: Add operator document management.

Tasks:

- Add document routes, API clients, list/detail/version/job/orphan screens.
- Extend asset types with hash/storage/embedding fields.
- Align frontend modality inference with backend allowlist.
- Add destructive confirmations.

Acceptance:

- Operators can inspect and retry failed ingestion.
- Delete/reprocess/new-version flows are visible.

### Phase 7: Observability and operational tooling

Objective: Make every stage debuggable.

Tasks:

- Add metrics, traces, structured logs, event timeline.
- Add health checks for objectstore, extractor, embedding, Neo4j, and worker lag.
- Add admin job counters and queue lag.

Acceptance:

- A stuck asset can be diagnosed from UI plus logs/metrics.

### Phase 8: End-to-end validation and hardening

Objective: Prove the 10/10 lifecycle.

Tasks:

- Add full E2E matrix.
- Add failure injection for objectstore, Neo4j, extractor, embedding provider.
- Add CI tiers or operator-gated live tiers with no-skip-as-green behavior.

Acceptance:

- Upload/update/delete/retry/cleanup pass with backend, frontend, and E2E proof.

## End-to-End Validation Plan

| Scenario | Preconditions | Steps | Expected state and assertions |
| --- | --- | --- | --- |
| 1. Upload new document | Clean tenant, objectstore and sidecars running | Upload, finalize, wait ready | Document/version ready, raw object exists, chunks and embeddings active, UI ready. |
| 2. Upload duplicate same SHA-1/SHA-256 | Existing ready version | Upload same bytes | Duplicate detected; no duplicate active version unless user explicitly saves another doc. |
| 3. Upload changed document | Existing doc | Upload new bytes as new version | New version queued/ready; old version inactive after cutover. |
| 4. Update metadata only | Ready doc | PATCH title/tags/metadata | No new version; audit log exists; tag filter and search still work. |
| 5. Upload new version | Ready doc | New-version API | Version number increments; active only after ready. |
| 6. Reprocess | Ready version | Reprocess with config hash | Idempotent job; chunks/embeddings refreshed or new generation activated. |
| 7. Delete document | Ready doc | Soft delete | UI hides doc; search/RAG returns no hits; delete event exists. |
| 8. Delete with embeddings | Ready doc with vectors | Hard delete | Raw/derived objects removed or tombstoned; vectors deleted/deactivated. |
| 9. Delete while Neo4j unavailable | Neo4j down | Hard delete | Delete job retryable; object steps tracked; no false complete. |
| 10. Retry failed embedding | Embedding sidecar fails once | Retry job | Job resumes from embedding stage; ready when vectors written. |
| 11. Retry failed vector deletion | Vector delete fails once | Retry delete job | Delete eventually complete; no stale vector hits. |
| 12. Cleanup orphan bucket object | Bucket has object without DB row | Dry-run then execute | Dry-run lists object; execute deletes selected object only. |
| 13. Cleanup stale embedding | Vector exists for inactive version | Scan and cleanup | Stale vector deleted/deactivated; active version unaffected. |
| 14. Worker crash recovery | Job locked mid-stage | Kill worker, restart | Lease expires; next worker resumes idempotently. |
| 15. Partial DB/object/vector failure | Inject failure after object write | Retry | Ledger and cleanup reconcile all stores. |
| 16. UI upload flow | Browser auth | Upload doc from UI | Progress, ready state, document link visible. |
| 17. UI update flow | Ready doc | Edit title, tags, and metadata | Form validates and updates detail; tag filters reflect the change. |
| 18. UI delete flow | Ready doc | Confirm delete | Confirmation required; doc disappears; no stale search. |
| 19. UI retry flow | Failed job | Click retry | Job transitions queued/running/ready or failed with visible error. |
| 20. Search excludes deleted | Deleted doc with old chunks | Run sparse/vector/GraphRAG | Zero deleted hits across all retrieval modes. |

## Test Matrix

| Layer | Tests to add | Proof required |
| --- | --- | --- |
| Go unit | Hash utility, state transitions, idempotency keys, retry policy | Deterministic state behavior. |
| Go repository | Documents, versions, jobs, events, storage objects, delete jobs | Constraints, leases, event append, no duplicate jobs. |
| Go service | Create, new version, reprocess, delete, restore, cleanup | Cross-table transitions and idempotency. |
| Worker integration | Parse/chunk/embed/index/delete with fake side effects | Resume after crash and partial failure. |
| Neo4j integration | Active filters, vector delete/deactivate, fulltext lifecycle | Deleted/inactive chunks never returned. |
| Objectstore integration | Presign, immutable keys, object ledger, cleanup dry-run/execute | Bucket and DB consistency. |
| API tests | All new endpoints, tag filters, and auth failures | Correct validation, status codes, and idempotency. |
| React unit | Pages, forms, tag chips, modals, status timelines | Loading/error/empty/destructive/tag states. |
| React integration | API client contract and mocked flows | Frontend matches backend payloads. |
| Playwright | Upload, detail, new version, delete, retry, cleanup | Full browser workflow. |
| CI/operator live | MarkItDown, embedding sidecar, Neo4j, Garage | No-skip-as-green for configured live tiers. |

## Risk Register

| Risk | Severity | Why | Mitigation |
| --- | --- | --- | --- |
| Stale chunks/vectors after delete | Critical | Current delete does not clean Neo4j. | Active filters plus delete jobs and E2E assertion. |
| Lost background work | Critical | Goroutine queues vanish on process death. | Durable job table, leases, retry. |
| Cross-store inconsistency | High | DB, objectstore, Neo4j, embeddings commit separately. | Outbox, idempotent steps, reconciliation scanners. |
| Duplicate processing | High | Retry lacks operation idempotency. | Unique idempotency keys and step checkpoints. |
| Data loss from hard delete | High | Object delete errors currently ignored. | Soft delete first, hard delete via auditable retry job. |
| Search leaks across tenant/scope | High | Retrieval lacks identity/scope filters. | Store and filter identity/scope in graph queries. |
| UI hides operational failure | Medium | Chat chip UI is not enough. | Jobs/events screens and error detail. |
| SHA-1 ambiguity | Medium | Requested SHA-1 conflicts with current SHA-256. | Add SHA-1 only as compatibility, keep SHA-256 canonical. |
| Large migration blast radius | Medium | Data model touches many systems. | Ship append-only schema first, then cut over flows incrementally. |

## Security and Authorization Notes

- All document and asset reads must be scoped by `identity_id` unless an admin capability is explicitly present.
- Admin cleanup and hard delete need a distinct capability, not ordinary chat access.
- Follow OWASP file upload controls: allowlist extensions and MIME types, verify actual size/type, limit filenames, store outside executable paths, and treat all extracted text as untrusted.
- Presigned uploads should remain short-lived and limited to one object key, content type, and size.
- The agent should never receive raw objectstore credentials or arbitrary object URLs.
- Extracted filenames, OCR, transcripts, and document text remain prompt-injection surfaces. The existing protected attachment block is the right pattern and should be preserved.
- Hard delete and cleanup should require typed confirmation and audit logging.

## Observability Plan

Metrics:

- `aura_ingestion_jobs_total{type,status}`
- `aura_ingestion_job_duration_seconds{type,stage}`
- `aura_ingestion_queue_lag_seconds{type}`
- `aura_asset_status_total{status,modality}`
- `aura_storage_orphans_total{kind}`
- `aura_vector_cleanup_failures_total`
- `aura_embedding_chunks_total{model,status}`

Traces:

- `asset.presign`
- `asset.finalize`
- `document.version.create`
- `ingestion.parse`
- `ingestion.chunk`
- `ingestion.index_sparse`
- `ingestion.embed`
- `ingestion.index_vector`
- `document.delete`
- `storage.cleanup`

Structured logs:

- Include `identity_id`, `asset_id`, `document_id`, `version_id`, `job_id`, `stage`, `attempt`, `idempotency_key`, `trace_id`, and `error_code`.

UI/operator visibility:

- Timeline from `ingestion_events`.
- Job attempts and latest error.
- Worker lease owner/expiry.
- Storage object list by version.
- Embedding model/version and vector namespace.
- Cleanup dry-run result with impacted objects and graph/vector rows.

## 10/10 Quality Score Rubric

| Dimension | Current score | Why below 10 | 10/10 requirement | Validation |
| --- | ---: | --- | --- | --- |
| Correctness | 6 | Create/search works, but delete/update consistency is incomplete. | Full lifecycle invariants across stores. | E2E matrix plus integration tests. |
| Completeness | 4 | No document versions, admin UI, cleanup, or update flow. | All requested create/update/delete/reprocess/cleanup flows. | API and UI tests. |
| Reliability | 3 | Background work is goroutine-based. | Durable jobs with leases and retries. | Crash recovery tests. |
| Idempotency | 3 | Some `MERGE` use, but no operation keys or job dedupe. | Every mutation retry-safe. | Duplicate request tests. |
| Update/delete safety | 2 | Delete leaves graph/vector state. | Soft/hard delete with retryable cleanup. | Deleted doc never returned. |
| Embedding consistency | 3 | Embeddings write eventually, but no version/update/delete lifecycle. | Versioned active embeddings and model migrations. | Re-embed and delete tests. |
| Storage cleanup safety | 2 | No orphan scanner or retention policy. | Dry-run and execute cleanup with audit. | Cleanup tests. |
| Observability | 3 | General obs exists, ingestion-specific telemetry thin. | Metrics, traces, event UI, health checks. | Metrics/traces/events assertions. |
| Web UI completeness | 3 | Upload chips exist, no admin/library cockpit. | List/detail/versions/jobs/orphans UI. | React and Playwright tests. |
| Test coverage | 5 | Good unit base, thin full lifecycle E2E. | Cross-store failure matrix. | CI/operator gates. |
| Operational readiness | 4 | Good docs and CI base, missing workers/admin tooling. | Worker operations and cleanup runbooks. | Runbook drills. |
| Security and authorization | 6 | Identity-scoped assets, but upload scanning/admin caps incomplete. | OWASP upload posture plus admin controls. | Auth/security tests. |

Target proposed design score: 10/10 when all roadmap phases and validation gates pass.

## Final Recommendations

1. Do not rewrite the current pipeline. Keep the working asset, MarkItDown, Neo4j, and attachment foundations.
2. Add the missing control plane around it: documents, versions, storage objects, durable jobs, and events.
3. Fix deletion and stale retrieval before building a large admin UI. A beautiful document library that can return deleted chunks is still unsafe.
4. Keep SHA-256 canonical. Add SHA-1 only for compatibility and duplicate reporting.
5. Treat Neo4j GraphRAG Python as a design reference for filtered vector retrieval, hybrid rankers, lexical graph conventions, and progress events. Do not import its Python pipeline as the durable worker model.
6. Ship incrementally: tests first, append-only schema, durable jobs, lifecycle filters, delete cleanup, then UI.
7. Make dry-run cleanup and deleted-document search exclusion non-negotiable acceptance gates.
