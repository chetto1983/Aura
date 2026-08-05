# PHASE0 — Rehearse migration 0093 against a copy of live data

Date: 2026-08-05
Task: Task 1 (and Task 1b) of `docs/superpowers/plans/2026-08-05-document-pipeline-operator-e2e.md`
Verdict: **RESOLVED by Task 1b — 0093 now reaches `93 / clean` on a faithful copy of live
data.** See "Task 1b — the fix and Attempt 3" below for the corrected root cause, the
one-line fix, and the successful re-rehearsal. The narrative below (Attempts 1 and 2) is
retained verbatim as the historical record of how the failure was found; its root-cause
paragraph at the end of Attempt 2 has been superseded — do not act on it, see the
correction in the Task 1b section.

## Baseline (measured before any action)

Live `aura` database, confirmed identical to the numbers given in the task brief:

```
$ docker exec aura-postgres psql -U aura -d aura -c "SELECT version, dirty FROM public.schema_migrations;"
 version | dirty
---------+-------
      92 | f
(1 row)

$ docker exec aura-postgres psql -U aura -d aura -c "SELECT status, count(*) FROM aura.documents GROUP BY status ORDER BY 1;"
 status  | count
---------+-------
 deleted |     1
 ready   |     3
(2 rows)

$ docker exec aura-postgres psql -U aura -d aura -c "SELECT count(*) FROM aura.document_versions;"
 count
-------
     1
(1 row)
```

No deviation from the plan's stated baseline. Proceeded.

## Step 1 — Rollback dump

```
$ export MSYS_NO_PATHCONV=1   # Git Bash mangles /tmp/... into a Windows path otherwise
$ mkdir -p /d/tmp/aura-backups
$ docker exec aura-postgres pg_dump -U aura -d aura -Fc -f /tmp/aura-pre0093.dump
(exit 0)
$ docker cp aura-postgres:/tmp/aura-pre0093.dump /d/tmp/aura-backups/aura-pre0093-2026-08-05.dump
(exit 0)
$ ls -la /d/tmp/aura-backups/aura-pre0093-2026-08-05.dump
-rw-r--r-- 1 Davide 197121 537381 Aug  5 18:25 /d/tmp/aura-backups/aura-pre0093-2026-08-05.dump
```

**Dump file:** `D:\tmp\aura-backups\aura-pre0093-2026-08-05.dump`
**Size:** 537,381 bytes (~525 KB) — well above the "tens of KB" floor, not zero-byte.

## Step 2 — Prove the dump restores

```
$ docker exec aura-postgres psql -U aura -d postgres -c "DROP DATABASE IF EXISTS aura_0093_rehearsal;"
DROP DATABASE
$ docker exec aura-postgres psql -U aura -d postgres -c "CREATE DATABASE aura_0093_rehearsal OWNER aura;"
CREATE DATABASE
$ docker exec aura-postgres pg_restore -U aura -d aura_0093_rehearsal --no-owner --role=aura /tmp/aura-pre0093.dump
(exit 0)
$ docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c "SELECT version, dirty FROM public.schema_migrations;"
 version | dirty
---------+-------
      92 | f
(1 row)
$ docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c "SELECT status, count(*) FROM aura.documents GROUP BY status ORDER BY 1;"
 status  | count
---------+-------
 deleted |     1
 ready   |     3
(2 rows)
```

Matches live exactly: `92 | f`, `deleted 1`, `ready 3`. The dump is faithful. **Rollback is proven usable.**

## Step 3 — Build the image that carries 0093

```
$ docker compose build aura
... (full multi-stage build, Python deps + Go binary + garage sidecar) ...
 Image aura:local Built
$ docker images --format "{{.Repository}}:{{.Tag}} {{.ID}} {{.CreatedAt}}" | grep "^aura:local"
aura:local 574a043c50bb 2026-08-05 18:28:38 +0200 CEST
```

Fresh image, built 2026-08-05 (today) — not the stale 2026-08-03 22:44 image that was known to
contain zero occurrences of `0093_document_pipeline_convergence`.

## Step 4 — Confirm the binary carries the migration

The `aura:local` image's shell has no `strings` binary (minimal image — `which strings` fails).
Substituted `grep -a` (GNU grep 3.8, present in the image), which is functionally equivalent for
this purpose: it searches the binary's raw bytes for the literal string, same as `strings | grep`
would after string-extraction.

```
$ docker run --rm --entrypoint sh aura:local -lc 'which strings'
sh: 1: strings: not found

$ docker run --rm --entrypoint sh aura:local -lc 'grep -ac "0093_document_pipeline_convergence" /usr/local/bin/aura'
2
```

Cross-checked with `grep -ao ... | wc -l` (occurrence count rather than matching-line count):
also **2**. Consistent across both methods.

**`strings`-equivalent count: 2** (>= 1 required). The migration is embedded in the binary.

## Step 5 — Run the migration against the copy (Attempt 1 — defective rehearsal, superseded by Attempt 2 below)

```
$ set -a; . /d/Aura/.env; set +a
$ docker run --rm --network aura_default \
  -e AURA_DB_MIGRATE_URL="postgres://aura_migrate:REDACTED@postgres:5432/aura_0093_rehearsal?sslmode=disable" \
  -e AURA_DB_URL="postgres://aura_app:REDACTED@postgres:5432/aura_0093_rehearsal?sslmode=disable" \
  -e AURA_DB_BOOTSTRAP_URL="postgres://aura:REDACTED@postgres:5432/aura_0093_rehearsal?sslmode=disable" \
  -e AURA_CONFIG_DIR=/tmp \
  --entrypoint sh aura:local -lc 'aura db migrate'

migrate exit: 1
operation-key: e04b27ed-4d6f-4623-84dc-5868179346ec
migrate up against postgres://aura_migrate:REDACTED@postgres:5432/aura_0093_rehearsal?sslmode=disable: pq: permission denied for table schema_migrations (42501) in line 0: SELECT version, dirty FROM "public"."schema_migrations" LIMIT 1
```

