#!/usr/bin/env bash
set -euo pipefail
probe_script="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/cloudflare_remote_access_probe.sh"
test_dir="$(mktemp -d)"
trap 'rm -f "$test_dir/requests" "$test_dir/jq_calls" "$test_dir/out" "$test_dir/err"; rmdir "$test_dir"' EXIT
export TEST_DIR="$test_dir"
export REAL_JQ="$(command -v jq)"
export CLOUDFLARE_API_TOKEN='fixture-secret-token'
export CLOUDFLARE_TEST_ACCOUNT_ID='fixture-account'

curl() {
  local header url="${!#}"
  IFS= read -r header
  [[ "$header" == 'Authorization: Bearer fixture-secret-token' ]] || return 90
  [[ " $* " == *' --header @- '* ]] || return 91
  [[ " $* " == *' --header Accept: application/json '* ]] || return 92
  [[ " $* " != *fixture-secret-token* ]] || return 93
  printf '%s\n' "$url" >> "$TEST_DIR/requests"
  if [[ "${PROBE_CASE:-}" == transport ]]; then
    printf 'fixture-secret-token fixture-account admin@example.com\n' >&2
    return 22
  fi
  if [[ "${PROBE_CASE:-}" == denied || "${PROBE_CASE:-}" == http_denied ]]; then
    printf '%s\n' '{"success":false,"errors":[{"code":10000,"message":"fixture-secret-token admin@example.com"}],"result":null}'
    if [[ "${PROBE_CASE:-}" == http_denied ]]; then return 22; fi
    return 0
  fi
  printf '%s\n' '{"success":true,"errors":[],"result":[{"id":"fixture-account","email":"admin@example.com","name_servers":["private.ns.example.com"],"token":"fixture-secret-token"}],"result_info":{"page":1,"per_page":50,"total_count":1}}'
}
jq() {
  printf 'called\n' >> "$TEST_DIR/jq_calls"
  "$REAL_JQ" "$@"
}
export -f curl jq

assert_redacted() {
  if grep -Eq 'fixture-secret-token|fixture-account|admin@example.com|private.ns.example.com' "$test_dir/out" "$test_dir/err"; then
    echo 'FAIL: probe leaked response or credential' >&2; exit 1
  fi
}

bash "$probe_script" > "$test_dir/out" 2> "$test_dir/err"
[[ $(wc -l < "$test_dir/requests") -eq 5 ]]
[[ $(wc -l < "$test_dir/jq_calls") -eq 5 ]]
expected=(
  'https://api.cloudflare.com/client/v4/user/tokens/verify'
  'https://api.cloudflare.com/client/v4/accounts?page=1&per_page=50'
  'https://api.cloudflare.com/client/v4/zones?account.id=fixture-account&page=1&per_page=50'
  'https://api.cloudflare.com/client/v4/accounts/fixture-account/access/identity_providers?page=1&per_page=50'
  'https://api.cloudflare.com/client/v4/accounts/fixture-account/devices/posture'
)
mapfile -t actual < "$test_dir/requests"
for i in "${!expected[@]}"; do [[ "${actual[$i]}" == "${expected[$i]}" ]]; done
"$REAL_JQ" -se 'length == 5 and all(.[]; .success == true and .result_type == "array" and .result_count == 1 and .page == 1)' "$test_dir/out" >/dev/null
assert_redacted
for case_name in denied http_denied transport; do
  rc=0
  PROBE_CASE="$case_name" bash "$probe_script" > "$test_dir/out" 2> "$test_dir/err" || rc=$?
  [[ "$rc" -eq 1 ]]
  assert_redacted
  if [[ "$case_name" != transport ]]; then
    "$REAL_JQ" -e '.success == false and .error_codes == [10000]' "$test_dir/out" >/dev/null
  fi
done
rc=0
CLOUDFLARE_API_TOKEN='' bash "$probe_script" > "$test_dir/out" 2> "$test_dir/err" || rc=$?
[[ "$rc" -eq 2 ]]
assert_redacted
echo 'PASS: probe URLs, bearer stdin, exit status, jq redaction, and failure paths'
