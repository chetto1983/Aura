#!/usr/bin/env bash
# Four-plane production DR drill: Postgres, sidecars, Garage, ArcadeDB memory.
set -euo pipefail
export MSYS_NO_PATHCONV=1
export MSYS2_ARG_CONV_EXCL='*'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/restore_drill_lib.sh"

OUTPUT="${AURA_DR_REPORT:-artifacts/production-readiness/dr-report.json}"
RUN_ID="${AURA_DR_RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)-$$}"
PG_SERVICE="${PG_CONTAINER:-postgres}"
SOURCE_DB="${POSTGRES_DB:-aura}"
DB_USER="${POSTGRES_USER:-aura}"
DB_PASS="${POSTGRES_PASSWORD:?POSTGRES_PASSWORD required}"
ARCADE_SERVICE="${ARCADEDB_CONTAINER:-arcadedb}"
ARCADE_USER="${ARCADEDB_ADMIN_USER:-root}"
ARCADE_PASS="${ARCADEDB_ADMIN_PASSWORD:-${ARCADEDB_PASSWORD:?ARCADEDB_PASSWORD required}}"
ARCADE_URL="${AURA_DR_ARCADEDB_URL:-http://127.0.0.1:2480}"
ARCADE_WAIT_SECONDS="${AURA_DR_ARCADEDB_BACKUP_WAIT_SECONDS:-60}"
CANDIDATE_COMMIT="$(git rev-parse HEAD)"

case "$ARCADE_WAIT_SECONDS" in
  '' | *[!0-9]*) echo "restore drill: ArcadeDB backup wait must be a positive integer" >&2; exit 2 ;;
esac
if [ "$ARCADE_WAIT_SECONDS" -lt 1 ] || [ "$ARCADE_WAIT_SECONDS" -gt 300 ]; then
  echo "restore drill: ArcadeDB backup wait must be between 1 and 300 seconds" >&2
  exit 2
fi

case "$RUN_ID" in
  *[!A-Za-z0-9_-]*) echo "restore drill: unsafe run id" >&2; exit 2 ;;
esac
SAFE_ID="$(dr_safe_id "$RUN_ID")"
if [ -z "$SAFE_ID" ]; then
  echo "restore drill: run id has no usable identifier characters" >&2
  exit 2
fi
TARGET_DB="aura_dr_${SAFE_ID}"
FIXTURE_SCHEMA="aura_dr_${SAFE_ID}"
FIXTURE_HASH="$(printf '%s' "$RUN_ID" | sha256sum | awk '{print $1}')"
ARCADE_DATABASE="$(dr_arcadedb_database "$RUN_ID")"
if ! dr_arcadedb_database_is_safe "$ARCADE_DATABASE"; then
  echo "restore drill: unsafe ArcadeDB database name" >&2
  exit 2
fi
WORK_DIR="$(mktemp -d "${PWD}/.aura-dr-tmp.XXXXXX")"
mkdir -p "$(dirname "$OUTPUT")"
chmod 0777 "$WORK_DIR"

if [[ -z "${DOCKER:-}" ]]; then
  if command -v docker >/dev/null 2>&1; then
    DOCKER=docker
  else
    DOCKER=docker.exe
  fi
fi
GO_BIN="${GO_BIN:-$(command -v go || true)}"
if [ -z "$GO_BIN" ] && [ -x "${HOME}/.local/bin/go" ]; then
  GO_BIN="${HOME}/.local/bin/go"
fi
if [ -z "$GO_BIN" ]; then
  echo "restore drill: go executable not found" >&2
  exit 1
fi

run_pg() {
  "$DOCKER" compose exec -T -e PGPASSWORD="$DB_PASS" "$PG_SERVICE" "$@"
}

pg_sql() {
  run_pg psql -v ON_ERROR_STOP=1 -At -U "$DB_USER" -d "$1" -c "$2"
}

