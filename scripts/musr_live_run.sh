#!/usr/bin/env bash
# scripts/musr_live_run.sh — Phase 01 Plan 06 (D-16/D-17/D-18): the committed, re-runnable
# harness that drives the two-identity live acceptance run this phase closes on.
#
# It authenticates as EACH identity and drives two concurrent `POST /agent/run` conversations
# through the real AG-UI gateway against a live `aura serve`, capturing both transcripts and
# their timing into ONE fixed directory the blocking check (scripts/musr_live_run_assert.go)
# reads by the same name. Ported from scripts/agui_smoke.sh's process-lifecycle and Authula
# authentication shapes (build, background `serve`, poll /healthz with no fixed sleep, CSRF
# login, cookie-header construction) — read that file's full 448 lines before touching this
# one; this script does NOT re-derive those shapes, it reuses them.
#
# Unlike scripts/musr_e2e.sh (01-04), THIS script runs against the REAL deployment: it must
# tear down only the `aura serve` process it starts, never the compose stack, and it must not
# touch the operator's own documents or plant facts in the operator's memory graph. Identity B
# is provisioned fresh, here, through `aura identity create` — the documented path (D-07), not
# a raw insert.
#
# Identity B's first login is FORCED per D-15 (internal/agui/onboarding_provision.go:200-210,
# EnforceFirstLogin): the admin-set password is single-use and TOTP enrollment is mandatory.
# MEASURED THIS SESSION (2026-09-08), beyond what 01-AUTHULA-TOTP-CONTRACT.md covers — read
# that document's own §6 first, then this: the metadata markers EnforceFirstLogin sets
# (`aura_must_change_password`, `aura_totp_enrollment_required`) have NO consumer anywhere in
# this codebase (`grep -rn aura_totp_enrollment_required --include=*.go .` returns only the
# three lines that define/set it in cmd/aura/serve_onboarding.go); Authula's own sign-in
# handler (email-password/handlers/sign_in_handler.go) never reads them; and the TOTP plugin's
# login-intercept hook (plugins/totp/hooks.go:57-61) only pauses login for a user who ALREADY
# has TOTP enabled — for a never-enrolled user it returns nil and login completes normally.
# So: B's first login with the admin-set password succeeds directly, with no forced redirect
# of any kind. This harness therefore does not wait for a server-forced redirect (there is
# none to wait for) — it PROACTIVELY drives the two things D-15 requires of a first login:
#
#   - TOTP enrollment: POST /totp/enable (real, on B's freshly-authenticated session) then
#     POST /totp/verify with a code computed from the returned otpauth:// secret — the exact
#     headlessly-automatable mechanism 01-AUTHULA-TOTP-CONTRACT.md measured (§5 verdict).
#   - Password change: NO headless, plan-compliant path exists in this build. Measured three
#     independent ways: (1) Authula's own /email-password/change-password requires a token
#     from /email-password/request-password-reset, which is only EVER delivered by email —
#     Aura wires zero mailer plugin (internal/webauth/authula.go buildPlugins: "no mailer
#     plugin is wired... operator is provisioned out-of-band") so that token is generated,
#     stored, and never surfaces anywhere reachable; (2) Aura's OWN security-question reset
#     (internal/agui/password_reset.go) requires RecoveryRecord.canRecover(), which requires
#     TelegramUserID != 0 — a COMPLETED Telegram link, which needs a human to open the deep
#     link the provisioning saga mints and message the bot, not something this harness can do
#     headlessly without either a manual step (E2E-02 forbids one) or a second Telegram
#     message (D-08/this plan's own prohibitions forbid that — the bot is used exactly once,
#     for the deep-link mint); (3) no admin plugin is wired at all (buildPlugins' own comment:
#     access-control/oauth2/jwt/bearer "deliberately OMITTED"). This is recorded, not silently
#     worked around: docs/runbooks/two-identity-live-run.md and 01-LIVE-RUN-EVIDENCE.md both
#     state it under "what this run does not show." The plan's own escape valve for exactly
#     this shape of gap ("Fail with a message naming that field... rather than looping on a
#     verify that cannot succeed") is applied here to the password-change leg specifically.
#
# Invoke (WSL — the primary dev environment; needs `go`, `python3`, a writable /tmp with a
# real PTY, and Docker reachable):
#   wsl bash -lc 'cd /mnt/d/Repo/Aura && set -a; source <(awk "{ sub(/\r\$/, \"\"); print }" .env); set +a; bash scripts/musr_live_run.sh'
#
# Debug env vars (not part of the acceptance contract):
#   MUSR_SKIP_CONVERSATIONS=1  stop after document upload + the ingest wait, print readiness,
#                              exit 0 without spending a model turn — the gpu_budget dry-run
#                              path: everything up to here is free to re-run while debugging.
#   MUSR_DOC_INGEST_WAIT_SEC   override the async multi-tenant ingest wait (default 90).
#   MUSR_KEEP_IDENTITY_B=1     skip the EXIT-trap deprovision and leave identity B in the
#                              deployment — for inspecting a failed run's state before it is
#                              torn down. Every run without it purges her, on failure too.
set -euo pipefail
if (set +H) 2>/dev/null; then
  set +H # disable history expansion for any '!'-containing password
