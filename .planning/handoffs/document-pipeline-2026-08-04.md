# Document pipeline handoff — 2026-08-04

## Latest authoritative checkpoint (written at credit handoff)

This section supersedes any conflicting older status later in this file. The older
sections remain useful as implementation history, but several of their "not yet
formatted/tested" statements are now stale.

### Current outcome

The industrial ingestion/retrieval implementation is still **in progress** and must
not be presented as complete. No real production E2E has run yet. No code from this
implementation has been committed after the three PRD amendments listed below.

All delegated agents have finished; none is still editing the worktree:

- `document_durable_delete`: changed the delete snapshot to include every live
  `document_versions.pipeline_generation`, added a real PostgreSQL regression test,
  regenerated sqlc, and reported focused unit/integration/race/vet/build green.
- `document_production_e2e`: finalized redaction in the three E2E files, removed
  toy/synthetic/canary wording, kept only corpus basename/size/SHA-256 in reports,
  and reported `gofmt`, `bash -n`, and helper build/help green. It did not run
  production.
- `document_pipeline_audit`: read-only audit completed with the open findings listed
  below. It changed nothing and ran no tests.

### Work completed since the older checkpoint

- Restored formatting/compilation after the cache, compensation, and runtime wiring
  edits. Focused WSL tests passed for `internal/documents`, `internal/assets`,
  `internal/arcadedb`, and `cmd/aura`.
- Added `internal/objectstore/errors.go` plus tests. Absence now means only typed
  `fs.ErrNotExist`, HTTP 404, or exact S3 codes `NotFound`, `NoSuchKey`, and
  `NoSuchObject`; auth/network errors are not accepted as absence. The fake object
  store now wraps `fs.ErrNotExist`. Focused WSL tests for `internal/objectstore` and
  `internal/documents` passed.
- `GetPipelineCandidateVersion` now locks document+version with
  `FOR UPDATE OF version, document`.
- `RecordPipelineDerivedArtifact` now uses a materialized eligibility CTE that locks
  and revalidates document+version before inserting the ledger row.
- sqlc v1.31.1 was regenerated after those SQL changes.
- Added the real PostgreSQL test
  `TestPipelineWritesSerializeBeforeDocumentDelete` covering candidate persistence
  and derived-artifact insertion. It waits for the second transaction to be visibly
  blocked in `pg_stat_activity`, lets delete win, then requires `pgx.ErrNoRows` and
  proves there are no late chunks/artifacts. Actual WSL `db_integration` result:
  PASS, both subtests, 1.37 seconds, no skip.
- The delete snapshot now derives `projection_generations` from all live document
  versions rather than chunks, so a generation exists in the snapshot even if delete
  starts before projection/chunk persistence.
- The production E2E report hashes identity, asset, document, version, conversation,
  database, producer, model, and citation identifiers. Only authorized corpus
  basename/size/SHA-256 is emitted.

### Latest audit: unresolved findings

The audit initially reported the ingest/delete SQL race. The current worktree already
contains the two locking fixes and the broader projection-generation snapshot above,
so that part is mitigated and has a real two-path DB regression test. The following
items remain open and are completion blockers:

1. **HIGH -- version creation during deletion.** `CreateDocumentVersion` locks the
   document but still checks only `deleted_at IS NULL`; it must also reject
   `status IN ('deleting','deleted')`. Add a deterministic delete-vs-version DB test.
2. **HIGH -- destructive projection compensation.** `ProjectionResult.Created` is
   lost when converted into `ProjectionCommit`. `DiscardCandidate` currently runs on
   any PostgreSQL save failure, including replay/existing projection or ambiguous
   commit, and may remove an already authoritative projection. Propagate `Created` and
   discard only a projection created by this attempt after PostgreSQL proves that its
   generation is not authoritative. Add replay/existing plus PG-error tests proving
   zero tombstone/delete calls.
3. **HIGH -- same-SHA production replay is not wired.** `ReserveCandidateVersion` has
   no non-test caller. The real ingress path still creates/catalogs first, and a repeat
   source or same SHA can conflict or attach a newly written raw object/asset to the
   old immutable version. Route the real recorder through one atomic reserve path and
   prove two concurrent ingress requests with distinct asset IDs produce one version,
   one live raw object, and zero Docling/embed/project calls on replay.
