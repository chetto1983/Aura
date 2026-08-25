#!/usr/bin/env bash

# PostgreSQL folds unquoted identifiers to lower case. Keep the generated database and
# schema identifier byte-identical across SQL (`CREATE DATABASE`) and argv (`pg_restore
# -d`) by normalizing before either consumer sees it.
dr_safe_id() {
  printf '%s' "$1" \
    | tr '[:upper:]' '[:lower:]' \
    | tr -cd 'a-z0-9_' \
    | cut -c1-24
}

# Resolve the engine-level name from `docker compose config --format json`.
# This honors top-level project names, COMPOSE_PROJECT_NAME, and explicit volume names.
dr_compose_volume_name() {
  python3 -c '
import json
import sys

logical = sys.argv[1]
try:
    name = json.load(sys.stdin)["volumes"][logical]["name"]
except (KeyError, TypeError, json.JSONDecodeError) as exc:
    raise SystemExit(f"Compose volume {logical!r} is missing or invalid: {exc}")
if not isinstance(name, str) or not name:
    raise SystemExit(f"Compose volume {logical!r} has no engine name")
print(name)
' "$1"
}

# Derive a tenant-shaped disposable ArcadeDB name from a run identifier. Production
# tenant databases use mem_<uuid-with-underscores>; keeping the drill on that exact
# shape exercises scheduler discovery without touching or enumerating a real tenant.
dr_arcadedb_database() {
  local digest
  digest="$(printf '%s' "$1" | sha256sum | awk '{print $1}')"
  printf 'mem_%s_%s_%s_%s_%s' \
    "${digest:0:8}" "${digest:8:4}" "${digest:12:4}" \
    "${digest:16:4}" "${digest:20:12}"
}

dr_arcadedb_database_is_safe() {
  [[ "$1" =~ ^mem_[0-9a-f]{8}_[0-9a-f]{4}_[0-9a-f]{4}_[0-9a-f]{4}_[0-9a-f]{12}$ ]]
}