fi

cd "$(git rev-parse --show-toplevel)"

RUN_DIR="artifacts/musr-live-run"
rm -rf "${RUN_DIR}"
mkdir -p "${RUN_DIR}"

NULL_OUT="/dev/null"
WINDOWS_BASH=0
case "$(uname -s 2>/dev/null || true)" in
Windows_NT | MINGW*_NT* | MSYS*_NT* | CYGWIN*_NT*)
  NULL_OUT="NUL"
  WINDOWS_BASH=1
  ;;
esac

# ---- section 1: preconditions (T-06 threat register: name the first missing one, refuse) ---
# Split into scripts/musr_live_run_preconditions.sh (600-LOC ceiling) — store-aware (aura.
# settings overlay, MEASURED 2026-09-08 — see that file's header) rather than env-only, which
# a purely-env gate proved to false-negative on a correctly configured host.
. scripts/musr_live_run_preconditions.sh

# ---- section 2: build + start our own aura serve (never the compose one) --------------------
WORK="$(mktemp -d)"
BIN="${WORK}/aura"
DAEMON_PID=""
IDENTITY_B_ID=""

# deprovision_identity_b tears down the identity this run provisioned, through the SAME
# saga that built her (`aura identity purge`, cmd/aura/identity_deprovision.go) — the D-27
# reverse legs in order: sandbox box, conversations, ArcadeDB database, Garage bucket+key,
# filesystem roots, identity row, Authula user. Never a SQL DELETE: that cascades the
# Postgres catalog and strands every plane outside it with no owner row left to find it by.
#
# It is called from the EXIT trap, so it runs on failure and on interrupt too — the twelve
# identities this harness accumulated on 2026-09-08 are what a run that only cleaned up on
# success leaves behind. It never fails the run: the exit status the harness reports is the
# acceptance verdict, and a teardown problem is reported, not substituted for it.
deprovision_identity_b() {
  [[ -n "${IDENTITY_B_ID}" ]] || return 0
  [[ -x "${BIN}" ]] || return 0
  if [[ "${MUSR_KEEP_IDENTITY_B:-0}" == "1" ]]; then
    echo "==> MUSR_KEEP_IDENTITY_B=1 — leaving identity B ${IDENTITY_B_ID} provisioned"
    return 0
  fi
  echo "==> deprovisioning identity B ${IDENTITY_B_ID} (aura identity purge, the D-27 saga)"
  if "${BIN}" identity purge "${IDENTITY_B_ID}" --confirm >"${RUN_DIR}/deprovision.log" 2>&1; then
    echo "==> identity B deprovisioned"
  else
    echo "WARN: deprovisioning identity B failed — see ${RUN_DIR}/deprovision.log." >&2
    echo "      The saga is resumable: re-run 'aura identity purge ${IDENTITY_B_ID} --confirm'," >&2
    echo "      or '--confirm --resume-resources' if the identity row is already gone." >&2
  fi
}

cleanup() {
  # Captured FIRST: everything below must leave the acceptance verdict untouched, and the
  # explicit exit at the end is what guarantees it rather than the last command's status.
  local status=$?
  if [[ -n "${DAEMON_PID:-}" ]] && kill -0 "${DAEMON_PID}" 2>/dev/null; then
    kill -TERM "${DAEMON_PID}" 2>/dev/null || true
    wait "${DAEMON_PID}" 2>/dev/null || true
  fi
  # After the daemon is down, so nothing is still writing to the planes being torn down, and
  # before WORK is removed, because ${BIN} lives inside it.
  deprovision_identity_b || true
  cp -f "${SERVE_LOG:-/dev/null}" "${RUN_DIR}/daemon.log" 2>/dev/null || true
  if [[ "${WINDOWS_BASH}" -eq 0 ]]; then
    rm -rf "${WORK}" 2>/dev/null || true
  fi
  exit "${status}"
}
trap cleanup EXIT

echo "==> building aura"
go build -o "${BIN}" ./cmd/aura

BIND="$(python3 - <<'PY'
import socket
with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
    s.bind(("127.0.0.1", 0))
    print(f"127.0.0.1:{s.getsockname()[1]}")
PY
)"
export AURA_AGUI_BIND="${BIND}"
BASE="http://${BIND}"

