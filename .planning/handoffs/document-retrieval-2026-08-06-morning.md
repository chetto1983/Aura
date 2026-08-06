# Handoff — document retrieval, 2026-08-06 morning

Continues `document-pipeline-operator-e2e-2026-08-05-night.md`, which closed with CI red on
`db_integration` and named that the next session's first job. It is done, plus two production
defects the red tier was hiding.

**Branch:** `master` @ `62208c52d` — pushed, working tree has uncommitted operator work (see
§Working tree, read it before you build anything).
**CI:** green on `62208c52d` — CI ✅ Skills ✅ CodeQL ✅.
**Live:** deployed and verified; stack healthy.

---

## What shipped

| Commit | What |
|---|---|
| `7dd7ca6ac` | Fixed the 42P01 alias in `SearchDocumentDigests`; rebuilt six `db_integration` fixtures against the 0093 publish contract |
| `36622ae99` | Publish fixtures write a real hex sha256 (`%064s` pads with SPACES, not zeros) |
| `62208c52d` | **The card leg answers an unscoped search** — the defect the operator actually felt |

### 1. `SearchDocumentDigests` — malformed SQL (real, but narrower than first reported)

`v.pipeline_generation = aura.documents.pipeline_generation` inside a statement whose FROM
clause says `FROM aura.documents d`. Aliasing removes the original name from scope, so every
execution raised `42P01`. Postgres named the fix in its own HINT.

**A claim made earlier in this session is WITHDRAWN.** It was reported as breaking
`document_search`, `aura docs search` and `GET /api/documents`. It was not. **`SearchDigests`
has ZERO production callers** — only tests reach it. `document_search` runs through
`HostRetriever` on the `lexical-dense-card-cascade-v1` cascade. The blast radius was the red
`db_integration` tier, not operator retrieval. The claim came from reading the method's doc
comment instead of grepping for callers; the operator's own chat transcript disproved it.

That leaves an open question in §Remains: a fully documented, fully tested retrieval method
with no callers, whose SQL was broken for weeks *because* nothing calls it.

### 2. The card leg returned nothing for every unscoped search (the real one)

An absent scope means "every ready document". It reaches `RouteDocumentCards` as a **nil**
`[]string` — `ResolveDocumentScope` returns nil early when the caller named no ids — and pgx
encodes a nil slice as SQL **NULL**, not an empty array. The predicate was

```sql
$3::text[] = '{}'::text[] OR document.id::text = ANY($3::text[])
```

Under three-valued logic both halves are NULL when `$3` is NULL, so every row was filtered.
Reproduced against a published document: nil scope → **0 cards for every query**; empty
non-nil slice → the document.

Two consequences, both operator-visible:

- The leg exists so a file is findable by its machine card the moment it lands, and it was
  contributing nothing. Measured live: `document_search("Clienti")` returned nothing for a
  ready, activated `Clienti.xlsx` while `document_search("Clienti.xlsx")` found it. Recall was
  resting entirely on the ArcadeDB passage legs. **Nobody types the exact filename** — that is
  the defect the operator reported.
- Sharper: when ArcadeDB is unreachable `Retrieve` degrades to card-only and returns exactly
  these cards. So a degraded retrieval returned an **empty** result set while reporting status
  `degraded_card_only` — total failure dressed as partial.

Fixed with `cardinality(coalesce($3::text[], '{}'::text[])) = 0`. Swept the tree: this was the
idiom's only occurrence.

`TestRouteDocumentCardsAnswersAnUnscopedQuery` pins it and **was confirmed to fail against the
old predicate before being kept**. It asserts the nil case, that nil and empty agree, that a
named scope still narrows (so "ignore `$3`" cannot pass), and that an unrelated query still
returns nothing.

### 3. Six fixtures rebuilt on the 0093 contract

The tier had been red since the convergence, so it could not warn about anything. Its fixtures
still described the pre-0093 world where ingest published a document the instant it registered
one. Publication is now a separate act: `ready` only at a positive generation, over an active
version above a live raw object whose asset is alive.

Two were worse than stale — they could not fail:

- The RLS negative controls would have been refused by a `NOT NULL` constraint whether the
  policy held or failed open. They now share `rlsDocumentInsert`, which spells out every
  mandatory column, so the ONLY reason those inserts can fail is the policy.
- A status-reason check compared `""` to `""`. `SetDocumentStatus` writes the
  `error_message` column; the test read a metadata key nothing sets.

Also: `TestOwnedIngestCreatesAReadyCatalogRow` asserted a job status nothing has written since
the convergence deleted the transition. Renamed `...AStoredCatalogRow`. `types.go` still
carried a comment claiming `accepted → searchable` happens — the convergence replaced the
sentence above it and left that one behind. **`JobSearchable` is dead exactly like
`JobComplete`; a job resting at `accepted` after a completed ingest is the design, not a
stall.**

