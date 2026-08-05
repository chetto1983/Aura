# PHASE0 — Rehearse migration 0093 against a copy of live data

Date: 2026-08-05
Task: Task 1 of `docs/superpowers/plans/2026-08-05-document-pipeline-operator-e2e.md`
Verdict: **DONE_WITH_CONCERNS — 0093 FAILS against real data.** Two rehearsal attempts were
made. Attempt 1 (documented below) used `pg_restore --no-owner --role=aura`, which flattened
object ownership and blocked `aura_migrate` before any 0093 DDL ran — a defect in the rehearsal
recipe, not in 0093, so it proved nothing. Attempt 2 used a plain `pg_restore` (ownership
preserved, verified to match live before migrating) and **actually exercised 0093's DDL**. It
failed with a genuine Postgres error: `pq: cannot ALTER TABLE "documents" because it has
pending trigger events (55006)`. Live `aura` was never touched in either attempt (confirmed
before/after both). **This is the finding the rehearsal exists to produce: 0093 is not safe to
run against live as written.**

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

## Summary for Task 3

- **Rollback dump:** verified faithful and restorable — `D:\tmp\aura-backups\aura-pre0093-2026-08-05.dump` (537,381 bytes).
- **Image:** `aura:local` built 2026-08-05, confirmed (via `grep -a` byte search, `strings` unavailable in the image) to embed `0093_document_pipeline_convergence` (count 2).
- **0093 verdict: FAILS.** Against a faithful, ownership-verified copy of live data, migration
  0093 aborts partway through with `pq: cannot ALTER TABLE "documents" because it has pending
  trigger events (55006)`. Root cause is intrinsic to 0093's own statement ordering (UPDATEs on
  `aura.documents` earlier in the transaction, `DEFERRABLE INITIALLY DEFERRED` FK additions and
  `ALTER COLUMN ... SET NOT NULL` later in the same transaction) — not caused by any pre-existing
  trigger or schema drift on live (verified: zero triggers, zero deferrable constraints on live's
  `aura.documents`). The migration's own transaction rolls back cleanly on failure (no partial
  schema/data damage), but leaves `schema_migrations` at `93 | t` (dirty), which would need a
  manual `migrate force` before any retry. **Task 3 must NOT cut 0093 over to live as currently
  written.** This is a decision for the human operator: 0093's statement ordering needs to change
  (e.g., defer the FK additions earlier, or split the migration, or explicitly manage
  `SET CONSTRAINTS ... IMMEDIATE` at the right point) before it is safe to run against live.