SERVE_LOG="${WORK}/serve.log"
echo "==> starting our own aura serve (bind ${BIND}) — the compose 'aura' service is untouched"

# MEASURED (2026-09-08, this host): the settings-store overlay (internal/settings/settings.go
# OverlayEnv, invoked from cmd/aura/chat_boot.go's resolveConfigAndPoolWithSettings) applies
# aura.settings rows onto the process env UNCONDITIONALLY at boot (os.Setenv, overwriting
# anything already set), then reloads config -- so AURA_LLM_BASE_URL ends up being whatever the
# store says regardless of what this shell exports first. On this host the store's value is
# `http://host.docker.internal:11434/v1` (Ollama on the Windows host, reached correctly by the
# REAL `aura` compose container via Docker Desktop's own host-gateway proxying -- confirmed:
# `docker exec aura curl host.docker.internal:11434/api/version` succeeds). A bare process
# launched from WSL is NOT a Docker Desktop container, so it does not get that proxying: WSL's
# own `host.docker.internal` DNS entry resolves to a different address (this host's LAN IP,
# 2026-09-08) that Ollama — bound to Windows' loopback only — never accepts a connection on.
# What DOES work, measured the same session: the WSL default-route gateway IP (`ip route show`)
# reaches Windows' loopback-bound services, including Ollama, because Docker Desktop's WSL2
# host-forwarding attaches there. Rather than edit the SHARED /etc/hosts (this WSL instance may
# be running other sessions' work concurrently — CLAUDE.md/this plan: never touch shared
# resources), this remaps host.docker.internal -> that gateway IP in a throwaway, unprivileged
# user+mount namespace scoped to ONLY the aura serve child (`unshare --user --map-root-user
# --mount`, no elevation, verified standalone this session: /etc/hosts outside the namespace is
# provably untouched). `unshare` execve()s straight through (no --pid unshare, so no --fork is
# needed) into the final `exec "${BIN}" serve`, so $! below is the real daemon's PID the whole
# time -- `kill -TERM` still targets it directly, no orphaned wrapper process.
USE_HOSTS_NS=0
if command -v unshare >/dev/null 2>&1 && [[ "${WINDOWS_BASH}" -eq 0 ]]; then
  GATEWAY_IP="$(ip route show 2>/dev/null | awk '/^default/ {print $3; exit}')"
  if [[ -n "${GATEWAY_IP}" ]]; then
    NS_HOSTS="${WORK}/hosts-with-gateway"
    cat /etc/hosts >"${NS_HOSTS}" 2>/dev/null || true
    echo "${GATEWAY_IP} host.docker.internal" >>"${NS_HOSTS}"
    if unshare --user --map-root-user --mount --propagation private true 2>/dev/null; then
      USE_HOSTS_NS=1
    fi
  fi
fi
if [[ "${USE_HOSTS_NS}" -eq 1 ]]; then
  echo "==> host.docker.internal -> ${GATEWAY_IP} (scoped to this process only, via an unprivileged mount namespace — /etc/hosts elsewhere is untouched)"
  unshare --user --map-root-user --mount --propagation private bash -c \
    "mount --bind '${NS_HOSTS}' /etc/hosts && exec '${BIN}' serve" >"${SERVE_LOG}" 2>&1 &
else
  echo "==> unshare unavailable or no default route found — starting aura serve without the host.docker.internal remap (fine on a host where the LLM backend does not need it)"
  "${BIN}" serve >"${SERVE_LOG}" 2>&1 &
fi
DAEMON_PID=$!
if [[ "${WINDOWS_BASH}" -eq 1 ]]; then
  disown "${DAEMON_PID}" 2>/dev/null || true
fi

READY=0
for _ in $(seq 1 120); do
  if ! kill -0 "${DAEMON_PID}" 2>/dev/null; then
    echo "FAIL: aura serve exited during boot — log:" >&2
    cat "${SERVE_LOG}" >&2
    if grep -qi "AURA_SANDBOX_IMAGE" "${SERVE_LOG}"; then
      echo "      (this looks like the D-03 sandbox-image preflight refusal — a precondition failure, not a run failure)" >&2
    fi
    exit 1
  fi
  if curl -fsS -o "${NULL_OUT}" "${BASE}/healthz" 2>/dev/null; then
    READY=1
    break
  fi
  sleep 0.5
done
if [[ "${READY}" -ne 1 ]]; then
  echo "FAIL: daemon did not accept connections on ${BIND} — log:" >&2
  cat "${SERVE_LOG}" >&2
  exit 1
fi
echo "==> daemon ready on ${BIND}"

