#!/usr/bin/env bash
# One contract for fresh packages, reinstalls and unattended appliance updates.
set -euo pipefail

env_file="${1:?usage: appliance_posture.sh ENV_FILE}"
[[ -f "$env_file" ]] || { echo 'FAIL: appliance configuration is missing' >&2; exit 1; }
temporary="$(mktemp "${env_file}.posture.XXXXXX")"
trap 'rm -f "$temporary"' EXIT

# Compose takes the last occurrence. Remove all duplicates of the two owned keys,
# and preserve every other line without interpreting or logging secret values.
awk '
  { lines[NR] = $0 }
  /^AURA_PROFILE=/ {
    profile = substr($0, index($0, "=") + 1)
    gsub(/^[[:space:]\047\042]+|[[:space:]\047\042]+$/, "", profile)
  }
  END {
    if (profile == "" || profile == "dev" || profile == "local_trusted")
      profile = "single_user_hardened"
    if (profile != "single_user_hardened" && profile != "server_production") {
      print "FAIL: unknown AURA_PROFILE in appliance configuration" > "/dev/stderr"
      exit 2
    }
    for (i = 1; i <= NR; i++)
      if (lines[i] !~ /^(AURA_PROFILE|AURA_MUSR_ISOLATION)=/) print lines[i]
    print "AURA_PROFILE=" profile
    print "AURA_MUSR_ISOLATION=true"
  }
' "$env_file" > "$temporary"

if ! cmp -s "$env_file" "$temporary"; then
  chmod 600 "$temporary"
  mv -- "$temporary" "$env_file"
  echo 'appliance: strict runtime profile and identity isolation configured.'
fi
