# Document pipeline operator-driven E2E — handoff, 2026-08-05 (night)

Supersedes `document-pipeline-2026-08-05-evening.md` for everything it listed as open item 1.
Its carried-forward known-open list (§3) still stands, untouched.

## The one thing that changed irreversibly

**Migration 0093 is applied to the live database. `schema_migrations` = 93, clean.**

The previous handoff's licence to amend 0093 in place has expired. It expired at commit
`a8435155c`. Any further change to that migration needs a new migration.

## Where the plan stands

Spec: `docs/superpowers/specs/2026-08-05-document-pipeline-operator-e2e-design.md`
Plan: `docs/superpowers/plans/2026-08-05-document-pipeline-operator-e2e.md`
SDD ledger (gitignored, but the recovery map): `.superpowers/sdd/2026-08-05-document-pipeline-operator-e2e/progress.md`

| Task | State |
|---|---|
| 1 — Rehearse 0093 on a copy | complete, review clean |
| 1b — Fix 0093's 55006 failure | complete, review clean |
| 2 — Observability liveness | complete, 1 parked + 1 deferred minor |
| 3 — **Live cutover** | complete, 1 parked + 2 deferred minors |
| 4 — Probe harness | complete, review clean |
| **5–10 — OPERATOR-PAIRED checkpoints** | **NOT STARTED — resume here** |
| 11 — Phase 2 restart hook | not started |
| 12 — Hermetic two-tenant gate | not started |

Phase 0 evidence: `docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/PHASE0.md`
Findings ledger (CP1–CP7 stubs, ready to fill): same directory, `FINDINGS.md`

## Live state — the baseline every checkpoint asserts against

Measured after the cutover. **This supersedes every earlier baseline in every earlier document.**

```
schema_migrations              93 | clean
assets                         accepted 7, deleted 1
documents                      failed 3 (original_unavailable), deleted 1
document_versions              ready 1   (belongs to the DELETED document)
storage_objects                live 1
document_pipeline_stages       0 rows
document_pipeline_quarantine   6 rows
```

**Nothing is `ready`.** Any document reaching `ready` from here is necessarily newly ingested,
which is what makes the CP1 guard meaningful.

### Why the three `ready` documents became `failed`

Not damage. Those three had `active_version_id = NULL` and no retrievable version — the
3-ready-behind-1-version shape the recorder bug produced. They were presenting as usable
while having nothing behind them. 0093 reclassified them honestly rather than guessing an
owner. Independently confirmed in review; the dedup rule (`up.sql:169-181`) never fired, and
the 6 quarantined rows are preserved as JSONB. **Do not try to repair those rows.**

Consequence: the existing corpus is unusable as a fixture. CP1 starts by uploading fresh
files, which the plan already does.

## Things that were wrong and are now right — do not reintroduce them

These each cost a cycle. They are recorded so the next session does not pay again.

1. **The identity GUC is `app.current_identity`** (`internal/db/tx.go:120`), not
   `aura.identity_id`. Eleven checkpoint queries named the wrong one.

2. **Read as `aura_app`, never as `aura`.** `aura` is `rolsuper` with `rolbypassrls=t`: it
   returns every row with or without the GUC, so a probe run as `aura` cannot fail its own
   negative control. Verified contrast as `aura_app`: **0 rows without the GUC, 4 with**.
   `document_pipeline_quarantine` is the exception — `aura_app` is denied; use `aura_migrate`.

   Working shape:
   ```bash
   set -a; . /d/Aura/.env; set +a
   docker exec -e PGPASSWORD="$POSTGRES_PASSWORD" aura-postgres psql -U aura_app -d aura \
     -t -A -F'|' -c "BEGIN; SELECT set_config('app.current_identity',
     'dc98a3ee-e38e-4288-8d64-27ce4c9cde65',true); <SELECT ...>; COMMIT;"
   ```

3. **`pg_restore` must preserve ownership.** `--no-owner --role=aura` strips `aura_migrate`'s
   ownership of `schema_migrations`, and migrate then dies on a permission pre-flight before
   running any DDL — producing a rehearsal that proves nothing. Use plain `pg_restore`.

4. **`curl http://127.0.0.1:9080/` returns 401, and that is correct.**
   `redirectToLogin` (`internal/agui/auth.go`) 302s on `Accept: text/html` and 401s otherwise;
   bare curl sends `*/*`. With the browser header it 302s to `/login` → 200. Not an outage.

5. **`observability_sidecar_check.sh` must not use `-O /dev/null`.** MSYS rewrites that
   argument to `nul` before it reaches busybox wget in the container, producing a false
   "unreachable" on a healthy stack. It uses `-O -` with a host-side redirect, and
   `2>&1 >/dev/null` to surface wget's diagnostic — that ordering is correct and was
   verified empirically; a reviewer claimed otherwise and was wrong.

