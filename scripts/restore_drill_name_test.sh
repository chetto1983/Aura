#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
source scripts/restore_drill_lib.sh

got="$(dr_safe_id '20260731T021604Z-228043')"
if [ "$got" != "20260731t021604z228043" ]; then
  echo "restore-drill-name-test: default UTC run id normalized to $got" >&2
  exit 1
fi
case "$got" in
  *[!a-z0-9_]* | "") echo "restore-drill-name-test: unsafe identifier $got" >&2; exit 1 ;;
esac

long="$(dr_safe_id 'ABCDEF0123456789_ABCDEF0123456789')"
if [ "${#long}" -ne 24 ]; then
  echo "restore-drill-name-test: identifier cap is ${#long}, want 24" >&2
  exit 1
fi

volume="$(
  printf '%s' '{"volumes":{"aura-home":{"name":"ci-aura_home"}}}' \
    | dr_compose_volume_name aura-home
)"
if [ "$volume" != "ci-aura_home" ]; then
  echo "restore-drill-name-test: resolved Compose volume as $volume" >&2
  exit 1
fi
if printf '%s' '{"volumes":{}}' | dr_compose_volume_name aura-home >/dev/null 2>&1; then
  echo "restore-drill-name-test: missing Compose volume passed" >&2
  exit 1
fi

arcade_database="$(dr_arcadedb_database '20260731T021604Z-228043')"
if [ "$arcade_database" != "mem_090c49fe_ca8b_51b4_77ce_b6175088646e" ]; then
  echo "restore-drill-name-test: unexpected ArcadeDB drill database $arcade_database" >&2
  exit 1
fi
if ! dr_arcadedb_database_is_safe "$arcade_database"; then
  echo "restore-drill-name-test: generated ArcadeDB database is unsafe" >&2
  exit 1
fi
for unsafe in mem_local aura_memory 'mem_../../databases/aura_memory' ''; do
  if dr_arcadedb_database_is_safe "$unsafe"; then
    echo "restore-drill-name-test: unsafe ArcadeDB database passed: $unsafe" >&2
    exit 1
  fi
done

echo "restore-drill-name-test: pass"