# ---- shared helpers: cookie header from a curl jar, CSRF config fetch, sign-in, TOTP code ---
# Split into scripts/musr_live_run_authula_helpers.sh (600-LOC ceiling) — the Authula
# CSRF/cookie-jar/sign-in shapes ported from scripts/agui_smoke.sh, plus the RFC 6238 TOTP
# generator. Needs BASE and WORK set (both are, by this point).
. scripts/musr_live_run_authula_helpers.sh

# ============================================================================================
# section 3: provision identity B — the documented path (`aura identity create`), via a real
# PTY so its TTY-only secret prompts (readHiddenFromStdin -> golang.org/x/term.ReadPassword)
# work headlessly without ever putting the password/security-answer on argv, in env, or in a
# log (T-01-03 secret discipline — reused exactly, not relaxed).
#
# Identity B IS deprovisioned at the end of this run, from the EXIT trap, through
# `aura identity purge` — the documented reverse of the verb that created her. That verb did
# not exist when this harness was written, and the cost of the gap is measured: twelve
# musr-live-run-b-* identities accumulated in the live deployment on 2026-09-08, one per
# debug iteration, each holding an ArcadeDB database, a Garage bucket+key, a sandbox box, an
# Authula user and a filesystem root. "Left provisioned as live proof" is one identity's
# worth of argument and twelve identities' worth of debris; the transcripts under
# artifacts/musr-live-run/ and the COT dumps are the evidence, not the row.
#
# Pass MUSR_KEEP_IDENTITY_B=1 to keep her — for inspecting a failed run's state before it is
# torn down. Her seed document and memory fact are scoped to her own data, never the
# operator's, so nothing outside her own planes is touched either way.
# ============================================================================================
IDENTITY_B_EMAIL="musr-live-run-b-$(date +%s)@example.invalid"
IDENTITY_B_PASSWORD="$(python3 -c 'import secrets; print(secrets.token_urlsafe(24))')"
IDENTITY_B_ANSWER="$(python3 -c 'import secrets; print(secrets.token_urlsafe(16))')"

# MEASURED (2026-09-08): `-operator` defaults to localSeededIdentityID (serve_provisioning.
# go:41, the fresh-install seed UUID). On a live, already-onboarded deployment that row does
# not exist — the real operator is whoever holds '*' (onboarding_provision.go:481-505
# validateNoEscalation; a dry run this session hit exactly this: "missing required
# capability"). Resolve the real one — read-only, id only, never a value.
OPERATOR_IDENTITY_ID="$(docker exec aura-postgres psql -U aura -d aura -tAc \
  "select identity_id from aura.capability_grants where capability = '*' limit 1;" 2>/dev/null || true)"
OPERATOR_IDENTITY_ID="$(echo "${OPERATOR_IDENTITY_ID}" | tr -d '[:space:]')"
if [[ -z "${OPERATOR_IDENTITY_ID}" ]]; then
  echo "FAIL: could not resolve an identity holding the '*' capability to create identity B as — checked aura.capability_grants" >&2
  exit 1
fi
echo "==> creating identity B as operator ${OPERATOR_IDENTITY_ID} (resolved: holds '*')"

PTY_RUNNER="scripts/musr_live_run_ptyexpect.py"

IDENTITY_CREATE_OUT="${WORK}/identity-create.out"
EXCHANGES_FILE="${WORK}/ptyrun-exchanges"
(
  umask 077
  printf 'identity password: \x1e%s\x1esecurity answer: \x1e%s' \
    "${IDENTITY_B_PASSWORD}" "${IDENTITY_B_ANSWER}" >"${EXCHANGES_FILE}"
)
export PTYRUN_OUTPUT="${IDENTITY_CREATE_OUT}"
export PTYRUN_EXCHANGES_FILE="${EXCHANGES_FILE}"
echo "==> provisioning identity B via 'aura identity create' (real saga, real Telegram deep link — D-08's one use)"
set +e
python3 "${PTY_RUNNER}" "${BIN}" identity create \
  -email "${IDENTITY_B_EMAIL}" \
  -security-question "musr-live-run automated security question" \
  -operator "${OPERATOR_IDENTITY_ID}" \
  -capability agent.run
IDENTITY_CREATE_STATUS=$?
set -e
rm -f "${EXCHANGES_FILE}"
unset PTYRUN_EXCHANGES_FILE
if [[ "${IDENTITY_CREATE_STATUS}" -ne 0 ]]; then
  echo "FAIL: aura identity create exited ${IDENTITY_CREATE_STATUS} — output:" >&2
  cat "${IDENTITY_CREATE_OUT}" >&2
  exit 1
