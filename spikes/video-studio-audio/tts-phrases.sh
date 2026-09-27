#!/usr/bin/env bash
# Asks the lab VM's own TTS for the three phrases the speech fixtures are built from. Signs in
# with the operator's cockpit account from .env.google (never printed) through the repo's
# Authula helpers. Output: out/phrase-{1,2,3}.bin, exactly the bytes /api/tts answered.
set -euo pipefail
cd "$(dirname "$0")"
export BASE=https://192.168.101.158
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
printf 'insecure\n' > "$WORK/.curlrc"
export CURL_HOME="$WORK"
envval() { grep "^$1=" ../../.env.google | head -1 | cut -d= -f2- | sed -e 's/^["'\'']//' -e 's/["'\'']$//' | tr -d '\r'; }
# shellcheck source=../../scripts/musr_live_run_authula_helpers.sh
source ../../scripts/musr_live_run_authula_helpers.sh
JAR="$WORK/jar"
: > "$JAR"
read -r base_path csrf_header csrf_cookie csrf_token < <(fetch_auth_config "$JAR")
sign_in "$JAR" "$(envval VM_COCKPIT_EMAIL)" "$(envval VM_COCKPIT_PASSWORD)" \
  "$base_path" "$csrf_header" "$csrf_cookie" "$csrf_token"
COOKIE="$(cookie_header_from_jar "$JAR")"
caps="$(curl -fsS "$BASE/api/voice/capabilities" -H "Cookie: $COOKIE")"
echo "capabilities: $caps"
grep -q '"tts":true' <<<"$caps" || { echo "the VM has no TTS configured: stop and ask the operator" >&2; exit 1; }
mkdir -p out
phrases=(
  "The river runs past the old mill every morning."
  "Nobody expected the concert to start so early."
  "Please keep the second box beside the window."
)
for i in 0 1 2; do
  curl -fsS -X POST "$BASE/api/tts" -H "Cookie: $COOKIE" -H "Origin: $BASE" \
    -H "Content-Type: application/json" -H "Idempotency-Key: $(cat /proc/sys/kernel/random/uuid)" \
    -H "$csrf_header: $csrf_token" \
    -d "$(printf '{"text":"%s"}' "${phrases[$i]}")" -o "out/phrase-$((i + 1)).bin"
  echo "phrase $((i + 1)): $(wc -c < "out/phrase-$((i + 1)).bin") bytes, $(file -b "out/phrase-$((i + 1)).bin")"
done
