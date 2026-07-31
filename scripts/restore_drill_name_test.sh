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

echo "restore-drill-name-test: pass"