fi
IDENTITY_B_ID="$(grep -oE 'ok: identity [0-9a-fA-F-]+ created' "${IDENTITY_CREATE_OUT}" | awk '{print $3}')"
IDENTITY_B_DEEPLINK="$(grep -oE 'https?://t\.me/\S+' "${IDENTITY_CREATE_OUT}" | head -1)"
if [[ -z "${IDENTITY_B_ID}" ]]; then
  echo "FAIL: could not parse identity B's UUID from 'aura identity create' output:" >&2
  cat "${IDENTITY_CREATE_OUT}" >&2
  exit 1
fi
echo "==> identity B provisioned: ${IDENTITY_B_ID}"
if [[ -n "${IDENTITY_B_DEEPLINK}" ]]; then
  echo "==> Telegram deep link (D-08, minted once): ${IDENTITY_B_DEEPLINK}"
fi
{
  echo "identity_b_id=${IDENTITY_B_ID}"
  echo "identity_b_email=${IDENTITY_B_EMAIL}"
  echo "identity_b_deeplink=${IDENTITY_B_DEEPLINK}"
} >"${RUN_DIR}/identities.env"

# ============================================================================================
# section 4: authenticate identity A — bootstrap `local`, the existing agui_smoke.sh flow
# (CSRF + email/password + TOTP verify, since `local` was already enrolled before this run
# and never passes through EnforceFirstLogin — 01-01/01-07 measured this).
# ============================================================================================
JAR_A="${WORK}/cookies-a.txt"
: >"${JAR_A}"
read -r AUTH_BASE_PATH CSRF_HEADER CSRF_COOKIE CSRF_TOKEN < <(fetch_auth_config "${JAR_A}")
BODY_A="$(sign_in "${JAR_A}" "${AURA_E2E_AUTHULA_EMAIL}" "${AURA_E2E_AUTHULA_PASSWORD}" \
  "${AUTH_BASE_PATH}" "${CSRF_HEADER}" "${CSRF_COOKIE}" "${CSRF_TOKEN}")"
COOKIE_A="$(cookie_header_from_jar "${JAR_A}")"
NEED_TOTP_A="$(python3 - "${BODY_A}" <<'PY'
import json, sys
try:
    body = json.load(open(sys.argv[1], encoding="utf-8"))
except Exception:
    body = {}
print("1" if body.get("totp_redirect") is True else "0")
PY
)"
if [[ "${NEED_TOTP_A}" == "1" ]]; then
  A_CODE=""
  if [[ -n "${AURA_E2E_AUTHULA_TOTP_SECRET:-}" ]]; then
    # Preferred when available: a code computed fresh from the seed cannot go stale across
    # an unattended multi-minute run, unlike a pre-typed AURA_E2E_AUTHULA_TOTP_CODE.
    A_CODE="$(python3 "${WORK}/totp_compute.py" "${AURA_E2E_AUTHULA_TOTP_SECRET}")"
  elif [[ -n "${AURA_E2E_AUTHULA_TOTP_CODE:-}" ]]; then
    A_CODE="${AURA_E2E_AUTHULA_TOTP_CODE}"
  fi
  if [[ -z "${A_CODE}" ]]; then
    echo "FAIL: Authula requested TOTP for identity A; set AURA_E2E_AUTHULA_TOTP_SECRET (preferred) or a fresh AURA_E2E_AUTHULA_TOTP_CODE" >&2
    exit 2
  fi
  VERIFY_BODY="${WORK}/a-totp-verify.json"
  VERIFY_PAYLOAD="$(json_body code "${A_CODE}")"
  VERIFY_CODE="$(curl -sS -o "${VERIFY_BODY}" -w '%{http_code}' \
    -X POST "${BASE}${AUTH_BASE_PATH}/totp/verify" \
    -H 'Content-Type: application/json' \
    -H "${CSRF_HEADER}: ${CSRF_TOKEN}" \
    -H "Origin: ${BASE}" \
    -H "Cookie: ${COOKIE_A}" \
    -b "${JAR_A}" -c "${JAR_A}" \
    -d "${VERIFY_PAYLOAD}")"
  if [[ "${VERIFY_CODE}" != "200" ]]; then
    echo "FAIL: identity A TOTP verify returned HTTP ${VERIFY_CODE}" >&2
    cat "${VERIFY_BODY}" >&2
    exit 1
  fi
  COOKIE_A="$(cookie_header_from_jar "${JAR_A}")"
fi
if [[ "${COOKIE_A}" != *"__Host-authula_session="* ]]; then
  echo "FAIL: identity A login did not yield a session cookie" >&2
  exit 1
fi
echo "==> identity A authenticated"