arcade_http_post() {
  local endpoint="$1"
  local response_file="$2"
  if ! curl --fail-with-body --silent --show-error \
    --connect-timeout 5 --max-time 180 \
    --user "$ARCADE_USER:$ARCADE_PASS" \
    --header 'Content-Type: application/json' \
    --data-binary "@$WORK_DIR/arcadedb-request.json" \
    --output "$response_file" \
    "$endpoint"; then
    python3 - "$response_file" <<'PY' >&2
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
try:
    body = json.loads(path.read_text(encoding="utf-8"))
except (OSError, json.JSONDecodeError):
    print("restore drill: ArcadeDB request failed without a JSON error response")
else:
    safe = {key: body.get(key) for key in ("error", "detail", "exception") if body.get(key)}
    print("restore drill: ArcadeDB request failed: " + json.dumps(safe, sort_keys=True))
PY
    return 1
  fi
}

arcade_server_command() {
  local command="$1"
  local response_file="${2:-$WORK_DIR/arcadedb-server-response.json}"
  python3 - "$WORK_DIR/arcadedb-request.json" "$command" <<'PY'
import json
import pathlib
import sys

pathlib.Path(sys.argv[1]).write_text(
    json.dumps({"command": sys.argv[2]}), encoding="utf-8"
)
PY
  arcade_http_post "$ARCADE_URL/api/v1/server" "$response_file"
}

arcade_database_request() {
  local endpoint="$1"
  local database="$2"
  local language="$3"
  local statement="$4"
  local response_file="$5"
  shift 5
  python3 - "$WORK_DIR/arcadedb-request.json" "$language" "$statement" "$@" <<'PY'
import json
import pathlib
import sys

extra = sys.argv[4:]
if len(extra) % 2:
    raise SystemExit("ArcadeDB request parameters must be key/value pairs")
params = dict(zip(extra[::2], extra[1::2]))
payload = {"language": sys.argv[2], "command": sys.argv[3], "limit": -1}
if params:
    payload["params"] = params
pathlib.Path(sys.argv[1]).write_text(json.dumps(payload), encoding="utf-8")
PY
  arcade_http_post "$ARCADE_URL/api/v1/$endpoint/$database" "$response_file"
}

arcade_latest_archive() {
  "$DOCKER" compose exec -T "$ARCADE_SERVICE" sh -eu -c '
    if [ -d "backups/$1" ]; then
      find "backups/$1" -maxdepth 1 -type f -name "$1-backup-*.zip" -print 2>/dev/null \
        | sort | tail -n 1
    fi
  ' sh "$ARCADE_DATABASE" | tr -d '\r'
}

SIDECAR_SOURCE_VOLUME=""
SIDECAR_TARGET_VOLUME="aura-dr-sidecars-${RUN_ID}"
SIDECAR_FIXTURE="drill-${RUN_ID}"
SOURCE_FIXTURE_CREATED=0
SIDECAR_SOURCE_VOLUME_CREATED=0
TARGET_DB_CREATED=0
ARCADE_DATABASE_OWNED=0
ARCADE_BACKUP_OWNED=0

