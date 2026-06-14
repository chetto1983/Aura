# 17-08 Backup Wiring And Lifecycle Docs Summary

## Result

Completed the final Packaging & Distribution plan.

- Replaced the scheduled backup implementation with an in-box, socketless model:
  - Postgres runs `pg_dump` over the Compose network from `AURA_DB_MIGRATE_URL`/`POSTGRES_*`.
  - Neo4j runs `apoc.export.cypher.all(null, {stream:true,...})` over Bolt and writes a restorable `.cypher` artifact.
  - Both variants write under `AURA_BACKUP_DIR`, verify the artifact exists, preserve retention, and preserve `MissedBackupAlert`.
- Removed the old Docker-exec backup seam and fake Docker tests; replaced them with `pgDumper`/`neo4jDumper` seams.
- Added a `db_integration && backup_live` manual Postgres backup test for real network `pg_dump` runs.
- Extended `scripts/restore_drill.sh` with a Neo4j `cypher-shell` restore leg alongside the Postgres `pg_restore` drill.
- Rewrote `README.md` around the end-user appliance flow: installer, Windows Docker Desktop path, appliance/systemd, update path, backups/restores, WhatsApp residuals, Caddy trust, and retired host installs.
- Added artifact regression tests for the backup lifecycle docs and restore script.

## Decisions

- The selected Req 16 execution model is option-b-network-dump: no Docker socket and no `docker exec` path in the scheduled backup.
- Neo4j export uses the native Bolt driver rather than MCP because the plan explicitly allowed a direct-Bolt fallback when streamed APOC payloads might not fit the MCP channel.
- Neo4j backups now use the `.cypher` suffix; retention also prunes legacy `neo4j-*.dump` files so old artifacts age out.
- The live backup test is tagged `db_integration && backup_live`, keeping normal CI's Postgres-only `db_integration` tier from requiring host `pg_dump`.

## Verification

- `bash -n scripts/restore_drill.sh`
- `go test ./internal/cron/handlers/ -v`
- `go test ./internal/cron/handlers/ -coverprofile=coverage-handlers` -> `coverage: 86.8% of statements`
- `go vet ./internal/cron/handlers/`
- `go test -race ./internal/cron/handlers/`
- `go test ./cmd/aura -v`
- `go build ./...`
- `bash scripts/check-file-size.sh`
- Static checks:
  - `rg "docker exec|resolveDocker|dockerCLI" internal/cron/handlers/backup.go` returned no matches.
  - README contains `install.sh`, `docker compose pull`, restore docs, `cypher-shell`, WhatsApp Terms/QR notes, and `tls internal`.
  - `scripts/restore_drill.sh` contains `pg_restore`, `cypher-shell`, `bolt://neo4j:7687`, and `NEO4J_DUMPFILE`.

## Manual-Only

- Not run locally: `go test -tags "db_integration backup_live" -count=1 -run TestBackupNetworkPostgresLive ./internal/cron/handlers/`.
- Not run locally: destructive Neo4j restore against a live graph. The README and restore drill document the rebuild behavior and require an existing `neo4j-*.cypher` artifact.
