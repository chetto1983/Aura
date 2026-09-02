#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/payload_files.sh
source "$repo_root/scripts/payload_files.sh"
# NO SKIP-AS-GREEN: a missing makeself in CI must redden the job, not pass it silently.
# Locally it still skips, so a developer without the tool is not blocked.
if ! command -v makeself >/dev/null 2>&1; then
  if [ -n "${CI:-}" ]; then
    echo "FAIL: makeself is required in CI (apt-get install -y makeself)" >&2
    exit 1
  fi
  echo "SKIP: makeself is not installed" >&2
  exit 0
fi

fixture_root="$(mktemp -d)"
trap 'rm -rf "$fixture_root"' EXIT
artifact="$fixture_root/install-appliance.run"

bash "$repo_root/scripts/build_installer.sh" "$artifact"
if [ ! -x "$artifact" ]; then
  echo "FAIL: artifact is not executable" >&2
  exit 1
fi

# --check verifies the embedded payload checksum only -- it does not cover the archive's
# shell header (the part that runs as root), so a pass here means the payload survived
# transport intact, not that the archive is authenticated.
if ! "$artifact" --check >/dev/null; then
  echo "FAIL: artifact failed its own payload checksum check" >&2
  exit 1
fi

# The payload must round-trip byte-for-byte. A silently truncated or re-encoded file is
# the failure this test exists to catch.
extract="$fixture_root/extracted"
"$artifact" --noexec --keep --target "$extract" >/dev/null
payload_rels="$(payload_files "$repo_root")" || exit 1

# The expected list is derived with the same expression as the builder, so an empty
# derivation would make both sides empty and every per-file assertion below would pass
# vacuously. The manifest's line count is the one expectation that does not come from
# that expression. `|| true` on both: grep -c exits 1 on a zero count, which is exactly the
# case this check exists to report -- without it, `set -e` would kill the script right here
# with no FAIL message at all, the empty-payload case reporting nothing rather than reporting
# itself.
expected_count="$(grep -c . "$repo_root/scripts/payload_manifest.txt" || true)"
derived_count="$(printf '%s\n' "$payload_rels" | grep -c . || true)"
if [ "$derived_count" != "$expected_count" ]; then
  echo "FAIL: derived $derived_count payload files but the manifest names $expected_count" >&2
  exit 1
fi

while read -r rel; do
  if [ -z "$rel" ]; then continue; fi
  if ! cmp -s "$repo_root/$rel" "$extract/$rel"; then
    echo "FAIL: payload differs for $rel" >&2
    exit 1
  fi
done <<< "$payload_rels"

# makeself's header parses argv for its own flags before exec'ing startup.sh, so an
# installer flag only survives after '--'. Both directions are asserted: if a future
# makeself made the plain form work, the first assertion fails and this comment gets
# revisited deliberately rather than by accident.
plain_err="$fixture_root/plain.err"
if "$artifact" --config /nonexistent/aura.conf >/dev/null 2>"$plain_err"; then
  echo "FAIL: the plain flag form unexpectedly succeeded" >&2
  exit 1
fi
grep -q 'Unrecognized flag' "$plain_err" \
  || { echo "FAIL: the plain form failed for an unexpected reason: $(cat "$plain_err")" >&2; exit 1; }

dashdash_err="$fixture_root/dashdash.err"
if "$artifact" -- --config /nonexistent/aura.conf >/dev/null 2>"$dashdash_err"; then
  echo "FAIL: a nonexistent config should have been refused by install.sh" >&2
  exit 1
fi
grep -q 'config not found' "$dashdash_err" \
  || { echo "FAIL: '--' did not carry the flag through to install.sh: $(cat "$dashdash_err")" >&2; exit 1; }

echo "ok: the artifact self-checks, its payload round-trips, and '--' gates every installer flag"