# ============================================================================================
# section 5: authenticate identity B — the FORCED first-login leg this plan builds. Sign-in
# succeeds directly (measured above: no server-side redirect exists), then this harness
# PROACTIVELY drives the real TOTP enrollment (/totp/enable -> compute code -> /totp/verify,
# verify called with ONLY the totp_pending cookie so reqCtx.Actor resolves to nil, satisfying
# verify_totp_handler.go's "you're already authenticated" refusal for an actor-bearing
# session — B's original full session cookie is untouched and reused afterward).
# ============================================================================================
JAR_B="${WORK}/cookies-b.txt"
: >"${JAR_B}"
read -r B_AUTH_BASE_PATH B_CSRF_HEADER B_CSRF_COOKIE B_CSRF_TOKEN < <(fetch_auth_config "${JAR_B}")
BODY_B="$(sign_in "${JAR_B}" "${IDENTITY_B_EMAIL}" "${IDENTITY_B_PASSWORD}" \
  "${B_AUTH_BASE_PATH}" "${B_CSRF_HEADER}" "${B_CSRF_COOKIE}" "${B_CSRF_TOKEN}")"
COOKIE_B="$(cookie_header_from_jar "${JAR_B}")"
if [[ "${COOKIE_B}" != *"__Host-authula_session="* ]]; then
  echo "FAIL: identity B first login did not yield a session (expected: it is not yet TOTP-enrolled, so login completes without a redirect)" >&2
  exit 1
fi
echo "==> identity B first login OK (no forced redirect exists to wait for — measured; see header comment)"

# The jar now correctly carries BOTH the CSRF cookie (from fetch_auth_config, merged forward
# by sign_in's -b/-c pair — see musr_live_run_authula_helpers.sh) and the session cookie
# (set by sign_in). -b sends the jar's cookies automatically; B_CSRF_HEADER/B_CSRF_TOKEN from
# fetch_auth_config above stay valid (sign-in does not rotate the CSRF cookie). This call ALSO
# depends on internal/webauth/authula.go's RouteMappings fix (MEASURED 2026-09-08): without
# it, /totp/enable's RequireActor never sees an Actor at all — 401 regardless of how correct
# the cookie is — because Authula's session.auth hook is PluginID-scoped to routes whose
# metadata declares it, and nothing declared it for this route before that fix.
ENABLE_BODY="${WORK}/b-totp-enable.json"
ENABLE_CODE="$(curl -sS -o "${ENABLE_BODY}" -w '%{http_code}' \
  -X POST "${BASE}${B_AUTH_BASE_PATH}/totp/enable" \
  -H 'Content-Type: application/json' \
  -H "${B_CSRF_HEADER}: ${B_CSRF_TOKEN}" \
  -H "Origin: ${BASE}" \
  -b "${JAR_B}" -c "${JAR_B}" \
  -d '{}')"
if [[ "${ENABLE_CODE}" != "200" ]]; then
  echo "FAIL: identity B POST /totp/enable returned HTTP ${ENABLE_CODE} — the mandatory TOTP enrollment EnforceFirstLogin requires could not start" >&2
  cat "${ENABLE_BODY}" >&2
  exit 1
fi
B_TOTP_SECRET="$(python3 - "${ENABLE_BODY}" <<'PY'
import json, sys, urllib.parse
body = json.load(open(sys.argv[1], encoding="utf-8"))
uri = body.get("totpUri") or body.get("TotpURI") or body.get("totp_uri") or ""
if not uri:
    print("")
    raise SystemExit(0)
q = urllib.parse.urlsplit(uri).query
params = urllib.parse.parse_qs(q)
print((params.get("secret") or [""])[0])
PY
)"
if [[ -z "${B_TOTP_SECRET}" ]]; then
  echo "FAIL: /totp/enable's response did not carry a 'totpUri' with a 'secret' query param — the response field a headless code generator needs" >&2
  cat "${ENABLE_BODY}" >&2
  exit 1
fi
B_PENDING_COOKIE="$(cookie_value_from_jar "${JAR_B}" "totp_pending")"
if [[ -z "${B_PENDING_COOKIE}" ]]; then
  echo "FAIL: /totp/enable did not set a totp_pending cookie — the verify step B needs cannot proceed" >&2
  exit 1
fi

B_CODE="$(python3 "${WORK}/totp_compute.py" "${B_TOTP_SECRET}")"
VERIFY_B_BODY="${WORK}/b-totp-verify.json"
VERIFY_B_PAYLOAD="$(json_body code "${B_CODE}")"
VERIFY_B_CODE="$(curl -sS -o "${VERIFY_B_BODY}" -w '%{http_code}' \
  -X POST "${BASE}${B_AUTH_BASE_PATH}/totp/verify" \
  -H 'Content-Type: application/json' \
  -H "${B_CSRF_HEADER}: ${B_CSRF_TOKEN}" \
  -H "Origin: ${BASE}" \
  -H "Cookie: totp_pending=${B_PENDING_COOKIE}" \
  -d "${VERIFY_B_PAYLOAD}")"