cleanup() {
  set +e
  if [ "$TARGET_DB_CREATED" = "1" ]; then
    pg_sql postgres "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='${TARGET_DB}' AND pid <> pg_backend_pid();" >/dev/null 2>&1
    pg_sql postgres "DROP DATABASE IF EXISTS ${TARGET_DB};" >/dev/null 2>&1
  fi
  pg_sql "$SOURCE_DB" "DROP SCHEMA IF EXISTS ${FIXTURE_SCHEMA} CASCADE;" >/dev/null 2>&1
  run_pg rm -f "/tmp/${TARGET_DB}.dump" >/dev/null 2>&1
  if [ "$SOURCE_FIXTURE_CREATED" = "1" ] && [ -n "$SIDECAR_SOURCE_VOLUME" ]; then
    "$DOCKER" run --rm -e "FIXTURE=${SIDECAR_FIXTURE}" \
      -v "${SIDECAR_SOURCE_VOLUME}:/source" alpine:3.22 \
      sh -eu -c 'rm -rf "/source/runs/$FIXTURE"' >/dev/null 2>&1
  fi
  if [ "$SIDECAR_SOURCE_VOLUME_CREATED" = "1" ] && [ -n "$SIDECAR_SOURCE_VOLUME" ]; then
    "$DOCKER" volume rm "$SIDECAR_SOURCE_VOLUME" >/dev/null 2>&1
  fi
  "$DOCKER" volume rm "$SIDECAR_TARGET_VOLUME" >/dev/null 2>&1
  if [ "$ARCADE_DATABASE_OWNED" = "1" ]; then
    arcade_server_command "drop database $ARCADE_DATABASE" >/dev/null 2>&1
  fi
  if [ "$ARCADE_BACKUP_OWNED" = "1" ]; then
    "$DOCKER" compose exec -T "$ARCADE_SERVICE" sh -eu -c \
      'rm -rf -- "backups/$1"' sh "$ARCADE_DATABASE" >/dev/null 2>&1
  fi
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

echo "==> DR plane 1/4: Postgres dump -> isolated database"
FIXTURE_EPOCH="$(date +%s)"
pg_sql "$SOURCE_DB" \
  "CREATE SCHEMA ${FIXTURE_SCHEMA}; CREATE TABLE ${FIXTURE_SCHEMA}.sentinel (id text PRIMARY KEY, hash text NOT NULL); INSERT INTO ${FIXTURE_SCHEMA}.sentinel VALUES ('${RUN_ID}', '${FIXTURE_HASH}');" \
  >/dev/null

run_pg pg_dump -U "$DB_USER" -Fc -d "$SOURCE_DB" -f "/tmp/${TARGET_DB}.dump"
PG_SNAPSHOT_EPOCH="$(date +%s)"
PG_DUMP_BYTES="$(run_pg stat -c %s "/tmp/${TARGET_DB}.dump" | tr -d '[:space:]')"
pg_sql "$SOURCE_DB" "DROP SCHEMA ${FIXTURE_SCHEMA} CASCADE;" >/dev/null

pg_sql postgres "DROP DATABASE IF EXISTS ${TARGET_DB};" >/dev/null
pg_sql postgres "CREATE DATABASE ${TARGET_DB} OWNER ${DB_USER};" >/dev/null
TARGET_DB_CREATED=1
PG_RESTORE_START_NS="$(date +%s%N)"
run_pg pg_restore -U "$DB_USER" -d "$TARGET_DB" --no-owner --no-acl "/tmp/${TARGET_DB}.dump"
RESTORED_HASH="$(pg_sql "$TARGET_DB" "SELECT hash FROM ${FIXTURE_SCHEMA}.sentinel WHERE id='${RUN_ID}';" | tr -d '[:space:]')"
PG_RTO_MS=$(( ( $(date +%s%N) - PG_RESTORE_START_NS ) / 1000000 ))
if [ "$RESTORED_HASH" != "$FIXTURE_HASH" ]; then
  echo "restore drill: Postgres sentinel checksum mismatch" >&2
  exit 1
fi
pg_sql postgres "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='${TARGET_DB}' AND pid <> pg_backend_pid();" >/dev/null
pg_sql postgres "DROP DATABASE ${TARGET_DB};" >/dev/null
TARGET_DB_CREATED=0
run_pg rm -f "/tmp/${TARGET_DB}.dump"
python3 - "$WORK_DIR/postgres.json" "$((PG_SNAPSHOT_EPOCH - FIXTURE_EPOCH))" "$PG_RTO_MS" "$PG_DUMP_BYTES" <<'PY'
import json, pathlib, sys
path = pathlib.Path(sys.argv[1])
path.write_text(json.dumps({
    "plane": "postgres", "rpo_seconds": int(sys.argv[2]), "rto_ms": int(sys.argv[3]),
    "restored_items": 1, "restored_bytes": int(sys.argv[4]),
    "checksum_ok": True, "cleanup_ok": True,
}, indent=2) + "\n", encoding="utf-8")
PY

echo "==> DR plane 2/4: conversation sidecar archive -> disposable volume"
SIDECAR_SOURCE_VOLUME="$(
  "$DOCKER" compose config --format json | dr_compose_volume_name aura-home
)"
if ! "$DOCKER" volume inspect "$SIDECAR_SOURCE_VOLUME" >/dev/null 2>&1; then
  "$DOCKER" volume create "$SIDECAR_SOURCE_VOLUME" >/dev/null
  SIDECAR_SOURCE_VOLUME_CREATED=1