**This is the exact error, verbatim except for password redaction.**

### Root-cause diagnosis (read-only queries; no fix attempted, no re-run)

The failure is on the golang-migrate library's own pre-flight health check
(`SELECT version, dirty FROM public.schema_migrations`), before any 0093 DDL statement runs.
Compared ACL and ownership between live and rehearsal:

```
$ docker exec aura-postgres psql -U aura -d aura -c "\dp public.schema_migrations"
 Schema |       Name        | Type  | Access privileges | ...
--------+-------------------+-------+--------------------+----
 public | schema_migrations | table |                    | ...   <- empty ACL (relies on owner)

$ docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c "\dp public.schema_migrations"
 Schema |       Name        | Type  | Access privileges | ...
--------+-------------------+-------+--------------------+----
 public | schema_migrations | table |                    | ...   <- empty ACL too (identical)

$ docker exec aura-postgres psql -U aura -d aura -c "SELECT tableowner FROM pg_tables WHERE schemaname='public' AND tablename='schema_migrations';"
 tableowner
--------------
 aura_migrate                                                     <- LIVE: owned by aura_migrate

$ docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c "SELECT tableowner FROM pg_tables WHERE schemaname='public' AND tablename='schema_migrations';"
 tableowner
------------
 aura                                                              <- REHEARSAL: owned by aura
```

On live, `schema_migrations` has no explicit ACL grants — `aura_migrate` can read/write it only
because it is the table's **owner** (Postgres owners get implicit full access with no ACL entry
needed). Step 2's restore command (`pg_restore --no-owner --role=aura`) reassigns ownership of
every restored object to `aura`, per `pg_restore`'s documented behavior for `--no-owner`. That
silently strips `aura_migrate`'s only path to access on `schema_migrations` in the rehearsal
copy, since there was never an explicit GRANT to fall back on (matching live, where none exists
either — live doesn't need one, because live's owner already is `aura_migrate`).

**Consequence: migration 0093 itself was never reached or exercised.** The failure is a defect
in the rehearsal-database construction recipe (ownership flattening breaks the multi-role
permission model that `AURA_DB_MIGRATE_URL`/`AURA_DB_URL`/`AURA_DB_BOOTSTRAP_URL` depend on), not
evidence about whether 0093's backfill/dedup/CHECK-tightening SQL survives real data. Per the
task's constraints, no fix was attempted (neither to 0093 nor to the restore recipe) — this is a
decision for the human operator (e.g., amend Step 2 to re-run the source database's role grants
after restore, or restore with owner preservation instead of `--no-owner --role=aura`).

## Step 6 (Attempt 1) — Assert the rehearsed end state (captured against the UNMIGRATED rehearsal DB)

Since Step 5 did not apply the migration, these were run against the rehearsal DB in its
restored (pre-0093, version-92) state, for the record, as the task's report contract requires:

```
$ docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c "SELECT version, dirty FROM public.schema_migrations;"
 version | dirty
---------+-------
      92 | f
(1 row)
```
**Assertion 1 (expected `93 | f`): FAILED — still 92, unmigrated, as expected given Step 5's outcome.**

```
$ docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c \
  "SELECT count(*) AS docs, count(*) FILTER (WHERE source_kind IS NULL) AS null_kind, count(*) FILTER (WHERE source_key IS NULL) AS null_key FROM aura.documents;"
ERROR:  column "source_kind" does not exist
LINE 1: SELECT count(*) AS docs, count(*) FILTER (WHERE source_kind ...
```
**Assertion 2 (expected `docs=4, null_kind=0, null_key=0`): FAILED — columns don't exist yet, as expected pre-0093.**

```
$ docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c \
  "SELECT to_regclass('aura.document_pipeline_stages') AS stages, to_regclass('aura.document_pipeline_quarantine') AS quarantine;"
 stages | quarantine
--------+------------
        |
(1 row)
```
**Assertion 3 (expected both non-null): FAILED — both null, tables don't exist yet, as expected pre-0093.**

```
$ docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c \
  "SELECT indexname FROM pg_indexes WHERE schemaname='aura' AND indexname='documents_identity_source_live_idx';"
 indexname
-----------
(0 rows)
```
**Assertion 4 (expected the index present): FAILED — 0 rows, index doesn't exist yet, as expected pre-0093.**

All four assertions fail, consistently with "migration never applied" rather than with any
inconsistency in the dump/restore. They confirm the rehearsal DB was correctly at version 92
right up to the point Step 5 was blocked.

## Step 7 (Attempt 1) — Drop the rehearsal database, retained the dump for a re-run

Attempt 1's rehearsal DB was dropped and its in-container dump copy removed at the time, but the
host-side dump (`D:\tmp\aura-backups\aura-pre0093-2026-08-05.dump`) was retained per the brief,
which made Attempt 2 possible without re-running `pg_dump` against live.

Coordinator feedback after Attempt 1: the ownership-flattening restore was a defect in the
plan's Step 2, not something to route around — an unexercised rehearsal leaves Task 3's live
cutover unguarded. Directed fix: re-run with a plain `pg_restore` (no `--no-owner`, no
`--role`), which restores original ownership since the source roles (`aura`, `aura_app`,
`aura_migrate`) all already exist in this cluster — and to verify ownership actually matches
live before trusting it and migrating, rather than assuming.