if [[ "${VERIFY_B_CODE}" != "200" ]]; then
  echo "FAIL: identity B POST /totp/verify (enrollment) returned HTTP ${VERIFY_B_CODE}" >&2
  cat "${VERIFY_B_BODY}" >&2
  exit 1
fi
echo "==> identity B TOTP enrollment complete (the mandatory leg of D-15's first login)"
echo "==> identity B: no headless password-change path exists in this build (measured — see header comment); not exercised, recorded honestly"

# MEASURED (2026-09-08): Aura's session config sets UpdateAge == ExpiresIn (both
# sessionAbsoluteTTL, internal/webauth/authula.go's WithSession call) — a sliding renewal
# window as wide as the session lifetime itself, so validateSessionHook renews (deletes the
# old session row, issues a new cookie) on EVERY request it runs for. My own RouteMappings
# fix above is what makes it run for /totp/enable at all; the response's Set-Cookie rotated
# COOKIE_B out from under the bash variable captured before that call. -c "${JAR_B}" already
# wrote the new cookie to the jar (a live dry run caught the stale-variable 401 on the very
# next authenticated call, /api/conversations); re-reading here is the fix.
COOKIE_B="$(cookie_header_from_jar "${JAR_B}")"

# ============================================================================================
# section 6: one thread per identity, owned by her (D-06 owner-scoped resolution)
# ============================================================================================
create_thread() {
  local cookie="$1" body code idem
  body="${WORK}/thread-$$-${RANDOM}.json"
  idem="musr-live-run-thread-$(python3 -c 'import uuid; print(uuid.uuid4())')"
  code="$(curl -sS -o "${body}" -w '%{http_code}' -X POST "${BASE}/api/conversations" \
    -H "Cookie: ${cookie}" -H 'Content-Type: application/json' \
    -H "Idempotency-Key: ${idem}" -d '{}')"
  if [[ "${code}" != "201" && "${code}" != "200" ]]; then
    echo "FAIL: POST /api/conversations returned HTTP ${code}" >&2
    cat "${body}" >&2
    exit 1
  fi
  python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['ID'])" "${body}"
}
THREAD_A="$(create_thread "${COOKIE_A}")"
THREAD_B="$(create_thread "${COOKIE_B}")"
echo "==> threads created: A=${THREAD_A} B=${THREAD_B}"

# ============================================================================================
# section 7: seed each identity's own document — a small marker file naming its own token, so
# document_search has real per-identity data to find (D-17: symmetric, self-owned, named).
# ============================================================================================
TOKEN_A="MUSR-A-$(python3 -c 'import secrets; print(secrets.token_hex(4))')"
TOKEN_B="MUSR-B-$(python3 -c 'import secrets; print(secrets.token_hex(4))')"

upload_marker() {
  local cookie="$1" token="$2" file="$3" code body
  cat >"${file}" <<EOF
Documento di test per la run live a due identita' (Phase 01 Plan 06, artifacts/musr-live-run).
Il codice segreto per questa identita' e': ${token}
EOF
  body="${WORK}/upload-$$-${RANDOM}.json"
  code="$(curl -sS -o "${body}" -w '%{http_code}' \
    -X POST "${BASE}/api/filemanager/upload?id=musr-live-run" \
    -H "Cookie: ${cookie}" \
    -F "file=@${file};filename=musr-live-run-marker.txt;type=text/plain")"
  if [[ "${code}" != "200" && "${code}" != "201" ]]; then
    echo "FAIL: file-manager upload returned HTTP ${code}" >&2
    cat "${body}" >&2
    exit 1
  fi
}
upload_marker "${COOKIE_A}" "${TOKEN_A}" "${WORK}/marker-a.txt"
upload_marker "${COOKIE_B}" "${TOKEN_B}" "${WORK}/marker-b.txt"
echo "==> seed documents uploaded: A token=${TOKEN_A} B token=${TOKEN_B}"

# ============================================================================================
# section 8: wait for the async multi-tenant ingest sweep. compose.yaml's aura-ingest service
# runs a supervisor (AURA_INGEST_SUPERVISOR_INTERVAL=15s) that discovers per-identity
# databases and a per-tenant ingest cycle (AURA_INGEST_INTERVAL_SEC=60s) — there is no HTTP
# readiness signal for "this identity's upload is now searchable", so this wait is a measured
# accommodation of that real latency, not a proof of a bounded ingest SLA. The actual elapsed
# time before document_search first found each marker is recorded in the evidence file from
# the transcript timestamps, not asserted here.
# ============================================================================================
DOC_INGEST_WAIT_SEC="${MUSR_DOC_INGEST_WAIT_SEC:-90}"
echo "==> waiting ${DOC_INGEST_WAIT_SEC}s for the async ingest sweep to pick up both identities' new documents"
sleep "${DOC_INGEST_WAIT_SEC}"