`publishForSearch` (documents) and `musrPublishDocument` (cmd/aura) build asset + raw object +
candidate version through `RecordAssetVersion`, the production statement, then write the
columns activation writes. They deliberately stop short of `ActivateCandidate`: it refuses a
candidate with no passages (`expected_passage_count > 0`), and fabricating a chunk, an
embedding vector and a projection commit inside a text-ranking test would bury the subject.
`TestPipelineStoreActivatesOnlyAFencedCandidate` already owns that contract.

---

## Verified live, not inferred

**The pipeline is healthy end to end.** Two spreadsheets uploaded through the repaired ingest
path both reached `ready` at `pipeline_generation = 1` with `candidate_activated` recorded in
`aura.ingestion_events`. The "upload spins at 100%" report was a 42-second convert+embed+
project+activate, not a hang.

**Deletion is clean.** Audited every store after the operator deleted all documents:

| Store | Result |
|---|---|
| `aura.documents` | 0 live |
| `document_chunks` / `document_embeddings` | **0 rows, physically** — not merely flagged |
| Versions + objects for all 5 documents deleted today | 0 live, 0 live |
| Garage `aura-assets` | 17 objects = 16 live assets + 1 orphan — **reconciles exactly** |
| ArcadeDB `Passage` / `DocumentProjection` / `HAS_PASSAGE` | **0** |
| ArcadeDB memory | intact — 19 `Entity`, 23 `FACT` |

**Deployment.** `aura:local` rebuilt and the binary itself checked before deploying (the
pre-built image was stale — it predated the card-leg fix):

```
card-leg fix        PRESENT
digest alias fix    PRESENT
stale alias         gone
```

Running container image id == tag id (`7662b8f3…`). All sidecars healthy.

---

## Remains

### Retrieval — do this first

1. **recall@1 is `UNKNOWN`** in `docs/aura-quality-snapshot.md` and has been for days. It gates
   everything else in this section; we cannot tune what we have not measured. Fixture corpus:
   `D:/turing_AgentMemory_MCP/test` (Clienti.xlsx ground truth + ~18k Normattiva PDFs).
2. **The card-leg fix is not proven end to end.** It is pinned by a test confirmed to fail
   against the old SQL, but no real upload has been searched by partial word since. One upload
   + one partial-word search settles it, and doubles as (1)'s smoke.
3. **`SearchDigests` / `SearchDocumentDigests` is dead code.** Zero production callers, fully
   tested, and its SQL was broken for weeks precisely because nothing calls it. Delete it, or
   record what it is staged for. CLAUDE.md forbids dark code.
4. **Known ranking gaps**, if the number comes back low: the config is `simple`, so there is no
   stemming (`venditori` will not match `venditore` — documented in the SQL as an accepted
   trade), and there is no trigram/fuzzy fallback for typos.

### Bytes with nothing behind them — one cleanup pass

5. **One orphan from 2026-08-03.** Version `3e17918a-22e7-4988-8286-02a03a70f961` is still
   `ready` and raw object `7f8ae047-afe8-4304-93ce-4fee2dc37e5b` still `live`, for document
   `1f79970c` (Clienti.xlsx) deleted 2026-08-03. **The original bytes are still in Garage.**
   Predates durable deletion (`8d2701bd1`, 08-05), so it is legacy debris rather than a live
   defect — but it is a file the operator asked to erase and it is still there.
6. **7 assets stuck at `accepted`** (source `agent`, 2026-08-04). `aura.ingestion_jobs` has zero
   non-terminal rows, so nothing is driving them. Orphaned mid-pipeline, still holding bytes.
7. **9 assets `failed`** (source `web`, 2026-08-05) — the Lista Fornitori retries. Terminal but
   still holding bytes. Possibly deliberate for retry/debug; it deserves an explicit decision.

### Structural

8. **`arcadedb_integration` runs NOWHERE.** 10 test files carry the tag; not CI, not the
   Makefile, not the coverage scripts. It is not even `go vet`-compiled, so it can rot
   silently. The memory substrate is untested in the pipeline. (Carried from the previous
   handoff, still open.)
9. **Task 11 — restart preflight.** Hit manually today; see §Gotchas. It is a small script and
   the failure is guaranteed on every deploy.
10. **CocoIndex** — evaluated, parked. See §CocoIndex.
11. **E2E tasks 5–10** (CP1–CP7, operator-paired) and task 12 (hermetic two-tenant gate)
    untouched.

---

## CocoIndex (evaluated 2026-08-06, verified against the repo, not the landing page)

Rust core + Python API, Apache-2.0, 11.2k stars, pushed same-day. Internal state in **LMDB**
(`COCOINDEX_DB`). 20 connectors: s3, azure_blob, bigquery, doris, falkordb, google_drive, iggy,
kafka, lancedb, localfs, neo4j, oci, postgres, qdrant, snowflake, sqlite, surrealdb,
turbopuffer, valkey, zvec. **No ArcadeDB** (0 code hits). **No Go bindings** (no `go.mod`).
Custom targets are Python: a `TargetHandler` protocol with a non-blocking `reconcile()`, a
`TargetActionSink.from_fn/from_async_fn`, and `register_root_target_states_provider`.

**Recommendation: not for retrieval.**

