# Document plane — two stores, no catalog

**Date:** 2026-08-08 · **Scope:** sub-projects **A** (demolition) and **B** (catalog fate)
**Status:** design, awaiting review · **Evidence:** `spikes/two-store-catalog/FINDINGS.md`

Every decision below was measured on the live stack before it was written down, per the PRD-first
principle. Where something is unmeasured this document says so and does not decide it.

---

## 1. The problem

Amendment #118 replaced the hand-built ingestion with three engines — iscc-tika extracts, CocoIndex
reconciles, ArcadeDB retrieves. That landed. What did not change is everything *around* it: 8,648
LOC of stage machine, Docling client, durable-delete workers and projection writer that nothing
calls any more, plus 3,563 LOC of catalog machinery whose purpose was to answer questions the
passage can now answer itself, plus 2,156 LOC of hand-written file-browser UI.

The principle is unchanged and is the whole design: **Aura orchestrates, never reinvents.**

## 2. What was measured

Seven real files, nested prefixes, disposable bucket, ingested by the CURRENT sidecar in catch-up
mode (`AURA_INGEST_LIVE=false`, so extraction failures propagate instead of being swallowed).

| measurement | result |
|---|---|
| `source_key` on every passage | full nested bucket key, e.g. `fatture/2026/q1/fattura-acme.pdf` |
| `raw_sha256` vs uploaded bytes | **7/7 exact match** |
| `search_document_id` vs `ids.go` framing | **7/7 reproduce** from `(identity, "s3", source_key)` |
| multi-chunk splitting, live | 101,288 bytes → **14 chunks** (previously unit-test-only) |
| `list_objects_v2(Delimiter="/")` | returns SVAR's `{id,size,date,type}` model with no transform |
| `feature/s3/manager` multipart vs Garage | **PASS** stock and pinned; `Downloader` SHA matches |

A correction worth keeping: the handoff's claim that passages carry `source_key` was read as
*disproven* when the live probe databases turned out to have no such field. They carry an **older
schema**. Stale probe databases are worse than no evidence — reading them as current nearly killed
a sound design.

## 3. Decision 1 — two stores, and the catalog is deleted

**Garage holds bytes and names. ArcadeDB holds passages and vectors. PostgreSQL holds no document
state at all** — it keeps only the generic asset queue and the storage ledger, neither of which is
about documents specifically.

This is the standard production RAG shape (object storage as truth, vector store as a reconciled
projection), not an invention.

The catalog existed to answer three questions. Each is now answerable without it:

| question | old path | new path |
|---|---|---|
| where are the bytes? | `doc_<hex>` → resolver → catalog uuid → `GetDocument` → active version → asset id → object (**5 hops**) | `source_key` **is** the object key (**1 hop**) |
| are these the indexed bytes? | version row `sha256` | `raw_sha256` on the passage |
| which documents are indexed? | `SELECT … FROM aura.documents` | `SELECT DISTINCT source_key FROM Passage` |

**Dropped tables:** `documents`, `document_versions`, `document_chunks`, `document_embeddings`,
`document_tags`, `document_ingest_jobs`, `document_pipeline_stages`, `document_pipeline_quarantine`,
`ingestion_events`.

**Retained tables:** `ingestion_jobs` (the generic asset queue — see §4) and `storage_objects` (the
ledger `orphans.go` reconciles against).

**Accepted consequence, recorded because it is a behaviour change and not a cleanup:** document
titles and tags stop existing. A document's name is its object key. Anything richer must live in S3
object metadata or not at all. Catalog rows with no asset stop existing for ingestion.

## 4. Decision 2 — the demolition, on a corrected inventory

The inventory in the previous brief grouped files by **package location** rather than by function,
and two groups were mis-assigned. Both corrections are measured facts, not preferences.

**Deleted (8,648 LOC):**

| group | LOC |
|---|---|
| `pipeline_worker`, `pipeline_store*`, `pipeline_types`, `pipeline_artifact_cache` | 3,808 |
| `docling_*` | 1,606 |
| `delete_durable_*`, `delete.go` | 1,935 |
| `internal/arcadedb/document_projection*` | 978 |
| `cmd/aura/document_pipeline_wiring*`, `document_delete_worker_wiring` | 321 |

Plus the catalog surface from §3, and the cascade: `internal/assets/document_processor.go` and the
`Pipeline` leg of the asset processor.

**MUST SURVIVE — corrections to the inventory:**

- **`jobs_store`, `jobs_worker`, `job_context`, `retry_backoff` (1,396 LOC) are the GENERIC asset
  queue.** Every asset — document, image and audio — is enqueued through
  `documents.CreateIngestionJobRequest` (`cmd/aura/asset_processing_queue.go:47`) and dispatched by
  modality through `ProcessorSet.For` (`internal/assets/processor.go:16-27`). CocoIndex replaces
  document *extraction*; it has nothing to do with vision summaries or audio STT. Deleting these
  takes image and audio processing down with the pipeline.
