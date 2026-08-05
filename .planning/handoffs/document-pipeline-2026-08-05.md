# Document pipeline handoff — 2026-08-05

Supersedes `document-pipeline-2026-08-04.md`. Several of that document's
statements are now stale or were wrong; the corrections are called out below so
the next session does not re-derive them.

## State

**The worktree is clean.** Three commits landed, every lefthook gate green
(gofmt, file-size, vet, lint):

```
a974cfb22  Give the nightly Postgres backup a clock      (operator's work, own commit)
8d2701bd1  Converge the industrial document pipeline     (163 files)
fe20c2c72  Rule passage erasure physical, not tombstoned (PRD amendment #117)
```

Nothing has been pushed. `origin/master` is still at `975b7d521`.

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

## Open work, in order

### 1. F3 — same-SHA replay wiring (the only remaining finding)

`ReserveCandidateVersion` still has **zero production callers**. The real ingress
can attach a newly written raw object to an old immutable version, and a repeat
ingest redoes Docling + embed + project work.

Its one unproven assumption is now **proven**: a single statement can insert a
storage object referencing a not-yet-existing version and the version referencing
that object, because both FKs are `DEFERRABLE INITIALLY DEFERRED` (0093:378-394).
Verified on a disposable database (`aura_f3_probe`, 68 migrations): committed,
`versions=1 objects=1`. Probe DB dropped; live `aura` untouched.

The corrected design (first pass was rejected on review):

1. rewrite `ReservePipelineCandidateVersion` so one statement owns the raw
   object, the version and the asset binding, projecting a `replay_is_active`
   boolean computed from `status='ready' AND activated_at IS NOT NULL AND
   document.active_version_id = existing.id`
2. surface it as `CandidateVersion.ReplayedActive` / `DocumentVersionRecord.ReplayedActive`.
   **Do NOT add a bare `Replayed` to `DocumentVersionRecord`** — a caller that can
   see identity without liveness will short-circuit on a still-`processing`
   version and mark an unindexed document searchable.
3. route the real recorder through it; delete the four-statement body it replaces
4. `internal/assets/document_processor.go`: short-circuit **only** on
   `ReplayedActive`; every other case falls through to the existing
   `p.Pipeline.Run`, so `beginStage` resumes from the ledger as it does today.
   The unit test needs both cases, and the not-live case must assert `Run` WAS called.
5. `linked_asset` binds `sqlc.arg(asset_id)`; use
   `GREATEST(asset.pipeline_generation, selected.pipeline_generation)` — never lower it
6. **Do not delete `CreateStorageObject`** until the duplicate asset's bytes have
   an owner, or `SoftDeleteDocument`'s object sweep stops reaping them. Preferred:
   register the duplicate's object as an additional non-raw ledger row bound to
   the same version.

Tests (all `db_integration`, all via `pipelineDisposablePool`, whose
`pipelineEnvOrSkip` already `t.Fatal`s under `$CI`):
- **T1** drive the production `PostgresCatalogStore.RecordAssetVersion` twice —
  two assets, same SHA, different object keys. Assert exactly 1 live version,
  exactly 1 live `kind='raw'` object, `asset_id` still A1, A2 bound to V1.
- **T2** replay onto a still-`processing` version → `ReplayedActive == false`,
  and a daemon-free `internal/assets` test asserting `Pipeline.Run` ran.
- **T3** replay onto an activated version → `ReplayedActive == true`, `Run` not called.

**Split out of F3, own commit:** the repeat-source `23505`. It needs 0093 amended
— replace `ADD CONSTRAINT documents_source_unique UNIQUE (...)` (:306) with a
partial unique index `WHERE deleted_at IS NULL`, mirroring its sibling at :308,
and mirror the DROP in the `.down.sql`. Only then may `CreateDocument` take an
`ON CONFLICT ... DO UPDATE`. Without the partial index, delete-then-reingest
cannot create a fresh document.

### 2. Production E2E (Amendment #115/#116)

Corpus verified intact — exactly the 7 authorized files under
`D:\tmp\aura-document-pipeline-references\document_ingestion\baseline-corpus`,
`Clienti.xlsx` present for the 699/TORINO ground truth.

**Run it after F3, not before**: F3 changes the version and activation counts the
E2E asserts, so a green run now would be misread either way.

### 3. Before any push

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