fi
"$DOCKER" run --rm -e "FIXTURE=${SIDECAR_FIXTURE}" -e "HASH=${FIXTURE_HASH}" \
  -v "${SIDECAR_SOURCE_VOLUME}:/source" alpine:3.22 \
  sh -eu -c 'mkdir -p "/source/runs/$FIXTURE/nested"; printf "%s\n" "$HASH" >"/source/runs/$FIXTURE/sentinel.sha256"; printf "sidecar-state\n" >"/source/runs/$FIXTURE/nested/state.txt"' \
  >/dev/null
SOURCE_FIXTURE_CREATED=1
SIDECAR_FIXTURE_EPOCH="$(date +%s)"
"$DOCKER" run --rm \
  -v "${SIDECAR_SOURCE_VOLUME}:/source:ro" \
  -v "${WORK_DIR}:/backup" alpine:3.22 \
  tar -C /source -czf /backup/sidecars.tgz runs
SIDECAR_SNAPSHOT_EPOCH="$(date +%s)"
SIDECAR_ARCHIVE_BYTES="$(wc -c <"$WORK_DIR/sidecars.tgz" | tr -d '[:space:]')"
"$DOCKER" run --rm -e "FIXTURE=${SIDECAR_FIXTURE}" \
  -v "${SIDECAR_SOURCE_VOLUME}:/source" alpine:3.22 \
  sh -eu -c 'rm -rf "/source/runs/$FIXTURE"' >/dev/null
SOURCE_FIXTURE_CREATED=0
if [ "$SIDECAR_SOURCE_VOLUME_CREATED" = "1" ]; then
  "$DOCKER" volume rm "$SIDECAR_SOURCE_VOLUME" >/dev/null
  SIDECAR_SOURCE_VOLUME_CREATED=0
fi

"$DOCKER" volume create "$SIDECAR_TARGET_VOLUME" >/dev/null
SIDECAR_RESTORE_START_NS="$(date +%s%N)"
"$DOCKER" run --rm \
  -v "${SIDECAR_TARGET_VOLUME}:/restore" \
  -v "${WORK_DIR}:/backup:ro" alpine:3.22 \
  tar -C /restore -xzf /backup/sidecars.tgz
SIDECAR_RESTORED_HASH="$("$DOCKER" run --rm -e "FIXTURE=${SIDECAR_FIXTURE}" \
  -v "${SIDECAR_TARGET_VOLUME}:/restore:ro" alpine:3.22 \
  sh -eu -c 'tr -d "[:space:]" <"/restore/runs/$FIXTURE/sentinel.sha256"')"
SIDECAR_RESTORED_FILES="$("$DOCKER" run --rm \
  -v "${SIDECAR_TARGET_VOLUME}:/restore:ro" alpine:3.22 \
  sh -eu -c 'find /restore/runs -type f | wc -l' | tr -d '[:space:]')"
SIDECAR_RTO_MS=$(( ( $(date +%s%N) - SIDECAR_RESTORE_START_NS ) / 1000000 ))
if [ "$SIDECAR_RESTORED_HASH" != "$FIXTURE_HASH" ]; then
  echo "restore drill: sidecar sentinel checksum mismatch" >&2
  exit 1