---

## Attempt 2 — ownership-preserving restore, real rehearsal

### Copy the dump back into the container

Attempt 1's Step 7 had removed the in-container copy; the host copy was still present.

```
$ docker cp /d/tmp/aura-backups/aura-pre0093-2026-08-05.dump aura-postgres:/tmp/aura-pre0093.dump
(exit 0)
$ docker exec aura-postgres ls -la /tmp/aura-pre0093.dump   # (MSYS_NO_PATHCONV=1 scoped to this call)
-rwxr-xr-x    1 root     root        537381 Aug  5 16:25 /tmp/aura-pre0093.dump
```

Same 537,381 bytes as the original dump — confirmed intact.

### Recreate the rehearsal DB with an ownership-preserving restore

```
$ docker exec aura-postgres psql -U aura -d postgres -c "DROP DATABASE IF EXISTS aura_0093_rehearsal;"
NOTICE:  database "aura_0093_rehearsal" does not exist, skipping
DROP DATABASE
$ docker exec aura-postgres psql -U aura -d postgres -c "CREATE DATABASE aura_0093_rehearsal OWNER aura;"
CREATE DATABASE
$ docker exec aura-postgres pg_restore -U aura -d aura_0093_rehearsal /tmp/aura-pre0093.dump
(exit 0, no --no-owner, no --role)
```

### Verify ownership matches live BEFORE migrating (not assumed)

```
$ docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c "SELECT version, dirty FROM public.schema_migrations;"
 version | dirty
---------+-------
      92 | f
(1 row)
$ docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c "SELECT status, count(*) FROM aura.documents GROUP BY status ORDER BY 1;"
 status  | count
---------+-------
 deleted |     1
 ready   |     3
(2 rows)
$ docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c "\dt public.schema_migrations"
                  List of tables
 Schema |       Name        | Type  |    Owner
--------+-------------------+-------+--------------
 public | schema_migrations | table | aura_migrate
(1 row)
$ docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c "SELECT tablename, tableowner FROM pg_tables WHERE schemaname='aura' ORDER BY tablename LIMIT 5;"
            tablename             |  tableowner
----------------------------------+--------------
 agent_job_runs                   | aura_migrate
 asset_events                     | aura_migrate
 assets                           | aura_migrate
 audit_logs                       | aura_migrate
 benchmark_settings_override_rows | aura_migrate
(5 rows)
$ docker exec aura-postgres psql -U aura -d aura -c "SELECT tablename, tableowner FROM pg_tables WHERE schemaname='aura' ORDER BY tablename LIMIT 5;"    # LIVE, same query, for comparison
            tablename             |  tableowner
----------------------------------+--------------
 agent_job_runs                   | aura_migrate
 asset_events                     | aura_migrate
 assets                           | aura_migrate
 audit_logs                       | aura_migrate
 benchmark_settings_override_rows | aura_migrate
(5 rows)
```

`schema_migrations` is owned by `aura_migrate` (matching live). The five sampled `aura.*` tables
all match live's ownership exactly. Baseline row counts also match (`92 | f`, `deleted 1`,
`ready 3`). **The rehearsal is now faithful — ownership was verified, not assumed.**

### Step 5 (Attempt 2) — run the migration for real

```
$ set -a; . /d/Aura/.env; set +a
$ docker run --rm --network aura_default \
  -e AURA_DB_MIGRATE_URL="postgres://aura_migrate:REDACTED@postgres:5432/aura_0093_rehearsal?sslmode=disable" \
  -e AURA_DB_URL="postgres://aura_app:REDACTED@postgres:5432/aura_0093_rehearsal?sslmode=disable" \
  -e AURA_DB_BOOTSTRAP_URL="postgres://aura:REDACTED@postgres:5432/aura_0093_rehearsal?sslmode=disable" \
  -e AURA_CONFIG_DIR=/tmp \
  --entrypoint sh aura:local -lc 'aura db migrate'

migrate exit: 1
operation-key: adf03a91-feab-4279-9ab5-ef01d544f614
migrate up against postgres://aura_migrate:REDACTED@postgres:5432/aura_0093_rehearsal?sslmode=disable: migration failed: cannot ALTER TABLE "documents" because it has pending trigger events in line 0: -- Amendment #114: make PostgreSQL the tenant-scoped document pipeline control plane.
[... full 0093 migration SQL body, verbatim, echoed by the tool as the failing statement's context ...]
 (details: pq: cannot ALTER TABLE "documents" because it has pending trigger events (55006))
```

**This time the migration actually ran against a faithful copy of live data, got well into
0093's DDL/DML body (table alterations, multi-statement backfill UPDATEs, quarantine
inserts/deletes, constraint tightening), and failed with a genuine PostgreSQL error:
`pq: cannot ALTER TABLE "documents" because it has pending trigger events (55006)`.**

Postgres state after the failed attempt:

```
$ docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c "SELECT version, dirty FROM public.schema_migrations;"
 version | dirty
---------+-------
      93 | t
(1 row)
```

`dirty=t` at version 93 is golang-migrate's standard "manual repair required" flag after a
failed migration — an operator would need `migrate force 92` (or equivalent) before any retry
on a database left in this state. Note this is orthogonal to whether the underlying DDL/DML
rolled back (see Step 6 below): Postgres migrations here run inside one transaction per file, so
a mid-migration failure rolls back all schema/data changes even though the bookkeeping row is
left dirty.

### Root-cause diagnosis (read-only queries; no fix attempted to 0093 or otherwise)

