# Document pipeline handoff — 2026-08-05 (evening)

Supersedes `document-pipeline-2026-08-05.md`. That document's open items 1 and 2 are now
shipped; its corrections and known-open list still stand except where restated below.

## State

**Worktree clean.** Five code commits landed this session, on `master`, every lefthook gate
green (gofmt, file-size, vet, lint). Nothing pushed.

```
7248b5880  Release a document's source key when it is deleted
29a263000  Recognize stored as in-flight and dead_letter as failed, symmetrically
9c8dd9d81  Correct stale comment and narrow-regex gaps in the vocabulary tests
6fc0fb384  Show the document statuses the pipeline can actually reach
38d78e403  Speak the status vocabulary the database admits
```

**26 commits are unpushed**, most of them the operator's parallel v2.1.0 milestone planning
(`.planning/`), which is interleaved with the five above. Any review range must be chosen to
exclude them; they are contiguous only by accident.

**Correction to the previous handoff:** it claimed `origin/master` was at `975b7d521` with
five commits unpushed. `origin/master` was in fact `30c781358`, and `975b7d521` was itself
unpushed — nine commits, not five, before this session added to them.

## What shipped

### Status vocabularies now match the constraints that close them

The recorder hard-coded `"processing"` for both the document and its version.
`aura.document_versions_status_check` has never admitted that value since migration 0025, so
`RecordAssetVersion` could not insert a version **at all** — visible in the live database as
three `ready` documents behind a single `document_versions` row. The document half was
latent rather than live: 0093 replaces `documents_status_check` with a set that drops
`draft`, `processing` and `archived`, so the first `migrate up` would have broken catalog
create and update too.

Both vocabularies now match. Version status gained a Go type (`DocumentVersionStatus`) — it
was a bare `string`, which is precisely why it drifted from 0025 unnoticed. Fresh rows start
at `stored`, not at 0093's legacy `processing → converting` remap: that remap is for rows
already mid-flight, and at every call site the bytes are hashed and in Garage but conversion
has not begun.

The recurrence guard is `TestDocumentVocabulariesMatchTheDatabase`
(`internal/documents/status_vocabulary_integration_test.go`, `db_integration`): it reads
`pg_get_constraintdef` out of a migrated disposable database and asserts set equality against
`AllDocumentStatuses` / `AllDocumentVersionStatuses`. It degrades loudly — a renamed or
dropped constraint is `ErrNoRows` → `t.Fatalf`, an unparseable definition trips an explicit
`len(seen) == 0` guard.

**Note what that guard is and is not.** The type change makes *cross-vocabulary* confusion
(`VersionStatus: documents.DocumentStatusStored`) a compile error. It does NOT catch an
illegal *literal*: Go assigns untyped string constants to any string-derived named type, so
`VersionStatus: "processing"` still compiles. Two in-tree tests do exactly that and build
clean. The conformance test is the guard, not the compiler.

### A deleted document's source key is released

`aura.documents_source_unique` spanned every row, deleted ones included, so a deleted
document owned its source forever and re-ingesting the same file returned `23505`. The path
could not see it coming: `documentForAssetVersion` looks the document up with
`deleted_at IS NULL`, finds nothing, and inserts into a constraint still counting the row it
could not see. User-visible shape: delete a document, upload it again, nothing happens.

Replaced with a partial unique index `documents_identity_source_live_idx ... WHERE deleted_at
IS NULL`, mirroring `documents_identity_search_document_live_idx` three lines below it, plus
an `ON CONFLICT` on `CreateDocument` so the insert is an atomic get-or-create rather than a
check-then-insert two concurrent uploads can lose a race on.

**Migration 0093 was amended in place, and that was legitimate**: it is committed (in
`8d2701bd1`) but applied **nowhere** — live `aura` is at `schema_migrations = 92` and has no
`source_kind` column. No slot was burned on a schema that has never existed. *This stops
being true the moment anyone runs `migrate up` on this deployment.* Check before assuming it
again.