fi
"$DOCKER" volume rm "$SIDECAR_TARGET_VOLUME" >/dev/null
python3 - "$WORK_DIR/sidecars.json" \
  "$((SIDECAR_SNAPSHOT_EPOCH - SIDECAR_FIXTURE_EPOCH))" "$SIDECAR_RTO_MS" \
  "$SIDECAR_RESTORED_FILES" "$SIDECAR_ARCHIVE_BYTES" <<'PY'
import json, pathlib, sys
path = pathlib.Path(sys.argv[1])
path.write_text(json.dumps({
    "plane": "sidecars", "rpo_seconds": int(sys.argv[2]), "rto_ms": int(sys.argv[3]),
    "restored_items": int(sys.argv[4]), "restored_bytes": int(sys.argv[5]),
    "checksum_ok": True, "cleanup_ok": True,
}, indent=2) + "\n", encoding="utf-8")
PY

echo "==> DR plane 3/4: Garage authenticated export/delete/restore"
"$GO_BIN" run ./scripts/objectstore_drill.go \
  --prefix "dr/${RUN_ID}/" \
  --backup-dir "$WORK_DIR/garage" \
  --output "$WORK_DIR/garage.json"

echo "==> DR plane 4/4: ArcadeDB tenant backup -> drop -> native restore"
if "$DOCKER" compose exec -T "$ARCADE_SERVICE" sh -eu -c \
  'test -e "databases/$1" || test -e "backups/$1"' sh "$ARCADE_DATABASE"; then
  echo "restore drill: disposable ArcadeDB target already exists" >&2
  exit 1
fi

arcade_server_command "create database $ARCADE_DATABASE"
ARCADE_DATABASE_OWNED=1
ARCADE_BACKUP_OWNED=1
arcade_database_request command "$ARCADE_DATABASE" sql \
  "CREATE DOCUMENT TYPE DRSentinel" "$WORK_DIR/arcadedb-create-type.json"
arcade_database_request command "$ARCADE_DATABASE" sql \
  "INSERT INTO DRSentinel SET run_id = :run_id, hash = :hash" \
  "$WORK_DIR/arcadedb-insert.json" run_id "$RUN_ID" hash "$FIXTURE_HASH"

ARCADE_FIXTURE_EPOCH="$(date +%s)"
arcade_server_command "trigger backup $ARCADE_DATABASE" "$WORK_DIR/arcadedb-backup.json"
ARCADE_ARCHIVE=""
for ((attempt = 0; attempt < ARCADE_WAIT_SECONDS; attempt++)); do
  ARCADE_ARCHIVE="$(arcade_latest_archive)"
  if [ -n "$ARCADE_ARCHIVE" ]; then
    break
  fi
  sleep 1
done
if [ -z "$ARCADE_ARCHIVE" ]; then
  echo "restore drill: ArcadeDB backup archive did not appear within ${ARCADE_WAIT_SECONDS}s" >&2
  exit 1
fi
read -r ARCADE_ARCHIVE_BYTES ARCADE_SNAPSHOT_EPOCH < <(
  "$DOCKER" compose exec -T "$ARCADE_SERVICE" sh -eu -c \
    'stat -c "%s %Y" "$1"' sh "$ARCADE_ARCHIVE" | tr -d '\r'
)
if [ "$ARCADE_ARCHIVE_BYTES" -lt 1 ]; then
  echo "restore drill: ArcadeDB backup archive is empty" >&2
  exit 1
fi

arcade_server_command "drop database $ARCADE_DATABASE" "$WORK_DIR/arcadedb-drop.json"
ARCADE_DATABASE_OWNED=0
ARCADE_RESTORE_START_NS="$(date +%s%N)"
arcade_server_command \
  "restore database $ARCADE_DATABASE file:///home/arcadedb/$ARCADE_ARCHIVE" \
  "$WORK_DIR/arcadedb-restore.json"