if [[ "${MUSR_SKIP_CONVERSATIONS:-0}" == "1" ]]; then
  echo "==> MUSR_SKIP_CONVERSATIONS=1: stopping before the scored conversations (dry-run — no model turn spent)"
  echo "==> identity A: thread=${THREAD_A} token=${TOKEN_A}"
  echo "==> identity B: thread=${THREAD_B} token=${TOKEN_B} id=${IDENTITY_B_ID}"
  exit 0
fi

# ============================================================================================
# section 9: two concurrent POST /agent/run SSE conversations, released together as background
# jobs. Each identity performs the SAME three tasks on her own data (D-17): find her marker
# via document_search, record it via the memory tool, and confirm it via a sandbox command.
# ============================================================================================
CONV_DRIVER="scripts/musr_live_run_conversation.py"
# MEASURED (2026-09-08): the first real scored run completed both conversations correctly
# (tokens, tools, no leak) but failed ONLY the overlap assertion — the transcripts show
# genuine progress interleaving across the whole run (B's tools, then A's, then B's again,
# spanning the same ~19s window), but every individual tool call is sub-second, so no two
# calls' [start,end] intervals literally overlapped even though the underlying infrastructure
# was genuinely shared throughout. Widening the sandbox task's own duration (a `sleep`, which
# costs no LLM/GPU time) is what D-17's own "time the three tasks to overlap deliberately"
# asks the harness to arrange — it does not fabricate overlap, it gives real concurrent
# execution enough width for the ts-based check to resolve it.
PROMPT="Esegui questi tre passi con i tuoi strumenti reali, nell'ordine che preferisci: (1) cerca nei miei documenti il file 'musr-live-run-marker.txt' e leggi il codice segreto che contiene; (2) registra in memoria un fatto che riporti quel codice esatto; (3) esegui nel sandbox il comando shell 'sleep 3 && echo <codice>' sostituendo <codice> con il codice trovato, e osservane l'output. Alla fine scrivi una risposta breve che ripeta per intero il codice trovato."

TRANSCRIPT_A="${RUN_DIR}/transcript-a.jsonl"
TRANSCRIPT_B="${RUN_DIR}/transcript-b.jsonl"
TIMING_A="${WORK}/timings-a.jsonl"
TIMING_B="${WORK}/timings-b.jsonl"
: >"${TIMING_A}"
: >"${TIMING_B}"

echo "==> starting two concurrent /agent/run conversations (released together)"
CONV_A_STATUS_FILE="${WORK}/conv-a.status"
CONV_B_STATUS_FILE="${WORK}/conv-b.status"
(
  python3 "${CONV_DRIVER}" "${BASE}" "${COOKIE_A}" "${THREAD_A}" "${PROMPT}" "${TRANSCRIPT_A}" "${TIMING_A}" "a"
  echo $? >"${CONV_A_STATUS_FILE}"
) &
CONV_A_PID=$!
(
  python3 "${CONV_DRIVER}" "${BASE}" "${COOKIE_B}" "${THREAD_B}" "${PROMPT}" "${TRANSCRIPT_B}" "${TIMING_B}" "b"
  echo $? >"${CONV_B_STATUS_FILE}"
) &
CONV_B_PID=$!
wait "${CONV_A_PID}" "${CONV_B_PID}" || true
CONV_A_STATUS="$(cat "${CONV_A_STATUS_FILE}" 2>/dev/null || echo 1)"
CONV_B_STATUS="$(cat "${CONV_B_STATUS_FILE}" 2>/dev/null || echo 1)"
echo "==> conversation A exit=${CONV_A_STATUS} conversation B exit=${CONV_B_STATUS}"

# ---- section 10: merge the two per-identity timing files, sorted by ts ----------------------
python3 scripts/musr_live_run_merge_timings.py "${TIMING_A}" "${TIMING_B}" "${RUN_DIR}/timings.jsonl"

# ---- section 11: the blocking machine-checkable half (D-18) ---------------------------------
echo "==> running the blocking assertion set"
set +e
go run ./scripts/musr_live_run_assert.go --transcripts "${RUN_DIR}"
ASSERT_STATUS=$?
set -e

echo "==> artifacts:"
echo "    ${TRANSCRIPT_A}"
echo "    ${TRANSCRIPT_B}"
echo "    ${RUN_DIR}/timings.jsonl"
echo "    ${RUN_DIR}/daemon.log"
echo "    ${RUN_DIR}/identities.env"

exit "${ASSERT_STATUS}"
