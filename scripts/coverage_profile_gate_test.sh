#!/usr/bin/env bash
# Contract test for the shared exact coverage-profile evaluator.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/bin"

cat > "$TMP/bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [ "${1:-}" = "tool" ] && [ "${2:-}" = "cover" ]; then
  printf 'total:\t(statements)\t85.0%%\n'
  exit 0
fi
echo "unexpected fake go invocation: $*" >&2
exit 2
EOF
chmod +x "$TMP/bin/go"

write_profile() {
  local path="$1"
  local covered="$2"
  local total=31527
  {
    echo "mode: atomic"
    echo "example/internal/covered.go:1.1,2.1 ${covered} 1"
    echo "example/internal/uncovered.go:1.1,2.1 $((total - covered)) 0"
  } > "$path"
}

run_profile_gate() {
  local covered="$1"
  local profile="$TMP/profile-${covered}.out"
  local report="$TMP/report-${covered}.json"
  write_profile "$profile" "$covered"
  PATH="$TMP/bin:$PATH" \
    AURA_COVERAGE_PROFILE="$profile" \
    AURA_COVERAGE_REPORT="$report" \
    AURA_COVERAGE_TAGS="fixture_tier" \
    AURA_COVERAGE_SCOPE="fixture_scope" \
    bash scripts/coverage_profile_gate.sh
}

set +e
below_out="$(run_profile_gate 26778 2>&1)"
below_rc=$?
set -e
if [ "$below_rc" -eq 0 ]; then
  printf 'FAIL: exact below-floor fixture passed\n%s\n' "$below_out" >&2
  exit 1
fi

above_out="$(run_profile_gate 26798 2>&1)"
grep -q 'ok: fixture_scope coverage' <<<"$above_out"
python3 - "$TMP/report-26798.json" <<'PY'
import json
import pathlib
import sys

report = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
assert report["passed"] is True
assert report["scope"] == "fixture_scope"
assert report["minimum_percent"] == 85
assert report["covered_statements"] == 26798
assert report["total_statements"] == 31527
assert report["tiers_executed"] == ["fixture_tier"]
PY

echo "ok: shared coverage profile gate enforces the exact ratio"
