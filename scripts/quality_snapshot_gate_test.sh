#!/usr/bin/env bash
# Self-test for scripts/quality_snapshot_gate.sh.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

write_snapshot() {
  local path="$1"
  local measured="$2"
  cat > "$path" <<EOF
# Snapshot Fixture

| Metric | Target | Last measured | Last value | Owner phase | CI gate path |
|---|---|---|---|---|---|
| Agent tools | pass | ${measured} | pass | Phase X | \`internal/foo/**\`, \`scripts/foo.sh\` |

EOF
}

run_expect() {
  local name="$1"
  local want="$2"
  local snapshot="$3"
  local changed="$4"
  local base_date="$5"
  local out rc
  set +e
  out="$(AURA_QUALITY_SNAPSHOT="$snapshot" \
    AURA_QUALITY_CHANGED_FILES="$changed" \
    AURA_QUALITY_BASE_DATE="$base_date" \
    bash scripts/quality_snapshot_gate.sh 2>&1)"
  rc=$?
  set -e
  if [[ "$rc" != "$want" ]]; then
    printf 'FAIL %s: rc=%s want=%s\n%s\n' "$name" "$rc" "$want" "$out" >&2
    exit 1
  fi
}

SNAP="$TMP/snapshot.md"

write_snapshot "$SNAP" "2026-06-01"
run_expect "unmatched path" 0 "$SNAP" "docs/readme.md" "2026-06-10"

write_snapshot "$SNAP" "operator evidence"
run_expect "missing iso date" 1 "$SNAP" "internal/foo/a.go" "2026-06-10"

write_snapshot "$SNAP" "2026-06-01"
run_expect "old measured date" 1 "$SNAP" "internal/foo/a.go" "2026-06-10"

write_snapshot "$SNAP" "2026-06-11"
run_expect "fresh measured date" 0 "$SNAP" "internal/foo/a.go" "2026-06-10"

cat > "$SNAP" <<'EOF'
# Malformed Fixture

| Metric | Target | Last measured | Last value | Owner phase | CI gate path |
|---|---|---|---|---|---|
| Broken | pass | 2026-06-11 |
EOF
run_expect "malformed table" 2 "$SNAP" "internal/foo/a.go" "2026-06-10"

echo "ok: quality snapshot gate tests passed"
