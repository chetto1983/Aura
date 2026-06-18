#!/usr/bin/env bash
set -euo pipefail
if (set +H) 2>/dev/null; then
  set +H
fi

cd "$(git rev-parse --show-toplevel)"

usage() {
  echo "usage: scripts/asset_smoke.sh <file>" >&2
  exit 2
}

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 2
  fi
}

if [ "$#" -ne 1 ]; then
  usage
fi

need curl
need jq
need mktemp
need wc
need basename

base="${AURA_BASE_URL:-http://127.0.0.1:9080}"
base="${base%/}"
file="$1"
if [ ! -f "$file" ]; then
  echo "asset smoke file not found: $file" >&2
  exit 2
fi

mime="${AURA_ASSET_SMOKE_MIME:-application/pdf}"
modality="${AURA_ASSET_SMOKE_MODALITY:-document}"
thread_id="${AURA_ASSET_SMOKE_THREAD_ID:-}"
name="$(basename "$file")"
size="$(wc -c < "$file" | tr -d '[:space:]')"

auth_header=""
cookie_jar=""
upload_headers_cfg="$(mktemp)"
cleanup() {
  rm -f "$upload_headers_cfg"
}
trap cleanup EXIT

mint_passphrase_cookie() {
  need python3
  python3 - "$1" "${AURA_ASSET_SMOKE_IDENTITY_ID:-00000000-0000-0000-0000-000000000001}" <<'PY'
import base64
import hashlib
import hmac
import sys
import time

secret = sys.argv[1].encode()
identity_id = sys.argv[2]
payload = f"{identity_id}|{int(time.time())}".encode()
key = hashlib.sha256(secret).digest()
sig = hmac.new(key, payload, hashlib.sha256).digest()

def enc(raw: bytes) -> str:
    return base64.urlsafe_b64encode(raw).decode().rstrip("=")

print(f"{enc(payload)}.{enc(sig)}")
PY
}

if [ -n "${AURA_ASSET_SMOKE_COOKIE_JAR:-}" ]; then
  cookie_jar="$AURA_ASSET_SMOKE_COOKIE_JAR"
elif [ -n "${AURA_ASSET_SMOKE_COOKIE:-}" ]; then
  auth_header="Cookie: $AURA_ASSET_SMOKE_COOKIE"
elif [ -n "${AURA_WEB_AUTH_SECRET:-}" ]; then
  cookie="$(mint_passphrase_cookie "$AURA_WEB_AUTH_SECRET")"
  auth_header="Cookie: __Host-aura_session=$cookie"
fi

curl_api() {
  if [ -n "$cookie_jar" ]; then
    curl -fsS -b "$cookie_jar" "$@"
    return
  fi
  if [ -n "$auth_header" ]; then
    curl -fsS -H "$auth_header" "$@"
    return
  fi
  curl -fsS "$@"
}

payload="$(
  jq -n \
    --arg thread_id "$thread_id" \
    --arg file_name "$name" \
    --arg mime_type "$mime" \
    --argjson size_bytes "$size" \
    --arg modality_hint "$modality" \
    '{
      thread_id: $thread_id,
      file_name: $file_name,
      mime_type: $mime_type,
      size_bytes: $size_bytes,
      modality_hint: $modality_hint
    }'
)"

presign="$(
  curl_api \
    -X POST "$base/api/assets/presign" \
    -H 'Accept: application/json' \
    -H 'Content-Type: application/json' \
    -d "$payload"
)"

upload_url="$(printf '%s' "$presign" | jq -er '.upload.upload_url')"
method="$(printf '%s' "$presign" | jq -r '.upload.method // "PUT"')"
asset_id="$(printf '%s' "$presign" | jq -er '.asset.id')"

printf '%s' "$presign" |
  jq -r '.upload.required_headers // {} | to_entries[] | "header = \"" + .key + ": " + .value + "\""' \
    >"$upload_headers_cfg"

curl -fsS -K "$upload_headers_cfg" -X "$method" "$upload_url" --data-binary "@$file" >/dev/null

curl_api \
  -X POST "$base/api/assets/$asset_id/finalize" \
  -H 'Accept: application/json' \
  >/dev/null

asset=""
attempts="${AURA_ASSET_SMOKE_ATTEMPTS:-60}"
interval="${AURA_ASSET_SMOKE_POLL_SEC:-1}"

i=1
while [ "$i" -le "$attempts" ]; do
  asset="$(curl_api "$base/api/assets/$asset_id" -H 'Accept: application/json')"
  status="$(printf '%s' "$asset" | jq -r '.status')"
  case "$status" in
    searchable|complete)
      printf '%s\n' "$asset"
      exit 0
      ;;
    failed|refused)
      printf '%s\n' "$asset"
      exit 1
      ;;
  esac
  sleep "$interval"
  i=$((i + 1))
done

echo "asset did not become searchable in time: $asset_id" >&2
if [ -n "$asset" ]; then
  printf '%s\n' "$asset" >&2
fi
exit 1