4. **HIGH -- PostgreSQL PII remains after durable delete.** Finalization marks
   document/version/assets deleted but does not tombstone/delete
   `document_chunks`/`document_embeddings`. Implement the PRD retention contract and
   gate finalization on zero live rows; add a real DB assertion.
5. **MEDIUM-HIGH -- artifact cleanup after ambiguous DB commit.** Derived artifact
   keys are deterministic. A returned transaction/commit error does not prove the
   ledger insert failed, so unconditional object deletion can remove a valid shared
   artifact. Query ledger ownership/key/hash after the error; delete only when PG
   proves absence. If PG is unavailable, leave it for reconciliation. Add the
   commit-ambiguous regression test.
6. **MEDIUM -- deletion starvation.** The runtime coordinator waits for ingestion and
   deletion sequentially, with ingestion first. A long Docling conversion can delay
   delete claims indefinitely. Use fair/separate loops with the intended global bound
   and test that a blocked ingest does not prevent delete claim within a bounded time.

The centralized object-store not-found classifier was audited as correct.

### Exact resume order

1. Re-read `D:\Aura\CLAUDE.md`, this checkpoint, current Git status, and every file
   before editing. Preserve the operator's nightly-backup hunk in `cmd/aura/serve.go`
   and all unrelated `cmd/aura/serve_provisioning.go` work. Never `git add .`.
2. Close the six findings above, starting with delete/version serialization,
   projection compensation, same-SHA replay, and Postgres chunk/vector erasure.
3. Add daemon-free tests for the new runtime deletion composition: coordinator error
   joining/counts, distinct `aura-document-delete-*` worker IDs, exact owner-scoped
   object-store routing, and non-no-op sandbox purge.
4. Regenerate sqlc v1.31.1 and prove generated output is stable.
5. In WSL, run focused unit and race tests, then `go vet ./...`, `go build ./...`, and
   `git diff --check`.
6. Run the real disposable PostgreSQL gates with no skip: migration 0093
   `up -> down -> up`, intentional down-refusal after verified hard delete, durable
   delete integration, and all three ingest/delete serialization paths.
7. Re-audit the E2E scripts, run `bash -n`, and build the helper in WSL.
8. Resolve and implement the exact Granite model architecture before sign-off. Current
   `internal/documents/docling_client.go` still requests `convert_pipeline=standard`;
   it does **not** run the requested
   `onnx-community/granite-docling-258M-ONNX`. Choose truthfully between Docling's VLM
   preset (not necessarily the exact artifact) and a direct ONNX Runtime service pinned
   to the exact Hugging Face revision. CocoIndex alone does not solve this.
9. Run the E2E only against the real deployed production Aura agent/models and exactly
   the seven authorized files under
   `D:\tmp\aura-document-pipeline-references\document_ingestion\baseline-corpus`.
   It is not green unless restart/reclaim, cross-identity isolation, real tool use,
   the `699` spreadsheet ground truth, citations, delete, object absence, non-recall,
   and cleanup all pass.
10. Before completion: combined coverage >=85%, mutation score >=70%, quality snapshot
    gate, atomic selective commits, and fresh post-commit/full verification. No
    production push/merge has been performed.

### Last independently observed green commands

Run with WSL Ubuntu and Go 1.26.3 on PATH. These passed after the latest root edits:

```bash
go test ./internal/documents ./internal/assets ./internal/arcadedb ./cmd/aura
go test ./internal/objectstore ./internal/documents
go test ./internal/db ./internal/documents ./internal/objectstore ./cmd/aura
go test -tags=db_integration -run 'TestPipelineWritesSerializeBeforeDocumentDelete' ./internal/documents -count=1 -v
```

Do not infer that full race/vet/build/migration/E2E gates are current from those
focused passes; they remain required after the open fixes.

## Objective and non-negotiable operator constraints

Implement and prove the complete production document ingestion/retrieval lifecycle:

- WSL is the mandatory Go/test toolchain.
- Migration 0093 must be proven `up -> down -> up` on a disposable PostgreSQL database.
- The final E2E must call the real production Aura agent and real production models; no mock, toy corpus, skipped stage, degraded fallback, or synthetic green.
- The only authorized E2E corpus is exactly:
  `D:\tmp\aura-document-pipeline-references\document_ingestion\baseline-corpus`
  (WSL: `/mnt/d/tmp/aura-document-pipeline-references/document_ingestion/baseline-corpus`).
