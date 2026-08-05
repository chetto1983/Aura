# Document pipeline operator-driven E2E — design

Date: 2026-08-05
Status: approved (shape), pending plan
Supersedes nothing. Consumes `.planning/handoffs/document-pipeline-2026-08-05-evening.md` open item 1.

## Goal

Prove the document pipeline end to end on the production path, through the surface a human
actually uses, and close Amendment #115/#116.

Two runs, serial, different purposes:

- **Phase 1 — interactive.** The operator drives the Cockpit; Claude asserts the backend
  underneath at defined checkpoints. Proves the human story and the four gaps the headless
  script structurally cannot reach.
- **Phase 2 — unattended.** Claude runs `scripts/document_pipeline_e2e.sh`, the hermetic
  two-tenant gate. It restarts the service mid-run, so no operator can be in the Cockpit
  while it executes.

Phase 1 first: it is short, it covers the fixes that shipped 2026-08-05, and any blocker it
finds would have failed Phase 2 an hour deeper in.

Target: the local Windows Docker stack, Cockpit at `127.0.0.1:9080`. Not the appliance. A
repeat on `192.168.1.225` is out of scope here and gets its own decision once this runbook
is proven.

## Measured ground truth (2026-08-05, before any change)

Everything below was probed on the live stack, not inferred. It is recorded because three of
the four items contradict what the handoff or a plain reading of `compose.yaml` implies.

| Claim | Evidence |
|---|---|
| Live DB is at 92, clean; `source_kind` absent | `aura db status` → `92 / false`; `information_schema.columns` returns only `deleted_at`, `status` |
| `aura-migrate` exited 0 **without** applying 0093 | The running binary (`/usr/local/bin/aura`, built Aug 3 22:44) contains **zero** occurrences of `0093_document_pipeline_convergence`. Migrations are embedded; the stale image has no 0093 to apply. |
| The OTLP exporter is fail-soft | `"otel error" ... dial tcp 127.0.0.1:4317: connection refused` logged at WARN with `suppressed:4`. App stayed healthy throughout. It self-healed: last error 16:08:46, tempo up 16:08:51, zero errors after. |
| tempo/prometheus never crashed | Both `FinishedAt = 2026-08-04T06:23:51.079`, identical to the microsecond, exit 255, `OOMKilled=false`, logs ending on ordinary INFO lines. Two independent processes cannot fail identically at one microsecond — they were killed with their netns owner. |
| `docker restart aura` leaves the sidecars **falsely green** | After restart both report `running` / `healthy` and are unreachable: `wget http://aura:3200` → `Connection refused (172.19.0.11)`. Recovery requires `--force-recreate` of the sidecars. |
| The recorder bug's shape is present in live data | `aura.documents` → 3 `ready`, 1 `deleted`; `aura.document_versions` → **1 row**. Three ready documents with no version behind them. |
| 0093 admits `deleted` | Its new `documents_status_check` lists `accepted, stored, queued, converting, chunking, embedding, projecting, ready, failed, dead_letter, deleting, deleted` — so the live `deleted` row does not trip it. |

### Consequence 1 — the one-way door is the rebuild

0093 lands when the **image is rebuilt**, not when the stack comes up. It replaces
`documents_status_check`, adds `source_kind`, swaps `documents_source_unique` for the partial
live-only index, and remaps existing `processing` rows to `converting`. The handoff's licence
to amend 0093 in place expires at that moment.

A `pg_dump` of live `aura` precedes the rebuild. The 2026-07-10 precedent — live auth tables
truncated with no backup — is this exact shape.

### Consequence 2 — the observability healthchecks lie

`tempo`'s healthcheck is `-config.verify=true`, which validates a file and never touches the
network. `prometheus`'s is `wget 127.0.0.1:9090/-/ready` from **inside its own orphaned
namespace**. Both pass while the component is unreachable from everything else.

This is a falsely-green signal in the sense CLAUDE.md forbids, and it is directly material:
Phase 2 restarts `aura` mid-run, so without a fix the backend lens goes blind at exactly the
lease-reclaim assertion, with no indication.

Both healthchecks get replaced with reachability probes as part of Phase 0. Liveness for a
netns sidecar must be asserted **from outside the namespace**.

## Division of labour

**Operator — frontend.** Drives the Cockpit only. Never runs a probe, never reads a row.
What the operator sees is the evidence for the UI half; if a status is wrong on screen, that
is a finding regardless of what the database holds.

**Claude — backend.** Three lenses, in descending authority:

1. `scripts/document_pipeline_e2e_probe.go`, pointed at the operator's identity. It already
   encodes the hard assertions and goes through `db.WithIdentityTxRaw`. `status` needs only
   `--identity --asset`; `snapshot` additionally needs the three `expected-*` values and a
   scratch `--state` path. Reuse, not rewrite.
