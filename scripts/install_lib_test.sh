#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Sourcing must define the functions and do nothing else. If the guard is missing the
# source runs the whole installer, which needs root and Docker and would hang CI.
# shellcheck source=/dev/null
source "$repo_root/scripts/install.sh"

declare -F download_file >/dev/null || { echo "FAIL: download_file undefined after source" >&2; exit 1; }
declare -F ensure_env_default >/dev/null || { echo "FAIL: ensure_env_default undefined after source" >&2; exit 1; }
declare -F set_env_value >/dev/null || { echo "FAIL: set_env_value undefined after source" >&2; exit 1; }

echo "ok: install.sh sources cleanly and defines its functions"