6. **Recreate `aura` BEFORE `tempo`/`prometheus`, always.** They share aura's network
   namespace. Recreated first, they survive as processes attached to a dead namespace,
   reporting `running`/`healthy` while unreachable. `docker restart aura` does the same.
   Grafana's healthcheck now catches it, but takes ~180s; `scripts/observability_sidecar_check.sh`
   is immediate and is the authority.

## The first upload failed, and that is the session's most useful finding

The operator started CP1 by uploading a file through the Cockpit. It returned **HTTP 400** at
`/api/assets/{id}/finalize`. Four assets are still in the database as evidence, all
`failed / processing_enqueue_failed`, with:

```
invalid ingestion job identity id "": invalid UUID length: 0
```

**Cause.** `assetProcessingIngestionJobRequest` (`cmd/aura/asset_processing_queue.go`) wrote
`identity_id` into the job's untyped `Payload` map but never set the request struct's own
`IdentityID` field. Go zero-values it, `createIngestionJobParams` rejects `""`, and every
finalize failed. **Document ingestion was broken outright** — not degraded, not slow, unable
to accept a single file.

**Fixed** in commit `488fa6714`: two lines (`IdentityID`, plus `AssetID`, which had a real
column but lived only in the payload map) and a regression test that fails without them. The
image has been rebuilt with the fix.

**Why it escaped.** Commit `8d2701bd1` ("Converge the industrial document pipeline") added
the field and its validation without updating this call site. The site lives in `cmd/aura/`,
which is excluded from the coverage floor on the grounds that it is "behaviourally covered".
Nothing behaviourally covers it. `make quality` passes green with ingestion completely
broken — a green build proved nothing about whether the product works.

That exclusion deserves reconsideration, and the four failed asset rows should be left in
place until it is.

**Not yet verified end to end.** The fix is unit-tested and the image is rebuilt, but no file
has been successfully ingested since. **CP1 must start by confirming an upload now reaches
`ready` with its own version row.**

## Resume instructions

The stack was left healthy and traces flowing. After a reboot:

```bash
docker compose up -d
docker compose --profile observability up -d --no-deps tempo prometheus   # AFTER aura is healthy
docker compose --profile observability up -d --no-deps grafana
bash scripts/observability_sidecar_check.sh          # must print "observability sidecars reachable"
docker exec aura-postgres psql -U aura -d aura -c "SELECT version, dirty FROM public.schema_migrations;"
```

Expect `93 | f`. The probe binary at `/tmp/aura-e2e-probe` (WSL) will not survive a reboot —
rebuild it:

```bash
wsl -e bash -lc 'export PATH=$HOME/.local/go1.26.3/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && \
  go build -o /tmp/aura-e2e-probe ./scripts/document_pipeline_e2e_probe.go ./scripts/document_pipeline_e2e_support.go'
```

Then resume at **Task 5**, which is operator-paired: the operator uploads
`documenti da stampare.pdf` (5 KB) first, then `Clienti.xlsx` and `TESEBRO000050EN.pdf`
(31 MB, chosen to hold the in-flight window open). Corpus:
`D:\tmp\aura-document-pipeline-references\document_ingestion\baseline-corpus`.

**Task 5 also captures the one value Phase 2 cannot run without**: the `convert` stage's
`producer_version` from `aura.document_pipeline_stages`. It is unknowable until a document is
converted post-0093, because 0093 creates that table. That is a hard data dependency of
Phase 2 on Phase 1 — the ordering is forced, not preferred.

## Rollback

`D:\tmp\aura-backups\aura-pre0093-2026-08-05.dump` — 537,381 bytes, taken from live before
the cutover and **proven restorable** (restored and asserted against three times). It is the
only rollback that exists. Do not overwrite it.

## Open work carried forward

Everything in the previous handoff's §3 and §4, plus:

- `ErrDocumentDeleteInFlight` is still not routed at the API edge. CP4 exists to measure what
  it actually surfaces; expect a defect.
- Neither the rehearsals nor the cutover asserted **status distributions** — only row counts,
  null-ness, table existence and the partial index. That blind spot is exactly what hid the
  `ready → failed` reclassification through three rehearsals. Tasks 5–10 now open with a
  measured distribution baseline and assert against it, but the same gap applies to any
  future migration.
- Three orphaned disposable databases from earlier sessions remain on Postgres:
  `aura_phase4_migrate_drill`, `aura_migratesteps_drill`,
  `aura_pipeline_0a0469f3cd4a4e00889fba07b1f89582`. Not this plan's scope, not deleted.

## Process note

Every task in this plan was caught by verification rather than by its implementer's report:
the restore recipe, the 55006 root cause, an incomplete FK inventory, the MSYS false negative,
the wrong GUC name, and a superuser read whose negative control could never fail. Several of
those were defects in the plan itself, written by the controller.

Two of them are the same failure in different clothes — an instrument that reports success
because it cannot fail. Treat any "green" from this subsystem as a claim to be reproduced,
not a result.
