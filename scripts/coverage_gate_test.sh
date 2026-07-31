#!/usr/bin/env bash
# Regression test for exact, unrounded coverage-floor comparison.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/bin"

cat > "$TMP/bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

if [ "${1:-}" = "test" ]; then
  profile=""
  for arg in "$@"; do
    case "$arg" in
      -coverprofile=*) profile="${arg#-coverprofile=}" ;;
    esac
  done
  uncovered=$((FAKE_TOTAL - FAKE_COVERED))
  {
    echo "mode: atomic"
    echo "example/internal/covered.go:1.1,2.1 ${FAKE_COVERED} 1"
    echo "example/internal/uncovered.go:1.1,2.1 ${uncovered} 0"
  } > "$profile"
  exit 0
fi

if [ "${1:-}" = "tool" ] && [ "${2:-}" = "cover" ]; then
  printf 'total:\t(statements)\t85.0%%\n'
  exit 0
fi

echo "unexpected fake go invocation: $*" >&2
exit 2
EOF
chmod +x "$TMP/bin/go"

run_gate() {
  local covered="$1"
  local profile="$TMP/profile-${covered}.out"
  local report="$TMP/report-${covered}.json"
  PATH="$TMP/bin:$PATH" \
    FAKE_COVERED="$covered" \
    FAKE_TOTAL=31527 \
    AURA_COVERAGE_TAGS=unit \
    AURA_COVERAGE_PROFILE="$profile" \
    AURA_COVERAGE_REPORT="$report" \
    bash scripts/coverage_gate.sh
}

set +e
below_out="$(run_gate 26778 2>&1)"
below_rc=$?
set -e
if [ "$below_rc" -eq 0 ]; then
  printf 'FAIL: 26778/31527 (84.9367209059%%) passed via rounded 85.0%%\n%s\n' \
    "$below_out" >&2
  exit 1
fi

above_out="$(run_gate 26798 2>&1)"
if ! grep -q 'ok: owned coverage' <<<"$above_out"; then
  printf 'FAIL: 26798/31527 (85.0001585942%%) did not pass\n%s\n' \
    "$above_out" >&2
  exit 1
fi
python3 - "$TMP/report-26798.json" <<'PY'
import json, pathlib, re, sys
report = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
assert report["passed"] is True
assert report["covered_statements"] == 26798
assert report["total_statements"] == 31527
assert report["statements_percent"] > 85
assert report["tiers_executed"] == ["unit"]
assert report["empty_tiers"] == 0
assert re.fullmatch(r"[0-9a-f]{40}", report["candidate_commit"])
PY

echo "ok: coverage gate compares the exact statement ratio"
