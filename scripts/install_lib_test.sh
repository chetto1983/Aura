#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture_root="$(mktemp -d)"
trap 'rm -rf "$fixture_root"' EXIT

# Sourcing must define the functions and do nothing else. If the guard is missing the
# source runs the whole installer, which needs root and Docker and would hang CI.
# shellcheck source=/dev/null
source "$repo_root/scripts/install.sh"

declare -F download_file >/dev/null || { echo "FAIL: download_file undefined after source" >&2; exit 1; }
declare -F ensure_env_default >/dev/null || { echo "FAIL: ensure_env_default undefined after source" >&2; exit 1; }
declare -F set_env_value >/dev/null || { echo "FAIL: set_env_value undefined after source" >&2; exit 1; }

# A sourcing script is not always argument-free: the argument loop in install.sh must not
# treat the SOURCING script's arguments as its own, because a stray --help would call
# `exit 0` and a sourced exit is the CALLER's exit -- this process would end early and
# green, having asserted nothing above.
helper="$fixture_root/sourcing_with_args.sh"
cat > "$helper" <<'HELPER'
#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=/dev/null
source "$1"
# Reaching here at all is the assertion: install.sh must not have consumed our arguments
# nor exited on our behalf.
printf 'survived with %d args\n' "$#"
HELPER
out="$(bash "$helper" "$repo_root/scripts/install.sh" --help --appliance)"
[ "$out" = "survived with 3 args" ] || { echo "FAIL: sourcing install.sh with arguments did not survive: $out" >&2; exit 1; }

echo "ok: install.sh sources cleanly and defines its functions"
