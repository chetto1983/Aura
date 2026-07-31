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