To rule out schema drift (i.e., to confirm this is intrinsic to 0093's own statement ordering
and not caused by some pre-existing trigger/constraint on live that 0093's author didn't know
about), checked live's `aura.documents` for any pre-existing trigger or deferrable constraint
that could explain "pending trigger events":

```
$ docker exec aura-postgres psql -U aura -d aura -c "SELECT tgname, tgtype, tgdeferrable, tginitdeferred, tgenabled, tgconstraint FROM pg_trigger WHERE tgrelid = 'aura.documents'::regclass AND NOT tgisinternal;"
 tgname | tgtype | tgdeferrable | tginitdeferred | tgenabled | tgconstraint
--------+--------+--------------+----------------+-----------+--------------
(0 rows)

$ docker exec aura-postgres psql -U aura -d aura -c "SELECT conname, confrelid::regclass AS references_table, conrelid::regclass AS on_table, condeferrable, condeferred FROM pg_constraint WHERE confrelid = 'aura.documents'::regclass AND condeferrable;"
 conname | references_table | on_table | condeferrable | condeferred
---------+------------------+----------+---------------+-------------
(0 rows)
```

**Zero pre-existing triggers or deferrable constraints on live's `aura.documents`.** This rules
out schema drift as the cause. The collision is intrinsic to 0093's own migration file: it
issues multiple `UPDATE`s against `aura.documents` (and tables that `UPDATE ... FROM
aura.documents`) earlier in the same transaction, then later in the same file adds several
`DEFERRABLE INITIALLY DEFERRED` foreign keys touching `documents`/`document_versions`/
`storage_objects` (e.g. `documents_active_version_identity_fkey`,
`document_versions_storage_identity_fkey`, `storage_objects_version_identity_fkey`), and still
later does `ALTER TABLE aura.documents ALTER COLUMN ... SET NOT NULL ...` type operations.
PostgreSQL refuses certain `ALTER TABLE` operations on a table while there are still
unfired/pending trigger events queued against it earlier in the same transaction. Per this
task's constraints, no fix to 0093's statement ordering was attempted — this diagnostic is
reported as a hint for whoever addresses the finding, not as a prescribed fix.

> **CORRECTION (Task 1b, 2026-08-05):** the query above (`WHERE confrelid =
> 'aura.documents'::regclass AND condeferrable`) checks constraints that *reference*
> `aura.documents` from other tables, not constraints *on* `aura.documents` itself — the
> wrong direction. Queried correctly (`conname='documents_active_version_id_fkey'`), a
> pre-existing `DEFERRABLE INITIALLY DEFERRED` FK **does** exist on `aura.documents` at
> version 92 (`documents_active_version_id_fkey`, `documents → document_versions`,
> `condeferrable=t condeferred=t`), and it is what queues the pending RI check events. The
> FKs 0093 itself adds at lines 384-397 are not the cause — they execute after the failure
> point at line 290 and never run. See the Task 1b section below for the verified diagnosis
> and the fix.

## Step 6 (Attempt 2) — Assert the rehearsed end state (captured against the FAILED/dirty rehearsal DB)

**Assertion 1** — `SELECT version, dirty FROM public.schema_migrations;`
Expected `93 | f`. Got:
```
 version | dirty
---------+-------
      93 | t
(1 row)
```
Result: **FAILED** — version advanced to 93 but `dirty=t`, i.e. migrate's bookkeeping marks this
a failed/incomplete migration requiring manual repair, not a clean success.

**Assertion 2** — `SELECT count(*) AS docs, count(*) FILTER (WHERE source_kind IS NULL) AS null_kind, count(*) FILTER (WHERE source_key IS NULL) AS null_key FROM aura.documents;`
Expected `docs=4, null_kind=0, null_key=0`. Got:
```
ERROR:  column "source_kind" does not exist
LINE 1: SELECT count(*) AS docs, count(*) FILTER (WHERE source_kind ...
```
Result: **FAILED** — `source_kind` doesn't exist, meaning the migration's own transaction rolled
back its `ADD COLUMN` statements along with everything else when it errored (single-transaction
migration; no partial DDL survives a failure).

**Assertion 3** — `SELECT to_regclass('aura.document_pipeline_stages') AS stages, to_regclass('aura.document_pipeline_quarantine') AS quarantine;`
Expected both non-null. Got:
```
 stages | quarantine
--------+------------
        |
(1 row)
```
Result: **FAILED** — both null; neither table exists, consistent with a full transaction rollback.

**Assertion 4** — `SELECT indexname FROM pg_indexes WHERE schemaname='aura' AND indexname='documents_identity_source_live_idx';`
Expected the index present. Got:
```
 indexname
-----------
(0 rows)
```
Result: **FAILED** — 0 rows, consistent with a full transaction rollback.

**All four assertions fail.** The underlying data/schema is intact (rolled back cleanly to
pre-0093 shape — no half-applied columns or orphaned objects were found), but
`public.schema_migrations` is left at `93 | t` (dirty), which is itself an operationally
important fact: a retry against this exact DB state would need a manual `migrate force` step
first. **0093 does not survive contact with real data as currently written.**

## Step 7 (Attempt 2) — Drop the rehearsal database

```
$ docker exec aura-postgres psql -U aura -d postgres -c "DROP DATABASE aura_0093_rehearsal;"
DROP DATABASE
$ docker exec aura-postgres rm -f /tmp/aura-pre0093.dump   # (MSYS_NO_PATHCONV=1 scoped to this call)
(exit 0)
$ docker exec aura-postgres psql -U aura -d postgres -c "SELECT datname FROM pg_database WHERE datname LIKE 'aura%';"
                    datname
------------------------------------------------
 aura_phase4_migrate_drill
 aura
 aura_migratesteps_drill
 aura_pipeline_0a0469f3cd4a4e00889fba07b1f89582
(4 rows)
```