- E2E logs may contain only safe metadata such as basename, byte size and SHA-256, never document contents.
- Do not introduce `AURA_DOCUMENT_EMBEDDING_MODEL`. Aura already has one shared embedding route/model knob: `AURA_EMBED_MODEL`. The new immutable identity fields are global `AURA_EMBED_REVISION` and `AURA_EMBED_FINGERPRINT`.
- Preserve the operator's unrelated edits in `cmd/aura/serve.go` (nightly backup seed) and `cmd/aura/serve_provisioning.go`. Never revert them and never stage the whole files blindly.

Read `D:\Aura\CLAUDE.md` completely before resuming. Do not use `git add .` in this shared/dirty worktree.

## Product decisions already committed

- `149132def` — PRD amendment #114, industrial document pipeline.
- `ce02125ff` — PRD amendment #115, isolated real production document E2E.
- `975b7d521` — PRD amendment #116, exact operator-authorized corpus.

The implementation after those PRD commits is mostly uncommitted in the shared worktree.

## CocoIndex / MCP / ArcadeDB conclusion

The CocoIndex MCP connected to ArcadeDB is not sufficient by itself.

- CocoIndex can replace the internal transformation DAG/cache/retry engine and has first-class Docling examples.
- CocoIndex 1.0.14 does not list ArcadeDB as a built-in target; an Aura-owned custom target connector is required.
- MCP is an agent-facing tool surface, not the durable ingestion control plane. It does not by itself provide owner-scoped ingress, immutable versions, lease fencing, atomic activation, citation revalidation, RLS, or verified deletion.
- The least disruptive architecture is hybrid: Aura Go remains control plane, isolation, activation, retrieval and deletion; CocoIndex may later become the internal transformation orchestrator; ArcadeDB remains the per-identity projection; MCP exposes search/open to the agent.
- Treat replacing the current Go `PipelineWorker` with CocoIndex as a new PRD architecture amendment, not an implicit refactor.

Official references used:

- https://cocoindex.io/docs/targets
- https://cocoindex.io/docs/advanced_topics/custom_target_connector/
- https://cocoindex.io/docs/getting_started/quickstart/
- https://cocoindex.io/docs/examples/pdf-embedding/
- https://cocoindex.io/cocoindex-code/

## Exact Granite ONNX status — do not misrepresent it

The requested artifact is `https://huggingface.co/onnx-community/granite-docling-258M-ONNX`.

Current code does **not** run that exact ONNX artifact. `internal/documents/docling_client.go` sends `convert_pipeline=standard` to pinned Docling Serve. The Compose allow-list mentions the `granite_docling` VLM preset, but the request does not select the VLM pipeline/preset. The Hugging Face ONNX repository is a direct ONNX/Transformers.js artifact and is not evidence that Docling Serve consumes that exact repository.

Before production sign-off choose and implement one truthful option:

1. Docling Serve VLM preset (`vlm` + `granite_docling`), while explicitly documenting that it may use a different artifact/runtime; or
2. a new direct ONNX Runtime service/function pinned to the exact Hugging Face revision.

Simply adding CocoIndex does not resolve this: CocoIndex can call a custom parser, but the parser/runtime, artifact fingerprint and provenance contract still need implementation and tests.

Pinned research clones/artifacts are under `D:\tmp\aura-document-pipeline-references`. Known source pins from this session: GraphRAG v3.1.1 `14a00ad`, Docling v2.118.0 `9b454c`, docling-core 2.90.0 `23fa247`, CocoIndex `714bbc`, Docling Serve 1.29.0 `7f2b890`, ArcadeDB 26.7.3 `f2f3d880`, ONNX repo `e8602580`, IBM base `982fe3`.

Pinned Docling image:
`quay.io/docling-project/docling-serve-cpu:v1.29.0@sha256:51c18e0e0fec8a13ab858f03d5e9a140178d8d95962400553cb2644ad5ee84da`.

## Implemented work in the shared worktree

### Control plane and migration

