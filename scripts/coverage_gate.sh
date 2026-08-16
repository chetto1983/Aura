#!/usr/bin/env bash
# Industrial coverage floor for the OWNED library surface (internal/*), enforcing
# the CLAUDE.md COVERAGE FLOOR 85% across the full tag matrix (unit + integration).
#
# Why internal/* and not ./...:
#   - cmd/aura is CLI glue (flag parsing, os.Exit dispatch) — covered behaviourally
#     by integration + smoke, not by line-unit tests; folding it into a statement
#     floor measures the wrong thing (it sits ~20% by nature).
#   - generated (internal/db/sqlc) and the pre-rewrite llm/client.go skeleton
#     (owned by Slice 1) are excluded until rewritten. (internal/sandbox was
#     removed and internal/swarm is now measured — neither is filtered here.)
#   - internal/agent/agenttest and internal/dbtest are test-support (shared Agent
#     fakes/mocks, and the guard that stops a db_integration run migrating the live
#     database); like sqlc they are not owned runtime surface, and their own
#     self-coverage dilutes the floor without measuring any (T-04).
#
# Integration tiers REQUIRE the container stack + env (AURA_DB_URL, AURA_DB_MIGRATE_URL).
# Run after `make db-migrate memory-up`, or in the CI coverage job that has the stack.
# NO-SKIP-AS-GREEN: the tagged tests t.Fatal under $CI when their env is unset, so a
# skipped tier cannot pass this gate falsely.
#
# The tag set lost the graph tier when Aura's graph store (internal/knowledge) was
# retired: no file carries that tag any more, so naming it would only suggest a tier
# that measures something.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

MIN="${AURA_COVERAGE_MIN:-85}"
TAGS="${AURA_COVERAGE_TAGS:-db_integration}"
PROFILE="${AURA_COVERAGE_PROFILE:-cover_gate.out}"
REPORT="${AURA_COVERAGE_REPORT:-artifacts/production-readiness/coverage-report.json}"

# Anti-footgun (defense-in-depth; see scripts/coverage_docker.sh + the 2026-07-10
# data-loss incident): the db_integration tier TRUNCATEs/DELETEs shared auth tables
# on setup. On a developer host a database named `aura` is the live personal
# deployment — running the gate against it destroys the operator identity + the
# authula schema. CI provisions a fresh ephemeral `aura` and always sets
# GITHUB_ACTIONS, so it is exempt; locally, point the gate at a disposable DB
# (`make coverage-docker` does this automatically). Escape hatch: AURA_COVERAGE_ALLOW_LIVE_AURA_DB=1.
if [[ " ${TAGS} " == *" db_integration "* ]] && [ -z "${GITHUB_ACTIONS:-}" ]; then
  _cov_db="$(printf '%s' "${AURA_DB_URL:-}" | sed -E 's#.*/([^/?]+)(\?.*)?$#\1#')"
  if [ "$_cov_db" = "aura" ] && [ "${AURA_COVERAGE_ALLOW_LIVE_AURA_DB:-}" != "1" ]; then
    echo "FATAL: refusing db_integration coverage against the live 'aura' database — it TRUNCATEs shared auth tables (data loss, see the 2026-07-10 incident)." >&2
    echo "       Use 'make coverage-docker' (disposable DB), or export AURA_DB_URL for a throwaway DB. Intentional override (danger): AURA_COVERAGE_ALLOW_LIVE_AURA_DB=1." >&2
    exit 5
  fi
fi

echo "==> coverage gate: internal/* >= ${MIN}% (tags: ${TAGS})"
# -p 1 (serial package execution) is MANDATORY: the integration tiers across
# internal/* share ONE Postgres, so running packages concurrently collides on
# global cluster state — CREATE ROLE (EnsureRoles) races to "tuple concurrently
# updated (XX000)" on pg_authid, and golang-migrate's pg_advisory_lock deadlocks.
# The default parallel run is flaky once >2 integration packages exist (broke CI
# after Phase 4 added identity/askuser/conversations/runner). Fail loud — never
# discard the test output, or a real failure looks like a coverage miss.
if ! go test -tags "${TAGS}" -p 1 -count=1 -covermode=atomic -coverprofile="${PROFILE}" \
  ./internal/... > "${PROFILE}.testlog" 2>&1; then
  echo "FAIL: integration coverage test run failed (see output below)" >&2
  cat "${PROFILE}.testlog" >&2
  exit 1
fi

# Drop generated + skeleton rows. Anchor each at a path-segment boundary ('/<x>')
# so a future sibling whose name merely contains one of these is not silently
# dropped from the floor.
{
  head -1 "${PROFILE}"
  grep -v '^mode:' "${PROFILE}" | grep -vE '/internal/db/sqlc/|/internal/agent/agenttest/|/internal/dbtest/|/internal/llm/client\.go:'
} > "${PROFILE}.filtered"

ROWS="$(grep -c -v '^mode:' "${PROFILE}.filtered" || true)"
if [[ "${ROWS:-0}" -lt 1 ]]; then
  echo "FAIL: filtered coverage profile has no statement rows (filter too aggressive?)" >&2
  exit 1
fi

TOTAL_LINE="$(go tool cover -func="${PROFILE}.filtered" | tail -1)"
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
  END { printf "%d %d\n", covered, total }' "${PROFILE}.filtered"
)
if [[ "${TOTAL_STATEMENTS:-0}" -lt 1 ]]; then
  echo "FAIL: filtered coverage profile has no statements" >&2
  exit 1
fi
awk \
  -v covered="${COVERED_STATEMENTS}" \
  -v total="${TOTAL_STATEMENTS}" \
  -v displayed="${PCT%\%}" \
  -v min="${MIN}" \
  'BEGIN {
  if (covered * 100 < min * total) {
    printf "FAIL: owned coverage %d/%d (%s%% displayed) < %s%% (CLAUDE.md floor)\n", covered, total, displayed, min > "/dev/stderr"
    exit 1
  }
  printf "ok: owned coverage %d/%d (%s%% displayed) >= %s%%\n", covered, total, displayed, min
}'

python3 - "$REPORT" "$COVERED_STATEMENTS" "$TOTAL_STATEMENTS" "$TAGS" <<'PY'
import datetime as dt
import json
import pathlib
import subprocess
import sys

output = pathlib.Path(sys.argv[1])
covered = int(sys.argv[2])
total = int(sys.argv[3])
tags = sys.argv[4].split()
candidate = subprocess.run(
    ["git", "rev-parse", "HEAD"], text=True, capture_output=True, check=True
).stdout.strip()
report = {
    "schema_version": 1,
    "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
    "candidate_commit": candidate,
    "passed": True,
    "statements_percent": covered * 100 / total,
    "covered_statements": covered,
    "total_statements": total,
    "tiers_executed": tags,
    "empty_tiers": 0,
}
output.parent.mkdir(parents=True, exist_ok=True)
output.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
PY
