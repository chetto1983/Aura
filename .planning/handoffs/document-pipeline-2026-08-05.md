# Document pipeline handoff — 2026-08-05

Supersedes `document-pipeline-2026-08-04.md`. Several of that document's
statements are now stale or were wrong; the corrections are called out below so
the next session does not re-derive them.

## State

**The worktree is clean.** Commits landed, every lefthook gate green
(gofmt, file-size, vet, lint):

```
<F3>       Wire the same-SHA replay through one atomic reservation
afd5a05e6  Record the document pipeline checkpoint
a974cfb22  Give the nightly Postgres backup a clock      (operator's work, own commit)
8d2701bd1  Converge the industrial document pipeline     (163 files)
fe20c2c72  Rule passage erasure physical, not tombstoned (PRD amendment #117)
```

Nothing has been pushed. `origin/master` is still at `975b7d521`.

**All six audit findings are now closed.** F3 shipped 2026-08-05; see below.

## What is done

Five of the six audit findings are closed, each with a test that fails without
its fix (verified, not assumed):

| # | Defect | Resolution |
|---|---|---|
| F1 | version creation during deletion | the `.sql` guard was correct but the generated sqlc output was 2h stale, so the **unguarded** query was executing. Regenerated. |
| F2 | projection compensation destroyed generations it did not own | ownership travels on `ProjectionCommit.Created`; a lost stage fence also blocks compensation |
| F4 | document text + embeddings survived a "successful" delete | physical erasure, finalize gated on zero live passage rows (PRD #117) |
| F5 | artifact cleanup deleted on any ledger error | requires PostgreSQL to positively prove non-ownership |
| F6 | deletion starved behind ingestion's Docling conversions | deletion has its own loop, fixed width 1 |

Also cleared in the same commit: **73 golangci-lint findings → 0**; an **import
cycle** that had made the entire `arcadedb_integration` tier (the whole LOCOMO
suite) uncompilable — `locomo_test.go` now imports `internal/embeddings`
directly instead of reaching it through the `documents.EmbeddingClient` alias;
and `IsDoclingRetryable`, dead code duplicating what `failStage` already does
inline.

### Docling is provisioned and proven — it had never run here

The 2026-08-04 handoff treated Docling as working. It had never started on this
host: image never pulled, no container, and the `aura-docling-models` volume did
not exist. Compose mounts that volume **read-only** with `HF_HUB_OFFLINE=1` on an
`internal: true` network, and its own comment says an install step must seed the
tokenizer "before readiness can pass" — but no such step existed anywhere in the
repo. The healthcheck could therefore never pass, which is why no E2E had run.

Now: image pulled, `scripts/seed_docling_tokenizer.sh` written (the missing
install step), container **healthy**, and a real conversion verified end to end —
table extracted and serialized, headings and `doc_items` provenance intact, and
**Docling's token count matches the embedder's GGUF exactly** (22 = 22).

`google/embeddinggemma-300m` is licence-gated (401 on every file). The operator
chose a mirror. It is verified, not assumed: two independent re-uploads (unsloth,
onnx-community) carry byte-identical vocab (262144) and merges (514906), and the
vocab is index-for-index identical to the GGUF `aura-llama-embed` already serves
— proven by decoding that server's own `/tokenize` output back to the source
string. That last check is the load-bearing one.

### Three required env vars were missing

Compose would not even resolve. Added to `.env`:

- `AURA_DOCLING_API_KEY` — generated
- `AURA_EMBED_REVISION=0f741b5a6585bd53aeb15cd1372c56f2a0f65e12`
- `AURA_EMBED_FINGERPRINT=b5ce9d77a3fc4b3b39ccb5643c36777911cc4eb46a66962eadfa3f5f60490d63`

The fingerprint is the deployed GGUF's real SHA-256, confirmed equal to
upstream's `X-Linked-ETag` (size 333590944 matches `X-Linked-Size`). It is not
decoration: the pipeline mixes it into stage fingerprints, so a wrong value
silently reuses vectors from a different model.

## Corrections to the previous handoff

- **sqlc IS available in WSL.** `/home/davide/go/bin/sqlc` is a native Linux ELF
  at v1.31.1. The claim that only `sqlc.exe` exists was MSYS path mangling from
  the Bash tool, not a missing binary. Never run the .exe.
- **The Granite ONNX decision is not open; the PRD already settled it.** Docling
  has **no ONNX VLM engine at all** — `InferenceFramework` is
  `{MLX, TRANSFORMERS, VLLM}` and `grep -rn "onnx-community" docling/` returns
  zero. The `granite_docling` preset loads `ibm-granite/granite-docling-258M`
  safetensors via transformers, a different artifact. No configuration of Docling
  Serve 1.29.0 can load the requested ONNX repo. prd.md:4575 already rules that
  the ONNX conversion "remains an offline benchmark/export artifact until
  upstream Docling exposes a tested ONNX VLM engine", so `convert_pipeline=standard`
  is PRD-compliant, not a shortfall. Item 8 of the old resume order is closed.
  Minor cleanup available: `DOCLING_SERVE_ALLOWED_VLM_PRESETS: '["granite_docling"]'`
  is inert — it pins docling-serve's own default and constrains a preset the
  request never selects.
- **The git index held a stale, non-compiling state.** 11 files were staged from
  an earlier session; the staged `docs.go` called `library.SearchDigests`, which
  the worktree had removed. Unstaged (worktree bytes verified identical).
- **lefthook lints a MIXED tree**: staged files at their staged state, unstaged
  files at worktree state. A new unused function in an unstaged file therefore
  trips `unused` no matter what you stage. This is why the operator's
  nightly-backup work had to be committed as its own unit.

## F3 — same-SHA replay wiring — DONE (2026-08-05)

`ReservePipelineCandidateVersion` now owns the raw storage object, the version and
the asset binding in ONE statement, and `PostgresCatalogStore.RecordAssetVersion`
routes through it. The four-statement body is gone.

Shipped shape:

- **`bound_asset`** validates the INCOMING asset (live, owned) for both legs.
- **`binding`** decides ONCE — which version, `raw` vs `temp`, which generation —
  because sqlc cannot resolve a computed CTE column referenced downstream.
- **`asset_object`** INSERTs the ledger row naming a version that does not exist
  yet. `ON CONFLICT (bucket, object_key)` deliberately does NOT rewrite `kind`:
  demoting a version's own raw object to `temp` would break the resolution
  `existing` depends on. Its guard is stricter than `CreateStorageObject`'s —
  other bytes under that key, or a key already scheduled for deletion, return no
  row and the statement resolves to `ErrPipelineCandidateRejected`.
- **`existing`** projects `replay_is_active` using `IS NOT DISTINCT FROM`, not
  `=`: under `=` a document with no active version yields SQL NULL, and a NULL
  decoding to Go false is a guarantee resting on luck. The conjunction is total.
- **`linked_asset`** binds `bound_asset.id` (the incoming asset, not
  `selected.asset_id`) and uses `GREATEST(...)` so a replay never walks an
  asset's generation backwards.
- **`inserted`** carries `ON CONFLICT DO NOTHING`: reaching a conflict means a row
  holds these bytes while `existing` refused it (its asset or raw object is no
  longer live) — incoherent, so it becomes the statement's own empty result rather
  than a raw 23505 the caller retries to dead-letter.

Surfaced as `CandidateVersion.ReplayedActive` and
`DocumentVersionRecord.ReplayedActive`. `DocumentVersionRecord` deliberately has
NO bare `Replayed`. `internal/assets/document_processor.go` short-circuits only on
`ReplayedActive` (`replayedAssetResult`), which omits `pipeline_activation_job_id`
so the queue worker still settles the job; every other case falls through to
`Pipeline.Run` unchanged.

**Deleted as dead:** `UpdateStorageObjectVersion`, `LinkAssetDocumentVersion`,
`deterministicDocumentVersionID`. **`CreateStorageObject` was KEPT** per review,
and is now caller-less — the duplicate's bytes get their owner from the statement's
non-raw ledger row instead. That tension is open for the operator to settle.

### Proven, not argued

Probe against a disposable DB (68 migrations), driving the RENDERED live query
text — both probe DBs dropped, live `aura` untouched:

| leg | result |
|---|---|
| fresh insert | `replayed=f replay_is_active=f`; object written naming a not-yet-existing version |
| 2nd asset, new key, same SHA, not activated | `replayed=t replay_is_active=f` |
| re-drive same asset+key | idempotent; raw object NOT demoted to `temp` |
| 2nd asset after activation | `replayed=t replay_is_active=t` |
| same key, different SHA | **0 rows** → `ErrPipelineCandidateRejected` |

End state: 1 live version · 1 live `raw` · 2 `temp` duplicates all carrying
`document_id` and all swept to `delete_pending` by `SoftDeleteDocument` ·
`version.asset_id` still the FIRST asset.

**T1 was proven to fail against the pre-change code by executing it**, not by
reasoning: the old four-statement body run twice yields `live raw objects=2` where
T1 asserts 1. The version already de-duplicated via `CreateDocumentVersion`'s
`ON CONFLICT`; what did not was the OBJECT — a fresh raw row rebound to the old
version, exactly the finding. T2/T3 could not compile before (`ReplayedActive` did
not exist). Honest caveat: the `internal/assets` **false** case passes before AND
after — it constrains the wrong implementation (short-circuiting on `Replayed`),
not the old one.

Tests: `internal/documents/catalog_store_replay_integration_test.go` (T1/T2/T3,
`db_integration`, disposable pool) and
`internal/assets/document_processor_replay_test.go` (daemon-free).

### sqlc gotcha, cost ~40 minutes — read before editing this query

sqlc's STATIC analyzer cannot name a SELECT-list expression wrapping a named
parameter (`*ast.ResTarget has nil name` — sqlc-dev/sqlc#1646, #3991) and cannot
resolve a virtual CTE column referenced downstream (#3555, still open). The
documented no-managed-DB fix is an explicit cast: `sqlc.arg(id)::uuid`. That cast
in `binding` is load-bearing — removing it fails the whole package's codegen.
Enabling the database-backed analyzer would also fix it but requires a reachable
server at `sqlc generate` time (config `database.managed` + `servers`), which CI
does not have.

## Open work, in order

### 1. Document status vocabularies never followed migration 0093 — NEW, blocking

The production recorder cannot insert a version at all. Probed on a disposable DB:

- `document_versions_status_check` admits `uploaded, hash_calculated, stored,
  queued, parsing, parsed, chunking, chunked, embedding, embedded, indexed, ready,
  failed, deleting, deleted, archived` — **not `processing`**, which
  `cmd/aura/document_version_recorder.go:66` hard-codes and
  `catalog_service.go:239` defaults to. `ERROR: violates check constraint`.
- `documents_status_check` admits `accepted, stored, queued, converting, chunking,
  embedding, projecting, ready, failed, dead_letter, deleting, deleted` — **not
  `processing`** (`document_version_recorder.go:65`, `catalog_service.go:224`,
  `documents/service.go:133`) and **not `draft`** (`catalog_service.go:126,166`).
- `web/src/documents/documentApi.ts:2-10` mirrors the SAME stale vocabulary:
  it lists `draft`/`processing`/`archived`, which the DB can no longer produce,
  and lacks `accepted`/`stored`/`converting`/`chunking`/`projecting`/`dead_letter`,
  which it can.
- Corroborating live evidence: `aura` has 3 `ready` documents but only ONE
  `document_versions` row.

0093:279-288 already states the project's own mapping — `draft→accepted`,
`processing→converting`, `archived→failed`. `catalog_service_test.go:168` pins
`"processing"`, so that test is broken too and its rewrite needs justification in
the commit message. **This is wire-visible; it is its own commit.** F3 does not
depend on it — F3 is a strict improvement either way — which is why the F3 tests
pass explicitly-legal statuses (the precedent `card_ingest_integration_test.go`
already sets) rather than masking the defect.

### 2. Repeat-source `23505` (was: split out of F3)

Needs 0093 amended — replace `ADD CONSTRAINT documents_source_unique UNIQUE (...)`
(:306) with a partial unique index `WHERE deleted_at IS NULL`, mirroring its
sibling at :308, and mirror the DROP in the `.down.sql`. Only then may
`CreateDocument` take an `ON CONFLICT ... DO UPDATE`. Without the partial index,
delete-then-reingest cannot create a fresh document.

### 3. Production E2E (Amendment #115/#116)

Corpus verified intact — exactly the 7 authorized files under
`D:\tmp\aura-document-pipeline-references\document_ingestion\baseline-corpus`,
`Clienti.xlsx` present for the 699/TORINO ground truth.

F3 has landed, so the version and activation counts the E2E asserts are now
stable. **It still cannot pass until item 1 is fixed**: the recorder's own status
defaults are rejected by both CHECK constraints, so the real ingress never gets as
far as producing a version.

### 4. Before any push

- `make quality-full` (needs the stack up)
- combined coverage ≥85% over the full tag matrix; mutation ≥70% on the
  lifecycle/retrieval/delete critical files
- **quality snapshot**: 5 rows will flag stale — sandbox execution baseline,
  Telegram channel, Scheduler North-Star, AG-UI gateway, document routing recall@1.
  Verify locally first; it must print `ok: ... checked N row(s)`.

## Known-open, deliberately out of scope

- `aura.document_pipeline_stages` survives document delete with
  `artifact_object_key`/`artifact_sha256` pointing at deleted Garage keys
  (0093:516-546, no DELETE anywhere). Metadata, not body text.
- No per-job timeout in `DurableDeleteWorker.RunClaimed`. One hung ArcadeDB or
  object-store call wedges the pass; `Stop`'s 30s cap returns anyway, so drain
  reports success while the goroutine lives and holds a 20-minute lease. F6 makes
  this independently reachable — it is its own finding.
- `runtimeIngestionWorker.Stop` bounds its join at 30s while a Docling convert may
  run 15 minutes, so Stop can return with a lease still held.
- Five dead legacy stage queries in `document_control_plane.sql:418-471`
  (`UpsertDocumentPipelineStage`, `ClaimDocumentPipelineStage`,
  `CompleteDocumentPipelineStage`, `HeartbeatDocumentPipelineStage`,
  `UpdateDocumentPipelineStageStatus`) — zero non-generated callers.
- `CreateStorageObject` is now caller-less. It was kept on review instruction; the
  duplicate asset's bytes get their owner from the reservation's non-raw ledger
  row instead. Delete it or give it a caller — the deadcode gate does NOT catch
  this (sqlc methods satisfy the used `Querier` interface, so they read as
  reachable), which is also why the three below survived unnoticed.
- `MarkStorageObjectDeletePending`, `MarkStorageObjectDeleted` and
  `ListDocumentStorageObjects` have zero callers anywhere, tests included —
  pre-existing, untouched by F3.
- `cmd/aura/document_pipeline_wiring.go:101-129` duplicates the
  tombstone→delete→verify sequence in `delete_durable_worker.go:102-133`.
- `StorageOrphanService.DryRun` detects orphan objects only; there is no
  reconciliation path for the row-without-object case, which is what F5 prevents
  creating. `ErrPipelineArtifactOwnershipUnproven` and
  `ErrPipelineProjectionUnreconciled` now deliberately leave work for a
  reconciler that does not exist yet.

## CocoIndex + ArcadeDB — cheaper than the old handoff assumed

The 2026-08-04 conclusion ("CocoIndex 1.0.14 does not list ArcadeDB; an
Aura-owned custom target connector is required") understates how close it is.
CocoIndex's Cypher layer is already multi-backend: `cypher_graph.rs` (2231 LOC
shared) with thin adapters `neo4j.rs` (326 LOC) and `falkordb.rs`. ArcadeDB's own
conformance matrix is green across all 15 official Neo4j driver/version combos
(verified 2026-07-08 in their CI), and it now has a **native** openCypher engine
with first-class `MERGE`/`SET` and DDL for indexes and unique constraints. The
only real gap is vector-index DDL, which ArcadeDB expresses in its own SQL — and
Aura already owns that code.

Caveats: the built-in targets are **Rust inside CocoIndex's SDK**, so this is a
fork with rebase burden; and Bolt is not enabled in Aura's ArcadeDB deployment
today (no plugin flag, no port). Useful bonus: conformance MDB-001/002
(named-database selection, cross-database isolation on one driver) are green,
which fits the per-identity database model.

**This changes none of the current blockers.** Every finding F2-F6 was control
plane — compensation semantics, atomic reservation, PII erasure, ambiguous-commit
safety, delete starvation. CocoIndex replaces the transformation DAG; it does not
provide owner-scoped ingress, immutable versions, lease fencing, atomic
activation, or verified deletion. Record it as a PRD architecture-amendment
candidate, not a shortcut.

## Environment notes

- Go toolchain is **WSL only**:
  `wsl -e bash -lc 'export PATH=$HOME/.local/go1.26.3/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && <cmd>'`
- Never run a `.exe` on the Windows host.
- Windows Python cannot see MSYS `/tmp` — use the scratchpad path.
- `docker cp` and `psql -f` mangle POSIX paths; use `MSYS_NO_PATHCONV=1`, or pipe
  over stdin.
- Disposable databases only for any `db_integration` tier. A 2026-07-10 run
  truncated the live deployment's auth tables with no backup.