- The defect the operator felt was `NULL = '{}'` in one predicate. No framework would have
  caught it.
- What CocoIndex overlaps is the pipeline just shipped — Docling → chunk → embed → project →
  atomic activation with generations, fencing, leases, verified-erasure deletion. Adopting it
  means retiring 0093 and writing the ArcadeDB side in Python.
- **Multi-tenancy is the blocker.** Each identity gets its own ArcadeDB database with an
  HMAC-derived credential, enforced server-side. A CocoIndex flow targets *a* configured store;
  per-identity fan-out across N tenant databases with N credentials fights its shape. Its LMDB
  state is single-process, and Postgres — not the index — owns which version is active.
- It does not improve ranking. Quality lives in the cascade and the card text, not the ETL.

**Where it would earn its keep: sources, not targets.** Google Drive, S3, Kafka with
incremental freshness. Aura has nothing there — today a document enters only by upload or a CLI
path. "Aura watches my Drive and stays current" is a real capability gap and CocoIndex's core
competence. Separable from retrieval; worth its own discussion.

---

## Working tree — READ BEFORE BUILDING

One uncommitted thing, not mine:

- **6 modified files under `web/src/documents/`** — operator work in progress on the upload
  flow (`documentUpload.ts`, `DocumentsWorkspace.tsx`, `DocumentUploadDialog.tsx` + tests).
  These were swept into a commit by `git add -A` and **split back out before pushing**; they
  are untouched. Do not `git add -A` — stage explicitly.

Transient, already resolved, recorded because it will recur: mid-session
`internal/webui/dist/` went **empty** (95 files deleted, `assets/` at 0) while a Vite build was
in flight — it cleans `outDir` first. In that window **`go build` fails on the `go:embed`**,
and CI's D-05 "web dist freshness" job compares the committed dist to a fresh build. It
repopulated on its own and now matches HEAD byte-for-byte (94 assets, no diff). If you find it
empty again, finish the build (Linux Node 24) or `git restore internal/webui/dist` — do not
commit the deletions.

The running container is unaffected by any of this: the UI is `go:embed`-ed into the binary,
and the image was built while dist was intact.

The running container is unaffected: the UI is `go:embed`-ed into the binary, and the image was
built while dist was intact. `GET :9080/` answers `401` (Authula), i.e. the server is up.

---

## Gotchas earned this session

**Recreating `aura` orphans the netns sidecars.** `tempo` and `prometheus` use
`network_mode: "service:aura"`, so `docker compose up -d aura` destroys the namespace they
live in. `docker compose restart` **cannot** fix it and fails outright:
`joining network namespace of container: No such container: <old-id>`. You must
`up -d --force-recreate tempo prometheus`, then wait — tempo serves `503` on `/ready` for a
while. Grafana's healthcheck probes both, so **grafana `unhealthy` after an aura deploy is a
symptom of this, not a grafana fault**. Verify with
`bash scripts/observability_sidecar_check.sh`. Saved to project memory.

**Running the tiered tests safely.** They migrate whatever `AURA_DB_URL` names, which locally
is the LIVE database. Create a disposable DB and hand the tests only that DSN:

```bash
docker exec -i -e PGPASSWORD="$PGPW" aura-postgres psql -U aura -d postgres \
  -c 'DROP DATABASE IF EXISTS aura_it WITH (FORCE)' -c 'CREATE DATABASE aura_it OWNER aura_migrate'
# then export AURA_DB_URL/AURA_DB_MIGRATE_URL at aura_it, POSTGRES_PASSWORD, CI=true
```

`bootstrapURL` hardcodes `/aura` for `EnsureRoles` — that is safe (roles + grants only, no
table DDL/DML), verified by reading it, but do not widen what you point at that DSN.

**WSL, not the Bash tool.** The Bash tool here is Git Bash (MSYS). Go work runs in WSL via
`wsl -e bash …` with `PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"`. Prefix `MSYS_NO_PATHCONV=1`
or MSYS mangles `/mnt/...` arguments into `C:/Program Files/Git/mnt/...`.

**Backticks inside a Go raw-string SQL comment terminate the literal.** Cost one confusing
`expected ';', found NULL` while commenting the card-leg fix.

---

## Verification performed

Everything below was run locally in WSL rather than push-and-wait:

- `make quality` — green (deadcode, vet, build, file-size, embedding-model-contract, lint,
  test-race, vuln)
- `bash scripts/coverage_docker.sh` — **85.3%** owned surface (≥85 floor) on a disposable
  `aura_cov`
- `go test -tags db_integration -p 1 ./internal/documents/ ./internal/db/...` — green
- `musr_e2e` `TestTwoIdentityCrossDeny/documents_cross_deny` — run live, green
- `scripts/quality_snapshot_gate.sh` — `ok: checked 1 row(s)`
- Negative control: the new regression test **fails** against the old predicate
- CI on `62208c52d` — CI ✅ Skills ✅ CodeQL ✅

The quality-snapshot row *Document routing recall@1* carries the full account, including the
withdrawal. It remains **UNKNOWN, not green** — the live corpus is empty because the operator
deleted every document.
