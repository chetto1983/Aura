#!/usr/bin/env bash
# scripts/musr_live_run_authula_helpers.sh — sourced by scripts/musr_live_run.sh (`. scripts/
# musr_live_run_authula_helpers.sh`, not executed standalone). Split out to keep the
# orchestrator under CLAUDE.md's 600-LOC ceiling: the Authula CSRF/cookie-jar/sign-in shapes
# ported from scripts/agui_smoke.sh, plus the RFC 6238 TOTP code generator both identities'
# verify/enrollment steps need. Requires the caller to have set BASE (the daemon's base URL)
# and WORK (a scratch directory) before sourcing.

cookie_header_from_jar() {
  awk '
    /^#HttpOnly_/ { sub(/^#HttpOnly_/, "", $0) }
    /^#/ || NF < 7 { next }
    { printf "%s%s=%s", sep, $6, $7; sep="; " }
  ' "$1"
}
cookie_value_from_jar() {
  # $1=jar $2=cookie name — extracts ONE named cookie's value, for the totp_pending-only send.
  awk -v want="$2" '
    /^#HttpOnly_/ { sub(/^#HttpOnly_/, "", $0) }
    /^#/ || NF < 7 { next }
    $6 == want { print $7 }
  ' "$1"
}

cat >"${WORK}/totp_compute.py" <<'PYEOF'
# RFC 6238 TOTP, 6 digits / 30s period (plugin defaults, 01-AUTHULA-TOTP-CONTRACT.md §1: Aura's
# wiring does not override Digits/PeriodSeconds). stdlib only. Usage: totp_compute.py <secret>
import base64, hashlib, hmac, struct, sys, time

secret = sys.argv[1]
pad = secret + "=" * ((8 - len(secret) % 8) % 8)
key = base64.b32decode(pad.upper())
counter = int(time.time() // 30)
msg = struct.pack(">Q", counter)
digest = hmac.new(key, msg, hashlib.sha1).digest()
offset = digest[-1] & 0x0F
code = (struct.unpack(">I", digest[offset:offset + 4])[0] & 0x7FFFFFFF) % 1_000_000
print(f"{code:06d}")
PYEOF

json_body() {
  # $1=key $2=value -> {"<key>": "<value>", "trust_device": false} — the one shape every
  # /totp/verify call the harness makes needs (both the verify-only leg and enrollment).
  python3 -c 'import json,sys; print(json.dumps({sys.argv[1]: sys.argv[2], "trust_device": False}, separators=(",", ":")))' "$1" "$2"
}

fetch_auth_config() {
  # $1=jar -> prints "base_path csrf_header csrf_cookie csrf_token" on stdout
  #
  # MEASURED (2026-09-08): `-c <jar>` WITHOUT `-b <jar>` does not merge — curl's cookie
  # engine starts EMPTY for that invocation and `-c` OVERWRITES the file with only what THIS
  # response set, discarding every cookie a prior call had written. A live dry run hit this
  # directly: a later re-fetch of /api/auth/config (its own `-c`-only call) silently erased
  # the session cookie sign-in had just set, and the next request's Cookie header carried
  # only the CSRF cookie — RequireActor saw reqCtx.Actor == nil and returned 401
  # "unauthorized", which read exactly like an auth failure but was a jar-merge bug. Every
  # call in this file therefore uses `-b <jar> -c <jar>` together: read-then-write, so state
  # accumulates across the whole login sequence the way a real browser's cookie jar would.
  local jar="$1" cfg
  cfg="$(curl -fsS -b "${jar}" -c "${jar}" "${BASE}/api/auth/config" 2>&1)" || {
    echo "FAIL: GET /api/auth/config failed" >&2
    printf '%s\n' "${cfg}" >&2
    exit 1
  }
  AUTH_CFG="${cfg}" python3 - <<'PY'
import json, os
cfg = json.loads(os.environ["AUTH_CFG"])
base = cfg.get("auth_base_path") or cfg.get("authBasePath") or "/auth"
header = cfg.get("csrf_header_name") or cfg.get("csrfHeaderName") or "X-AUTHULA-CSRF-TOKEN"
cookie = cfg.get("csrf_cookie_name") or cfg.get("csrfCookieName") or "__Host-authula_csrf_token"
token = cfg.get("csrf_token") or cfg.get("csrfToken") or ""
if cfg.get("provider") != "authula" or not token:
    raise SystemExit(f"unexpected auth config provider={cfg.get('provider')!r} csrf={bool(token)}")
print(base, header, cookie, token)
PY
}

sign_in() {
  # $1=jar $2=email $3=password $4=base_path $5=csrf_header $6=csrf_cookie $7=csrf_token
  # -> writes the sign-in response body path to stdout; caller checks totp_redirect.
  local jar="$1" email="$2" password="$3" base_path="$4" csrf_header="$5" csrf_cookie="$6" csrf_token="$7"
  local body="${WORK}/signin-$$-${RANDOM}.json"
  local payload
  payload="$(AUTH_EMAIL="${email}" AUTH_PASSWORD="${password}" python3 - <<'PY'
import json, os
print(json.dumps({"email": os.environ["AUTH_EMAIL"], "password": os.environ["AUTH_PASSWORD"]}, separators=(",", ":")))
PY
)"
  local code
  code="$(curl -sS -o "${body}" -w '%{http_code}' \
    -X POST "${BASE}${base_path}/email-password/sign-in" \
    -H 'Content-Type: application/json' \
    -H "${csrf_header}: ${csrf_token}" \
    -H "Origin: ${BASE}" \
    -H "Cookie: ${csrf_cookie}=${csrf_token}" \
    -b "${jar}" -c "${jar}" \
    -d "${payload}")" || {
    echo "FAIL: POST ${base_path}/email-password/sign-in curl failed" >&2
    exit 1
  }
  if [[ "${code}" != "200" ]]; then
    echo "FAIL: sign-in for ${email} returned HTTP ${code}" >&2
    cat "${body}" >&2
    exit 1
  fi
  echo "${body}"
}
