#!/usr/bin/env bash
# Offline Neo4j Community dump/load drill. The live source is never overwritten.
set -euo pipefail

WORK_DIR="${1:?usage: neo4j_offline_drill.sh WORK_DIR REPORT_JSON}"
REPORT_JSON="${2:?usage: neo4j_offline_drill.sh WORK_DIR REPORT_JSON}"
RUN_ID="${AURA_DR_RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)-$$}"
SOURCE_CONTAINER="${NEO4J_CONTAINER:-aura-neo4j}"
DATABASE="${AURA_NEO4J_DATABASE:-neo4j}"
USER_NAME="${NEO4J_USER:-neo4j}"
PASSWORD="${NEO4J_PASSWORD:?NEO4J_PASSWORD required}"

case "$RUN_ID" in
  *[!A-Za-z0-9_-]*) echo "neo4j drill: unsafe run id" >&2; exit 2 ;;
esac
mkdir -p "$WORK_DIR" "$(dirname "$REPORT_JSON")"
chmod 0777 "$WORK_DIR"

TARGET_CONTAINER="aura-dr-neo4j-${RUN_ID}"
TARGET_VOLUME="aura-dr-neo4j-${RUN_ID}"
FIXTURE_ID="aura-dr-${RUN_ID}"
FIXTURE_HASH="$(printf '%s' "$FIXTURE_ID" | sha256sum | awk '{print $1}')"
SOURCE_STOPPED=0
FIXTURE_SEEDED=0
TARGET_STARTED=0

source_cypher() {
  docker exec "$SOURCE_CONTAINER" cypher-shell \
    -u "$USER_NAME" -p "$PASSWORD" -d "$DATABASE" "$1"
}

wait_cypher() {
  local container="$1"
  local deadline=$(( $(date +%s) + 180 ))
  until docker exec "$container" cypher-shell \
      -u "$USER_NAME" -p "$PASSWORD" -d "$DATABASE" "RETURN 1" >/dev/null 2>&1; do
    if [ "$(date +%s)" -ge "$deadline" ]; then
      echo "neo4j drill: $container did not become queryable" >&2
      docker logs "$container" --tail 80 >&2 || true
      return 1
    fi
    sleep 2
  done
}

cleanup() {
  if [ "$TARGET_STARTED" = "1" ]; then
    docker rm -f "$TARGET_CONTAINER" >/dev/null 2>&1 || true
  fi
  docker volume rm "$TARGET_VOLUME" >/dev/null 2>&1 || true
  if [ "$SOURCE_STOPPED" = "1" ]; then
    docker start "$SOURCE_CONTAINER" >/dev/null 2>&1 || true
    wait_cypher "$SOURCE_CONTAINER" >/dev/null 2>&1 || true
    SOURCE_STOPPED=0
  fi
  if [ "$FIXTURE_SEEDED" = "1" ]; then
    source_cypher "MATCH (n:AuraDRFixture {id:'${FIXTURE_ID}'}) DETACH DELETE n" >/dev/null 2>&1 || true
  fi
  rm -f "$WORK_DIR/neo4j.dump"
}
trap cleanup EXIT

if ! docker inspect "$SOURCE_CONTAINER" >/dev/null 2>&1; then
  echo "neo4j drill: source container $SOURCE_CONTAINER does not exist" >&2
  exit 1
fi
if [ "$(docker inspect -f '{{.State.Running}}' "$SOURCE_CONTAINER")" != "true" ]; then
  echo "neo4j drill: source container $SOURCE_CONTAINER is not running" >&2
  exit 1
fi

IMAGE="$(docker inspect -f '{{.Config.Image}}' "$SOURCE_CONTAINER")"
SOURCE_VOLUME="$(docker inspect -f '{{range .Mounts}}{{if eq .Destination "/data"}}{{.Name}}{{end}}{{end}}' "$SOURCE_CONTAINER")"
if [ -z "$SOURCE_VOLUME" ]; then
  echo "neo4j drill: source /data is not a named volume" >&2
  exit 1