`aura_0093_rehearsal` is gone. (The other three `aura_*` names are unrelated leftovers from
prior work, out of scope for this task — not touched.) In-container copy of the dump removed
again; the host copy at `D:\tmp\aura-backups\aura-pre0093-2026-08-05.dump` remains for Task 3.

## Post-check — live database confirmed untouched (after both attempts)

```
$ docker exec aura-postgres psql -U aura -d aura -c "SELECT version, dirty FROM public.schema_migrations;"
 version | dirty
---------+-------
      92 | f
(1 row)
$ docker exec aura-postgres psql -U aura -d aura -c "SELECT status, count(*) FROM aura.documents GROUP BY status ORDER BY 1;"
 status  | count
---------+-------
 deleted |     1
 ready   |     3
(2 rows)
```

Identical to baseline. No live-database command was ever run in this task beyond read-only
`SELECT` — across both attempts.

## Task 1b — the fix and Attempt 3 (2026-08-05, supersedes the FAILS verdict above)

**Corrected root cause (measured):** `aura.documents` carries a pre-existing `DEFERRABLE
INITIALLY DEFERRED` foreign key, `documents_active_version_id_fkey` (`documents →
document_versions`), live at version 92. Every `UPDATE aura.documents` statement in 0093
(lines 102-288) queues a deferred RI check event against it; those events stay pending
until COMMIT, and the `ALTER TABLE aura.documents ... SET NOT NULL` block at line 290 then
fails with SQLSTATE 55006. The FKs 0093 itself adds at lines 384-397 are not the cause —
they run after line 290 and never execute before the failure. Full detail of the prior
diagnosis's error is in the correction note inline above.

> **CORRECTION (Task 1b fix-round, 2026-08-05): the inventory above was not exhaustive.**
> The paragraph above, and Attempt 3's assertion 5 below, name and check only
> `documents_active_version_id_fkey` — inferred from the failing table name in the error
> message, not from an exhaustive scan of the schema. An exhaustive scan
> (`SELECT conname, conrelid::regclass, condeferrable, condeferred FROM pg_constraint WHERE
> contype='f' AND condeferrable;`) finds **two** pre-existing deferrable FKs at version 92,
> both added in `0025_document_control_plane.up.sql`:
>
> ```
>              conname              |   table_name    | condeferrable | condeferred
> ----------------------------------+-----------------+---------------+-------------
>  documents_active_version_id_fkey | documents       | t             | t
>  storage_objects_version_id_fkey  | storage_objects  | t             | t
> ```
>
> `storage_objects_version_id_fkey` is not exploitable here — verified, not assumed: 0093's
> only `UPDATE aura.storage_objects` (line 182-183) sets `status` alone
> (`SET status = CASE WHEN deleted_at IS NULL THEN 'live' ELSE 'delete_pending' END;`), never
> `version_id`. PostgreSQL's referencing-side RI check trigger only queues a pending event
> when the FK's own column(s) change, so this UPDATE queues nothing against it regardless of
> deferral. `SET CONSTRAINTS ALL IMMEDIATE;` (`ALL`, not a named constraint) would flush it
> in any case, so the fix is unaffected. See "Fix-round — exhaustive inventory measured, not
> reasoned" below for the fresh rehearsal that checks both constraints' flags post-migration.

### The fix

The single `SET CONSTRAINTS ALL IMMEDIATE;` statement below was already present, uncommitted,
in the working tree's `0093_document_pipeline_convergence.up.sql` when Task 1b began — a
prior session had applied it but not committed. Task 1b's job was to verify it matched the
diagnosis, then independently execute the rehearsal end to end (fresh restore, fresh image
build/grep check, fresh `aura db migrate` run, all post-migration assertions, live-untouched
check) and commit. The diff below documents what the file contains relative to the version
committed before this fix, not an edit authored in this task:

```diff
 WHERE status IN ('draft', 'processing', 'archived');

+-- aura.documents carries a DEFERRABLE INITIALLY DEFERRED FK
+-- (documents_active_version_id_fkey), so every UPDATE above queues an RI check event that
+-- stays pending until COMMIT, and ALTER TABLE refuses to run while any are outstanding
+-- (SQLSTATE 55006). Flush them here. No DML follows this point, so one flush covers the
+-- rest of the migration.
+SET CONSTRAINTS ALL IMMEDIATE;
+
 ALTER TABLE aura.documents
     ALTER COLUMN source_kind SET NOT NULL,
```

No other line in `0093_document_pipeline_convergence.up.sql` changed. `.down.sql` untouched.

### Attempt 3 — re-rehearsal against a fresh, ownership-preserving restore

Rebuilt the dump into `aura_0093_rehearsal` exactly as in Attempt 2 (plain `pg_restore`,
ownership preserved), verified faithful before trusting it:

```
$ docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c "SELECT version, dirty FROM public.schema_migrations;"
 version | dirty
---------+-------
      92 | f
(1 row)
$ docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c "SELECT status, count(*) FROM aura.documents GROUP BY 1 ORDER BY 1;"
 status  | count
---------+-------
 deleted |     1
 ready   |     3
(2 rows)
$ docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c "SELECT conname, condeferrable, condeferred FROM pg_constraint WHERE conname='documents_active_version_id_fkey';"
             conname              | condeferrable | condeferred
-----------------------------------+---------------+-------------
 documents_active_version_id_fkey | t             | t
(1 row)
```

Rebuilt `aura:local` (`docker compose build aura`) so the binary carries the amended file,
confirmed via byte search:

```
$ docker run --rm --entrypoint sh aura:local -lc 'grep -ac "SET CONSTRAINTS ALL IMMEDIATE" /usr/local/bin/aura'
1
```

Migrated the copy:

```
$ docker run --rm --network aura_default \
  -e AURA_DB_MIGRATE_URL="postgres://aura_migrate:REDACTED@postgres:5432/aura_0093_rehearsal?sslmode=disable" \
  -e AURA_DB_URL="postgres://aura_app:REDACTED@postgres:5432/aura_0093_rehearsal?sslmode=disable" \
  -e AURA_DB_BOOTSTRAP_URL="postgres://aura:REDACTED@postgres:5432/aura_0093_rehearsal?sslmode=disable" \
  -e AURA_CONFIG_DIR=/tmp \
  --entrypoint sh aura:local -lc 'aura db migrate'

ok: 1 migration(s) applied
migrate exit=0
```

**Exit 0 — the SQLSTATE 55006 failure is gone.** Post-migration assertions, all five pass:

| Assertion | Expected | Got | Result |
|---|---|---|---|
| `schema_migrations` version/dirty | `93 \| f` | `93 \| f` | PASS |
| docs/null_kind/null_key | `4 / 0 / 0` | `4 / 0 / 0` | PASS |
| `to_regclass` stages/quarantine | both non-null | `document_pipeline_stages` / `document_pipeline_quarantine` | PASS |
| `documents_identity_source_live_idx` present | present | present | PASS |
| `documents_active_version_id_fkey` deferrable/deferred | `t \| t` (unaltered) | `t \| t` | PASS |

**Note (added in the fix-round below):** the fifth assertion above checks only
`documents_active_version_id_fkey`. It does not cover `storage_objects_version_id_fkey`,
the second pre-existing deferrable FK found by the exhaustive inventory. See "Fix-round —
exhaustive inventory measured, not reasoned" immediately below for the corrected assertion
that checks both, run fresh against a new restore.

Live confirmed still at `92 | f` after the re-rehearsal (this task never migrates live).
Rehearsal database dropped, in-container dump copy removed; host dump
(`D:\tmp\aura-backups\aura-pre0093-2026-08-05.dump`, 537,381 bytes) retained for Task 3,
not re-dumped.

Full command transcript, including the `MSYS_NO_PATHCONV=1` gotcha hit while re-copying
the dump into the container, is in
`.superpowers/sdd/2026-08-05-document-pipeline-operator-e2e/task-1b-report.md`.

## Fix-round — exhaustive inventory measured, not reasoned (2026-08-05)

Review of Tasks 1+1b approved the fix and its execution but flagged that the
deferrable-constraint inventory above was not exhaustive (it named one FK, inferred from
the failing table in the error message, not from a schema-wide scan) and that assertion 5
checked only that one. This section re-runs the rehearsal fresh — new restore, new migrate,
new assertions — to measure both constraints, not reason about them. The SQL in
`0093_document_pipeline_convergence.up.sql` was **not** changed for this round.

### Exhaustive pre-migration inventory, against a fresh restore

```
$ docker cp /d/tmp/aura-backups/aura-pre0093-2026-08-05.dump aura-postgres:/tmp/aura-pre0093-fix2.dump
(exit 0)
$ docker exec aura-postgres psql -U aura -d postgres -c "DROP DATABASE IF EXISTS aura_0093_rehearsal;"
NOTICE:  database "aura_0093_rehearsal" does not exist, skipping
DROP DATABASE
$ docker exec aura-postgres psql -U aura -d postgres -c "CREATE DATABASE aura_0093_rehearsal OWNER aura;"
CREATE DATABASE
$ docker exec aura-postgres pg_restore -U aura -d aura_0093_rehearsal /tmp/aura-pre0093-fix2.dump
(exit 0)

$ docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c "SELECT version, dirty FROM public.schema_migrations;"
 version | dirty
---------+-------
      92 | f
(1 row)

$ docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c "SELECT conname, conrelid::regclass AS table_name, condeferrable, condeferred FROM pg_constraint WHERE contype='f' AND condeferrable ORDER BY conname;"
             conname              |   table_name    | condeferrable | condeferred
----------------------------------+-----------------+---------------+-------------
 documents_active_version_id_fkey | documents       | t             | t
 storage_objects_version_id_fkey  | storage_objects  | t             | t
(2 rows)
```

Faithful copy (`92 | f`, matching baseline) with both pre-existing deferrable FKs confirmed
present, measured directly from `pg_constraint` with `contype='f' AND condeferrable` (no
table name assumed) — same result as the exhaustive scan against live shown in the
correction note above. Both originate in `0025_document_control_plane.up.sql`
(`documents_active_version_id_fkey` at its line 79-83, `storage_objects_version_id_fkey` at
its line 85-90 — confirmed by `grep -n` against that file).

### Why the second constraint is harmless (verified, not assumed)

```
$ grep -n -A1 "^UPDATE aura.storage_objects$" internal/db/migrations/0093_document_pipeline_convergence.up.sql
182:UPDATE aura.storage_objects
183-SET status = CASE WHEN deleted_at IS NULL THEN 'live' ELSE 'delete_pending' END;
```

0093's only `UPDATE` against `aura.storage_objects` sets `status` alone — it never touches
`version_id`, the column `storage_objects_version_id_fkey` constrains. PostgreSQL's
referencing-side RI check trigger fires only when the FK's own column(s) actually change, so
this UPDATE queues no pending event against that constraint regardless of its deferral mode.
`SET CONSTRAINTS ALL IMMEDIATE;` flushes `ALL` pending events (not one named constraint), so
it would cover this FK too if it ever did queue one. The fix does not depend on this
constraint being harmless — it is unconditionally correct either way — but the record should
say so measured, not implied.

