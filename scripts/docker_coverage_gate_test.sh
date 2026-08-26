#!/usr/bin/env bash
# Contract test for native covdata merging and Docker coverage report generation.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/bin"

cat > "$TMP/bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$FAKE_GO_LOG"

if [ "${1:-}" = "test" ]; then
  exit 0
fi
if [ "${1:-}" = "tool" ] && [ "${2:-}" = "covdata" ] && [ "${3:-}" = "textfmt" ]; then
  output=""
  for arg in "$@"; do
    case "$arg" in
      -o=*) output="${arg#-o=}" ;;
    esac
  done
  {
    echo "mode: atomic"
    echo "example/internal/covered.go:1.1,2.1 851 1"
    echo "example/internal/uncovered.go:1.1,2.1 149 0"
  } > "$output"
  exit 0
fi
if [ "${1:-}" = "tool" ] && [ "${2:-}" = "cover" ]; then
  printf 'total:\t(statements)\t85.1%%\n'
  exit 0
fi
echo "unexpected fake go invocation: $*" >&2
exit 2
EOF
chmod +x "$TMP/bin/go"

PATH="$TMP/bin:$PATH" \
FAKE_GO_LOG="$TMP/go.log" \
AURA_DOCKER_COVERAGE_PROFILE="$TMP/docker.cover" \
AURA_DOCKER_COVERAGE_REPORT="$TMP/docker-report.json" \
  bash scripts/docker_coverage_gate.sh

grep -q -- '-tags docker_integration' "$TMP/go.log"
grep -q -- '-coverpkg=./internal/sandbox/usersandbox/...,./internal/agent/tools/...' "$TMP/go.log"
grep -q -- './internal/sandbox/usersandbox/... ./internal/agent/tools/... ./cmd/aura/...' "$TMP/go.log"
grep -q -- 'tool covdata textfmt' "$TMP/go.log"
python3 - "$TMP/docker-report.json" <<'PY'
import json
import pathlib
import sys

report = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
assert report["passed"] is True
assert report["scope"] == "docker_owned_internal"
assert report["minimum_percent"] == 85
assert report["covered_statements"] == 851
assert report["total_statements"] == 1000
assert report["statements_percent"] == 85.1
assert report["tiers_executed"] == ["docker_integration"]
PY

echo "ok: docker coverage gate uses native covdata and emits tier evidence"