### The delete window, and why the upsert is guarded

Deletion is **asynchronous**, and the original plan for the above was wrong about it.
`SoftDeleteDocument` only sets `status='deleting'` and enqueues a `delete_jobs` row;
`documents.deleted_at` is written solely by `FinalizeDocumentDelete`
(`document_control_plane.sql:704`) after the durable worker erases the objects.

So between soft-delete and finalize, the dying row still occupies the partial index. A naked
`DO UPDATE` would have upserted onto it — attaching a fresh upload to a document the finalize
then erases. Today that case is a loud `23505`; the change would have converted a loud
failure into silent data loss.

The upsert therefore carries `WHERE documents.status <> 'deleting'`. Zero rows becomes
`ErrDocumentDeleteInFlight`. **Routing that error to a sensible HTTP status at the API edge
is not done** — the store returns it and a test pins it, nothing else.

Rejected, deliberately: freeing the source at delete-*start* by excluding `deleting` from the
index predicate. The sibling `documents_identity_search_document_live_idx` has the identical
window and would raise the 23505 instead, so it needs the same predicate, which changes
`GetDocumentBySearchID`'s semantics. That belongs to whoever owns the delete lifecycle.

## Open work, in order

### 1. Production E2E (Amendment #115/#116) — now unblocked

`scripts/document_pipeline_e2e.sh` plus `scripts/document_pipeline_e2e_*.go` exist; the
corpus is the seven authorized files under
`D:\tmp\aura-document-pipeline-references\document_ingestion\baseline-corpus`, with
`Clienti.xlsx` carrying the 699/TORINO ground truth.

Both blockers are gone: the recorder's statuses are legal, and repeat-source ingest no longer
23505s. This is the next thing to run.

It is also where the **production-path proof** belongs that this session's plans deliberately
left out: delete a document, re-upload the same file through `RecordAssetVersion`, and assert
a fresh live document with a working version. The unit-level work proved the seams; only the
E2E proves the story.

### 2. Quality gates, then push

- `make quality-full` (stack up). **Not run this session.**
- Combined coverage ≥85% over the full tag matrix; mutation ≥70% on the
  lifecycle/retrieval/delete critical files. **Not run this session.**