### Migrate and assert both constraints survive the flush unaltered

```
$ set -a; . /d/Aura/.env; set +a
$ docker run --rm --network aura_default \
  -e AURA_DB_MIGRATE_URL="postgres://aura_migrate:REDACTED@postgres:5432/aura_0093_rehearsal?sslmode=disable" \
  -e AURA_DB_URL="postgres://aura_app:REDACTED@postgres:5432/aura_0093_rehearsal?sslmode=disable" \
  -e AURA_DB_BOOTSTRAP_URL="postgres://aura:REDACTED@postgres:5432/aura_0093_rehearsal?sslmode=disable" \
  -e AURA_CONFIG_DIR=/tmp \
  --entrypoint sh aura:local -lc 'aura db migrate'

operation-key: c9b0fabe-35a0-4610-aaa3-b4e9669328da
ok: 1 migration(s) applied
migrate exit=0

$ docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c "SELECT version, dirty FROM public.schema_migrations;"
 version | dirty
---------+-------
      93 | f
(1 row)

$ docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c \
  "SELECT conname, conrelid::regclass AS on_table, condeferrable, condeferred FROM pg_constraint WHERE contype='f' AND condeferrable ORDER BY 2,1;"
                 conname                 |     on_table       | condeferrable | condeferred
-----------------------------------------+--------------------+---------------+-------------
 document_versions_storage_identity_fkey | document_versions  | t             | t
 documents_active_version_id_fkey        | documents          | t             | t
 documents_active_version_identity_fkey  | documents          | t             | t
 storage_objects_version_id_fkey         | storage_objects    | t             | t
 storage_objects_version_identity_fkey   | storage_objects    | t             | t
(5 rows)
```

`93 | f` — clean, as before. The query now returns five rows because 0093 itself adds three
more `DEFERRABLE INITIALLY DEFERRED` FKs (the `*_identity_fkey` constraints, lines 384-404)
in addition to the two pre-existing ones. Both pre-existing constraints —
`documents_active_version_id_fkey` and `storage_objects_version_id_fkey` — are present in
the post-migration result with `condeferrable=t, condeferred=t`, unaltered: **the flush did
not change either constraint's declared deferrability, measured for both, not just one.**

### Cleanup

```
$ docker exec aura-postgres psql -U aura -d postgres -c "DROP DATABASE aura_0093_rehearsal;"
DROP DATABASE
$ docker exec aura-postgres rm -f /tmp/aura-pre0093-fix2.dump   # MSYS_NO_PATHCONV=1
(exit 0)
$ docker exec aura-postgres psql -U aura -d postgres -c "SELECT datname FROM pg_database WHERE datname LIKE 'aura%';"
                    datname
------------------------------------------------
 aura_phase4_migrate_drill
 aura
 aura_migratesteps_drill
 aura_pipeline_0a0469f3cd4a4e00889fba07b1f89582
(4 rows)

$ docker exec aura-postgres psql -U aura -d aura -c "SELECT version, dirty FROM public.schema_migrations;"
 version | dirty
---------+-------
      92 | f
(1 row)
```

`aura_0093_rehearsal` dropped again. Live confirmed still `92 | f` — unmigrated and
unmodified by this fix-round, same as every prior round.

## Summary for Task 3

- **Rollback dump:** verified faithful and restorable — `D:\tmp\aura-backups\aura-pre0093-2026-08-05.dump` (537,381 bytes). Reused unchanged across Attempts 2 and 3 and the fix-round.
- **Image:** `aura:local` rebuilt 2026-08-05 (Task 1b), confirmed via `grep -a` byte search to embed both `0093_document_pipeline_convergence` and the `SET CONSTRAINTS ALL IMMEDIATE` fix.
- **0093 verdict: FIXED — reaches `93 / clean` on a faithful copy of live data.** The
  original failure (`pq: cannot ALTER TABLE "documents" because it has pending trigger
  events (55006)`) was caused by pre-existing `DEFERRABLE INITIALLY DEFERRED` FKs on
  `aura.documents` and `aura.storage_objects` — an exhaustive scan found **two**
  (`documents_active_version_id_fkey`, `storage_objects_version_id_fkey`), both added in
  `0025_document_control_plane.up.sql`; the second queues no event here because 0093 never
  writes the column it constrains, but the fix does not depend on that — `SET CONSTRAINTS
  ALL IMMEDIATE;` flushes both unconditionally. A single `SET CONSTRAINTS ALL IMMEDIATE;`
  inserted immediately before the `ALTER TABLE aura.documents ... SET NOT NULL` block
  (after the last `UPDATE aura.documents` in the file) flushes the pending RI checks and
  resolves it — verified by Attempt 3's and the fix-round's clean `93 | f` results, all
  post-migration assertions, and the fix-round's fresh, measured check that **both**
  pre-existing deferrable constraints survive the flush unaltered. **Task 3 may proceed
  with the cutover**, using the amended migration file and this rehearsal as evidence.

## Task 2 — The observability healthchecks that lie

**Verdict: FIXED.** `tempo` and `prometheus` share `aura`'s network namespace
(`network_mode: "service:aura"`). Their own healthchecks probe loopback from inside that
namespace, so after `docker restart aura` both processes stay attached to a dead namespace
and keep reporting `healthy` while completely unreachable from everywhere else. Neither can
detect its own orphaning: a loopback probe inside the dead namespace still passes, and
`tempo`'s image is distroless (no shell, no wget — `exec: "sh": executable file not found in
$PATH`), so it cannot even run a `CMD-SHELL` network test. `grafana` is the one container
that sits outside that namespace, has a shell, and already depends on both — so the
reachability assertion moved there.

### Step 1 — the check script

Created `scripts/observability_sidecar_check.sh` (verbatim per plan), executable, asserting
both sidecars from outside the namespace via `docker exec aura-grafana-1 wget … aura:3200/ready`
and `… aura:9090/-/ready`.

### Step 2 — reproduce the false green (baseline)

```
$ bash /d/Aura/scripts/observability_sidecar_check.sh
observability sidecars reachable
baseline exit=0

