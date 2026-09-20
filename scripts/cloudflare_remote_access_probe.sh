#!/usr/bin/env bash
# Read-only contract probe. Pipe only a structural allowlist to stdout; never raw API data.
set +x
set -euo pipefail

if [[ -z "${CLOUDFLARE_API_TOKEN:-}" || -z "${CLOUDFLARE_TEST_ACCOUNT_ID:-}" ]]; then
  echo 'Cloudflare probe requires token and test account.' >&2
  exit 2
fi
if [[ ! "$CLOUDFLARE_TEST_ACCOUNT_ID" =~ ^[a-zA-Z0-9_-]+$ || "$CLOUDFLARE_API_TOKEN" == *$'\n'* || "$CLOUDFLARE_API_TOKEN" == *$'\r'* ]]; then
  echo 'Cloudflare probe input invalid.' >&2
  exit 2
fi

probe() {
  local label="$1" path="$2" raw summary curl_rc=0
  # Header is read on stdin so the API credential never enters curl argv.
  raw=$(printf 'Authorization: Bearer %s\n' "$CLOUDFLARE_API_TOKEN" |
      curl --silent --fail-with-body --max-time 15 --max-filesize 2097152 \
        --header @- --header 'Accept: application/json' \
        "https://api.cloudflare.com/client/v4${path}" 2>/dev/null) || curl_rc=$?
  if ! summary=$(printf '%s' "$raw" | jq -ce --arg endpoint "$label" '
      if type != "object" or (.success | type) != "boolean"
      then error("Cloudflare envelope rejected")
      else {
        endpoint: $endpoint,
        success: .success,
        error_codes: [.errors[]? | .code | select(type == "number")],
        result_type: (.result | type),
        result_count: (if (.result | type) == "array" then (.result | length) else null end),
        page: (if (.result_info.page | type) == "number" then .result_info.page else null end),
        per_page: (if (.result_info.per_page | type) == "number" then .result_info.per_page else null end),
        total_count: (if (.result_info.total_count | type) == "number" then .result_info.total_count else null end)
      } | if .success and (.error_codes | length) == 0 then .
          else ., ("" | halt_error(1)) end end' 2>/dev/null); then
    if [[ -n "$summary" ]]; then printf '%s\n' "$summary"; fi
    echo "${label}: invalid response (response redacted)." >&2
    return 1
  fi
  printf '%s\n' "$summary"
  if [[ "$curl_rc" -ne 0 ]]; then return 1; fi
}

probe token /user/tokens/verify
probe accounts '/accounts?page=1&per_page=50'
probe zones "/zones?account.id=${CLOUDFLARE_TEST_ACCOUNT_ID}&page=1&per_page=50"
probe identity_providers "/accounts/${CLOUDFLARE_TEST_ACCOUNT_ID}/access/identity_providers?page=1&per_page=50"
probe posture "/accounts/${CLOUDFLARE_TEST_ACCOUNT_ID}/devices/posture"
