#!/usr/bin/env bash
# Native-dockerd coverage gate for Aura's owned Docker sandbox runtime surfaces.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

MIN="${AURA_COVERAGE_MIN:-85}"
PROFILE="${AURA_DOCKER_COVERAGE_PROFILE:-artifacts/production-readiness/docker-coverage.cover}"
REPORT="${AURA_DOCKER_COVERAGE_REPORT:-artifacts/production-readiness/docker-coverage-report.json}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
COVDIR="${TMP}/covdata"
mkdir -p "${COVDIR}" "$(dirname "${PROFILE}")" "$(dirname "${REPORT}")"

echo "==> docker coverage gate: owned sandbox surfaces >= ${MIN}%"
# One covdata directory lets Go merge blocks emitted by all package test binaries
# without concatenating profiles or counting -coverpkg blocks more than once.
if ! go test -tags docker_integration -count=1 -p 1 -covermode=atomic \
  -coverpkg=./internal/sandbox/usersandbox/...,./internal/agent/tools/... \
  ./internal/sandbox/usersandbox/... ./internal/agent/tools/... ./cmd/aura/... \
  -args -test.gocoverdir="${COVDIR}"; then
  echo "FAIL: docker_integration coverage test run failed" >&2
  exit 1
fi

go tool covdata textfmt -i="${COVDIR}" -o="${PROFILE}"

AURA_COVERAGE_PROFILE="${PROFILE}" \
AURA_COVERAGE_REPORT="${REPORT}" \
AURA_COVERAGE_TAGS="docker_integration" \
AURA_COVERAGE_SCOPE="docker_owned_internal" \
AURA_COVERAGE_MIN="${MIN}" \
  bash scripts/coverage_profile_gate.sh