2. Tempo/Grafana as the live lens — the Docling convert and the agent's tool sequence watched
   as they happen rather than reconstructed from rows.
3. Read-only `psql` for what neither covers. **Every query wrapped in the identity GUC.** RLS
   (migration 0087) returns zero rows to a naked `pool.Query`, so an unwrapped probe passes
   against nothing — worse than no probe.

No `db_integration` test runs at any point during Phase 1. The tier's helpers migrate
whatever `AURA_DB_URL` names, which locally is the live `aura`.

## Phase 0 — preconditions

Ordered. The order is proven by the tests above, not chosen for convenience.

1. `pg_dump` live `aura` to a timestamped file outside the repo. Verify the dump is non-empty
   and restorable-looking before proceeding. This is the rollback for 0093.
2. Rebuild `aura:local` from HEAD. **This is the one-way door** — the new binary is the first
   one anywhere to carry 0093.
3. **Rehearse 0093 on a copy, before it ever touches live.** Restore the step-1 dump into a
   disposable database (`aura_0093_rehearsal`), point the new binary's migrate at it, and
   assert it reaches `93 / clean`. Drop the database afterwards.

   This exists because 0093 is not a pure schema change on this data: it adds `source_kind`
   and `source_key` as nullable, backfills them (`'legacy'`, `'document:'||id`), dedups any
   colliding `(identity_id, source_kind, source_key)` groups, and only *then* tightens to
   `NOT NULL` plus non-empty CHECKs. Whether that sequence survives contact with the four
   live rows is an empirical question, and the cheap place to answer it is a copy.

   A rehearsal failure stops Phase 0 with live untouched and zero recovery needed.
4. Apply to live: recreate `aura`; `aura-migrate` runs. Assert `aura db status` → `93 /
   clean` **and** `source_kind` present in `information_schema.columns`. A dirty version or a
   missing column triggers the restore path.
5. Recreate `tempo` and `prometheus` — **after** step 4, never before. They die silently with
   a recreated netns owner.
6. Assert traces flow by querying Tempo for a span with `rootServiceName: aura`. Never accept
   a healthcheck as proof.
7. Replace the two lying healthchecks with reachability probes.
8. Record the operator's identity UUID and the four `EXPECT_*` values (model, embed model,
   embed version, Docling producer) read off the live deployment. Phase 2 needs them and they
   must describe the rebuilt image, not the old one.

Cockpit is confirmed reachable at `127.0.0.1:9080` before the operator is asked to do
anything.

## Phase 1 — the interactive pass

Seven checkpoints. Smallest file first; the 31 MB PDF is checkpoint 2, not checkpoint 1.
Corpus: `D:\tmp\aura-document-pipeline-references\document_ingestion\baseline-corpus`.

Upload is **presign → PUT to Garage → finalize**, and asset status is distinct from document
status: terminal asset values are `searchable | complete`, and `failed | refused |
dead_letter | canceled` are terminal failures.

Each checkpoint states what the operator does, what Claude asserts, and what counts as pass.
A checkpoint that fails is recorded and the pass continues where the failure is not blocking —
the point is to surface the full defect set in one sitting, not to stop at the first.

### CP1 — first upload, status vocabulary on a real row

*Gap: status rendering.*

- **Operator:** upload `documenti da stampare.pdf` (5 KB). Watch which tab it appears in,
  what badge and tone it carries, and whether it moves without a manual refresh.
- **Claude:** poll `aura.documents.status` and `aura.document_versions.status` inside the
  identity GUC for the whole window.
- **Pass:** the document reaches `ready`; **a `document_versions` row exists**; the observed
  status sequence is drawn only from `AllDocumentStatuses`; the Garage object exists.

The version-row assertion is the specific regression guard. The live database currently shows
three `ready` documents behind a single `document_versions` row — the shape the recorder bug
produced.

### CP2 — the long window

*Gap: status rendering, in flight.*

- **Operator:** upload `Clienti.xlsx` (331 KB), then `TESEBRO000050EN.pdf` (31 MB). The large
  file exists to hold the in-flight window open long enough to observe.
- **Claude:** follow the Docling convert in Tempo; sample document and version status
  throughout; record every distinct status observed.
- **Pass:** the set of statuses the UI renders during the window matches the set the database
  actually holds. A status the UI shows that no row ever held, or a row status the UI cannot
  render, is a finding either way.

The handoff notes the UI's in-flight set deliberately spans reachable and aspirational
statuses, with a comment saying so. This checkpoint measures which are reachable; it does not
license deleting the others.