- **`orphans.go` (223 LOC) is a storage reconciler, not pipeline machinery.** It compares a Garage
  listing against the storage ledger, depends only on `objectstore` and an interface the surviving
  code implements, and backs a live cockpit route. Under an architecture where the bucket is the
  truth, an orphan detector is *more* relevant, not less.

**Ordering is binding.** Consumers are deleted before the code they consume; build + vet + test run
between every group; no group is ever left half-deleted across a commit.

## 5. Decision 3 — the replacement surfaces

**`document_open`** takes a `source_key`, reads the object through the identity's resolved store,
and verifies `raw_sha256` before returning bytes. `SearchDocumentResolver`, `activeVersion` and the
uuid/`doc_<hex>` dual namespace all disappear.

**`document_search`** returns `source_key` on every hit, so find→open needs no lookup.

**The file manager is the MIT SVAR React FileManager embedded in the existing cockpit.** This
section fixes the *approach* — binding on sub-project D, which implements it — because the catalog's
deletion is only safe if its replacement read surface is settled. It is backed by
Go handlers implementing SVAR's published REST contract (`/files`, `/files/{id}`, `/direct`,
`/upload`, `/info`, `/preview`, `/icons/{size}/{name}`) over Aura's existing `objectstore.Store` +
`ObjectResolverBundle`. `list_objects_v2(Delimiter="/")` maps onto the widget's model directly and
`onRequestData(id)` onto a prefixed listing.

**Upload and download go through `github.com/aws/aws-sdk-go-v2/feature/s3/manager`** — measured to
work against Garage in both stock and pinned configurations. Hand-rolled multipart is forbidden.
Note the version: the v1 `aws-sdk-go/service/s3/s3manager` reached end-of-support in July 2025 and
Aura is on v2.

## 6. Alternatives considered and rejected

- **`xbsoftware/wfs` (MIT) + a new Go S3 driver.** Rejected: it is a *filesystem* abstraction whose
  model S3 only emulates, no Go S3 driver exists, and — decisively — routing file operations through
  a generic drive interface would bypass `objectstore.Store` + `ObjectResolverBundle`, which is
  exactly where per-identity credentials and the F-007 foreign-bucket guard are enforced. It would
  put a tenancy-unaware library in front of the isolation seam.
- **`svar-widgets/filemanager-backend-go`.** Rejected as a dependency: **no license file at all**
  (all rights reserved by default), 3 stars, local-disk only. Its REST contract is used as a
  reference; its code is not adopted.
- **Filestash as a sidecar (AGPL-3.0).** Rejected: zero code, but a second auth surface outside
  Aura, no per-identity credential routing, and no ingest status.
- **`Noooste/garage-ui` as a sidecar or fork (MIT).** Rejected as the product surface for the same
  integration reasons; retained as the **reference implementation** for the object-browsing handlers.
- **Catalog as a thin one-row-per-object projection.** Rejected: a third copy that can drift from
  the bucket is the exact bug class reconciliation exists to remove.

## 7. Prerequisites and open risks

- **Per-identity bucket provisioning is a prerequisite, not a detail.** `object_resolver.go`
  implements bucket-per-identity (D-08) with a documented fallback to the shared bucket for
  unprovisioned identities. Garage currently holds **only `aura-assets`** — zero per-identity
  buckets — so today isolation is prefix-based under one shared key. Until provisioning runs, "the
  bucket route's isolation IS the credential" is true of the code and false of the deployment, and
  a sidecar holding the shared key would see every identity's documents. **No per-identity ingest
  sidecar may be launched before this is closed.**
- **Durable erasure changes character.** Deleting `delete_durable_*` makes erasure eventual and
  reconciliation-driven rather than a fenced, retrying, multi-store proof. Amendment #118 keeps
  #114's contract "in full", and durable erasure is one of its three named guarantees, so **#114
  must be amended to record the new mechanism**, and the #115 gate's delete / Garage-absence /
  post-delete-non-recall checks must prove it end to end.

## 8. What this design does not prove

- **Retrieval quality.** No question was asked of any corpus. The blind test — ≥6 documents with
  vocabulary-sharing distractors, questions naming neither file nor content, plus one unanswerable
  to check abstention — has still never run. It belongs to sub-project E.
- **Agent-driven `document_open`.** Unmeasurable before the change, since the Go path still resolves
  through the catalog.
- **Multi-identity isolation.** One identity, one bucket. Untested.
- **Incremental reconciliation.** One catch-up pass only; memoization and delete-reconciliation rest
  on the earlier spike, not this one.

## 9. Testing

Unit tests are daemon-free for pure logic (key↔prefix mapping, the SVAR response shapes, the
`raw_sha256` verification path) so the 85% owned-surface floor survives deleting ~10k LOC and its
tests. Integration tests run the real S3 round-trip through `manager` against Garage under
`db_integration`. The closing proof is amendment #115's gate above 98%, driven by the real agent —
a green unit suite closes nothing.

## 10. Out of scope

Sub-projects **C** (push ranking/fusion into ArcadeDB, delete `retrieval_rank.go`), **D** (the UI
implementation itself) and **E** (the #115 gate and the blind test) each get their own spec. This
document settles only what is demolished and what the catalog becomes.