- Migration `0093_document_pipeline_convergence` up/down plus real PostgreSQL migration tests.
- Durable candidate reservation, job binding, stage lease/heartbeat/fencing, artifact ledger, candidate persistence and atomic activation.
- Activation validates owner/document/version/asset/raw object/job/generation and exact passage/embedding/projection counts before publishing.
- Same-SHA replay was being fixed so a new current asset can bind to the immutable existing version without pretending the original version belonged to the new asset.

Primary files: `internal/db/migrations/0093_*`, `internal/db/queries/document_pipeline*.sql`, generated `internal/db/sqlc/document_pipeline*.go`, and `internal/documents/pipeline_store*.go`.

### Ingestion and immutable pipeline

- Stable source/search document IDs.
- Upload/catalog stays `accepted/processing`, never searchable before activation.
- Asset processing requires a real claimed/fenced ingestion job and owner-specific object store.
- Real Docling async conversion, provenance-aware passages, shared embedding transport, ArcadeDB projection, then atomic activation.
- Full-jitter retry, stage heartbeat, completion sentinel preventing double job completion.
- Cached successful stage artifacts are now loaded from object storage and SHA/size verified instead of silently rerunning conversion/embedding/projection.
- Fingerprints were expanded to cover the Docling option profile, tokenizer/token cap, embed model/revision/artifact/dimensions/MRL normalization, and ArcadeDB schema version.
- Projection compensation was just added: if PostgreSQL rejects `SaveCandidate` after Arcade projection, `DiscardCandidate` tombstones/deletes the exact projection.
- Artifact compensation was just added: if an uploaded derived artifact cannot be entered in the ledger, delete it and verify absence.

The last two compensation edits were made immediately before this handoff and have **not** been gofmt/tested yet.

### Retrieval

- One host retriever shared by `document_search`, `aura docs search`, and `GET /api/documents?q=`.
- Deterministic lexical+dense+card cascade; PostgreSQL title/tag/digest/card routing; ArcadeDB lexical+dense candidates.
- PostgreSQL revalidation before returning text, locator or canonical citation.
- Owner-validated `document_ids`, explicit lexical/card fallbacks, query validation, version/SHA/locator evidence.
- Retrieval agent's final WSL gates before later root edits: `go vet ./...`, `go build ./...`, and focused race all passed.

### Durable deletion

- Durable delete store/worker with exact claim/lease/heartbeat fencing, full-jitter retry, dead-letter, projection tombstone+physical delete+absence verification, per-object delete+HEAD absence, deletion-generation ledger, sandbox purge and atomic finalize.
- Real PostgreSQL integration proved stale fence rejection, `projection_verified=false` refusal, exact deletion generation, atomic finalization and retry-to-dead-letter.
- Runtime wiring was added to the existing processing lifecycle in `cmd/aura/document_delete_worker_wiring.go` and `cmd/aura/asset_processing_worker.go`; one call in `cmd/aura/serve.go` now passes the sandbox router. This runtime wiring is not yet gofmt/tested.

### Production E2E harness

New files:

- `scripts/document_pipeline_e2e.sh`
- `scripts/document_pipeline_e2e_probe.go`
- `scripts/document_pipeline_e2e_support.go`

The harness is locked to the exact seven authorized files, creates two disposable production identities, ingests the corpus for both, forces a restart/reclaim, checks ArcadeDB isolation, invokes the real agent, requires `document_search` and (for the spreadsheet count) `document_open` + `shell_exec`, forbids `web_search`/`web_fetch`, verifies the exact ground truth `699` for `Clienti.xlsx`, then deletes and proves object absence/non-recall/cleanup.

It has passed `bash -n` and helper build at intermediate points, but has **not** been run against production. The E2E agent was interrupted at operator request. Recheck its latest report redaction: all identity/asset/document/conversation/database identifiers should be hashed; only corpus basename/size/SHA may be emitted. Remove remaining `synthetic` wording in CLI flag descriptions.

## Last known green gates

Using WSL Ubuntu with the installed Go toolchain path:

```bash
export PATH=/home/davide/.local/go1.26.3/bin:$PATH
cd /mnt/d/Aura
go test ./internal/config ./internal/documents ./internal/assets ./cmd/aura
```

passed before the final cache/compensation/runtime-wiring edits.

After the immutable-cache/fingerprint change, this also passed:

```bash
go test ./internal/documents ./internal/arcadedb ./cmd/aura
```

The durable-delete agent separately reported real DB integration green and `go test -race ./internal/documents` green. These results predate the final root edits; rerun them.

