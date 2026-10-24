#!/usr/bin/env bash
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fixtures="$(mktemp -d)"
trap 'rm -rf -- "$fixtures"' EXIT

fail() { echo "security-scan report test: $*" >&2; exit 1; }

# A fake gh: records every call, answers the two reads from files.
mkdir -p "$fixtures/bin"
cat >"$fixtures/bin/gh" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_GH_LOG"
case "$1 $2" in
  "api repos/acme/aura/actions/runs/42/jobs") cat "$FAKE_GH_JOBS" ;;
  "issue list") cat "$FAKE_GH_ISSUES" ;;
esac
SH
chmod +x "$fixtures/bin/gh"

printf '%s\n' '| Scan postgres | https://example.invalid/job/1 |' '| Craft apps | https://example.invalid/job/2 |' >"$fixtures/jobs"
report() {
  : >"$fixtures/log"
  PATH="$fixtures/bin:$PATH" FAKE_GH_LOG="$fixtures/log" FAKE_GH_JOBS="$fixtures/jobs" \
    FAKE_GH_ISSUES="$fixtures/issues" GH_REPO=acme/aura RUN_ID=42 bash "$here/report.sh"
}

: >"$fixtures/issues"
report
grep -q '^label create security-scan .*--force' "$fixtures/log" || fail "the label is not ensured: $(cat "$fixtures/log")"
grep -q '^issue create .*--label security-scan' "$fixtures/log" || fail "no issue was opened: $(cat "$fixtures/log")"
grep -q 'Scan postgres' "$fixtures/log" || fail "the body does not name the failed job"
if grep -q '^issue comment' "$fixtures/log"; then fail "it commented with no issue open"; fi

echo 17 >"$fixtures/issues"
report
grep -q '^issue comment 17 ' "$fixtures/log" || fail "it did not comment on the open issue: $(cat "$fixtures/log")"
if grep -q '^issue create' "$fixtures/log"; then fail "it opened a second issue"; fi
grep -q 'actions/runs/42' "$fixtures/log" || fail "the comment does not link the run"

echo "security-scan report test: pass"