$ docker restart aura >/dev/null; echo exit=$?
exit=0

$ docker inspect aura-tempo-1 aura-prometheus-1 --format '{{.Name}} {{.State.Status}} {{.State.Health.Status}}'
/aura-tempo-1 running healthy
/aura-prometheus-1 running healthy

$ bash /d/Aura/scripts/observability_sidecar_check.sh; echo "check exit=$?"
wget: can't connect to remote host (172.19.0.11): Connection refused
tempo unreachable at aura:3200/ready
wget: can't connect to remote host (172.19.0.11): Connection refused
prometheus unreachable at aura:9090/-/ready
check exit=1
```

Docker's own verdict (`running healthy`/`running healthy`) and the external script
(`tempo unreachable`, `prometheus unreachable`, exit 1) disagree — that disagreement is the
defect. Confirmed live, matching the plan's stated measurement exactly.

Note: on the very first, pre-restart invocation (before `MSYS_NO_PATHCONV=1` was exported for
this Git Bash session), the script's own `/dev/null` argument was mangled by MSYS path
conversion into the Windows null device (`wget: can't open 'nul': Permission denied`) and a
concurrent tempo compaction cycle returned one transient `503`. Both were session/timing
artifacts, not the defect under test, and disappeared on retry; every reproduction quoted
above and below ran with `MSYS_NO_PATHCONV=1` exported.

### Step 3 — the fix

`compose.yaml`: `grafana`'s `healthcheck.test` replaced with a `CMD-SHELL` chain that probes
its own `/api/health`, then `aura:3200/ready`, then `aura:9090/-/ready` — all three must
succeed. `tempo` and `prometheus`'s `test:` lines are **unchanged**; a one-line (multi-line)
comment was added above each pointing at grafana's healthcheck as the reachability authority,
so a future reader does not "fix" them into another false green.

### Step 4 — restore and verify agreement

```
$ docker compose --profile observability up -d --no-deps --force-recreate tempo prometheus
$ docker compose --profile observability up -d --no-deps --force-recreate grafana
# waited for grafana's healthcheck to leave `starting` (start_period 20s)
$ bash /d/Aura/scripts/observability_sidecar_check.sh; echo "check exit=$?"
observability sidecars reachable
check exit=0
$ docker inspect aura-grafana-1 --format 'grafana health={{.State.Health.Status}}'
grafana health=healthy
```

Both authorities agree: reachable, exit 0, grafana `healthy`.

### Step 5 — prove the new healthcheck catches the orphaning

```
$ docker restart aura >/dev/null
```

`grafana`'s healthcheck runs at `interval: 15s` with `retries: 12` — i.e. Docker requires 12
**consecutive** failures before flipping the reported status to `unhealthy`, roughly 180s of
sustained failure, not the ~60s the plan's polling loop budgets. Deviation from the plan:
extended the wait loop past the specified 12×5s to observe the actual transition, polling
`{{.State.Health.Status}}` and `{{.State.Health.FailingStreak}}`:

```
poll 1: grafana health=healthy failingStreak=6
poll 2: grafana health=healthy failingStreak=7
...
poll 8: grafana health=healthy failingStreak=11
poll 9: grafana health=unhealthy failingStreak=12

$ docker inspect aura-grafana-1 --format 'grafana health={{.State.Health.Status}} failingStreak={{.State.Health.FailingStreak}}'
grafana health=unhealthy failingStreak=12

$ bash /d/Aura/scripts/observability_sidecar_check.sh; echo "check exit=$?"
wget: can't connect to remote host (172.19.0.11): Connection refused
tempo unreachable at aura:3200/ready
wget: can't connect to remote host (172.19.0.11): Connection refused
prometheus unreachable at aura:9090/-/ready
check exit=1
```

Every intermediate healthcheck log entry during the failing streak showed the same
`Connection refused` — the mechanism was working correctly throughout, it simply needed more
wall-clock time than the plan's loop budgeted before Docker's own status field caught up.
Before this change Docker reported everything healthy while the sidecars were unreachable;
now Docker's own health status (`unhealthy`) and the external script (exit 1) **agree**. The
fix works.

### Step 6 — restored working state

```
$ docker compose --profile observability up -d --no-deps --force-recreate tempo prometheus
$ docker compose --profile observability up -d --no-deps --force-recreate grafana
# waited for grafana healthcheck to leave `starting`
$ bash /d/Aura/scripts/observability_sidecar_check.sh; echo "check exit=$?"
observability sidecars reachable
check exit=0

$ docker ps --format '{{.Names}}\t{{.Status}}'
aura-grafana-1      Up 27 seconds (healthy)
aura-prometheus-1   Up 28 seconds (healthy)
aura-tempo-1        Up 28 seconds (healthy)
aura                Up 3 minutes (healthy)
... (all other stack services healthy/up, unaffected)
```

Stack left green: script exits 0, grafana `healthy`, tempo and prometheus `healthy` and
actually reachable this time. **Task 3's cutover and any later `document_pipeline_e2e.sh`
restart-mid-run assertion can rely on `scripts/observability_sidecar_check.sh` as ground
truth — Docker's per-sidecar health flags alone are no longer sufficient, by design.**
