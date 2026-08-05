# PHASE0 — Rehearse migration 0093 against a copy of live data

Date: 2026-08-05
Task: Task 1 of `docs/superpowers/plans/2026-08-05-document-pipeline-operator-e2e.md`
Verdict: **DONE_WITH_CONCERNS** — migration 0093 was never actually exercised. The rehearsal
database's restore recipe (Step 2, `pg_restore --no-owner --role=aura`) flattened all object
ownership to `aura`, which stripped `aura_migrate`'s implicit owner-level access to
`public.schema_migrations` before any 0093 DDL could run. Live `aura` was never touched. This
is a gap in the rehearsal harness, not evidence that 0093's SQL is broken.

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

## Step 5 — Run the migration against the copy

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

## Step 6 — Assert the rehearsed end state (captured against the UNMIGRATED rehearsal DB)

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

## Step 7 — Drop the rehearsal database

```
$ docker exec aura-postgres psql -U aura -d postgres -c "DROP DATABASE aura_0093_rehearsal;"
DROP DATABASE
$ docker exec aura-postgres rm -f /tmp/aura-pre0093.dump
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

`aura_0093_rehearsal` is gone. (The other `aura_*` databases listed are unrelated leftovers from
prior work, out of scope for this task — not touched.) In-container copy of the dump removed;
the host copy at `D:\tmp\aura-backups\aura-pre0093-2026-08-05.dump` remains, as intended, for
Task 3's use.

## Post-check — live database confirmed untouched

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
`SELECT`.

## Summary for Task 3

- **Rollback dump:** verified faithful and restorable — `D:\tmp\aura-backups\aura-pre0093-2026-08-05.dump` (537,381 bytes).
- **Image:** `aura:local` built 2026-08-05, confirmed (via `grep -a` byte search, `strings` unavailable in the image) to embed `0093_document_pipeline_convergence` (count 2).
- **0093 verdict: UNKNOWN.** The rehearsal never reached 0093's DDL because the restore recipe's `--no-owner --role=aura` flattening broke `aura_migrate`'s access to `public.schema_migrations`. Before Task 3 proceeds to cut over 0093 against live, the rehearsal must be re-run with a restore recipe that preserves (or re-establishes) `aura_migrate`/`aura_app` ownership/grants, so that 0093's actual backfill/dedup/CHECK-tightening logic is exercised against a faithful copy of the real data before it touches live.
