#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
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
while read -r rel; do
  if ! cmp -s "$repo_root/$rel" "$extract/$rel"; then
    echo "FAIL: payload differs for $rel" >&2
    exit 1
  fi
done < <(grep '^download_file ' "$repo_root/scripts/install.sh" | awk '{print $2}')

echo "ok: the artifact self-checks and its payload round-trips"
