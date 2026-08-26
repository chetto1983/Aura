#!/usr/bin/env bash
# Evaluate one already-merged Go coverage profile using the exact statement ratio.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

PROFILE="${AURA_COVERAGE_PROFILE:?AURA_COVERAGE_PROFILE is required}"
REPORT="${AURA_COVERAGE_REPORT:?AURA_COVERAGE_REPORT is required}"
TAGS="${AURA_COVERAGE_TAGS:?AURA_COVERAGE_TAGS is required}"
SCOPE="${AURA_COVERAGE_SCOPE:?AURA_COVERAGE_SCOPE is required}"
MIN="${AURA_COVERAGE_MIN:-85}"

if [ ! -s "${PROFILE}" ]; then
  echo "FAIL: coverage profile is missing or empty: ${PROFILE}" >&2
  exit 1
fi
if [ "$(head -1 "${PROFILE}")" != "mode: atomic" ] && \
   [ "$(head -1 "${PROFILE}")" != "mode: count" ] && \
   [ "$(head -1 "${PROFILE}")" != "mode: set" ]; then
  echo "FAIL: coverage profile has no valid mode header: ${PROFILE}" >&2
  exit 1
fi

ROWS="$(grep -c -v '^mode:' "${PROFILE}" || true)"
if [[ "${ROWS:-0}" -lt 1 ]]; then
  echo "FAIL: coverage profile has no statement rows: ${PROFILE}" >&2
  exit 1
fi

TOTAL_LINE="$(go tool cover -func="${PROFILE}" | tail -1)"
echo "${TOTAL_LINE}"
PCT="$(printf '%s\n' "${TOTAL_LINE}" | grep -oE '[0-9]+(\.[0-9]+)?%' | tail -1)"
if [[ -z "${PCT}" ]]; then
  echo "FAIL: could not parse a coverage percentage from: ${TOTAL_LINE}" >&2
  exit 1
fi

read -r COVERED_STATEMENTS TOTAL_STATEMENTS < <(
  awk 'NR > 1 {
    total += $2
    if ($3 > 0) covered += $2
  }
  END { printf "%d %d\n", covered, total }' "${PROFILE}"
)
if [[ "${TOTAL_STATEMENTS:-0}" -lt 1 ]]; then
  echo "FAIL: coverage profile has no statements: ${PROFILE}" >&2
  exit 1
fi

awk \
  -v covered="${COVERED_STATEMENTS}" \
  -v total="${TOTAL_STATEMENTS}" \
  -v displayed="${PCT%\%}" \
  -v min="${MIN}" \
  -v scope="${SCOPE}" \
  'BEGIN {
  if (covered * 100 < min * total) {
    printf "FAIL: %s coverage %d/%d (%s%% displayed) < %s%%\n", scope, covered, total, displayed, min > "/dev/stderr"
    exit 1
  }
  printf "ok: %s coverage %d/%d (%s%% displayed) >= %s%%\n", scope, covered, total, displayed, min
}'

python3 - "${REPORT}" "${COVERED_STATEMENTS}" "${TOTAL_STATEMENTS}" "${TAGS}" "${SCOPE}" "${MIN}" <<'PY'
import datetime as dt
import json
import pathlib
import subprocess
import sys

output = pathlib.Path(sys.argv[1])
covered = int(sys.argv[2])
total = int(sys.argv[3])
tags = sys.argv[4].split()
scope = sys.argv[5]
minimum = float(sys.argv[6])
candidate = subprocess.run(
    ["git", "rev-parse", "HEAD"], text=True, capture_output=True, check=True
).stdout.strip()
report = {
    "schema_version": 1,
    "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
    "candidate_commit": candidate,
    "passed": True,
    "scope": scope,
    "minimum_percent": minimum,
    "statements_percent": covered * 100 / total,
    "covered_statements": covered,
    "total_statements": total,
    "tiers_executed": tags,
    "empty_tiers": 0,
}
output.parent.mkdir(parents=True, exist_ok=True)
output.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
PY