fi

FIXTURE_EPOCH="$(date +%s)"
source_cypher "MERGE (n:AuraDRFixture {id:'${FIXTURE_ID}'}) SET n.hash='${FIXTURE_HASH}', n.created_at=datetime()" >/dev/null
FIXTURE_SEEDED=1

MAINTENANCE_START_NS="$(date +%s%N)"
docker stop --time 30 "$SOURCE_CONTAINER" >/dev/null
SOURCE_STOPPED=1

docker run --rm --user neo4j --entrypoint neo4j-admin \
  -v "${SOURCE_VOLUME}:/data" \
  -v "${WORK_DIR}:/backups" \
  "$IMAGE" database dump "$DATABASE" --to-path=/backups --overwrite-destination=true \
  >"$WORK_DIR/dump.log" 2>&1
test -s "$WORK_DIR/${DATABASE}.dump"
SNAPSHOT_EPOCH="$(date +%s)"

docker start "$SOURCE_CONTAINER" >/dev/null
wait_cypher "$SOURCE_CONTAINER"
SOURCE_STOPPED=0
MAINTENANCE_MS=$(( ( $(date +%s%N) - MAINTENANCE_START_NS ) / 1000000 ))

docker volume create "$TARGET_VOLUME" >/dev/null
RESTORE_START_NS="$(date +%s%N)"
docker run --rm --user neo4j --entrypoint neo4j-admin \
  -v "${TARGET_VOLUME}:/data" \
  -v "${WORK_DIR}:/backups:ro" \
  "$IMAGE" database load "$DATABASE" --from-path=/backups --overwrite-destination=true \
  >"$WORK_DIR/load.log" 2>&1

docker run -d --name "$TARGET_CONTAINER" \
  -e "NEO4J_AUTH=${USER_NAME}/${PASSWORD}" \
  -v "${TARGET_VOLUME}:/data" \
  "$IMAGE" >/dev/null
TARGET_STARTED=1
wait_cypher "$TARGET_CONTAINER"

RESTORED_HASH="$(docker exec "$TARGET_CONTAINER" cypher-shell \
  -u "$USER_NAME" -p "$PASSWORD" -d "$DATABASE" --format plain \
  "MATCH (n:AuraDRFixture {id:'${FIXTURE_ID}'}) RETURN n.hash AS hash" \
  | tail -n 1 | tr -d '\"[:space:]')"
if [ "$RESTORED_HASH" != "$FIXTURE_HASH" ]; then
  echo "neo4j drill: restored sentinel checksum mismatch" >&2
  exit 1
fi
RTO_MS=$(( ( $(date +%s%N) - RESTORE_START_NS ) / 1000000 ))
RPO_SECONDS=$(( SNAPSHOT_EPOCH - FIXTURE_EPOCH ))

docker rm -f "$TARGET_CONTAINER" >/dev/null
TARGET_STARTED=0
docker volume rm "$TARGET_VOLUME" >/dev/null
source_cypher "MATCH (n:AuraDRFixture {id:'${FIXTURE_ID}'}) DETACH DELETE n" >/dev/null
FIXTURE_SEEDED=0
rm -f "$WORK_DIR/${DATABASE}.dump"

python3 - "$REPORT_JSON" "$RPO_SECONDS" "$RTO_MS" "$MAINTENANCE_MS" <<'PY'
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
path.parent.mkdir(parents=True, exist_ok=True)
path.write_text(json.dumps({
    "plane": "neo4j",
    "mode": "offline_dump_load",
    "rpo_seconds": int(sys.argv[2]),
    "rto_ms": int(sys.argv[3]),
    "maintenance_ms": int(sys.argv[4]),
    "restored_items": 1,
    "restored_bytes": 0,
    "checksum_ok": True,
    "cleanup_ok": True,
}, indent=2) + "\n", encoding="utf-8")
PY