### CP3 — the agent cites the document

- **Operator:** in the Cockpit chat, ask the TORINO question against `Clienti.xlsx`.
- **Claude:** read the tool sequence from the trace; assert the answer and the persisted
  conversation model via `probe conversation`.
- **Pass:** `document_search` is called; the answer contains **699**; the persisted
  conversation model equals the expected production model; no `web_search` / `web_fetch`.

### CP4 — the delete-in-flight window

*Gap: delete-in-flight. A defect is the expected outcome.*

- **Operator:** delete `Clienti.xlsx` from the action menu, then **immediately** re-upload the
  same file, inside the async window before finalize.
- **Claude:** confirm the row is at `status='deleting'` with `deleted_at` still NULL, and
  capture the HTTP status and body the API edge actually returns.
- **Pass criterion is honesty, not success:** `ErrDocumentDeleteInFlight` is known not to be
  routed at the API edge. Record the real status code and what the UI showed. If it surfaces
  as a 500 or as silence, that is the finding, and it becomes work — not a Phase 1 failure.

### CP5 — delete then re-upload, after finalize

*Gap: repeat-source ingest. This is the production proof the handoff says is owed for commit
`7248b5880`.*

- **Operator:** wait for the document to disappear from the library, then re-upload the same
  `Clienti.xlsx`.
- **Claude:** assert a fresh live document with a working version row, no `23505`, and the old
  row tombstoned with `deleted_at` set.
- **Pass:** the re-upload produces a usable document. The unit work proved the seams; only
  this proves the story.

### CP6 — the workspace surfaces

*Gap: the rest of the workspace.*

- **Operator:** open the details drawer; open the events panel; use filters and search; rename
  a document; open the storage-orphans panel.
- **Claude:** assert the rename persisted via PATCH; the events panel matches
  `aura.document_events`; the orphans panel matches `StorageOrphanService.DryRun`.
- **Pass:** each surface reflects backend state. Divergence is a finding.

### CP7 — teardown and ledger

- **Operator:** delete the remaining test documents through the UI.
- **Claude:** assert tombstones and Garage 404 HEADs for each; write the findings ledger.
- **Pass:** no orphaned objects; every CP's outcome recorded with evidence.

## Phase 2 — the hermetic script

Runs after Phase 1's findings are triaged and any blocker is fixed. Unattended.

Setup work it needs, none of which exists today:

- `AURA_DOCUMENT_E2E_RESTART_HOOK` — an absolute, executable, non-symlink file owned by the
  WSL operator, not group/world writable. **It must recreate `tempo` and `prometheus` after
  restarting `aura`**, or observability dies silently at the lease-reclaim assertion. This is
  the direct consequence of the falsely-green finding.
- The four `EXPECT_*` values, from Phase 0 step 8.
- `AURA_DOCUMENT_E2E_PRODUCTION_CONFIRM=I_ACKNOWLEDGE_PRODUCTION_E2E`, base URL
  `http://127.0.0.1:9080`, an absolute WSL report path.

Cost to expect: 14 ingests including a 31 MB PDF and a 21 MB PPTX, plus 4 real LLM agent
turns. Budget over an hour. The corpus is `realpath`-locked to the exact seven files, so
there is no smoke subset — the first run is the full run.

The script provisions and cleans up its own two identities, so it does not collide with the
operator's Cockpit identity.

## Risks

| Risk | Handling |
|---|---|
| 0093 breaks live data on first application | Rehearsed on a restored copy first (Phase 0 step 3); `pg_dump` taken before that; `0093.down.sql` exists; `93 / clean` asserted immediately after the live run |
| 0093's backfill collides on live rows | The rehearsal is exactly this test. Live has 4 documents (3 `ready`, 1 `deleted`) and 1 `document_versions` row — measured, and small enough that a collision is inspectable by hand |
| A probe passes against zero rows under RLS | Every direct query goes through the identity GUC; the probe CLI already uses `WithIdentityTxRaw` |
| Observability silently blind after a restart | Reachability healthchecks in Phase 0; sidecar recreation inside the Phase 2 restart hook |
| Phase 1 leaves residue in live `aura` | CP7 teardown asserts tombstones and Garage 404s |
| A `db_integration` run wipes live data | No tagged tier runs during either phase |

## Out of scope

- The appliance (`192.168.1.225`). Decided after this runbook is proven locally.
- The carried-forward known-open list in the handoff (dead sqlc queries, missing per-job
  timeouts, the duplicated wiring block). Findings only; fixes are separate work.
- Binding the TypeScript `DocumentStatus` union to Go's `AllDocumentStatuses`.
- Making the aspirational lifecycle states reachable.
