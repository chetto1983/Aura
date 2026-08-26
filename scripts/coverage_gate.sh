#!/usr/bin/env bash
# Industrial coverage floor for the OWNED library surface (internal/*), enforcing
# the CLAUDE.md COVERAGE FLOOR 85% across unit + db_integration tests.
#
# Why internal/* and not ./...:
#   - cmd/aura is CLI glue (flag parsing, os.Exit dispatch) — covered behaviourally
#     by integration + smoke, not by line-unit tests; folding it into a statement
#     floor measures the wrong thing (it sits ~20% by nature). Its tests still run as
#     consumers so execution they drive inside internal/* is attributed correctly.
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
PACKAGE_POLICY="${AURA_COVERAGE_PACKAGE_POLICY:-scripts/coverage_package_policy.json}"
COV_TMP="$(mktemp -d)"
COVDIR="${COV_TMP}/covdata"
mkdir -p "${COVDIR}"
cleanup_coverage_tmp() {
  case "${COV_TMP}" in
    /tmp/*|/var/folders/*) rm -rf -- "${COV_TMP}" ;;
    *) echo "refusing unexpected coverage temp cleanup: ${COV_TMP}" >&2 ;;
  esac
}
trap cleanup_coverage_tmp EXIT

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
if ! go test -tags "${TAGS}" -p 1 -count=1 -covermode=atomic \
  -coverpkg=./internal/... ./internal/... ./cmd/aura/... \
  -args -test.gocoverdir="${COVDIR}" > "${PROFILE}.testlog" 2>&1; then
  echo "FAIL: integration coverage test run failed (see output below)" >&2
  cat "${PROFILE}.testlog" >&2
  exit 1
fi
if ! go tool covdata textfmt -i="${COVDIR}" -o="${PROFILE}"; then
  echo "FAIL: could not merge native coverage data" >&2
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

AURA_COVERAGE_PROFILE="${PROFILE}.filtered" \
AURA_COVERAGE_REPORT="${REPORT}" \
AURA_COVERAGE_TAGS="${TAGS}" \
AURA_COVERAGE_SCOPE="owned_internal" \
AURA_COVERAGE_MIN="${MIN}" \
  bash scripts/coverage_profile_gate.sh

python3 scripts/coverage_package_gate.py \
  --profile "${PROFILE}.filtered" \
  --policy "${PACKAGE_POLICY}" \
  --report "${REPORT}"