- Quality snapshot (PRD amendment #20): rows whose CI-gate glob matches `internal/documents/**`
  and `web/src/**` will flag stale. Verify locally first — it must print
  `ok: … checked N row(s)`.

### 3. Carried-forward known-open (from the previous handoff, still true)

- `aura.document_pipeline_stages` survives document delete with `artifact_object_key` /
  `artifact_sha256` pointing at deleted Garage keys. Metadata, not body text.
- No per-job timeout in `DurableDeleteWorker.RunClaimed`; `Stop`'s 30s cap returns anyway, so
  drain reports success while the goroutine lives holding a 20-minute lease.
- `runtimeIngestionWorker.Stop` bounds its join at 30s while a Docling convert may run 15
  minutes.
- Five dead legacy stage queries in `document_control_plane.sql:418-471`.
- `CreateStorageObject`, `MarkStorageObjectDeletePending`, `MarkStorageObjectDeleted`,
  `ListDocumentStorageObjects` have zero callers. **The deadcode gate structurally cannot see
  these** — sqlc methods satisfy the used `Querier` interface, so they read as reachable.
  That blind spot is why they accumulated and will keep accumulating.
- `cmd/aura/document_pipeline_wiring.go:101-129` duplicates `delete_durable_worker.go:102-133`.
- `StorageOrphanService.DryRun` detects orphan objects only; no reconciliation path exists for
  the row-without-object case that `ErrPipelineArtifactOwnershipUnproven` and
  `ErrPipelineProjectionUnreconciled` deliberately create work for.

### 4. New known-open, from this session

- **The document lifecycle is aspirational.** `documents_status_check` and `prd.md:4571`
  describe `accepted → stored → queued → converting → chunking → embedding → projecting →
  ready`. The code implements `accepted|stored → ready`; nothing writes the five middle
  states. `failed` is likewise unreachable — `SetSearchDocumentStatus`
  (`internal/documents/catalog_store_identity.go`) has **no production caller at all**. The
  UI's in-flight set deliberately spans both the reachable and the aspirational values, with
  a comment saying so; do not "clean up" the unreachable ones.
- `ErrDocumentDeleteInFlight` is not surfaced at the API edge (see above).
- `web/src/documents/__tests__/documentViewModel.test.ts` has no
  `documentMatchesTab(status:'stored', tab:'processing')` assertion — the one
  production-reachable case. Killed indirectly via `statusToneFor('stored')` since both read
  the same Set, but a mutant local to `documentMatchesTab`'s branch survives.
- Nothing binds the TypeScript `DocumentStatus` union to the Go `AllDocumentStatuses` at build
  time. Note this would NOT have caught the bug that actually shipped: both sides matched the
  database and still disagreed with each other. The higher-value guard is a UI-side
  exhaustiveness check (`switch` + `assertNever`) so a new union member forces both a tone and
  a tab decision. Not added — no such idiom exists anywhere in `web/src` today.
- **`migratedDocumentPool(t)` is a live-database footgun.** It reads `AURA_DB_MIGRATE_URL` /
  `AURA_DB_URL` and migrates whatever they name, which locally is the live `aura`. Existing
  catalog-store integration tests use it. New `db_integration` tests must use
  `pipelineDisposablePool(t)`, which provisions `aura_pipeline_<uuid>` and drops it.

## Process notes worth keeping

Work ran through plan → subagent-per-task → task review → fix loop → final review.

The review layer caught defects in the **plan's reasoning**, twice, not in the
implementation:

- The final whole-branch review found the web half shipped a "processing" tab whose entire
  membership set was statuses no code path writes — it relocated the symptom the commit
  claimed to fix. Root cause: Task 1 chose `stored` for the Go call sites and Task 2 then
  built the UI's in-flight set from the *constraint's* vocabulary instead. Both halves
  independently matched the database and still disagreed with each other.
- The repeat-source implementer refused to proceed on a false premise (`SoftDeleteDocument`
  sets `deleted_at`) before writing a line, which is what surfaced the delete-window data-loss
  hazard above.

Two lessons, both about grounding rather than execution: **verify what the code actually
writes, not what the schema permits**, and **the delete lifecycle on this subsystem is
asynchronous in ways that are easy to assume away.**

One accepted risk: the repeat-source plan's final whole-branch review was **not run** — the
session was called to a close and, with a single task, it would have re-read the same diff
under the same lens the task review had just applied. The task review was clean at every
severity and independently verified three named structural risks. That is a judgment call,
not a guarantee.

## Environment notes

- Go toolchain and sqlc (v1.31.1) are **WSL only**:
  `wsl -e bash -lc 'export PATH=$HOME/.local/go1.26.3/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && <cmd>'`
- Never run a `.exe` on the Windows host. Web tests run on **Windows** — WSL has no node.
- `db_integration` needs `POSTGRES_PASSWORD`; source it from `.env` so it stays out of logs.
  **A run finishing in under a second SKIPPED** — a skip is not a pass and not a red.
- RLS (migration 0087) means a raw `pool.Query` against `aura.documents` returns zero rows
  regardless of content. Direct assertions must run inside `asDocumentIdentity(...)`, or they
  silently pass against nothing.
- Disposable databases only for any `db_integration` tier. A 2026-07-10 run truncated the live
  deployment's auth tables with no backup.
- SDD working artifacts — ledgers, task briefs, implementer reports with the TDD evidence, and
  review packages — are under `.superpowers/sdd/<plan-basename>/` (gitignored). The plans
  themselves are in `docs/superpowers/plans/`.