## Immediate blockers / exact resume order

1. Run gofmt first. There is at least one known compile cleanup: `cmd/aura/document_retrieval_wiring.go` still imports `internal/arcadedb` after index-builder deduplication and that import is now unused.

   ```bash
   export PATH=/home/davide/.local/go1.26.3/bin:$PATH
   cd /mnt/d/Aura
   gofmt -w cmd/aura/asset_processing_worker.go cmd/aura/document_delete_worker_wiring.go \
     cmd/aura/document_index_wiring.go cmd/aura/document_pipeline_wiring.go \
     cmd/aura/document_retrieval_wiring.go internal/assets/object_resolver.go \
     internal/documents/pipeline_worker.go internal/documents/pipeline_artifact_cache.go \
     internal/documents/pipeline_worker_test.go
   ```

2. Resolve the ingest-vs-delete race completely.

   - `SoftDeleteDocument` currently derived `projection_generations` from chunks. It must include every live `document_versions.pipeline_generation`, because the version exists before Arcade projection. The deletion agent may have started this edit before interruption; inspect the worktree rather than assuming.
   - `GetPipelineCandidateVersion` should lock the document/version (`FOR UPDATE OF document, version`) inside `SaveCandidate` so successful candidate persistence serializes before the delete snapshot.
   - `RecordPipelineDerivedArtifact` should similarly lock/revalidate document/version in the same transaction. If delete wins, the newly added object-store compensation must remove the untracked object and verify an actual not-found response, not treat an arbitrary HEAD/network/auth error as absence.
   - Add a race/integration test: version reserved, soft delete snapshots before chunks/projection, late projection/save cannot leave an Arcade generation or object after finalize.

3. Review `deletePipelineArtifact`: its current HEAD verification treats any error as absence. Reuse/export a real S3/object-store not-found classifier and fail on other errors.

4. Regenerate sqlc after all SQL changes. WSL does not have `sqlc` on PATH, but Windows has `C:\Users\Davide\go\bin\sqlc.exe`; invoke that from WSL or install/use the pinned v1.31.1 tool. Then prove generated files are in sync.

5. Fix/add tests for the new runtime deletion composition. Verify ingestion and deletion both run, worker IDs differ, nil/fake test configurations preserve expected behavior, owner routing uses the exact per-identity Garage credentials, and sandbox purge is not a no-op in production.

6. Rerun focused WSL gates, then full gates:

   ```bash
   go test ./internal/config ./internal/documents ./internal/assets ./internal/arcadedb ./internal/agent/tools ./internal/agui ./cmd/aura
   go test -race ./internal/documents ./internal/assets ./internal/arcadedb ./internal/agent/tools ./internal/agui ./cmd/aura -count=1
   go vet ./...
   go build ./...
   git diff --check
   ```

7. Rerun the real disposable PostgreSQL migration proof after final SQL/codegen:

   - fresh `up -> down -> up` for migration 0093;
   - down refusal after a verified hard delete (intentional irreversible-data guard);
   - durable-delete DB integration with no skip.

8. Audit the production E2E report for redaction, run `bash -n`, compile the helper in WSL, then run it only with all real production expectations and the real restart hook supplied. Required variables are declared at the top of `scripts/document_pipeline_e2e.sh`; do not print secrets. A run is not green unless every hard check passes and cleanup proves absence.

9. Do not claim completion until the exact Granite ONNX decision is implemented and the production E2E succeeds with the actual deployed model/artifact identities. No production E2E has been executed yet.

10. Final repository gates still required by `CLAUDE.md`: full combined coverage >=85%, mutation score >=70%, quality snapshot update/gate, atomic commits. Do mutation testing only after the shared worktree is stable because mutators rewrite files.

## Current worktree safety notes

- The worktree contains many modified, added and partially staged files from this effort and pre-existing operator work.
- `cmd/aura/serve.go` contains both the operator's nightly-backup hunk and the new one-line document-worker router argument. Stage hunks selectively.
- `cmd/aura/serve_provisioning.go` is operator-owned and unrelated; do not stage/revert it.
- Agents were interrupted on request to conserve credits. Trust the filesystem, not an assumption that their last intended edit completed.
- Do not expose `.env`, credentials, corpus contents or source paths beyond the already authorized corpus root and safe file metadata.