ARCADE_DATABASE_OWNED=1
ARCADE_RTO_MS=$(( ( $(date +%s%N) - ARCADE_RESTORE_START_NS ) / 1000000 ))

arcade_database_request query "$ARCADE_DATABASE" sql \
  "SELECT hash FROM DRSentinel WHERE run_id = :run_id" \
  "$WORK_DIR/arcadedb-query.json" run_id "$RUN_ID"
ARCADE_RESTORED_HASH="$(python3 - "$WORK_DIR/arcadedb-query.json" <<'PY'
import json
import pathlib
import sys

rows = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8")).get("result", [])
if len(rows) != 1 or not isinstance(rows[0], dict):
    raise SystemExit("restore drill: ArcadeDB sentinel query did not return exactly one row")
print(rows[0].get("hash", ""))
PY
)"
if [ "$ARCADE_RESTORED_HASH" != "$FIXTURE_HASH" ]; then
  echo "restore drill: ArcadeDB sentinel checksum mismatch" >&2
  exit 1
fi

arcade_server_command "drop database $ARCADE_DATABASE" "$WORK_DIR/arcadedb-final-drop.json"
ARCADE_DATABASE_OWNED=0
"$DOCKER" compose exec -T "$ARCADE_SERVICE" sh -eu -c \
  'rm -rf -- "backups/$1"' sh "$ARCADE_DATABASE"
ARCADE_BACKUP_OWNED=0
python3 - "$WORK_DIR/arcadedb.json" \
  "$((ARCADE_SNAPSHOT_EPOCH - ARCADE_FIXTURE_EPOCH))" "$ARCADE_RTO_MS" \
  "$ARCADE_ARCHIVE_BYTES" <<'PY'
import json
import pathlib
import sys

pathlib.Path(sys.argv[1]).write_text(json.dumps({
    "plane": "arcadedb", "rpo_seconds": int(sys.argv[2]), "rto_ms": int(sys.argv[3]),
    "restored_items": 1, "restored_bytes": int(sys.argv[4]),
    "checksum_ok": True, "cleanup_ok": True,
}, indent=2) + "\n", encoding="utf-8")
PY

python3 - "$OUTPUT" "$RUN_ID" "$CANDIDATE_COMMIT" \
  "$WORK_DIR/postgres.json" "$WORK_DIR/sidecars.json" "$WORK_DIR/garage.json" \
  "$WORK_DIR/arcadedb.json" <<'PY'
import datetime as dt
import json
import pathlib
import sys

output = pathlib.Path(sys.argv[1])
run_id = sys.argv[2]
candidate_commit = sys.argv[3]
planes = [json.loads(pathlib.Path(path).read_text(encoding="utf-8")) for path in sys.argv[4:]]
expected = {"postgres", "sidecars", "garage", "arcadedb"}
observed = {plane.get("plane") for plane in planes}
if observed != expected:
    raise SystemExit(f"DR report planes {sorted(observed)} != {sorted(expected)}")
for plane in planes:
    if not plane.get("checksum_ok") or not plane.get("cleanup_ok"):
        raise SystemExit(f"DR plane failed postcondition: {plane}")
    if plane.get("restored_items", 0) < 1:
        raise SystemExit(f"DR plane restored no items: {plane}")
report = {
    "schema_version": 1,
    "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
    "candidate_commit": candidate_commit,
    "run_id": run_id,
    "passed": True,
    "planes_executed": len(planes),
    "planes_required": len(expected),
    "max_rpo_seconds": max(plane["rpo_seconds"] for plane in planes),
    "total_rto_ms": sum(plane["rto_ms"] for plane in planes),
    "planes": planes,
}
output.parent.mkdir(parents=True, exist_ok=True)
output.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
print(json.dumps(report, indent=2))
PY

echo "ok: four-plane DR drill passed; report=$OUTPUT"
