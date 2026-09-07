# Backup and restore

Verified on 2026-09-07. These procedures describe the current Compose deployment.
Configuration, successful archive creation, and successful restore are separate
checks; a healthy database alone proves none of the latter two.

## Automatic backups

| Data | Mechanism | Schedule and retention | Destination |
|---|---|---|---|
| Postgres | Aura's `backup_postgres` job runs network `pg_dump` | Seeded at `0 1 * * *` in `Europe/Rome`; 14-day rolling retention | Host `AURA_BACKUP_DIR`, mounted at `/backups` in Aura and Postgres |
| ArcadeDB | Native automatic backup scheduler | Every 60 minutes; `maxFiles=60`, tiered hourly/daily/weekly/monthly buckets 24/7/4/6 | Logical Compose volume `aura-arcadedb-backups`, mounted at `/home/arcadedb/backups` |

The seeded Postgres task explicitly uses `Europe/Rome`; inspect the persisted task
if its schedule has been customized. An interrupted dump
is not promoted to the final filename; completed files are named
`postgres-YYYYMMDDTHHMMSSZ.dump`. Missed jobs participate in scheduler recovery.

ArcadeDB reads [backup.json](../docker/arcadedb/backup.json) at startup. Its defaults
apply to every database, including identity databases created later. Native
retention selects archives by time bucket. This is independent of Aura's scheduler
and `AURA_BACKUP_DIR`. Archives have this layout inside the ArcadeDB container:

```text
backups/<database>/<database>-backup-<timestamp>.zip
```

The backup directory must remain relative to the ArcadeDB server root. The
Compose mount provides persistence separately from the live database volume.
See the [native scheduler documentation](https://docs.arcadedb.com/arcadedb/how-to/operations/auto-backup).

## Check the running deployment

```bash
docker compose ps arcadedb aura postgres
docker compose exec -T arcadedb cat config/backup.json
docker compose exec -T arcadedb find backups -maxdepth 2 -type f -name '*.zip'
docker compose logs --since 2h arcadedb
docker compose exec -T aura aura task list
```

Inspect backup timestamps, errors and storage use. Cadence applies while services
are running; it is not a guarantee of a one-hour recovery point through downtime
or repeated backup failures. The latest successful archive determines recoverability.

The configuration file is mounted read-only. Change the host file and restart
the ArcadeDB service to apply deployment-managed changes; do not assume a Studio
edit can persist through that mount.

## Run the four-plane restore drill

From a configured checkout on Linux or WSL, with Docker, Go, Python 3 and curl:

```bash
set -a
. ./.env
set +a
# Host-side object access must use the published port, not the Compose hostname.
export AURA_OBJECTSTORE_ENDPOINT=http://127.0.0.1:3900
bash scripts/restore_drill.sh
```

The script needs Postgres and ArcadeDB credentials plus the configured Garage
bucket, region, access key and secret. Container-only hostnames in environment
values must be replaced with endpoints reachable from the test host. The default
report is `artifacts/production-readiness/dr-report.json`; `AURA_DR_REPORT` selects
another destination.

The drill creates uniquely-named resources and removes them after testing:

1. Dump Postgres and restore into a disposable database, verifying a sentinel.
2. Archive the conversation `runs` directory and restore into a disposable volume.
3. Export, delete and restore two owned Garage fixture objects under a unique prefix.
4. Create a tenant-shaped ArcadeDB database, trigger its native backup, drop that
   test database, restore it from the ZIP, and verify the sentinel checksum.

It does not drop a real identity database or overwrite an operator's document.
It adds temporary fixture data to the relevant stores and cleans up that data.
The ArcadeDB leg proves scheduler discovery and native restore for a newly-created
database; the Garage leg proves fixture recovery rather than a fleet-wide backup.

## Restore an existing memory archive for inspection

Use ArcadeDB's root-only server API to restore into a **new database name**. The
target must not exist. The Compose deployment explicitly enables local archive
URLs for this administrative operation. Example request body:

```json
{
  "command": "restore database aura_restore_check file:///home/arcadedb/backups/<database>/<archive>.zip"
}
```

Submit it to `POST http://127.0.0.1:2480/api/v1/server` using root authentication.
Verify the restored schema, record counts, historical facts and required queries
before planning a production cutover. Keep the source archive. Restoring under a
test name does not rebind an Aura identity or change its credential mapping.
See [native restore](https://docs.arcadedb.com/arcadedb/how-to/operations/restore).

## Recovery scope

The two database backups are not a complete appliance backup. Preserve separately:

- Garage object data and metadata, with its configuration and access bindings.
- Persistent workspace files and required Aura runtime files beyond `runs`.
- Deployment `.env`, Compose overrides and service configuration, using protected
  storage. Existing encrypted records and derived credentials need their original keys.
- Required integration state and credentials, and the images or immutable image
  references needed to recreate the deployment.

Copy recovery material off-host. A separate Docker volume on the same disk does
not protect against loss of that disk. The four stores are not captured as one
globally consistent snapshot, and the drill does not claim that they are.

Archives retain the state captured at their creation. An older restore can therefore
contain records deleted later; validate the intended recovery point and subsequent
erasure requirements before using it as live state.

## Measured result

[Validation evidence](launch-validation-2026-09-07.json) records:

- Four-plane drill: **4/4 passed**, including checksum and cleanup checks.
- An existing scheduled operator-memory archive: ZIP CRC valid; restored into a
  disposable database with **93 entities, 75 facts, 40 mentions, and one closed
  historical fact**. The live database and source archive were unchanged.

The fixture drill's summed restore operations took 3,570 ms and its largest
fixture-to-backup interval was one second. These measurements describe the small
test workload. They are not production RPO/RTO commitments, a full host-loss drill,
or proof of every tenant's complete semantic behavior after recovery.
